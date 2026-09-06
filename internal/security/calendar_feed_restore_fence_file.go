package security

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
)

const (
	// calendarFeedFenceTokenLength / calendarFeedFenceTokenAlphabet size the
	// fence token. It is an identity, not a secret: knowing it grants nothing,
	// because the only value it is ever compared against is one an attacker
	// would already need write access to this host to influence.
	calendarFeedFenceTokenLength   = 32
	calendarFeedFenceTokenAlphabet = "abcdefghijklmnopqrstuvwxyzABCDEFGHIJKLMNOPQRSTUVWXYZ0123456789"

	// calendarFeedFenceMaxBytes bounds the read of an operator-managed path, so
	// a wrong CALENDAR_FEED_FENCE_PATH cannot pull an arbitrary file into memory.
	calendarFeedFenceMaxBytes = 512

	calendarFeedFenceLabel = "calendar feed restore fence"
)

// CalendarFeedFencePathEnv names the variable that points at the fence file.
// It is declared once, here beside the reader, because the server and the
// operator CLI must resolve the SAME fence: a CLI reading a different name
// silently writes only the database half, and the server's next boot reads that
// as a restore and disarms every armed feed.
const CalendarFeedFencePathEnv = "CALENDAR_FEED_FENCE_PATH"

// CalendarFeedFencePathRooted reports whether a configured fence path names a
// location no working directory changes. It lives here, beside the variable's
// name, for the same reason that name does: the server and the operator CLI
// must accept the SAME set of values. A path one side takes and the other
// refuses gives the server a fence no operator command can ever confirm, and a
// removal the CLI then refuses to perform.
//
// filepath.IsAbs is not enough on its own: this is developed on Windows, where
// it demands a drive letter and therefore calls `/app/fence/calendar-feed.fence`
// relative — which is precisely the value an operator copies out of every
// shipped compose file (all of them use forward slashes), and precisely the
// case the check exists to accept. A leading forward slash settles it on
// either platform.
//
// A leading BACKslash is deliberately not treated the same way, even though it
// looks like the same shape with the other separator: on Linux a backslash is
// an ordinary filename character, so `\app\fence\x.fence` is one relative
// component that resolves against whatever directory started the process —
// exactly the server-vs-CLI divergence this predicate exists to close, not a
// case it should accept. On Windows it is drive-relative — rooted to the
// CURRENT drive, not to a fixed location — for the same reason a bare
// `C:state\...` below is refused rather than accepted. No shipped compose file
// ever produces this shape, so there is no real value that needs it.
//
// The empty path is not this predicate's subject: "not configured" is a normal
// operator state both sides answer on their own (the server fails closed, the
// CLI refuses with its own message), so callers judge emptiness first.
func CalendarFeedFencePathRooted(path string) bool {
	return filepath.IsAbs(path) || strings.HasPrefix(path, "/")
}

// ErrCalendarFeedFenceNotConfigured is returned by both halves of the anchor
// when no path was supplied. It is a normal operator state, never a boot
// failure: the caller answers it by disarming feeds, not by exiting.
var ErrCalendarFeedFenceNotConfigured = errors.New("calendar feed restore fence: CALENDAR_FEED_FENCE_PATH is not set")

// NewCalendarFeedFenceToken mints the value that identifies one continuous run
// of this instance against one generation of its database.
func NewCalendarFeedFenceToken() (string, error) {
	return RandomString(calendarFeedFenceTokenLength, calendarFeedFenceTokenAlphabet)
}

// CalendarFeedFenceFile is the on-disk half of the calendar-feed restore fence:
// the half a database restore does NOT roll back.
//
// The key-epoch sentinel next door catches a SECRET_KEY rotation, and it can
// only catch that, because the epoch it compares against lives in app_state —
// inside the very dump a restore replaces. Restoring a backup taken before a
// revocation therefore returned the feed columns AND the epoch together, the
// comparison matched, and a subscribe URL the owner had revoked served the
// calendar again. Containment has to survive a restore (see
// docs/SECURITY_INVARIANTS.md), so the fence's second half is deliberately
// placed outside the database: a small file the operator mounts from a
// directory that is not part of any database backup. After a restore that file
// still holds the token this instance minted, the restored app_state holds an
// older one, and the mismatch is the restore.
//
// The path is operator-supplied and may legitimately be absent (no mount, no
// variable, a bare binary). Absence is reported, never fatal: a missing fence
// cannot prove continuity, so the caller disarms every armed feed instead of
// refusing to start.
type CalendarFeedFenceFile struct {
	path string
}

