package services

import (
	"context"
	"testing"
	"time"

	"github.com/ovumcy/ovumcy-web/internal/db"
)

// TestSettingsStartMoveKeepsAnOldStartWhoseNextDayTheOwnerTouched: onboarding
// on 10-03 with auto-fill wrote 10-03..10-07, and the owner edited 10-04. The
// walk from the old start stops at the touched day, so the fill it can see is
// the old start's one row — and one row is no cohort: a lone bare row cannot be
// told apart from a hand tick, so the move clears nothing, the old start's row
// included (oldStartFillRun).
func TestSettingsStartMoveKeepsAnOldStartWhoseNextDayTheOwnerTouched(t *testing.T) {
	touched := startMoveDay(time.October, 4)
	_, logs := startMoveFixture(t, "start-move-touched-next-day@example.com", func(t *testing.T, repositories *db.Repositories, userID uint) {
		t.Helper()
		entry, found, err := repositories.DailyLogs.FindByUserAndDayRange(context.Background(), userID, touched, touched.AddDate(0, 0, 1))
		if err != nil || !found {
			t.Fatalf("fixture: onboarding wrote no 10-04 row (found=%v err=%v)", found, err)
		}
		entry.Mood = 3
		if err := repositories.DailyLogs.Save(context.Background(), &entry); err != nil {
			t.Fatalf("touch 10-04: %v", err)
		}
	})

	if oldStart, found := startMoveRowOn(logs, "2026-10-03"); !found || !oldStart.IsPeriod {
		t.Fatalf("the old start's fill row 2026-10-03 was cleared on a one-row cohort (logged %v)", startMoveLoggedDays(logs))
	}
	if kept, found := startMoveRowOn(logs, "2026-10-04"); !found || kept.Mood != 3 || !kept.IsPeriod {
		t.Fatalf("the touched day 2026-10-04 did not survive as the owner left it: %+v (found=%v)", kept, found)
	}
	for _, day := range []string{"2026-10-05", "2026-10-06", "2026-10-07"} {
		if !startMoveHasPeriodDay(logs, day) {
			t.Errorf("fill day %s past the touched one was deleted", day)
		}
	}
}
