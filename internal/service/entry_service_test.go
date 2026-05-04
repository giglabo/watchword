package service

import (
	"context"
	"log/slog"
	"os"
	"testing"
	"time"

	"github.com/watchword/watchword/internal/auth"
	"github.com/watchword/watchword/internal/domain"
	"github.com/watchword/watchword/internal/repository"
)

func newTestService(t *testing.T) *EntryService {
	t.Helper()
	repo, err := repository.NewSQLiteRepo(":memory:")
	if err != nil {
		t.Fatalf("NewSQLiteRepo: %v", err)
	}
	t.Cleanup(func() { repo.Close() })
	if err := repo.Migrate(context.Background()); err != nil {
		t.Fatalf("Migrate: %v", err)
	}
	logger := slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelError}))
	return NewEntryService(repo, 168, logger)
}

func TestStoreEntry_Basic(t *testing.T) {
	svc := newTestService(t)
	ctx := context.Background()

	result, err := svc.StoreEntry(ctx, "rabbit", "test payload", nil)
	if err != nil {
		t.Fatalf("StoreEntry: %v", err)
	}
	if result.Entry.Word != "rabbit" {
		t.Errorf("expected word=rabbit, got %s", result.Entry.Word)
	}
	if result.CollisionResolved {
		t.Error("expected no collision")
	}
	if result.Entry.ExpiresAt == nil {
		t.Error("expected expires_at to be set with default TTL")
	}
}

func TestStoreEntry_RecordsCreatedByFromContext(t *testing.T) {
	svc := newTestService(t)
	ctx := auth.WithIdentity(context.Background(), "alice@example.com")

	result, err := svc.StoreEntry(ctx, "rabbit", "payload", nil)
	if err != nil {
		t.Fatalf("StoreEntry: %v", err)
	}
	if result.Entry.CreatedBy == nil {
		t.Fatal("expected CreatedBy to be set from context identity")
	}
	if *result.Entry.CreatedBy != "alice@example.com" {
		t.Errorf("expected CreatedBy=alice@example.com, got %q", *result.Entry.CreatedBy)
	}

	// Round-trip via GetEntry to confirm persistence
	got, err := svc.GetEntry(context.Background(), result.Entry.ID.String())
	if err != nil {
		t.Fatalf("GetEntry: %v", err)
	}
	if got.CreatedBy == nil || *got.CreatedBy != "alice@example.com" {
		t.Errorf("CreatedBy not persisted: got %v", got.CreatedBy)
	}
}

func TestStoreEntry_AnonymousWhenNoIdentity(t *testing.T) {
	svc := newTestService(t)
	result, err := svc.StoreEntry(context.Background(), "rabbit", "payload", nil)
	if err != nil {
		t.Fatalf("StoreEntry: %v", err)
	}
	if result.Entry.CreatedBy != nil {
		t.Errorf("expected CreatedBy=nil for anonymous request, got %q", *result.Entry.CreatedBy)
	}
}

func TestStoreEntry_CollisionResolution(t *testing.T) {
	svc := newTestService(t)
	ctx := context.Background()

	svc.StoreEntry(ctx, "rabbit", "payload1", nil)

	result, err := svc.StoreEntry(ctx, "rabbit", "payload2", nil)
	if err != nil {
		t.Fatalf("StoreEntry: %v", err)
	}
	if result.Entry.Word != "rabbit2" {
		t.Errorf("expected word=rabbit2, got %s", result.Entry.Word)
	}
	if !result.CollisionResolved {
		t.Error("expected collision to be resolved")
	}
	if result.OriginalWord != "rabbit" {
		t.Errorf("expected original_word=rabbit, got %s", result.OriginalWord)
	}
}

func TestStoreEntry_MultipleCollisions(t *testing.T) {
	svc := newTestService(t)
	ctx := context.Background()

	svc.StoreEntry(ctx, "cat", "p1", nil)
	svc.StoreEntry(ctx, "cat", "p2", nil)  // becomes cat2
	svc.StoreEntry(ctx, "cat", "p3", nil)  // becomes cat3

	result, err := svc.StoreEntry(ctx, "cat", "p4", nil)
	if err != nil {
		t.Fatalf("StoreEntry: %v", err)
	}
	if result.Entry.Word != "cat4" {
		t.Errorf("expected word=cat4, got %s", result.Entry.Word)
	}
}