// NewCalendarFeedFenceFile wires the anchor at path. An empty path is accepted
// and makes both operations return ErrCalendarFeedFenceNotConfigured, so "not
// configured" reaches the caller through the same channel as "not writable"
// and cannot be mistaken for a proof of continuity.
func NewCalendarFeedFenceFile(path string) *CalendarFeedFenceFile {
	return &CalendarFeedFenceFile{path: strings.TrimSpace(path)}
}

// Read returns the stored token. A file that does not exist yet — including one
// whose parent directory is missing, which is what an unmounted fence volume
// looks like — reads as ("", false, nil): a first boot and an unmounted volume
// are told apart by whether Write then succeeds, never by the read. Every other
// failure is returned, because a fence that cannot be read has not been proved
// equal to anything.
func (fence *CalendarFeedFenceFile) Read() (string, bool, error) {
	if fence.path == "" {
		return "", false, ErrCalendarFeedFenceNotConfigured
	}
	content, err := ReadBoundedRegularFile(fence.path, calendarFeedFenceLabel, calendarFeedFenceMaxBytes)
	if errors.Is(err, fs.ErrNotExist) {
		return "", false, nil
	}
	if err != nil {
		return "", false, err
	}
	// A zero-length file is a torn write, not a token: treat it as absent so the
	// caller disarms rather than comparing against nothing.
	value := strings.TrimSpace(string(content))
	if value == "" {
		return "", false, nil
	}
	return value, true, nil
}

// Write replaces the stored token atomically — a temp file in the same
// directory, then a rename — so a crash mid-write leaves the previous token
// rather than a truncated one. The parent directory is never created: an absent
// directory IS the unmounted-volume signal, and silently creating it on the
// container's read-only layer (or on a developer's disk) would replace that
// signal with a fence that vanishes again on the next restart.
func (fence *CalendarFeedFenceFile) Write(value string) error {
	if fence.path == "" {
		return ErrCalendarFeedFenceNotConfigured
	}
	cleanPath := filepath.Clean(fence.path)
	temp, err := os.CreateTemp(filepath.Dir(cleanPath), ".calendar-feed-fence-*")
	if err != nil {
		return fmt.Errorf("%s could not be written: %s: %w", calendarFeedFenceLabel, fence.path, err)
	}
	tempName := temp.Name()
	// Removes the temp file on every failure path; a no-op once the rename below
	// has moved it away.
	defer func() { _ = os.Remove(tempName) }()

	// The three failure arms below refuse a file this call created moments
	// earlier, in a directory os.CreateTemp had just proved writable: a volume
	// that went away mid-write, or a disk that filled between two syscalls.
	// Nothing this package exposes reaches them, and a fake that could would be
	// testing the fake — so each arm's BODY is marked, leaving the calls
	// themselves measured. The step that CAN fail on an operator's own mistake
	// is the rename below, and that one is covered by a test.
	if _, err := temp.WriteString(value + "\n"); err != nil {
		_ = temp.Close() // codecov:ignore -- a write refused on a file this call just created
		return fmt.Errorf("%s could not be written: %s: %w", calendarFeedFenceLabel, fence.path, err)
	}
	if err := temp.Sync(); err != nil {
		_ = temp.Close() // codecov:ignore -- a sync refused on a file this call just created
		return fmt.Errorf("%s could not be written: %s: %w", calendarFeedFenceLabel, fence.path, err)
	}
	if err := temp.Close(); err != nil {
		// codecov:ignore -- a close refused on a file this call just created
		return fmt.Errorf("%s could not be written: %s: %w", calendarFeedFenceLabel, fence.path, err)
	}
	if err := os.Rename(tempName, cleanPath); err != nil {
		return fmt.Errorf("%s could not be written: %s: %w", calendarFeedFenceLabel, fence.path, err)
	}
	return nil
}
