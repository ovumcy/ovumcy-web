package services

import "testing"

// TestBBTShiftThirdDayMarginHoldsAtExactlyTwoTenths pins the boundary the rule
// names: the third elevated day at EXACTLY coverline + 0.2 must pass, one
// hundredth short must not. float64(36.2+0.2) is 36.400000000000006, so a
// naive `dayValues[dayThree] < coverline+bbtThirdDayMarginCelsius` compare
// rejects the exact boundary the rule is defined to accept.
func TestBBTShiftThirdDayMarginHoldsAtExactlyTwoTenths(t *testing.T) {
	cases := []struct {
		name          string
		values        []float64
		wantOK        bool
		wantFirstDay  int
		wantCoverline float64
	}{
		{
			name:          "boundary at coverline 36.2",
			values:        []float64{36.2, 36.2, 36.2, 36.2, 36.2, 36.2, 36.3, 36.3, 36.4},
			wantOK:        true,
			wantFirstDay:  7,
			wantCoverline: 36.2,
		},
		{
			name:          "boundary at coverline 36.5",
			values:        []float64{36.5, 36.5, 36.5, 36.5, 36.5, 36.5, 36.6, 36.6, 36.7},
			wantOK:        true,
			wantFirstDay:  7,
			wantCoverline: 36.5,
		},
		{
			name:          "boundary at coverline 36.7",
			values:        []float64{36.7, 36.7, 36.7, 36.7, 36.7, 36.7, 36.8, 36.8, 36.9},
			wantOK:        true,
			wantFirstDay:  7,
			wantCoverline: 36.7,
		},
		{
			name:          "boundary at coverline 36.1",
			values:        []float64{36.1, 36.1, 36.1, 36.1, 36.1, 36.1, 36.2, 36.2, 36.3},
			wantOK:        true,
			wantFirstDay:  7,
			wantCoverline: 36.1,
		},
		{
			name:   "one hundredth short of the margin is rejected",
			values: []float64{36.2, 36.2, 36.2, 36.2, 36.2, 36.2, 36.3, 36.3, 36.39},
			wantOK: false,
		},
		{
			name:   "no margin above the coverline is rejected",
			values: []float64{36.2, 36.2, 36.2, 36.2, 36.2, 36.2, 36.3, 36.3, 36.3},
			wantOK: false,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			recordedDays, dayValues := bbtSeriesFromValues(tc.values)

			firstHighDay, coverline, ok := detectBBTShiftFirstHighDay(recordedDays, dayValues)
			if ok != tc.wantOK {
				t.Fatalf("detectBBTShiftFirstHighDay ok = %v, want %v", ok, tc.wantOK)
			}
			if !tc.wantOK {
				return
			}
			if firstHighDay != tc.wantFirstDay {
				t.Errorf("firstHighDay = %d, want %d", firstHighDay, tc.wantFirstDay)
			}
			if coverline != tc.wantCoverline {
				t.Errorf("coverline = %v, want %v", coverline, tc.wantCoverline)
			}
		})
	}
}

// TestBBTShiftMarginHoldsForValuesEnteredInEitherUnit repeats the same
// boundary through ConvertDayBBTToStorage, so the assertion covers the values
// the detector actually receives (post-conversion, post-rounding) rather than
// hand-typed Celsius literals that happen to be exact in float64.
func TestBBTShiftMarginHoldsForValuesEnteredInEitherUnit(t *testing.T) {
	cases := []struct {
		name   string
		raw    []float64
		unit   string
		wantOK bool
	}{
		{
			name:   "fahrenheit entry exactly at the margin",
			raw:    []float64{97.16, 97.16, 97.16, 97.16, 97.16, 97.16, 97.52, 97.52, 97.88},
			unit:   TemperatureUnitFahrenheit,
			wantOK: true,
		},
		{
			name:   "fahrenheit entry one hundredth short of the margin",
			raw:    []float64{97.16, 97.16, 97.16, 97.16, 97.16, 97.16, 97.52, 97.52, 97.50},
			unit:   TemperatureUnitFahrenheit,
			wantOK: false,
		},
		{
			name:   "celsius entry exactly at the margin",
			raw:    []float64{36.2, 36.2, 36.2, 36.2, 36.2, 36.2, 36.3, 36.3, 36.4},
			unit:   TemperatureUnitCelsius,
			wantOK: true,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			recordedDays := make([]int, len(tc.raw))
			dayValues := make(map[int]float64, len(tc.raw))
			for index, rawValue := range tc.raw {
				day := index + 1
				recordedDays[index] = day
				stored := ConvertDayBBTToStorage(&rawValue, tc.unit)
				if stored == nil {
					t.Fatalf("ConvertDayBBTToStorage(%v, %q) returned nil", rawValue, tc.unit)
				}
				dayValues[day] = *stored
			}

			_, _, ok := detectBBTShiftFirstHighDay(recordedDays, dayValues)
			if ok != tc.wantOK {
				t.Fatalf("detectBBTShiftFirstHighDay ok = %v, want %v", ok, tc.wantOK)
			}
		})
	}
}

// bbtSeriesFromValues lays out values as calendar-consecutive recorded cycle
// days 1..len(values), matching what collectCycleBBTPoints/bbtSeriesFromPoints
// would produce for an unbroken run of daily readings.
func bbtSeriesFromValues(values []float64) ([]int, map[int]float64) {
	recordedDays := make([]int, len(values))
	dayValues := make(map[int]float64, len(values))
	for index, value := range values {
		day := index + 1
		recordedDays[index] = day
		dayValues[day] = value
	}
	return recordedDays, dayValues
}
