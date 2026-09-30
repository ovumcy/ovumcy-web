package api

import (
	"html/template"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"

	"github.com/ovumcy/ovumcy-web/internal/models"
	"golang.org/x/net/html"
)

// The onboarding steps and the manual cycle-start control are hx-post forms
// that declared no action or method, so without JavaScript they submitted as
// GET to the page they were on: the CSRF token, the timezone, the last period
// date and the cycle lengths landed in the query string, and nothing was saved.
// They now post to their htmx URL; no _method is needed or rendered. Where the
// htmx surface relies on a script (the onboarding date picker, the cycle-start
// confirm dialog) the form carries a data-nojs-only control that scripts drop,
// and a refusal answers a page with a link back instead of the JSON envelope.

// postingToItsHTMXURL narrows match to a form whose action is its hx-post URL.
func postingToItsHTMXURL(match func(*html.Node) bool) func(*html.Node) bool {
	return func(node *html.Node) bool {
		return match(node) && htmlHasAttr(node, "action") && htmlAttr(node, "action") == htmlAttr(node, "hx-post")
	}
}

// browserFormBody serializes form as a browser without JavaScript does: every
// named, enabled control in document order, a checkbox or radio only when
// checked, a select's selected (else first) option, and the value typed into a
// visible field in place of its own. A name in checked overrides the box's
// rendered state.
func browserFormBody(form *html.Node, checked map[string]bool, typed map[string]string) url.Values {
	body := url.Values{}
	for _, control := range htmlFindElements(form, func(node *html.Node) bool {
		return node.Type == html.ElementNode && (node.Data == "input" || node.Data == "select" || node.Data == "textarea")
	}) {
		name := htmlAttr(control, "name")
		if name == "" || htmlHasAttr(control, "disabled") {
			continue
		}
		if value, ok := typed[name]; ok && control.Data != "input" {
			body.Add(name, value)
			continue
		}
		switch {
		case control.Data == "textarea":
			body.Add(name, htmlNodeText(control))
		case control.Data == "select":
			body.Add(name, selectedOptionValue(control))
		case htmlAttr(control, "type") == "hidden":
			body.Add(name, htmlAttr(control, "value"))
		case htmlAttr(control, "type") == "checkbox" || htmlAttr(control, "type") == "radio":
			on, stated := checked[name]
			if !stated {
				on = htmlHasAttr(control, "checked")
			}
			if on {
				body.Add(name, htmlAttr(control, "value"))
			}
		default:
			if value, ok := typed[name]; ok {
				body.Add(name, value)
			} else {
				body.Add(name, htmlAttr(control, "value"))
			}
		}
	}
	return body
}

func selectedOptionValue(selectNode *html.Node) string {
	options := htmlFindElements(selectNode, func(node *html.Node) bool {
		return node.Type == html.ElementNode && node.Data == "option"
	})
	chosen := (*html.Node)(nil)
	for _, option := range options {
		if htmlHasAttr(option, "selected") {
			chosen = option
			break
		}
	}
	if chosen == nil && len(options) > 0 {
		chosen = options[0]
	}
	if chosen == nil {
		return ""
	}
	if htmlHasAttr(chosen, "value") {
		return htmlAttr(chosen, "value")
	}
	return strings.TrimSpace(htmlNodeText(chosen))
}

func cycleStartOn(t *testing.T, ctx settingsSecurityTestContext, iso string) bool {
	t.Helper()
	return dailyLogOn(t, ctx, iso, func(log models.DailyLog) bool { return log.CycleStart })
}

// newOnboardingNoJSContext is a signed-in owner who has not finished onboarding.
func newOnboardingNoJSContext(t *testing.T, email string) settingsSecurityTestContext {
	t.Helper()
	ctx := newSettingsSecurityTestContext(t, email)
	if err := ctx.database.Model(&models.User{}).Where("id = ?", ctx.user.ID).Update("onboarding_completed", false).Error; err != nil {
		t.Fatalf("reopen onboarding: %v", err)
	}
	return ctx
}

func utcToday() (time.Time, string) {
	day := time.Now().UTC()
	day = time.Date(day.Year(), day.Month(), day.Day(), 0, 0, 0, 0, time.UTC)
	return day, day.Format("2006-01-02")
}

