package services

import (
	"fmt"
	"regexp"
	"strings"
	"testing"
	"time"

	"github.com/ovumcy/ovumcy-web/internal/models"
)

// predictableFeedUser builds an owner with a stable, predictable cycle so the
// builder emits prediction events. now is chosen so the last period start is in
// the recent past and future cycles fall ahead.
func predictableFeedUser(t *testing.T, lastPeriodStart string) *models.User {
	t.Helper()
	start := mustParseDashboardDay(t, lastPeriodStart)
	return &models.User{
		ID:              7,
		CycleLength:     28,
		PeriodLength:    5,
		LutealPhase:     14,
		LastPeriodStart: &start,
	}
}

func predictableFeedLogs(t *testing.T) []models.DailyLog {
	t.Helper()
	// Three prior cycle starts ~28 days apart establish an observed, stable
	// cadence so predictions are enabled (not sparse/unpredictable).
	return []models.DailyLog{
		{Date: mustParseDashboardDay(t, "2026-01-05"), IsPeriod: true},
		{Date: mustParseDashboardDay(t, "2026-02-02"), IsPeriod: true},
		{Date: mustParseDashboardDay(t, "2026-03-02"), IsPeriod: true},
	}
}

// TestFoldICSLine pins RFC 5545 §3.1 line folding: a content line over 75
// octets is split with CRLF+space and no folded segment exceeds 75 octets, while
// a line at or under the limit is emitted verbatim. Kills the foldICSLine
// boundary/negation survivors (the fold on/off guard at len<=75 and the
// segment-length guard) — a broken fold yields .ics lines strict calendar
// parsers reject, or a spurious fold in a short line.
func TestFoldICSLine(t *testing.T) {
	t.Parallel()

	// At or under the limit: returned verbatim, never folded.
	for _, n := range []int{3, 75} {
		line := strings.Repeat("a", n)
		if got := foldICSLine(line); got != line {
			t.Fatalf("foldICSLine(%d-octet line) = %q, want it unchanged", n, got)
		}
	}

	// Over the limit: folded with CRLF+space, first segment exactly 75 octets,
	// and no segment over 75.
	folded := foldICSLine(strings.Repeat("a", 76))
	if !strings.Contains(folded, "\r\n ") {
		t.Fatalf("expected a 76-octet line to be folded, got %q", folded)
	}
	segments := strings.Split(folded, "\r\n ")
	if len(segments[0]) != 75 {
		t.Fatalf("first fold segment = %d octets, want 75", len(segments[0]))
	}
	for i, seg := range segments {
		if len(seg) > 75 {
			t.Fatalf("fold segment %d exceeds 75 octets (%d)", i, len(seg))
		}
	}
}

func TestBuildCalendarFeedICSEmitsNeutralEventsWithDisclaimer(t *testing.T) {
	user := predictableFeedUser(t, "2026-03-02")
	now := mustParseDashboardDay(t, "2026-03-20")

	body := string(BuildCalendarFeedICS(CalendarFeedICSInput{
		User:       user,
		Logs:       predictableFeedLogs(t),
		Now:        now,
		Location:   time.UTC,
		Disclaimer: "These are estimates, not medical advice or a method of contraception.",
	}))

	// Structural RFC 5545 markers (never localized copy).
	for _, marker := range []string{"BEGIN:VCALENDAR", "END:VCALENDAR", "BEGIN:VEVENT", "END:VEVENT", "SUMMARY:", "DESCRIPTION:", "DTSTART;VALUE=DATE:"} {
		if !strings.Contains(body, marker) {
			t.Fatalf("expected .ics to contain %q, got:\n%s", marker, body)
		}
	}

	if !strings.Contains(body, "\r\n") {
		t.Fatalf("expected CRLF line endings per RFC 5545")
	}

	// Neutral-title invariant: no phase word, no date digits, no symptom hint in
	// any SUMMARY line. The concrete date must live only in DTSTART/DTEND.
	assertNeutralSummaries(t, body)

	// Disclaimer must be present in a DESCRIPTION line (medical-safety).
	if !strings.Contains(body, "DESCRIPTION:These are estimates") {
		t.Fatalf("expected medical-safety disclaimer in DESCRIPTION, got:\n%s", body)
	}
}

// assertNeutralSummaries fails if any SUMMARY line leaks a cycle phase, a date,
// or a symptom token. It asserts the fixed neutral label and the absence of
// health specifics — the data-minimization invariant.
func assertNeutralSummaries(t *testing.T, body string) {
	t.Helper()
	leakyTokens := []string{
		"ovulation", "Ovulation", "fertile", "Fertile", "period", "Period",
		"menstru", "luteal", "follicular", "2026", "03-", "symptom",
	}
	sawSummary := false
	for _, line := range strings.Split(body, "\r\n") {
		if !strings.HasPrefix(line, "SUMMARY:") {
			continue
		}
		sawSummary = true
		if line != "SUMMARY:Ovumcy: reminder (estimate)" {
			t.Fatalf("SUMMARY is not the fixed neutral label: %q", line)
		}
		for _, token := range leakyTokens {
			if strings.Contains(line, token) {
				t.Fatalf("SUMMARY leaks health-specific token %q: %q", token, line)
			}
		}
	}
	if !sawSummary {
		t.Fatalf("expected at least one SUMMARY line")
	}
}

