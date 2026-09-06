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
	"github.com/ovumcy/ovumcy-web/internal/models"
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

// resolve mirrors usersDeleteOptions.resolve exactly: the same two-address
// handles, resolved through the same OperatorUserService methods, so an
// ambiguous or unknown address is refused the same way for both commands.
func (opts resetPasswordOptions) resolve(service *services.OperatorUserService) (models.OperatorUserSummary, error) {
	if opts.userID != 0 {
		return service.GetUserByID(context.Background(), opts.userID)
	}
	return service.GetUserByEmail(context.Background(), opts.email)
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
	// Checked here, ahead of the fence gate below, for the same reason the
	// target is resolved ahead of it: a password ForceResetPasswordByID would
	// refuse anyway must not spend the gate's one-shot confirmation first.
	// ValidatePasswordStrength has no side effects, so running it twice — once
	// here and once more inside ForceResetPasswordByID a moment later — costs
	// nothing beyond the CPU of checking a string twice.
	if err := resetPasswordPolicyError(string(newPassword)); err != nil {
		return mapResetPasswordError(err, opts, normalizedEmail)
	}

	repositories, fence, fencePath := buildRepositories(database)
	authService := services.NewAuthService(repositories.Users)
	operatorUsers := services.NewOperatorUserService(repositories.Users, authService)

	target, err := opts.resolve(operatorUsers)
	if err != nil {
		return mapResetPasswordError(err, opts, normalizedEmail)
	}

	// A forced reset force-clears the owner's calendar feed, so this process
	// must confirm and advance the SAME fence the server does before the reset
	// is even attempted — never merely warn about it.
	if err := confirmOperatorFeedRevocation(context.Background(), fencePath, fence, os.Stderr); err != nil {
		return err
	}

	if resetErr := authService.ForceResetPasswordByID(context.Background(), target.ID, string(newPassword)); resetErr != nil {
		return mapResetPasswordError(resetErr, opts, normalizedEmail)
	}

	if output == nil {
		output = os.Stdout
	}
	_, _ = fmt.Fprintln(output, "✅ Password reset successful")
	_, _ = fmt.Fprintln(output, "Existing auth sessions were invalidated.")
	_, _ = fmt.Fprintln(output, "User must sign in again and reset the password before continuing.")

	return nil
}

// mapResetPasswordError translates the service's sentinels into the
// operator-facing wording. The id and email arms of the not-found case say
// different things because they were addressed differently: an unknown id
// points the operator at `users list`, an unknown email might just be a typo
// of an address that was never an account. The ambiguous-address case is
// finding DB-2's fix: services.AmbiguousEmailError (shared with
// OperatorUserService.GetUserByEmail — `users delete <email>` — and
// WebhookSettingsCLIService.resolveOwner — `webhook show|set`, which has no
// --id form at all) is never resolved silently to one match; this command's
// own mapping adds the explicit "retry with --id" pointer the other two
// leave to services.AmbiguousEmailError.Error()'s bare id list.
//
// Every case below is duplicated across two sentinel families on purpose:
// opts.resolve (services.ErrOperatorUser*, from OperatorUserService) runs
// ahead of the fence gate and normally reaches this function first, while
// ForceResetPasswordByID's own internal re-resolution (services.ErrAuth*) is
// what a row deleted in the narrow window between that resolve and this
// command's own write would surface instead — both must report identically,
// since the operator cannot tell which one fired.
func mapResetPasswordError(err error, opts resetPasswordOptions, normalizedEmail string) error {
	var ambiguous *services.AmbiguousEmailError
	if errors.As(err, &ambiguous) {
		return fmt.Errorf(
			"email %s matches %d accounts (ids %s); retry with --id (see ovumcy users list)",
			ambiguous.Email, len(ambiguous.IDs), formatUserIDs(ambiguous.IDs),
		)
	}

	switch {
	case errors.Is(err, services.ErrAuthUserNotFound), errors.Is(err, services.ErrOperatorUserNotFound):
		if opts.userID != 0 {
			return fmt.Errorf("no account carries id %d (see ovumcy users list)", opts.userID)
		}
		return fmt.Errorf("user %s not found", normalizedEmail)
	case errors.Is(err, services.ErrAuthUserIDRequired), errors.Is(err, services.ErrOperatorUserIDRequired):
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

// resetPasswordPolicyError runs the SAME password-length/composition check
// AuthService.ForceResetPasswordByID performs internally — it calls its own
// unexported authPasswordPolicyError(ValidatePasswordStrength(...)) — and
// translates the result into the same services.ErrAuth* sentinels
// mapResetPasswordError already knows how to report. Duplicated here rather
// than exported from services because it is four lines with no state:
// ValidatePasswordStrength is the actual policy, and this is only the
// too-long/weak split AuthService already applies to its own result.
func resetPasswordPolicyError(newPassword string) error {
	switch err := services.ValidatePasswordStrength(newPassword); {
	case err == nil:
		return nil
	case errors.Is(err, services.ErrPasswordTooLong):
		return services.ErrAuthPasswordTooLong
	default:
		return services.ErrAuthWeakPassword
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
