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

	"github.com/ovumcy/ovumcy-web/internal/db"
	"github.com/ovumcy/ovumcy-web/internal/services"
)

func RunUsersCommand(databaseConfig db.Config, args []string) error {
	return runUsersCommand(databaseConfig, args, os.Stdin, os.Stdout)
}

func runUsersCommand(databaseConfig db.Config, args []string, input io.Reader, output io.Writer) error {
	if len(args) == 0 {
		return errors.New("usage: ovumcy users <list|delete>")
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
	default:
		return errors.New("usage: ovumcy users <list|delete>")
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
	service := services.NewOperatorUserService(repositories.Users)

	switch subcommand {
	case "list":
		return runUsersList(service, output)
	case "delete":
		return runUsersDelete(service, args[1:], input, output)
	default:
		return errors.New("usage: ovumcy users <list|delete>")
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
