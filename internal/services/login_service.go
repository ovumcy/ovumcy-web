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

// ResetAttempts forgives the failures of the client that just signed in. The
// caller runs it only after the cookie that carries the sign-in onward — the
// session, the TOTP-pending or the forced-reset cookie — has been issued, so a
// correct password whose sign-in then failed keeps the count it found.
// clientKey must be the one Authenticate was given. Password sign-in runs
// without a session, so only the client bucket is cleared and the account's
// identity bucket is left to age out (see AuthAttemptPolicy.ResetClient).
func (service *LoginService) ResetAttempts(clientKey string) {
	service.attemptPolicy.ResetClient(clientKey)
}

// Authenticate checks the password and routes the sign-in. It never resets
// the attempt budget, even on success: that is ResetAttempts, run by the
// caller once the sign-in has landed.
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
	// The attempt is reserved BEFORE the compare and stays booked if the compare
	// fails: a check that only reads the count would let every request that
	// arrives during the compare (a full bcrypt) pass the same count.
	reservation, admitted := service.attemptPolicy.Reserve(secretKey, clientKey, normalizedEmail, now)
	if !admitted {
		return LoginResult{}, ErrAuthLoginRateLimited
	}
	// An address that normalizes to nothing names no identity bucket, so the
	// attempt would be budgeted by client alone; it can also name no account.
	// Refuse it as a failed credential before any lookup.
	if normalizedEmail == "" {
		return LoginResult{}, ErrAuthInvalidCreds
	}

	// The lookup gets the SAME normalized address the budget is keyed on, so
	// the account a success is booked against is the one the failures were.
	user, err := service.auth.AuthenticateCredentials(ctx, normalizedEmail, password)
	if err != nil {
		if !errors.Is(err, ErrAuthInvalidCreds) {
			// Not a verdict on the password (a lookup or storage error): the
			// attempt is not a failure.
			reservation.Refund()
		}
		return LoginResult{}, err
	}
	// The password is correct: the reservation was never a failure. The count
	// goes back to what it was; the budget is still only reset by the caller
	// once the sign-in has landed.
	reservation.Refund()

	// A correct password does not reset the budget here: the sign-in can
	// still fail below (the reset token) and in the caller (the cookie that
	// carries it onward). The caller resets through ResetAttempts once that
	// cookie is issued.
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
		// has verified a LOCAL password, and the plain login route is
		// Authenticate's only caller (WEB-77 removed the other one, OIDC
		// link-confirm's own password challenge), so the token always
		// carries the forced-from-local purpose, never forced-from-OIDC. See
		// PasswordResetTokenPurposeForcedLocal.
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
