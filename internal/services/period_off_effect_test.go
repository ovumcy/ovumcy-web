package services

import (
	"slices"
	"testing"
	"time"

	"github.com/ovumcy/ovumcy-web/internal/db"
	"github.com/ovumcy/ovumcy-web/internal/models"
)

// The question the day forms ask before un-ticking a period day is only as
// honest as the dry run behind it, so every case here runs the dry run, then
// the real write on the same database, and holds the two together: the
// boundary set moved exactly when the dry run said so, and exactly the
// counted days lost their period. Each case also pins the expected effect, so
// a dry run and a write that both did nothing cannot pass as agreement.

var periodOffNow = time.Date(2026, time.October, 5, 12, 0, 0, 0, time.UTC)

func seedPeriodOffPeriod(t *testing.T, service *DayService, userID uint, first time.Time, days int) {
	t.Helper()
	for offset := range days {
		withdrawSave(t, service, userID, first.AddDate(0, 0, offset), DayEntryInput{IsPeriod: true, Flow: models.FlowMedium})
	}
}

func periodOffEffectAgainstTheWrite(t *testing.T, service *DayService, repositories *db.Repositories, userID uint, day time.Time, fromStoredStart bool) PeriodOffEffect {
	t.Helper()
	user, logs := withdrawReload(t, repositories, userID)
	predicted := periodOffEffectFor(&user, logs, day, periodOffNow, time.UTC)
	before := CycleBoundaries(logs, BoundaryContextFor(&user, periodOffNow))
	periodBefore := map[string]bool{}
	for _, entry := range logs {
		periodBefore[CalendarDayKey(entry.Date)] = entry.IsPeriod
	}

	withdrawSave(t, service, userID, day, DayEntryInput{IsPeriod: false, Flow: models.FlowNone, PeriodFromStoredStart: fromStoredStart})

	afterUser, afterLogs := withdrawReload(t, repositories, userID)
	after := CycleBoundaries(afterLogs, BoundaryContextFor(&afterUser, periodOffNow))
	cleared := 0
	for _, entry := range afterLogs {
		key := CalendarDayKey(entry.Date)
		if key != CalendarDayKey(day) && periodBefore[key] && !entry.IsPeriod {
			cleared++
		}
	}
	moved := !slices.EqualFunc(before, after, time.Time.Equal)
	if predicted.MovesCycleStart != moved || predicted.ClearedDays != cleared {
		t.Fatalf("dry run %+v, the write moved the boundaries=%t (%v -> %v) and cleared %d days", predicted, moved, before, after, cleared)
	}
	return predicted
}

func TestPeriodOffOnACycleStartDayMovesTheBoundaryWithAutoFillOff(t *testing.T) {
	service, repositories, userID := withdrawFixture(t, "period-off-start@example.com", false)
	seedPeriodOffPeriod(t, service, userID, startMoveDay(time.July, 13), 5)
	seedPeriodOffPeriod(t, service, userID, startMoveDay(time.August, 10), 5)

	effect := periodOffEffectAgainstTheWrite(t, service, repositories, userID, startMoveDay(time.August, 10), false)
	if effect != (PeriodOffEffect{MovesCycleStart: true}) {
		t.Fatalf("effect = %+v, want the boundary moved and no day cleared", effect)
	}
}

func TestPeriodOffOnAnAutoFilledStartCountsTheDaysTheWalkClears(t *testing.T) {
	service, repositories, userID := withdrawFixture(t, "period-off-fill@example.com", true)
	seedPeriodOffPeriod(t, service, userID, startMoveDay(time.July, 13), 5)
	// One save: the auto-fill writes 08-11..08-14 behind it.
	withdrawSave(t, service, userID, startMoveDay(time.August, 10), DayEntryInput{IsPeriod: true, Flow: models.FlowMedium})

	effect := periodOffEffectAgainstTheWrite(t, service, repositories, userID, startMoveDay(time.August, 10), false)
	if effect != (PeriodOffEffect{ClearedDays: 4, MovesCycleStart: true}) {
		t.Fatalf("effect = %+v, want four cleared days and the boundary gone", effect)
	}
}

