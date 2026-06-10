package services

import (
	"context"
	"testing"
	"time"

	"github.com/ovumcy/ovumcy-web/internal/models"
)

func TestExportBuildJSONEntriesKeepsMinimumMoodRating(t *testing.T) {
	service := NewExportService(
		&stubExportDayReader{
			logs: []models.DailyLog{
				{
					Date: mustParseExportDay(t, "2026-02-21"),
					Mood: MinDayMood,
				},
			},
		},
		&stubExportSymptomReader{},
	)

	entries, err := service.BuildJSONEntries(context.Background(), 42, nil, nil, time.UTC)
	if err != nil {
		t.Fatalf("BuildJSONEntries() unexpected error: %v", err)
	}
	if len(entries) != 1 {
		t.Fatalf("expected one entry, got %d", len(entries))
	}
	if entries[0].MoodRating != MinDayMood {
		t.Fatalf("expected mood rating %d preserved at lower boundary, got %d", MinDayMood, entries[0].MoodRating)
	}
}
