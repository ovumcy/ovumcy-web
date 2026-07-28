package api

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/gofiber/fiber/v3"
)

// --- helpers for cookie tests ---

// newTOTPCookieTestApp returns a *fiber.App and a *Handler wired to use the
// given secret key. Two routes are registered: /seal-pending and /parse-pending
// (and the same pair for setup) — they thinly wrap the cookie helpers under
// test so the tests can drive the seal/parse flow through real HTTP.
func newTOTPCookieTestApp(t *testing.T, secretKey []byte) (*fiber.App, *Handler) {
	t.Helper()
	handler := &Handler{secretKey: secretKey, cookieSecure: false}
	app := fiber.New()
	app.Get("/seal-pending", func(c fiber.Ctx) error {
		userID := uint(fiber.Query[int](c, "user_id", 0))
		remember := fiber.Query[bool](c, "remember_me", false)
		if err := handler.setTOTPPendingCookie(c, userID, remember); err != nil {
			return c.Status(fiber.StatusInternalServerError).SendString(err.Error())
		}
		return c.SendStatus(fiber.StatusOK)
	})
	app.Get("/parse-pending", func(c fiber.Ctx) error {
		uid, remember, err := handler.parseTOTPPendingCookie(c)
		if err != nil {
			return c.Status(fiber.StatusBadRequest).SendString(err.Error())
		}
		return c.JSON(fiber.Map{"user_id": uid, "remember_me": remember})
	})
	// The setup routes take the acting account as a query parameter so a test
	// can seal for one owner and parse as another, the way two independent
	// owners on one household instance would.
	app.Get("/seal-setup", func(c fiber.Ctx) error {
		userID := uint(fiber.Query[int](c, "user_id", 0))
		raw := c.Query("raw_secret", "")
		if err := handler.setTOTPSetupCookie(c, userID, raw); err != nil {
			return c.Status(fiber.StatusInternalServerError).SendString(err.Error())
		}
		return c.SendStatus(fiber.StatusOK)
	})
	app.Get("/parse-setup", func(c fiber.Ctx) error {
		sessionUserID := uint(fiber.Query[int](c, "user_id", 0))
		raw, err := handler.parseTOTPSetupCookie(c, sessionUserID)
		if err != nil {
			return c.Status(fiber.StatusBadRequest).SendString(err.Error())
		}
		return c.JSON(fiber.Map{"raw_secret": raw})
	})
	return app, handler
}

func captureCookieValue(t *testing.T, resp *http.Response, name string) string {
	t.Helper()
	c := responseCookie(resp.Cookies(), name)
	if c == nil || c.Value == "" {
		t.Fatalf("expected Set-Cookie %q with non-empty value", name)
	}
	return c.Value
}

// sealTOTPCookiePayloadForTest seals an arbitrary payload under a cookie
// purpose, so a test can present shapes no writer would mint — an expired
// payload, one naming no account, one carrying no secret.
func sealTOTPCookiePayloadForTest(t *testing.T, secretKey []byte, purpose string, payload any) string {
	t.Helper()
	serialized, err := json.Marshal(payload)
	if err != nil {
		t.Fatalf("marshal payload: %v", err)
	}
	return sealTOTPCookiePlaintextForTest(t, secretKey, purpose, serialized)
}

// sealTOTPCookiePlaintextForTest seals raw bytes under a cookie purpose. It
// exists for the one shape the payload helper above cannot produce: a value
// whose AEAD envelope is perfectly good but whose plaintext is not the JSON the
// reader expects.
func sealTOTPCookiePlaintextForTest(t *testing.T, secretKey []byte, purpose string, plaintext []byte) string {
	t.Helper()
	codec, err := newSecureCookieCodec(secretKey)
	if err != nil {
		t.Fatalf("newSecureCookieCodec: %v", err)
	}
	sealed, err := codec.seal(purpose, plaintext)
	if err != nil {
		t.Fatalf("seal: %v", err)
	}
	return sealed
}

