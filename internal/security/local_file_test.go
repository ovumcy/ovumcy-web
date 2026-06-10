package security

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// ReadBoundedRegularFile guards the operator-supplied SECRET_KEY_FILE and
// OIDC_CA_FILE reads: it must reject directories / special files and cap the
// read size. These tests pin those boundaries so a future refactor that weakens
// the size cap or the regular-file check fails loudly rather than silently. (The
// size and regular-file conditionals previously had no test exercising them and
// survived mutation.)

func TestReadBoundedRegularFile_RejectsOversizedFile(t *testing.T) {
	path := filepath.Join(t.TempDir(), "secret.key")
	if err := os.WriteFile(path, []byte(strings.Repeat("x", 64)), 0o600); err != nil {
		t.Fatalf("write file: %v", err)
	}
	if _, err := ReadBoundedRegularFile(path, "TEST_FILE", 32); err == nil {
		t.Fatal("expected a file larger than maxBytes to be rejected")
	}
}

func TestReadBoundedRegularFile_RejectsDirectory(t *testing.T) {
	if _, err := ReadBoundedRegularFile(t.TempDir(), "TEST_FILE", 1024); err == nil {
		t.Fatal("expected a directory path to be rejected as non-regular")
	}
}

func TestReadBoundedRegularFile_RejectsBlankAndDotPath(t *testing.T) {
	if _, err := ReadBoundedRegularFile("   ", "TEST_FILE", 1024); err == nil {
		t.Fatal("expected a blank path to be rejected")
	}
	if _, err := ReadBoundedRegularFile(".", "TEST_FILE", 1024); err == nil {
		t.Fatal("expected '.' to be rejected")
	}
}

func TestReadBoundedRegularFile_RejectsMissingFile(t *testing.T) {
	if _, err := ReadBoundedRegularFile(filepath.Join(t.TempDir(), "nope.key"), "TEST_FILE", 1024); err == nil {
		t.Fatal("expected a missing file to be rejected")
	}
}

// TestReadBoundedRegularFile_ReadsFileExactlyAtLimit pins the inclusive
// boundary: a file whose size equals maxBytes must read successfully. A mutant
// that flips `info.Size() > maxBytes` to `>=` would reject this file and is
// killed here.
func TestReadBoundedRegularFile_ReadsFileExactlyAtLimit(t *testing.T) {
	path := filepath.Join(t.TempDir(), "secret.key")
	content := []byte(strings.Repeat("k", 32))
	if err := os.WriteFile(path, content, 0o600); err != nil {
		t.Fatalf("write file: %v", err)
	}
	got, err := ReadBoundedRegularFile(path, "TEST_FILE", 32)
	if err != nil {
		t.Fatalf("expected a file exactly at the byte limit to read, got %v", err)
	}
	if string(got) != string(content) {
		t.Fatalf("content mismatch: got %q want %q", got, content)
	}
}
