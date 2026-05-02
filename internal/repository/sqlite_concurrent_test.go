package repository

import (
	"context"
	"database/sql"
	"fmt"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/watchword/watchword/internal/domain"
)

func newDiskRepo(t *testing.T) *SQLiteRepo {
	t.Helper()
	dbPath := filepath.Join(t.TempDir(), "test.db")
	repo, err := NewSQLiteRepo(dbPath)
	if err != nil {
		t.Fatalf("NewSQLiteRepo: %v", err)
	}
	t.Cleanup(func() { repo.Close() })
	if err := repo.Migrate(context.Background()); err != nil {
		t.Fatalf("Migrate: %v", err)
	}
	return repo
}

func TestBuildSQLiteDSN(t *testing.T) {
	const wantPragmas = "_pragma=journal_mode(WAL)" +
		"&_pragma=busy_timeout(5000)" +
		"&_pragma=foreign_keys(1)" +
		"&_pragma=synchronous(NORMAL)" +
		"&_txlock=immediate"

	cases := []struct {
		name string
		in   string
		want string
	}{
		{"memory", ":memory:", "file::memory:?" + wantPragmas},
		{"absolute path", "/tmp/test.db", "file:/tmp/test.db?" + wantPragmas},
		{"relative path", "./data/test.db", "file:./data/test.db?" + wantPragmas},
		{"uri without query", "file:/tmp/test.db", "file:/tmp/test.db?" + wantPragmas},
		{"uri with query", "file:/tmp/test.db?cache=shared", "file:/tmp/test.db?cache=shared&" + wantPragmas},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := buildSQLiteDSN(tc.in); got != tc.want {
				t.Errorf("buildSQLiteDSN(%q)\n  got:  %q\n  want: %q", tc.in, got, tc.want)
			}
		})
	}
}

// TestSQLite_PragmasOnEveryPoolConnection verifies that the pragmas are
// applied per-connection rather than once at startup. Every connection in the
// pool must report WAL, busy_timeout=5000, and foreign_keys=1; otherwise
// concurrent writes can hit instant SQLITE_BUSY and FK constraints become
// silently inconsistent.
func TestSQLite_PragmasOnEveryPoolConnection(t *testing.T) {
	repo := newDiskRepo(t)
	ctx := context.Background()

	// Hold several connections open simultaneously so the pool is forced
	// to allocate distinct underlying connections.
	const n = 4
	conns := make([]*sql.Conn, n)
	for i := 0; i < n; i++ {
		c, err := repo.db.Conn(ctx)
		if err != nil {
			t.Fatalf("Conn(%d): %v", i, err)
		}
		conns[i] = c
		defer c.Close()
	}

	for i, c := range conns {
		var journal string
		if err := c.QueryRowContext(ctx, "PRAGMA journal_mode").Scan(&journal); err != nil {
			t.Fatalf("conn %d journal_mode: %v", i, err)
		}
		if !strings.EqualFold(journal, "wal") {
			t.Errorf("conn %d journal_mode=%q, want wal", i, journal)
		}

		var busy int
		if err := c.QueryRowContext(ctx, "PRAGMA busy_timeout").Scan(&busy); err != nil {
			t.Fatalf("conn %d busy_timeout: %v", i, err)
		}
		if busy != 5000 {
			t.Errorf("conn %d busy_timeout=%d, want 5000", i, busy)
		}

		var fk int
		if err := c.QueryRowContext(ctx, "PRAGMA foreign_keys").Scan(&fk); err != nil {
			t.Fatalf("conn %d foreign_keys: %v", i, err)
		}
		if fk != 1 {
			t.Errorf("conn %d foreign_keys=%d, want 1", i, fk)
		}
	}
}

// TestSQLite_ConcurrentDistinctWrites stresses the writer-serialization path:
// many goroutines insert distinct words at the same time. With busy_timeout
// and BEGIN IMMEDIATE in place, every write must succeed even though SQLite
// only admits one writer at a time.
func TestSQLite_ConcurrentDistinctWrites(t *testing.T) {
	repo := newDiskRepo(t)
	ctx := context.Background()

	const goroutines = 32
	const perGoroutine = 25

	var wg sync.WaitGroup
	var failures atomic.Int32
	for g := 0; g < goroutines; g++ {
		wg.Add(1)
		go func(g int) {
			defer wg.Done()
			for i := 0; i < perGoroutine; i++ {
				word := fmt.Sprintf("w_%d_%d", g, i)
				if _, err := repo.Store(ctx, &domain.Entry{Word: word, Payload: "x"}); err != nil {
					failures.Add(1)
					t.Errorf("Store(%s): %v", word, err)
					return
				}
			}
		}(g)
	}
	wg.Wait()
	if failures.Load() != 0 {
		t.Fatalf("had %d store failures under concurrency", failures.Load())
	}

	_, total, err := repo.List(ctx, "active", 1, 0, "word", "asc")
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	if want := goroutines * perGoroutine; total != want {
		t.Errorf("expected %d entries, got %d", want, total)
	}
}

