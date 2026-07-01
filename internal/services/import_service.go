package services

import (
	"context"
	"encoding/json"
	"errors"
	"sort"
	"strings"
	"time"

	"github.com/ovumcy/ovumcy-web/internal/models"
)

// MaxImportEntries bounds how many day records a single restore payload may
// carry. Well above a lifetime of daily logging (~50 years), but finite so a
// crafted file cannot force unbounded work. The transport layer additionally
// caps the raw body size before this is ever reached.
const MaxImportEntries = 20000

// MaxImportCustomSymptoms bounds how many *new* custom symptoms a single restore
// may create. Symptom reconciliation creates one row per distinct
// other_symptoms name, outside the day-write transaction and each preceded by a
// catalog scan; without this cap a crafted file could force hundreds of
// thousands of INSERTs (O(n^2) with the growing catalog) plus permanent catalog
// bloat — a persistent DoS an authenticated owner could self-inflict. The bound
// is far above any realistic hand-curated catalog; names beyond it are skipped
// (the day still imports, just without that association), mirroring how an
// invalid name is dropped. Symmetric to MaxImportEntries for day rows.
const MaxImportCustomSymptoms = 200

var (
	// ErrImportMalformed marks a payload that is not the expected JSON shape.
	ErrImportMalformed = errors.New("import payload is malformed")
	// ErrImportTooLarge marks a payload carrying more than MaxImportEntries.
	ErrImportTooLarge = errors.New("import payload too large")
	// ErrImportWriteFailed marks a persistence failure during the atomic
	// restore transaction; the whole import is rolled back when it fires.
	ErrImportWriteFailed = errors.New("import write failed")
)

// importSymptomFlagGetters mirrors exportSymptomFlagSetters in reverse: given a
// built-in symptom column key it reports whether the corresponding flag is set
// on an exported entry. Deriving symptom IDs through this map plus
// exportSymptomColumn keeps the import direction in lockstep with the export
// direction without duplicating the name→column vocabulary.
var importSymptomFlagGetters = map[string]func(ExportSymptomFlags) bool{
	"cramps":            func(f ExportSymptomFlags) bool { return f.Cramps },
	"headache":          func(f ExportSymptomFlags) bool { return f.Headache },
	"acne":              func(f ExportSymptomFlags) bool { return f.Acne },
	"mood":              func(f ExportSymptomFlags) bool { return f.Mood },
	"bloating":          func(f ExportSymptomFlags) bool { return f.Bloating },
	"fatigue":           func(f ExportSymptomFlags) bool { return f.Fatigue },
	"breast_tenderness": func(f ExportSymptomFlags) bool { return f.BreastTenderness },
	"back_pain":         func(f ExportSymptomFlags) bool { return f.BackPain },
	"nausea":            func(f ExportSymptomFlags) bool { return f.Nausea },
	"spotting":          func(f ExportSymptomFlags) bool { return f.Spotting },
	"irritability":      func(f ExportSymptomFlags) bool { return f.Irritability },
	"insomnia":          func(f ExportSymptomFlags) bool { return f.Insomnia },
	"food_cravings":     func(f ExportSymptomFlags) bool { return f.FoodCravings },
	"diarrhea":          func(f ExportSymptomFlags) bool { return f.Diarrhea },
	"constipation":      func(f ExportSymptomFlags) bool { return f.Constipation },
}

// ImportResult reports the outcome of a restore. Added and Skipped sum to the
// number of well-formed days in the payload; Rejected counts malformed or
// duplicate day records that were dropped without touching the database.
type ImportResult struct {
	Added    int
	Skipped  int
	Rejected int
}

// ImportSymptomReconciler is the subset of SymptomService the import path
// needs: guarantee the built-in catalog exists, read it, and create missing
// custom symptoms by name.
type ImportSymptomReconciler interface {
	EnsureBuiltinSymptoms(ctx context.Context, userID uint) error
	FetchSymptoms(ctx context.Context, userID uint) ([]models.SymptomType, error)
	CreateSymptomForUser(ctx context.Context, userID uint, name string, icon string, color string) (models.SymptomType, error)
}

