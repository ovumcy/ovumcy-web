package cli

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"strconv"
	"strings"

	"github.com/ovumcy/ovumcy-web/internal/db"
	"github.com/ovumcy/ovumcy-web/internal/services"
)

// webhookURLEnv is the environment variable the operator may use to supply the
// webhook endpoint out-of-band. The endpoint is a SECRET (it can embed an
// ntfy/Gotify token), so it must never be passed as an argv argument — argv
// leaks into shell history, `ps`, and process listings. It is read either from
// this env var or interactively from stdin (--url-stdin); it is never printed
// back, only its host.
const webhookURLEnv = "OVUMCY_WEBHOOK_URL"

// webhookUsage is the single usage string returned for every argument error so
// the contract is stable and testable. It is a single line without trailing
// punctuation (staticcheck ST1005), mirroring notifyUsage. The endpoint URL is
// a secret and is NEVER an argv argument: it is supplied via the
// OVUMCY_WEBHOOK_URL environment variable or --url-stdin (see the flag reference
// in the RunWebhookCommand doc comment).
const webhookUsage = "usage: ovumcy webhook <show|set> <email> " +
	"[--enabled=<bool>] [--notify-period=<bool>] [--notify-ovulation=<bool>] " +
	"[--reminder-lead-days=<0-14>] [--url-stdin] [--clear-url] [--dry-run] " +
	"(URL via " + webhookURLEnv + " env or --url-stdin, never argv)"

// webhookSetOptions holds the parsed `webhook set` flags. The tri-state pointer
// fields distinguish "flag not given" (nil, leave as-is) from an explicit
// true/false, so a single flag changes exactly one setting.
type webhookSetOptions struct {
	email            string
	enabled          *bool
	notifyPeriod     *bool
	notifyOvulation  *bool
	reminderLeadDays *int
	readURLFromStdin bool
	clearURL         bool
	dryRun           bool
}

// RunWebhookCommand is the operator entry point for `ovumcy webhook`: a
// local-only command to configure an owner's webhook notification settings
// (issue #124), mirroring the users/notify subcommands. It resolves the DB and
// the decrypt/encrypt secretKey from the same configuration the web binary uses
// (the caller passes them, exactly like RunNotifyCommand) and delegates to
// runWebhookCommand with real stdin/stdout.
//
// Subcommands:
//
//	show <email>   print status: configured/not + the destination HOST only
//	               (never the full URL, path, query, or token) and the toggles.
//	set  <email>   configure settings. Flags:
//	                 --enabled=<true|false>          master delivery switch
//	                 --notify-period=<true|false>    upcoming-period reminders
//	                 --notify-ovulation=<true|false> upcoming-ovulation reminders
//	                 --reminder-lead-days=<0-14>     shared lead window (clamped)
//	                 --url-stdin                     read the endpoint from stdin
//	                 --clear-url                     remove any stored endpoint
//	                 --dry-run                       validate + print, write nothing
//
// SECURITY: the endpoint URL is a secret (it can embed an ntfy/Gotify token) and
// is NEVER accepted as an argv argument (argv leaks into shell history, `ps`, and
// process listings). Supply it out-of-band via the OVUMCY_WEBHOOK_URL environment
// variable or interactively via --url-stdin (a no-echo prompt on a TTY, or the
// first line of a piped stdin). The URL is never echoed back — only its host.
func RunWebhookCommand(databaseConfig db.Config, secretKey string, args []string) error {
	return runWebhookCommand(databaseConfig, secretKey, args, os.Stdin, os.Stdout)
}

// runWebhookCommand parses and dispatches the webhook subcommand. input/output
// are injected so tests drive the flow with buffers (no real terminal). It
// validates arguments before opening the DB so a bad invocation never touches
// persistence.
func runWebhookCommand(databaseConfig db.Config, secretKey string, args []string, input io.Reader, output io.Writer) error {
	if len(args) == 0 {
		return errors.New(webhookUsage)
	}

	subcommand := strings.ToLower(strings.TrimSpace(args[0]))
	switch subcommand {
	case "show":
		email, err := parseWebhookShowArgs(args[1:])
		if err != nil {
			return err
		}
		service, cleanup, err := openWebhookCLIService(databaseConfig, secretKey)
		if err != nil {
			return err
		}
		defer cleanup()
		return runWebhookShow(service, email, output)
	case "set":
		opts, err := parseWebhookSetArgs(args[1:])
		if err != nil {
			return err
		}
		// Resolve the endpoint URL (a secret) BEFORE opening the DB so an
		// interactive prompt is not wedged between DB-open and close, and so a
		// missing-secret error surfaces without side effects.
		patch, err := buildWebhookPatch(opts, input)
		if err != nil {
			return err
		}
		service, cleanup, err := openWebhookCLIService(databaseConfig, secretKey)
		if err != nil {
			return err
		}
		defer cleanup()
		return runWebhookSet(service, opts, patch, output)
	default:
		return errors.New(webhookUsage)
	}
}

