package api

import (
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"

	"github.com/ovumcy/ovumcy-web/internal/models"
	"golang.org/x/net/html"
)

// WEB-120: the account-deletion form and the three day forms declared an htmx
// verb and no method="post", so without JavaScript they submitted as GET to the
// page they were on, the deletion password included in the query string. Each
// now posts to its htmx URL with the hidden _method field; these tests submit
// exactly what the rendered page gives a browser without JavaScript.

func accountExists(t *testing.T, ctx settingsSecurityTestContext) bool {
	t.Helper()
	var count int64
	if err := ctx.database.Model(&models.User{}).Where("id = ?", ctx.user.ID).Count(&count).Error; err != nil {
		t.Fatalf("count user: %v", err)
	}
	return count == 1
}

// periodLoggedOn reports whether the user has a period entry on iso, or on any
// day when iso is empty.
func periodLoggedOn(t *testing.T, ctx settingsSecurityTestContext, iso string) bool {
	t.Helper()
	var logs []models.DailyLog
	if err := ctx.database.Where("user_id = ?", ctx.user.ID).Find(&logs).Error; err != nil {
		t.Fatalf("load daily logs: %v", err)
	}
	for _, log := range logs {
		if log.IsPeriod && (iso == "" || log.Date.Format("2006-01-02") == iso) {
			return true
		}
	}
	return false
}

func noJSDay() (time.Time, string) {
	day := time.Now().UTC().AddDate(0, 0, -3)
	day = time.Date(day.Year(), day.Month(), day.Day(), 0, 0, 0, 0, time.UTC)
	return day, day.Format("2006-01-02")
}

// calendarLanding is the calendar page a no-JS day form must land on, spelled
// out here rather than taken from the handler's own helper.
func calendarLanding(iso string) string {
	return "/calendar?month=" + iso[:7] + "&day=" + iso
}

func deleteAccountCase(t *testing.T, email string) (noJSFormCase, settingsSecurityTestContext) {
	ctx := newSettingsSecurityTestContext(t, email)
	return noJSFormCase{
		app:      ctx.app,
		page:     "/settings",
		cookies:  authCookieMap(t, ctx.authCookie),
		match:    formWithAttr("hx-delete", "/api/v1/users/current"),
		verb:     http.MethodDelete,
		redirect: "/login",
		typed: func(*testing.T, noJSForm) url.Values {
			return url.Values{"password": {"StrongPass1"}}
		},
		happened: func(t *testing.T) bool { return !accountExists(t, ctx) },
	}, ctx
}

