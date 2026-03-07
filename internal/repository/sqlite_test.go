package repository

import (
	"context"
	"testing"
	"time"

	"github.com/watchword/watchword/internal/domain"
)

func newTestSQLiteRepo(t *testing.T) *SQLiteRepo {
	t.Helper()
	repo, err := NewSQLiteRepo(":memory:")
	if err != nil {
		t.Fatalf("NewSQLiteRepo: %v", err)
	}
	t.Cleanup(func() { repo.Close() })
	if err := repo.Migrate(context.Background()); err != nil {
		t.Fatalf("Migrate: %v", err)
	}
	return repo
}

func TestSQLite_StoreAndGetByID(t *testing.T) {
	repo := newTestSQLiteRepo(t)
	ctx := context.Background()

	expires := time.Now().UTC().Add(24 * time.Hour)
	entry := &domain.Entry{
		Word:      "rabbit",
		Payload:   "test payload",
		ExpiresAt: &expires,
	}

	created, err := repo.Store(ctx, entry)
	if err != nil {
		t.Fatalf("Store: %v", err)
	}
	if created.Word != "rabbit" {
		t.Errorf("expected word=rabbit, got %s", created.Word)
	}
	if created.Status != domain.StatusActive {
		t.Errorf("expected status=active, got %s", created.Status)
	}

	fetched, err := repo.GetByID(ctx, created.ID)
	if err != nil {
		t.Fatalf("GetByID: %v", err)
	}
	if fetched.Word != "rabbit" {
		t.Errorf("expected word=rabbit, got %s", fetched.Word)
	}
	if fetched.Payload != "test payload" {
		t.Errorf("expected payload='test payload', got %s", fetched.Payload)
	}
}

func TestSQLite_GetByWord(t *testing.T) {
	repo := newTestSQLiteRepo(t)
	ctx := context.Background()

	repo.Store(ctx, &domain.Entry{Word: "cat", Payload: "meow"})

	entry, err := repo.GetByWord(ctx, "cat", false)
	if err != nil {
		t.Fatalf("GetByWord: %v", err)
	}
	if entry.Word != "cat" {
		t.Errorf("expected word=cat, got %s", entry.Word)
	}

	_, err = repo.GetByWord(ctx, "nonexistent", false)
	if err != domain.ErrNotFound {
		t.Errorf("expected ErrNotFound, got %v", err)
	}
}

func TestSQLite_UniqueConstraint(t *testing.T) {
	repo := newTestSQLiteRepo(t)
	ctx := context.Background()

	_, err := repo.Store(ctx, &domain.Entry{Word: "dog", Payload: "woof"})
	if err != nil {
		t.Fatalf("first store: %v", err)
	}

	_, err = repo.Store(ctx, &domain.Entry{Word: "dog", Payload: "bark"})
	if err == nil {
		t.Error("expected error for duplicate word+status, got nil")
	}
}

func TestSQLite_SearchByLike(t *testing.T) {
	repo := newTestSQLiteRepo(t)
	ctx := context.Background()

	repo.Store(ctx, &domain.Entry{Word: "rabbit", Payload: "p1"})
	repo.Store(ctx, &domain.Entry{Word: "raccoon", Payload: "p2"})
	repo.Store(ctx, &domain.Entry{Word: "cat", Payload: "p3"})

	entries, total, err := repo.SearchByLike(ctx, "ra%", "active", 20, 0)
	if err != nil {
		t.Fatalf("SearchByLike: %v", err)
	}
	if total != 2 {
		t.Errorf("expected total=2, got %d", total)
	}
	if len(entries) != 2 {
		t.Errorf("expected 2 entries, got %d", len(entries))
	}
}

func TestSQLite_List(t *testing.T) {
	repo := newTestSQLiteRepo(t)
	ctx := context.Background()

	repo.Store(ctx, &domain.Entry{Word: "alpha", Payload: "p1"})
	repo.Store(ctx, &domain.Entry{Word: "beta", Payload: "p2"})
	repo.Store(ctx, &domain.Entry{Word: "gamma", Payload: "p3"})

	entries, total, err := repo.List(ctx, "active", 2, 0, "word", "asc")
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	if total != 3 {
		t.Errorf("expected total=3, got %d", total)
	}
	if len(entries) != 2 {
		t.Errorf("expected 2 entries, got %d", len(entries))
	}
	if entries[0].Word != "alpha" {
		t.Errorf("expected first=alpha, got %s", entries[0].Word)
	}
}

func TestSQLite_Delete(t *testing.T) {
	repo := newTestSQLiteRepo(t)
	ctx := context.Background()

	created, _ := repo.Store(ctx, &domain.Entry{Word: "temp", Payload: "p1"})
	if err := repo.Delete(ctx, created.ID); err != nil {
		t.Fatalf("Delete: %v", err)
	}
	_, err := repo.GetByID(ctx, created.ID)
	if err != domain.ErrNotFound {
		t.Errorf("expected ErrNotFound after delete, got %v", err)
	}
}

func TestSQLite_UpdateStatus(t *testing.T) {
	repo := newTestSQLiteRepo(t)
	ctx := context.Background()

	created, _ := repo.Store(ctx, &domain.Entry{Word: "test", Payload: "p1"})

	err := repo.UpdateStatus(ctx, created.ID, "expired", "test", nil)
	if err != nil {
		t.Fatalf("UpdateStatus: %v", err)
	}

	entry, _ := repo.GetByID(ctx, created.ID)
	if entry.Status != domain.StatusExpired {
		t.Errorf("expected status=expired, got %s", entry.Status)
	}
}

func TestSQLite_MarkExpiredBatch(t *testing.T) {
	repo := newTestSQLiteRepo(t)
	ctx := context.Background()

	past := time.Now().UTC().Add(-1 * time.Hour)
	future := time.Now().UTC().Add(24 * time.Hour)

	repo.Store(ctx, &domain.Entry{Word: "expired", Payload: "p1", ExpiresAt: &past})
	repo.Store(ctx, &domain.Entry{Word: "active", Payload: "p2", ExpiresAt: &future})
	repo.Store(ctx, &domain.Entry{Word: "noexpiry", Payload: "p3"})

	count, err := repo.MarkExpiredBatch(ctx, 500)
	if err != nil {
		t.Fatalf("MarkExpiredBatch: %v", err)
	}
	if count != 1 {
		t.Errorf("expected 1 expired, got %d", count)
	}

	entry, _ := repo.GetByWord(ctx, "expired", true)
	if entry.Status != domain.StatusExpired {
		t.Errorf("expected status=expired, got %s", entry.Status)
	}

	entry, _ = repo.GetByWord(ctx, "active", false)
	if entry.Status != domain.StatusActive {
		t.Errorf("expected status=active, got %s", entry.Status)
	}
}

func TestSQLite_WithTx(t *testing.T) {
	repo := newTestSQLiteRepo(t)
	ctx := context.Background()

	err := repo.WithTx(ctx, func(txRepo Repository) error {
		_, err := txRepo.Store(ctx, &domain.Entry{Word: "txtest", Payload: "p1"})
		if err != nil {
			return err
		}
		exists, err := txRepo.WordExistsActive(ctx, "txtest")
		if err != nil {
			return err
		}
		if !exists {
			t.Error("expected word to exist within tx")
		}
		return nil
	})
	if err != nil {
		t.Fatalf("WithTx: %v", err)
	}

	exists, _ := repo.WordExistsActive(ctx, "txtest")
	if !exists {
		t.Error("expected word to exist after tx commit")
	}
}
