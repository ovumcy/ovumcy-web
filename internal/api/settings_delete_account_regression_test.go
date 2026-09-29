package api

import (
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"

	"github.com/ovumcy/ovumcy-web/internal/models"
)

func TestSettingsDeleteAccountRejectsMissingPassword(t *testing.T) {
	ctx := newSettingsSecurityTestContext(t, "settings-delete-missing@example.com")

	response := settingsFormRequestWithCSRF(t, ctx, http.MethodDelete, "/api/v1/users/current", url.Values{}, map[string]string{
		"Accept": "application/json",
	})
	defer func() { _ = response.Body.Close() }()

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

	response := settingsFormRequestWithCSRF(t, ctx, http.MethodDelete, "/api/v1/users/current", url.Values{
		"password": {"WrongPass1"},
	}, map[string]string{
		"Accept": "application/json",
	})
	defer func() { _ = response.Body.Close() }()

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

// TestSettingsDeleteAccountRefusesABodyItCouldNotDecodeWhole pins the password
// step-up's read of the body to the binder's verdict: a body whose type the API
// does not declare (XML, whole or cut off after the password), or one the
// binder rejected part-way, is refused with the correct password in it, the
// account stays, and a form request with that password afterwards still deletes
// it (the refusals spent nothing that locks the owner out).
func TestSettingsDeleteAccountRefusesABodyItCouldNotDecodeWhole(t *testing.T) {
	ctx := newSettingsSecurityTestContext(t, "settings-delete-whole-body@example.com")

	send := func(contentType, body string) *http.Response {
		request := httptest.NewRequest(http.MethodDelete, "/api/v1/users/current", strings.NewReader(body))
		request.Header.Set("Content-Type", contentType)
		request.Header.Set("Accept", "application/json")
		request.Header.Set("X-CSRF-Token", ctx.csrfToken)
		request.Header.Set("Cookie", joinCookieHeader(ctx.authCookie, cookiePair(ctx.csrfCookie)))
		response, err := ctx.app.Test(request, testConfigNoTimeout)
		if err != nil {
			t.Fatalf("delete-account request failed: %v", err)
		}
		t.Cleanup(func() { _ = response.Body.Close() })
		return response
	}
	usersCount := func() int64 {
		var count int64
		if err := ctx.database.Model(&models.User{}).Where("id = ?", ctx.user.ID).Count(&count).Error; err != nil {
			t.Fatalf("count users: %v", err)
		}
		return count
	}

	for _, tc := range []struct{ name, contentType, body string }{
		{"well-formed xml", "application/xml", `<d><Password>StrongPass1</Password></d>`},
		{"partial xml", "application/xml", `<d><Password>StrongPass1</Password><broken>`},
		{"text xml", "text/xml", `<d><Password>StrongPass1</Password></d>`},
		{"json with the password given twice, the last of the wrong type", "application/json", `{"password":"StrongPass1","password":7}`},
	} {
		response := send(tc.contentType, tc.body)
		if response.StatusCode != http.StatusBadRequest {
			t.Errorf("%s: status = %d, want 400", tc.name, response.StatusCode)
		}
		if got := readAPIError(t, response.Body); got != "invalid password" {
			t.Errorf("%s: error = %q, want the missing-password refusal", tc.name, got)
		}
		if usersCount() != 1 {
			t.Fatalf("%s: the account was deleted from a body the binder rejected", tc.name)
		}
	}

	control := send("application/x-www-form-urlencoded", url.Values{"password": {"StrongPass1"}}.Encode())
	if control.StatusCode != http.StatusOK {
		t.Fatalf("form control: status = %d, want 200", control.StatusCode)
	}
	if usersCount() != 0 {
		t.Fatal("form control: the account survived a correct password in a form body")
	}
}

func TestSettingsDeleteAccountDeletesUserAndClearsAuthRelatedCookies(t *testing.T) {
	ctx := newSettingsSecurityTestContext(t, "settings-delete-success@example.com")
	seedSettingsDeleteAccountHealthData(t, ctx)

	form := url.Values{
		"password":   {"StrongPass1"},
		"csrf_token": {ctx.csrfToken},
	}
	request := httptest.NewRequest(http.MethodDelete, "/api/v1/users/current", strings.NewReader(form.Encode()))
	request.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	request.Header.Set("Accept", "application/json")
	request.Header.Set(
		"Cookie",
		joinCookieHeader(
			ctx.authCookie,
			cookiePair(ctx.csrfCookie),
			recoveryCodeCookieName+"=temporary-recovery",
			resetPasswordCookieName+"=temporary-reset",
			languageCookieName+"=ru",
		),
	)

	response, err := ctx.app.Test(request, testConfigNoTimeout)
	if err != nil {
		t.Fatalf("delete-account request failed: %v", err)
	}
	defer func() { _ = response.Body.Close() }()

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

	// The account the language cookie cached no longer exists, and the browser
	// may be shared: an erasure that leaves `ovumcy_lang=ru` behind still tells
	// the next visitor the app was used here, in Russian.
	languageCookieAfterDelete := responseCookie(response.Cookies(), languageCookieName)
	if languageCookieAfterDelete == nil {
		t.Fatalf("expected the language cookie to be cleared on delete-account success")
	}
	if languageCookieAfterDelete.Value != "" {
		t.Fatalf("expected cleared language cookie value, got %q", languageCookieAfterDelete.Value)
	}
	if !languageCookieAfterDelete.Expires.Before(time.Now()) {
		t.Fatalf("expected the language cookie to be retracted with a past expiry, got %s", languageCookieAfterDelete.Expires)
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
