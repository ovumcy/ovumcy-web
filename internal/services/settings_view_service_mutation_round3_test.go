package services

import "testing"

// settings_view_service.go:311:31 NEGATION `==`->`!=` in compareISODate.
// Equal inputs (including with surrounding spaces) must yield 0.
func TestMR3I18n_CompareISODateEqual(t *testing.T) {
	if got := compareISODate("2026-06-01", "2026-06-01"); got != 0 {
		t.Fatalf("compareISODate(equal) = %d, want 0", got)
	}
	if got := compareISODate("  2026-06-01  ", "2026-06-01"); got != 0 {
		t.Fatalf("compareISODate(equal w/ spaces) = %d, want 0", got)
	}
}

// settings_view_service.go:313:12 BOUNDARY+NEGATION `left < right`.
// Distinct dates: less -> -1, greater -> 1. Equal is handled by the prior case,
// so flipping the comparator here must change one of these two results.
func TestMR3I18n_CompareISODateOrdering(t *testing.T) {
	if got := compareISODate("2026-01-01", "2026-06-01"); got != -1 {
		t.Fatalf("compareISODate(less) = %d, want -1", got)
	}
	if got := compareISODate("2026-12-31", "2026-06-01"); got != 1 {
		t.Fatalf("compareISODate(greater) = %d, want 1", got)
	}
}
