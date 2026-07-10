package services

import (
	"fmt"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/ovumcy/ovumcy-web/internal/models"
)

// Calendar (.ics) feed builder (issue #126-replacement / .ics, slice 3). This
// file is the PURE, transport-free RFC 5545 builder: given an owner, their day
// logs, an injected now/location, and the localized medical-safety disclaimer,
// it renders a read-only text/calendar body of the owner's upcoming cycle
// events. It performs NO transport, NO auth, and NO persistence — the api layer
// owns token resolution, headers, and rate-limiting.
//
// Medical-safety invariant (same as the webhook reminder decision): it reuses
// the EXACT prediction path the dashboard uses (BuildCycleStatsFromLogs →
// DashboardUpcomingPredictions, gated by DashboardPredictionDisabled /
// stats.PregnancyPaused). It NEVER fabricates a date the app itself refuses to
// show — when in-app predictions are suppressed (unpredictable cycle, or a
// pregnancy pause), it emits ZERO prediction events, so a "fertile window" is
// never pushed into a pregnant owner's calendar.
//
// Data-minimization invariant: every VEVENT SUMMARY is the SAME neutral,
// contentless title (calendarFeedNeutralSummary) — no cycle phase, no date, no
// symptom appears in the SUMMARY, so a lock-screen preview or a shared calendar
// reveals nothing about the owner's health. The concrete date lives only in the
// DTSTART/DTEND all-day fields a calendar client needs to place the event; the
// disclaimer lives in the DESCRIPTION. (A detailed-event opt-in is a later
// slice; the default here is always neutral.)

const (
	// calendarFeedNeutralSummary is the single, contentless SUMMARY used for
	// every event. It carries the estimate qualifier (medical-safety) but no
	// health specifics (data minimization). It is intentionally NOT localized:
	// keeping it a fixed ASCII string means the neutral-title guarantee does not
	// depend on any locale file, and a shared-calendar viewer sees the same
	// contentless label regardless of the owner's language.
	calendarFeedNeutralSummary = "Ovumcy: reminder (estimate)"

	// calendarFeedProductID is the PRODID identifying this generator, per RFC
	// 5545 §3.7.3. It is a stable, non-secret product identifier.
	calendarFeedProductID = "-//Ovumcy//Calendar Feed//EN"

	// calendarFeedProjectionCycles bounds how many upcoming cycles the feed
	// projects. Three cycles at a typical ~28-30 day length spans ~60-90 days —
	// enough lead time for a calendar subscription without emitting far-future
	// events whose estimate error grows with distance.
	calendarFeedProjectionCycles = 3

	// calendarFeedDateLayout is the RFC 5545 DATE value form (VALUE=DATE) for
	// all-day events: YYYYMMDD with no time component.
	calendarFeedDateLayout = "20060102"

	// calendarFeedTimestampLayout is the RFC 5545 UTC DATE-TIME form used for
	// DTSTAMP: YYYYMMDDTHHMMSSZ.
	calendarFeedTimestampLayout = "20060102T150405Z"
)

// CalendarFeedICSInput is the transport-free input to BuildCalendarFeedICS. The
// user carries the cycle settings + baseline; logs are the already-fetched day
// history the prediction path consumes; now/location are injected (never
// time.Now()); disclaimer is the localized medical-safety string the caller
// resolved via the shared DisclaimerProvider seam (the same one the webhook
// notify pass uses), placed in each event's DESCRIPTION.
type CalendarFeedICSInput struct {
	User       *models.User
	Logs       []models.DailyLog
	Now        time.Time
	Location   *time.Location
	Disclaimer string
}

// calendarFeedEvent is one resolved all-day prediction event before rendering.
// kind is a stable, non-secret discriminator used only to build a deterministic
// UID; it is NEVER placed in the SUMMARY (that stays neutral).
type calendarFeedEvent struct {
	kind string
	date time.Time
}

