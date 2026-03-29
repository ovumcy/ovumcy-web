package services

import (
	"errors"
	"testing"
	"time"

	"github.com/ovumcy/ovumcy-web/internal/models"
)

type stubRegistrationAuthService struct {
	user         models.User
	recoveryCode string
	oidcUser     models.User
	err          error
	called       bool
	oidcCalled   bool
}

func (stub *stubRegistrationAuthService) RegisterOwner(string, string, string, time.Time) (models.User, string, error) {
	stub.called = true
	if stub.err != nil {
		return models.User{}, "", stub.err
	}
	return stub.user, stub.recoveryCode, nil
}

func (stub *stubRegistrationAuthService) BuildOIDCOwnerUser(string, time.Time) (models.User, error) {
	stub.oidcCalled = true
	if stub.err != nil {
		return models.User{}, stub.err
	}
	if stub.oidcUser.ID != 0 || stub.oidcUser.Email != "" {
		return stub.oidcUser, nil
	}
	return stub.user, nil
}

type stubRegistrationStore struct {
	err            error
	called         bool
	lastPersisted  models.User
	lastSymptomSet []models.SymptomType
}

func (stub *stubRegistrationStore) CreateUserWithSymptoms(user *models.User, symptoms []models.SymptomType) error {
	stub.called = true
	if user != nil {
		stub.lastPersisted = *user
	}
	stub.lastSymptomSet = make([]models.SymptomType, len(symptoms))
	copy(stub.lastSymptomSet, symptoms)
	return stub.err
}

type stubRegistrationUniqueConstraintError struct{}

func (stubRegistrationUniqueConstraintError) Error() string { return "unique constraint violation" }
func (stubRegistrationUniqueConstraintError) UniqueConstraint() string {
	return "users.email"
}

type stubRegistrationSeedWriteError struct{}

func (stubRegistrationSeedWriteError) Error() string { return "symptom seed write failed" }
func (stubRegistrationSeedWriteError) SymptomSeedFailure() bool {
	return true
}

func TestRegistrationServiceRegisterOwnerAccountSuccess(t *testing.T) {
	service, store := newRegistrationServiceForTest(stubRegistrationFixture{
		userID:       42,
		storeErr:     nil,
		recoveryCode: "OVUM-ABCD-1234-EFGH",
	})

	user, recoveryCode, err := service.RegisterOwnerAccount("owner@example.com", "StrongPass1", "StrongPass1", registrationServiceTestNow)
	if err != nil {
		t.Fatalf("RegisterOwnerAccount() unexpected error: %v", err)
	}
	if user.ID != 42 {
		t.Fatalf("expected user id 42, got %d", user.ID)
	}
	if recoveryCode != "OVUM-ABCD-1234-EFGH" {
		t.Fatalf("expected recovery code, got %q", recoveryCode)
	}
	if !store.called || store.lastPersisted.ID != 42 {
		t.Fatalf("expected CreateUserWithSymptoms to persist user 42")
	}
	if len(store.lastSymptomSet) == 0 {
		t.Fatalf("expected builtin symptoms batch for new registration")
	}
	for _, symptom := range store.lastSymptomSet {
		if symptom.UserID != 0 {
			t.Fatalf("expected symptoms without bound user id before persistence, got %d", symptom.UserID)
		}
		if !symptom.IsBuiltin {
			t.Fatalf("expected builtin symptom in seed set")
		}
	}
}

func TestRegistrationServiceRegisterOwnerAccountPropagatesAuthError(t *testing.T) {
	authErr := ErrAuthEmailExists
	auth := &stubRegistrationAuthService{err: authErr}
	store := &stubRegistrationStore{}
	service := NewRegistrationService(auth, store, RegistrationModeOpen)

	if _, _, err := service.RegisterOwnerAccount("owner@example.com", "StrongPass1", "StrongPass1", registrationServiceTestNow); !errors.Is(err, authErr) {
		t.Fatalf("expected auth error %v, got %v", authErr, err)
	}
	if !auth.called {
		t.Fatalf("expected auth registration attempt")
	}
	if store.called {
		t.Fatalf("did not expect persistence call on auth error")
	}
}

