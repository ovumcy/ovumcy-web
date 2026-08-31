package services

import (
	"context"
	"testing"
	"time"

	"github.com/ovumcy/ovumcy-web/internal/models"
)

func TestResolveDayFeedbackUsesSelfCareMessageForEarlyPeriodDays(t *testing.T) {
	logs := newDayLogRepositoryStub()
	users := &dayUserRepositoryStub{}
	service := NewDayService(logs, users)

	logs.entries["2026-02-01"] = models.DailyLog{UserID: 10, Date: mustParseDayFeedbackDate(t, "2026-02-01"), IsPeriod: true}
	logs.entries["2026-03-01"] = models.DailyLog{UserID: 10, Date: mustParseDayFeedbackDate(t, "2026-03-01"), IsPeriod: true}
	logs.entries["2026-03-02"] = models.DailyLog{UserID: 10, Date: mustParseDayFeedbackDate(t, "2026-03-02"), IsPeriod: true}

	state, err := service.ResolveDayFeedback(context.Background(), &models.User{ID: 10}, mustParseDayFeedbackDate(t, "2026-03-02"), mustParseDayFeedbackDate(t, "2026-03-02"), time.UTC)
	if err != nil {
		t.Fatalf("ResolveDayFeedback() unexpected error: %v", err)
	}
	if state.MessageKey != daySaveMessageSelfCare {
		t.Fatalf("expected self-care message, got %q", state.MessageKey)
	}
}

// The positive anchor for the suppression below: one COMPLETED cycle (two
// observed starts) makes the fertility window an observation of this account,
// and the fertile save message renders.
func TestResolveDayFeedbackUsesFertileMessageDuringFertilityWindow(t *testing.T) {
	logs := newDayLogRepositoryStub()
	users := &dayUserRepositoryStub{}
	service := NewDayService(logs, users)

	logs.entries["2026-02-01"] = models.DailyLog{UserID: 10, Date: mustParseDayFeedbackDate(t, "2026-02-01"), IsPeriod: true}
	logs.entries["2026-03-01"] = models.DailyLog{UserID: 10, Date: mustParseDayFeedbackDate(t, "2026-03-01"), IsPeriod: true}

	state, err := service.ResolveDayFeedback(context.Background(), &models.User{ID: 10}, mustParseDayFeedbackDate(t, "2026-03-12"), mustParseDayFeedbackDate(t, "2026-03-12"), time.UTC)
	if err != nil {
		t.Fatalf("ResolveDayFeedback() unexpected error: %v", err)
	}
	if state.MessageKey != daySaveMessageFertile {
		t.Fatalf("expected fertile message, got %q", state.MessageKey)
	}
}

