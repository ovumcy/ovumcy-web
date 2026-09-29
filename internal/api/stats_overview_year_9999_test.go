package api

import (
	"context"
	"encoding/json"
	"regexp"
	"strings"
	"testing"
	"time"

	"github.com/ovumcy/ovumcy-web/internal/models"
	"github.com/ovumcy/ovumcy-web/internal/services"
	"gorm.io/gorm"
)

// WEB-108: `format: date` has a four-digit year, so a projection that would fall
// after 9999-12-31 is answered null, exactly like a withheld one — never as a
// five-digit year and never as a 500 from an encoder that refuses it.

// fiveDigitYearDate matches a date spelled with a year past 9999, in the JSON
// payload or in an .ics DATE value.
var fiveDigitYearDate = regexp.MustCompile(`"\d{5,}-\d{2}-\d{2}"|VALUE=DATE:\d{9,}`)

func TestStatsOverviewDateIsNullPastYear9999(t *testing.T) {
	t.Parallel()
	day := func(year int, month time.Month, dayOfMonth int) time.Time {
		return time.Date(year, month, dayOfMonth, 0, 0, 0, 0, time.UTC)
	}
	response := newStatsOverviewResponse(services.CycleStats{
		LastPeriodStart:      day(9999, 12, 20),
		NextPeriodStart:      day(10000, 1, 17),
		OvulationDate:        day(10000, 1, 2),
		FertilityWindowStart: day(9999, 12, 31),
		FertilityWindowEnd:   day(10000, 1, 2),
	}, services.PredictionSuppression{}, false, "")

	// The window's start, 9999-12-31, is a spellable date, but its end is not:
	// the pair is null as a whole, never a start without its end.
	for field, value := range map[string]*string{
		"next_period_start":      response.NextPeriodStart,
		"ovulation_date":         response.OvulationDate,
		"fertility_window_start": response.FertilityWindowStart,
		"fertility_window_end":   response.FertilityWindowEnd,
	} {
		if value != nil {
			t.Fatalf("%s = %q, want null for a date or window past 9999-12-31", field, *value)
		}
	}
	if response.LastPeriodStart == nil || *response.LastPeriodStart != "9999-12-20" {
		t.Fatalf("last_period_start = %v, want 9999-12-20", response.LastPeriodStart)
	}
}

// The fertile window's two ends are published together or not at all, whichever
// end a producer failed to clear; a window ending on 9999-12-31 is still a date.
func TestStatsOverviewFertilityWindowIsPublishedAsAPair(t *testing.T) {
	t.Parallel()
	day := func(year int, month time.Month, dayOfMonth int) time.Time {
		return time.Date(year, month, dayOfMonth, 0, 0, 0, 0, time.UTC)
	}
	for _, tc := range []struct {
		name       string
		start, end time.Time
		wantStart  string
		wantEnd    string
	}{
		{name: "end past the year", start: day(9999, 12, 31), end: day(10000, 1, 2)},
		{name: "end absent", start: day(9999, 12, 26)},
		{name: "start absent", end: day(9999, 12, 31)},
		{name: "last day of the year", start: day(9999, 12, 26), end: day(9999, 12, 31), wantStart: "9999-12-26", wantEnd: "9999-12-31"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			response := newStatsOverviewResponse(services.CycleStats{
				FertilityWindowStart: tc.start,
				FertilityWindowEnd:   tc.end,
			}, services.PredictionSuppression{}, false, "")
			got := func(value *string) string {
				if value == nil {
					return ""
				}
				return *value
			}
			if got(response.FertilityWindowStart) != tc.wantStart || got(response.FertilityWindowEnd) != tc.wantEnd {
				t.Fatalf("fertility window = %q/%q, want %q/%q", got(response.FertilityWindowStart), got(response.FertilityWindowEnd), tc.wantStart, tc.wantEnd)
			}
		})
	}
}

