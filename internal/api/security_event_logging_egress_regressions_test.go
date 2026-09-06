package api

import (
	"bytes"
	"fmt"
	"log"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/gofiber/fiber/v3"
)

// Health-data EGRESS audit regressions. The sibling file
// security_event_logging_mutation_regression_test.go covers the mutation half:
// the actions that change or destroy tracked data. These cover the half that
// carries it out — the three export formats and the two one-time secret
// reveals — plus the discriminator that keeps an operator's erasure filter from
// collecting every routine download.
//
// They live in one file named after the mechanism rather than split across the
// surfaces' own aggregators, because the contract under test is the mechanism:
// a new egress surface that logs by hand fails here, not in its own suite.
//
// Field assertions resolve the ONE line an action/outcome pair produced through
// securityEventLine, which the mutation file owns: a request emits several
// security events, so a whole-buffer substring check passes as long as any line
// carries the field. Both halves of the audit stream share that discipline and
// therefore share the helper.

// captureAuditedRequest drives one request against an app with the audit stream
// on and returns the response together with the security-event output that
// request produced. The log writer is swapped around the request only, so the
// captured text holds the handler's own lines and not the sign-in that built
// the session.
func captureAuditedRequest(t *testing.T, app *fiber.App, request *http.Request) (*http.Response, string) {
	t.Helper()

	originalWriter := log.Writer()
	defer log.SetOutput(originalWriter)
	var output bytes.Buffer
	log.SetOutput(&output)

	response, err := app.Test(request, testConfigNoTimeout)
	if err != nil {
		t.Fatalf("audited request %s %s failed: %v", request.Method, request.URL.Path, err)
	}
	return response, output.String()
}

// assertHealthEgressAudited pins the fields the typed egress mechanism owns, on
// one line: the wire-visible action, the health_egress domain that separates
// "data left the instance" from "data changed", and a non-empty target naming
// what left. A handler that logs through the plain security-event path emits
// the action alone, so the domain and target assertions are what catch it.
func assertHealthEgressAudited(t *testing.T, logOutput string, action string, outcome string, target string, extra ...SecurityEventField) string {
	t.Helper()

	line := securityEventLine(t, logOutput, action, outcome)
	if !strings.Contains(line, fmt.Sprintf("domain=%q", healthEgressDomain)) {
		t.Fatalf("expected %s to be tagged domain=%q, got %q", action, healthEgressDomain, line)
	}
	if strings.Contains(line, fmt.Sprintf("domain=%q", healthDataDomain)) {
		t.Fatalf("expected %s to stay out of the mutation domain %q, got %q", action, healthDataDomain, line)
	}
	if target == "" {
		t.Fatalf("expected a non-empty target for %s", action)
	}
	if !strings.Contains(line, fmt.Sprintf("target=%q", target)) {
		t.Fatalf("expected %s to carry target=%q, got %q", action, target, line)
	}
	for _, field := range extra {
		if !strings.Contains(line, fmt.Sprintf("%s=%q", field.Key, field.Value)) {
			t.Fatalf("expected %s to carry %s=%q, got %q", action, field.Key, field.Value, line)
		}
	}
	return line
}

