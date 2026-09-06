package services

import (
	"time"

	"github.com/ovumcy/ovumcy-web/internal/models"
)

// Webhook reminder decision (issue #124, slice 2). This file is the PURE
// decision layer: given an owner, their webhook settings (including the two
// per-kind "already sent" watermarks passed in by the caller), their day logs,
// and an injected now/location, it reports which reminders are DUE right now.
//
// It performs NO delivery, NO outbound HTTP, and NO persistence — a later
// slice owns sending the POST and advancing the watermark. Keeping the decision
// pure makes every gate (window, suppression, idempotency, timezone) directly
// unit-testable with an injected clock.
//
// Medical-safety invariant: this function reuses the EXACT prediction path the
// dashboard uses (BuildCycleStatsFromLogs → DashboardUpcomingPredictions, gated
// by DashboardPredictionDisabled / PregnancyPaused / DashboardCycleOverdue). It
// never fabricates a date the app itself refuses to show — when in-app
// predictions are suppressed, it emits nothing.

const (
	// DueReminderTypePeriod and DueReminderTypeOvulation identify which upcoming
	// prediction a due reminder summarizes. They mirror the in-app banner kinds
	// (DashboardReminderBannerKind*) so downstream copy selection can stay
	// aligned with the dashboard.
	DueReminderTypePeriod    = models.WebhookReminderTypePeriod
	DueReminderTypeOvulation = models.WebhookReminderTypeOvulation
)

// DueReminder is the transport-free result of the decision: one upcoming
// reminder that is within the owner's lead window and has not already been sent
// for its cycle. It carries no delivery concern (no URL, no payload) — that is
// the delivery slice's job.
//
//   - Type is DueReminderTypePeriod or DueReminderTypeOvulation.
//   - EventDate is the predicted event's owner-local calendar day (the next
//     period start, or the ovulation date).
//   - CycleAnchor is the cycle-start the event belongs to. It is the watermark
//     KEY: the delivery slice records it so at most one reminder of each kind is
//     sent per cycle, and this decision skips a reminder whose incoming
//     watermark already equals it.
//   - LeadDays echoes the window that was in force (settings.ReminderLeadDays,
//     clamped), for observability by the caller.
//
// EVERY EventDate here is an estimate, never fact (medical-safety invariant), so
// every surface rendering one must carry the estimate qualifier and the
// non-medical-advice disclaimer. That obligation used to be spelled as an
// Estimate field set to a literal true by both producers and read by nothing: a
// constant cannot distinguish a case, so no consumer could branch on it and the
// invariant travelled as prose wearing the shape of data. It travels as data
// where it is actually enforced — buildPayload sets WebhookPayload.Disclaimer on
// every payload unconditionally, and reminderCopy words the date as an estimate.
// Regression: TestNotifyDisclaimerPresentInEveryPayload.
type DueReminder struct {
	Type        string
	EventDate   time.Time
	CycleAnchor time.Time
	LeadDays    int
}

// WebhookReminderSettings is the transport-free webhook decision input: the
// per-owner webhook fields plus the two per-kind watermarks. The watermarks are
// passed IN (rather than read from a store here) so "already sent" exclusion is
// part of the pure decision and is directly unit-testable.
//
// This mirrors the persisted columns on models.User / models.WebhookNotifyRecord
// but is a distinct decision-scoped type: the decision needs only these fields,
// not the whole account, and never the (still-ciphertext) webhook URL — the URL
// is a delivery concern, decrypted by the delivery slice, and deliberately
// absent here so it cannot leak into a decision log.
//
//   - Enabled is the master switch; false ⇒ no reminders at all.
//   - NotifyPeriod / NotifyOvulation are the per-kind opt-ins.
//   - ReminderLeadDays is the lead window in days; it is clamped through
//     NormalizeReminderLeadDays before use, so an out-of-range stored value can
//     never widen or invert the window.
//   - PeriodWatermark / OvulationWatermark are the cycle-start anchors a reminder
//     of each kind was last sent for (nil until the first send). A reminder is
//     skipped when its computed CycleAnchor already equals the matching
//     watermark.
type WebhookReminderSettings struct {
	Enabled            bool
	NotifyPeriod       bool
	NotifyOvulation    bool
	ReminderLeadDays   int
	PeriodWatermark    *time.Time
	OvulationWatermark *time.Time
}

