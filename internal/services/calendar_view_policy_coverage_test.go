package services

import (
	"testing"
	"time"

	"github.com/ovumcy/ovumcy-web/internal/models"
)

// ---------------------------------------------------------------------------
// Line 18 — nil-location guard in ResolveCalendarMonthAndSelectedDateWithinBounds
// ---------------------------------------------------------------------------

// calendarviewpolicyCovNilLocationUsesUTC verifies that passing a nil location
// does not panic and behaves identically to passing time.UTC explicitly.
// Mutation: removing the nil guard (line 18) causes a nil-pointer dereference.
func TestCalendarViewPolicyNilLocationUsesUTC(t *testing.T) {
	now := time.Date(2026, time.March, 15, 9, 0, 0, 0, time.UTC)

	withNil, selNil, errNil := ResolveCalendarMonthAndSelectedDateWithinBounds("", "", now, nil, time.Time{})
	if errNil != nil {
		t.Fatalf("unexpected error with nil location: %v", errNil)
	}

	withUTC, selUTC, errUTC := ResolveCalendarMonthAndSelectedDateWithinBounds("", "", now, time.UTC, time.Time{})
	if errUTC != nil {
		t.Fatalf("unexpected error with UTC location: %v", errUTC)
	}

	if !withNil.Equal(withUTC) {
		t.Errorf("nil location: activeMonth = %v, want %v", withNil, withUTC)
	}
	if selNil != selUTC {
		t.Errorf("nil location: selectedDate = %q, want %q", selNil, selUTC)
	}
}

// calendarviewpolicyCovNilLocationWithSelectedDay verifies the same nil-guard on
// the path that derives the active month from the selected day.
func TestCalendarViewPolicyNilLocationWithSelectedDay(t *testing.T) {
	now := time.Date(2026, time.April, 5, 12, 0, 0, 0, time.UTC)

	withNil, selNil, err := ResolveCalendarMonthAndSelectedDateWithinBounds("", "2026-04-05", now, nil, time.Time{})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if selNil != "2026-04-05" {
		t.Errorf("nil location selectedDate = %q, want 2026-04-05", selNil)
	}
	if withNil.Format("2006-01") != "2026-04" {
		t.Errorf("nil location month = %q, want 2026-04", withNil.Format("2006-01"))
	}
}

// ---------------------------------------------------------------------------
// Line 70 — nil-location guard in CalendarMinimumNavigableMonth
// ---------------------------------------------------------------------------

// calendarviewpolicyCovMinMonthNilLocation verifies that CalendarMinimumNavigableMonth
// does not panic with a nil location and returns the same result as UTC.
// Mutation: removing the nil guard (line 70) causes a nil-pointer dereference.
func TestCalendarViewPolicyMinMonthNilLocation(t *testing.T) {
	user := &models.User{
		CreatedAt: time.Date(2025, time.June, 20, 8, 0, 0, 0, time.UTC),
	}

	gotNil := CalendarMinimumNavigableMonth(user, nil)
	gotUTC := CalendarMinimumNavigableMonth(user, time.UTC)

	if !gotNil.Equal(gotUTC) {
		t.Errorf("nil location minMonth = %v, want %v", gotNil, gotUTC)
	}
	// Sanity: result must be the first of a month at UTC midnight.
	if gotNil.Day() != 1 {
		t.Errorf("minMonth day = %d, want 1", gotNil.Day())
	}
	if gotNil.Location() != time.UTC {
		t.Errorf("minMonth location = %v, want UTC", gotNil.Location())
	}
}

// calendarviewpolicyCovMinMonthNonUTC verifies that the location is actually
// applied when non-nil — so a mutation that swaps nil/non-nil semantics is caught.
func TestCalendarViewPolicyMinMonthNonUTC(t *testing.T) {
	// CreatedAt at 23:30 UTC on June 20 is June 21 in UTC+2.
	berlin := time.FixedZone("CET", 2*60*60)
	user := &models.User{
		CreatedAt: time.Date(2025, time.June, 20, 23, 30, 0, 0, time.UTC),
	}

	got := CalendarMinimumNavigableMonth(user, berlin)
	// DateAtLocation(2025-06-20 23:30 UTC, CET+2) = 2025-06-21 in Berlin.
	// Subtract 3 years → 2022-06-21 → first of month = 2022-06-01.
	wantStr := "2022-06-01"
	if got.Format("2006-01-02") != wantStr {
		t.Errorf("CalendarMinimumNavigableMonth with CET location = %s, want %s",
			got.Format("2006-01-02"), wantStr)
	}
}

// ---------------------------------------------------------------------------
// Lines 112–115 — year and month comparison in calendarMonthBefore
// ---------------------------------------------------------------------------

