package services

import (
	"context"
	"errors"
	"fmt"
	"sort"

	"github.com/ovumcy/ovumcy-web/internal/models"
)

var (
	ErrSymptomDuplicateInspectFailed = errors.New("symptom duplicate inspection failed")
	ErrSymptomDuplicateMergeFailed   = errors.New("symptom duplicate merge failed")
)

// SymptomDuplicateRepository is the offline view of the symptom catalogue: the
// groups migration 037's per-owner name index cannot cover, and the one write
// that resolves them.
type SymptomDuplicateRepository interface {
	ListDuplicateSymptomNameGroups(ctx context.Context) ([]models.SymptomDuplicateGroup, error)
	MergeDuplicateSymptoms(ctx context.Context, merges []models.SymptomMerge) (models.SymptomMergeOutcome, error)
}

// SymptomDuplicateRepairService is the operator's way out of a refused
// migration 037.
//
// The refusal itself is correct and stays: a migration may not delete, merge or
// rewrite a row to make room for its own index, because nobody asked the owner.
// This service is where that consent arrives — an operator running a named
// subcommand, on a stopped instance, after a backup — and it does the one thing
// the migration would not: it collapses each colliding group into a single
// symptom and moves every day-log reference onto it.
//
// Merging rather than renaming is forced by the product, not chosen for
// convenience. The reachable half of this class is the built-in seeding race,
// so a group is typically two identical BUILT-IN rows — and a built-in symptom
// can be neither renamed, archived nor removed through the application, and
// does not appear on the settings page at all. A rename would therefore leave
// the owner a permanent second "Cramps" in the day-entry picker with no surface
// anywhere that can clear it, which is a worse outcome than one row carrying
// both days.
type SymptomDuplicateRepairService struct {
	duplicates SymptomDuplicateRepository
}

func NewSymptomDuplicateRepairService(duplicates SymptomDuplicateRepository) *SymptomDuplicateRepairService {
	return &SymptomDuplicateRepairService{duplicates: duplicates}
}

// Inspect reports every group that would refuse the migration, changing
// nothing.
func (service *SymptomDuplicateRepairService) Inspect(ctx context.Context) ([]models.SymptomDuplicateGroup, error) {
	groups, err := service.duplicates.ListDuplicateSymptomNameGroups(ctx)
	if err != nil {
		return nil, fmt.Errorf("%w: %v", ErrSymptomDuplicateInspectFailed, err)
	}
	return groups, nil
}

// Repair inspects and then merges, returning the plan it carried out alongside
// what changed. Running it a second time finds no groups, plans nothing and
// reports a zero outcome — the repair is the operator's to repeat safely,
// including after a partial run that failed and rolled back.
func (service *SymptomDuplicateRepairService) Repair(ctx context.Context) ([]models.SymptomMerge, models.SymptomMergeOutcome, error) {
	groups, err := service.Inspect(ctx)
	if err != nil {
		return nil, models.SymptomMergeOutcome{}, err
	}

	merges := PlanSymptomDuplicateMerges(groups)
	if len(merges) == 0 {
		return nil, models.SymptomMergeOutcome{}, nil
	}

	outcome, err := service.duplicates.MergeDuplicateSymptoms(ctx, merges)
	if err != nil {
		return nil, models.SymptomMergeOutcome{}, fmt.Errorf("%w: %v", ErrSymptomDuplicateMergeFailed, err)
	}
	return merges, outcome, nil
}

// PlanSymptomDuplicateMerges chooses, for each group, the row that survives.
//
// The order is active before archived, then built-in before custom, then oldest
// id. Active comes first because a merge must never bury a symptom the owner is
// still using under an archived row — the archived one would keep the days and
// disappear from the picker. Built-in comes next so the catalogue keeps the
// identity the picker's built-in handling is keyed on; it can only decide a
// group whose rows agree on being active, since a built-in is unarchivable
// through the application. The oldest id last, so the row the owner has had
// longest is the one that stays and the choice is deterministic.
//
// A group of fewer than two rows plans nothing: it is not a conflict, and a
// merge with no absorbed row would be a write with no reason.
func PlanSymptomDuplicateMerges(groups []models.SymptomDuplicateGroup) []models.SymptomMerge {
	merges := make([]models.SymptomMerge, 0, len(groups))
	for _, group := range groups {
		if len(group.Symptoms) < 2 {
			continue
		}

		ordered := make([]models.SymptomType, len(group.Symptoms))
		copy(ordered, group.Symptoms)
		sort.SliceStable(ordered, func(first, second int) bool {
			return symptomSurvivesOver(ordered[first], ordered[second])
		})

		merges = append(merges, models.SymptomMerge{
			UserID:   group.UserID,
			Survivor: ordered[0],
			Absorbed: ordered[1:],
		})
	}
	return merges
}

// symptomSurvivesOver is that order as one comparison. The id decides last and
// is unique within a group, so no two rows compare equal and the survivor never
// depends on the order the rows were read in.
func symptomSurvivesOver(candidate models.SymptomType, other models.SymptomType) bool {
	if candidate.IsActive() != other.IsActive() {
		return candidate.IsActive()
	}
	if candidate.IsBuiltin != other.IsBuiltin {
		return candidate.IsBuiltin
	}
	return candidate.ID < other.ID
}
