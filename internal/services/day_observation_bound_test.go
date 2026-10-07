package services

import (
	"context"
	"errors"
	"reflect"
	"testing"
	"time"

	"github.com/ovumcy/ovumcy-web/internal/models"
)

// WEB-246: a period and a pregnancy-test result are observations, so a day
// write that records one is held to the bound a cycle start is — today plus
// manualCycleStartFutureDays in the owner's zone — and refused past it: a period
// with the cycle-start refusal, a test result alone with the pregnancy-test
// refusal. A write that records neither goes through, and an entry
// already stored past the bound stays as stored.

// observationBoundClock is late evening eight hours behind UTC: the owner's
// today is 6 October while UTC is already on the 7th, so a bound read in UTC
// would accept 9 October and only the owner's zone refuses it.
func observationBoundClock() (time.Time, *time.Location) {
	location := time.FixedZone("UTC-8", -8*60*60)
	return time.Date(2026, time.October, 6, 22, 30, 0, 0, location), location
}

func observationDay(location *time.Location, day int) time.Time {
	return StartOfCalendarDay(2026, time.October, day, location)
}

// fullDay is a full write's payload: every enum the write states and the test
// leaves out is stated as "none", as the day form states it.
func fullDay(input DayEntryInput) DayEntryInput {
	if input.Flow == "" {
		input.Flow = models.FlowNone
	}
	if input.SexActivity == "" {
		input.SexActivity = models.SexActivityNone
	}
	if input.CervicalMucus == "" {
		input.CervicalMucus = models.CervicalMucusNone
	}
	if input.PregnancyTest == "" {
		input.PregnancyTest = models.PregnancyTestNone
	}
	return input
}

type observationWrite struct {
	name  string
	write func(service *DayService, day time.Time, now time.Time, location *time.Location) error
	// recordsPeriod selects the refusal past the bound: a write turning the
	// period on is refused as a cycle start, one recording only a test result
	// with the pregnancy-test refusal.
	recordsPeriod bool
}

func observationWrites() []observationWrite {
	return []observationWrite{
		{name: "full write records a period", recordsPeriod: true, write: func(service *DayService, day time.Time, now time.Time, location *time.Location) error {
			_, err := service.UpsertDayEntryWithAutoFillAt(context.Background(), 10, day, fullDay(DayEntryInput{IsPeriod: true}), now, location)
			return err
		}},
		{name: "full write records a pregnancy test", write: func(service *DayService, day time.Time, now time.Time, location *time.Location) error {
			_, err := service.UpsertDayEntryWithAutoFillAt(context.Background(), 10, day, fullDay(DayEntryInput{PregnancyTest: models.PregnancyTestPositive}), now, location)
			return err
		}},
		{name: "full write records a period and a pregnancy test", recordsPeriod: true, write: func(service *DayService, day time.Time, now time.Time, location *time.Location) error {
			_, err := service.UpsertDayEntryWithAutoFillAt(context.Background(), 10, day, fullDay(DayEntryInput{IsPeriod: true, PregnancyTest: models.PregnancyTestPositive}), now, location)
			return err
		}},
		{name: "partial write records a period", recordsPeriod: true, write: func(service *DayService, day time.Time, now time.Time, location *time.Location) error {
			_, err := service.PatchDayEntryWithAutoFillAt(context.Background(), 10, day, DayEntryInput{IsPeriod: true}, DayEntryFields{IsPeriod: true}, now, location)
			return err
		}},
		{name: "partial write records a pregnancy test", write: func(service *DayService, day time.Time, now time.Time, location *time.Location) error {
			_, err := service.PatchDayEntryWithAutoFillAt(context.Background(), 10, day, DayEntryInput{PregnancyTest: models.PregnancyTestNegative}, DayEntryFields{PregnancyTest: true}, now, location)
			return err
		}},
	}
}