func TestBuildCalendarFeedICSSuppressesForPregnancyPause(t *testing.T) {
	user := predictableFeedUser(t, "2026-03-02")
	now := mustParseDashboardDay(t, "2026-03-20")

	logs := append(predictableFeedLogs(t),
		// A positive pregnancy test with no later cycle start pauses predictions.
		models.DailyLog{Date: mustParseDashboardDay(t, "2026-03-10"), PregnancyTest: models.PregnancyTestPositive},
	)

	body := string(BuildCalendarFeedICS(CalendarFeedICSInput{
		User:       user,
		Logs:       logs,
		Now:        now,
		Location:   time.UTC,
		Disclaimer: "disclaimer",
	}))

	if strings.Contains(body, "BEGIN:VEVENT") {
		t.Fatalf("pregnancy-pause must suppress ALL prediction events, got:\n%s", body)
	}
	// The calendar shell must still be well-formed (a valid, empty feed).
	if !strings.Contains(body, "BEGIN:VCALENDAR") || !strings.Contains(body, "END:VCALENDAR") {
		t.Fatalf("expected a well-formed empty VCALENDAR, got:\n%s", body)
	}
}

func TestBuildCalendarFeedICSSuppressesForUnpredictableCycle(t *testing.T) {
	user := predictableFeedUser(t, "2026-03-02")
	user.UnpredictableCycle = true
	now := mustParseDashboardDay(t, "2026-03-20")

	body := string(BuildCalendarFeedICS(CalendarFeedICSInput{
		User:       user,
		Logs:       predictableFeedLogs(t),
		Now:        now,
		Location:   time.UTC,
		Disclaimer: "disclaimer",
	}))

	if strings.Contains(body, "BEGIN:VEVENT") {
		t.Fatalf("unpredictable-cycle mode must suppress ALL prediction events, got:\n%s", body)
	}
}

func TestBuildCalendarFeedICSHandlesNoBaseline(t *testing.T) {
	// A user with no last period start and no logs yields a valid empty feed,
	// never a panic or a fabricated event.
	body := string(BuildCalendarFeedICS(CalendarFeedICSInput{
		User:       &models.User{ID: 1, CycleLength: 28},
		Logs:       nil,
		Now:        mustParseDashboardDay(t, "2026-03-20"),
		Location:   time.UTC,
		Disclaimer: "disclaimer",
	}))
	if strings.Contains(body, "BEGIN:VEVENT") {
		t.Fatalf("expected no events without a baseline, got:\n%s", body)
	}
	if !strings.Contains(body, "BEGIN:VCALENDAR") {
		t.Fatalf("expected a well-formed VCALENDAR, got:\n%s", body)
	}
}

func TestBuildCalendarFeedICSHandlesNilUser(t *testing.T) {
	// A nil user (defensive guard) must yield a well-formed, empty VCALENDAR —
	// never a panic and never a fabricated event.
	body := string(BuildCalendarFeedICS(CalendarFeedICSInput{
		User:       nil,
		Logs:       predictableFeedLogs(t),
		Now:        mustParseDashboardDay(t, "2026-03-20"),
		Location:   time.UTC,
		Disclaimer: "disclaimer",
	}))
	if strings.Contains(body, "BEGIN:VEVENT") {
		t.Fatalf("expected no events for a nil user, got:\n%s", body)
	}
	if !strings.Contains(body, "BEGIN:VCALENDAR") || !strings.Contains(body, "END:VCALENDAR") {
		t.Fatalf("expected a well-formed empty VCALENDAR, got:\n%s", body)
	}
}

// TestBuildCalendarFeedICSUIDsAreStableAcrossRenderTime pins the RFC 5545 UID
// stability invariant: two renders over the same logs at different `now`
// (different subscription polls) must mint the IDENTICAL set of VEVENT UIDs,
// so a calendar client recognizes the same logical events instead of
// recreating them (which would lose alarms/edits and fire spurious
// notifications). Only DTSTAMP may differ between the two renders.
func TestBuildCalendarFeedICSUIDsAreStableAcrossRenderTime(t *testing.T) {
	user := predictableFeedUser(t, "2026-03-02")
	logs := predictableFeedLogs(t)

	firstBody := string(BuildCalendarFeedICS(CalendarFeedICSInput{
		User:       user,
		Logs:       logs,
		Now:        mustParseDashboardDay(t, "2026-03-20"),
		Location:   time.UTC,
		Disclaimer: "disclaimer",
	}))
	secondBody := string(BuildCalendarFeedICS(CalendarFeedICSInput{
		User:       user,
		Logs:       logs,
		Now:        mustParseDashboardDay(t, "2026-03-21"),
		Location:   time.UTC,
		Disclaimer: "disclaimer",
	}))

	firstUIDs := extractICSUIDs(t, firstBody)
	secondUIDs := extractICSUIDs(t, secondBody)
	if len(firstUIDs) == 0 {
		t.Fatalf("expected at least one VEVENT, got:\n%s", firstBody)
	}
	if diff := cmpStringSets(firstUIDs, secondUIDs); diff != "" {
		t.Fatalf("UID set changed across renders at different `now`: %s", diff)
	}
}

