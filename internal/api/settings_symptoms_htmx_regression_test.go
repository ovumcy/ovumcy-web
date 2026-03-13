package api

import (
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/gofiber/fiber/v2"
	"github.com/terraincognita07/ovumcy/internal/models"
	"gorm.io/gorm"
)

func TestSettingsSymptomsHTMXCreateArchiveRestoreRerendersSection(t *testing.T) {
	ctx := newSettingsSymptomsHTMXTestContext(t, "settings-symptoms-htmx@example.com")

	createForm := url.Values{
		"csrf_token": {ctx.csrfToken},
		"name":       {"Joint stiffness"},
		"icon":       {"J"},
	}
	renderedCreate := performSettingsSymptomsHTMXRequest(t, ctx, http.MethodPost, "/api/symptoms", createForm)
	assertBodyContainsAll(t, renderedCreate,
		bodyStringMatch{fragment: `data-settings-symptoms`, message: "expected settings symptoms section rerender"},
		bodyStringMatch{fragment: `maxlength="40"`, message: "expected create form to cap symptom name at 40 characters"},
		bodyStringMatch{fragment: `data-symptom-name-count`, message: "expected create form to render symptom name counter"},
		bodyStringMatch{fragment: `toast-close`, message: "expected shared dismissible success status for created symptom"},
	)

	stored := models.SymptomType{}
	if err := ctx.database.Where("user_id = ? AND name = ?", ctx.user.ID, "Joint stiffness").First(&stored).Error; err != nil {
		t.Fatalf("load created custom symptom: %v", err)
	}
	if stored.Color != "#E8799F" {
		t.Fatalf("expected default symptom color, got %q", stored.Color)
	}

	archiveForm := url.Values{"csrf_token": {ctx.csrfToken}}
	renderedArchive := performSettingsSymptomsHTMXRequest(t, ctx, http.MethodPost, "/api/symptoms/"+strconv.FormatUint(uint64(stored.ID), 10)+"/archive", archiveForm)
	assertBodyContainsAll(t, renderedArchive,
		bodyStringMatch{fragment: `data-settings-symptoms`, message: "expected settings symptoms section rerender after archive"},
	)
	archivedState := models.SymptomType{}
	if err := ctx.database.First(&archivedState, stored.ID).Error; err != nil {
		t.Fatalf("reload archived custom symptom: %v", err)
	}
	if archivedState.ArchivedAt == nil {
		t.Fatal("expected archived symptom to have archived_at set")
	}

	restoreForm := url.Values{"csrf_token": {ctx.csrfToken}}
	renderedRestore := performSettingsSymptomsHTMXRequest(t, ctx, http.MethodPost, "/api/symptoms/"+strconv.FormatUint(uint64(stored.ID), 10)+"/restore", restoreForm)
	assertBodyContainsAll(t, renderedRestore,
		bodyStringMatch{fragment: `data-settings-symptoms`, message: "expected settings symptoms section rerender after restore"},
	)
	restoredState := models.SymptomType{}
	if err := ctx.database.First(&restoredState, stored.ID).Error; err != nil {
		t.Fatalf("reload restored custom symptom: %v", err)
	}
	if restoredState.ArchivedAt != nil {
		t.Fatal("expected restored symptom to clear archived_at")
	}
}

func TestSettingsSymptomsHTMXUpdateDuplicateShowsRowLocalError(t *testing.T) {
	app, database := newOnboardingTestAppWithCSRF(t)
	user := createOnboardingTestUser(t, database, "settings-symptoms-htmx-duplicate@example.com", "StrongPass1", true)
	authCookie := loginAndExtractAuthCookieWithCSRF(t, app, user.Email, "StrongPass1")
	csrfCookie, csrfToken := loadSettingsCSRFContext(t, app, authCookie)

	active := models.SymptomType{
		UserID: user.ID,
		Name:   "Joint stiffness",
		Icon:   "✨",
		Color:  "#334455",
	}
	if err := database.Create(&active).Error; err != nil {
		t.Fatalf("create active symptom: %v", err)
	}

	archivedAt := time.Now().UTC()
	archived := models.SymptomType{
		UserID:     user.ID,
		Name:       "Joint support",
		Icon:       "🔥",
		Color:      "#14B8A6",
		ArchivedAt: &archivedAt,
	}
	if err := database.Create(&archived).Error; err != nil {
		t.Fatalf("create archived symptom: %v", err)
	}

	updateForm := url.Values{
		"csrf_token": {csrfToken},
		"name":       {"Joint stiffness"},
		"icon":       {"🔥"},
	}
	updateRequest := httptest.NewRequest(http.MethodPost, "/api/symptoms/"+strconv.FormatUint(uint64(archived.ID), 10), strings.NewReader(updateForm.Encode()))
	updateRequest.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	updateRequest.Header.Set("HX-Request", "true")
	updateRequest.Header.Set("Cookie", joinCookieHeader(authCookie, cookiePair(csrfCookie)))

	updateResponse, err := app.Test(updateRequest, -1)
	if err != nil {
		t.Fatalf("update duplicate symptom htmx request failed: %v", err)
	}
	defer updateResponse.Body.Close()

	if updateResponse.StatusCode != http.StatusOK {
		t.Fatalf("expected htmx update status 200, got %d", updateResponse.StatusCode)
	}
	updateBody, err := io.ReadAll(updateResponse.Body)
	if err != nil {
		t.Fatalf("read htmx update body: %v", err)
	}
	renderedUpdate := string(updateBody)
	assertBodyContainsAll(t, renderedUpdate,
		bodyStringMatch{fragment: `data-symptom-row-error`, message: "expected row-local duplicate-name error container"},
		bodyStringMatch{fragment: "That symptom name already exists in your list.", message: "expected duplicate-name validation message"},
	)
	storedArchived := models.SymptomType{}
	if err := database.First(&storedArchived, archived.ID).Error; err != nil {
		t.Fatalf("reload archived symptom after duplicate update: %v", err)
	}
	if storedArchived.Name != "Joint support" {
		t.Fatalf("expected archived symptom name to remain unchanged, got %q", storedArchived.Name)
	}
}

