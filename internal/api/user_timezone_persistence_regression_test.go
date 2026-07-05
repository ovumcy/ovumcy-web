package api

import (
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/gofiber/fiber/v3"
	"github.com/ovumcy/ovumcy-web/internal/models"
	"gorm.io/gorm"
)

// dashboardRequestWithTimezone drives an authenticated GET /dashboard carrying a
// timezone via both the X-Ovumcy-Timezone header and the ovumcy_tz cookie. The
// per-user timezone write path lives in AuthRequired, so any authenticated GET
// under it exercises the persistence side-effect.
func dashboardRequestWithTimezone(t *testing.T, app *fiber.App, authCookie string, timezoneName string) *http.Response {
	t.Helper()

	request := httptest.NewRequest(http.MethodGet, "/dashboard", nil)
	request.Header.Set("Cookie", joinCookieHeader(authCookie, timezoneCookieName+"="+timezoneName))
	if timezoneName != "" {
		request.Header.Set(timezoneHeaderName, timezoneName)
	}
	return mustAppResponse(t, app, request)
}

func reloadPersistedTimezone(t *testing.T, database *gorm.DB, userID uint) string {
	t.Helper()

	var reloaded models.User
	if err := database.First(&reloaded, userID).Error; err != nil {
		t.Fatalf("reload user %d: %v", userID, err)
	}
	return reloaded.Timezone
}

func TestAuthenticatedRequestPersistsValidRequestTimezone(t *testing.T) {
	app, database, _ := newOnboardingTestAppWithLocation(t, time.UTC)
	user := createOnboardingTestUser(t, database, "tz-persist@example.com", "StrongPass1", true)
	authCookie := loginAndExtractAuthCookie(t, app, user.Email, "StrongPass1")

	if got := reloadPersistedTimezone(t, database, user.ID); got != "" {
		t.Fatalf("expected empty stored timezone before any request, got %q", got)
	}

	response := dashboardRequestWithTimezone(t, app, authCookie, "America/Toronto")
	assertStatusCode(t, response, http.StatusOK)

	if got := reloadPersistedTimezone(t, database, user.ID); got != "America/Toronto" {
		t.Fatalf("expected persisted timezone America/Toronto, got %q", got)
	}

	// A second identical request must leave the stored value correct (the
	// no-DB-write-on-unchanged behavior is asserted at the service layer).
	second := dashboardRequestWithTimezone(t, app, authCookie, "America/Toronto")
	assertStatusCode(t, second, http.StatusOK)
	if got := reloadPersistedTimezone(t, database, user.ID); got != "America/Toronto" {
		t.Fatalf("expected timezone to remain America/Toronto after repeat request, got %q", got)
	}
}

// TestAuthenticatedRequestNeverPersistsUnsafeTimezone proves the validator gate:
// the "Local" token is rejected by input, so it must never reach the column.
func TestAuthenticatedRequestNeverPersistsUnsafeTimezone(t *testing.T) {
	app, database, _ := newOnboardingTestAppWithLocation(t, time.UTC)
	user := createOnboardingTestUser(t, database, "tz-unsafe@example.com", "StrongPass1", true)
	authCookie := loginAndExtractAuthCookie(t, app, user.Email, "StrongPass1")

	response := dashboardRequestWithTimezone(t, app, authCookie, "Local")
	assertStatusCode(t, response, http.StatusOK)

	if got := reloadPersistedTimezone(t, database, user.ID); got != "" {
		t.Fatalf("expected unsafe timezone to never persist, got %q", got)
	}
}

// TestRequestCannotPersistAnotherOwnersTimezone drives requests for two distinct
// owners and asserts each request only ever writes its own session user's
// timezone — the write is scoped to c.Locals user_id, never a request-supplied
// id, so owner B's request cannot rewrite owner A's stored value.
func TestRequestCannotPersistAnotherOwnersTimezone(t *testing.T) {
	app, database, _ := newOnboardingTestAppWithLocation(t, time.UTC)

	ownerA := createOnboardingTestUser(t, database, "tz-owner-a@example.com", "StrongPass1", true)
	ownerB := createOnboardingTestUser(t, database, "tz-owner-b@example.com", "StrongPass1", true)
	cookieA := loginAndExtractAuthCookie(t, app, ownerA.Email, "StrongPass1")
	cookieB := loginAndExtractAuthCookie(t, app, ownerB.Email, "StrongPass1")

	assertStatusCode(t, dashboardRequestWithTimezone(t, app, cookieA, "Europe/Belgrade"), http.StatusOK)
	assertStatusCode(t, dashboardRequestWithTimezone(t, app, cookieB, "Asia/Tokyo"), http.StatusOK)

	if got := reloadPersistedTimezone(t, database, ownerA.ID); got != "Europe/Belgrade" {
		t.Fatalf("expected owner A timezone Europe/Belgrade, got %q", got)
	}
	if got := reloadPersistedTimezone(t, database, ownerB.ID); got != "Asia/Tokyo" {
		t.Fatalf("expected owner B timezone Asia/Tokyo, got %q", got)
	}

	// Owner B issues another request; owner A's stored timezone stays untouched.
	assertStatusCode(t, dashboardRequestWithTimezone(t, app, cookieB, "Pacific/Auckland"), http.StatusOK)
	if got := reloadPersistedTimezone(t, database, ownerA.ID); got != "Europe/Belgrade" {
		t.Fatalf("expected owner A timezone unchanged at Europe/Belgrade after owner B request, got %q", got)
	}
	if got := reloadPersistedTimezone(t, database, ownerB.ID); got != "Pacific/Auckland" {
		t.Fatalf("expected owner B timezone updated to Pacific/Auckland, got %q", got)
	}
}
