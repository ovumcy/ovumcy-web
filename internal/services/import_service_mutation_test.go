package services

// import_service_mutation_test.go — behavior tests that pin surviving mutants
// in import_service.go that the baseline mutation run (gremlins) reported as
// LIVED and that no existing test discriminated. Each test was proven by
// per-mutant faulting (apply the mutation, confirm this test fails, revert):
//
//   - L264  `if key != ""`         (CONDITIONALS_NEGATION) — catalog dedup key guard
//   - L376  `ids[i] < ids[j]`      (CONDITIONALS_NEGATION) — resolved symptom-id order
//   - L384  service/users/logs nil (CONDITIONALS_NEGATION ×3) — best-effort refresh guards
//
// The "importmut" prefix keeps this file's stubs from colliding with the
// package-wide helpers in import_service_coverage_test.go.

import (
	"context"
	"encoding/json"
	"testing"
	"time"

	"github.com/ovumcy/ovumcy-web/internal/models"
)

// ---------------------------------------------------------------------------
// L376 — resolveImportSymptomIDs must return ids in ascending order.
// NEGATION `ids[i] < ids[j]` -> `ids[i] >= ids[j]` reverses the sort; a strict
// ascending assertion over unsorted input ids fails under the mutation.
// ---------------------------------------------------------------------------

func TestImportMut_ResolveImportSymptomIDsSortedAscending(t *testing.T) {
	// Built-in symptoms whose names map (via exportSymptomColumn) to flag
	// getters, deliberately given ids that are NOT already ascending in
	// iteration order so a broken comparator is observable.
	builtins := []models.SymptomType{
		{ID: 9, Name: "Cramps", IsBuiltin: true},
		{ID: 2, Name: "Headache", IsBuiltin: true},
		{ID: 5, Name: "Nausea", IsBuiltin: true},
	}
	flags := ExportSymptomFlags{Cramps: true, Headache: true, Nausea: true}

	ids := resolveImportSymptomIDs(flags, nil, builtins, map[string]uint{})

	want := []uint{2, 5, 9}
	if len(ids) != len(want) {
		t.Fatalf("expected %d ids, got %v", len(want), ids)
	}
	for i := range want {
		if ids[i] != want[i] {
			t.Fatalf("ids must be ascending: got %v, want %v", ids, want)
		}
	}
}

// ---------------------------------------------------------------------------
// L264 — reconcileSymptoms indexes each existing catalog symptom by its
// normalized name so an imported "other" symptom of the same name is DEDUPED
// (reuses the existing id) instead of creating a duplicate.
// NEGATION `if key != ""` -> `if key == ""` leaves the catalog index empty, so
// the existing symptom is not found and CreateSymptomForUser is wrongly called.
// ---------------------------------------------------------------------------

type importmutCountingReconciler struct {
	catalog     []models.SymptomType
	createCalls int
	createID    uint
}

func (importmutCountingReconciler) EnsureBuiltinSymptoms(context.Context, uint) error { return nil }
func (r *importmutCountingReconciler) FetchSymptoms(context.Context, uint) ([]models.SymptomType, error) {
	return r.catalog, nil
}
func (r *importmutCountingReconciler) CreateSymptomForUser(_ context.Context, userID uint, name, _, _ string) (models.SymptomType, error) {
	r.createCalls++
	return models.SymptomType{ID: r.createID, UserID: userID, Name: name}, nil
}

func TestImportMut_ExistingCatalogSymptomDedupedNotRecreated(t *testing.T) {
	// A pre-existing custom symptom named "Cramps" (id 7) already in the
	// owner's catalog; the import references the same name via other_symptoms.
	reconciler := &importmutCountingReconciler{
		catalog:  []models.SymptomType{{ID: 7, Name: "Cramps", IsBuiltin: false}},
		createID: 99,
	}
	raw, err := json.Marshal(importPayload{Entries: []ExportJSONEntry{
		{Date: "2026-05-03", Period: false, CycleFactors: []string{}, OtherSymptoms: []string{"Cramps"}},
	}})
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}

	svc := NewImportService(&importStubLogs{}, importStubUsers{}, reconciler, nil)
	result, err := svc.ImportJSON(context.Background(), 1, raw, time.UTC)
	if err != nil {
		t.Fatalf("import: %v", err)
	}
	if result.Added != 1 {
		t.Fatalf("expected 1 day added, got %+v", result)
	}
	// The existing catalog entry keyed by name must be reused; no new symptom
	// may be created for a name already present in the catalog.
	if reconciler.createCalls != 0 {
		t.Fatalf("existing catalog symptom must be deduped by name, but CreateSymptomForUser was called %d time(s)", reconciler.createCalls)
	}
}

