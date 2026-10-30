package services

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/ovumcy/ovumcy-web/internal/models"
)

// WEB-379: the detailed-titles opt-in. The neutral default is the privacy
// floor, so it is pinned byte-for-byte against a body built with the titles
// supplied but the opt-in off. Cited by SECURITY.md's Calendar Feed rows.

var testDetailedFeedTitles = CalendarFeedTitles{Period: "Period (estimate)", Ovulation: "Ovulation (estimate)"}

func detailedTitlesFeedInput(t *testing.T, optIn bool, titles CalendarFeedTitles) CalendarFeedICSInput {
	t.Helper()
	user := predictableFeedUser(t, "2026-03-02")
	user.CalendarFeedDetailedTitles = optIn
	return CalendarFeedICSInput{
		User:       user,
		Logs:       predictableFeedLogs(t),
		Now:        mustParseDashboardDay(t, "2026-03-20"),
		Location:   time.UTC,
		Disclaimer: "Predictions are estimates, not medical advice or a method of contraception.",
		Titles:     titles,
	}
}

func withoutSummaryLines(body string) string {
	kept := make([]string, 0, 64)
	for _, line := range strings.Split(body, "\r\n") {
		if !strings.HasPrefix(line, "SUMMARY:") {
			kept = append(kept, line)
		}
	}
	return strings.Join(kept, "\r\n")
}

func TestCalendarFeedNeutralDefaultIsByteIdenticalWithoutTheOptIn(t *testing.T) {
	baseline := string(buildTestFeedICS(detailedTitlesFeedInput(t, false, CalendarFeedTitles{})))
	withTitles := string(buildTestFeedICS(detailedTitlesFeedInput(t, false, testDetailedFeedTitles)))

	if withTitles != baseline {
		t.Fatalf("titles supplied without the opt-in changed the body:\n--- neutral\n%s\n--- got\n%s", baseline, withTitles)
	}
	assertNeutralSummaries(t, withTitles)
}

func TestCalendarFeedOptInWithAMissingTitleStaysNeutral(t *testing.T) {
	baseline := string(buildTestFeedICS(detailedTitlesFeedInput(t, false, CalendarFeedTitles{})))
	for name, titles := range map[string]CalendarFeedTitles{
		"no period title":    {Ovulation: "Ovulation (estimate)"},
		"no ovulation title": {Period: "Period (estimate)"},
		"blank titles":       {Period: " ", Ovulation: " "},
	} {
		t.Run(name, func(t *testing.T) {
			got := string(buildTestFeedICS(detailedTitlesFeedInput(t, true, titles)))
			if got != baseline {
				t.Fatalf("an incomplete title pair must fall back to the neutral body, got:\n%s", got)
			}
		})
	}
}

func TestCalendarFeedDetailedTitlesNameTheTypeOverTheSameEvents(t *testing.T) {
	neutral := string(buildTestFeedICS(detailedTitlesFeedInput(t, false, CalendarFeedTitles{})))
	detailed := string(buildTestFeedICS(detailedTitlesFeedInput(t, true, testDetailedFeedTitles)))

	if withoutSummaryLines(detailed) != withoutSummaryLines(neutral) {
		t.Fatalf("the opt-in changed something other than SUMMARY:\n--- neutral\n%s\n--- detailed\n%s", neutral, detailed)
	}
	seen := map[string]int{}
	for _, line := range strings.Split(detailed, "\r\n") {
		if strings.HasPrefix(line, "SUMMARY:") {
			seen[strings.TrimPrefix(line, "SUMMARY:")]++
		}
	}
	if seen["Period (estimate)"] == 0 || seen["Ovulation (estimate)"] == 0 || len(seen) != 2 {
		t.Fatalf("expected only the two estimate titles, both present, got %v", seen)
	}
	if !strings.Contains(detailed, "DESCRIPTION:Predictions are estimates") {
		t.Fatalf("the disclaimer left DESCRIPTION under the opt-in:\n%s", detailed)
	}
}

