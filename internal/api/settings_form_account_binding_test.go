package api

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
	"reflect"
	"regexp"
	"runtime"
	"sort"
	"strings"
	"testing"
	"time"

	"github.com/gofiber/fiber/v3"
	"github.com/ovumcy/ovumcy-web/internal/models"
	"github.com/ovumcy/ovumcy-web/internal/services"
	"golang.org/x/net/html"
	"gorm.io/gorm"
)

// settingsAccountChangedKey is the stable key a settings write refused for
// another account renders in markup; settingsAccountChangedError is the key
// its JSON envelope carries. Both are spelled out so a 409 from any other
// cause — the day forms' own refusal included — cannot satisfy these tests.
const (
	settingsAccountChangedKey   = "settings.error.account_changed"
	settingsAccountChangedError = "settings form account changed"
)

// Routes the cases below name outright; every other case names its route in
// the table where it is written.
const (
	cycleSettingsRouteName = "PATCH /api/v1/users/current/cycle"
	importJSONRouteName    = "POST /api/v1/imports/json"
)

func assertSettingsAccountChangedRefusal(t *testing.T, label string, response *http.Response) {
	t.Helper()
	body := mustReadBodyString(t, response.Body)
	if response.StatusCode != http.StatusConflict {
		t.Fatalf("%s: must be refused 409, got %d %q", label, response.StatusCode, body)
	}
	if !strings.Contains(body, settingsAccountChangedKey) && !strings.Contains(body, settingsAccountChangedError) {
		t.Fatalf("%s: the 409 must carry the %s reason, got %q", label, settingsAccountChangedKey, body)
	}
	if strings.Contains(body, accountChangedKey) {
		t.Fatalf("%s: a refused settings write must not claim a day entry was not saved (%s), got %q", label, accountChangedKey, body)
	}
}

// settingsWriteCase is one way a rendered page writes through a bound
// settings route: the request it sends and the state of the account it lands
// on that the write changes.
type settingsWriteCase struct {
	// route is the route as the router registers it, "METHOD /path/:param".
	route string
	shape string
	// prepare puts the signed-in account in the state the write acts on; nil
	// when the table order already has.
	prepare func(t *testing.T, database *gorm.DB, userID uint)
	// request builds the write. binding "" sends none; page makes it the
	// request a rendered page sends (HTMX), otherwise a JSON API client's.
	request func(t *testing.T, cookie, binding string, page bool, seq int) *http.Request
	state   func(t *testing.T, database *gorm.DB, userID uint) string
}

// accountFingerprint is everything the settings and onboarding writes can
// change on an account: its whole users row and its symptom rows. A refused
// write leaves it as it was, and a write that reached its handler changes it.
func accountFingerprint(t *testing.T, database *gorm.DB, userID uint) string {
	t.Helper()
	var user map[string]any
	if err := database.Table("users").Where("id = ?", userID).Take(&user).Error; err != nil {
		t.Fatalf("load user %d: %v", userID, err)
	}
	var symptoms []map[string]any
	if err := database.Table("symptom_types").Where("user_id = ?", userID).Order("id").Find(&symptoms).Error; err != nil {
		t.Fatalf("load symptoms of user %d: %v", userID, err)
	}
	return fmt.Sprint(user, symptoms)
}

// boundFormRequest is a form write as a rendered page sends it (HTMX, the
// binding in the form body) or as a JSON API client does (the binding, when
// it sends one, in the header). binding "" sends none.
func boundFormRequest(method, path string, form url.Values, cookie, binding string, page bool) *http.Request {
	if page && binding != "" {
		form.Set(dayFormAccountWireName, binding)
	}
	request := httptest.NewRequest(method, path, strings.NewReader(form.Encode()))
	request.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	request.Header.Set("Cookie", cookie)
	if page {
		request.Header.Set("HX-Request", "true")
		return request
	}
	request.Header.Set("Accept", "application/json")
	if binding != "" {
		request.Header.Set(dayFormAccountWireHeader, binding)
	}
	return request
}

func formCase(route, shape string, form func(seq int) url.Values) settingsWriteCase {
	method, pattern, _ := strings.Cut(route, " ")
	return settingsWriteCase{
		route: route,
		shape: shape,
		request: func(t *testing.T, cookie, binding string, page bool, seq int) *http.Request {
			return boundFormRequest(method, pattern, form(seq), cookie, binding, page)
		},
		state: func(t *testing.T, database *gorm.DB, userID uint) string {
			return accountFingerprint(t, database, userID)
		},
	}
}

