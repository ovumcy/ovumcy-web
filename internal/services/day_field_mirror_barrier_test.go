package services

import (
	"go/ast"
	"go/parser"
	"go/token"
	"path/filepath"
	"testing"

	"github.com/ovumcy/ovumcy-web/internal/models"
)

// Barrier for the two mirrored day-field predicates.
//
// DayHasData asks whether a day carries any signal at all;
// IsAutoFilledPeriodCandidate asks whether a period day carries nothing BUT the
// auto-filled period marks, so toggling the anchor off may clear it. The second
// question is the first one inverted, and the two functions answer it by
// testing the same DailyLog fields in the same order with opposite returns.
//
// Nothing held them together. Both had their own per-function test table, and a
// ninth field wired into DayHasData alone would leave
// ClearAutoFilledPeriodNeighbors deleting a day that carries that manual signal
// — a silent loss of what the owner typed, which is the consequence this
// barrier exists to prevent.
//
// It reads the shipped source of both functions rather than a list of fields
// kept here, so a field added later is covered the moment it is written. The
// two exception sets below are the only asymmetry the source is allowed to
// carry, and each is documented at its function.
var (
	// dayHasDataOnlyFields may appear in DayHasData and not in the candidate
	// check: web's auto-fill replays the anchor's Flow into its neighbours, so a
	// neighbour whose only manual change was a flow override is still inside the
	// auto-fill window for clearing purposes (day_utils.go).
	dayHasDataOnlyFields = map[string]string{
		"Flow": "auto-fill propagates the anchor's flow, so a flow-only neighbour stays clearable",
	}

	// dayCandidateOnlyFields may appear in IsAutoFilledPeriodCandidate and not
	// in DayHasData: they mark a day as an anchor rather than as carrying data,
	// which is a reason to keep it that DayHasData answers elsewhere.
	dayCandidateOnlyFields = map[string]string{
		"CycleStart":  "an explicit cycle anchor is never an auto-filled neighbour",
		"IsUncertain": "an uncertain anchor is never an auto-filled neighbour",
	}

	// dayMirrorGateFields are read by both functions as the period gate rather
	// than as a manual signal, so they carry no behavioural row below.
	dayMirrorGateFields = map[string]struct{}{
		"IsPeriod": {},
	}
)

// dayManualSignalFixtures maps a DailyLog field to a log carrying that field as
// its only manual signal. Every field the two predicates share, minus the gate,
// must appear here — the coverage assertion below is what stops a field wired
// into both functions from shipping with no proof that it flips either.
var dayManualSignalFixtures = map[string]models.DailyLog{
	"Mood":            {Mood: MinDayMood},
	"SexActivity":     {SexActivity: models.SexActivityProtected},
	"BBT":             {BBT: new(36.5)},
	"CervicalMucus":   {CervicalMucus: models.CervicalMucusEggWhite},
	"PregnancyTest":   {PregnancyTest: models.PregnancyTestPositive},
	"CycleFactorKeys": {CycleFactorKeys: []string{models.CycleFactorStress}},
	"SymptomIDs":      {SymptomIDs: []uint{1}},
	"Notes":           {Notes: "spotty"},
}

// TestDayDataAndAutoFillPredicatesReadTheSameFields fails when one predicate
// starts reading a DailyLog field the other does not, outside the two
// documented exception sets.
func TestDayDataAndAutoFillPredicatesReadTheSameFields(t *testing.T) {
	hasData, candidate := dayPredicateFields(t)

	if len(hasData) == 0 || len(candidate) == 0 {
		t.Fatalf("one of the predicates read no DailyLog field — the barrier parsed a file it does not understand, not a tree without drift")
	}

	for field := range dayHasDataOnlyFields {
		delete(hasData, field)
	}
	for field := range dayCandidateOnlyFields {
		delete(candidate, field)
	}

	for _, field := range sortedKeys(hasData) {
		if _, ok := candidate[field]; !ok {
			t.Errorf("DayHasData reads DailyLog.%s and IsAutoFilledPeriodCandidate does not: a day carrying only that signal counts as data, yet ClearAutoFilledPeriodNeighbors would delete it. Mirror the check, or add the field to dayHasDataOnlyFields with the reason", field)
		}
	}
	for _, field := range sortedKeys(candidate) {
		if _, ok := hasData[field]; !ok {
			t.Errorf("IsAutoFilledPeriodCandidate reads DailyLog.%s and DayHasData does not: a day carrying only that signal is kept from auto-clearing, yet it counts as empty everywhere DayHasData decides. Mirror the check, or add the field to dayCandidateOnlyFields with the reason", field)
		}
	}
}

