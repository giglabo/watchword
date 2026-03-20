package worker

import (
	"context"
	"log/slog"
	"time"

	"github.com/watchword/watchword/internal/repository"
)

const batchSize = 500

type ExpirationWorker struct {
	repo                 repository.Repository
	interval             time.Duration
	historyRetentionDays int
	logger               *slog.Logger
}

func NewExpirationWorker(repo repository.Repository, intervalHours int, historyRetentionDays int, logger *slog.Logger) *ExpirationWorker {
	return &ExpirationWorker{
		repo:                 repo,
		interval:             time.Duration(intervalHours) * time.Hour,
		historyRetentionDays: historyRetentionDays,
		logger:               logger,
	}
}

func (w *ExpirationWorker) Start(ctx context.Context) {
	ticker := time.NewTicker(w.interval)
	defer ticker.Stop()

	// Run once on startup
	w.runWithRetry(ctx)

	for {
		select {
		case <-ctx.Done():
			w.logger.Info("expiration worker shutting down")
			return
		case <-ticker.C:
			w.runWithRetry(ctx)
		}
	}
}

func (w *ExpirationWorker) runWithRetry(ctx context.Context) {
	var totalExpired int
	maxRetries := 3

	for {
		var lastErr error
		var expired int

		for attempt := 0; attempt < maxRetries; attempt++ {
			if ctx.Err() != nil {
				return
			}

			var err error
			expired, err = w.repo.MarkExpiredBatch(ctx, batchSize)
			if err != nil {
				lastErr = err
				w.logger.Error("expiration batch failed", "attempt", attempt+1, "error", err)
				backoff := time.Duration(1<<uint(attempt)) * time.Second
				select {
				case <-ctx.Done():
					return
				case <-time.After(backoff):
				}
				continue
			}
			lastErr = nil
			break
		}

		if lastErr != nil {
			w.logger.Error("expiration batch exhausted retries", "error", lastErr)
			return
		}

		totalExpired += expired
		if expired < batchSize {
			break
		}
	}

	if totalExpired > 0 {
		w.logger.Info("expiration worker completed", "expired_count", totalExpired)
	}

	// Clean up old download history
	if w.historyRetentionDays > 0 {
		cutoff := time.Now().UTC().AddDate(0, 0, -w.historyRetentionDays)
		cleaned, err := w.repo.CleanDownloadHistory(ctx, cutoff)
		if err != nil {
			w.logger.Error("failed to clean download history", "error", err)
		} else if cleaned > 0 {
			w.logger.Info("cleaned download history", "deleted_count", cleaned)
		}
	}
}
