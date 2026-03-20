package repository

import (
	"context"
	"time"

	"github.com/google/uuid"
	"github.com/watchword/watchword/internal/domain"
)

type Repository interface {
	Store(ctx context.Context, entry *domain.Entry) (*domain.Entry, error)
	GetByID(ctx context.Context, id uuid.UUID) (*domain.Entry, error)
	GetByWord(ctx context.Context, word string, includeExpired bool) (*domain.Entry, error)
	SearchByLike(ctx context.Context, pattern string, status string, limit int, offset int) ([]*domain.Entry, int, error)
	List(ctx context.Context, status string, limit int, offset int, sortBy string, sortOrder string) ([]*domain.Entry, int, error)
	UpdateStatus(ctx context.Context, id uuid.UUID, newStatus string, newWord string, expiresAt *time.Time) error
	Delete(ctx context.Context, id uuid.UUID) error
	MarkExpiredBatch(ctx context.Context, batchSize int) (int, error)
	WordExistsActive(ctx context.Context, word string) (bool, error)
	RecordDownload(ctx context.Context, entryID uuid.UUID, word, filename, clientIP, userAgent string) error
	CleanDownloadHistory(ctx context.Context, olderThan time.Time) (int, error)
	WithTx(ctx context.Context, fn func(Repository) error) error
	Ping(ctx context.Context) error
	Migrate(ctx context.Context) error
	Close() error
}

var allowedSortColumns = map[string]bool{
	"created_at": true,
	"updated_at": true,
	"word":       true,
}

func ValidateSortBy(sortBy string) string {
	if allowedSortColumns[sortBy] {
		return sortBy
	}
	return "created_at"
}

func ValidateSortOrder(sortOrder string) string {
	if sortOrder == "asc" {
		return "ASC"
	}
	return "DESC"
}