// The handler's own chain — derivation, the publishing adapter, the DTO — run
// with the owner's today on the last accepted day, over in-memory logs.
func TestStatsOverviewAnswersProjectionsPastYear9999AsNull(t *testing.T) {
	t.Parallel()
	now := time.Date(9999, 12, 30, 12, 0, 0, 0, time.UTC)
	today := services.DateAtLocation(now, time.UTC)
	owner := &models.User{ID: 7, Role: models.RoleOwner, CycleLength: 28, PeriodLength: 5, LutealPhase: 14}

	for _, tc := range []struct {
		name          string
		lastStart     time.Time
		wantOvulation string
		wantFertility string
	}{
		{name: "whole projection past the year", lastStart: time.Date(9999, 12, 20, 0, 0, 0, 0, time.UTC), wantFertility: "unknown"},
		{name: "ovulation inside the year", lastStart: time.Date(9999, 12, 10, 0, 0, 0, 0, time.UTC), wantOvulation: "9999-12-23", wantFertility: "not_fertile"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			logs := make([]models.DailyLog, 0, 3)
			for _, back := range []int{56, 28, 0} {
				logs = append(logs, models.DailyLog{Date: tc.lastStart.AddDate(0, 0, -back), IsPeriod: true, CycleStart: true})
			}

			stats := services.BuildCycleStatsFromLogs(owner, logs, now, time.UTC)
			published, suppression, confirmed := services.PublishedOverviewStats(owner, logs, stats, today, time.UTC)
			payload := newStatsOverviewResponse(published, suppression, confirmed, "")
			body, err := json.Marshal(payload)
			if err != nil {
				t.Fatalf("encode stats overview: %v", err)
			}

			// Nothing withholds this projection, so a null below comes from the
			// year and not from a gate.
			if payload.Suppression.Predictions || payload.Suppression.Fertility {
				t.Fatalf("precondition: suppression = %+v, want none", payload.Suppression)
			}
			if payload.NextPeriodStart != nil {
				t.Fatalf("next_period_start = %q, want null past 9999-12-31", *payload.NextPeriodStart)
			}
			if payload.OvulationImpossible {
				t.Fatal("ovulation_impossible = true for a window that is only unnamed")
			}
			// current_fertility is unknown beside a null window even though
			// suppression.fertility is false, as docs/openapi.yaml says.
			if payload.CurrentFertility != tc.wantFertility {
				t.Fatalf("current_fertility = %q, want %q", payload.CurrentFertility, tc.wantFertility)
			}
			if tc.wantOvulation == "" {
				if payload.OvulationDate != nil || payload.FertilityWindowStart != nil || payload.FertilityWindowEnd != nil {
					t.Fatalf("window = %v/%v/%v, want null as a whole", payload.OvulationDate, payload.FertilityWindowStart, payload.FertilityWindowEnd)
				}
				if payload.CurrentPhase != "menstrual" && payload.CurrentPhase != "unknown" {
					t.Fatalf("current_phase = %q beside a null ovulation_date", payload.CurrentPhase)
				}
			} else if payload.OvulationDate == nil || *payload.OvulationDate != tc.wantOvulation {
				t.Fatalf("ovulation_date = %v, want %s", payload.OvulationDate, tc.wantOvulation)
			}
			if match := fiveDigitYearDate.FindString(string(body)); match != "" {
				t.Fatalf("payload spells %s:\n%s", match, body)
			}
		})
	}
}

// seedLateYear9999Cycles records three five-day periods 28 days apart, the last
// one starting on lastStart.
func seedLateYear9999Cycles(t *testing.T, database *gorm.DB, userID uint, lastStart time.Time) {
	t.Helper()
	for _, back := range []int{56, 28, 0} {
		start := lastStart.AddDate(0, 0, -back)
		for offset := range 5 {
			seedStatsOverviewLog(t, database, models.DailyLog{UserID: userID, Date: start.AddDate(0, 0, offset), IsPeriod: true, Flow: models.FlowMedium, CycleStart: offset == 0})
		}
	}
}