// TestEveryMirroredDayFieldFlipsBothPredicates drives each shared field through
// both functions, and fails when the fixture set stops covering the fields the
// source actually reads — so a field wired into both predicates cannot ship
// with nothing proving it is honoured.
func TestEveryMirroredDayFieldFlipsBothPredicates(t *testing.T) {
	hasData, candidate := dayPredicateFields(t)

	shared := map[string]struct{}{}
	for field := range hasData {
		if _, ok := candidate[field]; !ok {
			continue
		}
		if _, gate := dayMirrorGateFields[field]; gate {
			continue
		}
		shared[field] = struct{}{}
	}

	for _, field := range sortedKeys(shared) {
		if _, ok := dayManualSignalFixtures[field]; !ok {
			t.Errorf("both predicates read DailyLog.%s and dayManualSignalFixtures has no log carrying it — add one, so the field is proved to flip both answers", field)
		}
	}
	for _, field := range sortedKeys(dayManualSignalFixtures) {
		if _, ok := shared[field]; !ok {
			t.Errorf("dayManualSignalFixtures carries DailyLog.%s, which the two predicates no longer share — the fixture is measuring a field nobody reads", field)
		}
	}

	for _, field := range sortedKeys(dayManualSignalFixtures) {
		entry := dayManualSignalFixtures[field]
		t.Run(field, func(t *testing.T) {
			if !DayHasData(entry) {
				t.Errorf("DayHasData reports no data for a log whose only signal is %s", field)
			}
			periodEntry := entry
			periodEntry.IsPeriod = true
			if IsAutoFilledPeriodCandidate(periodEntry) {
				t.Errorf("IsAutoFilledPeriodCandidate would clear a period day whose only manual signal is %s", field)
			}
		})
	}

	// Anti-vacuity anchor, on fixtures this test owns rather than on the field
	// list it judges: the bare period day must classify the other way in both
	// predicates, or the two assertions above hold for every input.
	bare := models.DailyLog{IsPeriod: true}
	if !DayHasData(bare) {
		t.Fatalf("DayHasData denies a bare period day — the fixtures above prove nothing")
	}
	if !IsAutoFilledPeriodCandidate(bare) {
		t.Fatalf("IsAutoFilledPeriodCandidate refuses a bare period day — the fixtures above prove nothing")
	}
}

// dayPredicateFields returns the DailyLog fields each predicate reads, taken
// from the shipped source of day_utils.go.
func dayPredicateFields(t *testing.T) (hasData, candidate map[string]struct{}) {
	t.Helper()

	path := filepath.Join(servicesSourceBarrierRoot(t), "internal", "services", "day_utils.go")
	fset := token.NewFileSet()
	parsed, err := parser.ParseFile(fset, path, nil, 0)
	if err != nil {
		t.Fatalf("parsing %s: %v", path, err)
	}

	found := map[string]map[string]struct{}{}
	for _, decl := range parsed.Decls {
		fn, ok := decl.(*ast.FuncDecl)
		if !ok || fn.Body == nil {
			continue
		}
		if fn.Name.Name != "DayHasData" && fn.Name.Name != "IsAutoFilledPeriodCandidate" {
			continue
		}
		found[fn.Name.Name] = dailyLogFieldsRead(fn)
	}

	hasData, ok := found["DayHasData"]
	if !ok {
		t.Fatalf("DayHasData is no longer declared in %s — rewrite the barrier with the function", path)
	}
	candidate, ok = found["IsAutoFilledPeriodCandidate"]
	if !ok {
		t.Fatalf("IsAutoFilledPeriodCandidate is no longer declared in %s — rewrite the barrier with the function", path)
	}
	return hasData, candidate
}

// dailyLogFieldsRead collects every `<param>.Field` selector in the body, where
// <param> is the function's single DailyLog parameter. Reading the parameter's
// own name rather than a hard-coded "entry" keeps the sweep honest through a
// rename.
func dailyLogFieldsRead(fn *ast.FuncDecl) map[string]struct{} {
	param := ""
	if fn.Type.Params != nil {
		for _, field := range fn.Type.Params.List {
			for _, name := range field.Names {
				param = name.Name
			}
		}
	}

	fields := map[string]struct{}{}
	if param == "" {
		return fields
	}
	ast.Inspect(fn.Body, func(node ast.Node) bool {
		selector, ok := node.(*ast.SelectorExpr)
		if !ok {
			return true
		}
		ident, ok := selector.X.(*ast.Ident)
		if !ok || ident.Name != param {
			return true
		}
		fields[selector.Sel.Name] = struct{}{}
		return true
	})
	return fields
}