// customSymptomIDOf resolves the account's first custom symptom, archived or
// active, at the moment the request is built: the table order creates, edits,
// archives and restores one in turn.
func customSymptomIDOf(t *testing.T, database *gorm.DB, userID uint, archived bool) uint {
	t.Helper()
	query := database.Model(&models.SymptomType{}).Where("user_id = ? AND is_builtin = ?", userID, false)
	if archived {
		query = query.Where("archived_at IS NOT NULL")
	} else {
		query = query.Where("archived_at IS NULL")
	}
	var symptom models.SymptomType
	if err := query.Order("id").First(&symptom).Error; err != nil {
		t.Fatalf("find a custom symptom of user %d (archived=%t): %v", userID, archived, err)
	}
	return symptom.ID
}

func symptomCase(database *gorm.DB, userID uint, route, shape string, archived bool, form func(seq int) url.Values) settingsWriteCase {
	method, pattern, _ := strings.Cut(route, " ")
	write := formCase(route, shape, form)
	write.request = func(t *testing.T, cookie, binding string, page bool, seq int) *http.Request {
		path := strings.ReplaceAll(pattern, ":id", fmt.Sprint(customSymptomIDOf(t, database, userID, archived)))
		return boundFormRequest(method, path, form(seq), cookie, binding, page)
	}
	return write
}

func settingsWriteCases(fixture dayFormAccountFixture) []settingsWriteCase {
	user := fixture.second
	database := fixture.database
	alternativeGoal := services.AlternativeUsageGoals(user.UsageGoal)[0]
	cycleState := func(t *testing.T, database *gorm.DB, userID uint) string {
		t.Helper()
		stored := models.User{}
		if err := database.Select("cycle_length", "period_length", "usage_goal").First(&stored, userID).Error; err != nil {
			t.Fatalf("load cycle settings of user %d: %v", userID, err)
		}
		return fmt.Sprintf("cycle_length=%d period_length=%d usage_goal=%s", stored.CycleLength, stored.PeriodLength, stored.UsageGoal)
	}
	return []settingsWriteCase{
		{
			route: cycleSettingsRouteName,
			shape: "the settings cycle form",
			request: func(t *testing.T, cookie, binding string, page bool, seq int) *http.Request {
				cycleLength := fmt.Sprintf("%d", 30+seq)
				if !page {
					request := httptest.NewRequest(http.MethodPatch, "/api/v1/users/current/cycle", strings.NewReader(`{"cycle_length":`+cycleLength+`,"period_length":6}`))
					request.Header.Set("Content-Type", "application/json")
					request.Header.Set("Accept", "application/json")
					return withSettingsWriteSession(request, cookie, binding)
				}
				form := url.Values{"cycle_length": {cycleLength}, "period_length": {"6"}}
				if binding != "" {
					form.Set(dayFormAccountWireName, binding)
				}
				request := httptest.NewRequest(http.MethodPatch, "/api/v1/users/current/cycle", strings.NewReader(form.Encode()))
				request.Header.Set("Content-Type", "application/x-www-form-urlencoded")
				request.Header.Set("HX-Request", "true")
				return withSettingsWriteSession(request, cookie, "")
			},
			state: cycleState,
		},
		{
			route: cycleSettingsRouteName,
			shape: "the dashboard usage-goal switch",
			request: func(t *testing.T, cookie, binding string, page bool, _ int) *http.Request {
				if !page {
					request := httptest.NewRequest(http.MethodPatch, "/api/v1/users/current/cycle", strings.NewReader(`{"usage_goal":"`+alternativeGoal+`"}`))
					request.Header.Set("Content-Type", "application/json")
					request.Header.Set("Accept", "application/json")
					return withSettingsWriteSession(request, cookie, binding)
				}
				form := url.Values{"_method": {"PATCH"}, "usage_goal": {alternativeGoal}}
				if binding != "" {
					form.Set(dayFormAccountWireName, binding)
				}
				request := httptest.NewRequest(http.MethodPatch, "/api/v1/users/current/cycle?source=dashboard", strings.NewReader(form.Encode()))
				request.Header.Set("Content-Type", "application/x-www-form-urlencoded")
				request.Header.Set("HX-Request", "true")
				return withSettingsWriteSession(request, cookie, "")
			},
			state: cycleState,
		},
		{
			route: importJSONRouteName,
			shape: "the settings restore",
			request: func(t *testing.T, cookie, binding string, page bool, seq int) *http.Request {
				body := fmt.Sprintf(`{"entries":[{"date":"2026-03-%02d","period":true,"flow":"medium","cycle_factors":[]}]}`, 1+seq)
				request := httptest.NewRequest(http.MethodPost, "/api/v1/imports/json", strings.NewReader(body))
				request.Header.Set("Content-Type", "application/json")
				if page {
					request.Header.Set("HX-Request", "true")
				} else {
					request.Header.Set("Accept", "application/json")
				}
				return withSettingsWriteSession(request, cookie, binding)
			},
			state: func(t *testing.T, database *gorm.DB, userID uint) string {
				t.Helper()
				var rows int64
				if err := database.Model(&models.DailyLog{}).Where("user_id = ?", userID).Count(&rows).Error; err != nil {
					t.Fatalf("count days of user %d: %v", userID, err)
				}
				return fmt.Sprintf("days=%d", rows)
			},
		},
		formCase("PATCH /api/v1/users/current/profile", "the profile form", func(seq int) url.Values {
			return url.Values{"display_name": {fmt.Sprintf("Renamed %d", seq)}}
		}),
		formCase("PATCH /api/v1/users/current/interface", "the interface form", func(int) url.Values {
			return url.Values{"language": {"fr"}, "theme": {"dark"}}
		}),
		formCase("PATCH /api/v1/users/current/tracking", "the tracking form", func(int) url.Values {
			return url.Values{"track_bbt": {"true"}, "temperature_unit": {"f"}, "week_starts_on": {"sunday"}}
		}),
		formCase("PATCH /api/v1/users/current/reminders", "the reminder window form", func(int) url.Values {
			return url.Values{"reminder_lead_days": {"5"}}
		}),
		formCase("POST /api/v1/users/current/timezone", "the timezone sync", func(int) url.Values {
			return url.Values{"timezone": {"Pacific/Auckland"}}
		}),
		// The webhook is configured before it is removed, and the feed generated
		// before it is rotated and revoked: the table order is the sequence.
		formCase("POST /api/v1/users/current/webhook", "the webhook form", func(int) url.Values {
			return url.Values{"webhook_url": {"https://ntfy.example.test/stale-tab"}, "webhook_enabled": {"true"}, "webhook_notify_period": {"true"}}
		}),
		formCase("DELETE /api/v1/users/current/webhook", "the webhook removal", func(int) url.Values {
			return url.Values{}
		}),
		formCase("POST /api/v1/users/current/calendar-feed", "the calendar feed generation", func(int) url.Values {
			return url.Values{}
		}),
		formCase("POST /api/v1/users/current/calendar-feed/rotate", "the calendar feed rotation", func(int) url.Values {
			return url.Values{}
		}),
		formCase("DELETE /api/v1/users/current/calendar-feed", "the calendar feed revocation", func(int) url.Values {
			return url.Values{}
		}),
		formCase("POST /api/v1/symptoms", "the new symptom form", func(seq int) url.Values {
			return url.Values{"name": {fmt.Sprintf("Stale tab %d", seq)}, "icon": {"✨"}}
		}),
		symptomCase(database, user.ID, "PATCH /api/v1/symptoms/:id", "the symptom edit form", false, func(seq int) url.Values {
			return url.Values{"name": {fmt.Sprintf("Edited %d", seq)}, "icon": {"✨"}}
		}),
		symptomCase(database, user.ID, "DELETE /api/v1/symptoms/:id", "the symptom hide control", false, func(int) url.Values {
			return url.Values{}
		}),
		symptomCase(database, user.ID, "POST /api/v1/symptoms/:id/restore", "the symptom restore control", true, func(int) url.Values {
			return url.Values{}
		}),
	}
}