// TestBuildCalendarFeedICSUIDsAreUniqueWithinARender pins uniqueness: no two
// VEVENTs in the same feed share a UID (a calendar client keys events by UID,
// so a collision would silently drop one event).
func TestBuildCalendarFeedICSUIDsAreUniqueWithinARender(t *testing.T) {
	user := predictableFeedUser(t, "2026-03-02")

	body := string(BuildCalendarFeedICS(CalendarFeedICSInput{
		User:       user,
		Logs:       predictableFeedLogs(t),
		Now:        mustParseDashboardDay(t, "2026-03-20"),
		Location:   time.UTC,
		Disclaimer: "disclaimer",
	}))

	uids := extractICSUIDs(t, body)
	if len(uids) == 0 {
		t.Fatalf("expected at least one VEVENT, got:\n%s", body)
	}
	seen := make(map[string]struct{}, len(uids))
	for _, uid := range uids {
		if _, dup := seen[uid]; dup {
			t.Fatalf("duplicate UID %q within one render:\n%s", uid, body)
		}
		seen[uid] = struct{}{}
	}
}

// TestBuildCalendarFeedICSUIDCarriesNoIdentifyingData pins data-minimization
// on the UID itself: it must be exactly "kind-YYYYMMDD@ovumcy" — no email, no
// user id, no capability token, no render-order index.
func TestBuildCalendarFeedICSUIDCarriesNoIdentifyingData(t *testing.T) {
	user := predictableFeedUser(t, "2026-03-02")
	user.Email = "owner@example.com"

	body := string(BuildCalendarFeedICS(CalendarFeedICSInput{
		User:       user,
		Logs:       predictableFeedLogs(t),
		Now:        mustParseDashboardDay(t, "2026-03-20"),
		Location:   time.UTC,
		Disclaimer: "disclaimer",
	}))

	uidPattern := regexp.MustCompile(`^(period|ovulation)-\d{8}@ovumcy$`)
	uids := extractICSUIDs(t, body)
	if len(uids) == 0 {
		t.Fatalf("expected at least one VEVENT, got:\n%s", body)
	}
	for _, uid := range uids {
		if !uidPattern.MatchString(uid) {
			t.Fatalf("UID %q does not match the pure kind-date form, got:\n%s", uid, body)
		}
		if strings.Contains(uid, "example.com") || strings.Contains(uid, "owner") {
			t.Fatalf("UID %q appears to leak owner-identifying data", uid)
		}
	}
}

// extractICSUIDs pulls every UID: content-line value out of a rendered .ics
// body, in order.
func extractICSUIDs(t *testing.T, body string) []string {
	t.Helper()
	var uids []string
	for _, line := range strings.Split(body, "\r\n") {
		if rest, ok := strings.CutPrefix(line, "UID:"); ok {
			uids = append(uids, rest)
		}
	}
	return uids
}

// cmpStringSets reports a human-readable difference between two string slices
// treated as sets, or "" if they contain the same elements.
func cmpStringSets(a, b []string) string {
	setA := make(map[string]int)
	for _, v := range a {
		setA[v]++
	}
	setB := make(map[string]int)
	for _, v := range b {
		setB[v]++
	}
	if len(setA) != len(setB) {
		return fmt.Sprintf("different sizes: %v vs %v", a, b)
	}
	for k, v := range setA {
		if setB[k] != v {
			return fmt.Sprintf("%v vs %v", a, b)
		}
	}
	return ""
}

func TestBuildCalendarFeedICSProjectsMultipleCyclesAndEscapesDescription(t *testing.T) {
	user := predictableFeedUser(t, "2026-03-02")
	now := mustParseDashboardDay(t, "2026-03-20")

	body := string(BuildCalendarFeedICS(CalendarFeedICSInput{
		User:     user,
		Logs:     predictableFeedLogs(t),
		Now:      now,
		Location: time.UTC,
		// Commas/semicolons/newlines must be escaped per RFC 5545 §3.3.11.
		Disclaimer: "estimate; not advice, really\nsecond line",
	}))

	// At least two upcoming cycles => multiple VEVENTs across the ~60-90d horizon.
	if got := strings.Count(body, "BEGIN:VEVENT"); got < 2 {
		t.Fatalf("expected multiple projected events, got %d:\n%s", got, body)
	}
	// The raw newline must not create a new content line; it is escaped to \n.
	if strings.Contains(body, "DESCRIPTION:estimate; not advice") {
		t.Fatalf("expected ';' and ',' escaped in DESCRIPTION, got:\n%s", body)
	}
	if !strings.Contains(body, `DESCRIPTION:estimate\; not advice\, really\nsecond line`) {
		t.Fatalf("expected escaped DESCRIPTION content, got:\n%s", body)
	}
}
