package services

// day_service_mutation_test.go — gremlins mutation-survivor kill-tests for
// internal/services/day_service.go.
//
// Each test pins a specific surviving mutant on a specific line so the mutation
// is observably killed via a behavioral assertion. Helpers and stubs
// (newDayLogRepositoryStub, dayserviceCovNewService, dayserviceCovUserStub) and
// the package const defaultLutealPhaseDays are reused from existing test files
// in this package and are intentionally NOT redefined here.

import (
	"context"
	"testing"
	"time"

	"github.com/ovumcy/ovumcy-web/internal/models"
)

// Line 561 — autofill recent-period lookback anchors on the PREVIOUS day:
//
//	previousDay := dayStart.AddDate(0, 0, -1)
//
// Kills both the INVERT_NEGATIVES (-1 -> +1) and ARITHMETIC_BASE mutants on the
// day offset: the two proposed code blocks for this line are identical, so a
// single test function covers both. The exact previous day (and day-2, day-3)
// carry no period, so the recent-days lookback stays false and the
// previousEntry.IsPeriod term is decisive. dayStart itself and the FOLLOWING
// day are periods. Correct code looks at day-1 (empty) and returns true. A
// mutant that reads day+1 (or dayStart) sees a period and wrongly returns false.
func TestDayService_ShouldAutoFill_UsesPreviousDayNotFollowingDay(t *testing.T) {
	logs := newDayLogRepositoryStub()
	service := dayserviceCovNewService(logs, &dayserviceCovUserStub{})
	day := time.Date(2026, time.February, 10, 0, 0, 0, 0, time.UTC)

	// The exact previous day (and day-2, day-3) carry no period, so the
	// recent-days lookback stays false and the previousEntry.IsPeriod term is
	// decisive. dayStart itself and the FOLLOWING day are periods. Correct code
	// looks at day-1 (empty) and returns true. A mutant that reads day+1 (or
	// dayStart) sees a period and wrongly returns false.
	logs.entries["2026-02-10"] = models.DailyLog{ID: 1, UserID: 10, Date: day, IsPeriod: true}
	logs.entries["2026-02-11"] = models.DailyLog{ID: 2, UserID: 10, Date: day.AddDate(0, 0, 1), IsPeriod: true}

	should, err := service.ShouldAutoFillPeriodDays(context.Background(), 10, day, false, true, 5, time.UTC)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !should {
		t.Fatal("expected true: the previous day (day-1) is empty, so autofill should proceed; a wrong-day offset reads a period and returns false")
	}
}

// Line 666 — clearCompetingCycleStarts must demote EVERY competing cycle start
// in the cluster, not stop after the first successful Save:
//
//	if err := service.logs.Save(ctx, &logEntry); err != nil { return err }
//
// Kills the CONDITIONALS_NEGATION mutant (err != nil -> err == nil), which would
// return after the first successful Save and leave later competing starts set.
func TestDayService_ClearCompetingCycleStarts_ClearsAllCompetingStartsNotJustFirst(t *testing.T) {
	logs := newDayLogRepositoryStub()
	service := dayserviceCovNewService(logs, &dayserviceCovUserStub{})

	// Three contiguous period days (gap 1 day each) form a single cluster
	// [03-08 .. 03-10]. 03-08 and 03-09 are competing explicit cycle starts;
	// 03-10 is the selected start. Correct code demotes BOTH 03-08 and 03-09.
	// A mutant that returns after the first successful Save leaves the second
	// competing start (03-09) still flagged CycleStart=true.
	d08 := time.Date(2026, time.March, 8, 0, 0, 0, 0, time.UTC)
	d09 := time.Date(2026, time.March, 9, 0, 0, 0, 0, time.UTC)
	d10 := time.Date(2026, time.March, 10, 0, 0, 0, 0, time.UTC)

	logs.entries["2026-03-08"] = models.DailyLog{ID: 1, UserID: 10, Date: d08, IsPeriod: true, CycleStart: true}
	logs.entries["2026-03-09"] = models.DailyLog{ID: 2, UserID: 10, Date: d09, IsPeriod: true, CycleStart: true}
	logs.entries["2026-03-10"] = models.DailyLog{ID: 3, UserID: 10, Date: d10, IsPeriod: true, CycleStart: true}

	selected := logs.entries["2026-03-10"]
	allLogs := []models.DailyLog{logs.entries["2026-03-08"], logs.entries["2026-03-09"], logs.entries["2026-03-10"]}
	if err := service.clearCompetingCycleStarts(context.Background(), 10, allLogs, selected, time.UTC); err != nil {
		t.Fatalf("clearCompetingCycleStarts: %v", err)
	}

	if logs.entries["2026-03-08"].CycleStart {
		t.Fatal("expected first competing cycle start (03-08) demoted")
	}
	if logs.entries["2026-03-09"].CycleStart {
		t.Fatal("expected SECOND competing cycle start (03-09) demoted; a mutant returning after the first Save leaves it set")
	}
	if !logs.entries["2026-03-10"].CycleStart {
		t.Fatal("the selected cycle start (03-10) must remain set")
	}
}

