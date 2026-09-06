package httpx

import "strings"

// RoutingNormalizedPath returns path in the SAME normalization fiber uses to
// pick a route, so a predicate that is not fiber's own router can still
// compare what the router compared.
//
// c.Path() hands back the untouched path off the wire (DefaultCtx.pathOriginal).
// Route matching runs against a separate, unexported "detection path"
// (DefaultCtx.configDependentPaths): ASCII-lowercased while CaseSensitive is
// off, with trailing slashes stripped while StrictRouting is off — the app's
// shipped fiberConfig leaves both off. Nothing else is folded: "%6c", "//" in
// the middle and "/." stay literal and simply fail to match a route.
//
// This normalization is a SUPERSET of the router's whenever CaseSensitive or
// StrictRouting is turned on: it then claims a spelling fiber would answer
// 404 to. The opposite mistake — comparing the raw path — is the one that
// matters here, since it claims FEWER spellings than the router accepts.
//
// strings.ToLower is the wrong primitive for the lowering half: it is
// Unicode-aware and folds code points such as U+212A KELVIN SIGN onto ASCII
// letters, which fiber's own byte table (gofiber/utils/v2/bytes.UnsafeToLower)
// leaves alone — that would claim a normalization the router itself never
// performs.
func RoutingNormalizedPath(path string) string {
	normalized := asciiLowerPath(path)
	if len(normalized) > 1 && normalized[len(normalized)-1] == '/' {
		normalized = strings.TrimRight(normalized, "/")
	}
	return normalized
}

// asciiLowerPath lowercases the ASCII letters of path and leaves every other
// byte untouched, mirroring the byte table fiber applies to the routing path
// (gofiber/utils/v2/bytes.UnsafeToLower).
func asciiLowerPath(path string) string {
	lowered := []byte(path)
	changed := false
	for i := range lowered {
		if lowered[i] >= 'A' && lowered[i] <= 'Z' {
			lowered[i] += 'a' - 'A'
			changed = true
		}
	}
	if !changed {
		return path
	}
	return string(lowered)
}
