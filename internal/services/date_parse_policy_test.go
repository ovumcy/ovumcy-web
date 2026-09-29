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

// TestParseDayDateRefusesACalendarDayTheZoneNeverHad pins the refusal half of
// the parse contract. A zone that crosses the date line skips a WHOLE calendar
// day: Pacific/Apia jumped from 2011-12-29 23:59:59 UTC-10 straight to
// 2011-12-31 00:00 UTC+14, and Pacific/Kiritimati did the same over 1994-12-31.
// No instant at all exists on those days, so time.Date normalizes backward into
// the previous one. Returning that with a nil error is indistinguishable from a
// successful parse and silently saves the wrong day, which is the shift the
// skipped-midnight fix removed everywhere else — the parse entry point must
// report the input as invalid instead.
func TestParseDayDateRefusesACalendarDayTheZoneNeverHad(t *testing.T) {
	t.Parallel()

	cases := []struct {
		zone string
		day  string
		note string
	}{
		{zone: "Pacific/Apia", day: "2011-12-30", note: "UTC-10 -> UTC+14 date-line crossing"},
		{zone: "Pacific/Kiritimati", day: "1994-12-31", note: "UTC-10 -> UTC+14 date-line crossing"},
	}

	for _, tc := range cases {
		t.Run(tc.zone+" "+tc.day, func(t *testing.T) {
			t.Parallel()

			location, err := time.LoadLocation(tc.zone)
			if err != nil {
				t.Fatalf("load %s: %v", tc.zone, err)
			}

			got, err := ParseDayDate(tc.day, location)
			if !errors.Is(err, ErrDayDateNonexistent) {
				t.Fatalf("ParseDayDate(%s, %s) = %s, %v; want ErrDayDateNonexistent (%s)",
					tc.day, tc.zone, got.Format(time.RFC3339), err, tc.note)
			}
			if !got.IsZero() {
				t.Fatalf("ParseDayDate(%s, %s) returned %s alongside the refusal; want the zero time",
					tc.day, tc.zone, got.Format(time.RFC3339))
			}
		})
	}
}

// TestParseDayDateResolvesADayWhoseMidnightIsSkipped is the other half of the
// same distinction: a missing MIDNIGHT is not a missing day. In a UTC-minus zone
// whose DST jump lands exactly on midnight the day still exists — it simply
// begins at the transition — so the parse must succeed and return that first
// existing instant. The list carries the two midnight-transition zones plus a
// positive-offset zone and UTC as controls, so a future tightening of the
// refusal that swallows an existing day is caught here.
func TestParseDayDateResolvesADayWhoseMidnightIsSkipped(t *testing.T) {
	t.Parallel()

	cases := []struct {
		zone string
		day  string
		note string
	}{
		{zone: "America/Santiago", day: "2026-09-06", note: "UTC-4 -> UTC-3 jump at 00:00"},
		{zone: "America/Havana", day: "2026-03-08", note: "UTC-5 -> UTC-4 jump at 00:00"},
		{zone: "Asia/Beirut", day: "2026-03-29", note: "control: positive offset, jump at 00:00"},
		{zone: "UTC", day: "2026-03-08", note: "control: no transitions"},
	}

	for _, tc := range cases {
		t.Run(tc.zone+" "+tc.day, func(t *testing.T) {
			t.Parallel()

			location, err := time.LoadLocation(tc.zone)
			if err != nil {
				t.Fatalf("load %s: %v", tc.zone, err)
			}

			parsed, err := ParseDayDate(tc.day, location)
			if err != nil {
				t.Fatalf("ParseDayDate(%s, %s): %v (%s)", tc.day, tc.zone, err, tc.note)
			}
			if got := parsed.Format("2006-01-02"); got != tc.day {
				t.Fatalf("ParseDayDate(%s, %s) = %s, want %s (%s)", tc.day, tc.zone, got, tc.day, tc.note)
			}
			// The returned instant is the day's FIRST existing instant: one
			// nanosecond earlier is already the previous calendar day.
			if got := parsed.Add(-time.Nanosecond).Format("2006-01-02"); got == tc.day {
				t.Fatalf("ParseDayDate(%s, %s) = %s is not the first instant of the day",
					tc.day, tc.zone, parsed.Format(time.RFC3339))
			}
		})
	}
}

// TestParseDayDateAcceptsExactlyTheDocumentedRangeInEveryZone pins both edges of
// DayDateMin..DayDateMax from each side, in zones on both sides of UTC: the bound
// is read off the parsed calendar components, so a zone's offset must move
// neither edge. Pacific/Kiritimati (UTC+14) would pull 1900-01-01 before the
// bound and Pacific/Pago_Pago (UTC-11) would push 9999-12-30 past it if the
// comparison ran on the resolved instant instead.
func TestParseDayDateAcceptsExactlyTheDocumentedRangeInEveryZone(t *testing.T) {
	t.Parallel()

	if DayDateMin != "1900-01-01" || DayDateMax != "9999-12-30" {
		t.Fatalf("accepted range is %s..%s, the documented range is 1900-01-01..9999-12-30", DayDateMin, DayDateMax)
	}

	cases := []struct {
		day      string
		accepted bool
	}{
		{day: "0001-01-01", accepted: false},
		{day: "1899-12-31", accepted: false},
		{day: "1900-01-01", accepted: true},
		{day: "1900-01-02", accepted: true},
		{day: "9999-12-29", accepted: true},
		{day: "9999-12-30", accepted: true},
		{day: "9999-12-31", accepted: false},
	}

	for _, zone := range []string{"UTC", "Pacific/Kiritimati", "America/Santiago", "Pacific/Pago_Pago"} {
		location, err := time.LoadLocation(zone)
		if err != nil {
			t.Fatalf("load %s: %v", zone, err)
		}
		for _, tc := range cases {
			got, err := ParseDayDate(tc.day, location)
			if !tc.accepted {
				if !errors.Is(err, ErrDayDateOutOfRange) {
					t.Errorf("ParseDayDate(%s, %s) = %s, %v; want ErrDayDateOutOfRange", tc.day, zone, got.Format(time.RFC3339), err)
				}
				if !got.IsZero() {
					t.Errorf("ParseDayDate(%s, %s) returned %s alongside the refusal; want the zero time", tc.day, zone, got.Format(time.RFC3339))
				}
				continue
			}
			if err != nil {
				t.Errorf("ParseDayDate(%s, %s): %v, want the day accepted", tc.day, zone, err)
				continue
			}
			if key := CalendarDayKey(got); key != tc.day {
				t.Errorf("ParseDayDate(%s, %s) = %q, want the same calendar day back", tc.day, zone, key)
			}
			// The upper edge is where the read range ends: the [start, end) bounds
			// of an accepted day must stay inside year 9999, or the range end is
			// unencodable and, stored as text, sorts before its own start.
			if _, end := DayRange(got, location); end.Year() > 9999 {
				t.Errorf("DayRange(%s, %s) ends in year %d, want an accepted day's range to stay encodable", tc.day, zone, end.Year())
			}
		}
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