// TestResolveDayFeedbackWithholdsTheFertileMessageBeforeTheFirstCompletedCycle
// pins the medical-safety floor for the day-save message: with a single logged
// period start the account has completed no cycle, so cycleLengths returns
// nothing, the median and the average stay 0, and predictedCycleLength falls
// through to models.DefaultCycleLength. The fertility window that comes out is
// that default projected forward with the default luteal phase — configuration,
// not observation. Display confidence follows data confidence there:
// suppression is the floor and a qualifier is not enough
// (docs/SECURITY_INVARIANTS.md → medical safety), so the save message drops
// back to the neutral one.
func TestResolveDayFeedbackWithholdsTheFertileMessageBeforeTheFirstCompletedCycle(t *testing.T) {
	logs := newDayLogRepositoryStub()
	users := &dayUserRepositoryStub{}
	service := NewDayService(logs, users)

	firstStart := mustParseDayFeedbackDate(t, "2026-03-01")
	seeded := make([]models.DailyLog, 0, 4)
	for offset := range 4 {
		day := firstStart.AddDate(0, 0, offset)
		entry := models.DailyLog{UserID: 10, Date: day, IsPeriod: true}
		logs.entries[day.Format("2006-01-02")] = entry
		seeded = append(seeded, entry)
	}

	// The precondition this guard rests on, asserted rather than assumed: no
	// completed cycle, and the requested day genuinely inside the projected
	// window — otherwise the test could go green on a window that moved.
	day := mustParseDayFeedbackDate(t, "2026-03-12")
	stats := BuildCycleStats(seeded, day)
	if stats.CompletedCycleCount != 0 {
		t.Fatalf("expected zero completed cycles from one logged period start, got %d", stats.CompletedCycleCount)
	}
	if stats.FertilityWindowStart.IsZero() || day.Before(stats.FertilityWindowStart) || day.After(stats.FertilityWindowEnd) {
		t.Fatalf("expected %s inside the projected fertility window %s..%s",
			day.Format("2006-01-02"),
			stats.FertilityWindowStart.Format("2006-01-02"),
			stats.FertilityWindowEnd.Format("2006-01-02"))
	}

	state, err := service.ResolveDayFeedback(context.Background(), &models.User{ID: 10}, day, day, time.UTC)
	if err != nil {
		t.Fatalf("ResolveDayFeedback() unexpected error: %v", err)
	}
	if state.MessageKey == daySaveMessageFertile {
		t.Fatal("expected no fertile claim on a window derived only from configuration defaults")
	}
	if state.MessageKey != daySaveMessageNeutral {
		t.Fatalf("expected the neutral save message before the first completed cycle, got %q", state.MessageKey)
	}
}

func TestResolveDayFeedbackReturnsNeutralMessageForUnpredictableCycle(t *testing.T) {
	logs := newDayLogRepositoryStub()
	users := &dayUserRepositoryStub{}
	service := NewDayService(logs, users)

	logs.entries["2026-02-01"] = models.DailyLog{UserID: 10, Date: mustParseDayFeedbackDate(t, "2026-02-01"), IsPeriod: true}
	logs.entries["2026-03-01"] = models.DailyLog{UserID: 10, Date: mustParseDayFeedbackDate(t, "2026-03-01"), IsPeriod: true}

	state, err := service.ResolveDayFeedback(context.Background(), &models.User{ID: 10, UnpredictableCycle: true}, mustParseDayFeedbackDate(t, "2026-03-12"), mustParseDayFeedbackDate(t, "2026-03-12"), time.UTC)
	if err != nil {
		t.Fatalf("ResolveDayFeedback() unexpected error: %v", err)
	}
	if state.MessageKey != daySaveMessageNeutral {
		t.Fatalf("expected neutral message for unpredictable cycle mode, got %q", state.MessageKey)
	}
}

func TestResolveDayFeedbackUsesPregnancyPausedMessageAfterPositiveTest(t *testing.T) {
	logs := newDayLogRepositoryStub()
	users := &dayUserRepositoryStub{}
	service := NewDayService(logs, users)

	logs.entries["2026-02-01"] = models.DailyLog{UserID: 10, Date: mustParseDayFeedbackDate(t, "2026-02-01"), IsPeriod: true}
	logs.entries["2026-03-01"] = models.DailyLog{UserID: 10, Date: mustParseDayFeedbackDate(t, "2026-03-01"), IsPeriod: true}
	logs.entries["2026-03-20"] = models.DailyLog{UserID: 10, Date: mustParseDayFeedbackDate(t, "2026-03-20"), PregnancyTest: models.PregnancyTestPositive}

	state, err := service.ResolveDayFeedback(context.Background(), &models.User{ID: 10}, mustParseDayFeedbackDate(t, "2026-03-20"), mustParseDayFeedbackDate(t, "2026-03-20"), time.UTC)
	if err != nil {
		t.Fatalf("ResolveDayFeedback() unexpected error: %v", err)
	}
	if state.MessageKey != daySaveMessagePregnancyPaused {
		t.Fatalf("expected pregnancy-paused message, got %q", state.MessageKey)
	}
}

