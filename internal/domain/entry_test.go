package domain

import (
	"strings"
	"testing"
)

func TestValidateWord(t *testing.T) {
	tests := []struct {
		name    string
		word    string
		wantErr bool
	}{
		{"english", "rabbit", false},
		{"single char", "a", false},
		{"with digits", "rabbit2", false},
		{"with spaces", "hello world", false},
		{"with hyphens", "hello-world", false},
		{"sentence", "my favorite prompt", false},
		{"mixed", "test 123 phrase", false},
		{"russian", "кролик", false},
		{"russian sentence", "мой любимый промпт", false},
		{"chinese", "兔子", false},
		{"emoji", "🐰 rabbit", false},
		{"mixed languages", "hello мир world", false},
		{"uppercase", "Hello World", false},
		{"empty", "", true},
		{"leading space", " leading", true},
		{"trailing space", "trailing ", true},
		{"tab char", "hello\tworld", true},
		{"newline", "hello\nworld", true},
		{"null byte", "hello\x00world", true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := ValidateWord(tt.word)
			if (err != nil) != tt.wantErr {
				t.Errorf("ValidateWord(%q) error = %v, wantErr %v", tt.word, err, tt.wantErr)
			}
		})
	}
}

func TestValidateWord_MaxLength(t *testing.T) {
	// 500 runes should pass
	word := strings.Repeat("a", 500)
	if err := ValidateWord(word); err != nil {
		t.Errorf("500-rune word should be valid, got: %v", err)
	}

	// 501 runes should fail
	word = strings.Repeat("a", 501)
	if err := ValidateWord(word); err == nil {
		t.Error("501-rune word should be invalid")
	}

	// 500 multi-byte runes should pass
	word = strings.Repeat("я", 500)
	if err := ValidateWord(word); err != nil {
		t.Errorf("500 Cyrillic runes should be valid, got: %v", err)
	}

	// 501 multi-byte runes should fail
	word = strings.Repeat("я", 501)
	if err := ValidateWord(word); err == nil {
		t.Error("501 Cyrillic runes should be invalid")
	}
}