// assertTOTPCookieCleared pins the second obligation of every refusal on these
// two cookies: the response must retract the value it just refused. Both are
// session-scoped (zero `expires`) at path "/", so a value left in place rides on
// every later request for the rest of the browser session — and for the setup
// cookie that value is the raw TOTP secret of an enrollment nobody completed.
func assertTOTPCookieCleared(t *testing.T, response *http.Response, cookieName string) {
	t.Helper()

	cleared := responseCookie(response.Cookies(), cookieName)
	if cleared == nil {
		t.Fatalf("the refused %s cookie survives the response: no Set-Cookie retracts it, so it rides on every later request of this session", cookieName)
	}
	if strings.TrimSpace(cleared.Value) != "" {
		t.Fatalf("expected an empty cleared %s cookie value, got %q", cookieName, cleared.Value)
	}
	if cleared.Expires.IsZero() || cleared.Expires.After(time.Now()) {
		t.Fatalf("expected the cleared %s cookie to expire in the past, got expiry %s", cookieName, cleared.Expires)
	}
}

// assertTOTPCookieLeftInPlace is the counterpart: a refusal that is not about
// the cookie — a wrong six-digit code — must leave the pending value alone, so
// the retry the response invites is still possible. It is what keeps the
// clear-on-refusal assertions from passing against a reader that simply clears
// on the way past.
func assertTOTPCookieLeftInPlace(t *testing.T, response *http.Response, cookieName string) {
	t.Helper()

	touched := responseCookie(response.Cookies(), cookieName)
	if touched != nil && strings.TrimSpace(touched.Value) == "" {
		t.Fatalf("the %s cookie was cleared by a response that refused for another reason; the flow cannot be retried", cookieName)
	}
}

// --- TOTP pending cookie ---

func TestTOTPPendingCookie_RoundTrip(t *testing.T) {
	app, _ := newTOTPCookieTestApp(t, []byte("test-secret-key"))

	sealReq := httptest.NewRequest(http.MethodGet, "/seal-pending?user_id=42&remember_me=true", nil)
	sealResp, err := app.Test(sealReq, testConfigNoTimeout)
	if err != nil {
		t.Fatalf("GET /seal-pending: %v", err)
	}
	defer func() { _ = sealResp.Body.Close() }()
	if sealResp.StatusCode != http.StatusOK {
		t.Fatalf("seal status = %d, want 200", sealResp.StatusCode)
	}
	cookieValue := captureCookieValue(t, sealResp, totpPendingCookieName)

	parseReq := httptest.NewRequest(http.MethodGet, "/parse-pending", nil)
	parseReq.Header.Set("Cookie", totpPendingCookieName+"="+cookieValue)
	parseResp, err := app.Test(parseReq, testConfigNoTimeout)
	if err != nil {
		t.Fatalf("GET /parse-pending: %v", err)
	}
	defer func() { _ = parseResp.Body.Close() }()
	if parseResp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(parseResp.Body)
		t.Fatalf("parse status = %d, body = %q", parseResp.StatusCode, body)
	}

	var got struct {
		UserID     uint `json:"user_id"`
		RememberMe bool `json:"remember_me"`
	}
	if err := json.NewDecoder(parseResp.Body).Decode(&got); err != nil {
		t.Fatalf("decode parse response: %v", err)
	}
	if got.UserID != 42 {
		t.Errorf("user_id = %d, want 42", got.UserID)
	}
	if !got.RememberMe {
		t.Error("remember_me = false, want true")
	}
}

func TestTOTPPendingCookie_ExpiredPayload_ParseError(t *testing.T) {
	secretKey := []byte("test-secret-key")
	app, _ := newTOTPCookieTestApp(t, secretKey)

	sealed := sealTOTPCookiePayloadForTest(t, secretKey, totpPendingCookieName, totpPendingCookiePayload{
		UserID:    1,
		ExpiresAt: time.Now().Add(-1 * time.Minute),
	})

	req := httptest.NewRequest(http.MethodGet, "/parse-pending", nil)
	req.Header.Set("Cookie", totpPendingCookieName+"="+sealed)
	resp, err := app.Test(req, testConfigNoTimeout)
	if err != nil {
		t.Fatalf("GET /parse-pending: %v", err)
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400", resp.StatusCode)
	}
	body, _ := io.ReadAll(resp.Body)
	if !strings.Contains(string(body), "expired") {
		t.Errorf("error %q, want to contain %q", string(body), "expired")
	}
}

