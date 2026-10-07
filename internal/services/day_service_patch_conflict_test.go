package services

import (
	"context"
	"errors"
	"reflect"
	"slices"
	"testing"
	"time"

	"github.com/ovumcy/ovumcy-web/internal/models"
)

// A partial write carries, for each field it changes, the value it based the
// edit on. These pin the per-field rule: the write is refused only when a field
// it names was changed elsewhere to a third value; a field it does not name, or
// one already holding the written value, never refuses it.

// dayFieldConflictCase is, for one day field, a base that differs from the
// stored value seeded by seedPatchCycleStartDay and a written value that
// differs from both.
type dayFieldConflictCase struct {
	base    func(*DayEntryInput)
	written func(*DayEntryInput)
}

var dayFieldConflictCases = map[string]dayFieldConflictCase{
	"IsPeriod": {
		base:    func(in *DayEntryInput) { in.IsPeriod = false },
		written: func(in *DayEntryInput) { in.IsPeriod = false },
	},
	"Flow": {
		base:    func(in *DayEntryInput) { in.Flow = models.FlowLight },
		written: func(in *DayEntryInput) { in.Flow = models.FlowHeavy },
	},
	"Mood": {
		base:    func(in *DayEntryInput) { in.Mood = 4 },
		written: func(in *DayEntryInput) { in.Mood = 5 },
	},
	"SexActivity": {
		base:    func(in *DayEntryInput) { in.SexActivity = models.SexActivityNone },
		written: func(in *DayEntryInput) { in.SexActivity = models.SexActivityUnprotected },
	},
	"BBT": {
		base:    func(in *DayEntryInput) { in.BBT = new(36.7) },
		written: func(in *DayEntryInput) { in.BBT = new(36.9) },
	},
	"CervicalMucus": {
		base:    func(in *DayEntryInput) { in.CervicalMucus = models.CervicalMucusDry },
		written: func(in *DayEntryInput) { in.CervicalMucus = models.CervicalMucusMoist },
	},
	"PregnancyTest": {
		base:    func(in *DayEntryInput) { in.PregnancyTest = models.PregnancyTestNone },
		written: func(in *DayEntryInput) { in.PregnancyTest = models.PregnancyTestPositive },
	},
	"CycleFactorKeys": {
		base:    func(in *DayEntryInput) { in.CycleFactorKeys = []string{} },
		written: func(in *DayEntryInput) { in.CycleFactorKeys = []string{models.CycleFactorIllness} },
	},
	"Notes": {
		base:    func(in *DayEntryInput) { in.Notes = "" },
		written: func(in *DayEntryInput) { in.Notes = "mine" },
	},
	"SymptomIDs": {
		base:    func(in *DayEntryInput) { in.SymptomIDs = []uint{} },
		written: func(in *DayEntryInput) { in.SymptomIDs = []uint{5} },
	},
}

// dayEntryFieldNames enumerates the fields a partial write can name, from the
// type itself, so a field added later is held to the same rule.
func dayEntryFieldNames(t *testing.T) []string {
	t.Helper()
	fieldsType := reflect.TypeFor[DayEntryFields]()
	names := make([]string, 0, fieldsType.NumField())
	for index := range fieldsType.NumField() {
		names = append(names, fieldsType.Field(index).Name)
	}
	if !slices.Contains(names, "IsPeriod") {
		t.Fatalf("expected DayEntryFields to enumerate is_period, got %v", names)
	}
	return names
}

func namedDayEntryFields(t *testing.T, names ...string) DayEntryFields {
	t.Helper()
	var fields DayEntryFields
	value := reflect.ValueOf(&fields).Elem()
	for _, name := range names {
		value.FieldByName(name).SetBool(true)
	}
	return fields
}

func TestPatchDayEntryRefusesEachFieldChangedElsewhereSinceItsBase(t *testing.T) {
	for _, name := range dayEntryFieldNames(t) {
		t.Run(name, func(t *testing.T) {
			testCase, ok := dayFieldConflictCases[name]
			if !ok {
				t.Fatalf("DayEntryFields.%s has no conflict case: add one, the conflict rule must cover every field a write can name", name)
			}
			logs := newDayLogRepositoryStub()
			service := NewDayService(logs, &dayUserRepositoryStub{})
			day := time.Date(2026, time.September, 7, 0, 0, 0, 0, time.UTC)
			seedPatchCycleStartDay(t, logs, day)
			before := logs.entries[logs.dayKey(day)]

			fields := namedDayEntryFields(t, name)
			var written, base DayEntryInput
			testCase.written(&written)
			testCase.base(&base)

			_, err := service.PatchDayEntryWithAutoFillAt(context.Background(), 10, day,
				written, fields, DayEntryBase{Values: base, Fields: fields}, time.Now(), time.UTC)
			if !errors.Is(err, ErrDayEntryConflict) {
				t.Fatalf("expected ErrDayEntryConflict for %s changed elsewhere, got %v", name, err)
			}
			if after := logs.entries[logs.dayKey(day)]; !reflect.DeepEqual(after, before) {
				t.Fatalf("expected the refused write to leave the day as stored, got %+v", after)
			}
		})
	}
}

