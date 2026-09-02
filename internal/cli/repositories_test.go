package cli

import (
	"bytes"
	"path/filepath"
	"strings"
	"testing"

	"github.com/ovumcy/ovumcy-web/internal/security"
)

// TestCalendarFeedFenceWarningSpeaksOnlyWhenTheFenceIsUnreachable pins both
// halves of the CLI's answer. It must warn, because the server's next boot will
// disarm every armed feed and an operator who was not told reads that as a
// backup restore nobody performed. And it must stay silent when the fence is
// reachable, because a warning printed beside every ordinary command is one
// nobody reads by the time it matters.
func TestCalendarFeedFenceWarningSpeaksOnlyWhenTheFenceIsUnreachable(t *testing.T) {
	reachable := filepath.Join(t.TempDir(), "calendar-feed.fence")
	if got := calendarFeedFenceWarning(reachable); got != "" {
		t.Fatalf("a fence whose directory exists must warn about nothing, got %q", got)
	}

	cases := map[string]string{
		"unset": "",
		// The variable is set but its directory is gone, so an emptiness check
		// would pass it. Written with the host's own separator.
		"a set path whose directory is gone": filepath.Join(t.TempDir(), "never-mounted", "calendar-feed.fence"),
		// The mistake the warning exists for, in the spelling it actually
		// arrives in: the value copied out of the compose file. It is POSIX
		// absolute and therefore NOT filepath.IsAbs on Windows, where this CLI
		// is developed — judging it by IsAbs alone left the probe silent on the
		// only case it was written for.
		"a container path in a host shell": "/app/fence/never-mounted/calendar-feed.fence",
	}
	for name, fencePath := range cases {
		t.Run(name, func(t *testing.T) {
			warning := calendarFeedFenceWarning(fencePath)
			if warning == "" {
				t.Fatal("an unreachable fence must warn: the operator is the only one who can connect the coming disarm to this command")
			}
			for _, want := range []string{security.CalendarFeedFencePathEnv, "disarms every armed calendar feed", "docker compose exec"} {
				if !strings.Contains(warning, want) {
					t.Fatalf("the warning must name %q so it is actionable, got %q", want, warning)
				}
			}
		})
	}
}

// TestCalendarFeedFenceWarningSaysNothingAboutARelativePath pins the one shape
// the probe must NOT judge. A relative path resolves against the working
// directory, and this process's is not the server's, so a missing directory
// here is no evidence about the fence — and a warning that cries wolf is worse
// than none, because the next real one reads the same.
func TestCalendarFeedFenceWarningSaysNothingAboutARelativePath(t *testing.T) {
	if got := calendarFeedFenceWarning(filepath.Join("state", "never-mounted", "calendar-feed.fence")); got != "" {
		t.Fatalf("a relative path must not be judged from this process's working directory, got %q", got)
	}
}

// TestRootedPathAcceptsTheRootsFilepathIsAbsMisses is the half the silence
// above cannot prove: a probe deleted outright would satisfy that test too. It
// runs on the classifier directly, touching no filesystem.
//
// The two rooted shapes are deliberate, because one of them alone would guard
// only the platform it was written on. A leading slash is IsAbs on Linux, so
// putting filepath.IsAbs back would still pass there — on a Linux CI the
// regression would go through green. A leading backslash is IsAbs on NEITHER
// platform, so it is the case that fails everywhere the suite runs.
func TestRootedPathAcceptsTheRootsFilepathIsAbsMisses(t *testing.T) {
	for _, fencePath := range []string{
		"/app/fence/calendar-feed.fence",
		`\app\fence\calendar-feed.fence`,
	} {
		if !rootedPath(fencePath) {
			t.Fatalf("%q names a location no working directory changes: judging it by filepath.IsAbs alone silences the probe on the value an operator copies out of the compose file", fencePath)
		}
	}
	if rootedPath(filepath.Join("state", "calendar-feed.fence")) {
		t.Fatal("a path with no root must stay unjudged: it resolves against a working directory that is not the server's")
	}
}

// TestWarnAboutAnUnreachableCalendarFeedFenceWritesTheLine covers the half that
// actually reaches an operator. calendarFeedFenceWarning composing the right
// text proves nothing if the subcommands never print it, and only this call is
// wired into `reset` and `users delete`.
func TestWarnAboutAnUnreachableCalendarFeedFenceWritesTheLine(t *testing.T) {
	t.Setenv(security.CalendarFeedFencePathEnv, "")
	var unset bytes.Buffer
	warnAboutAnUnreachableCalendarFeedFence(&unset)
	if !strings.HasSuffix(unset.String(), "\n") || !strings.Contains(unset.String(), security.CalendarFeedFencePathEnv) {
		t.Fatalf("an unreachable fence must write one complete line, got %q", unset.String())
	}

	t.Setenv(security.CalendarFeedFencePathEnv, filepath.Join(t.TempDir(), "calendar-feed.fence"))
	var reachable bytes.Buffer
	warnAboutAnUnreachableCalendarFeedFence(&reachable)
	if reachable.Len() != 0 {
		t.Fatalf("a reachable fence must write nothing, got %q", reachable.String())
	}
}
