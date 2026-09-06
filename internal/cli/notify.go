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
const notifyUsage = "usage: ovumcy notify [--dry-run] [--show-health-details] [--fail-on-delivery-error]"

// notifyOptions holds the parsed notify flags.
type notifyOptions struct {
	// dryRun computes and prints what WOULD be sent, making no outbound request
	// and writing no watermark.
	dryRun bool
	// showHealthDetails opts the dry-run preview into the per-reminder health
	// specifics (reminder type + estimated date). Off by default: those specifics
	// describe an identified owner's predicted cycle, so an operator has to ask
	// for them consciously rather than capture them by accident in a log or a
	// cron mailer. It has no effect outside --dry-run — a delivery pass produces
	// no preview at all.
	showHealthDetails bool
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
		// codecov:ignore -- defensive: (*gorm.DB).DB() only errors when the
		// underlying connection pool is unavailable, which cannot happen on the
		// handle OpenDatabase just returned successfully. Mirrors the same guard in
		// users.go; kept so a future driver change fails cleanly instead of panicking.
		return fmt.Errorf("database init failed: %w", err)
	}
	defer func() {
		_ = sqlDB.Close()
	}()

	i18nManager, err := i18n.NewManager(defaultLanguage)
	if err != nil {
		// codecov:ignore -- unreachable from the CLI: NewManager only errors on
		// corrupt/missing EMBEDDED locales or an absent required locale; the "en"
		// bundle is embedded and always present, and an unknown defaultLanguage is
		// normalized (never errors). Kept as a fail-safe against a build that ships
		// a broken embedded locale set.
		return fmt.Errorf("i18n init failed: %w", err)
	}

	repositories, _ := buildRepositories(database, calendarFeedFencePath())
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

	printNotifyReport(output, report, opts.showHealthDetails)

	if opts.failOnDeliveryError && report.Failed > 0 {
		return fmt.Errorf("%d webhook deliveries failed", report.Failed)
	}
	return nil
}

// printNotifyReport writes the aggregate counts. By default it prints ONLY
// counts, owner ids, and destination hosts — never a URL, token, reminder type,
// estimated date, or payload — so the default notify output is safe in an
// operator log or install script.
//
// showHealthDetails opts the dry-run preview into the per-reminder specifics
// (type + estimated date). Those ARE health data about an identified owner, so
// that output belongs in the same protection class as the database, never a
// shared log or a cron mailer.
func printNotifyReport(output io.Writer, report services.NotifyReport, showHealthDetails bool) {
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

	// On a dry run, print what WOULD be sent. The destination is a HOST at most
	// (never the URL or token) in either form.
	if report.DryRun && len(report.DryRunPreview) > 0 {
		printNotifyDryRunPreview(output, report.DryRunPreview, showHealthDetails)
	}
}

// printNotifyDryRunPreview renders the "would send" block of a dry run.
//
// By default it prints one line per owner endpoint: how many reminders would go
// out and to which HOST — enough to verify a schedule or a fresh deployment
// (which owners are armed, how many reminders are pending, where they point)
// without recording what any owner's cycle is predicted to do. With
// showHealthDetails the operator has explicitly asked for the specifics and gets
// one line per reminder, carrying its type and estimated date. Neither form ever
// prints the URL, its path or query, or a token.
func printNotifyDryRunPreview(output io.Writer, preview []services.NotifyPreviewLine, showHealthDetails bool) {
	if showHealthDetails {
		_, _ = fmt.Fprintln(output, "  would send (health details shown on request):")
		for _, line := range preview {
			_, _ = fmt.Fprintf(output, "    owner %d: %s on %s -> host %s\n", line.OwnerID, line.Type, line.EventDate, line.Host)
		}
		return
	}

	_, _ = fmt.Fprintln(output, "  would send:")
	for _, entry := range summarizeNotifyPreview(preview) {
		_, _ = fmt.Fprintf(output, "    owner %d: %d due -> host %s\n", entry.ownerID, entry.count, entry.host)
	}
	_, _ = fmt.Fprintln(output, "  (reminder types and estimated dates omitted; pass --show-health-details to include them)")
}

// notifyPreviewSummary is one aggregated dry-run preview row: how many reminders
// would reach one owner's destination host, carrying no per-reminder specific.
type notifyPreviewSummary struct {
	ownerID uint
	host    string
	count   int
}

// summarizeNotifyPreview collapses the per-reminder preview into one row per
// owner endpoint, preserving first-seen order so the output is deterministic. It
// deliberately drops the reminder type and the estimated date — the two health
// specifics — keeping only what the default preview is allowed to state.
func summarizeNotifyPreview(preview []services.NotifyPreviewLine) []notifyPreviewSummary {
	type endpoint struct {
		ownerID uint
		host    string
	}

	summaries := make([]notifyPreviewSummary, 0, len(preview))
	positions := make(map[endpoint]int, len(preview))
	for _, line := range preview {
		key := endpoint{ownerID: line.OwnerID, host: line.Host}
		if position, seen := positions[key]; seen {
			summaries[position].count++
			continue
		}
		positions[key] = len(summaries)
		summaries = append(summaries, notifyPreviewSummary{ownerID: line.OwnerID, host: line.Host, count: 1})
	}
	return summaries
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
		case "--show-health-details":
			opts.showHealthDetails = true
		case "--fail-on-delivery-error":
			opts.failOnDeliveryError = true
		default:
			return notifyOptions{}, errors.New(notifyUsage)
		}
	}
	return opts, nil
}
