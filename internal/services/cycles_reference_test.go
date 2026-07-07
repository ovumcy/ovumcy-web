package services

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
	"time"
)

// Reference vectors for the cycle-prediction math. Every case here is also a row
// in docs/cycle-prediction.md — the worked examples and the code are asserted to
// agree, so the public documentation cannot silently drift from the behavior.
//
// These deterministic example-based tests complement the property tests
// (cycles_property_test.go, invariants) and the fuzz tests (policy_fuzz_test.go,
// robustness). Together: transparent and verifiable.
//
// The per-vector prediction cases (TestCyclePrediction_GoldenVectors) are driven
// by a shared golden-vector fixture,
// testdata/cycle-prediction-golden-vectors.json, that is kept byte-identical
// with ovumcy-app's src/services/__fixtures__/cycle-prediction-golden-vectors.json
// (ovumcy-app PR #75). The Go (cycles.go) and TypeScript
// (cycle-prediction-policy.ts) prediction implementations are hand-parallel
// ports; consuming one shared file makes any divergence between them fail CI on
// both sides instead of silently drifting. If the prediction math changes,
// update the fixture, docs/cycle-prediction.md, and BOTH reference tests
// (this file and ovumcy-app's cycle-prediction-reference.test.ts) in the same
// change.

// goldenVectorsFile is the shared golden-vector fixture, vendored byte-identical
// from ovumcy-app (see the package comment above).
const goldenVectorsFile = "cycle-prediction-golden-vectors.json"

// goldenVectorFixture is the parsed shape of the shared fixture. Field names
// mirror the JSON (which uses the TypeScript port's vocabulary); the mapping
// onto the Go CycleWindowPrediction struct happens in the test body. Nullable
// dates are pointers so a JSON null (the non-calculable case) decodes to nil
// rather than the zero date, and can be asserted as "absent".
type goldenVectorFixture struct {
	SchemaVersion int            `json:"schemaVersion"`
	Vectors       []goldenVector `json:"vectors"`
}

type goldenVector struct {
	Name  string `json:"name"`
	Input struct {
		CycleStartDate string `json:"cycleStartDate"`
		CycleLength    int    `json:"cycleLength"`
		LutealPhase    int    `json:"lutealPhase"`
	} `json:"input"`
	Expected struct {
		Calculable      bool    `json:"calculable"`
		FertilityStart  *string `json:"fertilityStart"`
		FertilityEnd    *string `json:"fertilityEnd"`
		OvulationDate   *string `json:"ovulationDate"`
		IsExact         bool    `json:"isExact"`
		NextPeriodStart string  `json:"nextPeriodStart"`
	} `json:"expected"`
}

// loadGoldenVectors reads and parses the shared fixture, failing the test on any
// read or decode error.
func loadGoldenVectors(t *testing.T) goldenVectorFixture {
	t.Helper()
	raw, err := os.ReadFile(filepath.Join("testdata", goldenVectorsFile))
	if err != nil {
		t.Fatalf("read golden vectors: %v", err)
	}
	var fixture goldenVectorFixture
	if err := json.Unmarshal(raw, &fixture); err != nil {
		t.Fatalf("parse golden vectors: %v", err)
	}
	if len(fixture.Vectors) == 0 {
		t.Fatal("golden vectors fixture has no vectors")
	}
	return fixture
}

// goldenDate parses a fixture "YYYY-MM-DD" calendar date into a UTC-midnight
// time.Time, matching the UTC-anchored dateOnly arithmetic in cycles.go.
func goldenDate(t *testing.T, s string) time.Time {
	t.Helper()
	d, err := time.ParseInLocation("2006-01-02", s, time.UTC)
	if err != nil {
		t.Fatalf("parse golden date %q: %v", s, err)
	}
	return d
}

func TestResolveLutealPhase_ReferenceVectors(t *testing.T) {
	cases := []struct {
		name  string
		input int
		want  int
	}{
		{"zero defaults to 14", 0, 14},
		{"negative defaults to 14", -5, 14},
		{"below minimum clamps to 10", 5, 10},
		{"just below minimum clamps to 10", 9, 10},
		{"at minimum is unchanged", 10, 10},
		{"default value is unchanged", 14, 14},
		{"above minimum is unchanged", 20, 20},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := ResolveLutealPhase(tc.input); got != tc.want {
				t.Fatalf("ResolveLutealPhase(%d) = %d, want %d", tc.input, got, tc.want)
			}
		})
	}
}

