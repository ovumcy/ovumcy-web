package services

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/ovumcy/ovumcy-web/internal/models"
)

func TestPeriodOffEffectMessageKeyNamesTheQuestionItAsks(t *testing.T) {
	cases := []struct {
		effect PeriodOffEffect
		want   string
	}{
		{PeriodOffEffect{}, ""},
		{PeriodOffEffect{MovesCycleStart: true}, "dashboard.period_off_confirm.moves_start"},
		{PeriodOffEffect{ClearedDays: 2}, "dashboard.period_off_confirm.clears"},
		{PeriodOffEffect{ClearedDays: 2, MovesCycleStart: true}, "dashboard.period_off_confirm.moves_start_and_clears"},
	}
	for _, tc := range cases {
		if got := tc.effect.MessageKey(); got != tc.want {
			t.Fatalf("%+v: key = %q, want %q", tc.effect, got, tc.want)
		}
	}
}

// ResolvePeriodOff answers for a save whose feedback could not be read, over
// the entry as saved: the effect ResolveDayFeedback would have carried.
func TestResolvePeriodOffMatchesTheFeedbackAndRefusesWhatItCannotRead(t *testing.T) {
	logs := newDayLogRepositoryStub()
	service := NewDayService(logs, &dayUserRepositoryStub{})
	user := &models.User{ID: 10, Role: models.RoleOwner}
	first := mustParseDayFeedbackDate(t, "2026-03-01")
	logs.entries["2026-03-01"] = models.DailyLog{UserID: 10, Date: first, IsPeriod: true, CycleStart: true}
	ctx := context.Background()

	state, err := service.ResolveDayFeedback(ctx, user, first, first, nil)
	if err != nil {
		t.Fatalf("ResolveDayFeedback: %v", err)
	}
	effect, err := service.ResolvePeriodOff(ctx, user, logs.entries["2026-03-01"], first, first, nil)
	if err != nil || effect != state.PeriodOff {
		t.Fatalf("ResolvePeriodOff = %+v, %v; want the feedback's %+v", effect, err, state.PeriodOff)
	}

	effect, err = service.ResolvePeriodOff(ctx, nil, models.DailyLog{IsPeriod: true}, first, first, time.UTC)
	if err != nil || effect.Any() {
		t.Fatalf("no user: %+v, %v; want no effect", effect, err)
	}
	var none *DayService
	effect, err = none.ResolvePeriodOff(ctx, user, models.DailyLog{IsPeriod: true}, first, first, time.UTC)
	if err != nil || effect.Any() {
		t.Fatalf("no service: %+v, %v; want no effect", effect, err)
	}

	logs.listErr = errors.New("db closed")
	if _, err := service.ResolvePeriodOff(ctx, user, logs.entries["2026-03-01"], first, first, time.UTC); err == nil {
		t.Fatal("an unreadable history produced an effect")
	}
}
