// Package testpackages is the one way a test in this tree type-checks Go
// packages with golang.org/x/tools/go/packages.
//
// go/types on Go 1.27 races between (*Named).unpack and (*Checker).isComplete
// when go/packages type-checks several packages at once and they share an
// imported generic instance (golang/go#81122). Under -race that failure lands on
// whichever guard happened to load, not on a defect of this tree. go/packages
// bounds its concurrent type-checks by a semaphore sized from GOMAXPROCS when it
// is initialised, so the only knob is the process: under -race, Isolate re-runs
// the calling test in a child of the same test binary with GOMAXPROCS=1, where
// every check holds the one token in turn and is ordered after the last. The
// child runs the whole test, so no assertion is dropped. Without -race both
// functions are plain pass-throughs.
//
// Retire this package once go.mod names a Go release that carries the fix.
package testpackages

import (
	"errors"
	"fmt"
	"os"
	"os/exec"
	"regexp"
	"runtime"
	"strings"
	"testing"
	"time"

	"golang.org/x/tools/go/packages"
)

const serialChildEnv = "OVUMCY_TESTPACKAGES_SERIAL_CHILD"

// ErrNotIsolated is Load's answer under -race outside a serialised child: the
// test that reached Load does not open with Isolate.
var ErrNotIsolated = errors.New("testpackages.Load under -race outside a serialised child: open the test with `if testpackages.Isolate(t) { return }`")

// Isolate must open every test that reaches Load. It reports true when the
// test has already run to completion in a serialised child, and the caller
// returns at once; the parent then passes, skips or fails as the child did.
func Isolate(t *testing.T) bool {
	t.Helper()
	if !raceEnabled || os.Getenv(serialChildEnv) == "1" {
		return false
	}

	segments := strings.Split(t.Name(), "/")
	for i, segment := range segments {
		segments[i] = "^" + regexp.QuoteMeta(segment) + "$"
	}
	args := []string{"-test.run=" + strings.Join(segments, "/"), "-test.count=1", "-test.v"}
	if deadline, ok := t.Deadline(); ok {
		args = append(args, "-test.timeout="+time.Until(deadline).Round(time.Second).String())
	}
	child := exec.Command(os.Args[0], args...) // #nosec G204 -- test-only (imported solely by _test.go callers); the binary is this test binary itself, and the arguments are the running test's own name, regexp-quoted, and its deadline, never external input.
	child.Env = append(os.Environ(), serialChildEnv+"=1", "GOMAXPROCS=1")
	output, err := child.CombinedOutput()

	verdict := func(word string) bool {
		return strings.Contains(string(output), "--- "+word+": "+t.Name()+" (")
	}
	switch {
	case err != nil:
		t.Fatalf("serialised run of %s failed: %v\n%s", t.Name(), err, output)
	case verdict("PASS"):
	case verdict("SKIP"):
		t.Skipf("serialised run of %s skipped:\n%s", t.Name(), output)
	default:
		t.Fatalf("serialised run of %s did not run the test:\n%s", t.Name(), output)
	}
	return true
}

// Load is packages.Load, refused under -race unless the calling test went
// through Isolate.
func Load(config *packages.Config, patterns ...string) ([]*packages.Package, error) {
	if raceEnabled {
		if os.Getenv(serialChildEnv) != "1" {
			return nil, ErrNotIsolated
		}
		if procs := runtime.GOMAXPROCS(0); procs != 1 {
			return nil, fmt.Errorf("testpackages.Load in a serialised child with GOMAXPROCS=%d, want 1", procs)
		}
	}
	return packages.Load(config, patterns...)
}
