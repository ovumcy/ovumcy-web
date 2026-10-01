package api

import (
	"fmt"
	"io"
	"io/fs"
	"net/http"
	"net/http/httptest"
	"regexp"
	"sort"
	"strconv"
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
//
// The probe sends the request the browser sends: a form that names PATCH, PUT
// or DELETE in a hidden `_method` field goes through the real override
// middleware, so a refusal raised by CSRF or a limiter sees the overridden verb
// exactly as it does in production.

var (
	plainFormActionPattern = regexp.MustCompile(`(?s)\{\{.*?\}\}`)
	plainFormFieldAction   = regexp.MustCompile(`^\{\{-?\s*\.(\w+)\s*-?\}\}$`)
	plainFormControlAction = regexp.MustCompile(`^\{\{-?\s*(if|else|end|range|with|template|block|define)\b`)
	plainFormDefineName    = regexp.MustCompile(`^\{\{-?\s*define\s+"([^"]+)"`)
	plainFormBlockOpen     = regexp.MustCompile(`^\{\{-?\s*(if|range|with|block)\b`)
	plainFormBlockEnd      = regexp.MustCompile(`^\{\{-?\s*end\b`)
	plainFormPlaceholder   = regexp.MustCompile(`TPLACTION(\d+)X`)
	plainFormPrintfVerb    = regexp.MustCompile(`%[sdv]`)
)

// plainFormSampleDate stands for every template action inside an action: a
// date is accepted by every route shape under test.
const plainFormSampleDate = "2026-09-27"

type plainFormRefusalExemption struct {
	file, action, reason string
}

// The forms below answer a refusal raised outside their handler (a stale CSRF
// token, a limiter, a transport rejection) with the JSON envelope, so a browser
// without JavaScript paints it as the page. Each needs a fixed back link in
// plainPageFormBackPath or a flash redirect. The list may only shrink, and an
// entry whose form now answers a page fails the test until it is deleted. A
// date or id in an action stands for the template action the file carries there.
const plainFormRefusalGap = "WEB-135: a refusal before the handler paints the JSON envelope without JS; fix pending"

var plainFormRefusalExemptions = []plainFormRefusalExemption{
	{file: "components/settings_account.html", action: "/api/v1/users/current/oidc/identities/2026-09-27", reason: plainFormRefusalGap},
	{file: "components/settings_account.html", action: "/api/v1/users/current/oidc/link/step-up", reason: plainFormRefusalGap},
	{file: "components/settings_account.html", action: "/api/v1/users/current/password", reason: plainFormRefusalGap},
	{file: "components/settings_account.html", action: "/api/v1/users/current/password/step-up", reason: plainFormRefusalGap},
	{file: "components/settings_account.html", action: "/api/v1/users/current/profile", reason: plainFormRefusalGap},
	{file: "components/settings_account.html", action: "/api/v1/users/current/recovery-code", reason: plainFormRefusalGap},
	{file: "components/settings_danger_zone.html", action: "/api/v1/users/current", reason: plainFormRefusalGap},
	{file: "components/settings_danger_zone.html", action: "/api/v1/users/current/data-wipe", reason: plainFormRefusalGap},
	{file: "components/settings_danger_zone.html", action: "/api/v1/users/current/data-wipe/step-up", reason: plainFormRefusalGap},
	{file: "components/settings_danger_zone.html", action: "/api/v1/users/current/deletion/step-up", reason: plainFormRefusalGap},
	{file: "components/settings_egress.html", action: "/api/v1/users/current/calendar-feed", reason: plainFormRefusalGap},
	{file: "components/settings_egress.html", action: "/api/v1/users/current/calendar-feed/rotate", reason: plainFormRefusalGap},
	{file: "components/settings_egress.html", action: "/api/v1/users/current/webhook", reason: plainFormRefusalGap},
	{file: "settings_2fa.html", action: "/api/v1/users/current/2fa", reason: plainFormRefusalGap},
}

// plainPostForm is one <form method="post"> the scan reports. An action it could
// not resolve is kept with the reason, never dropped: the test fails on it.
type plainPostForm struct {
	file, action string
	override     string // the hidden _method value the form carries, if any
	line         int
	unresolved   string
}

// plainPostFormsInSource returns every <form method="post"> of sources[file]
// that submits to /api/v1/. A template action inside an action becomes a fixed
// date, which every route shape here accepts. A leading action (a base path)
// is dropped, and an action that is one field of a template's own data
// ({{.Endpoint}}) is resolved through the literal values the template's call
// sites pass for it. An action the scan cannot resolve comes back with its
// reason; only a literal path that is not under /api/v1/ is left out.
func plainPostFormsInSource(file string, sources map[string]string) []plainPostForm {
	originals := map[string]string{}
	stripped := plainFormActionPattern.ReplaceAllStringFunc(sources[file], func(action string) string {
		placeholder := fmt.Sprintf("TPLACTION%dX", len(originals))
		originals[placeholder] = action
		// A multi-line action keeps its newlines for the line numbers.
		return placeholder + strings.Repeat("\n", strings.Count(action, "\n"))
	})
	restore := func(value string) string {
		return plainFormPlaceholder.ReplaceAllStringFunc(value, func(placeholder string) string {
			return originals[strings.TrimSpace(placeholder)]
		})
	}

	var forms []plainPostForm
	var open []int // indexes into forms of the form being read
	line, offset := 1, 0
	tokenizer := html.NewTokenizer(strings.NewReader(stripped))
	for {
		kind := tokenizer.Next()
		if kind == html.ErrorToken {
			return forms
		}
		startLine, startOffset := line, offset
		raw := tokenizer.Raw()
		line += strings.Count(string(raw), "\n")
		offset += len(raw)
		token := tokenizer.Token()
		isStart := kind == html.StartTagToken || kind == html.SelfClosingTagToken
		switch {
		case kind == html.EndTagToken && token.Data == "form":
			open = nil
		case isStart && token.Data == "input" && len(open) > 0:
			attrs := plainFormAttrs(token)
			if attrs["name"] == "_method" && strings.EqualFold(attrs["type"], "hidden") {
				for _, index := range open {
					forms[index].override = strings.ToUpper(strings.TrimSpace(attrs["value"]))
				}
			}
		case isStart && token.Data == "form":
			open = nil
			attrs := plainFormAttrs(token)
			if !strings.EqualFold(attrs["method"], "post") {
				continue
			}
			rawAction := restore(attrs["action"])
			enclosing := enclosingPlainFormDefine(stripped[:startOffset], originals)
			actions, problem := resolvePlainFormAction(enclosing, rawAction, sources)
			if problem != "" {
				forms = append(forms, plainPostForm{file: file, action: rawAction, line: startLine, unresolved: problem})
				open = append(open, len(forms)-1)
				continue
			}
			for _, action := range actions {
				forms = append(forms, plainPostForm{file: file, action: action, line: startLine})
				open = append(open, len(forms)-1)
			}
		}
	}
}

func plainFormAttrs(token html.Token) map[string]string {
	attrs := make(map[string]string, len(token.Attr))
	for _, attr := range token.Attr {
		attrs[attr.Key] = attr.Val
	}
	return attrs
}

// enclosingPlainFormDefine names the template definition the text before a form
// sits in, or "" outside any: the nearest {{define "name"}} in prefix that no
// {{end}} has closed. Every block action (if, range, with, block) opens a level
// and every {{end}} closes the innermost one.
func enclosingPlainFormDefine(prefix string, originals map[string]string) string {
	var open []string
	for _, placeholder := range plainFormPlaceholder.FindAllString(prefix, -1) {
		action := originals[placeholder]
		switch match := plainFormDefineName.FindStringSubmatch(action); {
		case match != nil:
			open = append(open, match[1])
		case plainFormBlockOpen.MatchString(action):
			open = append(open, "")
		case plainFormBlockEnd.MatchString(action) && len(open) > 0:
			open = open[:len(open)-1]
		}
	}
	for index := len(open) - 1; index >= 0; index-- {
		if open[index] != "" {
			return open[index]
		}
	}
	return ""
}

// resolvePlainFormAction turns one action as written into the paths to probe.
// It returns no path and no problem only for a literal path outside /api/v1/.
func resolvePlainFormAction(enclosing, raw string, sources map[string]string) ([]string, string) {
	if strings.TrimSpace(raw) == "" {
		return nil, "the form declares no action"
	}
	for _, piece := range plainFormActionPattern.FindAllString(raw, -1) {
		if plainFormControlAction.MatchString(piece) {
			return nil, fmt.Sprintf("action %q branches on a template condition, which the scan cannot resolve", raw)
		}
	}
	if field := plainFormFieldAction.FindStringSubmatch(raw); field != nil {
		return resolvePlainFormField(enclosing, field[1], raw, sources)
	}
	rest := raw
	for strings.HasPrefix(rest, "{{") {
		end := strings.Index(rest, "}}")
		if end < 0 {
			break
		}
		rest = rest[end+2:]
	}
	return classifyPlainFormAction(raw, plainFormActionPattern.ReplaceAllString(rest, plainFormSampleDate))
}

func classifyPlainFormAction(raw, resolved string) ([]string, string) {
	switch {
	case strings.HasPrefix(resolved, "/api/v1/"):
		return []string{resolved}, ""
	case strings.HasPrefix(resolved, "/"):
		return nil, ""
	}
	return nil, fmt.Sprintf("action %q resolves to %q, which is neither a path nor a base path followed by one", raw, resolved)
}

// resolvePlainFormField resolves an action that is one field of the data its
// template is called with. Every call site of the enclosing template must pass
// the field as a string or printf literal; one that does not is reported, so a
// call site that starts passing a computed value cannot hide a form.
func resolvePlainFormField(enclosing, field, raw string, sources map[string]string) ([]string, string) {
	if enclosing == "" {
		return nil, fmt.Sprintf("action %q is a field of the page's own data, not of a template call, so the scan cannot resolve it", raw)
	}
	call := regexp.MustCompile(`(?s)\{\{-?\s*template\s+` + regexp.QuoteMeta(strconv.Quote(enclosing)) + `\s*\(dict(.*?)\)\s*-?\}\}`)
	value := regexp.MustCompile(`"` + regexp.QuoteMeta(field) + `"\s+(?:\(printf\s+"([^"]*)"|"([^"]*)")`)
	paths := make([]string, 0, len(sources))
	for path := range sources {
		paths = append(paths, path)
	}
	sort.Strings(paths)

	var resolved []string
	calls := 0
	for _, path := range paths {
		for _, site := range call.FindAllStringSubmatch(sources[path], -1) {
			calls++
			match := value.FindStringSubmatch(site[1])
			if match == nil {
				return nil, fmt.Sprintf("action %q: a call of template %q in %s passes %s as something other than a string or printf literal", raw, enclosing, path, field)
			}
			literal := match[2]
			if match[1] != "" {
				literal = plainFormPrintfVerb.ReplaceAllString(match[1], plainFormSampleDate)
			}
			actions, problem := classifyPlainFormAction(raw, literal)
			if problem != "" {
				return nil, problem
			}
			resolved = append(resolved, actions...)
		}
	}
	if calls == 0 {
		return nil, fmt.Sprintf("action %q: no call of template %q was found, so the scan cannot resolve %s", raw, enclosing, field)
	}
	return resolved, ""
}

// plainFormRefusalProblems judges every form: an unresolved action fails, a
// form that is not exempt must answer a page, an exempt form must still answer
// the JSON envelope (one that now answers a page keeps no entry), and an entry
// matching no form or carrying no reason fails.
func plainFormRefusalProblems(forms []plainPostForm, exemptions []plainFormRefusalExemption, answersAPage func(plainPostForm) (bool, string)) []string {
	used := map[int]bool{}
	var problems []string
	for _, form := range forms {
		if form.unresolved != "" {
			problems = append(problems, fmt.Sprintf("%s:%d: the scan cannot resolve this form: %s", form.file, form.line, form.unresolved))
			continue
		}
		exempt := false
		for index, exemption := range exemptions {
			if exemption.file == form.file && exemption.action == form.action {
				used[index] = true
				exempt = true
			}
		}
		page, answer := answersAPage(form)
		switch {
		case !exempt && !page:
			problems = append(problems, fmt.Sprintf("%s:%d (%s): refusal answers %s, want text/html or 303", form.file, form.line, form.action, answer))
		case exempt && page:
			problems = append(problems, fmt.Sprintf("%s:%d (%s): exempt, but its refusal now answers %s; delete the exemption", form.file, form.line, form.action, answer))
		}
	}
	sort.Strings(problems)
	for index, exemption := range exemptions {
		if strings.TrimSpace(exemption.reason) == "" {
			problems = append(problems, fmt.Sprintf("exemption %s %s has no reason", exemption.file, exemption.action))
		}
		if !used[index] {
			problems = append(problems, fmt.Sprintf("exemption %s %s matches no form any more; delete it", exemption.file, exemption.action))
		}
	}
	return problems
}

func TestEveryNoJSPostFormActionAnswersARefusalAsAPage(t *testing.T) {
	t.Parallel()

	sources := map[string]string{}
	err := fs.WalkDir(templates.Files, ".", func(path string, entry fs.DirEntry, err error) error {
		if err != nil || entry.IsDir() || !strings.HasSuffix(path, ".html") {
			return err
		}
		source, err := fs.ReadFile(templates.Files, path)
		if err != nil {
			return err
		}
		sources[path] = string(source)
		return nil
	})
	if err != nil {
		t.Fatalf("walk templates: %v", err)
	}
	var forms []plainPostForm
	for path := range sources {
		forms = append(forms, plainPostFormsInSource(path, sources)...)
	}
	sort.Slice(forms, func(i, j int) bool {
		if forms[i].file != forms[j].file {
			return forms[i].file < forms[j].file
		}
		if forms[i].line != forms[j].line {
			return forms[i].line < forms[j].line
		}
		return forms[i].action < forms[j].action
	})

	// The load-bearing sites, by name: a scan that lost the onboarding forms, the
	// forms whose action is a template field, or the hidden override field would
	// pass for a clean tree.
	seen := map[string]bool{}
	for _, form := range forms {
		seen[form.file+" "+form.action+" "+form.override] = true
	}
	for _, anchor := range []string{
		"onboarding.html /api/v1/onboarding/steps/1 ",
		"components/cycle_start_form.html /api/v1/days/2026-09-27/cycle-start?source=dashboard ",
		"components/cycle_start_form.html /api/v1/days/2026-09-27/cycle-start?source=calendar ",
		"dashboard.html /api/v1/users/current/cycle?source=dashboard PATCH",
		"day_editor_partial.html /api/v1/days/2026-09-27?source=calendar PUT",
		"day_editor_partial.html /api/v1/days/2026-09-27?source=calendar DELETE",
		"components/settings_interface.html /api/v1/users/current/interface PATCH",
		"components/settings_symptoms.html /api/v1/symptoms/2026-09-27 DELETE",
	} {
		if !seen[anchor] {
			t.Fatalf("the scan found no form %q: the scan is broken, not the tree clean", anchor)
		}
	}

	handler := newBareRefusalHandler(t)
	app := fiber.New()
	app.Use(MethodOverride(handler))
	app.Use(handler.LanguageMiddleware)
	app.All("/*", func(c fiber.Ctx) error {
		// A CSRF refusal raises fiber.ErrForbidden; the app's ErrorHandler answers
		// it through RespondTransportError — the path a stale-token form takes.
		return handler.RespondTransportError(c, fiber.StatusForbidden)
	})
	answersAPage := func(form plainPostForm) (bool, string) {
		var body io.Reader
		if form.override != "" {
			body = strings.NewReader("_method=" + form.override)
		}
		request := httptest.NewRequest(http.MethodPost, form.action, body)
		if body != nil {
			request.Header.Set("Content-Type", "application/x-www-form-urlencoded")
		}
		request.Header.Set("Accept", "text/html,application/xhtml+xml")
		response, err := app.Test(request)
		if err != nil {
			t.Fatalf("%s: request failed: %v", form.action, err)
		}
		_ = response.Body.Close()
		contentType := response.Header.Get("Content-Type")
		page := response.StatusCode == http.StatusSeeOther || strings.HasPrefix(contentType, "text/html")
		return page, fmt.Sprintf("%d %q", response.StatusCode, contentType)
	}

	if problems := plainFormRefusalProblems(forms, plainFormRefusalExemptions, answersAPage); len(problems) > 0 {
		t.Errorf("no-JS forms whose refusal does not match the exemption list:\n\t%s", strings.Join(problems, "\n\t"))
	}
}

func TestPlainPostFormScanClassifiesItsOwnFixtures(t *testing.T) {
	t.Parallel()

	sources := map[string]string{
		"fixture.html": `
<form action="/api/v1/a/{{.ID}}" method="POST"></form>
<form action="/api/v1/b" method="get"></form>
<form action="/api/v1/c"></form>
<form action="/login" method="post"></form>
<form action="/api/v1/d" method="post" hx-post="/api/v1/d"></form>
<form action="{{.Base}}/api/v1/e" method="post"></form>
{{define "caller_form"}}
<form action="{{.Endpoint}}" method="post"></form>
{{end}}
<form action="/api/v1/f/{{.ID}}" method="post">
  <input type="hidden" name="_method" value="patch">
  <input type="hidden" name="other" value="x">
</form>
<form action="{{.Endpoint}}" method="post"></form>
{{define "missing_key"}}
<form action="{{.Endpoint}}" method="post"></form>
{{end}}
{{define "computed"}}
<form action="{{.Endpoint}}" method="post"></form>
{{end}}
{{define "orphan"}}
<form action="{{.Endpoint}}" method="post"></form>
{{end}}
{{define "branching"}}
<form action="{{if .X}}/api/v1/g{{else}}/api/v1/h{{end}}" method="post"></form>
<form method="post"></form>
<form action="https://example.test/x" method="post"></form>
{{end}}
`,
		"caller.html": `
{{template "caller_form" (dict "Other" "x" "Endpoint" (printf "/api/v1/days/%s/cycle-start?source=calendar" .Date) "Last" 1)}}
{{template "caller_form" (dict "Endpoint" "/api/v1/plain")}}
{{template "missing_key" (dict "Other" "x")}}
{{template "computed" (dict "Endpoint" .Computed)}}
`,
	}
	type scanned struct {
		action, override string
		line             int
		unresolved       bool
	}
	var got []scanned
	var reasons []string
	for _, form := range plainPostFormsInSource("fixture.html", sources) {
		got = append(got, scanned{form.action, form.override, form.line, form.unresolved != ""})
		if form.unresolved != "" {
			reasons = append(reasons, form.unresolved)
		}
	}
	wantReasons := []string{
		"not of a template call",
		"passes Endpoint as something other than a string or printf literal",
		"passes Endpoint as something other than a string or printf literal",
		"no call of template \"orphan\" was found",
		"branches on a template condition",
		"declares no action",
		"neither a path nor a base path",
	}
	if len(reasons) != len(wantReasons) {
		t.Fatalf("unresolved reasons %q, want %d", reasons, len(wantReasons))
	}
	for index, fragment := range wantReasons {
		if !strings.Contains(reasons[index], fragment) {
			t.Errorf("unresolved form %d: reason %q, want one holding %q", index, reasons[index], fragment)
		}
	}
	want := []scanned{
		{"/api/v1/a/2026-09-27", "", 2, false},
		{"/api/v1/d", "", 6, false},
		{"/api/v1/e", "", 7, false},
		{"/api/v1/days/2026-09-27/cycle-start?source=calendar", "", 9, false},
		{"/api/v1/plain", "", 9, false},
		{"/api/v1/f/2026-09-27", "PATCH", 11, false},
		// A field of the page's own data, outside any definition.
		{"{{.Endpoint}}", "", 15, true},
		// A call site that omits the key.
		{"{{.Endpoint}}", "", 17, true},
		// A call site that passes a computed value.
		{"{{.Endpoint}}", "", 20, true},
		// No call site at all.
		{"{{.Endpoint}}", "", 23, true},
		{"{{if .X}}/api/v1/g{{else}}/api/v1/h{{end}}", "", 26, true},
		// No action.
		{"", "", 27, true},
		{"https://example.test/x", "", 28, true},
	}
	if len(got) != len(want) {
		t.Fatalf("scan got %d forms %+v, want %d %+v", len(got), got, len(want), want)
	}
	for index := range want {
		if got[index] != want[index] {
			t.Errorf("form %d: got %+v, want %+v", index, got[index], want[index])
		}
	}
}

func TestPlainFormRefusalProblemsJudgesExemptionsBothWays(t *testing.T) {
	t.Parallel()

	form := func(action string) plainPostForm { return plainPostForm{file: "f.html", action: action, line: 3} }
	answers := func(pages ...string) func(plainPostForm) (bool, string) {
		return func(form plainPostForm) (bool, string) {
			for _, page := range pages {
				if form.action == page {
					return true, "303"
				}
			}
			return false, "403 application/json"
		}
	}
	exemption := func(action, reason string) plainFormRefusalExemption {
		return plainFormRefusalExemption{file: "f.html", action: action, reason: reason}
	}

	cases := []struct {
		name       string
		forms      []plainPostForm
		exemptions []plainFormRefusalExemption
		pages      []string
		want       string // a fragment every problem list must hold; "" means none
	}{
		{"page-shaped form", []plainPostForm{form("/a")}, nil, []string{"/a"}, ""},
		{"JSON refusal with no entry", []plainPostForm{form("/a")}, nil, nil, "refusal answers 403"},
		{"exempt form still answers JSON", []plainPostForm{form("/a")}, []plainFormRefusalExemption{exemption("/a", "gap")}, nil, ""},
		{"exempt form now answers a page", []plainPostForm{form("/a")}, []plainFormRefusalExemption{exemption("/a", "gap")}, []string{"/a"}, "delete the exemption"},
		{"entry matches no form", []plainPostForm{form("/a")}, []plainFormRefusalExemption{exemption("/gone", "gap")}, []string{"/a"}, "matches no form any more"},
		{"entry has no reason", []plainPostForm{form("/a")}, []plainFormRefusalExemption{exemption("/a", " ")}, nil, "has no reason"},
		{"unresolved form", []plainPostForm{{file: "f.html", action: "{{.X}}", line: 3, unresolved: "why"}}, nil, nil, "cannot resolve this form: why"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			problems := strings.Join(plainFormRefusalProblems(tc.forms, tc.exemptions, answers(tc.pages...)), "\n")
			if tc.want == "" && problems != "" {
				t.Fatalf("problems %q, want none", problems)
			}
			if tc.want != "" && !strings.Contains(problems, tc.want) {
				t.Fatalf("problems %q, want one holding %q", problems, tc.want)
			}
		})
	}
}
