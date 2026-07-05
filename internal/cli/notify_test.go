package cli

import (
	"bytes"
	"context"
	"errors"
	"strings"
	"testing"
	"time"

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
		{name: "no args", args: nil, check: func(o notifyOptions) bool { return !o.dryRun && !o.failOnDeliveryError }},
		{name: "dry run", args: []string{"--dry-run"}, check: func(o notifyOptions) bool { return o.dryRun }},
		{name: "fail on delivery error", args: []string{"--fail-on-delivery-error"}, check: func(o notifyOptions) bool { return o.failOnDeliveryError }},
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
// covered in the service tests).
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
	// The preview must print type + date + host.
	for _, want := range []string{"would send", "period-soon", "2026-03-14", "ntfy.example.io"} {
		if !strings.Contains(text, want) {
			t.Fatalf("dry-run preview missing %q: %q", want, text)
		}
	}
	// It must NOT print a scheme/URL — host only.
	if strings.Contains(text, "http://") || strings.Contains(text, "https://") {
		t.Fatalf("dry-run preview leaked a URL scheme: %q", text)
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
