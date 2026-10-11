package services

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/ovumcy/ovumcy-web/internal/models"
)

const topUpOwnerID uint = 10

func topUpDay(day int) time.Time {
	return time.Date(2026, time.February, day, 0, 0, 0, 0, time.UTC)
}

func topUpKey(day int) string {
	return topUpDay(day).Format("2006-01-02")
}

func newTopUpService(autoFill bool, periodLength int) (*DayService, *dayLogRepositoryStub, *dayUserRepositoryStub) {
	logs := newDayLogRepositoryStub()
	users := &dayUserRepositoryStub{settings: models.User{
		AutoPeriodFill: autoFill,
		PeriodLength:   periodLength,
		CycleLength:    28,
	}}
	return NewDayService(logs, users), logs, users
}

func seedTopUpRow(logs *dayLogRepositoryStub, day int, row models.DailyLog) {
	row.ID = logs.nextID
	logs.nextID++
	row.UserID = topUpOwnerID
	row.Date = topUpDay(day)
	logs.entries[topUpKey(day)] = row
}

func markTopUpPeriodDay(t *testing.T, service *DayService, day int, today int) {
	t.Helper()
	now := time.Date(2026, time.February, today, 12, 0, 0, 0, time.UTC)
	if _, err := service.UpsertDayEntryWithAutoFillAt(context.Background(), topUpOwnerID, topUpDay(day),
		DayEntryInput{IsPeriod: true, Flow: models.FlowMedium}, now, time.UTC); err != nil {
		t.Fatalf("UpsertDayEntryWithAutoFillAt(%d): %v", day, err)
	}
}

func requireTopUpPeriodRow(t *testing.T, logs *dayLogRepositoryStub, day int) {
	t.Helper()
	row, ok := logs.entries[topUpKey(day)]
	if !ok || !row.IsPeriod {
		t.Fatalf("expected %s to be a period day, got ok=%t row=%#v", topUpKey(day), ok, row)
	}
	if row.UserID != topUpOwnerID {
		t.Fatalf("expected %s to belong to owner %d, got %d", topUpKey(day), topUpOwnerID, row.UserID)
	}
	if row.Flow != models.FlowMedium {
		t.Fatalf("expected %s to carry the marked day's flow, got %q", topUpKey(day), row.Flow)
	}
}

func requireTopUpNoRow(t *testing.T, logs *dayLogRepositoryStub, days ...int) {
	t.Helper()
	for _, day := range days {
		if row, ok := logs.entries[topUpKey(day)]; ok {
			t.Fatalf("expected no row on %s, got %#v", topUpKey(day), row)
		}
	}
}

func TestTopUpFillsAGapWithNoRows(t *testing.T) {
	service, logs, users := newTopUpService(true, 5)
	seedTopUpRow(logs, 10, models.DailyLog{IsPeriod: true, Flow: models.FlowMedium})

	markTopUpPeriodDay(t, service, 13, 13)

	requireTopUpPeriodRow(t, logs, 11)
	requireTopUpPeriodRow(t, logs, 12)
	requireTopUpPeriodRow(t, logs, 13)
	users.assertUserRepositoryCallsTargetOwner(t, topUpOwnerID)
}

func TestTopUpFillsFromTheOnboardingStartThatHasNoRow(t *testing.T) {
	service, logs, users := newTopUpService(true, 5)
	start := topUpDay(10)
	users.settings.LastPeriodStart = &start

	markTopUpPeriodDay(t, service, 12, 12)

	requireTopUpPeriodRow(t, logs, 11)
	requireTopUpPeriodRow(t, logs, 12)
	requireTopUpNoRow(t, logs, 10)
}

func TestTopUpFillsOnlyBehindTheLastPeriodDayBeforeTheMarkedOne(t *testing.T) {
	service, logs, _ := newTopUpService(true, 7)
	seedTopUpRow(logs, 10, models.DailyLog{IsPeriod: true, Flow: models.FlowMedium})
	seedTopUpRow(logs, 12, models.DailyLog{IsPeriod: true, Flow: models.FlowMedium})

	markTopUpPeriodDay(t, service, 14, 14)

	// Only the 13th lies behind the 14th and after the last period day (the
	// 12th); the 11th has no row either but sits before that period day.
	requireTopUpPeriodRow(t, logs, 13)
	requireTopUpNoRow(t, logs, 11)
	if logs.saveCalls[topUpKey(10)] != 0 || logs.saveCalls[topUpKey(12)] != 0 {
		t.Fatalf("expected the days before the gap to stay untouched, saves: %v", logs.saveCalls)
	}
}

