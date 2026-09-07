package services

import (
	"testing"
	"time"

	"github.com/ovumcy/ovumcy-web/internal/models"
)

// This file is the regression for C07: before it, CycleStats carried no field
// that told a client whether LutealPhase came from the owner's own logs
// (InferUserLutealPhase's refined return) or from the defaultLutealPhaseDays
// model constant. ovulation_exact could not stand in for that signal — it
// reports the absence of CalcOvulationDay's arithmetic clamp, which is true on
// the 14-day default whenever that default happens to fit the cycle, and false
// on a personalised value the cycle was too short to hold.
//
// Repro (base df6cbcb7, before LutealPhasePersonalised existed):
// TestOvulationExactAloneCannotTellDefaultFromPersonalisedLutealPhase below
// built exactly this pair — a default-14 cycle that fits and a personalised-13
// cycle that also fits — and found both report OvulationExact=true, with no
// other field on CycleStats distinguishing them: `go test ./internal/services/
// -run TestOvulationExactAloneCannotTellDefaultFromPersonalisedLutealPhase -v`
// failed at the `stats.LutealPhase != 14` / personalised-mismatch assertion
// once LutealPhasePersonalised was referenced, because the field did not exist
// on CycleStats at that SHA (compile failure: undefined field
// LutealPhasePersonalised) — the same absence the fix closes.

// personalisedBaselineFixture is the shared fixture of this file: an owner with
// the 14-day setting, and logs whose ovulation signals are placed cycleLength
// days apart from origin so InferUserLutealPhase refines a value the setting
// does not hold. now sits five days into the logs' last (in-progress) cycle.
func personalisedBaselineFixture(t *testing.T, cycleLength int, ovulationCycleDays []int, kind lutealSignalKind) (*models.User, []models.DailyLog, time.Time) {
	t.Helper()
	origin := time.Date(2026, time.January, 1, 0, 0, 0, 0, time.UTC)
	logs := lutealRoundTripLogs(t, origin, cycleLength, ovulationCycleDays, kind)
	now := origin.AddDate(0, 0, len(ovulationCycleDays)*cycleLength+5).Add(9 * time.Hour)
	user := &models.User{
		Role:        models.RoleOwner,
		CycleLength: cycleLength,
		LutealPhase: 14,
	}
	return user, logs, now
}

// TestOvulationExactAloneCannotTellDefaultFromPersonalisedLutealPhase pins the
// invariant this task's fix depends on staying true: OvulationExact by itself
// must never be read as a personalisation signal, because a default-14 cycle
// that fits and a personalised-13 cycle that also fits report the identical
// OvulationExact=true. LutealPhasePersonalised is the field that tells them
// apart.
func TestOvulationExactAloneCannotTellDefaultFromPersonalisedLutealPhase(t *testing.T) {
	t.Parallel()

	// Default: no observed cycle history, so InferUserLutealPhase has nothing to
	// refine from and ApplyUserCycleBaseline falls back to the 14-day model
	// constant. A 28-day cycle absorbs it without a clamp.
	defaultLastPeriod := mustParseBaselineDay(t, "2026-02-01")
	defaultUser := &models.User{
		Role:            models.RoleOwner,
		CycleLength:     28,
		PeriodLength:    5,
		LastPeriodStart: &defaultLastPeriod,
	}
	defaultLogs := []models.DailyLog{
		{Date: defaultLastPeriod, IsPeriod: true, CycleStart: true, Flow: models.FlowMedium},
	}
	defaultNow := mustParseBaselineDay(t, "2026-02-10")
	defaultStats := ApplyUserCycleBaseline(defaultUser, defaultLogs, BuildCycleStats(defaultLogs, defaultNow), defaultNow, time.UTC)

	if defaultStats.LutealPhase != 14 {
		t.Fatalf("default fixture: stats.LutealPhase = %d, want 14 (the model default)", defaultStats.LutealPhase)
	}
	if !defaultStats.OvulationExact {
		t.Fatal("default fixture: stats.OvulationExact = false; the 14-day default fits a 28-day cycle without a clamp")
	}

	// Personalised: the round-trip fixture from cycle_luteal_round_trip_test.go,
	// which infers 13 from two observed cycles and also fits without a clamp.
	personalisedUser, personalisedLogs, personalisedNow := personalisedBaselineFixture(t, 28, []int{15, 15}, lutealSignalBBT)
	personalisedStats := ApplyUserCycleBaseline(personalisedUser, personalisedLogs, BuildCycleStats(personalisedLogs, personalisedNow), personalisedNow, time.UTC)

	if personalisedStats.LutealPhase != 13 {
		t.Fatalf("personalised fixture: stats.LutealPhase = %d, want 13 (the live inference)", personalisedStats.LutealPhase)
	}
	if !personalisedStats.OvulationExact {
		t.Fatal("personalised fixture: stats.OvulationExact = false; the inferred 13 fits a 28-day cycle without a clamp")
	}

	// The whole point: OvulationExact agrees on both, so it cannot be the signal
	// a client reads to tell a personalised cycle from a default one.
	if defaultStats.OvulationExact != personalisedStats.OvulationExact {
		t.Fatal("fixture invalid: this test needs OvulationExact identical on both sides to demonstrate it cannot distinguish them")
	}

	// LutealPhasePersonalised is the field that actually distinguishes them.
	if defaultStats.LutealPhasePersonalised {
		t.Error("default fixture: stats.LutealPhasePersonalised = true, want false: no observed cycle supported an inference")
	}
	if !personalisedStats.LutealPhasePersonalised {
		t.Error("personalised fixture: stats.LutealPhasePersonalised = false, want true: the live inference refined a value from the owner's own logs")
	}
}

