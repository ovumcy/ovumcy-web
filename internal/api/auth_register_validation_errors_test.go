package api

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"

	"github.com/ovumcy/ovumcy-web/internal/models"
	"golang.org/x/crypto/bcrypt"
)

func TestRegisterRejectsWeakNumericPassword(t *testing.T) {
	app, database := newOnboardingTestApp(t)
	email := "weak-register@example.com"

	form := url.Values{
		"email":            {email},
		"password":         {"12345678"},
		"confirm_password": {"12345678"},
	}
	request := httptest.NewRequest(http.MethodPost, "/api/auth/register", strings.NewReader(form.Encode()))
	request.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	request.Header.Set("Accept", "application/json")

	response, err := app.Test(request, -1)
	if err != nil {
		t.Fatalf("register request failed: %v", err)
	}
	defer response.Body.Close()

	if response.StatusCode != http.StatusBadRequest {
		t.Fatalf("expected status 400, got %d", response.StatusCode)
	}

	errorValue := readAPIError(t, response.Body)
	if errorValue != "weak password" {
		t.Fatalf("expected weak password error, got %q", errorValue)
	}

	var usersCount int64
	if err := database.Model(&models.User{}).Where("email = ?", email).Count(&usersCount).Error; err != nil {
		t.Fatalf("count users: %v", err)
	}
	if usersCount != 0 {
		t.Fatalf("expected user not to be created, found %d records", usersCount)
	}
}

func TestRegisterRejectsPasswordMismatch(t *testing.T) {
	app, database := newOnboardingTestApp(t)
	email := "mismatch-register@example.com"

	form := url.Values{
		"email":            {email},
		"password":         {"StrongPass1"},
		"confirm_password": {"StrongPass2"},
	}
	request := httptest.NewRequest(http.MethodPost, "/api/auth/register", strings.NewReader(form.Encode()))
	request.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	request.Header.Set("Accept", "application/json")

	response, err := app.Test(request, -1)
	if err != nil {
		t.Fatalf("register mismatch request failed: %v", err)
	}
	defer response.Body.Close()

	if response.StatusCode != http.StatusBadRequest {
		t.Fatalf("expected status 400, got %d", response.StatusCode)
	}

	errorValue := readAPIError(t, response.Body)
	if errorValue != "password mismatch" {
		t.Fatalf("expected password mismatch error, got %q", errorValue)
	}

	var usersCount int64
	if err := database.Model(&models.User{}).Where("email = ?", email).Count(&usersCount).Error; err != nil {
		t.Fatalf("count users: %v", err)
	}
	if usersCount != 0 {
		t.Fatalf("expected user not to be created, found %d records", usersCount)
	}
}

// assertRegisterDuplicateResponseLooksLikeSuccess pins the silencing fix for
// #5: a register POST hitting an existing email must return the same status
// and JSON shape as a new-email enrollment. Set-Cookie inspection can still
// distinguish the cases (no auth/recovery cookies are issued for the duplicate),
// and that residual signal is documented in SECURITY.md.
func assertRegisterDuplicateResponseLooksLikeSuccess(t *testing.T, response *http.Response) {
	t.Helper()
	if response.StatusCode != http.StatusCreated {
		t.Fatalf("expected status 201 (mirroring new-account flow), got %d", response.StatusCode)
	}
	payload := map[string]any{}
	if err := json.NewDecoder(response.Body).Decode(&payload); err != nil {
		t.Fatalf("decode duplicate register response: %v", err)
	}
	if ok, _ := payload["ok"].(bool); !ok {
		t.Fatalf("expected ok=true in duplicate register response, got %#v", payload)
	}
	if step, _ := payload["next_step"].(string); step != "recovery_code" {
		t.Fatalf("expected next_step=recovery_code, got %#v", payload["next_step"])
	}
	if path, _ := payload["next_path"].(string); path != "/register" {
		t.Fatalf("expected next_path=/register, got %#v", payload["next_path"])
	}
	if _, exposed := payload["error"]; exposed {
		t.Fatalf("expected no error field in silenced response, got %#v", payload["error"])
	}
	if cookie := responseCookieValue(response.Cookies(), authCookieName); cookie != "" {
		t.Fatalf("expected no auth cookie in silenced response (would leak duplicate via Set-Cookie), got %q", cookie)
	}
	if cookie := responseCookieValue(response.Cookies(), recoveryCodeCookieName); cookie != "" {
		t.Fatalf("expected no recovery cookie in silenced response, got %q", cookie)
	}
}

func TestRegisterRejectsCaseInsensitiveDuplicateEmail(t *testing.T) {
	app, database := newOnboardingTestApp(t)
	existingEmail := "QA-Test2@Ovumcy.Local"

	passwordHash, err := bcrypt.GenerateFromPassword([]byte("StrongPass1"), bcrypt.DefaultCost)
	if err != nil {
		t.Fatalf("hash password: %v", err)
	}
	existingUser := models.User{
		Email:               existingEmail,
		PasswordHash:        string(passwordHash),
		LocalAuthEnabled:    true,
		Role:                models.RoleOwner,
		OnboardingCompleted: true,
		CycleLength:         models.DefaultCycleLength,
		PeriodLength:        models.DefaultPeriodLength,
		AutoPeriodFill:      true,
		CreatedAt:           time.Now().UTC(),
	}
	if err := database.Create(&existingUser).Error; err != nil {
		t.Fatalf("create existing user: %v", err)
	}

	form := url.Values{
		"email":            {"qa-test2@ovumcy.local"},
		"password":         {"StrongPass1"},
		"confirm_password": {"StrongPass1"},
	}
	request := httptest.NewRequest(http.MethodPost, "/api/auth/register", strings.NewReader(form.Encode()))
	request.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	request.Header.Set("Accept", "application/json")

	response, err := app.Test(request, -1)
	if err != nil {
		t.Fatalf("register duplicate request failed: %v", err)
	}
	defer response.Body.Close()

	assertRegisterDuplicateResponseLooksLikeSuccess(t, response)

	var usersCount int64
	if err := database.Model(&models.User{}).Where("lower(trim(email)) = ?", "qa-test2@ovumcy.local").Count(&usersCount).Error; err != nil {
		t.Fatalf("count normalized users: %v", err)
	}
	if usersCount != 1 {
		t.Fatalf("expected exactly one normalized email record, found %d", usersCount)
	}
}

