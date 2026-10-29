package services

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
)

// This file consumes the ADDITIVE "irregularRange" section of the shared
// golden-vector fixture (testdata/cycle-prediction-golden-vectors.json): the
// current cycle's fertile window in irregular range mode,
// [PredictCycleWindow(min).FertilityWindowStart, PredictCycleWindow(max).OvulationDate]
// (irregularFertilityWindow), with its clamp to the recorded cycle start when the
// shortest cycle is below the 15-day floor, the median window end standing in
// when the longest is, and the 9999-12-31 cut that withholds the window as a whole.
//
// Like "projection", the section is a new top-level key, so a consumer that
// decodes only "vectors" keeps passing against a byte-identical copy. The
// ovumcy-app twin consumer is not written yet.

type goldenIrregularRangeSection struct {
	IrregularRange irregularRangeFixture `json:"irregularRange"`
}

type irregularRangeFixture struct {
	Comment       string                 `json:"$comment"`
	SchemaVersion int                    `json:"schemaVersion"`
	Vectors       []irregularRangeVector `json:"vectors"`
}

type irregularRangeVector struct {
	Name  string `json:"name"`
	Input struct {
		LastPeriodStart string `json:"lastPeriodStart"`
		CycleLength     int    `json:"cycleLength"`
		MinCycleLength  int    `json:"minCycleLength"`
		MaxCycleLength  int    `json:"maxCycleLength"`
		LutealPhase     int    `json:"lutealPhase"`
	} `json:"input"`
	Expected struct {
		Withheld    bool    `json:"withheld"`
		WindowStart *string `json:"windowStart"`
		WindowEnd   *string `json:"windowEnd"`
	} `json:"expected"`
}

func TestIrregularRangeWindow_GoldenVectors(t *testing.T) {
	raw, err := os.ReadFile(filepath.Join("testdata", goldenVectorsFile))
	if err != nil {
		t.Fatalf("read golden vectors: %v", err)
	}
	var section goldenIrregularRangeSection
	if err := json.Unmarshal(raw, &section); err != nil {
		t.Fatalf("parse irregular range vectors: %v", err)
	}
	if len(section.IrregularRange.Vectors) == 0 {
		t.Fatal("irregularRange section has no vectors")
	}

	for _, vector := range section.IrregularRange.Vectors {
		t.Run(vector.Name, func(t *testing.T) {
			in := vector.Input
			periodStart := goldenDate(t, in.LastPeriodStart)
			median := PredictCycleWindow(periodStart, in.CycleLength, in.LutealPhase)
			start, end := irregularFertilityWindow(median, periodStart, in.MinCycleLength, in.MaxCycleLength, in.LutealPhase)

			// The caller withholds the window as a whole when its last day
			// cannot be spelled (applyPredictedCycleStats).
			withheld := projectedDay(end).IsZero()
			if withheld != vector.Expected.Withheld {
				t.Fatalf("withheld = %t, want %t (window end %s)", withheld, vector.Expected.Withheld, end.Format("2006-01-02"))
			}
			if withheld {
				if vector.Expected.WindowStart != nil || vector.Expected.WindowEnd != nil {
					t.Fatalf("fixture bug: withheld vector %q must have null window dates", vector.Name)
				}
				return
			}
			if got, want := start.Format("2006-01-02"), *vector.Expected.WindowStart; got != want {
				t.Errorf("window start = %s, want %s", got, want)
			}
			if got, want := end.Format("2006-01-02"), *vector.Expected.WindowEnd; got != want {
				t.Errorf("window end = %s, want %s", got, want)
			}
		})
	}
}