// TestTOTPPendingCookieUnusableValueIsClearedNotLeftRiding walks every way the
// pending cookie can be refused and pins that each refusal also retracts the
// cookie.
//
// The payload's own `ExpiresAt` already fails closed, so a stale value is not a
// bypass — it is a value that cannot do anything except keep being sent. The
// cookie is session-scoped at path "/", so before this each of these branches
// left it riding on every request for the rest of the browser session, and only
// success and the rate-limit branch ever cleared anything.
//
// The anchor comes first: a freshly sealed cookie is written and reads back
// without being touched. Without it every case below would also pass against a
// reader that refuses everything and clears unconditionally.
func TestTOTPPendingCookieUnusableValueIsClearedNotLeftRiding(t *testing.T) {
	secretKey := []byte("test-secret-key")
	app, _ := newTOTPCookieTestApp(t, secretKey)

	sealResponse := mustAppResponse(t, app, httptest.NewRequest(http.MethodGet, "/seal-pending?user_id=42&remember_me=true", nil))
	usable := captureCookieValue(t, sealResponse, totpPendingCookieName)

	validRead := mustAppResponse(t, app, requestWithCookie(t, "/parse-pending", totpPendingCookieName, usable))
	if validRead.StatusCode != http.StatusOK {
		t.Fatalf("a freshly sealed pending cookie must still parse, got status %d (%s)", validRead.StatusCode, mustReadBodyString(t, validRead.Body))
	}
	assertTOTPCookieLeftInPlace(t, validRead, totpPendingCookieName)

	unusable := []struct {
		name  string
		value string
	}{
		{name: "not_a_sealed_envelope", value: "definitely-not-a-sealed-cookie-value"},
		{name: "tampered_ciphertext", value: flipLastBaseEncodedByte(t, usable)},
		{
			name:  "sealed_but_not_the_expected_json",
			value: sealTOTPCookiePlaintextForTest(t, secretKey, totpPendingCookieName, []byte("[not the payload shape]")),
		},
		{
			name: "payload_naming_no_account",
			value: sealTOTPCookiePayloadForTest(t, secretKey, totpPendingCookieName, totpPendingCookiePayload{
				UserID:    0,
				ExpiresAt: time.Now().Add(5 * time.Minute),
			}),
		},
		{
			name: "expired_payload",
			value: sealTOTPCookiePayloadForTest(t, secretKey, totpPendingCookieName, totpPendingCookiePayload{
				UserID:    42,
				ExpiresAt: time.Now().Add(-1 * time.Minute),
			}),
		},
	}
	for _, refusal := range unusable {
		t.Run(refusal.name, func(t *testing.T) {
			response := mustAppResponse(t, app, requestWithCookie(t, "/parse-pending", totpPendingCookieName, refusal.value))
			if response.StatusCode != http.StatusBadRequest {
				t.Fatalf("expected %s to be refused, got status %d", refusal.name, response.StatusCode)
			}
			assertTOTPCookieCleared(t, response, totpPendingCookieName)
		})
	}

	// The remaining branch is the one no cookie value can reach: the codec itself
	// unavailable, which is how a deployment with no usable secret key behaves.
	// The cookie is no more usable then than in any case above.
	t.Run("codec_unavailable", func(t *testing.T) {
		keyless, _ := newTOTPCookieTestApp(t, nil)
		response := mustAppResponse(t, keyless, requestWithCookie(t, "/parse-pending", totpPendingCookieName, usable))
		if response.StatusCode != http.StatusBadRequest {
			t.Fatalf("expected an unopenable pending cookie to be refused, got status %d", response.StatusCode)
		}
		assertTOTPCookieCleared(t, response, totpPendingCookieName)
	})
}

