package services

import (
	"context"
	"crypto/subtle"
	"errors"
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/ovumcy/ovumcy-web/internal/models"
	"github.com/ovumcy/ovumcy-web/internal/security"
	"github.com/pquerna/otp"
	"github.com/pquerna/otp/totp"
)

const (
	DefaultTOTPAttemptsLimit         = 5
	DefaultTOTPAttemptsWindow        = 15 * time.Minute
	DefaultTOTPDisableAttemptsLimit  = 5
	DefaultTOTPDisableAttemptsWindow = 15 * time.Minute
	// The enrollment-code budget mirrors totp.disable: both guard a check an
	// attacker can only reach with a session for one account already in hand.
	DefaultTOTPEnrollAttemptsLimit  = 5
	DefaultTOTPEnrollAttemptsWindow = 15 * time.Minute
	totpStepSeconds                 = 30
)

var (
	ErrTOTPRateLimited        = errors.New("totp rate limited")
	ErrTOTPDisableRateLimited = errors.New("totp disable rate limited")
	ErrTOTPSecretEncrypt      = errors.New("totp secret encrypt failed")
	ErrTOTPSecretDecrypt      = errors.New("totp secret decrypt failed")
	ErrTOTPUpdateFailed       = errors.New("totp update failed")
	ErrTOTPReplayed           = errors.New("totp code already used")
)

var (
	// ErrTOTPEnrollRateLimited is returned once the totp.enroll budget is spent.
	// It is returned BEFORE the code is checked, so an exhausted budget refuses
	// the correct code too.
	ErrTOTPEnrollRateLimited = errors.New("totp enroll rate limited")
	// ErrTOTPEnrollCodeInvalid is an enrollment code that does not verify
	// against the pending secret; it has drawn the totp.enroll budget.
	ErrTOTPEnrollCodeInvalid = errors.New("totp enroll code invalid")
	// ErrTOTPEnrollmentStepMissing is an EnableTOTP call whose enrollment step is
	// not positive: no enrollment code produced it, so there is no step to
	// record as spent. RFC 6238 step 0 is 1970; no real code matches it. It is
	// the shared value from internal/models, the one the repository's TOTP
	// writer raises for the same fact.
	ErrTOTPEnrollmentStepMissing = models.ErrTOTPEnrollmentStepMissing
)

// TOTPFactorVerifier answers the question a routing flag like
// MustChangePassword cannot: can THIS account's second factor actually be
// checked right now. TOTP has three states — not enrolled, enrolled and
// verifiable, enrolled but unverifiable (the stored secret does not
// decrypt, which is what a SECRET_KEY rotation leaves behind) — and every
// session-issuing path that used to read the raw TOTPEnabled column to
// decide whether to raise a 2FA challenge must consult this instead: the
// column alone cannot distinguish "checkable" from "broken," and treating
// the two alike is either a permanent lockout (routing "broken" into a
// challenge no code can ever satisfy) or a second-factor bypass (routing
// "broken" straight through). Both methods derive their answer by
// attempting the decryption on every call; neither state is stored.
// Implemented by *TOTPService.
type TOTPFactorVerifier interface {
	// Verifiable reports whether user has TOTP enrolled AND the stored
	// secret currently decrypts.
	Verifiable(user models.User) bool
	// Unverifiable reports whether user has TOTP enrolled but the stored
	// secret does NOT decrypt. Callers making an authentication decision
	// must route this case to the forced-reset escape hatch, never to a
	// 2FA challenge, and never as a silent bypass of the factor.
	Unverifiable(user models.User) bool
}

