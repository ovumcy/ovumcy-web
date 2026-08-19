package services

import (
	"testing"
	"time"

	"github.com/ovumcy/ovumcy-web/internal/models"
)

// One stats render builds its cycle history twice: the ribbon and the cycle
// factor context read buildCompletedCycleSpans, the phase cards and the history
// statements read buildCompletedCyclePhaseContexts. Both answer "where does a
// cycle begin" over the same logs, so they must answer it the same way — a page
// whose two halves count different cycles reports a merged 56-day cycle in one
// panel and two 28-day cycles in the other, and the longer of the two then
// carries the owner's logged factors as if it were measured.

type completedCycleAgreementCase struct {
	name string
	logs []models.DailyLog
	// wantCycles is what BOTH builders must report, so the table pins the shared
	// answer rather than merely pinning the two to each other.
	wantCycles  []int
	wantStarts  []string
	description string
}

func agreementPeriodDays(t *testing.T, start string, days int, cycleStart bool, uncertain bool) []models.DailyLog {
	t.Helper()
	startDay, err := time.Parse("2006-01-02", start)
	if err != nil {
		t.Fatalf("parse day %q: %v", start, err)
	}
	logs := make([]models.DailyLog, 0, days)
	for offset := range days {
		logs = append(logs, models.DailyLog{
			Date:        startDay.AddDate(0, 0, offset),
			IsPeriod:    true,
			CycleStart:  cycleStart && offset == 0,
			IsUncertain: uncertain && offset == 0,
		})
	}
	return logs
}

func completedCycleAgreementCases(t *testing.T) []completedCycleAgreementCase {
	t.Helper()

	plain := append(agreementPeriodDays(t, "2026-01-01", 4, false, false), agreementPeriodDays(t, "2026-01-29", 4, false, false)...)
	plain = append(plain, agreementPeriodDays(t, "2026-02-26", 4, false, false)...)

	explicit := append(agreementPeriodDays(t, "2026-01-01", 4, true, false), agreementPeriodDays(t, "2026-01-29", 4, true, false)...)
	explicit = append(explicit, agreementPeriodDays(t, "2026-02-26", 4, true, false)...)

	uncertainMiddle := append(agreementPeriodDays(t, "2026-01-01", 4, true, false), agreementPeriodDays(t, "2026-01-29", 4, true, true)...)
	uncertainMiddle = append(uncertainMiddle, agreementPeriodDays(t, "2026-02-26", 4, true, false)...)

	uncertainLast := append(agreementPeriodDays(t, "2026-01-01", 4, true, false), agreementPeriodDays(t, "2026-01-29", 4, true, false)...)
	uncertainLast = append(uncertainLast, agreementPeriodDays(t, "2026-02-26", 4, true, true)...)

	return []completedCycleAgreementCase{
		{
			name:        "three period clusters, no explicit starts",
			logs:        plain,
			wantCycles:  []int{28, 28},
			wantStarts:  []string{"2026-01-01", "2026-01-29"},
			description: "the cluster's first day is the start",
		},
		{
			name:        "three explicit starts",
			logs:        explicit,
			wantCycles:  []int{28, 28},
			wantStarts:  []string{"2026-01-01", "2026-01-29"},
			description: "the explicit start is the start",
		},
		{
			name:        "middle start marked uncertain",
			logs:        uncertainMiddle,
			wantCycles:  []int{56},
			wantStarts:  []string{"2026-01-01"},
			description: "an uncertain-only cluster yields no start, so the two cycles read as one",
		},
		{
			name:        "latest start marked uncertain",
			logs:        uncertainLast,
			wantCycles:  []int{28},
			wantStarts:  []string{"2026-01-01"},
			description: "the uncertain cluster closes no cycle",
		},
	}
}

func TestCompletedCycleSpansAndPhaseContextsAgree(t *testing.T) {
	for _, testCase := range completedCycleAgreementCases(t) {
		t.Run(testCase.name, func(t *testing.T) {
			spans := buildCompletedCycleSpans(testCase.logs, time.UTC)
			contexts := buildCompletedCyclePhaseContexts(testCase.logs, time.UTC)

			if len(spans) != len(contexts) {
				t.Errorf("cycle count disagrees: %d span(s) against %d phase context(s) — %s", len(spans), len(contexts), testCase.description)
			}

			spanLengths := make([]int, 0, len(spans))
			spanStarts := make([]string, 0, len(spans))
			for _, span := range spans {
				spanLengths = append(spanLengths, span.CycleLength)
				spanStarts = append(spanStarts, span.Start.Format("2006-01-02"))
			}
			contextLengths := make([]int, 0, len(contexts))
			contextStarts := make([]string, 0, len(contexts))
			for _, context := range contexts {
				contextLengths = append(contextLengths, context.CycleLength)
				contextStarts = append(contextStarts, context.Start.Format("2006-01-02"))
			}

			assertIntSliceEquals(t, "span cycle lengths", spanLengths, testCase.wantCycles)
			assertIntSliceEquals(t, "phase-context cycle lengths", contextLengths, testCase.wantCycles)
			assertStringSliceEquals(t, "span starts", spanStarts, testCase.wantStarts)
			assertStringSliceEquals(t, "phase-context starts", contextStarts, testCase.wantStarts)
		})
	}
}

func assertIntSliceEquals(t *testing.T, label string, got []int, want []int) {
	t.Helper()
	if len(got) != len(want) {
		t.Errorf("%s = %v, want %v", label, got, want)
		return
	}
	for index := range want {
		if got[index] != want[index] {
			t.Errorf("%s = %v, want %v", label, got, want)
			return
		}
	}
}

func assertStringSliceEquals(t *testing.T, label string, got []string, want []string) {
	t.Helper()
	if len(got) != len(want) {
		t.Errorf("%s = %v, want %v", label, got, want)
		return
	}
	for index := range want {
		if got[index] != want[index] {
			t.Errorf("%s = %v, want %v", label, got, want)
			return
		}
	}
}
