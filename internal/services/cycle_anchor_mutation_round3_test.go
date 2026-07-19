package services

import (
	"testing"
	"time"

	"github.com/ovumcy/ovumcy-web/internal/models"
)

// TestMR3Cycles_LatestExplicitCycleStartNilLocation targets
// cycle_anchor.go:25 `if location == nil` (NEGATION) in
// latestExplicitCycleStartBeforeOrOn (reached via the exported
// LatestCycleStartAnchorBeforeOrOn). A nil location must not panic and must fall
// back to UTC, returning the explicit cycle start. Under the NEGATION mutation
// (`location != nil`), a nil location is left nil and CalendarDay / DateAtLocation
// downstream would receive nil — exercised here to ensure correct fallback.
func TestMR3Cycles_LatestExplicitCycleStartNilLocation(t *testing.T) {
	logs := []models.DailyLog{
		mr3cycPeriodLog(mr3cycDay(2026, time.March, 1), true, false),
	}
	day := mr3cycDay(2026, time.March, 10)

	var result time.Time
	func() {
		defer func() {
			if r := recover(); r != nil {
				t.Fatalf("nil location must not panic: %v", r)
			}
		}()
		// user == nil so the anchor comes purely from the explicit log start.
		result = LatestCycleStartAnchorBeforeOrOn(nil, logs, day, nil)
	}()

	if !sameCalendarDay(result, mr3cycDay(2026, time.March, 1)) {
		t.Fatalf("expected explicit cycle start 2026-03-01, got %v", result)
	}
}

// TestMR3Cycles_LatestExplicitCycleStartHonorsLocation also targets
// cycle_anchor.go:25. The NEGATION mutation (`location != nil`) leaves a
// passed-in non-nil location intact only when it is nil and otherwise clobbers
// it to UTC. With a UTC+2 location and a same-day explicit cycle start anchored
// at that location's calendar day, the explicit start is on-or-before the
// target and must be returned. Clobbering the location to UTC shifts the target
// day one calendar day earlier, pushing the explicit start into the future so
// it is wrongly skipped and a zero anchor is returned.
func TestMR3Cycles_LatestExplicitCycleStartHonorsLocation(t *testing.T) {
	loc := time.FixedZone("UTCplus2", 2*60*60)
	// Explicit cycle start stored as the calendar date 2026-03-11.
	logs := []models.DailyLog{
		mr3cycPeriodLog(mr3cycDay(2026, time.March, 11), true, false),
	}
	// `day` is an instant that, projected into UTC+2, lands on 2026-03-11.
	day := time.Date(2026, time.March, 11, 1, 0, 0, 0, loc)

	result := LatestCycleStartAnchorBeforeOrOn(nil, logs, day, loc)
	if !sameCalendarDay(result, mr3cycDay(2026, time.March, 11)) {
		t.Fatalf("expected explicit cycle start 2026-03-11 honoring UTC+2, got %v", result)
	}
}

// TestMR3Cycles_LatestCycleStartAnchorNilLocation targets
// cycle_anchor.go:10 `if location == nil` (NEGATION) in
// latestCycleStartAnchorBeforeOrOn. Driven through the user anchor path with a
// nil location: the user's LastPeriodStart must be returned without panic.
// Under the NEGATION mutation a nil location stays nil and the location is never
// defaulted to UTC.
func TestMR3Cycles_LatestCycleStartAnchorNilLocation(t *testing.T) {
	lastPeriod := mr3cycDay(2026, time.February, 20)
	user := &models.User{
		Role:            models.RoleOwner,
		LastPeriodStart: &lastPeriod,
	}
	day := mr3cycDay(2026, time.March, 10)

	var result time.Time
	func() {
		defer func() {
			if r := recover(); r != nil {
				t.Fatalf("nil location must not panic: %v", r)
			}
		}()
		// No logs -> explicit start is zero; anchor comes from the user value.
		result = LatestCycleStartAnchorBeforeOrOn(user, nil, day, nil)
	}()

	if !sameCalendarDay(result, lastPeriod) {
		t.Fatalf("expected user anchor 2026-02-20, got %v", result)
	}
}

// TestMR3Cycles_LatestCycleStartAnchorHonorsLocation also targets
// cycle_anchor.go:10. The NEGATION mutation (`location != nil`) clobbers any
// non-nil location to UTC. With a UTC+2 location and a user anchor on the same
// calendar day as the localized target, the anchor is on-or-before the target
// and must be returned. Clobbering to UTC moves the target one calendar day
// earlier so the anchor looks like it is in the future and is dropped, yielding
// a zero anchor.
func TestMR3Cycles_LatestCycleStartAnchorHonorsLocation(t *testing.T) {
	loc := time.FixedZone("UTCplus2", 2*60*60)
	lastPeriod := mr3cycDay(2026, time.March, 11)
	user := &models.User{
		Role:            models.RoleOwner,
		LastPeriodStart: &lastPeriod,
	}
	// `day` projected into UTC+2 lands on 2026-03-11.
	day := time.Date(2026, time.March, 11, 1, 0, 0, 0, loc)

	result := LatestCycleStartAnchorBeforeOrOn(user, nil, day, loc)
	if !sameCalendarDay(result, mr3cycDay(2026, time.March, 11)) {
		t.Fatalf("expected user anchor 2026-03-11 honoring UTC+2, got %v", result)
	}
}
