package api

import (
	"html"
	"net/http"
	"net/url"
	"strings"
	"testing"
)

// TestLanguageSwitchRefusalPageFiltersItsOwnAddressQuery submits the language
// switch the way a page that is not ours would — no CSRF token, a `next` path
// carrying an address the app never asked for — and reads the refusal page's
// layout: the switcher's hidden `next` and the footer's privacy link render the
// page's own address, and that address goes through the same query allowlist
// every other page's does. A `next` query outside it never reaches either; an
// allowlisted parameter of the right shape still does, so the check cannot pass
// on a layout that renders neither.
func TestLanguageSwitchRefusalPageFiltersItsOwnAddressQuery(t *testing.T) {
	t.Parallel()

	ctx := newRefusalPageContext(t, "lang-refusal-current-path@example.com")
	request := languageSwitchRequest("ru", "/?email=leak-probe%40example.com&error=leak-probe&month=2026-01", "")
	request.Header.Set("Accept", noJSBrowserAccept)
	response := mustAppResponse(t, ctx.app, request)
	defer func() { _ = response.Body.Close() }()

	assertStatusCode(t, response, http.StatusForbidden)
	body := mustReadBodyString(t, response.Body)
	// The refusal's own back link is the form's `next`, exactly where a granted
	// switch would have redirected; the layout around it is what is read here.
	before, rest, found := strings.Cut(body, "data-page-form-refusal")
	_, after, closed := strings.Cut(rest, "</section>")
	if !found || !closed {
		t.Fatalf("refusal page lacks its content section: %s", body)
	}
	layout := before + after
	if strings.Contains(layout, "leak-probe") {
		t.Fatalf("the refusal page's layout renders a query outside the current-path allowlist: %s", layout)
	}

	const want = "/?month=2026-01"
	if !strings.Contains(layout, `name="next" value="`+want+`"`) {
		t.Fatalf("the switcher's hidden next is not the filtered address %q: %s", want, layout)
	}
	_, privacy, found := strings.Cut(layout, `href="/privacy?back=`)
	privacy, _, closed = strings.Cut(privacy, `"`)
	if !found || !closed {
		t.Fatalf("the refusal page renders no privacy link: %s", layout)
	}
	if back, err := url.QueryUnescape(html.UnescapeString(privacy)); err != nil || back != want {
		t.Fatalf("the privacy link carries back=%q (%v), want the filtered address %q", back, err, want)
	}
}