func TestCalendarFeedSummaryMapsEveryKindToItsType(t *testing.T) {
	for kind, want := range map[string]string{
		calendarFeedKindPeriod:          "Period (estimate)",
		calendarFeedKindPeriodWindow:    "Period (estimate)",
		calendarFeedKindOvulation:       "Ovulation (estimate)",
		calendarFeedKindOvulationWindow: "Ovulation (estimate)",
		"unknown-kind":                  calendarFeedNeutralSummary,
	} {
		titles := testDetailedFeedTitles
		if got := calendarFeedSummary(kind, &titles); got != want {
			t.Fatalf("kind %q: got %q, want %q", kind, got, want)
		}
		if got := calendarFeedSummary(kind, nil); got != calendarFeedNeutralSummary {
			t.Fatalf("kind %q without the opt-in: got %q", kind, got)
		}
	}
}

func TestCalendarFeedDetailedTitlesKeepEverySuppression(t *testing.T) {
	cases := map[string]func(input *CalendarFeedICSInput){
		"unpredictable cycle": func(input *CalendarFeedICSInput) { input.User.UnpredictableCycle = true },
		"pregnancy pause": func(input *CalendarFeedICSInput) {
			input.Logs = append(input.Logs, models.DailyLog{Date: input.Now.AddDate(0, 0, -10), PregnancyTest: models.PregnancyTestPositive})
		},
		"overdue cycle": func(input *CalendarFeedICSInput) { input.Now = input.Now.AddDate(0, 0, 36) },
	}
	for name, mutate := range cases {
		t.Run(name, func(t *testing.T) {
			input := detailedTitlesFeedInput(t, true, testDetailedFeedTitles)
			mutate(&input)
			if body := string(buildTestFeedICS(input)); strings.Contains(body, "BEGIN:VEVENT") {
				t.Fatalf("the opt-in reopened a suppressed feed:\n%s", body)
			}
		})
	}
}

func TestResolveFeedResolvesDetailedTitlesInTheOwnersLanguage(t *testing.T) {
	user, token := armedFeedUser(t, 42, "2026-03-02")
	user.CalendarFeedDetailedTitles = true
	user.InterfaceLanguage = "ru"
	copyProvider := &recordingFeedCopy{titles: map[string]string{
		calendarFeedPeriodTitleKey:    "Месячные (оценка)",
		calendarFeedOvulationTitleKey: "Овуляция (оценка)",
	}}
	svc := NewCalendarFeedService(
		&stubFeedUserStore{selector: user.CalendarFeedSelector, user: user},
		&stubFeedDayReader{logs: predictableFeedLogs(t)},
		copyProvider,
		[]byte(calendarFeedTestSecretKey),
	)

	body, ok, err := svc.ResolveFeed(context.Background(), token, mustParseDashboardDay(t, "2026-03-20"), time.UTC)
	if err != nil || !ok {
		t.Fatalf("ResolveFeed: ok=%v err=%v", ok, err)
	}
	if !strings.Contains(string(body), "SUMMARY:Месячные (оценка)") || !strings.Contains(string(body), "SUMMARY:Овуляция (оценка)") {
		t.Fatalf("expected the owner-language titles, got:\n%s", body)
	}
	for _, language := range copyProvider.languages {
		if language != "ru" {
			t.Fatalf("titles resolved at %q, not the owner's language", language)
		}
	}
}

func TestResolveFeedWithoutTheOptInNeverAsksForTitles(t *testing.T) {
	user, token := armedFeedUser(t, 42, "2026-03-02")
	copyProvider := &recordingFeedCopy{titles: map[string]string{
		calendarFeedPeriodTitleKey:    "Period (estimate)",
		calendarFeedOvulationTitleKey: "Ovulation (estimate)",
	}}
	svc := NewCalendarFeedService(
		&stubFeedUserStore{selector: user.CalendarFeedSelector, user: user},
		&stubFeedDayReader{logs: predictableFeedLogs(t)},
		copyProvider,
		[]byte(calendarFeedTestSecretKey),
	)

	body, ok, err := svc.ResolveFeed(context.Background(), token, mustParseDashboardDay(t, "2026-03-20"), time.UTC)
	if err != nil || !ok {
		t.Fatalf("ResolveFeed: ok=%v err=%v", ok, err)
	}
	if len(copyProvider.languages) != 0 {
		t.Fatalf("titles were looked up for an owner who did not opt in")
	}
	assertNeutralSummaries(t, string(body))
}

type recordingFeedCopy struct {
	titles    map[string]string
	languages []string
}

func (r *recordingFeedCopy) Disclaimer(string) string { return "d" }

func (r *recordingFeedCopy) Message(language string, key string) string {
	r.languages = append(r.languages, language)
	return r.titles[key]
}
