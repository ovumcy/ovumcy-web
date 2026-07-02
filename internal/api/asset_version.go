package api

import "strings"

// defaultAssetVersion is the cache-busting token used when the build revision
// is unavailable (for example a `go run` build with no VCS stamping). Any
// non-empty static value keeps the ?v= query present so the caching contract
// holds; it just does not change between such builds.
const defaultAssetVersion = "dev"

// SetAssetVersion records the build revision used to cache-bust static asset
// URLs (?v=<version>). The composition root passes buildRevision() here after
// constructing the handler, keeping the api layer free of any dependency on the
// binary's build-info wiring. The value is normalized to a short, URL-safe
// token so a malformed or empty revision can never break an asset URL.
func (handler *Handler) SetAssetVersion(revision string) {
	handler.assetVersion = normalizeAssetVersion(revision)
}

// normalizeAssetVersion reduces a raw build revision to a compact query-safe
// token: it trims surrounding space, drops any character outside a conservative
// URL-safe set, caps the length (a full git SHA is redundant for cache
// busting), and falls back to defaultAssetVersion when nothing usable remains.
func normalizeAssetVersion(revision string) string {
	var builder strings.Builder
	for _, r := range strings.TrimSpace(revision) {
		switch {
		case r >= 'a' && r <= 'z', r >= 'A' && r <= 'Z', r >= '0' && r <= '9', r == '-', r == '.', r == '_':
			builder.WriteRune(r)
		}
		if builder.Len() >= 16 {
			break
		}
	}
	token := builder.String()
	if token == "" {
		return defaultAssetVersion
	}
	return token
}
