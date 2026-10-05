package services

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/ovumcy/ovumcy-web/internal/models"
)

func TestUpsertDayEntryReportsRepositoryFailuresWithNoPriorState(t *testing.T) {
	day := time.Date(2026, time.March, 1, 0, 0, 0, 0, time.UTC)
	failure := errors.New("repository unavailable")

	testCases := []struct {
		name      string
		arrange   func(logs *dayLogRepositoryStub)
		seedEntry bool
		expected  error
	}{
		{
			name:     "the day cannot be read",
			arrange:  func(logs *dayLogRepositoryStub) { logs.findErrByDay["2026-03-01"] = failure },
			expected: ErrDayEntryLoadFailed,
		},
		{
			name:      "the existing day cannot be saved",
			arrange:   func(logs *dayLogRepositoryStub) { logs.saveErrByDay["2026-03-01"] = failure },
			seedEntry: true,
			expected:  ErrDayEntryUpdateFailed,
		},
		{
			name:     "the new day cannot be created",
			arrange:  func(logs *dayLogRepositoryStub) { logs.createErrByDay["2026-03-01"] = failure },
			expected: ErrDayEntryCreateFailed,
		},
	}

	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			logs := newDayLogRepositoryStub()
			if testCase.seedEntry {
				logs.entries["2026-03-01"] = models.DailyLog{
					ID: 1, UserID: 10, Date: day, IsPeriod: true, Flow: models.FlowHeavy,
				}
			}
			testCase.arrange(logs)
			service := NewDayService(logs, &dayUserRepositoryStub{})

			_, previous, err := service.UpsertDayEntry(context.Background(), 10, day,
				DayEntryInput{IsPeriod: true, Flow: models.FlowMedium}, day, time.UTC)
			if !errors.Is(err, testCase.expected) {
				t.Fatalf("expected %v when %s, got %v", testCase.expected, testCase.name, err)
			}
			if previous != (priorDayState{}) {
				t.Fatalf("expected no prior state on a failed write, got %+v", previous)
			}
		})
	}
}
