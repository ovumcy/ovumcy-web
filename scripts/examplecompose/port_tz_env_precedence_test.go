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
	rootEnvironmentPort = regexp.MustCompile(`(?ms)^\s{4}environment:\n(?:.*\n)*?\s{6}-\s*PORT=(\S+)\s*$`)
	rootEnvironmentTZ   = regexp.MustCompile(`(?ms)^\s{4}environment:\n(?:.*\n)*?\s{6}-\s*TZ=`)
	rootPortsMapping    = regexp.MustCompile(`(?m)^\s{4}ports:\n\s{6}-\s*".*:(\$\{PORT:-\d+\}):(\$\{PORT:-\d+\})"\s*$`)
)

// TestRootComposePortTracksTheEnvironmentBlock asserts the root
// docker-compose.yml sets PORT inside `environment:` with the SAME default
// the `ports:` mapping uses for both host and container target — not merely
// that a PORT= entry exists. A guard that only checked presence would stay
// green while the two defaults drifted apart, which is exactly the class of
// bug this test exists to catch (the value, not just the key, is what has to
// track).
func TestRootComposePortTracksTheEnvironmentBlock(t *testing.T) {
	content := readRootCompose(t)

	envMatch := rootEnvironmentPort.FindSubmatch(content)
	if envMatch == nil {
		t.Fatal("docker-compose.yml: `environment:` has no PORT= entry, so the host/container ports: mapping (${PORT:-8080}) can diverge from the value env_file delivers to the process")
	}

	portsMatch := rootPortsMapping.FindSubmatch(content)
	if portsMatch == nil {
		t.Fatal("docker-compose.yml: no `ports:` mapping of the form \"host:${PORT:-N}:${PORT:-N}\" found — the fixture this test's premise depends on has changed shape")
	}

	envDefault, hostDefault, containerDefault := string(envMatch[1]), string(portsMatch[1]), string(portsMatch[2])
	if envDefault != hostDefault || hostDefault != containerDefault {
		t.Fatalf("docker-compose.yml: PORT defaults disagree — environment: %s, ports: host %s / container %s (the container listens on env_file's PORT, so all three must resolve to the same value)", envDefault, hostDefault, containerDefault)
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

// TestPortDefaultPatternsDetectTheRegression proves the patterns above tell a
// matching PORT default from a diverged one, on a fixture this test owns
// rather than on the shipped file it judges.
func TestPortDefaultPatternsDetectTheRegression(t *testing.T) {
	matching := "    environment:\n      - PORT=${PORT:-8080}\n    ports:\n      - \"127.0.0.1:${PORT:-8080}:${PORT:-8080}\"\n"
	envMatch := rootEnvironmentPort.FindStringSubmatch(matching)
	portsMatch := rootPortsMapping.FindStringSubmatch(matching)
	if envMatch == nil || portsMatch == nil {
		t.Fatalf("both patterns must match a well-formed stack, got env=%v ports=%v", envMatch, portsMatch)
	}
	if envMatch[1] != portsMatch[1] || portsMatch[1] != portsMatch[2] {
		t.Fatalf("a matching fixture must report equal defaults, got env=%q host=%q container=%q", envMatch[1], portsMatch[1], portsMatch[2])
	}

	// The regression: ports: bumped its default, environment: left behind.
	diverged := "    environment:\n      - PORT=${PORT:-8080}\n    ports:\n      - \"127.0.0.1:${PORT:-9090}:${PORT:-9090}\"\n"
	envMatch = rootEnvironmentPort.FindStringSubmatch(diverged)
	portsMatch = rootPortsMapping.FindStringSubmatch(diverged)
	if envMatch == nil || portsMatch == nil {
		t.Fatalf("both patterns must still match the diverged fixture, got env=%v ports=%v", envMatch, portsMatch)
	}
	if envMatch[1] == portsMatch[1] {
		t.Fatalf("the diverged fixture must report unequal defaults, got env=%q ports host=%q", envMatch[1], portsMatch[1])
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
