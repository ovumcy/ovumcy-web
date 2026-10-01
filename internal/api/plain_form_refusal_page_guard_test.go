package api

import (
	"fmt"
	"io/fs"
	"net/http"
	"net/http/httptest"
	"regexp"
	"sort"
	"strings"
	"testing"

	"github.com/gofiber/fiber/v3"
	"golang.org/x/net/html"

	"github.com/ovumcy/ovumcy-web/internal/templates"
)

// WEB-134. A <form method="post"> that submits to /api/v1/ without JavaScript
// paints whatever a refusal answers as the page. The JSON envelope is not a
// page, so every such form's action must be a route apiError answers with
// markup (or a redirect) for a browser Accept header. This walks the template
// tree, so a new no-JS form cannot ship without a back path.

var plainFormActionPattern = regexp.MustCompile(`(?s)\{\{.*?\}\}`)

type plainFormRefusalExemption struct {
	file, action, reason string
}

// The forms below answer a refusal raised outside their handler (a stale CSRF
// token, a limiter, a transport rejection) with the JSON envelope, so a browser
// without JavaScript paints it as the page. Each needs a fixed back link in
// plainPageFormBackPath or a flash redirect. The list may only shrink. A date
// or id in an action stands for the template action the file carries there.
const plainFormRefusalGap = "WEB-135: a refusal before the handler paints the JSON envelope without JS; fix pending"

var plainFormRefusalExemptions = []plainFormRefusalExemption{
	{file: "components/settings_account.html", action: "/api/v1/users/current/oidc/identities/2026-09-27", reason: plainFormRefusalGap},
	{file: "components/settings_account.html", action: "/api/v1/users/current/oidc/link/step-up", reason: plainFormRefusalGap},
	{file: "components/settings_account.html", action: "/api/v1/users/current/password", reason: plainFormRefusalGap},
	{file: "components/settings_account.html", action: "/api/v1/users/current/password/step-up", reason: plainFormRefusalGap},
	{file: "components/settings_account.html", action: "/api/v1/users/current/profile", reason: plainFormRefusalGap},
	{file: "components/settings_account.html", action: "/api/v1/users/current/recovery-code", reason: plainFormRefusalGap},
	{file: "components/settings_cycle.html", action: "/api/v1/users/current/reminders", reason: plainFormRefusalGap},
	{file: "components/settings_danger_zone.html", action: "/api/v1/users/current", reason: plainFormRefusalGap},
	{file: "components/settings_danger_zone.html", action: "/api/v1/users/current/data-wipe", reason: plainFormRefusalGap},
	{file: "components/settings_danger_zone.html", action: "/api/v1/users/current/data-wipe/step-up", reason: plainFormRefusalGap},
	{file: "components/settings_danger_zone.html", action: "/api/v1/users/current/deletion/step-up", reason: plainFormRefusalGap},
	{file: "components/settings_egress.html", action: "/api/v1/users/current/calendar-feed", reason: plainFormRefusalGap},
	{file: "components/settings_egress.html", action: "/api/v1/users/current/calendar-feed/rotate", reason: plainFormRefusalGap},
	{file: "components/settings_egress.html", action: "/api/v1/users/current/webhook", reason: plainFormRefusalGap},
	{file: "components/settings_interface.html", action: "/api/v1/users/current/interface", reason: plainFormRefusalGap},
	{file: "components/settings_symptoms.html", action: "/api/v1/symptoms", reason: plainFormRefusalGap},
	{file: "components/settings_symptoms.html", action: "/api/v1/symptoms/2026-09-27", reason: plainFormRefusalGap},
	{file: "components/settings_symptoms.html", action: "/api/v1/symptoms/2026-09-27/restore", reason: plainFormRefusalGap},
	{file: "components/settings_tracking.html", action: "/api/v1/users/current/tracking", reason: plainFormRefusalGap},
	{file: "settings_2fa.html", action: "/api/v1/users/current/2fa", reason: plainFormRefusalGap},
}

type plainPostForm struct {
	file, action string
	line         int
}