// ---------------------------------------------------------------------------
// L384 — refreshDerivedCycleSettings is a best-effort side effect guarded by
// `if service == nil || service.users == nil || service.logs == nil { return }`.
// Each clause independently short-circuits before any repository call. Negating
// a single clause lets the method fall through to a nil dereference, so each
// case below drives exactly one clause to nil and asserts the method returns
// without panicking.
// ---------------------------------------------------------------------------

// importmutDataLogs returns one log from ListByUser so that, if the guard is
// wrongly bypassed, execution proceeds far enough to dereference a nil users
// repository (isolating the users-nil clause).
type importmutDataLogs struct{}

func (importmutDataLogs) FindByUserAndDayRange(context.Context, uint, time.Time, time.Time) (models.DailyLog, bool, error) {
	return models.DailyLog{}, false, nil
}
func (importmutDataLogs) Create(context.Context, *models.DailyLog) error { return nil }
func (importmutDataLogs) CreateBatch(context.Context, []models.DailyLog) error {
	return nil
}
func (importmutDataLogs) ListByUser(context.Context, uint) ([]models.DailyLog, error) {
	return []models.DailyLog{{Date: time.Date(2026, 5, 1, 0, 0, 0, 0, time.UTC), IsPeriod: true}}, nil
}
func (importmutDataLogs) ListByUserRange(context.Context, uint, *time.Time, *time.Time) ([]models.DailyLog, error) {
	return nil, nil
}
func (importmutDataLogs) ListByUserDayRange(context.Context, uint, time.Time, time.Time) ([]models.DailyLog, error) {
	return nil, nil
}
func (importmutDataLogs) Save(context.Context, *models.DailyLog) error { return nil }
func (importmutDataLogs) DeleteByUserAndDayRange(context.Context, uint, time.Time, time.Time) error {
	return nil
}

// ---------------------------------------------------------------------------
// SEC-M14 (WEB-15) — refreshDerivedCycleSettings must bound the persisted
// users.luteal_phase cache at the OWNER's stored timezone, never the
// request's (header/cookie) zone threaded in as `location`. Mirrors
// TestDayService_RefreshDerivedCycleSettings_BoundsAtOwnerZoneNotRequestZone
// (day_service_mutation_test.go) with the same fixture and boundary instant:
// a "bulk restore" is the import writer's own slot in the derivation's three
// writers (services-cycle.md), so it needs its own induced-red proof.
// ---------------------------------------------------------------------------

// importmutTZLogs serves a fixed, pre-seeded log set regardless of the
// import payload's own writes, so the historical fixture below is what
// refreshDerivedCycleSettings's ListByUser call observes.
type importmutTZLogs struct {
	entries []models.DailyLog
}

func (s importmutTZLogs) FindByUserAndDayRange(context.Context, uint, time.Time, time.Time) (models.DailyLog, bool, error) {
	return models.DailyLog{}, false, nil
}
func (s importmutTZLogs) Create(context.Context, *models.DailyLog) error { return nil }
func (s importmutTZLogs) CreateBatch(context.Context, []models.DailyLog) error {
	return nil
}
func (s importmutTZLogs) ListByUser(context.Context, uint) ([]models.DailyLog, error) {
	return s.entries, nil
}
func (s importmutTZLogs) ListByUserRange(context.Context, uint, *time.Time, *time.Time) ([]models.DailyLog, error) {
	return nil, nil
}
func (s importmutTZLogs) ListByUserDayRange(context.Context, uint, time.Time, time.Time) ([]models.DailyLog, error) {
	return nil, nil
}
func (s importmutTZLogs) Save(context.Context, *models.DailyLog) error { return nil }
func (s importmutTZLogs) DeleteByUserAndDayRange(context.Context, uint, time.Time, time.Time) error {
	return nil
}

// importmutTZUsers records the persisted luteal_phase and serves a fixed
// owner timezone from LoadSettingsByID.
type importmutTZUsers struct {
	timezone string
	luteal   int
}

func (u *importmutTZUsers) LoadSettingsByID(context.Context, uint) (models.User, error) {
	return models.User{Timezone: u.timezone}, nil
}
func (u *importmutTZUsers) UpdateByID(_ context.Context, _ uint, updates map[string]any) error {
	if v, ok := updates["luteal_phase"]; ok {
		if lp, ok := v.(int); ok {
			u.luteal = lp
		}
	}
	return nil
}

