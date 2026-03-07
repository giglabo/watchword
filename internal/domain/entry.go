package domain

import (
	"time"
	"unicode"
	"unicode/utf8"

	"github.com/google/uuid"
)

type EntryStatus string

const (
	StatusActive  EntryStatus = "active"
	StatusExpired EntryStatus = "expired"

	MaxPayloadSize       = 1048576 // 1MB
	MaxCollisionAttempts = 999
	MaxTTLHours          = 8760 // 1 year
	MaxWordLength        = 500  // rune count
	MaxPatternLength     = 500
	MaxLimit             = 100
	DefaultLimit         = 20
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
	CreatedAt time.Time   `json:"created_at"`
	UpdatedAt time.Time   `json:"updated_at"`
	ExpiresAt *time.Time  `json:"expires_at,omitempty"`
}
