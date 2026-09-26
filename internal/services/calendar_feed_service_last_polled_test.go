package services

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"
)

// WEB-46: ResolveFeed records, at most once per owner calendar day, that a
// successful token-verified poll served the feed. These regressions pin the
// day the mark is written under (the OWNER's calendar day, never the request
// or server day), when the write is skipped outright, and that the write can
// never turn a success into a failure or a failure into a success. Cited by
// SECURITY.md's Calendar Feed Subscription rows.

// TestResolveFeedMarksThePollUnderTheOwnersDayAtUTCPlus14 is the east-of-UTC
// boundary: at 2026-03-10 23:00 UTC the owner (Pacific/Kiritimati, UTC+14) is
// already on 2026-03-11, a day the UTC clock has not reached yet.
func TestResolveFeedMarksThePollUnderTheOwnersDayAtUTCPlus14(t *testing.T) {
	owner, token := armedFeedUser(t, 71, "2026-02-25")
	owner.Timezone = "Pacific/Kiritimati"
	svc, users, _ := newFeedServiceForTest(owner, predictableFeedLogs(t))

	now := time.Date(2026, time.March, 10, 23, 0, 0, 0, time.UTC)
	if _, ok, err := svc.ResolveFeed(context.Background(), token, now, time.UTC); err != nil || !ok {
		t.Fatalf("expected the feed to resolve: ok=%v err=%v", ok, err)
	}

	if users.markCalls != 1 {
		t.Fatalf("expected exactly one mark write, got %d", users.markCalls)
	}
	if y, m, d := users.markedDay.Date(); y != 2026 || m != time.March || d != 11 {
		t.Fatalf("expected the mark under the owner's day 2026-03-11, got %04d-%02d-%02d", y, m, d)
	}
	if users.markedDay.Location() != time.UTC {
		t.Fatalf("expected the stored mark canonicalized to UTC midnight, got location %v", users.markedDay.Location())
	}
}

// TestResolveFeedMarksThePollUnderTheOwnersDayAtUTCMinus11 is the west-of-UTC
// boundary: at 2026-03-11 09:00 UTC the owner (Pacific/Pago_Pago, UTC-11) is
// still on 2026-03-10, a day the UTC clock has already left behind. This is
// the direction .In(loc) style shifts get wrong in view-layer rendering;
// here it pins that ResolveFeed's own day resolution
// (which the mark reuses verbatim) gets it right.
func TestResolveFeedMarksThePollUnderTheOwnersDayAtUTCMinus11(t *testing.T) {
	owner, token := armedFeedUser(t, 72, "2026-02-25")
	owner.Timezone = "Pacific/Pago_Pago"
	svc, users, _ := newFeedServiceForTest(owner, predictableFeedLogs(t))

	now := time.Date(2026, time.March, 11, 9, 0, 0, 0, time.UTC)
	if _, ok, err := svc.ResolveFeed(context.Background(), token, now, time.UTC); err != nil || !ok {
		t.Fatalf("expected the feed to resolve: ok=%v err=%v", ok, err)
	}

	if users.markCalls != 1 {
		t.Fatalf("expected exactly one mark write, got %d", users.markCalls)
	}
	if y, m, d := users.markedDay.Date(); y != 2026 || m != time.March || d != 10 {
		t.Fatalf("expected the mark under the owner's day 2026-03-10, got %04d-%02d-%02d", y, m, d)
	}
}

