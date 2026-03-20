package proxy

import (
	"net/url"
	"strconv"
	"testing"
	"time"
)

func TestSignAndValidateRoundTrip(t *testing.T) {
	secret := "test-secret-key"
	baseURL := "https://watchword.example.com"
	entryID := "550e8400-e29b-41d4-a716-446655440000"
	filename := "report.pdf"
	ttl := 5 * time.Minute

	rawURL := SignURL(baseURL, secret, entryID, filename, ttl)

	parsed, err := url.Parse(rawURL)
	if err != nil {
		t.Fatalf("failed to parse signed URL: %v", err)
	}

	gotEntry, gotFile, err := ValidateSignature(secret, parsed.Query())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if gotEntry != entryID {
		t.Errorf("entryID = %q, want %q", gotEntry, entryID)
	}
	if gotFile != filename {
		t.Errorf("filename = %q, want %q", gotFile, filename)
	}
}

func TestValidateSignature_Expired(t *testing.T) {
	secret := "test-secret-key"
	entryID := "550e8400-e29b-41d4-a716-446655440000"
	filename := "report.pdf"

	// Create an already-expired signature
	exp := time.Now().Add(-1 * time.Minute).Unix()
	msg := canonicalMessage(entryID, filename, exp)
	sig := computeHMAC(secret, msg)

	params := url.Values{
		"entry": {entryID},
		"file":  {filename},
		"exp":   {strconv.FormatInt(exp, 10)},
		"sig":   {sig},
	}

	_, _, err := ValidateSignature(secret, params)
	if err == nil {
		t.Fatal("expected error for expired URL")
	}
	if err.Error() != "proxy download URL has expired" {
		t.Errorf("unexpected error: %v", err)
	}
}

func TestValidateSignature_TamperedSig(t *testing.T) {
	secret := "test-secret-key"
	baseURL := "https://watchword.example.com"
	entryID := "550e8400-e29b-41d4-a716-446655440000"
	filename := "report.pdf"

	rawURL := SignURL(baseURL, secret, entryID, filename, 5*time.Minute)
	parsed, _ := url.Parse(rawURL)
	q := parsed.Query()
	q.Set("sig", "deadbeef0000000000000000000000000000000000000000000000000000000000")

	_, _, err := ValidateSignature(secret, q)
	if err == nil {
		t.Fatal("expected error for tampered signature")
	}
}

func TestValidateSignature_TamperedParams(t *testing.T) {
	secret := "test-secret-key"
	baseURL := "https://watchword.example.com"
	entryID := "550e8400-e29b-41d4-a716-446655440000"
	filename := "report.pdf"

	rawURL := SignURL(baseURL, secret, entryID, filename, 5*time.Minute)
	parsed, _ := url.Parse(rawURL)
	q := parsed.Query()
	// Tamper with entry ID
	q.Set("entry", "aaaaaaaa-bbbb-cccc-dddd-eeeeeeeeeeee")

	_, _, err := ValidateSignature(secret, q)
	if err == nil {
		t.Fatal("expected error for tampered params")
	}
}

func TestValidateSignature_MissingParams(t *testing.T) {
	secret := "test-secret-key"

	tests := []struct {
		name   string
		params url.Values
	}{
		{"missing entry", url.Values{"file": {"f"}, "exp": {"9999999999"}, "sig": {"abc"}}},
		{"missing file", url.Values{"entry": {"e"}, "exp": {"9999999999"}, "sig": {"abc"}}},
		{"missing exp", url.Values{"entry": {"e"}, "file": {"f"}, "sig": {"abc"}}},
		{"missing sig", url.Values{"entry": {"e"}, "file": {"f"}, "exp": {"9999999999"}}},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, _, err := ValidateSignature(secret, tt.params)
			if err == nil {
				t.Fatal("expected error")
			}
		})
	}
}
