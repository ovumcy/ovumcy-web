package api

import (
	"testing"

	"github.com/gofiber/fiber/v3"
)

func TestBuildForgotPasswordPageDataUsesFlashEmailForRecoveryStep(t *testing.T) {
	t.Parallel()

	flash := FlashPayload{
		AuthError:   "invalid recovery code",
		ForgotEmail: " Owner@Example.com ",
	}

	payload := evaluateAuthPageBuilder(t, func(c fiber.Ctx) error {
		return c.JSON(buildForgotPasswordPageData(map[string]string{}, flash))
	})

	if payload["ErrorKey"] != "auth.error.invalid_recovery_code" {
		t.Fatalf("expected flash error key, got %#v", payload["ErrorKey"])
	}
	if payload["Email"] != "owner@example.com" {
		t.Fatalf("expected normalized flash email, got %#v", payload["Email"])
	}
	if payload["ShowRecoveryCodeStep"] != true {
		t.Fatalf("expected ShowRecoveryCodeStep=true, got %#v", payload["ShowRecoveryCodeStep"])
	}
}

func TestBuildForgotPasswordPageDataWithoutFlashHasNoEmail(t *testing.T) {
	t.Parallel()

	payload := evaluateAuthPageBuilder(t, func(c fiber.Ctx) error {
		return c.JSON(buildForgotPasswordPageData(map[string]string{}, FlashPayload{}))
	})

	if payload["Email"] != "" {
		t.Fatalf("expected empty forgot-password email without flash, got %#v", payload["Email"])
	}
	if payload["ShowRecoveryCodeStep"] != false {
		t.Fatalf("expected ShowRecoveryCodeStep=false, got %#v", payload["ShowRecoveryCodeStep"])
	}
}

// The query exclusion is pinned on the real route, not on the builder:
// buildForgotPasswordPageData takes no fiber.Ctx, so nothing calling it
// directly can observe a query read added to ShowForgotPasswordPage. The
// email-field anchor also proves the page stayed on its first step — a
// query-sourced email would have advanced it to the recovery-code step.
func TestForgotPasswordPageIgnoresAQueryOfferedPIIAndErrorState(t *testing.T) {
	app, _ := newOnboardingTestApp(t)

	body := requestAuthPageWithHostileQuery(t, app, "/forgot-password")

	assertAuthPageIgnoredTheQuery(t, body, "forgot-email")
}