// plainPostFormsInTemplate returns every <form method="post"> whose action is
// under /api/v1/. A template action becomes a fixed date, which every route
// shape here accepts; a multi-line one keeps its newlines for the line numbers.
func plainPostFormsInTemplate(file, source string) []plainPostForm {
	stripped := plainFormActionPattern.ReplaceAllStringFunc(source, func(action string) string {
		if strings.Contains(action, "\n") {
			return strings.Repeat("\n", strings.Count(action, "\n"))
		}
		return "2026-09-27"
	})
	var forms []plainPostForm
	line := 1
	tokenizer := html.NewTokenizer(strings.NewReader(stripped))
	for {
		kind := tokenizer.Next()
		if kind == html.ErrorToken {
			return forms
		}
		startLine := line
		line += strings.Count(string(tokenizer.Raw()), "\n")
		token := tokenizer.Token()
		if (kind != html.StartTagToken && kind != html.SelfClosingTagToken) || token.Data != "form" {
			continue
		}
		attrs := map[string]string{}
		for _, attr := range token.Attr {
			attrs[attr.Key] = attr.Val
		}
		action := attrs["action"]
		if strings.EqualFold(attrs["method"], "post") && strings.HasPrefix(action, "/api/v1/") {
			forms = append(forms, plainPostForm{file: file, action: action, line: startLine})
		}
	}
}

func TestEveryNoJSPostFormActionAnswersARefusalAsAPage(t *testing.T) {
	t.Parallel()

	var forms []plainPostForm
	err := fs.WalkDir(templates.Files, ".", func(path string, entry fs.DirEntry, err error) error {
		if err != nil || entry.IsDir() || !strings.HasSuffix(path, ".html") {
			return err
		}
		source, err := fs.ReadFile(templates.Files, path)
		if err != nil {
			return err
		}
		forms = append(forms, plainPostFormsInTemplate(path, string(source))...)
		return nil
	})
	if err != nil {
		t.Fatalf("walk templates: %v", err)
	}
	onboarding := false
	for _, form := range forms {
		onboarding = onboarding || strings.HasPrefix(form.action, "/api/v1/onboarding/steps/")
	}
	if !onboarding {
		t.Fatal("found no onboarding step form: the scan is broken, not the tree clean")
	}

	handler := newBareRefusalHandler(t)
	app := fiber.New()
	app.Use(handler.LanguageMiddleware)
	app.All("/*", func(c fiber.Ctx) error {
		// A CSRF refusal raises fiber.ErrForbidden; the app's ErrorHandler answers
		// it through RespondTransportError — the path a stale-token form takes.
		return handler.RespondTransportError(c, fiber.StatusForbidden)
	})

	used := map[int]bool{}
	var failures []string
	for _, form := range forms {
		exempt := false
		for index, exemption := range plainFormRefusalExemptions {
			if exemption.file == form.file && exemption.action == form.action {
				used[index] = true
				exempt = true
			}
		}
		if exempt {
			continue
		}
		request := httptest.NewRequest(http.MethodPost, form.action, nil)
		request.Header.Set("Accept", "text/html,application/xhtml+xml")
		response, err := app.Test(request)
		if err != nil {
			t.Fatalf("%s: request failed: %v", form.action, err)
		}
		_ = response.Body.Close()
		contentType := response.Header.Get("Content-Type")
		if response.StatusCode != http.StatusSeeOther && !strings.HasPrefix(contentType, "text/html") {
			failures = append(failures, fmt.Sprintf("%s:%d (%s): refusal answers %d %q, want text/html or 303", form.file, form.line, form.action, response.StatusCode, contentType))
		}
	}

	sort.Strings(failures)
	if len(failures) > 0 {
		t.Errorf("no-JS forms whose refusal paints the JSON envelope as the page:\n\t%s", strings.Join(failures, "\n\t"))
	}
	for index, exemption := range plainFormRefusalExemptions {
		if strings.TrimSpace(exemption.reason) == "" {
			t.Errorf("exemption %s %s has no reason", exemption.file, exemption.action)
		}
		if !used[index] {
			t.Errorf("exemption %s %s matches no form any more; delete it", exemption.file, exemption.action)
		}
	}
}

func TestPlainPostFormScanClassifiesItsOwnFixtures(t *testing.T) {
	t.Parallel()

	forms := plainPostFormsInTemplate("fixture.html", `
<form action="/api/v1/a/{{.ID}}" method="POST"></form>
<form action="/api/v1/b" method="get"></form>
<form action="/api/v1/c"></form>
<form action="/login" method="post"></form>
<form action="/api/v1/d" method="post" hx-post="/api/v1/d"></form>
`)
	if len(forms) != 2 || forms[0].action != "/api/v1/a/2026-09-27" || forms[0].line != 2 || forms[1].action != "/api/v1/d" {
		t.Fatalf("scan got %+v, want the two method=post forms under /api/v1/", forms)
	}
}
