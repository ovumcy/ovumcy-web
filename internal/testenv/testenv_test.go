package testenv

import (
	"errors"
	"fmt"
	"os/exec"
	"strings"
	"testing"
	"time"
)

// TestTimeZoneErrorClassificationSeparatesAbsenceFromCorruption pins the
// judgement RequireTimeZone makes on time.LoadLocation's error, which is what
// decides skip-versus-fail for every timezone site in this tree. The malformed
// case cannot be provoked through LoadLocation without shipping a corrupt tz
// database, so the classifier is driven directly; an unrecognised error must
// land on the failing side, because it is not evidence the data is absent.
func TestTimeZoneErrorClassificationSeparatesAbsenceFromCorruption(t *testing.T) {
	for _, testCase := range []struct {
		what      string
		err       error
		isAbsence bool
	}{
		{"the zone is not in the database", errors.New("unknown time zone America/Toronto"), true},
		{"the database is present and corrupt", errors.New("malformed time zone information"), false},
		{"an error this classifier has never seen", errors.New("permission denied reading zoneinfo"), false},
	} {
		t.Run(testCase.what, func(t *testing.T) {
			if got := tzErrorIsAbsence(testCase.err); got != testCase.isAbsence {
				t.Fatalf("tzErrorIsAbsence(%q) = %v, want %v: only a zone that is missing may reach the skip path", testCase.err, got, testCase.isAbsence)
			}
		})
	}
}

// recordingT records the verdict a helper reaches instead of delivering it.
// Fatalf and Skipf never return on a real *testing.T, so a test asking "which
// of the two did this call reach" cannot use one directly — this substitute
// records both and returns, letting the helper under observation run on to
// its own `return`. Modeled on internal/testdb's recordingT.
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

// TestIsRequiredReadsTheGateFailClosed pins the value handling: a gate that
// silently disarms on an unexpected value is worse than no gate, because the
// lane keeps reporting success. Only an absent, empty or explicitly false
// value disarms it.
func TestIsRequiredReadsTheGateFailClosed(t *testing.T) {
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
			t.Setenv(RequireEnv("widget"), value)

			if got := IsRequired("widget"); got != want {
				t.Fatalf("%s=%q: expected required=%v, got %v", RequireEnv("widget"), value, want, got)
			}
		})
	}
}

// TestRequireEnvIsTheOneMapping pins the mechanical name derivation a lane's
// declaration and a test's probe both rely on.
func TestRequireEnvIsTheOneMapping(t *testing.T) {
	cases := map[string]string{
		"docker": "OVUMCY_REQUIRE_DOCKER",
		"bash":   "OVUMCY_REQUIRE_BASH",
		"git":    "OVUMCY_REQUIRE_GIT",
		"tzdata": "OVUMCY_REQUIRE_TZDATA",
	}
	for resource, want := range cases {
		if got := RequireEnv(resource); got != want {
			t.Fatalf("RequireEnv(%q) = %q, want %q", resource, got, want)
		}
	}
}

// TestSkipUnlessRequiredSkipsOnAbsenceWhenNotRequired is the developer-host
// half: with the gate unset, a genuine absence is not this test's problem.
func TestSkipUnlessRequiredSkipsOnAbsenceWhenNotRequired(t *testing.T) {
	t.Setenv(RequireEnv("widget"), "")

	recorder := &recordingT{}
	SkipUnlessRequired(recorder, "widget", "widget is required: %v", exec.ErrNotFound)

	if recorder.fatal != "" {
		t.Fatalf("an absence must skip when the gate is unset; got %s", recorder.verdict())
	}
	if recorder.skipped == "" {
		t.Fatalf("an absence must skip when the gate is unset; got %s", recorder.verdict())
	}
}

// TestSkipUnlessRequiredFailsOnAbsenceWhenRequired is the owning-lane half:
// the same absence becomes a failure once the lane declares the resource
// mandatory, and the failure must name the gate that turned it into one.
func TestSkipUnlessRequiredFailsOnAbsenceWhenRequired(t *testing.T) {
	t.Setenv(RequireEnv("widget"), "1")

	recorder := &recordingT{}
	SkipUnlessRequired(recorder, "widget", "widget is required: %v", exec.ErrNotFound)

	if recorder.skipped != "" {
		t.Fatalf("with the gate set an absence must fail, not skip; got %s", recorder.verdict())
	}
	if recorder.fatal == "" {
		t.Fatalf("with the gate set an absence must fail; got %s", recorder.verdict())
	}
	if !strings.Contains(recorder.fatal, RequireEnv("widget")) {
		t.Fatalf("the failure must name the gate that turned the skip into one; got %s", recorder.verdict())
	}
}