// TestResolveFeedSkipsTheMarkWriteWhenTheLoadedRowAlreadyHoldsToday pins
// that a repeated poll the same owner-day must not cost a
// DB write: the row loaded by FindByCalendarFeedSelector already carries
// today's date, so ResolveFeed must not even ATTEMPT the write.
func TestResolveFeedSkipsTheMarkWriteWhenTheLoadedRowAlreadyHoldsToday(t *testing.T) {
	owner, token := armedFeedUser(t, 73, "2026-02-25")
	today := time.Date(2026, time.March, 20, 0, 0, 0, 0, time.UTC)
	owner.CalendarFeedLastPolledOn = &today
	svc, users, _ := newFeedServiceForTest(owner, predictableFeedLogs(t))

	now := mustParseDashboardDay(t, "2026-03-20")
	if _, ok, err := svc.ResolveFeed(context.Background(), token, now, time.UTC); err != nil || !ok {
		t.Fatalf("expected the feed to resolve: ok=%v err=%v", ok, err)
	}
	if users.markCalls != 0 {
		t.Fatalf("expected zero mark writes when the loaded row already holds today, got %d", users.markCalls)
	}
}

// TestResolveFeedServesTheIdenticalFeedWhenTheMarkWriteFails proves the write
// is best-effort in both directions: ok, the body, and the error the caller
// sees are exactly the same whether the store's write succeeds or fails.
func TestResolveFeedServesTheIdenticalFeedWhenTheMarkWriteFails(t *testing.T) {
	user, token := armedFeedUser(t, 74, "2026-03-02")

	failingStore := &stubFeedUserStore{selector: user.CalendarFeedSelector, user: user, markErr: errors.New("simulated mark failure")}
	failingSvc := NewCalendarFeedService(failingStore, &stubFeedDayReader{logs: predictableFeedLogs(t)}, stubFeedDisclaimer{text: "estimates disclaimer"}, []byte(calendarFeedTestSecretKey))

	succeedingSvc, succeedingStore, _ := newFeedServiceForTest(user, predictableFeedLogs(t))

	now := mustParseDashboardDay(t, "2026-03-20")

	failingBody, failingOK, failingErr := failingSvc.ResolveFeed(context.Background(), token, now, time.UTC)
	succeedingBody, succeedingOK, succeedingErr := succeedingSvc.ResolveFeed(context.Background(), token, now, time.UTC)

	if failingErr != nil || succeedingErr != nil {
		t.Fatalf("expected nil errors, got failing=%v succeeding=%v", failingErr, succeedingErr)
	}
	if failingOK != succeedingOK || !failingOK {
		t.Fatalf("expected ok=true on both branches, got failing=%v succeeding=%v", failingOK, succeedingOK)
	}
	if string(failingBody) != string(succeedingBody) {
		t.Fatal("a failed mark write changed the served feed body")
	}
	if failingStore.markCalls != 1 || succeedingStore.markCalls != 1 {
		t.Fatalf("expected the mark write to be attempted on both branches, got failing=%d succeeding=%d", failingStore.markCalls, succeedingStore.markCalls)
	}
}

// TestResolveFeedMarksAPre032RowInTheSameRequestItBackfills proves the two
// writes a legacy row's first successful poll performs — the MAC backfill and
// the poll mark — both happen off the one request that verified it through
// bcrypt.
func TestResolveFeedMarksAPre032RowInTheSameRequestItBackfills(t *testing.T) {
	user, token := legacyArmedFeedUser(t, 75, "2026-03-02")
	svc, users, _ := newFeedServiceForTest(user, predictableFeedLogs(t))

	now := mustParseDashboardDay(t, "2026-03-20")
	if _, ok, err := svc.ResolveFeed(context.Background(), token, now, time.UTC); err != nil || !ok {
		t.Fatalf("expected the legacy row to resolve: ok=%v err=%v", ok, err)
	}
	if users.backfillCalls != 1 {
		t.Fatalf("expected exactly one MAC backfill, got %d", users.backfillCalls)
	}
	if users.markCalls != 1 {
		t.Fatalf("expected exactly one poll mark alongside the backfill, got %d", users.markCalls)
	}
}