func TestNoJSDayAndAccountFormsPerformTheActionTheyName(t *testing.T) {
	t.Parallel()

	cases := map[string]func(t *testing.T) noJSFormCase{
		"delete account": func(t *testing.T) noJSFormCase {
			c, _ := deleteAccountCase(t, "nojs-delete-account@example.com")
			return c
		},
		"calendar day save": func(t *testing.T) noJSFormCase {
			ctx := newSettingsSecurityTestContext(t, "nojs-calendar-save@example.com")
			_, iso := noJSDay()
			return noJSFormCase{
				app:      ctx.app,
				page:     "/calendar/day/" + iso + "?mode=edit",
				cookies:  authCookieMap(t, ctx.authCookie),
				match:    formWithFlag("data-day-editor-form"),
				verb:     http.MethodPut,
				redirect: calendarLanding(iso),
				typed: func(*testing.T, noJSForm) url.Values {
					return url.Values{"is_period": {"true"}}
				},
				happened: func(t *testing.T) bool { return periodLoggedOn(t, ctx, iso) },
			}
		},
		"calendar day delete": func(t *testing.T) noJSFormCase {
			ctx := newSettingsSecurityTestContext(t, "nojs-calendar-delete@example.com")
			day, iso := noJSDay()
			if err := ctx.database.Create(&models.DailyLog{UserID: ctx.user.ID, Date: day, IsPeriod: true, Flow: models.FlowNone}).Error; err != nil {
				t.Fatalf("create daily log: %v", err)
			}
			return noJSFormCase{
				app:      ctx.app,
				page:     "/calendar/day/" + iso + "?mode=edit",
				cookies:  authCookieMap(t, ctx.authCookie),
				match:    formWithFlag("data-day-delete-form"),
				verb:     http.MethodDelete,
				redirect: calendarLanding(iso),
				happened: func(t *testing.T) bool { return !periodLoggedOn(t, ctx, iso) },
			}
		},
		"dashboard day save": func(t *testing.T) noJSFormCase {
			ctx := newSettingsSecurityTestContext(t, "nojs-dashboard-save@example.com")
			return noJSFormCase{
				app:      ctx.app,
				page:     "/dashboard",
				cookies:  authCookieMap(t, ctx.authCookie),
				match:    formWithFlag("data-dashboard-save-form"),
				verb:     http.MethodPut,
				redirect: "/dashboard",
				typed: func(*testing.T, noJSForm) url.Values {
					return url.Values{"is_period": {"true"}}
				},
				happened: func(t *testing.T) bool { return periodLoggedOn(t, ctx, "") },
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

// TestNoJSDeleteAccountRequiresThePasswordInTheBody pins the re-entry the form
// asks for: the account survives a submit whose body lacks the password or
// carries a wrong one, and a password placed in the query string is never read.
func TestNoJSDeleteAccountRequiresThePasswordInTheBody(t *testing.T) {
	t.Parallel()

	cases := map[string]func(form *noJSForm) url.Values{
		"no password": func(*noJSForm) url.Values { return nil },
		"wrong password": func(*noJSForm) url.Values {
			return url.Values{"password": {"NotThePassword9"}}
		},
		"password only in the query string": func(form *noJSForm) url.Values {
			form.action += "?password=StrongPass1"
			return nil
		},
	}

	for name, typed := range cases {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			c, ctx := deleteAccountCase(t, "nojs-delete-refused-"+strings.ReplaceAll(name, " ", "-")+"@example.com")
			form := renderNoJSForm(t, c.app, c.page, c.cookies, c.match)
			if form.fields.Has("password") {
				t.Fatal("the form renders a hidden password field")
			}
			response := form.submit(t, c.app, typed(&form))
			if location := response.Header.Get("Location"); location == "/login" {
				t.Fatalf("status %d redirected to /login: the refused deletion reported success", response.StatusCode)
			}
			if !accountExists(t, ctx) {
				t.Fatalf("status %d: the account was deleted", response.StatusCode)
			}
		})
	}
}

// TestDayWritesAnswerProgrammaticClientsAsBefore pins the other side of the
// no-JS redirects: they apply only to a form POST the override routed, so a
// client sending the real verb keeps the entry body and the 204 — with or
// without an Accept header, and even with the calendar form's own fields.
func TestDayWritesAnswerProgrammaticClientsAsBefore(t *testing.T) {
	t.Parallel()

	for _, accept := range []string{"application/json", ""} {
		t.Run("Accept="+accept, func(t *testing.T) {
			t.Parallel()
			ctx := newSettingsSecurityTestContext(t, "nojs-day-programmatic-"+strings.ReplaceAll(accept, "/", "-")+"@example.com")
			_, iso := noJSDay()

			send := func(method string, target string, body string) *http.Response {
				request := httptest.NewRequest(method, target, strings.NewReader(body))
				request.Header.Set("Content-Type", "application/x-www-form-urlencoded")
				if accept != "" {
					request.Header.Set("Accept", accept)
				}
				request.Header.Set("X-CSRF-Token", ctx.csrfToken)
				request.Header.Set("Cookie", ctx.authCookie+"; "+ctx.csrfCookie.Name+"="+ctx.csrfCookie.Value)
				return mustAppResponse(t, ctx.app, request)
			}

			saved := send(http.MethodPut, "/api/v1/days/"+iso, url.Values{"is_period": {"true"}, "source": {"calendar"}, "_method": {"PUT"}}.Encode())
			assertStatusCode(t, saved, http.StatusOK)
			if contentType := saved.Header.Get("Content-Type"); !strings.HasPrefix(contentType, "application/json") {
				t.Fatalf("PUT answered Content-Type %q, want JSON", contentType)
			}

			deleted := send(http.MethodDelete, "/api/v1/days/"+iso+"?source=calendar", url.Values{"_method": {"DELETE"}}.Encode())
			assertStatusCode(t, deleted, http.StatusNoContent)
			if periodLoggedOn(t, ctx, iso) {
				t.Fatal("the programmatic DELETE left the entry")
			}
		})
	}
}

// TestHTMXDayFormsAreUnaffectedByTheFieldsTheyNowCarry sends each day form the
// way htmx does — its real verb, every rendered hidden input in the body, the
// new _method and source included — and expects the fragment htmx swaps in.
func TestHTMXDayFormsAreUnaffectedByTheFieldsTheyNowCarry(t *testing.T) {
	t.Parallel()

	cases := map[string]struct {
		page   func(iso string) string
		match  func(*html.Node) bool
		verb   string
		typed  url.Values
		seeded bool
		want   bool
	}{
		"calendar save": {
			page:  func(iso string) string { return "/calendar/day/" + iso + "?mode=edit" },
			match: formWithFlag("data-day-editor-form"), verb: http.MethodPut,
			typed: url.Values{"is_period": {"true"}}, want: true,
		},
		"calendar delete": {
			page:  func(iso string) string { return "/calendar/day/" + iso + "?mode=edit" },
			match: formWithFlag("data-day-delete-form"), verb: http.MethodDelete,
			seeded: true, want: false,
		},
		"dashboard save": {
			page:  func(string) string { return "/dashboard" },
			match: formWithFlag("data-dashboard-save-form"), verb: http.MethodPut,
			typed: url.Values{"is_period": {"true"}}, want: true,
		},
	}

	for name, c := range cases {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			ctx := newSettingsSecurityTestContext(t, "htmx-day-"+strings.ReplaceAll(name, " ", "-")+"@example.com")
			day, iso := noJSDay()
			if c.seeded {
				if err := ctx.database.Create(&models.DailyLog{UserID: ctx.user.ID, Date: day, IsPeriod: true, Flow: models.FlowNone}).Error; err != nil {
					t.Fatalf("create daily log: %v", err)
				}
			}
			form := renderNoJSForm(t, ctx.app, c.page(iso), authCookieMap(t, ctx.authCookie), c.match)
			body := cloneFormValues(form.fields)
			for key, values := range c.typed {
				body[key] = values
			}
			request := httptest.NewRequest(c.verb, form.action, strings.NewReader(body.Encode()))
			request.Header.Set("Content-Type", "application/x-www-form-urlencoded")
			request.Header.Set("HX-Request", "true")
			request.Header.Set("Accept-Language", "en")
			request.Header.Set("Cookie", cookieHeaderFromMap(form.cookies))
			response := mustAppResponse(t, ctx.app, request)
			assertStatusCode(t, response, http.StatusOK)
			if trigger := response.Header.Get("HX-Trigger"); trigger != "calendar-day-updated" {
				t.Fatalf("HX-Trigger %q, want calendar-day-updated", trigger)
			}
			if got := periodLoggedOn(t, ctx, ""); got != c.want {
				t.Fatalf("period logged = %v after the htmx %s, want %v", got, c.verb, c.want)
			}
		})
	}
}