func TestRegistrationServiceRegisterOwnerAccountMapsSeedError(t *testing.T) {
	service, store := newRegistrationServiceForTest(stubRegistrationFixture{
		userID:       55,
		storeErr:     stubRegistrationSeedWriteError{},
		recoveryCode: "OVUM-ABCD-1234-EFGH",
	})

	if _, _, err := service.RegisterOwnerAccount("owner@example.com", "StrongPass1", "StrongPass1", registrationServiceTestNow); !errors.Is(err, ErrRegistrationSeedSymptoms) {
		t.Fatalf("expected ErrRegistrationSeedSymptoms, got %v", err)
	}
	if !store.called || store.lastPersisted.ID != 55 {
		t.Fatalf("expected persistence call for user 55")
	}
}

func TestRegistrationServiceRegisterOwnerAccountMapsUniqueViolationToEmailExists(t *testing.T) {
	service, _ := newRegistrationServiceForTest(stubRegistrationFixture{
		userID:       56,
		storeErr:     stubRegistrationUniqueConstraintError{},
		recoveryCode: "OVUM-ABCD-1234-EFGH",
	})

	if _, _, err := service.RegisterOwnerAccount("owner@example.com", "StrongPass1", "StrongPass1", registrationServiceTestNow); !errors.Is(err, ErrAuthEmailExists) {
		t.Fatalf("expected ErrAuthEmailExists, got %v", err)
	}
}

func TestRegistrationServiceRegisterOwnerAccountMapsGenericPersistenceError(t *testing.T) {
	service, _ := newRegistrationServiceForTest(stubRegistrationFixture{
		userID:       57,
		storeErr:     errors.New("db down"),
		recoveryCode: "OVUM-ABCD-1234-EFGH",
	})

	if _, _, err := service.RegisterOwnerAccount("owner@example.com", "StrongPass1", "StrongPass1", registrationServiceTestNow); !errors.Is(err, ErrAuthRegisterFailed) {
		t.Fatalf("expected ErrAuthRegisterFailed, got %v", err)
	}
}

func TestRegistrationServiceRegisterOwnerAccountRejectsClosedMode(t *testing.T) {
	auth := &stubRegistrationAuthService{
		user:         models.User{ID: 99, Email: "owner@example.com"},
		recoveryCode: "OVUM-ABCD-1234-EFGH",
	}
	store := &stubRegistrationStore{}
	service := NewRegistrationService(auth, store, RegistrationModeClosed)

	if _, _, err := service.RegisterOwnerAccount("owner@example.com", "StrongPass1", "StrongPass1", registrationServiceTestNow); !errors.Is(err, ErrAuthRegistrationDisabled) {
		t.Fatalf("expected ErrAuthRegistrationDisabled, got %v", err)
	}
	if auth.called {
		t.Fatalf("did not expect auth registration attempt in closed mode")
	}
	if store.called {
		t.Fatalf("did not expect persistence call in closed mode")
	}
}

func TestRegistrationServiceAutoProvisionOwnerAccountSuccess(t *testing.T) {
	service, store := newRegistrationServiceForTest(stubRegistrationFixture{
		userID:       88,
		storeErr:     nil,
		recoveryCode: "OVUM-ABCD-1234-EFGH",
	})

	user, err := service.AutoProvisionOwnerAccount("owner@example.com", registrationServiceTestNow)
	if err != nil {
		t.Fatalf("AutoProvisionOwnerAccount() unexpected error: %v", err)
	}
	if user.ID != 88 {
		t.Fatalf("expected user id 88, got %d", user.ID)
	}
	if !store.called || store.lastPersisted.ID != 88 {
		t.Fatalf("expected CreateUserWithSymptoms to persist provisioned user 88")
	}
}

var registrationServiceTestNow = time.Date(2026, time.March, 2, 12, 0, 0, 0, time.UTC)

type stubRegistrationFixture struct {
	userID       uint
	storeErr     error
	recoveryCode string
}

func newRegistrationServiceForTest(fixture stubRegistrationFixture) (*RegistrationService, *stubRegistrationStore) {
	auth := &stubRegistrationAuthService{
		user:         models.User{ID: fixture.userID, Email: "owner@example.com"},
		recoveryCode: fixture.recoveryCode,
	}
	store := &stubRegistrationStore{err: fixture.storeErr}
	return NewRegistrationService(auth, store, RegistrationModeOpen), store
}
