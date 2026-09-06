package cli

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"os"
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
	return runResetPasswordCommand(databaseConfig, args, calendarFeedFencePath(), resetPasswordReader(os.Stdin), os.Stdout)
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

func runResetPasswordCommand(databaseConfig db.Config, args []string, fencePath string, prompt passwordPromptFunc, output io.Writer) error {
	opts, err := parseResetPasswordArgs(args)
	if err != nil {
		return err
	}

	// parseResetPasswordArgs already guarantees opts.email is non-blank
	// whenever opts.userID is zero (a blank positional argument is skipped
	// during parsing, same as parseUsersDeleteArgs), so there is no separate
	// "email is required" case left to check here — only whether the
	// non-blank value is an address this command can resolve.
	normalizedEmail := ""
	if opts.userID == 0 {
		normalizedEmail, err = normalizeOperatorEmailArgument(opts.email)
		if err != nil {
			return err
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

	rawPassword, err := prompt()
	if err != nil {
		return fmt.Errorf("read new password: %w", err)
	}
	defer clear(rawPassword)
	if len(rawPassword) == 0 {
		return errors.New("password is required")
	}
	// Applied here, ahead of the fence gate below, for the same reason the
	// target is resolved ahead of it: a password ForceResetPasswordByID would
	// refuse anyway must not spend the gate's one-shot confirmation first.
	// This is the service's own rule rather than a local copy of it, and what
	// it returns is what gets submitted a few lines down — a value checked in
	// one shape and submitted in another would put the refusal back after the
	// gate, which is the whole thing this ordering exists to prevent.
	newPassword, err := services.NormalizeForcedResetPassword(string(rawPassword))
	if err != nil {
		return mapResetPasswordError(err, opts, normalizedEmail)
	}

	repositories, fence := buildRepositories(database, fencePath)
	authService := services.NewAuthService(repositories.Users)
	operatorUsers := services.NewOperatorUserService(repositories.Users, authService)

	target, err := resolveOperatorUser(operatorUsers, opts.userID, normalizedEmail)
	if err != nil {
		return mapOperatorUserLookupError(err, opts.userID, normalizedEmail)
	}

	// A forced reset force-clears the owner's calendar feed, so this process
	// must confirm and advance the SAME fence the server does before the reset
	// is even attempted — never merely warn about it.
	if err := confirmOperatorFeedRevocation(context.Background(), fencePath, fence, os.Stderr); err != nil {
		return err
	}

	if resetErr := authService.ForceResetPasswordByID(context.Background(), target.ID, newPassword); resetErr != nil {
		return operatorFeedRevocationCommitted(mapResetPasswordError(resetErr, opts, normalizedEmail))
	}

	if output == nil {
		output = os.Stdout
	}
	_, _ = fmt.Fprintln(output, "✅ Password reset successful")
	_, _ = fmt.Fprintln(output, "Existing auth sessions were invalidated.")
	_, _ = fmt.Fprintln(output, "User must sign in again and reset the password before continuing.")

	return nil
}

// mapResetPasswordError translates AuthService's own sentinels — the password
// policy this command applies ahead of the fence gate, and the id
// re-resolution ForceResetPasswordByID performs internally — into the
// operator-facing wording.
//
// Account-lookup refusals do not reach it: the address is resolved through
// mapOperatorUserLookupError, shared with `users delete` and
// `link-oidc-identity` so all three name an unknown account identically. The
// one lookup sentinel left here is ErrAuthUserNotFound, which only a row
// deleted in the narrow window between that resolve and this command's own
// write can produce — the operator cannot tell which of the two fired, so it
// is answered by that same shared helper.
func mapResetPasswordError(err error, opts resetPasswordOptions, normalizedEmail string) error {
	switch {
	case errors.Is(err, services.ErrAuthUserNotFound):
		return operatorUserNotFoundError(opts.userID, normalizedEmail)
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