// onboardingWriteCases are the three onboarding writes, run against an
// account that still has onboarding to do. Each case resets the account first:
// the second step completes onboarding, which the first and the explicit
// completion both need undone.
func onboardingWriteCases(fixture dayFormAccountFixture) []settingsWriteCase {
	reset := func(withFirstStep bool) func(t *testing.T, database *gorm.DB, userID uint) {
		return func(t *testing.T, database *gorm.DB, userID uint) {
			t.Helper()
			updates := map[string]any{"onboarding_completed": false, "cycle_length": 28, "period_length": 5, "last_period_start": nil}
			if withFirstStep {
				updates["last_period_start"] = time.Now().UTC().AddDate(0, 0, -20).Truncate(24 * time.Hour)
			}
			if err := database.Model(&models.User{}).Where("id = ?", userID).Updates(updates).Error; err != nil {
				t.Fatalf("reset onboarding of user %d: %v", userID, err)
			}
		}
	}
	day := time.Now().UTC().AddDate(0, 0, -10).Format("2006-01-02")
	step1 := formCase("POST /api/v1/onboarding/steps/1", "the first onboarding step", func(int) url.Values {
		return url.Values{"last_period_start": {day}}
	})
	step1.prepare = reset(false)
	step2 := formCase("POST /api/v1/onboarding/steps/2", "the second onboarding step", func(int) url.Values {
		return url.Values{"cycle_length": {"31"}, "period_length": {"6"}}
	})
	step2.prepare = reset(true)
	complete := formCase("POST /api/v1/onboarding/complete", "the explicit completion", func(int) url.Values {
		return url.Values{}
	})
	complete.prepare = reset(true)
	return []settingsWriteCase{step1, step2, complete}
}