// TestTOTPSetupCookieUnusableValueIsClearedNotLeftRiding is the same sweep on the
// enrollment cookie, where the stakes are higher: its payload carries the RAW
// TOTP secret of an enrollment that was never confirmed. An abandoned or expired
// one used to stay in transport on every later request of the session.
func TestTOTPSetupCookieUnusableValueIsClearedNotLeftRiding(t *testing.T) {
	secretKey := []byte("test-secret-key")
	app, _ := newTOTPCookieTestApp(t, secretKey)

	const owner = uint(42)
	const otherOwner = uint(43)
	const rawSecret = "JBSWY3DPEHPK3PXP"

	sealResponse := mustAppResponse(t, app, httptest.NewRequest(http.MethodGet, "/seal-setup?user_id=42&raw_secret="+rawSecret, nil))
	usable := captureCookieValue(t, sealResponse, totpSetupCookieName)

	validRead := parseTOTPSetupAs(t, app, owner, totpSetupCookieName+"="+usable)
	defer func() { _ = validRead.Body.Close() }()
	if validRead.StatusCode != http.StatusOK {
		t.Fatalf("a freshly sealed setup cookie must still parse for its own owner, got status %d (%s)", validRead.StatusCode, mustReadBodyString(t, validRead.Body))
	}
	assertTOTPCookieLeftInPlace(t, validRead, totpSetupCookieName)

	unusable := []struct {
		name    string
		session uint
		value   string
	}{
		{name: "not_a_sealed_envelope", session: owner, value: "definitely-not-a-sealed-cookie-value"},
		{name: "tampered_ciphertext", session: owner, value: flipLastBaseEncodedByte(t, usable)},
		{
			name:    "sealed_but_not_the_expected_json",
			session: owner,
			value:   sealTOTPCookiePlaintextForTest(t, secretKey, totpSetupCookieName, []byte("[not the payload shape]")),
		},
		{name: "payload_minted_for_another_owner", session: otherOwner, value: usable},
		{
			name:    "payload_naming_no_account",
			session: owner,
			value: sealTOTPCookiePayloadForTest(t, secretKey, totpSetupCookieName, totpSetupCookiePayload{
				UserID:    0,
				RawSecret: rawSecret,
				ExpiresAt: time.Now().Add(5 * time.Minute),
			}),
		},
		{
			name:    "payload_carrying_no_secret",
			session: owner,
			value: sealTOTPCookiePayloadForTest(t, secretKey, totpSetupCookieName, totpSetupCookiePayload{
				UserID:    owner,
				RawSecret: "",
				ExpiresAt: time.Now().Add(5 * time.Minute),
			}),
		},
		{
			name:    "expired_payload",
			session: owner,
			value: sealTOTPCookiePayloadForTest(t, secretKey, totpSetupCookieName, totpSetupCookiePayload{
				UserID:    owner,
				RawSecret: rawSecret,
				ExpiresAt: time.Now().Add(-1 * time.Minute),
			}),
		},
	}
	for _, refusal := range unusable {
		t.Run(refusal.name, func(t *testing.T) {
			response := parseTOTPSetupAs(t, app, refusal.session, totpSetupCookieName+"="+refusal.value)
			defer func() { _ = response.Body.Close() }()

			if response.StatusCode != http.StatusBadRequest {
				t.Fatalf("expected %s to be refused, got status %d", refusal.name, response.StatusCode)
			}
			assertTOTPCookieCleared(t, response, totpSetupCookieName)
		})
	}

	t.Run("codec_unavailable", func(t *testing.T) {
		keyless, _ := newTOTPCookieTestApp(t, nil)
		response := parseTOTPSetupAs(t, keyless, owner, totpSetupCookieName+"="+usable)
		defer func() { _ = response.Body.Close() }()

		if response.StatusCode != http.StatusBadRequest {
			t.Fatalf("expected an unopenable setup cookie to be refused, got status %d", response.StatusCode)
		}
		assertTOTPCookieCleared(t, response, totpSetupCookieName)
	})
}

func requestWithCookie(t *testing.T, path string, cookieName string, cookieValue string) *http.Request {
	t.Helper()

	request := httptest.NewRequest(http.MethodGet, path, nil)
	request.Header.Set("Cookie", cookieName+"="+cookieValue)
	return request
}

