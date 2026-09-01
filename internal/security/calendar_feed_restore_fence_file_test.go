package security

import (
	"errors"
	"os"
	"path/filepath"
	"testing"
)

// TestCalendarFeedFenceFileRoundTripsAToken pins the ordinary lifecycle: an
// absent file is "no token yet" rather than an error, and what Write stored is
// what Read returns.
func TestCalendarFeedFenceFileRoundTripsAToken(t *testing.T) {
	path := filepath.Join(t.TempDir(), "calendar-feed.fence")
	fence := NewCalendarFeedFenceFile(path)

	value, found, err := fence.Read()
	if err != nil || found || value != "" {
		t.Fatalf("an absent fence must read as empty and not-found, got (%q, %v, %v)", value, found, err)
	}

	token, err := NewCalendarFeedFenceToken()
	if err != nil {
		t.Fatalf("NewCalendarFeedFenceToken: %v", err)
	}
	if err := fence.Write(token); err != nil {
		t.Fatalf("Write: %v", err)
	}

	value, found, err = fence.Read()
	if err != nil || !found || value != token {
		t.Fatalf("expected the stored token back, got (%q, %v, %v)", value, found, err)
	}

	// A rewrite replaces rather than appends — the fence is one token, and a
	// second one on a second line would read back as a value matching neither.
	second, err := NewCalendarFeedFenceToken()
	if err != nil {
		t.Fatalf("NewCalendarFeedFenceToken (second): %v", err)
	}
	if second == token {
		t.Fatal("two minted tokens must differ; the fence cannot detect anything if they do not")
	}
	if err := fence.Write(second); err != nil {
		t.Fatalf("Write (second): %v", err)
	}
	if value, _, err := fence.Read(); err != nil || value != second {
		t.Fatalf("expected the replaced token, got (%q, %v)", value, err)
	}
}

// TestCalendarFeedFenceFileWriteLeavesNoTemporaryFileBehind pins the atomic
// write's cleanup: a directory that accumulates one temp file per boot would
// eventually be the reason the fence volume fills up.
func TestCalendarFeedFenceFileWriteLeavesNoTemporaryFileBehind(t *testing.T) {
	directory := t.TempDir()
	fence := NewCalendarFeedFenceFile(filepath.Join(directory, "calendar-feed.fence"))
	for range 3 {
		token, err := NewCalendarFeedFenceToken()
		if err != nil {
			t.Fatalf("NewCalendarFeedFenceToken: %v", err)
		}
		if err := fence.Write(token); err != nil {
			t.Fatalf("Write: %v", err)
		}
	}

	entries, err := os.ReadDir(directory)
	if err != nil {
		t.Fatalf("ReadDir: %v", err)
	}
	if len(entries) != 1 || entries[0].Name() != "calendar-feed.fence" {
		names := make([]string, 0, len(entries))
		for _, entry := range entries {
			names = append(names, entry.Name())
		}
		t.Fatalf("expected only the fence file to remain, got %v", names)
	}
}

// TestCalendarFeedFenceFileUnconfiguredPathFailsBothHalves pins the state an
// operator who never set CALENDAR_FEED_FENCE_PATH is in. It must be an ERROR on
// both halves, never a quiet ("", false): a not-found read is how a first boot
// looks, and reporting "no fence configured" that way would arm a fence that
// exists nowhere and prove continuity on every later boot.
func TestCalendarFeedFenceFileUnconfiguredPathFailsBothHalves(t *testing.T) {
	fence := NewCalendarFeedFenceFile("   ")

	if _, _, err := fence.Read(); !errors.Is(err, ErrCalendarFeedFenceNotConfigured) {
		t.Fatalf("expected ErrCalendarFeedFenceNotConfigured from Read, got %v", err)
	}
	if err := fence.Write("token"); !errors.Is(err, ErrCalendarFeedFenceNotConfigured) {
		t.Fatalf("expected ErrCalendarFeedFenceNotConfigured from Write, got %v", err)
	}
}

// TestCalendarFeedFenceFileMissingDirectoryReadsAbsentAndFailsToWrite pins the
// shape of an unmounted fence volume, which is the whole reason the caller
// cannot decide from the read alone: the directory is not there, so the read
// looks exactly like a first boot and only the write can tell them apart.
func TestCalendarFeedFenceFileMissingDirectoryReadsAbsentAndFailsToWrite(t *testing.T) {
	fence := NewCalendarFeedFenceFile(filepath.Join(t.TempDir(), "never-mounted", "calendar-feed.fence"))

	value, found, err := fence.Read()
	if err != nil || found || value != "" {
		t.Fatalf("an unmounted fence must read as absent, not as an error, got (%q, %v, %v)", value, found, err)
	}
	if err := fence.Write("token"); err == nil {
		t.Fatal("writing into a missing directory must fail; creating it would replace the unmounted-volume signal with a fence that vanishes on the next restart")
	}
}

// TestCalendarFeedFenceFileTornWriteReadsAbsent pins the one content case that
// is not a token: an empty (or whitespace-only) file, which a torn write or a
// hand-cleared file leaves. Returning it as a value would compare "" against
// the stored marker and could match another empty half.
func TestCalendarFeedFenceFileTornWriteReadsAbsent(t *testing.T) {
	path := filepath.Join(t.TempDir(), "calendar-feed.fence")
	if err := os.WriteFile(path, []byte("  \n"), 0o600); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}

	value, found, err := NewCalendarFeedFenceFile(path).Read()
	if err != nil || found || value != "" {
		t.Fatalf("a blank fence file must read as absent, got (%q, %v, %v)", value, found, err)
	}
}

// TestCalendarFeedFenceFileRefusesANonRegularPath pins that a mis-set variable
// pointing at a directory is an error rather than a silent absence: absence is
// answered by arming a fence, and arming one at a path that can never hold a
// file would report continuity it cannot have.
func TestCalendarFeedFenceFileRefusesANonRegularPath(t *testing.T) {
	if _, _, err := NewCalendarFeedFenceFile(t.TempDir()).Read(); err == nil {
		t.Fatal("a directory path must be an error, not an absent fence")
	}
}
