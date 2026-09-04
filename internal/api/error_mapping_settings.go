package api

import (
	"errors"

	"github.com/gofiber/fiber/v3"
	"github.com/ovumcy/ovumcy-web/internal/services"
)

func settingsValidationErrorSpec(key string) APIErrorSpec {
	return settingsFormErrorSpec(fiber.StatusBadRequest, APIErrorCategoryValidation, key)
}

func settingsInvalidInputErrorSpec() APIErrorSpec {
	return settingsValidationErrorSpec("invalid settings input")
}

func settingsMissingPasswordErrorSpec() APIErrorSpec {
	return settingsFormErrorSpec(fiber.StatusBadRequest, APIErrorCategoryValidation, "invalid password")
}

func settingsInvalidPasswordErrorSpec() APIErrorSpec {
	return settingsFormErrorSpec(fiber.StatusUnauthorized, APIErrorCategoryUnauthorized, "invalid password")
}

func settingsLocalPasswordRequiredErrorSpec() APIErrorSpec {
	return settingsFormErrorSpec(fiber.StatusForbidden, APIErrorCategoryForbidden, "local password required")
}

// settingsOIDCReauthRequiredErrorSpec is returned when an OIDC-only user
// attempts to enable a local password via the legacy ChangePassword endpoint
// instead of going through StartLocalPasswordSetupReauth. It signals the UI
// to redirect into the step-up flow.
func settingsOIDCReauthRequiredErrorSpec() APIErrorSpec {
	return settingsFormErrorSpec(fiber.StatusForbidden, APIErrorCategoryForbidden, "oidc reauth required")
}

func settingsOIDCReauthStaleErrorSpec() APIErrorSpec {
	return settingsFormErrorSpec(fiber.StatusUnauthorized, APIErrorCategoryUnauthorized, "oidc reauth stale")
}

func settingsOIDCReauthMismatchErrorSpec() APIErrorSpec {
	return settingsFormErrorSpec(fiber.StatusUnauthorized, APIErrorCategoryUnauthorized, "oidc reauth identity mismatch")
}

// settingsOIDCIdentityLinkClaimedErrorSpec is returned when the (issuer,
// subject) the step-up exchange resolved to is already linked to a DIFFERENT
// account — ConfirmAndLinkIdentity's cross-user-claim guard. Distinct from the
// reauth-mismatch spec above: that one means "not the same session that
// started the flow"; this one means the identity itself belongs elsewhere.
func settingsOIDCIdentityLinkClaimedErrorSpec() APIErrorSpec {
	return settingsFormErrorSpec(fiber.StatusConflict, APIErrorCategoryValidation, "oidc identity already linked")
}

// mapOIDCIdentityLinkReauthError maps failures of CompleteIdentityLinkReauth
// (the Settings step-up that links a NEW OIDC identity). Stale freshness and a
// cross-user claim keep their own specs so the owner learns what to do next;
// everything else collapses into the generic SSO failure so provider state
// never leaks through error granularity.
func mapOIDCIdentityLinkReauthError(err error) APIErrorSpec {
	switch {
	case errors.Is(err, services.ErrOIDCReauthStale):
		return settingsOIDCReauthStaleErrorSpec()
	case errors.Is(err, services.ErrOIDCLinkFailed):
		return settingsOIDCIdentityLinkClaimedErrorSpec()
	case errors.Is(err, services.ErrOIDCDisabled),
		errors.Is(err, services.ErrOIDCUnavailable),
		errors.Is(err, services.ErrOIDCIdentityResolveFailed):
		return authOIDCUnavailableErrorSpec()
	default:
		return authOIDCAuthenticationFailedErrorSpec()
	}
}

func settingsCycleUpdateErrorSpec() APIErrorSpec {
	return globalErrorSpec(fiber.StatusInternalServerError, APIErrorCategoryInternal, "failed to update cycle settings")
}

func settingsTrackingUpdateErrorSpec() APIErrorSpec {
	return globalErrorSpec(fiber.StatusInternalServerError, APIErrorCategoryInternal, "failed to update tracking settings")
}

// settingsWebhookInvalidURLErrorSpec is the form-level 400 for a webhook save
// whose URL is missing/unparseable/non-http(s) (services.ErrWebhookURLInvalid).
// The key never carries the offending URL, so the secret cannot leak into the
// response or a log line.
func settingsWebhookInvalidURLErrorSpec() APIErrorSpec {
	return settingsFormErrorSpec(fiber.StatusBadRequest, APIErrorCategoryValidation, "invalid webhook url")
}

