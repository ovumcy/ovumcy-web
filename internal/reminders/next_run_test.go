package reminders

import (
	"testing"
	"time"
)

// mustLoadLocation loads an IANA zone or skips the test when the platform lacks
// the tz database (some minimal Windows dev boxes). The Linux runtime that ships
// always has it, and CI validates there.
func mustLoadLocation(t *testing.T, name string) *time.Location {
	t.Helper()
	loc, err := time.LoadLocation(name)
	if err != nil {
		t.Skipf("timezone %q unavailable on this platform: %v", name, err)
	}
	return loc
}

// TestNextRunBasicSameDayAndRollover covers the non-DST core: when the target
// hour is still ahead today, nextRun picks today; when it has passed (or equals
// now), it rolls to tomorrow.
func TestNextRunBasicSameDayAndRollover(t *testing.T) {
	utc := time.UTC
	hour := 9

	cases := []struct {
		name string
		now  time.Time
		want time.Time
	}{
		{
			name: "before target hour -> today",
			now:  time.Date(2026, 3, 10, 6, 30, 0, 0, utc),
			want: time.Date(2026, 3, 10, 9, 0, 0, 0, utc),
		},
		{
			name: "after target hour -> tomorrow",
			now:  time.Date(2026, 3, 10, 12, 0, 0, 0, utc),
			want: time.Date(2026, 3, 11, 9, 0, 0, 0, utc),
		},
		{
			name: "exactly at target hour -> tomorrow (strictly after)",
			now:  time.Date(2026, 3, 10, 9, 0, 0, 0, utc),
			want: time.Date(2026, 3, 11, 9, 0, 0, 0, utc),
		},
		{
			name: "one second before target -> today",
			now:  time.Date(2026, 3, 10, 8, 59, 59, 0, utc),
			want: time.Date(2026, 3, 10, 9, 0, 0, 0, utc),
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := nextRun(tc.now, hour, utc)
			if !got.Equal(tc.want) {
				t.Fatalf("nextRun(%s) = %s, want %s", tc.now.Format(time.RFC3339), got.Format(time.RFC3339), tc.want.Format(time.RFC3339))
			}
		})
	}
}

// TestNextRunHourZeroAccepted guards the midnight edge that motivated
// getEnvIntInRange: hour 0 is a valid run hour and must schedule to local
// 00:00, not be treated as "unset".
func TestNextRunHourZeroAccepted(t *testing.T) {
	utc := time.UTC
	now := time.Date(2026, 3, 10, 23, 30, 0, 0, utc)
	want := time.Date(2026, 3, 11, 0, 0, 0, 0, utc)
	if got := nextRun(now, 0, utc); !got.Equal(want) {
		t.Fatalf("nextRun hour=0 = %s, want %s", got.Format(time.RFC3339), want.Format(time.RFC3339))
	}
}

// TestNextRunNilLocationDefaultsUTC covers the defensive nil-location branch.
func TestNextRunNilLocationDefaultsUTC(t *testing.T) {
	now := time.Date(2026, 3, 10, 6, 0, 0, 0, time.UTC)
	want := time.Date(2026, 3, 10, 9, 0, 0, 0, time.UTC)
	if got := nextRun(now, 9, nil); !got.Equal(want) {
		t.Fatalf("nextRun(nil location) = %s, want %s", got.Format(time.RFC3339), want.Format(time.RFC3339))
	}
}

