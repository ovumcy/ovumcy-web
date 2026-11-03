package services

import (
	"bytes"
	"context"
	"go/ast"
	"go/parser"
	"go/token"
	"log"
	"os"
	"path/filepath"
	"slices"
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

// suppressionReasonConstants lists every SuppressionReason constant declared in
// the predicate file, read from the source so a reason added later is a case
// owed here rather than a silent gap.
func suppressionReasonConstants(t *testing.T) map[SuppressionReason]string {
	t.Helper()
	root := predictionSuppressionRepoRoot(t)
	source, err := os.ReadFile(filepath.Join(root, filepath.FromSlash(predictionSuppressionPredicateFile)))
	if err != nil {
		t.Fatalf("read %s: %v", predictionSuppressionPredicateFile, err)
	}
	file, err := parser.ParseFile(token.NewFileSet(), predictionSuppressionPredicateFile, source, 0)
	if err != nil {
		t.Fatalf("parse %s: %v", predictionSuppressionPredicateFile, err)
	}
	found := map[SuppressionReason]string{}
	for _, decl := range file.Decls {
		gen, ok := decl.(*ast.GenDecl)
		if !ok || gen.Tok != token.CONST {
			continue
		}
		for _, spec := range gen.Specs {
			value, ok := spec.(*ast.ValueSpec)
			if !ok || value.Type == nil || len(value.Names) != 1 || len(value.Values) != 1 {
				continue
			}
			if ident, ok := value.Type.(*ast.Ident); !ok || ident.Name != "SuppressionReason" {
				continue
			}
			if literal, ok := value.Values[0].(*ast.BasicLit); ok {
				found[SuppressionReason(strings.Trim(literal.Value, `"`))] = value.Names[0].Name
			}
		}
	}
	if len(found) == 0 {
		t.Fatal("no SuppressionReason constant found — this check read a tree nobody wrote")
	}
	return found
}

// TestCalendarFeedDetailedTitlesKeepEverySuppression runs every SuppressionReason
// through the feed with the titles opted in. A whole-projection reason leaves no
// event; a fertility-only reason leaves no ovulation event. Each case carries a
// control one step before the signal, which must name the ovulation title, so
// the absence is the signal's doing and not an empty fixture's.
func TestCalendarFeedDetailedTitlesKeepEverySuppression(t *testing.T) {
	baseInput := func(t *testing.T) CalendarFeedICSInput {
		return detailedTitlesFeedInput(t, true, testDetailedFeedTitles)
	}
	lastStarts := func(t *testing.T, count int) []models.DailyLog {
		logs := predictableFeedLogs(t)
		return logs[len(logs)-count:]
	}
	parityInput := func(t *testing.T, today string) CalendarFeedICSInput {
		user := dayFeedbackParityUser(378)
		user.CalendarFeedDetailedTitles = true
		return CalendarFeedICSInput{
			User:       user,
			Logs:       cycleStartLogs(t, "2026-06-03", "2026-06-30", "2026-07-28", "2026-08-25", "2026-09-30"),
			Now:        mustParseDashboardDay(t, today),
			Location:   time.UTC,
			Disclaimer: "Predictions are estimates, not medical advice or a method of contraception.",
			Titles:     testDetailedFeedTitles,
		}
	}
	bleedingInput := func(t *testing.T, today string) CalendarFeedICSInput {
		user := dayFeedbackParityUser(434)
		user.CalendarFeedDetailedTitles = true
		logs := web434History(t, models.DailyLog{Date: mustParseDay(t, "2026-11-02"), IsPeriod: true, CycleStart: true, IsUncertain: true, Flow: models.FlowSpotting})
		return CalendarFeedICSInput{
			User:       user,
			Logs:       logs,
			Now:        mustParseDashboardDay(t, today),
			Location:   time.UTC,
			Disclaimer: "Predictions are estimates, not medical advice or a method of contraception.",
			Titles:     testDetailedFeedTitles,
		}
	}
	type suppressionCase struct {
		reason   SuppressionReason
		whole    bool
		suppress func(t *testing.T) CalendarFeedICSInput
		control  func(t *testing.T) CalendarFeedICSInput
		// feedBlind, when set, says why the feed's own reaction to this reason is
		// not observable on a regular account: the absence assertion is skipped
		// for it (it would hold with the signal removed), so the row guards only
		// the verdict fixture and the control, not the feed.
		feedBlind string
	}
	// whole marks a reason that withholds every event, not only the fertility half.
	cases := []suppressionCase{
		{reason: SuppressionReasonUnpredictableCycle, whole: true, suppress: func(t *testing.T) CalendarFeedICSInput {
			input := baseInput(t)
			input.User.UnpredictableCycle = true
			return input
		}},
		{reason: SuppressionReasonPregnancyPause, whole: true, suppress: func(t *testing.T) CalendarFeedICSInput {
			input := baseInput(t)
			input.Logs = append(input.Logs, models.DailyLog{Date: input.Now.AddDate(0, 0, -10), PregnancyTest: models.PregnancyTestPositive})
			return input
		}},
		{reason: SuppressionReasonCycleOverdue, whole: true, suppress: func(t *testing.T) CalendarFeedICSInput {
			input := baseInput(t)
			input.Now = input.Now.AddDate(0, 0, 36)
			return input
		}},
		{reason: SuppressionReasonIrregularNeedsData, whole: true, suppress: func(t *testing.T) CalendarFeedICSInput {
			input := baseInput(t)
			input.User.IrregularCycle = true
			input.Logs = lastStarts(t, 3)
			return input
		}},
		{reason: SuppressionReasonAwaitingFirstCycle, suppress: func(t *testing.T) CalendarFeedICSInput {
			input := baseInput(t)
			input.Logs = lastStarts(t, 1)
			return input
		}},
		{reason: SuppressionReasonAwaitingMoreCycles, suppress: func(t *testing.T) CalendarFeedICSInput {
			input := baseInput(t)
			input.Logs = lastStarts(t, 3)
			return input
		}},
		{
			reason: SuppressionReasonCycleDataStale,
			feedBlind: "the band starts at the reference length, after the projected ovulation " +
				"of the running cycle is already past, and the feed never projects fertility into later cycles for a regular account",
			suppress: func(t *testing.T) CalendarFeedICSInput {
				input := baseInput(t)
				input.Now = mustParseDashboardDay(t, "2026-03-31")
				return input
			},
		},
		{
			// Spotting logged on 2026-11-02 opens no cycle: on that day the running
			// cycle's ovulation is withheld; two days later the bleed is history.
			reason:   SuppressionReasonBleedingOutsideCycleStart,
			suppress: func(t *testing.T) CalendarFeedICSInput { return bleedingInput(t, "2026-11-02") },
			control:  func(t *testing.T) CalendarFeedICSInput { return bleedingInput(t, "2026-11-04") },
		},
		{
			reason: SuppressionReasonCyclePastProjectionLength,
			feedBlind: "the days between the median and the average projection length fall after the projected " +
				"ovulation, which the feed already drops as past, so no future ovulation is left to withhold",
			suppress: func(t *testing.T) CalendarFeedICSInput { return parityInput(t, "2026-10-28") },
			control:  func(t *testing.T) CalendarFeedICSInput { return parityInput(t, "2026-10-27") },
		},
	}

	reasonsOf := func(input CalendarFeedICSInput) []SuppressionReason {
		today := DateAtLocation(input.Now, input.Location)
		_, verdict := PublishedStats(input.User, BuildCycleStatsFromLogs(input.User, input.Logs, input.Now, input.Location), input.Logs, today, input.Location)
		return verdict.Reasons
	}
	tabled := map[SuppressionReason]bool{}
	for _, testCase := range cases {
		tabled[testCase.reason] = true
		t.Run(string(testCase.reason), func(t *testing.T) {
			input := testCase.suppress(t)
			if !slices.Contains(reasonsOf(input), testCase.reason) {
				t.Fatalf("fixture: reasons %v do not name %s", reasonsOf(input), testCase.reason)
			}
			body := string(buildTestFeedICS(input))
			if testCase.feedBlind != "" {
				t.Logf("feed reaction not observable for %s: %s", testCase.reason, testCase.feedBlind)
			} else {
				if testCase.whole && strings.Contains(body, "BEGIN:VEVENT") {
					t.Fatalf("the opt-in reopened a suppressed feed:\n%s", body)
				}
				if strings.Contains(body, "SUMMARY:Ovulation (estimate)") {
					t.Fatalf("the opt-in named an ovulation under %s:\n%s", testCase.reason, body)
				}
				// A fertility-only reason keeps the projected period: the feed must not
				// whole-suppress on it.
				if !testCase.whole && !strings.Contains(body, "SUMMARY:Period (estimate)") {
					t.Fatalf("a fertility-only reason %s withheld the period event too:\n%s", testCase.reason, body)
				}
			}

			control := baseInput(t)
			if testCase.control != nil {
				control = testCase.control(t)
			}
			if slices.Contains(reasonsOf(control), testCase.reason) {
				t.Fatalf("control: reasons %v still name %s", reasonsOf(control), testCase.reason)
			}
			if controlBody := string(buildTestFeedICS(control)); !strings.Contains(controlBody, "SUMMARY:Ovulation (estimate)") {
				t.Fatalf("control: the feed without %s names no ovulation, so the absence above proves nothing:\n%s", testCase.reason, controlBody)
			}
		})
	}
	for reason, name := range suppressionReasonConstants(t) {
		if !tabled[reason] {
			t.Errorf("%s (%q) is a SuppressionReason with no feed case here: add its fixture", name, reason)
		}
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

// TestResolveFeedLogsOnceWhenTheDisclaimerResolvesBlank: a blank disclaimer
// empties the feed, so the service says so — the reason and the locale, nothing
// that names an owner or the token — and a resolved disclaimer stays silent.
func TestResolveFeedLogsOnceWhenTheDisclaimerResolvesBlank(t *testing.T) {
	var captured bytes.Buffer
	previous := log.Writer()
	log.SetOutput(&captured)
	t.Cleanup(func() { log.SetOutput(previous) })

	resolve := func(disclaimer string) (string, string) {
		captured.Reset()
		user, token := armedFeedUser(t, 4242, "2026-03-02")
		user.InterfaceLanguage = "ru"
		svc := NewCalendarFeedService(
			&stubFeedUserStore{selector: user.CalendarFeedSelector, user: user},
			&stubFeedDayReader{logs: predictableFeedLogs(t)},
			stubFeedDisclaimer{text: disclaimer},
			[]byte(calendarFeedTestSecretKey),
		)
		body, ok, err := svc.ResolveFeed(context.Background(), token, mustParseDashboardDay(t, "2026-03-20"), time.UTC)
		if err != nil || !ok {
			t.Fatalf("ResolveFeed: ok=%v err=%v", ok, err)
		}
		return string(body), token
	}

	body, token := resolve("  ")
	if strings.Contains(body, "BEGIN:VEVENT") {
		t.Fatalf("fixture: a blank disclaimer must withhold every event:\n%s", body)
	}
	logged := captured.String()
	if strings.Count(logged, "reason=disclaimer_blank") != 1 || !strings.Contains(logged, `locale="ru"`) {
		t.Fatalf("want one disclaimer_blank warning carrying the locale, got %q", logged)
	}
	if strings.Contains(logged, token) || strings.Contains(logged, "4242") {
		t.Fatalf("the warning carries user data or the token: %q", logged)
	}

	resolve("estimates disclaimer")
	if strings.Contains(captured.String(), "disclaimer_blank") {
		t.Fatalf("a resolved disclaimer must not warn, got %q", captured.String())
	}
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