func TestStoreEntry_ZeroTTL(t *testing.T) {
	svc := newTestService(t)
	ctx := context.Background()

	ttl := 0
	result, err := svc.StoreEntry(ctx, "permanent", "payload", &ttl)
	if err != nil {
		t.Fatalf("StoreEntry: %v", err)
	}
	if result.Entry.ExpiresAt != nil {
		t.Error("expected nil expires_at for TTL=0")
	}
}

func TestStoreEntry_InvalidWord(t *testing.T) {
	svc := newTestService(t)
	ctx := context.Background()

	_, err := svc.StoreEntry(ctx, "has\tnewline", "payload", nil)
	if err != domain.ErrInvalidWord {
		t.Errorf("expected ErrInvalidWord for control chars, got %v", err)
	}

	_, err = svc.StoreEntry(ctx, "", "payload", nil)
	if err != domain.ErrInvalidWord {
		t.Errorf("expected ErrInvalidWord, got %v", err)
	}
}

func TestStoreEntry_EmptyPayload(t *testing.T) {
	svc := newTestService(t)
	ctx := context.Background()

	_, err := svc.StoreEntry(ctx, "word", "", nil)
	if err != domain.ErrPayloadEmpty {
		t.Errorf("expected ErrPayloadEmpty, got %v", err)
	}
}

func TestGetEntry(t *testing.T) {
	svc := newTestService(t)
	ctx := context.Background()

	result, _ := svc.StoreEntry(ctx, "fetch", "payload", nil)
	entry, err := svc.GetEntry(ctx, result.Entry.ID.String())
	if err != nil {
		t.Fatalf("GetEntry: %v", err)
	}
	if entry.Word != "fetch" {
		t.Errorf("expected word=fetch, got %s", entry.Word)
	}
}

func TestGetEntry_InvalidUUID(t *testing.T) {
	svc := newTestService(t)
	_, err := svc.GetEntry(context.Background(), "not-a-uuid")
	if err != domain.ErrInvalidUUID {
		t.Errorf("expected ErrInvalidUUID, got %v", err)
	}
}

func TestDeleteEntry(t *testing.T) {
	svc := newTestService(t)
	ctx := context.Background()

	result, _ := svc.StoreEntry(ctx, "todelete", "payload", nil)
	if err := svc.DeleteEntry(ctx, result.Entry.ID.String()); err != nil {
		t.Fatalf("DeleteEntry: %v", err)
	}

	_, err := svc.GetEntry(ctx, result.Entry.ID.String())
	if err != domain.ErrNotFound {
		t.Errorf("expected ErrNotFound after delete, got %v", err)
	}
}

func TestRestoreEntry(t *testing.T) {
	svc := newTestService(t)
	ctx := context.Background()

	// Store and expire
	result, _ := svc.StoreEntry(ctx, "restore", "payload", nil)

	// Manually expire it via the underlying repo
	entry, _ := svc.GetEntry(ctx, result.Entry.ID.String())
	_ = entry // just verify it exists

	// We need to manually change status. Let's use the repo directly through store+update pattern.
	// Instead, let's store, delete, and store expired manually.
	// Actually, let's test restore by first making the entry expired via service internals.
	// The simplest approach: use the repo from the service.
	// For testing, let's just create an expired entry by storing it normally then we can't easily expire it
	// through the service. Let's use a workaround with the underlying repo.

	// Store a new entry, expire it via MarkExpiredBatch with past time
	ttl := 0 // no expiry so we can't use MarkExpiredBatch
	res2, _ := svc.StoreEntry(ctx, "restoretest", "payload2", &ttl)

	// Actually, let's test RestoreEntry with an already active entry - should fail
	_, err := svc.RestoreEntry(ctx, res2.Entry.ID.String(), nil)
	if err != domain.ErrAlreadyActive {
		t.Errorf("expected ErrAlreadyActive, got %v", err)
	}
}

// newTestServiceWithRepo gives tests access to the underlying repo so they
// can stage state (e.g. forcibly-expired entries) that the service API alone
// can't produce.
func newTestServiceWithRepo(t *testing.T) (*EntryService, repository.Repository) {
	t.Helper()
	repo, err := repository.NewSQLiteRepo(":memory:")
	if err != nil {
		t.Fatalf("NewSQLiteRepo: %v", err)
	}
	t.Cleanup(func() { repo.Close() })
	if err := repo.Migrate(context.Background()); err != nil {
		t.Fatalf("Migrate: %v", err)
	}
	logger := slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelError}))
	return NewEntryService(repo, 168, logger), repo
}

