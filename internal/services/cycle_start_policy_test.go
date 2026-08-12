package services

import (
	"testing"
	"time"

	"github.com/ovumcy/ovumcy-web/internal/models"
)

func TestLatestCycleStartAnchorBeforeOrOnPrefersMoreRecentSettingsBaseline(t *testing.T) {
	userLastPeriod := mustParseCycleStartPolicyDay(t, "2026-03-13")
	user := &models.User{LastPeriodStart: &userLastPeriod}
	logs := []models.DailyLog{
		{Date: mustParseCycleStartPolicyDay(t, "2026-01-01"), IsPeriod: true, CycleStart: true},
		{Date: mustParseCycleStartPolicyDay(t, "2026-02-16"), IsPeriod: true, CycleStart: true},
	}

	anchor := LatestCycleStartAnchorBeforeOrOn(user, logs, mustParseCycleStartPolicyDay(t, "2026-03-14"), time.UTC)
	if got := anchor.Format("2006-01-02"); got != "2026-03-13" {
		t.Fatalf("expected latest anchor to use user baseline 2026-03-13, got %s", got)
	}
}

func TestShouldSuggestManualCycleStartUsesMostRecentKnownAnchor(t *testing.T) {
	userLastPeriod := mustParseCycleStartPolicyDay(t, "2026-03-13")
	user := &models.User{LastPeriodStart: &userLastPeriod}
	logs := []models.DailyLog{
		{Date: mustParseCycleStartPolicyDay(t, "2026-01-01"), IsPeriod: true, CycleStart: true},
		{Date: mustParseCycleStartPolicyDay(t, "2026-02-16"), IsPeriod: true, CycleStart: true},
	}
	logEntry := models.DailyLog{
		Date:     mustParseCycleStartPolicyDay(t, "2026-03-14"),
		IsPeriod: true,
	}
	now := mustParseCycleStartPolicyDay(t, "2026-03-14")

	if ShouldSuggestManualCycleStart(user, logs, logEntry, logEntry.Date, now, time.UTC) {
		t.Fatalf("expected no long-gap suggestion when a newer baseline already exists")
	}
}

// Period day and cycle start are one event, so the question the day form asks
// beside the period toggle cannot wait for IsPeriod to be persisted: on the
// first day of bleeding the stored entry is still empty while the form renders.
// The suggestion hint keeps its old, IsPeriod-gated meaning — asserted here as
// the positive anchor, so a policy that simply started returning true
// everywhere fails instead of passing.
func TestShouldAskCycleStartQuestionDoesNotWaitForTheStoredPeriodFlag(t *testing.T) {
	user := &models.User{}
	logs := []models.DailyLog{
		{Date: mustParseCycleStartPolicyDay(t, "2026-02-01"), IsPeriod: true, CycleStart: true},
	}
	day := mustParseCycleStartPolicyDay(t, "2026-03-01")
	unsavedEntry := models.DailyLog{}

	if !ShouldAskCycleStartQuestion(user, logs, unsavedEntry, day, day, time.UTC) {
		t.Fatal("expected the inline question on a day being marked as a period day 28 days after the anchor")
	}
	if ShouldSuggestManualCycleStart(user, logs, unsavedEntry, day, day, time.UTC) {
		t.Fatal("expected the manual-control hint to stay gated on the stored period flag")
	}

	savedPeriodEntry := models.DailyLog{Date: day, IsPeriod: true}
	if !ShouldAskCycleStartQuestion(user, logs, savedPeriodEntry, day, day, time.UTC) {
		t.Fatal("expected the inline question to survive on an already saved period day")
	}
}

// A nil location is UTC, as everywhere else in this policy — a caller that
// omits it must not get a different answer than one passing time.UTC.
func TestShouldAskCycleStartQuestionTreatsANilLocationAsUTC(t *testing.T) {
	user := &models.User{}
	logs := []models.DailyLog{
		{Date: mustParseCycleStartPolicyDay(t, "2026-02-01"), IsPeriod: true, CycleStart: true},
	}
	day := mustParseCycleStartPolicyDay(t, "2026-03-01")

	if !ShouldAskCycleStartQuestion(user, logs, models.DailyLog{}, day, day, nil) {
		t.Fatal("expected a nil location to resolve to UTC and keep the question")
	}
}

func TestShouldAskCycleStartQuestionStaysSilentOutsideTheSuggestionState(t *testing.T) {
	anchor := mustParseCycleStartPolicyDay(t, "2026-02-01")
	day := mustParseCycleStartPolicyDay(t, "2026-03-01")
	anchorLogs := []models.DailyLog{{Date: anchor, IsPeriod: true, CycleStart: true}}

	testCases := []struct {
		name     string
		user     *models.User
		logs     []models.DailyLog
		logEntry models.DailyLog
		day      time.Time
	}{
		{
			name: "no anchor to measure a gap against",
			user: &models.User{},
			logs: []models.DailyLog{{Date: day, IsPeriod: true}},
			day:  day,
		},
		{
			name:     "the day already is a cycle start",
			user:     &models.User{},
			logs:     anchorLogs,
			logEntry: models.DailyLog{Date: day, IsPeriod: true, CycleStart: true},
			day:      day,
		},
		{
			name: "the previous start is only a few days back",
			user: &models.User{},
			logs: []models.DailyLog{{Date: mustParseCycleStartPolicyDay(t, "2026-02-26"), IsPeriod: true, CycleStart: true}},
			day:  day,
		},
		{
			name: "a competing cycle start sits in the same period cluster",
			user: &models.User{},
			logs: []models.DailyLog{
				{Date: anchor, IsPeriod: true, CycleStart: true},
				{Date: mustParseCycleStartPolicyDay(t, "2026-03-02"), IsPeriod: true, CycleStart: true},
			},
			day: day,
		},
		{
			name: "the day is further ahead than a cycle start may be marked",
			user: &models.User{},
			logs: anchorLogs,
			day:  mustParseCycleStartPolicyDay(t, "2026-03-05"),
		},
	}

	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			if ShouldAskCycleStartQuestion(testCase.user, testCase.logs, testCase.logEntry, testCase.day, day, time.UTC) {
				t.Fatalf("expected no inline cycle-start question when %s", testCase.name)
			}
		})
	}
}

func mustParseCycleStartPolicyDay(t *testing.T, raw string) time.Time {
	t.Helper()
	parsed, err := time.ParseInLocation("2006-01-02", raw, time.UTC)
	if err != nil {
		t.Fatalf("parse day %q: %v", raw, err)
	}
	return parsed
}
