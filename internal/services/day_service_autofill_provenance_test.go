package services

import (
	"context"
	"testing"
	"time"

	"github.com/ovumcy/ovumcy-web/internal/models"
)

// WEB-282: the un-tick walk reads which days the fill wrote rather than
// guessing it from the anchor's flow, so neither heuristic gap erases or
// strands a day.

type provenanceStep struct {
	name   string
	offset int
	input  DayEntryInput
}

func runProvenanceSteps(t *testing.T, steps []provenanceStep) *dayLogRepositoryStub {
	t.Helper()
	logs := newDayLogRepositoryStub()
	service := NewDayService(logs, &dayUserRepositoryStub{settings: models.User{PeriodLength: 5, AutoPeriodFill: true}})
	anchor := time.Date(2026, time.September, 7, 0, 0, 0, 0, time.UTC)
	now := anchor.AddDate(0, 0, 14)
	for _, step := range steps {
		if _, err := service.UpsertDayEntryWithAutoFillAt(context.Background(), 10, anchor.AddDate(0, 0, step.offset), step.input, now, time.UTC); err != nil {
			t.Fatalf("%s: %v", step.name, err)
		}
	}
	return logs
}

// dropAutoFillProvenance turns the named stub rows into rows that predate
// migration 042, which carry no mark.
func dropAutoFillProvenance(logs *dayLogRepositoryStub, keys ...string) {
	for _, key := range keys {
		entry := logs.entries[key]
		entry.AutoFilled = nil
		logs.entries[key] = entry
	}
}

func TestAutoFillMarksTheDaysItWritesAndAnOwnerWriteUnmarksThem(t *testing.T) {
	logs := runProvenanceSteps(t, []provenanceStep{
		{name: "log the anchor light", input: DayEntryInput{IsPeriod: true, Flow: models.FlowLight}},
		{name: "re-save the second day unchanged", offset: 1, input: DayEntryInput{IsPeriod: true, Flow: models.FlowLight}},
	})

	for key, want := range map[string]bool{"2026-09-07": false, "2026-09-08": false, "2026-09-09": true, "2026-09-10": true, "2026-09-11": true} {
		got := logs.entries[key].AutoFilled
		if got == nil || *got != want {
			t.Fatalf("%s: want AutoFilled=%t, got %v", key, want, got)
		}
	}
}

func TestUncheckingAnchorFallsBackToTheFlowHeuristicForUnmarkedRows(t *testing.T) {
	logs := newDayLogRepositoryStub()
	service := NewDayService(logs, &dayUserRepositoryStub{settings: models.User{PeriodLength: 5, AutoPeriodFill: true}})
	anchor := time.Date(2026, time.September, 7, 0, 0, 0, 0, time.UTC)
	now := anchor.AddDate(0, 0, 14)
	ctx := context.Background()
	if _, err := service.UpsertDayEntryWithAutoFillAt(ctx, 10, anchor, DayEntryInput{IsPeriod: true, Flow: models.FlowLight}, now, time.UTC); err != nil {
		t.Fatalf("log the anchor: %v", err)
	}
	dropAutoFillProvenance(logs, "2026-09-08", "2026-09-09", "2026-09-10", "2026-09-11")
	heavy := logs.entries["2026-09-10"]
	heavy.Flow = models.FlowHeavy
	logs.entries["2026-09-10"] = heavy

	if _, err := service.UpsertDayEntryWithAutoFillAt(ctx, 10, anchor, DayEntryInput{IsPeriod: false, Flow: models.FlowNone}, now, time.UTC); err != nil {
		t.Fatalf("uncheck the anchor: %v", err)
	}

	for _, key := range []string{"2026-09-08", "2026-09-09"} {
		if logs.entries[key].IsPeriod {
			t.Fatalf("%s carries the anchor's flow and no mark, so the heuristic reads it as filled and must clear it", key)
		}
	}
	for _, key := range []string{"2026-09-10", "2026-09-11"} {
		if !logs.entries[key].IsPeriod {
			t.Fatalf("%s must be kept: the walk stops at the unmarked day with a different flow", key)
		}
	}
}

func TestUncheckingAnchorAfterItsFlowChangedClearsTheFillDays(t *testing.T) {
	logs := runProvenanceSteps(t, []provenanceStep{
		{name: "log the anchor light with auto-fill on", input: DayEntryInput{IsPeriod: true, Flow: models.FlowLight}},
		{name: "edit the anchor to heavy", input: DayEntryInput{IsPeriod: true, Flow: models.FlowHeavy}},
		{name: "uncheck the anchor", input: DayEntryInput{IsPeriod: false, Flow: models.FlowNone}},
	})

	for _, key := range []string{"2026-09-07", "2026-09-08", "2026-09-09", "2026-09-10", "2026-09-11"} {
		entry := logs.entries[key]
		if entry.IsPeriod || entry.Flow != models.FlowNone {
			t.Fatalf("%s was written by the fill and must be cleared with the anchor, got IsPeriod=%t Flow=%q", key, entry.IsPeriod, entry.Flow)
		}
	}
}

func TestUncheckingAnchorKeepsAHandSetNeighbourCarryingTheAnchorFlow(t *testing.T) {
	for _, tc := range []struct {
		name  string
		steps []provenanceStep
	}{
		{
			name: "hand-set flow equals the anchor's",
			steps: []provenanceStep{
				{name: "log the anchor light", input: DayEntryInput{IsPeriod: true, Flow: models.FlowLight}},
				{name: "hand-set the third day light", offset: 2, input: DayEntryInput{IsPeriod: true, Flow: models.FlowLight}},
				{name: "uncheck the anchor", input: DayEntryInput{IsPeriod: false, Flow: models.FlowNone}},
			},
		},
		{
			// The second direction: the fill wrote none, the anchor became
			// medium without a re-fill, and the owner set the third day medium.
			name: "anchor flow edited to match the hand-set day",
			steps: []provenanceStep{
				{name: "log the anchor with no flow", input: DayEntryInput{IsPeriod: true, Flow: models.FlowNone}},
				{name: "edit the anchor to medium", input: DayEntryInput{IsPeriod: true, Flow: models.FlowMedium}},
				{name: "hand-set the third day medium", offset: 2, input: DayEntryInput{IsPeriod: true, Flow: models.FlowMedium}},
				{name: "uncheck the anchor", input: DayEntryInput{IsPeriod: false, Flow: models.FlowNone}},
			},
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			logs := runProvenanceSteps(t, tc.steps)

			if bare := logs.entries["2026-09-08"]; bare.IsPeriod {
				t.Fatal("the fill day before the hand-set day must be cleared")
			}
			kept := logs.entries["2026-09-09"]
			if !kept.IsPeriod || kept.Flow == models.FlowNone {
				t.Fatalf("the hand-set day must survive the un-tick, got IsPeriod=%t Flow=%q", kept.IsPeriod, kept.Flow)
			}
		})
	}
}
