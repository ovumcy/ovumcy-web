package services

import (
	"context"
	"testing"
	"time"

	"github.com/ovumcy/ovumcy-web/internal/models"
)

// TestBuildCycleLengthTrendStatementGatesOnCompletedCycles pins the tier: two
// completed cycles carry the basic insights shelf but not a trend statement,
// and the third one turns it on. One sample below the gate renders nothing —
// there is no hedged wording for a thinner sample.
func TestBuildCycleLengthTrendStatementGatesOnCompletedCycles(t *testing.T) {
	if _, ok := buildCycleLengthTrendStatement([]int{30, 27}); ok {
		t.Fatal("expected no cycle-length trend statement with two completed cycles")
	}

	statement, ok := buildCycleLengthTrendStatement([]int{30, 29, 27})
	if !ok {
		t.Fatal("expected a cycle-length trend statement at three completed cycles")
	}
	if statement.Kind != StatsStatementKindCycleLengthTrend {
		t.Fatalf("unexpected statement kind %q", statement.Kind)
	}
}

// TestBuildCycleLengthTrendStatementReportsDirectionAndSize covers the
// arithmetic: the recent half of the window against the earlier half, the
// middle cycle dropped on an odd window, rounded to whole days.
func TestBuildCycleLengthTrendStatementReportsDirectionAndSize(t *testing.T) {
	cases := []struct {
		name        string
		lengths     []int
		direction   string
		key         string
		count       int
		detailKey   string
		detailCount int
	}{
		{
			name:        "three cycles compare first against last",
			lengths:     []int{30, 29, 27},
			direction:   StatsStatementDirectionShorter,
			key:         "stats.statement_cycle_trend_shorter",
			count:       3,
			detailKey:   "stats.statement_cycle_trend_window",
			detailCount: 3,
		},
		{
			name:        "four cycles compare two against two",
			lengths:     []int{28, 29, 31, 32},
			direction:   StatsStatementDirectionLonger,
			key:         "stats.statement_cycle_trend_longer",
			count:       3,
			detailKey:   "stats.statement_cycle_trend_window",
			detailCount: 4,
		},
		{
			name:        "six cycles round the halves to whole days",
			lengths:     []int{30, 30, 29, 29, 29, 28},
			direction:   StatsStatementDirectionShorter,
			key:         "stats.statement_cycle_trend_shorter",
			count:       1,
			detailKey:   "stats.statement_cycle_trend_window",
			detailCount: 6,
		},
		{
			name:      "a change below half a day is steady, and carries the window itself",
			lengths:   []int{28, 29, 29, 28},
			direction: StatsStatementDirectionSteady,
			key:       "stats.statement_cycle_trend_steady",
			count:     4,
		},
		{
			name:      "the window never reaches past six completed cycles",
			lengths:   []int{10, 60, 30, 30, 30, 30, 30, 30},
			direction: StatsStatementDirectionSteady,
			key:       "stats.statement_cycle_trend_steady",
			count:     6,
		},
		{
			name:        "a non-positive span never joins a mean",
			lengths:     []int{0, 30, 30, 26, 26},
			direction:   StatsStatementDirectionShorter,
			key:         "stats.statement_cycle_trend_shorter",
			count:       4,
			detailKey:   "stats.statement_cycle_trend_window",
			detailCount: 4,
		},
	}

	for _, testCase := range cases {
		t.Run(testCase.name, func(t *testing.T) {
			statement, ok := buildCycleLengthTrendStatement(testCase.lengths)
			if !ok {
				t.Fatal("expected a cycle-length trend statement")
			}
			if statement.Direction != testCase.direction {
				t.Errorf("expected direction %q, got %q", testCase.direction, statement.Direction)
			}
			if statement.Key != testCase.key {
				t.Errorf("expected key %q, got %q", testCase.key, statement.Key)
			}
			if statement.Count != testCase.count {
				t.Errorf("expected count %d, got %d", testCase.count, statement.Count)
			}
			if statement.DetailKey != testCase.detailKey {
				t.Errorf("expected detail key %q, got %q", testCase.detailKey, statement.DetailKey)
			}
			if statement.DetailCount != testCase.detailCount {
				t.Errorf("expected detail count %d, got %d", testCase.detailCount, statement.DetailCount)
			}
		})
	}
}

// TestBuildSymptomPhaseRecurrenceStatementsGatesOnCompletedCycles pins the
// phase-pattern tier at three completed cycles: dropping the closing period
// start leaves two, and the recurrence family goes silent rather than
// softening its wording.
func TestBuildSymptomPhaseRecurrenceStatementsGatesOnCompletedCycles(t *testing.T) {
	symptomByID := map[uint]models.SymptomType{
		1: {ID: 1, Name: "Headache", Icon: "H"},
	}
	logs := buildRecurrenceStatementLogs(t)

	if statements := buildSymptomPhaseRecurrenceStatements(logs[:len(logs)-1], symptomByID, time.UTC); len(statements) != 0 {
		t.Fatalf("expected no recurrence statements with two completed cycles, got %#v", statements)
	}

	statements := buildSymptomPhaseRecurrenceStatements(logs, symptomByID, time.UTC)
	if len(statements) != 1 {
		t.Fatalf("expected one recurrence statement at three completed cycles, got %#v", statements)
	}
}

