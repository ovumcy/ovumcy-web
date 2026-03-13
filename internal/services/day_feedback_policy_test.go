package services

import (
	"testing"
	"time"

	"github.com/terraincognita07/ovumcy/internal/models"
)

func TestResolveDayFeedbackUsesSelfCareMessageForEarlyPeriodDays(t *testing.T) {
	logs := newDayLogRepositoryStub()
	users := &dayUserRepositoryStub{}
	service := NewDayService(logs, users)

	logs.entries["2026-02-01"] = models.DailyLog{UserID: 10, Date: mustParseDayFeedbackDate(t, "2026-02-01"), IsPeriod: true}
	logs.entries["2026-03-01"] = models.DailyLog{UserID: 10, Date: mustParseDayFeedbackDate(t, "2026-03-01"), IsPeriod: true}
	logs.entries["2026-03-02"] = models.DailyLog{UserID: 10, Date: mustParseDayFeedbackDate(t, "2026-03-02"), IsPeriod: true}

	state, err := service.ResolveDayFeedback(&models.User{ID: 10}, mustParseDayFeedbackDate(t, "2026-03-02"), mustParseDayFeedbackDate(t, "2026-03-02"), time.UTC)
	if err != nil {
		t.Fatalf("ResolveDayFeedback() unexpected error: %v", err)
	}
	if state.MessageKey != daySaveMessageSelfCare {
		t.Fatalf("expected self-care message, got %q", state.MessageKey)
	}
}

func TestResolveDayFeedbackUsesFertileMessageDuringFertilityWindow(t *testing.T) {
	logs := newDayLogRepositoryStub()
	users := &dayUserRepositoryStub{}
	service := NewDayService(logs, users)

	logs.entries["2026-02-01"] = models.DailyLog{UserID: 10, Date: mustParseDayFeedbackDate(t, "2026-02-01"), IsPeriod: true}
	logs.entries["2026-03-01"] = models.DailyLog{UserID: 10, Date: mustParseDayFeedbackDate(t, "2026-03-01"), IsPeriod: true}

	state, err := service.ResolveDayFeedback(&models.User{ID: 10}, mustParseDayFeedbackDate(t, "2026-03-12"), mustParseDayFeedbackDate(t, "2026-03-12"), time.UTC)
	if err != nil {
		t.Fatalf("ResolveDayFeedback() unexpected error: %v", err)
	}
	if state.MessageKey != daySaveMessageFertile {
		t.Fatalf("expected fertile message, got %q", state.MessageKey)
	}
}

func TestResolveDayFeedbackReturnsNeutralMessageForUnpredictableCycle(t *testing.T) {
	logs := newDayLogRepositoryStub()
	users := &dayUserRepositoryStub{}
	service := NewDayService(logs, users)

	logs.entries["2026-02-01"] = models.DailyLog{UserID: 10, Date: mustParseDayFeedbackDate(t, "2026-02-01"), IsPeriod: true}
	logs.entries["2026-03-01"] = models.DailyLog{UserID: 10, Date: mustParseDayFeedbackDate(t, "2026-03-01"), IsPeriod: true}

	state, err := service.ResolveDayFeedback(&models.User{ID: 10, UnpredictableCycle: true}, mustParseDayFeedbackDate(t, "2026-03-12"), mustParseDayFeedbackDate(t, "2026-03-12"), time.UTC)
	if err != nil {
		t.Fatalf("ResolveDayFeedback() unexpected error: %v", err)
	}
	if state.MessageKey != daySaveMessageNeutral {
		t.Fatalf("expected neutral message for unpredictable cycle mode, got %q", state.MessageKey)
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

	state, err := service.ResolveDayFeedback(&models.User{ID: 10}, mustParseDayFeedbackDate(t, "2026-03-01"), mustParseDayFeedbackDate(t, "2026-03-01"), time.UTC)
	if err != nil {
		t.Fatalf("ResolveDayFeedback() unexpected error: %v", err)
	}
	if !state.ShowSpottingCycleWarning {
		t.Fatalf("expected spotting warning on the first spotted cycle day")
	}
}

func TestResolveDayFeedbackShowsLongPeriodWarningOnlyOncePerCycle(t *testing.T) {
	logs := newDayLogRepositoryStub()
	users := &dayUserRepositoryStub{}
	service := NewDayService(logs, users)
	cycleStart := mustParseDayFeedbackDate(t, "2026-03-01")

	for offset := 0; offset < 9; offset++ {
		day := cycleStart.AddDate(0, 0, offset)
		logs.entries[day.Format("2006-01-02")] = models.DailyLog{
			UserID:   10,
			Date:     day,
			IsPeriod: true,
		}
	}

	state, err := service.ResolveDayFeedback(&models.User{ID: 10}, mustParseDayFeedbackDate(t, "2026-03-09"), mustParseDayFeedbackDate(t, "2026-03-09"), time.UTC)
	if err != nil {
		t.Fatalf("ResolveDayFeedback() unexpected error: %v", err)
	}
	if !state.ShowLongPeriodWarning {
		t.Fatalf("expected long-period warning after nine consecutive period days")
	}
	if got := state.LongPeriodCycleStart.Format("2006-01-02"); got != "2026-03-01" {
		t.Fatalf("expected long-period cycle start 2026-03-01, got %s", got)
	}

	warnedState, err := service.ResolveDayFeedback(&models.User{ID: 10, LongPeriodWarnedAt: ptrDayFeedbackTime(cycleStart)}, mustParseDayFeedbackDate(t, "2026-03-09"), mustParseDayFeedbackDate(t, "2026-03-09"), time.UTC)
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

	if err := service.AcknowledgeLongPeriodWarning(10, cycleStart, time.UTC); err != nil {
		t.Fatalf("AcknowledgeLongPeriodWarning() unexpected error: %v", err)
	}
	if users.settings.LongPeriodWarnedAt == nil {
		t.Fatal("expected long-period warning date to be persisted")
	}
	if got := users.settings.LongPeriodWarnedAt.Format("2006-01-02"); got != "2026-03-01" {
		t.Fatalf("expected persisted warning date 2026-03-01, got %s", got)
	}
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