func TestTOTPPendingCookie_WrongSigningKey_ParseError(t *testing.T) {
	sealedSecret := []byte("seal-key-original")
	openSecret := []byte("open-key-different")

	sealApp, _ := newTOTPCookieTestApp(t, sealedSecret)
	sealReq := httptest.NewRequest(http.MethodGet, "/seal-pending?user_id=7", nil)
	sealResp, err := sealApp.Test(sealReq, testConfigNoTimeout)
	if err != nil {
		t.Fatalf("GET /seal-pending: %v", err)
	}
	defer func() { _ = sealResp.Body.Close() }()
	cookieValue := captureCookieValue(t, sealResp, totpPendingCookieName)

	openApp, _ := newTOTPCookieTestApp(t, openSecret)
	parseReq := httptest.NewRequest(http.MethodGet, "/parse-pending", nil)
	parseReq.Header.Set("Cookie", totpPendingCookieName+"="+cookieValue)
	parseResp, err := openApp.Test(parseReq, testConfigNoTimeout)
	if err != nil {
		t.Fatalf("GET /parse-pending: %v", err)
	}
	defer func() { _ = parseResp.Body.Close() }()
	if parseResp.StatusCode != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400", parseResp.StatusCode)
	}
	body, _ := io.ReadAll(parseResp.Body)
	if !strings.Contains(string(body), "invalid") {
		t.Errorf("error %q, want to contain %q", string(body), "invalid")
	}
}

// --- TOTP setup cookie ---

func TestTOTPSetupCookie_RoundTrip(t *testing.T) {
	app, _ := newTOTPCookieTestApp(t, []byte("test-secret-key"))

	const rawSecret = "JBSWY3DPEHPK3PXP"

	sealReq := httptest.NewRequest(http.MethodGet, "/seal-setup?user_id=42&raw_secret="+rawSecret, nil)
	sealResp, err := app.Test(sealReq, testConfigNoTimeout)
	if err != nil {
		t.Fatalf("GET /seal-setup: %v", err)
	}
	defer func() { _ = sealResp.Body.Close() }()
	if sealResp.StatusCode != http.StatusOK {
		t.Fatalf("seal status = %d, want 200", sealResp.StatusCode)
	}
	cookieValue := captureCookieValue(t, sealResp, totpSetupCookieName)

	parseReq := httptest.NewRequest(http.MethodGet, "/parse-setup?user_id=42", nil)
	parseReq.Header.Set("Cookie", totpSetupCookieName+"="+cookieValue)
	parseResp, err := app.Test(parseReq, testConfigNoTimeout)
	if err != nil {
		t.Fatalf("GET /parse-setup: %v", err)
	}
	defer func() { _ = parseResp.Body.Close() }()
	if parseResp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(parseResp.Body)
		t.Fatalf("parse status = %d, body = %q", parseResp.StatusCode, body)
	}

	var got struct {
		RawSecret string `json:"raw_secret"`
	}
	if err := json.NewDecoder(parseResp.Body).Decode(&got); err != nil {
		t.Fatalf("decode parse response: %v", err)
	}
	if got.RawSecret != rawSecret {
		t.Errorf("raw_secret = %q, want %q", got.RawSecret, rawSecret)
	}
}

func TestTOTPSetupCookie_ExpiredPayload_ParseError(t *testing.T) {
	secretKey := []byte("test-secret-key")
	app, _ := newTOTPCookieTestApp(t, secretKey)

	// Attributed to the parsing account on purpose: the owner check runs first,
	// so an unattributed payload would be refused before expiry is ever reached
	// and this test would stop covering the TTL.
	sealed := sealTOTPCookiePayloadForTest(t, secretKey, totpSetupCookieName, totpSetupCookiePayload{
		UserID:    42,
		RawSecret: "JBSWY3DPEHPK3PXP",
		ExpiresAt: time.Now().Add(-1 * time.Minute),
	})

	req := httptest.NewRequest(http.MethodGet, "/parse-setup?user_id=42", nil)
	req.Header.Set("Cookie", totpSetupCookieName+"="+sealed)
	resp, err := app.Test(req, testConfigNoTimeout)
	if err != nil {
		t.Fatalf("GET /parse-setup: %v", err)
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400", resp.StatusCode)
	}
	body, _ := io.ReadAll(resp.Body)
	if !strings.Contains(string(body), "expired") {
		t.Errorf("error %q, want to contain %q", string(body), "expired")
	}
}