func TestSettingsSymptomsHTMXCreateTooLongDoesNotPersistSymptom(t *testing.T) {
	app, database := newOnboardingTestAppWithCSRF(t)
	user := createOnboardingTestUser(t, database, "settings-symptoms-htmx-too-long@example.com", "StrongPass1", true)
	authCookie := loginAndExtractAuthCookieWithCSRF(t, app, user.Email, "StrongPass1")
	csrfCookie, csrfToken := loadSettingsCSRFContext(t, app, authCookie)

	createForm := url.Values{
		"csrf_token": {csrfToken},
		"name":       {"12345678901234567890123456789012345678901"},
		"icon":       {"✨"},
	}
	createRequest := httptest.NewRequest(http.MethodPost, "/api/symptoms", strings.NewReader(createForm.Encode()))
	createRequest.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	createRequest.Header.Set("HX-Request", "true")
	createRequest.Header.Set("Cookie", joinCookieHeader(authCookie, cookiePair(csrfCookie)))

	createResponse, err := app.Test(createRequest, -1)
	if err != nil {
		t.Fatalf("create too-long symptom htmx request failed: %v", err)
	}
	defer createResponse.Body.Close()

	if createResponse.StatusCode != http.StatusOK {
		t.Fatalf("expected htmx create status 200, got %d", createResponse.StatusCode)
	}
	createBody, err := io.ReadAll(createResponse.Body)
	if err != nil {
		t.Fatalf("read htmx create body: %v", err)
	}
	renderedCreate := string(createBody)
	assertBodyContainsAll(t, renderedCreate,
		bodyStringMatch{fragment: `data-symptom-create-form`, message: "expected create form rerender after too-long validation"},
		bodyStringMatch{fragment: "Use 40 characters or fewer. For longer details, use notes.", message: "expected too-long create validation message"},
	)
	var count int64
	if err := database.Model(&models.SymptomType{}).Where("user_id = ? AND is_builtin = ?", user.ID, false).Count(&count).Error; err != nil {
		t.Fatalf("count symptoms after too-long create: %v", err)
	}
	if count != 0 {
		t.Fatalf("expected no custom symptoms after too-long create, found %d", count)
	}
}

func TestSettingsSymptomsHTMXUpdateTooLongKeepsStoredSymptomUnchanged(t *testing.T) {
	app, database := newOnboardingTestAppWithCSRF(t)
	user := createOnboardingTestUser(t, database, "settings-symptoms-htmx-update-too-long@example.com", "StrongPass1", true)
	authCookie := loginAndExtractAuthCookieWithCSRF(t, app, user.Email, "StrongPass1")
	csrfCookie, csrfToken := loadSettingsCSRFContext(t, app, authCookie)

	symptom := models.SymptomType{
		UserID: user.ID,
		Name:   "Joint ease",
		Icon:   "💧",
		Color:  "#38BDF8",
	}
	if err := database.Create(&symptom).Error; err != nil {
		t.Fatalf("create custom symptom: %v", err)
	}

	updateForm := url.Values{
		"csrf_token": {csrfToken},
		"name":       {"12345678901234567890123456789012345678901"},
		"icon":       {"🔥"},
	}
	updateRequest := httptest.NewRequest(http.MethodPost, "/api/symptoms/"+strconv.FormatUint(uint64(symptom.ID), 10), strings.NewReader(updateForm.Encode()))
	updateRequest.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	updateRequest.Header.Set("HX-Request", "true")
	updateRequest.Header.Set("Cookie", joinCookieHeader(authCookie, cookiePair(csrfCookie)))

	updateResponse, err := app.Test(updateRequest, -1)
	if err != nil {
		t.Fatalf("update too-long symptom htmx request failed: %v", err)
	}
	defer updateResponse.Body.Close()

	if updateResponse.StatusCode != http.StatusOK {
		t.Fatalf("expected htmx update status 200, got %d", updateResponse.StatusCode)
	}
	updateBody, err := io.ReadAll(updateResponse.Body)
	if err != nil {
		t.Fatalf("read htmx update body: %v", err)
	}
	renderedUpdate := string(updateBody)
	assertBodyContainsAll(t, renderedUpdate,
		bodyStringMatch{fragment: `data-symptom-row-error`, message: "expected row-local too-long update error container"},
		bodyStringMatch{fragment: "Use 40 characters or fewer. For longer details, use notes.", message: "expected too-long update validation message"},
	)
	stored := models.SymptomType{}
	if err := database.First(&stored, symptom.ID).Error; err != nil {
		t.Fatalf("reload symptom after too-long update: %v", err)
	}
	if stored.Name != "Joint ease" {
		t.Fatalf("expected stored symptom name to remain unchanged, got %q", stored.Name)
	}
}

