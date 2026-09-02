package examplecompose

import (
	"os"
	"path/filepath"
	"regexp"
	"testing"
)

// The root docker-compose.yml is the only shipped stack that combines
// `env_file: ./.env` with an `environment:` override block AND a `ports:`
// mapping built from the same ${PORT:-8080} substitution. That combination
// has two failure modes:
//
//   - PORT: `ports:` resolves ${PORT:-8080} from the shell/project .env at
//     compose-parse time for BOTH host and container target, but the
//     container's actual listening port came only from env_file's .env
//     (until PORT was added to `environment:` too). `PORT=9090 docker
//     compose up` then published 9090 while the process still listened on
//     8080 from .env — reachable-but-silent.
//   - TZ: an entry inside `environment:` always outranks env_file for the
//     container's runtime env, so a bare `TZ=${TZ:-UTC}` there let a
//     leftover shell TZ silently outrank .env's TZ, and TZ decides cycle-day
//     boundaries in this product.
var (
	rootEnvironmentPort = regexp.MustCompile(`(?ms)^\s{4}environment:\n(?:.*\n)*?\s{6}-\s*PORT=`)
	rootEnvironmentTZ   = regexp.MustCompile(`(?ms)^\s{4}environment:\n(?:.*\n)*?\s{6}-\s*TZ=`)
)

// TestRootComposePortTracksTheEnvironmentBlock asserts the root
// docker-compose.yml sets PORT inside `environment:`, so the value read for
// the `ports:` mapping and the value the container actually listens on come
// from the same substitution and cannot diverge.
func TestRootComposePortTracksTheEnvironmentBlock(t *testing.T) {
	content := readRootCompose(t)
	if !rootEnvironmentPort.Match(content) {
		t.Fatal("docker-compose.yml: `environment:` has no PORT= entry, so the host/container ports: mapping (${PORT:-8080}) can diverge from the value env_file delivers to the process")
	}
}

// TestRootComposeTZComesOnlyFromEnvFile asserts TZ is NOT re-declared inside
// `environment:`, so a shell TZ cannot silently outrank .env's TZ (env_file
// entries only win when `environment:` does not also set the key).
func TestRootComposeTZComesOnlyFromEnvFile(t *testing.T) {
	content := readRootCompose(t)
	if rootEnvironmentTZ.Match(content) {
		t.Fatal("docker-compose.yml: `environment:` re-declares TZ=, so a leftover shell TZ silently outranks .env's TZ (environment: always wins over env_file)")
	}
}

func readRootCompose(t *testing.T) []byte {
	t.Helper()
	root := repoRoot(t)
	content, err := os.ReadFile(filepath.Join(root, "docker-compose.yml"))
	if err != nil {
		t.Fatalf("read docker-compose.yml: %v", err)
	}
	return content
}
