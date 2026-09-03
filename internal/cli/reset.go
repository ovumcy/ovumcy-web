package cli

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"net/mail"
	"os"
	"strconv"
	"strings"

	"github.com/ovumcy/ovumcy-web/internal/db"
	"github.com/ovumcy/ovumcy-web/internal/services"
)

// resetUsage matches the addressing convention `users delete` already
// established: a bare address or `--id <id>`, mutually exclusive, exactly one
// required. reset-password gained the id form for the same reason that
// command has it — a legacy row a strict NormalizeAuthEmail rule refuses, or
// one sharing a mailbox with another account, cannot be reached by any
// address-taking command at all, and its bare address can silently reach the
// OTHER account on that mailbox.
//
// Named without "password" deliberately: gosec's G101 (hardcoded-credentials)
// rule flags a ValueSpec purely on the identifier matching its
// `passwd|pass|password|...` name pattern, regardless of what the string
// value actually holds — a usage line naming this command is not a
// credential, and the fix is to stop the name from looking like one rather
// than to suppress a real finding.
const resetUsage = "usage: ovumcy reset-password <email>|--id <id>"

type resetPasswordOptions struct {
	email  string
	userID uint
}

// parseResetPasswordArgs mirrors parseUsersDeleteArgs exactly (same flag
// spelling, same precedence, same ambiguity and error wording) rather than
// inventing a third addressing style for this command.
func parseResetPasswordArgs(args []string) (resetPasswordOptions, error) {
	opts := resetPasswordOptions{}
	for index := 0; index < len(args); index++ {
		value := strings.TrimSpace(args[index])
		switch {
		case value == "":
			continue
		case isUsersIDFlag(value):
			userID, consumed, err := parseUsersIDFlag(args, index, resetUsage)
			if err != nil {
				return resetPasswordOptions{}, err
			}
			if opts.userID != 0 {
				return resetPasswordOptions{}, errors.New(resetUsage)
			}
			opts.userID = userID
			index += consumed
		case strings.HasPrefix(value, "--"):
			return resetPasswordOptions{}, errors.New(resetUsage)
		default:
			if opts.email != "" {
				return resetPasswordOptions{}, errors.New(resetUsage)
			}
			opts.email = value
		}
	}

	if (opts.email == "") == (opts.userID == 0) {
		return resetPasswordOptions{}, errors.New(resetUsage)
	}
	return opts, nil
}

func RunResetPasswordCommand(databaseConfig db.Config, args []string) error {
	return runResetPasswordCommand(databaseConfig, args, resetPasswordReader(os.Stdin), os.Stdout)
}

// resetPasswordReader picks how the new password is obtained, matching what
// `users create` already does: an interactive terminal gets the twice-typed,
// echo-disabled prompt, and piped or redirected stdin is read as the password's
// first line.
//
// The non-interactive path is what makes the documented recovery step runnable
// without a terminal. `ovumcy reset-password` is the operator's way back in for
// an owner locked out by a SECRET_KEY rotation or an OIDC-only account with no
// recovery code, and the runtime image is shell-free, so the only way to reach
// it is `docker exec`. Prompting unconditionally meant the plain `docker exec`
// form the runbook shows for the other subcommands failed with "secure password
// prompt requires an interactive terminal", and recovering several accounts
// could not be scripted at all. The password still never travels in argv or the
// environment.
func resetPasswordReader(input *os.File) passwordPromptFunc {
	return func() ([]byte, error) {
		if input == nil {
			// A typed nil *os.File would satisfy the io.Reader interface and
			// reach bufio, so refuse it here rather than one frame later.
			return nil, errors.New("password input is required")
		}
		// codecov:ignore:start -- interactive TTY prompt; the terminal branch needs a real terminal and is exercised only interactively
		if stdinIsTerminal(input) {
			return promptNewPassword()
		}
		// codecov:ignore:end
		return readPasswordLine(input)
	}
}

type passwordPromptFunc func() ([]byte, error)

