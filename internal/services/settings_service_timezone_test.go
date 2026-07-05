package services

import (
	"context"
	"errors"
	"testing"

	"github.com/ovumcy/ovumcy-web/internal/models"
)

// recordingTimezoneRepo is a full SettingsUserRepository whose only live method
// is UpdateUserTimezone; every timezone write is captured so the tests can
// assert whether the service issued a DB write at all.
type recordingTimezoneRepo struct {
	calls        int
	lastUserID   uint
	lastTimezone string
	updateErr    error
}

func (repo *recordingTimezoneRepo) UpdateDisplayName(context.Context, uint, string) error {
	return nil
}

func (repo *recordingTimezoneRepo) UpdateUserTimezone(_ context.Context, userID uint, timezone string) error {
	repo.calls++
	repo.lastUserID = userID
	repo.lastTimezone = timezone
	return repo.updateErr
}

func (repo *recordingTimezoneRepo) UpdatePasswordAndRevokeSessions(context.Context, uint, string, bool) error {
	return nil
}

func (repo *recordingTimezoneRepo) UpdatePasswordRecoveryCodeAndRevokeSessions(context.Context, uint, string, string, bool) error {
	return nil
}

func (repo *recordingTimezoneRepo) UpdateByID(context.Context, uint, map[string]any) error {
	return nil
}

func (repo *recordingTimezoneRepo) LoadSettingsByID(context.Context, uint) (models.User, error) {
	return models.User{}, nil
}

func (repo *recordingTimezoneRepo) ClearAllDataAndResetSettings(context.Context, uint) error {
	return nil
}

func (repo *recordingTimezoneRepo) DeleteAccountAndRelatedData(context.Context, uint) error {
	return nil
}

func TestPersistTimezoneWritesOnChange(t *testing.T) {
	repo := &recordingTimezoneRepo{}
	service := NewSettingsService(repo)

	wrote, err := service.PersistTimezone(context.Background(), 7, "UTC", "Europe/Belgrade")
	if err != nil {
		t.Fatalf("PersistTimezone: %v", err)
	}
	if !wrote {
		t.Fatal("expected a write when the timezone changed")
	}
	if repo.calls != 1 {
		t.Fatalf("expected exactly 1 repo write, got %d", repo.calls)
	}
	if repo.lastUserID != 7 {
		t.Fatalf("expected write scoped to user 7, got %d", repo.lastUserID)
	}
	if repo.lastTimezone != "Europe/Belgrade" {
		t.Fatalf("expected persisted timezone Europe/Belgrade, got %q", repo.lastTimezone)
	}
}

func TestPersistTimezoneWritesWhenPreviouslyUnset(t *testing.T) {
	repo := &recordingTimezoneRepo{}
	service := NewSettingsService(repo)

	wrote, err := service.PersistTimezone(context.Background(), 7, "", "Europe/Belgrade")
	if err != nil {
		t.Fatalf("PersistTimezone: %v", err)
	}
	if !wrote || repo.calls != 1 {
		t.Fatalf("expected a write when no timezone was previously stored, wrote=%v calls=%d", wrote, repo.calls)
	}
}

func TestPersistTimezoneNoopWhenUnchanged(t *testing.T) {
	repo := &recordingTimezoneRepo{}
	service := NewSettingsService(repo)

	wrote, err := service.PersistTimezone(context.Background(), 7, "Europe/Belgrade", "Europe/Belgrade")
	if err != nil {
		t.Fatalf("PersistTimezone: %v", err)
	}
	if wrote {
		t.Fatal("expected no write when the timezone is unchanged")
	}
	if repo.calls != 0 {
		t.Fatalf("expected no repo write on unchanged timezone, got %d", repo.calls)
	}
}

func TestPersistTimezoneNoopWhenNewValueEmpty(t *testing.T) {
	repo := &recordingTimezoneRepo{}
	service := NewSettingsService(repo)

	wrote, err := service.PersistTimezone(context.Background(), 7, "Europe/Belgrade", "")
	if err != nil {
		t.Fatalf("PersistTimezone: %v", err)
	}
	if wrote || repo.calls != 0 {
		t.Fatalf("expected no write when the new timezone is empty, wrote=%v calls=%d", wrote, repo.calls)
	}
}

func TestPersistTimezonePropagatesRepoError(t *testing.T) {
	sentinel := errors.New("db down")
	repo := &recordingTimezoneRepo{updateErr: sentinel}
	service := NewSettingsService(repo)

	wrote, err := service.PersistTimezone(context.Background(), 7, "UTC", "Europe/Belgrade")
	if !errors.Is(err, sentinel) {
		t.Fatalf("expected repo error to propagate, got %v", err)
	}
	if wrote {
		t.Fatal("expected wrote=false when the repo write failed")
	}
}
