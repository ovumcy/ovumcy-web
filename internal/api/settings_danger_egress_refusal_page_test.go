package api

import (
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"

	"github.com/ovumcy/ovumcy-web/internal/models"
)

// WEB-135: the erasure and egress forms of the settings page, submitted without
// JavaScript, answer a refusal raised before their handler as a page. A stale
// CSRF token and a request with no session both end on the localized message and
// one fixed link back to /settings, with no cookie of any kind. The refused form
// must also have done nothing: the account, the data and the feed are untouched.

type settingsRefusalCase struct {
	action string
	verb   string
	// arm prepares the account before the refused submit and returns what kept
	// compares against.
	arm func(t *testing.T, ctx settingsSecurityTestContext) string
	// kept reports whether what the form would have changed is untouched.
	kept func(t *testing.T, ctx settingsSecurityTestContext, armed string) bool
}

func settingsRefusalCases() map[string]settingsRefusalCase {
	const root = "/api/v1/users/current"
	feedSelector := func(t *testing.T, ctx settingsSecurityTestContext) string {
		return reloadUserForNoJSForm(t, ctx).CalendarFeedSelector
	}
	armFeed := func(t *testing.T, ctx settingsSecurityTestContext) string {
		armCalendarFeedForUser(t, ctx.database, ctx.user.ID)
		armed := feedSelector(t, ctx)
		if armed == "" {
			t.Fatal("precondition: the armed feed stored no selector")
		}
		return armed
	}
	feedKept := func(t *testing.T, ctx settingsSecurityTestContext, armed string) bool {
		return feedSelector(t, ctx) == armed
	}
	return map[string]settingsRefusalCase{
		"delete account": {
			action: root, verb: http.MethodDelete,
			kept: func(t *testing.T, ctx settingsSecurityTestContext, _ string) bool {
				var count int64
				if err := ctx.database.Model(&models.User{}).Where("id = ?", ctx.user.ID).Count(&count).Error; err != nil {
					t.Fatalf("count users: %v", err)
				}
				return count == 1
			},
		},
		"clear data": {
			action: root + "/data-wipe",
			arm: func(t *testing.T, ctx settingsSecurityTestContext) string {
				day, _ := noJSDay()
				if err := ctx.database.Create(&models.DailyLog{UserID: ctx.user.ID, Date: day, IsPeriod: true, Flow: models.FlowNone}).Error; err != nil {
					t.Fatalf("create daily log: %v", err)
				}
				return ""
			},
			kept: func(t *testing.T, ctx settingsSecurityTestContext, _ string) bool { return periodLoggedOn(t, ctx, "") },
		},
		"clear data step-up": {action: root + "/data-wipe/step-up"},
		"deletion step-up":   {action: root + "/deletion/step-up"},
		"webhook save":       {action: root + "/webhook"},
		"webhook remove":     {action: root + "/webhook", verb: http.MethodDelete},
		"calendar feed create": {
			action: root + "/calendar-feed",
			kept:   func(t *testing.T, ctx settingsSecurityTestContext, _ string) bool { return feedSelector(t, ctx) == "" },
		},
		"calendar feed rotate": {action: root + "/calendar-feed/rotate", arm: armFeed, kept: feedKept},
		"calendar feed revoke": {action: root + "/calendar-feed", verb: http.MethodDelete, arm: armFeed, kept: feedKept},
	}
}

