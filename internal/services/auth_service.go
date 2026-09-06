package services

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/ovumcy/ovumcy-web/internal/models"
	"golang.org/x/crypto/bcrypt"
)

// ErrResetTokenAlreadyConsumed is the shared value from internal/models, not a
// second one with the same text. It is raised by the repository's CAS update
// and reaches callers here unchanged, and this package cannot import
// internal/db — a redeclaration would leave errors.Is false across the layer
// boundary for the very error being tested for.
var ErrResetTokenAlreadyConsumed = models.ErrResetTokenAlreadyConsumed

var (
	ErrRecoveryCodeNotFound = errors.New("recovery code not found")
	ErrInvalidResetToken    = errors.New("invalid reset token")
	ErrAuthUserRequired     = errors.New("auth user is required")
	ErrAuthUserNotFound     = errors.New("auth user not found")
	ErrAuthUserLookupFailed = errors.New("auth user lookup failed")
	ErrRecoveryCodeGenerate = errors.New("recovery code generation failed")
	ErrRecoveryCodeUpdate   = errors.New("recovery code update failed")
	ErrAuthRegisterInvalid  = errors.New("auth register invalid input")
	ErrAuthEmailExists      = errors.New("auth email already exists")
	ErrAuthRegisterFailed   = errors.New("auth register failed")
	ErrAuthPasswordMismatch = errors.New("auth register password mismatch")
	ErrAuthWeakPassword     = errors.New("auth register weak password")
	ErrAuthPasswordTooLong  = errors.New("auth register password too long")
	ErrAuthInvalidCreds     = errors.New("auth invalid credentials")
	ErrAuthResetInvalid     = errors.New("auth reset invalid input")
	ErrAuthPasswordHash     = errors.New("auth password hash failed")
	ErrAuthPasswordUpdate   = errors.New("auth password update failed")
	// ErrAuthUserIDRequired mirrors ErrOperatorUserIDRequired: an id-addressed
	// reset with no id is invalid input, the same way a blank email is, never a
	// reason to fall through to a lookup that would compare a zero id against
	// nothing and match nothing meaningful.
	ErrAuthUserIDRequired = errors.New("auth user id is required")
)

// recoveryCodeTimingEqualizationHash and credentialsTimingEqualizationHash are
// fixed placeholder hashes used by the equalize* helpers below to spend bcrypt
// compute time on the early-return paths in recovery and login. They are never
// compared against a real credential — the result of
// bcrypt.CompareHashAndPassword is discarded — and never authenticate anyone.
// Their embedded cost MUST stay equal to passwordHashCost (test-pinned): a
// cheaper placeholder would make the equalized paths measurably faster than a
// real comparison and reintroduce the account-enumeration timing oracle.
const recoveryCodeTimingEqualizationHash = "$2a$12$KeFGg3nMPoiaOcsZpE9qUevfmpFV3VlY5cAQ.8FazuuHUIgnQrBwS" // #nosec G101 -- fixed placeholder bcrypt hash, see comment above; never authenticates a real user
const credentialsTimingEqualizationHash = "$2a$12$pI5aDx1kby9ZEk9.2NzhBeq77y41xgUaCrP/vyyRCgdGnvaV.UxZm"  // #nosec G101 -- fixed placeholder bcrypt hash, see comment on recoveryCodeTimingEqualizationHash

type AuthUserRepository interface {
	ExistsByNormalizedEmail(ctx context.Context, email string) (bool, error)
	FindByNormalizedEmail(ctx context.Context, email string) (models.User, error)
	FindByNormalizedEmailOptional(ctx context.Context, email string) (models.User, bool, error)
	FindByID(ctx context.Context, userID uint) (models.User, error)
	FindByIDOptional(ctx context.Context, userID uint) (models.User, bool, error)
	Create(ctx context.Context, user *models.User) error
	UpdateRecoveryCodeHashAndRevokeSessions(ctx context.Context, userID uint, recoveryHash string) error
	UpdatePasswordAndRevokeSessions(ctx context.Context, userID uint, passwordHash string, mustChangePassword bool) error
	// ForceResetPasswordAndRevokeSessions is the operator-reset variant: it
	// rewrites the password, forces change-on-next-login, bumps the session
	// version, AND force-clears the calendar-feed token in one atomic update
	// (feed-clear arm of the force-rotate-on-recovery rule). Distinct from the
	// routine UpdatePasswordAndRevokeSessions, which must NOT touch the feed.
	ForceResetPasswordAndRevokeSessions(ctx context.Context, userID uint, passwordHash string) error
	UpdatePasswordRecoveryCodeAndRevokeSessions(ctx context.Context, userID uint, passwordHash string, recoveryHash string, mustChangePassword bool) error
	UpdatePasswordRecoveryCodeAndRevokeSessionsCAS(ctx context.Context, userID uint, oldPasswordHash string, newPasswordHash string, recoveryHash string) error
	// UpdatePasswordHashOnly rewrites password_hash WITHOUT bumping
	// auth_session_version — a transparent storage-format upgrade (bcrypt cost
	// rise), not a credential change. Used only by the opportunistic rehash on
	// successful login; the caller has already proven the password.
	UpdatePasswordHashOnly(ctx context.Context, userID uint, passwordHash string) error
	BumpAuthSessionVersion(ctx context.Context, userID uint) error
	// ClaimRecoveryCodeReveal atomically consumes the account's one-time
	// recovery-code reveal, returning true only for the call that consumed it.
	// Every UPDATE that mints a recovery code re-arms it in the same statement
	// (migration 036).
	ClaimRecoveryCodeReveal(ctx context.Context, userID uint, revealedAt time.Time) (bool, error)
}

