package api

import (
	"net/http"
	"net/url"
	"strings"
	"testing"

	"golang.org/x/net/html"
)

// TestRefusalPagePastTheCSRFCheckKeepsAWorkingLanguageSwitch is the other side
// of WEB-293: a refusal the handler produced passed the CSRF check, so the page
// still offers the language switch, carrying the token the request was issued.
func TestRefusalPagePastTheCSRFCheckKeepsAWorkingLanguageSwitch(t *testing.T) {
	t.Parallel()

	ctx := newRefusalPageContext(t, "refusal-lang-switch@example.com")
	_, iso := noJSDay()
	form := renderNoJSForm(t, ctx.app, "/calendar/day/"+iso+"?mode=edit", authCookieMap(t, ctx.authCookie), formWithFlag("data-day-editor-form"))

	response := form.submit(t, ctx.app, url.Values{"is_period": {"true"}, "mood": {"abc"}})
	body := mustReadBodyString(t, response.Body)
	assertStatusCode(t, response, http.StatusUnprocessableEntity)
	document, err := html.Parse(strings.NewReader(body))
	if err != nil {
		t.Fatalf("parse refusal page: %v", err)
	}
	langForm := htmlFindElement(document, func(node *html.Node) bool {
		return node.Type == html.ElementNode && node.Data == "form" && htmlAttr(node, "action") == "/lang"
	})
	if langForm == nil {
		t.Fatal("the 422 refusal page offers no language switch")
	}
	token := htmlFindElement(langForm, func(node *html.Node) bool {
		return node.Type == html.ElementNode && node.Data == "input" && htmlAttr(node, "name") == "csrf_token"
	})
	if token == nil || htmlAttr(token, "value") == "" {
		t.Fatal("the 422 refusal page's language switch carries no CSRF token")
	}
}
