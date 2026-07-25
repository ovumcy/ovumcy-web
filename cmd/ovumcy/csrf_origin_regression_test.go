package main

import (
	"bytes"
	"io"
	"log"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"

	"github.com/gofiber/fiber/v3"
	"github.com/gofiber/fiber/v3/middleware/csrf"
)

// The CSRF middleware validates the browser Origin (falling back to Referer)
// against the app-observed scheme+host on every mutating request. Until this
// file, nothing in Go exercised that control: the only other place the real
// csrfMiddlewareConfig is mounted (TestCSRFMiddlewareErrorHandlerLogsSecurityEvent-
// WithoutPII) runs with cookieSecure=false over a plain-HTTP posture, where the
// missing-Origin branch is deliberately inert — so the control that answers 403
// had no coverage at all, and its absence surfaced only as three dozen unrelated
// Playwright failures the first time the suite ran against HTTPS.
//
// Two things make the control invisible unless a test asks for them explicitly:
//   - It only refuses an ORIGIN-LESS request when the app observes https.
//     Over plain http fiber clears the error and proceeds.
//   - The app observes https only through the terminator's X-Forwarded-Proto,
//     which fiber reads only when the connection's peer is a trusted proxy
//     (TRUST_PROXY_ENABLED=true). Without that, COOKIE_SECURE=true alone
//     changes nothing here.
//
// Every test below therefore sends a VALID token plus its cookie, so a 403 can
// only come from the Origin/Referer control, and asserts the logged failure
// reason to prove it did not come from the token checks instead.

const (
	csrfProbeMutatingPath = "/settings/change-password"
	csrfProbeTokenPath    = "/csrf-token"
	// csrfProbeAppOrigin is the origin app.Test requests carry: httptest.NewRequest
	// defaults the Host to example.com, and the probe app is reached over the
	// forwarded https scheme below.
	csrfProbeAppOrigin     = "https://example.com"
	csrfProbeForeignOrigin = "https://attacker.example"
)

// csrfProbePosture selects the transport posture the probe app is mounted in.
type csrfProbePosture struct {
	// behindHTTPSTerminator mounts the app the way the public deploy profile
	// recommends: COOKIE_SECURE=true plus TRUST_PROXY_ENABLED=true, with the
	// terminator forwarding X-Forwarded-Proto: https.
	behindHTTPSTerminator bool
}

// newCSRFOriginProbeApp mounts the REAL production csrfMiddlewareConfig (and the
// real fiberConfig, whose trust-proxy wiring decides which scheme the app
// observes) in front of one mutating route and one route that hands the issued
// token back, so a test can assemble an otherwise-valid request and vary only
// its Origin.
func newCSRFOriginProbeApp(t *testing.T, posture csrfProbePosture) *fiber.App {
	t.Helper()

	handler := newRateLimitTestHandler(t)

	proxy := proxySettings{}
	if posture.behindHTTPSTerminator {
		// app.Test serves every request over a placeholder TCP connection whose
		// remote address is 0.0.0.0:0, so that address IS the terminator as far as
		// fiber's trusted-proxy check can see. Trusting it is what makes c.Scheme()
		// read X-Forwarded-Proto — precisely what TRUST_PROXY_ENABLED=true buys in
		// production. Leave it out and the app observes plain http, at which point
		// the whole Origin control below switches itself off.
		proxy = proxySettings{
			Enabled:        true,
			Header:         "X-Real-IP",
			TrustedProxies: []string{"0.0.0.0"},
		}
	}

	app := fiber.New(fiberConfig(proxy))
	app.Use(csrf.New(csrfMiddlewareConfig(true, handler)))
	app.Get(csrfProbeTokenPath, func(c fiber.Ctx) error {
		return c.SendString(csrf.TokenFromContext(c))
	})
	app.Post(csrfProbeMutatingPath, func(c fiber.Ctx) error {
		return c.SendStatus(http.StatusOK)
	})
	return app
}

// csrfProbeCredentials is a token/cookie pair the middleware itself issued, i.e.
// everything a legitimate form submission carries apart from the Origin header.
type csrfProbeCredentials struct {
	token  string
	cookie string
}