func TestTopUpLeavesAnUntickedDayInsideTheGapAlone(t *testing.T) {
	service, logs, _ := newTopUpService(true, 5)
	seedTopUpRow(logs, 10, models.DailyLog{IsPeriod: true, Flow: models.FlowMedium})
	seedTopUpRow(logs, 11, models.DailyLog{IsPeriod: false, Flow: models.FlowNone})
	seedTopUpRow(logs, 12, models.DailyLog{IsPeriod: false, Flow: models.FlowNone, Mood: 3, Notes: "kept"})

	markTopUpPeriodDay(t, service, 14, 14)

	for _, day := range []int{11, 12} {
		row := logs.entries[topUpKey(day)]
		if row.IsPeriod || row.Flow != models.FlowNone {
			t.Fatalf("expected the un-ticked row on %s to stay as saved, got %#v", topUpKey(day), row)
		}
		if logs.saveCalls[topUpKey(day)] != 0 {
			t.Fatalf("expected the un-ticked row on %s not to be written, saves: %v", topUpKey(day), logs.saveCalls)
		}
	}
	if logs.entries[topUpKey(12)].Notes != "kept" || logs.entries[topUpKey(12)].Mood != 3 {
		t.Fatalf("expected the notes-and-mood row to stay as saved, got %#v", logs.entries[topUpKey(12)])
	}
	requireTopUpPeriodRow(t, logs, 13)
	requireTopUpPeriodRow(t, logs, 14)
}

func TestTopUpNeverWritesPastTheOwnersLocalToday(t *testing.T) {
	service, logs, _ := newTopUpService(true, 5)
	seedTopUpRow(logs, 10, models.DailyLog{IsPeriod: true, Flow: models.FlowMedium})

	// The 14th is two days ahead of the owner's today (the 12th): the 11th and
	// 12th are reached days, the 13th is not.
	markTopUpPeriodDay(t, service, 14, 12)

	requireTopUpNoRow(t, logs, 11, 12, 13)
	for key, row := range logs.entries {
		if key > topUpKey(12) && key != topUpKey(14) {
			t.Fatalf("expected no row written ahead of today, got %s=%#v", key, row)
		}
	}
}

func TestTopUpFollowsTheOwnersLocalTodayNotUTC(t *testing.T) {
	logs := newDayLogRepositoryStub()
	users := &dayUserRepositoryStub{settings: models.User{AutoPeriodFill: true, PeriodLength: 5, CycleLength: 28}}
	service := NewDayService(logs, users)
	seedTopUpRow(logs, 10, models.DailyLog{IsPeriod: true, Flow: models.FlowMedium})

	// 20:00 UTC on the 12th is already the 13th in Tokyo: marking the 13th is
	// today there, so the gap behind it is reached.
	tokyo := time.FixedZone("Asia/Tokyo", 9*60*60)
	now := time.Date(2026, time.February, 12, 20, 0, 0, 0, time.UTC)
	if _, err := service.UpsertDayEntryWithAutoFillAt(context.Background(), topUpOwnerID, topUpDay(13),
		DayEntryInput{IsPeriod: true, Flow: models.FlowMedium}, now, tokyo); err != nil {
		t.Fatalf("UpsertDayEntryWithAutoFillAt: %v", err)
	}

	requireTopUpPeriodRow(t, logs, 11)
	requireTopUpPeriodRow(t, logs, 12)
}

func TestTopUpFillsNothingForADayBeyondTheConfiguredPeriodLength(t *testing.T) {
	cases := []struct {
		name         string
		periodLength int
		markedDay    int
		filled       []int
		empty        []int
	}{
		{name: "last day of the window fills", periodLength: 5, markedDay: 14, filled: []int{11, 12, 13}},
		{name: "first day past the window fills nothing", periodLength: 5, markedDay: 15, empty: []int{11, 12, 13, 14}},
		{name: "a shorter setting narrows the window", periodLength: 3, markedDay: 13, empty: []int{11, 12}},
		{name: "a shorter setting still fills inside it", periodLength: 3, markedDay: 12, filled: []int{11}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			service, logs, _ := newTopUpService(true, tc.periodLength)
			seedTopUpRow(logs, 10, models.DailyLog{IsPeriod: true, Flow: models.FlowMedium})

			markTopUpPeriodDay(t, service, tc.markedDay, tc.markedDay)

			for _, day := range tc.filled {
				requireTopUpPeriodRow(t, logs, day)
			}
			requireTopUpNoRow(t, logs, tc.empty...)
		})
	}
}

