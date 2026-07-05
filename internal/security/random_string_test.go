package security

import (
	"strings"
	"testing"
)

func TestRandomString(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name     string
		length   int
		alphabet string
		wantErr  bool
	}{
		{
			name:     "negative length",
			length:   -1,
			alphabet: "abc",
			wantErr:  true,
		},
		{
			name:     "empty alphabet",
			length:   1,
			alphabet: "",
			wantErr:  true,
		},
		{
			name:     "zero length",
			length:   0,
			alphabet: "abc",
			wantErr:  false,
		},
		{
			name:     "single alphabet character",
			length:   8,
			alphabet: "X",
			wantErr:  false,
		},
		{
			name:     "normal generation",
			length:   64,
			alphabet: "ABCDEFGHJKLMNPQRSTUVWXYZ23456789",
			wantErr:  false,
		},
	}

	for _, test := range tests {

		t.Run(test.name, func(t *testing.T) {
			t.Parallel()

			got, err := RandomString(test.length, test.alphabet)
			if test.wantErr {
				if err == nil {
					t.Fatalf("RandomString(%d, %q) expected error, got nil", test.length, test.alphabet)
				}
				return
			}

			if err != nil {
				t.Fatalf("RandomString(%d, %q) returned error: %v", test.length, test.alphabet, err)
			}
			if len(got) != test.length {
				t.Fatalf("RandomString(%d, %q) len = %d, want %d", test.length, test.alphabet, len(got), test.length)
			}

			if test.alphabet == "X" {
				if got != strings.Repeat("X", test.length) {
					t.Fatalf("RandomString(%d, %q) = %q, want %q", test.length, test.alphabet, got, strings.Repeat("X", test.length))
				}
				return
			}

			for _, char := range got {
				if !strings.ContainsRune(test.alphabet, char) {
					t.Fatalf("RandomString(%d, %q) produced char %q outside alphabet", test.length, test.alphabet, char)
				}
			}
		})
	}
}
