package services

import "testing"

func TestFormatDayBBTForDisplayUsesTheLanguageSeparatorAndTheStatsPrecision(t *testing.T) {
	celsius := 36.7
	fahrenheit := 36.6 // stored in Celsius; 97.88 F

	cases := []struct {
		name     string
		language string
		value    float64
		unit     string
		want     string
	}{
		{name: "en C", language: "en", value: celsius, unit: "c", want: "36.7"},
		{name: "ru C", language: "ru", value: celsius, unit: "c", want: "36,7"},
		{name: "en F", language: "en", value: 37, unit: "f", want: "98.6"},
		{name: "ru F", language: "ru", value: 37, unit: "f", want: "98,6"},
		{name: "ru F second value", language: "ru", value: fahrenheit, unit: "f", want: "97,9"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			value := tc.value
			if got := FormatDayBBTForDisplay(tc.language, &value, tc.unit); got != tc.want {
				t.Fatalf("FormatDayBBTForDisplay(%q, %v, %q) = %q, want %q", tc.language, tc.value, tc.unit, got, tc.want)
			}
		})
	}

	if got := FormatDayBBTForDisplay("ru", nil, "f"); got != "" {
		t.Fatalf("an unmeasured day must print nothing, got %q", got)
	}
}