const (
	DefaultLogoutAttemptsLimit  = 20
	DefaultLogoutAttemptsWindow = 15 * time.Minute
)

// passwordHashCost is the bcrypt cost used for every password and recovery-code
// hash written by this package. It is deliberately higher than
// bcrypt.DefaultCost (10) to widen the offline-guessing margin if the database
// and SECRET_KEY ever leak together. Successful logins opportunistically
// rehash any stored password below this cost (see AuthenticateCredentials), so
// the effective floor rises without forcing a reset. Raising this value is a
// per-hash CPU trade-off: bcrypt work doubles per +1.
const passwordHashCost = 12

type AuthService struct {
	users               AuthUserRepository
	logoutAttemptPolicy *AuthAttemptPolicy
}

func NewAuthService(users AuthUserRepository) *AuthService {
	return &AuthService{
		users:               users,
		logoutAttemptPolicy: NewAuthAttemptPolicy("logout", nil, DefaultLogoutAttemptsLimit, DefaultLogoutAttemptsWindow),
	}
}

func (service *AuthService) ConfigureLogoutAttemptLimits(attempts int, window time.Duration) {
	service.logoutAttemptPolicy.Configure(attempts, window)
}

// CheckAndRecordLogoutAttempt returns true if the per-account logout rate limit is
// exceeded for this (clientKey, identity) pair. If not exceeded, it also records
// the attempt so subsequent calls count it toward the window.
func (service *AuthService) CheckAndRecordLogoutAttempt(secretKey []byte, clientKey string, identity string, now time.Time) bool {
	if service.logoutAttemptPolicy.TooManyRecent(secretKey, clientKey, identity, now) {
		return true
	}
	service.logoutAttemptPolicy.AddFailure(secretKey, clientKey, identity, now)
	return false
}

func (service *AuthService) RegistrationEmailExists(ctx context.Context, email string) (bool, error) {
	return service.users.ExistsByNormalizedEmail(ctx, email)
}

func (service *AuthService) CreateUser(ctx context.Context, user *models.User) error {
	return service.users.Create(ctx, user)
}

func (service *AuthService) FindByNormalizedEmail(ctx context.Context, email string) (models.User, error) {
	return service.users.FindByNormalizedEmail(ctx, email)
}

func (service *AuthService) FindByID(ctx context.Context, userID uint) (models.User, error) {
	return service.users.FindByID(ctx, userID)
}

// authPasswordPolicyError translates the shared password-policy verdict into
// this layer's sentinels. The too-long refusal keeps its own sentinel the whole
// way to the owner: collapsing it back into ErrAuthWeakPassword here would undo
// the split at the first hop and hand the transport a single spec key again.
func authPasswordPolicyError(err error) error {
	switch {
	case err == nil:
		return nil
	case errors.Is(err, ErrPasswordTooLong):
		return ErrAuthPasswordTooLong
	default:
		return ErrAuthWeakPassword
	}
}

func (service *AuthService) ValidateRegistrationCredentials(password string, confirmPassword string) error {
	password = strings.TrimSpace(password)
	confirmPassword = strings.TrimSpace(confirmPassword)

	if password == "" || confirmPassword == "" {
		return ErrAuthRegisterInvalid
	}
	if password != confirmPassword {
		return ErrAuthPasswordMismatch
	}
	if err := authPasswordPolicyError(ValidatePasswordStrength(password)); err != nil {
		return err
	}
	return nil
}

