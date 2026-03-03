package api

import (
	"net/url"
	"testing"

	"github.com/gofiber/fiber/v2"
)

func TestBuildRegisterPageDataUsesOnlyFlashSources(t *testing.T) {
	t.Parallel()

	query := url.Values{
		"error": {"weak password"},
		"email": {"query@example.com"},
	}
	flash := FlashPayload{
		AuthError:     "email already exists",
		RegisterEmail: " Flash@Example.com ",
	}

	payload := evaluateAuthPageBuilder(t, query, func(c *fiber.Ctx) error {
		return c.JSON(buildRegisterPageData(c, map[string]string{}, flash, true))
	})

	if payload["ErrorKey"] != "auth.error.email_exists" {
		t.Fatalf("expected flash-based register error key, got %#v", payload["ErrorKey"])
	}
	if payload["Email"] != "flash@example.com" {
		t.Fatalf("expected normalized flash register email, got %#v", payload["Email"])
	}
	if payload["IsFirstLaunch"] != true {
		t.Fatalf("expected IsFirstLaunch=true, got %#v", payload["IsFirstLaunch"])
	}
}

func TestBuildRegisterPageDataIgnoresRegisterQueryFallback(t *testing.T) {
	t.Parallel()

	query := url.Values{
		"error": {"weak password"},
		"email": {"query@example.com"},
	}

	payload := evaluateAuthPageBuilder(t, query, func(c *fiber.Ctx) error {
		return c.JSON(buildRegisterPageData(c, map[string]string{}, FlashPayload{}, false))
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
