package main

import (
	"encoding/base64"
	"encoding/json"
	"go/ast"
	"go/types"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"

	"github.com/gofiber/fiber/v3"
	"github.com/ovumcy/ovumcy-web/internal/api"
	"github.com/ovumcy/ovumcy-web/internal/security"
	"golang.org/x/tools/go/packages"
)

// This file is the WEB-40 round 5 guard: every limiter.Config wired in
// configureFiberMiddleware (cmd/ovumcy/server.go) runs before csrf.New, so its
// LimitReached handler's refusal must never write the shared page flash slot
// ovumcy_flash — only ovumcy_flash_exempt, with fixed keys, never a
// request-derived value.
//
// The set of LimitReached constructors is resolved FROM server.go's source by
// declaration (go/packages + types.Info), never by a hand-written list: a new
// limiter.Config added later with a LimitReached this map has no entry for
// fails the sweep instead of passing silently about a handler nobody drove.
// limiterReachabilityProbes is therefore not an allowlist that narrows the
// subject — it is the OTHER half the guard cross-checks the resolved set
// against, in both directions (see TestEveryLimiterConfigLimitReachedIsDrivenForThePageFlashCookie).

// limiterReachabilityProbe drives one distinct LimitReached constructor with a
// token-less, cross-site-shaped request and asserts the refusal never sets
// the page flash cookie.
type limiterReachabilityProbe func(t *testing.T)

var limiterReachabilityProbes = map[string]limiterReachabilityProbe{
	"newAuthRateLimitHandler":         probeAuthRateLimitHandlerNeverWritesPageFlash,
	"newAPIRateLimitHandler":          probeAPIRateLimitHandlerNeverWritesPageFlash,
	"newCalendarFeedRateLimitHandler": probeCalendarFeedRateLimitHandlerNeverWritesPageFlash,
	"newCalendarPageRateLimitHandler": probeCalendarPageRateLimitHandlerNeverWritesPageFlash,
}

// TestEveryLimiterConfigLimitReachedIsDrivenForThePageFlashCookie is the guard.
// It resolves every distinct LimitReached constructor server.go wires by
// declaration, requires newAuthRateLimitHandler and newAPIRateLimitHandler
// among them (the anti-vacuity floor — those are the two N1 named), refuses
// silently narrowing the subject (a resolved constructor with no probe fails
// the sweep) and refuses a stale probe (one naming a constructor server.go no
// longer wires), then runs every probe.
func TestEveryLimiterConfigLimitReachedIsDrivenForThePageFlashCookie(t *testing.T) {
	resolved := resolveLimiterReachedConstructors(t)
	if len(resolved) < 2 {
		t.Fatalf("resolved only %d LimitReached constructor(s) from cmd/ovumcy/server.go; the sweep is reading the wrong file", len(resolved))
	}
	if !resolved["newAuthRateLimitHandler"] || !resolved["newAPIRateLimitHandler"] {
		t.Fatalf("expected newAuthRateLimitHandler and newAPIRateLimitHandler among the resolved constructors %s; the sweep is not reading configureFiberMiddleware's limiter.Config literals",
			describeSortedSet(resolved))
	}

	var missingProbe []string
	for name := range resolved {
		if _, ok := limiterReachabilityProbes[name]; !ok {
			missingProbe = append(missingProbe, name)
		}
	}
	sort.Strings(missingProbe)
	if len(missingProbe) > 0 {
		t.Fatalf("server.go wires a LimitReached constructor with no probe in limiterReachabilityProbes: %s — every limiter must be driven and asserted, not left to the ones already listed",
			strings.Join(missingProbe, ", "))
	}

	var stale []string
	for name := range limiterReachabilityProbes {
		if !resolved[name] {
			stale = append(stale, name)
		}
	}
	sort.Strings(stale)
	if len(stale) > 0 {
		t.Fatalf("limiterReachabilityProbes names %s, which configureFiberMiddleware no longer wires as a LimitReached constructor; the map is stale",
			strings.Join(stale, ", "))
	}

	for name, probe := range limiterReachabilityProbes {
		t.Run(name, probe)
	}
}

func describeSortedSet(set map[string]bool) string {
	var names []string
	for name := range set {
		names = append(names, name)
	}
	sort.Strings(names)
	if len(names) == 0 {
		return "(none)"
	}
	return strings.Join(names, ", ")
}

