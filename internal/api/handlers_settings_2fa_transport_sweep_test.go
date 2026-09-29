package api

import (
	"errors"
	"net/http"
	"strconv"
	"strings"
	"testing"

	"github.com/ovumcy/ovumcy-web/internal/services"
	"github.com/valyala/fasthttp"
)

// This file sweeps the two 2FA settings mutations, PUT (enroll) and DELETE
// (disable) /api/v1/users/current/2fa, across the request-body cap and the
// transports a client can send them over. Every expectation below is written
// out per method and per encoding; none is derived from the body-limit guard's
// own method predicate, so excluding a method from that guard reddens the row
// that names it instead of moving the expectation along with the code.
//
// The app runs with the small bodyLimitGuardTestLimit cap. A wire body past it
// is refused by fasthttp while reading, before any route; app.Test surfaces
// that as fasthttp.ErrBodyTooLarge and discards the response written for it,
// so those rows assert the read error (the deployed ErrorHandler's 413 mapping
// is pinned by the entry point's own tests). A compressed body is capped again on its DECODED
// size by requestBodyLimitGuard, which answers the mapped request_too_large
// envelope before any handler runs.

// twoFASweepPassword is the password createOnboardingTestUser gives the
// account behind newSettingsSecurityTestContextWithOptions.
const twoFASweepPassword = "StrongPass1"

// twoFASweepBody encodes a padded JSON body of decodedSize bytes for the named
// encoding: "identity" sends it as is, "gzip" compresses it, so decodedSize is
// the wire size for the first and the decoded size for the second.
func twoFASweepBody(t *testing.T, encoding string, fields map[string]string, decodedSize int) (body, contentEncoding string) {
	t.Helper()
	plain := padBodyToSize(t, jsonBodyFor(fields), decodedSize)
	switch encoding {
	case "identity":
		return plain, ""
	case "gzip":
		return string(gzipTestBody(t, []byte(plain))), "gzip"
	default:
		t.Fatalf("unknown encoding %q", encoding)
		return "", ""
	}
}

// TestTOTPSettingsBodyLimitHoldsPerMethodAtAndPastTheCap is the per-method
// matrix: for each of PUT and DELETE, over a plain and a gzip JSON body, the
// request one byte past the cap is refused and changes nothing, and the same
// request at exactly the cap succeeds. The refusal runs first on the same
// account, so it is judged in a state where success was still reachable.
func TestTOTPSettingsBodyLimitHoldsPerMethodAtAndPastTheCap(t *testing.T) {
	const limit = bodyLimitGuardTestLimit

	rows := []struct {
		operation string
		method    string
		encoding  string
		// overCapEnvelope is true where the refusal is requestBodyLimitGuard's
		// mapped envelope, false where fasthttp refuses the wire body while
		// reading it and app.Test returns fasthttp.ErrBodyTooLarge, no response.
		overCapEnvelope bool
	}{
		{operation: "enroll 2FA, plain JSON", method: http.MethodPut, encoding: "identity", overCapEnvelope: false},
		{operation: "enroll 2FA, gzip JSON", method: http.MethodPut, encoding: "gzip", overCapEnvelope: true},
		{operation: "disable 2FA, plain JSON", method: http.MethodDelete, encoding: "identity", overCapEnvelope: false},
		{operation: "disable 2FA, gzip JSON", method: http.MethodDelete, encoding: "gzip", overCapEnvelope: true},
	}

	for _, row := range rows {
		t.Run(row.operation, func(t *testing.T) {
			ctx := newSettingsSecurityTestContextWithOptions(t,
				"totp-sweep-"+strings.ToLower(row.method)+"-"+row.encoding+"@example.com",
				onboardingTestAppOptions{enableCSRF: true, bodyLimit: limit})

			setupCookie, secret := "", ""
			enabledBefore := false
			if row.method == http.MethodPut {
				setupCookie, secret = enrollmentSecretFixture(t, ctx)
			} else {
				enableTOTPForSettingsTest(t, &ctx)
				enabledBefore = true
			}

			// The enrollment code is minted as each request is built, so the
			// at-cap success never carries a code whose step ran out while the
			// refusal before it was being served.
			request := func(size int) twoFARequest {
				fields := map[string]string{"password": twoFASweepPassword}
				if secret != "" {
					fields["code"] = currentTOTPCode(t, secret)
				}
				body, contentEncoding := twoFASweepBody(t, row.encoding, fields, size)
				return twoFARequest{
					method: row.method, contentType: "application/json", contentEncoding: contentEncoding,
					body: body, setupCookie: setupCookie, withSession: true, withCSRFHead: true,
				}
			}

			label := row.operation + " at " + strconv.Itoa(limit+1) + " bytes"
			if row.overCapEnvelope {
				assert2FARefusal(t, send2FARequest(t, ctx, request(limit+1)),
					http.StatusRequestEntityTooLarge, `"error":"request_too_large"`, label)
			} else {
				// fasthttp refuses the body while reading it; app.Test reports
				// that read error instead of the response written for it.
				resp, err := ctx.app.Test(new2FARequest(ctx, request(limit+1)), testConfigNoTimeout)
				if err == nil {
					_ = resp.Body.Close()
				}
				if !errors.Is(err, fasthttp.ErrBodyTooLarge) {
					t.Errorf("%s: app.Test error = %v, want the wire cap's %v", label, err, fasthttp.ErrBodyTooLarge)
				}
			}
			if got := totpEnabledInDatabase(t, ctx); got != enabledBefore {
				t.Fatalf("%s: 2FA enabled = %v after the refusal, want it unchanged at %v", label, got, enabledBefore)
			}

			assert2FAOkEnvelope(t, ctx.app, send2FARequest(t, ctx, request(limit)))
			if got := totpEnabledInDatabase(t, ctx); got == enabledBefore {
				t.Fatalf("%s at exactly %d bytes: 2FA enabled = %v, want the request to have taken effect", row.operation, limit, got)
			}
		})
	}
}

