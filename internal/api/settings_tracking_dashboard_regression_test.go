package api

import (
	"net/http"
	"net/http/httptest"
	"net/url"
	"regexp"
	"testing"
	"time"

	"github.com/ovumcy/ovumcy-web/internal/models"
	"github.com/ovumcy/ovumcy-web/internal/services"
)

func TestTrackingSettingsExposeBBTAndCervicalMucusOnDashboard(t *testing.T) {
	ctx := newSettingsSecurityTestContext(t, "settings-tracking-dashboard@example.com")

	response := settingsFormRequestWithCSRF(t, ctx, http.MethodPatch, "/api/v1/users/current/tracking", url.Values{
		"track_bbt":            {"true"},
		"track_cervical_mucus": {"true"},
		"temperature_unit":     {"c"},
	}, map[string]string{
		"HX-Request": "true",
	})
	assertStatusCode(t, response, http.StatusOK)

	dashboardRequest := httptest.NewRequest(http.MethodGet, "/dashboard", nil)
	dashboardRequest.Header.Set("Accept-Language", "en")
	dashboardRequest.Header.Set("Cookie", ctx.authCookie)

	dashboardResponse := mustAppResponse(t, ctx.app, dashboardRequest)
	assertStatusCode(t, dashboardResponse, http.StatusOK)
	rendered := mustReadBodyString(t, dashboardResponse.Body)

	assertBodyContainsAll(t, rendered,
		bodyStringMatch{fragment: `id="dashboard-bbt"`, message: "expected dashboard BBT field after enabling tracking"},
		bodyStringMatch{fragment: `name="cervical_mucus"`, message: "expected dashboard cervical mucus controls after enabling tracking"},
	)
}

func TestTrackingSettingsGateLHTestFieldOnDashboard(t *testing.T) {
	ctx := newSettingsSecurityTestContext(t, "settings-tracking-lh@example.com")

	// The LH test field is opt-in: it must stay absent until the owner enables it,
	// then appear on the dashboard once track_lh_test is on (render-path gating).
	beforeRequest := httptest.NewRequest(http.MethodGet, "/dashboard", nil)
	beforeRequest.Header.Set("Accept-Language", "en")
	beforeRequest.Header.Set("Cookie", ctx.authCookie)
	beforeResponse := mustAppResponse(t, ctx.app, beforeRequest)
	assertStatusCode(t, beforeResponse, http.StatusOK)
	assertBodyNotContainsAll(t, mustReadBodyString(t, beforeResponse.Body),
		bodyStringMatch{fragment: `name="lh_test"`, message: "did not expect dashboard LH test controls before enabling tracking"},
	)

	response := settingsFormRequestWithCSRF(t, ctx, http.MethodPatch, "/api/v1/users/current/tracking", url.Values{
		"track_lh_test":    {"true"},
		"temperature_unit": {"c"},
	}, map[string]string{
		"HX-Request": "true",
	})
	assertStatusCode(t, response, http.StatusOK)

	dashboardRequest := httptest.NewRequest(http.MethodGet, "/dashboard", nil)
	dashboardRequest.Header.Set("Accept-Language", "en")
	dashboardRequest.Header.Set("Cookie", ctx.authCookie)
	dashboardResponse := mustAppResponse(t, ctx.app, dashboardRequest)
	assertStatusCode(t, dashboardResponse, http.StatusOK)
	assertBodyContainsAll(t, mustReadBodyString(t, dashboardResponse.Body),
		bodyStringMatch{fragment: `name="lh_test"`, message: "expected dashboard LH test controls after enabling tracking"},
	)
}

