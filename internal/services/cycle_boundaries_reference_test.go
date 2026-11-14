package services

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/ovumcy/ovumcy-web/internal/models"
)

// The cycleBoundaries section of the shared golden-vector fixture pins where
// cycles start and the two readers that must agree on it: the running-cycle
// anchor and the pregnancy pause. ovumcy-app consumes the same cases in
// src/services/cycle-boundaries-reference.test.ts.

type cycleBoundaryFixture struct {
	CycleBoundaries struct {
		SchemaVersion int                 `json:"schemaVersion"`
		Cases         []cycleBoundaryCase `json:"cases"`
	} `json:"cycleBoundaries"`
}

type cycleBoundaryCase struct {
	Name        string             `json:"name"`
	Today       string             `json:"today"`
	StoredStart *string            `json:"storedStart"`
	Days        []cycleBoundaryDay `json:"days"`
	Expected    struct {
		Boundaries  []string `json:"boundaries"`
		Anchor      *string  `json:"anchor"`
		PausedSince *string  `json:"pausedSince"`
	} `json:"expected"`
}

type cycleBoundaryDay struct {
	Date            string `json:"date"`
	Period          bool   `json:"period"`
	Flow            string `json:"flow"`
	SpottingSymptom bool   `json:"spottingSymptom"`
	CycleStart      bool   `json:"cycleStart"`
	Uncertain       bool   `json:"uncertain"`
	PregnancyTest   string `json:"pregnancyTest"`
}

func loadCycleBoundaryCases(t *testing.T) []cycleBoundaryCase {
	t.Helper()
	raw, err := os.ReadFile(filepath.Join("testdata", goldenVectorsFile))
	if err != nil {
		t.Fatalf("read golden vectors: %v", err)
	}
	var fixture cycleBoundaryFixture
	if err := json.Unmarshal(raw, &fixture); err != nil {
		t.Fatalf("parse golden vectors: %v", err)
	}
	if fixture.CycleBoundaries.SchemaVersion != 1 || len(fixture.CycleBoundaries.Cases) == 0 {
		t.Fatalf("cycleBoundaries section missing or unexpected schema %d", fixture.CycleBoundaries.SchemaVersion)
	}
	return fixture.CycleBoundaries.Cases
}

func mustBoundaryDate(t *testing.T, value string) time.Time {
	t.Helper()
	day, err := time.Parse("2006-01-02", value)
	if err != nil {
		t.Fatalf("parse %q: %v", value, err)
	}
	return day
}

func formatBoundaryDates(days []time.Time) []string {
	out := make([]string, 0, len(days))
	for _, day := range days {
		out = append(out, day.Format("2006-01-02"))
	}
	return out
}

func TestCycleBoundaries_GoldenVectors(t *testing.T) {
	for _, tc := range loadCycleBoundaryCases(t) {
		t.Run(tc.Name, func(t *testing.T) {
			today := mustBoundaryDate(t, tc.Today)
			user := &models.User{}
			if tc.StoredStart != nil {
				stored := mustBoundaryDate(t, *tc.StoredStart)
				user.LastPeriodStart = &stored
			}
			logs := make([]models.DailyLog, 0, len(tc.Days))
			var latestPositive time.Time
			for _, day := range tc.Days {
				flow := day.Flow
				if flow == "" {
					flow = models.FlowNone
				}
				test := day.PregnancyTest
				if test == "" {
					test = models.PregnancyTestNone
				}
				entry := models.DailyLog{
					Date:               mustBoundaryDate(t, day.Date),
					IsPeriod:           day.Period,
					Flow:               flow,
					HasSpottingSymptom: day.SpottingSymptom,
					CycleStart:         day.CycleStart,
					IsUncertain:        day.Uncertain,
					PregnancyTest:      test,
				}
				if test == models.PregnancyTestPositive {
					latestPositive = entry.Date
				}
				logs = append(logs, entry)
			}
			ctx := BoundaryContextFor(user, today)

			got := formatBoundaryDates(CycleBoundaries(filterLogsNotAfter(logs, today), ctx))
			if len(got) != len(tc.Expected.Boundaries) {
				t.Fatalf("boundaries = %v, want %v", got, tc.Expected.Boundaries)
			}
			for index := range got {
				if got[index] != tc.Expected.Boundaries[index] {
					t.Fatalf("boundaries = %v, want %v", got, tc.Expected.Boundaries)
				}
			}

			anchor := LatestCycleStartAnchorBeforeOrOn(user, logs, today, time.UTC)
			switch {
			case tc.Expected.Anchor == nil && !anchor.IsZero():
				t.Fatalf("anchor = %s, want none", anchor.Format("2006-01-02"))
			case tc.Expected.Anchor != nil && anchor.Format("2006-01-02") != *tc.Expected.Anchor:
				t.Fatalf("anchor = %s, want %s", anchor.Format("2006-01-02"), *tc.Expected.Anchor)
			}

			pausedSince, paused := ResolvePregnancyPause(logs, ctx)
			switch {
			case tc.Expected.PausedSince == nil && paused:
				t.Fatalf("paused since %s, want not paused", pausedSince.Format("2006-01-02"))
			case tc.Expected.PausedSince != nil && (!paused || pausedSince.Format("2006-01-02") != *tc.Expected.PausedSince):
				t.Fatalf("pause = (%s, %v), want paused since %s", pausedSince.Format("2006-01-02"), paused, *tc.Expected.PausedSince)
			}

			if !latestPositive.IsZero() && !paused && !anchor.After(latestPositive) {
				t.Fatalf("pause lifted while anchor %s is not after the positive test %s",
					anchor.Format("2006-01-02"), latestPositive.Format("2006-01-02"))
			}
		})
	}
}
