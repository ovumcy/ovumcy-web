package api

import (
	"regexp"
	"strconv"
	"strings"

	"github.com/gofiber/fiber/v3"
)

// SafeRequestLogPath returns a privacy-safe request path for logs.
// It prefers the matched route template and falls back to a sanitized raw path.
func SafeRequestLogPath(c fiber.Ctx) string {
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
	case isOpaqueTokenDotICSSegment(segment):
		// The calendar-feed URL carries its bearer token as "<token>.ics" in a
		// single path segment. The matched-route template already logs this as
		// ":token.ics" (its segment starts with ':'), but this raw-path fallback
		// must mask it too: the trailing ".ics" contains a '.', which
		// isOpaqueRequestLogSegment rejects, so without this case the token VALUE
		// would leak verbatim on the (route-template-unavailable) fallback path.
		return ":token.ics"
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

// isOpaqueTokenDotICSSegment reports whether segment is an opaque bearer token
// immediately followed by a ".ics" suffix — the shape a calendar-feed URL takes
// in a single raw-path segment ("<token>.ics"). It strips the suffix and reuses
// the same opaque-token test applied to a bare token segment, so the redaction
// threshold is identical whether or not the ".ics" suffix is present.
func isOpaqueTokenDotICSSegment(segment string) bool {
	const suffix = ".ics"
	if !strings.HasSuffix(segment, suffix) {
		return false
	}
	return isOpaqueRequestLogSegment(strings.TrimSuffix(segment, suffix))
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

var (
	logEmailPattern = regexp.MustCompile(`[^\s@]+@[^\s@]+`)
	// logOpaqueTokenPattern masks long random-looking values: the 48-char
	// calendar-feed token and the 32-char base32 TOTP secret both clear the
	// 24-char floor. The floor exists to keep ordinary words, ids and route
	// components readable, which is also why it cannot be lowered to reach the
	// short secrets below — those get their own shape-specific rules instead.
	logOpaqueTokenPattern = regexp.MustCompile(`[A-Za-z0-9_-]{24,}`)
	// logRecoveryCodePattern masks a recovery code: the issued
	// OVUM-XXXX-XXXX-XXXX form (19 chars, below the opaque floor) plus the
	// separator-free and mixed-separator forms the input normalizer also
	// accepts. The "OVUM" literal is what keeps the rule targeted — a bare
	// 12-symbol alphanumeric body is shape-identical to an ordinary identifier,
	// so matching it without the prefix would redact routine diagnostic text.
	// The prefix is deliberately not anchored on a word boundary: matching a
	// code that is glued to a preceding word over-masks, which is the safe
	// direction, while an anchor would let that same input through verbatim.
	logRecoveryCodePattern = regexp.MustCompile(`(?i)OVUM-?[A-Za-z0-9]{4}-?[A-Za-z0-9]{4}-?[A-Za-z0-9]{4}`)
	// logSubmittedCodePattern masks a submitted one-time code: a standalone run
	// of exactly six digits, the length this app pins TOTP codes to (the
	// challenge field declares pattern="[0-9]{6}" and validation uses the RFC
	// 6238 six-digit default). Both the delimiters and the exact length are
	// load-bearing for diagnostic value: an HTTP status (3 digits), a port (up
	// to 5), a year or date component (up to 4), and any longer id, byte count
	// or epoch timestamp (7 or more) all stay readable.
	logSubmittedCodePattern = regexp.MustCompile(`\b[0-9]{6}\b`)
)

// SafeLogError renders a chain error for the request log with PII/secret-shaped
// substrings masked, mirroring SafeRequestLogPath for the ${request_path} tag.
// Handlers currently return only nil or generic *fiber.Error values (verified:
// no handler returns a raw fmt.Errorf carrying user input), so this is
// defense-in-depth: a future handler that does cannot leak an email, an opaque
// token, a recovery code or a submitted code into the always-on Fiber request
// log.
//
// Rule order is load-bearing. The two long-value rules run before the two
// short-value ones, because a short rule firing inside a long match would split
// that match into fragments which no longer meet the long rule's own length
// floor — and those fragments would then be written out verbatim. Masking the
// long shapes first makes them unconditional, and every placeholder written by
// an earlier rule is inert for the rules that follow.
func SafeLogError(err error) string {
	if err == nil {
		return ""
	}
	msg := strings.TrimSpace(err.Error())
	msg = logEmailPattern.ReplaceAllString(msg, ":email")
	msg = logOpaqueTokenPattern.ReplaceAllString(msg, ":token")
	msg = logRecoveryCodePattern.ReplaceAllString(msg, ":code")
	msg = logSubmittedCodePattern.ReplaceAllString(msg, ":code")
	return msg
}