func TestNoJSHXPostFormsPostToTheirEndpoint(t *testing.T) {
	t.Parallel()

	cases := map[string]func(t *testing.T) noJSFormCase{
		"onboarding step 1": func(t *testing.T) noJSFormCase {
			ctx := newOnboardingNoJSContext(t, "nojs-onboarding-step1@example.com")
			_, iso := noJSDay()
			return noJSFormCase{
				app:      ctx.app,
				page:     "/onboarding",
				cookies:  authCookieMap(t, ctx.authCookie),
				match:    postingToItsHTMXURL(formWithAttr("data-onboarding-form-step", "1")),
				redirect: "/onboarding?step=2",
				typed: func(_ *testing.T, form noJSForm) url.Values {
					return browserFormBody(form.node, nil, map[string]string{"last_period_start": iso})
				},
				happened: func(t *testing.T) bool {
					start := reloadUserForNoJSForm(t, ctx).LastPeriodStart
					return start != nil && start.Format("2006-01-02") == iso
				},
			}
		},
		"onboarding step 2": func(t *testing.T) noJSFormCase {
			ctx := newOnboardingNoJSContext(t, "nojs-onboarding-step2@example.com")
			day, _ := noJSDay()
			if err := ctx.database.Model(&models.User{}).Where("id = ?", ctx.user.ID).Update("last_period_start", day).Error; err != nil {
				t.Fatalf("seed step 1: %v", err)
			}
			return noJSFormCase{
				app:      ctx.app,
				page:     "/onboarding?step=2",
				cookies:  authCookieMap(t, ctx.authCookie),
				match:    postingToItsHTMXURL(formWithAttr("data-onboarding-form-step", "2")),
				redirect: "/dashboard",
				typed: func(_ *testing.T, form noJSForm) url.Values {
					return browserFormBody(form.node, nil, map[string]string{"cycle_length": "30"})
				},
				happened: func(t *testing.T) bool {
					user := reloadUserForNoJSForm(t, ctx)
					return user.OnboardingCompleted && user.CycleLength == 30
				},
			}
		},
		"dashboard cycle start": func(t *testing.T) noJSFormCase {
			ctx := newSettingsSecurityTestContext(t, "nojs-dashboard-cycle-start@example.com")
			_, today := utcToday()
			return noJSFormCase{
				app:      ctx.app,
				page:     "/dashboard",
				cookies:  authCookieMap(t, ctx.authCookie),
				match:    postingToItsHTMXURL(formWithFlag("data-dashboard-cycle-start-form")),
				redirect: "/dashboard",
				happened: func(t *testing.T) bool { return cycleStartOn(t, ctx, today) },
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
				happened: func(t *testing.T) bool { return cycleStartOn(t, ctx, iso) },
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

// assertRefusalPage checks a no-JS refusal answered a page, not the JSON
// envelope: the mapped status, text/html, and a link back to the form's page.
func assertRefusalPage(t *testing.T, response *http.Response, status int, back string) {
	t.Helper()
	body := mustReadBodyString(t, response.Body)
	if response.StatusCode != status {
		t.Fatalf("status %d, want %d: %s", response.StatusCode, status, body)
	}
	if contentType := response.Header.Get("Content-Type"); !strings.HasPrefix(contentType, "text/html") {
		t.Fatalf("Content-Type %q, want text/html: %s", contentType, body)
	}
	if want := `href="` + template.HTMLEscapeString(back) + `"`; !strings.Contains(body, want) {
		t.Fatalf("refusal page has no %s: %s", want, body)
	}
}

// cycleStartConflictCase is a calendar day two days after a recorded cycle
// start: marking it both replaces that start and closes a short gap.
func cycleStartConflictCase(t *testing.T, email string) (settingsSecurityTestContext, noJSForm, string, string) {
	t.Helper()
	ctx := newSettingsSecurityTestContext(t, email)
	day, iso := noJSDay()
	earlier := day.AddDate(0, 0, -2)
	if err := ctx.database.Create(&models.DailyLog{UserID: ctx.user.ID, Date: earlier, IsPeriod: true, CycleStart: true, Flow: models.FlowMedium}).Error; err != nil {
		t.Fatalf("seed earlier cycle start: %v", err)
	}
	form := renderNoJSForm(t, ctx.app, "/calendar/day/"+iso, authCookieMap(t, ctx.authCookie),
		postingToItsHTMXURL(formWithFlag("data-day-cycle-start-form")))
	return ctx, form, iso, earlier.Format("2006-01-02")
}

func TestNoJSCycleStartConfirmsAConflictWithItsOwnBoxes(t *testing.T) {
	t.Parallel()

	t.Run("the boxes precede their hidden twins", func(t *testing.T) {
		t.Parallel()
		_, form, _, _ := cycleStartConflictCase(t, "nojs-cycle-start-boxes@example.com")
		body := browserFormBody(form.node, map[string]bool{"replace_existing": true, "mark_uncertain": true}, nil)
		for _, name := range []string{"replace_existing", "mark_uncertain"} {
			if got := body[name]; len(got) != 2 || got[0] != "true" || got[1] != "false" {
				t.Fatalf("%s serializes as %q, want the checked box before the hidden false", name, got)
			}
		}
	})

	t.Run("unconfirmed answers a page with a way back", func(t *testing.T) {
		t.Parallel()
		ctx, form, iso, earlier := cycleStartConflictCase(t, "nojs-cycle-start-unconfirmed@example.com")
		response := form.submit(t, ctx.app, browserFormBody(form.node, nil, nil))
		assertRefusalPage(t, response, http.StatusConflict, calendarLanding(iso))
		if cycleStartOn(t, ctx, iso) || !cycleStartOn(t, ctx, earlier) {
			t.Fatal("the refused mark changed the recorded cycle start")
		}
	})

	t.Run("confirmed replaces the earlier start", func(t *testing.T) {
		t.Parallel()
		ctx, form, iso, earlier := cycleStartConflictCase(t, "nojs-cycle-start-confirmed@example.com")
		response := form.submit(t, ctx.app, browserFormBody(form.node, map[string]bool{"replace_existing": true, "mark_uncertain": true}, nil))
		if response.StatusCode != http.StatusSeeOther || response.Header.Get("Location") != calendarLanding(iso) {
			t.Fatalf("status %d Location %q, want 303 %q", response.StatusCode, response.Header.Get("Location"), calendarLanding(iso))
		}
		if !cycleStartOn(t, ctx, iso) || cycleStartOn(t, ctx, earlier) {
			t.Fatal("the confirmed mark did not replace the earlier cycle start")
		}
	})

	t.Run("an API client keeps the JSON envelope", func(t *testing.T) {
		t.Parallel()
		ctx, form, _, _ := cycleStartConflictCase(t, "nojs-cycle-start-api@example.com")
		request := httptest.NewRequest(http.MethodPost, form.action, strings.NewReader(""))
		request.Header.Set("Content-Type", "application/x-www-form-urlencoded")
		request.Header.Set("X-CSRF-Token", ctx.csrfToken)
		request.Header.Set("Cookie", ctx.authCookie+"; "+ctx.csrfCookie.Name+"="+ctx.csrfCookie.Value)
		response := mustAppResponse(t, ctx.app, request)
		assertStatusCode(t, response, http.StatusConflict)
		if contentType := response.Header.Get("Content-Type"); !strings.HasPrefix(contentType, "application/json") {
			t.Fatalf("Content-Type %q, want JSON", contentType)
		}
	})
}

func TestNoJSOnboardingStep1WithoutADateAnswersAPageWithAWayBack(t *testing.T) {
	t.Parallel()
	ctx := newOnboardingNoJSContext(t, "nojs-onboarding-empty-date@example.com")
	form := renderNoJSForm(t, ctx.app, "/onboarding", authCookieMap(t, ctx.authCookie),
		postingToItsHTMXURL(formWithAttr("data-onboarding-form-step", "1")))
	response := form.submit(t, ctx.app, browserFormBody(form.node, nil, map[string]string{"last_period_start": ""}))
	assertRefusalPage(t, response, http.StatusBadRequest, "/onboarding?step=1")
	if reloadUserForNoJSForm(t, ctx).LastPeriodStart != nil {
		t.Fatal("the refused step saved a date")
	}
}

// TestNoJSCycleStartShowsTheImplantationCautionBeforeTheMark pins where the
// no-JS path gets its caution: the confirm dialog and the htmx notice both need
// JavaScript, so the page that renders the form must state it beside the button.
func TestNoJSCycleStartShowsTheImplantationCautionBeforeTheMark(t *testing.T) {
	t.Parallel()
	_, today := utcToday()
	surfaces := map[string]struct {
		page string
		form string
	}{
		"dashboard":          {"/dashboard", "data-dashboard-cycle-start-form"},
		"calendar day":       {"/calendar/day/" + today, "data-day-cycle-start-form"},
		"calendar day, edit": {"/calendar/day/" + today + "?mode=edit", "data-day-cycle-start-form"},
	}
	for name, surface := range surfaces {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			ctx := newSettingsSecurityTestContext(t, "nojs-implantation-"+strings.ReplaceAll(strings.ReplaceAll(name, " ", "-"), ",", "")+"@example.com")
			// Same shape as TestMarkCycleStartImplantationWarningSetsEncodedNoticeWithKey:
			// two starts 28 days apart put today 6-12 days past the projected ovulation.
			day, _ := utcToday()
			for _, daysAgo := range []int{50, 22} {
				if err := ctx.database.Create(&models.DailyLog{UserID: ctx.user.ID, Date: day.AddDate(0, 0, -daysAgo), IsPeriod: true, CycleStart: true, Flow: models.FlowMedium}).Error; err != nil {
					t.Fatalf("seed cycle start %d days back: %v", daysAgo, err)
				}
			}

			request := httptest.NewRequest(http.MethodGet, surface.page, nil)
			request.Header.Set("Accept-Language", "en")
			request.Header.Set("Cookie", ctx.authCookie)
			response := mustAppResponse(t, ctx.app, request)
			assertStatusCode(t, response, http.StatusOK)
			document := mustParseHTMLDocument(t, mustReadBodyString(t, response.Body))

			form := htmlFindElement(document, postingToItsHTMXURL(formWithFlag(surface.form)))
			if form == nil {
				t.Fatalf("%s rendered no no-JS cycle-start form", surface.page)
			}
			// The form sits in its button row; the caution is in the card around it.
			if text := htmlNodeText(form.Parent.Parent); !strings.Contains(text, "timing alone cannot tell") {
				t.Fatalf("the cycle-start form on %s stands without the implantation caution: %q", surface.page, text)
			}
		})
	}
}
