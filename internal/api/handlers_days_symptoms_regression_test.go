package api

import (
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/ovumcy/ovumcy-web/internal/models"
)

func TestCreateSymptomRejectsInvalidName(t *testing.T) {
	app, database := newOnboardingTestApp(t)
	user := createOnboardingTestUser(t, database, "create-symptom-invalid-name@example.com", "StrongPass1", true)
	authCookie := loginAndExtractAuthCookie(t, app, user.Email, "StrongPass1")

	request := httptest.NewRequest(http.MethodPost, "/api/v1/symptoms", strings.NewReader(`{"name":"   ","icon":"x","color":"#123456"}`))
	request.Header.Set("Content-Type", "application/json")
	request.Header.Set("Accept", "application/json")
	request.Header.Set("Cookie", authCookie)

	response, err := app.Test(request, testConfigNoTimeout)
	if err != nil {
		t.Fatalf("create symptom request failed: %v", err)
	}
	defer func() { _ = response.Body.Close() }()

	if response.StatusCode != http.StatusBadRequest {
		t.Fatalf("expected status 400, got %d", response.StatusCode)
	}
	if got := readAPIError(t, response.Body); got != "symptom name is required" {
		t.Fatalf("expected symptom name is required, got %q", got)
	}
}

func TestCreateSymptomRejectsInvalidColor(t *testing.T) {
	app, database := newOnboardingTestApp(t)
	user := createOnboardingTestUser(t, database, "create-symptom-invalid-color@example.com", "StrongPass1", true)
	authCookie := loginAndExtractAuthCookie(t, app, user.Email, "StrongPass1")

	request := httptest.NewRequest(http.MethodPost, "/api/v1/symptoms", strings.NewReader(`{"name":"Custom","icon":"x","color":"not-a-color"}`))
	request.Header.Set("Content-Type", "application/json")
	request.Header.Set("Accept", "application/json")
	request.Header.Set("Cookie", authCookie)

	response, err := app.Test(request, testConfigNoTimeout)
	if err != nil {
		t.Fatalf("create symptom request failed: %v", err)
	}
	defer func() { _ = response.Body.Close() }()

	if response.StatusCode != http.StatusBadRequest {
		t.Fatalf("expected status 400, got %d", response.StatusCode)
	}
	if got := readAPIError(t, response.Body); got != "invalid symptom color" {
		t.Fatalf("expected invalid symptom color, got %q", got)
	}
}

func TestCreateSymptomRejectsDuplicateName(t *testing.T) {
	app, database := newOnboardingTestApp(t)
	user := createOnboardingTestUser(t, database, "create-symptom-duplicate@example.com", "StrongPass1", true)
	authCookie := loginAndExtractAuthCookie(t, app, user.Email, "StrongPass1")

	symptom := models.SymptomType{
		UserID: user.ID,
		Name:   "Custom",
		Icon:   "x",
		Color:  "#123456",
	}
	if err := database.Create(&symptom).Error; err != nil {
		t.Fatalf("create existing custom symptom: %v", err)
	}

	request := httptest.NewRequest(http.MethodPost, "/api/v1/symptoms", strings.NewReader(`{"name":" cUsToM ","icon":"y","color":"#abcdef"}`))
	request.Header.Set("Content-Type", "application/json")
	request.Header.Set("Accept", "application/json")
	request.Header.Set("Cookie", authCookie)

	response, err := app.Test(request, testConfigNoTimeout)
	if err != nil {
		t.Fatalf("create duplicate symptom request failed: %v", err)
	}
	defer func() { _ = response.Body.Close() }()

	if response.StatusCode != http.StatusConflict {
		t.Fatalf("expected status 409, got %d", response.StatusCode)
	}
	if got := readAPIError(t, response.Body); got != "symptom name already exists" {
		t.Fatalf("expected duplicate name error, got %q", got)
	}
}

