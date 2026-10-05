package services

import (
	"reflect"
	"testing"
	"time"

	"github.com/ovumcy/ovumcy-web/internal/models"
)

// ShowHistoricalPhases is a display preference with a stated scope, and the
// scope is narrower than its name suggests. Its settings copy promises to
// "paint ovulation, fertility peak, and pre-fertile markers on past completed
// cycles in the calendar" — drawn markers, on the calendar and on the stats
// cycle stack that shares the calendar's encoding.
//
// The stats prose that groups moods and symptoms by inferred phase does not
// ride it. Those surfaces derive their phases from CalcOvulationDay for their
// own gate — owner, plus the completed-cycle pattern minimum — and never read
// the preference, so an owner who turns the markers off still reads
// phase-grouped statistics.
//
// That asymmetry is deliberate, and until now nothing said so: an audit reading
// the preference's name would take the prose surfaces for a gap. Widening it to
// them would be a behaviour change beyond what the toggle promises, removing
// insights the owner never asked to hide. So the scope is pinned instead, in
// both directions — the surfaces it governs, and the surfaces it does not.
func showHistoricalPhasesScopeLogs(t *testing.T) []models.DailyLog {
	t.Helper()

	logs := statscycleribbonHistory(t)
	for index := range logs {
		logs[index].Mood = MinDayMood + 1
		logs[index].SymptomIDs = []uint{1}
	}
	return logs
}

func showHistoricalPhasesScopeSymptoms() map[uint]models.SymptomType {
	return map[uint]models.SymptomType{1: {ID: 1, Name: "Cramps", Icon: "C"}}
}

// TestShowHistoricalPhasesGovernsTheDrawnMarkersOnly asserts the preference's
// scope in both directions: the calendar's historical pass and the stats cycle
// stack follow it, and the three phase-derived prose surfaces are unchanged by
// it.
func TestShowHistoricalPhasesGovernsTheDrawnMarkersOnly(t *testing.T) {
	logs := showHistoricalPhasesScopeLogs(t)
	stats := CycleStats{LutealPhase: 14}
	symptomByID := showHistoricalPhasesScopeSymptoms()

	t.Run("the calendar's historical markers follow it", func(t *testing.T) {
		offMarkers := historicalPhaseMarkerCount(logs, stats, statscycleribbonOwner(false))
		onMarkers := historicalPhaseMarkerCount(logs, stats, statscycleribbonOwner(true))

		if offMarkers != 0 {
			t.Errorf("the preference is off and the calendar still painted %d historical phase markers", offMarkers)
		}
		if onMarkers == 0 {
			t.Fatal("the preference is on and the calendar painted no historical phase marker — the off-case above proves nothing")
		}
	})

	t.Run("the stats cycle stack follows it", func(t *testing.T) {
		spans := buildCompletedCycleSpans(logs, time.UTC)
		off := buildStatsCycleRibbon(statscycleribbonOwner(false), stats, logs, spans)
		on := buildStatsCycleRibbon(statscycleribbonOwner(true), stats, logs, spans)

		if off.ShowPhases {
			t.Error("the preference is off and the cycle stack still shades inferred phases")
		}
		if !on.ShowPhases {
			t.Fatal("the preference is on and the cycle stack shades nothing — the off-case above proves nothing")
		}
	})

	// The three surfaces below are the ones the preference does NOT govern.
	// Each is asserted to produce data at all — an empty result would make the
	// equality that follows hold for any implementation — and then to produce
	// the SAME data with the preference on and off.
	t.Run("phase mood insights do not follow it", func(t *testing.T) {
		service := &StatsService{}
		off, offOK := service.BuildPhaseMoodInsights(statscycleribbonOwner(false), logs, time.UTC)
		on, onOK := service.BuildPhaseMoodInsights(statscycleribbonOwner(true), logs, time.UTC)

		// The reachability anchor reads the ON case: under any gating it is the
		// larger of the two, so an empty result there is a broken fixture and
		// not the preference doing its work.
		if !onOK || len(on) == 0 {
			t.Fatalf("no phase mood insight to compare (ok=%v, count=%d) — the fixture does not reach the surface", onOK, len(on))
		}
		if onOK != offOK || !reflect.DeepEqual(off, on) {
			t.Error("phase mood insights changed with the historical-phase preference: the preference's scope is the drawn markers, and widening it here removes prose the owner never asked to hide")
		}
	})

	t.Run("phase symptom insights do not follow it", func(t *testing.T) {
		// The builder takes no user at all, which is the structural half of the
		// same statement: it could not consult the preference if it wanted to.
		insights, ok := buildPhaseSymptomInsightsWithMap(logs, time.UTC, symptomByID)
		if !ok || len(insights) == 0 {
			t.Fatalf("no phase symptom insight to assert on (ok=%v, count=%d) — the fixture does not reach the surface", ok, len(insights))
		}
	})

	t.Run("symptom phase recurrence statements do not follow it", func(t *testing.T) {
		statements := buildSymptomPhaseRecurrenceStatements(logs, symptomByID, time.UTC)
		if len(statements) == 0 {
			t.Fatal("no recurrence statement to assert on — the fixture does not reach the surface")
		}
	})
}

// The structural half of the two claims above, enforced by the compiler rather
// than by a run: neither builder takes a user, so neither CAN consult the
// preference. Giving one a *models.User parameter breaks this package's build
// at these lines, which is the point — the change is deliberate, and the scope
// statement in this file has to be rewritten with it.
var (
	_ func([]models.DailyLog, *time.Location, map[uint]models.SymptomType) ([]StatsPhaseSymptomInsight, bool) = buildPhaseSymptomInsightsWithMap
	_ func([]models.DailyLog, map[uint]models.SymptomType, *time.Location) []StatsStatement                   = buildSymptomPhaseRecurrenceStatements
)

// historicalPhaseMarkerCount runs the calendar's historical pass and returns how
// many day markers it painted across the four maps it fills.
func historicalPhaseMarkerCount(logs []models.DailyLog, stats CycleStats, user *models.User) int {
	preFertile := map[string]bool{}
	fertilityEdge := map[string]bool{}
	fertilityPeak := map[string]bool{}
	ovulation := map[string]bool{}

	appendHistoricalCycles(preFertile, fertilityEdge, fertilityPeak, ovulation, logs, stats, user, time.UTC)
	return len(preFertile) + len(fertilityEdge) + len(fertilityPeak) + len(ovulation)
}
