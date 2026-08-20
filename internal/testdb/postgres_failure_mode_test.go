package testdb

import (
	"errors"
	"fmt"
	"os/exec"
	"strings"
	"testing"
)

// recordingT records the verdict a docker helper reaches instead of delivering
// it. It is deliberately not a general test double: the only thing this file
// needs to observe is WHICH of the two verdicts a docker error produces, and a
// real *testing.T cannot report that — a helper that calls Fatalf on it fails
// the very test asking the question, and one that calls Skipf ends it.
//
// Fatalf and Skipf on a real *testing.T never return; here they record and do
// return, so a helper under observation runs on to its own `return`. Every
// assertion below is about the recorder, never about what the helper returned.
type recordingT struct {
	fatal   string
	skipped string
}

func (r *recordingT) Helper() {}

func (r *recordingT) Fatalf(format string, args ...any) {
	r.fatal = fmt.Sprintf(format, args...)
}

func (r *recordingT) Skipf(format string, args ...any) {
	r.skipped = fmt.Sprintf(format, args...)
}

func (r *recordingT) verdict() string {
	switch {
	case r.fatal != "" && r.skipped != "":
		return fmt.Sprintf("both (fatal=%q skip=%q)", r.fatal, r.skipped)
	case r.fatal != "":
		return "fatal: " + r.fatal
	case r.skipped != "":
		return "skip: " + r.skipped
	default:
		return "neither"
	}
}

// injectDockerCommandFailure makes every docker invocation fail, as a broken
// image, an exhausted port range or a pull that timed out would.
func injectDockerCommandFailure(t *testing.T, failure error) {
	t.Helper()

	original := runDockerCommandWithError
	runDockerCommandWithError = func(args ...string) (string, error) {
		return "", fmt.Errorf("docker %s: %w", strings.Join(args, " "), failure)
	}
	t.Cleanup(func() { runDockerCommandWithError = original })
}

// injectMissingDockerBinary makes the docker-absent preflight fail, as a
// developer machine with no docker installed would.
func injectMissingDockerBinary(t *testing.T) {
	t.Helper()

	original := dockerLookPath
	dockerLookPath = func(file string) (string, error) {
		return "", fmt.Errorf("%s: %w", file, exec.ErrNotFound)
	}
	t.Cleanup(func() { dockerLookPath = original })
}

// TestDockerCommandFailureAfterThePreflightFailsRatherThanSkips pins the
// distinction the whole package rests on: the docker-absent probe may skip,
// and nothing after it may. The three argument sets are the three real
// post-preflight call sites — the image pull, the container start and the port
// lookup — which all reach the daemon through the same helper, so a skip there
// reported a broken runner exactly as it reported a host with no docker.
func TestDockerCommandFailureAfterThePreflightFailsRatherThanSkips(t *testing.T) {
	callSites := map[string][]string{
		"image pull":      {"pull", postgresTestImage},
		"container start": {"run", "-d", "--rm", "-P", postgresTestImage},
		"port lookup":     {"port", "abc123", "5432/tcp"},
	}

	for name, args := range callSites {
		t.Run(name, func(t *testing.T) {
			injectDockerCommandFailure(t, errors.New("no space left on device"))

			recorder := &recordingT{}
			runDockerCommand(recorder, args...)

			if recorder.skipped != "" {
				t.Fatalf("a docker %q failure after the preflight must fail the test, not skip it; got %s", args[0], recorder.verdict())
			}
			if recorder.fatal == "" {
				t.Fatalf("a docker %q failure after the preflight must fail the test; got %s", args[0], recorder.verdict())
			}
			if !strings.Contains(recorder.fatal, "no space left on device") {
				t.Fatalf("the failure must carry the underlying docker error; got %s", recorder.verdict())
			}
		})
	}
}

// TestMissingDockerBinaryStillSkipsWithoutTheRequireGate is the other half of
// the same rule, and the one that protects the developer machine: with the
// gate unset, no docker binary still means "not my test to run".
func TestMissingDockerBinaryStillSkipsWithoutTheRequireGate(t *testing.T) {
	t.Setenv(requirePostgresEnv, "")
	injectMissingDockerBinary(t)

	recorder := &recordingT{}
	requireDockerBinary(recorder)

	if recorder.fatal != "" {
		t.Fatalf("an absent docker binary must skip when %s is unset; got %s", requirePostgresEnv, recorder.verdict())
	}
	if recorder.skipped == "" {
		t.Fatalf("an absent docker binary must skip when %s is unset; got %s", requirePostgresEnv, recorder.verdict())
	}
}

// TestMissingDockerBinaryFailsWhenPostgresIsRequired arms the gate CI sets on
// the job that runs the Postgres suite: there a skipped suite is a lane
// reporting success for tests that never ran, so the last remaining skip
// becomes a failure too.
func TestMissingDockerBinaryFailsWhenPostgresIsRequired(t *testing.T) {
	t.Setenv(requirePostgresEnv, "1")
	injectMissingDockerBinary(t)

	recorder := &recordingT{}
	requireDockerBinary(recorder)

	if recorder.skipped != "" {
		t.Fatalf("with %s=1 an absent docker binary must fail; got %s", requirePostgresEnv, recorder.verdict())
	}
	if recorder.fatal == "" {
		t.Fatalf("with %s=1 an absent docker binary must fail; got %s", requirePostgresEnv, recorder.verdict())
	}
	if !strings.Contains(recorder.fatal, requirePostgresEnv) {
		t.Fatalf("the failure must name the gate that turned the skip into one; got %s", recorder.verdict())
	}
}

// TestPostgresIsRequiredReadsTheGateFailClosed pins the value handling: a gate
// that silently disarms on an unexpected value is worse than no gate, because
// the lane keeps reporting success. Only an absent, empty or explicitly false
// value disarms it.
func TestPostgresIsRequiredReadsTheGateFailClosed(t *testing.T) {
	cases := map[string]bool{
		"":      false,
		" ":     false,
		"0":     false,
		"false": false,
		"FALSE": false,
		"1":     true,
		"true":  true,
		"yes":   true,
		"y3s":   true,
	}

	for value, want := range cases {
		t.Run(fmt.Sprintf("%q", value), func(t *testing.T) {
			t.Setenv(requirePostgresEnv, value)

			if got := postgresIsRequired(); got != want {
				t.Fatalf("%s=%q: expected required=%v, got %v", requirePostgresEnv, value, want, got)
			}
		})
	}
}
