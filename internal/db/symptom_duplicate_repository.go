package db

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"

	"github.com/ovumcy/ovumcy-web/internal/models"
	"gorm.io/gorm"
)

// SymptomDuplicateRepository is the offline half of migration 037: it reads the
// duplicate symptom names that stop the migration, and merges them when the
// operator asks.
//
// It is deliberately NOT part of Repositories. Every set built by
// bootstrap.BuildRepositories serves a running instance, and this repository
// exists for the case where there is none: the migration refuses, the server
// never reaches its listener, and the database can only be reached by a process
// that opened it WITHOUT applying migrations. Handing it out beside the serving
// repositories would invite a request path to merge somebody's symptoms.
type SymptomDuplicateRepository struct {
	database *gorm.DB
}

func NewSymptomDuplicateRepository(database *gorm.DB) *SymptomDuplicateRepository {
	return &SymptomDuplicateRepository{database: database}
}

// symptomCatalogueTable is the table every query below reads. It is named once
// so the presence check and the queries cannot come to disagree about which
// table decides whether this is an Ovumcy database.
const symptomCatalogueTable = "symptom_types"

// ErrSymptomCatalogueAbsent is what the repair answers when the database it was
// pointed at holds no symptom catalogue at all.
var ErrSymptomCatalogueAbsent = errors.New("no symptom catalogue (table " + symptomCatalogueTable + ") in this database")

// RequireSymptomCatalogue answers whether this is an Ovumcy database before any
// query assumes it.
//
// This repair is the one entry point that opens a database WITHOUT applying
// migrations, which is also the one thing that stops it telling "a schema older
// than this binary" — exactly what it exists for — from "not this application's
// database at all". Left to the query, the second case comes back as the
// engine's own `no such table`, printed under a dump of the SQL, naming the very
// table the operator was told to repair: the natural reading is that the repair
// is broken or the data is gone. On SQLite the mistake is both easy and silent,
// because opening a path that is not there creates an empty database rather than
// refusing, so a mistyped DB_PATH reaches this point looking like a real one.
func (repo *SymptomDuplicateRepository) RequireSymptomCatalogue(ctx context.Context) error {
	if repo.database.WithContext(ctx).Migrator().HasTable(symptomCatalogueTable) {
		return nil
	}
	return ErrSymptomCatalogueAbsent
}

// duplicateSymptomNameKey is one conflicting (user_id, lower(name)) pair as the
// engine groups it.
type duplicateSymptomNameKey struct {
	UserID      uint   `gorm:"column:user_id"`
	ConflictKey string `gorm:"column:conflict_key"`
}

// ListDuplicateSymptomNameGroups names every group migration 037's index would
// refuse, in the engine's own reading of lower(name).
//
// The grouping expression is the index's, character for character, so the two
// agree by construction rather than by a fold this layer reimplements — which
// is the only way one reader can serve both engines when they disagree about
// what lower() means for a non-ASCII name.
func (repo *SymptomDuplicateRepository) ListDuplicateSymptomNameGroups(ctx context.Context) ([]models.SymptomDuplicateGroup, error) {
	database := repo.database.WithContext(ctx)

	keys := make([]duplicateSymptomNameKey, 0)
	if err := database.Raw(`
SELECT user_id, lower(name) AS conflict_key
FROM symptom_types
GROUP BY user_id, lower(name)
HAVING COUNT(*) > 1
ORDER BY user_id, lower(name)`).Scan(&keys).Error; err != nil {
		return nil, fmt.Errorf("list duplicate symptom name groups: %w", err)
	}

	groups := make([]models.SymptomDuplicateGroup, 0, len(keys))
	for _, key := range keys {
		symptoms := make([]models.SymptomType, 0, 2)
		if err := database.Raw(`
SELECT id, user_id, name, icon, color, is_builtin, archived_at
FROM symptom_types
WHERE user_id = ? AND lower(name) = ?
ORDER BY id`, key.UserID, key.ConflictKey).Scan(&symptoms).Error; err != nil {
			return nil, fmt.Errorf("load duplicate symptom name group %q: %w", key.ConflictKey, err)
		}
		groups = append(groups, models.SymptomDuplicateGroup{
			UserID:      key.UserID,
			ConflictKey: key.ConflictKey,
			Symptoms:    symptoms,
		})
	}
	return groups, nil
}

