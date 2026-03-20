package proxy

import (
	"context"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/watchword/watchword/internal/domain"
	"github.com/watchword/watchword/internal/repository"
)

// --- mock repository ---

type mockRepo struct {
	entry *domain.Entry
	err   error
}

func (m *mockRepo) GetByID(_ context.Context, _ uuid.UUID) (*domain.Entry, error) {
	if m.err != nil {
		return nil, m.err
	}
	return m.entry, nil
}

func (m *mockRepo) RecordDownload(_ context.Context, _ uuid.UUID, _, _, _, _ string) error {
	return nil
}

func (m *mockRepo) CleanDownloadHistory(_ context.Context, _ time.Time) (int, error) {
	return 0, nil
}

// Stubs for repository.Repository interface
func (m *mockRepo) Store(_ context.Context, _ *domain.Entry) (*domain.Entry, error) {
	return nil, nil
}
func (m *mockRepo) GetByWord(_ context.Context, _ string, _ bool) (*domain.Entry, error) {
	return nil, nil
}
func (m *mockRepo) SearchByLike(_ context.Context, _ string, _ string, _ int, _ int) ([]*domain.Entry, int, error) {
	return nil, 0, nil
}
func (m *mockRepo) List(_ context.Context, _ string, _ int, _ int, _ string, _ string) ([]*domain.Entry, int, error) {
	return nil, 0, nil
}
func (m *mockRepo) UpdateStatus(_ context.Context, _ uuid.UUID, _ string, _ string, _ *time.Time) error {
	return nil
}
func (m *mockRepo) Delete(_ context.Context, _ uuid.UUID) error            { return nil }
func (m *mockRepo) MarkExpiredBatch(_ context.Context, _ int) (int, error) { return 0, nil }
func (m *mockRepo) WordExistsActive(_ context.Context, _ string) (bool, error) {
	return false, nil
}
func (m *mockRepo) WithTx(_ context.Context, _ func(repository.Repository) error) error {
	return nil
}
func (m *mockRepo) Ping(_ context.Context) error    { return nil }
func (m *mockRepo) Migrate(_ context.Context) error { return nil }
func (m *mockRepo) Close() error                    { return nil }

// --- mock streamer ---

type mockStreamer struct {
	body        string
	contentType string
	size        int64
	err         error
}

func (m *mockStreamer) GetObject(_ context.Context, _ string) (io.ReadCloser, string, int64, error) {
	if m.err != nil {
		return nil, "", 0, m.err
	}
	return io.NopCloser(strings.NewReader(m.body)), m.contentType, m.size, nil
}

// --- tests ---

func TestHandler_Success(t *testing.T) {
	entryID := uuid.New()
	secret := "test-secret"

	repo := &mockRepo{
		entry: &domain.Entry{
			ID:        entryID,
			Word:      "testfile",
			EntryType: domain.EntryTypeFile,
			Payload:   `{"s3_key":"` + entryID.String() + `/doc.pdf","filename":"doc.pdf","content_type":"application/pdf","size_limit":1073741824}`,
		},
	}

	streamer := &mockStreamer{
		body:        "fake-pdf-content",
		contentType: "application/pdf",
		size:        16,
	}

	handler := NewHandler(secret, streamer, repo, slog.Default())

	signedURL := SignURL("http://localhost", secret, entryID.String(), "doc.pdf", 5*time.Minute)

	req := httptest.NewRequest(http.MethodGet, signedURL, nil)
	rec := httptest.NewRecorder()

	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200; body: %s", rec.Code, rec.Body.String())
	}
	if rec.Body.String() != "fake-pdf-content" {
		t.Errorf("body = %q, want %q", rec.Body.String(), "fake-pdf-content")
	}
	if ct := rec.Header().Get("Content-Type"); ct != "application/pdf" {
		t.Errorf("Content-Type = %q, want application/pdf", ct)
	}
	if cd := rec.Header().Get("Content-Disposition"); !strings.Contains(cd, "doc.pdf") {
		t.Errorf("Content-Disposition = %q, want to contain doc.pdf", cd)
	}
	if cc := rec.Header().Get("Cache-Control"); cc != "no-store" {
		t.Errorf("Cache-Control = %q, want no-store", cc)
	}
}

func TestHandler_MethodNotAllowed(t *testing.T) {
	handler := NewHandler("secret", nil, nil, slog.Default())
	req := httptest.NewRequest(http.MethodPost, "/dl", nil)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	if rec.Code != http.StatusMethodNotAllowed {
		t.Errorf("status = %d, want 405", rec.Code)
	}
}

func TestHandler_ExpiredURL(t *testing.T) {
	secret := "test-secret"
	entryID := uuid.New().String()

	exp := time.Now().Add(-1 * time.Minute).Unix()
	msg := canonicalMessage(entryID, "doc.pdf", exp)
	sig := computeHMAC(secret, msg)

	reqURL := "/dl?entry=" + entryID + "&file=doc.pdf&exp=" + strconv.FormatInt(exp, 10) + "&sig=" + sig

	handler := NewHandler(secret, nil, nil, slog.Default())
	req := httptest.NewRequest(http.MethodGet, reqURL, nil)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	if rec.Code != http.StatusGone {
		t.Errorf("status = %d, want 410", rec.Code)
	}
}

func TestHandler_BadSignature(t *testing.T) {
	secret := "test-secret"

	reqURL := "/dl?entry=abc&file=doc.pdf&exp=9999999999&sig=badsig"
	handler := NewHandler(secret, nil, nil, slog.Default())
	req := httptest.NewRequest(http.MethodGet, reqURL, nil)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	if rec.Code != http.StatusForbidden {
		t.Errorf("status = %d, want 403", rec.Code)
	}
}

func TestHandler_EntryNotFound(t *testing.T) {
	secret := "test-secret"
	entryID := uuid.New()

	repo := &mockRepo{err: domain.ErrNotFound}
	handler := NewHandler(secret, nil, repo, slog.Default())

	signedURL := SignURL("http://localhost", secret, entryID.String(), "doc.pdf", 5*time.Minute)
	req := httptest.NewRequest(http.MethodGet, signedURL, nil)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	if rec.Code != http.StatusNotFound {
		t.Errorf("status = %d, want 404", rec.Code)
	}
}
