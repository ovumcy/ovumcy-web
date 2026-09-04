package cli

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net/mail"
	"os"
	"strings"
	"time"

	"github.com/ovumcy/ovumcy-web/internal/db"
	"github.com/ovumcy/ovumcy-web/internal/models"
	"github.com/ovumcy/ovumcy-web/internal/security"
	"github.com/ovumcy/ovumcy-web/internal/services"
)

// link-oidc-identity is the no-session recovery path for issue #701: linking
// an OIDC identity is a permanent, password-change-weight binding, so the two
// sanctioned ways to create one are an authenticated Settings step-up
// (internal/api's /api/v1/users/current/oidc/link/step-up) or this operator
// command. The public /auth/oidc/link-confirm route that used to authorise the
// same binding with a password alone, on a page reachable without a session,
// stays closed — this command exists so closing it does not strand an account
// with no working sign-in path at all.
//
// Addressing mirrors reset-password exactly (see PR #699): a bare email or
// `--id <id>`, mutually exclusive, exactly one required. The id form reaches a
// legacy row the strict NormalizeAuthEmail rule refuses, or one sharing a
// mailbox with another account, neither of which any email-taking command can
// address at all.
const linkOIDCIdentityUsage = "usage: ovumcy link-oidc-identity <email>|--id <id> --issuer <issuer> --subject <subject>"

type linkOIDCIdentityOptions struct {
	email   string
	userID  uint
	issuer  string
	subject string
}

// parseLinkOIDCIdentityArgs mirrors parseResetPasswordArgs's addressing (same
// flag spelling, same precedence, same ambiguity wording) and adds the two
// required identity flags this command carries instead of a password.
func parseLinkOIDCIdentityArgs(args []string) (linkOIDCIdentityOptions, error) {
	opts := linkOIDCIdentityOptions{}
	for index := 0; index < len(args); index++ {
		value := strings.TrimSpace(args[index])
		switch {
		case value == "":
			continue
		case isUsersIDFlag(value):
			userID, consumed, err := parseUsersIDFlag(args, index, linkOIDCIdentityUsage)
			if err != nil {
				return linkOIDCIdentityOptions{}, err
			}
			if opts.userID != 0 {
				return linkOIDCIdentityOptions{}, errors.New(linkOIDCIdentityUsage)
			}
			opts.userID = userID
			index += consumed
		case value == "--issuer" || strings.HasPrefix(value, "--issuer="):
			issuer, consumed, err := parseLinkOIDCIdentityStringFlag(args, index, "--issuer", linkOIDCIdentityUsage)
			if err != nil {
				return linkOIDCIdentityOptions{}, err
			}
			if opts.issuer != "" {
				return linkOIDCIdentityOptions{}, errors.New(linkOIDCIdentityUsage)
			}
			opts.issuer = issuer
			index += consumed
		case value == "--subject" || strings.HasPrefix(value, "--subject="):
			subject, consumed, err := parseLinkOIDCIdentityStringFlag(args, index, "--subject", linkOIDCIdentityUsage)
			if err != nil {
				return linkOIDCIdentityOptions{}, err
			}
			if opts.subject != "" {
				return linkOIDCIdentityOptions{}, errors.New(linkOIDCIdentityUsage)
			}
			opts.subject = subject
			index += consumed
		case strings.HasPrefix(value, "--"):
			return linkOIDCIdentityOptions{}, errors.New(linkOIDCIdentityUsage)
		default:
			if opts.email != "" {
				return linkOIDCIdentityOptions{}, errors.New(linkOIDCIdentityUsage)
			}
			opts.email = value
		}
	}

	if (opts.email == "") == (opts.userID == 0) {
		return linkOIDCIdentityOptions{}, errors.New(linkOIDCIdentityUsage)
	}
	if strings.TrimSpace(opts.issuer) == "" || strings.TrimSpace(opts.subject) == "" {
		return linkOIDCIdentityOptions{}, errors.New(linkOIDCIdentityUsage)
	}
	return opts, nil
}

// parseLinkOIDCIdentityStringFlag accepts both `--name value` and
// `--name=value` and reports how many FOLLOWING arguments it consumed, the
// same shape parseUsersIDFlag uses for --id.
func parseLinkOIDCIdentityStringFlag(args []string, index int, name string, usage string) (string, int, error) {
	if after, found := strings.CutPrefix(strings.TrimSpace(args[index]), name+"="); found {
		value := strings.TrimSpace(after)
		if value == "" {
			return "", 0, errors.New(usage)
		}
		return value, 0, nil
	}
	if index+1 >= len(args) {
		return "", 0, errors.New(usage)
	}
	value := strings.TrimSpace(args[index+1])
	if value == "" {
		return "", 0, errors.New(usage)
	}
	return value, 1, nil
}

func RunLinkOIDCIdentityCommand(databaseConfig db.Config, oidcConfig security.OIDCConfig, args []string) error {
	return runLinkOIDCIdentityCommand(databaseConfig, oidcConfig, args, os.Stdout)
}

