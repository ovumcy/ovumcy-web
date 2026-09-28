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
