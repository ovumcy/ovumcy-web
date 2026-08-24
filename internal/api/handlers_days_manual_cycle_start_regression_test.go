package api

import (
	"bytes"
	"context"
	"errors"
	"log"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/gofiber/fiber/v3"
	"github.com/ovumcy/ovumcy-web/internal/db"
	"github.com/ovumcy/ovumcy-web/internal/models"
	"github.com/ovumcy/ovumcy-web/internal/services"
	"gorm.io/gorm"
)

func TestMarkCycleStartRequiresAuthJSON(t *testing.T) {
	app, _ := newOnboardingTestApp(t)

	request := httptest.NewRequest(http.MethodPost, "/api/v1/days/2026-02-19/cycle-start", nil)
	request.Header.Set("Accept", "application/json")

	response, err := app.Test(request, testConfigNoTimeout)
	if err != nil {
		t.Fatalf("unauthenticated cycle-start request failed: %v", err)
	}
	defer func() { _ = response.Body.Close() }()

	if response.StatusCode != http.StatusUnauthorized {
		t.Fatalf("expected status 401, got %d", response.StatusCode)
	}
	if got := readAPIError(t, response.Body); got != "unauthorized" {
		t.Fatalf("expected unauthorized error, got %q", got)
	}
}

func TestMarkCycleStartRejectsUnsupportedLegacyRoleJSON(t *testing.T) {
	app, database := newOnboardingTestApp(t)
	user := createOnboardingTestUser(t, database, "manual-cycle-start-legacy@example.com", "StrongPass1", true)
	if err := database.Model(&models.User{}).Where("id = ?", user.ID).Update("role", "partner").Error; err != nil {
		t.Fatalf("set unsupported legacy role: %v", err)
	}
	user.Role = "partner"
	authCookie := issueAuthCookieForUser(t, user)

	request := httptest.NewRequest(http.MethodPost, "/api/v1/days/2026-02-19/cycle-start", nil)
	request.Header.Set("Accept", "application/json")
	request.Header.Set("Cookie", authCookie)

	response, err := app.Test(request, testConfigNoTimeout)
	if err != nil {
		t.Fatalf("unsupported legacy role cycle-start request failed: %v", err)
	}
	defer func() { _ = response.Body.Close() }()

	if response.StatusCode != http.StatusForbidden {
		t.Fatalf("expected status 403, got %d", response.StatusCode)
	}
	if got := readAPIError(t, response.Body); got != "web sign-in unavailable" {
		t.Fatalf("expected unsupported-role sign-in error, got %q", got)
	}
}

func TestMarkCycleStartHTMXWithCSRFRefreshesAndPersists(t *testing.T) {
	app, database := newOnboardingTestAppWithCSRF(t)
	user := createOnboardingTestUser(t, database, "manual-cycle-start-ui@example.com", "StrongPass1", true)
	authCookie := loginAndExtractAuthCookieWithCSRF(t, app, user.Email, "StrongPass1")
	csrfCookie, csrfToken := loadManualCycleStartCSRFContext(t, app, authCookie)

	targetDay := "2026-02-19"
	if err := database.Create(&models.DailyLog{
		UserID:   user.ID,
		Date:     mustParseManualCycleStartDay(t, targetDay),
		IsPeriod: false,
		Flow:     models.FlowMedium,
		Notes:    "keep me",
	}).Error; err != nil {
		t.Fatalf("create log: %v", err)
	}

	form := url.Values{"csrf_token": {csrfToken}}
	request := httptest.NewRequest(http.MethodPost, "/api/v1/days/"+targetDay+"/cycle-start?source=calendar", strings.NewReader(form.Encode()))
	request.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	request.Header.Set("HX-Request", "true")
	request.Header.Set("Cookie", joinCookieHeader(authCookie, cookiePair(csrfCookie)))

	response := mustAppResponse(t, app, request)
	if response.StatusCode != http.StatusNoContent {
		t.Fatalf("expected status 204, got %d", response.StatusCode)
	}
	if got := response.Header.Get("HX-Trigger"); got != "calendar-day-updated" {
		t.Fatalf("expected HX-Trigger calendar-day-updated, got %q", got)
	}
	if got := response.Header.Get("HX-Refresh"); got != "true" {
		t.Fatalf("expected HX-Refresh=true, got %q", got)
	}

	day := mustParseManualCycleStartDay(t, targetDay)
	entry, err := fetchLogByDateForTest(database, user.ID, day, time.UTC)
	if err != nil {
		t.Fatalf("load updated log: %v", err)
	}
	if !entry.IsPeriod {
		t.Fatalf("expected selected day to persist as period day")
	}
	if !entry.CycleStart {
		t.Fatalf("expected selected day to persist as the explicit cycle start")
	}
	if entry.Notes != "keep me" {
		t.Fatalf("expected existing notes to be preserved, got %q", entry.Notes)
	}

	persisted := models.User{}
	if err := database.First(&persisted, user.ID).Error; err != nil {
		t.Fatalf("load updated user: %v", err)
	}
	if persisted.LastPeriodStart != nil {
		t.Fatalf("expected manual cycle start to leave settings last_period_start unchanged, got %v", persisted.LastPeriodStart)
	}
}

