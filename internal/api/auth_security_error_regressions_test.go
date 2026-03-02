package api

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/gofiber/fiber/v2"
	"github.com/terraincognita07/ovumcy/internal/models"
	"github.com/terraincognita07/ovumcy/internal/services"
)

type stubLoginWorkflowService struct {
	result services.LoginResult
	err    error
}

func (stub *stubLoginWorkflowService) Authenticate([]byte, string, string, time.Duration, time.Time) (services.LoginResult, error) {
	if stub.err != nil {
		return services.LoginResult{}, stub.err
	}
	return stub.result, nil
}

func TestRegisterReturnsSeedFailureAndRollsBackUser(t *testing.T) {
	app, database := newOnboardingTestApp(t)
	if err := database.Exec("DROP TABLE symptom_types").Error; err != nil {
		t.Fatalf("drop symptom_types: %v", err)
	}

	requestBody := map[string]any{
		"email":            "seed-failure@example.com",
		"password":         "StrongPass1",
		"confirm_password": "StrongPass1",
	}
	serialized, err := json.Marshal(requestBody)
	if err != nil {
		t.Fatalf("marshal register request: %v", err)
	}

	request := httptest.NewRequest(http.MethodPost, "/api/auth/register", bytes.NewReader(serialized))
	request.Header.Set("Content-Type", "application/json")
	request.Header.Set("Accept", "application/json")

	response, err := app.Test(request, -1)
	if err != nil {
		t.Fatalf("register request failed: %v", err)
	}
	defer response.Body.Close()

	if response.StatusCode != http.StatusInternalServerError {
		t.Fatalf("expected status 500, got %d", response.StatusCode)
	}
	if message := readAPIError(t, response.Body); message != "failed to seed symptoms" {
		t.Fatalf("expected error %q, got %q", "failed to seed symptoms", message)
	}

	var count int64
	if err := database.Model(&models.User{}).
		Where("lower(trim(email)) = ?", "seed-failure@example.com").
		Count(&count).Error; err != nil {
		t.Fatalf("count users by email: %v", err)
	}
	if count != 0 {
		t.Fatalf("expected transactional rollback and zero users, got %d", count)
	}
}

func TestLoginReturnsResetTokenIssueError(t *testing.T) {
	handler := &Handler{
		location:     time.UTC,
		loginService: &stubLoginWorkflowService{err: services.ErrLoginResetTokenIssue},
	}

	app := fiber.New()
	app.Post("/api/auth/login", handler.Login)

	request := httptest.NewRequest(http.MethodPost, "/api/auth/login", bytes.NewBufferString("email=owner%40example.com&password=StrongPass1"))
	request.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	request.Header.Set("Accept", "application/json")

	response, err := app.Test(request, -1)
	if err != nil {
		t.Fatalf("login request failed: %v", err)
	}
	defer response.Body.Close()

	if response.StatusCode != http.StatusInternalServerError {
		t.Fatalf("expected status 500, got %d", response.StatusCode)
	}
	if message := readAPIError(t, response.Body); message != "failed to create reset token" {
		t.Fatalf("expected error %q, got %q", "failed to create reset token", message)
	}
}

func TestLoginForcedResetCookieWriteFailureReturns500(t *testing.T) {
	handler := &Handler{
		location: time.UTC,
		loginService: &stubLoginWorkflowService{
			result: services.LoginResult{
				User: models.User{
					ID:                 1,
					Role:               models.RoleOwner,
					MustChangePassword: true,
				},
				RequiresPasswordReset: true,
				ResetToken:            "reset-token-value",
			},
		},
	}

	app := fiber.New()
	app.Post("/api/auth/login", handler.Login)

	request := httptest.NewRequest(http.MethodPost, "/api/auth/login", bytes.NewBufferString("email=owner%40example.com&password=StrongPass1"))
	request.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	request.Header.Set("Accept", "application/json")

	response, err := app.Test(request, -1)
	if err != nil {
		t.Fatalf("login request failed: %v", err)
	}
	defer response.Body.Close()

	if response.StatusCode != http.StatusInternalServerError {
		t.Fatalf("expected status 500, got %d", response.StatusCode)
	}
	if message := readAPIError(t, response.Body); message != "failed to create reset token" {
		t.Fatalf("expected error %q, got %q", "failed to create reset token", message)
	}
}

func TestRenderRecoveryCodeResponseCookieWriteFailureReturns500(t *testing.T) {
	handler := &Handler{
		location: time.UTC,
	}

	app := fiber.New()
	app.Get("/api/auth/recovery-response-test", func(c *fiber.Ctx) error {
		user := &models.User{
			ID:   1,
			Role: models.RoleOwner,
		}
		return handler.renderRecoveryCodeResponse(c, user, "ABCD-1234", fiber.StatusCreated)
	})

	request := httptest.NewRequest(http.MethodGet, "/api/auth/recovery-response-test", nil)

	response, err := app.Test(request, -1)
	if err != nil {
		t.Fatalf("recovery response request failed: %v", err)
	}
	defer response.Body.Close()

	if response.StatusCode != http.StatusInternalServerError {
		t.Fatalf("expected status 500, got %d", response.StatusCode)
	}
	if message := readAPIError(t, response.Body); message != "failed to persist recovery code" {
		t.Fatalf("expected error %q, got %q", "failed to persist recovery code", message)
	}
}