// TestExportIsAuditedAsHealthEgressOnEveryOutcome walks each export format
// through the three outcomes a download can have — the data left, the request
// was refused before anything was read, and storage failed underneath it — and
// requires all three to be attributable to that format. The denied branch is
// the one the shared prologue used to log with the action alone, so a refused
// CSV download and a refused JSON one were indistinguishable in the stream.
func TestExportIsAuditedAsHealthEgressOnEveryOutcome(t *testing.T) {
	for _, testCase := range []struct {
		format string
		path   string
	}{
		{format: "csv", path: "/api/v1/exports/csv"},
		{format: "json", path: "/api/v1/exports/json"},
		{format: "summary", path: "/api/v1/exports/summary"},
	} {
		t.Run(testCase.format, func(t *testing.T) {
			app, database := newOnboardingTestAppWithOptions(t, onboardingTestAppOptions{auditLogEnabled: true})
			user := createOnboardingTestUser(t, database, "export-egress-audit-"+testCase.format+"@example.com", "StrongPass1", true)
			authCookie := loginAndExtractAuthCookie(t, app, user.Email, "StrongPass1")
			formatField := securityEventField("export_format", testCase.format)

			succeeded, successLog := captureAuditedRequest(t, app, newExportRequestForTest(t, testCase.path, authCookie))
			defer func() { _ = succeeded.Body.Close() }()
			if succeeded.StatusCode != http.StatusOK {
				t.Fatalf("expected status 200 for an accepted %s export, got %d", testCase.format, succeeded.StatusCode)
			}
			successLine := assertHealthEgressAudited(t, successLog, dataExportAction, "success", exportEgressTarget, formatField)
			if !strings.Contains(successLine, fmt.Sprintf("user_id=%q", strconv.FormatUint(uint64(user.ID), 10))) {
				t.Fatalf("expected the export audit line to name the owner whose data left, got %q", successLine)
			}
			if strings.Contains(successLine, user.Email) {
				t.Fatalf("did not expect the owner's email in an egress audit line: %q", successLine)
			}

			denied, deniedLog := captureAuditedRequest(t, app, newExportRequestForTest(t, testCase.path+"?from=not-a-date", authCookie))
			defer func() { _ = denied.Body.Close() }()
			if denied.StatusCode != http.StatusBadRequest {
				t.Fatalf("expected status 400 for an unparseable %s export range, got %d", testCase.format, denied.StatusCode)
			}
			deniedLine := assertHealthEgressAudited(t, deniedLog, dataExportAction, "denied", exportEgressTarget, formatField)
			if !strings.Contains(deniedLine, `reason="invalid from date"`) {
				t.Fatalf("expected the mapped range-rejection reason on the denied line, got %q", deniedLine)
			}

			// Both export reads start at daily_logs, so dropping that table fails the
			// download without disturbing the users table the auth middleware reads.
			if err := database.Exec("DROP TABLE daily_logs").Error; err != nil {
				t.Fatalf("drop daily_logs: %v", err)
			}
			failed, failureLog := captureAuditedRequest(t, app, newExportRequestForTest(t, testCase.path, authCookie))
			defer func() { _ = failed.Body.Close() }()
			if failed.StatusCode != http.StatusInternalServerError {
				t.Fatalf("expected status 500 once %s export storage is gone, got %d", testCase.format, failed.StatusCode)
			}
			failureLine := assertHealthEgressAudited(t, failureLog, dataExportAction, "failure", exportEgressTarget, formatField)
			if !strings.Contains(failureLine, `reason="failed to fetch logs"`) {
				t.Fatalf("expected the mapped fetch-failure reason on the failure line, got %q", failureLine)
			}
		})
	}
}

// TestExportPrologueAttributesTheFormatWithoutASession covers the prologue's
// other refusal, the one routing cannot reach (both export routes sit behind
// AuthRequired): a call with no session on the context. It is asserted here
// because it shares the branch that lost the format field, and because a future
// route that forgets AuthRequired must still produce an attributable line.
func TestExportPrologueAttributesTheFormatWithoutASession(t *testing.T) {
	for _, testCase := range []struct {
		format string
		kind   healthEgressKind
	}{
		{format: "csv", kind: exportCSVEgress},
		{format: "json", kind: exportJSONEgress},
		{format: "summary", kind: exportSummaryEgress},
	} {
		t.Run(testCase.format, func(t *testing.T) {
			logOutput := captureEmittedSecurityEvent(t, "/api/v1/exports/"+testCase.format, func(handler *Handler, c fiber.Ctx) {
				_, _, _, spec := handler.exportUserAndRange(c, testCase.kind)
				if spec == nil {
					t.Fatal("expected the export prologue to refuse a request with no session user")
				}
			})
			assertHealthEgressAudited(t, logOutput, dataExportAction, "denied", exportEgressTarget,
				securityEventField("export_format", testCase.format))
		})
	}
}

