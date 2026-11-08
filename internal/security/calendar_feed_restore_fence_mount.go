package security

import (
	"errors"
	"fmt"
	"path"
	"strings"
	"sync"
)

// CalendarFeedFenceRequireMountEnv names the variable that makes the server
// refuse a fence directory which is not a mounted volume. The container image
// sets it; a bare binary leaves it unset and keeps the behaviour it always had,
// since only inside a container is "a directory that exists and is writable"
// not the same as "a directory that survives the process".
//
// Without it, `docker run` with a data volume and no fence volume found the
// image's own /app/fence on the container's writable layer: the first start
// armed the fence there, and every recreation of the container — an image
// upgrade, a Watchtower pull, a Portainer redeploy — threw that file away, so
// the next start read the database as a restored backup and revoked every
// owner's sessions, local password and webhook endpoints.
const CalendarFeedFenceRequireMountEnv = "CALENDAR_FEED_FENCE_REQUIRE_MOUNT"

// ErrCalendarFeedFenceNotMounted is the anchor's refusal of a fence directory
// that is not a mounted volume. Read returns it before touching the file, so
// the boot pass reads it as an unusable fence: refused without
// CALENDAR_FEED_FENCE_OPTOUT, writing nothing in either half.
var ErrCalendarFeedFenceNotMounted = errors.New("calendar feed restore fence: the fence directory is not a mounted volume")

// CalendarFeedFenceMountCheck judges the directory that holds the fence file:
// nil when it is a mounted volume (or cannot be judged as a directory at all),
// an error wrapping ErrCalendarFeedFenceNotMounted otherwise.
type CalendarFeedFenceMountCheck func(dir string) error

// CalendarFeedFenceOption configures a CalendarFeedFenceFile.
type CalendarFeedFenceOption func(*CalendarFeedFenceFile)

// WithCalendarFeedFenceMountCheck makes the anchor run check on the fence
// directory once, before its first Read. A refusal is kept and returned by
// every later Read too, so one process never sees two answers.
//
// Once, and only on Read: the boot pass reads before it writes anything, so a
// refusal there stops the start before either half moves. A runtime fence
// advance also reads, and a second probe there would add a /proc read to every
// sign-out and let a transient failure of that read refuse an owner's write
// on a mount that has not changed since the boot accepted it.
func WithCalendarFeedFenceMountCheck(check CalendarFeedFenceMountCheck) CalendarFeedFenceOption {
	return func(fence *CalendarFeedFenceFile) {
		if check == nil {
			return
		}
		fence.mountCheck = &calendarFeedFenceMountGate{check: check}
	}
}

type calendarFeedFenceMountGate struct {
	check CalendarFeedFenceMountCheck
	once  sync.Once
	err   error
}

func (gate *calendarFeedFenceMountGate) verdict(dir string) error {
	gate.once.Do(func() { gate.err = gate.check(dir) })
	return gate.err
}

// calendarFeedFenceMountEvidence is what the host reports about the fence
// directory, gathered by the platform file and judged here so that every
// shape can be tested on any platform.
type calendarFeedFenceMountEvidence struct {
	// fenceDevice / rootDevice are st_dev of the fence directory and of "/".
	// deviceErr is set when either stat failed; the devices are then unknown.
	fenceDevice uint64
	rootDevice  uint64
	deviceErr   error
	// mountInfo is /proc/self/mountinfo; mountInfoErr is set when it could not
	// be read.
	mountInfo    []byte
	mountInfoErr error
}