// TestFailNeverSkipsRegardlessOfTheGate pins the other half of the
// absence/operational split: an operational error fails on every machine,
// whether or not the lane declared the resource required. Fail takes no
// resource argument at all, so there is no gate value that could route it to
// Skipf — this exercises both settings to make that structural guarantee
// explicit rather than assumed.
func TestFailNeverSkipsRegardlessOfTheGate(t *testing.T) {
	for _, gate := range []string{"", "1"} {
		t.Run(fmt.Sprintf("gate=%q", gate), func(t *testing.T) {
			t.Setenv(RequireEnv("widget"), gate)

			recorder := &recordingT{}
			Fail(recorder, "widget answered garbage")

			if recorder.skipped != "" {
				t.Fatalf("an operational error must never skip; got %s", recorder.verdict())
			}
			if recorder.fatal == "" {
				t.Fatalf("an operational error must fail; got %s", recorder.verdict())
			}
		})
	}
}

// TestRequireLookPathSkipsOnAbsenceAndFailsWhenRequired exercises the PATH
// probe end to end against a binary name that cannot exist, the same
// distinction TestSkipUnlessRequired... pins directly.
func TestRequireLookPathSkipsOnAbsenceAndFailsWhenRequired(t *testing.T) {
	const missing = "ovumcy-testenv-nonexistent-binary"

	t.Run("not required", func(t *testing.T) {
		t.Setenv(RequireEnv("widget"), "")
		recorder := &recordingT{}
		if got := RequireLookPath(recorder, "widget", missing); got != "" {
			t.Fatalf("an absent binary must not report a path; got %q", got)
		}
		if recorder.fatal != "" || recorder.skipped == "" {
			t.Fatalf("an absent binary must skip when the gate is unset; got %s", recorder.verdict())
		}
	})

	t.Run("required", func(t *testing.T) {
		t.Setenv(RequireEnv("widget"), "1")
		recorder := &recordingT{}
		if got := RequireLookPath(recorder, "widget", missing); got != "" {
			t.Fatalf("an absent binary must not report a path; got %q", got)
		}
		if recorder.skipped != "" || recorder.fatal == "" {
			t.Fatalf("an absent binary must fail when the gate is set; got %s", recorder.verdict())
		}
	})
}

// TestRequireTimeZoneSkipsOnAbsenceAndFailsWhenRequired exercises the
// zoneinfo probe end to end against a zone name that cannot exist, pinning
// the same absence/required distinction the PATH probe above pins for a
// binary.
func TestRequireTimeZoneSkipsOnAbsenceAndFailsWhenRequired(t *testing.T) {
	const bogus = "Ovumcy/Nonexistent_Zone"

	t.Run("not required", func(t *testing.T) {
		t.Setenv(RequireEnv("tzdata"), "")
		recorder := &recordingT{}
		if got := RequireTimeZone(recorder, bogus); got != nil {
			t.Fatalf("a nonexistent zone must not report a location; got %v", got)
		}
		if recorder.fatal != "" || recorder.skipped == "" {
			t.Fatalf("a missing zone must skip when the gate is unset; got %s", recorder.verdict())
		}
	})

	t.Run("required", func(t *testing.T) {
		t.Setenv(RequireEnv("tzdata"), "1")
		recorder := &recordingT{}
		if got := RequireTimeZone(recorder, bogus); got != nil {
			t.Fatalf("a nonexistent zone must not report a location; got %v", got)
		}
		if recorder.skipped != "" || recorder.fatal == "" {
			t.Fatalf("a missing zone must fail when the gate is set; got %s", recorder.verdict())
		}
	})
}

