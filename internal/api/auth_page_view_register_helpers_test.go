package api

import (
	"testing"

	"github.com/gofiber/fiber/v3"
)

func TestBuildRegisterPageDataUsesOnlyFlashSources(t *testing.T) {
	t.Parallel()

	flash := FlashPayload{
		AuthError: "email already exists",
	}

	payload := evaluateAuthPageBuilder(t, func(c fiber.Ctx) error {
		return c.JSON(buildRegisterPageData(map[string]string{}, flash, true, true))
	})

	if payload["ErrorKey"] != "auth.error.email_exists" {
		t.Fatalf("expected flash-based register error key, got %#v", payload["ErrorKey"])
	}
	if payload["Email"] != "" {
		t.Fatalf("expected no prefilled email (PII no longer round-trips), got %#v", payload["Email"])
	}
	if payload["IsFirstLaunch"] != true {
		t.Fatalf("expected IsFirstLaunch=true, got %#v", payload["IsFirstLaunch"])
	}
	if payload["RegistrationOpen"] != true {
		t.Fatalf("expected RegistrationOpen=true, got %#v", payload["RegistrationOpen"])
	}
}

func TestBuildRegisterPageDataWithoutFlashHasNoErrorOrEmail(t *testing.T) {
	t.Parallel()

	payload := evaluateAuthPageBuilder(t, func(c fiber.Ctx) error {
		return c.JSON(buildRegisterPageData(map[string]string{}, FlashPayload{}, false, true))
	})

	if payload["ErrorKey"] != "" {
		t.Fatalf("expected empty register error without flash, got %#v", payload["ErrorKey"])
	}
	if payload["Email"] != "" {
		t.Fatalf("expected empty register email without flash, got %#v", payload["Email"])
	}
	if payload["IsFirstLaunch"] != false {
		t.Fatalf("expected IsFirstLaunch=false, got %#v", payload["IsFirstLaunch"])
	}
}

func TestBuildRegisterPageDataDefaultsToRegistrationDisabledWhenClosed(t *testing.T) {
	t.Parallel()

	payload := evaluateAuthPageBuilder(t, func(c fiber.Ctx) error {
		return c.JSON(buildRegisterPageData(map[string]string{}, FlashPayload{}, false, false))
	})

	if payload["ErrorKey"] != "auth.error.registration_disabled" {
		t.Fatalf("expected registration disabled error key, got %#v", payload["ErrorKey"])
	}
	if payload["RegistrationOpen"] != false {
		t.Fatalf("expected RegistrationOpen=false, got %#v", payload["RegistrationOpen"])
	}
}

// The query exclusion is pinned on the real route, not on the builder:
// buildRegisterPageData takes no fiber.Ctx, so nothing calling it directly can
// observe a query read added to ShowRegisterPage.
func TestRegisterPageIgnoresAQueryOfferedPIIAndErrorState(t *testing.T) {
	app, _ := newOnboardingTestApp(t)

	body := requestAuthPageWithHostileQuery(t, app, "/register")

	assertAuthPageIgnoredTheQuery(t, body, "register-email")
}