// TestCalendarFeedRevealIsAuditedAsHealthEgress pins the audited unit for the
// subscribe URL: the moment the capability reaches a person, not the polls a
// calendar client makes with it afterwards. The reveal is one-time, so the test
// also proves the negative half — a replay of the original sealed cookie reveals
// nothing and logs nothing — against the first visit as its positive anchor.
func TestCalendarFeedRevealIsAuditedAsHealthEgress(t *testing.T) {
	ctx := newSettingsSecurityTestContextWithOptions(t, "feed-reveal-audit@example.com", onboardingTestAppOptions{enableCSRF: true, auditLogEnabled: true})

	generated := settingsFormRequestWithCSRF(t, ctx, http.MethodPost, "/api/v1/users/current/calendar-feed", url.Values{}, nil)
	defer func() { _ = generated.Body.Close() }()
	if generated.StatusCode != http.StatusSeeOther {
		t.Fatalf("expected 303 on feed generate, got %d", generated.StatusCode)
	}
	revealCookie := responseCookie(generated.Cookies(), calendarFeedRevealCookieName)
	if revealCookie == nil {
		t.Fatal("expected a sealed reveal cookie on the generate response")
	}

	revealRequest := httptest.NewRequest(http.MethodGet, calendarFeedRevealPath, nil)
	revealRequest.Header.Set("Accept-Language", "en")
	revealRequest.Header.Set("Cookie", joinCookieHeader(ctx.authCookie, cookiePair(revealCookie)))
	revealed, revealLog := captureAuditedRequest(t, ctx.app, revealRequest)
	defer func() { _ = revealed.Body.Close() }()
	if revealed.StatusCode != http.StatusOK {
		t.Fatalf("expected the reveal page to render, got %d", revealed.StatusCode)
	}

	// Positive anchor: the page really did hand over the URL, so the audit line
	// below is attached to a reveal that happened.
	revealBody := mustReadBodyString(t, revealed.Body)
	urlNode := htmlElementByID(mustParseHTMLDocument(t, revealBody), "calendar-feed-url")
	if urlNode == nil {
		t.Fatal("expected the reveal page to carry the subscribe URL element")
	}
	revealedURL := strings.TrimSpace(htmlNodeText(urlNode))
	token := extractFeedTokenFromURL(t, revealedURL)

	revealLine := assertHealthEgressAudited(t, revealLog, "settings.calendar_feed_reveal", "success", "calendar_feed")
	if strings.Contains(revealLine, token) || strings.Contains(revealLine, "/calendar/feed/") {
		t.Fatalf("the reveal audit line must record the fact, never the subscribe URL: %q", revealLine)
	}
	if !strings.Contains(revealLine, fmt.Sprintf("user_id=%q", strconv.FormatUint(uint64(ctx.user.ID), 10))) {
		t.Fatalf("expected the reveal audit line to name the owner it was shown to, got %q", revealLine)
	}

	clearedCookie := responseCookie(revealed.Cookies(), calendarFeedRevealCookieName)
	if clearedCookie == nil || strings.TrimSpace(clearedCookie.Value) != "" {
		t.Fatal("expected the reveal page to clear the one-time cookie")
	}
	// The second visit presents the ORIGINAL sealed value, not the cleared one:
	// a client that kept the cookie is the case this negative is about, and an
	// empty value proves only that an empty value reveals nothing. The audit
	// stream is the sharper half of the assertion, and it is read by OUTCOME, not
	// by action: an operator counting disclosures counts success lines, which is
	// already what data.export forces on them. A refused replay must therefore
	// carry no success line and must carry a denied one — silence would make the
	// attempt indistinguishable from nobody visiting.
	secondRequest := httptest.NewRequest(http.MethodGet, calendarFeedRevealPath, nil)
	secondRequest.Header.Set("Accept-Language", "en")
	secondRequest.Header.Set("Cookie", joinCookieHeader(ctx.authCookie, cookiePair(revealCookie)))
	second, secondLog := captureAuditedRequest(t, ctx.app, secondRequest)
	defer func() { _ = second.Body.Close() }()
	if second.StatusCode != http.StatusSeeOther {
		t.Fatalf("expected a replayed reveal cookie to redirect, got %d", second.StatusCode)
	}
	if strings.Contains(secondLog, `outcome="success"`) {
		t.Fatalf("a visit that reveals nothing must not log a disclosure, got %q", secondLog)
	}
	deniedLine := assertHealthEgressAudited(t, secondLog, "settings.calendar_feed_reveal", "denied", "calendar_feed")
	if strings.Contains(deniedLine, token) || strings.Contains(deniedLine, "/calendar/feed/") {
		t.Fatalf("a refused reveal must record the refusal, never the subscribe URL: %q", deniedLine)
	}
}