// TOTPUserRepository is the minimal repository interface required by TOTPService.
type TOTPUserRepository interface {
	// UpdateTOTPFieldsAndRevokeSessions writes the new TOTP-related columns AND
	// bumps auth_session_version in the same transaction, so toggling 2FA
	// invalidates every active auth cookie for the account. lastUsedStep is
	// written as totp_last_used_step in that same write: the step the
	// enrollment code matched on enable, 0 on disable.
	UpdateTOTPFieldsAndRevokeSessions(ctx context.Context, userID uint, expectedSessionVersion int, encryptedSecret string, enabled bool, lastUsedStep int64) error
	// UpgradeTOTPSecretCiphertextCAS rewrites just the encrypted secret column
	// WITHOUT bumping auth_session_version or touching totp_enabled — and only
	// while totp_secret still equals oldCiphertext. It exists for transparent
	// re-encryption of legacy ciphertexts under the new aad-bound format on a
	// successful 2FA login: nothing about the account's security posture
	// changed, so no active session should be revoked. applied is false when a
	// concurrent re-enrollment or disable won.
	UpgradeTOTPSecretCiphertextCAS(ctx context.Context, userID uint, oldCiphertext string, newCiphertext string) (applied bool, err error)
	// ClaimTOTPStep atomically advances totp_last_used_step to step iff it is
	// strictly greater than the persisted value. Returns true when the row was
	// updated (the step is now consumed by this caller) and false when the step
	// was already at or beyond `step` (replay or concurrent loser).
	ClaimTOTPStep(ctx context.Context, userID uint, step int64) (bool, error)
}

// aadForTOTPSecret returns the additional-authenticated-data used to bind
// an encrypted TOTP secret to a single user. The string is opaque to the
// caller and only needs to be stable across encrypt/decrypt for the same
// (purpose, user) pair. Including the user id prevents a swap of one user's
// ciphertext into another user's row from being acceptable to DecryptField.
func aadForTOTPSecret(userID uint) []byte {
	return []byte(fmt.Sprintf("ovumcy.field.totp_secret:%d", userID))
}

// TOTPService handles TOTP secret generation, enrollment, validation, and removal.
type TOTPService struct {
	users                TOTPUserRepository
	secretKey            []byte
	attemptPolicy        *AuthAttemptPolicy
	disableAttemptPolicy *AuthAttemptPolicy
	enrollAttemptPolicy  *AuthAttemptPolicy
}

// NewTOTPService creates a TOTPService. secretKey is used to encrypt TOTP secrets
// before they are written to the database. limiter is the shared AttemptLimiter;
// pass nil to use a dedicated one.
func NewTOTPService(users TOTPUserRepository, secretKey []byte, limiter *AttemptLimiter) *TOTPService {
	return &TOTPService{
		users:                users,
		secretKey:            secretKey,
		attemptPolicy:        NewAuthAttemptPolicy("totp", limiter, DefaultTOTPAttemptsLimit, DefaultTOTPAttemptsWindow),
		disableAttemptPolicy: NewAuthAttemptPolicy("totp.disable", limiter, DefaultTOTPDisableAttemptsLimit, DefaultTOTPDisableAttemptsWindow),
		enrollAttemptPolicy:  NewAuthAttemptPolicy("totp.enroll", limiter, DefaultTOTPEnrollAttemptsLimit, DefaultTOTPEnrollAttemptsWindow),
	}
}

// ConfigureEnrollAttempts sets the totp.enroll budget's limit and window, on the
// limiter the service was built with. Bootstrap applies the defaults: like
// totp.disable, the budget guards a credential check and is not operator-tunable.
func (service *TOTPService) ConfigureEnrollAttempts(attempts int, window time.Duration) {
	service.enrollAttemptPolicy.Configure(attempts, window)
}

// CheckRateLimit returns ErrTOTPRateLimited when the client or user has exceeded
// the allowed number of verification attempts within the configured window.
func (service *TOTPService) CheckRateLimit(secretKey []byte, clientKey string, userID uint, now time.Time) error {
	if service.attemptPolicy.TooManyRecent(secretKey, clientKey, strconv.FormatUint(uint64(userID), 10), now) {
		return ErrTOTPRateLimited
	}
	return nil
}

// RecordFailure records a failed verification attempt for rate-limit tracking.
func (service *TOTPService) RecordFailure(secretKey []byte, clientKey string, userID uint, now time.Time) {
	service.attemptPolicy.AddFailure(secretKey, clientKey, strconv.FormatUint(uint64(userID), 10), now)
}

// ResetAttempts clears the succeeding client's failure counter after a
// successful sign-in verification. Its one caller, the sign-in TOTP step, runs
// it only after the session is minted, so a correct code whose session could
// not be issued keeps the count. That step runs without a session, so the
// account's identity counter is left to age out (see AuthAttemptPolicy.ResetClient);
// secretKey and userID are kept so the call names the same operands as its
// check and its failure.
func (service *TOTPService) ResetAttempts(secretKey []byte, clientKey string, userID uint) {
	_, _ = secretKey, userID
	service.attemptPolicy.ResetClient(clientKey)
}