func runLinkOIDCIdentityCommand(databaseConfig db.Config, oidcConfig security.OIDCConfig, args []string, output io.Writer) error {
	opts, err := parseLinkOIDCIdentityArgs(args)
	if err != nil {
		return err
	}

	normalizedEmail := ""
	if opts.userID == 0 {
		normalizedEmail = strings.ToLower(strings.TrimSpace(opts.email))
		if _, err := mail.ParseAddress(normalizedEmail); err != nil {
			return fmt.Errorf("invalid email address: %w", err)
		}
	}

	oidcClient := security.NewOIDCClient(oidcConfig)
	if !oidcClient.Enabled() {
		return errors.New("OIDC is not enabled on this instance (set OIDC_ENABLED=true)")
	}
	issuer := strings.TrimSpace(opts.issuer)
	if configuredIssuer := strings.TrimSpace(oidcConfig.IssuerURL); configuredIssuer != "" && issuer != configuredIssuer {
		// Not a security check (nothing here is reachable without operator
		// access) — a guard against the operator's likeliest mistake. A
		// mismatched issuer creates a row no future sign-in can ever match: the
		// login path resolves identities by the EXACT issuer string the ID
		// token carries, which is this instance's configured issuer.
		return fmt.Errorf("issuer %q does not match the configured OIDC_ISSUER_URL %q", issuer, configuredIssuer)
	}

	database, err := db.OpenDatabase(databaseConfig)
	if err != nil {
		return fmt.Errorf("database init failed: %w", err)
	}
	sqlDB, err := database.DB()
	if err != nil {
		// codecov:ignore:start -- defensive: gorm's DB() accessor fails only if the
		// dialector has no underlying *sql.DB, which neither the sqlite nor the
		// postgres driver this command can be configured with ever lacks.
		return fmt.Errorf("database init failed: %w", err)
		// codecov:ignore:end
	}
	defer func() {
		_ = sqlDB.Close()
	}()

	repositories := buildRepositories(database)
	userService := services.NewOperatorUserService(repositories.Users, services.NewAuthService(repositories.Users))

	target, err := resolveLinkOIDCIdentityTarget(userService, opts)
	if err != nil {
		return mapLinkOIDCIdentityLookupError(err, opts, normalizedEmail)
	}

	oidcLoginService := services.NewOIDCLoginService(oidcClient, repositories.OIDCIdentities, repositories.Users, nil)
	claims := security.OIDCClaims{
		Issuer:  issuer,
		Subject: strings.TrimSpace(opts.subject),
	}
	if err := oidcLoginService.ConfirmAndLinkIdentity(context.Background(), target.ID, claims, time.Now()); err != nil {
		return mapLinkOIDCIdentityLinkError(err)
	}

	if output == nil {
		output = os.Stdout
	}
	_, _ = fmt.Fprintf(output, "✅ Linked OIDC identity (issuer=%s, subject=%s) to account %q (id=%d)\n", claims.Issuer, claims.Subject, target.Email, target.ID)
	return nil
}

func resolveLinkOIDCIdentityTarget(userService *services.OperatorUserService, opts linkOIDCIdentityOptions) (models.OperatorUserSummary, error) {
	if opts.userID != 0 {
		return userService.GetUserByID(context.Background(), opts.userID)
	}
	return userService.GetUserByEmail(context.Background(), opts.email)
}

// mapLinkOIDCIdentityLookupError mirrors mapResetPasswordError's account-
// lookup arms: the id and email cases point the operator at different next
// steps because they were addressed differently.
func mapLinkOIDCIdentityLookupError(err error, opts linkOIDCIdentityOptions, normalizedEmail string) error {
	var ambiguous *services.AmbiguousEmailError
	if errors.As(err, &ambiguous) {
		return fmt.Errorf(
			"email %s matches %d accounts (ids %s); retry with --id (see ovumcy users list)",
			ambiguous.Email, len(ambiguous.IDs), formatUserIDs(ambiguous.IDs),
		)
	}

	switch {
	case errors.Is(err, services.ErrOperatorUserNotFound):
		if opts.userID != 0 {
			return fmt.Errorf("no account carries id %d (see ovumcy users list)", opts.userID)
		}
		return fmt.Errorf("user %s not found", normalizedEmail)
	case errors.Is(err, services.ErrOperatorUserIDRequired):
		return errors.New("an account id is required (see ovumcy users list)")
	default:
		return fmt.Errorf("look up account: %w", err)
	}
}

// mapLinkOIDCIdentityLinkError translates OIDCLoginService.ConfirmAndLinkIdentity's
// sentinels into operator-facing wording. ErrOIDCIdentityResolveFailed (a
// storage failure resolving the identity lookup) shares the default arm
// deliberately: both wrap the same way, and giving it its own case would be
// two branches a test has to prove behave identically rather than one.
func mapLinkOIDCIdentityLinkError(err error) error {
	switch {
	case errors.Is(err, services.ErrOIDCDisabled):
		return errors.New("OIDC is not enabled on this instance (set OIDC_ENABLED=true)")
	case errors.Is(err, services.ErrOIDCLinkFailed):
		return errors.New("that (issuer, subject) pair is already linked to a different account")
	default:
		return fmt.Errorf("link oidc identity: %w", err)
	}
}
