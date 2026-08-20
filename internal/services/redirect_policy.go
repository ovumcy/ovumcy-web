package services

import (
	"net/url"
	"strings"
	"time"
)

func SanitizeRedirectPath(raw string, fallback string) string {
	candidate := strings.TrimSpace(raw)
	if candidate == "" {
		return fallback
	}
	// Reject CR/LF so a crafted next-path can never split a Location or
	// HX-Redirect header (defence-in-depth; the HTTP layer also strips these).
	if strings.ContainsAny(candidate, "\r\n") {
		return fallback
	}
	if !strings.HasPrefix(candidate, "/") {
		return fallback
	}
	if len(candidate) > 1 && (candidate[1] == '/' || candidate[1] == '\\') {
		return fallback
	}
	parsed, err := url.Parse(candidate)
	if err != nil || parsed.IsAbs() {
		return fallback
	}
	return candidate
}

// currentPathQueryAllowlist is the closed set of query parameters a page may
// echo back into its own rendered address, each paired with the shape its own
// consumer accepts. It holds exactly the parameters the page handlers read: the
// calendar anchors, the onboarding step and the privacy back-link. Anything
// else is dropped whatever its value, because the rendered address reaches the
// markup of every page and, through the footer privacy link, a new outgoing URL
// — so an attacker-supplied `email` or `error` in a crafted link would land in
// browser history, the Referer header and the server access log.
//
// The names are only half the filter. An allowlist over names alone lets any
// value through under an allowed key, which is the very same leak wearing a
// different key: `?step=victim@example.com` renders exactly like `?email=`
// would. So a retained value must also match what its consumer accepts, and a
// parameter whose value does not match is dropped rather than trimmed — the
// same fail-closed direction the rest of this function takes.
// `back` is deliberately NOT a member: its policy is not a predicate over a
// value but a rewrite of it (fragment stripped, own query filtered, path
// re-checked), so it is allowlisted by its own branch in
// sanitizeCurrentPathQuery and decided by SanitizeBackNavigationValue. Holding
// a nil entry for it here instead made this map claim to answer a question it
// cannot, and left an unreachable nil check standing at the point of use.
var currentPathQueryShapes = map[string]func(string) bool{
	// parseCalendarMonthQuery: time.Parse("2006-01") on the trimmed value.
	"month": isCalendarMonthQueryShape,
	// parseCalendarDayParam -> ParseDayDate: time.Parse("2006-01-02") on the
	// trimmed value, plus a zone-existence check this policy cannot mirror
	// (see isCalendarDayQueryShape).
	"day":      isCalendarDayQueryShape,
	"selected": isCalendarDayQueryShape,
	// ParseBoolLike: a closed set of truthy spellings.
	"edit": isBoolLikeQueryShape,
	// ResolveOnboardingStep: Atoi then clamped to 1..2.
	"step": isOnboardingStepQueryShape,
}

const currentPathBackParameter = "back"

// isCalendarMonthQueryShape mirrors parseCalendarMonthQuery: the month anchor is
// parsed with the "2006-01" layout after trimming, so anything else never
// reaches the calendar as a month and has no business being rendered.
func isCalendarMonthQueryShape(value string) bool {
	_, err := time.Parse("2006-01", strings.TrimSpace(value))
	return err == nil
}

// isCalendarDayQueryShape mirrors the format half of ParseDayDate: the
// "2006-01-02" layout after trimming.
//
// ParseDayDate additionally refuses a date the request's own zone skips
// entirely (a DST jump over local midnight), which needs a *time.Location this
// policy deliberately does not take — it also filters `back` values, where no
// request zone applies. The residual is therefore exactly "a well-formed date
// that does not exist in the viewer's zone": ten digits and two dashes, which
// cannot carry an address, a token or an error code. The page itself still
// refuses such a date through ParseDayDate; only the rendered address is
// slightly more permissive than the consumer.
func isCalendarDayQueryShape(value string) bool {
	_, err := time.Parse("2006-01-02", strings.TrimSpace(value))
	return err == nil
}

// isBoolLikeQueryShape mirrors ParseBoolLike's truthy set. A value ParseBoolLike
// reads as false is dropped rather than kept: an absent parameter parses to
// false too, so dropping it renders a shorter address with identical meaning.
func isBoolLikeQueryShape(value string) bool {
	switch strings.ToLower(strings.TrimSpace(value)) {
	case "1", "true", "on", "yes":
		return true
	default:
		return false
	}
}

// isOnboardingStepQueryShape mirrors ResolveOnboardingStep's reachable range:
// it parses an integer and clamps it to 1..2, so those two are the only step
// values that ever describe a real page state.
func isOnboardingStepQueryShape(value string) bool {
	switch strings.TrimSpace(value) {
	case "1", "2":
		return true
	default:
		return false
	}
}

// isLocalRoutePathShape reports whether a path looks like a route this app can
// actually serve. Every path registered in RegisterRoutes is built from ASCII
// letters, digits, "/", "-", "_" and "." (`/calendar/day/2026-02-21`,
// `/register/welcome`, `/favicon.ico`), and a concrete request path never
// carries the ":" of a route template. Excluding everything else keeps an
// address, a percent-escape or a control character out of a rendered `back`,
// which SanitizeRedirectPath alone would accept as "some local path".
func isLocalRoutePathShape(path string) bool {
	if path == "" {
		return false
	}
	for _, character := range path {
		switch {
		case character >= 'a' && character <= 'z',
			character >= 'A' && character <= 'Z',
			character >= '0' && character <= '9',
			character == '/', character == '-', character == '_', character == '.':
		default:
			return false
		}
	}
	return true
}