// settingsWebhookUnreadableURLErrorSpec is the owner-fault spec for a save that
// would have had to invent an endpoint: the stored ciphertext will not open (a
// rotated SECRET_KEY) and the form left the field blank, which means "keep it".
// It is separate from the invalid-URL spec because the owner's next action is
// different -- enter a new endpoint, or withdraw the stored one -- and identical
// copy for two different remedies is how a surface stops being actionable.
func settingsWebhookUnreadableURLErrorSpec() APIErrorSpec {
	return settingsFormErrorSpec(fiber.StatusBadRequest, APIErrorCategoryValidation, "webhook url unreadable")
}

func settingsWebhookUpdateErrorSpec() APIErrorSpec {
	return globalErrorSpec(fiber.StatusInternalServerError, APIErrorCategoryInternal, "failed to update webhook settings")
}

// mapSettingsWebhookSaveError maps the webhook-settings save outcome to a spec by
// matching the service sentinel directly (per the api rule: errors.Is on service
// sentinels, no classifier indirection). ErrWebhookURLInvalid is the owner's
// fault (bad/empty/non-http(s) URL) → 400; anything else is an internal failure.
func mapSettingsWebhookSaveError(err error) APIErrorSpec {
	if errors.Is(err, services.ErrWebhookURLInvalid) {
		return settingsWebhookInvalidURLErrorSpec()
	}
	if errors.Is(err, services.ErrWebhookURLUnreadable) {
		return settingsWebhookUnreadableURLErrorSpec()
	}
	return settingsWebhookUpdateErrorSpec()
}

// settingsCalendarFeedUpdateErrorSpec is the internal-failure spec for the .ics
// feed lifecycle (generate/rotate/revoke). Token generation and persistence are
// server-side concerns — there is no owner-fault input on these endpoints, so
// every failure (ErrCalendarFeedTokenGenerate or ErrCalendarFeedTokenPersist)
// is a generic 500 with no owner-actionable distinction. The key never carries
// the token, so no secret can leak into the response or a log line.
func settingsCalendarFeedUpdateErrorSpec() APIErrorSpec {
	return globalErrorSpec(fiber.StatusInternalServerError, APIErrorCategoryInternal, "failed to update calendar feed")
}

// settingsInterfaceUpdateErrorSpec is the internal-failure spec for the
// interface save's account-side half (users.interface_language). The save is
// reported as failed rather than silently degraded to a cookie-only change:
// an owner who picked a language in Settings and saw a success flash would
// otherwise find the choice gone on the next device.
func settingsInterfaceUpdateErrorSpec() APIErrorSpec {
	return globalErrorSpec(fiber.StatusInternalServerError, APIErrorCategoryInternal, "failed to update interface settings")
}

func settingsTimezoneUpdateErrorSpec() APIErrorSpec {
	return globalErrorSpec(fiber.StatusInternalServerError, APIErrorCategoryInternal, "failed to update timezone")
}

func settingsRemindersUpdateErrorSpec() APIErrorSpec {
	return globalErrorSpec(fiber.StatusInternalServerError, APIErrorCategoryInternal, "failed to update reminder settings")
}

func settingsClearDataErrorSpec() APIErrorSpec {
	return globalErrorSpec(fiber.StatusInternalServerError, APIErrorCategoryInternal, "failed to clear data")
}

func settingsValidatePasswordErrorSpec() APIErrorSpec {
	return globalErrorSpec(fiber.StatusInternalServerError, APIErrorCategoryInternal, "failed to validate password")
}

func settingsDeleteAccountErrorSpec() APIErrorSpec {
	return globalErrorSpec(fiber.StatusInternalServerError, APIErrorCategoryInternal, "failed to delete account")
}

func settingsProfileUpdateErrorSpec() APIErrorSpec {
	return globalErrorSpec(fiber.StatusInternalServerError, APIErrorCategoryInternal, "failed to update profile")
}

func mapSettingsProfileNormalizeError(err error) APIErrorSpec {
	switch {
	case errors.Is(err, services.ErrSettingsDisplayNameTooLong):
		return settingsValidationErrorSpec("display name too long")
	case errors.Is(err, services.ErrSettingsDisplayNameInvalidCharacters):
		return settingsValidationErrorSpec("display name contains invalid characters")
	default:
		return settingsValidationErrorSpec("invalid profile input")
	}
}

func mapSettingsDeleteAccountPasswordError(err error) APIErrorSpec {
	switch {
	// Checked first: an exhausted re-auth budget refuses the request before the
	// password is compared, so it must not be reported as an invalid password.
	case errors.Is(err, services.ErrSettingsReauthRateLimited):
		return settingsRateLimitErrorSpec()
	case errors.Is(err, services.ErrSettingsPasswordMissing):
		return settingsMissingPasswordErrorSpec()
	case errors.Is(err, services.ErrSettingsPasswordInvalid):
		return settingsInvalidPasswordErrorSpec()
	case errors.Is(err, services.ErrSettingsLocalPasswordNotSet):
		return settingsLocalPasswordRequiredErrorSpec()
	default:
		return settingsValidatePasswordErrorSpec()
	}
}
