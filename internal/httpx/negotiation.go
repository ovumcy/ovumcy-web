package httpx

import (
	"strings"

	"github.com/gofiber/fiber/v3"
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

// AcceptsJSON reports whether the caller asked for JSON, either through the
// Accept header or by sending a JSON body.
func AcceptsJSON(c fiber.Ctx) bool {
	accept := strings.ToLower(c.Get("Accept"))
	if strings.Contains(accept, fiber.MIMEApplicationJSON) {
		return true
	}

	return HasJSONContentType(c)
}

func NegotiateResponseFormat(c fiber.Ctx) ResponseFormat {
	switch {
	case IsHTMX(c):
		return ResponseFormatHTMX
	case AcceptsJSON(c):
		return ResponseFormatJSON
	default:
		return ResponseFormatHTML
	}
}
