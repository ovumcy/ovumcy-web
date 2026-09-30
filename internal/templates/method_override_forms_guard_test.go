package templates

import (
	"fmt"
	"io/fs"
	"regexp"
	"sort"
	"strings"
	"testing"

	"golang.org/x/net/html"
)

// WEB-107. A form that htmx submits as PUT, PATCH or DELETE but that also
// declares method="post" for the no-JavaScript path posts, without JS, to a URL
// that either has no POST route (405) or has one that does something else — the
// calendar-feed revoke form used to regenerate the feed. The server honours a
// hidden `_method` field on a urlencoded POST (api.MethodOverride), so every
// such form must carry one naming its htmx verb, submit to the same URL, and
// stay urlencoded. This guard holds the whole template tree to that, so a new
// form cannot reintroduce the class.
//
// A form with an htmx verb and no method="post" is the same class one step
// worse: without JavaScript it submits as GET to the page it is on, putting
// every field (a password included) in the query string. It fails here too,
// and so does an hx-post form without method="post" and an action equal to its
// hx-post URL; such a form needs no _method and must not carry one.

// methodOverrideExemption names one form the guard skips, and why.
type methodOverrideExemption struct {
	file   string
	verb   string
	url    string
	reason string
}

var methodOverrideExemptions = []methodOverrideExemption{
	{
		file: "components/settings_egress.html", verb: "DELETE", url: "/api/v1/users/current/webhook",
		reason: "its no-JS POST deliberately reaches the save endpoint, which removes the destination when the form's hidden webhook_remove_url is set",
	},
	// The PATCH forms below fail closed without JS today (405: only PATCH is
	// registered for their URLs). They are outside WEB-107's six forms and are
	// tracked as WEB-119: each needs only the hidden _method input, since PATCH
	// is already allowlisted. The list may only shrink.
	{file: "components/settings_account.html", verb: "PATCH", url: "/api/v1/users/current/profile", reason: "WEB-119: no-JS 405; add the hidden _method input"},
	{file: "components/settings_interface.html", verb: "PATCH", url: "/api/v1/users/current/interface", reason: "WEB-119: no-JS 405; add the hidden _method input"},
	{file: "components/settings_tracking.html", verb: "PATCH", url: "/api/v1/users/current/tracking", reason: "WEB-119: no-JS 405; add the hidden _method input"},
	{file: "components/settings_cycle.html", verb: "PATCH", url: "/api/v1/users/current/cycle", reason: "WEB-119: no-JS 405; add the hidden _method input"},
	{file: "components/settings_cycle.html", verb: "PATCH", url: "/api/v1/users/current/reminders", reason: "WEB-119: no-JS 405; add the hidden _method input"},
	{file: "components/settings_symptoms.html", verb: "PATCH", url: "/api/v1/symptoms/{{.Symptom.ID}}", reason: "WEB-119: no-JS 405; add the hidden _method input"},
	{file: "dashboard.html", verb: "PATCH", url: "/api/v1/users/current/cycle", reason: "WEB-119: no-JS 405; add the hidden _method input"},
}

// overrideForm is one <form> that declares an htmx verb.
type overrideForm struct {
	line      int
	verb      string
	hxURL     string
	action    string
	multipart bool
	post      bool     // method="post" is declared; anything else is a GET without JS
	methods   []string // values of every hidden _method input inside the form
}

var templateActionPattern = regexp.MustCompile(`(?s)\{\{.*?\}\}`)

