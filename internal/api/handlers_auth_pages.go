package api

import (
	"time"

	"github.com/gofiber/fiber/v3"
	"github.com/ovumcy/ovumcy-web/internal/services"
)

// recoveryCodeRevealEgress tags the one-time reveal of an account's recovery
// code. The code is not health data, but it is a standing means of taking over
// the account and therefore everything in it, so the moment it reaches a person
// belongs in the same view as an export rather than in no view at all. It is
// declared once for BOTH surfaces that show a code — the dedicated page and the
// inline post-registration block — because the answer an operator needs
// ("was this account's recovery code displayed in that session?") is the same
// on either, and the sanitized path already distinguishes them. The event
// records the fact of the reveal; the code itself never enters the line.
var recoveryCodeRevealEgress = healthEgressKind{action: "auth.recovery_code_reveal", target: "recovery_code"}

// claimRecoveryCodeReveal consumes the account's one-time recovery-code reveal
// and reports whether this request may display the code. It is the gate BOTH
// reveal surfaces pass — the dedicated page and the inline post-registration
// block — because the answer is a property of the account, not of the surface.
//
// Retracting the sealed cookie is what the response can do about a browser; it
// is not a record, and a client that kept the sealed value can present it again
// on this same session. users.recovery_code_revealed_at is the record: every
// UPDATE that mints a recovery code NULLs it in the same statement, and this
// claim is a compare-and-set, so the second presentation of any sealed value —
// replay, reload, or a concurrent tab — loses the race.
//
// A refusal retracts the cookie in the same response, so the caller only has to
// take the code-less path it already takes when no cookie was presented. An
// errored claim refuses too: the mark is what makes the reveal single-use, and a
// reveal that cannot record itself is not single-use. That costs the display
// only — the session stands, and the code is regenerated from Settings.
func (handler *Handler) claimRecoveryCodeReveal(c fiber.Ctx, userID uint) bool {
	claimed, err := handler.authService.ClaimRecoveryCodeReveal(c.Context(), userID, time.Now())
	if err != nil || !claimed {
		// Both recovery surfaces claim through this helper, so auditing the
		// refusal here covers the dedicated page and the inline register block
		// together — the calendar-feed reveal is the third site of the same
		// class and audits its own refusal for the same reason.
		if err != nil {
			handler.logEgressFailure(c, recoveryCodeRevealEgress, "reveal_claim_failed") // codecov:ignore -- the claim errors only on a storage fault, which the authenticated user load reaches first
		} else {
			handler.logEgressDenied(c, recoveryCodeRevealEgress, "reveal_already_claimed")
		}
		handler.clearRecoveryCodePageCookie(c)
		return false
	}
	return true
}

func (handler *Handler) ShowLoginPage(c fiber.Ctx) error {
	redirected, err := handler.redirectAuthenticatedUserIfPresent(c)
	if err != nil {
		return err
	}
	if redirected {
		return nil
	}
	needsSetup, err := handler.setupService.RequiresInitialSetup(c.Context())
	if err != nil {
		return handler.respondMappedError(c, setupStateLoadErrorSpec())
	}

	flash := handler.popFlashCookie(c)
	data := buildLoginPageData(
		currentMessages(c),
		flash,
		needsSetup,
		handler.registrationService.RegistrationOpen(),
		handler.oidcEnabled(),
		handler.localPublicAuthEnabled(),
	)
	return handler.render(c, "login", data)
}

