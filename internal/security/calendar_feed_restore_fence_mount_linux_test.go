//go:build linux

package security

import (
	"errors"
	"path/filepath"
	"testing"
)

// TestCheckCalendarFeedFenceMountedReadsTheHost drives the real evidence
// gathering on shapes every Linux host answers the same way: "/" is the root
// itself, never a mount below it, and /proc is always its own filesystem.
func TestCheckCalendarFeedFenceMountedReadsTheHost(t *testing.T) {
	if err := CheckCalendarFeedFenceMounted("/"); !errors.Is(err, ErrCalendarFeedFenceNotMounted) {
		t.Fatalf("the root directory must read as unmounted, got %v", err)
	}
	if err := CheckCalendarFeedFenceMounted("/proc"); err != nil {
		t.Fatalf("/proc must read as mounted, got %v", err)
	}
	if err := CheckCalendarFeedFenceMounted(filepath.Join(t.TempDir(), "absent")); err != nil {
		t.Fatalf("a directory that does not exist is left to the anchor's own refusal, got %v", err)
	}
}