func TestTopUpFillsNothingWhenAutoFillIsOff(t *testing.T) {
	service, logs, _ := newTopUpService(false, 5)
	seedTopUpRow(logs, 10, models.DailyLog{IsPeriod: true, Flow: models.FlowMedium})

	markTopUpPeriodDay(t, service, 13, 13)

	requireTopUpNoRow(t, logs, 11, 12)
	requireTopUpPeriodRow(t, logs, 13)
}

func TestTopUpDoesNotReachIntoAnOlderPeriod(t *testing.T) {
	service, logs, _ := newTopUpService(true, 5)
	seedTopUpRow(logs, 1, models.DailyLog{IsPeriod: true, Flow: models.FlowMedium})
	// A later period is already running, so the first one is history.
	seedTopUpRow(logs, 20, models.DailyLog{IsPeriod: true, Flow: models.FlowMedium})

	markTopUpPeriodDay(t, service, 4, 20)

	requireTopUpNoRow(t, logs, 2, 3)
}

func TestTopUpDoesNotRefillAfterAReSaveOfAPeriodDay(t *testing.T) {
	service, logs, _ := newTopUpService(true, 5)
	seedTopUpRow(logs, 10, models.DailyLog{IsPeriod: true, Flow: models.FlowMedium})
	seedTopUpRow(logs, 13, models.DailyLog{IsPeriod: true, Flow: models.FlowMedium})

	// The 13th already was a period day; the owner removed the rows between and
	// saves it again with a changed flow.
	markTopUpPeriodDay(t, service, 13, 13)

	requireTopUpNoRow(t, logs, 11, 12)
}

func TestTopUpCapsTheForwardFillAtTheRunningPeriodsLastDay(t *testing.T) {
	// Four clear days before the 15th is outside the 3-day lookback, so the
	// ordinary fill also runs from the 15th. The 15th continues the period that
	// started on the 10th, whose seven days end on the 16th: the fill may not
	// carry it on to the 21st, and with today the 17th it may not pass the 16th.
	for _, today := range []int{17, 21} {
		service, logs, _ := newTopUpService(true, 7)
		seedTopUpRow(logs, 10, models.DailyLog{IsPeriod: true, Flow: models.FlowMedium})

		markTopUpPeriodDay(t, service, 15, today)

		for _, day := range []int{11, 12, 13, 14, 15, 16} {
			requireTopUpPeriodRow(t, logs, day)
		}
		requireTopUpNoRow(t, logs, 17, 18, 19, 20, 21)
	}
}

func TestTopUpLeavesTheForwardFillOfANewPeriodUnbounded(t *testing.T) {
	// The 20th is nine days after the last period day: a period of its own, not
	// a continuation, so it fills its seven days (through today, the 24th).
	service, logs, _ := newTopUpService(true, 7)
	seedTopUpRow(logs, 10, models.DailyLog{IsPeriod: true, Flow: models.FlowMedium})

	markTopUpPeriodDay(t, service, 20, 24)

	for _, day := range []int{20, 21, 22, 23, 24} {
		requireTopUpPeriodRow(t, logs, day)
	}
	requireTopUpNoRow(t, logs, 11, 19)
}

func TestTopUpIgnoresALongChainTheBoundedReadCutsShort(t *testing.T) {
	// A period logged every fourth day for a month reads as one episode that
	// started long ago. The history read starts only periodLength plus the cluster
	// gap before the marked day, and the first row inside it must not pass for the
	// episode's start.
	service, logs, _ := newTopUpService(true, 5)
	for _, day := range []int{-21, -17, -13, -9, -5, -1, 3, 7, 11} {
		seedTopUpRow(logs, day, models.DailyLog{IsPeriod: true, Flow: models.FlowMedium})
	}

	markTopUpPeriodDay(t, service, 14, 14)

	requireTopUpNoRow(t, logs, 12, 13)
}

func TestRunningPeriodGapDaysCountsFromTheEpisodesExplicitStart(t *testing.T) {
	logs := []models.DailyLog{
		{UserID: topUpOwnerID, Date: topUpDay(8), IsPeriod: true},
		{UserID: topUpOwnerID, Date: topUpDay(10), IsPeriod: true, CycleStart: true},
		{UserID: topUpOwnerID, Date: topUpDay(13), IsPeriod: true},
	}
	now := time.Date(2026, time.February, 13, 12, 0, 0, 0, time.UTC)

	got := runningPeriodWindowFor(&models.User{}, logs, topUpDay(13), 5, now, time.UTC)
	if !got.matched || len(got.gap) != 2 || !got.gap[0].Equal(topUpDay(11)) || !got.gap[1].Equal(topUpDay(12)) {
		t.Fatalf("expected the 11th and 12th counted from the explicit start on the 10th, got %+v", got)
	}
	if want := topUpDay(14); !got.lastDay.Equal(want) {
		t.Fatalf("expected the period to end on %s, got %s", want, got.lastDay)
	}

	// Without the explicit mark the episode starts on the 8th and the 13th is
	// five days in: outside a five-day window.
	logs[1].CycleStart = false
	if got := runningPeriodWindowFor(&models.User{}, logs, topUpDay(13), 5, now, time.UTC); got.matched || len(got.gap) != 0 {
		t.Fatalf("expected nothing past the window counted from the 8th, got %+v", got)
	}
}

