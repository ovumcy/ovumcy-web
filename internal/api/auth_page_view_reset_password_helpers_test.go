package api

import (
	"encoding/json"
	"testing"
	"time"

	"github.com/gofiber/fiber/v2"
	"github.com/terraincognita07/ovumcy/internal/services"
)

func TestBuildResetPasswordPageDataValidTokenAndForcedFlag(t *testing.T) {
	t.Parallel()

	handler := &Handler{secretKey: []byte("test-reset-secret")}
	token, err := services.BuildPasswordResetToken(handler.secretKey, 42, "$2a$10$testhashvaluefortokenclaims", 30*time.Minute, time.Now())
	if err != nil {
		t.Fatalf("buildPasswordResetToken returned error: %v", err)
	}
	cookieHeader := mustBuildResetCookieHeader(t, handler.secretKey, resetPasswordCookiePayload{
		Token:  token,
		Forced: true,
	})
	flash := FlashPayload{AuthError: "invalid credentials"}

	payload := evaluateAuthPageBuilderWithCookie(t, nil, cookieHeader, func(c *fiber.Ctx) error {
		return c.JSON(handler.buildResetPasswordPageData(c, map[string]string{}, flash))
	})

	if payload["InvalidToken"] != false {
		t.Fatalf("expected InvalidToken=false, got %#v", payload["InvalidToken"])
	}
	if payload["ForcedReset"] != true {
		t.Fatalf("expected ForcedReset=true, got %#v", payload["ForcedReset"])
	}
	if payload["ErrorKey"] != "auth.error.invalid_credentials" {
		t.Fatalf("expected flash error key, got %#v", payload["ErrorKey"])
	}
}

func TestBuildResetPasswordPageDataMarksInvalidToken(t *testing.T) {
	t.Parallel()

	handler := &Handler{secretKey: []byte("test-reset-secret")}
	cookieHeader := mustBuildResetCookieHeader(t, handler.secretKey, resetPasswordCookiePayload{
		Token: "invalid-token",
	})

	payload := evaluateAuthPageBuilderWithCookie(t, nil, cookieHeader, func(c *fiber.Ctx) error {
		return c.JSON(handler.buildResetPasswordPageData(c, map[string]string{}, FlashPayload{}))
	})

	if payload["InvalidToken"] != true {
		t.Fatalf("expected InvalidToken=true, got %#v", payload["InvalidToken"])
	}
}

func mustBuildResetCookieHeader(t *testing.T, secret []byte, payload resetPasswordCookiePayload) string {
	t.Helper()

	serialized, err := json.Marshal(payload)
	if err != nil {
		t.Fatalf("marshal reset cookie payload: %v", err)
	}
	codec, err := newSecureCookieCodec(secret)
	if err != nil {
		t.Fatalf("new secure cookie codec: %v", err)
	}
	encoded, err := codec.seal(resetPasswordCookieName, serialized)
	if err != nil {
		t.Fatalf("seal reset cookie payload: %v", err)
	}
	return resetPasswordCookieName + "=" + encoded
}
