package api

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/ovumcy/ovumcy-web/internal/models"
	staticassets "github.com/ovumcy/ovumcy-web/web"
)

// The Insights cycle stack draws every row against one axis, and that axis is
// capped (statsCycleRibbonMaxAxisDays, 60) so the DOM stays bounded. A cycle
// longer than the cap therefore has its band stop at the cap rather than at its
// own end: two such rows are drawn the same width whatever their lengths.
//
// The service reports that — StatsCycleRibbon.AxisTruncated for the stack,
// StatsCycleRibbonRow.Truncated for the row — and its own tests cover both
// flags. What no service test can see is whether the render path carries them:
// a flag computed correctly and then never read leaves every service test green
// while the reader sees an unannotated cut. These tests seed real period starts,
// GET /stats, and assert the presence and the absence of the two data hooks.
//
// Both directions are asserted, and both anchor on the stack's own hook first:
// a positive-only check passes just as well against a template that prints the
// notice unconditionally, and a negative-only check passes when the stack does
// not render at all.
//
// The assertions name the hooks, never the sentence: a copy edit to
// stats.cycle_stack_truncated must not redden a structural test.
func TestStatsCycleStackMarksACycleLongerThanTheAxis(t *testing.T) {
	body := statsBodyForCyclePattern(t, "stats-cycle-stack-truncated@example.com", 70, 3)

	if !strings.Contains(body, "data-stats-cycle-stack") {
		t.Fatal("expected the cycle stack to render for three completed cycles")
	}
	if !strings.Contains(body, `data-truncated="true"`) {
		t.Fatal("expected a band cut off at the axis to be marked on its row")
	}
	if !strings.Contains(body, "data-cycle-stack-truncated-notice") {
		t.Fatal("expected the stack to disclose that a cycle outran the axis")
	}
}

func TestStatsCycleStackCarriesNoTruncationMarksWhenEveryCycleFits(t *testing.T) {
	body := statsBodyForCyclePattern(t, "stats-cycle-stack-within-axis@example.com", 30, 3)

	if !strings.Contains(body, "data-stats-cycle-stack") {
		t.Fatal("expected the cycle stack to render for three completed cycles")
	}
	if strings.Contains(body, `data-truncated="true"`) {
		t.Fatal("no band is cut off when every cycle fits the axis")
	}
	if strings.Contains(body, "data-cycle-stack-truncated-notice") {
		t.Fatal("the truncation notice must not render when every cycle fits the axis")
	}
}

// A stack where SOME rows outran the axis and some did not is the only shape
// that separates the per-row flag from the stack-level one.
//
// statsBodyForCyclePattern seeds one uniform gap, so every case it can build
// has either every row cut or none — and in both of those, the markup a correct
// template produces is byte-identical to what a template reading
// `$.CycleRibbon.AxisTruncated` inside the range would produce. Reading the
// stack's verdict onto every row is a real mistake to make, and a suite that
// cannot see it is agreeing with itself.
func TestStatsCycleStackMarksOnlyTheRowsThatOutranTheAxis(t *testing.T) {
	// 70, 30, 30: the axis caps at 60, so exactly the first row is cut.
	body := statsBodyForCycleGaps(t, "stats-cycle-stack-mixed@example.com", []int{70, 30, 30})

	if !strings.Contains(body, "data-stats-cycle-stack") {
		t.Fatal("expected the cycle stack to render for three completed cycles")
	}
	if marks := strings.Count(body, `data-truncated="true"`); marks != 1 {
		t.Fatalf("expected exactly one band to be marked as cut, got %d; the mark reports the ROW's own length against the axis, not whether the stack has any cut row in it", marks)
	}
	if !strings.Contains(body, "data-cycle-stack-truncated-notice") {
		t.Fatal("expected the stack to disclose that a cycle outran the axis, even though only one row did")
	}
}

