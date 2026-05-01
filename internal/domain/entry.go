package domain

import (
	"strings"
	"time"
	"unicode"
	"unicode/utf8"

	"github.com/google/uuid"
)

type EntryStatus string
type EntryType string

const (
	StatusActive  EntryStatus = "active"
	StatusExpired EntryStatus = "expired"

	EntryTypeText EntryType = "text"
	EntryTypeFile EntryType = "file"

	MaxPayloadSize       = 1048576 // 1MB
	MaxCollisionAttempts = 999
	MaxTTLHours          = 8760 // 1 year
	MaxWordLength        = 500  // rune count
	MaxPatternLength     = 500
	MaxLimit             = 100
	DefaultLimit         = 20
	MaxFilenameLength    = 255
)

func ValidateWord(word string) error {
	if word == "" {
		return ErrInvalidWord
	}
	runeCount := utf8.RuneCountInString(word)
	if runeCount > MaxWordLength {
		return ErrInvalidWord
	}
	// No leading/trailing whitespace
	first, _ := utf8.DecodeRuneInString(word)
	last, _ := utf8.DecodeLastRuneInString(word)
	if unicode.IsSpace(first) || unicode.IsSpace(last) {
		return ErrInvalidWord
	}
	// No control characters
	for _, r := range word {
		if unicode.IsControl(r) {
			return ErrInvalidWord
		}
	}
	return nil
}

type Entry struct {
	ID        uuid.UUID   `json:"id"`
	Word      string      `json:"word"`
	Payload   string      `json:"payload"`
	Status    EntryStatus `json:"status"`
	EntryType EntryType   `json:"entry_type"`
	CreatedAt time.Time   `json:"created_at"`
	UpdatedAt time.Time   `json:"updated_at"`
	ExpiresAt *time.Time  `json:"expires_at,omitempty"`
	CreatedBy *string     `json:"created_by,omitempty"`
}

// FileMetadata is stored as JSON in Payload for file entries.
type FileMetadata struct {
	S3Key       string `json:"s3_key"`
	Filename    string `json:"filename"`
	ContentType string `json:"content_type"`
	SizeLimit   int64  `json:"size_limit"`
}

func ValidateFilename(filename string) error {
	if filename == "" {
		return ErrInvalidFilename
	}
	runeCount := utf8.RuneCountInString(filename)
	if runeCount > MaxFilenameLength {
		return ErrInvalidFilename
	}
	for _, r := range filename {
		if unicode.IsControl(r) {
			return ErrInvalidFilename
		}
	}
	// Reject path traversal
	if strings.Contains(filename, "..") || strings.Contains(filename, "/") || strings.Contains(filename, "\\") {
		return ErrInvalidFilename
	}
	return nil
}