func TestPeriodOffStopsCountingAtAHandEditedNeighbour(t *testing.T) {
	service, repositories, userID := withdrawFixture(t, "period-off-hand@example.com", true)
	seedPeriodOffPeriod(t, service, userID, startMoveDay(time.July, 13), 5)
	withdrawSave(t, service, userID, startMoveDay(time.August, 10), DayEntryInput{IsPeriod: true, Flow: models.FlowMedium})
	withdrawSave(t, service, userID, startMoveDay(time.August, 12), DayEntryInput{IsPeriod: true, Flow: models.FlowMedium, Mood: MinDayMood})

	effect := periodOffEffectAgainstTheWrite(t, service, repositories, userID, startMoveDay(time.August, 10), false)
	if effect != (PeriodOffEffect{ClearedDays: 1, MovesCycleStart: true}) {
		t.Fatalf("effect = %+v, want one cleared day before the hand-edited one", effect)
	}
}

func TestPeriodOffInsideAPeriodAsksNothing(t *testing.T) {
	service, repositories, userID := withdrawFixture(t, "period-off-mid@example.com", false)
	seedPeriodOffPeriod(t, service, userID, startMoveDay(time.July, 13), 5)
	seedPeriodOffPeriod(t, service, userID, startMoveDay(time.August, 10), 5)

	if effect := periodOffEffectAgainstTheWrite(t, service, repositories, userID, startMoveDay(time.August, 12), false); effect.Any() {
		t.Fatalf("effect = %+v, want none for a day inside a period", effect)
	}
}

func TestPeriodOffOnTheRowlessStoredStartWithdrawsTheBoundary(t *testing.T) {
	service, repositories, userID := withdrawFixture(t, "period-off-onboarding@example.com", false)

	effect := periodOffEffectAgainstTheWrite(t, service, repositories, userID, startMoveDay(time.September, 14), true)
	if effect != (PeriodOffEffect{MovesCycleStart: true}) {
		t.Fatalf("effect = %+v, want the withdrawn start counted as a moved boundary", effect)
	}
}

func TestPeriodOffOnADayShownUntickedHasNoEffect(t *testing.T) {
	user := &models.User{Role: models.RoleOwner, AutoPeriodFill: true, PeriodLength: 5}
	logs := []models.DailyLog{{Date: startMoveDay(time.August, 10), Mood: MinDayMood}}
	if effect := periodOffEffectFor(user, logs, startMoveDay(time.August, 10), periodOffNow, time.UTC); effect.Any() {
		t.Fatalf("effect = %+v, want none: there is no period to un-tick", effect)
	}
	if effect := periodOffEffectFor(user, nil, startMoveDay(time.August, 11), periodOffNow, time.UTC); effect.Any() {
		t.Fatalf("effect = %+v, want none for a row-less day off the stored start", effect)
	}
}

func TestPeriodOffWithoutAUserAsksNothingAndWithoutALocationReadsUTC(t *testing.T) {
	day := startMoveDay(time.August, 10)
	var logs []models.DailyLog
	for _, first := range []time.Time{startMoveDay(time.July, 13), day} {
		for offset := range 5 {
			logs = append(logs, models.DailyLog{Date: first.AddDate(0, 0, offset), IsPeriod: true, Flow: models.FlowMedium})
		}
	}
	if effect := periodOffEffectFor(nil, logs, day, periodOffNow, time.UTC); effect.Any() {
		t.Fatalf("effect = %+v, want none without a user", effect)
	}
	user := &models.User{Role: models.RoleOwner}
	inUTC := periodOffEffectFor(user, logs, day, periodOffNow, time.UTC)
	if !inUTC.MovesCycleStart {
		t.Fatalf("effect = %+v, want the only boundary withdrawn", inUTC)
	}
	if effect := periodOffEffectFor(user, logs, day, periodOffNow, nil); effect != inUTC {
		t.Fatalf("effect without a location = %+v, want the UTC one %+v", effect, inUTC)
	}
}