// TestTOTPDisableOverCapCompressedBodyDrawsNothingFromTheBudget pins that a
// compressed DELETE refused on its decoded size never reached the handler: the
// correct password in such a body leaves 2FA on, and a full budget's worth of
// wrong passwords in such bodies leaves the failed-password budget untouched,
// so the correct request that follows still disables 2FA. The last subtest is
// the anchor: the same wrong passwords inside the cap do exhaust it.
//
// The budget claim has its own account. Fiber's over-limit placeholder text
// fails to bind and would never draw an attempt, so the claim is only falsified
// by a handler that received the password; the padded body carries it whole in
// its first limit bytes, so a guard that forwarded the decoded prefix instead of
// refusing would draw the budget here, even behind a 413 it still answered.
func TestTOTPDisableOverCapCompressedBodyDrawsNothingFromTheBudget(t *testing.T) {
	const limit = bodyLimitGuardTestLimit

	sendGzipDelete := func(t *testing.T, ctx settingsSecurityTestContext, password string, decodedSize int) *http.Response {
		t.Helper()
		body, contentEncoding := twoFASweepBody(t, "gzip", map[string]string{"password": password}, decodedSize)
		return send2FARequest(t, ctx, twoFARequest{
			method: http.MethodDelete, contentType: "application/json", contentEncoding: contentEncoding,
			body: body, withSession: true, withCSRFHead: true,
		})
	}

	t.Run("refused past the cap: the correct password leaves 2FA on", func(t *testing.T) {
		ctx := newSettingsSecurityTestContextWithOptions(t, "totp-sweep-overcap-correct@example.com",
			onboardingTestAppOptions{enableCSRF: true, bodyLimit: limit})
		enableTOTPForSettingsTest(t, &ctx)

		assert2FARefusal(t, sendGzipDelete(t, ctx, twoFASweepPassword, limit+1),
			http.StatusRequestEntityTooLarge, `"error":"request_too_large"`, "correct password past the cap")
		if !totpEnabledInDatabase(t, ctx) {
			t.Fatal("a compressed body refused on its decoded size disabled 2FA")
		}
	})

	t.Run("refused past the cap: wrong passwords draw nothing from the budget", func(t *testing.T) {
		ctx := newSettingsSecurityTestContextWithOptions(t, "totp-sweep-budget-overcap@example.com",
			onboardingTestAppOptions{enableCSRF: true, bodyLimit: limit})
		enableTOTPForSettingsTest(t, &ctx)

		for attempt := range services.DefaultTOTPDisableAttemptsLimit {
			assert2FARefusal(t, sendGzipDelete(t, ctx, "WrongPass9", limit+1),
				http.StatusRequestEntityTooLarge, `"error":"request_too_large"`,
				"wrong password past the cap, attempt "+strconv.Itoa(attempt+1))
		}

		resp := sendGzipDelete(t, ctx, twoFASweepPassword, limit)
		if resp.StatusCode == http.StatusTooManyRequests {
			t.Fatal("refused over-cap bodies drew the failed-password budget: the correct request after them was throttled")
		}
		assert2FAOkEnvelope(t, ctx.app, resp)
		if totpEnabledInDatabase(t, ctx) {
			t.Fatal("the correct request after the refused ones did not disable 2FA")
		}
	})

	t.Run("anchor: the same wrong passwords inside the cap exhaust the budget", func(t *testing.T) {
		ctx := newSettingsSecurityTestContextWithOptions(t, "totp-sweep-budget-anchor@example.com",
			onboardingTestAppOptions{enableCSRF: true, bodyLimit: limit})
		enableTOTPForSettingsTest(t, &ctx)

		for attempt := range services.DefaultTOTPDisableAttemptsLimit {
			assert2FARefusal(t, sendGzipDelete(t, ctx, "WrongPass9", limit),
				http.StatusUnauthorized, "invalid credentials",
				"wrong password inside the cap, attempt "+strconv.Itoa(attempt+1))
		}

		assert2FARefusal(t, sendGzipDelete(t, ctx, twoFASweepPassword, limit),
			http.StatusTooManyRequests, "totp too many attempts", "correct password after an exhausted budget")
		if !totpEnabledInDatabase(t, ctx) {
			t.Fatal("2FA was disabled past an exhausted failed-password budget")
		}
	})
}

