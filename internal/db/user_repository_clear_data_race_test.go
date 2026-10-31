package db

import (
	"context"
	"testing"
	"time"

	"github.com/ovumcy/ovumcy-web/internal/models"
)

// TestClearAllDataKeepsNoDayInsertedConcurrentlyOnPostgres: a day insert still
// open when clear-data starts holds only FOR KEY SHARE on the owner row, which
// the wipe's UPDATE does not wait for. When the deletes ran first they missed
// that row, the wipe committed around the open insert, and the day committed
// afterwards outlived the erasure. The wipe must wait for the insert and then
// delete what it left.
func TestClearAllDataKeepsNoDayInsertedConcurrentlyOnPostgres(t *testing.T) {
	database := openPostgresForMigrationBootstrapTest(t, startPostgresTestConfig(t))
	repos := NewRepositories(database)
	ctx := context.Background()

	user := &models.User{
		Email:            "clear-data-race@example.com",
		PasswordHash:     "hash",
		RecoveryCodeHash: "recovery",
		Role:             models.RoleOwner,
		CycleLength:      models.DefaultCycleLength,
		PeriodLength:     models.DefaultPeriodLength,
		CreatedAt:        time.Now().UTC(),
	}
	if err := repos.Users.Create(ctx, user); err != nil {
		t.Fatalf("create user: %v", err)
	}
	stored, err := repos.Users.FindByID(ctx, user.ID)
	if err != nil {
		t.Fatalf("reload user: %v", err)
	}

	insert := database.Begin()
	if insert.Error != nil {
		t.Fatalf("begin insert: %v", insert.Error)
	}
	committed := false
	t.Cleanup(func() {
		if !committed {
			insert.Rollback()
		}
	})
	if err := insert.Create(&models.DailyLog{
		UserID: user.ID, Date: time.Date(2026, time.March, 10, 0, 0, 0, 0, time.UTC), IsPeriod: true, CycleFactorKeys: []string{},
	}).Error; err != nil {
		t.Fatalf("insert day: %v", err)
	}

	wiped := make(chan error, 1)
	go func() {
		wiped <- repos.Users.ClearAllDataAndResetSettings(ctx, user.ID, stored.AuthSessionVersion)
	}()

	// Commit the insert once the wipe is waiting on a lock, or once it has
	// finished without waiting — the defect, which the count below reports.
	var wipeErr error
	finished := false
	deadline := time.Now().Add(15 * time.Second)
	for !finished && time.Now().Before(deadline) {
		select {
		case wipeErr = <-wiped:
			finished = true
		default:
		}
		if finished {
			break
		}
		var waiting int64
		if err := database.Raw(`SELECT count(*) FROM pg_stat_activity
			WHERE datname = current_database() AND wait_event_type = 'Lock' AND query ILIKE '%FOR UPDATE%'`).Scan(&waiting).Error; err != nil {
			t.Fatalf("read pg_stat_activity: %v", err)
		}
		if waiting > 0 {
			break
		}
		time.Sleep(10 * time.Millisecond)
	}
	if err := insert.Commit().Error; err != nil {
		t.Fatalf("commit insert: %v", err)
	}
	committed = true
	if !finished {
		select {
		case wipeErr = <-wiped:
		case <-time.After(15 * time.Second):
			t.Fatal("clear-data did not finish after the insert committed")
		}
	}
	if wipeErr != nil {
		t.Fatalf("clear-data: %v", wipeErr)
	}

	var days int64
	if err := database.Model(&models.DailyLog{}).Where("user_id = ?", user.ID).Count(&days).Error; err != nil {
		t.Fatalf("count days: %v", err)
	}
	if days != 0 {
		t.Fatalf("%d day(s) inserted while clear-data ran outlived the wipe", days)
	}
}