// TestLutealPhasePersonalisedTrueButClamped is the third control the task
// asks for: a personalised luteal phase that CalcOvulationDay had to clamp.
// LutealPhasePersonalised and OvulationExact answer different questions, so
// this fixture must report personalised=true, exact=false — neither field may
// borrow the other's answer.
func TestLutealPhasePersonalisedTrueButClamped(t *testing.T) {
	t.Parallel()

	// Egg-white ovulation on cycle day 2 (the earliest the peak-day rule
	// admits) of a 22-day cycle infers luteal phase 20 — inside the inference's
	// own plausible window (10-20) and therefore accepted. Predicting the next
	// 22-day cycle needs maxSupportedLutealPhase = 22-5 = 17, so CalcOvulationDay
	// clamps 20 down to 17 and reports ovulationExact=false.
	user, logs, now := personalisedBaselineFixture(t, 22, []int{2, 2}, lutealSignalEggWhite)

	luteal, refined := InferUserLutealPhase(logs, time.UTC)
	if !refined {
		t.Fatal("fixture: InferUserLutealPhase declined to refine")
	}
	if luteal != 20 {
		t.Fatalf("fixture: inferred luteal phase = %d, want 20", luteal)
	}

	stats := ApplyUserCycleBaseline(user, logs, BuildCycleStats(logs, now), now, time.UTC)

	if !stats.LutealPhasePersonalised {
		t.Error("stats.LutealPhasePersonalised = false, want true: the value came from the owner's own logs")
	}
	if stats.OvulationExact {
		t.Error("stats.OvulationExact = true, want false: the inferred 20-day luteal phase does not fit a 22-day cycle and CalcOvulationDay had to clamp it")
	}
}

// TestPublishedStatsClearsLutealPhasePersonalisedUnderFertilitySuppression
// pins the suppression contract this task asks be decided explicitly:
// LutealPhasePersonalised must not keep asserting personalisation once the
// fertility tier has withheld the ovulation date and window it is claimed
// about.
func TestPublishedStatsClearsLutealPhasePersonalisedUnderFertilitySuppression(t *testing.T) {
	t.Parallel()

	user, logs, now := personalisedBaselineFixture(t, 28, []int{15, 15}, lutealSignalBBT)
	// Unpredictable-cycle mode is one of the PredictionsSuppressed disjuncts
	// that FertilityProjectionSuppressed folds in; any gate that reaches
	// suppression.FertilitySuppressed exercises the same clearing branch.
	user.UnpredictableCycle = true
	stats := ApplyUserCycleBaseline(user, logs, BuildCycleStats(logs, now), now, time.UTC)
	if !stats.LutealPhasePersonalised {
		t.Fatal("fixture: stats.LutealPhasePersonalised = false before publishing, want true (the inference must have refined)")
	}

	published, suppression := PublishedStats(user, stats, logs, DateAtLocation(now, time.UTC), time.UTC)
	if !suppression.FertilitySuppressed {
		t.Fatal("fixture: suppression.FertilitySuppressed = false, want true")
	}
	if published.LutealPhasePersonalised {
		t.Error("published.LutealPhasePersonalised = true under fertility suppression: it must not assert personalisation about an ovulation date this tier just withheld")
	}
}

// TestLutealPhasePersonalisedStaysFalseWhenTheInferredValueNeverLandsOnLastPeriodStart
// pins the gap between the two conditions the flag used to conflate.
// InferUserLutealPhase needs only ObservedCycleStarts, which accepts a period
// cluster with no CycleStart flag; ApplyUserCycleBaseline's projection needs an
// anchor — a flagged start or user.LastPeriodStart — and writes the inferred
// value into stats.LutealPhase only past that anchor. An owner who logs periods
// without ever flagging a start therefore gets a successful inference and no
// projection, and the flag must follow the projection, not the inference.
//
// Repro (6fb73c13, the PR's first commit): this test failed with
// `stats.LutealPhasePersonalised = true` beside `stats.LutealPhase = 14`.
func TestLutealPhasePersonalisedStaysFalseWhenTheInferredValueNeverLandsOnLastPeriodStart(t *testing.T) {
	t.Parallel()

	user, logs, now := personalisedBaselineFixture(t, 28, []int{15, 15}, lutealSignalBBT)
	for index := range logs {
		logs[index].CycleStart = false
	}

	luteal, refined := InferUserLutealPhase(logs, time.UTC)
	if !refined {
		t.Fatal("fixture: InferUserLutealPhase declined to refine from unflagged period clusters")
	}
	if luteal != 13 {
		t.Fatalf("fixture: inferred luteal phase = %d, want 13", luteal)
	}

	stats := ApplyUserCycleBaseline(user, logs, BuildCycleStats(logs, now), now, time.UTC)
	if !stats.LastPeriodStart.IsZero() {
		t.Fatalf("fixture: stats.LastPeriodStart = %s, want zero (no flagged start and no user.LastPeriodStart to anchor on)", stats.LastPeriodStart.Format("2006-01-02"))
	}
	if stats.LutealPhase == luteal {
		t.Fatalf("fixture: stats.LutealPhase = %d equals the inferred value; the test needs the projection to have been skipped", stats.LutealPhase)
	}
	if stats.LutealPhasePersonalised {
		t.Errorf("stats.LutealPhasePersonalised = true while stats.LutealPhase = %d, not the inferred %d: the flag claims a value that never landed", stats.LutealPhase, luteal)
	}
}
