package proxy

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"net/url"
	"strconv"
	"time"

	"github.com/watchword/watchword/internal/domain"
)

// SignURL generates an HMAC-signed proxy URL for the given path.
// Format: <baseURL>/<path>?entry=<id>&file=<name>&exp=<unix>&sig=<hex-hmac>
func SignURL(baseURL, path, secret, entryID, filename string, ttl time.Duration) string {
	exp := time.Now().Add(ttl).Unix()
	msg := canonicalMessage(entryID, filename, exp)
	sig := computeHMAC(secret, msg)

	return fmt.Sprintf("%s/%s?entry=%s&file=%s&exp=%d&sig=%s",
		baseURL,
		path,
		url.QueryEscape(entryID),
		url.QueryEscape(filename),
		exp,
		sig,
	)
}

// SignDownloadURL generates an HMAC-signed download URL (/dl).
func SignDownloadURL(baseURL, secret, entryID, filename string, ttl time.Duration) string {
	return SignURL(baseURL, "dl", secret, entryID, filename, ttl)
}

// SignUploadURL generates an HMAC-signed upload URL (/ul).
func SignUploadURL(baseURL, secret, entryID, filename string, ttl time.Duration) string {
	return SignURL(baseURL, "ul", secret, entryID, filename, ttl)
}

// ValidateSignature checks the HMAC signature and expiry of URL parameters.
// Returns the entryID and filename if valid.
func ValidateSignature(secret string, params url.Values) (entryID, filename string, err error) {
	entryID = params.Get("entry")
	filename = params.Get("file")
	expStr := params.Get("exp")
	sig := params.Get("sig")

	if entryID == "" || filename == "" || expStr == "" || sig == "" {
		return "", "", domain.ErrProxyURLInvalid
	}

	exp, err := strconv.ParseInt(expStr, 10, 64)
	if err != nil {
		return "", "", domain.ErrProxyURLInvalid
	}

	// Check expiry first
	if time.Now().Unix() > exp {
		return "", "", domain.ErrProxyURLExpired
	}

	// Verify HMAC
	msg := canonicalMessage(entryID, filename, exp)
	expected := computeHMAC(secret, msg)
	if !hmac.Equal([]byte(sig), []byte(expected)) {
		return "", "", domain.ErrProxyURLInvalid
	}

	return entryID, filename, nil
}

// canonicalMessage builds the canonical string for HMAC signing.
// Format: entry=<id>&file=<name>&exp=<unix>
func canonicalMessage(entryID, filename string, exp int64) string {
	return fmt.Sprintf("entry=%s&file=%s&exp=%d", entryID, filename, exp)
}

func computeHMAC(secret, message string) string {
	mac := hmac.New(sha256.New, []byte(secret))
	mac.Write([]byte(message))
	return hex.EncodeToString(mac.Sum(nil))
}
