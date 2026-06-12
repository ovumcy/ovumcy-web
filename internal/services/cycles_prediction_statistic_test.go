package services

import (
	"math"
	"testing"

	"github.com/ovumcy/ovumcy-web/internal/models"
)

// TestPredictedCycleLength_PrefersMedianOverMean pins the medical-accuracy fix:
// the prediction statistic is the MEDIAN of recent cycles, not the mean. The
// mean is sensitive to a single outlier cycle (a missed period log merges two
// real cycles into one ~60-90 day gap), which would push every prediction late.
// docs/cycle-prediction.md documents the median; this test guards the contract.
func TestPredictedCycleLength_PrefersMedianOverMean(t *testing.T) {
	cases := []struct {
		name    string
		median  int
		average float64
		want    int
	}{
		// The decisive case: a 90-day outlier among five 28-day cycles yields a
		// mean of ~38 but a median of 28. The median must win.
		{"outlier-skewed mean is ignored in favor of median", 28, 38.3, 28},
		// Even when median and rounded mean differ by one, the median wins.
		{"median chosen over rounded mean", 27, 27.6, 27},
		// Mean is only a fallback when no median exists.
		{"falls back to rounded mean when median is zero", 0, 30.4, 30},
		{"falls back to default when neither is available", 0, 0, models.DefaultCycleLength},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := predictedCycleLength(tc.median, tc.average); got != tc.want {
				t.Fatalf("predictedCycleLength(median=%d, average=%.2f) = %d, want %d",
					tc.median, tc.average, got, tc.want)
			}
		})
	}
}

// TestMedianInt_EvenCount closes a coverage gap: the even-count branch of
// medianInt (round-half-up of the two middle values) was never exercised by a
// fixture. Verify it returns the true median, rounding half up.
func TestMedianInt_EvenCount(t *testing.T) {
	cases := []struct {
		name   string
		values []int
		want   int
	}{
		{"empty is zero", nil, 0},
		{"single value", []int{28}, 28},
		{"odd count takes the middle", []int{27, 28, 30}, 28},
		{"even count, exact integer median", []int{26, 30}, 28},
		{"even count rounds half up", []int{27, 28}, 28},
		{"even count of four", []int{20, 21, 28, 29}, 25},
		{"unsorted input is sorted first", []int{30, 26}, 28},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := medianInt(tc.values); got != tc.want {
				t.Fatalf("medianInt(%v) = %d, want %d", tc.values, got, tc.want)
			}
		})
	}
}

// TestPopulateObservedCycleStatsRangeAndSpreadUseRecentWindow pins the range
// (min/max) and variability (stddev) statistics to the same recent
// prediction window the median uses. A merged-gap outlier that has aged out
// of the window must stop widening irregular prediction ranges and the
// variability spread; before the fix min/max and stddev were computed over
// the all-time length history.
func TestPopulateObservedCycleStatsRangeAndSpreadUseRecentWindow(t *testing.T) {
	// 7 completed cycles: the oldest is a 90-day merged-gap outlier, the
	// recent six (the cyclePredictionWindow) are a stable 27..30 spread.
	lengths := []int{90, 28, 29, 27, 30, 28, 28}

	stats := CycleStats{}
	populateObservedCycleStats(&stats, lengths, nil)

	if stats.MinCycleLength != 27 {
		t.Fatalf("MinCycleLength = %d, want 27 (windowed)", stats.MinCycleLength)
	}
	if stats.MaxCycleLength != 30 {
		t.Fatalf("MaxCycleLength = %d, want 30 (the aged-out 90-day outlier must not widen the range)", stats.MaxCycleLength)
	}

	// Sample stddev over the recent window [28 29 27 30 28 28]:
	// mean = 28.333..., squared diffs sum = 5.333..., n-1 = 5.
	want := math.Sqrt((16.0 / 3.0) / 5.0)
	if math.Abs(stats.CycleLengthStdDev-want) > 1e-9 {
		t.Fatalf("CycleLengthStdDev = %.10f, want %.10f (windowed sample stddev)", stats.CycleLengthStdDev, want)
	}

	if stats.CompletedCycleCount != 7 {
		t.Fatalf("CompletedCycleCount = %d, want 7 (count stays all-time)", stats.CompletedCycleCount)
	}
	if stats.LastCycleLength != 28 {
		t.Fatalf("LastCycleLength = %d, want 28", stats.LastCycleLength)
	}
}
