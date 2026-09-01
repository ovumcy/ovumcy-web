package cli

import (
	"strings"
	"testing"

	"github.com/ovumcy/ovumcy-web/internal/security"
)

// TestCalendarFeedFenceWarningSpeaksOnlyWhenTheFenceIsMissing pins both halves
// of the CLI's answer to an unreachable fence. It must warn, because the
// server's next boot will disarm every armed feed and an operator who was not
// told reads that as a backup restore nobody performed. And it must stay silent
// otherwise, because a warning printed on every ordinary `ovumcy users list` is
// a warning nobody reads by the time it matters.
func TestCalendarFeedFenceWarningSpeaksOnlyWhenTheFenceIsMissing(t *testing.T) {
	if got := calendarFeedFenceWarning("/app/fence/calendar-feed.fence"); got != "" {
		t.Fatalf("a configured fence must warn about nothing, got %q", got)
	}

	warning := calendarFeedFenceWarning("")
	if warning == "" {
		t.Fatal("an unset fence must warn: the operator is the only one who can connect the coming disarm to this command")
	}
	for _, want := range []string{security.CalendarFeedFencePathEnv, "disarms every armed calendar feed", "docker compose exec"} {
		if !strings.Contains(warning, want) {
			t.Fatalf("the warning must name %q so it is actionable, got %q", want, warning)
		}
	}
}
