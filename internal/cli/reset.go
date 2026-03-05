package cli

import (
	"bytes"
	"errors"
	"fmt"
	"io"
	"net/mail"
	"os"
	"strings"

	"github.com/terraincognita07/ovumcy/internal/db"
	"github.com/terraincognita07/ovumcy/internal/services"
)

func RunResetPasswordCommand(dbPath string, email string) error {
	return runResetPasswordCommand(dbPath, email, promptNewPassword, os.Stdout)
}

type passwordPromptFunc func() ([]byte, error)

func runResetPasswordCommand(dbPath string, email string, prompt passwordPromptFunc, output io.Writer) error {
	normalizedEmail := strings.ToLower(strings.TrimSpace(email))
	if normalizedEmail == "" {
		return errors.New("email is required")
	}
	if _, err := mail.ParseAddress(normalizedEmail); err != nil {
		return fmt.Errorf("invalid email address: %w", err)
	}

	database, err := db.OpenSQLite(dbPath)
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

	repositories := db.NewRepositories(database)
	authService := services.NewAuthService(repositories.Users)
	if err := authService.ForceResetPasswordByEmail(normalizedEmail, string(newPassword)); err != nil {
		switch {
		case errors.Is(err, services.ErrAuthUserNotFound):
			return fmt.Errorf("user %s not found", normalizedEmail)
		case errors.Is(err, services.ErrAuthResetInvalid):
			return errors.New("password is required")
		case errors.Is(err, services.ErrAuthWeakPassword):
			return errors.New("password does not meet strength requirements")
		default:
			return fmt.Errorf("reset password: %w", err)
		}
	}

	if output == nil {
		output = os.Stdout
	}
	fmt.Fprintln(output, "✅ Password reset successful")
	fmt.Fprintln(output, "User must change password on next login.")

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
		fmt.Fprint(os.Stdout, prompt)
	}

	password, err := readPasswordNoEcho(os.Stdin)
	fmt.Fprintln(os.Stdout)
	if err != nil {
		return nil, errors.New("secure password prompt requires an interactive terminal")
	}
	return password, nil
}