// TestRecoveryCodeRevealIsAuditedOnEverySurfaceThatShowsIt sweeps the whole set
// of surfaces that put a recovery code in front of a person: the dedicated
// reveal page and the inline block the register page renders straight after
// sign-up. Auditing one and not the other would leave the new filter looking
// complete while under-reporting reveals, which is the defect class this
// mechanism exists to close.
func TestRecoveryCodeRevealIsAuditedOnEverySurfaceThatShowsIt(t *testing.T) {
	t.Run("dedicated page", func(t *testing.T) {
		app, database := newOnboardingTestAppWithOptions(t, onboardingTestAppOptions{auditLogEnabled: true})
		owner := createOnboardingTestUser(t, database, "recovery-reveal-dedicated-audit@example.com", "StrongPass1", true)
		authCookie := loginAndExtractAuthCookie(t, app, owner.Email, "StrongPass1")

		const revealedCode = "OVUM-DEDI-CATE-D001"
		sealed := recoveryCodePageCookieForTest(t, owner.ID, revealedCode, time.Now().Add(recoveryCodeCookieTTL))

		request := httptest.NewRequest(http.MethodGet, "/recovery-code", nil)
		request.Header.Set("Accept-Language", "en")
		request.Header.Set("Cookie", authCookie+"; "+recoveryCodeCookieName+"="+sealed)
		response, logOutput := captureAuditedRequest(t, app, request)
		defer func() { _ = response.Body.Close() }()

		if response.StatusCode != http.StatusOK {
			t.Fatalf("expected the dedicated reveal page to render, got %d", response.StatusCode)
		}
		if !strings.Contains(mustReadBodyString(t, response.Body), revealedCode) {
			t.Fatal("expected the dedicated page to actually reveal the code the audit line claims")
		}
		assertRecoveryCodeRevealAudited(t, logOutput, revealedCode, owner.ID)
	})

	t.Run("inline register block", func(t *testing.T) {
		app, database := newOnboardingTestAppWithOptions(t, onboardingTestAppOptions{auditLogEnabled: true})
		authCookie, recoveryCookie := registerAndExtractRecoveryCookies(t, app, "recovery-reveal-inline-audit@example.com", "StrongPass1")
		if authCookie == "" || recoveryCookie == "" {
			t.Fatal("expected auth and recovery cookies from the register pickup")
		}
		var ownerID uint
		if err := database.Raw("SELECT id FROM users WHERE email = ?", "recovery-reveal-inline-audit@example.com").Scan(&ownerID).Error; err != nil {
			t.Fatalf("load registered owner id: %v", err)
		}

		request := httptest.NewRequest(http.MethodGet, "/register", nil)
		request.Header.Set("Accept-Language", "en")
		request.Header.Set("Cookie", authCookieName+"="+authCookie+"; "+recoveryCodeCookieName+"="+recoveryCookie)
		response, logOutput := captureAuditedRequest(t, app, request)
		defer func() { _ = response.Body.Close() }()

		if response.StatusCode != http.StatusOK {
			t.Fatalf("expected the inline register reveal to render, got %d", response.StatusCode)
		}
		body := mustReadBodyString(t, response.Body)
		revealedCode := recoveryCodeFromRevealPage(t, body)
		assertRecoveryCodeRevealAudited(t, logOutput, revealedCode, ownerID)
	})
}

// assertRecoveryCodeRevealAudited is the per-surface half of the recovery-code
// contract: the reveal is on the line, the code is not.
func assertRecoveryCodeRevealAudited(t *testing.T, logOutput string, revealedCode string, ownerID uint) {
	t.Helper()

	line := assertHealthEgressAudited(t, logOutput, "auth.recovery_code_reveal", "success", "recovery_code")
	if strings.TrimSpace(revealedCode) == "" {
		t.Fatal("expected a revealed code to compare the audit line against")
	}
	if strings.Contains(line, revealedCode) {
		t.Fatalf("the reveal audit line must record the fact, never the recovery code: %q", line)
	}
	if !strings.Contains(line, fmt.Sprintf("user_id=%q", strconv.FormatUint(uint64(ownerID), 10))) {
		t.Fatalf("expected the reveal audit line to name the owner it was shown to, got %q", line)
	}
}