func (handler *Handler) ShowRegisterPage(c fiber.Ctx) error {
	if !handler.localPublicAuthEnabled() {
		return c.Redirect().Status(fiber.StatusSeeOther).To("/login")
	}
	if user := handler.optionalAuthenticatedUser(c); user != nil {
		recoveryState := handler.readRecoveryCodeDisplayState(c, user.ID, services.PostLoginRedirectPath(user))
		if recoveryState.RecoveryCode != "" && recoveryState.Surface == recoveryCodeSurfaceInlineRegister {
			// The claim runs only once a code would actually be displayed, so a
			// visit to /register that reveals nothing never spends the account's
			// one reveal. A refusal takes the same exit as an absent cookie.
			if !handler.claimRecoveryCodeReveal(c, user.ID) {
				return c.Redirect().Status(fiber.StatusSeeOther).To(services.PostLoginRedirectPath(user))
			}
			flash := handler.popFlashCookie(c)
			handler.clearRecoveryCodePageCookie(c)
			// The actor is already on the request context: the optional-auth lookup
			// above resolves the session through authenticateRequest, which publishes
			// it for the whole request.
			handler.logEgressSuccess(c, recoveryCodeRevealEgress)
			data := buildRegisterPageData(currentMessages(c), flash, false, handler.registrationService.RegistrationOpen())
			data["Title"] = localizedPageTitle(currentMessages(c), "meta.title.recovery_code", "Ovumcy | Recovery Code")
			data["CurrentUser"] = user
			data["HideNavigation"] = true
			data["RecoveryCode"] = recoveryState.RecoveryCode
			data["ContinuePath"] = recoveryState.ContinuePath
			data["ContinueTarget"] = recoveryState.ContinueTarget
			data["ShowInlineRecoveryCode"] = true
			return handler.render(c, "register", data)
		}
		if redirectErr := c.Redirect().Status(fiber.StatusSeeOther).To(services.PostLoginRedirectPath(user)); redirectErr != nil {
			return redirectErr
		}
		return nil
	}
	needsSetup, err := handler.setupService.RequiresInitialSetup(c.Context())
	if err != nil {
		return handler.respondMappedError(c, setupStateLoadErrorSpec())
	}

	flash := handler.popFlashCookie(c)
	data := buildRegisterPageData(currentMessages(c), flash, needsSetup, handler.registrationService.RegistrationOpen())
	return handler.render(c, "register", data)
}

func (handler *Handler) ShowRecoveryCodePage(c fiber.Ctx) error {
	user, err := handler.authenticateRequest(c)
	if err != nil {
		return c.Redirect().Status(fiber.StatusSeeOther).To("/login")
	}
	c.Locals(contextUserKey, user)

	fallbackContinuePath := services.PostLoginRedirectPath(user)
	recoveryState := handler.readRecoveryCodeDisplayState(c, user.ID, fallbackContinuePath)
	if recoveryState.RecoveryCode == "" {
		return c.Redirect().Status(fiber.StatusSeeOther).To(fallbackContinuePath)
	}
	if recoveryState.Surface == recoveryCodeSurfaceInlineRegister {
		return c.Redirect().Status(fiber.StatusSeeOther).To("/register")
	}
	if !handler.claimRecoveryCodeReveal(c, user.ID) {
		return c.Redirect().Status(fiber.StatusSeeOther).To(fallbackContinuePath)
	}
	handler.clearRecoveryCodePageCookie(c)
	handler.logEgressSuccess(c, recoveryCodeRevealEgress)

	return handler.render(c, "recovery_code", fiber.Map{
		"Title":          localizedPageTitle(currentMessages(c), "meta.title.recovery_code", "Ovumcy | Recovery Code"),
		"RecoveryCode":   recoveryState.RecoveryCode,
		"ContinuePath":   recoveryState.ContinuePath,
		"ContinueTarget": recoveryState.ContinueTarget,
		"HideNavigation": true,
	})
}

func (handler *Handler) ShowForgotPasswordPage(c fiber.Ctx) error {
	if !handler.localPublicAuthEnabled() {
		return c.Redirect().Status(fiber.StatusSeeOther).To("/login")
	}
	flash := handler.popFlashCookie(c)
	data := buildForgotPasswordPageData(currentMessages(c), flash)
	return handler.render(c, "forgot_password", data)
}

func (handler *Handler) ShowResetPasswordPage(c fiber.Ctx) error {
	// The N+1 site of the redeem gate in ResetPassword, and the sibling of
	// ShowForgotPasswordPage above: with local public auth off, the only reset
	// that still has a redeem is the forced one, so a recovery-minted form — and
	// the cookieless page that offers to start one — must not render either. The
	// cookie is read only when the gate can fire, so the ordinary configuration
	// pays nothing; an absent cookie reads as not forced and takes the same exit.
	if !handler.localPublicAuthEnabled() {
		if token, forced := handler.readResetPasswordCookie(c); !forced {
			// Only a cookie that was actually presented is retracted. An absent
			// one has nothing to retract, and a value that would not open was
			// already cleared by the reader — the same rule the TOTP readers
			// follow, so no reader here answers a bare visit with a Set-Cookie.
			if token != "" {
				handler.clearResetPasswordCookie(c)
			}
			return c.Redirect().Status(fiber.StatusSeeOther).To("/login")
		}
	}
	flash := handler.popFlashCookie(c)
	data := handler.buildResetPasswordPageData(c, currentMessages(c), flash)
	return handler.render(c, "reset_password", data)
}
