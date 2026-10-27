package services

import (
	"net/url"
	"strings"
)

// Masking thresholds for the destination the settings ledger shows. They are
// constants of the display, not of validation: nothing here decides whether a
// URL may be stored.
const (
	// webhookDestinationMinSegments is how many path segments a path needs
	// before its first one is shown in clear. A single segment is usually the
	// secret itself (an ntfy topic), so it is never shown.
	webhookDestinationMinSegments = 2
	// webhookDestinationTailMinSegment is the length of the LAST path segment
	// above which its last webhookDestinationTailChars characters are shown. A
	// shorter segment would be mostly revealed by its own tail.
	webhookDestinationTailMinSegment = 8
	webhookDestinationTailChars      = 4
	webhookDestinationElision        = "…"
)

// webhookDestinationDisplay is the render-safe destination line for the owner's
// own settings page: host[:port] and a MASKED path, so an owner with several
// endpoints can tell which one is stored without the page ever carrying the
// token the path may embed.
//
//   - Userinfo, query and fragment are never shown.
//   - A default port (443 for https, 80 for http) is omitted.
//   - The first path segment is shown only when the path has at least two
//     segments; the last four characters of the LAST segment only when that
//     segment is longer than eight characters (the tail never crosses a "/").
//     Everything between is an ellipsis.
//   - A path of "/" or nothing adds nothing; any other path shows at least "/…".
//
// It returns "" for a value that does not parse or names no host. This is a
// separate projection from hostOnly: the delivery log, the CLI and the host
// state of the ledger stay host-only, and only this settings line widens.
func webhookDestinationDisplay(rawURL string) string {
	parsed, err := url.Parse(strings.TrimSpace(rawURL))
	if err != nil {
		return ""
	}
	host := parsed.Hostname()
	if host == "" {
		return ""
	}
	if strings.Contains(host, ":") {
		host = "[" + host + "]"
	}
	if port := parsed.Port(); port != "" && !isDefaultWebhookPort(parsed.Scheme, port) {
		host += ":" + port
	}
	return host + maskedWebhookPath(parsed.EscapedPath())
}

func isDefaultWebhookPort(scheme string, port string) bool {
	return (strings.EqualFold(scheme, "https") && port == "443") ||
		(strings.EqualFold(scheme, "http") && port == "80")
}

func maskedWebhookPath(path string) string {
	segments := make([]string, 0, 4)
	for _, segment := range strings.Split(path, "/") {
		if segment != "" {
			segments = append(segments, segment)
		}
	}
	if len(segments) == 0 {
		return ""
	}

	masked := "/"
	if len(segments) >= webhookDestinationMinSegments {
		masked += segments[0] + "/"
	}
	masked += webhookDestinationElision
	// The tail is cut from the LAST SEGMENT alone, never across a "/": cut from
	// the whole path, a short final segment would be shown whole together with
	// the end of the one before it.
	if last := segments[len(segments)-1]; len(last) > webhookDestinationTailMinSegment {
		masked += "/" + last[len(last)-webhookDestinationTailChars:]
	}
	return masked
}