// TestTOTPDisableRefusesACompressedFormBody pins that a gzip urlencoded DELETE
// inside the cap is not a submission: form parsing reads the raw bytes and
// never decompresses them, so the handler finds no password, answers invalid
// input and leaves 2FA on. The CSRF token rides the header, because the body
// cannot carry it.
//
// The form is padded so deflate really compresses it. A short or random-looking
// input comes out as a stored block, and then the raw bytes still carry the
// form in the clear, with the gzip header before it and the end-of-stream bytes
// after it: `password=StrongPass1&csrf_token=<token>` gzipped that way parses
// as a password of "StrongPass1" plus those trailing bytes, a wrong password
// that answers 401 and draws the disable budget rather than the 400 pinned
// here. The precondition keeps this test on the compressed case it names.
func TestTOTPDisableRefusesACompressedFormBody(t *testing.T) {
	ctx := newSettingsSecurityTestContextWithOptions(t, "totp-sweep-gzip-form@example.com",
		onboardingTestAppOptions{enableCSRF: true, bodyLimit: bodyLimitGuardTestLimit})
	enableTOTPForSettingsTest(t, &ctx)

	form := "password=" + twoFASweepPassword + "&padding=" + strings.Repeat("a", 256)
	compressed := string(gzipTestBody(t, []byte(form)))
	if strings.Contains(compressed, "password=") {
		t.Fatalf("the gzip body carries the form in the clear (a stored block); this test needs it compressed")
	}
	resp := send2FARequest(t, ctx, twoFARequest{
		method: http.MethodDelete, contentType: "application/x-www-form-urlencoded", contentEncoding: "gzip",
		body: compressed, withSession: true, withCSRFHead: true,
	})
	assert2FARefusal(t, resp, http.StatusBadRequest, "invalid settings input", "gzip urlencoded disable")
	if !totpEnabledInDatabase(t, ctx) {
		t.Fatal("a compressed form body disabled 2FA")
	}
}

