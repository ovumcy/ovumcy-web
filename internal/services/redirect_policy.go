package services

import (
	"net/url"
	"strings"
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
// echo back into its own rendered address. It holds exactly the parameters the
// page handlers read: the calendar anchors, the onboarding step and the privacy
// back-link. Anything else is dropped whatever its value, because the rendered
// address reaches the markup of every page and, through the footer privacy
// link, a new outgoing URL — so an attacker-supplied `email` or `error` in a
// crafted link would land in browser history, the Referer header and the server
// access log.
var currentPathQueryAllowlist = map[string]struct{}{
	"month":    {},
	"day":      {},
	"selected": {},
	"edit":     {},
	"step":     {},
	"back":     {},
}

const currentPathBackParameter = "back"

// SanitizeCurrentPathQuery filters the query of a raw request URI down to
// currentPathQueryAllowlist and returns the path with the surviving parameters
// in a deterministic (sorted) order, or the bare path when none survive. The
// path half is returned unchanged — deciding whether a path may be navigated to
// is SanitizeRedirectPath's job, not this one's.
//
// It fails closed: a query the parser rejects yields the bare path rather than
// the raw input, so a malformed query can never pass through unfiltered.
//
// `back` carries a path of its own, so its value is filtered by the same policy
// one level down. Exactly one level: a `back` nested inside a `back` value is
// dropped rather than recursed into, and an inner value that does not parse
// drops `back` entirely instead of being kept raw.
func SanitizeCurrentPathQuery(rawURI string) string {
	sanitized, _ := sanitizeCurrentPathQuery(rawURI, true)
	return sanitized
}

// sanitizeCurrentPathQuery reports false when the query could not be parsed, so
// the caller filtering a nested value can drop that value instead of keeping an
// unfiltered one. The returned string is always safe to render either way.
func sanitizeCurrentPathQuery(rawURI string, allowBack bool) (string, bool) {
	path, query, hasQuery := strings.Cut(rawURI, "?")
	if !hasQuery || query == "" {
		return path, true
	}

	values, err := url.ParseQuery(query)
	if err != nil {
		return path, false
	}

	retained := url.Values{}
	for key, parameterValues := range values {
		if _, allowed := currentPathQueryAllowlist[key]; !allowed {
			continue
		}
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
		retained[key] = parameterValues
	}

	if len(retained) == 0 {
		return path, true
	}
	return path + "?" + retained.Encode(), true
}

// sanitizeNestedBackValues runs each back value through the same policy with
// further nesting disabled, and reports false as soon as one of them fails to
// parse — the whole parameter is then dropped.
func sanitizeNestedBackValues(parameterValues []string) ([]string, bool) {
	sanitized := make([]string, 0, len(parameterValues))
	for _, value := range parameterValues {
		nested, ok := sanitizeCurrentPathQuery(value, false)
		if !ok {
			return nil, false
		}
		sanitized = append(sanitized, nested)
	}
	return sanitized, true
}