// withSettingsWriteSession sets the session cookie and, when binding is not
// empty, the binding header.
func withSettingsWriteSession(request *http.Request, cookie, binding string) *http.Request {
	request.Header.Set("Cookie", cookie)
	if binding != "" {
		request.Header.Set(dayFormAccountWireHeader, binding)
	}
	return request
}

// registeredRoutes lists the router's routes by method and path.
func registeredRoutes(app *fiber.App) map[string]bool {
	registered := map[string]bool{}
	for _, route := range app.GetRoutes(true) {
		registered[route.Method+" "+route.Path] = true
	}
	return registered
}

// onboardingAccountFixture is dayFormAccountFixture for two accounts that
// still have onboarding to do. The bindings are read off the onboarding page,
// the only page such an account is served.
func newOnboardingAccountFixture(t *testing.T, prefix string) dayFormAccountFixture {
	t.Helper()
	app, database := newOnboardingTestAppWithOptions(t, onboardingTestAppOptions{})
	first := createOnboardingTestUser(t, database, prefix+"-first@example.com", "StrongPass1", false)
	second := createOnboardingTestUser(t, database, prefix+"-second@example.com", "StrongPass1", false)
	fixture := dayFormAccountFixture{app: app, database: database, first: first, second: second}
	fixture.firstCookie = loginAndExtractAuthCookie(t, app, first.Email, "StrongPass1")
	fixture.secondCookie = loginAndExtractAuthCookie(t, app, second.Email, "StrongPass1")
	fixture.firstBinding = renderedDayFormAccount(t, app, fixture.firstCookie, "/onboarding")
	fixture.secondBinding = renderedDayFormAccount(t, app, fixture.secondCookie, "/onboarding")
	return fixture
}

// refuseAPageFromAnotherAccount drives the stale-tab hazard on each case: a
// page rendered for the first account, used after the second signed in in
// another tab, must leave the second account untouched; a page that sends no
// binding at all is refused the same way. Positive control: the same request
// under the second account's own binding reaches the handler and changes it.
func refuseAPageFromAnotherAccount(t *testing.T, fixture dayFormAccountFixture, cases []settingsWriteCase) {
	t.Helper()
	registered := registeredRoutes(fixture.app)

	for index, write := range cases {
		if !registered[write.route] {
			t.Fatalf("%s: %s is not a registered route", write.shape, write.route)
		}
		label := write.route + " (" + write.shape + ")"
		if write.prepare != nil {
			write.prepare(t, fixture.database, fixture.second.ID)
		}
		before := write.state(t, fixture.database, fixture.second.ID)

		response := mustAppResponse(t, fixture.app, write.request(t, fixture.secondCookie, fixture.firstBinding, true, 2*index))
		assertSettingsAccountChangedRefusal(t, label+" with a binding of another account", response)
		if after := write.state(t, fixture.database, fixture.second.ID); after != before {
			t.Fatalf("%s: a page of another account changed the signed-in account: %s -> %s", label, before, after)
		}

		response = mustAppResponse(t, fixture.app, write.request(t, fixture.secondCookie, "", true, 2*index))
		assertSettingsAccountChangedRefusal(t, label+" with no binding", response)
		if after := write.state(t, fixture.database, fixture.second.ID); after != before {
			t.Fatalf("%s: a page without a binding changed the signed-in account: %s -> %s", label, before, after)
		}

		// Any refusal before the handler — this guard, the owner guard —
		// answers 4xx and leaves the state as it was, so both are asserted.
		response = mustAppResponse(t, fixture.app, write.request(t, fixture.secondCookie, fixture.secondBinding, true, 2*index+1))
		if body := mustReadBodyString(t, response.Body); response.StatusCode >= 400 {
			t.Fatalf("%s: the account's own binding must reach the handler and succeed, got %d %q", label, response.StatusCode, body)
		}
		if after := write.state(t, fixture.database, fixture.second.ID); after == before {
			t.Fatalf("%s: the account's own binding must reach the handler, but the state is unchanged (%s)", label, after)
		}
	}
}

// TestEverySettingsWriteRouteRefusesAPageFromAnotherAccount drives the
// stale-tab hazard on the settings writes a page hosts: the cycle settings
// form, the dashboard's usage-goal switch, the restore, and every other
// settings write the route table guards (the next test derives that set from
// the router, so a write added to it without a case here reddens).
func TestEverySettingsWriteRouteRefusesAPageFromAnotherAccount(t *testing.T) {
	t.Parallel()

	fixture := newDayFormAccountFixture(t, "settings-write-routes")
	refuseAPageFromAnotherAccount(t, fixture, settingsWriteCases(fixture))
}