func issueCSRFProbeCredentials(t *testing.T, app *fiber.App, posture csrfProbePosture) csrfProbeCredentials {
	t.Helper()

	request := httptest.NewRequest(http.MethodGet, csrfProbeTokenPath, nil)
	if posture.behindHTTPSTerminator {
		request.Header.Set("X-Forwarded-Proto", "https")
	}

	response, err := app.Test(request, testConfigNoTimeout)
	if err != nil {
		t.Fatalf("csrf token request failed: %v", err)
	}
	defer func() { _ = response.Body.Close() }()

	if response.StatusCode != http.StatusOK {
		t.Fatalf("expected csrf token request to succeed, got %d", response.StatusCode)
	}

	body, err := io.ReadAll(response.Body)
	if err != nil {
		t.Fatalf("read csrf token body: %v", err)
	}
	token := strings.TrimSpace(string(body))
	if token == "" {
		t.Fatal("expected the csrf middleware to issue a token on the safe request")
	}

	var cookie string
	for _, candidate := range response.Cookies() {
		if candidate.Name == "ovumcy_csrf" {
			cookie = candidate.Name + "=" + candidate.Value
		}
	}
	if cookie == "" {
		t.Fatal("expected the csrf middleware to set the ovumcy_csrf cookie on the safe request")
	}
	return csrfProbeCredentials{token: token, cookie: cookie}
}

// csrfProbeHeaders names the extra request headers a case varies.
type csrfProbeHeaders struct {
	origin  string
	referer string
}

// submitCSRFProbeMutation sends an otherwise-valid mutating request (correct
// token in the form, matching cookie attached) and returns its status together
// with everything the security-event stream logged while it ran.
func submitCSRFProbeMutation(
	t *testing.T,
	app *fiber.App,
	posture csrfProbePosture,
	credentials csrfProbeCredentials,
	headers csrfProbeHeaders,
) (int, string) {
	t.Helper()

	originalWriter := log.Writer()
	defer log.SetOutput(originalWriter)
	var logged bytes.Buffer
	log.SetOutput(&logged)

	form := url.Values{"csrf_token": {credentials.token}}
	request := httptest.NewRequest(http.MethodPost, csrfProbeMutatingPath, strings.NewReader(form.Encode()))
	request.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	request.Header.Set("Cookie", credentials.cookie)
	if posture.behindHTTPSTerminator {
		request.Header.Set("X-Forwarded-Proto", "https")
	}
	if headers.origin != "" {
		request.Header.Set("Origin", headers.origin)
	}
	if headers.referer != "" {
		request.Header.Set("Referer", headers.referer)
	}

	response, err := app.Test(request, testConfigNoTimeout)
	if err != nil {
		t.Fatalf("csrf probe mutation failed: %v", err)
	}
	defer func() { _ = response.Body.Close() }()

	return response.StatusCode, logged.String()
}

// assertRefusedByOriginCheck pins that the refusal came from the Origin/Referer
// control and not from a token check. Without this the tests would stay green if
// the Origin control were deleted and the token pair happened to break for an
// unrelated reason — the failure reason is the only observable that distinguishes
// them (CSRFFailureReason maps every Origin/Referer sentinel to "invalid referer").
func assertRefusedByOriginCheck(t *testing.T, status int, logged string) {
	t.Helper()

	if status != http.StatusForbidden {
		t.Fatalf("expected the csrf middleware to refuse the request with 403, got %d", status)
	}
	if !strings.Contains(logged, `security event: action="csrf" outcome="denied"`) {
		t.Fatalf("expected a csrf denial security event, got %q", logged)
	}
	if !strings.Contains(logged, `reason="invalid referer"`) {
		t.Fatalf("expected the refusal to come from the origin/referer check, got %q", logged)
	}
}

// TestCSRFRefusesOriginlessMutationBehindHTTPSTerminator is the control that
// answered 403 to every Playwright request which reached the app over HTTPS
// without an Origin. A valid token is not sufficient: behind the terminator the
// request must also prove where it came from.
func TestCSRFRefusesOriginlessMutationBehindHTTPSTerminator(t *testing.T) {
	posture := csrfProbePosture{behindHTTPSTerminator: true}
	app := newCSRFOriginProbeApp(t, posture)
	credentials := issueCSRFProbeCredentials(t, app, posture)

	status, logged := submitCSRFProbeMutation(t, app, posture, credentials, csrfProbeHeaders{})

	assertRefusedByOriginCheck(t, status, logged)
}

