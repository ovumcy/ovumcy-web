package main

import (
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"

	"golang.org/x/net/html"
)

// languageSwitchForms returns the csrf_token of every /lang form in a page.
func languageSwitchForms(t *testing.T, body string) []string {
	t.Helper()
	document, err := html.Parse(strings.NewReader(body))
	if err != nil {
		t.Fatalf("parse page: %v", err)
	}
	var tokens []string
	var inLangForm bool
	var walk func(node *html.Node)
	walk = func(node *html.Node) {
		attr := func(key string) string {
			for _, a := range node.Attr {
				if a.Key == key {
					return a.Val
				}
			}
			return ""
		}
		entered := false
		if node.Type == html.ElementNode && node.Data == "form" && attr("action") == "/lang" {
			inLangForm, entered = true, true
			tokens = append(tokens, "")
		}
		if inLangForm && node.Type == html.ElementNode && node.Data == "input" && attr("name") == "csrf_token" {
			tokens[len(tokens)-1] = attr("value")
		}
		for child := node.FirstChild; child != nil; child = child.NextSibling {
			walk(child)
		}
		if entered {
			inLangForm = false
		}
	}
	walk(document)
	return tokens
}

// TestCSRFRefusalPageOffersNoLanguageSwitchItCannotHonour (WEB-293): the CSRF
// 403 on a browser's /lang post renders the refusal page in the full layout,
// whose header carries the /lang form. The middleware refused the request, so
// no token reaches the page: a switcher there would post an empty token and be
// refused again, a loop broken only by the back link.
func TestCSRFRefusalPageOffersNoLanguageSwitchItCannotHonour(t *testing.T) {
	app := newCSRFGuardTestApp(t)
	request := httptest.NewRequest(http.MethodPost, "/lang", strings.NewReader(url.Values{"lang": {"ru"}, "next": {"/login"}}.Encode()))
	request.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	request.Header.Set("Accept", "text/html,application/xhtml+xml")
	response, err := app.Test(request, testConfigNoTimeout)
	if err != nil {
		t.Fatalf("POST /lang: %v", err)
	}
	body := string(mustReadAll(t, response))
	_ = response.Body.Close()
	if response.StatusCode != http.StatusForbidden {
		t.Fatalf("POST /lang without a token answered %d, want the CSRF 403", response.StatusCode)
	}
	if !strings.Contains(body, "data-page-form-refusal") {
		t.Fatalf("the CSRF 403 is not the refusal page: %q", body)
	}
	if tokens := languageSwitchForms(t, body); len(tokens) != 0 {
		t.Fatalf("the refusal page offers %d language switch form(s) with tokens %q; the request carried none, so every button would be refused again", len(tokens), tokens)
	}
}