// TestTOTPSetupCookieRefusesSecretlessSeal pins the seal-side half of the
// secret-presence validation. The reader has always refused a payload whose
// RawSecret is blank; without the matching writer refusal a caller that lost
// the secret seals a well-formed, correctly-attributed cookie that only fails
// later, at read time, in a different request. Both ends of one payload apply
// the same validation. The positive anchor proves a real secret still seals.
func TestTOTPSetupCookieRefusesSecretlessSeal(t *testing.T) {
	secretKey := []byte("test-secret-key")
	app, _ := newTOTPCookieTestApp(t, secretKey)

	for _, testCase := range []struct {
		name      string
		rawSecret string
	}{
		{name: "empty", rawSecret: ""},
		{name: "whitespace only", rawSecret: "%20%20"},
	} {
		refused, err := app.Test(httptest.NewRequest(http.MethodGet, "/seal-setup?user_id=42&raw_secret="+testCase.rawSecret, nil), testConfigNoTimeout)
		if err != nil {
			t.Fatalf("GET /seal-setup with a %s secret: %v", testCase.name, err)
		}
		defer func() { _ = refused.Body.Close() }()
		if refused.StatusCode != http.StatusInternalServerError {
			t.Fatalf("expected setTOTPSetupCookie to refuse a %s secret, got status %d", testCase.name, refused.StatusCode)
		}
		if written := responseCookieValue(refused.Cookies(), totpSetupCookieName); written != "" {
			t.Fatalf("a refused seal must not leave a usable setup cookie, got %q", written)
		}
	}

	// Positive anchor: the same route still seals a real secret.
	sealed, err := app.Test(httptest.NewRequest(http.MethodGet, "/seal-setup?user_id=42&raw_secret=JBSWY3DPEHPK3PXP", nil), testConfigNoTimeout)
	if err != nil {
		t.Fatalf("GET /seal-setup with a real secret: %v", err)
	}
	defer func() { _ = sealed.Body.Close() }()
	_ = captureCookieValue(t, sealed, totpSetupCookieName)
}

