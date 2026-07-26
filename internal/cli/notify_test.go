package cli

import (
	"bytes"
	"context"
	"errors"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
	"time"

	"github.com/ovumcy/ovumcy-web/internal/db"
	"github.com/ovumcy/ovumcy-web/internal/services"
)

// stubNotifyRunner records the dryRun flag it was asked to run with and returns
// a canned report/error, so the CLI command can be tested without a DB or a
// socket.
type stubNotifyRunner struct {
	report    services.NotifyReport
	err       error
	gotDryRun bool
	called    bool
}

func (stub *stubNotifyRunner) RunOnce(_ context.Context, _ time.Time, _ *time.Location, dryRun bool) (services.NotifyReport, error) {
	stub.called = true
	stub.gotDryRun = dryRun
	report := stub.report
	report.DryRun = dryRun
	return report, stub.err
}

func TestParseNotifyArgs(t *testing.T) {
	cases := []struct {
		name    string
		args    []string
		wantErr bool
		check   func(notifyOptions) bool
	}{
		{name: "no args", args: nil, check: func(o notifyOptions) bool {
			return !o.dryRun && !o.failOnDeliveryError && !o.showHealthDetails
		}},
		{name: "dry run", args: []string{"--dry-run"}, check: func(o notifyOptions) bool { return o.dryRun && !o.showHealthDetails }},
		{name: "fail on delivery error", args: []string{"--fail-on-delivery-error"}, check: func(o notifyOptions) bool { return o.failOnDeliveryError }},
		{name: "show health details", args: []string{"--show-health-details"}, check: func(o notifyOptions) bool { return o.showHealthDetails }},
		{name: "dry run with health details", args: []string{"--dry-run", "--show-health-details"}, check: func(o notifyOptions) bool {
			return o.dryRun && o.showHealthDetails
		}},
		{name: "both flags", args: []string{"--dry-run", "--fail-on-delivery-error"}, check: func(o notifyOptions) bool { return o.dryRun && o.failOnDeliveryError }},
		{name: "blank ignored", args: []string{"", "--dry-run"}, check: func(o notifyOptions) bool { return o.dryRun }},
		{name: "unknown flag", args: []string{"--nope"}, wantErr: true},
		{name: "positional rejected", args: []string{"someowner"}, wantErr: true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			opts, err := parseNotifyArgs(tc.args)
			if tc.wantErr {
				if err == nil {
					t.Fatal("expected error, got nil")
				}
				if !strings.Contains(err.Error(), "usage: ovumcy notify") {
					t.Fatalf("expected usage string, got %q", err.Error())
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if !tc.check(opts) {
				t.Fatalf("options did not match expectation: %+v", opts)
			}
		})
	}
}

// TestRunNotifyCommandDryRunPropagates proves the CLI passes --dry-run through to
// the service (which is where "no outbound request / no watermark" is enforced,
// covered in the service tests) and renders the preview block for it.
func TestRunNotifyCommandDryRunPropagates(t *testing.T) {
	runner := &stubNotifyRunner{report: services.NotifyReport{
		OwnersScanned: 3,
		Due:           1,
		DryRunPreview: []services.NotifyPreviewLine{
			{OwnerID: 1, Type: "period-soon", EventDate: "2026-03-14", Host: "ntfy.example.io"},
		},
	}}
	var out bytes.Buffer
	err := runNotifyCommand(runner, notifyOptions{dryRun: true}, time.Now(), time.UTC, &out)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !runner.called {
		t.Fatal("service was never invoked")
	}
	if !runner.gotDryRun {
		t.Fatal("dry-run flag was not propagated to the service")
	}
	text := out.String()
	if !strings.Contains(text, "dry-run") {
		t.Fatalf("dry-run output should note the mode, got: %q", text)
	}
	// The default preview names the owner, the count, and the destination host.
	for _, want := range []string{"would send", "owner 1", "1 due", "ntfy.example.io"} {
		if !strings.Contains(text, want) {
			t.Fatalf("dry-run preview missing %q: %q", want, text)
		}
	}
	// It must NOT print a scheme/URL — host only.
	if strings.Contains(text, "http://") || strings.Contains(text, "https://") {
		t.Fatalf("dry-run preview leaked a URL scheme: %q", text)
	}
}

// notifyEventDatePattern matches any ISO calendar date the preview could print.
// The assertions below use the pattern rather than the literal fixture date, so
// the guard still fires if the printed date format changes.
var notifyEventDatePattern = regexp.MustCompile(`\d{4}-\d{2}-\d{2}`)

// notifyReminderTypes are the two reminder-type tokens the decision layer emits.
// Either one identifies which prediction is due for the named owner, so neither
// may appear in the default output.
var notifyReminderTypes = []string{"period-soon", "ovulation-soon"}

// multiOwnerDryRunReport is the shared dry-run fixture: two owners, one of them
// with both reminder kinds due against the same endpoint, so the default preview
// has to aggregate and the opt-in preview has to enumerate.
func multiOwnerDryRunReport() services.NotifyReport {
	return services.NotifyReport{
		OwnersScanned: 4,
		Due:           3,
		DryRunPreview: []services.NotifyPreviewLine{
			{OwnerID: 7, Type: "period-soon", EventDate: "2026-08-03", Host: "ntfy.example"},
			{OwnerID: 7, Type: "ovulation-soon", EventDate: "2026-08-17", Host: "ntfy.example"},
			{OwnerID: 9, Type: "period-soon", EventDate: "2026-08-05", Host: "gotify.example"},
		},
	}
}

// TestRunNotifyCommandDryRunOmitsHealthSpecificsByDefault is the headline privacy
// assertion for the dry run: without --show-health-details the preview names the
// owner, how many reminders are pending, and the destination host — and never the
// reminder type or the estimated date, which together are a prediction about an
// identified owner's cycle. The positive half of the assertion (the aggregated
// line is really rendered, for both owners, with the multi-reminder owner counted
// as 2) keeps this from passing merely because the preview block went missing.
func TestRunNotifyCommandDryRunOmitsHealthSpecificsByDefault(t *testing.T) {
	runner := &stubNotifyRunner{report: multiOwnerDryRunReport()}
	var out bytes.Buffer
	if err := runNotifyCommand(runner, notifyOptions{dryRun: true}, time.Now(), time.UTC, &out); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	text := out.String()

	// The two health specifics must be absent. Checked first so a regression is
	// reported as the leak it is, not as a formatting mismatch further down.
	if match := notifyEventDatePattern.FindString(text); match != "" {
		t.Fatalf("default dry-run output printed an estimated event date %q — a prediction about an identified owner: %q", match, text)
	}
	for _, reminderType := range notifyReminderTypes {
		if strings.Contains(text, reminderType) {
			t.Fatalf("default dry-run output printed the reminder type %q — which prediction is due for an identified owner: %q", reminderType, text)
		}
	}

	// Positive anchor: the preview block really rendered, aggregated per owner
	// endpoint, so the assertions above cannot pass on a missing preview.
	for _, want := range []string{"would send", "owner 7: 2 due -> host ntfy.example", "owner 9: 1 due -> host gotify.example"} {
		if !strings.Contains(text, want) {
			t.Fatalf("default dry-run preview missing %q: %q", want, text)
		}
	}
}

// TestRunNotifyCommandDryRunShowsHealthSpecificsOnRequest proves the opt-in half:
// with --show-health-details the operator gets the per-reminder type and
// estimated date back, one line per reminder, still host-only for the
// destination.
func TestRunNotifyCommandDryRunShowsHealthSpecificsOnRequest(t *testing.T) {
	runner := &stubNotifyRunner{report: multiOwnerDryRunReport()}
	var out bytes.Buffer
	if err := runNotifyCommand(runner, notifyOptions{dryRun: true, showHealthDetails: true}, time.Now(), time.UTC, &out); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	text := out.String()

	for _, want := range []string{
		"owner 7: period-soon on 2026-08-03 -> host ntfy.example",
		"owner 7: ovulation-soon on 2026-08-17 -> host ntfy.example",
		"owner 9: period-soon on 2026-08-05 -> host gotify.example",
	} {
		if !strings.Contains(text, want) {
			t.Fatalf("opt-in dry-run preview missing %q: %q", want, text)
		}
	}
	if !notifyEventDatePattern.MatchString(text) {
		t.Fatalf("opt-in dry-run preview should print estimated dates: %q", text)
	}
	// Even on the opt-in path the destination stays a host — never a URL.
	if strings.Contains(text, "http://") || strings.Contains(text, "https://") {
		t.Fatalf("opt-in dry-run preview leaked a URL scheme: %q", text)
	}
}

// TestRunNotifyCommandDeliveryPassIgnoresHealthDetailsFlag proves the opt-in is
// scoped to the dry run: a real delivery pass produces no preview, so the flag
// cannot turn one on and the pass output stays counts-only.
func TestRunNotifyCommandDeliveryPassIgnoresHealthDetailsFlag(t *testing.T) {
	runner := &stubNotifyRunner{report: services.NotifyReport{OwnersScanned: 2, Due: 1, Sent: 1}}
	var out bytes.Buffer
	if err := runNotifyCommand(runner, notifyOptions{showHealthDetails: true}, time.Now(), time.UTC, &out); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	text := out.String()
	if !strings.Contains(text, "reminders due") {
		t.Fatalf("delivery pass should still print the counts: %q", text)
	}
	if strings.Contains(text, "would send") {
		t.Fatalf("a delivery pass must not print a would-send preview: %q", text)
	}
	if match := notifyEventDatePattern.FindString(text); match != "" {
		t.Fatalf("delivery pass output printed an estimated date %q: %q", match, text)
	}
}

// TestRunNotifyCommandNilOutputDefaultsToStdout proves the nil-output guard:
// runNotifyCommand tolerates a nil writer (falling back to os.Stdout) instead of
// panicking. The stub runner keeps it DB- and socket-free.
func TestRunNotifyCommandNilOutputDefaultsToStdout(t *testing.T) {
	runner := &stubNotifyRunner{report: services.NotifyReport{OwnersScanned: 1}}
	if err := runNotifyCommand(runner, notifyOptions{}, time.Now(), time.UTC, nil); err != nil {
		t.Fatalf("nil output should default to stdout, got %v", err)
	}
	if !runner.called {
		t.Fatal("service was never invoked")
	}
}

// TestRunNotifyCommandZeroDueExitsZero proves the exit-code contract: a completed
// pass with nothing due returns nil (exit 0).
func TestRunNotifyCommandZeroDueExitsZero(t *testing.T) {
	runner := &stubNotifyRunner{report: services.NotifyReport{OwnersScanned: 5, Due: 0}}
	var out bytes.Buffer
	if err := runNotifyCommand(runner, notifyOptions{}, time.Now(), time.UTC, &out); err != nil {
		t.Fatalf("zero-due pass should exit 0, got %v", err)
	}
}

// TestRunNotifyCommandPassLevelErrorPropagates proves a pass-level failure
// (DB/config) becomes a non-nil error (exit non-zero).
func TestRunNotifyCommandPassLevelErrorPropagates(t *testing.T) {
	runner := &stubNotifyRunner{err: errors.New("db down")}
	var out bytes.Buffer
	err := runNotifyCommand(runner, notifyOptions{}, time.Now(), time.UTC, &out)
	if err == nil {
		t.Fatal("expected a non-nil error for a pass-level failure")
	}
}

// TestRunNotifyCommandDeliveryErrorExitCode proves the --fail-on-delivery-error
// contract: with the flag, a delivery failure yields a non-zero exit; without
// it, the same failure still exits 0 (a single unreachable endpoint is a
// transient, not a pass failure).
func TestRunNotifyCommandDeliveryErrorExitCode(t *testing.T) {
	base := services.NotifyReport{OwnersScanned: 2, Due: 2, Sent: 1, Failed: 1, OwnerIDsFailed: []uint{2}}

	// Default: delivery failure does not fail the command.
	runnerDefault := &stubNotifyRunner{report: base}
	var out1 bytes.Buffer
	if err := runNotifyCommand(runnerDefault, notifyOptions{}, time.Now(), time.UTC, &out1); err != nil {
		t.Fatalf("without --fail-on-delivery-error a delivery failure must exit 0, got %v", err)
	}

	// With the flag: delivery failure fails the command.
	runnerStrict := &stubNotifyRunner{report: base}
	var out2 bytes.Buffer
	err := runNotifyCommand(runnerStrict, notifyOptions{failOnDeliveryError: true}, time.Now(), time.UTC, &out2)
	if err == nil {
		t.Fatal("with --fail-on-delivery-error a delivery failure must exit non-zero")
	}
}

// TestRunNotifyCommandReportPrintsCountsOnly proves the printed report carries
// counts and owner ids but NEVER a URL, token, or health specific.
func TestRunNotifyCommandReportPrintsCountsOnly(t *testing.T) {
	runner := &stubNotifyRunner{report: services.NotifyReport{
		OwnersScanned:     4,
		Due:               3,
		Sent:              2,
		SkippedIdempotent: 1,
		Failed:            1,
		OwnerIDsFailed:    []uint{7},
	}}
	var out bytes.Buffer
	if err := runNotifyCommand(runner, notifyOptions{}, time.Now(), time.UTC, &out); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	text := out.String()
	for _, want := range []string{"owners scanned", "reminders due", "delivered", "already sent", "failed", "owners with failures"} {
		if !strings.Contains(text, want) {
			t.Fatalf("report missing %q line: %q", want, text)
		}
	}
	if !strings.Contains(text, "7") {
		t.Fatalf("failed owner id should be printed, got: %q", text)
	}
	// The report struct has no URL/token field, so this is a belt-and-suspenders
	// scan against obvious secret shapes leaking into the print format.
	for _, banned := range []string{"http://", "https://", "token", "webhook_url", "SECRET"} {
		if strings.Contains(text, banned) {
			t.Fatalf("report leaked a secret-shaped substring %q: %q", banned, text)
		}
	}
}

// testNotifySecretKey is a syntactically valid SECRET_KEY (>= 32 chars) for the
// wiring tests. It only constructs the decrypt service; with zero owners it is
// never used to open a ciphertext, so its value is inert here.
const testNotifySecretKey = "0123456789abcdef0123456789abcdef"

// TestRunNotifyCommandEndToEndZeroOwners drives the real RunNotifyCommand wiring
// against a fresh migrated SQLite DB with no owners: it opens the DB, builds the
// notify service via bootstrap.BuildNotifyService, runs one pass, and returns nil
// with a zero-count report. This covers the DB-open + service-build path that the
// stub-based command tests deliberately bypass.
func TestRunNotifyCommandEndToEndZeroOwners(t *testing.T) {
	databasePath := filepath.Join(t.TempDir(), "cli-notify-test.db")
	config := db.Config{Driver: db.DriverSQLite, SQLitePath: databasePath}

	err := RunNotifyCommand(config, testNotifySecretKey, "en", time.UTC, false, nil)
	if err != nil {
		t.Fatalf("zero-owner notify pass should succeed, got %v", err)
	}
}

// TestRunNotifyCommandEndToEndDryRun proves the same real wiring honors --dry-run
// (no request, no watermark — enforced in the service tests) and completes.
func TestRunNotifyCommandEndToEndDryRun(t *testing.T) {
	databasePath := filepath.Join(t.TempDir(), "cli-notify-dry.db")
	config := db.Config{Driver: db.DriverSQLite, SQLitePath: databasePath}

	if err := RunNotifyCommand(config, testNotifySecretKey, "en", time.UTC, false, []string{"--dry-run"}); err != nil {
		t.Fatalf("dry-run notify pass should succeed, got %v", err)
	}
}

// TestRunNotifyCommandRejectsBadArgs proves argument validation happens before
// any DB work: an unknown flag returns the usage error and never opens the DB.
func TestRunNotifyCommandRejectsBadArgs(t *testing.T) {
	config := db.Config{Driver: db.DriverSQLite, SQLitePath: filepath.Join(t.TempDir(), "unused.db")}

	err := RunNotifyCommand(config, testNotifySecretKey, "en", time.UTC, false, []string{"--nope"})
	if err == nil || !strings.Contains(err.Error(), "usage: ovumcy notify") {
		t.Fatalf("expected usage error for a bad flag, got %v", err)
	}
}

// TestRunNotifyCommandReportsDatabaseInitFailure proves a bad DB config surfaces
// as a pass-level error (the "database init failed" branch), not a panic.
func TestRunNotifyCommandReportsDatabaseInitFailure(t *testing.T) {
	// An empty SQLite path fails db.Config validation inside OpenDatabase.
	config := db.Config{Driver: db.DriverSQLite, SQLitePath: ""}

	err := RunNotifyCommand(config, testNotifySecretKey, "en", time.UTC, false, nil)
	if err == nil {
		t.Fatal("expected a database init failure, got nil")
	}
}
