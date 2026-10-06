package services

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/ovumcy/ovumcy-web/internal/models"
)

// startMoveSettingsRepo serves a stored owner row to SaveCycleSettings and
// records which write a save chose: the start-moving write (moveCalls, move) or
// the plain column write (the embedded stub's updates).
type startMoveSettingsRepo struct {
	*stubSettingsTrackingUserRepo
	stored    models.User
	loadErr   error
	moveCalls int
	move      models.PeriodStartMove
}

func (repo *startMoveSettingsRepo) LoadSettingsByID(context.Context, uint) (models.User, error) {
	return repo.stored, repo.loadErr
}

func (repo *startMoveSettingsRepo) UpdateCycleSettingsMovingPeriodStart(ctx context.Context, userID uint, updates map[string]any, move models.PeriodStartMove) error {
	repo.moveCalls++
	repo.move = move
	return repo.stubSettingsTrackingUserRepo.UpdateCycleSettingsMovingPeriodStart(ctx, userID, updates, move)
}

// settingsRepoWithoutStartMover exposes only SettingsUserRepository, so the
// start-moving write is absent from its method set.
type settingsRepoWithoutStartMover struct {
	SettingsUserRepository
}

type failingSettingsDayLogReader struct {
	err   error
	calls int
}

func (reader *failingSettingsDayLogReader) FetchAllLogsForUser(context.Context, uint) ([]models.DailyLog, error) {
	reader.calls++
	return nil, reader.err
}

func newStartMoveSettingsRepo(stored models.User) *startMoveSettingsRepo {
	return &startMoveSettingsRepo{stubSettingsTrackingUserRepo: &stubSettingsTrackingUserRepo{}, stored: stored}
}

func startOnlyUpdate(start time.Time) CycleSettingsUpdate {
	return CycleSettingsUpdate{LastPeriodStartSet: true, LastPeriodStart: &start}
}

func assertNoSettingsWrite(t *testing.T, repo *startMoveSettingsRepo) {
	t.Helper()
	if repo.moveCalls != 0 || repo.updates != nil {
		t.Fatalf("a failed save must write nothing, got %d move call(s) and updates %#v", repo.moveCalls, repo.updates)
	}
}

func TestSaveCycleSettingsStartFailsWhenTheStoredRowCannotBeRead(t *testing.T) {
	repo := newStartMoveSettingsRepo(models.User{})
	repo.loadErr = errors.New("settings read failed")
	service := NewSettingsService(repo)

	err := service.SaveCycleSettings(context.Background(), 42, startOnlyUpdate(time.Date(2026, time.February, 8, 0, 0, 0, 0, time.UTC)))
	if !errors.Is(err, repo.loadErr) {
		t.Fatalf("expected the stored-row read error, got %v", err)
	}
	assertNoSettingsWrite(t, repo)
}

// TestSaveCycleSettingsStartFailsWhenTheDayLogsCannotBeRead covers a move that
// needs the day logs to decide whether the old start's fill may be removed: a
// failed read fails the save rather than guessing either way.
func TestSaveCycleSettingsStartFailsWhenTheDayLogsCannotBeRead(t *testing.T) {
	oldStart := time.Date(2026, time.February, 10, 0, 0, 0, 0, time.UTC)
	repo := newStartMoveSettingsRepo(models.User{LastPeriodStart: &oldStart, AutoPeriodFill: true, PeriodLength: 5})
	reader := &failingSettingsDayLogReader{err: errors.New("day logs read failed")}
	service := NewSettingsService(repo)
	service.AttachDayLogReader(reader)

	err := service.SaveCycleSettings(context.Background(), 42, startOnlyUpdate(oldStart.AddDate(0, 0, -2)))
	if !errors.Is(err, reader.err) {
		t.Fatalf("expected the day-log read error, got %v", err)
	}
	if reader.calls != 1 {
		t.Fatalf("expected one day-log read, got %d", reader.calls)
	}
	assertNoSettingsWrite(t, repo)
}