func TestCalendarDayEditorRendersPersistedLHTestValue(t *testing.T) {
	ctx := newSettingsSecurityTestContext(t, "settings-tracking-lh-value@example.com")

	if err := ctx.database.Model(&models.User{}).Where("id = ?", ctx.user.ID).
		Update("track_lh_test", true).Error; err != nil {
		t.Fatalf("enable lh tracking: %v", err)
	}
	today := services.DateAtLocation(time.Now().In(time.UTC), time.UTC)
	if err := ctx.database.Create(&models.DailyLog{
		UserID: ctx.user.ID,
		Date:   today,
		LHTest: models.LHTestPeak,
	}).Error; err != nil {
		t.Fatalf("seed persisted lh day: %v", err)
	}

	editorRequest := httptest.NewRequest(http.MethodGet, "/calendar/day/"+today.Format("2006-01-02")+"?mode=edit", nil)
	editorRequest.Header.Set("Accept-Language", "en")
	editorRequest.Header.Set("Cookie", ctx.authCookie)
	editorResponse := mustAppResponse(t, ctx.app, editorRequest)
	assertStatusCode(t, editorResponse, http.StatusOK)
	rendered := mustReadBodyString(t, editorResponse.Body)

	// A persisted value must render as the pre-selected radio (regression guard
	// for the FindByUserAndDayRange column-select load path).
	if !regexp.MustCompile(`value="peak"\s+class="choice-input"\s+checked`).MatchString(rendered) {
		t.Fatal("expected persisted LH peak value to render as the checked option")
	}
}

func TestTrackingSettingsJSONResponseIncludesLHTest(t *testing.T) {
	ctx := newSettingsSecurityTestContext(t, "settings-tracking-lh-json@example.com")

	response := settingsFormRequestWithCSRF(t, ctx, http.MethodPatch, "/api/v1/users/current/tracking", url.Values{
		"track_lh_test":    {"true"},
		"temperature_unit": {"c"},
	}, map[string]string{
		"Accept": "application/json",
	})
	assertStatusCode(t, response, http.StatusOK)
	assertBodyContainsAll(t, mustReadBodyString(t, response.Body),
		bodyStringMatch{fragment: `"track_lh_test":true`, message: "expected JSON tracking response to echo track_lh_test"},
	)
}

func TestSettingsPageKeepsPersistedCycleValuesAfterRecoveryCodeRegeneration(t *testing.T) {
	ctx := newSettingsSecurityTestContext(t, "settings-recovery-return@example.com")

	response := settingsFormRequestWithCSRF(t, ctx, http.MethodPost, "/api/v1/users/current/recovery-code", url.Values{
		"password": {"StrongPass1"},
	}, nil)
	assertStatusCode(t, response, http.StatusSeeOther)

	recoveryCookie := responseCookieValue(response.Cookies(), recoveryCodeCookieName)
	if recoveryCookie == "" {
		t.Fatal("expected recovery-code page cookie after regeneration")
	}
	newAuthCookie := responseCookieValue(response.Cookies(), authCookieName)
	if newAuthCookie == "" {
		t.Fatal("expected fresh auth cookie after recovery code regeneration (session version was bumped)")
	}
	refreshedAuthCookie := authCookieName + "=" + newAuthCookie

	recoveryPageRequest := httptest.NewRequest(http.MethodGet, "/recovery-code", nil)
	recoveryPageRequest.Header.Set("Accept-Language", "en")
	recoveryPageRequest.Header.Set("Cookie", refreshedAuthCookie+"; "+recoveryCodeCookieName+"="+recoveryCookie)

	recoveryPageResponse := mustAppResponse(t, ctx.app, recoveryPageRequest)
	assertStatusCode(t, recoveryPageResponse, http.StatusOK)
	recoveryPage := mustReadBodyString(t, recoveryPageResponse.Body)
	assertBodyContainsAll(t, recoveryPage,
		bodyStringMatch{fragment: `form action="/settings"`, message: "expected recovery confirmation to return to settings"},
	)
	assertBodyNotContainsAll(t, recoveryPage,
		bodyStringMatch{fragment: `name="saved"`, message: "did not expect recovery confirmation checkbox to submit a saved query parameter"},
	)

	var persisted struct {
		PeriodLength       int
		UnpredictableCycle bool
	}
	if err := ctx.database.Model(&models.User{}).
		Select("period_length", "unpredictable_cycle").
		Where("id = ?", ctx.user.ID).
		First(&persisted).Error; err != nil {
		t.Fatalf("load persisted settings after recovery-code regeneration: %v", err)
	}
	if persisted.PeriodLength != 5 {
		t.Fatalf("expected persisted settings period length to stay at 5 after recovery-code regeneration, got %d", persisted.PeriodLength)
	}
	if persisted.UnpredictableCycle {
		t.Fatalf("did not expect persisted unpredictable_cycle to change after recovery-code regeneration")
	}

	rendered := renderSettingsPageForTest(t, ctx.app, refreshedAuthCookie)
	if !regexp.MustCompile(`name="period_length"[^>]*value="5"`).MatchString(rendered) {
		t.Fatalf("expected persisted settings period length to stay at 5 days after recovery-code regeneration")
	}
	if regexp.MustCompile(`name="unpredictable_cycle"[^>]*checked`).MatchString(rendered) {
		t.Fatalf("did not expect unpredictable_cycle to become checked after recovery-code regeneration")
	}
}

