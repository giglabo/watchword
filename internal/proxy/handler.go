package proxy

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"

	"github.com/google/uuid"

	"github.com/watchword/watchword/internal/domain"
	"github.com/watchword/watchword/internal/repository"
	s3client "github.com/watchword/watchword/internal/s3"
)

// Handler serves proxied file downloads with HMAC-signed URL validation.
type Handler struct {
	secret   string
	streamer s3client.Streamer
	repo     repository.Repository
	logger   *slog.Logger
}

func NewHandler(secret string, streamer s3client.Streamer, repo repository.Repository, logger *slog.Logger) *Handler {
	return &Handler{
		secret:   secret,
		streamer: streamer,
		repo:     repo,
		logger:   logger,
	}
}

func (h *Handler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}

	entryID, filename, err := ValidateSignature(h.secret, r.URL.Query())
	if err != nil {
		if errors.Is(err, domain.ErrProxyURLExpired) {
			http.Error(w, "download link has expired", http.StatusGone)
			return
		}
		http.Error(w, "invalid download link", http.StatusForbidden)
		return
	}

	// Look up entry
	id, err := uuid.Parse(entryID)
	if err != nil {
		http.Error(w, "invalid entry ID", http.StatusBadRequest)
		return
	}

	entry, err := h.repo.GetByID(r.Context(), id)
	if err != nil {
		if errors.Is(err, domain.ErrNotFound) {
			http.Error(w, "entry not found", http.StatusNotFound)
			return
		}
		h.logger.Error("failed to look up entry", "entry_id", entryID, "error", err)
		http.Error(w, "internal error", http.StatusInternalServerError)
		return
	}

	if entry.EntryType != domain.EntryTypeFile {
		http.Error(w, "entry is not a file", http.StatusBadRequest)
		return
	}

	var meta domain.FileMetadata
	if err := json.Unmarshal([]byte(entry.Payload), &meta); err != nil {
		h.logger.Error("failed to parse file metadata", "entry_id", entryID, "error", err)
		http.Error(w, "internal error", http.StatusInternalServerError)
		return
	}

	// Stream from S3
	body, contentType, contentLength, err := h.streamer.GetObject(r.Context(), meta.S3Key)
	if err != nil {
		h.logger.Error("failed to get S3 object", "entry_id", entryID, "s3_key", meta.S3Key, "error", err)
		http.Error(w, "failed to retrieve file", http.StatusInternalServerError)
		return
	}
	defer body.Close()

	// Set response headers
	if contentType == "" {
		contentType = meta.ContentType
	}
	w.Header().Set("Content-Type", contentType)
	w.Header().Set("Content-Disposition", fmt.Sprintf(`attachment; filename="%s"`, meta.Filename))
	if contentLength > 0 {
		w.Header().Set("Content-Length", fmt.Sprintf("%d", contentLength))
	}
	w.Header().Set("Cache-Control", "no-store")

	if _, err := io.Copy(w, body); err != nil {
		h.logger.Error("failed to stream file", "entry_id", entryID, "error", err)
		return
	}

	// Record download best-effort in background
	go func() {
		clientIP := r.RemoteAddr
		userAgent := r.UserAgent()
		if err := h.repo.RecordDownload(r.Context(), id, entry.Word, filename, clientIP, userAgent); err != nil {
			h.logger.Warn("failed to record download", "entry_id", entryID, "error", err)
		}
	}()
}
