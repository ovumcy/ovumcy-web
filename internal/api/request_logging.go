package api

import (
	"strconv"
	"strings"

	"github.com/gofiber/fiber/v2"
)

// SafeRequestLogPath returns a privacy-safe request path for logs.
// It prefers the matched route template and falls back to a sanitized raw path.
func SafeRequestLogPath(c *fiber.Ctx) string {
	if c == nil {
		return "/"
	}

	if route := c.Route(); route != nil {
		routePath := strings.TrimSpace(route.Path)
		if routePath == "/*" || routePath == "*" {
			return routePath
		}
		if routePath != "" && !strings.EqualFold(strings.TrimSpace(route.Method), "USE") {
			return sanitizeRequestLogPath(routePath)
		}
	}

	return sanitizeRequestLogPath(strings.TrimSpace(c.Path()))
}

func sanitizeRequestLogPath(path string) string {
	path = strings.TrimSpace(path)
	if path == "" {
		return "/"
	}

	segments := strings.Split(path, "/")
	for index, segment := range segments {
		if index == 0 {
			continue
		}
		segments[index] = sanitizeRequestLogSegment(segment)
	}

	sanitized := strings.Join(segments, "/")
	if sanitized == "" {
		return "/"
	}
	if !strings.HasPrefix(sanitized, "/") {
		return "/" + sanitized
	}
	return sanitized
}

func sanitizeRequestLogSegment(segment string) string {
	segment = strings.TrimSpace(segment)
	switch {
	case segment == "":
		return ""
	case strings.HasPrefix(segment, ":"):
		return segment
	case segment == "*":
		return segment
	case isDateRequestLogSegment(segment):
		return ":date"
	case isNumericRequestLogSegment(segment), isUUIDRequestLogSegment(segment):
		return ":id"
	case strings.Contains(segment, "@"):
		return ":email"
	case isOpaqueRequestLogSegment(segment):
		return ":token"
	default:
		return segment
	}
}

func isDateRequestLogSegment(segment string) bool {
	if len(segment) != len("2006-01-02") {
		return false
	}
	for index, char := range segment {
		switch index {
		case 4, 7:
			if char != '-' {
				return false
			}
		default:
			if char < '0' || char > '9' {
				return false
			}
		}
	}
	return true
}

func isNumericRequestLogSegment(segment string) bool {
	if segment == "" {
		return false
	}
	_, err := strconv.ParseUint(segment, 10, 64)
	return err == nil
}

func isUUIDRequestLogSegment(segment string) bool {
	if len(segment) != 36 {
		return false
	}
	for index, char := range segment {
		switch index {
		case 8, 13, 18, 23:
			if char != '-' {
				return false
			}
		default:
			switch {
			case char >= '0' && char <= '9':
			case char >= 'a' && char <= 'f':
			case char >= 'A' && char <= 'F':
			default:
				return false
			}
		}
	}
	return true
}

func isOpaqueRequestLogSegment(segment string) bool {
	if len(segment) < 24 {
		return false
	}
	for _, char := range segment {
		switch {
		case char >= '0' && char <= '9':
		case char >= 'a' && char <= 'z':
		case char >= 'A' && char <= 'Z':
		case char == '-', char == '_':
		default:
			return false
		}
	}
	return true
}
