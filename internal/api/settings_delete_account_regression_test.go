package api

import (
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
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

func TestSettingsDeleteAccountDeletesUserAndClearsAuthRelatedCookies(t *testing.T) {
	ctx := newSettingsSecurityTestContext(t, "settings-delete-success@example.com")

	form := url.Values{
		"password":   {"StrongPass1"},
		"csrf_token": {ctx.csrfToken},
	}
	request := httptest.NewRequest(http.MethodDelete, "/api/settings/delete-account", strings.NewReader(form.Encode()))
	request.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	request.Header.Set("Accept", "application/json")
	request.Header.Set(
		"Cookie",
		joinCookieHeader(
			ctx.authCookie,
			cookiePair(ctx.csrfCookie),
			recoveryCodeCookieName+"=temporary-recovery",
			resetPasswordCookieName+"=temporary-reset",
		),
	)

	response, err := ctx.app.Test(request, -1)
	if err != nil {
		t.Fatalf("delete-account request failed: %v", err)
	}
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

	recoveryCookieAfterDelete := responseCookie(response.Cookies(), recoveryCodeCookieName)
	if recoveryCookieAfterDelete == nil {
		t.Fatalf("expected recovery code cookie to be cleared on delete-account success")
	}
	if recoveryCookieAfterDelete.Value != "" {
		t.Fatalf("expected cleared recovery code cookie value, got %q", recoveryCookieAfterDelete.Value)
	}

	resetCookieAfterDelete := responseCookie(response.Cookies(), resetPasswordCookieName)
	if resetCookieAfterDelete == nil {
		t.Fatalf("expected reset password cookie to be cleared on delete-account success")
	}
	if resetCookieAfterDelete.Value != "" {
		t.Fatalf("expected cleared reset password cookie value, got %q", resetCookieAfterDelete.Value)
	}
}
