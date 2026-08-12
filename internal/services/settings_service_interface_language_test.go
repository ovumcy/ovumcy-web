package services

import (
	"context"
	"errors"
	"testing"
)

// TestSaveInterfaceLanguagePersistsTheChosenCode pins the ordinary save: the
// code the transport layer validated reaches the repository unchanged, scoped
// to the account it was submitted for.
func TestSaveInterfaceLanguagePersistsTheChosenCode(t *testing.T) {
	repo := &stubSettingsTrackingUserRepo{languageRowMatched: true}
	service := NewSettingsService(repo)

	if err := service.SaveInterfaceLanguage(context.Background(), 42, "ru"); err != nil {
		t.Fatalf("SaveInterfaceLanguage: %v", err)
	}
	if repo.languageCalls != 1 {
		t.Fatalf("expected exactly one interface-language write, got %d", repo.languageCalls)
	}
	if repo.languageUpdatedUserID != 42 {
		t.Fatalf("expected the write scoped to user 42, got %d", repo.languageUpdatedUserID)
	}
	if repo.languagePersisted != "ru" {
		t.Fatalf("expected ru persisted, got %q", repo.languagePersisted)
	}
}

// TestSaveInterfaceLanguageReportsAWriteThatMatchedNoRow is the reason the
// repository returns a row-matched flag at all. GORM answers an UPDATE that
// matches nothing with a nil error, so an account deleted between the request's
// session check and the save would reach the owner as a success flash for a
// preference nothing stored — and the next sign-in would silently contradict
// it. The failure has to be an error the transport layer can map.
func TestSaveInterfaceLanguageReportsAWriteThatMatchedNoRow(t *testing.T) {
	repo := &stubSettingsTrackingUserRepo{languageRowMatched: false}
	service := NewSettingsService(repo)

	err := service.SaveInterfaceLanguage(context.Background(), 42, "ru")
	if !errors.Is(err, ErrSettingsInterfaceLanguageNotStored) {
		t.Fatalf("expected ErrSettingsInterfaceLanguageNotStored for a zero-row write, got %v", err)
	}
}

func TestSaveInterfaceLanguagePropagatesTheRepositoryError(t *testing.T) {
	writeErr := errors.New("write failed")
	repo := &stubSettingsTrackingUserRepo{languageErr: writeErr}
	service := NewSettingsService(repo)

	if err := service.SaveInterfaceLanguage(context.Background(), 42, "ru"); !errors.Is(err, writeErr) {
		t.Fatalf("expected the repository error to propagate, got %v", err)
	}
}