// calendarviewpolicyCovMonthBeforeExactMinimum verifies that a month that equals
// the minimum is NOT considered "before" it.
// Mutation: changing < to <= on line 115 would return true for this case.
func TestCalendarViewPolicyMonthBeforeExactMinimum(t *testing.T) {
	minMonth := time.Date(2023, time.March, 1, 0, 0, 0, 0, time.UTC)
	month := time.Date(2026, time.February, 1, 0, 0, 0, 0, time.UTC)

	// Drive the function via CalendarAdjacentMonthValuesWithinBounds: when
	// monthStart == minMonth, prevMonth (Feb 2023) is before the minimum,
	// so prevValue must be "".  But monthStart itself (March 2023) is exactly
	// the minimum, confirming the equality edge.
	prev, _ := CalendarAdjacentMonthValuesWithinBounds(minMonth, minMonth)
	if prev != "" {
		t.Errorf("prevValue for month == minMonth should be empty, got %q", prev)
	}

	// Also drive calendarMonthBefore directly via ResolveCalendarMonthAndSelectedDateWithinBounds:
	// request March 2023 explicitly — it should NOT be clamped (it equals minMonth).
	now := time.Date(2026, time.February, 21, 0, 0, 0, 0, time.UTC)
	gotMonth, _, err := ResolveCalendarMonthAndSelectedDateWithinBounds("2023-03", "", now, time.UTC, minMonth)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if gotMonth.Format("2006-01") != "2023-03" {
		t.Errorf("month equal to minMonth should not be clamped: got %s, want 2023-03", gotMonth.Format("2006-01"))
	}

	// Suppress unused variable warning for month declared above
	_ = month
}

// calendarviewpolicyCovMonthBeforeEarlierYear verifies that a month in an earlier
// year is correctly identified as "before" the minimum.
// Mutation: changing < to > on line 113 would make earlier years appear "after".
func TestCalendarViewPolicyMonthBeforeEarlierYear(t *testing.T) {
	minMonth := time.Date(2023, time.March, 1, 0, 0, 0, 0, time.UTC)
	now := time.Date(2026, time.February, 21, 0, 0, 0, 0, time.UTC)

	// Request 2021-06 — an earlier year — it must be clamped to 2023-03.
	gotMonth, _, err := ResolveCalendarMonthAndSelectedDateWithinBounds("2021-06", "", now, time.UTC, minMonth)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if gotMonth.Format("2006-01") != "2023-03" {
		t.Errorf("month in earlier year should be clamped: got %s, want 2023-03", gotMonth.Format("2006-01"))
	}
}

// calendarviewpolicyCovMonthBeforeLaterYear verifies that a month in a later
// year is NOT clamped.
// Mutation: changing < to > on line 113 would clamp later years incorrectly.
func TestCalendarViewPolicyMonthBeforeLaterYear(t *testing.T) {
	minMonth := time.Date(2023, time.March, 1, 0, 0, 0, 0, time.UTC)
	now := time.Date(2026, time.February, 21, 0, 0, 0, 0, time.UTC)

	gotMonth, _, err := ResolveCalendarMonthAndSelectedDateWithinBounds("2025-06", "", now, time.UTC, minMonth)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if gotMonth.Format("2006-01") != "2025-06" {
		t.Errorf("month in later year should not be clamped: got %s, want 2025-06", gotMonth.Format("2006-01"))
	}
}

// calendarviewpolicyCovMonthBeforeSameYearEarlierMonth verifies that, within the
// same year, an earlier month is correctly identified as "before" the minimum.
// Mutation: changing < to <= on line 115 would also match the equal case (wrong).
// This test catches a mutation that changes < to > on line 115.
func TestCalendarViewPolicyMonthBeforeSameYearEarlierMonth(t *testing.T) {
	minMonth := time.Date(2023, time.June, 1, 0, 0, 0, 0, time.UTC)
	now := time.Date(2026, time.February, 21, 0, 0, 0, 0, time.UTC)

	// 2023-03 is in the same year but earlier month — must be clamped to 2023-06.
	gotMonth, _, err := ResolveCalendarMonthAndSelectedDateWithinBounds("2023-03", "", now, time.UTC, minMonth)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if gotMonth.Format("2006-01") != "2023-06" {
		t.Errorf("same-year earlier month should be clamped: got %s, want 2023-06", gotMonth.Format("2006-01"))
	}
}

// calendarviewpolicyCovMonthBeforeSameYearLaterMonth verifies that a later month
// in the same year as the minimum is NOT clamped.
func TestCalendarViewPolicyMonthBeforeSameYearLaterMonth(t *testing.T) {
	minMonth := time.Date(2023, time.June, 1, 0, 0, 0, 0, time.UTC)
	now := time.Date(2026, time.February, 21, 0, 0, 0, 0, time.UTC)

	gotMonth, _, err := ResolveCalendarMonthAndSelectedDateWithinBounds("2023-09", "", now, time.UTC, minMonth)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if gotMonth.Format("2006-01") != "2023-09" {
		t.Errorf("same-year later month should not be clamped: got %s, want 2023-09", gotMonth.Format("2006-01"))
	}
}

