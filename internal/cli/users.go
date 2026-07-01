package cli

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"strings"
	"text/tabwriter"
	"time"

	"github.com/ovumcy/ovumcy-web/internal/db"
	"github.com/ovumcy/ovumcy-web/internal/services"
)

func RunUsersCommand(databaseConfig db.Config, args []string) error {
	return runUsersCommand(databaseConfig, args, os.Stdin, os.Stdout)
}

func runUsersCommand(databaseConfig db.Config, args []string, input io.Reader, output io.Writer) error {
	if len(args) == 0 {
		return errors.New("usage: ovumcy users <list|delete|create>")
	}

	subcommand := strings.ToLower(strings.TrimSpace(args[0]))
	switch subcommand {
	case "list":
		if len(args) != 1 {
			return errors.New("usage: ovumcy users list")
		}
	case "delete":
		if _, _, err := parseUsersDeleteArgs(args[1:]); err != nil {
			return err
		}
	case "create":
		if _, err := parseUsersCreateArgs(args[1:]); err != nil {
			return err
		}
	default:
		return errors.New("usage: ovumcy users <list|delete|create>")
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

	repositories := db.NewRepositories(database)
	service := services.NewOperatorUserService(repositories.Users, services.NewAuthService(repositories.Users))

	switch subcommand {
	case "list":
		return runUsersList(service, output)
	case "delete":
		return runUsersDelete(service, args[1:], input, output)
	case "create":
		return runUsersCreate(service, args[1:], input, output)
	default:
		return errors.New("usage: ovumcy users <list|delete|create>") // codecov:ignore -- unreachable: the subcommand is validated in the switch above
	}
}

func runUsersList(service *services.OperatorUserService, output io.Writer) error {
	users, err := service.ListUsers(context.Background())
	if err != nil {
		return err
	}

	if output == nil {
		output = os.Stdout
	}
	if len(users) == 0 {
		_, _ = fmt.Fprintln(output, "No users found.")
		return nil
	}

	writer := tabwriter.NewWriter(output, 0, 4, 2, ' ', 0)
	_, _ = fmt.Fprintln(writer, "ID\tEMAIL\tROLE\tDISPLAY NAME\tONBOARDED\tCREATED AT")
	for _, user := range users {
		displayName := strings.TrimSpace(user.DisplayName)
		if displayName == "" {
			displayName = "-"
		}
		onboarded := "no"
		if user.OnboardingCompleted {
			onboarded = "yes"
		}
		_, _ = fmt.Fprintf(
			writer,
			"%d\t%s\t%s\t%s\t%s\t%s\n",
			user.ID,
			user.Email,
			user.Role,
			displayName,
			onboarded,
			user.CreatedAt.UTC().Format("2006-01-02 15:04:05Z"),
		)
	}
	return writer.Flush()
}

func runUsersDelete(service *services.OperatorUserService, args []string, input io.Reader, output io.Writer) error {
	email, skipConfirm, err := parseUsersDeleteArgs(args)
	if err != nil {
		return err
	}

	user, err := service.GetUserByEmail(context.Background(), email)
	if err != nil {
		return err
	}

	if output == nil {
		output = os.Stdout
	}
	if !skipConfirm {
		_, _ = fmt.Fprintf(output, "Delete account %s (id=%d, role=%s) and all related health data? Type DELETE to continue: ", user.Email, user.ID, user.Role)
		confirmed, confirmErr := readDeleteConfirmation(input)
		if confirmErr != nil {
			return confirmErr
		}
		if !confirmed {
			return errors.New("account deletion cancelled")
		}
	}

	deletedUser, err := service.DeleteUserByEmail(context.Background(), email)
	if err != nil {
		return err
	}
	_, _ = fmt.Fprintf(output, "Deleted account %s (id=%d).\n", deletedUser.Email, deletedUser.ID)
	return nil
}

func parseUsersDeleteArgs(args []string) (string, bool, error) {
	if len(args) == 0 {
		return "", false, errors.New("usage: ovumcy users delete <email> [--yes]")
	}

	email := ""
	skipConfirm := false
	for _, arg := range args {
		value := strings.TrimSpace(arg)
		switch value {
		case "":
			continue
		case "--yes":
			skipConfirm = true
		default:
			if email != "" {
				return "", false, errors.New("usage: ovumcy users delete <email> [--yes]")
			}
			email = value
		}
	}

	if email == "" {
		return "", false, errors.New("usage: ovumcy users delete <email> [--yes]")
	}
	return email, skipConfirm, nil
}

func readDeleteConfirmation(input io.Reader) (bool, error) {
	if input == nil {
		return false, errors.New("confirmation input is required")
	}
	reader := bufio.NewReader(input)
	line, err := reader.ReadString('\n')
	if err != nil && !errors.Is(err, io.EOF) {
		return false, fmt.Errorf("read delete confirmation: %w", err)
	}
	return strings.EqualFold(strings.TrimSpace(line), "DELETE"), nil
}

func runUsersCreate(service *services.OperatorUserService, args []string, input io.Reader, output io.Writer) error {
	opts, err := parseUsersCreateArgs(args)
	if err != nil {
		return err
	}

	password, err := readCreatePassword(input)
	if err != nil {
		return err
	}
	defer clear(password)

	summary, recoveryCode, err := service.CreateOwner(context.Background(), opts.email, string(password), time.Now().UTC())
	if err != nil {
		// --skip-if-exists makes provisioning idempotent for install scripts
		// (re-runs, upgrades): an existing email is not an error. It never
		// updates the existing account — use reset-password to change a password.
		if opts.skipIfExists && errors.Is(err, services.ErrOperatorUserEmailExists) {
			if output == nil {
				output = os.Stdout
			}
			_, _ = fmt.Fprintf(output, "Account %s already exists — skipping.\n", opts.email)
			return nil
		}
		return mapUsersCreateError(err)
	}

	if output == nil {
		output = os.Stdout
	}
	_, _ = fmt.Fprintf(output, "✅ Created owner account %s (id=%d).\n", summary.Email, summary.ID)
	if opts.showRecoveryCode {
		_, _ = fmt.Fprintf(output, "Recovery code: %s\n", recoveryCode)
		_, _ = fmt.Fprintln(output, "Store it securely now — it is shown only once and must never be saved in install logs or scripts.")
	} else {
		_, _ = fmt.Fprintln(output, "No recovery code was printed. Sign in and regenerate one from Settings to enable self-service password recovery.")
	}
	_, _ = fmt.Fprintln(output, "The owner completes onboarding (last period start, cycle defaults) on first sign-in.")
	return nil
}

type usersCreateOptions struct {
	email            string
	showRecoveryCode bool
	skipIfExists     bool
}

func parseUsersCreateArgs(args []string) (usersCreateOptions, error) {
	const usage = "usage: ovumcy users create <email> [--show-recovery-code] [--skip-if-exists]"
	opts := usersCreateOptions{}
	for _, arg := range args {
		value := strings.TrimSpace(arg)
		switch {
		case value == "":
			continue
		case value == "--show-recovery-code":
			opts.showRecoveryCode = true
		case value == "--skip-if-exists":
			opts.skipIfExists = true
		case strings.HasPrefix(value, "--"):
			return usersCreateOptions{}, errors.New(usage)
		default:
			if opts.email != "" {
				return usersCreateOptions{}, errors.New(usage)
			}
			opts.email = value
		}
	}

	if opts.email == "" {
		return usersCreateOptions{}, errors.New(usage)
	}
	return opts, nil
}

// readCreatePassword obtains the new owner's password without exposing it in
// argv or the environment. On an interactive terminal it prompts twice with echo
// disabled (reusing the reset-password prompt). When stdin is piped or redirected
// — the declarative-provisioning path, e.g. a YunoHost install script — it reads
// the password as the first line of stdin.
func readCreatePassword(input io.Reader) ([]byte, error) {
	// codecov:ignore:start -- interactive TTY prompt; the terminal branch needs a real terminal and is exercised only interactively
	if file, ok := input.(*os.File); ok && stdinIsTerminal(file) {
		return promptNewPassword()
	}
	// codecov:ignore:end
	return readPasswordLine(input)
}

func readPasswordLine(input io.Reader) ([]byte, error) {
	if input == nil {
		return nil, errors.New("password input is required")
	}
	reader := bufio.NewReader(input)
	line, err := reader.ReadString('\n')
	if err != nil && !errors.Is(err, io.EOF) {
		return nil, fmt.Errorf("read password: %w", err)
	}
	// Trim surrounding whitespace so a CLI-set password matches web auth, which
	// normalizes the same way (services.NormalizeCredentialsInput) on both
	// registration and login. Without this, a stray leading/trailing space would
	// be hashed here but trimmed at web login, locking the owner out.
	line = strings.TrimSpace(line)
	if line == "" {
		return nil, errors.New("password is required")
	}
	return []byte(line), nil
}

func stdinIsTerminal(file *os.File) bool {
	info, err := file.Stat()
	if err != nil {
		return false
	}
	return info.Mode()&os.ModeCharDevice != 0
}

func mapUsersCreateError(err error) error {
	switch {
	case errors.Is(err, services.ErrOperatorUserEmailRequired):
		return errors.New("email is required")
	case errors.Is(err, services.ErrOperatorUserEmailInvalid):
		return errors.New("invalid email address")
	case errors.Is(err, services.ErrOperatorUserPasswordWeak):
		return errors.New("password does not meet strength requirements (min 8 characters, with upper, lower, and a digit)")
	case errors.Is(err, services.ErrOperatorUserEmailExists):
		return errors.New("an account with this email already exists (use reset-password to change its password)")
	default:
		return fmt.Errorf("create owner: %w", err)
	}
}