func TestResolveDayFeedbackShowsSpottingWarningOnCycleStart(t *testing.T) {
	logs := newDayLogRepositoryStub()
	users := &dayUserRepositoryStub{}
	service := NewDayService(logs, users)

	logs.entries["2026-02-01"] = models.DailyLog{UserID: 10, Date: mustParseDayFeedbackDate(t, "2026-02-01"), IsPeriod: true}
	logs.entries["2026-03-01"] = models.DailyLog{
		UserID:   10,
		Date:     mustParseDayFeedbackDate(t, "2026-03-01"),
		IsPeriod: true,
		Flow:     models.FlowSpotting,
	}

	state, err := service.ResolveDayFeedback(context.Background(), &models.User{ID: 10}, mustParseDayFeedbackDate(t, "2026-03-01"), mustParseDayFeedbackDate(t, "2026-03-01"), time.UTC)
	if err != nil {
		t.Fatalf("ResolveDayFeedback() unexpected error: %v", err)
	}
	if !state.ShowSpottingCycleWarning {
		t.Fatalf("expected spotting warning on the first spotted cycle day")
	}
}

func TestResolveDayFeedbackShowsSpottingWarningForLocalCycleStart(t *testing.T) {
	logs := newDayLogRepositoryStub()
	users := &dayUserRepositoryStub{}
	service := NewDayService(logs, users)
	location := time.FixedZone("UTC+2", 2*60*60)
	day := time.Date(2026, time.March, 1, 0, 0, 0, 0, location)

	// Canonical date-only storage: UTC midnight of the calendar day.
	// Pre-fix this test stored 2026-02-28T22:00Z (UTC+2 midnight) to verify
	// that DateAtLocation in(location) mapped it forward to March 1 — but that
	// path no longer runs for date-only values. CalendarDay takes components
	// from the stored value as-is, so test data must already carry the correct
	// calendar day (issue #48).
	logs.entries["2026-03-01"] = models.DailyLog{
		UserID:   10,
		Date:     time.Date(2026, time.March, 1, 0, 0, 0, 0, time.UTC),
		IsPeriod: true,
		Flow:     models.FlowSpotting,
	}

	state, err := service.ResolveDayFeedback(context.Background(), &models.User{ID: 10}, day, day, location)
	if err != nil {
		t.Fatalf("ResolveDayFeedback() unexpected error: %v", err)
	}
	if !state.ShowSpottingCycleWarning {
		t.Fatalf("expected spotting warning on the local cycle start day")
	}
}

func TestResolveDayFeedbackShowsLongPeriodWarningOnlyOncePerCycle(t *testing.T) {
	logs := newDayLogRepositoryStub()
	users := &dayUserRepositoryStub{}
	service := NewDayService(logs, users)
	cycleStart := mustParseDayFeedbackDate(t, "2026-03-01")

	for offset := range 9 {
		day := cycleStart.AddDate(0, 0, offset)
		logs.entries[day.Format("2006-01-02")] = models.DailyLog{
			UserID:   10,
			Date:     day,
			IsPeriod: true,
		}
	}

	state, err := service.ResolveDayFeedback(context.Background(), &models.User{ID: 10}, mustParseDayFeedbackDate(t, "2026-03-09"), mustParseDayFeedbackDate(t, "2026-03-09"), time.UTC)
	if err != nil {
		t.Fatalf("ResolveDayFeedback() unexpected error: %v", err)
	}
	if !state.ShowLongPeriodWarning {
		t.Fatalf("expected long-period warning after nine consecutive period days")
	}
	if got := state.LongPeriodCycleStart.Format("2006-01-02"); got != "2026-03-01" {
		t.Fatalf("expected long-period cycle start 2026-03-01, got %s", got)
	}

	warnedState, err := service.ResolveDayFeedback(context.Background(), &models.User{ID: 10, LongPeriodWarningCycleStart: ptrDayFeedbackTime(cycleStart)}, mustParseDayFeedbackDate(t, "2026-03-09"), mustParseDayFeedbackDate(t, "2026-03-09"), time.UTC)
	if err != nil {
		t.Fatalf("ResolveDayFeedback() unexpected error after warning acknowledgement: %v", err)
	}
	if warnedState.ShowLongPeriodWarning {
		t.Fatalf("expected warning to stay hidden once the cycle was acknowledged")
	}
}