func TestSettingsSymptomsHTMXUpdateWithoutColorPreservesStoredValue(t *testing.T) {
	app, database := newOnboardingTestAppWithCSRF(t)
	user := createOnboardingTestUser(t, database, "settings-symptoms-htmx-preserve-color@example.com", "StrongPass1", true)
	authCookie := loginAndExtractAuthCookieWithCSRF(t, app, user.Email, "StrongPass1")
	csrfCookie, csrfToken := loadSettingsCSRFContext(t, app, authCookie)

	symptom := models.SymptomType{
		UserID: user.ID,
		Name:   "Joint ease",
		Icon:   "💧",
		Color:  "#38BDF8",
	}
	if err := database.Create(&symptom).Error; err != nil {
		t.Fatalf("create custom symptom: %v", err)
	}

	updateForm := url.Values{
		"csrf_token": {csrfToken},
		"name":       {"Joint relief"},
		"icon":       {"🔥"},
	}
	updateRequest := httptest.NewRequest(http.MethodPost, "/api/symptoms/"+strconv.FormatUint(uint64(symptom.ID), 10), strings.NewReader(updateForm.Encode()))
	updateRequest.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	updateRequest.Header.Set("HX-Request", "true")
	updateRequest.Header.Set("Cookie", joinCookieHeader(authCookie, cookiePair(csrfCookie)))

	updateResponse, err := app.Test(updateRequest, -1)
	if err != nil {
		t.Fatalf("update symptom htmx request failed: %v", err)
	}
	defer updateResponse.Body.Close()

	if updateResponse.StatusCode != http.StatusOK {
		t.Fatalf("expected htmx update status 200, got %d", updateResponse.StatusCode)
	}

	stored := models.SymptomType{}
	if err := database.First(&stored, symptom.ID).Error; err != nil {
		t.Fatalf("reload updated custom symptom: %v", err)
	}
	if stored.Name != "Joint relief" {
		t.Fatalf("expected updated name, got %q", stored.Name)
	}
	if stored.Icon != "🔥" {
		t.Fatalf("expected updated icon, got %q", stored.Icon)
	}
	if stored.Color != "#38BDF8" {
		t.Fatalf("expected existing color to be preserved, got %q", stored.Color)
	}
}

type settingsSymptomsHTMXTestContext struct {
	app        *fiber.App
	database   *gorm.DB
	user       models.User
	authCookie string
	csrfCookie *http.Cookie
	csrfToken  string
}

func newSettingsSymptomsHTMXTestContext(t *testing.T, email string) settingsSymptomsHTMXTestContext {
	t.Helper()

	app, database := newOnboardingTestAppWithCSRF(t)
	user := createOnboardingTestUser(t, database, email, "StrongPass1", true)
	authCookie := loginAndExtractAuthCookieWithCSRF(t, app, user.Email, "StrongPass1")
	csrfCookie, csrfToken := loadSettingsCSRFContext(t, app, authCookie)

	return settingsSymptomsHTMXTestContext{
		app:        app,
		database:   database,
		user:       user,
		authCookie: authCookie,
		csrfCookie: csrfCookie,
		csrfToken:  csrfToken,
	}
}

func performSettingsSymptomsHTMXRequest(t *testing.T, ctx settingsSymptomsHTMXTestContext, method string, path string, form url.Values) string {
	t.Helper()

	request := httptest.NewRequest(method, path, strings.NewReader(form.Encode()))
	request.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	request.Header.Set("HX-Request", "true")
	request.Header.Set("Cookie", joinCookieHeader(ctx.authCookie, cookiePair(ctx.csrfCookie)))

	response, err := ctx.app.Test(request, -1)
	if err != nil {
		t.Fatalf("settings symptoms htmx request failed: %v", err)
	}
	defer response.Body.Close()

	if response.StatusCode != http.StatusOK {
		t.Fatalf("expected htmx status 200, got %d", response.StatusCode)
	}

	body, err := io.ReadAll(response.Body)
	if err != nil {
		t.Fatalf("read htmx response body: %v", err)
	}

	return string(body)
}
