package services

import "testing"

// TestBBTShiftThirdDayMarginHoldsAtExactlyTwoTenths pins the boundary the rule
// names: the third elevated day at EXACTLY coverline + 0.2 must pass, one
// hundredth short must not.
//
// The three passing levels are the ones where float64(coverline + 0.2) lands
// one ULP above the literal the third day is recorded as (36.1, 36.2, 36.7);
// each of them is red under the plain float comparison. Levels where the sum
// happens to round the other way (36.5) prove nothing about the fix and are
// deliberately not listed.
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
		{
			name:   "a second day equal to the coverline is not elevated",
			values: []float64{36.2, 36.2, 36.2, 36.2, 36.2, 36.2, 36.3, 36.2, 36.4},
			wantOK: false,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			recordedDays, dayValues := bbtSeriesFromDailyValues(tc.values)

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

// TestBBTShiftMarginHoldsForValuesEnteredInEitherUnit repeats the 36.2 boundary
// through ConvertDayBBTToStorage, so the assertion covers the values the
// detector actually receives (post-conversion, post-rounding). 97.16 / 97.34 /
// 97.52 °F convert to exactly 36.2 / 36.3 / 36.4 °C, so the Fahrenheit case
// reaches the same one-ULP boundary as the Celsius one and is red under the
// float comparison too; 97.50 °F is 36.3889 °C, short of the margin.
func TestBBTShiftMarginHoldsForValuesEnteredInEitherUnit(t *testing.T) {
	cases := []struct {
		name   string
		raw    []float64
		unit   string
		wantOK bool
	}{
		{
			name:   "fahrenheit entry exactly at the margin",
			raw:    []float64{97.16, 97.16, 97.16, 97.16, 97.16, 97.16, 97.34, 97.34, 97.52},
			unit:   TemperatureUnitFahrenheit,
			wantOK: true,
		},
		{
			name:   "fahrenheit entry short of the margin",
			raw:    []float64{97.16, 97.16, 97.16, 97.16, 97.16, 97.16, 97.34, 97.34, 97.50},
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
			stored := make([]float64, 0, len(tc.raw))
			for _, rawValue := range tc.raw {
				converted := ConvertDayBBTToStorage(&rawValue, tc.unit)
				if converted == nil {
					t.Fatalf("ConvertDayBBTToStorage(%v, %q) returned nil", rawValue, tc.unit)
				}
				stored = append(stored, *converted)
			}

			_, _, ok := detectBBTShiftFirstHighDay(bbtSeriesFromDailyValues(stored))
			if ok != tc.wantOK {
				t.Fatalf("detectBBTShiftFirstHighDay ok = %v, want %v", ok, tc.wantOK)
			}
		})
	}
}

// bbtSeriesFromDailyValues lays values out as an unbroken run of daily readings
// on cycle days 1..len(values) and hands them to the production series
// builder, so the detector sees the same shape it gets from collectCycleBBTPoints.
func bbtSeriesFromDailyValues(values []float64) ([]int, map[int]float64) {
	points := make([]cycleBBTPoint, len(values))
	for index, value := range values {
		points[index] = cycleBBTPoint{CycleDay: index + 1, Value: value}
	}
	return bbtSeriesFromPoints(points)
}