func TestAcknowledgeLongPeriodWarningPersistsCycleStart(t *testing.T) {
	users := &dayUserRepositoryStub{}
	service := NewDayService(newDayLogRepositoryStub(), users)
	cycleStart := mustParseDayFeedbackDate(t, "2026-03-01")

	if err := service.AcknowledgeLongPeriodWarning(context.Background(), 10, cycleStart, time.UTC); err != nil {
		t.Fatalf("AcknowledgeLongPeriodWarning() unexpected error: %v", err)
	}
	if users.settings.LongPeriodWarningCycleStart == nil {
		t.Fatal("expected long-period warning date to be persisted")
	}
	if got := users.settings.LongPeriodWarningCycleStart.Format("2006-01-02"); got != "2026-03-01" {
		t.Fatalf("expected persisted warning date 2026-03-01, got %s", got)
	}
	// The written map is only half of the claim: the acknowledgement must land
	// on the acting owner's row, never on another account's.
	users.assertUserRepositoryCallsTargetOwner(t, 10)
}

func mustParseDayFeedbackDate(t *testing.T, raw string) time.Time {
	t.Helper()
	parsed, err := time.ParseInLocation("2006-01-02", raw, time.UTC)
	if err != nil {
		t.Fatalf("parse day %q: %v", raw, err)
	}
	return parsed
}

func ptrDayFeedbackTime(value time.Time) *time.Time {
	return &value
}

// TestResolveDayFeedbackSelfCareMessageInUTCPlusZone is the issue-#48-class
// regression for the save-message policy: `day` reaches the policy as a
// location-midnight value while the cycle stats carry UTC-midnight dates.
// Before the fix, instant comparison made a UTC+9 request on cycle day 1
// fall before the stored cycle start and resolve to the neutral message
// instead of self-care.
func TestResolveDayFeedbackSelfCareMessageInUTCPlusZone(t *testing.T) {
	logs := newDayLogRepositoryStub()
	users := &dayUserRepositoryStub{}
	service := NewDayService(logs, users)

	logs.entries["2026-02-01"] = models.DailyLog{UserID: 10, Date: mustParseDayFeedbackDate(t, "2026-02-01"), IsPeriod: true}
	logs.entries["2026-03-01"] = models.DailyLog{UserID: 10, Date: mustParseDayFeedbackDate(t, "2026-03-01"), IsPeriod: true}

	tokyo := time.FixedZone("UTC+9", 9*60*60)
	day := time.Date(2026, time.March, 1, 0, 0, 0, 0, tokyo)

	state, err := service.ResolveDayFeedback(context.Background(), &models.User{ID: 10}, day, day, tokyo)
	if err != nil {
		t.Fatalf("ResolveDayFeedback() unexpected error: %v", err)
	}
	if state.MessageKey != daySaveMessageSelfCare {
		t.Fatalf("expected self-care message on cycle day 1 in UTC+9, got %q", state.MessageKey)
	}
}