// resolveLimiterReachedConstructors walks every limiter.Config composite
// literal in cmd/ovumcy/server.go and resolves its LimitReached field's value
// to the function it calls, by declaration (go/packages + types.Info) rather
// than by matching source text. A LimitReached value that is not a direct
// call to a bare identifier fails the sweep instead of being silently
// skipped, so an inline handler literal added later cannot escape the set
// this guard enumerates.
func resolveLimiterReachedConstructors(t *testing.T) map[string]bool {
	t.Helper()

	root, err := moduleRootForRateLimitReachabilityBarrier()
	if err != nil {
		t.Fatalf("resolving the module root: %v", err)
	}
	config := &packages.Config{
		Mode: packages.NeedName | packages.NeedFiles | packages.NeedCompiledGoFiles |
			packages.NeedSyntax | packages.NeedTypes | packages.NeedTypesInfo |
			packages.NeedImports | packages.NeedDeps,
		Dir:   root,
		Tests: false,
	}
	loaded, err := packages.Load(config, "./cmd/ovumcy")
	if err != nil {
		t.Fatalf("type-checking cmd/ovumcy: %v", err)
	}
	var loadErrors []string
	for _, pkg := range loaded {
		for _, packageError := range pkg.Errors {
			loadErrors = append(loadErrors, pkg.PkgPath+": "+packageError.Error())
		}
	}
	if len(loadErrors) > 0 {
		t.Fatalf("cmd/ovumcy does not type-check, so no LimitReached constructor could be resolved:\n  %s", strings.Join(loadErrors, "\n  "))
	}
	if len(loaded) != 1 {
		t.Fatalf("expected exactly one loaded package for cmd/ovumcy, got %d", len(loaded))
	}
	pkg := loaded[0]

	var serverFile *ast.File
	for _, file := range pkg.Syntax {
		if strings.HasSuffix(filepath.ToSlash(pkg.Fset.Position(file.Pos()).Filename), "cmd/ovumcy/server.go") {
			serverFile = file
			break
		}
	}
	if serverFile == nil {
		t.Fatalf("cmd/ovumcy has no server.go among its type-checked files; the sweep is reading the wrong tree")
	}

	resolved := map[string]bool{}
	literalCount := 0
	ast.Inspect(serverFile, func(node ast.Node) bool {
		composite, ok := node.(*ast.CompositeLit)
		if !ok || composite.Type == nil {
			return true
		}
		literalType := pkg.TypesInfo.TypeOf(composite.Type)
		if literalType == nil || !strings.HasSuffix(literalType.String(), "middleware/limiter.Config") {
			return true
		}
		literalCount++
		for _, element := range composite.Elts {
			pair, ok := element.(*ast.KeyValueExpr)
			if !ok {
				continue
			}
			key, ok := pair.Key.(*ast.Ident)
			if !ok || key.Name != "LimitReached" {
				continue
			}
			call, ok := pair.Value.(*ast.CallExpr)
			if !ok {
				t.Fatalf("a limiter.Config.LimitReached value at %s is not a direct function call; the sweep cannot resolve it by declaration",
					pkg.Fset.Position(pair.Value.Pos()))
				continue
			}
			identifier, ok := call.Fun.(*ast.Ident)
			if !ok {
				t.Fatalf("a limiter.Config.LimitReached call at %s is not a bare identifier call; the sweep cannot resolve it by declaration",
					pkg.Fset.Position(call.Pos()))
				continue
			}
			object := pkg.TypesInfo.Uses[identifier]
			if object == nil {
				t.Fatalf("the type checker did not resolve %s at %s", identifier.Name, pkg.Fset.Position(identifier.Pos()))
				continue
			}
			funcObject, ok := object.(*types.Func)
			if !ok {
				t.Fatalf("%s at %s does not resolve to a function declaration", identifier.Name, pkg.Fset.Position(identifier.Pos()))
				continue
			}
			resolved[funcObject.Name()] = true
		}
		return true
	})
	if literalCount < 5 {
		t.Fatalf("found only %d limiter.Config literal(s) in server.go; the sweep is not reading configureFiberMiddleware", literalCount)
	}
	return resolved
}