// ImportService restores an owner's own Ovumcy JSON export back into their
// account. It is additive by design: existing days are never overwritten or
// deleted, so no re-authentication is required (contrast clear-data). Every
// write is scoped to the authenticated user_id and every field is treated as
// untrusted input and re-normalized through the same policies as a manual day
// entry.
type ImportService struct {
	logs     DayLogRepository
	users    DayUserRepository
	symptoms ImportSymptomReconciler
	runInTx  DayLogTxRunner
}

// NewImportService wires the restore path. runInTx may be nil (tests with
// in-memory stubs), in which case the write loop runs non-atomically against
// the base repository.
func NewImportService(logs DayLogRepository, users DayUserRepository, symptoms ImportSymptomReconciler, runInTx DayLogTxRunner) *ImportService {
	return &ImportService{
		logs:     logs,
		users:    users,
		symptoms: symptoms,
		runInTx:  runInTx,
	}
}

func (service *ImportService) withinTransaction(ctx context.Context, fn func(DayLogRepository) error) error {
	if service.runInTx != nil {
		return service.runInTx(ctx, fn)
	}
	return fn(service.logs)
}

type importPayload struct {
	Entries []ExportJSONEntry `json:"entries"`
}

// plannedImportDay is a fully validated day held between the parse pass and the
// atomic write pass. Symptom IDs are resolved only in the write pass, after any
// missing custom symptoms have been created.
type plannedImportDay struct {
	dayStart    time.Time
	input       DayEntryInput
	cycleStart  bool
	isUncertain bool
	flags       ExportSymptomFlags
	otherNames  []string
}

// ImportJSON parses a prior JSON export and creates every day it does not
// already have, in a single transaction. Malformed or duplicate day records
// are counted as rejected and skipped without aborting the restore. Days that
// already exist are left untouched (skipped). location canonicalizes the
// calendar day; the stored Date is UTC-midnight, matching the export shape.
func (service *ImportService) ImportJSON(ctx context.Context, userID uint, raw []byte, location *time.Location) (ImportResult, error) {
	if location == nil {
		location = time.UTC
	}

	var payload importPayload
	if err := json.Unmarshal(raw, &payload); err != nil {
		return ImportResult{}, ErrImportMalformed
	}
	if len(payload.Entries) > MaxImportEntries {
		return ImportResult{}, ErrImportTooLarge
	}

	planned, otherOriginals, rejected := service.planEntries(payload.Entries, location)

	catalogByKey, builtins, err := service.reconcileSymptoms(ctx, userID, otherOriginals)
	if err != nil {
		return ImportResult{}, err
	}

	added, skipped, err := service.writeDays(ctx, userID, planned, builtins, catalogByKey)
	if err != nil {
		return ImportResult{}, err
	}

	service.refreshDerivedCycleSettings(ctx, userID, location)

	return ImportResult{Added: added, Skipped: skipped, Rejected: rejected}, nil
}

