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

// SignURL generates an HMAC-signed download URL.
// Format: <baseURL>/dl?entry=<id>&file=<name>&exp=<unix>&sig=<hex-hmac>
func SignURL(baseURL, secret, entryID, filename string, ttl time.Duration) string {
	exp := time.Now().Add(ttl).Unix()
	msg := canonicalMessage(entryID, filename, exp)
	sig := computeHMAC(secret, msg)

	return fmt.Sprintf("%s/dl?entry=%s&file=%s&exp=%d&sig=%s",
		baseURL,
		url.QueryEscape(entryID),
		url.QueryEscape(filename),
		exp,
		sig,
	)
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
