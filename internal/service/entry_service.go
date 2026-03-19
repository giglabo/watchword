package service

import (
	"context"
	"fmt"
	"log/slog"
	"strings"
	"time"
	"unicode"
	"unicode/utf8"

	"github.com/google/uuid"

	"github.com/watchword/watchword/internal/domain"
	"github.com/watchword/watchword/internal/repository"
)

func validatePattern(pattern string) error {
	if pattern == "" || utf8.RuneCountInString(pattern) > domain.MaxPatternLength {
		return domain.ErrInvalidPattern
	}
	// Reject wildcard-only patterns
	trimmed := strings.ReplaceAll(strings.ReplaceAll(pattern, "%", ""), "_", "")
	if trimmed == "" {
		return domain.ErrInvalidPattern
	}
	// No control characters
	for _, r := range pattern {
		if unicode.IsControl(r) {
			return domain.ErrInvalidPattern
		}
	}
	return nil
}

type StoreResult struct {
	Entry             *domain.Entry `json:"entry"`
	CollisionResolved bool          `json:"collision_resolved"`
	OriginalWord      string        `json:"original_word,omitempty"`
}

type RestoreResult struct {
	Entry             *domain.Entry `json:"entry"`
	CollisionResolved bool          `json:"collision_resolved"`
	OriginalWord      string        `json:"original_word,omitempty"`
}

type EntryService struct {
	repo       repository.Repository
	defaultTTL int // hours
	logger     *slog.Logger
}

func NewEntryService(repo repository.Repository, defaultTTLHours int, logger *slog.Logger) *EntryService {
	return &EntryService{
		repo:       repo,
		defaultTTL: defaultTTLHours,
		logger:     logger,
	}
}

func (s *EntryService) StoreEntry(ctx context.Context, word string, payload string, ttlHours *int) (*StoreResult, error) {
	word = strings.TrimSpace(word)
	if err := domain.ValidateWord(word); err != nil {
		return nil, err
	}
	if len(payload) == 0 {
		return nil, domain.ErrPayloadEmpty
	}
	if len(payload) > domain.MaxPayloadSize {
		return nil, domain.ErrPayloadTooLarge
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

	var result *StoreResult
	err := s.repo.WithTx(ctx, func(txRepo repository.Repository) error {
		resolvedWord, collision, err := resolveWord(ctx, txRepo, word)
		if err != nil {
			return err
		}

		entry := &domain.Entry{
			Word:      resolvedWord,
			Payload:   payload,
			ExpiresAt: expiresAt,
		}
		created, err := txRepo.Store(ctx, entry)
		if err != nil {
			return err
		}

		result = &StoreResult{
			Entry:             created,
			CollisionResolved: collision,
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

func (s *EntryService) GetEntry(ctx context.Context, idStr string) (*domain.Entry, error) {
	id, err := uuid.Parse(idStr)
	if err != nil {
		return nil, domain.ErrInvalidUUID
	}
	return s.repo.GetByID(ctx, id)
}

func (s *EntryService) GetEntryByWord(ctx context.Context, word string, includeExpired bool) (*domain.Entry, error) {
	word = strings.TrimSpace(word)
	if word == "" {
		return nil, domain.ErrInvalidWord
	}
	return s.repo.GetByWord(ctx, word, includeExpired)
}

func (s *EntryService) SearchEntries(ctx context.Context, pattern string, status string, limit int, offset int) ([]*domain.Entry, int, error) {
	if err := validatePattern(pattern); err != nil {
		return nil, 0, err
	}
	if status == "" {
		status = "active"
	}
	if limit <= 0 || limit > domain.MaxLimit {
		limit = domain.DefaultLimit
	}
	if offset < 0 {
		offset = 0
	}
	return s.repo.SearchByLike(ctx, pattern, status, limit, offset)
}

func (s *EntryService) ListEntries(ctx context.Context, status string, limit int, offset int, sortBy string, sortOrder string) ([]*domain.Entry, int, error) {
	if status == "" {
		status = "active"
	}
	if limit <= 0 || limit > domain.MaxLimit {
		limit = domain.DefaultLimit
	}
	if offset < 0 {
		offset = 0
	}
	return s.repo.List(ctx, status, limit, offset, sortBy, sortOrder)
}

func (s *EntryService) RestoreEntry(ctx context.Context, idStr string, newTTLHours *int) (*RestoreResult, error) {
	id, err := uuid.Parse(idStr)
	if err != nil {
		return nil, domain.ErrInvalidUUID
	}

	ttl := s.defaultTTL
	if newTTLHours != nil {
		if *newTTLHours < 0 || *newTTLHours > domain.MaxTTLHours {
			return nil, domain.ErrInvalidTTL
		}
		ttl = *newTTLHours
	}

	var expiresAt *time.Time
	if ttl > 0 {
		t := time.Now().UTC().Add(time.Duration(ttl) * time.Hour)
		expiresAt = &t
	}

	var result *RestoreResult
	err = s.repo.WithTx(ctx, func(txRepo repository.Repository) error {
		entry, err := txRepo.GetByID(ctx, id)
		if err != nil {
			return err
		}
		if entry.Status == domain.StatusActive {
			return domain.ErrAlreadyActive
		}

		resolvedWord, collision, err := resolveWord(ctx, txRepo, entry.Word)
		if err != nil {
			return err
		}

		if err := txRepo.UpdateStatus(ctx, id, string(domain.StatusActive), resolvedWord, expiresAt); err != nil {
			return err
		}

		entry.Status = domain.StatusActive
		entry.Word = resolvedWord
		entry.ExpiresAt = expiresAt
		entry.UpdatedAt = time.Now().UTC()

		result = &RestoreResult{
			Entry:             entry,
			CollisionResolved: collision,
		}
		if collision {
			result.OriginalWord = entry.Word
		}
		return nil
	})
	if err != nil {
		return nil, err
	}
	return result, nil
}

func (s *EntryService) DeleteEntry(ctx context.Context, idOrWord string) error {
	// Try UUID first
	id, err := uuid.Parse(idOrWord)
	if err == nil {
		return s.repo.Delete(ctx, id)
	}

	// Not a UUID — try to find by word
	word := strings.TrimSpace(idOrWord)
	if word == "" {
		return domain.ErrInvalidWord
	}
	entry, err := s.repo.GetByWord(ctx, word, false)
	if err != nil {
		return err
	}
	return s.repo.Delete(ctx, entry.ID)
}

func resolveWord(ctx context.Context, repo repository.Repository, baseWord string) (string, bool, error) {
	exists, err := repo.WordExistsActive(ctx, baseWord)
	if err != nil {
		return "", false, fmt.Errorf("checking word existence: %w", err)
	}
	if !exists {
		return baseWord, false, nil
	}

	for i := 2; i <= domain.MaxCollisionAttempts; i++ {
		candidate := fmt.Sprintf("%s%d", baseWord, i)
		exists, err := repo.WordExistsActive(ctx, candidate)
		if err != nil {
			return "", false, fmt.Errorf("checking word existence: %w", err)
		}
		if !exists {
			return candidate, true, nil
		}
	}
	return "", false, domain.ErrCollisionLimitExceeded
}
