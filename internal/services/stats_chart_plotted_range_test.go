package services

import "testing"

// TestStatsChartRangeSpansEveryPlottedBar pins the chart range to the plotted
// series (WEB-250): the extremes may sit first, last or mid-series, and a
// single bar is its own range.
func TestStatsChartRangeSpansEveryPlottedBar(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name     string
		lengths  []int
		min, max int
	}{
		{name: "no bars", lengths: nil, min: 0, max: 0},
		{name: "single bar", lengths: []int{31}, min: 31, max: 31},
		{name: "extremes mid-series", lengths: []int{28, 365, 28, 15, 128, 28}, min: 15, max: 365},
		{name: "extremes at the ends", lengths: []int{40, 29, 30, 21}, min: 21, max: 40},
		{name: "descending first bar is the max", lengths: []int{35, 30, 25}, min: 25, max: 35},
	}
	for _, testCase := range cases {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()
			chart := buildStatsChartViewData("", testCase.lengths, 0)
			if chart.RangeMin != testCase.min || chart.RangeMax != testCase.max {
				t.Fatalf("range %d..%d, want %d..%d", chart.RangeMin, chart.RangeMax, testCase.min, testCase.max)
			}
		})
	}
}
