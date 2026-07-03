package httpx

import (
	"strings"

	"github.com/gofiber/fiber/v3"
)

type JSONMode uint8

const (
	JSONModeAcceptOnly JSONMode = iota
	JSONModeAcceptOrContentType
)

type ResponseFormat uint8

const (
	ResponseFormatHTML ResponseFormat = iota
	ResponseFormatJSON
	ResponseFormatHTMX
)

func IsHTMX(c fiber.Ctx) bool {
	return strings.EqualFold(c.Get("HX-Request"), "true")
}

func HasJSONContentType(c fiber.Ctx) bool {
	contentType := strings.ToLower(strings.TrimSpace(c.Get(fiber.HeaderContentType)))
	return strings.Contains(contentType, fiber.MIMEApplicationJSON)
}

func AcceptsJSON(c fiber.Ctx, mode JSONMode) bool {
	accept := strings.ToLower(c.Get("Accept"))
	if strings.Contains(accept, fiber.MIMEApplicationJSON) {
		return true
	}

	if mode == JSONModeAcceptOrContentType && HasJSONContentType(c) {
		return true
	}

	return false
}

func NegotiateResponseFormat(c fiber.Ctx, mode JSONMode) ResponseFormat {
	switch {
	case IsHTMX(c):
		return ResponseFormatHTMX
	case AcceptsJSON(c, mode):
		return ResponseFormatJSON
	default:
		return ResponseFormatHTML
	}
}