// overrideFormsInTemplate returns every form with an hx-put, hx-patch,
// hx-delete or hx-post attribute, whatever its method; a form with more than
// one is classified by the first in that order. Template actions are replaced first
// by placeholders — the same text always by the same placeholder, so an action
// URL and an hx URL built from the same expression still compare equal —
// because an action such as {{t .Messages "key"}} puts double quotes inside a
// quoted attribute.
func overrideFormsInTemplate(source string) []overrideForm {
	placeholders := map[string]string{}
	originals := map[string]string{}
	stripped := templateActionPattern.ReplaceAllStringFunc(source, func(action string) string {
		placeholder, ok := placeholders[action]
		if !ok {
			placeholder = fmt.Sprintf("TPLACTION%dX", len(placeholders))
			placeholders[action] = placeholder
			originals[placeholder] = action
		}
		// A multi-line action (a template comment) keeps its newlines so the
		// reported line numbers stay the template's own.
		return placeholder + strings.Repeat("\n", strings.Count(action, "\n"))
	})
	restore := func(value string) string {
		for placeholder, action := range originals {
			value = strings.ReplaceAll(value, placeholder, action)
		}
		return value
	}

	var forms []overrideForm
	var current *overrideForm
	line := 1
	tokenizer := html.NewTokenizer(strings.NewReader(stripped))
	for {
		kind := tokenizer.Next()
		if kind == html.ErrorToken {
			break
		}
		raw := string(tokenizer.Raw())
		token := tokenizer.Token()
		startLine := line
		line += strings.Count(raw, "\n")

		switch {
		case (kind == html.StartTagToken || kind == html.SelfClosingTagToken) && token.Data == "form":
			attrs := tokenAttrs(token)
			current = nil
			for _, verb := range []string{"put", "patch", "delete", "post"} {
				if url, ok := attrs["hx-"+verb]; ok {
					forms = append(forms, overrideForm{
						line:      startLine,
						verb:      strings.ToUpper(verb),
						hxURL:     restore(url),
						action:    restore(attrs["action"]),
						post:      strings.EqualFold(attrs["method"], "post"),
						multipart: strings.Contains(strings.ToLower(attrs["enctype"]), "multipart"),
					})
					current = &forms[len(forms)-1]
					break
				}
			}
		case kind == html.EndTagToken && token.Data == "form":
			current = nil
		case (kind == html.StartTagToken || kind == html.SelfClosingTagToken) && token.Data == "input" && current != nil:
			attrs := tokenAttrs(token)
			if attrs["name"] == "_method" && strings.EqualFold(attrs["type"], "hidden") {
				current.methods = append(current.methods, attrs["value"])
			}
		}
	}
	return forms
}

func tokenAttrs(token html.Token) map[string]string {
	attrs := make(map[string]string, len(token.Attr))
	for _, attr := range token.Attr {
		attrs[attr.Key] = attr.Val
	}
	return attrs
}

// overrideFormProblem reports what is wrong with form, or "" when nothing is.
func overrideFormProblem(form overrideForm) string {
	var problems []string
	if !form.post {
		problems = append(problems, "declares no method=\"post\": without JS it submits as GET to the current page and puts its fields in the query string")
	}
	if form.verb == "POST" {
		if len(form.methods) != 0 {
			problems = append(problems, fmt.Sprintf("must carry no _method: its no-JS POST already is the htmx verb, has %q", form.methods))
		}
	} else if len(form.methods) != 1 || form.methods[0] != form.verb {
		problems = append(problems, fmt.Sprintf("needs exactly one <input type=\"hidden\" name=\"_method\" value=\"%s\">, has %q", form.verb, form.methods))
	}
	if form.action != form.hxURL {
		problems = append(problems, fmt.Sprintf("action %q must equal hx-%s %q", form.action, strings.ToLower(form.verb), form.hxURL))
	}
	if form.multipart && form.verb != "POST" {
		problems = append(problems, "must stay urlencoded: _method is not read from a multipart body")
	}
	return strings.Join(problems, "; ")
}

func TestEveryPostFormWithAnHTMXVerbCarriesTheMatchingMethodOverride(t *testing.T) {
	used := map[int]bool{}
	var failures []string
	scanned := 0

	err := fs.WalkDir(Files, ".", func(path string, entry fs.DirEntry, err error) error {
		if err != nil || entry.IsDir() || !strings.HasSuffix(path, ".html") {
			return err
		}
		source, err := fs.ReadFile(Files, path)
		if err != nil {
			return err
		}
		for _, form := range overrideFormsInTemplate(string(source)) {
			scanned++
			exempt := false
			for index, exemption := range methodOverrideExemptions {
				if exemption.file == path && exemption.verb == form.verb && exemption.url == form.hxURL {
					used[index] = true
					exempt = true
				}
			}
			if exempt {
				continue
			}
			if problem := overrideFormProblem(form); problem != "" {
				failures = append(failures, fmt.Sprintf("%s:%d (hx-%s %s): %s", path, form.line, strings.ToLower(form.verb), form.hxURL, problem))
			}
		}
		return nil
	})
	if err != nil {
		t.Fatalf("walk templates: %v", err)
	}
	if scanned == 0 {
		t.Fatal("scanned no form with an htmx verb: the scan is broken, not the tree clean")
	}

	var stale []string
	for index, exemption := range methodOverrideExemptions {
		if strings.TrimSpace(exemption.reason) == "" {
			t.Errorf("exemption %s hx-%s %s has no reason", exemption.file, strings.ToLower(exemption.verb), exemption.url)
		}
		if !used[index] {
			stale = append(stale, fmt.Sprintf("%s hx-%s %s", exemption.file, strings.ToLower(exemption.verb), exemption.url))
		}
	}
	sort.Strings(failures)
	if len(failures) > 0 {
		t.Errorf("forms with an htmx verb that a no-JS browser would submit wrongly:\n\t%s", strings.Join(failures, "\n\t"))
	}
	if len(stale) > 0 {
		t.Errorf("exemptions that match no form any more; delete them:\n\t%s", strings.Join(stale, "\n\t"))
	}
}