func TestCreateSymptomRejectsLocalizedBuiltinName(t *testing.T) {
	app, database := newOnboardingTestApp(t)
	user := createOnboardingTestUser(t, database, "create-symptom-localized-builtin@example.com", "StrongPass1", true)
	authCookie := loginAndExtractAuthCookie(t, app, user.Email, "StrongPass1")

	request := httptest.NewRequest(http.MethodPost, "/api/v1/symptoms", strings.NewReader(`{"name":"Усталость","icon":"x","color":"#123456"}`))
	request.Header.Set("Content-Type", "application/json")
	request.Header.Set("Accept", "application/json")
	request.Header.Set("Cookie", authCookie)

	response, err := app.Test(request, testConfigNoTimeout)
	if err != nil {
		t.Fatalf("create localized builtin symptom request failed: %v", err)
	}
	defer func() { _ = response.Body.Close() }()

	if response.StatusCode != http.StatusConflict {
		t.Fatalf("expected status 409, got %d", response.StatusCode)
	}
	if got := readAPIError(t, response.Body); got != "symptom name already exists" {
		t.Fatalf("expected duplicate builtin-name error, got %q", got)
	}
}

func TestCreateSymptomRejectsMarkupLikeName(t *testing.T) {
	app, database := newOnboardingTestApp(t)
	user := createOnboardingTestUser(t, database, "create-symptom-markup@example.com", "StrongPass1", true)
	authCookie := loginAndExtractAuthCookie(t, app, user.Email, "StrongPass1")

	request := httptest.NewRequest(http.MethodPost, "/api/v1/symptoms", strings.NewReader(`{"name":"<script>alert(1)</script>","icon":"x","color":"#123456"}`))
	request.Header.Set("Content-Type", "application/json")
	request.Header.Set("Accept", "application/json")
	request.Header.Set("Cookie", authCookie)

	response, err := app.Test(request, testConfigNoTimeout)
	if err != nil {
		t.Fatalf("create markup symptom request failed: %v", err)
	}
	defer func() { _ = response.Body.Close() }()

	if response.StatusCode != http.StatusBadRequest {
		t.Fatalf("expected status 400, got %d", response.StatusCode)
	}
	if got := readAPIError(t, response.Body); got != "symptom name contains invalid characters" {
		t.Fatalf("expected invalid character error, got %q", got)
	}
}

func TestCreateSymptomRejectsTooLongName(t *testing.T) {
	app, database := newOnboardingTestApp(t)
	user := createOnboardingTestUser(t, database, "create-symptom-too-long@example.com", "StrongPass1", true)
	authCookie := loginAndExtractAuthCookie(t, app, user.Email, "StrongPass1")

	request := httptest.NewRequest(http.MethodPost, "/api/v1/symptoms", strings.NewReader(`{"name":"12345678901234567890123456789012345678901","icon":"x","color":"#123456"}`))
	request.Header.Set("Content-Type", "application/json")
	request.Header.Set("Accept", "application/json")
	request.Header.Set("Cookie", authCookie)

	response, err := app.Test(request, testConfigNoTimeout)
	if err != nil {
		t.Fatalf("create too-long symptom request failed: %v", err)
	}
	defer func() { _ = response.Body.Close() }()

	if response.StatusCode != http.StatusBadRequest {
		t.Fatalf("expected status 400, got %d", response.StatusCode)
	}
	if got := readAPIError(t, response.Body); got != "symptom name is too long" {
		t.Fatalf("expected name-too-long error, got %q", got)
	}
}

func TestArchiveSymptomReturnsNotFoundWhenMissing(t *testing.T) {
	app, database := newOnboardingTestApp(t)
	user := createOnboardingTestUser(t, database, "hide-symptom-missing@example.com", "StrongPass1", true)
	authCookie := loginAndExtractAuthCookie(t, app, user.Email, "StrongPass1")

	request := httptest.NewRequest(http.MethodDelete, "/api/v1/symptoms/999999", nil)
	request.Header.Set("Accept", "application/json")
	request.Header.Set("Cookie", authCookie)

	response, err := app.Test(request, testConfigNoTimeout)
	if err != nil {
		t.Fatalf("archive missing symptom request failed: %v", err)
	}
	defer func() { _ = response.Body.Close() }()

	if response.StatusCode != http.StatusNotFound {
		t.Fatalf("expected status 404, got %d", response.StatusCode)
	}
	if got := readAPIError(t, response.Body); got != "symptom not found" {
		t.Fatalf("expected symptom not found error, got %q", got)
	}
}