// TestEveryOnboardingWriteRouteRefusesAPageFromAnotherAccount is the same
// hazard on the onboarding writes: an onboarding page left open in a tab while
// a different account that still has onboarding to do signs in elsewhere.
func TestEveryOnboardingWriteRouteRefusesAPageFromAnotherAccount(t *testing.T) {
	t.Parallel()

	fixture := newOnboardingAccountFixture(t, "onboarding-write-routes")
	refuseAPageFromAnotherAccount(t, fixture, onboardingWriteCases(fixture))
}

// letOnlyAClientWithoutAPageOmitTheBinding pins the other half of the rule: a
// JSON API client renders no page and may omit the binding, and its write
// lands; a binding it does send is still verified.
func letOnlyAClientWithoutAPageOmitTheBinding(t *testing.T, fixture dayFormAccountFixture, cases []settingsWriteCase) {
	t.Helper()
	for index, write := range cases {
		label := write.route + " (" + write.shape + ", JSON client)"
		if write.prepare != nil {
			write.prepare(t, fixture.database, fixture.second.ID)
		}
		before := write.state(t, fixture.database, fixture.second.ID)

		response := mustAppResponse(t, fixture.app, write.request(t, fixture.secondCookie, fixture.firstBinding, false, 2*index))
		assertSettingsAccountChangedRefusal(t, label+" with a binding of another account", response)
		if after := write.state(t, fixture.database, fixture.second.ID); after != before {
			t.Fatalf("%s: a binding of another account changed the signed-in account: %s -> %s", label, before, after)
		}

		// The restore script sends the header even when its page carries no
		// binding; an empty value is a binding that names no account.
		emptyHeader := write.request(t, fixture.secondCookie, "", false, 2*index)
		emptyHeader.Header.Set(dayFormAccountWireHeader, "")
		response = mustAppResponse(t, fixture.app, emptyHeader)
		assertSettingsAccountChangedRefusal(t, label+" with an empty binding header", response)
		if after := write.state(t, fixture.database, fixture.second.ID); after != before {
			t.Fatalf("%s: an empty binding header changed the signed-in account: %s -> %s", label, before, after)
		}

		response = mustAppResponse(t, fixture.app, write.request(t, fixture.secondCookie, "", false, 2*index+1))
		if body := mustReadBodyString(t, response.Body); response.StatusCode >= 300 {
			t.Fatalf("%s: a JSON client without the binding must keep its behaviour, got %d %q", label, response.StatusCode, body)
		}
		if after := write.state(t, fixture.database, fixture.second.ID); after == before {
			t.Fatalf("%s: a JSON client without the binding must reach the handler, but the state is unchanged (%s)", label, after)
		}
	}
}

func TestSettingsWriteRoutesLetOnlyAClientWithoutAPageOmitTheBinding(t *testing.T) {
	t.Parallel()

	fixture := newDayFormAccountFixture(t, "settings-write-json")
	letOnlyAClientWithoutAPageOmitTheBinding(t, fixture, settingsWriteCases(fixture))
}

func TestOnboardingWriteRoutesLetOnlyAClientWithoutAPageOmitTheBinding(t *testing.T) {
	t.Parallel()

	fixture := newOnboardingAccountFixture(t, "onboarding-write-json")
	letOnlyAClientWithoutAPageOmitTheBinding(t, fixture, onboardingWriteCases(fixture))
}

// unguardedMutationReasons names every state-changing /api/v1 route behind a
// session that does NOT mount the account binding, with the reason it does not
// need it. Each one either demands a credential the request itself carries
// (the account's current password, or a code bound to the account's own sealed
// setup cookie), so a page of another account cannot send it for the account
// now signed in, or it changes no setting and no data.
var unguardedMutationReasons = map[string]string{
	"DELETE /api/v1/users/current":                     "erases the account; demands the current password",
	"PUT /api/v1/users/current/password":               "changes the credential; demands the current password",
	"POST /api/v1/users/current/password/step-up":      "starts a provider re-authentication; stores nothing on the account",
	"POST /api/v1/users/current/recovery-code":         "regenerates the recovery code; demands the current password",
	"PUT /api/v1/users/current/2fa":                    "verifies a code against the owner-bound sealed enrollment cookie",
	"DELETE /api/v1/users/current/2fa":                 "disables two-factor; demands the current password",
	"POST /api/v1/users/current/data-wipe/validate":    "checks the password; writes nothing",
	"POST /api/v1/users/current/data-wipe":             "erases the data; demands the current password",
	"POST /api/v1/users/current/oidc/link/step-up":     "starts a provider re-authentication; stores nothing on the account",
	"POST /api/v1/users/current/oidc/link/confirm":     "links only the identity in the owner-bound sealed pending-link cookie",
	"DELETE /api/v1/users/current/oidc/identities/:id": "unlinks an identity; demands the current password",
	"DELETE /api/v1/sessions/current":                  "signs out; changes no setting and no data",
}