// assertObservationRefusal pins which refusal a write past the bound carries.
// A period is the cycle-start refusal (ErrManualCycleStartDateInvalid, whose
// copy names the cycle start); a test result alone is the pregnancy-test
// refusal, which must not be mistaken for the cycle-start one — the transport
// maps by errors.Is, so a wrapped cycle-start sentinel would show its copy.
func assertObservationRefusal(t *testing.T, err error, recordsPeriod bool) {
	t.Helper()
	if recordsPeriod {
		if !errors.Is(err, ErrDayPeriodDateInvalid) || !errors.Is(err, ErrManualCycleStartDateInvalid) || errors.Is(err, ErrDayPregnancyTestDateInvalid) {
			t.Fatalf("a period past the bound must be refused with the cycle-start refusal, got %v", err)
		}
		return
	}
	if !errors.Is(err, ErrDayPregnancyTestDateInvalid) || errors.Is(err, ErrManualCycleStartDateInvalid) {
		t.Fatalf("a pregnancy test past the bound must be refused with its own refusal, not the cycle-start one, got %v", err)
	}
}

func TestDayWriteRecordingAnObservationIsHeldToTheCycleStartBound(t *testing.T) {
	now, location := observationBoundClock()
	for _, write := range observationWrites() {
		t.Run(write.name+", today+2 accepted", func(t *testing.T) {
			logs := newDayLogRepositoryStub()
			service := NewDayService(logs, &dayUserRepositoryStub{})
			day := observationDay(location, 8)
			if err := write.write(service, day, now, location); err != nil {
				t.Fatalf("a write on today+2 must be accepted, as a cycle start there is: %v", err)
			}
			if _, ok := logs.entries["2026-10-08"]; !ok {
				t.Fatal("the accepted write stored no entry on 2026-10-08")
			}
		})
		t.Run(write.name+", today+3 refused", func(t *testing.T) {
			logs := newDayLogRepositoryStub()
			service := NewDayService(logs, &dayUserRepositoryStub{})
			day := observationDay(location, 9)
			assertObservationRefusal(t, write.write(service, day, now, location), write.recordsPeriod)
			if len(logs.entries) != 0 {
				t.Fatalf("the refused write stored %v", logs.entries)
			}
		})
	}
}

func seedStoredFutureDay(logs *dayLogRepositoryStub, isPeriod bool, test string) models.DailyLog {
	entry := models.DailyLog{
		ID:              1,
		UserID:          10,
		Date:            time.Date(2026, time.October, 16, 0, 0, 0, 0, time.UTC),
		IsPeriod:        isPeriod,
		Flow:            models.FlowNone,
		SexActivity:     models.SexActivityNone,
		CervicalMucus:   models.CervicalMucusNone,
		PregnancyTest:   test,
		CycleFactorKeys: []string{},
		SymptomIDs:      []uint{},
		Notes:           "stored ahead",
	}
	logs.entries["2026-10-16"] = entry
	logs.nextID = 2
	return entry
}