// WebhookReminderSettingsFromNotifyRecord adapts the persistence read-projection
// (models.WebhookNotifyRecord, produced by the future batch query) into the
// decision-scoped settings type. It copies only the decision fields; the
// ciphertext URL and cycle-prediction inputs on the record are consumed
// elsewhere (URL by delivery, prediction inputs via the *models.User the caller
// also builds). Kept here so the eventual batch caller has one obvious seam and
// the decision never learns the record shape.
func WebhookReminderSettingsFromNotifyRecord(record models.WebhookNotifyRecord) WebhookReminderSettings {
	return WebhookReminderSettings{
		Enabled:            record.WebhookEnabled,
		NotifyPeriod:       record.WebhookNotifyPeriod,
		NotifyOvulation:    record.WebhookNotifyOvulation,
		ReminderLeadDays:   record.ReminderLeadDays,
		PeriodWatermark:    record.WebhookPeriodLastSentCycleStart,
		OvulationWatermark: record.WebhookOvulationLastSentCycleStart,
	}
}

// DecideDueReminders reports the webhook reminders that are due for an owner
// right now. It is pure: now and location are injected (never time.Now()), and
// it reads — never writes — the incoming watermarks.
//
// The decision, in order:
//
//   - Webhook delivery disabled ⇒ nothing.
//
//   - Build cycle stats from the owner's logs via the SAME path the dashboard
//     uses (BuildCycleStatsFromLogs, which needs no repositories).
//
//   - In-app predictions suppressed (DashboardPredictionDisabled — the owner's
//     unpredictable-cycle mode — stats.PregnancyPaused, or DashboardCycleOverdue
//     — the running cycle is past the account's reference length by more than a
//     week) ⇒ nothing. This is the medical-safety gate: never emit a date the app
//     itself refuses to show.
//
//     The overdue disjunct also ends a repeat that had no end: past that point
//     DashboardUpcomingPredictions rolls the projection a whole cycle forward, so
//     the next-period date it yields is manufactured, and — because that same
//     date is the period reminder's watermark KEY — every roll produced an anchor
//     no watermark covered and re-armed a fresh "period soon" send, once per
//     projected cycle, for as long as the cycle stayed unclosed. Suppressing the
//     reminder suppresses the write too: this decision emits nothing, the notify
//     pass writes a watermark only after a 2xx delivery, so an overdue cycle
//     leaves the watermarks exactly where the last real send left them. When the
//     owner finally logs the real cycle start, the anchor is that new cycle's
//     projected next-period date — different from the stale watermark — so the
//     next legitimate reminder fires exactly once and its own watermark then
//     covers it.
//
//   - Resolve "today" as the owner-local calendar day (DateAtLocation) and the
//     median-first projection cycle length the dashboard feeds to its predictions
//     (DashboardProjectionCycleLength — the same statistic as stats.NextPeriodStart,
//     NOT the average-first staleness reference), then take the dashboard's own
//     upcoming prediction (DashboardUpcomingPredictions) for the authoritative
//     next-period and ovulation dates + flags.
//
//   - period-soon: emitted when NotifyPeriod is on, the next period start is
//     known, and it falls within [today, today+leadDays] (inclusive) — i.e. not
//     in the past and not beyond the window. Its cycle anchor is the next period
//     start itself (definitionally the start of the next cycle). Skipped when the
//     incoming period watermark already equals that anchor.
//
//   - ovulation-soon: emitted when NotifyOvulation is on and ovulation is
//     calculable (not impossible, non-zero) and within the same window. Its cycle
//     anchor is the start of the cycle the ovulation belongs to (derived with the
//     same projection helpers the dashboard uses). Skipped when the incoming
//     ovulation watermark already equals that anchor.
//
// When both kinds are due, both are returned (unlike the single-slot in-app
// banner): a webhook consumer can act on each independently. Period precedes
// ovulation in the returned slice.
func DecideDueReminders(user *models.User, settings WebhookReminderSettings, logs []models.DailyLog, now time.Time, location *time.Location) []DueReminder {
	reminders, _ := decideDueReminders(user, settings, logs, now, location)
	return reminders
}

