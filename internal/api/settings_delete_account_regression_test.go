package api

import (
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"

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
	seedSettingsDeleteAccountHealthData(t, ctx)

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
	assertSettingsDeleteAccountDataCounts(t, ctx, 0, 0)

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

func seedSettingsDeleteAccountHealthData(t *testing.T, ctx settingsSecurityTestContext) {
	t.Helper()

	symptom := models.SymptomType{
		UserID:    ctx.user.ID,
		Name:      "Delete custom",
		Icon:      "A",
		Color:     "#111111",
		IsBuiltin: false,
	}
	if err := ctx.database.Create(&symptom).Error; err != nil {
		t.Fatalf("create custom symptom: %v", err)
	}

	logEntry := models.DailyLog{
		UserID:     ctx.user.ID,
		Date:       time.Date(2026, time.March, 10, 0, 0, 0, 0, time.UTC),
		IsPeriod:   true,
		Flow:       models.FlowMedium,
		SymptomIDs: []uint{symptom.ID},
		Notes:      "delete me",
	}
	if err := ctx.database.Create(&logEntry).Error; err != nil {
		t.Fatalf("create daily log: %v", err)
	}

	assertSettingsDeleteAccountDataCounts(t, ctx, 1, 1)
}

func assertSettingsDeleteAccountDataCounts(t *testing.T, ctx settingsSecurityTestContext, wantSymptoms int64, wantLogs int64) {
	t.Helper()

	var symptomsCount int64
	if err := ctx.database.Model(&models.SymptomType{}).Where("user_id = ? AND is_builtin = ?", ctx.user.ID, false).Count(&symptomsCount).Error; err != nil {
		t.Fatalf("count custom symptoms: %v", err)
	}
	if symptomsCount != wantSymptoms {
		t.Fatalf("expected custom symptoms count %d, got %d", wantSymptoms, symptomsCount)
	}

	var logsCount int64
	if err := ctx.database.Model(&models.DailyLog{}).Where("user_id = ?", ctx.user.ID).Count(&logsCount).Error; err != nil {
		t.Fatalf("count daily logs: %v", err)
	}
	if logsCount != wantLogs {
		t.Fatalf("expected daily logs count %d, got %d", wantLogs, logsCount)
	}
}