// SanitizeCurrentPathQuery filters a raw request URI down to what may safely be
// rendered back into the page it describes: the fragment is dropped, and the
// query is reduced to currentPathQueryAllowlist — allowed names carrying values
// of the shape their own consumer accepts. It returns the path with the
// surviving parameters in a deterministic (sorted) order, or the bare path when
// none survive.
//
// The path half is otherwise returned unchanged — deciding whether a path may
// be navigated to is SanitizeRedirectPath's job, not this one's.
//
// It fails closed throughout: a query the parser rejects yields the bare path
// rather than the raw input, and a parameter whose value does not match its
// shape is dropped rather than trimmed into one.
//
// `back` carries a path of its own, so its value is filtered by the same policy
// one level down and must itself look like a local route. Exactly one level: a
// `back` nested inside a `back` value is dropped rather than recursed into, and
// an inner value that does not parse drops `back` entirely instead of being
// kept raw.
func SanitizeCurrentPathQuery(rawURI string) string {
	sanitized, _ := sanitizeCurrentPathQuery(rawURI, true)
	return sanitized
}

// sanitizeCurrentPathQuery reports false when the query could not be parsed, so
// the caller filtering a nested value can drop that value instead of keeping an
// unfiltered one. The returned string is always safe to render either way.
func sanitizeCurrentPathQuery(rawURI string, allowBack bool) (string, bool) {
	// The fragment is cut first and discarded. It lives in the path half, so
	// cutting at "?" alone would carry it through whole, and a URI with no "?"
	// at all would be returned verbatim. A browser never sends a fragment to
	// the server, but a caller-supplied `back` value reaches this same function
	// with one intact. Nothing downstream needs it: neither the language switch
	// nor the privacy back link.
	withoutFragment, _, _ := strings.Cut(rawURI, "#")

	path, query, hasQuery := strings.Cut(withoutFragment, "?")
	if !hasQuery || query == "" {
		return path, true
	}

	values, err := url.ParseQuery(query)
	if err != nil {
		return path, false
	}

	retained := url.Values{}
	for key, parameterValues := range values {
		// `back` is allowlisted here rather than through the shape map: its
		// decision rewrites the value instead of judging it.
		if key == currentPathBackParameter {
			if !allowBack {
				continue
			}
			nested, ok := sanitizeNestedBackValues(parameterValues)
			if !ok {
				continue
			}
			retained[key] = nested
			continue
		}
		matchesShape, allowed := currentPathQueryShapes[key]
		if !allowed {
			continue
		}
		if !everyValueMatchesShape(parameterValues, matchesShape) {
			continue
		}
		retained[key] = parameterValues
	}

	if len(retained) == 0 {
		return path, true
	}
	return path + "?" + retained.Encode(), true
}

// everyValueMatchesShape drops a repeated parameter as a whole as soon as one of
// its occurrences fails the shape: keeping the good half of a crafted pair would
// leave the caller in control of which one survives.
// A nil shape is not reachable from currentPathQueryShapes as it stands, and is
// refused rather than dereferenced anyway: calling a nil func would panic, so a
// future entry added to the map without its shape must degrade to "drop the
// parameter" instead of taking down the request. Pinned by
// TestEveryValueMatchesShapeRefusesAMissingShape, which constructs that state
// directly rather than claiming it occurs today.
func everyValueMatchesShape(parameterValues []string, matchesShape func(string) bool) bool {
	if matchesShape == nil {
		return false
	}
	for _, value := range parameterValues {
		if !matchesShape(value) {
			return false
		}
	}
	return true
}

// sanitizeNestedBackValues runs each back value through the same policy with
// further nesting disabled, and reports false as soon as one of them fails to
// parse or does not look like a local route — the whole parameter is then
// dropped. SanitizeRedirectPath supplies the structural rules it already owns
// (leading slash, no protocol-relative or absolute URL, no CR/LF); the route
// character set is checked on top, since SanitizeRedirectPath would accept
// "/victim@example.com" as just another local path.
func sanitizeNestedBackValues(parameterValues []string) ([]string, bool) {
	sanitized := make([]string, 0, len(parameterValues))
	for _, value := range parameterValues {
		nested, ok := SanitizeBackNavigationValue(value)
		if !ok {
			return nil, false
		}
		sanitized = append(sanitized, nested)
	}
	return sanitized, true
}

// SanitizeBackNavigationValue is the single decision about what a caller-supplied
// back-link value may become. It reports false when the value must be dropped in
// favour of a fallback.
//
// It exists so the two surfaces that render such a value cannot drift apart: the
// `back` parameter inside a rendered current path, and the privacy page's own
// back link, which receives the value straight from the query string. A check
// that lived on only one of them left the other reachable with exactly the input
// it refused.
//
// The value is fragment-stripped, its own query filtered by the same allowlist
// one level down, and the result must be both a redirect-safe local path
// (SanitizeRedirectPath's structural rules) and built from the characters this
// app's routes actually use.
func SanitizeBackNavigationValue(value string) (string, bool) {
	nested, ok := sanitizeCurrentPathQuery(value, false)
	if !ok {
		return "", false
	}
	// Emptiness is not tested separately here: SanitizeRedirectPath returns the
	// empty fallback for an empty candidate, so an empty value reaches
	// isLocalRoutePathShape, which is the one place that decides whether a path
	// can name a route. Two places answering that left the shared one unreached.
	if SanitizeRedirectPath(nested, "") != nested {
		return "", false
	}
	nestedPath, _, _ := strings.Cut(nested, "?")
	if !isLocalRoutePathShape(nestedPath) {
		return "", false
	}
	return nested, true
}
