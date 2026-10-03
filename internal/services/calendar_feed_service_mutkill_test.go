package services

import (
	"context"
	"testing"
	"time"

	"github.com/ovumcy/ovumcy-web/internal/models"
)

// mutkillFeedDayReader captures the FULL requested log-window [from, to] so a
// test can assert the lower bound, which the shared stubFeedDayReader discards
// (it takes `from` as `_`).
type mutkillFeedDayReader struct {
	logs          []models.DailyLog
	requestedFrom time.Time
	requestedTo   time.Time
	calls         int
}

func (r *mutkillFeedDayReader) FetchLogsForUser(_ context.Context, _ uint, from time.Time, to time.Time, _ *time.Location) ([]models.DailyLog, error) {
	r.calls++
	r.requestedFrom = from
	r.requestedTo = to
	return r.logs, nil
}

// TestResolveFeedLoadsHistoryWindowBeforeToday kills the INVERT_NEGATIVES and
// ARITHMETIC_BASE survivors on the year offset of the history window the feed
// requests. That offset used to be the feed's own arithmetic in
// calendar_feed_service.go and now lives in StatsOverviewRange (stats_service.go):
//
//	return now.AddDate(-statsOverviewWindowYears, 0, 0), now
//
// ResolveFeed reads it with `from, to := StatsOverviewRange(today)`, so the
// mutated line is the one in StatsOverviewRange, not in the feed. Both mutants
// flip the sign of the year offset, so `from` lands two years in the FUTURE,
// producing an inverted [today+2y, today] window that would load NONE of the
// owner's cycle history and silently degrade every prediction the feed emits.
// The shared day-reader stub ignores `from`, so no existing test observed the
// lower bound — which is why both survived. The request is also compared with
// StatsOverviewRange's own answer: a feed that went back to its own arithmetic
// would pass the span check and still fail the second comparison the day the
// window rule changes.
func TestResolveFeedLoadsHistoryWindowBeforeToday(t *testing.T) {
	user, token := armedFeedUser(t, 77, "2026-03-02")
	days := &mutkillFeedDayReader{logs: predictableFeedLogs(t)}
	svc := NewCalendarFeedService(
		&stubFeedUserStore{selector: user.CalendarFeedSelector, user: user},
		days,
		stubFeedDisclaimer{text: "d"},
		[]byte(calendarFeedTestSecretKey),
	)

	now := mustParseDashboardDay(t, "2026-03-20")
	if _, ok, err := svc.ResolveFeed(context.Background(), token, now, time.UTC); err != nil || !ok {
		t.Fatalf("expected the feed to resolve: ok=%v err=%v", ok, err)
	}
	if days.calls != 1 {
		t.Fatalf("expected exactly one owner-scoped log read, got %d", days.calls)
	}

	// The upper bound is the owner's today; the lower bound must be exactly
	// statsOverviewWindowYears BEFORE it — in the past, never after.
	if !days.requestedTo.Equal(now) {
		t.Fatalf("log-window upper bound = %s, want the owner's today %s",
			days.requestedTo.Format("2006-01-02"), now.Format("2006-01-02"))
	}
	wantFrom := days.requestedTo.AddDate(-statsOverviewWindowYears, 0, 0)
	if !days.requestedFrom.Equal(wantFrom) {
		t.Fatalf("log-window lower bound = %s, want %s (%d years before today %s)",
			days.requestedFrom.Format("2006-01-02"), wantFrom.Format("2006-01-02"),
			statsOverviewWindowYears, days.requestedTo.Format("2006-01-02"))
	}
	statsFrom, statsTo := StatsOverviewRange(days.requestedTo)
	if !days.requestedFrom.Equal(statsFrom) || !days.requestedTo.Equal(statsTo) {
		t.Fatalf("log window = [%s, %s], want StatsOverviewRange's [%s, %s]",
			days.requestedFrom.Format("2006-01-02"), days.requestedTo.Format("2006-01-02"),
			statsFrom.Format("2006-01-02"), statsTo.Format("2006-01-02"))
	}
	if !days.requestedFrom.Before(days.requestedTo) {
		t.Fatalf("log-window lower bound %s must precede the upper bound %s",
			days.requestedFrom.Format("2006-01-02"), days.requestedTo.Format("2006-01-02"))
	}
}