// TestCSRFRefusesForeignOriginMutation covers the case the control exists for: a
// cross-site form post carrying a token the attacker somehow learned. Unlike the
// origin-less case this one is refused in EVERY posture, because a present-but-
// mismatched Origin is an error fiber never clears.
func TestCSRFRefusesForeignOriginMutation(t *testing.T) {
	for name, posture := range map[string]csrfProbePosture{
		"behind https terminator": {behindHTTPSTerminator: true},
		"plain http":              {},
	} {
		t.Run(name, func(t *testing.T) {
			app := newCSRFOriginProbeApp(t, posture)
			credentials := issueCSRFProbeCredentials(t, app, posture)

			status, logged := submitCSRFProbeMutation(t, app, posture, credentials, csrfProbeHeaders{
				origin: csrfProbeForeignOrigin,
			})

			assertRefusedByOriginCheck(t, status, logged)
		})
	}
}

// TestCSRFAcceptsMatchingOriginMutationBehindHTTPSTerminator is the positive
// anchor: the same request the two tests above are refused for reaches its
// handler once it carries the app's own origin. Without it, "403" would be
// consistent with a middleware that refuses everything.
func TestCSRFAcceptsMatchingOriginMutationBehindHTTPSTerminator(t *testing.T) {
	posture := csrfProbePosture{behindHTTPSTerminator: true}
	app := newCSRFOriginProbeApp(t, posture)
	credentials := issueCSRFProbeCredentials(t, app, posture)

	status, logged := submitCSRFProbeMutation(t, app, posture, credentials, csrfProbeHeaders{
		origin: csrfProbeAppOrigin,
	})

	if status != http.StatusOK {
		t.Fatalf("expected a same-origin mutation with a valid token to reach its handler, got %d (log: %q)", status, logged)
	}
	if strings.Contains(logged, `action="csrf"`) {
		t.Fatalf("did not expect a csrf security event for an accepted mutation, got %q", logged)
	}
}

// TestCSRFAcceptsMatchingRefererWhenOriginAbsentBehindHTTPSTerminator pins the
// documented fallback: over https, a request with no Origin is checked against
// its Referer instead. This is why real browsers keep working on flows that omit
// Origin, and why the Playwright fix was to send one explicitly rather than to
// weaken the middleware.
func TestCSRFAcceptsMatchingRefererWhenOriginAbsentBehindHTTPSTerminator(t *testing.T) {
	posture := csrfProbePosture{behindHTTPSTerminator: true}
	app := newCSRFOriginProbeApp(t, posture)
	credentials := issueCSRFProbeCredentials(t, app, posture)

	status, logged := submitCSRFProbeMutation(t, app, posture, credentials, csrfProbeHeaders{
		referer: csrfProbeAppOrigin + "/settings",
	})

	if status != http.StatusOK {
		t.Fatalf("expected a matching Referer to stand in for the missing Origin, got %d (log: %q)", status, logged)
	}
}

// TestCSRFToleratesOriginlessMutationOnPlainHTTP characterizes the asymmetry that
// hid the gap: the identical origin-less request that is refused behind the
// terminator is accepted when the app observes plain http. It is fiber's
// behavior, not a guarantee this repo wants to keep — its value is that a change
// to the scheme condition (or to the trust-proxy wiring that feeds it) can no
// longer pass unnoticed as "the local suite is green".
func TestCSRFToleratesOriginlessMutationOnPlainHTTP(t *testing.T) {
	posture := csrfProbePosture{}
	app := newCSRFOriginProbeApp(t, posture)
	credentials := issueCSRFProbeCredentials(t, app, posture)

	status, logged := submitCSRFProbeMutation(t, app, posture, credentials, csrfProbeHeaders{})

	if status != http.StatusOK {
		t.Fatalf("expected the origin-less mutation to be tolerated over plain http, got %d (log: %q)", status, logged)
	}
}