// MergeDuplicateSymptoms applies a plan: for each merge, every day log naming an
// absorbed symptom names the survivor instead, and the absorbed rows are then
// removed.
//
// The whole plan is one transaction. The two halves cannot be separated: a
// removal that outran its rewrite would leave a day log pointing at a row that
// is gone, which is the one outcome a repair of a health record must not
// produce. A failure anywhere rolls the plan back whole, and the database is
// left exactly as the inspection found it.
func (repo *SymptomDuplicateRepository) MergeDuplicateSymptoms(ctx context.Context, merges []models.SymptomMerge) (models.SymptomMergeOutcome, error) {
	outcome := models.SymptomMergeOutcome{}
	if len(merges) == 0 {
		return outcome, nil
	}

	err := repo.database.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		// The day-log rewrite is one pass per OWNER, not one per merge. An owner
		// normally arrives here with many groups at once — the built-in seeding
		// race duplicates the whole built-in set in one go — and a pass per
		// merge would walk the same day once per group it names, rewriting it
		// repeatedly and reporting a count several times the number of days it
		// actually touched. That number is the operator's evidence that nothing
		// was lost, so it has to count days rather than passes.
		absorbedByOwner, owners := absorbedSymptomsByOwner(merges)
		for _, owner := range owners {
			rewritten, err := repointDailyLogSymptomIDs(tx, owner, absorbedByOwner[owner])
			if err != nil {
				return err
			}
			outcome.DailyLogsRewritten += rewritten
		}

		// Every reference has moved before any row goes, so no day can point at
		// a row that is already gone even midway through the transaction.
		for _, merge := range merges {
			if len(merge.Absorbed) == 0 {
				continue
			}
			for _, absorbed := range merge.Absorbed {
				// Scoped by user_id as well as by id: a repair is a write on
				// somebody's health record, and no write here may be able to
				// reach a row that belongs to another owner.
				result := tx.Exec(
					`DELETE FROM symptom_types WHERE id = ? AND user_id = ?`,
					absorbed.ID, merge.UserID,
				)
				if result.Error != nil {
					return fmt.Errorf("remove duplicate symptom %d: %w", absorbed.ID, result.Error)
				}
				outcome.SymptomsRemoved += int(result.RowsAffected)
			}
			outcome.GroupsMerged++
		}
		return nil
	})
	if err != nil {
		return models.SymptomMergeOutcome{}, err
	}
	return outcome, nil
}

// absorbedSymptomsByOwner folds a plan into one absorbed-to-survivor map per
// owner, alongside the owners in the order the plan first names them so a run
// is reproducible rather than following map iteration.
func absorbedSymptomsByOwner(merges []models.SymptomMerge) (map[uint]map[uint]uint, []uint) {
	absorbedByOwner := make(map[uint]map[uint]uint, len(merges))
	owners := make([]uint, 0, len(merges))
	for _, merge := range merges {
		if len(merge.Absorbed) == 0 {
			continue
		}
		remap, known := absorbedByOwner[merge.UserID]
		if !known {
			remap = make(map[uint]uint, len(merge.Absorbed))
			absorbedByOwner[merge.UserID] = remap
			owners = append(owners, merge.UserID)
		}
		for _, absorbed := range merge.Absorbed {
			remap[absorbed.ID] = merge.Survivor.ID
		}
	}
	return absorbedByOwner, owners
}