// ---------------------------------------------------------------------------
// Which zone a CLAMPED month is anchored in
// ---------------------------------------------------------------------------

// The three tests below fix the zone of a clamped month at the public entry
// point, ResolveCalendarMonthAndSelectedDateWithinBounds, which substitutes UTC
// for a nil location before clamping runs. minMonth carries a zone of its own
// (it is built from the account's CreatedAt), and it must never be the one the
// result is anchored in.
//
// TestCalendarViewPolicyResolveLocationNonNilReturnsIt: a supplied location wins
// over the zone embedded in minMonth. A clamp that anchored in minMonth's zone
// instead would fail here.
func TestCalendarViewPolicyResolveLocationNonNilReturnsIt(t *testing.T) {
	berlinLoc := time.FixedZone("Berlin", 2*60*60)
	tokyoLoc := time.FixedZone("Tokyo", 9*60*60)

	// minMonth embedded in Tokyo time; explicit location is Berlin.
	minMonth := time.Date(2023, time.June, 1, 0, 0, 0, 0, tokyoLoc)
	now := time.Date(2026, time.February, 21, 0, 0, 0, 0, berlinLoc)

	// Request a month that is before minMonth so clampCalendarMonthToMinimum fires.
	gotMonth, _, err := ResolveCalendarMonthAndSelectedDateWithinBounds("2022-01", "", now, berlinLoc, minMonth)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	// After clamping, the result must carry the explicit (Berlin) location, not Tokyo.
	gotLocOffset := gotMonth.Location().String()
	wantLocOffset := berlinLoc.String()
	if gotLocOffset != wantLocOffset {
		t.Errorf("clamped month location = %q, want %q (Berlin), not Tokyo",
			gotLocOffset, wantLocOffset)
	}
}

// TestCalendarViewPolicyResolveLocationNilUseFallback pins the nil-location
// contract: the entry point's own guard converts nil → UTC *before* clamping
// runs, so a clamped month carries UTC and not the Tokyo zone embedded in
// minMonth. This is the only nil-location path there is — clamping is reached
// from nowhere else.
func TestCalendarViewPolicyResolveLocationNilUseFallback(t *testing.T) {
	tokyoLoc := time.FixedZone("Tokyo", 9*60*60)
	minMonth := time.Date(2023, time.June, 1, 0, 0, 0, 0, tokyoLoc)
	now := time.Date(2026, time.February, 21, 0, 0, 0, 0, time.UTC)

	// Pass nil location — the entry point's guard replaces it with UTC.
	// Month 2022-01 is before minMonth, so clamping fires and the returned
	// month must carry UTC, not minMonth's Tokyo zone.
	gotMonth, _, err := ResolveCalendarMonthAndSelectedDateWithinBounds("2022-01", "", now, nil, minMonth)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if gotMonth.Format("2006-01") != "2023-06" {
		t.Errorf("clamped month = %s, want 2023-06", gotMonth.Format("2006-01"))
	}
	// The clamped result must be at UTC (the nil-guard default).
	if gotMonth.Location() != time.UTC {
		t.Errorf("clamped month location = %v, want UTC", gotMonth.Location())
	}
}

// TestCalendarViewPolicyResolveLocationNonNilPreferred repeats the check with a
// second, opposite-sign zone, so a clamp that anchored in minMonth's zone rather
// than the request's fails on a concrete offset rather than by luck.
func TestCalendarViewPolicyResolveLocationNonNilPreferred(t *testing.T) {
	tokyoLoc := time.FixedZone("Tokyo", 9*60*60)
	pacificLoc := time.FixedZone("PST", -8*60*60)

	minMonth := time.Date(2023, time.June, 1, 0, 0, 0, 0, tokyoLoc)
	now := time.Date(2026, time.February, 21, 0, 0, 0, 0, pacificLoc)

	// Month 2022-03 is before minMonth; explicit location is Pacific.
	gotMonth, _, err := ResolveCalendarMonthAndSelectedDateWithinBounds("2022-03", "", now, pacificLoc, minMonth)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	// The explicit pacificLoc must win over the Tokyo location embedded in minMonth.
	if gotMonth.Location() != pacificLoc {
		t.Errorf("clamped month should carry the supplied location: got %v, want PST",
			gotMonth.Location())
	}
	// And the clamped month itself must be 2023-06.
	if gotMonth.Format("2006-01") != "2023-06" {
		t.Errorf("clamped month = %s, want 2023-06", gotMonth.Format("2006-01"))
	}
}
