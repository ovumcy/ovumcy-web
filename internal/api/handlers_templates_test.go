package api

import (
	"net/http"
	"net/http/httptest"
	"testing"
)

// TestBaseLayoutVersionsStaticAssets pins the cache-busting contract: every
// versioned static asset URL in the base layout carries the ?v=<build revision>
// exposed through the shared page data (Handler.assetVersion → AssetVersion in
// withTemplateDefaults). Structural attribute assertion only — the token value
// comes from SetAssetVersion so a release changes it and busts stale bundles.
// Paired with the /static Cache-Control max-age asserted in cmd/ovumcy.
func TestBaseLayoutVersionsStaticAssets(t *testing.T) {
	t.Parallel()

	const version = "testrev1"
	app, _ := newOnboardingTestAppWithOptions(t, onboardingTestAppOptions{assetVersion: version})

	request := httptest.NewRequest(http.MethodGet, "/login", nil)
	request.Header.Set("Accept-Language", "en")
	response := mustAppResponse(t, app, request)
	assertStatusCode(t, response, http.StatusOK)

	rendered := mustReadBodyString(t, response.Body)
	assertBodyContainsAll(t, rendered,
		bodyStringMatch{fragment: "/static/js/app.js?v=" + version, message: "expected app.js bundle to carry the build-revision cache-buster"},
		bodyStringMatch{fragment: "/static/css/tailwind.css?v=" + version, message: "expected tailwind.css to carry the build-revision cache-buster"},
		bodyStringMatch{fragment: "/static/js/htmx.min.js?v=" + version, message: "expected htmx bundle to carry the build-revision cache-buster"},
		bodyStringMatch{fragment: "/static/js/theme-bootstrap.js?v=" + version, message: "expected theme-bootstrap to carry the build-revision cache-buster"},
	)
	assertBodyNotContainsAll(t, rendered,
		bodyStringMatch{fragment: "app.js?v=20260612", message: "did not expect a hardcoded legacy asset version"},
	)
}

// TestSetAssetVersionNormalizesToken locks the normalization applied to the raw
// build revision before it reaches an asset URL: a dirty git SHA is truncated
// and kept URL-safe, while an empty or fully invalid revision falls back to the
// static default rather than emitting a bare ?v=.
func TestSetAssetVersionNormalizesToken(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name     string
		revision string
		want     string
	}{
		{name: "short revision preserved", revision: "abc123", want: "abc123"},
		{name: "dirty suffix kept and truncated", revision: "0123456789abcdef0123-dirty", want: "0123456789abcdef"},
		{name: "surrounding space trimmed", revision: "  fe9c1a  ", want: "fe9c1a"},
		{name: "unsafe characters dropped", revision: "v1.2.3/../x", want: "v1.2.3..x"},
		{name: "empty falls back to default", revision: "", want: defaultAssetVersion},
		{name: "all-invalid falls back to default", revision: "///", want: defaultAssetVersion},
	}

	for _, tt := range tests {
		tt := tt
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			handler := &Handler{}
			handler.SetAssetVersion(tt.revision)
			if handler.assetVersion != tt.want {
				t.Fatalf("SetAssetVersion(%q) => %q, want %q", tt.revision, handler.assetVersion, tt.want)
			}
		})
	}
}

// TestParsePartialTemplatesIncludesBaseHelpers pins that parsePartialTemplates
// parses the embedded base.html alongside each partial, so a partial that
// delegates to a define declared in base.html (for example the
// current_user_identity_oob partial invoking nav_user_identity_chip) resolves.
// Asserting the parsed template set contains both defines keeps the contract
// without coupling to page copy.
func TestParsePartialTemplatesIncludesBaseHelpers(t *testing.T) {
	t.Parallel()

	partials, err := parsePartialTemplates(newTemplateFuncMap(), []string{"current_user_identity_oob.html"})
	if err != nil {
		t.Fatalf("parse partial templates: %v", err)
	}

	parsed, ok := partials["current_user_identity_oob"]
	if !ok {
		t.Fatal("expected current_user_identity_oob partial to be parsed")
	}
	for _, name := range []string{"current_user_identity_oob", "nav_user_identity_chip"} {
		if parsed.Lookup(name) == nil {
			t.Fatalf("expected parsed partial set to include %q define (base.html helpers must be parsed in)", name)
		}
	}
}
