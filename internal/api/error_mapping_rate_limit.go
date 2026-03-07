package api

import (
	"strconv"
	"strings"

	"github.com/gofiber/fiber/v2"
	"github.com/terraincognita07/ovumcy/internal/httpx"
)

func authRateLimitErrorSpec(key string) APIErrorSpec {
	normalized := strings.TrimSpace(key)
	if normalized == "" {
		normalized = "too many requests"
	}
	return authFormErrorSpec(fiber.StatusTooManyRequests, APIErrorCategoryRateLimited, normalized)
}

func settingsRateLimitErrorSpec() APIErrorSpec {
	return settingsFormErrorSpec(fiber.StatusTooManyRequests, APIErrorCategoryRateLimited, "too many requests")
}

func globalRateLimitErrorSpec() APIErrorSpec {
	return globalErrorSpec(fiber.StatusTooManyRequests, APIErrorCategoryRateLimited, "too many requests")
}

func (handler *Handler) RespondAuthRateLimited(c *fiber.Ctx, errorKey string) error {
	return handler.respondRateLimitedMappedError(c, authRateLimitErrorSpec(errorKey))
}

func (handler *Handler) RespondAPIRateLimited(c *fiber.Ctx) error {
	switch {
	case strings.HasPrefix(c.Path(), "/api/auth/"):
		return handler.respondRateLimitedMappedError(c, authRateLimitErrorSpec("too many requests"))
	case strings.HasPrefix(c.Path(), "/api/settings/"), strings.HasPrefix(c.Path(), "/settings/"):
		return handler.respondRateLimitedMappedError(c, settingsRateLimitErrorSpec())
	default:
		return handler.respondRateLimitedMappedError(c, globalRateLimitErrorSpec())
	}
}

func (handler *Handler) respondRateLimitedMappedError(c *fiber.Ctx, spec APIErrorSpec) error {
	if httpx.AcceptsJSON(c, httpx.JSONModeAcceptOrContentType) {
		payload := fiber.Map{"error": spec.Key}
		if retryAfter := retryAfterSeconds(c); retryAfter > 0 {
			payload["retry_after_seconds"] = retryAfter
		}
		return c.Status(spec.Status).JSON(payload)
	}
	return handler.respondMappedError(c, spec)
}

func retryAfterSeconds(c *fiber.Ctx) int {
	value := strings.TrimSpace(string(c.Response().Header.Peek(fiber.HeaderRetryAfter)))
	if value == "" {
		return 0
	}
	seconds, err := strconv.Atoi(value)
	if err != nil || seconds < 1 {
		return 0
	}
	return seconds
}
