//go:build linux

package security

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"syscall"
)

// calendarFeedFenceMountInfoMaxBytes bounds the mountinfo read. A container's
// table is a few kilobytes; a table past the bound is an unreadable one, which
// refuses (see judgeCalendarFeedFenceMount) rather than accepting on a part.
const calendarFeedFenceMountInfoMaxBytes = 4 << 20

// CheckCalendarFeedFenceMounted is the host's CalendarFeedFenceMountCheck: it
// stats the fence directory and "/", reads /proc/self/mountinfo, and hands the
// evidence to judgeCalendarFeedFenceMount.
//
// A directory that does not exist is not this check's subject and passes: the
// anchor already reads it as no token and its first write fails, which the
// boot pass refuses as an unwritable fence with its own message.
func CheckCalendarFeedFenceMounted(dir string) error {
	resolved, err := filepath.EvalSymlinks(filepath.Clean(dir))
	if errors.Is(err, fs.ErrNotExist) {
		return nil
	}
	if err != nil {
		resolved = filepath.Clean(dir)
	}
	return judgeCalendarFeedFenceMount(resolved, gatherCalendarFeedFenceMountEvidence(resolved))
}

func gatherCalendarFeedFenceMountEvidence(dir string) calendarFeedFenceMountEvidence {
	var evidence calendarFeedFenceMountEvidence
	evidence.fenceDevice, evidence.deviceErr = statDevice(dir)
	if evidence.deviceErr == nil {
		evidence.rootDevice, evidence.deviceErr = statDevice("/")
	}
	// procfs reports mountinfo as a regular file of size 0, so the bounded
	// reader's size check passes and its LimitReader does the bounding.
	evidence.mountInfo, evidence.mountInfoErr = ReadBoundedRegularFile("/proc/self/mountinfo", "mountinfo", calendarFeedFenceMountInfoMaxBytes)
	return evidence
}

func statDevice(path string) (uint64, error) {
	info, err := os.Stat(path)
	if err != nil {
		return 0, err
	}
	stat, ok := info.Sys().(*syscall.Stat_t)
	if !ok {
		return 0, fmt.Errorf("stat %s: no device number", path)
	}
	return uint64(stat.Dev), nil //nolint:unconvert // Dev is uint64 on amd64 but not on every linux GOARCH
}