func (service *AuthService) RegisterOwner(ctx context.Context, email string, rawPassword string, confirmPassword string, createdAt time.Time) (models.User, string, error) {
	if err := service.ValidateRegistrationCredentials(rawPassword, confirmPassword); err != nil {
		return models.User{}, "", err
	}

	exists, err := service.RegistrationEmailExists(ctx, email)
	if err != nil {
		return models.User{}, "", ErrAuthRegisterFailed
	}
	if exists {
		// Spend the same bcrypt time as the new-account branch so a duplicate-email probe
		// cannot be distinguished from a fresh email by POST /api/v1/users response
		// latency. BuildOwnerUserWithRecovery runs two bcrypt operations (password hash +
		// recovery code hash), so equalize against both placeholders.
		equalizeRegistrationTiming(rawPassword)
		return models.User{}, "", ErrAuthEmailExists
	}

	user, recoveryCode, err := service.BuildOwnerUserWithRecovery(email, rawPassword, createdAt)
	if err != nil {
		return models.User{}, "", ErrAuthRegisterFailed
	}

	return user, recoveryCode, nil
}

func (service *AuthService) ValidateResetPasswordInput(password string, confirmPassword string) error {
	password = strings.TrimSpace(password)
	confirmPassword = strings.TrimSpace(confirmPassword)

	if password == "" || confirmPassword == "" {
		return ErrAuthResetInvalid
	}
	if password != confirmPassword {
		return ErrAuthPasswordMismatch
	}
	if err := authPasswordPolicyError(ValidatePasswordStrength(password)); err != nil {
		return err
	}
	return nil
}

// NormalizeForcedResetPassword is the operator-forced reset's whole password
// rule — the trim AND the policy — in one exported call that returns the exact
// string ForceResetPasswordByID will hash.
//
// It is exported because the `ovumcy reset-password` CLI has to apply the rule
// BEFORE it spends the calendar-feed fence's one-shot confirmation, and a
// second, hand-written copy of the rule there does not stay equal to this one:
// a value the CLI trimmed differently is a value it checked and did not submit.
// A trailing space is enough — " Passwd1 " passes a check on the raw bytes and
// is refused by the policy once trimmed, and 71 characters plus two spaces are
// refused as over the byte limit while the trimmed value is inside it. Callers
// pass the returned string on to ForceResetPasswordByID, which applies this
// same function again; it is idempotent, so the second pass changes nothing.
func NormalizeForcedResetPassword(raw string) (string, error) {
	newPassword := strings.TrimSpace(raw)
	if newPassword == "" {
		return "", ErrAuthResetInvalid
	}
	if err := authPasswordPolicyError(ValidatePasswordStrength(newPassword)); err != nil {
		return "", err
	}
	return newPassword, nil
}

// ForceResetPasswordByID is the operator-forced reset: it applies the same
// password policy the owner-facing paths do, then rewrites the hash, sets
// must_change_password, re-enables local auth, clears the calendar-feed token
// and bumps auth_session_version in one atomic update (forceResetPassword
// below). It is addressed by id alone, because the id `users list` prints is
// the only handle that reaches EVERY row: a legacy row the strict
// NormalizeAuthEmail rule refuses, or one on a mailbox it shares with another
// account, cannot be reached by any address-taking form at all. Its one
// caller, the `ovumcy reset-password` CLI command, resolves the operator's
// address to that id through OperatorUserService first, so an ambiguous
// address is refused there rather than acted on here.
func (service *AuthService) ForceResetPasswordByID(ctx context.Context, userID uint, newPassword string) error {
	if userID == 0 {
		return ErrAuthUserIDRequired
	}
	newPassword, err := NormalizeForcedResetPassword(newPassword)
	if err != nil {
		return err
	}

	user, found, err := service.users.FindByIDOptional(ctx, userID)
	if err != nil {
		return fmt.Errorf("%w: %v", ErrAuthUserLookupFailed, err)
	}
	if !found {
		return ErrAuthUserNotFound
	}

	return service.forceResetPassword(ctx, user.ID, newPassword)
}

// forceResetPassword is the write shared by both addressing forms: hash the
// new password and apply the operator-reset update. Operator reset
// (compromise/lockout recovery) rewrites the password, forces
// change-on-next-login, revokes sessions, and force-clears the calendar-feed
// token in one atomic update. A ROUTINE authenticated change uses
// UpdatePasswordAndRevokeSessions and keeps the feed (manual rotate only).
func (service *AuthService) forceResetPassword(ctx context.Context, userID uint, newPassword string) error {
	passwordHash, err := bcrypt.GenerateFromPassword([]byte(newPassword), passwordHashCost)
	if err != nil {
		return fmt.Errorf("%w: %v", ErrAuthPasswordHash, err)
	}

	if err := service.users.ForceResetPasswordAndRevokeSessions(ctx, userID, string(passwordHash)); err != nil {
		return fmt.Errorf("%w: %v", ErrAuthPasswordUpdate, err)
	}

	return nil
}