// TestMarkCycleStartPlainRedirectHonorsCalendarSource pins the redirect target of
// the plain (non-HTMX, non-JSON) MarkCycleStart branch (handlers_days_write.go
// L185): `c.Query("source") == "calendar"` sends the user back to the calendar
// month view they marked from; every other source falls through to /dashboard. A
// CONDITIONALS_NEGATION mutant (`==` -> `!=`) swaps the two destinations, so a
// calendar-originated mark would bounce to /dashboard and lose the user's place.
func TestMarkCycleStartPlainRedirectHonorsCalendarSource(t *testing.T) {
	app, database := newOnboardingTestApp(t)
	user := createOnboardingTestUser(t, database, "manual-cycle-start-redirect@example.com", "StrongPass1", true)
	authCookie := issueAuthCookieForUser(t, user)

	// source=calendar -> back to the calendar month + day of the marked cycle start.
	calendarRequest := httptest.NewRequest(http.MethodPost, "/api/v1/days/2026-02-19/cycle-start?source=calendar", strings.NewReader(""))
	calendarRequest.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	calendarRequest.Header.Set("Cookie", authCookie)
	calendarResponse := mustAppResponse(t, app, calendarRequest)
	if calendarResponse.StatusCode != http.StatusSeeOther {
		t.Fatalf("expected plain cycle-start redirect status 303, got %d", calendarResponse.StatusCode)
	}
	if got := calendarResponse.Header.Get("Location"); got != "/calendar?month=2026-02&day=2026-02-19" {
		t.Fatalf("expected calendar-source cycle start to redirect back to the calendar, got %q", got)
	}

	// No calendar source -> the default dashboard destination (pins the else arm so
	// the negation is caught from both directions).
	dashboardRequest := httptest.NewRequest(http.MethodPost, "/api/v1/days/2026-03-19/cycle-start", strings.NewReader(""))
	dashboardRequest.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	dashboardRequest.Header.Set("Cookie", authCookie)
	dashboardResponse := mustAppResponse(t, app, dashboardRequest)
	if dashboardResponse.StatusCode != http.StatusSeeOther {
		t.Fatalf("expected plain cycle-start redirect status 303, got %d", dashboardResponse.StatusCode)
	}
	if got := dashboardResponse.Header.Get("Location"); got != "/dashboard" {
		t.Fatalf("expected non-calendar cycle start to redirect to /dashboard, got %q", got)
	}
}

func TestMarkCycleStartMissingCSRFRejectedByMiddleware(t *testing.T) {
	app, database := newOnboardingTestAppWithCSRF(t)
	user := createOnboardingTestUser(t, database, "manual-cycle-start-csrf@example.com", "StrongPass1", true)
	authCookie := loginAndExtractAuthCookieWithCSRF(t, app, user.Email, "StrongPass1")

	request := httptest.NewRequest(http.MethodPost, "/api/v1/days/2026-02-19/cycle-start", strings.NewReader(url.Values{}.Encode()))
	request.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	request.Header.Set("Cookie", authCookie)

	response := mustAppResponse(t, app, request)
	if response.StatusCode != http.StatusForbidden {
		t.Fatalf("expected csrf middleware status 403, got %d", response.StatusCode)
	}
}

func loadManualCycleStartCSRFContext(t *testing.T, app *fiber.App, authCookie string) (*http.Cookie, string) {
	t.Helper()

	request := httptest.NewRequest(http.MethodGet, "/dashboard", nil)
	request.Header.Set("Accept-Language", "en")
	request.Header.Set("Cookie", authCookie)

	response := mustAppResponse(t, app, request)
	if response.StatusCode != http.StatusOK {
		t.Fatalf("expected dashboard status 200 while preparing csrf context, got %d", response.StatusCode)
	}

	body := mustReadBodyString(t, response.Body)
	csrfToken := extractCSRFTokenFromHTML(t, body)
	csrfCookie := responseCookie(response.Cookies(), "ovumcy_csrf")
	if csrfCookie == nil || strings.TrimSpace(csrfCookie.Value) == "" {
		t.Fatalf("expected csrf cookie in dashboard response")
	}

	return csrfCookie, csrfToken
}

// failOnceListByUserRepository is the real daily-log repository with one
// scheduled read failure. It wraps rather than replaces, so every other call
// the request makes — including the mark's own re-read of the policy — reaches
// real storage and the handler runs to its ordinary success.
type failOnceListByUserRepository struct {
	services.DayLogRepository
	failNext atomic.Bool
}

