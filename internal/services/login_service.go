package services

import (
	"context"
	"errors"
	"time"

	"github.com/ovumcy/ovumcy-web/internal/models"
)

var ErrLoginResetTokenIssue = errors.New("login reset token issue")
var ErrAuthLoginRateLimited = errors.New("auth login rate limited")

type LoginAuthService interface {
	AuthenticateCredentials(ctx context.Context, email string, password string) (models.User, error)
}

type LoginResetTokenIssuer interface {
	IssueResetTokenForUser(secretKey []byte, user *models.User, purpose string, ttl time.Duration, now time.Time) (string, error)
}

type LoginService struct {
	auth          LoginAuthService
	reset         LoginResetTokenIssuer
	attemptPolicy *AuthAttemptPolicy
	// totp is consulted instead of the raw TOTPEnabled column so an
	// enrolled-but-unverifiable secret is never treated as "no second
	// factor" or "second factor, forever." Nil in tests that construct a
	// LoginService directly and never call SetTOTPVerifier: Authenticate
	// then falls back to the pre-existing TOTPEnabled-only check, which is
	// exactly today's behaviour for those tests. Production always wires a
	// real *TOTPService via SetTOTPVerifier in bootstrap.
	totp TOTPFactorVerifier
}

type LoginResult struct {
	User                  models.User
	RequiresPasswordReset bool
	ResetToken            string
	RequiresTOTP          bool
}

func NewLoginService(auth LoginAuthService, reset LoginResetTokenIssuer, limiter *AttemptLimiter) *LoginService {
	return &LoginService{
		auth:          auth,
		reset:         reset,
		attemptPolicy: NewAuthAttemptPolicy("login", limiter, DefaultLoginAttemptsLimit, DefaultLoginAttemptsWindow),
	}
}

func (service *LoginService) ConfigureAttemptLimits(attempts int, window time.Duration) {
	service.attemptPolicy.Configure(attempts, window)
}

// SetTOTPVerifier wires the derived TOTP-verifiability check Authenticate
// uses to decide between raising a 2FA challenge and routing into the
// forced-reset escape hatch. Called once from bootstrap with the same
// *TOTPService the 2FA handlers use, after both are constructed.
func (service *LoginService) SetTOTPVerifier(verifier TOTPFactorVerifier) {
	service.totp = verifier
}

func (service *LoginService) Authenticate(
	ctx context.Context,
	secretKey []byte,
	clientKey string,
	email string,
	password string,
	resetTokenTTL time.Duration,
	now time.Time,
) (LoginResult, error) {
	normalizedEmail := NormalizeAuthEmail(email)
	if service.attemptPolicy.TooManyRecent(secretKey, clientKey, normalizedEmail, now) {
		return LoginResult{}, ErrAuthLoginRateLimited
	}

	user, err := service.auth.AuthenticateCredentials(ctx, email, password)
	if err != nil {
		if errors.Is(err, ErrAuthInvalidCreds) {
			service.attemptPolicy.AddFailure(secretKey, clientKey, normalizedEmail, now)
		}
		return LoginResult{}, err
	}

	service.attemptPolicy.Reset(secretKey, clientKey, normalizedEmail)
	result := LoginResult{User: user}

	// MustChangePassword is a routing flag an operator sets out of band
	// (`ovumcy reset-password <email>`) and outranks TOTP unconditionally —
	// it answers "where does this request go next," not "is the second
	// factor checkable."
	//
	// totpUnverifiable answers the second question directly: the account is
	// enrolled in TOTP but its stored secret does not decrypt (a SECRET_KEY
	// rotation is the common cause), so no code the authenticator produces
	// can ever satisfy a 2FA challenge for it. Routing that account into the
	// challenge would be a permanent lockout; skipping the factor silently
	// would be a bypass. The forced-reset flow is the sanctioned escape
	// hatch for both reasons, so it is chosen here because the factor is
	// unverifiable — independently of whether an operator has also flagged
	// the account — not only when the routing flag happens to be set.
	totpUnverifiable := user.TOTPEnabled && service.totp != nil && service.totp.Unverifiable(user)

	if user.MustChangePassword || totpUnverifiable {
		// Authenticate only reaches this branch after AuthenticateCredentials
		// has verified a LOCAL password. Both callers that reach here through
		// it — the plain login route and OIDC link-confirm's own password
		// challenge — are therefore local authentications, so the token
		// always carries the forced-from-local purpose, never
		// forced-from-OIDC. See PasswordResetTokenPurposeForcedLocal.
		token, err := service.reset.IssueResetTokenForUser(secretKey, &user, PasswordResetTokenPurposeForcedLocal, resetTokenTTL, now)
		if err != nil {
			return LoginResult{}, ErrLoginResetTokenIssue
		}
		result.RequiresPasswordReset = true
		result.ResetToken = token
		return result, nil
	}

	// Require the factor everywhere it is verifiable: reaching here means
	// TOTP is either not enrolled, or enrolled and verifiable.
	if user.TOTPEnabled {
		result.RequiresTOTP = true
	}
	return result, nil
}