func (service *AuthService) BuildOwnerUserWithRecovery(email string, rawPassword string, createdAt time.Time) (models.User, string, error) {
	passwordHash, err := bcrypt.GenerateFromPassword([]byte(rawPassword), passwordHashCost)
	if err != nil {
		return models.User{}, "", err
	}
	recoveryCode, recoveryHash, err := GenerateRecoveryCodeHash()
	if err != nil {
		return models.User{}, "", err
	}
	if createdAt.IsZero() {
		createdAt = time.Now().UTC()
	}

	user := models.User{
		Email:              email,
		PasswordHash:       string(passwordHash),
		RecoveryCodeHash:   recoveryHash,
		LocalAuthEnabled:   true,
		AuthSessionVersion: 1,
		Role:               models.RoleOwner,
		CycleLength:        models.DefaultCycleLength,
		PeriodLength:       models.DefaultPeriodLength,
		AutoPeriodFill:     models.DefaultAutoPeriodFill,
		CreatedAt:          createdAt,
	}
	return user, recoveryCode, nil
}

func (service *AuthService) BuildOIDCOwnerUser(email string, createdAt time.Time) (models.User, error) {
	if createdAt.IsZero() {
		createdAt = time.Now().UTC()
	}

	return models.User{
		Email:              email,
		PasswordHash:       "",
		RecoveryCodeHash:   "",
		LocalAuthEnabled:   false,
		AuthSessionVersion: 1,
		Role:               models.RoleOwner,
		CycleLength:        models.DefaultCycleLength,
		PeriodLength:       models.DefaultPeriodLength,
		AutoPeriodFill:     models.DefaultAutoPeriodFill,
		CreatedAt:          createdAt,
	}, nil
}

func (service *AuthService) AuthenticateCredentials(ctx context.Context, email string, password string) (models.User, error) {
	user, err := service.users.FindByNormalizedEmail(ctx, email)
	if err != nil {
		equalizeAuthCredentialsTiming(password)
		return models.User{}, ErrAuthInvalidCreds
	}
	if !user.LocalAuthEnabled || strings.TrimSpace(user.PasswordHash) == "" {
		equalizeAuthCredentialsTiming(password)
		return models.User{}, ErrAuthInvalidCreds
	}
	if bcrypt.CompareHashAndPassword([]byte(user.PasswordHash), []byte(password)) != nil {
		// The comparison above spends only the work the STORED hash carries.
		// An account whose hash predates the current cost is therefore rejected
		// measurably faster than an address with no account, which pays a full
		// passwordHashCost comparison in the equalized branches above — the
		// account-enumeration oracle in reverse. Buy the difference here.
		topUpAuthCredentialsTiming(user.PasswordHash, password)
		return models.User{}, ErrAuthInvalidCreds
	}
	if err := ValidateSupportedWebUser(&user); err != nil {
		return models.User{}, err
	}
	service.rehashPasswordIfStale(ctx, &user, password)
	return user, nil
}

// rehashPasswordIfStale opportunistically re-hashes a valid password whose
// stored bcrypt cost is below passwordHashCost, so the effective cost floor
// rises for pre-existing accounts without forcing a reset. It runs only after
// a successful CompareHashAndPassword (the plaintext is proven), rewrites the
// hash in place via UpdatePasswordHashOnly (no auth_session_version bump — the
// credential itself is unchanged), and mutates user.PasswordHash so a caller
// that persists the struct sees the upgraded hash.
//
// Best-effort by design: a costing/read error or a failed write is swallowed
// so it can never turn a valid login into a failure. On the next login the
// upgrade is simply retried.
func (service *AuthService) rehashPasswordIfStale(ctx context.Context, user *models.User, password string) {
	if user == nil || user.ID == 0 {
		return
	}
	cost, err := bcrypt.Cost([]byte(user.PasswordHash))
	if err != nil || cost >= passwordHashCost {
		return
	}
	upgraded, err := bcrypt.GenerateFromPassword([]byte(password), passwordHashCost)
	if err != nil {
		return
	}
	if err := service.users.UpdatePasswordHashOnly(ctx, user.ID, string(upgraded)); err != nil {
		return
	}
	user.PasswordHash = string(upgraded)
}