// dailyLogSymptomIDsRow is one day's symptom references as the column stores
// them: a JSON array in TEXT on both engines, nullable since migration 003.
type dailyLogSymptomIDsRow struct {
	ID         uint    `gorm:"column:id"`
	SymptomIDs *string `gorm:"column:symptom_ids"`
}

// repointDailyLogSymptomIDs moves one owner's day-log references off the
// absorbed symptoms and onto their survivors.
//
// It reads and rewrites the raw column rather than going through the DailyLog
// model on purpose: the model's BeforeSave hook re-derives the date, and a
// repair has no business rewriting a day's date. For the same reason the UPDATE
// names only symptom_ids — updated_at stays where the owner's last real edit
// left it.
func repointDailyLogSymptomIDs(tx *gorm.DB, userID uint, absorbedIntoSurvivor map[uint]uint) (int, error) {
	rows := make([]dailyLogSymptomIDsRow, 0)
	if err := tx.Raw(
		`SELECT id, symptom_ids FROM daily_logs WHERE user_id = ? ORDER BY id`,
		userID,
	).Scan(&rows).Error; err != nil {
		return 0, fmt.Errorf("load day logs for owner %d: %w", userID, err)
	}

	rewritten := 0
	for _, row := range rows {
		if row.SymptomIDs == nil {
			continue
		}
		repointed, changed, err := repointSymptomIDList(*row.SymptomIDs, absorbedIntoSurvivor)
		if err != nil {
			return 0, fmt.Errorf("read symptom references of day log %d: %w", row.ID, err)
		}
		if !changed {
			continue
		}
		if err := tx.Exec(
			`UPDATE daily_logs SET symptom_ids = ? WHERE id = ? AND user_id = ?`,
			repointed, row.ID, userID,
		).Error; err != nil {
			return 0, fmt.Errorf("rewrite symptom references of day log %d: %w", row.ID, err)
		}
		rewritten++
	}
	return rewritten, nil
}

// repointSymptomIDList rewrites one day's JSON array, keeping the order the
// owner's list already had and collapsing the pair that a merge turns into one
// id. A value it cannot read is an error rather than an empty list: the column
// holds which symptoms somebody recorded that day, and silently replacing an
// unreadable one with [] would delete exactly the data this repair exists to
// keep.
func repointSymptomIDList(stored string, absorbedIntoSurvivor map[uint]uint) (string, bool, error) {
	if stored == "" {
		return stored, false, nil
	}

	ids := make([]uint, 0)
	if err := json.Unmarshal([]byte(stored), &ids); err != nil {
		return "", false, fmt.Errorf("symptom_ids is not a JSON array of ids (%q): %w", stored, err)
	}

	// The remap alone decides whether this day is written at all. A day the
	// merge does not name is left byte for byte as the owner left it, including
	// a repeated id this repair was never asked about: the operator consented to
	// a plan naming specific symptom rows, and tidying a row outside that plan
	// would be a write on a health record nobody requested.
	moved := make([]uint, len(ids))
	remapped := false
	for index, id := range ids {
		if survivor, absorbed := absorbedIntoSurvivor[id]; absorbed {
			id = survivor
			remapped = true
		}
		moved[index] = id
	}
	if !remapped {
		return stored, false, nil
	}

	// Only now does the pair the remap just created collapse. The survivor keeps
	// the absorbed row's position, because that is where the owner's own order
	// put it, and the later mention of the same symptom is what goes.
	repointed := make([]uint, 0, len(moved))
	seen := make(map[uint]struct{}, len(moved))
	for _, id := range moved {
		if _, duplicate := seen[id]; duplicate {
			continue
		}
		seen[id] = struct{}{}
		repointed = append(repointed, id)
	}

	encoded, err := json.Marshal(repointed)
	if err != nil {
		return "", false, fmt.Errorf("encode symptom references: %w", err) // codecov:ignore -- defensive: []uint always marshals
	}
	return string(encoded), true, nil
}