// TestSQLite_ConcurrentReadsAndWrites verifies that readers and writers can
// run concurrently — readers should never be blocked by an in-flight writer,
// and writers should not produce errors while readers iterate.
func TestSQLite_ConcurrentReadsAndWrites(t *testing.T) {
	repo := newDiskRepo(t)
	ctx := context.Background()

	for i := 0; i < 50; i++ {
		if _, err := repo.Store(ctx, &domain.Entry{Word: fmt.Sprintf("seed%d", i), Payload: "p"}); err != nil {
			t.Fatalf("seed %d: %v", i, err)
		}
	}

	deadline := time.Now().Add(2 * time.Second)
	var wg sync.WaitGroup
	var readErrs, writeErrs atomic.Int32

	for i := 0; i < 8; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for time.Now().Before(deadline) {
				if _, _, err := repo.List(ctx, "active", 50, 0, "word", "asc"); err != nil {
					readErrs.Add(1)
					t.Errorf("List: %v", err)
					return
				}
				if _, err := repo.GetByWord(ctx, "seed10", false); err != nil {
					readErrs.Add(1)
					t.Errorf("GetByWord: %v", err)
					return
				}
			}
		}()
	}

	var counter atomic.Int64
	for i := 0; i < 4; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for time.Now().Before(deadline) {
				n := counter.Add(1)
				w := fmt.Sprintf("conc%d", n)
				if _, err := repo.Store(ctx, &domain.Entry{Word: w, Payload: "p"}); err != nil {
					writeErrs.Add(1)
					t.Errorf("Store(%s): %v", w, err)
					return
				}
			}
		}()
	}

	wg.Wait()
	if readErrs.Load() != 0 || writeErrs.Load() != 0 {
		t.Fatalf("readErrs=%d writeErrs=%d", readErrs.Load(), writeErrs.Load())
	}
	if counter.Load() == 0 {
		t.Fatal("expected at least one concurrent write")
	}
}

// TestSQLite_ConcurrentTransactions runs many BEGIN IMMEDIATE transactions
// that read-then-write. Without _txlock=immediate this is the classic
// busy-snapshot trap; with it, every transaction must complete cleanly.
func TestSQLite_ConcurrentTransactions(t *testing.T) {
	repo := newDiskRepo(t)
	ctx := context.Background()

	const goroutines = 16
	var wg sync.WaitGroup
	var failures atomic.Int32

	for g := 0; g < goroutines; g++ {
		wg.Add(1)
		go func(g int) {
			defer wg.Done()
			err := repo.WithTx(ctx, func(txRepo Repository) error {
				w := fmt.Sprintf("tx%d", g)
				exists, err := txRepo.WordExistsActive(ctx, w)
				if err != nil {
					return err
				}
				if exists {
					return fmt.Errorf("word already exists in fresh DB: %s", w)
				}
				_, err = txRepo.Store(ctx, &domain.Entry{Word: w, Payload: "p"})
				return err
			})
			if err != nil {
				failures.Add(1)
				t.Errorf("tx %d: %v", g, err)
			}
		}(g)
	}
	wg.Wait()
	if failures.Load() != 0 {
		t.Fatalf("transaction failures: %d", failures.Load())
	}

	_, total, _ := repo.List(ctx, "active", 1, 0, "word", "asc")
	if total != goroutines {
		t.Errorf("expected %d, got %d", goroutines, total)
	}
}

// TestSQLite_ConcurrentExpirationAndWrites is closer to the real workload:
// the background expiration sweeper races with online writes. Both paths use
// UPDATE/INSERT and must not deadlock or fail.
func TestSQLite_ConcurrentExpirationAndWrites(t *testing.T) {
	repo := newDiskRepo(t)
	ctx := context.Background()

	past := time.Now().Add(-1 * time.Hour)
	for i := 0; i < 20; i++ {
		if _, err := repo.Store(ctx, &domain.Entry{Word: fmt.Sprintf("exp%d", i), Payload: "p", ExpiresAt: &past}); err != nil {
			t.Fatalf("seed %d: %v", i, err)
		}
	}

	deadline := time.Now().Add(time.Second)
	var wg sync.WaitGroup
	var failures atomic.Int32

	wg.Add(1)
	go func() {
		defer wg.Done()
		for time.Now().Before(deadline) {
			if _, err := repo.MarkExpiredBatch(ctx, 10); err != nil {
				failures.Add(1)
				t.Errorf("MarkExpiredBatch: %v", err)
				return
			}
		}
	}()

	var counter atomic.Int64
	for i := 0; i < 4; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for time.Now().Before(deadline) {
				n := counter.Add(1)
				w := fmt.Sprintf("live%d", n)
				if _, err := repo.Store(ctx, &domain.Entry{Word: w, Payload: "p"}); err != nil {
					failures.Add(1)
					t.Errorf("Store(%s): %v", w, err)
					return
				}
			}
		}()
	}
	wg.Wait()
	if failures.Load() != 0 {
		t.Fatalf("failures: %d", failures.Load())
	}
}
