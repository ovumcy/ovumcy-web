package api

import (
	"encoding/json"
	"errors"
	"strings"
	"time"

	"github.com/gofiber/fiber/v3"
)

const resetPasswordCookieTTL = 30 * time.Minute

// resetPasswordCookiePayload carries only the reset token itself. It used to
// also carry a "Forced" bool the mint call site chose, which every gate that
// mattered (redeem, page, display copy) then trusted — but that bool was
// never part of the signed token, so it could not be checked against the
// SIGNED purpose the token actually carries and a caller minting the wrong
// kind of "forced" silently bypassed the instance-wide local-sign-in gate
// (PRIV-4). Every decision that used to read it now parses the token itself
// and reads services.PasswordResetClaims.Purpose instead — see
// services.PasswordResetTokenBypassesLocalAuthGate and
// services.ParsePasswordResetToken.
type resetPasswordCookiePayload struct {
	Token string `json:"token"`
}

var resetPasswordCookieSpec = sealedCookieSpec{name: resetPasswordCookieName, path: "/"}

func (handler *Handler) setResetPasswordCookie(c fiber.Ctx, token string) error {
	token = strings.TrimSpace(token)
	if token == "" {
		handler.clearResetPasswordCookie(c)
		return errors.New("reset token is required")
	}

	payload := resetPasswordCookiePayload{
		Token: token,
	}
	serialized, err := json.Marshal(payload)
	if err != nil {
		return err
	}

	return handler.writeSealedCookie(c, resetPasswordCookieSpec, serialized, time.Now().Add(resetPasswordCookieTTL))
}

func (handler *Handler) readResetPasswordCookie(c fiber.Ctx) string {
	raw := strings.TrimSpace(c.Cookies(resetPasswordCookieName))
	if raw == "" {
		return ""
	}

	codec, err := handler.cookieCodec()
	if err != nil {
		handler.clearResetPasswordCookie(c)
		return ""
	}
	decoded, err := codec.open(resetPasswordCookieName, raw)
	if err != nil {
		handler.clearResetPasswordCookie(c)
		return ""
	}

	payload := resetPasswordCookiePayload{}
	if err := json.Unmarshal(decoded, &payload); err != nil {
		handler.clearResetPasswordCookie(c)
		return ""
	}

	token := strings.TrimSpace(payload.Token)
	if token == "" {
		handler.clearResetPasswordCookie(c)
		return ""
	}
	return token
}

func (handler *Handler) clearResetPasswordCookie(c fiber.Ctx) {
	handler.clearSealedCookie(c, resetPasswordCookieSpec)
}