// openWebhookCLIService opens the database and assembles the CLI webhook
// settings orchestration from the same repositories and secret the web path
// uses (services.NewWebhookSettingsService, mirroring bootstrap.BuildNotifyService's
// settings half). It returns a cleanup that closes the DB handle.
func openWebhookCLIService(databaseConfig db.Config, secretKey string) (*services.WebhookSettingsCLIService, func(), error) {
	database, err := db.OpenDatabase(databaseConfig)
	if err != nil {
		return nil, nil, fmt.Errorf("database init failed: %w", err)
	}
	sqlDB, err := database.DB()
	if err != nil {
		// codecov:ignore -- defensive: (*gorm.DB).DB() only errors when the pool
		// is unavailable, which cannot happen on the handle OpenDatabase just
		// returned. Mirrors the same guard in users.go/notify.go.
		return nil, nil, fmt.Errorf("database init failed: %w", err)
	}

	repositories := db.NewRepositories(database)
	settingsService := services.NewWebhookSettingsService(repositories.Users, []byte(secretKey))
	cliService := services.NewWebhookSettingsCLIService(repositories.Users, settingsService)
	cleanup := func() { _ = sqlDB.Close() }
	return cliService, cleanup, nil
}

// runWebhookShow prints the owner's webhook status: configured/not and the
// destination HOST only — never the full URL, path, query, or token.
func runWebhookShow(service *services.WebhookSettingsCLIService, email string, output io.Writer) error {
	view, err := service.ResolveWebhookSettings(context.Background(), email)
	if err != nil {
		return mapWebhookError(err, email)
	}
	if output == nil {
		output = os.Stdout
	}
	printWebhookStatus(output, email, view)
	return nil
}

// runWebhookSet applies the parsed patch (merged onto the current settings by
// the service) and prints a REDACTED confirmation — enabled state, toggles,
// lead days, and host only. On --dry-run nothing is written.
func runWebhookSet(service *services.WebhookSettingsCLIService, opts webhookSetOptions, patch services.WebhookSettingsPatch, output io.Writer) error {
	view, err := service.ApplyWebhookSettings(context.Background(), opts.email, patch, opts.dryRun)
	if err != nil {
		return mapWebhookError(err, opts.email)
	}
	if output == nil {
		output = os.Stdout
	}
	if opts.dryRun {
		_, _ = fmt.Fprintf(output, "[dry-run] no changes written. Would set webhook for %s:\n", opts.email)
	} else {
		_, _ = fmt.Fprintf(output, "webhook configured for %s:\n", opts.email)
	}
	printWebhookStatus(output, "", view)
	return nil
}

// printWebhookStatus writes the safe status view. It prints ONLY the toggle
// state, the lead window, and the destination HOST — never the URL/token — so
// the output is safe in an operator log or install script. When email is
// non-empty it is included as a leading line (the show path); the set path
// prints its own header first and passes an empty email here.
func printWebhookStatus(output io.Writer, email string, view services.WebhookSettingsView) {
	if strings.TrimSpace(email) != "" {
		_, _ = fmt.Fprintf(output, "webhook status for %s:\n", email)
	}
	_, _ = fmt.Fprintf(output, "  enabled:          %t\n", view.Enabled)
	_, _ = fmt.Fprintf(output, "  endpoint:         %s\n", webhookEndpointStatus(view))
	_, _ = fmt.Fprintf(output, "  notify period:    %t\n", view.NotifyPeriod)
	_, _ = fmt.Fprintf(output, "  notify ovulation: %t\n", view.NotifyOvulation)
	_, _ = fmt.Fprintf(output, "  reminder lead days: %d\n", view.ReminderLeadDays)
}