// BuildCalendarFeedICS renders the owner's upcoming-cycle .ics body. It always
// returns a well-formed VCALENDAR (even with zero events — a calendar client
// expects a valid, possibly empty, feed), so the api layer can serve a 200 with
// a stable structure whether or not predictions are currently available.
//
// The decision, in order (mirrors DecideDueReminders' medical-safety gate):
//   - Build cycle stats from the owner's logs via the SAME path the dashboard
//     uses (StatsService.BuildCycleStatsFromLogs, which needs no repositories).
//   - If in-app predictions are suppressed (DashboardPredictionDisabled — the
//     owner's unpredictable-cycle mode — or stats.PregnancyPaused), emit ZERO
//     events. This is the hard medical-safety suppression gate.
//   - Otherwise project the next calendarFeedProjectionCycles cycles forward from
//     the owner's last period start, reusing the dashboard's own cycle-start
//     projection + window helpers, and emit a predicted-next-period event and an
//     ovulation event per projected cycle when each is calculable and not in the
//     past.
func BuildCalendarFeedICS(input CalendarFeedICSInput) []byte {
	events := calendarFeedEvents(input)
	return renderCalendarFeedICS(events, input.Disclaimer, input.Now)
}

// calendarFeedEvents resolves the neutral, all-day prediction events for the
// feed. Returns nil when predictions are suppressed or no cycle can be
// projected.
func calendarFeedEvents(input CalendarFeedICSInput) []calendarFeedEvent {
	user := input.User
	if user == nil {
		return nil
	}

	// Reuse the exact dashboard prediction path — a zero-dependency StatsService
	// runs precisely the dashboard's stats derivation (baseline + pregnancy-pause
	// resolution) without a store, exactly as DecideDueReminders does.
	stats := NewStatsService(nil, nil).BuildCycleStatsFromLogs(user, input.Logs, input.Now, input.Location)

	// Medical-safety suppression gate: if the app suppresses predictions, emit
	// nothing. Pregnancy-pause OR unpredictable-cycle mode both suppress.
	if DashboardPredictionDisabled(user) || stats.PregnancyPaused {
		return nil
	}

	today := DateAtLocation(input.Now, input.Location)
	cycleLength := DashboardProjectionCycleLength(user, stats)
	if stats.LastPeriodStart.IsZero() || cycleLength <= 0 {
		return nil
	}

	// Anchor the first projected cycle to the current/next cycle start exactly as
	// DashboardUpcomingPredictions does, then step forward one cycle at a time.
	cycleStart, _, ok := ProjectCycleStart(stats.LastPeriodStart, cycleLength, today)
	if !ok {
		// codecov:ignore -- defensive: ProjectCycleStart only reports !ok for a zero
		// LastPeriodStart or non-positive cycleLength, both already returned above.
		return nil
	}

	events := make([]calendarFeedEvent, 0, calendarFeedProjectionCycles*2)
	seen := make(map[string]struct{}, calendarFeedProjectionCycles*2)
	appendEvent := func(kind string, date time.Time) {
		key := kind + "-" + date.Format(calendarFeedDateLayout)
		if _, dup := seen[key]; dup {
			// codecov:ignore -- defensive: projected cycles step strictly forward, so
			// (kind, date) is unique in practice; dedupe guards the UID invariant if
			// that ever stops holding rather than falling back to a disambiguating
			// suffix that would break UID stability across renders.
			return
		}
		seen[key] = struct{}{}
		events = append(events, calendarFeedEvent{kind: kind, date: date})
	}
	for cycle := range calendarFeedProjectionCycles {
		anchor := CalendarDay(cycleStart.AddDate(0, 0, cycle*cycleLength), input.Location)

		nextPeriodStart := CalendarDay(anchor.AddDate(0, 0, cycleLength), input.Location)
		if !nextPeriodStart.Before(today) {
			appendEvent("period", nextPeriodStart)
		}

		window := PredictCycleWindow(anchor, cycleLength, stats.LutealPhase)
		if window.Calculable && !window.OvulationDate.Before(today) {
			appendEvent("ovulation", CalendarDay(window.OvulationDate, input.Location))
		}
	}
	return events
}