// TestTOTPSetupCookieRefusesUnattributedOwner pins both halves of the owner
// scoping on the enrollment setup cookie, at the seal boundary and at the read
// boundary, so neither depends on the other.
//
// The payload carries the raw TOTP secret. A payload whose `user_id` is zero
// names no account, so there is nothing to scope that secret to; treating it as
// "no owner to compare against, so let it through" would enrol it against
// whichever session presented the cookie. So:
//   - the writer refuses to seal a zero owner id at all, and clears any prior
//     cookie rather than leaving a stale one presentable, and
//   - the reader refuses a zero owner id in the payload, refuses a payload
//     minted for a different account, and refuses any payload when the session
//     itself has no id.
//
// The positive anchor proves the cookie still round-trips for the account it was
// minted for; without it every assertion here would also pass against a reader
// that returns nothing to anybody.
func TestTOTPSetupCookieRefusesUnattributedOwner(t *testing.T) {
	secretKey := []byte("test-secret-key")
	app, _ := newTOTPCookieTestApp(t, secretKey)

	const ownerA = uint(42)
	const ownerB = uint(43)
	const rawSecret = "JBSWY3DPEHPK3PXP"

	// Seal-time guard: an owner id is mandatory, and the refusal leaves no
	// usable cookie behind.
	refusedSeal, err := app.Test(httptest.NewRequest(http.MethodGet, "/seal-setup?user_id=0&raw_secret="+rawSecret, nil), testConfigNoTimeout)
	if err != nil {
		t.Fatalf("GET /seal-setup with a zero owner id: %v", err)
	}
	defer func() { _ = refusedSeal.Body.Close() }()
	if refusedSeal.StatusCode != http.StatusInternalServerError {
		t.Fatalf("expected setTOTPSetupCookie to refuse a zero owner id, got status %d", refusedSeal.StatusCode)
	}
	if written := responseCookieValue(refusedSeal.Cookies(), totpSetupCookieName); written != "" {
		t.Fatalf("a refused seal must not leave a usable setup cookie, got %q", written)
	}

	// Positive anchor: owner A's own payload still parses back for owner A.
	ownedSeal, err := app.Test(httptest.NewRequest(http.MethodGet, "/seal-setup?user_id=42&raw_secret="+rawSecret, nil), testConfigNoTimeout)
	if err != nil {
		t.Fatalf("GET /seal-setup for owner A: %v", err)
	}
	defer func() { _ = ownedSeal.Body.Close() }()
	ownedCookie := totpSetupCookieName + "=" + captureCookieValue(t, ownedSeal, totpSetupCookieName)

	ownedParse := parseTOTPSetupAs(t, app, ownerA, ownedCookie)
	defer func() { _ = ownedParse.Body.Close() }()
	if ownedParse.StatusCode != http.StatusOK {
		t.Fatalf("owner A must still recover the secret sealed for owner A, got status %d (%s)", ownedParse.StatusCode, mustReadBodyString(t, ownedParse.Body))
	}

	// Read-time guards. A hand-sealed payload with no owner is the shape the
	// writer now refuses to mint; a payload minted for owner A is the shape a
	// stale cookie on a household instance would have.
	unattributed := sealTOTPSetupCookieForTest(t, secretKey, 0, rawSecret)
	foreign := sealTOTPSetupCookieForTest(t, secretKey, ownerA, rawSecret)

	rejections := []struct {
		name    string
		session uint
		cookie  string
	}{
		{name: "unattributed_payload", session: ownerA, cookie: unattributed},
		{name: "payload_minted_for_another_owner", session: ownerB, cookie: foreign},
		{name: "session_without_an_owner_id", session: 0, cookie: ownedCookie},
	}
	for _, rejection := range rejections {
		t.Run(rejection.name, func(t *testing.T) {
			response := parseTOTPSetupAs(t, app, rejection.session, rejection.cookie)
			defer func() { _ = response.Body.Close() }()

			if response.StatusCode != http.StatusBadRequest {
				t.Fatalf("expected %s to be refused, got status %d", rejection.name, response.StatusCode)
			}
			if body := mustReadBodyString(t, response.Body); strings.Contains(body, rawSecret) {
				t.Fatalf("a refused setup cookie must not surface the pending secret, got %q", body)
			}
		})
	}
}

func parseTOTPSetupAs(t *testing.T, app *fiber.App, sessionUserID uint, cookie string) *http.Response {
	t.Helper()

	request := httptest.NewRequest(http.MethodGet, "/parse-setup?user_id="+strconv.FormatUint(uint64(sessionUserID), 10), nil)
	request.Header.Set("Cookie", cookie)
	response, err := app.Test(request, testConfigNoTimeout)
	if err != nil {
		t.Fatalf("GET /parse-setup: %v", err)
	}
	return response
}

func TestTOTPSetupCookie_WrongSigningKey_ParseError(t *testing.T) {
	sealedSecret := []byte("seal-key-original")
	openSecret := []byte("open-key-different")

	sealApp, _ := newTOTPCookieTestApp(t, sealedSecret)
	sealReq := httptest.NewRequest(http.MethodGet, "/seal-setup?user_id=42&raw_secret=JBSWY3DPEHPK3PXP", nil)
	sealResp, err := sealApp.Test(sealReq, testConfigNoTimeout)
	if err != nil {
		t.Fatalf("GET /seal-setup: %v", err)
	}
	defer func() { _ = sealResp.Body.Close() }()
	cookieValue := captureCookieValue(t, sealResp, totpSetupCookieName)

	openApp, _ := newTOTPCookieTestApp(t, openSecret)
	parseReq := httptest.NewRequest(http.MethodGet, "/parse-setup?user_id=42", nil)
	parseReq.Header.Set("Cookie", totpSetupCookieName+"="+cookieValue)
	parseResp, err := openApp.Test(parseReq, testConfigNoTimeout)
	if err != nil {
		t.Fatalf("GET /parse-setup: %v", err)
	}
	defer func() { _ = parseResp.Body.Close() }()
	if parseResp.StatusCode != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400", parseResp.StatusCode)
	}
	body, _ := io.ReadAll(parseResp.Body)
	if !strings.Contains(string(body), "invalid") {
		t.Errorf("error %q, want to contain %q", string(body), "invalid")
	}
}