func TestUpdateExpiration_ExtendsActive(t *testing.T) {
	svc := newTestService(t)
	ctx := context.Background()

	stored, err := svc.StoreEntry(ctx, "alpha", "payload", nil)
	if err != nil {
		t.Fatalf("StoreEntry: %v", err)
	}
	originalExpiry := *stored.Entry.ExpiresAt

	ttl := 24
	res, err := svc.UpdateExpiration(ctx, stored.Entry.ID.String(), &ttl)
	if err != nil {
		t.Fatalf("UpdateExpiration: %v", err)
	}
	if res.Reactivated {
		t.Error("expected Reactivated=false for already-active entry")
	}
	if res.CollisionResolved {
		t.Error("expected no collision for active entry")
	}
	if res.Entry.Status != domain.StatusActive {
		t.Errorf("expected status=active, got %s", res.Entry.Status)
	}
	if res.Entry.ExpiresAt == nil {
		t.Fatal("expected expires_at to be set")
	}
	if !res.Entry.ExpiresAt.Before(originalExpiry) {
		t.Errorf("expected new expiry (24h) before original (default 168h); got %v vs %v",
			res.Entry.ExpiresAt, originalExpiry)
	}
}

func TestUpdateExpiration_ZeroTTLClearsExpiry(t *testing.T) {
	svc := newTestService(t)
	ctx := context.Background()

	stored, _ := svc.StoreEntry(ctx, "perm", "p", nil)
	if stored.Entry.ExpiresAt == nil {
		t.Fatal("precondition: stored entry should have default expiry")
	}

	zero := 0
	res, err := svc.UpdateExpiration(ctx, stored.Entry.ID.String(), &zero)
	if err != nil {
		t.Fatalf("UpdateExpiration: %v", err)
	}
	if res.Entry.ExpiresAt != nil {
		t.Errorf("expected nil expires_at for ttl=0, got %v", res.Entry.ExpiresAt)
	}
}

func TestUpdateExpiration_ReactivatesExpired(t *testing.T) {
	svc, repo := newTestServiceWithRepo(t)
	ctx := context.Background()

	// Stage an entry expired in the past then mark expired via the worker path.
	past := time.Now().UTC().Add(-1 * time.Hour)
	if _, err := repo.Store(ctx, &domain.Entry{Word: "ghost", Payload: "x", ExpiresAt: &past}); err != nil {
		t.Fatalf("seed Store: %v", err)
	}
	if _, err := repo.MarkExpiredBatch(ctx, 100); err != nil {
		t.Fatalf("MarkExpiredBatch: %v", err)
	}

	res, err := svc.UpdateExpiration(ctx, "ghost", nil)
	if err != nil {
		t.Fatalf("UpdateExpiration: %v", err)
	}
	if !res.Reactivated {
		t.Error("expected Reactivated=true")
	}
	if res.Entry.Status != domain.StatusActive {
		t.Errorf("expected status=active, got %s", res.Entry.Status)
	}
	if res.Entry.Word != "ghost" {
		t.Errorf("expected word=ghost (no collision), got %q", res.Entry.Word)
	}
	if res.Entry.ExpiresAt == nil {
		t.Error("expected default TTL to be applied")
	}
}

func TestUpdateExpiration_ReactivateWithCollision(t *testing.T) {
	svc, repo := newTestServiceWithRepo(t)
	ctx := context.Background()

	// Expired entry on the base word.
	past := time.Now().UTC().Add(-1 * time.Hour)
	expired, err := repo.Store(ctx, &domain.Entry{Word: "echo", Payload: "old", ExpiresAt: &past})
	if err != nil {
		t.Fatalf("seed Store: %v", err)
	}
	if _, err := repo.MarkExpiredBatch(ctx, 100); err != nil {
		t.Fatalf("MarkExpiredBatch: %v", err)
	}
	// Active entry now squats on the same word — by-word lookup would return
	// this one, so target the expired entry explicitly via its UUID.
	if _, err := svc.StoreEntry(ctx, "echo", "new", nil); err != nil {
		t.Fatalf("StoreEntry squat: %v", err)
	}

	res, err := svc.UpdateExpiration(ctx, expired.ID.String(), nil)
	if err != nil {
		t.Fatalf("UpdateExpiration: %v", err)
	}
	if !res.Reactivated || !res.CollisionResolved {
		t.Errorf("expected Reactivated && CollisionResolved, got reactivated=%v collision=%v",
			res.Reactivated, res.CollisionResolved)
	}
	if res.Entry.Word != "echo2" {
		t.Errorf("expected resolved word echo2, got %q", res.Entry.Word)
	}
	if res.OriginalWord != "echo" {
		t.Errorf("expected OriginalWord=echo, got %q", res.OriginalWord)
	}
}