func TestRunningPeriodGapDaysIgnoresAnEpisodeOfSpottingOnly(t *testing.T) {
	logs := []models.DailyLog{
		{UserID: topUpOwnerID, Date: topUpDay(10), IsPeriod: true, Flow: models.FlowSpotting},
		{UserID: topUpOwnerID, Date: topUpDay(13), IsPeriod: true, Flow: models.FlowSpotting},
	}
	now := time.Date(2026, time.February, 13, 12, 0, 0, 0, time.UTC)

	if got := runningPeriodWindowFor(&models.User{}, logs, topUpDay(13), 5, now, time.UTC); got.matched {
		t.Fatalf("expected spotting alone to open no window, got %+v", got)
	}
}

func TestRunningPeriodGapDaysAnswersNothingWithoutAnyPeriodDay(t *testing.T) {
	now := time.Date(2026, time.February, 13, 12, 0, 0, 0, time.UTC)
	if got := runningPeriodWindowFor(&models.User{}, nil, topUpDay(13), 5, now, time.UTC); got.matched {
		t.Fatalf("expected nothing without period days, got %+v", got)
	}
}

func TestTopUpSurfacesItsFailuresAsAutoFillErrors(t *testing.T) {
	cases := []struct {
		name    string
		arrange func(logs *dayLogRepositoryStub, users *dayUserRepositoryStub)
		want    error
	}{
		{
			name: "a failed row create",
			arrange: func(logs *dayLogRepositoryStub, _ *dayUserRepositoryStub) {
				logs.createErrByDay[topUpKey(12)] = context.DeadlineExceeded
			},
			want: ErrDayAutoFillApplyFailed,
		},
		{
			// The 11th lies outside the three-day lookback of the forward-fill
			// decision, so only the catch-up's own locking read reaches it.
			name: "a failed locking read of a gap day",
			arrange: func(logs *dayLogRepositoryStub, _ *dayUserRepositoryStub) {
				logs.findErrByDay[topUpKey(11)] = context.DeadlineExceeded
			},
			want: ErrDayAutoFillApplyFailed,
		},
		{
			name: "a failed history read",
			arrange: func(logs *dayLogRepositoryStub, _ *dayUserRepositoryStub) {
				logs.rangeErr = context.DeadlineExceeded
			},
			want: ErrDayAutoFillCheckFailed,
		},
		{
			name: "a failed settings read after the fill settings loaded",
			arrange: func(_ *dayLogRepositoryStub, users *dayUserRepositoryStub) {
				users.loadErr = context.DeadlineExceeded
				users.loadErrFromCall = 2
			},
			want: ErrDayAutoFillCheckFailed,
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			service, logs, users := newTopUpService(true, 7)
			seedTopUpRow(logs, 10, models.DailyLog{IsPeriod: true, Flow: models.FlowMedium})
			tc.arrange(logs, users)

			now := time.Date(2026, time.February, 15, 12, 0, 0, 0, time.UTC)
			_, err := service.UpsertDayEntryWithAutoFillAt(context.Background(), topUpOwnerID, topUpDay(15),
				DayEntryInput{IsPeriod: true, Flow: models.FlowMedium}, now, time.UTC)
			if !errors.Is(err, tc.want) {
				t.Fatalf("expected %v, got %v", tc.want, err)
			}
		})
	}
}

func TestTopUpReadsAMissingLocationAsUTC(t *testing.T) {
	service, logs, _ := newTopUpService(true, 5)
	seedTopUpRow(logs, 10, models.DailyLog{IsPeriod: true, Flow: models.FlowMedium})
	seedTopUpRow(logs, 13, models.DailyLog{IsPeriod: true, Flow: models.FlowMedium})

	now := time.Date(2026, time.February, 13, 12, 0, 0, 0, time.UTC)
	if err := service.autoFillNewPeriodAnchor(context.Background(), topUpOwnerID, topUpDay(13), false, true, 5, models.FlowMedium, now, nil); err != nil {
		t.Fatalf("autoFillNewPeriodAnchor: %v", err)
	}

	requireTopUpPeriodRow(t, logs, 11)
	requireTopUpPeriodRow(t, logs, 12)
}