// moduleRootForRateLimitReachabilityBarrier walks up from the working
// directory to the module root, the same way the sibling barrier in
// internal/api does — this package cannot import that one's unexported
// helper, so it carries its own copy.
func moduleRootForRateLimitReachabilityBarrier() (string, error) {
	dir, err := os.Getwd()
	if err != nil {
		return "", err
	}
	for {
		if _, statErr := os.Stat(filepath.Join(dir, "go.mod")); statErr == nil {
			return dir, nil
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			return "", os.ErrNotExist
		}
		dir = parent
	}
}

// tokenLessCrossSitePOST is the shape every probe below drives: no CSRF
// cookie, no CSRF header, a Sec-Fetch-Site the app cannot read as
// same-origin — exactly what a limiter's refusal must survive without ever
// touching the page flash slot.
func tokenLessCrossSitePOST(method, target, body string) *http.Request {
	request := httptest.NewRequest(method, target, strings.NewReader(body))
	if body != "" {
		request.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	}
	request.Header.Set("Sec-Fetch-Site", "cross-site")
	return request
}

func assertNoPageFlashCookie(t *testing.T, response *http.Response) {
	t.Helper()
	if cookie := testResponseCookie(response.Cookies(), "ovumcy_flash"); cookie != nil && cookie.Value != "" {
		t.Fatalf("a limiter refusal must never write the page flash cookie, got %q", cookie.Value)
	}
}

// assertNoExemptFlashCookie is assertNoPageFlashCookie's counterpart for a
// global-target spec (the two calendar limiters below): apiError never
// touches either flash cookie for that target (see respondRateLimitedFormError's
// default arm), so a probe reaching it must see NEITHER slot written, not just
// the page one.
func assertNoExemptFlashCookie(t *testing.T, response *http.Response) {
	t.Helper()
	if cookie := testResponseCookie(response.Cookies(), "ovumcy_flash_exempt"); cookie != nil && cookie.Value != "" {
		t.Fatalf("a global-target limiter refusal must never write the exempt flash cookie either, got %q", cookie.Value)
	}
}

// exemptFlashPayloadFromResponse opens the ovumcy_flash_exempt cookie the same
// way internal/api's sibling tests decode flash cookies (mustReadExemptFlashPayload
// in internal/api/error_mapping_transport_regression_test.go), reimplemented
// here because cmd/ovumcy cannot reach internal/api's unexported
// secureCookieCodec: the envelope is "v2." + base64url(AEAD-seal), AAD-bound to
// "ovumcy.cookie.<cookie-name>" under security.NewSecureCookieCipher — the same
// primitive, just without the cookie-name→AAD framing helper that lives in
// internal/api.
func exemptFlashPayloadFromResponse(t *testing.T, response *http.Response) api.FlashPayload {
	t.Helper()

	cookie := testResponseCookie(response.Cookies(), "ovumcy_flash_exempt")
	if cookie == nil || cookie.Value == "" {
		t.Fatal("expected the exempt flash cookie in the response")
	}

	version, encoded, found := strings.Cut(cookie.Value, ".")
	if !found || version != "v2" || strings.TrimSpace(encoded) == "" {
		t.Fatalf("exempt flash cookie %q is not a v2 sealed envelope", cookie.Value)
	}
	sealed, err := base64.RawURLEncoding.DecodeString(encoded)
	if err != nil {
		t.Fatalf("decode exempt flash cookie payload: %v", err)
	}

	cipher, err := security.NewSecureCookieCipher([]byte(rateLimitTestHandlerSecretKey))
	if err != nil {
		t.Fatalf("build secure cookie cipher: %v", err)
	}
	plaintext, err := cipher.Open(sealed, []byte("ovumcy.cookie.ovumcy_flash_exempt"))
	if err != nil {
		t.Fatalf("open exempt flash cookie: %v", err)
	}

	var payload api.FlashPayload
	if err := json.Unmarshal(plaintext, &payload); err != nil {
		t.Fatalf("decode exempt flash payload JSON: %v", err)
	}
	return payload
}

