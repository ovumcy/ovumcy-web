package api

import (
	"net/url"
	"testing"

	"github.com/gofiber/fiber/v3"
)

func TestBuildForgotPasswordPageDataUsesFlashEmailForRecoveryStep(t *testing.T) {
	t.Parallel()

	query := url.Values{
		"error": {"invalid input"},
		"email": {"query@example.com"},
	}
	flash := FlashPayload{
		AuthError:   "invalid recovery code",
		ForgotEmail: " Owner@Example.com ",
	}

	payload := evaluateAuthPageBuilder(t, query, func(c fiber.Ctx) error {
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

func TestBuildForgotPasswordPageDataDoesNotUseQueryEmail(t *testing.T) {
	t.Parallel()

	query := url.Values{
		"email": {"query@example.com"},
	}

	payload := evaluateAuthPageBuilder(t, query, func(c fiber.Ctx) error {
		return c.JSON(buildForgotPasswordPageData(map[string]string{}, FlashPayload{}))
	})

	if payload["Email"] != "" {
		t.Fatalf("expected empty forgot-password email without flash, got %#v", payload["Email"])
	}
	if payload["ShowRecoveryCodeStep"] != false {
		t.Fatalf("expected ShowRecoveryCodeStep=false, got %#v", payload["ShowRecoveryCodeStep"])
	}
}
