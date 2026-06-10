package services

import (
	"context"
	"testing"
	"time"

	"github.com/ovumcy/ovumcy-web/internal/models"
)

func TestResolveDayFeedbackNoLongPeriodWarningAtEightConsecutiveDays(t *testing.T) {
	logs := newDayLogRepositoryStub()
	users := &dayUserRepositoryStub{}
	service := NewDayService(logs, users)
	cycleStart := mustParseDayFeedbackDate(t, "2026-03-01")

	// Exactly eight consecutive period days: 2026-03-01 .. 2026-03-08.
	// The streak at the eighth day is 8, which is the boundary: the warning
	// fires only at nine or more consecutive days (streak > 8).
	for offset := 0; offset < 8; offset++ {
		day := cycleStart.AddDate(0, 0, offset)
		logs.entries[day.Format("2006-01-02")] = models.DailyLog{
			UserID:   10,
			Date:     day,
			IsPeriod: true,
		}
	}

	state, err := service.ResolveDayFeedback(context.Background(), &models.User{ID: 10}, mustParseDayFeedbackDate(t, "2026-03-08"), mustParseDayFeedbackDate(t, "2026-03-08"), time.UTC)
	if err != nil {
		t.Fatalf("ResolveDayFeedback() unexpected error: %v", err)
	}
	if state.ShowLongPeriodWarning {
		t.Fatalf("expected no long-period warning at exactly eight consecutive period days")
	}
}

func TestResolveDayFeedbackUsesSelfCareMessageOnFirstCycleDay(t *testing.T) {
	logs := newDayLogRepositoryStub()
	users := &dayUserRepositoryStub{}
	service := NewDayService(logs, users)

	// Two period starts 28 days apart make 2026-03-01 the last period start.
	// Querying the same day yields cycleDay == 1 (the lower self-care boundary).
	logs.entries["2026-02-01"] = models.DailyLog{UserID: 10, Date: mustParseDayFeedbackDate(t, "2026-02-01"), IsPeriod: true}
	logs.entries["2026-03-01"] = models.DailyLog{UserID: 10, Date: mustParseDayFeedbackDate(t, "2026-03-01"), IsPeriod: true}

	state, err := service.ResolveDayFeedback(context.Background(), &models.User{ID: 10}, mustParseDayFeedbackDate(t, "2026-03-01"), mustParseDayFeedbackDate(t, "2026-03-01"), time.UTC)
	if err != nil {
		t.Fatalf("ResolveDayFeedback() unexpected error: %v", err)
	}
	if state.MessageKey != daySaveMessageSelfCare {
		t.Fatalf("expected self-care message on the first cycle day, got %q", state.MessageKey)
	}
}

func TestResolveDayFeedbackUsesSelfCareMessageOnThirdCycleDay(t *testing.T) {
	logs := newDayLogRepositoryStub()
	users := &dayUserRepositoryStub{}
	service := NewDayService(logs, users)

	// 2026-03-01 is the last period start; 2026-03-03 is cycleDay == 3,
	// the upper self-care boundary and well before the fertility window.
	logs.entries["2026-02-01"] = models.DailyLog{UserID: 10, Date: mustParseDayFeedbackDate(t, "2026-02-01"), IsPeriod: true}
	logs.entries["2026-03-01"] = models.DailyLog{UserID: 10, Date: mustParseDayFeedbackDate(t, "2026-03-01"), IsPeriod: true}

	state, err := service.ResolveDayFeedback(context.Background(), &models.User{ID: 10}, mustParseDayFeedbackDate(t, "2026-03-03"), mustParseDayFeedbackDate(t, "2026-03-03"), time.UTC)
	if err != nil {
		t.Fatalf("ResolveDayFeedback() unexpected error: %v", err)
	}
	if state.MessageKey != daySaveMessageSelfCare {
		t.Fatalf("expected self-care message on the third cycle day, got %q", state.MessageKey)
	}
}
