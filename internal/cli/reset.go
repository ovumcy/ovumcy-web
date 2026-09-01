package cli

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"net/mail"
	"os"
	"strings"

	"github.com/ovumcy/ovumcy-web/internal/db"
	"github.com/ovumcy/ovumcy-web/internal/services"
)

func RunResetPasswordCommand(databaseConfig db.Config, email string) error {
	return runResetPasswordCommand(databaseConfig, email, resetPasswordReader(os.Stdin), os.Stdout)
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

func runResetPasswordCommand(databaseConfig db.Config, email string, prompt passwordPromptFunc, output io.Writer) error {
	normalizedEmail := strings.ToLower(strings.TrimSpace(email))
	if normalizedEmail == "" {
		return errors.New("email is required")
	}
	if _, err := mail.ParseAddress(normalizedEmail); err != nil {
		return fmt.Errorf("invalid email address: %w", err)
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

	// A forced reset force-clears the owner's calendar feed, so this process has
	// to be able to record that removal outside the database.
	warnAboutAnUnreachableCalendarFeedFence(os.Stderr)
	repositories := buildRepositories(database)
	authService := services.NewAuthService(repositories.Users)
	if err := authService.ForceResetPasswordByEmail(context.Background(), normalizedEmail, string(newPassword)); err != nil {
		switch {
		case errors.Is(err, services.ErrAuthUserNotFound):
			return fmt.Errorf("user %s not found", normalizedEmail)
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

	if output == nil {
		output = os.Stdout
	}
	_, _ = fmt.Fprintln(output, "✅ Password reset successful")
	_, _ = fmt.Fprintln(output, "Existing auth sessions were invalidated.")
	_, _ = fmt.Fprintln(output, "User must sign in again and reset the password before continuing.")

	return nil
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