// FindUserByEmailRecoveryCodeAndPassword resolves the account behind a recovery
// reset. It requires BOTH the account's current password and its recovery code:
// the code is the substitute for the second factor, never for the first, so one
// captured secret can no longer rewrite the password and mint a session.
//
// Every rejection returns the one ErrRecoveryCodeNotFound, and every rejection
// spends the same bcrypt work as every OTHER rejection — two comparisons at
// passwordHashCost. Where the row exists and its stored hashes were minted
// below that cost, topUpRecoveryLookupTiming buys the shortfall back, so a
// wrong code costs what an address with no account at all costs. A SUCCESS is
// deliberately not held to that total: it runs no top-up, so an account still
// carrying pre-raise hashes verifies more cheaply than it is rejected. Nothing
// leaks by that — reaching the success path already requires both secrets.
//
// The operands are therefore never short-circuited against each other: when the row
// exists, both comparisons run and only their combined result decides, and when
// it does not, equalizeRecoveryCodeLookupTiming spends both against the fixed
// placeholders. A password compare skipped after a failed code compare (or the
// reverse) would be a timing oracle telling an attacker which of the two
// secrets they got right — and, through the code operand, whether the account
// exists at all.
func (service *AuthService) FindUserByEmailRecoveryCodeAndPassword(ctx context.Context, email string, code string, password string) (*models.User, error) {
	normalizedEmail := NormalizeAuthEmail(email)
	if normalizedEmail == "" {
		return nil, ErrRecoveryCodeNotFound
	}
	user, found, err := service.users.FindByNormalizedEmailOptional(ctx, normalizedEmail)
	if err != nil {
		return nil, err
	}
	if !found {
		equalizeRecoveryCodeLookupTiming(code, password)
		return nil, ErrRecoveryCodeNotFound
	}

	hash := strings.TrimSpace(user.RecoveryCodeHash)
	passwordHash := strings.TrimSpace(user.PasswordHash)
	if !user.LocalAuthEnabled || hash == "" || passwordHash == "" {
		equalizeRecoveryCodeLookupTiming(code, password)
		return nil, ErrRecoveryCodeNotFound
	}
	recoveryCodeMatches := bcrypt.CompareHashAndPassword([]byte(hash), []byte(NormalizeRecoveryCode(code))) == nil
	passwordMatches := bcrypt.CompareHashAndPassword([]byte(passwordHash), []byte(password)) == nil
	if !recoveryCodeMatches || !passwordMatches {
		// Both comparisons ran, but each spent only the work ITS stored hash
		// carries, while equalizeRecoveryCodeLookupTiming above always spends
		// two passwordHashCost comparisons. An account holding hashes minted
		// before the current cost would answer faster than "no such address" —
		// and unlike passwords, a recovery-code hash is never re-minted on a
		// successful use, so a stale cost stays stale. Buy the difference.
		topUpRecoveryLookupTiming(hash, code, passwordHash, password)
		return nil, ErrRecoveryCodeNotFound
	}
	if err := ValidateSupportedWebUser(&user); err != nil {
		return nil, ErrRecoveryCodeNotFound
	}
	return &user, nil
}

// VerifyStoredRecoveryCode reports whether a presented recovery code matches a
// stored bcrypt recovery-code hash. The code is compared exactly as presented:
// the register-pickup flow feeds the server-minted value carried by its sealed
// payload, so normalizing here would only widen what a payload can match.
// User-typed recovery input goes through the normalizing email+code lookup
// above instead. A malformed or empty hash fails closed.
func (service *AuthService) VerifyStoredRecoveryCode(hash string, code string) bool {
	return bcrypt.CompareHashAndPassword([]byte(hash), []byte(code)) == nil
}

// BuildPasswordResetToken forwards to the package-level builder. storedHash is
// the account's bcrypt hash from the row — every caller passes user.PasswordHash
// — and it is fingerprinted, not re-hashed. See PasswordStateFingerprint.
// purpose must be one of the PasswordResetTokenPurpose* constants.
func (service *AuthService) BuildPasswordResetToken(secretKey []byte, userID uint, storedHash string, purpose string, ttl time.Duration, now time.Time) (string, error) {
	return BuildPasswordResetToken(secretKey, userID, storedHash, purpose, ttl, now)
}

func (service *AuthService) BuildAuthSessionTokenWithSessionID(secretKey []byte, userID uint, role string, sessionVersion int, ttl time.Duration, now time.Time) (string, string, error) {
	return BuildAuthSessionTokenWithVersionAndSessionID(secretKey, userID, role, sessionVersion, ttl, now)
}

func (service *AuthService) ResolveUserByAuthSessionToken(ctx context.Context, secretKey []byte, rawToken string, now time.Time) (*models.User, error) {
	user, _, err := service.ResolveAuthSession(ctx, secretKey, rawToken, now)
	return user, err
}