// TestResolveDayFeedbackFertileMessageOnWindowStartInUTCPlusZone pins the
// fertility-window edge of the same issue-#48-class bug: a UTC+9 request on
// the first day of the fertility window compared a location-midnight `day`
// instant against the UTC-midnight window start and missed the window.
func TestResolveDayFeedbackFertileMessageOnWindowStartInUTCPlusZone(t *testing.T) {
	logs := newDayLogRepositoryStub()
	users := &dayUserRepositoryStub{}
	service := NewDayService(logs, users)

	logs.entries["2026-02-01"] = models.DailyLog{UserID: 10, Date: mustParseDayFeedbackDate(t, "2026-02-01"), IsPeriod: true}
	logs.entries["2026-03-01"] = models.DailyLog{UserID: 10, Date: mustParseDayFeedbackDate(t, "2026-03-01"), IsPeriod: true}

	tokyo := time.FixedZone("UTC+9", 9*60*60)
	// 28-day observed cycle starting 2026-03-01 with the 14-day default luteal
	// phase predicts ovulation on 2026-03-14, so the fertility window is
	// 2026-03-09..2026-03-14.
	day := time.Date(2026, time.March, 9, 0, 0, 0, 0, tokyo)

	state, err := service.ResolveDayFeedback(context.Background(), &models.User{ID: 10}, day, day, tokyo)
	if err != nil {
		t.Fatalf("ResolveDayFeedback() unexpected error: %v", err)
	}
	if state.MessageKey != daySaveMessageFertile {
		t.Fatalf("expected fertile message on window start in UTC+9, got %q", state.MessageKey)
	}
}

// TestResolveDayFeedbackKeepsThePauseWhenTomorrowsCycleStartIsLogged is this
// policy's half of the one-timeline rule. It resolves the pregnancy pause from
// its own read of the owner's whole stored history rather than through
// BuildCycleStatsFromLogs, so bounding that derivation alone would have left
// this surface reading a cycle start recorded for TOMORROW as today's
// resumption — the save message announcing that predictions were back while the
// dashboard beside it still showed the pause.
func TestResolveDayFeedbackKeepsThePauseWhenTomorrowsCycleStartIsLogged(t *testing.T) {
	logs := newDayLogRepositoryStub()
	service := NewDayService(logs, &dayUserRepositoryStub{})

	today := mustParseDayFeedbackDate(t, "2026-03-12")
	logs.entries["2026-02-14"] = models.DailyLog{UserID: 10, Date: mustParseDayFeedbackDate(t, "2026-02-14"), IsPeriod: true, CycleStart: true}
	logs.entries["2026-03-12"] = models.DailyLog{UserID: 10, Date: today, PregnancyTest: models.PregnancyTestPositive}
	// Permitted by manualCycleStartFutureDays, and part of no timeline that has
	// happened yet.
	logs.entries["2026-03-13"] = models.DailyLog{UserID: 10, Date: mustParseDayFeedbackDate(t, "2026-03-13"), IsPeriod: true, CycleStart: true}

	state, err := service.ResolveDayFeedback(context.Background(), &models.User{ID: 10}, today, today, time.UTC)
	if err != nil {
		t.Fatalf("ResolveDayFeedback() unexpected error: %v", err)
	}
	if state.MessageKey != daySaveMessagePregnancyPaused {
		t.Fatalf("expected the pause to hold against a cycle start logged for tomorrow, got %q", state.MessageKey)
	}

	// Control: move that start into the past and the pause lifts here too, so
	// this case cannot pass against a policy that simply always reports a pause.
	resumedLogs := newDayLogRepositoryStub()
	resumedService := NewDayService(resumedLogs, &dayUserRepositoryStub{})
	resumedLogs.entries["2026-02-14"] = models.DailyLog{UserID: 10, Date: mustParseDayFeedbackDate(t, "2026-02-14"), IsPeriod: true, CycleStart: true}
	resumedLogs.entries["2026-03-10"] = models.DailyLog{UserID: 10, Date: mustParseDayFeedbackDate(t, "2026-03-10"), PregnancyTest: models.PregnancyTestPositive}
	resumedLogs.entries["2026-03-11"] = models.DailyLog{UserID: 10, Date: mustParseDayFeedbackDate(t, "2026-03-11"), IsPeriod: true, CycleStart: true}

	resumedState, err := resumedService.ResolveDayFeedback(context.Background(), &models.User{ID: 10}, today, today, time.UTC)
	if err != nil {
		t.Fatalf("ResolveDayFeedback() after a real resumption: %v", err)
	}
	if resumedState.MessageKey == daySaveMessagePregnancyPaused {
		t.Fatalf("a cycle start already in the past lifts the pause, got %q", resumedState.MessageKey)
	}
}