// TestNoJSDaySaveLeavesTheLongPeriodWarningPending pins that the no-JS save,
// whose redirect carries no notice, does not record the long-period warning as
// shown: the ninth period day saved without JavaScript leaves it pending.
func TestNoJSDaySaveLeavesTheLongPeriodWarningPending(t *testing.T) {
	t.Parallel()
	ctx := newSettingsSecurityTestContext(t, "nojs-long-period@example.com")
	if err := ctx.database.Model(&models.User{}).Where("id = ?", ctx.user.ID).Update("auto_period_fill", false).Error; err != nil {
		t.Fatalf("disable auto period fill: %v", err)
	}
	cycleStart := time.Date(2026, time.March, 1, 0, 0, 0, 0, time.UTC)
	for offset := range 8 {
		entry := models.DailyLog{UserID: ctx.user.ID, Date: cycleStart.AddDate(0, 0, offset), IsPeriod: true, Flow: models.FlowMedium}
		if err := ctx.database.Create(&entry).Error; err != nil {
			t.Fatalf("seed period day: %v", err)
		}
	}

	form := renderNoJSForm(t, ctx.app, "/calendar/day/2026-03-09?mode=edit", authCookieMap(t, ctx.authCookie), formWithFlag("data-day-editor-form"))
	response := form.submit(t, ctx.app, url.Values{"is_period": {"true"}, "flow": {models.FlowMedium}})
	if response.StatusCode != http.StatusSeeOther {
		t.Fatalf("no-JS save: status %d, want 303", response.StatusCode)
	}
	if !periodLoggedOn(t, ctx, "2026-03-09") {
		t.Fatal("precondition: the ninth period day was not saved")
	}

	var persisted models.User
	if err := ctx.database.Select("long_period_warning_cycle_start").First(&persisted, ctx.user.ID).Error; err != nil {
		t.Fatalf("reload user: %v", err)
	}
	if persisted.LongPeriodWarningCycleStart != nil {
		t.Fatal("the no-JS save recorded the long-period warning as shown, but its redirect showed nothing")
	}
}