func TestCalcOvulationDay_ReferenceVectors(t *testing.T) {
	// The second return is ovulationExact: false when the luteal phase had to be
	// clamped to the cycle reserve (or when the cycle is too short to predict).
	cases := []struct {
		name      string
		cycleLen  int
		luteal    int
		wantDay   int
		wantExact bool
	}{
		{"standard 28-day cycle", 28, 14, 14, true},
		{"30-day cycle, default luteal", 30, 0, 16, true},
		{"short 21-day cycle", 21, 14, 7, true},
		{"15-day cycle clamps luteal (non-exact)", 15, 14, 5, false},
		{"cycle too short for a prediction", 14, 14, 0, false},
		{"long luteal clamped to reserve (non-exact)", 28, 25, 5, false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			gotDay, gotExact := CalcOvulationDay(tc.cycleLen, tc.luteal)
			if gotDay != tc.wantDay || gotExact != tc.wantExact {
				t.Fatalf("CalcOvulationDay(%d,%d) = (%d,%t), want (%d,%t)",
					tc.cycleLen, tc.luteal, gotDay, gotExact, tc.wantDay, tc.wantExact)
			}
		})
	}
}

// TestCyclePrediction_GoldenVectors asserts every vector in the shared fixture
// against PredictCycleWindow (and the next-period formula), so the Go source of
// truth and ovumcy-app's TypeScript port cannot drift apart without failing CI
// on both sides. The fixture uses the TypeScript port's field vocabulary
// (calculable / fertilityStart / fertilityEnd / ovulationDate / isExact /
// nextPeriodStart); the mapping onto CycleWindowPrediction is spelled out below.
func TestCyclePrediction_GoldenVectors(t *testing.T) {
	fixture := loadGoldenVectors(t)

	for _, vector := range fixture.Vectors {
		t.Run(vector.Name, func(t *testing.T) {
			periodStart := goldenDate(t, vector.Input.CycleStartDate)
			window := PredictCycleWindow(periodStart, vector.Input.CycleLength, vector.Input.LutealPhase)

			if window.Calculable != vector.Expected.Calculable {
				t.Fatalf("calculable = %t, want %t", window.Calculable, vector.Expected.Calculable)
			}

			if vector.Expected.Calculable {
				// fertilityStart -> FertilityWindowStart
				wantFertFrom := goldenDate(t, *vector.Expected.FertilityStart)
				if !window.FertilityWindowStart.Equal(wantFertFrom) {
					t.Errorf("fertility start = %s, want %s",
						window.FertilityWindowStart.Format("2006-01-02"), wantFertFrom.Format("2006-01-02"))
				}
				// fertilityEnd -> FertilityWindowEnd
				wantFertTo := goldenDate(t, *vector.Expected.FertilityEnd)
				if !window.FertilityWindowEnd.Equal(wantFertTo) {
					t.Errorf("fertility end = %s, want %s",
						window.FertilityWindowEnd.Format("2006-01-02"), wantFertTo.Format("2006-01-02"))
				}
				// ovulationDate -> OvulationDate
				wantOvul := goldenDate(t, *vector.Expected.OvulationDate)
				if !window.OvulationDate.Equal(wantOvul) {
					t.Errorf("ovulation = %s, want %s",
						window.OvulationDate.Format("2006-01-02"), wantOvul.Format("2006-01-02"))
				}
				// isExact -> OvulationExact
				if window.OvulationExact != vector.Expected.IsExact {
					t.Errorf("exact = %t, want %t", window.OvulationExact, vector.Expected.IsExact)
				}
			} else {
				// A non-calculable prediction returns the zero struct: the
				// fixture pins these to null, so assert the dates are absent
				// rather than skipping the negative case.
				if vector.Expected.OvulationDate != nil || vector.Expected.FertilityStart != nil || vector.Expected.FertilityEnd != nil {
					t.Fatalf("fixture bug: non-calculable vector %q must have null date fields", vector.Name)
				}
				if !window.OvulationDate.IsZero() || !window.FertilityWindowStart.IsZero() || !window.FertilityWindowEnd.IsZero() {
					t.Errorf("non-calculable window should have zero dates, got ovul=%v fertFrom=%v fertTo=%v",
						window.OvulationDate, window.FertilityWindowStart, window.FertilityWindowEnd)
				}
			}

			// nextPeriodStart = cycleStartDate + cycleLength days. This is not
			// part of PredictCycleWindow's output, so assert it against the same
			// UTC-anchored formula the production pipeline uses
			// (applyPredictedCycleStats / PredictCycleWindow, via dateOnly +
			// AddDate). This mirrors ovumcy-app's cycle-prediction-reference.test.ts,
			// which recomputes cycleStartDate + cycleLength the same way to lock
			// the doc's "next period" column to the code on both sides.
			wantNext := goldenDate(t, vector.Expected.NextPeriodStart)
			if gotNext := dateOnly(periodStart.AddDate(0, 0, vector.Input.CycleLength)); !gotNext.Equal(wantNext) {
				t.Errorf("next period = %s, want %s", gotNext.Format("2006-01-02"), wantNext.Format("2006-01-02"))
			}
		})
	}
}