func TestTrackingSettingsHideSensitiveSectionsOnDashboardAndCalendar(t *testing.T) {
	ctx := newSettingsSecurityTestContext(t, "settings-tracking-privacy@example.com")

	response := settingsFormRequestWithCSRF(t, ctx, http.MethodPatch, "/api/v1/users/current/tracking", url.Values{
		"hide_sex_chip":      {"true"},
		"hide_cycle_factors": {"true"},
		"hide_notes_field":   {"true"},
		"temperature_unit":   {"c"},
	}, map[string]string{
		"HX-Request": "true",
	})
	assertStatusCode(t, response, http.StatusOK)

	dashboardRequest := httptest.NewRequest(http.MethodGet, "/dashboard", nil)
	dashboardRequest.Header.Set("Accept-Language", "en")
	dashboardRequest.Header.Set("Cookie", ctx.authCookie)

	dashboardResponse := mustAppResponse(t, ctx.app, dashboardRequest)
	assertStatusCode(t, dashboardResponse, http.StatusOK)
	dashboardBody := mustReadBodyString(t, dashboardResponse.Body)
	assertBodyContainsAll(t, dashboardBody,
		bodyStringMatch{fragment: "Intimacy", message: "expected intimacy section heading to remain visible"},
		bodyStringMatch{fragment: "This section is hidden in settings.", message: "expected dashboard intimacy hidden hint"},
	)
	assertBodyNotContainsAll(t, dashboardBody,
		bodyStringMatch{fragment: `id="today-notes"`, message: "did not expect dashboard notes field when hidden"},
		bodyStringMatch{fragment: `name="cycle_factor_keys"`, message: "did not expect dashboard cycle factor inputs when hidden"},
		bodyStringMatch{fragment: `name="sex_activity"`, message: "did not expect dashboard sex activity inputs when hidden"},
	)

	today := services.DateAtLocation(time.Now().In(time.UTC), time.UTC).Format("2006-01-02")
	panelRequest := httptest.NewRequest(http.MethodGet, "/calendar/day/"+today+"?mode=edit", nil)
	panelRequest.Header.Set("Accept-Language", "en")
	panelRequest.Header.Set("Cookie", ctx.authCookie)

	panelResponse := mustAppResponse(t, ctx.app, panelRequest)
	assertStatusCode(t, panelResponse, http.StatusOK)
	panelBody := mustReadBodyString(t, panelResponse.Body)
	assertBodyContainsAll(t, panelBody,
		bodyStringMatch{fragment: "Intimacy", message: "expected calendar intimacy section heading to remain visible"},
		bodyStringMatch{fragment: "This section is hidden in settings.", message: "expected calendar intimacy hidden hint"},
	)
	assertBodyNotContainsAll(t, panelBody,
		bodyStringMatch{fragment: `id="calendar-notes"`, message: "did not expect calendar notes field when hidden"},
		bodyStringMatch{fragment: `name="cycle_factor_keys"`, message: "did not expect calendar cycle factor inputs when hidden"},
		bodyStringMatch{fragment: `name="sex_activity"`, message: "did not expect calendar sex activity inputs when hidden"},
	)
}
