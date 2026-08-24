package services

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/ovumcy/ovumcy-web/internal/db"
	"github.com/ovumcy/ovumcy-web/internal/models"
	"gorm.io/gorm"
)

// Barrier for the per-owner symptom-name uniqueness class.
//
// The name rule used to live only in application code: the service listed the
// owner's symptoms, compared normalized keys in memory and then issued an
// unrelated INSERT, with no transaction, lock or constraint joining the two.
// Two requests that pass the check before either writes both write, and the
// owner ends up with two symptoms the picker, the frequency counts and the
// export cannot tell apart. The builtin-seeding path had the same shape on a
// READ path — two post-registration page loads each list, each compute the
// missing builtins and each insert them — which is the more reachable half.
//
// What closes it is a UNIQUE index in the schema, so the second writer is
// refused by the database rather than by a check it already passed. These cases
// drive the real repository against a real SQLite file, because a fake
// repository cannot refuse anything the service did not already refuse itself.
//
// What the index does NOT do is stated by
// TestSymptomNameIndexDoesNotCollapseTheWhitespaceTheServiceCollapses below:
// portable SQL can lower a name, it cannot collapse internal whitespace runs,
// so the index is a backstop strictly weaker than the service rule and the
// service check stays in front of it.

// newSymptomListRendezvous returns a function that blocks until it has been
// called by count goroutines, and lets every later call through.
//
// It is installed on the seam the class lives in — the return of ListByUser,
// after the availability check has read the catalogue and before anything is
// written — so both writers hold a decision made from the same snapshot. Simply
// starting two goroutines together does not reproduce the defect reliably: one
// usually finishes its INSERT before the other lists, and the guard then passes
// on a tree that has no constraint at all.
func newSymptomListRendezvous(count int) func() {
	mutex := sync.Mutex{}
	arrived := 0
	gate := make(chan struct{})

	return func() {
		mutex.Lock()
		if arrived < count {
			arrived++
			if arrived == count {
				close(gate)
			}
		}
		mutex.Unlock()
		<-gate
	}
}

// barrieredSymptomRepository is the real repository with a rendezvous on the
// return of ListByUser. Everything else passes straight through, so the writes
// under test are the shipped ones.
type barrieredSymptomRepository struct {
	SymptomRepository
	afterList func()
}

func (repo barrieredSymptomRepository) ListByUser(ctx context.Context, userID uint) ([]models.SymptomType, error) {
	symptoms, err := repo.SymptomRepository.ListByUser(ctx, userID)
	repo.afterList()
	return symptoms, err
}

// symptomUniquenessRace runs fn in parallel from n goroutines released
// together, and returns each call's error in start order.
func symptomUniquenessRace(count int, fn func(index int) error) []error {
	release := make(chan struct{})
	ready := sync.WaitGroup{}
	done := sync.WaitGroup{}
	results := make([]error, count)

	for worker := range count {
		ready.Add(1)
		done.Add(1)
		go func(index int) {
			defer done.Done()
			ready.Done()
			<-release
			results[index] = fn(index)
		}(worker)
	}

	ready.Wait()
	close(release)
	done.Wait()
	return results
}

func countSymptomRowsNamed(t *testing.T, database *gorm.DB, userID uint, name string) int64 {
	t.Helper()

	var rows int64
	if err := database.Model(&models.SymptomType{}).
		Where("user_id = ? AND name = ?", userID, name).
		Count(&rows).Error; err != nil {
		t.Fatalf("count symptom rows named %q: %v", name, err)
	}
	return rows
}

// TestConcurrentSymptomCreatesOfOneNameLeaveExactlyOneRow is the found red for
// R2-0100: before the unique index existed both goroutines committed and the
// owner held two rows called "Joint stiffness".
func TestConcurrentSymptomCreatesOfOneNameLeaveExactlyOneRow(t *testing.T) {
	_, database := newDayServiceIntegration(t)
	user := createDayServiceTestUser(t, database, "symptom-name-race@example.com")

	repositories := db.NewRepositories(database)
	service := NewSymptomService(barrieredSymptomRepository{
		SymptomRepository: repositories.Symptoms,
		afterList:         newSymptomListRendezvous(2),
	})

	errs := symptomUniquenessRace(2, func(int) error {
		_, err := service.CreateSymptomForUser(context.Background(), user.ID, "Joint stiffness", "🦴", "#334455")
		return err
	})

	created := 0
	for index, err := range errs {
		switch {
		case err == nil:
			created++
		case errors.Is(err, ErrSymptomNameAlreadyExists):
		default:
			t.Fatalf("concurrent create %d returned an unexpected error: %v", index, err)
		}
	}

	if created != 1 {
		t.Fatalf("expected exactly one concurrent create to succeed, got %d (errors: %v)", created, errs)
	}
	if rows := countSymptomRowsNamed(t, database, user.ID, "Joint stiffness"); rows != 1 {
		t.Fatalf("expected exactly one stored row named %q, got %d", "Joint stiffness", rows)
	}
}

