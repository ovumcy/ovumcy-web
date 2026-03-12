package api

import (
	"net/http"
	"net/url"
	"testing"

	"github.com/terraincognita07/ovumcy/internal/models"
	"golang.org/x/crypto/bcrypt"
)

func TestChangePasswordRejectsWeakNumericPassword(t *testing.T) {
	ctx := newSettingsSecurityTestContext(t, "change-password@example.com")

	form := url.Values{
		"current_password": {"StrongPass1"},
		"new_password":     {"12345678"},
		"confirm_password": {"12345678"},
	}
	response := settingsFormRequestWithCSRF(t, ctx, http.MethodPost, "/api/settings/change-password", form, map[string]string{
		"Accept": "application/json",
	})
	defer response.Body.Close()

	if response.StatusCode != http.StatusBadRequest {
		t.Fatalf("expected status 400, got %d", response.StatusCode)
	}

	errorValue := readAPIError(t, response.Body)
	if errorValue != "weak password" {
		t.Fatalf("expected weak password error, got %q", errorValue)
	}

	var updatedUser models.User
	if err := ctx.database.First(&updatedUser, ctx.user.ID).Error; err != nil {
		t.Fatalf("load updated user: %v", err)
	}
	if bcrypt.CompareHashAndPassword([]byte(updatedUser.PasswordHash), []byte("StrongPass1")) != nil {
		t.Fatalf("expected old password hash to stay unchanged")
	}
	if bcrypt.CompareHashAndPassword([]byte(updatedUser.PasswordHash), []byte("12345678")) == nil {
		t.Fatalf("expected weak password not to be applied")
	}
}

func TestChangePasswordRejectsPasswordMismatch(t *testing.T) {
	ctx := newSettingsSecurityTestContext(t, "change-password-mismatch@example.com")

	form := url.Values{
		"current_password": {"StrongPass1"},
		"new_password":     {"EvenStronger2"},
		"confirm_password": {"DifferentPass3"},
	}
	response := settingsFormRequestWithCSRF(t, ctx, http.MethodPost, "/api/settings/change-password", form, map[string]string{
		"Accept": "application/json",
	})
	defer response.Body.Close()

	if response.StatusCode != http.StatusBadRequest {
		t.Fatalf("expected status 400, got %d", response.StatusCode)
	}

	errorValue := readAPIError(t, response.Body)
	if errorValue != "password mismatch" {
		t.Fatalf("expected password mismatch error, got %q", errorValue)
	}

	var updatedUser models.User
	if err := ctx.database.First(&updatedUser, ctx.user.ID).Error; err != nil {
		t.Fatalf("load updated user: %v", err)
	}
	if bcrypt.CompareHashAndPassword([]byte(updatedUser.PasswordHash), []byte("StrongPass1")) != nil {
		t.Fatalf("expected old password hash to stay unchanged")
	}
}
