package api

import (
	"github.com/gofiber/fiber/v3"
)

// UpdateTimezone persists the authenticated owner's IANA timezone. It is the
// write path that the request-free reminder pass (issue #124) reads: a small
// client module POSTs the browser-detected zone here once per session when it
// differs from the value the server already holds.
//
// This is a dedicated state-mutating /api/v1 endpoint — chained behind
// handler.AuthRequired + handler.OwnerOnly and CSRF-protected by the global
// middleware (it is NOT in the CSRF exemption list). The IANA name is read from
// the request body and validated with the shared request-timezone validator
// (parseRequestTimezone), which rejects the "Local" token, injection, and
// non-IANA input, so an unsafe value can never be persisted. The write is
// scoped to the session user_id (user.ID from the sealed auth cookie), never an
// id from the request, and PersistTimezone skips the DB UPDATE when unchanged.
func (handler *Handler) UpdateTimezone(c fiber.Ctx) error {
	user, ok := currentUser(c)
	if !ok {
		return handler.respondMappedError(c, unauthorizedErrorSpec())
	}

	input, err := parseTimezoneSettingsInput(c)
	if err != nil {
		return handler.respondMappedError(c, settingsInvalidInputErrorSpec())
	}

	_, canonical, valid := parseRequestTimezone(input.Timezone)
	if !valid {
		return handler.respondMappedError(c, settingsInvalidInputErrorSpec())
	}

	changed, err := handler.settingsService.PersistTimezone(c.Context(), user.ID, user.Timezone, canonical)
	if err != nil {
		return handler.respondMappedError(c, settingsTimezoneUpdateErrorSpec())
	}
	if changed {
		user.Timezone = canonical
	}

	if isHTMX(c) {
		return c.SendStatus(fiber.StatusNoContent)
	}
	// Minimal JSON ack; the timezone name is never echoed in the response, a
	// redirect, or a URL.
	return c.JSON(fiber.Map{"ok": true, "changed": changed})
}

func parseTimezoneSettingsInput(c fiber.Ctx) (timezoneSettingsInput, error) {
	input := timezoneSettingsInput{}
	if hasJSONBody(c) {
		if err := c.Bind().Body(&input); err != nil {
			return timezoneSettingsInput{}, err
		}
		return input, nil
	}

	return timezoneSettingsInput{Timezone: c.FormValue("timezone")}, nil
}
