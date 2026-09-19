package services

// service_delegation_coverage_test.go — covers the thin repository-delegating
// service methods that no unit or integration test exercised (patch-coverage
// gaps in auth_service.go and symptom_service.go).

import (
	"context"
	"errors"
	"testing"

	"github.com/ovumcy/ovumcy-web/internal/models"
)

func TestAuthServiceCreateUserDelegates(t *testing.T) {
	service := NewAuthService(&stubAuthUserRepo{})
	if err := service.CreateUser(context.Background(), &models.User{Email: "owner@example.com"}); err != nil {
		t.Fatalf("CreateUser() unexpected error: %v", err)
	}

	wantErr := errors.New("create failed")
	failing := NewAuthService(&stubAuthUserRepo{createErr: wantErr})
	if err := failing.CreateUser(context.Background(), &models.User{}); !errors.Is(err, wantErr) {
		t.Fatalf("CreateUser error = %v, want %v", err, wantErr)
	}
}

func TestAuthServiceFindByNormalizedEmailDelegates(t *testing.T) {
	service := NewAuthService(&stubAuthUserRepo{findByEmailUser: models.User{ID: 42}})
	got, err := service.FindByNormalizedEmail(context.Background(), "owner@example.com")
	if err != nil {
		t.Fatalf("FindByNormalizedEmail() unexpected error: %v", err)
	}
	if got.ID != 42 {
		t.Fatalf("FindByNormalizedEmail returned user ID %d, want 42", got.ID)
	}

	wantErr := errors.New("lookup failed")
	failing := NewAuthService(&stubAuthUserRepo{findByEmailErr: wantErr})
	if _, err := failing.FindByNormalizedEmail(context.Background(), "owner@example.com"); !errors.Is(err, wantErr) {
		t.Fatalf("FindByNormalizedEmail error = %v, want %v", err, wantErr)
	}
}