// boundRoutesDrivenElsewhere names the bound routes whose cross-account case
// cannot use settingsWriteCase, with the test that drives it instead. The
// erasure step-up starts store nothing on the account — their effect is the
// sealed step-up cookie — and serve only an account without a local password
// on an instance with OIDC, which the shared fixture is not.
var boundRoutesDrivenElsewhere = map[string]string{
	"POST /api/v1/users/current/data-wipe/step-up": "TestErasureStepupStartRefusesAPageFromAnotherAccount",
	"POST /api/v1/users/current/deletion/step-up":  "TestErasureStepupStartRefusesAPageFromAnotherAccount",
}

// accountBindingGuardMarker reads the shared guard body's own name back off a
// closure it produced, the way the first-party sweep does: the factory is
// inlined into each registration, so only the name survives, not the pointer.
func accountBindingGuardMarker() string {
	name := runtime.FuncForPC(reflect.ValueOf((&Handler{}).refuseFormFromAnotherAccount(APIErrorSpec{}, nil)).Pointer()).Name()
	if index := strings.LastIndex(name, ".func"); index >= 0 {
		name = name[:index]
	}
	if index := strings.LastIndex(name, "."); index >= 0 {
		name = name[index+1:]
	}
	return name + ".func"
}

// guardedSettingsRoutes reads the account-bound routes outside /api/v1/days off
// the router: the chain of each carries the shared guard body.
func guardedSettingsRoutes(t *testing.T, app *fiber.App) []string {
	t.Helper()
	marker := accountBindingGuardMarker()
	var guarded []string
	for _, route := range app.GetRoutes(true) {
		if route.Method == fiber.MethodHead || strings.HasPrefix(route.Path, "/api/v1/days") {
			continue
		}
		for _, routeHandler := range route.Handlers {
			if strings.Contains(runtime.FuncForPC(reflect.ValueOf(routeHandler).Pointer()).Name(), marker) {
				guarded = append(guarded, route.Method+" "+route.Path)
				break
			}
		}
	}
	sort.Strings(guarded)
	return guarded
}

// TestEveryStateChangingV1RouteIsAccountBoundOrNamedWithItsReason sweeps the
// router, with no list of the bound routes: every state-changing /api/v1 route
// behind a session either mounts the account binding — and then a case above
// drives it — or is named in unguardedMutationReasons. A route added without
// either reddens here, and so does a reason left behind for a route that is
// bound or gone.
func TestEveryStateChangingV1RouteIsAccountBoundOrNamedWithItsReason(t *testing.T) {
	t.Parallel()

	fixture := newDayFormAccountFixture(t, "settings-route-sweep")
	guarded := guardedSettingsRoutes(t, fixture.app)
	if !routeListHas(guarded, cycleSettingsRouteName) || !routeListHas(guarded, "POST /api/v1/users/current/timezone") {
		t.Fatalf("the guard detection lost the load-bearing routes it must see, found %v", guarded)
	}

	cased := map[string]bool{}
	for _, write := range append(settingsWriteCases(fixture), onboardingWriteCases(fixture)...) {
		cased[write.route] = true
	}
	for route := range boundRoutesDrivenElsewhere {
		cased[route] = true
	}
	var bound []string
	for route := range cased {
		bound = append(bound, route)
	}
	sort.Strings(bound)
	if !reflect.DeepEqual(guarded, bound) {
		t.Fatalf("the bound routes and the routes with a cross-account case differ:\n  router: %v\n  cases:  %v", guarded, bound)
	}

	preSession := map[string]bool{}
	for _, route := range preSessionV1Mutations {
		preSession[route] = true
	}
	seen := map[string]bool{}
	for _, route := range fixture.app.GetRoutes(true) {
		key := route.Method + " " + route.Path
		switch route.Method {
		case fiber.MethodGet, fiber.MethodHead, fiber.MethodOptions, fiber.MethodConnect, fiber.MethodTrace:
			continue
		}
		if !strings.HasPrefix(route.Path, "/api/v1/") || preSession[key] || seen[key] || strings.HasPrefix(route.Path, "/api/v1/days") {
			continue
		}
		seen[key] = true
		reason, named := unguardedMutationReasons[key]
		switch {
		case cased[key] && named:
			t.Errorf("%s is account-bound and still named as exempt (%q)", key, reason)
		case !cased[key] && !named:
			t.Errorf("%s changes state behind a session but mounts neither the account binding nor a named reason", key)
		}
	}
	for key := range unguardedMutationReasons {
		if !seen[key] {
			t.Errorf("%s is named as exempt but the router no longer registers it", key)
		}
	}
}

func routeListHas(values []string, want string) bool {
	for _, value := range values {
		if value == want {
			return true
		}
	}
	return false
}

