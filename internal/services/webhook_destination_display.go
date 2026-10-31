package services

import (
	"net/url"
	"strings"
)

// webhookDestinationDisplay is the render-safe destination line for the owner's
// own settings page: host and, when it is not the scheme's default, port. No
// part of the path is shown, masked or otherwise: a path segment can be the
// credential itself (Telegram's /bot<id>:<token>/sendMessage puts the token in
// the FIRST segment, an ntfy topic is the only one), and no masking rule can
// tell a token from a route by its position.
//
// It returns "" for a value that does not parse or names no host. The port is
// the one thing this line adds over hostOnly, so an owner with two services on
// one host can tell which is stored.
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
	return host
}

func isDefaultWebhookPort(scheme string, port string) bool {
	return (strings.EqualFold(scheme, "https") && port == "443") ||
		(strings.EqualFold(scheme, "http") && port == "80")
}
