package api

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/gofiber/fiber/v3"
	"github.com/ovumcy/ovumcy-web/internal/services"
)

// TestLogEgressFailureSeparatesAFaultFromARefusal pins the outcome split the
// reveal surfaces depend on. Their failure arms are unreachable in a unit test —
// the claim errors only on a storage fault, which the authenticated user load
// reaches first — so the helper is exercised directly. Without the split a
// database outage reads as a burst of refused replays, and an operator watching
// outcome="failure" sees nothing.
func TestLogEgressFailureSeparatesAFaultFromARefusal(t *testing.T) {
	// Deliberately NOT parallel: captureAuditedRequest redirects the process-wide
	// logger, so a concurrent neighbour's security events would land in this
	// buffer and this one's in theirs. Every other file using that helper is
	// serial for the same reason.
	handler := &Handler{auditLogEnabled: true}
	kind := healthEgressKind{action: "settings.calendar_feed_reveal", target: "calendar_feed"}

	app := fiber.New()
	app.Get("/failure", func(c fiber.Ctx) error {
		handler.logEgressFailure(c, kind, "reveal_claim_failed")
		return c.SendStatus(fiber.StatusNoContent)
	})
	app.Get("/denied", func(c fiber.Ctx) error {
		handler.logEgressDenied(c, kind, "reveal_already_claimed")
		return c.SendStatus(fiber.StatusNoContent)
	})

	failure, failureLog := captureAuditedRequest(t, app, httptest.NewRequest(http.MethodGet, "/failure", nil))
	defer func() { _ = failure.Body.Close() }()
	line := assertHealthEgressAudited(t, failureLog, kind.action, "failure", kind.target)
	if !strings.Contains(line, `reason="reveal_claim_failed"`) {
		t.Fatalf("expected the fault to name its reason, got %q", line)
	}

	denied, deniedLog := captureAuditedRequest(t, app, httptest.NewRequest(http.MethodGet, "/denied", nil))
	defer func() { _ = denied.Body.Close() }()
	if strings.Contains(deniedLog, `outcome="failure"`) {
		t.Fatalf("a refusal must not be audited as a fault, got %q", deniedLog)
	}
	assertHealthEgressAudited(t, deniedLog, kind.action, "denied", kind.target)
}

// TestSessionWasRememberedFallsBackToNotRemembered covers the arm that decides
// what an unreadable answer costs. The remember-me choice is derived from the
// live token's own lifetime, so a request with no session on it — or a token
// minted without dates — has nothing to derive from. It must answer "not
// remembered": the default an unticked box gets, which costs a re-login at worst
// and can never extend a session past what its owner asked for.
func TestSessionWasRememberedFallsBackToNotRemembered(t *testing.T) {
	t.Parallel()

	for _, testCase := range []struct {
		name    string
		publish func(c fiber.Ctx)
	}{
		{name: "no session on the request", publish: func(fiber.Ctx) {}},
		{
			name: "token carrying no dates",
			publish: func(c fiber.Ctx) {
				c.Locals(contextAuthSessionKey, &services.AuthSessionClaims{UserID: 1, Role: "owner"})
			},
		},
	} {
		t.Run(testCase.name, func(t *testing.T) {
			app := fiber.New()
			app.Get("/probe", func(c fiber.Ctx) error {
				testCase.publish(c)
				if sessionWasRemembered(c) {
					return c.SendStatus(fiber.StatusOK)
				}
				return c.SendStatus(fiber.StatusNoContent)
			})

			response := mustAppResponse(t, app, httptest.NewRequest(http.MethodGet, "/probe", nil))
			assertStatusCode(t, response, http.StatusNoContent)
		})
	}
}
