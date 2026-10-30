package api

import (
	"bytes"
	"fmt"
	"io"
	"log"
	"net/http"
	"net/http/httptest"
	"net/url"
	"sort"
	"strings"
	"testing"

	"github.com/ovumcy/ovumcy-web/internal/security"
	"github.com/ovumcy/ovumcy-web/internal/services"
)

type oidcUnlinkedCallbackReply struct {
	status     int
	location   string
	setCookies string
	body       string
	authError  string
}

func runOIDCCallbackWithAuthError(t *testing.T, authErr error, auditLogEnabled bool) oidcUnlinkedCallbackReply {
	t.Helper()

	stub := newStubOIDCWorkflowService(true)
	stub.authURL = "https://id.example.com/authorize"
	stub.authErr = authErr
	app, _ := newOnboardingTestAppWithOptions(t, onboardingTestAppOptions{
		cookieSecure:    true,
		oidcService:     stub,
		auditLogEnabled: auditLogEnabled,
	})

	startResponse := mustAppResponse(t, app, httptest.NewRequest(http.MethodGet, "/auth/oidc/start", nil))
	stateCookie := responseCookie(startResponse.Cookies(), oidcStateCookieName)
	if stateCookie == nil {
		t.Fatal("expected OIDC state cookie from start flow")
	}

	callbackRequest := httptest.NewRequest(http.MethodPost, security.OIDCCallbackPath, strings.NewReader(url.Values{
		"state": {stub.lastStartState},
		"code":  {"provider-code"},
	}.Encode()))
	callbackRequest.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	callbackRequest.Header.Set("Cookie", stateCookie.String())

	response := mustAppResponse(t, app, callbackRequest)
	setCookies := make([]string, 0, len(response.Cookies()))
	for _, cookie := range response.Cookies() {
		setCookies = append(setCookies, fmt.Sprintf("%s|maxage=%d|empty=%t", cookie.Name, cookie.MaxAge, cookie.Value == ""))
	}
	sort.Strings(setCookies)
	body, err := io.ReadAll(response.Body)
	if err != nil {
		t.Fatalf("read callback body: %v", err)
	}
	reply := oidcUnlinkedCallbackReply{
		status:     response.StatusCode,
		location:   response.Header.Get("Location"),
		setCookies: strings.Join(setCookies, ","),
		body:       string(body),
	}
	if flashCookie := responseCookie(response.Cookies(), exemptFlashCookieName); flashCookie != nil && strings.TrimSpace(flashCookie.Value) != "" {
		reply.authError = decodeExemptFlashCookieForTest(t, flashCookie.Value).AuthError
	}
	return reply
}

// An unlinked IdP identity asserting a verified email must get the same reply
// whether or not a local account holds that email: a split here is an
// account-existence oracle for anyone with an account on the configured IdP
// (WEB-360).
func TestOIDCCallbackUnlinkedReplyIsIdenticalWhetherOrNotAnAccountHoldsTheEmail(t *testing.T) {
	t.Parallel()

	accountHoldsEmail := runOIDCCallbackWithAuthError(t, services.ErrOIDCLinkRequiresConfirmation, false)
	noAccountHoldsEmail := runOIDCCallbackWithAuthError(t, services.ErrOIDCAccountUnavailable, false)

	if accountHoldsEmail.authError == "" {
		t.Fatal("expected a flash auth_error explaining the refusal")
	}
	if accountHoldsEmail != noAccountHoldsEmail {
		t.Fatalf("expected identical replies, got link-required %+v vs account-unavailable %+v", accountHoldsEmail, noAccountHoldsEmail)
	}
	if accountHoldsEmail.status != http.StatusSeeOther || accountHoldsEmail.location != "/login" {
		t.Fatalf("expected 303 to /login, got %+v", accountHoldsEmail)
	}
}

// The operator's audit stream is not user-facing, so it keeps the two causes
// apart even though the reply does not.
func TestOIDCCallbackUnlinkedAuditLinesKeepTheCausesDistinct(t *testing.T) {
	originalWriter := log.Writer()
	defer log.SetOutput(originalWriter)

	var linkRequired, accountUnavailable bytes.Buffer
	log.SetOutput(&linkRequired)
	runOIDCCallbackWithAuthError(t, services.ErrOIDCLinkRequiresConfirmation, true)
	log.SetOutput(&accountUnavailable)
	runOIDCCallbackWithAuthError(t, services.ErrOIDCAccountUnavailable, true)

	if !strings.Contains(linkRequired.String(), "link_required") {
		t.Fatalf("expected link_required cause in audit line, got %q", linkRequired.String())
	}
	if !strings.Contains(accountUnavailable.String(), "account_unavailable") {
		t.Fatalf("expected account_unavailable cause in audit line, got %q", accountUnavailable.String())
	}
	if strings.Contains(linkRequired.String(), "account_unavailable") {
		t.Fatalf("link-required audit line must not carry account_unavailable: %q", linkRequired.String())
	}
	if strings.Contains(accountUnavailable.String(), "link_required") {
		t.Fatalf("account-unavailable audit line must not carry link_required: %q", accountUnavailable.String())
	}
}
