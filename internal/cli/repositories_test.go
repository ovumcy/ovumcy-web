package cli

import (
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
		// The mistake the warning exists for: a container path carried into a
		// host shell. The variable is set, so an emptiness check would pass it.
		"a container path in a host shell": filepath.Join(t.TempDir(), "never-mounted", "calendar-feed.fence"),
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