// The disable-confirmation password has no check/record/reset of its own here:
// it is drawn through DisableReauthBudget and SettingsService.VerifyReauth, the
// one re-auth path (settings_reauth_budget.go).

// GenerateSetupKey generates a new TOTP key for the given issuer and account name.
// The raw secret (key.Secret()) should be passed to VerifyEnrollmentCode during
// enrollment and then, with the step that call returned, to EnableTOTP once the
// user confirms their code.
func (service *TOTPService) GenerateSetupKey(issuer, accountName string) (*otp.Key, error) {
	return totp.Generate(totp.GenerateOpts{
		Issuer:      issuer,
		AccountName: accountName,
	})
}

// TOTPEnrollmentStep is the RFC 6238 step an enrollment confirmation code
// matched. VerifyEnrollmentCode returns it and EnableTOTP stores it as the
// account's totp_last_used_step in the write that enables the factor, so the
// confirmation code is already spent when 2FA goes live: the sign-in challenge
// refuses it as it refuses any replayed step. It is opaque to callers outside
// this package; EnableTOTP refuses the zero value.
type TOTPEnrollmentStep struct {
	step int64
}

// ValidateCode decrypts the stored TOTP secret, finds which RFC 6238 step the
// code belongs to (allowing ±1 step of clock skew), and atomically claims that
// step in the database. A replayed or concurrently-consumed step returns
// ErrTOTPReplayed so the caller can surface it separately in security logs.
// Used during the 2FA login challenge.
//
// If the persisted ciphertext was sealed by a pre-aad version of EncryptField
// (no aad binding), DecryptField returns isLegacy=true and we transparently
// re-encrypt the secret under the current aad-bound format after a
// successful step claim. The re-encryption uses a session-version-preserving
// repo call so the user's current login does not get invalidated by what is
// otherwise an internal storage upgrade. It is conditional on the ciphertext
// this call opened: a re-enrollment or disable landing in between wins, and
// the upgrade is dropped rather than restoring the old secret over the new
// one. A session minted from the row read before this call dies anyway,
// because both of those writers bump auth_session_version; a caller that
// re-reads the row before minting does not inherit that guarantee.
func (service *TOTPService) ValidateCode(ctx context.Context, userID uint, encryptedSecret, code string) (bool, error) {
	aad := aadForTOTPSecret(userID)
	rawSecret, isLegacy, err := security.DecryptField(encryptedSecret, service.secretKey, aad)
	if err != nil {
		return false, fmt.Errorf("%w: %v", ErrTOTPSecretDecrypt, err)
	}
	step, found := findValidatedTOTPStep(rawSecret, code, time.Now())
	if !found {
		return false, nil
	}
	claimed, err := service.users.ClaimTOTPStep(ctx, userID, step)
	if err != nil {
		return false, fmt.Errorf("%w: %v", ErrTOTPUpdateFailed, err)
	}
	if !claimed {
		return false, ErrTOTPReplayed
	}

	// Best-effort lazy re-encryption: failure here MUST NOT block login.
	// A failed write leaves the legacy ciphertext for the next login to
	// retry; a lost race leaves a newer secret that needs no upgrade.
	if isLegacy {
		if reEncrypted, encryptErr := security.EncryptField(rawSecret, service.secretKey, aad); encryptErr == nil {
			_, _ = service.users.UpgradeTOTPSecretCiphertextCAS(ctx, userID, encryptedSecret, reEncrypted)
		}
	}
	return true, nil
}

// findValidatedTOTPStep returns the RFC 6238 time step whose generated code
// matches the supplied code (within ±1 step of skew), and a boolean indicating
// whether a match was found. Comparison is constant-time to avoid leaking which
// step matched through timing.
func findValidatedTOTPStep(rawSecret, code string, now time.Time) (int64, bool) {
	trimmed := strings.TrimSpace(code)
	if len(trimmed) == 0 {
		return 0, false
	}
	currentStep := now.Unix() / totpStepSeconds
	for _, delta := range []int64{0, -1, +1} {
		step := currentStep + delta
		candidate, err := totp.GenerateCode(rawSecret, time.Unix(step*totpStepSeconds, 0))
		if err != nil {
			continue
		}
		if subtle.ConstantTimeCompare([]byte(candidate), []byte(trimmed)) == 1 {
			return step, true
		}
	}
	return 0, false
}