// Lines 675 and 680 — refreshDerivedCycleSettings must run past its nil/error
// guards and persist the resolved luteal phase after an upsert:
//
//	675: if service == nil || service.users == nil || service.logs == nil { return }
//	680: logs, err := service.logs.ListByUser(...); if err != nil { return }
//
// Kills the CONDITIONALS_NEGATION mutants on both guards: the two proposed code
// blocks (line 675 and line 680) are identical, so a single test function covers
// both. With no cycle history InferUserLutealPhase returns (14, false) and the
// service must persist luteal_phase=14 via UpdateByID at the end of the upsert.
func TestDayService_RefreshDerivedCycleSettings_WritesLutealPhaseAfterUpsert(t *testing.T) {
	logs := newDayLogRepositoryStub()
	// Stored luteal phase starts at 0 so the write is observable: with no
	// cycle history InferUserLutealPhase returns (14, false) and the service
	// must persist luteal_phase=14 via UpdateByID at the end of the upsert.
	users := &dayserviceCovUserStub{settings: models.User{PeriodLength: 5, LutealPhase: 0}}
	service := dayserviceCovNewService(logs, users)

	_, err := service.UpsertDayEntryWithAutoFillAt(context.Background(),
		10,
		time.Date(2026, time.February, 10, 0, 0, 0, 0, time.UTC),
		DayEntryInput{IsPeriod: false, Flow: models.FlowNone},
		time.Date(2026, time.February, 10, 8, 0, 0, 0, time.UTC),
		time.UTC,
	)
	if err != nil {
		t.Fatalf("UpsertDayEntryWithAutoFillAt: unexpected error: %v", err)
	}

	// refreshDerivedCycleSettings must have run past the nil guard and the
	// ListByUser success check and written the resolved default luteal phase.
	// A mutated nil-guard that returns early for a non-nil service, or a
	// mutated success check (`err == nil` -> early return), skips the write
	// and leaves the stored value at 0.
	if users.settings.LutealPhase != defaultLutealPhaseDays {
		t.Fatalf("expected luteal_phase persisted as %d after upsert, got %d (refresh must not be skipped)", defaultLutealPhaseDays, users.settings.LutealPhase)
	}
}