// recoveryCodeFromRevealPage reads the code out of the rendered page through its
// stable hook, so the assertion above compares the audit line against the value
// that was actually displayed rather than against a re-typed constant.
func recoveryCodeFromRevealPage(t *testing.T, body string) string {
	t.Helper()

	node := htmlElementByAttr(mustParseHTMLDocument(t, body), "data-recovery-code-value", "")
	if node == nil {
		t.Fatal("expected the reveal surface to carry the recovery-code element")
	}
	code := strings.TrimSpace(htmlNodeText(node))
	if code == "" {
		t.Fatal("expected a non-empty recovery code on the reveal surface")
	}
	return code
}

// TestHealthAuditDomainsSeparateEgressFromMutation is the discriminator itself,
// and the reason egress did not simply join domain="health_data". The stream
// answers two different incident questions — "was tracked data changed or
// destroyed?" and "did tracked data leave?" — and each filter must return only
// its own class, or an operator asking about an erasure collects every routine
// CSV download instead. The shared `health_` prefix is what answers both at
// once, so it is pinned here too.
func TestHealthAuditDomainsSeparateEgressFromMutation(t *testing.T) {
	mutation := captureEmittedSecurityEvent(t, "/api/v1/users/current/data-wipe", func(handler *Handler, c fiber.Ctx) {
		handler.logMutationSuccess(c, healthMutationKind{action: "settings.clear_data", target: "account_data"})
	})
	egress := captureEmittedSecurityEvent(t, "/api/v1/exports/csv", func(handler *Handler, c fiber.Ctx) {
		handler.logEgressSuccess(c, exportCSVEgress)
	})

	mutationLine := securityEventLine(t, mutation, "settings.clear_data", "success")
	egressLine := securityEventLine(t, egress, dataExportAction, "success")

	if !strings.Contains(mutationLine, fmt.Sprintf("domain=%q", healthDataDomain)) {
		t.Fatalf("expected a mutation to stay in %q, got %q", healthDataDomain, mutationLine)
	}
	if strings.Contains(mutationLine, fmt.Sprintf("domain=%q", healthEgressDomain)) {
		t.Fatalf("a mutation must not answer an egress filter, got %q", mutationLine)
	}
	if !strings.Contains(egressLine, fmt.Sprintf("domain=%q", healthEgressDomain)) {
		t.Fatalf("expected an egress event in %q, got %q", healthEgressDomain, egressLine)
	}
	if strings.Contains(egressLine, fmt.Sprintf("domain=%q", healthDataDomain)) {
		t.Fatalf("an export must not answer the erasure filter, got %q", egressLine)
	}
	for _, line := range []string{mutationLine, egressLine} {
		if !strings.Contains(line, `domain="health_`) {
			t.Fatalf("expected both health domains to share the health_ prefix one clause can match, got %q", line)
		}
	}
}

// TestHeadExportCarriesTheHEADMethodOnTheAuditLine pins the audit half of the
// export HEAD twin: registerHEADTwins lets HEAD /api/v1/exports/summary run
// the same chain GET does, and emitSecurityEvent reads the field straight off
// c.Method() rather than assuming GET — an operator reading the egress stream
// must be able to tell a headers-only probe from a download that actually
// carried data out.
func TestHeadExportCarriesTheHEADMethodOnTheAuditLine(t *testing.T) {
	app, database := newOnboardingTestAppWithOptions(t, onboardingTestAppOptions{auditLogEnabled: true})
	user := createOnboardingTestUser(t, database, "export-head-audit@example.com", "StrongPass1", true)
	authCookie := loginAndExtractAuthCookie(t, app, user.Email, "StrongPass1")

	request := httptest.NewRequest(http.MethodHead, "/api/v1/exports/summary", nil)
	request.Header.Set("Cookie", authCookie)
	response, logOutput := captureAuditedRequest(t, app, request)
	defer func() { _ = response.Body.Close() }()
	if response.StatusCode != http.StatusOK {
		t.Fatalf("expected status 200 for a HEAD export summary, got %d", response.StatusCode)
	}

	assertHealthEgressAudited(t, logOutput, dataExportAction, "success", exportEgressTarget,
		securityEventField("export_format", "summary"), securityEventField("method", "HEAD"))
}
