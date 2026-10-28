package services

import "testing"

func TestFormatLocalizedNumberUsesTheLanguageSeparatorAndOneRoundingRule(t *testing.T) {
	cases := []struct {
		language string
		value    float64
		want     string
	}{
		{"en", 30.5, "30.5"},
		{"de", 30.5, "30,5"},
		{"fr", 36.6, "36,6"},
		{"ru", 36.6, "36,6"},
		{"es", 27.5, "27,5"},
		{"it", 27.5, "27,5"},
		{"ru-RU", 27.5, "27,5"},
		{"xx", 27.5, "27.5"},
		{"en", 28, "28"},
		{"de", 28.04, "28"},
		{"en", 36.25, "36.3"},
		{"en", -36.25, "-36.3"},
		{"en", 36.55, "36.5"}, // the binary value is just under the tie, as toFixed reads it
		{"en", 36.45, "36.5"},
		{"en", -0.04, "0"},
		{"en", 0, "0"},
	}
	for _, tc := range cases {
		if got := FormatLocalizedNumber(tc.language, tc.value); got != tc.want {
			t.Errorf("FormatLocalizedNumber(%q, %v) = %q, want %q", tc.language, tc.value, got, tc.want)
		}
	}
}

func TestMedianOfTwoMiddleCyclesKeepsItsHalfForDisplay(t *testing.T) {
	var stats CycleStats
	populateObservedCycleStats(&stats, []int{26, 35}, nil)
	if stats.MedianCycleLength != 31 {
		t.Fatalf("the prediction median stays a whole day, got %d", stats.MedianCycleLength)
	}
	if got := FormatLocalizedNumber("en", stats.MedianCycleLengthDisplay()); got != "30.5" {
		t.Fatalf("the card must not round 30.5 up to 31, got %q", got)
	}
	if got := (CycleStats{MedianCycleLength: 28}).MedianCycleLengthDisplay(); got != 28 {
		t.Fatalf("a configured baseline has no exact median, expected 28, got %v", got)
	}
}

func TestFormatLocalizedQuantitySeparatesTheUnitWithANoBreakSpace(t *testing.T) {
	if got := FormatLocalizedQuantity("ru", 28, "дн."); got != "28\xc2\xa0дн." {
		t.Fatalf("got %q", got)
	}
	if got := FormatLocalizedQuantity("fr", 36.6, "°C"); got != "36,6\xc2\xa0°C" {
		t.Fatalf("got %q", got)
	}
	if got := JoinUnit("36.6", ""); got != "36.6" {
		t.Fatalf("a missing unit must leave the number alone, got %q", got)
	}
}
