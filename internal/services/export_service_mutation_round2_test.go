package services

import (
	"context"
	"testing"
	"time"

	"github.com/ovumcy/ovumcy-web/internal/models"
)

// TestExportBuildCSVRowsEmptyMoodRatingForUnsetMood kills the
// CONDITIONALS_BOUNDARY mutant at export_service.go:437 in csvMoodRating
// (`value <= 0` -> `value < 0`). An unset mood (0) normalizes to 0 and must
// render as an EMPTY "Mood rating" column. Under the boundary mutant the guard
// `0 < 0` is false, so the value falls through to fmt.Sprintf and renders "0".
func TestExportBuildCSVRowsEmptyMoodRatingForUnsetMood(t *testing.T) {
	service := NewExportService(
		&stubExportDayReader{
			logs: []models.DailyLog{
				{
					Date: mustParseExportDay(t, "2026-02-18"),
					Mood: 0,
				},
			},
		},
		&stubExportSymptomReader{},
	)

	rows, err := service.BuildCSVRows(context.Background(), 42, nil, nil, time.UTC)
	if err != nil {
		t.Fatalf("BuildCSVRows() unexpected error: %v", err)
	}
	if len(rows) != 1 {
		t.Fatalf("expected one row, got %d", len(rows))
	}

	columns := rows[0].Columns()
	indexByHeader := exportCSVIndexByHeader()
	if got := columns[indexByHeader["Mood rating"]]; got != "" {
		t.Fatalf("expected empty mood rating column for unset mood, got %q", got)
	}
}

// TestExportBuildCSVRowsEmptyBBTForUnsetTemperature kills the
// CONDITIONALS_BOUNDARY mutant at export_service.go:456 in csvBBTValue
// (`normalized <= 0` -> `normalized < 0`). An unset temperature (0) normalizes
// to 0 and must render as an EMPTY "BBT (C)" column. Under the boundary mutant
// the guard `0 < 0` is false, so the value falls through to fmt.Sprintf and
// renders "0.00".
func TestExportBuildCSVRowsEmptyBBTForUnsetTemperature(t *testing.T) {
	service := NewExportService(
		&stubExportDayReader{
			logs: []models.DailyLog{
				{
					Date: mustParseExportDay(t, "2026-02-18"),
					BBT:  models.NewBBT(0),
				},
			},
		},
		&stubExportSymptomReader{},
	)

	rows, err := service.BuildCSVRows(context.Background(), 42, nil, nil, time.UTC)
	if err != nil {
		t.Fatalf("BuildCSVRows() unexpected error: %v", err)
	}
	if len(rows) != 1 {
		t.Fatalf("expected one row, got %d", len(rows))
	}

	columns := rows[0].Columns()
	indexByHeader := exportCSVIndexByHeader()
	if got := columns[indexByHeader["BBT (C)"]]; got != "" {
		t.Fatalf("expected empty BBT column for unset temperature, got %q", got)
	}
}
