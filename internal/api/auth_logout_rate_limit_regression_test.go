package api

import (
	"bytes"
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/gofiber/fiber/v2"
	"github.com/ovumcy/ovumcy-web/internal/models"
	"github.com/ovumcy/ovumcy-web/internal/services"
)

// stubLogoutAuthRepo is the minimal AuthUserRepository surface required to
// drive Handler.Logout end-to-end without a database. Logout only reaches
// BumpAuthSessionVersion on the success path via RevokeAuthSessions; the
// other methods are unused but must exist to satisfy the interface.
type stubLogoutAuthRepo struct{}

func (stubLogoutAuthRepo) ExistsByNormalizedEmail(context.Context, string) (bool, error) {
	return false, nil
}

func (stubLogoutAuthRepo) FindByNormalizedEmail(context.Context, string) (models.User, error) {
	return models.User{}, nil
}

func (stubLogoutAuthRepo) FindByNormalizedEmailOptional(context.Context, string) (models.User, bool, error) {
	return models.User{}, false, nil
}

func (stubLogoutAuthRepo) FindByID(context.Context, uint) (models.User, error) {
	return models.User{}, nil
}

func (stubLogoutAuthRepo) Create(context.Context, *models.User) error { return nil }
func (stubLogoutAuthRepo) Save(context.Context, *models.User) error   { return nil }

func (stubLogoutAuthRepo) UpdateRecoveryCodeHashAndRevokeSessions(context.Context, uint, string) error {
	return nil
}

func (stubLogoutAuthRepo) UpdatePasswordAndRevokeSessions(context.Context, uint, string, bool) error {
	return nil
}

func (stubLogoutAuthRepo) UpdatePasswordRecoveryCodeAndRevokeSessions(context.Context, uint, string, string, bool) error {
	return nil
}

func (stubLogoutAuthRepo) BumpAuthSessionVersion(context.Context, uint) error { return nil }

// TestLogoutHandlerEnforcesPerAccountRateLimit asserts that Handler.Logout
// returns 429 with the documented error message when the per-account logout
// limit configured on AuthService is exceeded. The first request records an
// attempt under the limit; the second crosses it.
func TestLogoutHandlerEnforcesPerAccountRateLimit(t *testing.T) {
	authSvc := services.NewAuthService(stubLogoutAuthRepo{})
	authSvc.ConfigureLogoutAttemptLimits(1, time.Hour)

	handler := &Handler{
		location:    time.UTC,
		secretKey:   []byte("test-secret-key"),
		authService: authSvc,
	}

	app := fiber.New()
	app.Use(func(c *fiber.Ctx) error {
		c.Locals(contextUserKey, &models.User{ID: 1, Role: models.RoleOwner})
		return c.Next()
	})
	app.Delete("/api/v1/sessions/current", handler.Logout)

	first := httptest.NewRequest(http.MethodDelete, "/api/v1/sessions/current", bytes.NewBufferString(""))
	first.Header.Set("Accept", "application/json")
	firstResp, err := app.Test(first, -1)
	if err != nil {
		t.Fatalf("first logout request failed: %v", err)
	}
	defer firstResp.Body.Close()
	if firstResp.StatusCode != http.StatusOK && firstResp.StatusCode != http.StatusSeeOther {
		t.Fatalf("expected first logout to succeed (200 or 303), got %d", firstResp.StatusCode)
	}

	second := httptest.NewRequest(http.MethodDelete, "/api/v1/sessions/current", bytes.NewBufferString(""))
	second.Header.Set("Accept", "application/json")
	secondResp, err := app.Test(second, -1)
	if err != nil {
		t.Fatalf("second logout request failed: %v", err)
	}
	defer secondResp.Body.Close()

	if secondResp.StatusCode != http.StatusTooManyRequests {
		t.Fatalf("expected status 429 on rate-limited logout, got %d", secondResp.StatusCode)
	}
	if message := readAPIError(t, secondResp.Body); message != "too many logout attempts" {
		t.Fatalf("expected error %q, got %q", "too many logout attempts", message)
	}
}
