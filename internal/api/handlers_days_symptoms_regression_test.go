package api

import (
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"testing"

	"github.com/terraincognita07/ovumcy/internal/models"
)

func TestCreateSymptomRejectsInvalidName(t *testing.T) {
	app, database := newOnboardingTestApp(t)
	user := createOnboardingTestUser(t, database, "create-symptom-invalid-name@example.com", "StrongPass1", true)
	authCookie := loginAndExtractAuthCookie(t, app, user.Email, "StrongPass1")

	request := httptest.NewRequest(http.MethodPost, "/api/symptoms", strings.NewReader(`{"name":"   ","icon":"x","color":"#123456"}`))
	request.Header.Set("Content-Type", "application/json")
	request.Header.Set("Accept", "application/json")
	request.Header.Set("Cookie", authCookie)

	response, err := app.Test(request, -1)
	if err != nil {
		t.Fatalf("create symptom request failed: %v", err)
	}
	defer response.Body.Close()

	if response.StatusCode != http.StatusBadRequest {
		t.Fatalf("expected status 400, got %d", response.StatusCode)
	}
	if got := readAPIError(t, response.Body); got != "invalid symptom name" {
		t.Fatalf("expected invalid symptom name, got %q", got)
	}
}

func TestCreateSymptomRejectsInvalidColor(t *testing.T) {
	app, database := newOnboardingTestApp(t)
	user := createOnboardingTestUser(t, database, "create-symptom-invalid-color@example.com", "StrongPass1", true)
	authCookie := loginAndExtractAuthCookie(t, app, user.Email, "StrongPass1")

	request := httptest.NewRequest(http.MethodPost, "/api/symptoms", strings.NewReader(`{"name":"Custom","icon":"x","color":"not-a-color"}`))
	request.Header.Set("Content-Type", "application/json")
	request.Header.Set("Accept", "application/json")
	request.Header.Set("Cookie", authCookie)

	response, err := app.Test(request, -1)
	if err != nil {
		t.Fatalf("create symptom request failed: %v", err)
	}
	defer response.Body.Close()

	if response.StatusCode != http.StatusBadRequest {
		t.Fatalf("expected status 400, got %d", response.StatusCode)
	}
	if got := readAPIError(t, response.Body); got != "invalid symptom color" {
		t.Fatalf("expected invalid symptom color, got %q", got)
	}
}

func TestDeleteSymptomReturnsNotFoundWhenMissing(t *testing.T) {
	app, database := newOnboardingTestApp(t)
	user := createOnboardingTestUser(t, database, "delete-symptom-missing@example.com", "StrongPass1", true)
	authCookie := loginAndExtractAuthCookie(t, app, user.Email, "StrongPass1")

	request := httptest.NewRequest(http.MethodDelete, "/api/symptoms/999999", nil)
	request.Header.Set("Accept", "application/json")
	request.Header.Set("Cookie", authCookie)

	response, err := app.Test(request, -1)
	if err != nil {
		t.Fatalf("delete missing symptom request failed: %v", err)
	}
	defer response.Body.Close()

	if response.StatusCode != http.StatusNotFound {
		t.Fatalf("expected status 404, got %d", response.StatusCode)
	}
	if got := readAPIError(t, response.Body); got != "symptom not found" {
		t.Fatalf("expected symptom not found error, got %q", got)
	}
}

func TestDeleteSymptomRejectsBuiltinSymptom(t *testing.T) {
	app, database := newOnboardingTestApp(t)
	user := createOnboardingTestUser(t, database, "delete-symptom-builtin@example.com", "StrongPass1", true)
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

	request := httptest.NewRequest(http.MethodDelete, "/api/symptoms/"+strconv.FormatUint(uint64(symptom.ID), 10), nil)
	request.Header.Set("Accept", "application/json")
	request.Header.Set("Cookie", authCookie)

	response, err := app.Test(request, -1)
	if err != nil {
		t.Fatalf("delete builtin symptom request failed: %v", err)
	}
	defer response.Body.Close()

	if response.StatusCode != http.StatusBadRequest {
		t.Fatalf("expected status 400, got %d", response.StatusCode)
	}
	if got := readAPIError(t, response.Body); got != "built-in symptom cannot be deleted" {
		t.Fatalf("expected builtin-delete error, got %q", got)
	}
}

func TestDeleteSymptomRejectsOutOfRangeID(t *testing.T) {
	app, database := newOnboardingTestApp(t)
	user := createOnboardingTestUser(t, database, "delete-symptom-out-of-range@example.com", "StrongPass1", true)
	authCookie := loginAndExtractAuthCookie(t, app, user.Email, "StrongPass1")

	request := httptest.NewRequest(http.MethodDelete, "/api/symptoms/"+overflowUintStringForTest(), nil)
	request.Header.Set("Accept", "application/json")
	request.Header.Set("Cookie", authCookie)

	response, err := app.Test(request, -1)
	if err != nil {
		t.Fatalf("delete out-of-range symptom request failed: %v", err)
	}
	defer response.Body.Close()

	if response.StatusCode != http.StatusBadRequest {
		t.Fatalf("expected status 400, got %d", response.StatusCode)
	}
	if got := readAPIError(t, response.Body); got != "invalid symptom id" {
		t.Fatalf("expected invalid symptom id error, got %q", got)
	}
}
