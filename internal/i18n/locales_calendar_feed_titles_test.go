package i18n_test

import (
	"strings"
	"testing"
)

// The detailed calendar-feed titles (WEB-379) leave the instance for a
// third-party calendar, so every locale must keep the estimate qualifier the
// in-app ovulation label already carries. The qualifier is read from
// calendar.ovulation_icon's parenthesised suffix, the same word each locale
// already uses for "estimate".
func TestCalendarFeedTitlesCarryTheEstimateQualifierInEveryLocale(t *testing.T) {
	for language, messages := range shippedCatalogues(t) {
		icon := messages["calendar.ovulation_icon"]
		open := strings.LastIndex(icon, "(")
		if open < 0 || !strings.HasSuffix(icon, ")") {
			t.Fatalf("%s: calendar.ovulation_icon %q has no parenthesised qualifier to compare against", language, icon)
		}
		qualifier := icon[open:]
		for _, key := range []string{"calendar_feed.event.period", "calendar_feed.event.ovulation"} {
			title := messages[key]
			if strings.TrimSpace(title) == "" {
				t.Fatalf("%s: %s is empty, which silently reverts the opt-in to the neutral title", language, key)
			}
			if !strings.HasSuffix(title, qualifier) {
				t.Fatalf("%s: %s = %q lacks the estimate qualifier %q", language, key, title, qualifier)
			}
		}
		if messages["calendar_feed.event.period"] == messages["calendar_feed.event.ovulation"] {
			t.Fatalf("%s: the two feed titles are identical", language)
		}
	}
}