// settingsPathParameter turns a registered path pattern into the expression a
// rendered address must match.
var settingsPathParameter = regexp.MustCompile(`:[A-Za-z]+`)

func routePatternExpression(path string) *regexp.Regexp {
	return regexp.MustCompile("^" + settingsPathParameter.ReplaceAllString(regexp.QuoteMeta(path), "[^/]+") + "$")
}

// settingsWriteVerbs are the attributes that name a write's verb on an element;
// an element with only an action posts.
var settingsWriteVerbs = map[string]string{"hx-post": "POST", "hx-put": "PUT", "hx-patch": "PATCH", "hx-delete": "DELETE"}

// TestEveryRenderedSettingsWriteCarriesTheAccountBinding walks the markup of
// every page and fragment that hosts a bound settings or onboarding write and
// requires each element addressing a bound route — as the router reports them
// — to carry the rendering account's binding in its form, and the restore
// section, whose script sends the export through fetch, to carry it for the
// script to send. It reads the states that render the most forms: a configured
// webhook, an active calendar feed, an active and an archived custom symptom.
func TestEveryRenderedSettingsWriteCarriesTheAccountBinding(t *testing.T) {
	t.Parallel()

	fixture := newDayFormAccountFixture(t, "settings-write-markup")
	guarded := guardedSettingsRoutes(t, fixture.app)
	patterns := map[string]*regexp.Regexp{}
	for _, route := range guarded {
		_, path, _ := strings.Cut(route, " ")
		patterns[route] = routePatternExpression(path)
	}

	// A state with a webhook, a feed and both kinds of symptom, reached through
	// the account's own JSON writes.
	for _, write := range []struct {
		method, path string
		form         url.Values
	}{
		{http.MethodPost, "/api/v1/users/current/webhook", url.Values{"webhook_url": {"https://ntfy.example.test/markup"}, "webhook_enabled": {"true"}, "webhook_notify_period": {"true"}}},
		{http.MethodPost, "/api/v1/symptoms", url.Values{"name": {"Markup active"}, "icon": {"✨"}}},
		{http.MethodPost, "/api/v1/symptoms", url.Values{"name": {"Markup archived"}, "icon": {"✨"}}},
	} {
		response := mustAppResponse(t, fixture.app, boundFormRequest(write.method, write.path, write.form, fixture.firstCookie, "", false))
		if response.StatusCode >= 300 {
			t.Fatalf("seed %s %s: %d", write.method, write.path, response.StatusCode)
		}
	}
	archivedID := customSymptomIDOfName(t, fixture.database, fixture.first.ID, "Markup archived")
	response := mustAppResponse(t, fixture.app, boundFormRequest(http.MethodDelete, fmt.Sprintf("/api/v1/symptoms/%d", archivedID), url.Values{}, fixture.firstCookie, "", false))
	if response.StatusCode >= 300 {
		t.Fatalf("archive the seeded symptom: %d", response.StatusCode)
	}

	found := map[string]int{}
	// The settings page renders the feed's generate form while no feed exists
	// and its rotate and revoke forms once one does, so it is read in both.
	assertRenderedBindings(t, fixture.app, "/settings", fixture.firstCookie, fixture.firstBinding, patterns, found)
	response = mustAppResponse(t, fixture.app, boundFormRequest(http.MethodPost, "/api/v1/users/current/calendar-feed", url.Values{}, fixture.firstCookie, "", false))
	if response.StatusCode >= 300 {
		t.Fatalf("generate the calendar feed: %d", response.StatusCode)
	}
	for _, path := range []string{"/settings", "/onboarding"} {
		if path == "/onboarding" {
			// An onboarded account is redirected away; render the page for an
			// account that still has onboarding to do.
			onboarding := newOnboardingAccountFixture(t, "settings-write-markup-onboarding")
			assertRenderedBindings(t, onboarding.app, path, onboarding.firstCookie, onboarding.firstBinding, patterns, found)
			continue
		}
		assertRenderedBindings(t, fixture.app, path, fixture.firstCookie, fixture.firstBinding, patterns, found)
	}
	// An account with no local password renders the erasure step-up starts in
	// place of the password-confirmed erasure forms.
	if err := fixture.database.Model(&models.User{}).Where("id = ?", fixture.first.ID).Update("local_auth_enabled", false).Error; err != nil {
		t.Fatalf("drop the local password: %v", err)
	}
	assertRenderedBindings(t, fixture.app, "/settings", fixture.firstCookie, fixture.firstBinding, patterns, found)

	// The writes that have no form of their own on any page, by name: the
	// timezone sync is a script (its binding rides on <body>, checked below),
	// the restore script reads data-import-account, and the explicit onboarding
	// completion is not posted by any page.
	withoutAForm := map[string]string{
		"POST /api/v1/users/current/timezone": "sent by the timezone sync script, from the binding on <body>",
		importJSONRouteName:                   "sent by the restore script, from data-import-account",
		"POST /api/v1/onboarding/complete":    "posted by no page",
	}
	for _, route := range guarded {
		if _, exempt := withoutAForm[route]; exempt {
			if found[route] != 0 {
				t.Errorf("%s is declared as having no form, but the markup walk found %d", route, found[route])
			}
			continue
		}
		if found[route] == 0 {
			t.Errorf("no rendered element writes to %s, and it is not declared as a write with no form of its own", route)
		}
	}
	for route := range withoutAForm {
		if !routeListHas(guarded, route) {
			t.Errorf("%s is declared as having no form but is not a bound route", route)
		}
	}
	for _, hook := range []string{"data-import-section", "data-persisted-timezone"} {
		if found[hook] == 0 {
			t.Fatalf("the markup walk found no %s element — it resolved %v", hook, found)
		}
	}
}

