package proxy

import (
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"net/http"

	"github.com/google/uuid"

	"github.com/watchword/watchword/internal/domain"
	"github.com/watchword/watchword/internal/repository"
	s3client "github.com/watchword/watchword/internal/s3"
)

// UploadHandler accepts proxied file uploads with HMAC-signed URL validation.
type UploadHandler struct {
	secret      string
	streamer    s3client.Streamer
	repo        repository.Repository
	maxFileSize int64
	logger      *slog.Logger
}

func NewUploadHandler(secret string, streamer s3client.Streamer, repo repository.Repository, maxFileSize int64, logger *slog.Logger) *UploadHandler {
	return &UploadHandler{
		secret:      secret,
		streamer:    streamer,
		repo:        repo,
		maxFileSize: maxFileSize,
		logger:      logger,
	}
}

func (h *UploadHandler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPut && r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}

	entryID, filename, err := ValidateSignature(h.secret, r.URL.Query())
	if err != nil {
		if errors.Is(err, domain.ErrProxyURLExpired) {
			http.Error(w, "upload link has expired", http.StatusGone)
			return
		}
		http.Error(w, "invalid upload link", http.StatusForbidden)
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

	// Verify filename matches
	if meta.Filename != filename {
		http.Error(w, "filename mismatch", http.StatusBadRequest)
		return
	}

	// Enforce size limit
	if r.ContentLength > h.maxFileSize {
		http.Error(w, "file too large", http.StatusRequestEntityTooLarge)
		return
	}

	// Wrap body with a size-limited reader to prevent abuse
	limitedBody := io.LimitReader(r.Body, h.maxFileSize+1)

	contentType := r.Header.Get("Content-Type")
	if contentType == "" {
		contentType = meta.ContentType
	}

	// Stream to S3
	if err := h.streamer.PutObject(r.Context(), meta.S3Key, limitedBody, contentType, r.ContentLength); err != nil {
		h.logger.Error("failed to put S3 object", "entry_id", entryID, "s3_key", meta.S3Key, "error", err)
		http.Error(w, "failed to store file", http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	json.NewEncoder(w).Encode(map[string]string{
		"status":   "ok",
		"entry_id": entryID,
		"filename": filename,
	})

	h.logger.Info("proxy upload completed", "entry_id", entryID, "filename", filename)
}