// EnableTOTP encrypts rawSecret and stores it alongside totp_enabled=true for
// the user. The ciphertext is bound to the user's id via aad so a database-
// level swap of one user's encrypted secret into another row fails to open.
// The underlying repository call also bumps auth_session_version so every
// active auth cookie issued before 2FA was enabled is revoked.
//
// enrollment is the step VerifyEnrollmentCode matched; the same write records
// it as totp_last_used_step, so the code that confirmed enrollment cannot pass
// the first sign-in challenge after it. A step that is not positive (the zero
// value) is refused with ErrTOTPEnrollmentStepMissing before anything is
// written: enabling with it would leave the confirmation code replayable at the
// first sign-in.
//
// expectedSessionVersion is the AuthSessionVersion of the session that proved
// the enrollment code. The write happens only from that version: an account
// revoked by another write in between is left untouched and the result is
// ErrAuthSessionVersionChanged, so the caller never re-issues a session that
// would outlive that revocation.
func (service *TOTPService) EnableTOTP(ctx context.Context, userID uint, expectedSessionVersion int, rawSecret string, enrollment TOTPEnrollmentStep) error {
	if enrollment.step <= 0 {
		return ErrTOTPEnrollmentStepMissing
	}
	encrypted, err := security.EncryptField(rawSecret, service.secretKey, aadForTOTPSecret(userID))
	if err != nil {
		return fmt.Errorf("%w: %v", ErrTOTPSecretEncrypt, err)
	}
	if err := service.users.UpdateTOTPFieldsAndRevokeSessions(ctx, userID, NormalizeAuthSessionVersion(expectedSessionVersion), encrypted, true, enrollment.step); err != nil {
		if errors.Is(err, ErrAuthSessionVersionChanged) {
			return ErrAuthSessionVersionChanged
		}
		return fmt.Errorf("%w: %v", ErrTOTPUpdateFailed, err)
	}
	return nil
}

// DisableTOTP clears the TOTP secret and sets totp_enabled=false for the user.
// As with EnableTOTP, this bumps auth_session_version so any session that
// existed while 2FA was on is invalidated when 2FA is taken back off. The write
// happens only from expectedSessionVersion, as in EnableTOTP. The replay floor
// returns to 0: with the secret gone there is no step left to claim, and the
// next enrollment sets its own.
func (service *TOTPService) DisableTOTP(ctx context.Context, userID uint, expectedSessionVersion int) error {
	if err := service.users.UpdateTOTPFieldsAndRevokeSessions(ctx, userID, NormalizeAuthSessionVersion(expectedSessionVersion), "", false, 0); err != nil {
		if errors.Is(err, ErrAuthSessionVersionChanged) {
			return ErrAuthSessionVersionChanged
		}
		return fmt.Errorf("%w: %v", ErrTOTPUpdateFailed, err)
	}
	return nil
}

// totpFactorState derives the ternary TOTP state for user by attempting the
// same decryption ValidateCode performs, without claiming a step or
// touching the database: enrolled reports the raw totp_enabled column, and
// verifiable is only meaningful (and only true) when enrolled is true. An
// empty stored secret on an enrolled account (should not happen through
// EnableTOTP/DisableTOTP, which always write both columns together, but is
// not assumed impossible here) is treated as unverifiable rather than
// panicking or reporting verifiable.
func (service *TOTPService) totpFactorState(user models.User) (enrolled bool, verifiable bool) {
	if !user.TOTPEnabled {
		return false, false
	}
	if user.TOTPSecret == "" {
		return true, false
	}
	_, _, err := security.DecryptField(user.TOTPSecret, service.secretKey, aadForTOTPSecret(user.ID))
	return true, err == nil
}

// Verifiable implements TOTPFactorVerifier.
func (service *TOTPService) Verifiable(user models.User) bool {
	_, verifiable := service.totpFactorState(user)
	return verifiable
}

// Unverifiable implements TOTPFactorVerifier.
func (service *TOTPService) Unverifiable(user models.User) bool {
	enrolled, verifiable := service.totpFactorState(user)
	return enrolled && !verifiable
}