func TestArchiveSymptomRejectsBuiltinSymptom(t *testing.T) {
	app, database := newOnboardingTestApp(t)
	user := createOnboardingTestUser(t, database, "hide-symptom-builtin@example.com", "StrongPass1", true)
	authCookie := loginAndExtractAuthCookie(t, app, user.Email, "StrongPass1")

	symptom := models.SymptomType{
		UserID:    user.ID,
		Name:      "Builtin",
		Icon:      "x",
		Color:     "#123456",
		IsBuiltin: true,
	}
	if err := database.Create(&symptom).Error; err != nil {
		t.Fatalf("create builtin symptom: %v", err)
	}

	request := httptest.NewRequest(http.MethodDelete, "/api/v1/symptoms/"+strconv.FormatUint(uint64(symptom.ID), 10), nil)
	request.Header.Set("Accept", "application/json")
	request.Header.Set("Cookie", authCookie)

	response, err := app.Test(request, testConfigNoTimeout)
	if err != nil {
		t.Fatalf("archive builtin symptom request failed: %v", err)
	}
	defer func() { _ = response.Body.Close() }()

	if response.StatusCode != http.StatusBadRequest {
		t.Fatalf("expected status 400, got %d", response.StatusCode)
	}
	if got := readAPIError(t, response.Body); got != "built-in symptom cannot be hidden" {
		t.Fatalf("expected builtin-hide error, got %q", got)
	}
}

func TestArchiveSymptomRejectsOutOfRangeID(t *testing.T) {
	app, database := newOnboardingTestApp(t)
	user := createOnboardingTestUser(t, database, "hide-symptom-out-of-range@example.com", "StrongPass1", true)
	authCookie := loginAndExtractAuthCookie(t, app, user.Email, "StrongPass1")

	request := httptest.NewRequest(http.MethodDelete, "/api/v1/symptoms/"+overflowUintStringForTest(), nil)
	request.Header.Set("Accept", "application/json")
	request.Header.Set("Cookie", authCookie)

	response, err := app.Test(request, testConfigNoTimeout)
	if err != nil {
		t.Fatalf("archive out-of-range symptom request failed: %v", err)
	}
	defer func() { _ = response.Body.Close() }()

	if response.StatusCode != http.StatusBadRequest {
		t.Fatalf("expected status 400, got %d", response.StatusCode)
	}
	if got := readAPIError(t, response.Body); got != "invalid symptom id" {
		t.Fatalf("expected invalid symptom id error, got %q", got)
	}
}

func TestRestoreSymptomRejectsDuplicateActiveName(t *testing.T) {
	app, database := newOnboardingTestApp(t)
	user := createOnboardingTestUser(t, database, "restore-symptom-duplicate@example.com", "StrongPass1", true)
	authCookie := loginAndExtractAuthCookie(t, app, user.Email, "StrongPass1")

	activeSymptom := models.SymptomType{
		UserID: user.ID,
		Name:   "Joint stiffness",
		Icon:   "x",
		Color:  "#123456",
	}
	if err := database.Create(&activeSymptom).Error; err != nil {
		t.Fatalf("create active symptom: %v", err)
	}

	archivedAt := time.Now().UTC()
	archivedSymptom := models.SymptomType{
		UserID:     user.ID,
		Name:       "Joint stiffness",
		Icon:       "y",
		Color:      "#ABCDEF",
		ArchivedAt: &archivedAt,
	}
	if err := database.Create(&archivedSymptom).Error; err != nil {
		t.Fatalf("create archived symptom: %v", err)
	}

	request := httptest.NewRequest(http.MethodPost, "/api/v1/symptoms/"+strconv.FormatUint(uint64(archivedSymptom.ID), 10)+"/restore", nil)
	request.Header.Set("Accept", "application/json")
	request.Header.Set("Cookie", authCookie)

	response, err := app.Test(request, testConfigNoTimeout)
	if err != nil {
		t.Fatalf("restore duplicate symptom request failed: %v", err)
	}
	defer func() { _ = response.Body.Close() }()

	if response.StatusCode != http.StatusConflict {
		t.Fatalf("expected status 409, got %d", response.StatusCode)
	}
	if got := readAPIError(t, response.Body); got != "symptom name already exists" {
		t.Fatalf("expected duplicate restore error, got %q", got)
	}
}