// TestBuildSymptomPhaseRecurrenceStatementsCountsPhaseOccurrences checks the
// arithmetic: the numerator is the number of that phase's occurrences carrying
// the symptom, the denominator the number of occurrences the owner recorded at
// all.
func TestBuildSymptomPhaseRecurrenceStatementsCountsPhaseOccurrences(t *testing.T) {
	symptomByID := map[uint]models.SymptomType{
		1: {ID: 1, Name: "Headache", Icon: "H"},
	}

	statements := buildSymptomPhaseRecurrenceStatements(buildRecurrenceStatementLogs(t), symptomByID, time.UTC)
	if len(statements) != 1 {
		t.Fatalf("expected exactly one recurrence statement, got %#v", statements)
	}

	statement := statements[0]
	if statement.Kind != StatsStatementKindSymptomPhaseRecurrence {
		t.Errorf("unexpected kind %q", statement.Kind)
	}
	if statement.Key != "stats.statement_symptom_recurrence" {
		t.Errorf("unexpected key %q", statement.Key)
	}
	if statement.Phase != "luteal" {
		t.Errorf("expected the luteal phase, got %q", statement.Phase)
	}
	if statement.Count != 3 || statement.Total != 3 {
		t.Errorf("expected 3 of 3 luteal phases, got %d of %d", statement.Count, statement.Total)
	}
	if statement.SymptomName != "Headache" || statement.SymptomIcon != "H" {
		t.Errorf("expected the statement to carry its symptom, got %#v", statement)
	}
}

// TestBuildSymptomPhaseRecurrenceStatementsDropThinAndMinorityPatterns pins the
// two halves of the recurrence gate that the cycle count does not cover: a
// single hit is a coincidence, and a minority of the recorded occurrences is
// not a recurrence.
func TestBuildSymptomPhaseRecurrenceStatementsDropThinAndMinorityPatterns(t *testing.T) {
	cases := []struct {
		name  string
		count int
		total int
		want  bool
	}{
		{name: "a single hit never qualifies", count: 1, total: 3, want: false},
		{name: "two of three is a majority", count: 2, total: 3, want: true},
		{name: "two of four is only a tie", count: 2, total: 4, want: false},
		{name: "three of four is a majority", count: 3, total: 4, want: true},
		{name: "two recorded phases is below the tier", count: 2, total: 2, want: false},
	}

	for _, testCase := range cases {
		t.Run(testCase.name, func(t *testing.T) {
			if got := statsRecurrenceQualifies(testCase.count, testCase.total); got != testCase.want {
				t.Fatalf("statsRecurrenceQualifies(%d, %d) = %v, want %v", testCase.count, testCase.total, got, testCase.want)
			}
		})
	}
}

// TestBuildSymptomPhaseRecurrenceStatementsCapTheShelf pins the cap: four
// symptoms recurring in the same phase produce four qualifying statements, and
// the section keeps the strongest three.
func TestBuildSymptomPhaseRecurrenceStatementsCapTheShelf(t *testing.T) {
	symptomByID := map[uint]models.SymptomType{
		1: {ID: 1, Name: "Acne", Icon: "A"},
		2: {ID: 2, Name: "Bloating", Icon: "B"},
		3: {ID: 3, Name: "Cramps", Icon: "C"},
		4: {ID: 4, Name: "Headache", Icon: "H"},
	}

	logs := buildRecurrenceStatementLogs(t)
	for index := range logs {
		if len(logs[index].SymptomIDs) > 0 {
			logs[index].SymptomIDs = []uint{1, 2, 3, 4}
		}
	}

	statements := buildSymptomPhaseRecurrenceStatements(logs, symptomByID, time.UTC)
	if len(statements) != statsStatementRecurrenceLimit {
		t.Fatalf("expected the shelf to keep %d statements, got %d", statsStatementRecurrenceLimit, len(statements))
	}
	for index, name := range []string{"Acne", "Bloating", "Cramps"} {
		if statements[index].SymptomName != name {
			t.Errorf("position %d: expected %q, got %q", index, name, statements[index].SymptomName)
		}
	}
}

// TestMeanOfCycleLengthsAnswersZeroForAnEmptyHalf pins the divisor guard. The
// only production caller splits a window of at least three lengths, so both
// halves are non-empty there; the guard exists so a future caller cannot turn
// an empty slice into NaN, and this is what holds it in place.
func TestMeanOfCycleLengthsAnswersZeroForAnEmptyHalf(t *testing.T) {
	if got := meanOfCycleLengths(nil); got != 0 {
		t.Fatalf("expected 0 for an empty slice, got %v", got)
	}
	if got := meanOfCycleLengths([]int{28, 30}); got != 29 {
		t.Fatalf("expected 29, got %v", got)
	}
}