// statsBodyForCycleGaps seeds one period start per gap, so the caller can build
// a stack of cycles that are NOT all the same length, and renders /stats.
//
// gaps are read oldest-first: {70, 30, 30} is a 70-day cycle followed by two
// 30-day ones. Kept separate from statsBodyForCyclePattern rather than folded
// into it: that helper's single gapDays is what makes its own callers readable,
// and this one exists precisely because a uniform stack cannot express the case
// under test.
func statsBodyForCycleGaps(t *testing.T, email string, gaps []int) string {
	t.Helper()

	app, database := newOnboardingTestApp(t)
	user := createOnboardingTestUser(t, database, email, "StrongPass1", true)
	authCookie := loginAndExtractAuthCookie(t, app, user.Email, "StrongPass1")

	now := time.Now().UTC()
	today := time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, time.UTC)

	// n gaps are n completed cycles, which need n+1 period starts. Walk back
	// from the most recent start so each gap is the distance to the one before.
	starts := make([]time.Time, len(gaps)+1)
	starts[len(gaps)] = today.AddDate(0, 0, -8)
	for i := len(gaps) - 1; i >= 0; i-- {
		starts[i] = starts[i+1].AddDate(0, 0, -gaps[i])
	}

	logs := make([]models.DailyLog, 0, len(starts))
	for _, start := range starts {
		logs = append(logs, models.DailyLog{UserID: user.ID, Date: start, IsPeriod: true})
	}
	if err := database.Create(&logs).Error; err != nil {
		t.Fatalf("seed period logs: %v", err)
	}
	if err := database.Model(&models.User{}).Where("id = ?", user.ID).Updates(map[string]any{
		"last_period_start": starts[len(starts)-1],
	}).Error; err != nil {
		t.Fatalf("set last period start: %v", err)
	}

	request := httptest.NewRequest(http.MethodGet, "/stats", nil)
	request.Header.Set("Accept-Language", "en")
	request.Header.Set("Cookie", authCookie)
	response := mustAppResponse(t, app, request)
	assertStatusCode(t, response, http.StatusOK)
	return mustReadBodyString(t, response.Body)
}

// The template attribute is only half the disclosure: what turns
// data-truncated into something a reader can see is the stylesheet rule that
// draws the cut edge, and nothing pinned that rule.
//
// CI's stale-bundle guard does not reach it. That step (ci.yml, "Committed
// bundles must match a fresh build") runs `npm run build` and then
// `git diff --exit-code -- web/static/`, so it catches a bundle that DIFFERS
// from its source — a source edit committed without a rebuild. Delete the rule
// from input.css and rebuild and the two agree perfectly: the guard is green,
// every Go test is green, the linter is green, and the mark is gone, so two
// capped rows are once more the same width with nothing saying so.
// That blind spot is the removal direction; this is the dependency direction,
// where markup carries a hook whose only consumer is a component rule.
//
// That is exactly the failure this change exists to close, one layer down: #581
// computed Truncated and AxisTruncated and shipped them with no consumer, and
// nothing went red. The stylesheet is the consumer now, so it gets a barrier.
//
// It reads the EMBEDDED asset rather than the file on disk, because embed.FS is
// what the binary serves: a bundle correct in the working tree and missing from
// the embed would be a green suite and an unmarked band.
func TestTruncatedBandMarkReachesTheServedStylesheet(t *testing.T) {
	t.Parallel()

	stylesheet, err := staticassets.Files.ReadFile("static/css/tailwind.css")
	if err != nil {
		t.Fatalf("read the embedded stylesheet: %v", err)
	}
	rules := string(stylesheet)

	// Selector first: the rule has to be scoped to the band's own class, or it
	// would paint any element in the app that happens to carry the attribute.
	const selector = ".stats-cycle-stack-band[data-truncated=true]"
	if !strings.Contains(rules, selector) {
		t.Fatalf("the served stylesheet carries no %s rule; the row mark renders in the HTML and draws nothing, so a cut band is indistinguishable from a complete one", selector)
	}

	// The band must establish the containing block the mark is positioned
	// against. Without it the absolutely positioned edge resolves against
	// whatever ancestor happens to be positioned and lands elsewhere entirely.
	bandRule, found := cutCSSRule(rules, ".stats-cycle-stack-band{")
	if !found {
		t.Fatal("the served stylesheet carries no .stats-cycle-stack-band rule at all")
	}
	if !strings.Contains(bandRule, "position:relative") {
		t.Fatalf("the band rule does not establish a containing block (position:relative), so the cut mark is positioned against an ancestor instead of the band: %s", bandRule)
	}
}

// cutCSSRule returns the declaration block that opens at the first occurrence
// of open. Enough for the two assertions above; it is not a CSS parser and does
// not pretend to be one.
func cutCSSRule(rules string, open string) (string, bool) {
	at := strings.Index(rules, open)
	if at < 0 {
		return "", false
	}
	rest := rules[at+len(open):]
	end := strings.Index(rest, "}")
	if end < 0 {
		return "", false
	}
	return rest[:end], true
}
