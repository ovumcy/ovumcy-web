package api

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
	"regexp"
	"strings"
	"testing"
	"time"

	"github.com/ovumcy/ovumcy-web/internal/models"
	"github.com/ovumcy/ovumcy-web/internal/services"
	"gorm.io/gorm"
)

// TestSettingsFormRequestWithCSRFClosesItsResponseBody pins the helper to the
// same contract mustAppResponse already keeps: the response body is closed by
// the helper that produced it, not by whichever call site remembers. Ninety
// call sites shared that decision and sixteen of them made it, so an unclosed
// body could not be read as an oversight or as intent. The subtest is the
// instrument — its cleanups have run by the time the parent resumes, so the
// close is observable rather than assumed.
func TestSettingsFormRequestWithCSRFClosesItsResponseBody(t *testing.T) {
	ctx := newSettingsSecurityTestContext(t, "settings-form-helper-body-close@example.com")

	var response *http.Response
	t.Run("inside a scope whose cleanups run", func(t *testing.T) {
		response = settingsFormRequestWithCSRF(t, ctx, http.MethodPatch, "/api/v1/users/current/tracking", url.Values{
			"temperature_unit": {"c"},
		}, map[string]string{"HX-Request": "true"})
		assertStatusCode(t, response, http.StatusOK)
	})

	if _, err := response.Body.Read(make([]byte, 1)); err == nil {
		t.Fatal("settingsFormRequestWithCSRF left its response body open; it must register the same t.Cleanup close mustAppResponse does")
	}
}