func runResetPasswordCommand(databaseConfig db.Config, args []string, prompt passwordPromptFunc, output io.Writer) error {
	opts, err := parseResetPasswordArgs(args)
	if err != nil {
		return err
	}

	// parseResetPasswordArgs already guarantees opts.email is non-blank
	// whenever opts.userID is zero (a blank positional argument is skipped
	// during parsing, same as parseUsersDeleteArgs), so there is no separate
	// "email is required" case left to check here — only whether the
	// non-blank value is a valid address.
	normalizedEmail := ""
	if opts.userID == 0 {
		normalizedEmail = strings.ToLower(strings.TrimSpace(opts.email))
		if _, err := mail.ParseAddress(normalizedEmail); err != nil {
			return fmt.Errorf("invalid email address: %w", err)
		}
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

	if prompt == nil {
		return errors.New("password prompt is required")
	}

	newPassword, err := prompt()
	if err != nil {
		return fmt.Errorf("read new password: %w", err)
	}
	defer clear(newPassword)
	if len(newPassword) == 0 {
		return errors.New("password is required")
	}

	repositories := buildRepositories(database)
	authService := services.NewAuthService(repositories.Users)

	var resetErr error
	if opts.userID != 0 {
		resetErr = authService.ForceResetPasswordByID(context.Background(), opts.userID, string(newPassword))
	} else {
		resetErr = authService.ForceResetPasswordByEmail(context.Background(), normalizedEmail, string(newPassword))
	}
	if resetErr != nil {
		return mapResetPasswordError(resetErr, opts, normalizedEmail)
	}

	// Raised only once the reset has actually happened: a forced reset
	// force-clears the owner's calendar feed, and until this point the command
	// could still have exited on an unknown email or a rejected password,
	// having changed no feed state at all. A warning printed on those runs is
	// one an operator learns to skip past on the run that matters.
	warnAboutAnUnreachableCalendarFeedFence(os.Stderr)

	if output == nil {
		output = os.Stdout
	}
	_, _ = fmt.Fprintln(output, "✅ Password reset successful")
	_, _ = fmt.Fprintln(output, "Existing auth sessions were invalidated.")
	_, _ = fmt.Fprintln(output, "User must sign in again and reset the password before continuing.")

	return nil
}

// mapResetPasswordError translates the service's sentinels into the
// operator-facing wording. The id and email arms of ErrAuthUserNotFound say
// different things because they were addressed differently: an unknown id
// points the operator at `users list`, an unknown email might just be a typo
// of an address that was never an account. The ambiguous-address case is
// finding DB-2's fix: services.AmbiguousEmailError (shared with
// OperatorUserService.GetUserByEmail — `users delete <email>` — and
// WebhookSettingsCLIService.resolveOwner — `webhook show|set`, which has no
// --id form at all) is never resolved silently to one match; this command's
// own mapping adds the explicit "retry with --id" pointer the other two
// leave to services.AmbiguousEmailError.Error()'s bare id list.
func mapResetPasswordError(err error, opts resetPasswordOptions, normalizedEmail string) error {
	var ambiguous *services.AmbiguousEmailError
	if errors.As(err, &ambiguous) {
		return fmt.Errorf(
			"email %s matches %d accounts (ids %s); retry with --id (see ovumcy users list)",
			ambiguous.Email, len(ambiguous.IDs), formatUserIDs(ambiguous.IDs),
		)
	}

	switch {
	case errors.Is(err, services.ErrAuthUserNotFound):
		if opts.userID != 0 {
			return fmt.Errorf("no account carries id %d (see ovumcy users list)", opts.userID)
		}
		return fmt.Errorf("user %s not found", normalizedEmail)
	case errors.Is(err, services.ErrAuthUserIDRequired):
		return errors.New("an account id is required (see ovumcy users list)")
	case errors.Is(err, services.ErrAuthResetInvalid):
		return errors.New("password is required")
	case errors.Is(err, services.ErrAuthPasswordTooLong):
		// Without this arm the split would surface here as the raw sentinel
		// text under the default branch. The operator terminal counts bytes
		// happily, so unlike the owner-facing copy this one says so.
		return errors.New("password is longer than 72 bytes (bcrypt's input limit); note that non-ASCII characters take more than one byte each")
	case errors.Is(err, services.ErrAuthWeakPassword):
		return errors.New("password does not meet strength requirements")
	default:
		return fmt.Errorf("reset password: %w", err)
	}
}

// formatUserIDs renders the ambiguous match list the same style `users list`
// prints them in: ascending, comma-separated.
func formatUserIDs(ids []uint) string {
	parts := make([]string, len(ids))
	for i, id := range ids {
		parts[i] = strconv.FormatUint(uint64(id), 10)
	}
	return strings.Join(parts, ", ")
}

func promptNewPassword() ([]byte, error) {
	password, err := readPasswordFromTerminal("Enter new password: ")
	if err != nil {
		return nil, err
	}
	defer clear(password)

	confirm, err := readPasswordFromTerminal("Confirm new password: ")
	if err != nil {
		return nil, err
	}
	defer clear(confirm)

	if len(bytes.TrimSpace(password)) == 0 || len(bytes.TrimSpace(confirm)) == 0 {
		return nil, errors.New("password is required")
	}
	if !bytes.Equal(password, confirm) {
		return nil, errors.New("password confirmation does not match")
	}

	result := make([]byte, len(password))
	copy(result, password)
	return result, nil
}

func readPasswordFromTerminal(prompt string) ([]byte, error) {
	if strings.TrimSpace(prompt) != "" {
		_, _ = fmt.Fprint(os.Stdout, prompt)
	}

	password, err := readPasswordNoEcho(os.Stdin)
	_, _ = fmt.Fprintln(os.Stdout)
	if err != nil {
		return nil, errors.New("secure password prompt requires an interactive terminal")
	}
	return password, nil
}
