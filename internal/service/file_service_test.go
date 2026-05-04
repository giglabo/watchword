package service

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"os"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/watchword/watchword/internal/domain"
	"github.com/watchword/watchword/internal/repository"
)

type fakePresigner struct {
	mu       sync.Mutex
	putKeys  []string
	getKeys  []string
	deletes  []string
}

func (p *fakePresigner) PresignPUT(_ context.Context, key, _ string, _ int64) (string, error) {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.putKeys = append(p.putKeys, key)
	return "https://presigned.example/put?key=" + key, nil
}

func (p *fakePresigner) PresignGET(_ context.Context, key, _ string) (string, error) {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.getKeys = append(p.getKeys, key)
	return "https://presigned.example/get?key=" + key, nil
}

func (p *fakePresigner) DeleteObject(_ context.Context, key string) error {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.deletes = append(p.deletes, key)
	return nil
}

func newTestFileService(t *testing.T) (*FileService, repository.Repository, *fakePresigner) {
	return newTestFileServiceWithPrefix(t, "")
}

func newTestFileServiceWithPrefix(t *testing.T, keyPrefix string) (*FileService, repository.Repository, *fakePresigner) {
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
	pres := &fakePresigner{}
	svc := NewFileService(repo, pres, 168, 1<<20, keyPrefix, logger)
	return svc, repo, pres
}

func TestUploadFile_CollisionResolved_StoresConsistentMetadata(t *testing.T) {
	svc, repo, pres := newTestFileService(t)
	ctx := context.Background()

	// Pre-existing entry takes the base word.
	if _, err := repo.Store(ctx, &domain.Entry{Word: "owl", Payload: "x"}); err != nil {
		t.Fatalf("seed Store: %v", err)
	}

	result, err := svc.UploadFile(ctx, "owl", "doc.pdf", "application/pdf", nil)
	if err != nil {
		t.Fatalf("UploadFile: %v", err)
	}
	if !result.CollisionResolved {
		t.Fatal("expected collision_resolved=true")
	}
	if result.Word != "owl2" {
		t.Errorf("expected resolved word owl2, got %q", result.Word)
	}
	if result.OriginalWord != "owl" {
		t.Errorf("expected original_word=owl, got %q", result.OriginalWord)
	}

	expectedKey := fmt.Sprintf("%s/doc.pdf", result.ID)
	if len(pres.putKeys) != 1 || pres.putKeys[0] != expectedKey {
		t.Fatalf("presigner PUT keys = %v, want [%s]", pres.putKeys, expectedKey)
	}

	entry, err := repo.GetByWord(ctx, "owl2", false)
	if err != nil {
		t.Fatalf("GetByWord owl2: %v", err)
	}
	if entry.ID.String() != result.ID {
		t.Errorf("entry ID %s != result ID %s", entry.ID, result.ID)
	}
	if entry.EntryType != domain.EntryTypeFile {
		t.Errorf("entry_type = %q, want file", entry.EntryType)
	}

	var meta domain.FileMetadata
	if err := json.Unmarshal([]byte(entry.Payload), &meta); err != nil {
		t.Fatalf("unmarshal metadata: %v", err)
	}
	if meta.S3Key != expectedKey {
		t.Errorf("meta.S3Key = %q, want %q", meta.S3Key, expectedKey)
	}
	if meta.Filename != "doc.pdf" {
		t.Errorf("meta.Filename = %q, want doc.pdf", meta.Filename)
	}
}

func TestUploadFile_NoCollision(t *testing.T) {
	svc, repo, pres := newTestFileService(t)
	ctx := context.Background()

	result, err := svc.UploadFile(ctx, "fox", "report.txt", "text/plain", nil)
	if err != nil {
		t.Fatalf("UploadFile: %v", err)
	}
	if result.CollisionResolved {
		t.Error("expected no collision")
	}
	if result.Word != "fox" {
		t.Errorf("expected word=fox, got %q", result.Word)
	}

	expectedKey := fmt.Sprintf("%s/report.txt", result.ID)
	if len(pres.putKeys) != 1 || pres.putKeys[0] != expectedKey {
		t.Fatalf("presigner PUT keys = %v, want [%s]", pres.putKeys, expectedKey)
	}

	entry, err := repo.GetByWord(ctx, "fox", false)
	if err != nil {
		t.Fatalf("GetByWord fox: %v", err)
	}
	var meta domain.FileMetadata
	if err := json.Unmarshal([]byte(entry.Payload), &meta); err != nil {
		t.Fatalf("unmarshal metadata: %v", err)
	}
	if meta.S3Key != expectedKey {
		t.Errorf("meta.S3Key = %q, want %q", meta.S3Key, expectedKey)
	}
}

func TestUploadFile_KeyPrefix(t *testing.T) {
	cases := []struct {
		name    string
		prefix  string
		wantPfx string // expected normalized prefix in S3 key
	}{
		{"plain", "tenants/acme", "tenants/acme/"},
		{"trailing slash trimmed", "tenants/acme/", "tenants/acme/"},
		{"leading slash trimmed", "/tenants/acme", "tenants/acme/"},
		{"both slashes trimmed", "/tenants/acme/", "tenants/acme/"},
		{"empty prefix = legacy", "", ""},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			svc, _, pres := newTestFileServiceWithPrefix(t, tc.prefix)
			ctx := context.Background()

			result, err := svc.UploadFile(ctx, "fox", "report.txt", "text/plain", nil)
			if err != nil {
				t.Fatalf("UploadFile: %v", err)
			}
			wantKey := tc.wantPfx + result.ID + "/report.txt"
			if len(pres.putKeys) != 1 || pres.putKeys[0] != wantKey {
				t.Fatalf("PUT keys = %v, want [%s]", pres.putKeys, wantKey)
			}
		})
	}
}

// Resetting the expiration of a file entry — whether to extend its life or to
// reactivate it after expiry — must never delete the underlying S3 object.
// This is the contract relied on by the update_expiration tool.
func TestUpdateExpiration_FileEntry_PreservesS3(t *testing.T) {
	fileSvc, repo, pres := newTestFileService(t)
	ctx := context.Background()

	uploaded, err := fileSvc.UploadFile(ctx, "doc", "report.pdf", "application/pdf", nil)
	if err != nil {
		t.Fatalf("UploadFile: %v", err)
	}

	// Force-expire the entry through the repo, mirroring what the worker does.
	past := time.Now().UTC().Add(-1 * time.Hour)
	if err := repo.UpdateStatus(ctx, mustParseUUID(t, uploaded.ID), string(domain.StatusActive), uploaded.Word, &past); err != nil {
		t.Fatalf("seed expiry: %v", err)
	}
	if _, err := repo.MarkExpiredBatch(ctx, 100); err != nil {
		t.Fatalf("MarkExpiredBatch: %v", err)
	}

	logger := slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelError}))
	entrySvc := NewEntryService(repo, 168, logger)

	if _, err := entrySvc.UpdateExpiration(ctx, "doc", nil); err != nil {
		t.Fatalf("UpdateExpiration: %v", err)
	}

	if len(pres.deletes) != 0 {
		t.Errorf("update_expiration must not delete S3 objects; observed deletes=%v", pres.deletes)
	}
}

func mustParseUUID(t *testing.T, s string) uuid.UUID {
	t.Helper()
	id, err := uuid.Parse(s)
	if err != nil {
		t.Fatalf("uuid.Parse(%q): %v", s, err)
	}
	return id
}