// TestResolveFeedWritesNoMarkOnAnyFailureOrRefusalPath is the completeness
// sweep from the other side: the mark write must
// never be reached unless BuildCalendarFeedICS actually ran.
func TestResolveFeedWritesNoMarkOnAnyFailureOrRefusalPath(t *testing.T) {
	original := equalizeCalendarFeedTiming
	t.Cleanup(func() { equalizeCalendarFeedTiming = original })

	armed, validToken := armedFeedUser(t, 76, "2026-03-02")
	now := mustParseDashboardDay(t, "2026-03-20")
	selector, verifier := mustSplitFeedToken(t, validToken)

	t.Run("malformed token", func(t *testing.T) {
		equalizeCalendarFeedTiming = original
		svc, users, _ := newFeedServiceForTest(armed, predictableFeedLogs(t))
		if _, ok, err := svc.ResolveFeed(context.Background(), "TOOSHORT", now, time.UTC); err != nil || ok {
			t.Fatalf("expected ok=false, nil error, got ok=%v err=%v", ok, err)
		}
		if users.markCalls != 0 {
			t.Fatalf("expected zero marks, got %d", users.markCalls)
		}
	})

	t.Run("unknown selector (equalizer path)", func(t *testing.T) {
		var equalizeCalls int
		equalizeCalendarFeedTiming = func([]byte, string, string) { equalizeCalls++ }
		svc, users, _ := newFeedServiceForTest(armed, predictableFeedLogs(t))
		unknown := strings.Repeat("Z", len(selector)) + verifier
		if _, ok, err := svc.ResolveFeed(context.Background(), unknown, now, time.UTC); err != nil || ok {
			t.Fatalf("expected ok=false, nil error, got ok=%v err=%v", ok, err)
		}
		if equalizeCalls != 1 {
			t.Fatalf("expected the selector-miss path to run the equalizer once, got %d", equalizeCalls)
		}
		if users.markCalls != 0 {
			t.Fatalf("expected zero marks on the equalized selector-miss path, got %d", users.markCalls)
		}
	})

	t.Run("wrong verifier", func(t *testing.T) {
		equalizeCalendarFeedTiming = original
		svc, users, _ := newFeedServiceForTest(armed, predictableFeedLogs(t))
		wrong := selector + strings.Repeat("2", len(verifier))
		if _, ok, err := svc.ResolveFeed(context.Background(), wrong, now, time.UTC); err != nil || ok {
			t.Fatalf("expected ok=false, nil error, got ok=%v err=%v", ok, err)
		}
		if users.markCalls != 0 {
			t.Fatalf("expected zero marks, got %d", users.markCalls)
		}
	})

	t.Run("disabled feed (no selector stored)", func(t *testing.T) {
		equalizeCalendarFeedTiming = original
		disabled := armed
		disabled.CalendarFeedSelector = ""
		disabled.CalendarFeedVerifierHash = ""
		disabled.CalendarFeedVerifierMAC = ""
		users := &stubFeedUserStore{selector: "", user: disabled}
		svc := NewCalendarFeedService(users, &stubFeedDayReader{logs: predictableFeedLogs(t)}, stubFeedDisclaimer{text: "d"}, []byte(calendarFeedTestSecretKey))
		if _, ok, err := svc.ResolveFeed(context.Background(), validToken, now, time.UTC); err != nil || ok {
			t.Fatalf("expected ok=false, nil error, got ok=%v err=%v", ok, err)
		}
		if users.markCalls != 0 {
			t.Fatalf("expected zero marks, got %d", users.markCalls)
		}
	})

	t.Run("day-read (infrastructure) error", func(t *testing.T) {
		equalizeCalendarFeedTiming = original
		users := &stubFeedUserStore{selector: armed.CalendarFeedSelector, user: armed}
		days := &stubFeedDayReader{err: errors.New("simulated day-read failure")}
		svc := NewCalendarFeedService(users, days, stubFeedDisclaimer{text: "d"}, []byte(calendarFeedTestSecretKey))
		if _, ok, err := svc.ResolveFeed(context.Background(), validToken, now, time.UTC); err == nil || ok {
			t.Fatalf("expected an infrastructure error and ok=false, got ok=%v err=%v", ok, err)
		}
		if users.markCalls != 0 {
			t.Fatalf("expected zero marks when the day read fails, got %d", users.markCalls)
		}
	})
}
