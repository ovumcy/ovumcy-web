package services

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
	"time"
)

// This file consumes the ADDITIVE "projection" section of the shared
// golden-vector fixture (testdata/cycle-prediction-golden-vectors.json). Where
// TestCyclePrediction_GoldenVectors (cycles_reference_test.go) pins the pure
// window math of PredictCycleWindow, this test pins the projection/anchor layer
// that production layers ON TOP of it:
//
//   - the median-first prediction-length selection (predictedCycleLength),
//   - ProjectCycleStart (current-cycle anchor + 1-based cycle day for `today`),
//   - the displayed next-period derivation (projected start + selected length),
//   - the ShiftCycleStartToFutureOvulation forward roll for the ovulation date.
//
// The section is a NEW top-level key ("projection"), so ovumcy-app's existing
// reference test — which decodes only "vectors" — keeps passing against a
// byte-identical copy until its twin consumer lands. The two implementations
// (cycles.go here, cycle-prediction-policy.ts there) are hand-parallel ports;
// consuming one shared fixture makes any mean-vs-median or DST-drift divergence
// fail CI on both sides. If the projection math changes, update the fixture,
// docs/cycle-prediction.md, and BOTH reference tests in the same change.

// goldenProjectionSection decodes just the additive "projection" key of the
// shared fixture; the base "vectors" key is intentionally ignored here.
type goldenProjectionSection struct {
	Projection projectionFixture `json:"projection"`
}

type projectionFixture struct {
	Comment       string             `json:"$comment"`
	SchemaVersion int                `json:"schemaVersion"`
	Vectors       []projectionVector `json:"vectors"`
}

type projectionVector struct {
	Name  string `json:"name"`
	Input struct {
		CycleLengths    []int  `json:"cycleLengths"`
		LastPeriodStart string `json:"lastPeriodStart"`
		Today           string `json:"today"`
		Timezone        string `json:"timezone"`
		LutealPhase     int    `json:"lutealPhase"`
	} `json:"input"`
	Expected struct {
		PredictionLength         int    `json:"predictionLength"`
		ProjectedCycleStart      string `json:"projectedCycleStart"`
		ProjectedCycleDay        int    `json:"projectedCycleDay"`
		DisplayedNextPeriodStart string `json:"displayedNextPeriodStart"`
		OvulationDate            string `json:"ovulationDate"`
	} `json:"expected"`
}

func loadProjectionVectors(t *testing.T) projectionFixture {
	t.Helper()
	raw, err := os.ReadFile(filepath.Join("testdata", goldenVectorsFile))
	if err != nil {
		t.Fatalf("read golden vectors: %v", err)
	}
	var section goldenProjectionSection
	if err := json.Unmarshal(raw, &section); err != nil {
		t.Fatalf("parse projection vectors: %v", err)
	}
	if len(section.Projection.Vectors) == 0 {
		t.Fatal("projection section has no vectors")
	}
	return section.Projection
}

// projectionLocation resolves the fixture IANA timezone. "UTC" (and empty) map
// to time.UTC without a tz-database lookup; anything else is loaded, and the
// test is skipped rather than failed when the tz database is unavailable (the
// same policy the baseline coverage tests use for the DST cases).
func projectionLocation(t *testing.T, name string) *time.Location {
	t.Helper()
	if name == "" || name == "UTC" {
		return time.UTC
	}
	loc, err := time.LoadLocation(name)
	if err != nil {
		t.Skipf("tz database unavailable for %q: %v", name, err)
	}
	return loc
}

// projectionDate parses a fixture "YYYY-MM-DD" as midnight in loc, matching how
// stored date-only anchors and the request "today" carry a location wall clock
// (ProjectCycleStart / ShiftCycleStartToFutureOvulation re-anchor via
// today.Location()).
func projectionDate(t *testing.T, s string, loc *time.Location) time.Time {
	t.Helper()
	d, err := time.ParseInLocation("2006-01-02", s, loc)
	if err != nil {
		t.Fatalf("parse projection date %q: %v", s, err)
	}
	return d
}

// TestCycleProjection_GoldenVectors replays, per vector, the exact production
// projection sequence (DashboardUpcomingPredictions) and asserts each stage
// against the shared fixture, so a mean-vs-median regression or a DST/boundary
// drift in either port fails CI on both sides.
func TestCycleProjection_GoldenVectors(t *testing.T) {
	fixture := loadProjectionVectors(t)

	for _, vector := range fixture.Vectors {
		t.Run(vector.Name, func(t *testing.T) {
			loc := projectionLocation(t, vector.Input.Timezone)
			lastPeriodStart := projectionDate(t, vector.Input.LastPeriodStart, loc)
			today := projectionDate(t, vector.Input.Today, loc)

			// 1. Prediction length is median-first (predictedCycleLength),
			// computed over the recent-cycle window exactly as production does.
			// This is the mean-vs-median pin: [28,28,28,28,60] must pick the
			// median (28), never the mean (~34).
			recent := tailInts(vector.Input.CycleLengths, cyclePredictionWindow)
			predictionLength := predictedCycleLength(medianInt(recent), averageInts(recent))
			if predictionLength != vector.Expected.PredictionLength {
				t.Fatalf("prediction length = %d, want %d (median-first over %v)",
					predictionLength, vector.Expected.PredictionLength, recent)
			}

			// 2. Projected current-cycle anchor + 1-based cycle day for `today`.
			// The DST case exercises the calendar-day rollover that a truncating
			// Sub().Hours()/24 would get wrong across the 23-hour spring-forward day.
			cycleStart, cycleDay, ok := ProjectCycleStart(lastPeriodStart, predictionLength, today)
			if !ok {
				t.Fatalf("ProjectCycleStart returned ok=false for input %+v", vector.Input)
			}
			assertProjectionDay(t, "projected cycle start", cycleStart, vector.Expected.ProjectedCycleStart)
			if cycleDay != vector.Expected.ProjectedCycleDay {
				t.Errorf("projected cycle day = %d, want %d", cycleDay, vector.Expected.ProjectedCycleDay)
			}

			// 3. Displayed next-period date: the UN-shifted projected start plus
			// the selected length, re-anchored in the request location. Mirrors
			// DashboardUpcomingPredictions, which derives the displayed
			// next-period before the ovulation forward roll.
			displayedNext := CalendarDay(cycleStart.AddDate(0, 0, predictionLength), today.Location())
			assertProjectionDay(t, "displayed next-period start", displayedNext, vector.Expected.DisplayedNextPeriodStart)

			// 4. Ovulation date: the window for the projected cycle, rolled
			// forward once if its ovulation already fell before `today` (the
			// exact DashboardUpcomingPredictions sequence). The roll is applied to
			// a SEPARATE anchor so it cannot disturb the displayed next-period
			// from step 3.
			window := PredictCycleWindow(cycleStart, predictionLength, vector.Input.LutealPhase)
			if window.Calculable && window.OvulationDate.Before(today) {
				shifted := ShiftCycleStartToFutureOvulation(cycleStart, window.OvulationDate, predictionLength, today)
				window = PredictCycleWindow(shifted, predictionLength, vector.Input.LutealPhase)
			}
			if !window.Calculable {
				t.Fatalf("projected ovulation window not calculable for input %+v", vector.Input)
			}
			assertProjectionDay(t, "ovulation date", window.OvulationDate, vector.Expected.OvulationDate)
		})
	}
}

func assertProjectionDay(t *testing.T, label string, got time.Time, want string) {
	t.Helper()
	if gotStr := got.Format("2006-01-02"); gotStr != want {
		t.Errorf("%s = %s, want %s", label, gotStr, want)
	}
}