// GET /api/v1/stats/overview and the feed route read the wall clock and have no
// seam to set it, so no HTTP request can meet a late-9999 today. This runs what
// those two handlers run — the handler's own bootstrap-built services over a
// seeded database, then the overview's publishing adapter and DTO, encoded as
// c.JSON encodes it — at the last accepted day.
func TestOverviewAndFeedReadPathsAnswerProjectionsPastYear9999AsAbsent(t *testing.T) {
	_, database := newOnboardingTestApp(t)
	handler := mustFeedHandler(t, database)
	ctx := context.Background()

	owner := createOnboardingTestUser(t, database, "overview-year-9999@example.com", "StrongPass1", true)
	seedLateYear9999Cycles(t, database, owner.ID, time.Date(9999, 12, 20, 0, 0, 0, 0, time.UTC))
	now := time.Date(9999, 12, 30, 12, 0, 0, 0, time.UTC)

	stats, logs, err := handler.statsService.BuildOverviewStats(ctx, &owner, now, time.UTC)
	if err != nil {
		t.Fatalf("build overview stats: %v", err)
	}
	published, suppression, confirmed := services.PublishedOverviewStats(&owner, logs, stats, services.DateAtLocation(now, time.UTC), time.UTC)
	payload := newStatsOverviewResponse(published, suppression, confirmed, "")
	body, err := json.Marshal(payload)
	if err != nil {
		t.Fatalf("encode stats overview: %v", err)
	}
	if payload.LastPeriodStart == nil || *payload.LastPeriodStart != "9999-12-20" {
		t.Fatalf("precondition: last_period_start = %v, want 9999-12-20 read from the database:\n%s", payload.LastPeriodStart, body)
	}
	if payload.Suppression.Predictions || payload.Suppression.Fertility {
		t.Fatalf("precondition: suppression = %+v, want none", payload.Suppression)
	}
	for field, value := range map[string]*string{
		"next_period_start":      payload.NextPeriodStart,
		"ovulation_date":         payload.OvulationDate,
		"fertility_window_start": payload.FertilityWindowStart,
		"fertility_window_end":   payload.FertilityWindowEnd,
	} {
		if value != nil {
			t.Fatalf("%s = %q, want null past 9999-12-31:\n%s", field, *value, body)
		}
	}
	if match := fiveDigitYearDate.FindString(string(body)); match != "" {
		t.Fatalf("payload spells %s:\n%s", match, body)
	}

	// The feed: the same owner at the same today names no projected event,
	// while a control owner whose next start is 9999-12-30 still gets that one,
	// so the empty feed is the year and not an unread history.
	for _, tc := range []struct {
		email     string
		lastStart time.Time
		now       time.Time
		wantStart string
	}{
		{email: "feed-year-9999@example.com", lastStart: time.Date(9999, 12, 20, 0, 0, 0, 0, time.UTC), now: now},
		{email: "feed-year-9999-control@example.com", lastStart: time.Date(9999, 12, 2, 0, 0, 0, 0, time.UTC), now: time.Date(9999, 12, 29, 12, 0, 0, 0, time.UTC), wantStart: "99991230"},
	} {
		feedOwner := createOnboardingTestUser(t, database, tc.email, "StrongPass1", true)
		seedLateYear9999Cycles(t, database, feedOwner.ID, tc.lastStart)
		token := armCalendarFeedForUser(t, database, feedOwner.ID)

		feedBody, ok, err := handler.calendarFeedService.ResolveFeed(ctx, token, tc.now, time.UTC)
		if err != nil || !ok {
			t.Fatalf("%s: resolve feed ok=%v err=%v", tc.email, ok, err)
		}
		feed := string(feedBody)
		if match := fiveDigitYearDate.FindString(feed); match != "" {
			t.Fatalf("%s: feed spells %s:\n%s", tc.email, match, feed)
		}
		if tc.wantStart == "" {
			if strings.Contains(feed, "BEGIN:VEVENT") {
				t.Fatalf("%s: want no event past 9999-12-31:\n%s", tc.email, feed)
			}
			continue
		}
		if !strings.Contains(feed, "DTSTART;VALUE=DATE:"+tc.wantStart+"\r\n") {
			t.Fatalf("%s: want the event on %s:\n%s", tc.email, tc.wantStart, feed)
		}
	}
}
