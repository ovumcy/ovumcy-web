package services

import (
	"errors"
	"testing"
	"time"
)

func TestParseDayDateRequiresValue(t *testing.T) {
	t.Parallel()

	if _, err := ParseDayDate("   ", time.UTC); !errors.Is(err, ErrDayDateRequired) {
		t.Fatalf("expected ErrDayDateRequired, got %v", err)
	}
}

func TestParseDayDateRejectsInvalidValue(t *testing.T) {
	t.Parallel()

	if _, err := ParseDayDate("2026-99-99", time.UTC); err == nil {
		t.Fatal("expected parse error for invalid date")
	}
}

func TestParseDayDateNormalizesToLocationDate(t *testing.T) {
	t.Parallel()

	location, err := time.LoadLocation("Europe/Moscow")
	if err != nil {
		t.Fatalf("load location: %v", err)
	}

	day, err := ParseDayDate("2026-03-01", location)
	if err != nil {
		t.Fatalf("ParseDayDate unexpected error: %v", err)
	}
	if day.Location() != location {
		t.Fatalf("expected location %s, got %s", location, day.Location())
	}
	if got := day.Format("2006-01-02 15:04:05"); got != "2026-03-01 00:00:00" {
		t.Fatalf("expected normalized local midnight, got %s", got)
	}
}
