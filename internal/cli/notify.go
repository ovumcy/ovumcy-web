package cli

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"strings"
	"time"

	"github.com/ovumcy/ovumcy-web/internal/bootstrap"
	"github.com/ovumcy/ovumcy-web/internal/db"
	"github.com/ovumcy/ovumcy-web/internal/i18n"
	"github.com/ovumcy/ovumcy-web/internal/services"
)

// notifyUsage is the single usage string for the notify command, returned for
// every argument error so the contract is stable and testable.
const notifyUsage = "usage: ovumcy notify [--dry-run] [--fail-on-delivery-error]"

// notifyOptions holds the parsed notify flags.
type notifyOptions struct {
	// dryRun computes and prints what WOULD be sent, making no outbound request
	// and writing no watermark.
	dryRun bool
	// failOnDeliveryError makes the command exit non-zero when any individual
	// delivery failed, even though the pass itself completed. Off by default: a
	// single unreachable owner endpoint is an expected transient, not a pass
	// failure.
	failOnDeliveryError bool
}

// notifyServiceRunner is the narrow behavior the command needs from the notify
// service, so the command can be tested with a stub that never opens a DB or a
// socket.
type notifyServiceRunner interface {
	RunOnce(ctx context.Context, now time.Time, location *time.Location, dryRun bool) (services.NotifyReport, error)
}

// RunNotifyCommand is the operator entry point for `ovumcy notify`: a
// request-free pass that delivers any due webhook reminders (issue #124). It is
// local-only, mirroring the users/healthcheck subcommands. It resolves its
// collaborators (DB, decrypt key, i18n disclaimer, egress gate) from the same
// configuration the web binary uses and delegates to runNotifyCommand.
//
// secretKey decrypts each owner's stored webhook URL; defaultLanguage localizes
// the mandatory disclaimer; location is the server timezone fallback for owners
// with no persisted zone; blockPrivateAddresses is the off-by-default egress
// gate.
func RunNotifyCommand(
	databaseConfig db.Config,
	secretKey string,
	defaultLanguage string,
	location *time.Location,
	blockPrivateAddresses bool,
	args []string,
) error {
	opts, err := parseNotifyArgs(args)
	if err != nil {
		return err
	}

	database, err := db.OpenDatabase(databaseConfig)
	if err != nil {
		return fmt.Errorf("database init failed: %w", err)
	}
	sqlDB, err := database.DB()
	if err != nil {
		return fmt.Errorf("database init failed: %w", err)
	}
	defer func() {
		_ = sqlDB.Close()
	}()

	i18nManager, err := i18n.NewManager(defaultLanguage)
	if err != nil {
		return fmt.Errorf("i18n init failed: %w", err)
	}

	repositories := db.NewRepositories(database)
	service := bootstrap.BuildNotifyService(repositories, []byte(secretKey), i18nManager, blockPrivateAddresses)

	return runNotifyCommand(service, opts, time.Now(), location, os.Stdout)
}

// runNotifyCommand runs one notify pass and prints a secret-free report. now and
// location are injected so tests drive the decision deterministically. It exits
// (returns nil) when the pass completes even with zero due reminders; it returns
// an error only on a pass-level failure (DB/config) or — when
// --fail-on-delivery-error is set — when at least one delivery failed.
func runNotifyCommand(
	service notifyServiceRunner,
	opts notifyOptions,
	now time.Time,
	location *time.Location,
	output io.Writer,
) error {
	if output == nil {
		output = os.Stdout
	}

	report, err := service.RunOnce(context.Background(), now, location, opts.dryRun)
	if err != nil {
		return fmt.Errorf("notify pass failed: %w", err)
	}

	printNotifyReport(output, report)

	if opts.failOnDeliveryError && report.Failed > 0 {
		return fmt.Errorf("%d webhook deliveries failed", report.Failed)
	}
	return nil
}

// printNotifyReport writes the aggregate counts. It prints ONLY counts and owner
// ids — never a URL, token, health specific, or payload — so the notify output
// is safe in an operator log or install script.
func printNotifyReport(output io.Writer, report services.NotifyReport) {
	mode := "delivery"
	if report.DryRun {
		mode = "dry-run (no request sent, no watermark written)"
	}
	_, _ = fmt.Fprintf(output, "Webhook notify pass complete (%s).\n", mode)
	_, _ = fmt.Fprintf(output, "  owners scanned:     %d\n", report.OwnersScanned)
	_, _ = fmt.Fprintf(output, "  reminders due:      %d\n", report.Due)
	_, _ = fmt.Fprintf(output, "  delivered:          %d\n", report.Sent)
	_, _ = fmt.Fprintf(output, "  skipped (already sent): %d\n", report.SkippedIdempotent)
	_, _ = fmt.Fprintf(output, "  failed:             %d\n", report.Failed)
	if len(report.OwnerIDsFailed) > 0 {
		_, _ = fmt.Fprintf(output, "  owners with failures: %s\n", formatOwnerIDs(report.OwnerIDsFailed))
	}

	// On a dry run, print what WOULD be sent: type + estimated date + destination
	// HOST only (never the URL or token).
	if report.DryRun && len(report.DryRunPreview) > 0 {
		_, _ = fmt.Fprintln(output, "  would send:")
		for _, line := range report.DryRunPreview {
			_, _ = fmt.Fprintf(output, "    owner %d: %s on %s -> host %s\n", line.OwnerID, line.Type, line.EventDate, line.Host)
		}
	}
}

// formatOwnerIDs renders a list of owner ids for the report (ids are not
// secrets). Comma-separated, in the order encountered.
func formatOwnerIDs(ids []uint) string {
	parts := make([]string, 0, len(ids))
	for _, id := range ids {
		parts = append(parts, fmt.Sprintf("%d", id))
	}
	return strings.Join(parts, ", ")
}

// parseNotifyArgs parses the notify flags, returning notifyUsage on any unknown
// argument or positional. The command takes no positional arguments.
func parseNotifyArgs(args []string) (notifyOptions, error) {
	opts := notifyOptions{}
	for _, arg := range args {
		value := strings.TrimSpace(arg)
		switch value {
		case "":
			continue
		case "--dry-run":
			opts.dryRun = true
		case "--fail-on-delivery-error":
			opts.failOnDeliveryError = true
		default:
			return notifyOptions{}, errors.New(notifyUsage)
		}
	}
	return opts, nil
}
