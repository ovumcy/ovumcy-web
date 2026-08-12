package db

import (
	"context"
	"testing"

	"github.com/ovumcy/ovumcy-web/internal/models"
)

func reloadUserInterfaceLanguage(t *testing.T, repo *UserRepository, userID uint) string {
	t.Helper()
	var reloaded models.User
	if err := repo.database.First(&reloaded, userID).Error; err != nil {
		t.Fatalf("reload user %d: %v", userID, err)
	}
	return reloaded.InterfaceLanguage
}

func TestUpdateInterfaceLanguagePersistsValueScopedToTheOwner(t *testing.T) {
	repo := openTimezoneRepoForTest(t)
	owner := createUserForTimezoneTest(t, repo, "language-owner@example.com")
	other := createUserForTimezoneTest(t, repo, "language-other@example.com")

	if got := reloadUserInterfaceLanguage(t, repo, owner.ID); got != "" {
		t.Fatalf("expected an empty interface language on a fresh account, got %q", got)
	}

	if _, err := repo.UpdateInterfaceLanguage(context.Background(), other.ID, "de"); err != nil {
		t.Fatalf("seed other owner language: %v", err)
	}

	stored, err := repo.UpdateInterfaceLanguage(context.Background(), owner.ID, "ru")
	if err != nil {
		t.Fatalf("UpdateInterfaceLanguage: %v", err)
	}
	if !stored {
		t.Fatal("expected the write to report a matched row for an existing account")
	}

	if got := reloadUserInterfaceLanguage(t, repo, owner.ID); got != "ru" {
		t.Fatalf("expected the owner's language persisted as ru, got %q", got)
	}
	// The household-multi-owner isolation boundary: one owner's preference
	// write never reaches another owner's row.
	if got := reloadUserInterfaceLanguage(t, repo, other.ID); got != "de" {
		t.Fatalf("expected the other owner's language untouched at de, got %q", got)
	}
}

// TestUpdateInterfaceLanguageReportsAnUnmatchedRow pins the signal the settings
// save depends on. An UPDATE matching no row is not a driver error, so without
// the row-matched flag a save against an account that no longer exists would
// look exactly like a successful one.
func TestUpdateInterfaceLanguageReportsAnUnmatchedRow(t *testing.T) {
	repo := openTimezoneRepoForTest(t)

	stored, err := repo.UpdateInterfaceLanguage(context.Background(), 99999, "ru")
	if err != nil {
		t.Fatalf("expected no driver error for a nonexistent account, got %v", err)
	}
	if stored {
		t.Fatal("expected the write to report no matched row for a nonexistent account")
	}
}
