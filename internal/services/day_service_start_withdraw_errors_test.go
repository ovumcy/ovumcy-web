package services

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/ovumcy/ovumcy-web/internal/models"
)

// startClearingDayLogs is a day-log stub that also carries the transactional
// write of users.last_period_start, the shape the production repository has.
// clearErr makes that write fail; clearedDays records the day key it was asked
// to clear, so a test can tell which calendar day the service resolved.
type startClearingDayLogs struct {
	*dayLogRepositoryStub
	clearErr    error
	clearedDays []time.Time
}

func (stub *startClearingDayLogs) ClearLastPeriodStartOn(_ context.Context, _ uint, dayStart time.Time) error {
	stub.clearedDays = append(stub.clearedDays, dayStart)
	return stub.clearErr
}

// TestUnTickingAStoredStartFailsWhenItsClearFails covers the save that turns
// the stored start's period day into a non-period day while the transactional
// clear of that start fails: the save must fail as a day-update failure and
// return no entry, never report a write whose start was left behind. The
// driver's own error stays inside the service.
func TestUnTickingAStoredStartFailsWhenItsClearFails(t *testing.T) {
	day := time.Date(2026, time.March, 10, 0, 0, 0, 0, time.UTC)
	logs := &startClearingDayLogs{dayLogRepositoryStub: newDayLogRepositoryStub(), clearErr: errors.New("driver: database is locked")}
	logs.entries["2026-03-10"] = models.DailyLog{UserID: 7, Date: day, IsPeriod: true, Flow: models.FlowMedium}
	users := &dayUserRepositoryStub{settings: models.User{LastPeriodStart: &day}}
	service := NewDayService(logs, users)

	entry, err := service.UpsertDayEntryWithAutoFillAt(context.Background(), 7, day, DayEntryInput{IsPeriod: false, Flow: models.FlowNone, Mood: 3}, day.Add(12*time.Hour), time.UTC)
	if !errors.Is(err, ErrDayEntryUpdateFailed) {
		t.Fatalf("expected ErrDayEntryUpdateFailed when the start clear fails, got %v", err)
	}
	if errors.Is(err, logs.clearErr) {
		t.Fatalf("the driver error must not leak out of the service, got %v", err)
	}
	if entry.ID != 0 || !entry.Date.IsZero() {
		t.Fatalf("a failed save must return no entry, got %#v", entry)
	}
	if len(logs.clearedDays) != 1 || !logs.clearedDays[0].Equal(day) {
		t.Fatalf("expected exactly one clear of %s, got %v", day, logs.clearedDays)
	}
}

// TestUnTickingAStoredStartIsRefusedWithoutATransactionalClear covers a
// repository that cannot clear the start inside the day write: un-ticking the
// period on the stored start's date is refused, because clearing it elsewhere
// would commit outside the transaction and keeping it would leave a boundary
// the owner just removed.
func TestUnTickingAStoredStartIsRefusedWithoutATransactionalClear(t *testing.T) {
	day := time.Date(2026, time.March, 10, 0, 0, 0, 0, time.UTC)
	logs := newDayLogRepositoryStub()
	logs.entries["2026-03-10"] = models.DailyLog{UserID: 7, Date: day, IsPeriod: true, Flow: models.FlowMedium}
	users := &dayUserRepositoryStub{settings: models.User{LastPeriodStart: &day}}
	service := NewDayService(logs, users)

	entry, err := service.UpsertDayEntryWithAutoFillAt(context.Background(), 7, day, DayEntryInput{IsPeriod: false, Flow: models.FlowNone, Mood: 3}, day.Add(12*time.Hour), time.UTC)
	if !errors.Is(err, errLastPeriodStartClearUnsupported) {
		t.Fatalf("expected the unsupported-clear refusal, got %v", err)
	}
	if entry.ID != 0 || !entry.Date.IsZero() {
		t.Fatalf("a refused save must return no entry, got %#v", entry)
	}
	users.assertUserRepositoryCallsTargetOwner(t, 7)
}

// TestWithdrawingTheStartWithoutAClearerFailsWhenSettingsCannotBeRead covers
// the fallback's own read: without the stored settings the service cannot
// tell whether there is a start to withdraw, so it fails as a load failure
// instead of guessing either way.
func TestWithdrawingTheStartWithoutAClearerFailsWhenSettingsCannotBeRead(t *testing.T) {
	day := time.Date(2026, time.March, 10, 0, 0, 0, 0, time.UTC)
	users := &dayUserRepositoryStub{loadErr: errors.New("settings read failed")}
	service := NewDayService(newDayLogRepositoryStub(), users)

	err := service.withdrawOnboardingStartOn(context.Background(), 7, day)
	if !errors.Is(err, ErrDayEntryLoadFailed) {
		t.Fatalf("expected ErrDayEntryLoadFailed, got %v", err)
	}
	if errors.Is(err, users.loadErr) {
		t.Fatalf("the repository error must not leak out of the service, got %v", err)
	}
	users.assertUserRepositoryCallsTargetOwner(t, 7)
}

// TestDeleteDayEntryWithoutALocationResolvesTheUTCCalendarDay pins the nil
// location default: the day deleted, and the start withdrawn, is the UTC
// calendar day of the instant, not the calendar day in the instant's own zone.
func TestDeleteDayEntryWithoutALocationResolvesTheUTCCalendarDay(t *testing.T) {
	// 23:30 on 10 March at UTC-5 is 04:30 on 11 March in UTC.
	instant := time.Date(2026, time.March, 10, 23, 30, 0, 0, time.FixedZone("UTC-5", -5*60*60))
	utcDay := time.Date(2026, time.March, 11, 0, 0, 0, 0, time.UTC)
	zoneDay := time.Date(2026, time.March, 10, 0, 0, 0, 0, time.UTC)

	logs := &startClearingDayLogs{dayLogRepositoryStub: newDayLogRepositoryStub()}
	logs.entries["2026-03-11"] = models.DailyLog{UserID: 7, Date: utcDay, IsPeriod: true, Flow: models.FlowMedium}
	logs.entries["2026-03-10"] = models.DailyLog{UserID: 7, Date: zoneDay, IsPeriod: true, Flow: models.FlowMedium}
	service := NewDayService(logs, &dayUserRepositoryStub{})

	if err := service.DeleteDayEntry(context.Background(), 7, instant, nil); err != nil {
		t.Fatalf("DeleteDayEntry() unexpected error: %v", err)
	}
	if _, ok := logs.entries["2026-03-11"]; ok {
		t.Fatal("expected the UTC calendar day to be deleted")
	}
	if _, ok := logs.entries["2026-03-10"]; !ok {
		t.Fatal("the calendar day in the instant's own zone must be left alone")
	}
	if len(logs.clearedDays) != 1 || !logs.clearedDays[0].Equal(utcDay) {
		t.Fatalf("expected the start withdrawal on %s, got %v", utcDay, logs.clearedDays)
	}
}
