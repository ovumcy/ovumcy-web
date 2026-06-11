package services

// password_reset_service_coverage_test.go — covers
// PasswordResetService.IssueResetTokenForUser, which was entirely uncovered
// (gremlins reported the nil-user guard on line 42 as NOT COVERED).

import (
	"errors"
	"testing"
	"time"

	"github.com/ovumcy/ovumcy-web/internal/models"
)

func TestPasswordresetserviceCovIssueResetTokenRejectsNilUser(t *testing.T) {
	// Line 42–43: a nil user is rejected before any token is built, so the
	// (here unconfigured) auth dependency is never dereferenced.
	service := NewPasswordResetService(NewAuthService(nil), nil)

	token, err := service.IssueResetTokenForUser([]byte("secret"), nil, time.Minute, time.Time{})
	if !errors.Is(err, ErrAuthUserRequired) {
		t.Fatalf("IssueResetTokenForUser(nil user) error = %v, want %v", err, ErrAuthUserRequired)
	}
	if token != "" {
		t.Fatalf("IssueResetTokenForUser(nil user) token = %q, want empty", token)
	}
}

func TestPasswordresetserviceCovIssueResetTokenForValidUser(t *testing.T) {
	// Line 45: a valid user yields a parseable reset token carrying that user id.
	service := NewPasswordResetService(NewAuthService(nil), nil)
	now := time.Date(2026, time.March, 1, 10, 0, 0, 0, time.UTC)
	secret := []byte("secret")
	user := &models.User{ID: 7, PasswordHash: "$2a$10$testhashvaluefortokenclaims"}

	token, err := service.IssueResetTokenForUser(secret, user, 30*time.Minute, now)
	if err != nil {
		t.Fatalf("IssueResetTokenForUser(valid user) unexpected error: %v", err)
	}
	if token == "" {
		t.Fatal("IssueResetTokenForUser(valid user) returned empty token")
	}

	claims, err := ParsePasswordResetToken(secret, token, now.Add(time.Minute))
	if err != nil {
		t.Fatalf("ParsePasswordResetToken() unexpected error: %v", err)
	}
	if claims.UserID != 7 {
		t.Fatalf("reset token UserID = %d, want 7", claims.UserID)
	}
}
