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

// dayBoundaryFeedLogs is predictableFeedLogs shifted so the projected ovulation
// of the current cycle lands exactly on 2026-03-10: three cycle starts 28 days
// apart ending 2026-02-25, and ovulation = cycle start + (28 - 14) - 1. A feed
// rendered with "today" = 2026-03-10 keeps that event; one rendered with "today"
// = 2026-03-11 drops it as past. That single-day difference is what makes the
// owner-vs-request timezone choice observable in the .ics body itself.
func dayBoundaryFeedLogs(t *testing.T) []models.DailyLog {
	t.Helper()
	return []models.DailyLog{
		{Date: mustParseDashboardDay(t, "2025-12-31"), IsPeriod: true},
		{Date: mustParseDashboardDay(t, "2026-01-28"), IsPeriod: true},
		{Date: mustParseDashboardDay(t, "2026-02-25"), IsPeriod: true},
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
		Disclaimer: "Predictions are estimates, not medical advice or a method of contraception.",
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
	if !strings.Contains(body, "DESCRIPTION:Predictions are estimates") {
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

// TestBuildCalendarFeedICSSuppressesForOverdueCycle covers the third
// medical-safety gate on the feed: an account whose cycle has run past its own
// reference length by more than a week emits no prediction events.
//
// This is the surface where the phantom travelled furthest — the builder projects
// three cycles ahead, so an overdue account pushed up to six invented all-day
// events into whatever third-party calendar client holds the subscription, where
// they outlive any in-app correction. The 2026-03-02 anchor with a 28-day cadence
// puts "today" = 2026-04-25 on cycle day 55 against a 28-day reference.
//
// The first subtest is the positive anchor: the same account, same logs, a date
// inside the reference length still gets its events, so the suppressed case
// cannot pass by emitting nothing for an unrelated reason.
func TestBuildCalendarFeedICSSuppressesForOverdueCycle(t *testing.T) {
	user := predictableFeedUser(t, "2026-03-02")
	logs := predictableFeedLogs(t)

	renderFeed := func(now time.Time) string {
		return string(BuildCalendarFeedICS(CalendarFeedICSInput{
			User:       user,
			Logs:       logs,
			Now:        now,
			Location:   time.UTC,
			Disclaimer: "disclaimer",
		}))
	}

	t.Run("a cycle inside its reference length still emits events", func(t *testing.T) {
		body := renderFeed(mustParseDashboardDay(t, "2026-03-20"))
		if !strings.Contains(body, "BEGIN:VEVENT") {
			t.Fatalf("expected prediction events inside the reference length, got:\n%s", body)
		}
	})

	t.Run("an overdue cycle without a thermal shift emits no event", func(t *testing.T) {
		now := mustParseDashboardDay(t, "2026-04-25")

		stats := NewStatsService(nil, nil).BuildCycleStatsFromLogs(user, logs, now, time.UTC)
		if !DashboardCycleOverdue(user, stats) {
			t.Fatalf("test setup expects an overdue cycle: cycle day %d against reference %d",
				stats.CurrentCycleDay, DashboardCycleReferenceLength(user, stats))
		}

		body := renderFeed(now)
		if strings.Contains(body, "BEGIN:VEVENT") {
			t.Fatalf("an overdue cycle must suppress ALL prediction events, got:\n%s", body)
		}
		// The subscription must not break: with no prediction and no confirmed
		// day to publish, suppression is the well-formed empty VCALENDAR a client
		// keeps polling.
		for _, marker := range []string{"BEGIN:VCALENDAR", "VERSION:2.0", "PRODID:" + calendarFeedProductID, "END:VCALENDAR"} {
			if !strings.Contains(body, marker) {
				t.Fatalf("expected a well-formed empty VCALENDAR carrying %q, got:\n%s", marker, body)
			}
		}
	})

	// The overdue gate withholds projections, not the day the owner's own
	// temperatures confirmed: that one event survives it, and nothing else does.
	t.Run("an overdue cycle with a thermal shift emits only the confirmed day", func(t *testing.T) {
		shiftUser, shiftLogs, _ := outboundConfirmedFixture(t, true)
		now := mustParseDashboardDay(t, "2026-04-10")
		if stats := BuildCycleStatsFromLogs(shiftUser, shiftLogs, now, time.UTC); !DashboardCycleOverdue(shiftUser, stats) {
			t.Fatalf("test setup expects an overdue cycle: cycle day %d", stats.CurrentCycleDay)
		}

		events := calendarFeedEvents(CalendarFeedICSInput{User: shiftUser, Logs: shiftLogs, Now: now, Location: time.UTC})
		if len(events) != 1 || events[0].kind != calendarFeedKindOvulation || CalendarDayKey(events[0].date) != "2026-03-11" {
			t.Fatalf("overdue feed events = %#v, want exactly the confirmed ovulation on 2026-03-11", events)
		}
	})
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

// TestBuildCalendarFeedICSWithAnEmptyDisclaimerStillEmitsEveryEvent records the
// current behaviour when the localized disclaimer resolves to "": the builder has
// no runtime guard, so every VEVENT is still written, each with an empty
// DESCRIPTION line. This CONTRADICTS the documented invariant that every event
// carries the medical-safety disclaimer: the invariant currently rests on the
// locale catalogue never resolving to "" (guarded by a catalogue sweep), not on
// this builder. The test pins the gap so closing it (suppress the feed, or refuse
// the empty text) is a deliberate change that updates it.
func TestBuildCalendarFeedICSWithAnEmptyDisclaimerStillEmitsEveryEvent(t *testing.T) {
	input := CalendarFeedICSInput{
		User:       predictableFeedUser(t, "2026-03-02"),
		Logs:       predictableFeedLogs(t),
		Now:        mustParseDashboardDay(t, "2026-03-20"),
		Location:   time.UTC,
		Disclaimer: "",
	}

	body := string(BuildCalendarFeedICS(input))
	events := strings.Count(body, "BEGIN:VEVENT")
	if events == 0 {
		t.Fatalf("fixture: the feed must project events:\n%s", body)
	}
	if got := strings.Count(body, "\r\nDESCRIPTION:\r\n"); got != events {
		t.Fatalf("pinned: an empty disclaimer is currently emitted as an empty DESCRIPTION on every event (%d of %d); closing the gap should update this test\n%s", got, events, body)
	}

	input.Disclaimer = "estimate only"
	control := string(BuildCalendarFeedICS(input))
	if got := strings.Count(control, "\r\nDESCRIPTION:estimate only\r\n"); got != events {
		t.Fatalf("control: a non-empty disclaimer reaches every event: %d of %d", got, events)
	}
}

// TestBuildCalendarFeedICSProjectsExactlyThreeCyclesAheadAsSingleDays pins the
// CURRENT horizon on purpose, as a decision to revisit rather than a statement of
// approval. The feed chains next-period events two and three cycles beyond the
// first (and an ovulation event per cycle still ahead), each a single all-day date
// with no widened range. The calendar grid's rule against widening chained cycles
// concerns the START RANGE only (a spread drawn around a projection of a
// projection); the grid itself also chains single projected days across the visible
// month, so the feed's far dates are the same kind of claim, not a wider one. What
// stays unbounded by data confidence is the count: three cycles, whatever the
// number of completed cycles behind them.
func TestBuildCalendarFeedICSProjectsExactlyThreeCyclesAheadAsSingleDays(t *testing.T) {
	body := string(BuildCalendarFeedICS(CalendarFeedICSInput{
		User:       predictableFeedUser(t, "2026-03-02"),
		Logs:       predictableFeedLogs(t),
		Now:        mustParseDashboardDay(t, "2026-03-20"),
		Location:   time.UTC,
		Disclaimer: "estimate only",
	}))

	var periods []time.Time
	for _, uid := range extractICSUIDs(t, body) {
		if rest, ok := strings.CutPrefix(uid, "period-"); ok {
			periods = append(periods, mustParseDashboardDay(t, rest[:4]+"-"+rest[4:6]+"-"+rest[6:8]))
		}
	}
	if len(periods) != 3 {
		t.Fatalf("expected exactly three projected period events (this cycle's end and the two after it), got %d:\n%s", len(periods), body)
	}
	for i := 1; i < len(periods); i++ {
		if gap := CalendarDaysBetween(periods[i-1], periods[i]); gap != 28 {
			t.Fatalf("chained period events are one cycle apart, got %d days between #%d and #%d", gap, i, i+1)
		}
	}
	// 2026-03-02 + 28 days is the first projected start; the third is 56 days later.
	if want := mustParseDashboardDay(t, "2026-03-30"); !periods[0].Equal(want) {
		t.Fatalf("first projected period = %s, want %s", periods[0].Format("2006-01-02"), want.Format("2006-01-02"))
	}

	// Ovulation: cycle 0's day (2026-03-16) is already behind now, so the two
	// chained cycles carry one each.
	ovulations := 0
	for _, uid := range extractICSUIDs(t, body) {
		if strings.HasPrefix(uid, "ovulation-") {
			ovulations++
		}
	}
	if ovulations != 2 {
		t.Fatalf("expected exactly two projected ovulation events (cycles 1 and 2), got %d:\n%s", ovulations, body)
	}

	// Every event is a single all-day date: DTEND is the day after DTSTART.
	pairs := regexp.MustCompile(`DTSTART;VALUE=DATE:(\d{8})\r\nDTEND;VALUE=DATE:(\d{8})`).FindAllStringSubmatch(body, -1)
	if len(pairs) != strings.Count(body, "BEGIN:VEVENT") {
		t.Fatalf("every event has a DTSTART/DTEND pair: %d of %d", len(pairs), strings.Count(body, "BEGIN:VEVENT"))
	}
	for _, pair := range pairs {
		start, _ := time.Parse("20060102", pair[1])
		end, _ := time.Parse("20060102", pair[2])
		if end.Sub(start) != 24*time.Hour {
			t.Fatalf("event %s..%s is not a single all-day date", pair[1], pair[2])
		}
	}
}

// The test below pins CURRENT behaviour — a decision to revisit, not approval:
// the feed's horizon does not shrink with the data behind it.
func TestBuildCalendarFeedICSOfAnAccountWithOneCompletedCycleStillProjectsThreeCycles(t *testing.T) {
	logs := []models.DailyLog{
		{Date: mustParseDashboardDay(t, "2026-02-02"), IsPeriod: true},
		{Date: mustParseDashboardDay(t, "2026-03-02"), IsPeriod: true},
	}
	now := mustParseDashboardDay(t, "2026-03-20")
	user := predictableFeedUser(t, "2026-03-02")

	if got := BuildCycleStatsFromLogs(user, logs, now, time.UTC).CompletedCycleCount; got != 1 {
		t.Fatalf("fixture: the account must hold exactly one completed cycle, got %d", got)
	}

	body := string(BuildCalendarFeedICS(CalendarFeedICSInput{User: user, Logs: logs, Now: now, Location: time.UTC, Disclaimer: "estimate only"}))
	periods, ovulations := 0, 0
	for _, uid := range extractICSUIDs(t, body) {
		if strings.HasPrefix(uid, "period-") {
			periods++
		}
		if strings.HasPrefix(uid, "ovulation-") {
			ovulations++
		}
	}
	if periods != 3 {
		t.Fatalf("one completed cycle projects three period events, got %d:\n%s", periods, body)
	}
	if ovulations < 1 {
		t.Fatalf("one completed cycle already clears the first-cycle floor, so ovulation events are projected too, got %d:\n%s", ovulations, body)
	}
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