func (service *AuthService) ResolveAuthSession(ctx context.Context, secretKey []byte, rawToken string, now time.Time) (*models.User, *AuthSessionClaims, error) {
	claims, err := ParseAuthSessionToken(secretKey, rawToken, now)
	if err != nil {
		return nil, nil, err
	}

	user, err := service.users.FindByID(ctx, claims.UserID)
	if err != nil {
		return nil, nil, ErrAuthInvalidCreds
	}
	if user.MustChangePassword {
		return nil, nil, ErrAuthSessionTokenRevoked
	}
	if NormalizeAuthSessionVersion(claims.SessionVersion) != NormalizeAuthSessionVersion(user.AuthSessionVersion) {
		return nil, nil, ErrAuthSessionTokenRevoked
	}
	if err := ValidateSupportedWebUser(&user); err != nil {
		return nil, nil, err
	}
	return &user, claims, nil
}

func (service *AuthService) ResolveUserByResetToken(ctx context.Context, secretKey []byte, rawToken string, now time.Time) (*models.User, error) {
	claims, err := ParsePasswordResetToken(secretKey, rawToken, now)
	if err != nil {
		return nil, ErrInvalidResetToken
	}

	user, err := service.users.FindByID(ctx, claims.UserID)
	if err != nil {
		return nil, ErrInvalidResetToken
	}
	if !user.LocalAuthEnabled || strings.TrimSpace(user.PasswordHash) == "" {
		return nil, ErrInvalidResetToken
	}
	if !IsPasswordStateFingerprintMatch(claims.PasswordState, user.PasswordHash) {
		return nil, ErrInvalidResetToken
	}
	if err := ValidateSupportedWebUser(&user); err != nil {
		return nil, ErrInvalidResetToken
	}
	return &user, nil
}

func (service *AuthService) RegenerateRecoveryCode(ctx context.Context, userID uint) (string, error) {
	recoveryCode, recoveryHash, err := GenerateRecoveryCodeHash()
	if err != nil {
		return "", fmt.Errorf("%w: %v", ErrRecoveryCodeGenerate, err)
	}
	if err := service.users.UpdateRecoveryCodeHashAndRevokeSessions(ctx, userID, recoveryHash); err != nil {
		return "", fmt.Errorf("%w: %v", ErrRecoveryCodeUpdate, err)
	}
	return recoveryCode, nil
}

// ClaimRecoveryCodeReveal consumes the account's one-time recovery-code reveal
// and reports whether THIS call is the one that got it. False means the reveal
// was already spent — a replayed sealed cookie, a second tab, a browser that
// re-issued the request — and the caller must show nothing.
//
// The transport half (retracting the sealed cookie in the reveal response) asks
// the browser to forget the value and cannot bind a client that kept it; this
// mark is the half that holds.
//
// A zero userID is refused here, before the repository is reached: an absent
// owner id is invalid input, not a claim that applies to whichever row comes
// first. The compare-and-set below would match no row either — the guard makes
// the refusal explicit rather than resting on that.
func (service *AuthService) ClaimRecoveryCodeReveal(ctx context.Context, userID uint, revealedAt time.Time) (bool, error) {
	if userID == 0 {
		return false, ErrAuthUserRequired
	}
	return service.users.ClaimRecoveryCodeReveal(ctx, userID, revealedAt)
}

func (service *AuthService) RevokeAuthSessions(ctx context.Context, userID uint) error {
	if userID == 0 {
		return ErrAuthUserRequired
	}
	return service.users.BumpAuthSessionVersion(ctx, userID)
}

// equalizeRecoveryCodeLookupTiming spends, on the early-return paths of the
// recovery lookup, the bcrypt compute of BOTH operands the reset route now
// verifies — the recovery code and the account password. Equalizing only the
// code would make "this address has no account" measurably cheaper than a wrong
// password on a real account by one whole cost-12 comparison, which is the
// account-enumeration oracle this route must not have.
func equalizeRecoveryCodeLookupTiming(code string, password string) {
	_ = bcrypt.CompareHashAndPassword([]byte(recoveryCodeTimingEqualizationHash), []byte(NormalizeRecoveryCode(code)))
	_ = bcrypt.CompareHashAndPassword([]byte(credentialsTimingEqualizationHash), []byte(password))
}

