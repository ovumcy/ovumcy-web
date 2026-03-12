package api

import (
	"net/http"
	"net/url"
	"testing"

	"github.com/terraincognita07/ovumcy/internal/models"
)

func TestSettingsDeleteAccountRejectsMissingPassword(t *testing.T) {
	ctx := newSettingsSecurityTestContext(t, "settings-delete-missing@example.com")

	response := settingsFormRequestWithCSRF(t, ctx, http.MethodDelete, "/api/settings/delete-account", url.Values{}, map[string]string{
		"Accept": "application/json",
	})
	defer response.Body.Close()

	if response.StatusCode != http.StatusBadRequest {
		t.Fatalf("expected status 400, got %d", response.StatusCode)
	}
	if got := readAPIError(t, response.Body); got != "invalid password" {
		t.Fatalf("expected invalid password error, got %q", got)
	}

	var usersCount int64
	if err := ctx.database.Model(&models.User{}).Where("id = ?", ctx.user.ID).Count(&usersCount).Error; err != nil {
		t.Fatalf("count users: %v", err)
	}
	if usersCount != 1 {
		t.Fatalf("expected user to stay in database, got count=%d", usersCount)
	}
}

func TestSettingsDeleteAccountRejectsInvalidPassword(t *testing.T) {
	ctx := newSettingsSecurityTestContext(t, "settings-delete-invalid@example.com")

	response := settingsFormRequestWithCSRF(t, ctx, http.MethodDelete, "/api/settings/delete-account", url.Values{
		"password": {"WrongPass1"},
	}, map[string]string{
		"Accept": "application/json",
	})
	defer response.Body.Close()

	if response.StatusCode != http.StatusUnauthorized {
		t.Fatalf("expected status 401, got %d", response.StatusCode)
	}
	if got := readAPIError(t, response.Body); got != "invalid password" {
		t.Fatalf("expected invalid password error, got %q", got)
	}

	var usersCount int64
	if err := ctx.database.Model(&models.User{}).Where("id = ?", ctx.user.ID).Count(&usersCount).Error; err != nil {
		t.Fatalf("count users: %v", err)
	}
	if usersCount != 1 {
		t.Fatalf("expected user to stay in database, got count=%d", usersCount)
	}
}

func TestSettingsDeleteAccountDeletesUserAndClearsAuthCookie(t *testing.T) {
	ctx := newSettingsSecurityTestContext(t, "settings-delete-success@example.com")

	response := settingsFormRequestWithCSRF(t, ctx, http.MethodDelete, "/api/settings/delete-account", url.Values{
		"password": {"StrongPass1"},
	}, map[string]string{
		"Accept": "application/json",
	})
	defer response.Body.Close()

	if response.StatusCode != http.StatusOK {
		t.Fatalf("expected status 200, got %d", response.StatusCode)
	}

	var usersCount int64
	if err := ctx.database.Model(&models.User{}).Where("id = ?", ctx.user.ID).Count(&usersCount).Error; err != nil {
		t.Fatalf("count users: %v", err)
	}
	if usersCount != 0 {
		t.Fatalf("expected user to be deleted, got count=%d", usersCount)
	}

	authCookieAfterDelete := responseCookie(response.Cookies(), authCookieName)
	if authCookieAfterDelete == nil {
		t.Fatalf("expected auth cookie to be cleared on delete-account success")
	}
	if authCookieAfterDelete.Value != "" {
		t.Fatalf("expected cleared auth cookie value, got %q", authCookieAfterDelete.Value)
	}
}