func TestImportMut_RefreshDerivedCycleSettings_BoundsAtOwnerZoneNotRequestZone(t *testing.T) {
	day := func(s string) time.Time {
		parsed, err := time.Parse("2006-01-02", s)
		if err != nil {
			t.Fatalf("parse %q: %v", s, err)
		}
		return parsed
	}
	bbt := func(v float64) *float64 { return &v }

	// Two BBT-confirmed cycles (Jan1->Jan20, luteal 13; Jan20->Feb8, luteal
	// 12 — same coverline/rise shape as the day-service sibling test) plus a
	// THIRD observed cycle start on Feb 8, the boundary log.
	// InferUserLutealPhase needs 3 starts to refine at all, so whether Feb 8
	// counts as "observed yet" decides refined (13, true) vs the unrefined
	// default (14, false).
	logs := importmutTZLogs{entries: []models.DailyLog{
		{UserID: 42, Date: day("2026-01-01"), IsPeriod: true, CycleStart: true, Flow: models.FlowMedium, BBT: bbt(36.20)},
		{UserID: 42, Date: day("2026-01-02"), BBT: bbt(36.20)},
		{UserID: 42, Date: day("2026-01-03"), BBT: bbt(36.20)},
		{UserID: 42, Date: day("2026-01-04"), BBT: bbt(36.20)},
		{UserID: 42, Date: day("2026-01-05"), BBT: bbt(36.20)},
		{UserID: 42, Date: day("2026-01-06"), BBT: bbt(36.20)},
		{UserID: 42, Date: day("2026-01-07"), BBT: bbt(36.50)},
		{UserID: 42, Date: day("2026-01-08"), BBT: bbt(36.50)},
		{UserID: 42, Date: day("2026-01-09"), BBT: bbt(36.50)},
		{UserID: 42, Date: day("2026-01-20"), IsPeriod: true, CycleStart: true, Flow: models.FlowMedium, BBT: bbt(36.20)},
		{UserID: 42, Date: day("2026-01-21"), BBT: bbt(36.20)},
		{UserID: 42, Date: day("2026-01-22"), BBT: bbt(36.20)},
		{UserID: 42, Date: day("2026-01-23"), BBT: bbt(36.20)},
		{UserID: 42, Date: day("2026-01-24"), BBT: bbt(36.20)},
		{UserID: 42, Date: day("2026-01-25"), BBT: bbt(36.20)},
		{UserID: 42, Date: day("2026-01-27"), BBT: bbt(36.50)},
		{UserID: 42, Date: day("2026-01-28"), BBT: bbt(36.50)},
		{UserID: 42, Date: day("2026-01-29"), BBT: bbt(36.50)},
		{UserID: 42, Date: day("2026-02-08"), IsPeriod: true, CycleStart: true, Flow: models.FlowMedium},
	}}
	users := &importmutTZUsers{timezone: "Pacific/Midway"}
	svc := &ImportService{logs: logs, users: users}

	// now = 2026-02-08T00:30 UTC: the request's zone (UTC, `requestLocation`
	// below) already reads today as Feb 8, the boundary log's own date. The
	// owner's stored zone, Pacific/Midway (UTC-11), reads local time as
	// 2026-02-07T13:30 — today is still Feb 7, one calendar day before the
	// boundary log.
	now := time.Date(2026, time.February, 8, 0, 30, 0, 0, time.UTC)
	requestLocation := time.UTC

	svc.refreshDerivedCycleSettings(context.Background(), 42, now, requestLocation)

	if users.luteal != defaultLutealPhaseDays {
		t.Fatalf("expected luteal_phase bounded at the OWNER's zone (Pacific/Midway, today=Feb 7) to stay the unrefined default %d; got %d — a request-zone bound (UTC, today=Feb 8) would count the not-yet-owner-observed Feb 8 cycle start and refine it to 13", defaultLutealPhaseDays, users.luteal)
	}
}

func TestImportMut_RefreshNilGuardsShortCircuitPerClause(t *testing.T) {
	ctx := context.Background()

	assertNoPanic := func(name string, fn func()) {
		defer func() {
			if r := recover(); r != nil {
				t.Fatalf("%s: refresh must short-circuit, but panicked: %v", name, r)
			}
		}()
		fn()
	}

	// Clause 1: nil *ImportService receiver. Negating `service == nil` skips
	// this clause and dereferences service.users on a nil receiver.
	assertNoPanic("nil receiver", func() {
		var svc *ImportService
		svc.refreshDerivedCycleSettings(ctx, 1, time.Now(), time.UTC)
	})

	// Clause 2: users == nil (logs non-nil and returning data). Negating
	// `service.users == nil` lets the method reach users.UpdateByID on nil.
	assertNoPanic("nil users", func() {
		svc := &ImportService{logs: importmutDataLogs{}, users: nil}
		svc.refreshDerivedCycleSettings(ctx, 1, time.Now(), time.UTC)
	})

	// Clause 3: logs == nil (users non-nil). Negating `service.logs == nil`
	// lets the method reach logs.ListByUser on nil.
	assertNoPanic("nil logs", func() {
		svc := &ImportService{logs: nil, users: importStubUsers{}}
		svc.refreshDerivedCycleSettings(ctx, 1, time.Now(), time.UTC)
	})
}