// TestNextRunDSTSpringForward pins the spring-forward edge in America/New_York:
// on 2026-03-08 local clocks jump 02:00 -> 03:00, so local 02:00 does not exist.
// A scheduler configured for hour 2 must still produce a real future instant on
// that day (time.Date normalizes the missing wall-clock hour to a concrete
// instant), never a zero/past time that would stall the loop or fire in the
// past. Go's time.Date maps the skipped 02:00 to the same absolute instant as
// pre-transition 01:00 (offset -05:00), which is what nextRun returns unchanged
// because it is still after the 00:30 "now" — the pass fires on the right day
// without the schedule stalling. The key invariants are: future, same local
// calendar day, and the exact instant time.Date resolves the skipped hour to.
func TestNextRunDSTSpringForward(t *testing.T) {
	ny := mustLoadLocation(t, "America/New_York")

	// Just after midnight local on the spring-forward day, target hour 2 (skipped).
	now := time.Date(2026, 3, 8, 0, 30, 0, 0, ny)
	got := nextRun(now, 2, ny)

	if !got.After(now) {
		t.Fatalf("expected a future fire across the skipped hour, got %s (now %s)", got.Format(time.RFC3339), now.Format(time.RFC3339))
	}
	// nextRun builds the candidate with time.Date on the target day; the skipped
	// 02:00 resolves to one concrete instant, which must equal a direct time.Date
	// for the same wall-clock hour (the schedule uses that exact instant).
	wantResolved := time.Date(2026, 3, 8, 2, 0, 0, 0, ny)
	if !got.Equal(wantResolved) {
		t.Fatalf("expected skipped-hour fire to equal time.Date's normalized instant %s, got %s", wantResolved.Format(time.RFC3339), got.Format(time.RFC3339))
	}
	// And it stays on the same local calendar day (no accidental skip to tomorrow).
	if got.In(ny).Day() != 8 {
		t.Fatalf("expected fire to stay on local day 8, got %s", got.In(ny).Format(time.RFC3339))
	}
}

// TestNextRunDSTFallBack pins the fall-back edge in America/New_York: on
// 2026-11-01 local clocks fall 02:00 -> 01:00, so local 01:00 occurs twice.
// nextRun must resolve the target to ONE concrete future instant (the once-per-
// local-day marker, tested elsewhere, prevents the repeated hour from firing a
// second pass).
func TestNextRunDSTFallBack(t *testing.T) {
	ny := mustLoadLocation(t, "America/New_York")

	// Just after midnight local on the fall-back day, target hour 1 (repeats).
	now := time.Date(2026, 11, 1, 0, 15, 0, 0, ny)
	got := nextRun(now, 1, ny)

	if !got.After(now) {
		t.Fatalf("expected a future fire for the repeated hour, got %s (now %s)", got.Format(time.RFC3339), now.Format(time.RFC3339))
	}
	if got.In(ny).Hour() != 1 || got.In(ny).Day() != 1 {
		t.Fatalf("expected fire at local 01:00 on day 1, got %s", got.In(ny).Format(time.RFC3339))
	}

	// Recomputing from just after the FIRST 01:00 occurrence must advance to the
	// next day, not re-fire the second 01:00 — nextRun keys off the calendar day,
	// so once today's 01:00 has passed the next fire is tomorrow.
	afterFirst := got.Add(30 * time.Minute)
	next := nextRun(afterFirst, 1, ny)
	if next.In(ny).Day() != 2 {
		t.Fatalf("expected the following fire to roll to day 2, got %s", next.In(ny).Format(time.RFC3339))
	}
}

// TestNextRunAcrossDSTStaysPinnedToLocalHour is the anti-drift property a bare
// 24h ticker would fail: recomputing each day keeps the fire at local 09:00
// across the spring-forward boundary even though the UTC offset changed.
func TestNextRunAcrossDSTStaysPinnedToLocalHour(t *testing.T) {
	ny := mustLoadLocation(t, "America/New_York")

	// Day before the 2026-03-08 spring-forward, after 09:00 -> next fire is the
	// 8th at 09:00 local; the offset changes that morning, but the local hour must
	// remain 9.
	now := time.Date(2026, 3, 7, 10, 0, 0, 0, ny)
	got := nextRun(now, 9, ny)
	if got.In(ny).Hour() != 9 || got.In(ny).Day() != 8 {
		t.Fatalf("expected next fire at local 09:00 on day 8 across DST, got %s", got.In(ny).Format(time.RFC3339))
	}
}
