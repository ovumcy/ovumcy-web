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

	"strconv"

	"github.com/ovumcy/ovumcy-web/internal/db"
	"github.com/ovumcy/ovumcy-web/internal/models"
	"github.com/ovumcy/ovumcy-web/internal/services"
)

func RunUsersCommand(databaseConfig db.Config, args []string) error {
	return runUsersCommand(databaseConfig, args, os.Stdin, os.Stdout)
}

func runUsersCommand(databaseConfig db.Config, args []string, input io.Reader, output io.Writer) error {
	if len(args) == 0 {
		return errors.New("usage: ovumcy users <list|delete|create|set-email>")
	}

	subcommand := strings.ToLower(strings.TrimSpace(args[0]))
	switch subcommand {
	case "list":
		if len(args) != 1 {
			return errors.New("usage: ovumcy users list")
		}
	case "delete":
		if _, err := parseUsersDeleteArgs(args[1:]); err != nil {
			return err
		}
	case "create":
		if _, err := parseUsersCreateArgs(args[1:]); err != nil {
			return err
		}
	case "set-email":
		if _, err := parseUsersSetEmailArgs(args[1:]); err != nil {
			return err
		}
	default:
		return errors.New("usage: ovumcy users <list|delete|create|set-email>")
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

	repositories := buildRepositories(database)
	service := services.NewOperatorUserService(repositories.Users, services.NewAuthService(repositories.Users))

	switch subcommand {
	case "list":
		return runUsersList(service, output)
	case "delete":
		return runUsersDelete(service, args[1:], input, output)
	case "create":
		return runUsersCreate(service, args[1:], input, output)
	case "set-email":
		return runUsersSetEmail(service, args[1:], output)
	default:
		return errors.New("usage: ovumcy users <list|delete|create|set-email>") // codecov:ignore -- unreachable: the subcommand is validated in the switch above
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
	opts, err := parseUsersDeleteArgs(args)
	if err != nil {
		return err
	}

	user, err := opts.resolve(service)
	if err != nil {
		return err
	}

	if output == nil {
		output = os.Stdout
	}
	if !opts.skipConfirm {
		// %q, not %s: an account addressed by id may be one of the legacy rows
		// the boot repair had to leave standing, whose stored value carries a
		// display name and looks nothing like the address the operator has in
		// mind. Quoting it is what makes the confirmation a real check.
		_, _ = fmt.Fprintf(output, "Delete account %q (id=%d, role=%s) and all related health data? Type DELETE to continue: ", user.Email, user.ID, user.Role)
		confirmed, confirmErr := readDeleteConfirmation(input)
		if confirmErr != nil {
			return confirmErr
		}
		if !confirmed {
			return errors.New("account deletion cancelled")
		}
	}

	// The erasure takes the account row and its calendar feed with it, so this
	// process has to be able to record that removal outside the database. It is
	// raised here, past the confirmation and immediately before the write, so
	// that a cancelled deletion does not print it and a deletion that fails
	// halfway still does.
	warnAboutAnUnreachableCalendarFeedFence(os.Stderr)

	deletedUser, err := opts.delete(service)
	if err != nil {
		return err
	}
	_, _ = fmt.Fprintf(output, "Deleted account %q (id=%d).\n", deletedUser.Email, deletedUser.ID)
	return nil
}

const usersDeleteUsage = "usage: ovumcy users delete <email>|--id <id> [--yes]"

// usersDeleteOptions carries exactly one handle. The id form exists because the
// address form cannot reach every row: a legacy stored value the strict
// NormalizeAuthEmail rule refuses is rejected before any lookup runs, and its
// bare address resolves the OTHER account on that mailbox — the one the boot
// repair let keep the address — so an operator following the leftover runbook
// with the address in front of them would erase the wrong account's health
// history.
type usersDeleteOptions struct {
	email       string
	userID      uint
	skipConfirm bool
}

func (opts usersDeleteOptions) resolve(service *services.OperatorUserService) (models.OperatorUserSummary, error) {
	if opts.userID != 0 {
		return service.GetUserByID(context.Background(), opts.userID)
	}
	return service.GetUserByEmail(context.Background(), opts.email)
}

func (opts usersDeleteOptions) delete(service *services.OperatorUserService) (models.OperatorUserSummary, error) {
	if opts.userID != 0 {
		return service.DeleteUserByID(context.Background(), opts.userID)
	}
	return service.DeleteUserByEmail(context.Background(), opts.email)
}

func parseUsersDeleteArgs(args []string) (usersDeleteOptions, error) {
	opts := usersDeleteOptions{}
	for index := 0; index < len(args); index++ {
		value := strings.TrimSpace(args[index])
		switch {
		case value == "":
			continue
		case value == "--yes":
			opts.skipConfirm = true
		case isUsersIDFlag(value):
			userID, consumed, err := parseUsersIDFlag(args, index, usersDeleteUsage)
			if err != nil {
				return usersDeleteOptions{}, err
			}
			if opts.userID != 0 {
				return usersDeleteOptions{}, errors.New(usersDeleteUsage)
			}
			opts.userID = userID
			index += consumed
		case strings.HasPrefix(value, "--"):
			return usersDeleteOptions{}, errors.New(usersDeleteUsage)
		default:
			if opts.email != "" {
				return usersDeleteOptions{}, errors.New(usersDeleteUsage)
			}
			opts.email = value
		}
	}

	if (opts.email == "") == (opts.userID == 0) {
		return usersDeleteOptions{}, errors.New(usersDeleteUsage)
	}
	return opts, nil
}

func isUsersIDFlag(value string) bool {
	return value == "--id" || strings.HasPrefix(value, "--id=")
}

// parseUsersIDFlag accepts both `--id 7` and `--id=7`, and reports how many
// FOLLOWING arguments it consumed. The id is the one printed by `users list`,
// so anything that is not a positive whole number is a typo, not a row.
func parseUsersIDFlag(args []string, index int, usage string) (uint, int, error) {
	raw := ""
	consumed := 0
	if after, found := strings.CutPrefix(strings.TrimSpace(args[index]), "--id="); found {
		raw = strings.TrimSpace(after)
	} else {
		if index+1 >= len(args) {
			return 0, 0, errors.New(usage)
		}
		raw = strings.TrimSpace(args[index+1])
		consumed = 1
	}

	userID, err := strconv.ParseUint(raw, 10, 32)
	if err != nil || userID == 0 {
		return 0, 0, fmt.Errorf("invalid account id %q (see ovumcy users list)", raw)
	}
	return uint(userID), consumed, nil
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

const usersSetEmailUsage = "usage: ovumcy users set-email --id <id> <email>"

// runUsersSetEmail is the non-destructive half of the leftover runbook: it
// re-homes ONE account, addressed by the id `users list` prints, to an address
// a later sign-in can actually reproduce. Deleting such a row was never a
// repair — the account's health history goes with it — and until this command
// existed the runbook's "remove or re-home it deliberately" had no re-home to
// name.
func runUsersSetEmail(service *services.OperatorUserService, args []string, output io.Writer) error {
	opts, err := parseUsersSetEmailArgs(args)
	if err != nil {
		return err
	}

	before, after, err := service.SetEmailByID(context.Background(), opts.userID, opts.email)
	if err != nil {
		return mapUsersSetEmailError(err)
	}

	if output == nil {
		output = os.Stdout
	}
	// %q on the stored value: a legacy row's decorated form differs from the
	// address it contains, and the operator needs to see which one moved.
	_, _ = fmt.Fprintf(output, "Account id=%d (role=%s) re-homed: %q → %s\n", before.ID, before.Role, before.Email, after.Email)
	_, _ = fmt.Fprintln(output, "Every session of this account was revoked; its health data is untouched. Sign in with the new address.")
	return nil
}

type usersSetEmailOptions struct {
	userID uint
	email  string
}

func parseUsersSetEmailArgs(args []string) (usersSetEmailOptions, error) {
	opts := usersSetEmailOptions{}
	for index := 0; index < len(args); index++ {
		value := strings.TrimSpace(args[index])
		switch {
		case value == "":
			continue
		case isUsersIDFlag(value):
			userID, consumed, err := parseUsersIDFlag(args, index, usersSetEmailUsage)
			if err != nil {
				return usersSetEmailOptions{}, err
			}
			if opts.userID != 0 {
				return usersSetEmailOptions{}, errors.New(usersSetEmailUsage)
			}
			opts.userID = userID
			index += consumed
		case strings.HasPrefix(value, "--"):
			return usersSetEmailOptions{}, errors.New(usersSetEmailUsage)
		default:
			if opts.email != "" {
				return usersSetEmailOptions{}, errors.New(usersSetEmailUsage)
			}
			opts.email = value
		}
	}

	if opts.userID == 0 || opts.email == "" {
		return usersSetEmailOptions{}, errors.New(usersSetEmailUsage)
	}
	return opts, nil
}

func mapUsersSetEmailError(err error) error {
	switch {
	case errors.Is(err, services.ErrOperatorUserIDRequired):
		return errors.New("an account id is required (see ovumcy users list)")
	case errors.Is(err, services.ErrOperatorUserNotFound):
		return errors.New("no account carries this id (see ovumcy users list)")
	case errors.Is(err, services.ErrOperatorUserEmailRequired):
		return errors.New("email is required")
	case errors.Is(err, services.ErrOperatorUserEmailInvalid):
		return errors.New("invalid email address: pass the bare address, with no display name or angle brackets")
	case errors.Is(err, services.ErrOperatorUserEmailExists):
		return errors.New("another account already answers to this email address")
	case errors.Is(err, services.ErrOperatorUserChangedUnderRepair):
		return errors.New("this account's email changed while the repair ran — re-read ovumcy users list and retry")
	default:
		return fmt.Errorf("set email: %w", err)
	}
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
