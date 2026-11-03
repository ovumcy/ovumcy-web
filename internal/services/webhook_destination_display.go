package services

import (
	"net/url"
	"strings"
)

const (
	// webhookTailRunes is how much of the path's end the display shows.
	webhookTailRunes = 4
	// webhookCredentialSegmentMin is the length from which a segment made only of
	// token characters is treated as a credential rather than a route name.
	webhookCredentialSegmentMin = 20
	webhookMaskedSegment        = "…"
)

// webhookDestinationDisplay is the render-safe destination line for the owner's
// own settings page: host, the port when it is not the scheme's default, and a
// MASKED path so two hooks on one host can still be told apart.
//
// The path is shown as the first segment in clear, then "/…/", then the last
// four characters of the last segment. Query, fragment and userinfo are never
// shown. Webhook paths often carry the secret (Slack, Discord and ntfy put it
// in the later segments or the only one), which is why the middle is masked and
// the tail is short.
//
// The first segment is shown in clear only when it is a route name. A segment
// is credential-shaped, and masked like the middle, when it contains ':' or is
// at least 20 characters of [A-Za-z0-9_-]: Telegram's
// /bot<id>:<token>/sendMessage puts the bot token in the FIRST segment, and a
// position rule alone would print a usable third-party token into the page. The
// check is a shape heuristic, not a proof: a token in the first segment that
// has neither shape still shows, which is the accepted residual.
//
// Where showing the masked parts would reveal the whole path, less is shown:
// a single-segment path (a topic that is the credential) never shows its
// segment in clear, and the tail is dropped unless at least as many characters
// stay hidden as are shown. Percent-escapes are judged decoded (an encoded ':'
// is still a colon) and displayed as stored.
//
// It returns "" for a value that does not parse or names no host.
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

// maskedWebhookPath renders an escaped URL path as "", "/…", "/…/tail",
// "/first/…" or "/first/…/tail". Empty segments and a trailing slash are
// ignored, so "/", "//" and "" all read as no path.
func maskedWebhookPath(escapedPath string) string {
	segments := make([]string, 0, 4)
	for _, segment := range strings.Split(escapedPath, "/") {
		if segment != "" {
			segments = append(segments, segment)
		}
	}
	if len(segments) == 0 {
		return ""
	}

	out := "/"
	// A lone segment is the whole path, so it is masked whatever its shape.
	if len(segments) > 1 && !isCredentialShapedSegment(segments[0]) {
		out += segments[0] + "/"
	}
	out += webhookMaskedSegment

	last := segments[len(segments)-1]
	if len(last) >= 2*webhookTailRunes {
		out += "/" + last[len(last)-webhookTailRunes:]
	}
	return out
}

// isCredentialShapedSegment reports whether an escaped path segment looks like
// a secret: a ':' (a "<id>:<token>" pair), or 20 or more characters drawn only
// from [A-Za-z0-9_-]. An escape that does not decode counts as credential-shaped.
func isCredentialShapedSegment(escapedSegment string) bool {
	segment, err := url.PathUnescape(escapedSegment)
	if err != nil {
		return true
	}
	if strings.Contains(segment, ":") {
		return true
	}
	if len(segment) < webhookCredentialSegmentMin {
		return false
	}
	for _, char := range segment {
		isToken := (char >= 'a' && char <= 'z') || (char >= 'A' && char <= 'Z') ||
			(char >= '0' && char <= '9') || char == '_' || char == '-'
		if !isToken {
			return false
		}
	}
	return true
}

func isDefaultWebhookPort(scheme string, port string) bool {
	return (strings.EqualFold(scheme, "https") && port == "443") ||
		(strings.EqualFold(scheme, "http") && port == "80")
}
