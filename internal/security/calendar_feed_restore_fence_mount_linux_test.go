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
	// The device signal itself, not only the mount table: procfs has its own
	// st_dev on every Linux host.
	if evidence := gatherCalendarFeedFenceMountEvidence("/proc"); evidence.deviceErr != nil || evidence.fenceDevice == evidence.rootDevice {
		t.Fatalf("/proc must read as a device of its own, got fence=%d root=%d err=%v", evidence.fenceDevice, evidence.rootDevice, evidence.deviceErr)
	}
	if err := CheckCalendarFeedFenceMounted(filepath.Join(t.TempDir(), "absent")); err != nil {
		t.Fatalf("a directory that does not exist is left to the anchor's own refusal, got %v", err)
	}
}

// TestCheckCalendarFeedFenceMountedDecidesOnMountInfoWhenTheDeviceIsUnreadable
// takes a path whose stat fails for a reason other than absence — a component
// below a regular file is "not a directory", not "does not exist" — so neither
// symlink resolution nor the device comparison yields evidence, and the mount
// table alone decides: /proc is its own mount on every Linux host.
func TestCheckCalendarFeedFenceMountedDecidesOnMountInfoWhenTheDeviceIsUnreadable(t *testing.T) {
	const belowAFile = "/proc/version/fence"
	evidence := gatherCalendarFeedFenceMountEvidence(belowAFile)
	if evidence.deviceErr == nil {
		t.Fatalf("stat of %s must fail, so the device comparison has no evidence", belowAFile)
	}
	if evidence.mountInfoErr != nil {
		t.Fatalf("the mount table must still read: %v", evidence.mountInfoErr)
	}
	if err := CheckCalendarFeedFenceMounted(belowAFile); err != nil {
		t.Fatalf("a directory under the /proc mount must be accepted on the mount table alone, got %v", err)
	}
}
