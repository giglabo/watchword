package repository

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"os"
	"testing"
	"time"

	"github.com/watchword/watchword/internal/domain"
)

// libsqlTestEnv is the env var pair used to point integration tests at a
// real Turso/libSQL database. Both must be set; otherwise the tests skip so
// CI builds without the secret continue to pass.
const (
	envLibSQLURL   = "WATCHWORD_TEST_TURSO_URL"
	envLibSQLToken = "WATCHWORD_TEST_TURSO_AUTH_TOKEN"
)

// newLibSQLTestRepo opens a repo against a real Turso DB. Each test uses a
// unique word prefix so concurrent runs (and re-runs of failing tests) don't
// step on each other's rows. The teardown deletes only rows this test wrote.
func newLibSQLTestRepo(t *testing.T) (*SQLiteRepo, string) {
	t.Helper()

	dbURL := os.Getenv(envLibSQLURL)
	token := os.Getenv(envLibSQLToken)
	if dbURL == "" {
		t.Skipf("libSQL integration tests skipped: set %s (and %s) to enable", envLibSQLURL, envLibSQLToken)
	}

	repo, err := NewLibSQLRepo(dbURL, token)
	if err != nil {
		t.Fatalf("NewLibSQLRepo: %v", err)
	}
	t.Cleanup(func() { repo.Close() })

	if err := repo.Migrate(context.Background()); err != nil {
		t.Fatalf("Migrate: %v", err)
	}

	prefix := testPrefix(t)
	t.Cleanup(func() {
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		_, _ = repo.db.ExecContext(ctx, `DELETE FROM entries WHERE word LIKE ?`, prefix+"%")
	})
	return repo, prefix
}

func testPrefix(t *testing.T) string {
	t.Helper()
	var b [4]byte
	_, _ = rand.Read(b[:])
	return "wwtest_" + hex.EncodeToString(b[:]) + "_"
}

func TestLibSQL_StoreAndGetByID(t *testing.T) {
	repo, prefix := newLibSQLTestRepo(t)
	ctx := context.Background()

	expires := time.Now().UTC().Add(24 * time.Hour)
	created, err := repo.Store(ctx, &domain.Entry{
		Word:      prefix + "rabbit",
		Payload:   "test payload",
		ExpiresAt: &expires,
	})
	if err != nil {
		t.Fatalf("Store: %v", err)
	}

	fetched, err := repo.GetByID(ctx, created.ID)
	if err != nil {
		t.Fatalf("GetByID: %v", err)
	}
	if fetched.Payload != "test payload" {
		t.Errorf("payload mismatch: got %q", fetched.Payload)
	}
}

func TestLibSQL_GetByWordAndDelete(t *testing.T) {
	repo, prefix := newLibSQLTestRepo(t)
	ctx := context.Background()

	word := prefix + "cat"
	created, err := repo.Store(ctx, &domain.Entry{Word: word, Payload: "meow"})
	if err != nil {
		t.Fatalf("Store: %v", err)
	}

	got, err := repo.GetByWord(ctx, word, false)
	if err != nil {
		t.Fatalf("GetByWord: %v", err)
	}
	if got.ID != created.ID {
		t.Errorf("id mismatch")
	}

	if err := repo.Delete(ctx, created.ID); err != nil {
		t.Fatalf("Delete: %v", err)
	}
	if _, err := repo.GetByID(ctx, created.ID); err != domain.ErrNotFound {
		t.Errorf("expected ErrNotFound after delete, got %v", err)
	}
}

func TestLibSQL_UniqueConstraint(t *testing.T) {
	repo, prefix := newLibSQLTestRepo(t)
	ctx := context.Background()

	word := prefix + "dog"
	if _, err := repo.Store(ctx, &domain.Entry{Word: word, Payload: "woof"}); err != nil {
		t.Fatalf("first store: %v", err)
	}
	if _, err := repo.Store(ctx, &domain.Entry{Word: word, Payload: "bark"}); err == nil {
		t.Error("expected unique-constraint error on duplicate word+status, got nil")
	}
}

func TestLibSQL_SearchAndList(t *testing.T) {
	repo, prefix := newLibSQLTestRepo(t)
	ctx := context.Background()

	for _, w := range []string{"rabbit", "raccoon", "cat"} {
		if _, err := repo.Store(ctx, &domain.Entry{Word: prefix + w, Payload: "p"}); err != nil {
			t.Fatalf("Store %s: %v", w, err)
		}
	}

	entries, total, err := repo.SearchByLike(ctx, prefix+"ra%", "active", 20, 0)
	if err != nil {
		t.Fatalf("SearchByLike: %v", err)
	}
	if total != 2 || len(entries) != 2 {
		t.Errorf("expected 2 ra-prefix matches, got total=%d len=%d", total, len(entries))
	}
}

func TestLibSQL_WithTx(t *testing.T) {
	repo, prefix := newLibSQLTestRepo(t)
	ctx := context.Background()

	word := prefix + "txtest"
	err := repo.WithTx(ctx, func(txRepo Repository) error {
		_, err := txRepo.Store(ctx, &domain.Entry{Word: word, Payload: "p1"})
		return err
	})
	if err != nil {
		t.Fatalf("WithTx: %v", err)
	}
	exists, err := repo.WordExistsActive(ctx, word)
	if err != nil {
		t.Fatalf("WordExistsActive: %v", err)
	}
	if !exists {
		t.Error("expected word to exist after tx commit")
	}
}
