package services

import (
	"strings"
	"testing"
)

func TestNormalizeSymptomIconInput(t *testing.T) {
	t.Run("blank falls back to the default icon", func(t *testing.T) {
		got, err := normalizeSymptomIconInput("   ")
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if got != defaultSymptomIcon {
			t.Fatalf("got %q, want default %q", got, defaultSymptomIcon)
		}
	})

	t.Run("valid emoji is kept", func(t *testing.T) {
		got, err := normalizeSymptomIconInput("🌙")
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if got != "🌙" {
			t.Fatalf("got %q, want 🌙", got)
		}
	})

	t.Run("markup is rejected like name labels", func(t *testing.T) {
		if _, err := normalizeSymptomIconInput("<b>"); err != ErrSymptomNameInvalidCharacters {
			t.Fatalf("got %v, want ErrSymptomNameInvalidCharacters", err)
		}
	})

	t.Run("control characters are rejected", func(t *testing.T) {
		if _, err := normalizeSymptomIconInput("x\x00"); err != ErrSymptomNameInvalidCharacters {
			t.Fatalf("got %v, want ErrSymptomNameInvalidCharacters", err)
		}
	})

	t.Run("over-long icon is rejected", func(t *testing.T) {
		if _, err := normalizeSymptomIconInput(strings.Repeat("x", maxSymptomIconLength+1)); err != ErrSymptomNameTooLong {
			t.Fatalf("got %v, want ErrSymptomNameTooLong", err)
		}
	})
}
