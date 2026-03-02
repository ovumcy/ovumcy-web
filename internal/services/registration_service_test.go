package services

import (
	"errors"
	"testing"
	"time"

	"github.com/terraincognita07/ovumcy/internal/models"
)

type stubRegistrationAuthService struct {
	user         models.User
	recoveryCode string
	err          error
}

func (stub *stubRegistrationAuthService) RegisterOwner(string, string, string, time.Time) (models.User, string, error) {
	if stub.err != nil {
		return models.User{}, "", stub.err
	}
	return stub.user, stub.recoveryCode, nil
}

type stubRegistrationSymptomSeeder struct {
	err            error
	called         bool
	lastSeedUserID uint
}

func (stub *stubRegistrationSymptomSeeder) SeedBuiltinSymptoms(userID uint) error {
	stub.called = true
	stub.lastSeedUserID = userID
	return stub.err
}

func TestRegistrationServiceRegisterOwnerAccount(t *testing.T) {
	now := time.Date(2026, time.March, 2, 12, 0, 0, 0, time.UTC)

	t.Run("success", func(t *testing.T) {
		auth := &stubRegistrationAuthService{
			user:         models.User{ID: 42, Email: "owner@example.com"},
			recoveryCode: "OVUM-ABCD-1234-EFGH",
		}
		seeder := &stubRegistrationSymptomSeeder{}
		service := NewRegistrationService(auth, seeder)

		user, recoveryCode, err := service.RegisterOwnerAccount("owner@example.com", "StrongPass1", "StrongPass1", now)
		if err != nil {
			t.Fatalf("RegisterOwnerAccount() unexpected error: %v", err)
		}
		if user.ID != 42 {
			t.Fatalf("expected user id 42, got %d", user.ID)
		}
		if recoveryCode != "OVUM-ABCD-1234-EFGH" {
			t.Fatalf("expected recovery code, got %q", recoveryCode)
		}
		if !seeder.called || seeder.lastSeedUserID != 42 {
			t.Fatalf("expected SeedBuiltinSymptoms to be called with user 42")
		}
	})

	t.Run("auth error propagated", func(t *testing.T) {
		authErr := ErrAuthEmailExists
		auth := &stubRegistrationAuthService{err: authErr}
		seeder := &stubRegistrationSymptomSeeder{}
		service := NewRegistrationService(auth, seeder)

		if _, _, err := service.RegisterOwnerAccount("owner@example.com", "StrongPass1", "StrongPass1", now); !errors.Is(err, authErr) {
			t.Fatalf("expected auth error %v, got %v", authErr, err)
		}
		if seeder.called {
			t.Fatalf("did not expect SeedBuiltinSymptoms call on auth error")
		}
	})

	t.Run("seed error mapped", func(t *testing.T) {
		auth := &stubRegistrationAuthService{
			user:         models.User{ID: 55, Email: "owner@example.com"},
			recoveryCode: "OVUM-ABCD-1234-EFGH",
		}
		seeder := &stubRegistrationSymptomSeeder{err: errors.New("seed failed")}
		service := NewRegistrationService(auth, seeder)

		if _, _, err := service.RegisterOwnerAccount("owner@example.com", "StrongPass1", "StrongPass1", now); !errors.Is(err, ErrRegistrationSeedSymptoms) {
			t.Fatalf("expected ErrRegistrationSeedSymptoms, got %v", err)
		}
		if !seeder.called || seeder.lastSeedUserID != 55 {
			t.Fatalf("expected SeedBuiltinSymptoms to be called with user 55")
		}
	})
}