// equalizeAuthCredentialsTiming runs a bcrypt comparison against a fixed
// placeholder hash so AuthenticateCredentials spends comparable time on every
// path. Without it, the early "user not found" / "local auth disabled" returns
// short-circuit before any bcrypt work and leak account existence through
// response timing (CWE-208 / CWE-204).
//
// Declared as a var so tests can replace it with an invocation counter,
// asserting "bcrypt was called" without measuring wall-clock time (which is
// flake-prone on shared CI runners). Production code never reassigns this.
var equalizeAuthCredentialsTiming = func(password string) {
	_ = bcrypt.CompareHashAndPassword([]byte(credentialsTimingEqualizationHash), []byte(password))
}

// equalizeRegistrationTiming mirrors the bcrypt work BuildOwnerUserWithRecovery
// performs on a fresh registration (password hash + recovery-code hash) so the
// duplicate-email branch of RegisterOwner spends comparable time and an attacker
// cannot tell a new email from an existing one through POST /api/v1/users
// response latency. Declared as a var for the same test-substitution reason as
// equalizeAuthCredentialsTiming.
var equalizeRegistrationTiming = func(password string) {
	_ = bcrypt.CompareHashAndPassword([]byte(credentialsTimingEqualizationHash), []byte(password))
	_ = bcrypt.CompareHashAndPassword([]byte(recoveryCodeTimingEqualizationHash), []byte(password))
}

// timingTopUpPlaceholderInput is the value the top-up placeholder hashes below
// are minted from. It is not a credential and cannot become one: the hashes it
// produces are only ever compared with the result discarded, and every real
// verification in this package compares against a hash read from the row.
const timingTopUpPlaceholderInput = "ovumcy timing equalization placeholder"

// timingTopUpHashesByCost holds one placeholder bcrypt hash per cost below
// passwordHashCost. A comparison against a hash at cost k spends ~2^k units of
// bcrypt work, so this table is what lets a rejection buy exactly the work a
// legacy stored hash did not spend (see bcryptCostTopUpSchedule).
//
// The hashes are MINTED, never pasted in as constants: hand-written literals
// would stay pinned to today's passwordHashCost, so raising the constant would
// silently leave the new top cost unpaid and reopen the gap this table closes.
// The one-time price is the sum of 2^k for k = bcrypt.MinCost ..
// passwordHashCost-1, which is just under a single passwordHashCost hash —
// roughly one login's work, paid once at startup and never per request.
var timingTopUpHashesByCost = mintTimingTopUpHashes()

func mintTimingTopUpHashes() map[int][]byte {
	minted := make(map[int][]byte, passwordHashCost-bcrypt.MinCost)
	for cost := bcrypt.MinCost; cost < passwordHashCost; cost++ {
		hash, err := bcrypt.GenerateFromPassword([]byte(timingTopUpPlaceholderInput), cost)
		if err != nil {
			continue // codecov:ignore -- bcrypt only errors on an out-of-range cost; spendBcryptCostTopUp overpays a missing entry rather than under-paying it
		}
		minted[cost] = hash
	}
	return minted
}

// bcryptCostTopUpSchedule names the costs at which dummy bcrypt work has to be
// spent so that a comparison already made against a hash at storedCost adds up
// to the work of a comparison at passwordHashCost.
//
// bcrypt work is exponential in the cost — a comparison at cost c spends ~2^c
// units — and the deficit 2^passwordHashCost - 2^c is exactly the sum of 2^k
// for k = c .. passwordHashCost-1. One dummy comparison at each of those costs
// therefore pays the deficit precisely. The schedule is derived from
// passwordHashCost and the observed stored cost, never from a fixed pair, so
// raising the constant widens it without a second edit.
//
// A stored cost at or above passwordHashCost owes nothing. A cost below
// bcrypt.MinCost cannot come out of bcrypt.Cost (it rejects such a hash
// outright); it is clamped so the schedule stays finite either way.
func bcryptCostTopUpSchedule(storedCost int) []int {
	if storedCost < bcrypt.MinCost {
		storedCost = bcrypt.MinCost
	}
	if storedCost >= passwordHashCost {
		return nil
	}

	schedule := make([]int, 0, passwordHashCost-storedCost)
	for cost := storedCost; cost < passwordHashCost; cost++ {
		schedule = append(schedule, cost)
	}
	return schedule
}

// timingTopUpCompare is the single point at which the top-up spends bcrypt
// work. Declared as a var for the same test-substitution reason as the
// equalize* helpers, but used differently: a guard WRAPS it to account the work
// spendBcryptCostTopUp actually spends, rather than recomputing that work from
// bcryptCostTopUpSchedule. Recomputing it would only prove the schedule is
// consistent with itself, and would stay green if the loop below stopped
// comparing altogether. Production code never reassigns it.
var timingTopUpCompare = func(hash []byte, operand []byte) {
	_ = bcrypt.CompareHashAndPassword(hash, operand)
}

