package api

import (
	"testing"

	"github.com/gofiber/fiber/v3"
)

func TestBuildLoginPageDataUsesFlashPriorityAndSetupFlag(t *testing.T) {
	t.Parallel()

	flash := FlashPayload{
		AuthError: "invalid credentials",
	}

	payload := evaluateAuthPageBuilder(t, func(c fiber.Ctx) error {
		return c.JSON(buildLoginPageData(map[string]string{}, flash, true, false, false, true))
	})

	if payload["ErrorKey"] != "auth.error.invalid_credentials" {
		t.Fatalf("expected flash error key, got %#v", payload["ErrorKey"])
	}
	if payload["Email"] != "" {
		t.Fatalf("expected no prefilled email (PII no longer round-trips), got %#v", payload["Email"])
	}
	if payload["IsFirstLaunch"] != true {
		t.Fatalf("expected IsFirstLaunch=true, got %#v", payload["IsFirstLaunch"])
	}
	if payload["RegistrationOpen"] != false {
		t.Fatalf("expected RegistrationOpen=false, got %#v", payload["RegistrationOpen"])
	}
	if payload["OIDCEnabled"] != false {
		t.Fatalf("expected OIDCEnabled=false, got %#v", payload["OIDCEnabled"])
	}
	if payload["LocalPublicAuthEnabled"] != true {
		t.Fatalf("expected LocalPublicAuthEnabled=true, got %#v", payload["LocalPublicAuthEnabled"])
	}
}

// Pinned on the real route, not on the builder: buildLoginPageData takes no
// fiber.Ctx, so nothing calling it directly can observe a query read added to
// ShowLoginPage. The claim is exactly the field and the error block — see
// assertAuthPageDidNotPrefillFromQuery for what this page does still carry.
func TestLoginPageDoesNotPrefillEmailOrErrorFromQuery(t *testing.T) {
	app, _ := newOnboardingTestApp(t)

	body := requestAuthPageWithHostileQuery(t, app, "/login")

	assertAuthPageDidNotPrefillFromQuery(t, body, "login-email")
}