// TestConcurrentBuiltinSeedingLeavesOneRowPerBuiltin covers the read-path half
// of the same class. Two concurrent page loads both list, both compute the same
// missing builtins and both insert them: the loser must not surface an error to
// a page that only wanted to read, and must not double-seed the catalogue.
func TestConcurrentBuiltinSeedingLeavesOneRowPerBuiltin(t *testing.T) {
	_, database := newDayServiceIntegration(t)
	user := createDayServiceTestUser(t, database, "symptom-seed-race@example.com")

	repositories := db.NewRepositories(database)
	service := NewSymptomService(barrieredSymptomRepository{
		SymptomRepository: repositories.Symptoms,
		afterList:         newSymptomListRendezvous(2),
	})

	errs := symptomUniquenessRace(2, func(int) error {
		_, err := service.FetchSymptoms(context.Background(), user.ID)
		return err
	})
	for index, err := range errs {
		if err != nil {
			t.Fatalf("concurrent FetchSymptoms %d returned an error: %v", index, err)
		}
	}

	var rows int64
	if err := database.Model(&models.SymptomType{}).Where("user_id = ?", user.ID).Count(&rows).Error; err != nil {
		t.Fatalf("count seeded symptoms: %v", err)
	}
	if expected := int64(len(models.DefaultBuiltinSymptoms())); rows != expected {
		t.Fatalf("expected %d builtin rows after a concurrent seed, got %d", expected, rows)
	}
}

// TestSymptomNameIndexDoesNotCollapseTheWhitespaceTheServiceCollapses pins the
// residual the database backstop deliberately leaves.
//
// normalizeSymptomNameKey lowercases AND collapses internal whitespace runs to
// a single space. Portable SQL can do the first and not the second, so the
// index keys on lower(name) and "Joint  stiffness" and "Joint stiffness" are
// two different keys to it while they are one name to the service. Both halves
// are asserted here so a later reader cannot conclude from the index that the
// database now enforces the application's rule: it enforces a weaker one, and
// the service check in front of it is what covers the difference.
func TestSymptomNameIndexDoesNotCollapseTheWhitespaceTheServiceCollapses(t *testing.T) {
	_, database := newDayServiceIntegration(t)
	user := createDayServiceTestUser(t, database, "symptom-name-residual@example.com")

	repositories := db.NewRepositories(database)
	service := NewSymptomService(repositories.Symptoms)

	if _, err := service.CreateSymptomForUser(context.Background(), user.ID, "Joint stiffness", "🦴", "#334455"); err != nil {
		t.Fatalf("create first symptom: %v", err)
	}

	// The service collapses the doubled space and refuses the name.
	if _, err := service.CreateSymptomForUser(context.Background(), user.ID, "Joint  stiffness", "🦴", "#334455"); !errors.Is(err, ErrSymptomNameAlreadyExists) {
		t.Fatalf("expected the service to refuse a whitespace-variant name, got %v", err)
	}

	// The index does not: a write that bypasses the service commits.
	spaced := models.SymptomType{UserID: user.ID, Name: "Joint  stiffness", Icon: "🦴", Color: "#334455"}
	if err := database.Create(&spaced).Error; err != nil {
		t.Fatalf("expected the index to admit a whitespace-variant name it cannot normalize, got %v", err)
	}

	// A case-only variant IS covered, which is the half the index does carry.
	cased := models.SymptomType{UserID: user.ID, Name: "joint stiffness", Icon: "🦴", Color: "#334455"}
	if err := database.Create(&cased).Error; err == nil {
		t.Fatal("expected the index to refuse a case-only variant of an existing name")
	}
}

// TestSymptomNameUniquenessIsPerOwnerAndSurvivesArchival pins the two scopes the
// index must NOT widen or narrow.
//
// Per owner: two accounts on one instance each hold their own "Joint stiffness".
//
// Across archival: ListByUser returns archived rows too, so the service already
// treats an archived name as taken and RestoreSymptomForUser re-checks it. The
// index therefore covers every row rather than only the unarchived ones — a
// partial index would be weaker than the rule the service has always applied,
// and would let an archive-then-recreate produce a pair that cannot be restored.
func TestSymptomNameUniquenessIsPerOwnerAndSurvivesArchival(t *testing.T) {
	_, database := newDayServiceIntegration(t)
	first := createDayServiceTestUser(t, database, "symptom-name-owner-a@example.com")
	second := createDayServiceTestUser(t, database, "symptom-name-owner-b@example.com")

	repositories := db.NewRepositories(database)
	service := NewSymptomService(repositories.Symptoms)

	created, err := service.CreateSymptomForUser(context.Background(), first.ID, "Joint stiffness", "🦴", "#334455")
	if err != nil {
		t.Fatalf("create symptom for the first owner: %v", err)
	}
	if _, err := service.CreateSymptomForUser(context.Background(), second.ID, "Joint stiffness", "🦴", "#334455"); err != nil {
		t.Fatalf("expected the second owner to hold the same name, got %v", err)
	}

	if err := service.ArchiveSymptomForUser(context.Background(), first.ID, created.ID, time.Now().UTC()); err != nil {
		t.Fatalf("archive symptom: %v", err)
	}
	if _, err := service.CreateSymptomForUser(context.Background(), first.ID, "Joint stiffness", "🦴", "#334455"); !errors.Is(err, ErrSymptomNameAlreadyExists) {
		t.Fatalf("expected an archived name to stay reserved for its owner, got %v", err)
	}
}
