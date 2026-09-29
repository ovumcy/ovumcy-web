package api

import (
	"strings"
	"testing"
)

// padBodyToSize returns body followed by trailing spaces, exactly size bytes
// long. A JSON decoder accepts insignificant whitespace after the top-level
// value, so a padded JSON body decodes to the same request at any size, and a
// body-cap regression can place one request at the cap and the same request one
// byte past it.
func padBodyToSize(t *testing.T, body string, size int) string {
	t.Helper()
	if len(body) > size {
		t.Fatalf("body %q is %d bytes, longer than the %d requested", body, len(body), size)
	}
	return body + strings.Repeat(" ", size-len(body))
}