func customSymptomIDOfName(t *testing.T, database *gorm.DB, userID uint, name string) uint {
	t.Helper()
	var symptom models.SymptomType
	if err := database.Where("user_id = ? AND name = ?", userID, name).First(&symptom).Error; err != nil {
		t.Fatalf("find symptom %q of user %d: %v", name, userID, err)
	}
	return symptom.ID
}

func assertRenderedBindings(t *testing.T, app *fiber.App, path, cookie, binding string, patterns map[string]*regexp.Regexp, found map[string]int) {
	t.Helper()
	request := httptest.NewRequest(http.MethodGet, path, nil)
	request.Header.Set("Cookie", cookie)
	response := mustAppResponse(t, app, request)
	assertStatusCode(t, response, http.StatusOK)
	document, err := html.Parse(strings.NewReader(mustReadBodyString(t, response.Body)))
	if err != nil {
		t.Fatalf("%s: parse page: %v", path, err)
	}
	walkSettingsWriteElements(document, nil, patterns, func(route string, element, form *html.Node, target string) {
		found[route]++
		if got := dayWriteElementBinding(t, element, form); got != binding {
			t.Errorf("%s: <%s target=%q> (%s) carries account binding %q, want the rendering account's", path, element.Data, target, route, got)
		}
	})
	walkElementsWithAttribute(document, "data-import-section", func(element *html.Node) {
		found["data-import-section"]++
		if got := htmlAttributeValue(element, "data-import-account"); got != binding {
			t.Errorf("%s: the restore section carries account binding %q, want the rendering account's", path, got)
		}
	})
	walkElementsWithAttribute(document, "data-persisted-timezone", func(element *html.Node) {
		found["data-persisted-timezone"]++
		if got := htmlAttributeValue(element, "data-account-binding"); got != binding {
			t.Errorf("%s: <body> carries account binding %q for the timezone sync, want the rendering account's", path, got)
		}
	})
}

// walkSettingsWriteElements visits each element that addresses a bound route:
// by the verb its hx-* attribute names, or POST for a plain action whose form
// carries a _method override, which is read as that verb.
func walkSettingsWriteElements(node, form *html.Node, patterns map[string]*regexp.Regexp, visit func(route string, element, form *html.Node, target string)) {
	if node.Type == html.ElementNode {
		if node.Data == "form" {
			form = node
		}
		verb, address := "", ""
		for _, attribute := range node.Attr {
			if method, isVerb := settingsWriteVerbs[attribute.Key]; isVerb {
				verb, address = method, attribute.Val
			}
		}
		if verb == "" && node.Data == "form" {
			verb, address = "POST", htmlAttributeValue(node, "action")
			if override, ok := hiddenFieldValue(node, "_method"); ok && override != "" {
				verb = strings.ToUpper(override)
			}
		}
		if address != "" {
			path, _, _ := strings.Cut(address, "?")
			for route, pattern := range patterns {
				method, _, _ := strings.Cut(route, " ")
				if method == verb && pattern.MatchString(path) {
					visit(route, node, form, address)
					break
				}
			}
		}
	}
	for child := node.FirstChild; child != nil; child = child.NextSibling {
		walkSettingsWriteElements(child, form, patterns, visit)
	}
}

func walkElementsWithAttribute(node *html.Node, name string, visit func(element *html.Node)) {
	if node.Type == html.ElementNode {
		for _, attribute := range node.Attr {
			if attribute.Key == name {
				visit(node)
				break
			}
		}
	}
	for child := node.FirstChild; child != nil; child = child.NextSibling {
		walkElementsWithAttribute(child, name, visit)
	}
}

func htmlAttributeValue(node *html.Node, name string) string {
	for _, attribute := range node.Attr {
		if attribute.Key == name {
			return attribute.Val
		}
	}
	return ""
}