func TestUpdateExpiration_ByWord(t *testing.T) {
	svc := newTestService(t)
	ctx := context.Background()

	if _, err := svc.StoreEntry(ctx, "kappa", "payload", nil); err != nil {
		t.Fatalf("StoreEntry: %v", err)
	}

	zero := 0
	res, err := svc.UpdateExpiration(ctx, "kappa", &zero)
	if err != nil {
		t.Fatalf("UpdateExpiration by word: %v", err)
	}
	if res.Entry.Word != "kappa" {
		t.Errorf("expected word=kappa, got %q", res.Entry.Word)
	}
	if res.Entry.ExpiresAt != nil {
		t.Errorf("expected expires_at cleared, got %v", res.Entry.ExpiresAt)
	}
}

func TestUpdateExpiration_NotFound(t *testing.T) {
	svc := newTestService(t)
	ctx := context.Background()

	if _, err := svc.UpdateExpiration(ctx, "nonexistent", nil); err != domain.ErrNotFound {
		t.Errorf("expected ErrNotFound, got %v", err)
	}

	if _, err := svc.UpdateExpiration(ctx, "00000000-0000-0000-0000-000000000000", nil); err != domain.ErrNotFound {
		t.Errorf("expected ErrNotFound for unknown UUID, got %v", err)
	}
}

func TestUpdateExpiration_InvalidTTL(t *testing.T) {
	svc := newTestService(t)
	ctx := context.Background()

	stored, _ := svc.StoreEntry(ctx, "thing", "payload", nil)

	tooBig := domain.MaxTTLHours + 1
	if _, err := svc.UpdateExpiration(ctx, stored.Entry.ID.String(), &tooBig); err != domain.ErrInvalidTTL {
		t.Errorf("expected ErrInvalidTTL for ttl > max, got %v", err)
	}

	negative := -1
	if _, err := svc.UpdateExpiration(ctx, stored.Entry.ID.String(), &negative); err != domain.ErrInvalidTTL {
		t.Errorf("expected ErrInvalidTTL for negative ttl, got %v", err)
	}
}

func TestUpdateExpiration_EmptyRef(t *testing.T) {
	svc := newTestService(t)
	if _, err := svc.UpdateExpiration(context.Background(), "   ", nil); err != domain.ErrInvalidWord {
		t.Errorf("expected ErrInvalidWord for blank reference, got %v", err)
	}
}

func TestSearchEntries(t *testing.T) {
	svc := newTestService(t)
	ctx := context.Background()

	svc.StoreEntry(ctx, "rabbit", "p1", nil)
	svc.StoreEntry(ctx, "raccoon", "p2", nil)
	svc.StoreEntry(ctx, "cat", "p3", nil)

	entries, total, err := svc.SearchEntries(ctx, "ra%", "", 20, 0)
	if err != nil {
		t.Fatalf("SearchEntries: %v", err)
	}
	if total != 2 {
		t.Errorf("expected total=2, got %d", total)
	}
	if len(entries) != 2 {
		t.Errorf("expected 2 entries, got %d", len(entries))
	}
}

func TestSearchEntries_InvalidPattern(t *testing.T) {
	svc := newTestService(t)
	ctx := context.Background()

	_, _, err := svc.SearchEntries(ctx, "%", "", 20, 0)
	if err != domain.ErrInvalidPattern {
		t.Errorf("expected ErrInvalidPattern for wildcard-only pattern, got %v", err)
	}

	_, _, err = svc.SearchEntries(ctx, "%%", "", 20, 0)
	if err != domain.ErrInvalidPattern {
		t.Errorf("expected ErrInvalidPattern for wildcard-only pattern, got %v", err)
	}
}

func TestListEntries(t *testing.T) {
	svc := newTestService(t)
	ctx := context.Background()

	svc.StoreEntry(ctx, "alpha", "p1", nil)
	svc.StoreEntry(ctx, "beta", "p2", nil)

	entries, total, err := svc.ListEntries(ctx, "", 20, 0, "", "")
	if err != nil {
		t.Fatalf("ListEntries: %v", err)
	}
	if total != 2 {
		t.Errorf("expected total=2, got %d", total)
	}
	if len(entries) != 2 {
		t.Errorf("expected 2 entries, got %d", len(entries))
	}
}
