package api

import (
	"testing"
)

func TestLocalizeSymptomFrequencySummaries(t *testing.T) {
	counts := []SymptomCount{
		{Name: "A", Count: 1, TotalDays: 1},
		{Name: "B", Count: 2, TotalDays: 4},
	}

	localizeSymptomFrequencySummaries("en", counts)
	if counts[0].FrequencySummary != "1 time (in 1 day)" {
		t.Fatalf("unexpected localized summary for first item: %q", counts[0].FrequencySummary)
	}
	if counts[1].FrequencySummary != "2 times (in 4 days)" {
		t.Fatalf("unexpected localized summary for second item: %q", counts[1].FrequencySummary)
	}
}
