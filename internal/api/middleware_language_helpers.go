package api

import (
	"strings"
	"time"

	"github.com/gofiber/fiber/v3"
)

func (handler *Handler) LanguageMiddleware(c fiber.Ctx) error {
	requestLocation, timezoneCookieValue := resolveRequestLocation(
		c.Get(timezoneHeaderName),
		c.Cookies(timezoneCookieName),
		handler.location,
	)
	if timezoneCookieValue != "" && strings.TrimSpace(c.Cookies(timezoneCookieName)) != timezoneCookieValue {
		handler.setTimezoneCookie(c, timezoneCookieValue)
	}

	cookieLanguage := c.Cookies(languageCookieName)
	language := handler.i18n.DetectFromAcceptLanguage(c.Get("Accept-Language"))
	if cookieLanguage != "" {
		language = handler.i18n.NormalizeLanguage(cookieLanguage)
	}

	c.Locals(contextLanguageKey, language)
	c.Locals(contextMessagesKey, handler.i18n.Messages(language))
	c.Locals(contextLocationKey, requestLocation)
	return c.Next()
}

func (handler *Handler) setLanguageCookie(c fiber.Ctx, language string) {
	c.Cookie(&fiber.Cookie{
		Name:     languageCookieName,
		Value:    handler.i18n.NormalizeLanguage(language),
		Path:     "/",
		HTTPOnly: false,
		Secure:   handler.cookieSecure,
		SameSite: "Lax",
		Expires:  time.Now().AddDate(1, 0, 0),
	})
}

func (handler *Handler) setTimezoneCookie(c fiber.Ctx, timezone string) {
	c.Cookie(&fiber.Cookie{
		Name:     timezoneCookieName,
		Value:    timezone,
		Path:     "/",
		HTTPOnly: false,
		Secure:   handler.cookieSecure,
		SameSite: "Lax",
		Expires:  time.Now().AddDate(1, 0, 0),
	})
}
