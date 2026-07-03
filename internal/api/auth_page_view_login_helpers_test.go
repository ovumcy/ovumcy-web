package api

import (
	"net/url"
	"testing"

	"github.com/gofiber/fiber/v3"
)

func TestBuildLoginPageDataUsesFlashPriorityAndSetupFlag(t *testing.T) {
	t.Parallel()

	query := url.Values{
		"error": {"weak password"},
		"email": {"query@example.com"},
	}
	flash := FlashPayload{
		AuthError: "invalid credentials",
	}

	payload := evaluateAuthPageBuilder(t, query, func(c fiber.Ctx) error {
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