// spendBcryptCostTopUp pays, against placeholder hashes, the bcrypt work a real
// comparison against storedHash left unspent relative to passwordHashCost. Like
// the equalize* helpers it never authenticates anyone: every result is
// discarded, and the placeholders are minted from a fixed non-credential input.
//
// fullCostPlaceholder is the caller's own passwordHashCost placeholder, spent
// whole when the stored hash is unparseable — there bcrypt.CompareHashAndPassword
// rejected the hash without running the KDF at all, so the entire cost is owed
// rather than a deficit, and a zero-work rejection would be the loudest signal
// of the three.
func spendBcryptCostTopUp(storedHash string, operand string, fullCostPlaceholder string) {
	storedCost, err := bcrypt.Cost([]byte(storedHash))
	if err != nil {
		timingTopUpCompare([]byte(fullCostPlaceholder), []byte(operand))
		return
	}

	for _, topUpCost := range bcryptCostTopUpSchedule(storedCost) {
		placeholder, ok := timingTopUpHashesByCost[topUpCost]
		if !ok {
			// Unreachable: the table covers every cost a schedule can name.
			// If it ever does not, overpay by a full comparison instead of
			// silently skipping the work.
			timingTopUpCompare([]byte(fullCostPlaceholder), []byte(operand)) // codecov:ignore -- unreachable while the table covers bcrypt.MinCost..passwordHashCost-1
			return
		}
		timingTopUpCompare(placeholder, []byte(operand))
	}
}

// topUpAuthCredentialsTiming closes the login half of the same gap the
// equalize* helpers close: the early-return branches of AuthenticateCredentials
// always spend a passwordHashCost comparison, while the real comparison spends
// only what the stored hash carries. Declared as a var for the same
// test-substitution reason as equalizeAuthCredentialsTiming — production code
// never reassigns it.
var topUpAuthCredentialsTiming = func(storedHash string, password string) {
	spendBcryptCostTopUp(storedHash, password, credentialsTimingEqualizationHash)
}

// topUpRecoveryLookupTiming is the recovery-lookup counterpart: it tops up both
// operands equalizeRecoveryCodeLookupTiming spends at full cost, each against
// its own placeholder, so a rejection costs the same whether the address has no
// account or the account's stored hashes predate the current cost. The code is
// normalized exactly as the real comparison normalizes it.
var topUpRecoveryLookupTiming = func(recoveryHash string, code string, passwordHash string, password string) {
	spendBcryptCostTopUp(recoveryHash, NormalizeRecoveryCode(code), recoveryCodeTimingEqualizationHash)
	spendBcryptCostTopUp(passwordHash, password, credentialsTimingEqualizationHash)
}

// ResetPasswordAndRotateRecoveryCodeCAS is the single-use variant of
// ResetPasswordAndRotateRecoveryCode used by the password-reset flow.
// oldPasswordHash must be the hash that was current when the reset token was
// issued (sourced from the resolved user before any write).
//
// The UPDATE carries the predicate
// `WHERE id = ? AND password_hash = oldPasswordHash`. Concurrent or replayed
// redeems both reach the UPDATE, but only one sees RowsAffected == 1; the
// loser receives ErrResetTokenAlreadyConsumed.
func (service *AuthService) ResetPasswordAndRotateRecoveryCodeCAS(ctx context.Context, user *models.User, oldPasswordHash string, newPassword string) (string, error) {
	if user == nil {
		return "", ErrAuthUserRequired // codecov:ignore -- defensive; callers always pass a resolved user
	}

	passwordHash, err := bcrypt.GenerateFromPassword([]byte(newPassword), passwordHashCost)
	if err != nil {
		return "", err // codecov:ignore -- bcrypt only errors on an out-of-range cost
	}
	recoveryCode, recoveryHash, err := GenerateRecoveryCodeHash()
	if err != nil {
		return "", err // codecov:ignore -- crypto/rand failure, not reachable in tests
	}

	// CAS predicate prevents concurrent / replayed redeems.
	if err := service.users.UpdatePasswordRecoveryCodeAndRevokeSessionsCAS(ctx, user.ID, oldPasswordHash, string(passwordHash), recoveryHash); err != nil {
		return "", err
	}
	user.PasswordHash = string(passwordHash)
	user.RecoveryCodeHash = recoveryHash
	user.LocalAuthEnabled = true
	user.AuthSessionVersion = NormalizeAuthSessionVersion(user.AuthSessionVersion) + 1
	user.MustChangePassword = false

	return recoveryCode, nil
}