func (repo *failOnceListByUserRepository) ListByUser(ctx context.Context, userID uint) ([]models.DailyLog, error) {
	if repo.failNext.CompareAndSwap(true, false) {
		return nil, errors.New("simulated cycle-start policy read failure")
	}
	return repo.DayLogRepository.ListByUser(ctx, userID)
}

// TestMarkCycleStartAuditsAnUnresolvedImplantationPolicy pins the one error
// this handler used to discard. ResolveManualCycleStartPolicy answers whether
// the marked day falls in the window the product cautions about; its failure
// left the policy at its zero value, so the caution was suppressed — the safe
// direction for a prediction claim — on a request that still answered 204 with
// nothing logged. Suppression is kept; the silence is not: the audit line the
// mark already emits carries the outcome, so an operator can tell a caution
// that was not warranted from one that could not be computed.
func TestMarkCycleStartAuditsAnUnresolvedImplantationPolicy(t *testing.T) {
	originalWriter := log.Writer()
	defer log.SetOutput(originalWriter)

	var faultyLogs *failOnceListByUserRepository
	app, database := newOnboardingTestAppWithOptions(t, onboardingTestAppOptions{
		auditLogEnabled: true,
		dayService: func(database *gorm.DB) *services.DayService {
			repositories := db.NewRepositories(database)
			faultyLogs = &failOnceListByUserRepository{DayLogRepository: repositories.DailyLogs}
			return services.NewDayServiceWithTx(faultyLogs, repositories.Users, func(ctx context.Context, fn func(services.DayLogRepository) error) error {
				return repositories.DailyLogs.WithinTransaction(ctx, func(tx *db.DailyLogRepository) error {
					return fn(tx)
				})
			})
		},
	})
	user := createOnboardingTestUser(t, database, "manual-cycle-start-policy-audit@example.com", "StrongPass1", true)
	authCookie := issueAuthCookieForUser(t, user)

	// Yesterday, relative to the live clock: the subject reads the clock, so a
	// fixed calendar date would pin a different case every year.
	targetDay := services.DateAtLocation(time.Now().In(time.UTC), time.UTC).AddDate(0, 0, -1).Format("2006-01-02")

	var output bytes.Buffer
	log.SetOutput(&output)
	faultyLogs.failNext.Store(true)

	request := httptest.NewRequest(http.MethodPost, "/api/v1/days/"+targetDay+"/cycle-start", strings.NewReader(""))
	request.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	request.Header.Set("HX-Request", "true")
	request.Header.Set("Cookie", authCookie)

	response := mustAppResponse(t, app, request)
	if response.StatusCode != http.StatusNoContent {
		t.Fatalf("expected the mark itself to succeed with 204 while only the policy read failed, got %d", response.StatusCode)
	}

	logged := output.String()
	if !strings.Contains(logged, `action="health.cycle_start_mark"`) {
		t.Fatalf("expected the mark's own audit line, got:\n%s", logged)
	}
	if !strings.Contains(logged, `cycle_start_policy="unresolved"`) {
		t.Fatalf("the implantation policy could not be resolved and the caution was suppressed, but no audit line says so; got:\n%s", logged)
	}
}

// The other direction: with every read healthy the field must be absent, or
// "unresolved" would be noise an operator learns to ignore rather than a
// signal.
func TestMarkCycleStartAuditOmitsThePolicyFieldWhenItResolves(t *testing.T) {
	originalWriter := log.Writer()
	defer log.SetOutput(originalWriter)

	app, database := newOnboardingTestAppWithOptions(t, onboardingTestAppOptions{auditLogEnabled: true})
	user := createOnboardingTestUser(t, database, "manual-cycle-start-policy-ok@example.com", "StrongPass1", true)
	authCookie := issueAuthCookieForUser(t, user)
	targetDay := services.DateAtLocation(time.Now().In(time.UTC), time.UTC).AddDate(0, 0, -1).Format("2006-01-02")

	var output bytes.Buffer
	log.SetOutput(&output)

	request := httptest.NewRequest(http.MethodPost, "/api/v1/days/"+targetDay+"/cycle-start", strings.NewReader(""))
	request.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	request.Header.Set("HX-Request", "true")
	request.Header.Set("Cookie", authCookie)

	response := mustAppResponse(t, app, request)
	if response.StatusCode != http.StatusNoContent {
		t.Fatalf("expected status 204, got %d", response.StatusCode)
	}

	logged := output.String()
	if !strings.Contains(logged, `action="health.cycle_start_mark"`) {
		t.Fatalf("expected the mark's own audit line, got:\n%s", logged)
	}
	if strings.Contains(logged, "cycle_start_policy=") {
		t.Fatalf("the policy resolved, so the audit line must not carry a degraded-read field; got:\n%s", logged)
	}
}

func mustParseManualCycleStartDay(t *testing.T, raw string) time.Time {
	t.Helper()

	day, err := services.ParseDayDate(raw, time.UTC)
	if err != nil {
		t.Fatalf("parse day %q: %v", raw, err)
	}
	return day
}