// webhookEndpointStatus renders the endpoint line: "not configured" when no
// endpoint is stored, otherwise "configured (host <host>)" with the HOST ONLY.
// It never renders the full URL or token.
func webhookEndpointStatus(view services.WebhookSettingsView) string {
	if !view.Configured {
		return "not configured"
	}
	host := strings.TrimSpace(view.Host)
	// codecov:ignore:start -- unreachable via any real flow: a stored endpoint
	// always passed ValidateWebhookURL (which requires a host), so the derived
	// host is never empty here. Kept as a fail-safe so a hostless value reports
	// "configured" without leaking rather than printing an empty host.
	if host == "" {
		return "configured"
	}
	// codecov:ignore:end
	return fmt.Sprintf("configured (host %s)", host)
}

// buildWebhookPatch turns the parsed set options into a service patch, reading
// the endpoint URL securely when the operator is setting one. The URL is read
// from the OVUMCY_WEBHOOK_URL env var or from stdin (--url-stdin) — NEVER argv.
// --clear-url takes precedence and needs no URL input.
func buildWebhookPatch(opts webhookSetOptions, input io.Reader) (services.WebhookSettingsPatch, error) {
	patch := services.WebhookSettingsPatch{
		Enabled:          opts.enabled,
		NotifyPeriod:     opts.notifyPeriod,
		NotifyOvulation:  opts.notifyOvulation,
		ReminderLeadDays: opts.reminderLeadDays,
	}

	if opts.clearURL {
		patch.ClearURL()
		return patch, nil
	}

	if envURL := strings.TrimSpace(os.Getenv(webhookURLEnv)); envURL != "" {
		patch.SetURL(envURL)
		return patch, nil
	}

	if opts.readURLFromStdin {
		rawURL, err := readWebhookURL(input)
		if err != nil {
			return services.WebhookSettingsPatch{}, err
		}
		patch.SetURL(string(rawURL))
		return patch, nil
	}

	// No URL source given: keep any existing endpoint (a flag-only edit). If the
	// operator is enabling delivery without a stored endpoint, the reused
	// SaveWebhookSettings path rejects it with ErrWebhookURLInvalid.
	return patch, nil
}

// readWebhookURL obtains the endpoint URL without exposing it in argv or the
// environment listing. On an interactive terminal it prompts with echo disabled
// (reusing the reset-password no-echo terminal read); when stdin is piped or
// redirected it reads the URL as the first line of stdin. It mirrors
// readCreatePassword's TTY-vs-pipe branch. The value is a secret and is never
// echoed.
func readWebhookURL(input io.Reader) ([]byte, error) {
	// codecov:ignore:start -- interactive TTY prompt; the terminal branch needs a real terminal and is exercised only interactively (mirrors readCreatePassword in users.go)
	if file, ok := input.(*os.File); ok && stdinIsTerminal(file) {
		raw, err := readPasswordFromTerminal("Enter webhook URL (input hidden): ")
		if err != nil {
			return nil, err
		}
		trimmed := strings.TrimSpace(string(raw))
		if trimmed == "" {
			return nil, errors.New("webhook url is required")
		}
		return []byte(trimmed), nil
	}
	// codecov:ignore:end
	return readWebhookURLLine(input)
}

// readWebhookURLLine reads the endpoint URL as the first line of a piped stdin.
// It never echoes the value.
func readWebhookURLLine(input io.Reader) ([]byte, error) {
	line, err := readPasswordLine(input)
	if err != nil {
		// readPasswordLine returns "password is required"/"password input is
		// required"; remap to a webhook-specific message without echoing the value.
		if errors.Is(err, io.EOF) || strings.Contains(err.Error(), "required") {
			return nil, errors.New("webhook url is required")
		}
		return nil, fmt.Errorf("read webhook url: %w", err)
	}
	return line, nil
}

// parseWebhookShowArgs parses `webhook show <email>`.
func parseWebhookShowArgs(args []string) (string, error) {
	email := ""
	for _, arg := range args {
		value := strings.TrimSpace(arg)
		switch {
		case value == "":
			continue
		case strings.HasPrefix(value, "--"):
			return "", errors.New(webhookUsage)
		default:
			if email != "" {
				return "", errors.New(webhookUsage)
			}
			email = value
		}
	}
	if email == "" {
		return "", errors.New(webhookUsage)
	}
	return email, nil
}