func TestRegisterRejectsExactDuplicateEmail(t *testing.T) {
	app, database := newOnboardingTestApp(t)
	existingEmail := "qatest2@ovumcy.local"

	passwordHash, err := bcrypt.GenerateFromPassword([]byte("StrongPass1"), bcrypt.DefaultCost)
	if err != nil {
		t.Fatalf("hash password: %v", err)
	}
	existingUser := models.User{
		Email:               existingEmail,
		PasswordHash:        string(passwordHash),
		LocalAuthEnabled:    true,
		Role:                models.RoleOwner,
		OnboardingCompleted: true,
		CycleLength:         models.DefaultCycleLength,
		PeriodLength:        models.DefaultPeriodLength,
		AutoPeriodFill:      true,
		CreatedAt:           time.Now().UTC(),
	}
	if err := database.Create(&existingUser).Error; err != nil {
		t.Fatalf("create existing user: %v", err)
	}

	form := url.Values{
		"email":            {existingEmail},
		"password":         {"StrongPass1"},
		"confirm_password": {"StrongPass1"},
	}
	request := httptest.NewRequest(http.MethodPost, "/api/auth/register", strings.NewReader(form.Encode()))
	request.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	request.Header.Set("Accept", "application/json")

	response, err := app.Test(request, -1)
	if err != nil {
		t.Fatalf("register exact duplicate request failed: %v", err)
	}
	defer response.Body.Close()

	assertRegisterDuplicateResponseLooksLikeSuccess(t, response)

	var usersCount int64
	if err := database.Model(&models.User{}).Where("email = ?", existingEmail).Count(&usersCount).Error; err != nil {
		t.Fatalf("count exact users: %v", err)
	}
	if usersCount != 1 {
		t.Fatalf("expected exactly one exact email record, found %d", usersCount)
	}
}

func TestRegisterRejectsExactDuplicateEmailHTMLFlow(t *testing.T) {
	app, database := newOnboardingTestApp(t)
	existingEmail := "qatest2@ovumcy.local"

	passwordHash, err := bcrypt.GenerateFromPassword([]byte("StrongPass1"), bcrypt.DefaultCost)
	if err != nil {
		t.Fatalf("hash password: %v", err)
	}
	existingUser := models.User{
		Email:               existingEmail,
		PasswordHash:        string(passwordHash),
		LocalAuthEnabled:    true,
		Role:                models.RoleOwner,
		OnboardingCompleted: true,
		CycleLength:         models.DefaultCycleLength,
		PeriodLength:        models.DefaultPeriodLength,
		AutoPeriodFill:      true,
		CreatedAt:           time.Now().UTC(),
	}
	if err := database.Create(&existingUser).Error; err != nil {
		t.Fatalf("create existing user: %v", err)
	}

	form := url.Values{
		"email":            {existingEmail},
		"password":         {"StrongPass1"},
		"confirm_password": {"StrongPass1"},
	}
	request := httptest.NewRequest(http.MethodPost, "/api/auth/register", strings.NewReader(form.Encode()))
	request.Header.Set("Content-Type", "application/x-www-form-urlencoded")

	response, err := app.Test(request, -1)
	if err != nil {
		t.Fatalf("register exact duplicate html request failed: %v", err)
	}
	defer response.Body.Close()

	if response.StatusCode != http.StatusSeeOther {
		t.Fatalf("expected status 303, got %d", response.StatusCode)
	}
	location := response.Header.Get("Location")
	if strings.TrimSpace(location) != "/register" {
		t.Fatalf("expected redirect to /register, got %q", location)
	}

	if flashValue := responseCookieValue(response.Cookies(), flashCookieName); flashValue != "" {
		t.Fatalf("expected no flash cookie in silenced duplicate-email response (would leak existence), got %q", flashValue)
	}
	if cookie := responseCookieValue(response.Cookies(), authCookieName); cookie != "" {
		t.Fatalf("expected no auth cookie in silenced duplicate-email response, got %q", cookie)
	}
	if cookie := responseCookieValue(response.Cookies(), recoveryCodeCookieName); cookie != "" {
		t.Fatalf("expected no recovery cookie in silenced duplicate-email response, got %q", cookie)
	}

	var usersCount int64
	if err := database.Model(&models.User{}).Where("lower(trim(email)) = ?", existingEmail).Count(&usersCount).Error; err != nil {
		t.Fatalf("count exact users: %v", err)
	}
	if usersCount != 1 {
		t.Fatalf("expected exactly one normalized email record, found %d", usersCount)
	}
}