// decideDueReminders is the single traversal behind DecideDueReminders. It
// returns the due set AND watermarkSuppressed: how many reminders this owner
// would have received but for a watermark that already covers them — the count
// the notify pass reports as SkippedIdempotent.
//
// The count comes from the same pass that builds the due set because the
// traversal is not cheap: it rebuilds the owner's cycle statistics from their
// whole logged history, so deriving the counter by deciding a second time with
// the watermarks cleared made the observability cost equal the cost of the work
// it observes. Nothing about the authoritative decision changes: a kind is
// counted exactly when its own watermark is what withheld it, which is the
// definition the second decision approximated by subtraction.
func decideDueReminders(user *models.User, settings WebhookReminderSettings, logs []models.DailyLog, now time.Time, location *time.Location) ([]DueReminder, int) {
	if !settings.Enabled {
		return nil, 0
	}

	// Reuse the exact dashboard prediction path. BuildCycleStatsFromLogs is
	// package-level precisely because it consults no repositories, so this pass
	// runs the dashboard's stats derivation (baseline + pregnancy-pause
	// resolution) without constructing — and without depending on never
	// dereferencing — a store-less service.
	// Published through the one adapter every projection surface shares, so this
	// pass holds the same cleared stats /stats and the JSON API publish, and reads
	// the verdict it returns rather than asking the predicates a second time.
	stats, suppression := PublishedStats(user, BuildCycleStatsFromLogs(user, logs, now, location))

	// Medical-safety gate: if the app suppresses predictions, emit nothing. The
	// three signals are read through the predicate every surface shares.
	if suppression.PredictionsSuppressed {
		return nil, 0
	}

	today := DateAtLocation(now, location)
	leadDays := NormalizeReminderLeadDays(settings.ReminderLeadDays)
	cycleLength := DashboardProjectionCycleLength(user, stats)
	prediction := DashboardUpcomingPredictions(stats, user, today, cycleLength)

	reminders := make([]DueReminder, 0, 2)
	watermarkSuppressed := 0

	due, ok, watermarked := decidePeriodReminder(settings, prediction, today, leadDays)
	if ok {
		reminders = append(reminders, due)
	}
	if watermarked {
		watermarkSuppressed++
	}
	// The ovulation reminder carries the extra first-cycle floor: before one
	// cycle has been observed its date comes from the onboarding slider alone,
	// and this pass sends it to an endpoint outside the instance
	// (FertilityProjectionSuppressed). The period reminder keeps its own path —
	// it is anchored on a recorded cycle start and rides the estimate flag.
	// A confirmed thermal shift outranks the projection it supersedes. This
	// reminder says an ovulation is COMING; once the temperatures have named the
	// day it happened on, sending the projected day puts a second date for one
	// shift outside the instance, where the dashboard and the grid have already
	// moved onto the day inferred from the temperature shift. ConfirmedOvulationSupersedes bounds that to
	// the confirmation's own cycle, so a projection that has rolled into the next
	// one still sends.
	if !suppression.FertilitySuppressed &&
		!ConfirmedOvulationSupersedes(user, logs, stats, prediction.OvulationDate, today, location) {
		due, ok, watermarked := decideOvulationReminder(stats, settings, prediction, today, cycleLength, leadDays)
		if ok {
			reminders = append(reminders, due)
		}
		if watermarked {
			watermarkSuppressed++
		}
	}

	if len(reminders) == 0 {
		return nil, watermarkSuppressed
	}
	return reminders, watermarkSuppressed
}

// decidePeriodReminder applies the period-soon rule. The next period start is
// the anchor of the upcoming cycle, so it doubles as the event date and the
// watermark key.
//
// The second result says the reminder is due; the third says it was withheld by
// its OWN watermark — an already-sent reminder, not an absent one. Only that
// case is idempotency: a reminder outside the lead window, or of a kind the
// owner turned off, is simply not due and must never be reported as skipped.
func decidePeriodReminder(settings WebhookReminderSettings, prediction DashboardUpcomingPrediction, today time.Time, leadDays int) (reminder DueReminder, due bool, watermarkSuppressed bool) {
	if !settings.NotifyPeriod {
		return DueReminder{}, false, false
	}
	eventDate := prediction.NextPeriodStart
	if !reminderWithinWindow(today, eventDate, leadDays) {
		return DueReminder{}, false, false
	}
	anchor := CalendarDay(eventDate, today.Location())
	if watermarkCoversAnchor(settings.PeriodWatermark, anchor) {
		return DueReminder{}, false, true
	}
	return DueReminder{
		Type:        DueReminderTypePeriod,
		EventDate:   eventDate,
		CycleAnchor: anchor,
		LeadDays:    leadDays,
	}, true, false
}

