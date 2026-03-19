package service

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"strings"
	"time"

	"github.com/watchword/watchword/internal/domain"
	"github.com/watchword/watchword/internal/repository"
	s3client "github.com/watchword/watchword/internal/s3"
)

type FileService struct {
	repo         repository.Repository
	presigner    s3client.Presigner
	defaultTTL   int // hours
	maxFileSize  int64
	logger       *slog.Logger
}

func NewFileService(repo repository.Repository, presigner s3client.Presigner, defaultTTLHours int, maxFileSize int64, logger *slog.Logger) *FileService {
	return &FileService{
		repo:        repo,
		presigner:   presigner,
		defaultTTL:  defaultTTLHours,
		maxFileSize: maxFileSize,
		logger:      logger,
	}
}

type UploadResult struct {
	Word              string `json:"word"`
	ID                string `json:"id"`
	UploadURL         string `json:"upload_url"`
	Filename          string `json:"filename"`
	ContentType       string `json:"content_type"`
	MaxSize           int64  `json:"max_size_bytes"`
	CollisionResolved bool   `json:"collision_resolved"`
	OriginalWord      string `json:"original_word,omitempty"`
	ExpiresAt         string `json:"expires_at,omitempty"`
	Hint              string `json:"hint"`
}

type DownloadResult struct {
	Word        string `json:"word"`
	DownloadURL string `json:"download_url"`
	Filename    string `json:"filename"`
	ContentType string `json:"content_type"`
	Hint        string `json:"hint"`
}

func (s *FileService) UploadFile(ctx context.Context, word string, filename string, contentType string, ttlHours *int) (*UploadResult, error) {
	word = strings.TrimSpace(word)
	if err := domain.ValidateWord(word); err != nil {
		return nil, err
	}
	if err := domain.ValidateFilename(filename); err != nil {
		return nil, err
	}

	if contentType == "" {
		contentType = "application/octet-stream"
	}

	ttl := s.defaultTTL
	if ttlHours != nil {
		if *ttlHours < 0 || *ttlHours > domain.MaxTTLHours {
			return nil, domain.ErrInvalidTTL
		}
		ttl = *ttlHours
	}

	var expiresAt *time.Time
	if ttl > 0 {
		t := time.Now().UTC().Add(time.Duration(ttl) * time.Hour)
		expiresAt = &t
	}

	var result *UploadResult
	err := s.repo.WithTx(ctx, func(txRepo repository.Repository) error {
		resolvedWord, collision, err := resolveWord(ctx, txRepo, word)
		if err != nil {
			return err
		}

		// Create a new entry UUID for the S3 key
		entry := &domain.Entry{
			Word:      resolvedWord,
			EntryType: domain.EntryTypeFile,
			ExpiresAt: expiresAt,
		}

		// Store first to get the generated UUID
		created, err := txRepo.Store(ctx, entry)
		if err != nil {
			return err
		}

		s3Key := fmt.Sprintf("%s/%s", created.ID.String(), filename)
		meta := domain.FileMetadata{
			S3Key:       s3Key,
			Filename:    filename,
			ContentType: contentType,
			SizeLimit:   s.maxFileSize,
		}
		metaJSON, err := json.Marshal(meta)
		if err != nil {
			return fmt.Errorf("marshalling file metadata: %w", err)
		}

		// Update the payload with metadata (using UpdateStatus to write the word back with payload)
		// We need to set the payload — but UpdateStatus doesn't touch payload.
		// Instead, delete and re-store with the correct payload.
		if err := txRepo.Delete(ctx, created.ID); err != nil {
			return err
		}
		created.Payload = string(metaJSON)
		created, err = txRepo.Store(ctx, created)
		if err != nil {
			return err
		}

		// Generate presigned PUT URL
		uploadURL, err := s.presigner.PresignPUT(ctx, s3Key, contentType, s.maxFileSize)
		if err != nil {
			return fmt.Errorf("generating upload URL: %w", err)
		}

		result = &UploadResult{
			Word:              created.Word,
			ID:                created.ID.String(),
			UploadURL:         uploadURL,
			Filename:          filename,
			ContentType:       contentType,
			MaxSize:           s.maxFileSize,
			CollisionResolved: collision,
			Hint:              fmt.Sprintf("Upload your file with: curl -X PUT -H 'Content-Type: %s' -T '%s' '%s'", contentType, filename, uploadURL),
		}
		if expiresAt != nil {
			result.ExpiresAt = expiresAt.Format(time.RFC3339)
		}
		if collision {
			result.OriginalWord = word
		}
		return nil
	})
	if err != nil {
		return nil, err
	}
	return result, nil
}

// DeleteFileObject deletes the S3 object for a file entry. Best-effort: logs
// errors but does not block entry deletion.
func (s *FileService) DeleteFileObject(ctx context.Context, entry *domain.Entry) {
	if entry.EntryType != domain.EntryTypeFile {
		return
	}
	var meta domain.FileMetadata
	if err := json.Unmarshal([]byte(entry.Payload), &meta); err != nil {
		s.logger.Warn("failed to parse file metadata for S3 cleanup", "id", entry.ID, "error", err)
		return
	}
	if err := s.presigner.DeleteObject(ctx, meta.S3Key); err != nil {
		s.logger.Warn("failed to delete S3 object", "id", entry.ID, "s3_key", meta.S3Key, "error", err)
		return
	}
	s.logger.Info("deleted S3 object", "id", entry.ID, "s3_key", meta.S3Key)
}

func (s *FileService) DownloadFile(ctx context.Context, word string) (*DownloadResult, error) {
	word = strings.TrimSpace(word)
	if word == "" {
		return nil, domain.ErrInvalidWord
	}

	entry, err := s.repo.GetByWord(ctx, word, false)
	if err != nil {
		return nil, err
	}

	if entry.EntryType != domain.EntryTypeFile {
		return nil, domain.ErrNotAFileEntry
	}

	var meta domain.FileMetadata
	if err := json.Unmarshal([]byte(entry.Payload), &meta); err != nil {
		return nil, fmt.Errorf("invalid file metadata: %w", err)
	}

	downloadURL, err := s.presigner.PresignGET(ctx, meta.S3Key, meta.Filename)
	if err != nil {
		return nil, fmt.Errorf("generating download URL: %w", err)
	}

	return &DownloadResult{
		Word:        entry.Word,
		DownloadURL: downloadURL,
		Filename:    meta.Filename,
		ContentType: meta.ContentType,
		Hint:        fmt.Sprintf("Download with: curl -o '%s' '%s'", meta.Filename, downloadURL),
	}, nil
}