func TestDayWriteRecordingNoObservationPassesPastTheBound(t *testing.T) {
	now, location := observationBoundClock()
	day := observationDay(location, 16)
	ctx := context.Background()

	t.Run("full write without a period on a day with no row", func(t *testing.T) {
		logs := newDayLogRepositoryStub()
		service := NewDayService(logs, &dayUserRepositoryStub{})
		saved, err := service.UpsertDayEntryWithAutoFillAt(ctx, 10, day, fullDay(DayEntryInput{IsPeriod: false, Notes: "plan"}), now, location)
		if err != nil || saved.IsPeriod || saved.Notes != "plan" {
			t.Fatalf("is_period=false past the bound must go through, got %+v, %v", saved, err)
		}
	})
	t.Run("partial write not naming the period keeps a stored future period", func(t *testing.T) {
		logs := newDayLogRepositoryStub()
		seedStoredFutureDay(logs, true, models.PregnancyTestPositive)
		service := NewDayService(logs, &dayUserRepositoryStub{})
		saved, err := service.PatchDayEntryWithAutoFillAt(ctx, 10, day, DayEntryInput{Notes: "edited"}, DayEntryFields{Notes: true}, now, location)
		if err != nil || !saved.IsPeriod || saved.PregnancyTest != models.PregnancyTestPositive || saved.Notes != "edited" {
			t.Fatalf("a write naming neither field must go through and keep both, got %+v, %v", saved, err)
		}
	})
	t.Run("full write re-stating a stored future period and test", func(t *testing.T) {
		logs := newDayLogRepositoryStub()
		seedStoredFutureDay(logs, true, models.PregnancyTestPositive)
		service := NewDayService(logs, &dayUserRepositoryStub{})
		saved, err := service.UpsertDayEntryWithAutoFillAt(ctx, 10, day, fullDay(DayEntryInput{IsPeriod: true, PregnancyTest: models.PregnancyTestPositive, Notes: "edited"}), now, location)
		if err != nil || !saved.IsPeriod || saved.Notes != "edited" {
			t.Fatalf("re-stating what is stored records nothing new and must go through, got %+v, %v", saved, err)
		}
	})
	t.Run("partial write clearing a stored future test", func(t *testing.T) {
		logs := newDayLogRepositoryStub()
		seedStoredFutureDay(logs, false, models.PregnancyTestPositive)
		service := NewDayService(logs, &dayUserRepositoryStub{})
		saved, err := service.PatchDayEntryWithAutoFillAt(ctx, 10, day, DayEntryInput{PregnancyTest: models.PregnancyTestNone}, DayEntryFields{PregnancyTest: true}, now, location)
		if err != nil || saved.PregnancyTest != models.PregnancyTestNone {
			t.Fatalf("clearing a test past the bound must go through, got %+v, %v", saved, err)
		}
	})
	t.Run("full write clearing a stored future period", func(t *testing.T) {
		logs := newDayLogRepositoryStub()
		seedStoredFutureDay(logs, true, models.PregnancyTestNone)
		service := NewDayService(logs, &dayUserRepositoryStub{})
		saved, err := service.UpsertDayEntryWithAutoFillAt(ctx, 10, day, fullDay(DayEntryInput{IsPeriod: false}), now, location)
		if err != nil || saved.IsPeriod {
			t.Fatalf("clearing a period past the bound must go through, got %+v, %v", saved, err)
		}
	})
}

func TestRefusedObservationLeavesTheStoredFutureEntryUntouched(t *testing.T) {
	now, location := observationBoundClock()
	day := observationDay(location, 16)
	ctx := context.Background()

	cases := map[string]struct {
		isPeriod      bool
		test          string
		recordsPeriod bool
		write         func(service *DayService) error
	}{
		"full write turning the period on": {
			test:          models.PregnancyTestNone,
			recordsPeriod: true,
			write: func(service *DayService) error {
				_, err := service.UpsertDayEntryWithAutoFillAt(ctx, 10, day, fullDay(DayEntryInput{IsPeriod: true, Notes: "overwritten"}), now, location)
				return err
			},
		},
		"partial write changing the stored test result": {
			isPeriod: true,
			test:     models.PregnancyTestPositive,
			write: func(service *DayService) error {
				_, err := service.PatchDayEntryWithAutoFillAt(ctx, 10, day, DayEntryInput{PregnancyTest: models.PregnancyTestNegative}, DayEntryFields{PregnancyTest: true}, now, location)
				return err
			},
		},
	}
	for name, c := range cases {
		t.Run(name, func(t *testing.T) {
			logs := newDayLogRepositoryStub()
			stored := seedStoredFutureDay(logs, c.isPeriod, c.test)
			service := NewDayService(logs, &dayUserRepositoryStub{})
			assertObservationRefusal(t, c.write(service), c.recordsPeriod)
			if got := logs.entries["2026-10-16"]; !reflect.DeepEqual(got, stored) {
				t.Fatalf("the refused write changed the stored entry:\n got %+v\nwant %+v", got, stored)
			}
		})
	}
}
