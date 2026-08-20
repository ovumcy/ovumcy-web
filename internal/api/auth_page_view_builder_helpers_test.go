package api

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"testing"

	"github.com/gofiber/fiber/v3"
	"golang.org/x/net/html"
)

func evaluateAuthPageBuilder(t *testing.T, handler fiber.Handler) map[string]any {
	t.Helper()
	return evaluateAuthPageBuilderWithCookie(t, "", handler)
}

func evaluateAuthPageBuilderWithCookie(t *testing.T, cookieHeader string, handler fiber.Handler) map[string]any {
	t.Helper()

	app := fiber.New()
	app.Get("/", handler)

	request := httptest.NewRequest(http.MethodGet, "/", nil)
	if cookieHeader != "" {
		request.Header.Set("Cookie", cookieHeader)
	}
	response, err := app.Test(request, testConfigNoTimeout)
	if err != nil {
		t.Fatalf("app test failed: %v", err)
	}
	if response.StatusCode != http.StatusOK {
		t.Fatalf("unexpected status code: got %d", response.StatusCode)
	}

	payload := map[string]any{}
	if err := json.NewDecoder(response.Body).Decode(&payload); err != nil {
		t.Fatalf("decode payload failed: %v", err)
	}
	return payload
}

// hostileAuthPageQuery is what an attacker-supplied link carries into a public
// auth page: an email to prefill and an error state to fake. Neither may be
// read — PII and error codes travel in the sealed flash cookie, never in a URL.
var hostileAuthPageQuery = url.Values{
	"email": {"query-source@example.com"},
	"error": {"invalid credentials"},
}

// requestAuthPageWithHostileQuery drives the REAL route through the real app so
// the assertion observes the markup a browser would receive. The builders take
// no fiber.Ctx today, which is exactly why the exclusion has to be pinned here
// rather than at the builder: a query read reintroduced into a page handler is
// invisible to any test that calls the builder directly.
func requestAuthPageWithHostileQuery(t *testing.T, app *fiber.App, path string) string {
	t.Helper()

	request := httptest.NewRequest(http.MethodGet, path+"?"+hostileAuthPageQuery.Encode(), nil)
	request.Header.Set("Accept-Language", "en")

	response := mustAppResponse(t, app, request)
	assertStatusCode(t, response, http.StatusOK)
	return mustReadBodyString(t, response.Body)
}

// assertAuthPageDidNotPrefillFromQuery pins both halves of what the page-data
// builders decide: the query email does not prefill the page's own email field,
// and the query error raises no server-error block. The email input is looked up
// by id first, so a page that stopped rendering its form fails here instead of
// passing on absence.
//
// The scope is deliberate and narrower than "this page carries no query-sourced
// PII", which is FALSE today and must not be inferred from this file. The raw
// request URI — query string included — does reach the markup through
// CurrentPath (internal/api/i18n_helpers.go:93), rendered into the language
// switch's hidden next field (internal/templates/base.html:53) and into the
// privacy link's back parameter (internal/templates/base.html:161). So
// /login?email=someone@example.com renders that address percent-encoded in both
// places, and the back parameter carries it into a further outbound URL. That is
// a separate finding against the shared layout with its own change; it is named
// here so this guard's silence about it is a stated limit rather than an
// accidental gap. Widening the assertion to the whole body belongs with that fix,
// not before it.
func assertAuthPageDidNotPrefillFromQuery(t *testing.T, body string, emailInputID string) {
	t.Helper()

	assertBodyNotContainsAll(t, body, bodyStringMatch{
		fragment: hostileAuthPageQuery.Get("email"),
		message:  "did not expect the query email in raw form to reach the rendered auth page",
	})

	root := mustParseHTMLDocument(t, body)
	emailInput := htmlElementByID(root, emailInputID)
	if emailInput == nil {
		t.Fatalf("expected the auth page to render its %q field", emailInputID)
	}
	if value := htmlAttr(emailInput, "value"); value != "" {
		t.Fatalf("expected an empty prefilled email, got %q", value)
	}
	serverError := htmlFindElement(root, func(node *html.Node) bool {
		return node.Type == html.ElementNode && htmlHasAttr(node, "data-auth-server-error")
	})
	if serverError != nil {
		t.Fatalf("did not expect the query error to raise the server-error block, got key %q", htmlAttr(serverError, "data-error-key"))
	}
}