// TestNoJSDayDeleteAsksForConfirmation pins the confirmation a browser without
// JavaScript gets instead of hx-confirm: a required checkbox the form cannot be
// submitted without, dropped by scripts, so only a browser without them keeps it.
func TestNoJSDayDeleteAsksForConfirmation(t *testing.T) {
	t.Parallel()
	ctx := newSettingsSecurityTestContext(t, "nojs-day-delete-confirm@example.com")
	day, iso := noJSDay()
	if err := ctx.database.Create(&models.DailyLog{UserID: ctx.user.ID, Date: day, IsPeriod: true, Flow: models.FlowNone}).Error; err != nil {
		t.Fatalf("create daily log: %v", err)
	}

	request := httptest.NewRequest(http.MethodGet, "/calendar/day/"+iso+"?mode=edit", nil)
	request.Header.Set("Accept-Language", "en")
	request.Header.Set("Cookie", ctx.authCookie)
	response := mustAppResponse(t, ctx.app, request)
	assertStatusCode(t, response, http.StatusOK)
	document, err := html.ParseWithOptions(strings.NewReader(mustReadBodyString(t, response.Body)), html.ParseOptionEnableScripting(false))
	if err != nil {
		t.Fatalf("parse without scripting: %v", err)
	}

	form := htmlFindElement(document, func(node *html.Node) bool {
		return node.Type == html.ElementNode && node.Data == "form" && htmlHasAttr(node, "data-day-delete-form")
	})
	if form == nil {
		t.Fatal("no day delete form rendered")
	}
	confirm := htmlFindElement(form, func(node *html.Node) bool {
		return node.Type == html.ElementNode && node.Data == "input" && htmlHasAttr(node, "data-day-delete-nojs-confirm")
	})
	if confirm == nil {
		t.Fatal("the delete form has no no-JS confirmation box")
	}
	if htmlAttr(confirm, "type") != "checkbox" || !htmlHasAttr(confirm, "required") {
		t.Fatalf("confirmation box type=%q required=%v, want a required checkbox", htmlAttr(confirm, "type"), htmlHasAttr(confirm, "required"))
	}
	if confirm.Parent == nil || !htmlHasAttr(confirm.Parent, "data-nojs-only") {
		t.Fatal("the confirmation label must carry data-nojs-only: with JavaScript, hx-confirm asks and scripts drop the box")
	}
}
