package api

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gofiber/fiber/v3"
	"github.com/ovumcy/ovumcy-web/internal/i18n"
	"github.com/ovumcy/ovumcy-web/internal/models"
)

// withTemplateDefaultsForTest drives withTemplateDefaults inside a real Fiber
// request context (needed for csrfToken and Locals) using a handler with a live
// i18n manager, and returns the augmented data map.
func withTemplateDefaultsForTest(t *testing.T, requestPath string, seed fiber.Map) fiber.Map {
	t.Helper()

	manager, err := i18n.NewManager("en")
	if err != nil {
		t.Fatalf("new i18n manager: %v", err)
	}
	handler := &Handler{i18n: manager, assetVersion: "testver"}

	var result fiber.Map
	app := fiber.New()
	app.Get("/*", func(c fiber.Ctx) error {
		result = handler.withTemplateDefaults(c, seed)
		return c.SendStatus(http.StatusOK)
	})
	request := httptest.NewRequest(http.MethodGet, requestPath, nil)
	response, err := app.Test(request, testConfigNoTimeout)
	if err != nil {
		t.Fatalf("withTemplateDefaults request failed: %v", err)
	}
	_ = response.Body.Close()
	return result
}

// TestWithTemplateDefaultsPreservesSeededValues pins the "keep caller-provided
// value, else inject default" branches in withTemplateDefaults (survivors: the
// Messages / Lang / CurrentPath existence guards and the NoData fallback). A
// pre-seeded Messages/Lang/CurrentPath must survive untouched, while omitted
// keys are filled — a negated guard would clobber caller data or skip the
// default.
func TestWithTemplateDefaultsPreservesSeededValues(t *testing.T) {
	t.Parallel()

	seededMessages := map[string]string{"custom.key": "kept"}
	data := withTemplateDefaultsForTest(t, "/dashboard?tab=x", fiber.Map{
		"Messages":    seededMessages,
		"Lang":        "fr",
		"CurrentPath": "/explicit/path",
	})

	if got, _ := data["Messages"].(map[string]string); got == nil || got["custom.key"] != "kept" {
		t.Fatalf("expected seeded Messages preserved, got %v", data["Messages"])
	}
	if data["Lang"] != "fr" {
		t.Fatalf("expected seeded Lang preserved, got %v", data["Lang"])
	}
	if data["CurrentPath"] != "/explicit/path" {
		t.Fatalf("expected seeded CurrentPath preserved, got %v", data["CurrentPath"])
	}
}

// TestWithTemplateDefaultsFillsMissingDefaults pins the default-injection side
// of the same guards: with an empty seed the language falls back to the i18n
// default, the request path is captured, the asset version is threaded, and
// NoDataLabel falls back to "-" when the translation is absent (survivor: the
// `noData == "common.not_available"` fallback).
func TestWithTemplateDefaultsFillsMissingDefaults(t *testing.T) {
	t.Parallel()

	data := withTemplateDefaultsForTest(t, "/stats?window=90", fiber.Map{})

	if data["Lang"] != "en" {
		t.Fatalf("expected default language en, got %v", data["Lang"])
	}
	if data["CurrentPath"] != "/stats?window=90" {
		t.Fatalf("expected request path captured, got %v", data["CurrentPath"])
	}
	if data["AssetVersion"] != "testver" {
		t.Fatalf("expected asset version threaded, got %v", data["AssetVersion"])
	}
	if data["NoDataLabel"] != "-" {
		t.Fatalf("expected NoDataLabel to fall back to '-', got %v", data["NoDataLabel"])
	}
}

// TestWithTemplateDefaultsNoDataFallbackWhenUntranslated isolates the
// NoDataLabel fallback conditional (`noData == "common.not_available"`): when
// the active message set has no translation for that key, translateMessage
// returns the key itself and the helper must substitute "-". Seeding a message
// map without the key makes the two sides of the guard produce different output
// (the raw key vs "-"), so the mutant is killed rather than masked by the real
// bundle where the translation already equals "-".
func TestWithTemplateDefaultsNoDataFallbackWhenUntranslated(t *testing.T) {
	t.Parallel()

	data := withTemplateDefaultsForTest(t, "/dashboard", fiber.Map{
		"Messages": map[string]string{"unrelated.key": "value"},
	})
	if data["NoDataLabel"] != "-" {
		t.Fatalf("expected NoDataLabel '-' when translation missing, got %v", data["NoDataLabel"])
	}
}

// TestIsActiveTemplateRoute pins the nav active-state predicate used to
// highlight the current section. The rows cover the empty-path root special
// case, the "/" root prefix handling, and the exact / query / subpath matches
// for a non-root route, plus a sibling route that must NOT be considered a
// prefix match. Each surviving conditional in isActiveTemplateRoute flips one
// of these active/inactive decisions.
func TestIsActiveTemplateRoute(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name        string
		currentPath string
		route       string
		want        bool
	}{
		{"empty path matches root route", "", "/", true},
		{"empty path does not match non-root", "", "/settings", false},
		{"root route matches bare slash", "/", "/", true},
		{"root route matches slash with query", "/?tab=x", "/", true},
		{"root route not active on subpage", "/settings", "/", false},
		{"exact non-root match", "/settings", "/settings", true},
		{"non-root match with query", "/settings?tab=account", "/settings", true},
		{"non-root match on subpath", "/settings/profile", "/settings", true},
		{"sibling route is not a prefix match", "/settings-extra", "/settings", false},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			if got := isActiveTemplateRoute(tc.currentPath, tc.route); got != tc.want {
				t.Fatalf("isActiveTemplateRoute(%q, %q) = %v, want %v", tc.currentPath, tc.route, got, tc.want)
			}
		})
	}
}

