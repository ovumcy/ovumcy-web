package api

import (
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"

	"github.com/terraincognita07/ovumcy/internal/models"
)

func TestForgotPasswordDoesNotExposeResetTokenInRedirect(t *testing.T) {
	app, database := newOnboardingTestApp(t)
	user := createOnboardingTestUser(t, database, "forgot-token-redirect@example.com", "StrongPass1", true)

	recoveryCode := mustSetRecoveryCodeForUser(t, database, user.ID)
	form := url.Values{
		"email":         {user.Email},
		"recovery_code": {recoveryCode},
	}
	request := httptest.NewRequest(http.MethodPost, "/api/auth/forgot-password", strings.NewReader(form.Encode()))
	request.Header.Set("Content-Type", "application/x-www-form-urlencoded")

	response, err := app.Test(request, -1)
	if err != nil {
		t.Fatalf("forgot-password request failed: %v", err)
	}
	defer response.Body.Close()

	if response.StatusCode != http.StatusSeeOther {
		t.Fatalf("expected status 303, got %d", response.StatusCode)
	}
	location := response.Header.Get("Location")
	if location != "/reset-password" {
		t.Fatalf("expected redirect to /reset-password, got %q", location)
	}
	if strings.Contains(location, "token=") {
		t.Fatalf("did not expect token in redirect location: %q", location)
	}

	resetCookie := responseCookie(response.Cookies(), resetPasswordCookieName)
	if resetCookie == nil || strings.TrimSpace(resetCookie.Value) == "" {
		t.Fatalf("expected reset-password cookie in forgot-password response")
	}
}

func TestForgotPasswordEmailStepDoesNotExposeEmailInRedirect(t *testing.T) {
	app, database := newOnboardingTestApp(t)
	user := createOnboardingTestUser(t, database, "forgot-email-step@example.com", "StrongPass1", true)

	form := url.Values{"email": {user.Email}}
	request := httptest.NewRequest(http.MethodPost, "/api/auth/forgot-password", strings.NewReader(form.Encode()))
	request.Header.Set("Content-Type", "application/x-www-form-urlencoded")

	response, err := app.Test(request, -1)
	if err != nil {
		t.Fatalf("forgot-password email-step request failed: %v", err)
	}
	defer response.Body.Close()

	if response.StatusCode != http.StatusSeeOther {
		t.Fatalf("expected status 303, got %d", response.StatusCode)
	}
	location := response.Header.Get("Location")
	if location != "/forgot-password" {
		t.Fatalf("expected redirect to /forgot-password, got %q", location)
	}
	if strings.Contains(location, "email=") {
		t.Fatalf("did not expect email in redirect location: %q", location)
	}
}

func TestForgotPasswordJSONDoesNotExposeResetToken(t *testing.T) {
	app, database := newOnboardingTestApp(t)
	user := createOnboardingTestUser(t, database, "forgot-token-json@example.com", "StrongPass1", true)

	recoveryCode := mustSetRecoveryCodeForUser(t, database, user.ID)
	form := url.Values{
		"email":         {user.Email},
		"recovery_code": {recoveryCode},
	}
	request := httptest.NewRequest(http.MethodPost, "/api/auth/forgot-password", strings.NewReader(form.Encode()))
	request.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	request.Header.Set("Accept", "application/json")

	response, err := app.Test(request, -1)
	if err != nil {
		t.Fatalf("forgot-password json request failed: %v", err)
	}
	defer response.Body.Close()

	if response.StatusCode != http.StatusOK {
		t.Fatalf("expected status 200, got %d", response.StatusCode)
	}

	body, err := io.ReadAll(response.Body)
	if err != nil {
		t.Fatalf("read forgot-password json body: %v", err)
	}
	rendered := string(body)
	if strings.Contains(rendered, "reset_token") {
		t.Fatalf("did not expect reset_token in json response: %s", rendered)
	}
	if strings.Contains(rendered, "token") {
		t.Fatalf("did not expect token field in json response: %s", rendered)
	}

	resetCookie := responseCookie(response.Cookies(), resetPasswordCookieName)
	if resetCookie == nil || strings.TrimSpace(resetCookie.Value) == "" {
		t.Fatalf("expected reset-password cookie in forgot-password json response")
	}
}

func TestLoginForcedResetDoesNotExposeResetToken(t *testing.T) {
	app, database := newOnboardingTestApp(t)
	user := createOnboardingTestUser(t, database, "forced-reset-login@example.com", "StrongPass1", true)
	if err := database.Model(&models.User{}).Where("id = ?", user.ID).Update("must_change_password", true).Error; err != nil {
		t.Fatalf("mark user must_change_password: %v", err)
	}

	form := url.Values{
		"email":    {user.Email},
		"password": {"StrongPass1"},
	}

	htmlRequest := httptest.NewRequest(http.MethodPost, "/api/auth/login", strings.NewReader(form.Encode()))
	htmlRequest.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	htmlResponse, err := app.Test(htmlRequest, -1)
	if err != nil {
		t.Fatalf("forced-reset login html request failed: %v", err)
	}
	defer htmlResponse.Body.Close()

	if htmlResponse.StatusCode != http.StatusSeeOther {
		t.Fatalf("expected html status 303, got %d", htmlResponse.StatusCode)
	}
	location := htmlResponse.Header.Get("Location")
	if location != "/reset-password" {
		t.Fatalf("expected html redirect to /reset-password, got %q", location)
	}
	if strings.Contains(location, "token=") {
		t.Fatalf("did not expect token in html redirect location: %q", location)
	}
	htmlResetCookie := responseCookie(htmlResponse.Cookies(), resetPasswordCookieName)
	if htmlResetCookie == nil || strings.TrimSpace(htmlResetCookie.Value) == "" {
		t.Fatalf("expected reset-password cookie in forced-reset html response")
	}

	jsonRequest := httptest.NewRequest(http.MethodPost, "/api/auth/login", strings.NewReader(form.Encode()))
	jsonRequest.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	jsonRequest.Header.Set("Accept", "application/json")
	jsonResponse, err := app.Test(jsonRequest, -1)
	if err != nil {
		t.Fatalf("forced-reset login json request failed: %v", err)
	}
	defer jsonResponse.Body.Close()

	if jsonResponse.StatusCode != http.StatusForbidden {
		t.Fatalf("expected json status 403, got %d", jsonResponse.StatusCode)
	}
	body, err := io.ReadAll(jsonResponse.Body)
	if err != nil {
		t.Fatalf("read forced-reset login json body: %v", err)
	}
	rendered := string(body)
	if strings.Contains(rendered, "reset_token") {
		t.Fatalf("did not expect reset_token in forced-reset json response: %s", rendered)
	}
	if strings.Contains(rendered, "token") {
		t.Fatalf("did not expect token field in forced-reset json response: %s", rendered)
	}
	jsonResetCookie := responseCookie(jsonResponse.Cookies(), resetPasswordCookieName)
	if jsonResetCookie == nil || strings.TrimSpace(jsonResetCookie.Value) == "" {
		t.Fatalf("expected reset-password cookie in forced-reset json response")
	}
}