// planEntries validates every incoming record: it parses and canonicalizes the
// date, rejects duplicate calendar days within the file, and normalizes every
// field through NormalizeDayEntryInput. It also collects the first-seen spelling
// of each custom symptom name so the reconciler can create the missing ones.
func (service *ImportService) planEntries(entries []ExportJSONEntry, location *time.Location) ([]plannedImportDay, map[string]string, int) {
	planned := make([]plannedImportDay, 0, len(entries))
	otherOriginals := make(map[string]string)
	seen := make(map[string]struct{}, len(entries))
	rejected := 0

	for _, entry := range entries {
		parsed, err := ParseDayDate(entry.Date, location)
		if err != nil {
			rejected++
			continue
		}
		// Date-only stored value: canonicalize to UTC-midnight from the parsed
		// calendar components WITHOUT In(location), or negative-offset zones
		// shift the day backward (day_utils.go / issue #48).
		dayStart := CalendarDay(parsed, time.UTC)
		dayKey := CalendarDayKey(dayStart)
		if _, dup := seen[dayKey]; dup {
			rejected++
			continue
		}

		seen[dayKey] = struct{}{}
		input := normalizeImportEntryInput(entry)

		// cycle_start / is_uncertain only make sense on a period day; drop them
		// otherwise so a crafted file cannot persist an inconsistent anchor.
		planned = append(planned, plannedImportDay{
			dayStart:    dayStart,
			input:       input,
			cycleStart:  entry.CycleStart && input.IsPeriod,
			isUncertain: entry.IsUncertain && input.IsPeriod,
			flags:       entry.Symptoms,
			otherNames:  entry.OtherSymptoms,
		})

		for _, name := range entry.OtherSymptoms {
			key := normalizeSymptomNameKey(name)
			if key == "" {
				continue
			}
			if _, ok := otherOriginals[key]; !ok {
				otherOriginals[key] = strings.TrimSpace(name)
			}
		}
	}

	return planned, otherOriginals, rejected
}

// normalizeImportEntryInput sanitizes every field of an incoming record to a
// safe value, mirroring what the export writer emits: unknown or malformed
// enum values collapse to their neutral default (none/0) rather than aborting
// the whole day, notes are length-capped, and invalid cycle-factor keys are
// dropped. This keeps a minimal or foreign file importable while guaranteeing
// no out-of-vocabulary value reaches the database. Flow is forced to none on a
// non-period day, matching the manual-entry contract.
func normalizeImportEntryInput(entry ExportJSONEntry) DayEntryInput {
	cycleFactors, _ := NormalizeDayCycleFactorKeys(entry.CycleFactors)
	input := DayEntryInput{
		IsPeriod:        entry.Period,
		Flow:            NormalizeDayFlow(entry.Flow),
		Mood:            normalizeExportMood(entry.MoodRating),
		SexActivity:     NormalizeDaySexActivity(entry.SexActivity),
		BBT:             normalizeExportBBT(entry.BBT),
		CervicalMucus:   NormalizeDayCervicalMucus(entry.CervicalMucus),
		PregnancyTest:   NormalizeDayPregnancyTest(entry.PregnancyTest),
		CycleFactorKeys: cycleFactors,
		Notes:           TrimDayNotes(entry.Notes),
	}
	if !input.IsPeriod {
		input.Flow = models.FlowNone
	}
	return input
}

// reconcileSymptoms guarantees the built-in catalog exists, indexes it by
// normalized name, and creates any custom symptom named in the payload that is
// not already present, up to MaxImportCustomSymptoms new rows. Symptom names
// that fail validation (too long, reserved, invalid characters) or exceed the
// cap are silently skipped: the day still imports, just without that
// association. Created symptoms live outside the day-write transaction, so a
// later rollback may leave an empty custom symptom behind — harmless,
// owner-scoped, and now bounded by the cap.
func (service *ImportService) reconcileSymptoms(ctx context.Context, userID uint, otherOriginals map[string]string) (map[string]uint, []models.SymptomType, error) {
	catalog, err := service.symptoms.FetchSymptoms(ctx, userID)
	if err != nil {
		return nil, nil, err
	}

	catalogByKey := make(map[string]uint, len(catalog))
	builtins := make([]models.SymptomType, 0, len(catalog))
	for _, symptom := range catalog {
		key := normalizeSymptomNameKey(symptom.Name)
		if key != "" {
			if _, ok := catalogByKey[key]; !ok {
				catalogByKey[key] = symptom.ID
			}
		}
		if symptom.IsBuiltin {
			builtins = append(builtins, symptom)
		}
	}

	createdCount := 0
	for key, original := range otherOriginals {
		if _, ok := catalogByKey[key]; ok {
			continue
		}
		if createdCount >= MaxImportCustomSymptoms {
			break
		}
		created, err := service.symptoms.CreateSymptomForUser(ctx, userID, original, "", "")
		if err != nil {
			continue
		}
		catalogByKey[normalizeSymptomNameKey(created.Name)] = created.ID
		createdCount++
	}

	return catalogByKey, builtins, nil
}

