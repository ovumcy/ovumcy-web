package db

import (
	"context"
	"testing"

	"github.com/ovumcy/ovumcy-web/internal/models"
)

// WEB-282: the repository writes that tick an existing row carry the mark of
// whoever ticked it — the onboarding fill's is the fill's, the start move's is
// the owner's — rather than keeping whatever mark the row had before.

func requireAutoFilled(t *testing.T, repo *UserRepository, dayID uint, want bool) {
	t.Helper()
	var reloaded models.DailyLog
	if err := repo.database.WithContext(context.Background()).First(&reloaded, dayID).Error; err != nil {
		t.Fatalf("reload day: %v", err)
	}
	if reloaded.AutoFilled == nil || *reloaded.AutoFilled != want {
		t.Fatalf("want AutoFilled=%t, got %v", want, reloaded.AutoFilled)
	}
}

func TestOnboardingFillMarksAnExistingDayItTicks(t *testing.T) {
	repo := openRegistrationRepositoryForTest(t)
	ownerID, startDay, dayID := seedOnboardingOwnerWithExistingDay(t, repo)

	if err := repo.CompleteOnboarding(context.Background(), ownerID, startDay, startDay, true, "", ""); err != nil {
		t.Fatalf("complete onboarding: %v", err)
	}

	requireAutoFilled(t, repo, dayID, true)
}

func TestStartMoveMarksTheDayItTicksAsTheOwners(t *testing.T) {
	database, owner := openStartMoveFailureDatabase(t, "start-move-provenance.db")
	day := startMoveFailureDay()
	filled := true
	entry := &models.DailyLog{UserID: owner, Date: day, Flow: models.FlowNone, AutoFilled: &filled}
	if err := NewDailyLogRepository(database).Create(context.Background(), entry); err != nil {
		t.Fatalf("create day log: %v", err)
	}

	if err := runStartMove(database, owner, models.PeriodStartMove{MarkDay: day}); err != nil {
		t.Fatalf("start move: %v", err)
	}

	requireAutoFilled(t, NewUserRepository(database), entry.ID, false)
}
