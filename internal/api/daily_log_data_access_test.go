package api

import (
	"testing"
	"time"

	"github.com/terraincognita07/ovumcy/internal/models"
)

func TestDayHasDataForDate(t *testing.T) {
	t.Parallel()

	handler, database := newDataAccessTestHandler(t)
	user := createDataAccessTestUser(t, database, "day-has-data@example.com")

	day := time.Date(2026, time.February, 18, 0, 0, 0, 0, time.UTC)

	hasData, err := dayHasDataForDateForTest(handler, user.ID, day)
	if err != nil {
		t.Fatalf("dayHasDataForDate returned error: %v", err)
	}
	if hasData {
		t.Fatal("expected false when no entries exist")
	}

	entry := models.DailyLog{
		UserID:   user.ID,
		Date:     day,
		IsPeriod: false,
		Flow:     models.FlowNone,
		Notes:    "note",
	}
	if err := database.Create(&entry).Error; err != nil {
		t.Fatalf("create log: %v", err)
	}

	hasData, err = dayHasDataForDateForTest(handler, user.ID, day)
	if err != nil {
		t.Fatalf("dayHasDataForDate returned error: %v", err)
	}
	if !hasData {
		t.Fatal("expected true when notes exist for the day")
	}
}

func TestRefreshUserLastPeriodStart(t *testing.T) {
	t.Parallel()

	handler, database := newDataAccessTestHandler(t)
	user := createDataAccessTestUser(t, database, "refresh-last-period@example.com")

	first := time.Date(2026, time.January, 10, 0, 0, 0, 0, time.UTC)
	second := time.Date(2026, time.February, 10, 0, 0, 0, 0, time.UTC)
	logs := []models.DailyLog{
		{UserID: user.ID, Date: first, IsPeriod: true, Flow: models.FlowMedium, SymptomIDs: []uint{}},
		{UserID: user.ID, Date: second, IsPeriod: true, Flow: models.FlowMedium, SymptomIDs: []uint{}},
	}
	if err := database.Create(&logs).Error; err != nil {
		t.Fatalf("create period logs: %v", err)
	}

	handler.ensureDependencies()
	if err := handler.dayService.RefreshUserLastPeriodStart(user.ID, handler.location); err != nil {
		t.Fatalf("RefreshUserLastPeriodStart returned error: %v", err)
	}

	updated := models.User{}
	if err := database.First(&updated, user.ID).Error; err != nil {
		t.Fatalf("load updated user: %v", err)
	}
	if updated.LastPeriodStart == nil {
		t.Fatal("expected last_period_start to be populated")
	}
	if updated.LastPeriodStart.Format("2006-01-02") != second.Format("2006-01-02") {
		t.Fatalf("expected latest period start %s, got %s", second.Format("2006-01-02"), updated.LastPeriodStart.Format("2006-01-02"))
	}

	if err := database.Where("user_id = ?", user.ID).Delete(&models.DailyLog{}).Error; err != nil {
		t.Fatalf("delete logs: %v", err)
	}
	if err := handler.dayService.RefreshUserLastPeriodStart(user.ID, handler.location); err != nil {
		t.Fatalf("RefreshUserLastPeriodStart second call returned error: %v", err)
	}

	updated = models.User{}
	if err := database.First(&updated, user.ID).Error; err != nil {
		t.Fatalf("reload user: %v", err)
	}
	if updated.LastPeriodStart != nil {
		t.Fatalf("expected last_period_start to be cleared, got %v", updated.LastPeriodStart)
	}
}
