package main

import (
	"fmt"
	"os"
	"strings"
	"time"

	"github.com/ovumcy/ovumcy-web/internal/cli"
	"github.com/ovumcy/ovumcy-web/internal/db"
)

func tryRunCLICommand() (bool, error) {
	return tryRunCLICommandWithHandlers(os.Args[1:], cliCommandHandlers{
		runResetPassword: cli.RunResetPasswordCommand,
		runUsers:         cli.RunUsersCommand,
		runHealthcheck:   cli.RunHealthcheckCommand,
		runNotify:        cli.RunNotifyCommand,  // codecov:ignore -- main() composition-root wiring; this os.Args dispatch wrapper runs only in the binary (the handler is unit-tested via tryRunCLICommandWithHandlers with a stub)
		runWebhook:       cli.RunWebhookCommand, // codecov:ignore -- main() composition-root wiring; this os.Args dispatch wrapper runs only in the binary (the handler is unit-tested via tryRunCLICommandWithHandlers with a stub)
	})
}

type cliCommandHandlers struct {
	runResetPassword func(databaseConfig db.Config, email string) error
	runUsers         func(databaseConfig db.Config, args []string) error
	runHealthcheck   func(port string, timeout time.Duration) error
	runNotify        func(databaseConfig db.Config, secretKey string, defaultLanguage string, location *time.Location, blockPrivateAddresses bool, args []string) error
	runWebhook       func(databaseConfig db.Config, secretKey string, args []string) error
}

func tryRunCLICommandWithHandlers(args []string, handlers cliCommandHandlers) (bool, error) {
	if len(args) == 0 {
		return false, nil
	}

	switch strings.TrimSpace(args[0]) {
	case "reset-password":
		return handleResetPasswordCommand(args, handlers)
	case "users":
		return handleUsersCommand(args, handlers)
	case "healthcheck":
		return handleHealthcheckCommand(args, handlers)
	case "notify":
		return handleNotifyCommand(args, handlers)
	case "webhook":
		return handleWebhookCommand(args, handlers)
	default:
		return false, nil
	}
}

func handleResetPasswordCommand(args []string, handlers cliCommandHandlers) (bool, error) {
	if len(args) != 2 {
		return true, fmt.Errorf("usage: ovumcy reset-password <email>")
	}
	if handlers.runResetPassword == nil {
		return true, fmt.Errorf("reset-password handler is required")
	}
	databaseConfig, err := resolveDatabaseConfig()
	if err != nil {
		return true, fmt.Errorf("invalid database config: %w", err)
	}
	email := strings.TrimSpace(args[1])
	return true, handlers.runResetPassword(databaseConfig, email)
}

func handleUsersCommand(args []string, handlers cliCommandHandlers) (bool, error) {
	if len(args) < 2 {
		return true, fmt.Errorf("usage: ovumcy users <list|delete|create>")
	}
	if handlers.runUsers == nil {
		return true, fmt.Errorf("users handler is required")
	}
	databaseConfig, err := resolveDatabaseConfig()
	if err != nil {
		return true, fmt.Errorf("invalid database config: %w", err)
	}
	return true, handlers.runUsers(databaseConfig, args[1:])
}

func handleHealthcheckCommand(args []string, handlers cliCommandHandlers) (bool, error) {
	if len(args) != 1 {
		return true, fmt.Errorf("usage: ovumcy healthcheck")
	}
	if handlers.runHealthcheck == nil {
		return true, fmt.Errorf("healthcheck handler is required")
	}
	port, err := resolvePort()
	if err != nil {
		return true, fmt.Errorf("invalid PORT: %w", err)
	}
	return true, handlers.runHealthcheck(port, 0)
}

func handleNotifyCommand(args []string, handlers cliCommandHandlers) (bool, error) {
	if handlers.runNotify == nil {
		return true, fmt.Errorf("notify handler is required")
	}
	databaseConfig, err := resolveDatabaseConfig()
	if err != nil {
		return true, fmt.Errorf("invalid database config: %w", err)
	}
	secretKey, err := resolveSecretKey()
	if err != nil {
		return true, fmt.Errorf("invalid SECRET_KEY: %w", err)
	}
	location := mustLoadLocation(getEnv("TZ", "Local"))
	defaultLanguage := getEnv("DEFAULT_LANGUAGE", "en")
	blockPrivateAddresses := getEnvBool("WEBHOOK_BLOCK_PRIVATE_ADDRESSES", false)
	return true, handlers.runNotify(databaseConfig, secretKey, defaultLanguage, location, blockPrivateAddresses, args[1:])
}

func handleWebhookCommand(args []string, handlers cliCommandHandlers) (bool, error) {
	if len(args) < 2 {
		return true, fmt.Errorf("usage: ovumcy webhook <show|set> <email> [flags]")
	}
	if handlers.runWebhook == nil {
		return true, fmt.Errorf("webhook handler is required")
	}
	databaseConfig, err := resolveDatabaseConfig()
	if err != nil {
		return true, fmt.Errorf("invalid database config: %w", err)
	}
	secretKey, err := resolveSecretKey()
	if err != nil {
		return true, fmt.Errorf("invalid SECRET_KEY: %w", err)
	}
	return true, handlers.runWebhook(databaseConfig, secretKey, args[1:])
}