// TestMethodOverrideFormScanClassifiesItsOwnFixtures anchors the guard on inputs
// this file owns, so a scan that silently stopped finding forms or fields
// cannot pass for a clean tree.
func TestMethodOverrideFormScanClassifiesItsOwnFixtures(t *testing.T) {
	fixture := `
<form action="/a/{{.ID}}" method="post" hx-delete="/a/{{.ID}}" data-confirm-accept="{{t .Messages "x.y"}}">
  <input type="hidden" name="_method" value="DELETE">
</form>
<form action="/b" method="POST" hx-put="/b">
  <input type="hidden" name="csrf_token" value="{{.CSRFToken}}">
</form>
<form action="/c" method="post" hx-patch="/c">
  <input type="hidden" name="_method" value="PUT">
</form>
<form method="post" hx-delete="/d">
  <input type="hidden" name="_method" value="DELETE">
</form>
<form action="/e" method="post" hx-delete="/e" enctype="multipart/form-data">
  <input type="hidden" name="_method" value="DELETE">
</form>
<form action="/f" method="post" hx-put="/f">
  <input type="hidden" name="_method" value="PUT">
  <input type="hidden" name="_method" value="PUT">
</form>
<form action="/g" hx-delete="/g"></form>
<form action="/i" method="get" hx-put="/i">
  <input type="hidden" name="_method" value="PUT">
</form>
<form action="/h?source=x" method="post" hx-post="/h?source=x"></form>
<form hx-post="/j"></form>
<form method="post" hx-post="/k"></form>
<form action="/l" hx-post="/l"></form>
<form action="/m-other" method="post" hx-post="/m"></form>
<form action="/n" method="post" hx-post="/n">
  <input type="hidden" name="_method" value="DELETE">
</form>
<form action="/o" method="post" hx-post="/o" enctype="multipart/form-data"></form>
<form action="/p" method="post" hx-delete="/p" hx-post="/p">
  <input type="hidden" name="_method" value="DELETE">
</form>
<form action="/q" method="post"></form>
<input type="hidden" name="_method" value="DELETE">
`
	forms := overrideFormsInTemplate(fixture)
	want := []struct {
		hxURL string
		ok    bool
	}{
		{"/a/{{.ID}}", true},
		{"/b", false}, // no _method
		{"/c", false}, // wrong verb
		{"/d", false}, // no action
		{"/e", false}, // multipart
		{"/f", false}, // repeated
		{"/g", false}, // no method: a GET without JS, even with no _method to send
		{"/i", false}, // method="get", even with the field
		{"/h?source=x", true},
		{"/j", false}, // hx-post alone: a GET to the current page without JS
		{"/k", false}, // hx-post, no action
		{"/l", false}, // hx-post, no method
		{"/m", false}, // hx-post, action elsewhere
		{"/n", false}, // hx-post with a _method that would turn it into DELETE
		{"/o", true},  // hx-post needs no _method, so multipart is fine
		{"/p", true},  // hx-delete wins over hx-post
	}
	if len(forms) != len(want) {
		t.Fatalf("scan found %d forms, want %d (the form without an htmx verb and the stray input must be ignored): %+v", len(forms), len(want), forms)
	}
	if forms[len(forms)-1].verb != "DELETE" {
		t.Errorf("form /p classified as %s, want DELETE", forms[len(forms)-1].verb)
	}
	for index, expected := range want {
		form := forms[index]
		if form.hxURL != expected.hxURL {
			t.Fatalf("form %d: hx URL %q, want %q", index, form.hxURL, expected.hxURL)
		}
		if problem := overrideFormProblem(form); (problem == "") != expected.ok {
			t.Errorf("form %s: ok=%v, want %v (problem %q)", expected.hxURL, problem == "", expected.ok, problem)
		}
	}
	if forms[0].line != 2 || forms[1].line != 5 {
		t.Errorf("form lines %d and %d, want 2 and 5", forms[0].line, forms[1].line)
	}
}
