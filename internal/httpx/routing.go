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
// (gofiber/utils/v2/bytes.UnsafeToLower). It scans path read-only for an
// 'A'-'Z' byte before allocating anything: most requests already carry a
// lowercase path, and RoutingNormalizedPath runs on every request the two
// Next predicates that key on it see, so a []byte(path) copy taken and then
// discarded on every one of them would be exactly the per-request allocation
// this function exists to spare its callers.
func asciiLowerPath(path string) string {
	needsFold := false
	for i := range len(path) {
		if path[i] >= 'A' && path[i] <= 'Z' {
			needsFold = true
			break
		}
	}
	if !needsFold {
		return path
	}

	lowered := []byte(path)
	for i := range lowered {
		lowered[i] = asciiLowerByte(lowered[i])
	}
	return string(lowered)
}

// asciiLowerByte folds a single 'A'-'Z' byte to lowercase and leaves every
// other byte untouched — the byte-level primitive asciiLowerPath and
// HasRoutingPrefix share, so the two can never fold a byte differently from
// each other.
func asciiLowerByte(b byte) byte {
	if b >= 'A' && b <= 'Z' {
		return b + ('a' - 'A')
	}
	return b
}

// HasRoutingPrefix reports whether path begins with prefix under the SAME
// ASCII-only case fold fiber's router applies when CaseSensitive is off
// (gofiber/utils/v2/bytes.UnsafeToLower) — only 'A'-'Z' fold onto 'a'-'z',
// every other byte must match literally. strings.EqualFold is the wrong
// primitive for a routing-prefix check: it performs Unicode simple case
// folding, which equates code points such as U+212A KELVIN SIGN with ASCII
// 'k' and U+017F LATIN SMALL LETTER LONG S with ASCII 's' — a normalization
// fiber's router never performs, so a caller using it can accept a byte
// sequence the router itself would refuse to route as that prefix. It
// performs no allocation: prefix is compared against the corresponding slice
// of path one byte at a time.
func HasRoutingPrefix(path, prefix string) bool {
	if len(path) < len(prefix) {
		return false
	}
	for i := range len(prefix) {
		if asciiLowerByte(path[i]) != asciiLowerByte(prefix[i]) {
			return false
		}
	}
	return true
}
