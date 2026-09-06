package security

import (
	"errors"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

// TestCalendarFeedFencePathRootedAcceptsTheRootsFilepathIsAbsMisses runs on the
// classifier directly, touching no filesystem.
//
// Most cases below hold on every GOOS the suite runs on, and are asserted
// unconditionally so a regression on either platform fails here rather than
// only in a CI job that happens to run on the other one:
//
//   - "/app/fence/..." must be accepted everywhere: it is precisely the value
//     an operator copies out of any shipped compose file, and precisely the
//     case filepath.IsAbs alone misses on Windows (it demands a drive letter).
//   - A leading BACKslash must be refused everywhere: on Linux it is one
//     ordinary relative filename (backslash is not a separator there), and on
//     Windows it is drive-relative, not rooted to a fixed location — the same
//     reason the bare drive-relative case just below is refused. Accepting it
//     on Linux is exactly the server-vs-CLI divergence this predicate exists
//     to close, so putting the old unconditional branch back must fail here.
//   - A path with no root at all, and a drive-relative one (a volume name with
//     no separator after it), must stay unjudged on every platform: both
//     resolve against a working directory that is not provably the server's.
//
// The drive-ABSOLUTE case is the one place GOOS changes the answer, because
// Go's own filepath.IsAbs does: on Windows a drive letter followed by a
// separator is what IsAbs considers absolute, so this is the positive case a
// mutant that dropped filepath.IsAbs from the predicate — keeping only the
// forward-slash branch — would still pass without. On every other platform
// the same string has no leading "/" and no meaning as a drive letter, so it
// is judged exactly like any other relative-looking string and refused.
//
// Both callers of this predicate — the server's boot-time config load and the
// operator CLI's revocation gate — depend on it answering the same way, which is
// why it is tested once, here, rather than per caller.
func TestCalendarFeedFencePathRootedAcceptsTheRootsFilepathIsAbsMisses(t *testing.T) {
	if !CalendarFeedFencePathRooted("/app/fence/calendar-feed.fence") {
		t.Fatal(`"/app/fence/calendar-feed.fence" names a location no working directory changes: judging it by filepath.IsAbs alone silences the check on the value an operator copies out of the compose file`)
	}
	if CalendarFeedFencePathRooted(`\app\fence\calendar-feed.fence`) {
		t.Fatal(`a leading backslash must stay unjudged: on Linux it is one relative filename, and on Windows it resolves against the current drive's own working directory, not a fixed location`)
	}
	if CalendarFeedFencePathRooted(filepath.Join("state", "calendar-feed.fence")) {
		t.Fatal("a path with no root must stay unjudged: it resolves against a working directory that is not the server's")
	}
	// Drive-relative on Windows: a volume name is not a root, the path still
	// resolves against that drive's working directory. Widening the predicate
	// to "has a volume name" would accept it on every platform.
	if CalendarFeedFencePathRooted(`C:state\calendar-feed.fence`) {
		t.Fatal("a drive-relative path must stay unjudged: it resolves against that drive's working directory, which is not the server's")
	}

	const driveAbsolute = `C:\app\fence\calendar-feed.fence`
	if runtime.GOOS == "windows" {
		if !CalendarFeedFencePathRooted(driveAbsolute) {
			t.Fatalf("%q must be accepted on Windows: a drive letter followed by a separator is what filepath.IsAbs treats as absolute there, and a host configured this way must be able to boot", driveAbsolute)
		}
	} else if CalendarFeedFencePathRooted(driveAbsolute) {
		t.Fatalf("%q must stay unjudged on %s: a Windows drive letter is not a root here, so this reads as an ordinary relative-looking string", driveAbsolute, runtime.GOOS)
	}
}

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

// TestCalendarFeedFenceFileWriteReportsAFailedRename covers the last step of
// the atomic write, the one that decides whether the token is published at all.
// A path whose final component is a directory is the reachable way to fail it —
// the same mis-set variable the read above refuses — and the failure has to
// surface: a Write that returned nil here would tell the fence a token was
// recorded outside the database when none was, and the boot pass would then
// read agreement it has no basis for.
func TestCalendarFeedFenceFileWriteReportsAFailedRename(t *testing.T) {
	occupied := filepath.Join(t.TempDir(), "calendar-feed.fence")
	if err := os.Mkdir(occupied, 0o755); err != nil {
		t.Fatalf("occupy the fence path with a directory: %v", err)
	}

	err := NewCalendarFeedFenceFile(occupied).Write("token-that-cannot-land")
	if err == nil {
		t.Fatal("a rename onto a directory must be reported, not swallowed")
	}
	if !strings.Contains(err.Error(), occupied) {
		t.Fatalf("the error must name the path an operator has to fix, got %v", err)
	}

	// The temp file is removed on this path too: a fence directory that fills
	// with .calendar-feed-fence-* leftovers after every failed write is a second
	// failure an operator has to notice separately.
	entries, err := os.ReadDir(filepath.Dir(occupied))
	if err != nil {
		t.Fatalf("read the fence directory: %v", err)
	}
	for _, entry := range entries {
		if strings.HasPrefix(entry.Name(), ".calendar-feed-fence-") {
			t.Fatalf("a failed write must leave no temporary file behind, found %q", entry.Name())
		}
	}
}