// TestDayService_RefreshDerivedCycleSettings_BoundsAtOwnerZoneNotRequestZone
// is SEC-M14 (WEB-15): the persisted users.luteal_phase cache must be
// recomputed at the OWNER's stored timezone, never the request's
// (header/cookie) zone, because the boot recompute (LutealPhaseRecomputer)
// reads the same column through resolveOwnerLocation's owner-zone
// preference with no request to agree with — a write bounded at the
// request's zone can disagree with the boot pass on any day the two zones
// name a different date.
//
// Fixture: two BBT-confirmed cycles (Jan1->Jan20, 19d, luteal 13; Jan20->Feb8,
// 19d, luteal 12 — same coverline/rise shape as
// TestCycleSignals_InferUserLutealPhase_UnchangedByDSTTransitionInCycle)
// plus a THIRD observed cycle start on Feb 8, the boundary log:
// InferUserLutealPhase needs 3 starts (2 completed cycles) to refine at all,
// so whether Feb 8 counts as "observed yet" decides refined (13, true) vs
// the unrefined default (14, false).
//
// now = 2026-02-08T00:30 UTC. The request's zone (UTC, simulated by
// `requestLocation` below, and normally the fallback resolveOwnerLocation
// falls back to when an owner has none) already reads today as Feb 8, the
// boundary log's own date. The owner's stored zone, Pacific/Midway
// (UTC-11), reads local time as 2026-02-07T13:30 — today is still Feb 7,
// one calendar day before the boundary log, so the owner has not reached
// that day yet and the derivation must not use it.
func TestDayService_RefreshDerivedCycleSettings_BoundsAtOwnerZoneNotRequestZone(t *testing.T) {
	logs := newDayLogRepositoryStub()
	day := func(s string) time.Time {
		parsed, err := time.Parse("2006-01-02", s)
		if err != nil {
			t.Fatalf("parse %q: %v", s, err)
		}
		return parsed
	}
	bbt := func(v float64) *float64 { return &v }
	seed := func(dateStr string, entry models.DailyLog) {
		entry.UserID = 42
		entry.Date = day(dateStr)
		logs.entries[dateStr] = entry
	}

	// Cycle 1 (Jan1 -> Jan20, 19d): coverline Jan1-6 @36.20, rise Jan7-9
	// @36.50 -> ovulation Jan6 (cycle day 6) -> luteal 19-6=13.
	seed("2026-01-01", models.DailyLog{IsPeriod: true, CycleStart: true, Flow: models.FlowMedium, BBT: bbt(36.20)})
	seed("2026-01-02", models.DailyLog{BBT: bbt(36.20)})
	seed("2026-01-03", models.DailyLog{BBT: bbt(36.20)})
	seed("2026-01-04", models.DailyLog{BBT: bbt(36.20)})
	seed("2026-01-05", models.DailyLog{BBT: bbt(36.20)})
	seed("2026-01-06", models.DailyLog{BBT: bbt(36.20)})
	seed("2026-01-07", models.DailyLog{BBT: bbt(36.50)})
	seed("2026-01-08", models.DailyLog{BBT: bbt(36.50)})
	seed("2026-01-09", models.DailyLog{BBT: bbt(36.50)})

	// Cycle 2 (Jan20 -> Feb8, 19d): coverline Jan20-25 @36.20, rise
	// Jan27-29 @36.50 -> ovulation Jan26 (cycle day 7) -> luteal 19-7=12.
	seed("2026-01-20", models.DailyLog{IsPeriod: true, CycleStart: true, Flow: models.FlowMedium, BBT: bbt(36.20)})
	seed("2026-01-21", models.DailyLog{BBT: bbt(36.20)})
	seed("2026-01-22", models.DailyLog{BBT: bbt(36.20)})
	seed("2026-01-23", models.DailyLog{BBT: bbt(36.20)})
	seed("2026-01-24", models.DailyLog{BBT: bbt(36.20)})
	seed("2026-01-25", models.DailyLog{BBT: bbt(36.20)})
	seed("2026-01-27", models.DailyLog{BBT: bbt(36.50)})
	seed("2026-01-28", models.DailyLog{BBT: bbt(36.50)})
	seed("2026-01-29", models.DailyLog{BBT: bbt(36.50)})

	// The boundary log: the third observed cycle start, with no BBT of its
	// own — it only needs to exist or not exist in the "observed" set.
	seed("2026-02-08", models.DailyLog{IsPeriod: true, CycleStart: true, Flow: models.FlowMedium})

	users := &dayserviceCovUserStub{settings: models.User{Timezone: "Pacific/Midway"}}
	service := dayserviceCovNewService(logs, users)

	now := time.Date(2026, time.February, 8, 0, 30, 0, 0, time.UTC)
	requestLocation := time.UTC

	// The day being edited is unrelated to the cycle-start chain, so the
	// write cannot disturb the seeded fixture above.
	if _, err := service.UpsertDayEntryWithAutoFillAt(context.Background(),
		42,
		day("2026-01-15"),
		DayEntryInput{IsPeriod: false, Flow: models.FlowNone},
		now,
		requestLocation,
	); err != nil {
		t.Fatalf("UpsertDayEntryWithAutoFillAt: unexpected error: %v", err)
	}

	// Owner-zone bound (Feb 7): the Feb 8 cycle start is not yet observed,
	// so only two starts exist and the inference stays unrefined at the
	// default. A regression that bounds this at requestLocation instead
	// (UTC, today=Feb 8) counts the boundary start and persists 13.
	if users.settings.LutealPhase != defaultLutealPhaseDays {
		t.Fatalf("expected luteal_phase bounded at the OWNER's zone (Pacific/Midway, today=Feb 7) to stay the unrefined default %d; got %d — a request-zone bound (UTC, today=Feb 8) would count the not-yet-owner-observed Feb 8 cycle start and refine it to 13", defaultLutealPhaseDays, users.settings.LutealPhase)
	}
}