func TestTrackingSettingsExposeBBTAndCervicalMucusOnDashboard(t *testing.T) {
	ctx := newSettingsSecurityTestContext(t, "settings-tracking-dashboard@example.com")

	response := settingsFormRequestWithCSRF(t, ctx, http.MethodPatch, "/api/v1/users/current/tracking", url.Values{
		"track_bbt":            {"true"},
		"track_cervical_mucus": {"true"},
		"show_sex_chip":        {"true"},
		"show_cycle_factors":   {"true"},
		"show_notes_field":     {"true"},
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

// TestTrackingSettingsJSONBodyKeepsThePublishedInvertedKeys pins the half of
// the tracking endpoint the positive settings form does not cover: the v1 JSON
// body still speaks hide_sex_chip / hide_cycle_factors / hide_notes_field, both
// on the way in and in the echo, because renaming a v1 field is breaking. The
// conversion into the positive view model happens on the service side, so the
// stored columns must come back exactly as the caller sent them.
func TestTrackingSettingsJSONBodyKeepsThePublishedInvertedKeys(t *testing.T) {
	ctx := newSettingsSecurityTestContext(t, "settings-tracking-json@example.com")

	body := `{"track_bbt":true,"temperature_unit":"c","track_cervical_mucus":false,` +
		`"hide_sex_chip":true,"hide_cycle_factors":false,"hide_notes_field":true,` +
		`"show_historical_phases":false,"week_starts_on":"monday"}`
	request := httptest.NewRequest(http.MethodPatch, "/api/v1/users/current/tracking", strings.NewReader(body))
	request.Header.Set("Content-Type", "application/json")
	request.Header.Set("Accept", "application/json")
	request.Header.Set("X-CSRF-Token", ctx.csrfToken)
	request.Header.Set("Cookie", settingsCookieHeader(ctx.authCookie, ctx.csrfCookie))

	response := mustAppResponse(t, ctx.app, request)
	assertStatusCode(t, response, http.StatusOK)

	var echoed map[string]any
	if err := json.NewDecoder(response.Body).Decode(&echoed); err != nil {
		t.Fatalf("decode tracking response: %v", err)
	}
	for key, expected := range map[string]bool{
		"hide_sex_chip":      true,
		"hide_cycle_factors": false,
		"hide_notes_field":   true,
	} {
		if echoed[key] != expected {
			t.Errorf("expected the v1 response to echo %s=%t, got %#v", key, expected, echoed[key])
		}
	}

	var persisted struct {
		HideSexChip      bool
		HideCycleFactors bool
		HideNotesField   bool
	}
	if err := ctx.database.Model(&models.User{}).
		Select("hide_sex_chip", "hide_cycle_factors", "hide_notes_field").
		Where("id = ?", ctx.user.ID).
		First(&persisted).Error; err != nil {
		t.Fatalf("load persisted tracking columns: %v", err)
	}
	if !persisted.HideSexChip || persisted.HideCycleFactors || !persisted.HideNotesField {
		t.Fatalf("a v1 JSON save must store its hide_* fields verbatim, got %+v", persisted)
	}
}

// recoveryRotationWritableColumns are the users columns a recovery-code
// regeneration is allowed to move: the fresh code and its one-time reveal
// mark, the feed capability it disarms, the session version it bumps, and the
// row's own timestamp. Every other column is settings state the rotation has
// no business touching, and the sweep below holds all of them at once instead
// of naming two sentinels.
var recoveryRotationWritableColumns = map[string]bool{
	"recovery_code_hash":          true,
	"recovery_code_revealed_at":   true,
	"calendar_feed_selector":      true,
	"calendar_feed_verifier_hash": true,
	"calendar_feed_verifier_mac":  true,
	"auth_session_version":        true,
	"updated_at":                  true,
}

// userRowSnapshot reads the owner's whole users row as columns, straight from
// the live schema rather than through a struct that would re-list the fields
// the assertion is supposed to discover. A column added by a later migration
// is covered the day it exists.
func userRowSnapshot(t *testing.T, database *gorm.DB, userID uint) map[string]string {
	t.Helper()

	row := map[string]any{}
	if err := database.Table("users").Where("id = ?", userID).Take(&row).Error; err != nil {
		t.Fatalf("snapshot users row: %v", err)
	}
	snapshot := make(map[string]string, len(row))
	for column, value := range row {
		snapshot[column] = fmt.Sprintf("%v", value)
	}
	return snapshot
}

func TestSettingsPageKeepsPersistedCycleValuesAfterRecoveryCodeRegeneration(t *testing.T) {
	ctx := newSettingsSecurityTestContext(t, "settings-recovery-return@example.com")

	before := userRowSnapshot(t, ctx.database, ctx.user.ID)
	if len(before) < 20 {
		t.Fatalf("the users snapshot resolved %d columns; the table is far wider, so this sweep read the wrong thing", len(before))
	}

	response := settingsFormRequestWithCSRF(t, ctx, http.MethodPost, "/api/v1/users/current/recovery-code", url.Values{
		"password": {"StrongPass1"},
	}, nil)
	assertStatusCode(t, response, http.StatusSeeOther)

	// The whole-row comparison. Two sentinels (period_length,
	// unpredictable_cycle) were presented as whole-settings isolation: a
	// scratch `cycle_length` added to the rotation's UPDATE left this test
	// green. Every column outside the allowlist above must come back
	// byte-identical, so a column the rotation starts writing fails here by
	// name.
	after := userRowSnapshot(t, ctx.database, ctx.user.ID)
	if len(after) != len(before) {
		t.Fatalf("the users row went from %d columns to %d across a recovery-code regeneration", len(before), len(after))
	}
	for column, wasValue := range before {
		if recoveryRotationWritableColumns[column] {
			continue
		}
		if after[column] != wasValue {
			t.Errorf("recovery-code regeneration changed users.%s from %q to %q; it may only write %d columns, and that is not one of them",
				column, wasValue, after[column], len(recoveryRotationWritableColumns))
		}
	}
	// The positive anchor: a comparison that saw a stale read would report no
	// drift for the same reason it reports no rotation.
	if after["auth_session_version"] == before["auth_session_version"] {
		t.Fatalf("auth_session_version stayed at %q; the snapshot did not observe the rotation at all", before["auth_session_version"])
	}

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

	// The three section toggles post positively: a checked box means "show".
	// Save them on first, so the hidden state asserted below is a state this
	// owner actually left rather than the value a request that mentions none of
	// them would have produced anyway.
	shownResponse := settingsFormRequestWithCSRF(t, ctx, http.MethodPatch, "/api/v1/users/current/tracking", url.Values{
		"show_sex_chip":      {"true"},
		"show_cycle_factors": {"true"},
		"show_notes_field":   {"true"},
		"temperature_unit":   {"c"},
	}, map[string]string{
		"HX-Request": "true",
	})
	assertStatusCode(t, shownResponse, http.StatusOK)

	shownDashboardRequest := httptest.NewRequest(http.MethodGet, "/dashboard", nil)
	shownDashboardRequest.Header.Set("Accept-Language", "en")
	shownDashboardRequest.Header.Set("Cookie", ctx.authCookie)
	shownDashboardResponse := mustAppResponse(t, ctx.app, shownDashboardRequest)
	assertStatusCode(t, shownDashboardResponse, http.StatusOK)
	assertBodyContainsAll(t, mustReadBodyString(t, shownDashboardResponse.Body),
		bodyStringMatch{fragment: `id="today-notes"`, message: "expected dashboard notes field while shown"},
		bodyStringMatch{fragment: `name="cycle_factor_keys"`, message: "expected dashboard cycle factor inputs while shown"},
		bodyStringMatch{fragment: `name="sex_activity"`, message: "expected dashboard sex activity inputs while shown"},
	)

	// Unchecking a box posts nothing at all — that omission is what has to read
	// as "hidden" now that the toggles are phrased positively.
	response := settingsFormRequestWithCSRF(t, ctx, http.MethodPatch, "/api/v1/users/current/tracking", url.Values{
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
