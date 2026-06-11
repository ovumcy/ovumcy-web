// Package apideps holds the dependency contract of the HTTP layer: the
// Dependencies aggregate that internal/api consumes and the workflow-service
// ports it depends on. It lives outside internal/api so the composition layer
// (internal/bootstrap) can construct Dependencies without importing
// internal/api, which would otherwise create an import cycle when the api test
// helpers reuse the shared wiring. apideps deliberately imports only lower
// layers (services, models, security) and never internal/db, preserving the
// rule that internal/api stays free of any internal/db dependency.
package apideps

import (
	"context"
	"time"

	"github.com/ovumcy/ovumcy-web/internal/models"
	"github.com/ovumcy/ovumcy-web/internal/security"
	"github.com/ovumcy/ovumcy-web/internal/services"
)

type RegistrationWorkflowService interface {
	RegisterOwnerAccount(ctx context.Context, email string, rawPassword string, confirmPassword string, createdAt time.Time) (models.User, string, error)
	RegistrationOpen() bool
}

type LoginWorkflowService interface {
	Authenticate(ctx context.Context, secretKey []byte, clientKey string, email string, password string, resetTokenTTL time.Duration, now time.Time) (services.LoginResult, error)
}

// RegisterPickupTokenStore persists and atomically consumes the nonces that
// back the sealed `ovumcy_register_pickup` cookie. The interface lets tests
// substitute an in-memory implementation without spinning up a database.
type RegisterPickupTokenStore interface {
	Issue(ctx context.Context, nonce string, userID uint, expiresAt time.Time) error
	Consume(ctx context.Context, nonce string, now time.Time) (uint, bool, error)
}

type OIDCWorkflowService interface {
	Enabled() bool
	LocalPublicAuthEnabled() bool
	StartAuth(ctx context.Context, state string, nonce string, codeVerifier string) (string, error)
	StartReauth(ctx context.Context, state string, nonce string, codeVerifier string) (string, error)
	Authenticate(ctx context.Context, code string, codeVerifier string, expectedNonce string, now time.Time) (services.OIDCLoginResult, error)
	ValidateReauthExchange(ctx context.Context, code string, codeVerifier string, expectedNonce string, expectedUserID uint, maxAuthAge time.Duration, now time.Time) error
	ConfirmAndLinkIdentity(ctx context.Context, targetUserID uint, claims security.OIDCClaims, linkTime time.Time) error
}