// parseWebhookSetArgs parses `webhook set <email> [flags]`. Boolean settings use
// an explicit --flag=<true|false> form (not bare presence) so the tri-state
// merge can leave an unspecified setting untouched. An unknown flag, a second
// positional, or a malformed value returns webhookUsage.
func parseWebhookSetArgs(args []string) (webhookSetOptions, error) {
	opts := webhookSetOptions{}
	for _, arg := range args {
		if err := applyWebhookSetArg(&opts, strings.TrimSpace(arg)); err != nil {
			return webhookSetOptions{}, err
		}
	}

	if opts.email == "" {
		return webhookSetOptions{}, errors.New(webhookUsage)
	}
	if opts.clearURL && opts.readURLFromStdin {
		return webhookSetOptions{}, errors.New("--clear-url and --url-stdin are mutually exclusive")
	}
	return opts, nil
}

// applyWebhookSetArg mutates opts for a single trimmed `webhook set` argument.
// An empty argument is a no-op; an unknown flag, a malformed value, or a second
// positional returns webhookUsage (or the flag parser's error).
func applyWebhookSetArg(opts *webhookSetOptions, value string) error {
	switch {
	case value == "":
		return nil
	case value == "--url-stdin":
		opts.readURLFromStdin = true
	case value == "--clear-url":
		opts.clearURL = true
	case value == "--dry-run":
		opts.dryRun = true
	case strings.HasPrefix(value, "--enabled="):
		parsed, err := parseWebhookBoolFlag(value)
		if err != nil {
			return err
		}
		opts.enabled = parsed
	case strings.HasPrefix(value, "--notify-period="):
		parsed, err := parseWebhookBoolFlag(value)
		if err != nil {
			return err
		}
		opts.notifyPeriod = parsed
	case strings.HasPrefix(value, "--notify-ovulation="):
		parsed, err := parseWebhookBoolFlag(value)
		if err != nil {
			return err
		}
		opts.notifyOvulation = parsed
	case strings.HasPrefix(value, "--reminder-lead-days="):
		parsed, err := parseWebhookIntFlag(value)
		if err != nil {
			return err
		}
		opts.reminderLeadDays = parsed
	case strings.HasPrefix(value, "--"):
		return errors.New(webhookUsage)
	default:
		if opts.email != "" {
			return errors.New(webhookUsage)
		}
		opts.email = value
	}
	return nil
}

// parseWebhookBoolFlag parses a --key=<true|false> flag value into a *bool. It
// accepts only strconv.ParseBool-recognized truthy/falsy tokens; anything else
// returns webhookUsage.
func parseWebhookBoolFlag(arg string) (*bool, error) {
	_, rawValue, _ := strings.Cut(arg, "=")
	parsed, err := strconv.ParseBool(strings.TrimSpace(rawValue))
	if err != nil {
		return nil, errors.New(webhookUsage)
	}
	return &parsed, nil
}

// parseWebhookIntFlag parses a --key=<int> flag value into an *int. Out-of-range
// values are NOT rejected here — the service clamps reminder_lead_days into
// [0,14] — but a non-numeric value returns webhookUsage.
func parseWebhookIntFlag(arg string) (*int, error) {
	_, rawValue, _ := strings.Cut(arg, "=")
	parsed, err := strconv.Atoi(strings.TrimSpace(rawValue))
	if err != nil {
		return nil, errors.New(webhookUsage)
	}
	return &parsed, nil
}

// mapWebhookError converts service errors into stable operator-facing messages.
// It never embeds a URL or token. The email is echoed only for not-found (the
// operator supplied it on the command line, so it is not a secret disclosure).
func mapWebhookError(err error, email string) error {
	switch {
	case errors.Is(err, services.ErrWebhookOwnerNotFound):
		return fmt.Errorf("owner %s not found", strings.ToLower(strings.TrimSpace(email)))
	case errors.Is(err, services.ErrOperatorUserEmailRequired):
		return errors.New("email is required")
	case errors.Is(err, services.ErrOperatorUserEmailInvalid):
		return errors.New("invalid email address")
	case errors.Is(err, services.ErrWebhookURLInvalid):
		return errors.New("webhook url invalid: must be an absolute http or https URL")
	default:
		return fmt.Errorf("configure webhook: %w", err)
	}
}
