package services

import "testing"

func TestLocalizedSymptomFrequencySummary_EnglishPluralization(t *testing.T) {
	tests := []struct {
		name     string
		count    int
		days     int
		expected string
	}{
		{name: "singular count and day", count: 1, days: 1, expected: "1 time (in 1 day)"},
		{name: "plural count and day", count: 2, days: 4, expected: "2 times (in 4 days)"},
	}

	for _, testCase := range tests {
		t.Run(testCase.name, func(t *testing.T) {
			got := LocalizedSymptomFrequencySummary("en", testCase.count, testCase.days)
			if got != testCase.expected {
				t.Fatalf("expected %q, got %q", testCase.expected, got)
			}
		})
	}
}

func TestLocalizedSymptomFrequencySummary_RussianPluralization(t *testing.T) {
	tests := []struct {
		name     string
		count    int
		days     int
		expected string
	}{
		{name: "one form", count: 1, days: 1, expected: "1 раз (за 1 день)"},
		{name: "few form", count: 2, days: 4, expected: "2 раза (за 4 дня)"},
		{name: "many form", count: 5, days: 7, expected: "5 раз (за 7 дней)"},
		{name: "teens form", count: 11, days: 12, expected: "11 раз (за 12 дней)"},
		{name: "mixed form", count: 21, days: 22, expected: "21 раз (за 22 дня)"},
	}

	for _, testCase := range tests {
		t.Run(testCase.name, func(t *testing.T) {
			got := LocalizedSymptomFrequencySummary("ru", testCase.count, testCase.days)
			if got != testCase.expected {
				t.Fatalf("expected %q, got %q", testCase.expected, got)
			}
		})
	}
}

func TestLocalizedSymptomFrequencySummary_SpanishPluralization(t *testing.T) {
	tests := []struct {
		name     string
		count    int
		days     int
		expected string
	}{
		{name: "singular count and day", count: 1, days: 1, expected: "1 vez (en 1 día)"},
		{name: "plural count and day", count: 2, days: 4, expected: "2 veces (en 4 días)"},
	}

	for _, testCase := range tests {
		t.Run(testCase.name, func(t *testing.T) {
			got := LocalizedSymptomFrequencySummary("es", testCase.count, testCase.days)
			if got != testCase.expected {
				t.Fatalf("expected %q, got %q", testCase.expected, got)
			}
		})
	}
}

func TestLocalizedSymptomFrequencySummary_FrenchPluralization(t *testing.T) {
	tests := []struct {
		name     string
		count    int
		days     int
		expected string
	}{
		{name: "singular day", count: 1, days: 1, expected: "1 fois (en 1 jour)"},
		{name: "plural day", count: 2, days: 4, expected: "2 fois (en 4 jours)"},
	}

	for _, testCase := range tests {
		t.Run(testCase.name, func(t *testing.T) {
			got := LocalizedSymptomFrequencySummary("fr", testCase.count, testCase.days)
			if got != testCase.expected {
				t.Fatalf("expected %q, got %q", testCase.expected, got)
			}
		})
	}
}
