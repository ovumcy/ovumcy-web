package services

import "testing"

func TestFormatDayBBTForInputLocalizedUsesTheLanguageSeparatorAndTrimsZeros(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name     string
		language string
		value    float64
		unit     string
		want     string
	}{
		{name: "en C", language: "en", value: 36.75, unit: "c", want: "36.75"},
		{name: "ru C", language: "ru", value: 36.75, unit: "c", want: "36,75"},
		{name: "ru C trims a zero", language: "ru", value: 36.5, unit: "c", want: "36,5"},
		{name: "en C whole", language: "en", value: 37, unit: "c", want: "37"},
		{name: "en F", language: "en", value: 37, unit: "f", want: "98.6"},
		{name: "ru F", language: "ru", value: 37, unit: "f", want: "98,6"},
		{name: "de F", language: "de", value: 37, unit: "f", want: "98,6"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			value := tc.value
			got := FormatDayBBTForInputLocalized(tc.language, &value, tc.unit)
			if got != tc.want {
				t.Fatalf("FormatDayBBTForInputLocalized(%q, %v, %q) = %q, want %q", tc.language, tc.value, tc.unit, got, tc.want)
			}
			// The field must read back to the same stored reading.
			parsed, err := ParseDayBBTRawWithUnit(got, tc.unit)
			if err != nil || parsed == nil {
				t.Fatalf("the rendered field %q does not parse: %v", got, err)
			}
			if !sameStoredDayBBT(parsed, &value) {
				t.Fatalf("the rendered field %q reads back as %v, want %v", got, *parsed, value)
			}
		})
	}

	if got := FormatDayBBTForInputLocalized("ru", nil, "f"); got != "" {
		t.Fatalf("an unmeasured day must render an empty field, got %q", got)
	}
}
