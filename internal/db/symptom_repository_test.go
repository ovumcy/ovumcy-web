package db

import (
	"context"
	"path/filepath"
	"testing"

	"github.com/ovumcy/ovumcy-web/internal/models"
)

// TestSymptomRepositoryOwnerScoping covers the owner-scoped symptom catalog
// repository: batch/single create, per-user listing and counts, cross-owner
// read refusal (FindByIDForUser / CountByUserAndIDs), and update.
func TestSymptomRepositoryOwnerScoping(t *testing.T) {
	database := openSQLiteForMigrationBootstrapTest(t, filepath.Join(t.TempDir(), "symptoms.db"))
	repo := NewSymptomRepository(database)
	ownerA := createDailyLogTestUser(t, database, "symptom-owner-a@example.com")
	ownerB := createDailyLogTestUser(t, database, "symptom-owner-b@example.com")

	mk := func(userID uint, name string, builtin bool) models.SymptomType {
		return models.SymptomType{UserID: userID, Name: name, Icon: "x", Color: "#FF0000", IsBuiltin: builtin}
	}

	// Empty batch is a no-op; a real batch seeds owner A (1 builtin + 1 custom).
	if err := repo.CreateBatch(context.Background(), nil); err != nil {
		t.Fatalf("empty batch should be a no-op: %v", err)
	}
	if err := repo.CreateBatch(context.Background(), []models.SymptomType{mk(ownerA, "Cramps", true), mk(ownerA, "Custom A", false)}); err != nil {
		t.Fatalf("create batch: %v", err)
	}
	bSymptom := mk(ownerB, "Custom B", false)
	if err := repo.Create(context.Background(), &bSymptom); err != nil {
		t.Fatalf("create B: %v", err)
	}

	// Listing is owner-scoped.
	aSymptoms, err := repo.ListByUser(context.Background(), ownerA)
	if err != nil {
		t.Fatalf("list A: %v", err)
	}
	if len(aSymptoms) != 2 {
		t.Fatalf("expected 2 symptoms for owner A, got %d", len(aSymptoms))
	}
	if bList, _ := repo.ListByUser(context.Background(), ownerB); len(bList) != 1 {
		t.Fatalf("expected 1 symptom for owner B, got %d", len(bList))
	}

	// Builtin count.
	if n, _ := repo.CountBuiltinByUser(context.Background(), ownerA); n != 1 {
		t.Fatalf("expected 1 builtin for owner A, got %d", n)
	}

	// FindByIDForUser refuses cross-owner reads.
	var aBuiltinID uint
	for _, s := range aSymptoms {
		if s.IsBuiltin {
			aBuiltinID = s.ID
		}
	}
	if _, err := repo.FindByIDForUser(context.Background(), aBuiltinID, ownerA); err != nil {
		t.Fatalf("expected owner A to read own symptom, got %v", err)
	}
	if _, err := repo.FindByIDForUser(context.Background(), aBuiltinID, ownerB); err == nil {
		t.Fatal("expected cross-owner symptom read to fail")
	}

	// CountByUserAndIDs filters by owner — owner B's id is excluded for A.
	ids := []uint{aSymptoms[0].ID, aSymptoms[1].ID, bSymptom.ID}
	if n, _ := repo.CountByUserAndIDs(context.Background(), ownerA, ids); n != 2 {
		t.Fatalf("expected 2 owner-A matches (owner B excluded), got %d", n)
	}

	// Update persists a rename.
	renamed, _ := repo.FindByIDForUser(context.Background(), aBuiltinID, ownerA)
	renamed.Name = "Renamed"
	if err := repo.Update(context.Background(), &renamed); err != nil {
		t.Fatalf("update: %v", err)
	}
	if got, _ := repo.FindByIDForUser(context.Background(), aBuiltinID, ownerA); got.Name != "Renamed" {
		t.Fatalf("expected rename to persist, got %q", got.Name)
	}
}
