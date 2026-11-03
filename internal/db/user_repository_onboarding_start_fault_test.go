package db

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/ovumcy/ovumcy-web/internal/models"
	"gorm.io/gorm"
)

// The start-day write shares completion's transaction: a failed read of the
// start day comes back as the error and leaves the account not onboarded and
// the day unwritten.
func TestUserRepositoryCompleteOnboardingRollsBackWhenTheStartDayReadFails(t *testing.T) {
	repo := openRegistrationRepositoryForTest(t)
	ctx := context.Background()
	owner := &models.User{
		Email:        "onboarding-start-fault@example.com",
		PasswordHash: "hash",
		Role:         models.RoleOwner,
		CycleLength:  models.DefaultCycleLength,
		PeriodLength: models.DefaultPeriodLength,
		CreatedAt:    time.Now().UTC(),
	}
	if err := repo.Create(ctx, owner); err != nil {
		t.Fatalf("create owner: %v", err)
	}

	injected := errors.New("injected start-day read failure")
	armed := true
	if err := repo.database.Callback().Query().Before("gorm:query").Register("test:refuse_start_day_read", func(tx *gorm.DB) {
		if armed && tx.Statement.Table == "daily_logs" {
			armed = false
			_ = tx.AddError(injected)
		}
	}); err != nil {
		t.Fatalf("register callback: %v", err)
	}

	startDay := time.Date(2026, time.June, 1, 0, 0, 0, 0, time.UTC)
	err := repo.CompleteOnboarding(ctx, owner.ID, startDay, startDay, false, "", "")
	if !errors.Is(err, injected) {
		t.Fatalf("expected the failed start-day read back, got %v", err)
	}

	reloaded, err := repo.FindByID(ctx, owner.ID)
	if err != nil {
		t.Fatalf("reload owner: %v", err)
	}
	if reloaded.OnboardingCompleted {
		t.Fatal("a failed completion left the account onboarded")
	}
	var count int64
	if err := repo.database.Model(&models.DailyLog{}).Where("user_id = ?", owner.ID).Count(&count).Error; err != nil {
		t.Fatalf("count days: %v", err)
	}
	if count != 0 {
		t.Fatalf("a failed completion wrote %d day(s)", count)
	}
}