// TestRequireTimeZoneLoadsARealZone is the positive control: a zone this
// machine actually has must load cleanly, with neither verdict recorded.
func TestRequireTimeZoneLoadsARealZone(t *testing.T) {
	recorder := &recordingT{}
	location := RequireTimeZone(recorder, "America/New_York")

	if recorder.fatal != "" || recorder.skipped != "" {
		t.Fatalf("a real zone must load without failing or skipping; got %s", recorder.verdict())
	}
	if location == nil {
		t.Fatal("a real zone must return a non-nil location")
	}
}

// TestRequireTimeZoneFailsOnACorruptDatabaseWhateverTheGateSays drives the
// branch time.LoadLocation cannot produce on a sound host: an error that is not
// a plain absence. It is asserted with the gate unset as well as set, because a
// present-and-broken database is an operational error and those fail on every
// machine — a case that only ran with the gate set would leave the dispatch
// indistinguishable from SkipUnlessRequired's.
func TestRequireTimeZoneFailsOnACorruptDatabaseWhateverTheGateSays(t *testing.T) {
	corrupt := func(string) (*time.Location, error) {
		return nil, errors.New("malformed time zone information")
	}

	for name, gate := range map[string]string{"gate unset": "", "gate set": "1"} {
		t.Run(name, func(t *testing.T) {
			t.Setenv(RequireEnv("tzdata"), gate)
			recorder := &recordingT{}
			if got := requireTimeZone(recorder, "Europe/Berlin", corrupt); got != nil {
				t.Fatalf("a corrupt database must not report a location; got %v", got)
			}
			if recorder.skipped != "" || recorder.fatal == "" {
				t.Fatalf("a present-and-broken database must fail, never skip; got %s", recorder.verdict())
			}
		})
	}
}

// bashForProbeTest resolves a real bash for the two tests below. This package
// tests its OWN mechanism, not a production regression that a CI lane
// promises to run, so an ordinary skip on a bash-less workstation is
// appropriate here — every lane that runs this package's tests (ubuntu-latest)
// has bash, so the fail-closed class this file exists to add elsewhere never
// applies to its own harness.
func bashForProbeTest(t *testing.T) string {
	t.Helper()
	path, err := exec.LookPath("bash")
	if err != nil {
		t.Skipf("bash unavailable to exercise ProbeShell's own mechanism: %v", err)
	}
	return path
}

// TestProbeShellFailsOnATransportThatDoesNotRun pins the first operational
// branch: a shell path that cannot even be invoked is not an absence (the
// caller already resolved it via RequireLookPath) and must fail outright.
func TestProbeShellFailsOnATransportThatDoesNotRun(t *testing.T) {
	recorder := &recordingT{}
	ProbeShell(recorder, "ovumcy-testenv-nonexistent-binary", "printf ok", "ok")

	if recorder.skipped != "" {
		t.Fatalf("a shell that does not run must fail, not skip; got %s", recorder.verdict())
	}
	if recorder.fatal == "" {
		t.Fatalf("a shell that does not run must fail; got %s", recorder.verdict())
	}
}

// TestProbeShellFailsWhenTheAnswerIsWrong pins the second operational branch:
// a real, runnable shell that answers something other than `want` is found
// but broken, and must fail exactly as a transport that could not run at all.
func TestProbeShellFailsWhenTheAnswerIsWrong(t *testing.T) {
	bash := bashForProbeTest(t)

	recorder := &recordingT{}
	ProbeShell(recorder, bash, "printf garbage", "ok")

	if recorder.skipped != "" {
		t.Fatalf("a shell answering the wrong thing must fail, not skip; got %s", recorder.verdict())
	}
	if recorder.fatal == "" {
		t.Fatalf("a shell answering the wrong thing must fail; got %s", recorder.verdict())
	}
}

// TestProbeShellPassesOnAGenuineMatch is the positive control for the two
// failing cases above: a real shell that answers correctly reports neither
// verdict.
func TestProbeShellPassesOnAGenuineMatch(t *testing.T) {
	bash := bashForProbeTest(t)

	recorder := &recordingT{}
	ProbeShell(recorder, bash, "printf ok", "ok")

	if recorder.fatal != "" || recorder.skipped != "" {
		t.Fatalf("a correct answer must neither fail nor skip; got %s", recorder.verdict())
	}
}