// renderCalendarFeedICS assembles the RFC 5545 VCALENDAR text. Every line is
// CRLF-terminated (RFC 5545 §3.1). DTSTAMP is the injected now in UTC. Each
// VEVENT is an all-day event (VALUE=DATE DTSTART, exclusive next-day DTEND per
// RFC 5545 §3.6.1), carries the fixed neutral SUMMARY, and puts the disclaimer
// in DESCRIPTION.
func renderCalendarFeedICS(events []calendarFeedEvent, disclaimer string, now time.Time) []byte {
	stamp := now.UTC().Format(calendarFeedTimestampLayout)

	var b strings.Builder
	writeICSLine(&b, "BEGIN:VCALENDAR")
	writeICSLine(&b, "VERSION:2.0")
	writeICSLine(&b, "PRODID:"+calendarFeedProductID)
	writeICSLine(&b, "CALSCALE:GREGORIAN")
	writeICSLine(&b, "METHOD:PUBLISH")
	for _, event := range events {
		writeCalendarFeedEvent(&b, event, stamp, disclaimer)
	}
	writeICSLine(&b, "END:VCALENDAR")
	return []byte(b.String())
}

func writeCalendarFeedEvent(b *strings.Builder, event calendarFeedEvent, stamp string, disclaimer string) {
	start := event.date.Format(calendarFeedDateLayout)
	end := event.date.AddDate(0, 0, 1).Format(calendarFeedDateLayout)

	writeICSLine(b, "BEGIN:VEVENT")
	// UID is a pure function of (kind, date) — stable across renders/polls at
	// different `now` so a calendar client recognizes the same logical event
	// instead of recreating it (losing alarms/edits). No owner id, no secret,
	// no render-order index.
	writeICSLine(b, fmt.Sprintf("UID:%s-%s@ovumcy", event.kind, start))
	writeICSLine(b, "DTSTAMP:"+stamp)
	writeICSLine(b, "DTSTART;VALUE=DATE:"+start)
	writeICSLine(b, "DTEND;VALUE=DATE:"+end)
	writeICSLine(b, "SUMMARY:"+escapeICSText(calendarFeedNeutralSummary))
	writeICSLine(b, "DESCRIPTION:"+escapeICSText(disclaimer))
	writeICSLine(b, "TRANSP:TRANSPARENT")
	writeICSLine(b, "END:VEVENT")
}

// writeICSLine appends one content line with RFC 5545's mandatory CRLF
// terminator, folding lines longer than 75 octets per §3.1. Folding is applied
// to the fully-escaped line so an escape sequence is never split.
func writeICSLine(b *strings.Builder, line string) {
	b.WriteString(foldICSLine(line))
	b.WriteString("\r\n")
}

// foldICSLine folds a content line to <=75 octets per RFC 5545 §3.1: where a
// fold is needed a CRLF followed by a single space is inserted and the octet
// count restarts. Folding is done on RUNE boundaries, never mid-rune, so a
// multi-byte UTF-8 sequence in a localized disclaimer is never split into
// invalid bytes (a byte-boundary fold could corrupt non-ASCII copy). CR/LF have
// already been stripped by escapeICSText, so no fold can be mistaken for a real
// line break.
func foldICSLine(line string) string {
	const maxOctets = 75
	if len(line) <= maxOctets {
		return line
	}
	var b strings.Builder
	segmentOctets := 0
	for _, r := range line {
		runeOctets := utf8.RuneLen(r)
		if runeOctets < 0 {
			runeOctets = 1 // codecov:ignore -- RuneLen only returns -1 for an invalid rune; ranging a Go string yields RuneError (valid, len 3), so this is unreachable here.
		}
		if segmentOctets > 0 && segmentOctets+runeOctets > maxOctets {
			b.WriteString("\r\n ")
			segmentOctets = 0
		}
		b.WriteRune(r)
		segmentOctets += runeOctets
	}
	return b.String()
}

// escapeICSText escapes a TEXT value per RFC 5545 §3.3.11: backslash, semicolon,
// comma are backslash-escaped; CR/LF are collapsed to the literal "\n" escape so
// a value can never inject a new content line (defense against a stray newline
// in translated copy breaking the calendar structure).
func escapeICSText(value string) string {
	replacer := strings.NewReplacer(
		"\\", "\\\\",
		";", "\\;",
		",", "\\,",
		"\r\n", "\\n",
		"\r", "\\n",
		"\n", "\\n",
	)
	return replacer.Replace(value)
}