// TestTemplateUserIdentityHelpers pins the nil-user guards and trimming in the
// display-name helpers (survivors: the `user == nil` conditionals): a nil user
// yields the empty identity / false, a blank-name user has no display name, and
// a real name is trimmed and reported present.
func TestTemplateUserIdentityHelpers(t *testing.T) {
	t.Parallel()

	if got := templateUserIdentity(nil); got != "" {
		t.Fatalf("templateUserIdentity(nil) = %q, want empty", got)
	}
	if templateHasDisplayName(nil) {
		t.Fatal("templateHasDisplayName(nil) = true, want false")
	}
	if got := templateUserIdentity(&models.User{DisplayName: "  Sam  "}); got != "Sam" {
		t.Fatalf("templateUserIdentity(trimmed) = %q, want \"Sam\"", got)
	}
	if templateHasDisplayName(&models.User{DisplayName: "   "}) {
		t.Fatal("templateHasDisplayName(blank) = true, want false")
	}
	if !templateHasDisplayName(&models.User{DisplayName: "Sam"}) {
		t.Fatal("templateHasDisplayName(name) = false, want true")
	}
}

// TestTemplateDictPairing pins templateDict: an even argument list builds the
// map and an odd list errors (survivor: the `len(values)%2 != 0` guard and the
// `index += 2` step). A wrong step would drop or mispair keys.
func TestTemplateDictPairing(t *testing.T) {
	t.Parallel()

	dict, err := templateDict("a", 1, "b", 2, "c", 3)
	if err != nil {
		t.Fatalf("templateDict even args errored: %v", err)
	}
	if len(dict) != 3 || dict["a"] != 1 || dict["b"] != 2 || dict["c"] != 3 {
		t.Fatalf("templateDict pairing wrong: %#v", dict)
	}

	if _, err := templateDict("a", 1, "b"); err == nil {
		t.Fatal("templateDict odd args = nil error, want error")
	}
}

// TestFormatTemplateFloat pins the one-decimal formatter used for BBT-style
// values. Integers and values that round to an integer render with no decimal
// (%.0f); fractional values keep one decimal (%.1f). The rows are chosen so the
// surviving arithmetic mutants change the output: a value*10 vs value/10 swap
// or a dropped rounding step moves 36.44 off "36.4", and the near-integer
// conditional decides whether 36.96 collapses to "37".
func TestFormatTemplateFloat(t *testing.T) {
	t.Parallel()

	cases := []struct {
		value float64
		want  string
	}{
		{37, "37"},
		{0, "0"},
		{36.5, "36.5"},
		{36.44, "36.4"},  // rounds down to one decimal, stays fractional
		{36.96, "37"},    // rounds up across the integer boundary -> no decimal
		{36.04, "36"},    // rounds down to the integer -> no decimal
		{-36.5, "-36.5"}, // negative fractional keeps one decimal
	}

	for _, tc := range cases {
		if got := formatTemplateFloat(tc.value); got != tc.want {
			t.Fatalf("formatTemplateFloat(%v) = %q, want %q", tc.value, got, tc.want)
		}
	}
}

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

// TestNormalizeAssetVersionCharacterClasses pins the exact allow-list applied
// to a raw build revision character by character. Each row isolates one
// boundary of the switch in normalizeAssetVersion: a char just outside a range
// ('/' below '0', ':' above '9', '@'/'[' around A-Z, backtick/'{' around a-z)
// is dropped, a char on a boundary ('0','9','a','z','A','Z') and the three
// literal separators ('-','.','_') are kept, and the 16-rune cap truncates.
// These are the mutation survivors on the composite case expression: a shifted
// range boundary or a negated separator equality changes which characters
// survive into the token.
func TestNormalizeAssetVersionCharacterClasses(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name     string
		revision string
		want     string
	}{
		{name: "lowercase boundaries kept", revision: "az", want: "az"},
		{name: "uppercase boundaries kept", revision: "AZ", want: "AZ"},
		{name: "digit boundaries kept", revision: "09", want: "09"},
		{name: "dash dot underscore kept", revision: "-._", want: "-._"},
		{name: "slash below zero dropped", revision: "a/b", want: "ab"},
		{name: "colon above nine dropped", revision: "a:b", want: "ab"},
		{name: "at below uppercase A dropped", revision: "A@B", want: "AB"},
		{name: "bracket above uppercase Z dropped", revision: "A[B", want: "AB"},
		{name: "backtick below lowercase a dropped", revision: "a`b", want: "ab"},
		{name: "brace above lowercase z dropped", revision: "a{b", want: "ab"},
		{name: "sixteen valid chars kept whole", revision: "0123456789abcdef", want: "0123456789abcdef"},
		{name: "seventeenth char truncated", revision: "0123456789abcdefX", want: "0123456789abcdef"},
		{name: "invalid chars do not count toward cap", revision: "////0123456789abcdef", want: "0123456789abcdef"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			if got := normalizeAssetVersion(tt.revision); got != tt.want {
				t.Fatalf("normalizeAssetVersion(%q) = %q, want %q", tt.revision, got, tt.want)
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