// TestTOTPSettingsJSONRefusalsHoldOverBothEncodings pins the JSON refusals of
// both 2FA mutations over a plain and a gzip body: a wrong password and a
// missing session are 401, a wrong enrollment code is 401 totp invalid code,
// and a missing CSRF header is the CSRF middleware's 403. None of them changes
// the 2FA state.
func TestTOTPSettingsJSONRefusalsHoldOverBothEncodings(t *testing.T) {
	for _, encoding := range []string{"identity", "gzip"} {
		encode := func(t *testing.T, fields map[string]string) (string, string) {
			t.Helper()
			plain := jsonBodyFor(fields)
			if encoding == "gzip" {
				return string(gzipTestBody(t, []byte(plain))), "gzip"
			}
			return plain, ""
		}

		t.Run("enroll 2FA, "+encoding, func(t *testing.T) {
			ctx := newSettingsSecurityTestContextWithOptions(t, "totp-sweep-refusals-put-"+encoding+"@example.com",
				onboardingTestAppOptions{enableCSRF: true, auditLogEnabled: true})
			setupCookie, code, wrongCode := enrollmentFixture(t, ctx)

			put := func(fields map[string]string, withSession, withCSRFHead bool) twoFARequest {
				body, contentEncoding := encode(t, fields)
				return twoFARequest{
					method: http.MethodPut, contentType: "application/json", contentEncoding: contentEncoding,
					body: body, setupCookie: setupCookie, withSession: withSession, withCSRFHead: withCSRFHead,
				}
			}
			valid := map[string]string{"password": twoFASweepPassword, "code": code}

			assert2FARefusal(t, send2FARequest(t, ctx, put(map[string]string{"password": "WrongPass9", "code": code}, true, true)),
				http.StatusUnauthorized, "invalid password", "wrong password")
			assert2FARefusal(t, send2FARequest(t, ctx, put(map[string]string{"password": twoFASweepPassword, "code": wrongCode}, true, true)),
				http.StatusUnauthorized, "totp invalid code", "wrong code")
			assert2FARefusal(t, send2FARequest(t, ctx, put(valid, false, true)), http.StatusUnauthorized, "unauthorized", "missing session")
			assert2FACSRFRefusal(t, ctx, put(valid, true, false))
			if totpEnabledInDatabase(t, ctx) {
				t.Fatal("a refused enrollment enabled 2FA")
			}
		})

		t.Run("disable 2FA, "+encoding, func(t *testing.T) {
			ctx := newSettingsSecurityTestContextWithOptions(t, "totp-sweep-refusals-delete-"+encoding+"@example.com",
				onboardingTestAppOptions{enableCSRF: true, auditLogEnabled: true})
			enableTOTPForSettingsTest(t, &ctx)

			del := func(fields map[string]string, withSession, withCSRFHead bool) twoFARequest {
				body, contentEncoding := encode(t, fields)
				return twoFARequest{
					method: http.MethodDelete, contentType: "application/json", contentEncoding: contentEncoding,
					body: body, withSession: withSession, withCSRFHead: withCSRFHead,
				}
			}
			valid := map[string]string{"password": twoFASweepPassword}

			assert2FARefusal(t, send2FARequest(t, ctx, del(map[string]string{"password": "WrongPass9"}, true, true)),
				http.StatusUnauthorized, "invalid credentials", "wrong password")
			assert2FARefusal(t, send2FARequest(t, ctx, del(valid, false, true)), http.StatusUnauthorized, "unauthorized", "missing session")
			assert2FACSRFRefusal(t, ctx, del(valid, true, false))
			if !totpEnabledInDatabase(t, ctx) {
				t.Fatal("a refused disable turned 2FA off")
			}
		})
	}
}

// assert2FACSRFRefusal sends spec, which carries no CSRF header, and requires
// the 403 to be the CSRF middleware's own: the response body is the framework's
// bare Forbidden and says nothing about who refused, so the proof is the
// csrf/denied security event with reason "missing token", which only the CSRF
// ErrorHandler emits. Any other guard answering 403 in its place fails here.
// ctx's app must run with the audit stream on.
func assert2FACSRFRefusal(t *testing.T, ctx settingsSecurityTestContext, spec twoFARequest) {
	t.Helper()
	resp, logOutput := captureAuditedRequest(t, ctx.app, new2FARequest(ctx, spec))
	t.Cleanup(func() { _ = resp.Body.Close() })
	if resp.StatusCode != http.StatusForbidden {
		t.Errorf("missing CSRF header: status = %d, want 403", resp.StatusCode)
	}
	line := securityEventLine(t, logOutput, "csrf", "denied")
	if !strings.Contains(line, `reason="missing token"`) {
		t.Errorf("missing CSRF header: csrf denial %q does not name the missing token", line)
	}
}
