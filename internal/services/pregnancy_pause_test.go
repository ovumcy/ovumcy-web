package services

import (
	"testing"
	"time"

	"github.com/ovumcy/ovumcy-web/internal/models"
)

func ppDay(year int, month time.Month, day int) time.Time {
	return time.Date(year, month, day, 0, 0, 0, 0, time.UTC)
}

func TestResolvePregnancyPauseNoLogs(t *testing.T) {
	if _, paused := ResolvePregnancyPause(nil); paused {
		t.Fatal("expected no pause for empty logs")
	}
}

func TestResolvePregnancyPauseNoPositiveTest(t *testing.T) {
	logs := []models.DailyLog{
		{Date: ppDay(2026, time.March, 1), PregnancyTest: models.PregnancyTestNegative},
		{Date: ppDay(2026, time.March, 2), IsPeriod: true, CycleStart: true},
	}
	if _, paused := ResolvePregnancyPause(logs); paused {
		t.Fatal("expected no pause without a positive test")
	}
}

func TestResolvePregnancyPausePositiveWithoutLaterCycleStart(t *testing.T) {
	positive := ppDay(2026, time.March, 10)
	logs := []models.DailyLog{
		{Date: ppDay(2026, time.March, 1), IsPeriod: true, CycleStart: true},
		{Date: positive, PregnancyTest: models.PregnancyTestPositive},
	}
	date, paused := ResolvePregnancyPause(logs)
	if !paused {
		t.Fatal("expected pause when positive test has no later cycle start")
	}
	if !date.Equal(positive) {
		t.Fatalf("expected pause date %s, got %s", positive, date)
	}
}

func TestResolvePregnancyPauseLiftedByLaterCycleStart(t *testing.T) {
	logs := []models.DailyLog{
		{Date: ppDay(2026, time.March, 10), PregnancyTest: models.PregnancyTestPositive},
		{Date: ppDay(2026, time.April, 5), IsPeriod: true, CycleStart: true},
	}
	if _, paused := ResolvePregnancyPause(logs); paused {
		t.Fatal("expected no pause when a cycle start follows the positive test")
	}
}

func TestResolvePregnancyPausePositiveWinsSameDayTie(t *testing.T) {
	day := ppDay(2026, time.March, 10)
	logs := []models.DailyLog{
		{Date: day, IsPeriod: true, CycleStart: true, PregnancyTest: models.PregnancyTestPositive},
	}
	date, paused := ResolvePregnancyPause(logs)
	if !paused {
		t.Fatal("expected pause when cycle start and positive test share a day")
	}
	if !date.Equal(day) {
		t.Fatalf("expected pause date %s, got %s", day, date)
	}
}

func TestResolvePregnancyPauseUsesLatestPositive(t *testing.T) {
	latest := ppDay(2026, time.March, 20)
	logs := []models.DailyLog{
		{Date: ppDay(2026, time.March, 5), PregnancyTest: models.PregnancyTestPositive},
		{Date: latest, PregnancyTest: models.PregnancyTestPositive},
	}
	date, paused := ResolvePregnancyPause(logs)
	if !paused {
		t.Fatal("expected pause")
	}
	if !date.Equal(latest) {
		t.Fatalf("expected latest positive date %s, got %s", latest, date)
	}
}

func TestResolvePregnancyPauseIgnoresCycleStartWithoutPeriod(t *testing.T) {
	positive := ppDay(2026, time.March, 10)
	logs := []models.DailyLog{
		{Date: positive, PregnancyTest: models.PregnancyTestPositive},
		{Date: ppDay(2026, time.April, 1), IsPeriod: false, CycleStart: true},
	}
	if _, paused := ResolvePregnancyPause(logs); !paused {
		t.Fatal("expected pause: a cycle-start flag without a period day must not lift it")
	}
}