// judgeCalendarFeedFenceMount decides whether dir (absolute, symlinks
// resolved) is a mounted volume that outlives the container.
//
// When the mount table is readable, the mount covering dir — the mountinfo
// entry at dir or at its nearest ancestor other than "/" — decides first:
//
//   - a covering tmpfs or ramfs refuses. Its st_dev differs from the root's
//     and it has a mountinfo entry, so either signal alone would accept it,
//     yet it is RAM: every restart of the container (not only a recreation)
//     loses the fence, and each next start would read as a restore and revoke
//     every owner's password, sessions and webhook endpoints. `--tmpfs
//     /app/fence` is what an operator on a read-only root reaches for after
//     the unwritable-fence refusal, so the device signal must never get to
//     accept past it;
//   - any other covering mount accepts — a named volume, a bind mount (from
//     the root's own device too), a PVC, a Docker Desktop grpcfuse/9p bind, a
//     volume mounted higher up (for example at /app);
//   - no covering mount: a device different from the root filesystem's still
//     accepts, as before; the root's device (or an unknown one) refuses.
//
// When the mount table is unreadable, the device signal alone decides: a
// device different from the root's accepts, the root's or an unknown device
// refuses. That accept cannot see the filesystem type, so a tmpfs is not
// refused there; it is kept because refusing it would newly turn away a
// mounted volume on a host that hides mountinfo, and a host that hides it is
// already outside the container this check is written for.
//
// Unreadable evidence otherwise refuses — fail closed — deliberately. The check
// runs only where the image asked for it, inside a container, where
// /proc/self/mountinfo is always present; a host that hides it is one this
// check cannot vouch for. Accepting there would let through the exact defect it
// exists for — every recreation silently revoking every owner's password —
// while refusing is loud, writes nothing, and names its two escapes (mount a
// volume, or opt out).
func judgeCalendarFeedFenceMount(dir string, evidence calendarFeedFenceMountEvidence) error {
	if evidence.mountInfoErr == nil {
		covering, fsType := coveringCalendarFeedFenceMount(dir, evidence.mountInfo)
		if isVolatileCalendarFeedFenceFSType(fsType) {
			return fmt.Errorf("%w: %s is on a %s mount at %s, which lives in memory, so a fence kept there is lost whenever the container restarts and every next start would read the database as a restored backup and revoke every owner's password. Mount a named volume or a bind mount from disk at %s instead (for example -v ovumcy_fence:%s)",
				ErrCalendarFeedFenceNotMounted, dir, fsType, covering, dir, dir)
		}
		if covering != "" {
			return nil
		}
	}
	if evidence.deviceErr == nil && evidence.fenceDevice != evidence.rootDevice {
		return nil
	}
	if evidence.deviceErr != nil || evidence.mountInfoErr != nil {
		return fmt.Errorf("%w: %s could not be confirmed to be a mounted volume (device: %v; mountinfo: %v), so a fence kept there may not survive a recreation of the container. Mount a volume at %s (for example -v ovumcy_fence:%s)",
			ErrCalendarFeedFenceNotMounted, dir, errOrOK(evidence.deviceErr), errOrOK(evidence.mountInfoErr), dir, dir)
	}
	return fmt.Errorf("%w: %s is on the container's writable layer (same device as /, no mount at or above it), so a fence kept there is thrown away whenever the container is recreated and the next start would read the database as a restored backup and revoke every owner's password. Mount a volume at %s (for example -v ovumcy_fence:%s), and never recreate the container without it",
		ErrCalendarFeedFenceNotMounted, dir, dir, dir)
}

func errOrOK(err error) string {
	if err == nil {
		return "ok"
	}
	return err.Error()
}

// isVolatileCalendarFeedFenceFSType reports whether a mount of this filesystem
// type keeps its files only in memory, so that they do not survive a restart.
func isVolatileCalendarFeedFenceFSType(fsType string) bool {
	return fsType == "tmpfs" || fsType == "ramfs"
}

// coveringCalendarFeedFenceMount returns the mountinfo mount point that is dir
// or its nearest ancestor, "/" excluded, and that mount's filesystem type, or
// two empty strings when none is. Of several entries at the same mount point
// the last one wins: a later mount stacks over an earlier one and is the one
// dir resolves to.
func coveringCalendarFeedFenceMount(dir string, mountInfo []byte) (string, string) {
	dir = path.Clean(dir)
	covering, coveringType := "", ""
	for _, line := range strings.Split(string(mountInfo), "\n") {
		// Field 5 (index 4) is the mount point, relative to this process's root.
		fields := strings.Fields(line)
		if len(fields) < 5 {
			continue
		}
		mountPoint := path.Clean(unescapeMountInfoField(fields[4]))
		if mountPoint == "/" || !strings.HasPrefix(mountPoint, "/") {
			continue
		}
		if (dir == mountPoint || strings.HasPrefix(dir, mountPoint+"/")) && len(mountPoint) >= len(covering) {
			covering, coveringType = mountPoint, mountInfoFSType(fields)
		}
	}
	return covering, coveringType
}

// mountInfoFSType returns the filesystem type of one mountinfo line split into
// fields: the field after the lone "-" that ends the optional fields. Those
// start at field 7 (index 6) and vary in number, from none to several, so the
// separator is searched for rather than counted to. A line without one yields
// "".
func mountInfoFSType(fields []string) string {
	for i := 6; i < len(fields)-1; i++ {
		if fields[i] == "-" {
			return unescapeMountInfoField(fields[i+1])
		}
	}
	return ""
}

// unescapeMountInfoField decodes the kernel's octal escapes (\040 for a space,
// \011, \012, \134) in a mountinfo path field.
func unescapeMountInfoField(field string) string {
	if !strings.Contains(field, `\`) {
		return field
	}
	var out strings.Builder
	for i := 0; i < len(field); i++ {
		if field[i] == '\\' && i+3 < len(field) && isOctalDigit(field[i+1]) && isOctalDigit(field[i+2]) && isOctalDigit(field[i+3]) {
			out.WriteByte((field[i+1]-'0')<<6 | (field[i+2]-'0')<<3 | (field[i+3] - '0'))
			i += 3
			continue
		}
		out.WriteByte(field[i])
	}
	return out.String()
}

func isOctalDigit(b byte) bool { return b >= '0' && b <= '7' }