// submitSettingsRefusalForm posts one form the way a browser without JavaScript
// would. withSession and withCSRF choose which of the two proofs the request
// carries; the password is always right, so a handler that ran would act on it.
func submitSettingsRefusalForm(t *testing.T, ctx settingsSecurityTestContext, c settingsRefusalCase, withSession bool, withCSRF bool) *http.Response {
	t.Helper()
	body := url.Values{"password": {"StrongPass1"}}
	if c.verb != "" {
		body.Set("_method", c.verb)
	}
	cookies := []string{}
	if withSession {
		cookies = append(cookies, ctx.authCookie)
	}
	if withCSRF {
		body.Set("csrf_token", ctx.csrfToken)
		cookies = append(cookies, ctx.csrfCookie.Name+"="+ctx.csrfCookie.Value)
	}
	request := httptest.NewRequest(http.MethodPost, c.action, strings.NewReader(body.Encode()))
	request.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	request.Header.Set("Accept", noJSBrowserAccept)
	request.Header.Set("Accept-Language", "en")
	request.Header.Set("Cookie", strings.Join(cookies, "; "))
	return mustAppResponse(t, ctx.app, request)
}

func TestNoJSDangerAndEgressFormsAnswerAPreHandlerRefusalAsAPage(t *testing.T) {
	t.Parallel()

	forbidden := englishCopy(t, "common.error.forbidden")
	for name, c := range settingsRefusalCases() {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			ctx := newRefusalPageContext(t, "refusal-"+strings.ReplaceAll(name, " ", "-")+"@example.com")
			armed := ""
			if c.arm != nil {
				armed = c.arm(t, ctx)
			}

			assertRefusalPageCarrying(t, submitSettingsRefusalForm(t, ctx, c, true, false), http.StatusForbidden, forbidden, "/settings")
			// Signed out, the page's link back would only bounce off AuthRequired:
			// the browser is sent to sign in instead (WEB-264).
			assertSentToSignIn(t, submitSettingsRefusalForm(t, ctx, c, false, true))
			if c.kept != nil && !c.kept(t, ctx, armed) {
				t.Fatal("a refused form changed what it names")
			}
		})
	}
}

// TestNoJSSettingsRefusalLinkIgnoresWhatTheRequestCarries submits a form whose
// action carries a hostile query string: the back link is the route's own.
func TestNoJSSettingsRefusalLinkIgnoresWhatTheRequestCarries(t *testing.T) {
	t.Parallel()

	ctx := newRefusalPageContext(t, "refusal-settings-link@example.com")
	hostile := "?source=https://evil.example/&next=//evil.example/&back=%22%3E%3Cscript%3E"
	for name, c := range settingsRefusalCases() {
		t.Run(name, func(t *testing.T) {
			c.action += hostile
			assertRefusalPageCarrying(t, submitSettingsRefusalForm(t, ctx, c, true, false), http.StatusForbidden, englishCopy(t, "common.error.forbidden"), "/settings")
		})
	}
}

// TestSettingsRefusalPagesAreForBrowsersOnly pins the other side: a caller that
// asks for JSON, and the real verb with no form behind it, keep the envelope.
func TestSettingsRefusalPagesAreForBrowsersOnly(t *testing.T) {
	t.Parallel()

	ctx := newRefusalPageContext(t, "refusal-settings-clients@example.com")
	for name, accept := range map[string]string{"JSON client": "application/json", "htmx": noJSBrowserAccept} {
		t.Run(name, func(t *testing.T) {
			request := httptest.NewRequest(http.MethodPost, "/api/v1/users/current/webhook", strings.NewReader("_method=DELETE"))
			request.Header.Set("Content-Type", "application/x-www-form-urlencoded")
			request.Header.Set("Accept", accept)
			request.Header.Set("Cookie", ctx.authCookie)
			if name == "htmx" {
				request.Header.Set("HX-Request", "true")
			}
			response := mustAppResponse(t, ctx.app, request)
			assertStatusCode(t, response, http.StatusForbidden)
			body := mustReadBodyString(t, response.Body)
			if strings.Contains(body, "href=") {
				t.Fatalf("%s got the page's back link: %s", name, body)
			}
			isJSON := strings.HasPrefix(response.Header.Get("Content-Type"), "application/json")
			if (name == "JSON client") != isJSON {
				t.Fatalf("%s: Content-Type %q", name, response.Header.Get("Content-Type"))
			}
		})
	}
}