// TestSaveCycleSettingsStartMoveIsRefusedWithoutTheMovingWrite pins that a
// repository unable to move the start with its days never gets a columns-only
// save: the start would move without its days.
func TestSaveCycleSettingsStartMoveIsRefusedWithoutTheMovingWrite(t *testing.T) {
	inner := newStartMoveSettingsRepo(models.User{AutoPeriodFill: true, PeriodLength: 5})
	service := NewSettingsService(settingsRepoWithoutStartMover{SettingsUserRepository: inner})

	err := service.SaveCycleSettings(context.Background(), 42, startOnlyUpdate(time.Date(2026, time.February, 8, 0, 0, 0, 0, time.UTC)))
	if !errors.Is(err, errPeriodStartMoveUnsupported) {
		t.Fatalf("expected errPeriodStartMoveUnsupported, got %v", err)
	}
	assertNoSettingsWrite(t, inner)
}

// TestSaveCycleSettingsSameStartWritesOnlyTheColumns covers a save that keeps
// the stored start's date: nothing moves, so the plain column write runs. The
// service has no day-log reader attached, which is also the reader-less path
// of the clearability check.
func TestSaveCycleSettingsSameStartWritesOnlyTheColumns(t *testing.T) {
	stored := time.Date(2026, time.February, 10, 9, 30, 0, 0, time.UTC)
	repo := newStartMoveSettingsRepo(models.User{LastPeriodStart: &stored, AutoPeriodFill: true, PeriodLength: 5})
	service := NewSettingsService(repo)

	sameDay := time.Date(2026, time.February, 10, 0, 0, 0, 0, time.UTC)
	if err := service.SaveCycleSettings(context.Background(), 42, startOnlyUpdate(sameDay)); err != nil {
		t.Fatalf("SaveCycleSettings() unexpected error: %v", err)
	}
	if repo.moveCalls != 0 {
		t.Fatalf("a save on the stored start's date must not move anything, got %d move call(s)", repo.moveCalls)
	}
	if got, ok := repo.updates["last_period_start"].(time.Time); !ok || !got.Equal(sameDay) {
		t.Fatalf("expected the column write of last_period_start=%s, got %#v", sameDay, repo.updates)
	}
}

// TestSaveCycleSettingsStartMoveWithoutADayLogReaderClearsNothing covers a move
// on a service with no day-log reader: whether the old start still opens the
// newest cycle cannot be established, so the old fill is left alone, while the
// new start is filled for the default period length when the stored one is not
// a valid onboarding length.
func TestSaveCycleSettingsStartMoveWithoutADayLogReaderClearsNothing(t *testing.T) {
	oldStart := time.Date(2026, time.February, 10, 0, 0, 0, 0, time.UTC)
	repo := newStartMoveSettingsRepo(models.User{LastPeriodStart: &oldStart, AutoPeriodFill: true, PeriodLength: 0})
	service := NewSettingsService(repo)

	newStart := oldStart.AddDate(0, 0, -2)
	if err := service.SaveCycleSettings(context.Background(), 42, startOnlyUpdate(newStart)); err != nil {
		t.Fatalf("SaveCycleSettings() unexpected error: %v", err)
	}
	if repo.moveCalls != 1 {
		t.Fatalf("expected one start-moving write, got %d", repo.moveCalls)
	}
	if repo.move.ClearRows != nil || !repo.move.ClearFrom.IsZero() || !repo.move.ClearTo.IsZero() {
		t.Fatalf("without a day-log reader the old fill must not be cleared, got %#v", repo.move)
	}
	if !repo.move.MarkDay.Equal(newStart) {
		t.Fatalf("expected the new start %s marked, got %s", newStart, repo.move.MarkDay)
	}
	if len(repo.move.FillDays) != models.DefaultPeriodLength {
		t.Fatalf("expected %d fill days for an invalid stored period length, got %d", models.DefaultPeriodLength, len(repo.move.FillDays))
	}
	for offset, day := range repo.move.FillDays {
		if want := newStart.AddDate(0, 0, offset); !day.Equal(want) {
			t.Fatalf("fill day %d = %s, want %s", offset, day, want)
		}
	}
}
