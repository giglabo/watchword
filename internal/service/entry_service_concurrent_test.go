package service

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"sync"
	"sync/atomic"
	"testing"

	"github.com/watchword/watchword/internal/repository"
)

func newDiskService(t *testing.T) *EntryService {
	t.Helper()
	dbPath := filepath.Join(t.TempDir(), "test.db")
	repo, err := repository.NewSQLiteRepo(dbPath)
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

// TestStoreEntry_ConcurrentCollisionResolution drives many goroutines at the
// same base word simultaneously. Because the collision-resolution flow is a
// read-then-write transaction (WordExistsActive -> Store), concurrent calls
// must serialize cleanly via BEGIN IMMEDIATE + busy_timeout. Each call must
// succeed and produce a distinct resolved word.
func TestStoreEntry_ConcurrentCollisionResolution(t *testing.T) {
	svc := newDiskService(t)
	ctx := context.Background()

	const goroutines = 24
	var wg sync.WaitGroup
	var failures atomic.Int32

	results := make([]string, goroutines)
	for i := 0; i < goroutines; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			res, err := svc.StoreEntry(ctx, "rabbit", "payload", nil)
			if err != nil {
				failures.Add(1)
				t.Errorf("StoreEntry %d: %v", i, err)
				return
			}
			results[i] = res.Entry.Word
		}(i)
	}
	wg.Wait()
	if failures.Load() != 0 {
		t.Fatalf("had %d failures", failures.Load())
	}

	seen := make(map[string]bool, goroutines)
	hasBase := false
	for _, w := range results {
		if w == "" {
			t.Fatal("empty result word")
		}
		if seen[w] {
			t.Errorf("duplicate resolved word: %s", w)
		}
		seen[w] = true
		if w == "rabbit" {
			hasBase = true
		}
	}
	if !hasBase {
		t.Error("expected exactly one goroutine to win the base word 'rabbit'")
	}
	if len(seen) != goroutines {
		t.Errorf("expected %d unique words, got %d", goroutines, len(seen))
	}
}

// TestStoreEntry_ConcurrentDistinctWords confirms that unrelated writers
// don't interfere with each other under load.
func TestStoreEntry_ConcurrentDistinctWords(t *testing.T) {
	svc := newDiskService(t)
	ctx := context.Background()

	const goroutines = 32
	const perGoroutine = 10
	var wg sync.WaitGroup
	var failures atomic.Int32

	for g := 0; g < goroutines; g++ {
		wg.Add(1)
		go func(g int) {
			defer wg.Done()
			for i := 0; i < perGoroutine; i++ {
				word := fmt.Sprintf("word_%d_%d", g, i)
				if _, err := svc.StoreEntry(ctx, word, "p", nil); err != nil {
					failures.Add(1)
					t.Errorf("StoreEntry(%s): %v", word, err)
					return
				}
			}
		}(g)
	}
	wg.Wait()
	if failures.Load() != 0 {
		t.Fatalf("had %d failures", failures.Load())
	}
}