func TestPatchDayEntryAcceptsEachFieldWhoseBaseIsStillStored(t *testing.T) {
	for _, name := range dayEntryFieldNames(t) {
		t.Run(name, func(t *testing.T) {
			testCase, ok := dayFieldConflictCases[name]
			if !ok {
				t.Fatalf("DayEntryFields.%s has no conflict case", name)
			}
			logs := newDayLogRepositoryStub()
			service := NewDayService(logs, &dayUserRepositoryStub{})
			day := time.Date(2026, time.September, 7, 0, 0, 0, 0, time.UTC)
			seedPatchCycleStartDay(t, logs, day)

			fields := namedDayEntryFields(t, name)
			var written DayEntryInput
			testCase.written(&written)
			base := normalizedStoredDayEntry(logs.entries[logs.dayKey(day)])

			if _, err := service.PatchDayEntryWithAutoFillAt(context.Background(), 10, day,
				written, fields, DayEntryBase{Values: base, Fields: fields}, time.Now(), time.UTC); err != nil {
				t.Fatalf("expected a write based on the stored %s to save, got %v", name, err)
			}
		})
	}
}

// The stored value already equals the value being written: an idempotent
// re-send of a committed write, or two writers agreeing. Not a conflict, for
// any field.
func TestPatchDayEntryTreatsAStoredValueEqualToTheWrittenOneAsNoConflict(t *testing.T) {
	logs := newDayLogRepositoryStub()
	service := NewDayService(logs, &dayUserRepositoryStub{})
	day := time.Date(2026, time.September, 7, 0, 0, 0, 0, time.UTC)
	seedPatchCycleStartDay(t, logs, day)

	names := dayEntryFieldNames(t)
	fields := namedDayEntryFields(t, names...)
	var base DayEntryInput
	for _, name := range names {
		dayFieldConflictCases[name].base(&base)
	}
	written := normalizedStoredDayEntry(logs.entries[logs.dayKey(day)])

	if _, err := service.PatchDayEntryWithAutoFillAt(context.Background(), 10, day,
		written, fields, DayEntryBase{Values: base, Fields: fields}, time.Now(), time.UTC); err != nil {
		t.Fatalf("expected a write equal to the stored day to save whatever its base, got %v", err)
	}
}

// Another device changed the note; this write changes only the mood and says
// nothing about the note. The note is kept, silently.
func TestPatchDayEntryNeverConflictsOnAFieldItDoesNotName(t *testing.T) {
	logs := newDayLogRepositoryStub()
	service := NewDayService(logs, &dayUserRepositoryStub{})
	day := time.Date(2026, time.September, 7, 0, 0, 0, 0, time.UTC)
	seedPatchCycleStartDay(t, logs, day)

	base := DayEntryInput{Mood: 2, Notes: "what this tab saw"}
	saved, err := service.PatchDayEntryWithAutoFillAt(context.Background(), 10, day,
		DayEntryInput{Mood: 4}, DayEntryFields{Mood: true},
		DayEntryBase{Values: base, Fields: DayEntryFields{Mood: true, Notes: true}}, time.Now(), time.UTC)
	if err != nil {
		t.Fatalf("expected a write that does not name the changed note to save, got %v", err)
	}
	if saved.Mood != 4 || saved.Notes != "keep me" {
		t.Fatalf("expected mood written and the note changed elsewhere kept, got mood=%d notes=%q", saved.Mood, saved.Notes)
	}
}