// writeDays creates the missing days atomically. Existing days are counted as
// skipped and left exactly as they are.
func (service *ImportService) writeDays(ctx context.Context, userID uint, planned []plannedImportDay, builtins []models.SymptomType, catalogByKey map[string]uint) (int, int, error) {
	added, skipped := 0, 0

	err := service.withinTransaction(ctx, func(txLogs DayLogRepository) error {
		for _, day := range planned {
			dayEnd := day.dayStart.AddDate(0, 0, 1)
			_, found, err := txLogs.FindByUserAndDayRange(ctx, userID, day.dayStart, dayEnd)
			if err != nil {
				return ErrImportWriteFailed
			}
			if found {
				skipped++
				continue
			}

			symptomIDs := resolveImportSymptomIDs(day.flags, day.otherNames, builtins, catalogByKey)
			entry := models.DailyLog{
				UserID:          userID,
				Date:            day.dayStart,
				IsPeriod:        day.input.IsPeriod,
				CycleStart:      day.cycleStart,
				IsUncertain:     day.isUncertain,
				Flow:            day.input.Flow,
				Mood:            day.input.Mood,
				SexActivity:     day.input.SexActivity,
				BBT:             day.input.BBT,
				CervicalMucus:   day.input.CervicalMucus,
				PregnancyTest:   day.input.PregnancyTest,
				CycleFactorKeys: day.input.CycleFactorKeys,
				SymptomIDs:      symptomIDs,
				Notes:           day.input.Notes,
			}
			if err := txLogs.Create(ctx, &entry); err != nil {
				return ErrImportWriteFailed
			}
			added++
		}
		return nil
	})
	if err != nil {
		return 0, 0, err
	}

	return added, skipped, nil
}

// resolveImportSymptomIDs turns the exported symptom representation back into
// owner-scoped symptom IDs: built-in flags map through exportSymptomColumn to
// the matching catalog entry, and each custom name resolves through the
// (already reconciled) name index. All IDs originate from the owner's own
// catalog, so no cross-account reference is possible.
func resolveImportSymptomIDs(flags ExportSymptomFlags, otherNames []string, builtins []models.SymptomType, catalogByKey map[string]uint) []uint {
	set := make(map[uint]struct{})

	for _, symptom := range builtins {
		column := exportSymptomColumn(symptom.Name)
		if column == "other" {
			continue
		}
		if getter, ok := importSymptomFlagGetters[column]; ok && getter(flags) {
			set[symptom.ID] = struct{}{}
		}
	}

	for _, name := range otherNames {
		key := normalizeSymptomNameKey(name)
		if key == "" {
			continue
		}
		if id, ok := catalogByKey[key]; ok {
			set[id] = struct{}{}
		}
	}

	if len(set) == 0 {
		return []uint{}
	}
	ids := make([]uint, 0, len(set))
	for id := range set {
		ids = append(ids, id)
	}
	sort.Slice(ids, func(i, j int) bool { return ids[i] < ids[j] })
	return ids
}

// refreshDerivedCycleSettings recomputes the owner's luteal-phase estimate once
// after a bulk restore. Mirrors DayService.refreshDerivedCycleSettings; kept as
// a best-effort side effect (a failure here never fails the import).
func (service *ImportService) refreshDerivedCycleSettings(ctx context.Context, userID uint, location *time.Location) {
	if service == nil || service.users == nil || service.logs == nil {
		return
	}
	logs, err := service.logs.ListByUser(ctx, userID)
	if err != nil {
		return
	}
	lutealPhase, ok := InferUserLutealPhase(logs, location)
	if !ok {
		lutealPhase = defaultLutealPhaseDays
	}
	_ = service.users.UpdateByID(ctx, userID, map[string]any{"luteal_phase": lutealPhase})
}
