package services

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/ovumcy/ovumcy-web/internal/models"
)

type stubSymptomDuplicateRepository struct {
	groups     []models.SymptomDuplicateGroup
	listErr    error
	mergeErr   error
	outcome    models.SymptomMergeOutcome
	seenMerges []models.SymptomMerge
	mergeCalls int
}

func (stub *stubSymptomDuplicateRepository) ListDuplicateSymptomNameGroups(context.Context) ([]models.SymptomDuplicateGroup, error) {
	if stub.listErr != nil {
		return nil, stub.listErr
	}
	return stub.groups, nil
}

func (stub *stubSymptomDuplicateRepository) MergeDuplicateSymptoms(_ context.Context, merges []models.SymptomMerge) (models.SymptomMergeOutcome, error) {
	stub.mergeCalls++
	stub.seenMerges = merges
	if stub.mergeErr != nil {
		return models.SymptomMergeOutcome{}, stub.mergeErr
	}
	return stub.outcome, nil
}

func archivedAt() *time.Time {
	moment := time.Date(2026, time.January, 2, 3, 4, 5, 0, time.UTC)
	return &moment
}

// TestPlanSymptomDuplicateMergesKeepsTheRowTheOwnerCanStillReach walks the
// survivor order one rule at a time, each case reaching exactly one of them.
//
// The order is not cosmetic. Keeping an archived row would take the symptom out
// of the day-entry picker while it kept every day that named it, and keeping a
// custom row over a built-in would change what the catalogue calls a built-in
// for that account.
func TestPlanSymptomDuplicateMergesKeepsTheRowTheOwnerCanStillReach(t *testing.T) {
	t.Parallel()

	for _, testCase := range []struct {
		Name             string
		Symptoms         []models.SymptomType
		ExpectedSurvivor uint
	}{
		{
			Name: "an active row outranks an archived one, whatever their ids",
			Symptoms: []models.SymptomType{
				{ID: 4, Name: "Cramps", ArchivedAt: archivedAt()},
				{ID: 9, Name: "cramps"},
			},
			ExpectedSurvivor: 9,
		},
		{
			Name: "among active rows the built-in outranks the custom one",
			Symptoms: []models.SymptomType{
				{ID: 3, Name: "cramps"},
				{ID: 8, Name: "Cramps", IsBuiltin: true},
			},
			ExpectedSurvivor: 8,
		},
		{
			Name: "otherwise the oldest row stays",
			Symptoms: []models.SymptomType{
				{ID: 12, Name: "Cramps", IsBuiltin: true},
				{ID: 5, Name: "cramps", IsBuiltin: true},
			},
			ExpectedSurvivor: 5,
		},
		{
			Name: "an archived pair still resolves, oldest first",
			Symptoms: []models.SymptomType{
				{ID: 7, Name: "Cramps", ArchivedAt: archivedAt()},
				{ID: 2, Name: "cramps", ArchivedAt: archivedAt()},
			},
			ExpectedSurvivor: 2,
		},
	} {
		t.Run(testCase.Name, func(t *testing.T) {
			t.Parallel()

			merges := PlanSymptomDuplicateMerges([]models.SymptomDuplicateGroup{
				{UserID: 41, ConflictKey: "cramps", Symptoms: testCase.Symptoms},
			})
			if len(merges) != 1 {
				t.Fatalf("expected one merge, got %d", len(merges))
			}
			if merges[0].Survivor.ID != testCase.ExpectedSurvivor {
				t.Fatalf("expected symptom %d to survive, got %d", testCase.ExpectedSurvivor, merges[0].Survivor.ID)
			}
			if len(merges[0].Absorbed) != len(testCase.Symptoms)-1 {
				t.Fatalf("every other row must be absorbed, got %d", len(merges[0].Absorbed))
			}
			for _, absorbed := range merges[0].Absorbed {
				if absorbed.ID == testCase.ExpectedSurvivor {
					t.Fatal("the survivor must not also be absorbed")
				}
			}
			if merges[0].UserID != 41 {
				t.Fatalf("a merge stays with its owner, got %d", merges[0].UserID)
			}
		})
	}
}

// TestPlanSymptomDuplicateMergesPlansNothingForAGroupThatIsNotOne keeps the
// planner from turning a report into a write. A group of one row is what an
// engine returns for a name nothing collides on, and a merge with no absorbed
// row would delete nothing while still opening a transaction over somebody's
// health record.
func TestPlanSymptomDuplicateMergesPlansNothingForAGroupThatIsNotOne(t *testing.T) {
	t.Parallel()

	merges := PlanSymptomDuplicateMerges([]models.SymptomDuplicateGroup{
		{UserID: 1, ConflictKey: "cramps", Symptoms: []models.SymptomType{{ID: 1, Name: "Cramps"}}},
		{UserID: 2, ConflictKey: "headache", Symptoms: nil},
	})
	if len(merges) != 0 {
		t.Fatalf("expected no merges, got %d", len(merges))
	}
}

// TestRepairMergesNothingWhenThereIsNothingToMerge pins that a clean database
// costs no write at all: the repair is documented as repeatable, and a second
// run that still opened a merge transaction would make that claim weaker than
// it reads.
func TestRepairMergesNothingWhenThereIsNothingToMerge(t *testing.T) {
	t.Parallel()

	repository := &stubSymptomDuplicateRepository{}
	merges, outcome, err := NewSymptomDuplicateRepairService(repository).Repair(context.Background())
	if err != nil {
		t.Fatalf("repair on a clean database returned error: %v", err)
	}
	if len(merges) != 0 || outcome != (models.SymptomMergeOutcome{}) {
		t.Fatalf("expected an empty result, got %v / %+v", merges, outcome)
	}
	if repository.mergeCalls != 0 {
		t.Fatalf("expected no merge call, got %d", repository.mergeCalls)
	}
}

// TestRepairCarriesTheStorageFailureUnderItsOwnSentinel keeps a failure
// diagnosable on both halves: an operator reading it has to be able to tell an
// inspection that could not read the catalogue from a merge that could not
// write it, because only one of the two leaves the database possibly touched.
func TestRepairCarriesTheStorageFailureUnderItsOwnSentinel(t *testing.T) {
	t.Parallel()

	storageErr := errors.New("connection refused")

	if _, err := NewSymptomDuplicateRepairService(&stubSymptomDuplicateRepository{listErr: storageErr}).Inspect(context.Background()); !errors.Is(err, ErrSymptomDuplicateInspectFailed) {
		t.Fatalf("expected the inspection sentinel, got %v", err)
	}

	_, _, err := NewSymptomDuplicateRepairService(&stubSymptomDuplicateRepository{
		groups: []models.SymptomDuplicateGroup{{
			UserID:   3,
			Symptoms: []models.SymptomType{{ID: 1, Name: "Cramps"}, {ID: 2, Name: "cramps"}},
		}},
		mergeErr: storageErr,
	}).Repair(context.Background())
	if !errors.Is(err, ErrSymptomDuplicateMergeFailed) {
		t.Fatalf("expected the merge sentinel, got %v", err)
	}
	if !strings.Contains(err.Error(), storageErr.Error()) {
		t.Fatalf("the storage error must survive into the message, got %v", err)
	}
}