// Turning the period off is a stated is_period=false, checked against its base
// like any other value: based on a period day still stored it saves and clears
// what follows from it; based on a day another device has since marked, it is
// refused rather than erasing that mark.
func TestPatchDayEntryChecksAnExplicitPeriodOffAgainstItsBase(t *testing.T) {
	day := time.Date(2026, time.September, 7, 0, 0, 0, 0, time.UTC)
	fields := DayEntryFields{IsPeriod: true}

	logs := newDayLogRepositoryStub()
	service := NewDayService(logs, &dayUserRepositoryStub{})
	seedPatchCycleStartDay(t, logs, day)
	saved, err := service.PatchDayEntryWithAutoFillAt(context.Background(), 10, day,
		DayEntryInput{IsPeriod: false}, fields,
		DayEntryBase{Values: DayEntryInput{IsPeriod: true}, Fields: fields}, time.Now(), time.UTC)
	if err != nil {
		t.Fatalf("expected period off based on the stored period day to save, got %v", err)
	}
	if saved.IsPeriod || saved.CycleStart || saved.Flow != models.FlowNone {
		t.Fatalf("expected the explicit is_period=false to clear the period, got is_period=%v cycle_start=%v flow=%q", saved.IsPeriod, saved.CycleStart, saved.Flow)
	}

	// The tab rendered the day without a period; another device marked one.
	// The tab's explicit false would erase it.
	staleLogs := newDayLogRepositoryStub()
	staleService := NewDayService(staleLogs, &dayUserRepositoryStub{})
	seedPatchCycleStartDay(t, staleLogs, day)
	_, err = staleService.PatchDayEntryWithAutoFillAt(context.Background(), 10, day,
		DayEntryInput{IsPeriod: false}, fields,
		DayEntryBase{Values: DayEntryInput{IsPeriod: false, Mood: 2}, Fields: fields}, time.Now(), time.UTC)
	if !errors.Is(err, ErrDayEntryConflict) {
		t.Fatalf("expected period off based on a day without a period to be refused once one is stored, got %v", err)
	}
	if stored := staleLogs.entries[staleLogs.dayKey(day)]; !stored.IsPeriod || !stored.CycleStart {
		t.Fatalf("expected the period marked elsewhere to stay, got is_period=%v cycle_start=%v", stored.IsPeriod, stored.CycleStart)
	}
}

// A day with no row is the empty day: a base describing it is current, and a
// base describing a value since deleted elsewhere is not.
func TestPatchDayEntryComparesTheBaseWithAnAbsentDayAsEmpty(t *testing.T) {
	day := time.Date(2026, time.September, 7, 0, 0, 0, 0, time.UTC)
	fields := DayEntryFields{Mood: true}

	logs := newDayLogRepositoryStub()
	service := NewDayService(logs, &dayUserRepositoryStub{})
	if _, err := service.PatchDayEntryWithAutoFillAt(context.Background(), 10, day,
		DayEntryInput{Mood: 4}, fields, DayEntryBase{Fields: fields}, time.Now(), time.UTC); err != nil {
		t.Fatalf("expected a write based on the empty day to create it, got %v", err)
	}

	gone := newDayLogRepositoryStub()
	goneService := NewDayService(gone, &dayUserRepositoryStub{})
	_, err := goneService.PatchDayEntryWithAutoFillAt(context.Background(), 10, day,
		DayEntryInput{Mood: 4}, fields, DayEntryBase{Values: DayEntryInput{Mood: 3}, Fields: fields}, time.Now(), time.UTC)
	if !errors.Is(err, ErrDayEntryConflict) {
		t.Fatalf("expected a write based on a mood deleted elsewhere to be refused, got %v", err)
	}
}

// A field written without a base keeps the unconditional partial write.
func TestPatchDayEntryWritesAFieldWithoutABaseUnconditionally(t *testing.T) {
	logs := newDayLogRepositoryStub()
	service := NewDayService(logs, &dayUserRepositoryStub{})
	day := time.Date(2026, time.September, 7, 0, 0, 0, 0, time.UTC)
	seedPatchCycleStartDay(t, logs, day)

	saved, err := service.PatchDayEntryWithAutoFillAt(context.Background(), 10, day,
		DayEntryInput{Notes: "mine"}, DayEntryFields{Notes: true},
		DayEntryBase{Values: DayEntryInput{Notes: "stale"}}, time.Now(), time.UTC)
	if err != nil || saved.Notes != "mine" {
		t.Fatalf("expected a field without a base to be written, got notes=%q err=%v", saved.Notes, err)
	}
}

// A base comes back from the page as rendered: two decimals in the account's
// unit. Read back, it must match the stored reading it shows in either unit,
// or every temperature edit on a Fahrenheit account would be refused.
func TestPatchDayEntryMatchesATemperatureBaseReadBackFromThePage(t *testing.T) {
	for _, unit := range []string{TemperatureUnitCelsius, TemperatureUnitFahrenheit} {
		for _, stored := range []float64{36.55, 36.5555, 36.6389, 37.0} {
			rendered := FormatDayBBTForInput(&stored, unit)
			base, err := ParseDayBBTRawWithUnit(rendered, unit)
			if err != nil {
				t.Fatalf("parse rendered %q: %v", rendered, err)
			}
			if !sameStoredDayBBT(&stored, base) {
				t.Fatalf("expected %v rendered as %q (%s) to read back as the same reading, got %v", stored, rendered, unit, *base)
			}
		}
	}
	if sameStoredDayBBT(new(36.55), new(36.56)) {
		t.Fatalf("expected a reading one display step apart to count as a change")
	}
	if sameStoredDayBBT(nil, new(36.55)) || !sameStoredDayBBT(nil, nil) {
		t.Fatalf("expected an unmeasured day to equal only an unmeasured day")
	}
}