// assertExemptFlashCookieCarriesFixedKey decodes the exempt flash cookie and
// requires that fieldValue (the AuthError or SettingsError field the caller's
// target writes) equals the spec's fixed key — never a value derived from the
// request the limiter refused — and that ForgotEmail is empty, since a
// limiter-originated flash never carries the request-supplied email
// (respondAuthErrorChannel's includeForgotEmail is false for every
// limiter-originated caller).
func assertExemptFlashCookieCarriesFixedKey(t *testing.T, response *http.Response, expectedKey string, fieldValue func(api.FlashPayload) string) {
	t.Helper()
	payload := exemptFlashPayloadFromResponse(t, response)
	if got := fieldValue(payload); got != expectedKey {
		t.Fatalf("expected the exempt flash cookie's fixed key %q, got %q", expectedKey, got)
	}
	if payload.ForgotEmail != "" {
		t.Fatalf("expected no ForgotEmail on a limiter-originated exempt flash, got %q", payload.ForgotEmail)
	}
}

func probeAuthRateLimitHandlerNeverWritesPageFlash(t *testing.T) {
	handler := newRateLimitTestHandler(t)
	app := fiber.New()
	app.Use(handler.LanguageMiddleware)
	app.Post("/api/v1/sessions", newAuthRateLimitHandler(handler, authRateLimitConfig{
		ErrorCode: "too_many_login_attempts",
	}))

	response, err := app.Test(tokenLessCrossSitePOST(http.MethodPost, "/api/v1/sessions", "email=probe%40example.com"), testConfigNoTimeout)
	if err != nil {
		t.Fatalf("request failed: %v", err)
	}
	defer func() { _ = response.Body.Close() }()
	assertNoPageFlashCookie(t, response)
	assertExemptFlashCookieCarriesFixedKey(t, response, "too_many_login_attempts", func(p api.FlashPayload) string { return p.AuthError })
}

func probeAPIRateLimitHandlerNeverWritesPageFlash(t *testing.T) {
	handler := newRateLimitTestHandler(t)
	app := fiber.New()
	app.Use(handler.LanguageMiddleware)
	app.Patch("/api/v1/users/current/profile", newAPIRateLimitHandler(handler))

	response, err := app.Test(tokenLessCrossSitePOST(http.MethodPatch, "/api/v1/users/current/profile", "display_name=probe"), testConfigNoTimeout)
	if err != nil {
		t.Fatalf("request failed: %v", err)
	}
	defer func() { _ = response.Body.Close() }()
	assertNoPageFlashCookie(t, response)
	assertExemptFlashCookieCarriesFixedKey(t, response, "too many requests", func(p api.FlashPayload) string { return p.SettingsError })
}

// probeCalendarFeedRateLimitHandlerNeverWritesPageFlash and its /calendar
// sibling below both answer through globalRateLimitErrorSpec (Target: global),
// which apiError renders as a bare JSON/HTML status with no Set-Cookie at
// all — see respondRateLimitedFormError's default arm and the class-wide
// invariant documented in docs/SECURITY_INVARIANTS.md ("Flash is two sealed
// cookies..."). Their probes therefore assert the ABSENCE of both slots,
// never the presence of the exempt one.
func probeCalendarFeedRateLimitHandlerNeverWritesPageFlash(t *testing.T) {
	handler := newRateLimitTestHandler(t)
	app := fiber.New()
	app.Use(handler.LanguageMiddleware)
	app.Get("/calendar/feed/:token.ics", newCalendarFeedRateLimitHandler(handler))

	response, err := app.Test(tokenLessCrossSitePOST(http.MethodGet, "/calendar/feed/probe-token.ics", ""), testConfigNoTimeout)
	if err != nil {
		t.Fatalf("request failed: %v", err)
	}
	defer func() { _ = response.Body.Close() }()
	assertNoPageFlashCookie(t, response)
	assertNoExemptFlashCookie(t, response)
}

func probeCalendarPageRateLimitHandlerNeverWritesPageFlash(t *testing.T) {
	handler := newRateLimitTestHandler(t)
	app := fiber.New()
	app.Use(handler.LanguageMiddleware)
	app.Get("/calendar", newCalendarPageRateLimitHandler(handler))

	response, err := app.Test(tokenLessCrossSitePOST(http.MethodGet, "/calendar", ""), testConfigNoTimeout)
	if err != nil {
		t.Fatalf("request failed: %v", err)
	}
	defer func() { _ = response.Body.Close() }()
	assertNoPageFlashCookie(t, response)
	assertNoExemptFlashCookie(t, response)
}
