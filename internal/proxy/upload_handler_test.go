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
)

// --- mock uploader streamer ---

type mockUploadStreamer struct {
	putCalled   bool
	putKey      string
	putBody     string
	getBody     string
	contentType string
	size        int64
	err         error
}

func (m *mockUploadStreamer) PutObject(_ context.Context, key string, body io.Reader, _ string, _ int64) error {
	m.putCalled = true
	m.putKey = key
	data, _ := io.ReadAll(body)
	m.putBody = string(data)
	return m.err
}

func (m *mockUploadStreamer) GetObject(_ context.Context, _ string) (io.ReadCloser, string, int64, error) {
	return io.NopCloser(strings.NewReader(m.getBody)), m.contentType, m.size, nil
}

// --- tests ---

func TestUploadHandler_Success(t *testing.T) {
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

	streamer := &mockUploadStreamer{}

	handler := NewUploadHandler(secret, streamer, repo, 1073741824, slog.Default())

	signedURL := SignUploadURL("http://localhost", secret, entryID.String(), "doc.pdf", 5*time.Minute)

	body := strings.NewReader("fake-pdf-content")
	req := httptest.NewRequest(http.MethodPut, signedURL, body)
	req.Header.Set("Content-Type", "application/pdf")
	rec := httptest.NewRecorder()

	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200; body: %s", rec.Code, rec.Body.String())
	}
	if !streamer.putCalled {
		t.Fatal("expected PutObject to be called")
	}
	if streamer.putBody != "fake-pdf-content" {
		t.Errorf("putBody = %q, want %q", streamer.putBody, "fake-pdf-content")
	}
	if !strings.Contains(streamer.putKey, "doc.pdf") {
		t.Errorf("putKey = %q, want to contain doc.pdf", streamer.putKey)
	}
}

func TestUploadHandler_POST(t *testing.T) {
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

	streamer := &mockUploadStreamer{}
	handler := NewUploadHandler(secret, streamer, repo, 1073741824, slog.Default())

	signedURL := SignUploadURL("http://localhost", secret, entryID.String(), "doc.pdf", 5*time.Minute)
	body := strings.NewReader("content")
	req := httptest.NewRequest(http.MethodPost, signedURL, body)
	rec := httptest.NewRecorder()

	handler.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200; body: %s", rec.Code, rec.Body.String())
	}
}

func TestUploadHandler_MethodNotAllowed(t *testing.T) {
	handler := NewUploadHandler("secret", nil, nil, 1024, slog.Default())
	req := httptest.NewRequest(http.MethodGet, "/ul", nil)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	if rec.Code != http.StatusMethodNotAllowed {
		t.Errorf("status = %d, want 405", rec.Code)
	}
}

func TestUploadHandler_ExpiredURL(t *testing.T) {
	secret := "test-secret"
	entryID := uuid.New().String()

	exp := time.Now().Add(-1 * time.Minute).Unix()
	msg := canonicalMessage(entryID, "doc.pdf", exp)
	sig := computeHMAC(secret, msg)

	reqURL := "/ul?entry=" + entryID + "&file=doc.pdf&exp=" + strconv.FormatInt(exp, 10) + "&sig=" + sig

	handler := NewUploadHandler(secret, nil, nil, 1024, slog.Default())
	req := httptest.NewRequest(http.MethodPut, reqURL, nil)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	if rec.Code != http.StatusGone {
		t.Errorf("status = %d, want 410", rec.Code)
	}
}

func TestUploadHandler_BadSignature(t *testing.T) {
	reqURL := "/ul?entry=abc&file=doc.pdf&exp=9999999999&sig=badsig"
	handler := NewUploadHandler("test-secret", nil, nil, 1024, slog.Default())
	req := httptest.NewRequest(http.MethodPut, reqURL, nil)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	if rec.Code != http.StatusForbidden {
		t.Errorf("status = %d, want 403", rec.Code)
	}
}

func TestUploadHandler_EntryNotFound(t *testing.T) {
	secret := "test-secret"
	entryID := uuid.New()

	repo := &mockRepo{err: domain.ErrNotFound}
	handler := NewUploadHandler(secret, nil, repo, 1024, slog.Default())

	signedURL := SignUploadURL("http://localhost", secret, entryID.String(), "doc.pdf", 5*time.Minute)
	req := httptest.NewRequest(http.MethodPut, signedURL, nil)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	if rec.Code != http.StatusNotFound {
		t.Errorf("status = %d, want 404", rec.Code)
	}
}

func TestUploadHandler_FilenameMismatch(t *testing.T) {
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

	handler := NewUploadHandler(secret, nil, repo, 1073741824, slog.Default())

	// Sign for a different filename
	signedURL := SignUploadURL("http://localhost", secret, entryID.String(), "other.pdf", 5*time.Minute)
	req := httptest.NewRequest(http.MethodPut, signedURL, strings.NewReader("data"))
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	if rec.Code != http.StatusBadRequest {
		t.Errorf("status = %d, want 400", rec.Code)
	}
}

func TestUploadHandler_TooLarge(t *testing.T) {
	entryID := uuid.New()
	secret := "test-secret"

	repo := &mockRepo{
		entry: &domain.Entry{
			ID:        entryID,
			Word:      "testfile",
			EntryType: domain.EntryTypeFile,
			Payload:   `{"s3_key":"` + entryID.String() + `/doc.pdf","filename":"doc.pdf","content_type":"application/pdf","size_limit":100}`,
		},
	}

	handler := NewUploadHandler(secret, nil, repo, 100, slog.Default())

	signedURL := SignUploadURL("http://localhost", secret, entryID.String(), "doc.pdf", 5*time.Minute)
	req := httptest.NewRequest(http.MethodPut, signedURL, strings.NewReader("data"))
	req.ContentLength = 200 // exceeds limit
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	if rec.Code != http.StatusRequestEntityTooLarge {
		t.Errorf("status = %d, want 413", rec.Code)
	}
}