// decideOvulationReminder applies the ovulation-soon rule. The ovulation's cycle
// anchor is the start of the cycle it belongs to, derived with the same
// projection helpers DashboardUpcomingPredictions uses so the two never drift.
//
// Its three results carry the same meaning as decidePeriodReminder's: due, and
// separately withheld by its own watermark.
func decideOvulationReminder(stats CycleStats, settings WebhookReminderSettings, prediction DashboardUpcomingPrediction, today time.Time, cycleLength int, leadDays int) (reminder DueReminder, due bool, watermarkSuppressed bool) {
	if !settings.NotifyOvulation || prediction.OvulationImpossible {
		return DueReminder{}, false, false
	}
	eventDate := prediction.OvulationDate
	if !reminderWithinWindow(today, eventDate, leadDays) {
		return DueReminder{}, false, false
	}
	anchor := ovulationCycleAnchor(stats, today, cycleLength)
	// codecov:ignore:start -- defensive and unreachable from this call path: an
	// in-window ovulation (reminderWithinWindow true above ⇒ non-zero, and
	// OvulationImpossible false) is only produced by DashboardUpcomingPredictions
	// when LastPeriodStart is non-zero and cycleLength > 0, which is exactly what
	// ovulationCycleAnchor needs to return a non-zero anchor. Kept so a reminder
	// is never emitted without a watermark key the delivery slice can dedupe on.
	if anchor.IsZero() {
		return DueReminder{}, false, false
	}
	// codecov:ignore:end
	if watermarkCoversAnchor(settings.OvulationWatermark, anchor) {
		return DueReminder{}, false, true
	}
	return DueReminder{
		Type:        DueReminderTypeOvulation,
		EventDate:   eventDate,
		CycleAnchor: anchor,
		LeadDays:    leadDays,
	}, true, false
}

// reminderWithinWindow reports whether eventDate falls in the inclusive lead
// window [today, today+leadDays]: not in the past (>= 0 days out) and not
// beyond the lead (<= leadDays). Both operands are calendar days; the distance
// is measured with CalendarDaysBetween so it is immune to time-of-day and
// timezone-midnight skew (never a raw time subtraction). A zero eventDate
// (not yet calculable) is never in-window.
func reminderWithinWindow(today time.Time, eventDate time.Time, leadDays int) bool {
	if eventDate.IsZero() {
		return false
	}
	daysUntil := CalendarDaysBetween(today, eventDate)
	return daysUntil >= 0 && daysUntil <= leadDays
}

// watermarkCoversAnchor reports whether an incoming per-kind watermark already
// marks the given cycle anchor as sent. Comparison is by calendar day (both
// re-anchored via CalendarDaysBetween), so a watermark stored as UTC-midnight
// and an anchor built at the owner's location still match on the same date.
func watermarkCoversAnchor(watermark *time.Time, anchor time.Time) bool {
	if watermark == nil || watermark.IsZero() || anchor.IsZero() {
		return false
	}
	return CalendarDaysBetween(*watermark, anchor) == 0
}

// ovulationCycleAnchor returns the cycle-start the predicted ovulation belongs
// to, reproducing DashboardUpcomingPredictions' own projection exactly:
// ProjectCycleStart from the last period start, then the ovulation-only forward
// shift when the first ovulation estimate has already passed. Reusing the same
// helpers (not reimplementing cycle math) keeps the anchor in lockstep with the
// date the dashboard shows. Returns the zero time when no cycle start can be
// projected.
func ovulationCycleAnchor(stats CycleStats, today time.Time, cycleLength int) time.Time {
	if stats.LastPeriodStart.IsZero() || cycleLength <= 0 {
		return time.Time{}
	}
	cycleStart, _, ok := ProjectCycleStart(stats.LastPeriodStart, cycleLength, today)
	if !ok {
		// codecov:ignore -- defensive: ProjectCycleStart only reports !ok for a
		// zero LastPeriodStart or non-positive cycleLength, both already returned
		// by the guard above.
		return time.Time{}
	}
	window := PredictCycleWindow(cycleStart, cycleLength, stats.LutealPhase)
	// The same calendar-day comparison DashboardUpcomingPredictions makes:
	// window.OvulationDate is a UTC-midnight date-only value, today an owner
	// location midnight, and comparing them as instants shifted the outbound
	// reminder's anchor a full cycle on the ovulation day itself in every
	// UTC-minus zone (issue #48 class).
	if window.Calculable && CalendarDaysBetween(window.OvulationDate, today) > 0 {
		cycleStart = ShiftCycleStartToFutureOvulation(cycleStart, window.OvulationDate, cycleLength, today)
	}
	return CalendarDay(cycleStart, today.Location())
}
