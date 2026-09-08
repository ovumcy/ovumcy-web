package api

import (
	"bytes"
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/gofiber/fiber/v3"
	"github.com/ovumcy/ovumcy-web/internal/models"
	"github.com/ovumcy/ovumcy-web/internal/services"
)

// countingLogoutAuthRepo records how many times the logout path reached the
// revoke, which is the one storage write Handler.Logout performs.
type countingLogoutAuthRepo struct {
	stubLogoutAuthRepo
	revokes int
}

func (repo *countingLogoutAuthRepo) BumpAuthSessionVersion(context.Context, uint) error {
	repo.revokes++
	return nil
}

func newLogoutTestApp(t *testing.T, repo *countingLogoutAuthRepo, limit int, sessionIDs []string) *fiber.App {
	t.Helper()
	authSvc := services.NewAuthService(repo)
	authSvc.ConfigureLogoutAttemptLimits(limit, time.Hour)

	handler := &Handler{
		location:    time.UTC,
		secretKey:   []byte("test-secret-key"),
		authService: authSvc,
	}

	app := fiber.New()
	requestIndex := 0
	app.Use(func(c fiber.Ctx) error {
		c.Locals(contextUserKey, &models.User{ID: 1, Role: models.RoleOwner})
		if requestIndex < len(sessionIDs) {
			c.Locals(contextAuthSessionKey, &services.AuthSessionClaims{UserID: 1, SessionID: sessionIDs[requestIndex]})
		}
		requestIndex++
		return c.Next()
	})
	app.Delete("/api/v1/sessions/current", handler.Logout)
	return app
}

func doLogout(t *testing.T, app *fiber.App) *http.Response {
	t.Helper()
	request := httptest.NewRequest(http.MethodDelete, "/api/v1/sessions/current", bytes.NewBufferString(""))
	request.Header.Set("Accept", "application/json")
	response, err := app.Test(request, testConfigNoTimeout)
	if err != nil {
		t.Fatalf("logout request failed: %v", err)
	}
	t.Cleanup(func() { _ = response.Body.Close() })
	return response
}

func authCookieIsCleared(response *http.Response) bool {
	for _, cookie := range response.Cookies() {
		if cookie.Name == authCookieName && cookie.Value == "" && (cookie.MaxAge < 0 || cookie.Expires.Before(time.Now())) {
			return true
		}
	}
	return false
}

// TestLogoutRevokesTheSessionBeforeTheBudgetIsChecked pins the order: a
// sign-out the owner asked for ends the session even when the per-account
// budget is spent. The refused request still reaches the revoke and still
// clears the auth cookie; only the success answer is withheld.
func TestLogoutRevokesTheSessionBeforeTheBudgetIsChecked(t *testing.T) {
	repo := &countingLogoutAuthRepo{}
	app := newLogoutTestApp(t, repo, 1, nil)

	first := doLogout(t, app)
	if first.StatusCode != http.StatusOK {
		t.Fatalf("first logout: status %d, want 200", first.StatusCode)
	}
	second := doLogout(t, app)
	if second.StatusCode != http.StatusTooManyRequests {
		t.Fatalf("second logout: status %d, want 429", second.StatusCode)
	}

	if repo.revokes != 2 {
		t.Fatalf("revokes = %d, want 2: the rate-limited sign-out must still revoke the session", repo.revokes)
	}
	if !authCookieIsCleared(second) {
		t.Fatal("the rate-limited sign-out answered 429 without clearing the auth cookie")
	}
}

// TestLogoutRefusalSendsABrowserToLogin: a plain form post (no JSON, no HTMX)
// whose budget is spent is already signed out, so it is answered the way a
// signed-out browser is — a redirect to /login carrying the refusal as a flash —
// not with a JSON envelope on a page whose cookies are gone.
func TestLogoutRefusalSendsABrowserToLogin(t *testing.T) {
	repo := &countingLogoutAuthRepo{}
	app := newLogoutTestApp(t, repo, 1, nil)

	if response := doLogout(t, app); response.StatusCode != http.StatusOK {
		t.Fatalf("first logout: status %d, want 200", response.StatusCode)
	}
	request := httptest.NewRequest(http.MethodDelete, "/api/v1/sessions/current", bytes.NewBufferString(""))
	request.Header.Set("Accept", "text/html")
	response, err := app.Test(request, testConfigNoTimeout)
	if err != nil {
		t.Fatalf("browser logout request failed: %v", err)
	}
	defer func() { _ = response.Body.Close() }()
	if response.StatusCode != http.StatusSeeOther || response.Header.Get("Location") != "/login" {
		t.Fatalf("browser logout on a spent budget: status %d location %q, want 303 to /login", response.StatusCode, response.Header.Get("Location"))
	}
	if !authCookieIsCleared(response) {
		t.Fatal("the refused browser sign-out did not clear the auth cookie")
	}
	if repo.revokes != 2 {
		t.Fatalf("revokes = %d, want 2", repo.revokes)
	}
}

// TestLogoutBudgetIsKeyedByTheSession: two sessions of one owner each get the
// budget; a budget keyed on the owner alone refused the second device's
// sign-out after the first had spent it.
func TestLogoutBudgetIsKeyedByTheSession(t *testing.T) {
	repo := &countingLogoutAuthRepo{}
	app := newLogoutTestApp(t, repo, 1, []string{"session-a", "session-b", "session-a"})

	if response := doLogout(t, app); response.StatusCode != http.StatusOK {
		t.Fatalf("session-a logout: status %d, want 200", response.StatusCode)
	}
	if response := doLogout(t, app); response.StatusCode != http.StatusOK {
		t.Fatalf("session-b logout: status %d, want 200 — the budget is per session, not per owner", response.StatusCode)
	}
	if response := doLogout(t, app); response.StatusCode != http.StatusTooManyRequests {
		t.Fatalf("session-a again: status %d, want 429 — the same session's budget is spent", response.StatusCode)
	}
	if repo.revokes != 3 {
		t.Fatalf("revokes = %d, want 3", repo.revokes)
	}
}