// TestBuildSymptomPhaseRecurrenceStatementsOrderAndLimit pins the order several
// qualifying statements take — more occurrences first, then the tighter
// denominator, then the product's phase order, then the symptom name — and the
// cap that keeps the section a shelf rather than a dump.
func TestBuildSymptomPhaseRecurrenceStatementsOrderAndLimit(t *testing.T) {
	statements := []StatsStatement{
		{Count: 3, Total: 4, Phase: "luteal", SymptomName: "Acne"},
		{Count: 2, Total: 3, Phase: "menstrual", SymptomName: "Bloating"},
		{Count: 3, Total: 3, Phase: "luteal", SymptomName: "Headache"},
		{Count: 3, Total: 3, Phase: "menstrual", SymptomName: "Cramps"},
		{Count: 3, Total: 3, Phase: "menstrual", SymptomName: "Backache"},
	}

	sortStatsRecurrenceStatements(statements)

	want := []string{"Backache", "Cramps", "Headache", "Acne", "Bloating"}
	for index, name := range want {
		if statements[index].SymptomName != name {
			t.Fatalf("position %d: expected %q, got %q (order: %#v)", index, name, statements[index].SymptomName, statements)
		}
	}
}

// TestStatsPageStatementsSurviveSuppressedPredictions is the display half of
// the section's claim: every number in it comes from recorded days, so
// unpredictable mode — which suppresses next-period, fertile and ovulation
// output across dashboard, calendar and stats — leaves the statements standing.
func TestStatsPageStatementsSurviveSuppressedPredictions(t *testing.T) {
	now := time.Now().UTC()
	today := CalendarDay(now, time.UTC)
	logs := buildStatementPageLogs(today)

	service := NewStatsService(
		&stubStatsDayReader{logsForRange: logs, logsForAll: logs},
		&stubStatsSymptomReader{symptoms: []models.SymptomType{{ID: 1, Name: "Headache", Icon: "H"}}},
	)
	lastPeriodStart := today.AddDate(0, 0, -112)
	user := &models.User{
		ID:                 7,
		Role:               models.RoleOwner,
		CycleLength:        28,
		PeriodLength:       5,
		LastPeriodStart:    &lastPeriodStart,
		UnpredictableCycle: true,
	}

	viewData, err := service.BuildStatsPageViewData(context.Background(), user, "en", "Cycle %d", now, time.UTC, 12)
	if err != nil {
		t.Fatalf("build stats page view data: %v", err)
	}

	if !viewData.PredictionDisabled {
		t.Fatal("expected the fixture to have predictions suppressed")
	}
	if !viewData.HasStatements || len(viewData.Statements) == 0 {
		t.Fatal("expected recorded-history statements to render while predictions are suppressed")
	}
	if viewData.Statements[0].Kind != StatsStatementKindCycleLengthTrend {
		t.Fatalf("expected the cycle-length trend to lead the section, got %q", viewData.Statements[0].Kind)
	}

	sawRecurrence := false
	for _, statement := range viewData.Statements {
		if statement.Kind == StatsStatementKindSymptomPhaseRecurrence {
			sawRecurrence = true
			if statement.Phase != "luteal" {
				t.Errorf("expected the luteal recurrence, got phase %q", statement.Phase)
			}
		}
	}
	if !sawRecurrence {
		t.Error("expected the symptom-by-phase recurrence statement to survive suppression too")
	}
}

// buildRecurrenceStatementLogs lays four period starts 28 days apart — three
// completed cycles — and logs the same symptom on cycle day 22, inside the
// luteal phase of each. Two further logged days per cycle keep the earlier
// phases recorded, so the denominator of a non-luteal phase is real.
func buildRecurrenceStatementLogs(t *testing.T) []models.DailyLog {
	t.Helper()

	logs := make([]models.DailyLog, 0, 13)
	for cycle := range 3 {
		start := mustParseStatsServiceDay(t, "2026-01-01").AddDate(0, 0, cycle*28)
		logs = append(logs,
			models.DailyLog{Date: start, IsPeriod: true},
			models.DailyLog{Date: start.AddDate(0, 0, 9)},
			models.DailyLog{Date: start.AddDate(0, 0, 21), SymptomIDs: []uint{1}},
		)
	}
	logs = append(logs, models.DailyLog{Date: mustParseStatsServiceDay(t, "2026-01-01").AddDate(0, 0, 84), IsPeriod: true})
	return logs
}

// buildStatementPageLogs is buildRecurrenceStatementLogs anchored to today, for
// the surfaces that measure completed cycles against the live clock.
// CompletedCycleTrendLengths counts only starts strictly before today, so the
// four seeded starts plus today's leave three completed cycles — the trend
// tier — while the phase contexts see four closed cycles.
func buildStatementPageLogs(today time.Time) []models.DailyLog {
	logs := make([]models.DailyLog, 0, 13)
	for cycle := range 4 {
		start := today.AddDate(0, 0, -112+cycle*28)
		logs = append(logs,
			models.DailyLog{Date: start, IsPeriod: true},
			models.DailyLog{Date: start.AddDate(0, 0, 9)},
			models.DailyLog{Date: start.AddDate(0, 0, 21), SymptomIDs: []uint{1}},
		)
	}
	return append(logs, models.DailyLog{Date: today, IsPeriod: true})
}
