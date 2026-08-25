package services

import (
	"errors"
	"testing"
	"time"
)

func TestParseExportRange(t *testing.T) {
	location := time.UTC

	t.Run("empty range", func(t *testing.T) {
		from, to, err := ParseExportRange("", "", location)
		if err != nil {
			t.Fatalf("expected nil error, got %v", err)
		}
		if from != nil || to != nil {
			t.Fatalf("expected nil from/to, got from=%v to=%v", from, to)
		}
	})

	t.Run("valid from and to", func(t *testing.T) {
		from, to, err := ParseExportRange("2026-02-10", "2026-02-20", location)
		if err != nil {
			t.Fatalf("expected nil error, got %v", err)
		}
		if from == nil || to == nil {
			t.Fatalf("expected non-nil range bounds")
		}
		if from.Format("2006-01-02") != "2026-02-10" || to.Format("2006-01-02") != "2026-02-20" {
			t.Fatalf("unexpected range: from=%s to=%s", from.Format("2006-01-02"), to.Format("2006-01-02"))
		}
	})

	// A single-day export has from == to. The order guard rejects only a `to`
	// strictly BEFORE `from`, and nothing exercised the equal case, so the
	// boundary mutant `to.Before(*from)` -> `!to.After(*from)` refused a
	// one-day range with no test noticing.
	t.Run("single day range", func(t *testing.T) {
		from, to, err := ParseExportRange("2026-02-10", "2026-02-10", location)
		if err != nil {
			t.Fatalf("expected a single-day range to be accepted, got %v", err)
		}
		if from == nil || to == nil {
			t.Fatalf("expected non-nil range bounds")
		}
		if !from.Equal(*to) {
			t.Fatalf("expected identical bounds, got from=%s to=%s", from, to)
		}
	})

	// The export form submits either half on its own, and the export service's
	// own tests drive those half-open ranges — but nothing reached the policy
	// with one bound set and the other empty, where the order guard's nil
	// operands decide whether it runs at all.
	t.Run("open ended from", func(t *testing.T) {
		from, to, err := ParseExportRange("2026-02-10", "", location)
		if err != nil {
			t.Fatalf("expected an open-ended range to be accepted, got %v", err)
		}
		if from == nil || from.Format("2006-01-02") != "2026-02-10" {
			t.Fatalf("expected from=2026-02-10, got %v", from)
		}
		if to != nil {
			t.Fatalf("expected nil to, got %v", to)
		}
	})

	t.Run("open ended to", func(t *testing.T) {
		from, to, err := ParseExportRange("", "2026-02-20", location)
		if err != nil {
			t.Fatalf("expected an open-ended range to be accepted, got %v", err)
		}
		if from != nil {
			t.Fatalf("expected nil from, got %v", from)
		}
		if to == nil || to.Format("2006-01-02") != "2026-02-20" {
			t.Fatalf("expected to=2026-02-20, got %v", to)
		}
	})

	t.Run("invalid from", func(t *testing.T) {
		_, _, err := ParseExportRange("not-a-date", "2026-02-20", location)
		if !errors.Is(err, ErrExportFromDateInvalid) {
			t.Fatalf("expected ErrExportFromDateInvalid, got %v", err)
		}
	})

	t.Run("invalid to", func(t *testing.T) {
		_, _, err := ParseExportRange("2026-02-10", "not-a-date", location)
		if !errors.Is(err, ErrExportToDateInvalid) {
			t.Fatalf("expected ErrExportToDateInvalid, got %v", err)
		}
	})

	t.Run("invalid range order", func(t *testing.T) {
		_, _, err := ParseExportRange("2026-02-20", "2026-02-10", location)
		if !errors.Is(err, ErrExportRangeInvalid) {
			t.Fatalf("expected ErrExportRangeInvalid, got %v", err)
		}
	})
}
