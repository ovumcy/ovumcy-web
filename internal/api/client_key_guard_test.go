package api

import (
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
)

// directIPRead matches IP() and IPs() on any receiver, whatever the handler
// names its fiber.Ctx.
var directIPRead = regexp.MustCompile(`\.IPs?\(\)`)

// TestHandlersKeyClientBucketsOnTheEdgeKey keeps every per-client attempt
// bucket on requestClientKey (WEB-395 L3). c.IP() behind a trusted proxy with
// PROXY_HEADER=X-Forwarded-For is the client-supplied leftmost entry, so a
// handler that reads it again lets a stranger spend a victim address's budget.
// Only client_key.go may read it, as the fallback for an app wired without the
// middleware.
func TestHandlersKeyClientBucketsOnTheEdgeKey(t *testing.T) {
	files, err := filepath.Glob("*.go")
	if err != nil {
		t.Fatalf("glob: %v", err)
	}
	scanned := 0
	for _, file := range files {
		if strings.HasSuffix(file, "_test.go") || file == "client_key.go" {
			continue
		}
		source, err := os.ReadFile(file)
		if err != nil {
			t.Fatalf("read %s: %v", file, err)
		}
		scanned++
		if directIPRead.Match(source) {
			t.Errorf("%s reads the fiber client IP directly: key per-client state on requestClientKey(c), which follows the edge limiters' trusted-proxy boundary", file)
		}
	}
	if scanned == 0 {
		t.Fatal("scanned no source files: the guard is not looking at the package")
	}
}
