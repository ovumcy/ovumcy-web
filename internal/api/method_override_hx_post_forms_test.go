package api

import (
	"net/url"
	"testing"

	"github.com/ovumcy/ovumcy-web/internal/models"
	"golang.org/x/net/html"
)

// The onboarding steps and the manual cycle-start control are hx-post forms
// that declared no action or method, so without JavaScript they submitted as
// GET to the page they were on: the CSRF token, the timezone, the last period
// date and the cycle lengths landed in the query string, and nothing was saved.
// They now post to their htmx URL; no _method is needed or rendered.

// postingToItsHTMXURL narrows match to a form whose action is its hx-post URL.
func postingToItsHTMXURL(match func(*html.Node) bool) func(*html.Node) bool {
	return func(node *html.Node) bool {
		return match(node) && htmlHasAttr(node, "action") && htmlAttr(node, "action") == htmlAttr(node, "hx-post")
	}
}

type onboardingNoJSContext struct {
	ctx     settingsSecurityTestContext
	cookies map[string]string
}

func newOnboardingNoJSContext(t *testing.T, email string) onboardingNoJSContext {
	t.Helper()
	app, database := newOnboardingTestAppWithCSRF(t)
	user := createOnboardingTestUser(t, database, email, "StrongPass1", false)
	authCookie := loginAndExtractAuthCookieWithCSRF(t, app, user.Email, "StrongPass1")
	return onboardingNoJSContext{
		ctx:     settingsSecurityTestContext{app: app, database: database, user: user, authCookie: authCookie},
		cookies: authCookieMap(t, authCookie),
	}
}

// cycleStartMarkedOn reports whether the user has an explicit cycle start on
// iso, or on any day when iso is empty.
func cycleStartMarkedOn(t *testing.T, ctx settingsSecurityTestContext, iso string) bool {
	t.Helper()
	var logs []models.DailyLog
	if err := ctx.database.Where("user_id = ?", ctx.user.ID).Find(&logs).Error; err != nil {
		t.Fatalf("load daily logs: %v", err)
	}
	for _, log := range logs {
		if log.CycleStart && (iso == "" || log.Date.Format("2006-01-02") == iso) {
			return true
		}
	}
	return false
}

func TestNoJSHXPostFormsPostToTheirEndpoint(t *testing.T) {
	t.Parallel()

	cases := map[string]func(t *testing.T) noJSFormCase{
		"onboarding step 1": func(t *testing.T) noJSFormCase {
			o := newOnboardingNoJSContext(t, "nojs-onboarding-step1@example.com")
			_, iso := noJSDay()
			return noJSFormCase{
				app:      o.ctx.app,
				page:     "/onboarding",
				cookies:  o.cookies,
				match:    postingToItsHTMXURL(formWithAttr("data-onboarding-form-step", "1")),
				redirect: "/onboarding?step=2",
				typed: func(*testing.T, noJSForm) url.Values {
					return url.Values{"last_period_start": {iso}}
				},
				happened: func(t *testing.T) bool {
					return reloadUserForNoJSForm(t, o.ctx).LastPeriodStart != nil
				},
			}
		},
		"onboarding step 2": func(t *testing.T) noJSFormCase {
			o := newOnboardingNoJSContext(t, "nojs-onboarding-step2@example.com")
			day, _ := noJSDay()
			if err := o.ctx.database.Model(&models.User{}).Where("id = ?", o.ctx.user.ID).Update("last_period_start", day).Error; err != nil {
				t.Fatalf("seed step 1: %v", err)
			}
			return noJSFormCase{
				app:      o.ctx.app,
				page:     "/onboarding?step=2",
				cookies:  o.cookies,
				match:    postingToItsHTMXURL(formWithAttr("data-onboarding-form-step", "2")),
				redirect: "/dashboard",
				typed: func(*testing.T, noJSForm) url.Values {
					return url.Values{"cycle_length": {"30"}, "period_length": {"5"}, "auto_period_fill": {"true"}}
				},
				happened: func(t *testing.T) bool {
					user := reloadUserForNoJSForm(t, o.ctx)
					return user.OnboardingCompleted && user.CycleLength == 30
				},
			}
		},
		"dashboard cycle start": func(t *testing.T) noJSFormCase {
			ctx := newSettingsSecurityTestContext(t, "nojs-dashboard-cycle-start@example.com")
			return noJSFormCase{
				app:      ctx.app,
				page:     "/dashboard",
				cookies:  authCookieMap(t, ctx.authCookie),
				match:    postingToItsHTMXURL(formWithFlag("data-dashboard-cycle-start-form")),
				redirect: "/dashboard",
				happened: func(t *testing.T) bool { return cycleStartMarkedOn(t, ctx, "") },
			}
		},
		"calendar cycle start": func(t *testing.T) noJSFormCase {
			ctx := newSettingsSecurityTestContext(t, "nojs-calendar-cycle-start@example.com")
			_, iso := noJSDay()
			return noJSFormCase{
				app:      ctx.app,
				page:     "/calendar/day/" + iso,
				cookies:  authCookieMap(t, ctx.authCookie),
				match:    postingToItsHTMXURL(formWithFlag("data-day-cycle-start-form")),
				redirect: calendarLanding(iso),
				happened: func(t *testing.T) bool { return cycleStartMarkedOn(t, ctx, iso) },
			}
		},
	}

	for name, build := range cases {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			runNoJSFormCase(t, build(t))
		})
	}
}
