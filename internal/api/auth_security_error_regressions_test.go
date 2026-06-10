package api

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/gofiber/fiber/v2"
	"github.com/ovumcy/ovumcy-web/internal/models"
	"github.com/ovumcy/ovumcy-web/internal/services"
)

type stubLoginWorkflowService struct {
	result services.LoginResult
	err    error
}

func (stub *stubLoginWorkflowService) Authenticate(context.Context, []byte, string, string, string, time.Duration, time.Time) (services.LoginResult, error) {
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
		"consent":          "true",
	}
	serialized, err := json.Marshal(requestBody)
	if err != nil {
		t.Fatalf("marshal register request: %v", err)
	}

	request := httptest.NewRequest(http.MethodPost, "/api/v1/users", bytes.NewReader(serialized))
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
	app.Post("/api/v1/sessions", handler.Login)

	request := httptest.NewRequest(http.MethodPost, "/api/v1/sessions", bytes.NewBufferString("email=owner%40example.com&password=StrongPass1"))
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

func TestLoginReturnsRateLimitedError(t *testing.T) {
	handler := &Handler{
		location:     time.UTC,
		loginService: &stubLoginWorkflowService{err: services.ErrAuthLoginRateLimited},
	}

	app := fiber.New()
	app.Post("/api/v1/sessions", handler.Login)

	request := httptest.NewRequest(http.MethodPost, "/api/v1/sessions", bytes.NewBufferString("email=owner%40example.com&password=StrongPass1"))
	request.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	request.Header.Set("Accept", "application/json")

	response, err := app.Test(request, -1)
	if err != nil {
		t.Fatalf("login request failed: %v", err)
	}
	defer response.Body.Close()

	if response.StatusCode != http.StatusTooManyRequests {
		t.Fatalf("expected status 429, got %d", response.StatusCode)
	}
	if message := readAPIError(t, response.Body); message != "too many login attempts" {
		t.Fatalf("expected error %q, got %q", "too many login attempts", message)
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
	app.Post("/api/v1/sessions", handler.Login)

	request := httptest.NewRequest(http.MethodPost, "/api/v1/sessions", bytes.NewBufferString("email=owner%40example.com&password=StrongPass1"))
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
