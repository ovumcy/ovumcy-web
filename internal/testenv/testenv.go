// Package testenv gives every test package one shared answer to the same
// question: a test that cannot find a resource it needs — a binary on PATH,
// the tz database — is answering "is it absent" and "did it misbehave once
// found" with the SAME t.Skipf, and only the first of those two may ever
// legitimately skip.
//
// An absence is a legitimate skip on a developer machine, and a failure on a
// lane that declared the resource mandatory (SkipUnlessRequired). An
// operational error — the resource was found but answered wrong, or a
// daemon behind it did not respond — is always a failure, on every machine,
// because a check that "ran" without actually exercising what it claims
// proves nothing either way (Fail). Conflating the two into one t.Skipf is
// exactly what lets a required check disappear under a misconfigured runner:
// the runner reports a broken tool the same way a developer machine reports
// one it never installed.
//
// Adapted from two mechanisms already in this tree: internal/testdb's
// OVUMCY_REQUIRE_POSTGRES gate (the fail-closed env-var reading, and the rule
// that only the docker-absent preflight may skip while every later docker
// error fails), and scripts/publishorder's requireShellTool (verifying a
// shell binary by running it through the same shell that will use it, not by
// exec.LookPath alone — the check has to say the right thing, not merely
// answer at all).
package testenv

import (
	"fmt"
	"os"
	"os/exec"
	"strings"
	"time"
)

// TestingT is the slice of *testing.T the helpers below need. A production
// *testing.T satisfies it unchanged; a package's own tests can substitute a
// recorder to observe which verdict a probe reaches without arming a real
// skip or failure (Fatalf and Skipf never return on a real *testing.T).
type TestingT interface {
	Helper()
	Fatalf(format string, args ...any)
	Skipf(format string, args ...any)
}

// RequireEnv derives the fail-closed switch a lane sets to declare a resource
// mandatory: RequireEnv("docker") is "OVUMCY_REQUIRE_DOCKER". A resource name
// and its env var are always this one mechanical mapping, so a lane's
// declaration in ci.yml and a test's probe can never drift into two
// different spellings of the same gate.
func RequireEnv(resource string) string {
	return "OVUMCY_REQUIRE_" + strings.ToUpper(resource)
}

// IsRequired reports whether `resource` is mandatory on this lane, read
// fail-closed: anything other than absent, empty, "0" or "false" arms it, so
// an unexpected value — a typo, a future spelling of "no" — never silently
// disarms a required resource; that would restore the exact silence this
// package exists to remove, invisibly.
func IsRequired(resource string) bool {
	switch strings.ToLower(strings.TrimSpace(os.Getenv(RequireEnv(resource)))) {
	case "", "0", "false":
		return false
	default:
		return true
	}
}

// SkipUnlessRequired is the only function in this package that may skip a
// test, and it may be called only for a genuine ABSENCE of `resource` — the
// tool is not on PATH, the data is not installed. An operational error (the
// resource was found but misbehaved) must never reach this function; call
// Fail for that instead. If the owning lane declared `resource` required via
// its RequireEnv variable, the same absence becomes a failure rather than a
// quiet skip, because a lane that promised the check ran cannot report green
// for one that never did.
func SkipUnlessRequired(t TestingT, resource, format string, args ...any) {
	t.Helper()

	reason := fmt.Sprintf(format, args...)
	if IsRequired(resource) {
		t.Fatalf("%s is set, so a skipped %s check is a failure: %s", RequireEnv(resource), resource, reason)
		return
	}
	t.Skipf("%s", reason)
}

// Fail always fails and never skips: call it once `resource` was found but
// did not behave — a non-zero exit, garbage output, a daemon that never
// answered. An operational error is a failure on every machine, whatever the
// owning lane declared, because "found but broken" is never a legitimate
// developer-host absence.
func Fail(t TestingT, format string, args ...any) {
	t.Helper()
	t.Fatalf(format, args...)
}

// RequireLookPath resolves `name` on PATH and applies SkipUnlessRequired's
// absence semantics on failure, returning the resolved path on success. It
// answers only "is the binary there", never "does it work" — pair it with a
// functional probe (ProbeShell below, for a shell) before trusting the
// binary it found.
func RequireLookPath(t TestingT, resource, name string) string {
	t.Helper()

	path, err := exec.LookPath(name)
	if err != nil {
		SkipUnlessRequired(t, resource, "%s is required: %v", name, err)
		return ""
	}
	return path
}

// ProbeShell checks that a shell resolved by RequireLookPath actually runs
// scripts the way the caller needs, by executing `probe` through it and
// comparing the trimmed output to `want`. Windows ships shells that answer a
// PATH lookup and then do the wrong thing once invoked — a WSL launcher stub
// with no distro installed, a store-advertisement stand-in for python3 — so a
// tool that does not run, or answers wrong, is an OPERATIONAL error and
// always fails, regardless of what the owning lane declared: the resource was
// found, so its absence never applies.
func ProbeShell(t TestingT, shellPath, probe, want string) {
	t.Helper()

	output, err := exec.Command(shellPath, "-c", probe).Output() // #nosec G204 -- shellPath comes from RequireLookPath's exec.LookPath, and probe is always a compile-time literal supplied by the caller, never external input.
	got := strings.TrimSpace(string(output))

	switch {
	case err != nil:
		Fail(t, "%s -c %q did not run: %v", shellPath, probe, err)
	case got != want:
		Fail(t, "%s -c %q answered %q, not %q", shellPath, probe, got, want)
	}
}

// tzdataResource names the one resource every zoneinfo-dependent regression
// in this tree gates on. Naming it once here, rather than at each of the
// timezone call sites, is what keeps OVUMCY_REQUIRE_TZDATA a single switch: a
// site that spelled its own resource string ("timezone", "zoneinfo", "tz")
// would silently split off a required-lane declaration armed against a
// different name than what its own probe reads.
const tzdataResource = "tzdata"

// RequireTimeZone loads an IANA zone via time.LoadLocation and applies
// SkipUnlessRequired's absence semantics under the "tzdata" resource on
// failure — the fail-closed switch for it is OVUMCY_REQUIRE_TZDATA. Every
// timezone-dependent regression in internal/api, internal/services and
// internal/reminders calls this instead of handling time.LoadLocation's own
// error, which is what lets one env var arm all of them together rather than
// however many call sites happened to write their own t.Skipf.
func RequireTimeZone(t TestingT, name string) *time.Location {
	t.Helper()

	location, err := time.LoadLocation(name)
	if err != nil {
		SkipUnlessRequired(t, tzdataResource, "zoneinfo for %s unavailable: %v", name, err)
		return nil
	}
	return location
}
