package services

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/ovumcy/ovumcy-web/internal/models"
	"github.com/ovumcy/ovumcy-web/internal/security"
)

var (
	ErrOIDCDisabled              = errors.New("oidc disabled")
	ErrOIDCUnavailable           = errors.New("oidc unavailable")
	ErrOIDCCallbackInvalid       = errors.New("oidc callback invalid")
	ErrOIDCAuthenticationFailed  = errors.New("oidc authentication failed")
	ErrOIDCAccountUnavailable    = errors.New("oidc account unavailable")
	ErrOIDCIdentityResolveFailed = errors.New("oidc identity resolve failed")
	ErrOIDCLinkFailed            = errors.New("oidc identity link failed")
	ErrOIDCProvisionFailed       = errors.New("oidc account provision failed")
	// ErrOIDCReauthStale indicates the provider returned a successful exchange
	// whose auth_time is older than the requested max age — the user did not
	// provably re-authenticate; the provider may have answered from a cached
	// SSO session despite prompt=login + max_age=0. Signing in again can clear
	// it, so the owner-facing copy asks for exactly that.
	ErrOIDCReauthStale = errors.New("oidc reauth stale")
	// ErrOIDCReauthAuthTimeMissing indicates the exchange carried NO auth_time
	// at all: the provider never said when the sign-in happened. OpenID
	// Connect Core requires the claim whenever max_age is sent, and every
	// step-up sends max_age=0, so this is a non-conforming provider rather
	// than a slow owner — no retry can succeed until the provider is fixed,
	// and an operator reading the audit stream must be able to tell the two
	// apart.
	//
	// It WRAPS ErrOIDCReauthStale on purpose. The two verdicts are distinct,
	// but "the freshness proof did not hold" is true of both, so a consumer
	// that knows only the coarse sentinel keeps refusing instead of falling
	// through to its default arm. A consumer that must distinguish them
	// matches this one FIRST; the switch order is not a convention here but a
	// pinned invariant — api.TestEveryReauthStaleMatchIsPrecededByTheMissingAuthTimeMatch.
	ErrOIDCReauthAuthTimeMissing = fmt.Errorf("oidc reauth auth_time missing: %w", ErrOIDCReauthStale)
	// ErrOIDCReauthIdentityMismatch indicates the (issuer, subject) returned by
	// the reauth callback is not linked to the user that started the step-up
	// flow. Treated as a hard failure to prevent cross-account substitution.
	ErrOIDCReauthIdentityMismatch = errors.New("oidc reauth identity mismatch")
	// ErrOIDCLinkRequiresConfirmation indicates the OIDC exchange resolved to a
	// pre-existing local user by email, but the (issuer, subject) pair is not
	// yet linked to that user. Auto-linking would let a malicious or sloppy
	// upstream IdP take over the account by asserting a verified email it does
	// not actually control. The handler must capture the pending claims and
	// require an explicit confirmation step (current local password) before
	// linkIdentity is called. OIDCLoginResult.PendingLinkClaims carries the
	// claims to persist; OIDCLoginResult.User carries the target user.
	ErrOIDCLinkRequiresConfirmation = errors.New("oidc identity link requires confirmation")
	// ErrOIDCIdentityNotFound indicates an unlink named no identity bound to the
	// requesting account — missing, zero, or another owner's id alike.
	ErrOIDCIdentityNotFound = errors.New("oidc identity not found")
	// ErrOIDCUnlinkLastSignIn indicates removing the identity would leave the
	// account with no way to sign in: no other linked identity, and no local
	// password sign-in available (none set, or OIDC_LOGIN_MODE=oidc_only). It is
	// the shared value from internal/models: the identity repository raises the
	// same error from inside the delete transaction, where the check that holds
	// under concurrency runs.
	ErrOIDCUnlinkLastSignIn = models.ErrOIDCUnlinkLastSignIn
	// ErrAuthSessionVersionChanged indicates the account's sessions were
	// revoked by another write after the caller verified its factors, so the
	// link was not written: the caller refuses rather than mint a session.
	ErrAuthSessionVersionChanged = models.ErrAuthSessionVersionChanged
)

type OIDCProviderClient interface {
	Enabled() bool
	LocalPublicAuthEnabled() bool
	Config() security.OIDCConfig
	AuthCodeURL(ctx context.Context, state string, nonce string, codeVerifier string, extra map[string]string) (string, error)
	ExchangeCode(ctx context.Context, code string, codeVerifier string, expectedNonce string) (security.OIDCExchangeResult, error)
}

type OIDCIdentityStore interface {
	FindByIssuerSubject(ctx context.Context, issuer string, subject string) (models.OIDCIdentity, bool, error)
	Create(ctx context.Context, identity *models.OIDCIdentity) error
	// CreateAndRevokeSessions binds an identity to an EXISTING account and
	// bumps its AuthSessionVersion in the same atomic write — only from
	// expectedSessionVersion; any other stored version rolls the link back with
	// ErrAuthSessionVersionChanged.
	CreateAndRevokeSessions(ctx context.Context, identity *models.OIDCIdentity, expectedSessionVersion int) error
	ListByUser(ctx context.Context, userID uint) ([]models.OIDCIdentity, error)
	// DeleteForUserAndRevokeSessions removes one owner-scoped identity and bumps
	// the owner's AuthSessionVersion in the same atomic write; false means no
	// row with that id belongs to userID. Inside that write it refuses, with
	// ErrOIDCUnlinkLastSignIn, a delete that leaves no identity and no usable
	// local password (localSignInOpen: the instance accepts password sign-in).
	DeleteForUserAndRevokeSessions(ctx context.Context, userID uint, identityID uint, expectedSessionVersion int, localSignInOpen bool) (bool, error)
	TouchLastUsed(ctx context.Context, identityID uint, userID uint, usedAt time.Time) error
}

// LinkedOIDCIdentity is the owner-facing view of one bound identity: enough to
// tell two links apart and pick one to remove, nothing the provider did not
// already show the owner.
type LinkedOIDCIdentity struct {
	ID       uint
	Issuer   string
	LinkedAt time.Time
}

type OIDCUserStore interface {
	FindByID(ctx context.Context, userID uint) (models.User, error)
	FindByNormalizedEmailOptional(ctx context.Context, email string) (models.User, bool, error)
}

type OIDCAutoProvisioner interface {
	AutoProvisionOwnerAccount(ctx context.Context, email string, createdAt time.Time) (models.User, error)
}

type OIDCLogoutState struct {
	UserID                uint
	EndSessionEndpoint    string
	IDTokenHint           string
	PostLogoutRedirectURL string
}

type OIDCLoginResult struct {
	User            models.User
	NewlyLinked     bool
	AutoProvisioned bool
	Logout          *OIDCLogoutState
	// RequiresTOTP mirrors LoginResult.RequiresTOTP (login_service.go): set
	// when the resolved account has TOTP enabled and is not also routed
	// through the MustChangePassword branch — that branch outranks TOTP for
	// the same reason it does on the local login path. The handler must gate
	// on this exactly as it gates the local login result: set the
	// pending-TOTP cookie and redirect to /auth/2fa before ever calling
	// setAuthCookie.
	RequiresTOTP bool
	// RequiresPasswordReset mirrors LoginResult.RequiresPasswordReset
	// (login_service.go): set when MustChangePassword is flagged on the
	// account OR when the account is enrolled in TOTP but its secret is
	// unverifiable (does not decrypt). Both reasons route to the same
	// forced-reset escape hatch; RequiresTOTP is never set alongside this.
	RequiresPasswordReset bool
	// PendingLinkClaims is non-nil only when Authenticate returned
	// ErrOIDCLinkRequiresConfirmation. The handler stores these in a sealed
	// short-lived cookie and dispatches to the link-confirmation step, where a
	// password check guards the actual linkIdentity call.
	PendingLinkClaims *security.OIDCClaims
}

type OIDCLoginService struct {
	client      OIDCProviderClient
	identities  OIDCIdentityStore
	users       OIDCUserStore
	provisioner OIDCAutoProvisioner
	config      security.OIDCConfig
	// totp is consulted instead of the raw TOTPEnabled column so an
	// enrolled-but-unverifiable secret is never treated as "no second
	// factor" or "second factor, forever." Nil unless SetTOTPVerifier is
	// called, which is exactly today's TOTPEnabled-only behaviour for tests
	// that never wire it. Production always wires a real *TOTPService via
	// SetTOTPVerifier in bootstrap.
	totp TOTPFactorVerifier
}

func NewOIDCLoginService(client OIDCProviderClient, identities OIDCIdentityStore, users OIDCUserStore, provisioner OIDCAutoProvisioner) *OIDCLoginService {
	config := security.OIDCConfig{}
	if client != nil {
		config = client.Config()
	}
	return &OIDCLoginService{
		client:      client,
		identities:  identities,
		users:       users,
		provisioner: provisioner,
		config:      config,
	}
}

// SetTOTPVerifier wires the derived TOTP-verifiability check Authenticate
// uses to decide between raising a 2FA challenge and routing into the
// forced-reset escape hatch. Called once from bootstrap with the same
// *TOTPService the local login path and the 2FA handlers use.
func (service *OIDCLoginService) SetTOTPVerifier(verifier TOTPFactorVerifier) {
	service.totp = verifier
}

func (service *OIDCLoginService) Enabled() bool {
	return service != nil && service.client != nil && service.client.Enabled()
}

func (service *OIDCLoginService) LocalPublicAuthEnabled() bool {
	if service == nil {
		return true
	}
	return service.config.LocalPublicAuthEnabled()
}

// ResponseMode reports the configured OIDC response mode (form_post or query).
// The transport layer uses it to decide whether the callback is also served
// over GET and which request source (body vs query) the callback parameters
// are read from. The config is sanitized at construction, so this is always a
// concrete mode; a nil service (OIDC disabled) reports the form_post default.
func (service *OIDCLoginService) ResponseMode() security.OIDCResponseMode {
	if service == nil {
		return security.OIDCResponseModeFormPost
	}
	return service.config.ResponseMode
}

// IssuerURL reports the configured issuer, the origin every provider endpoint
// is pinned to. The transport layer re-applies that pin to provider-logout
// state it reads back from storage. A nil service (OIDC disabled) reports no
// issuer, which pins nothing and so admits no stored endpoint.
func (service *OIDCLoginService) IssuerURL() string {
	if service == nil {
		return ""
	}
	return service.config.IssuerURL
}

// PostLogoutRedirectURL reports the post-logout return address the current
// configuration resolves to. The transport layer composes the provider
// end-session redirect from this value, never from the copy stored with the
// logout state, so a row written under an earlier configuration cannot name
// where the provider sends the browser afterwards. A nil service (OIDC
// disabled) reports none, which composes no provider redirect.
func (service *OIDCLoginService) PostLogoutRedirectURL() string {
	if service == nil {
		return ""
	}
	postLogoutRedirectURL := strings.TrimSpace(service.config.ResolvedPostLogoutRedirectURL())
	// First-party only, re-checked where the value is handed out rather than
	// trusted from boot validation alone: the provider sends the browser to
	// this address after sign-out, so it must be on this instance's own origin —
	// the origin of the configured OIDC callback. Anything else composes no
	// provider redirect and the sign-out completes locally.
	if !security.SameOriginURLString(service.config.RedirectURL, postLogoutRedirectURL) {
		return ""
	}
	return postLogoutRedirectURL
}

// ProviderLogoutEnabled reports whether the configuration in force NOW routes
// a sign-out through the provider's end-session endpoint. It is the single
// predicate both sides of the provider-logout bridge ask: buildLogoutState
// consults it before writing a row, and the transport layer consults it again
// at logout time before composing any end-session redirect from one. A stored
// row lives for days and is only a carrier of the per-session material the
// provider needs — never evidence of the mode that produced it — so an
// instance switched to OIDC_LOGOUT_MODE=local, or with OIDC turned off
// entirely, signs out locally from the first request after the switch. A nil
// or disabled service reports false, which composes no provider redirect.
func (service *OIDCLoginService) ProviderLogoutEnabled() bool {
	return service.Enabled() && service.config.ProviderLogoutEnabled()
}

func (service *OIDCLoginService) StartAuth(ctx context.Context, state string, nonce string, codeVerifier string) (string, error) {
	return service.startAuthWithExtra(ctx, state, nonce, codeVerifier, nil)
}

// StartReauth forces the provider to perform a fresh interactive login by
// adding prompt=login and max_age=0 to the authorize URL. Used for step-up
// flows like enabling a local password from an OIDC-only account where we
// must verify that the holder of the current session also controls the
// upstream identity right now (not via a stale cached SSO session).
func (service *OIDCLoginService) StartReauth(ctx context.Context, state string, nonce string, codeVerifier string) (string, error) {
	return service.startAuthWithExtra(ctx, state, nonce, codeVerifier, map[string]string{
		"prompt":  "login",
		"max_age": "0",
	})
}

func (service *OIDCLoginService) startAuthWithExtra(ctx context.Context, state string, nonce string, codeVerifier string, extra map[string]string) (string, error) {
	if !service.Enabled() {
		return "", ErrOIDCDisabled
	}
	if strings.TrimSpace(state) == "" || strings.TrimSpace(nonce) == "" || strings.TrimSpace(codeVerifier) == "" {
		return "", ErrOIDCCallbackInvalid
	}
	url, err := service.client.AuthCodeURL(ctx, state, nonce, codeVerifier, extra)
	if err != nil {
		return "", ErrOIDCUnavailable
	}
	return url, nil
}

// ValidateReauthExchange runs an OIDC code exchange in the context of a
// step-up re-auth (e.g. promoting an OIDC-only account to local auth). It
// enforces three properties that plain Authenticate does NOT:
//
//   - the (issuer, subject) returned by the provider must already be linked to
//     expectedUserID. This stops an attacker who hijacked an OIDC-only session
//     from completing the step-up by signing in with their OWN provider account.
//   - the provider must actually have performed a fresh interactive
//     authentication, proven by auth_time alone (REQUIRED by the spec
//     whenever max_age was sent). A token without auth_time is refused; iat
//     is never accepted in its place.
//   - auth_time must lie within maxAuthAge of now. A small
//     forward-tolerance handles modest clock skew.
//
// Deviations are reported as ErrOIDCReauthIdentityMismatch, or as one of the
// two freshness verdicts reauthFreshnessVerdict draws: ErrOIDCReauthStale for a
// sign-in that is too old, ErrOIDCReauthAuthTimeMissing for a provider that
// never dated it at all. The handler keeps them apart in the audit stream and
// in the owner-facing copy, because only one of the two can be cleared by
// trying again.
func (service *OIDCLoginService) ValidateReauthExchange(ctx context.Context, code string, codeVerifier string, expectedNonce string, expectedUserID uint, maxAuthAge time.Duration, now time.Time) error {
	if !service.Enabled() {
		return ErrOIDCDisabled
	}
	if expectedUserID == 0 {
		return ErrOIDCReauthIdentityMismatch
	}
	if strings.TrimSpace(code) == "" || strings.TrimSpace(codeVerifier) == "" || strings.TrimSpace(expectedNonce) == "" {
		return ErrOIDCCallbackInvalid
	}

	exchange, err := service.client.ExchangeCode(ctx, code, codeVerifier, expectedNonce)
	if err != nil {
		return ErrOIDCAuthenticationFailed
	}

	identity, found, err := service.identities.FindByIssuerSubject(ctx, exchange.Claims.Issuer, exchange.Claims.Subject)
	if err != nil {
		return ErrOIDCIdentityResolveFailed
	}
	if !found || identity.UserID != expectedUserID {
		return ErrOIDCReauthIdentityMismatch
	}

	if err := reauthFreshnessVerdict(exchange.Claims, maxAuthAge, now); err != nil {
		return err
	}

	_ = service.identities.TouchLastUsed(ctx, identity.ID, expectedUserID, effectiveOIDCLoginTime(now))
	return nil
}

// reauthFreshnessVerdict returns nil when the exchange proves a sign-in inside
// the window, and otherwise the sentinel that says WHY. The two refusals are
// separated here, at the single place that reads the claim, rather than at each
// caller: a copy per step-up is how the class would end up fixed at N of N+1
// sites, one of them still telling the owner to try again on a provider where
// no retry can work.
func reauthFreshnessVerdict(claims security.OIDCClaims, maxAuthAge time.Duration, now time.Time) error {
	if maxAuthAge <= 0 {
		// Not a provider fault: the caller asked for a window nothing can sit
		// inside. "Too old" is the honest verdict and it stays this side of the
		// missing-claim check, so a zero window cannot be reported as a
		// non-conforming provider.
		return ErrOIDCReauthStale
	}
	// iat is never a fallback: it dates the token, not the authentication, so a
	// provider answering prompt=login from a cached SSO session mints a fresh iat
	// over a stale sign-in. A token without auth_time proves nothing here — and
	// it is a different failure from a sign-in that is merely too old, because
	// the owner cannot resolve it by signing in again.
	reference := claims.AuthTime
	if reference.IsZero() {
		return ErrOIDCReauthAuthTimeMissing
	}
	if reference.After(now.Add(1 * time.Minute)) {
		// Clock skew tolerance in one direction only — clearly future-dated
		// timestamps look forged or like provider misconfiguration. The claim
		// IS present, so this is the stale verdict, not the missing one.
		return ErrOIDCReauthStale
	}
	if now.Sub(reference) > maxAuthAge {
		return ErrOIDCReauthStale
	}
	return nil
}

func (service *OIDCLoginService) Authenticate(ctx context.Context, code string, codeVerifier string, expectedNonce string, now time.Time) (OIDCLoginResult, error) {
	if !service.Enabled() {
		return OIDCLoginResult{}, ErrOIDCDisabled
	}
	if strings.TrimSpace(code) == "" || strings.TrimSpace(codeVerifier) == "" || strings.TrimSpace(expectedNonce) == "" {
		return OIDCLoginResult{}, ErrOIDCCallbackInvalid
	}

	exchange, err := service.client.ExchangeCode(ctx, code, codeVerifier, expectedNonce)
	if err != nil || !hasIdentityKey(exchange.Claims) {
		return OIDCLoginResult{}, ErrOIDCAuthenticationFailed
	}

	result, err := service.authenticateExchange(ctx, exchange, effectiveOIDCLoginTime(now))
	if errors.Is(err, ErrOIDCLinkRequiresConfirmation) {
		// Preserve the pending-link payload (User + PendingLinkClaims) so the
		// handler can stash them in the confirmation cookie. Logout state is
		// intentionally not built — no session was issued.
		return result, err
	}
	if err != nil {
		return OIDCLoginResult{}, err
	}
	// Session issuance parity (docs/security/oidc-and-sessions.md): a linked
	// identity re-authenticating here must clear the same second factor the
	// local login path requires, computed the same way as
	// LoginService.Authenticate — MustChangePassword outranks TOTP
	// unconditionally, and an enrolled-but-unverifiable secret (SECRET_KEY
	// rotation) routes to the same forced-reset escape hatch because the
	// factor cannot be checked, not only when the routing flag happens to be
	// set. RequiresTOTP is required only where TOTP is actually verifiable.
	result.RequiresPasswordReset = result.User.MustChangePassword ||
		(result.User.TOTPEnabled && service.totp != nil && service.totp.Unverifiable(result.User))
	result.RequiresTOTP = !result.RequiresPasswordReset && result.User.TOTPEnabled
	result.Logout = service.buildLogoutState(exchange.Session, result.User.ID)
	return result, nil
}

func (service *OIDCLoginService) authenticateExchange(ctx context.Context, exchange security.OIDCExchangeResult, loginTime time.Time) (OIDCLoginResult, error) {
	if result, found, err := service.authenticateLinkedIdentity(ctx, exchange, loginTime); found || err != nil {
		return result, err
	}

	user, autoProvisioned, err := service.resolveUserForClaims(ctx, exchange.Claims, loginTime)
	if err != nil {
		return OIDCLoginResult{}, err
	}

	// Defence against malicious / sloppy upstream IdP asserting an email that
	// already belongs to a pre-existing local account: refuse to auto-link.
	// The handler captures these claims in a sealed pending-link cookie and
	// requires the holder of the existing account to confirm with their
	// current local password before the link is created. Auto-provisioned new
	// users are unaffected — by definition no other party holds that email.
	if !autoProvisioned {
		claims := exchange.Claims
		return OIDCLoginResult{
			User:              user,
			PendingLinkClaims: &claims,
		}, ErrOIDCLinkRequiresConfirmation
	}

	if err := service.linkIdentity(ctx, user.ID, exchange.Claims, loginTime); err != nil {
		return OIDCLoginResult{}, err
	}

	return OIDCLoginResult{
		User:            user,
		NewlyLinked:     true,
		AutoProvisioned: autoProvisioned,
	}, nil
}

// ConfirmAndLinkIdentity performs the link previously refused by
// authenticateExchange after the holder of the target account has proven
// possession (typically via local-password confirmation). The handler is
// responsible for the password check; this method only persists the link and
// touches last-used. It refuses linkage if the (issuer, subject) is already
// taken by a different user — guarding against a concurrent claim from a
// second confirmation flow.
//
// expectedSessionVersion is the AuthSessionVersion the caller verified its
// factors against. The link is written only from that version, and the
// returned version is the one this call left the account at: a caller that
// mints a session does so only while a fresh read still shows it, so a
// revocation landing on either side of the link write is never outlived.
func (service *OIDCLoginService) ConfirmAndLinkIdentity(ctx context.Context, targetUserID uint, expectedSessionVersion int, claims security.OIDCClaims, linkTime time.Time) (int, error) {
	if !service.Enabled() {
		return 0, ErrOIDCDisabled
	}
	if targetUserID == 0 || !hasIdentityKey(claims) {
		return 0, ErrOIDCLinkFailed
	}
	expectedSessionVersion = NormalizeAuthSessionVersion(expectedSessionVersion)

	existing, found, err := service.identities.FindByIssuerSubject(ctx, claims.Issuer, claims.Subject)
	if err != nil {
		return 0, ErrOIDCIdentityResolveFailed
	}
	if found {
		if existing.UserID != targetUserID {
			// (issuer, subject) was claimed by somebody else between the OIDC
			// callback that issued the pending-link cookie and this
			// confirmation. Fail closed.
			return 0, ErrOIDCLinkFailed
		}
		_ = service.identities.TouchLastUsed(ctx, existing.ID, targetUserID, effectiveOIDCLoginTime(linkTime)) // codecov:ignore -- best-effort last-used touch; error intentionally ignored
		return expectedSessionVersion, nil
	}

	// An explicit link changes how an existing account can be entered, so it
	// bumps AuthSessionVersion in the same write (session invalidation
	// invariant); the caller re-issues its own session afterwards. The
	// auto-provision path keeps linkIdentity: that account was created by this
	// very sign-in and has no earlier session to revoke.
	identity := newOIDCIdentityRecord(targetUserID, claims, linkTime)
	if err := service.identities.CreateAndRevokeSessions(ctx, &identity, expectedSessionVersion); err != nil {
		if errors.Is(err, ErrAuthSessionVersionChanged) {
			return 0, ErrAuthSessionVersionChanged
		}
		return 0, ErrOIDCLinkFailed
	}
	return expectedSessionVersion + 1, nil
}

// ListLinkedIdentities returns the identities bound to userID for the owner's
// own settings page. A zero userID lists nothing.
func (service *OIDCLoginService) ListLinkedIdentities(ctx context.Context, userID uint) ([]LinkedOIDCIdentity, error) {
	if service == nil || service.identities == nil || userID == 0 {
		return nil, nil
	}
	identities, err := service.identities.ListByUser(ctx, userID)
	if err != nil {
		return nil, ErrOIDCIdentityResolveFailed
	}
	linked := make([]LinkedOIDCIdentity, 0, len(identities))
	for _, identity := range identities {
		if identity.UserID != userID {
			// codecov:ignore:start -- the store scopes by user_id in the query;
			// this is the second half of the privacy boundary, not a live branch.
			continue
			// codecov:ignore:end
		}
		linked = append(linked, LinkedOIDCIdentity{ID: identity.ID, Issuer: identity.Issuer, LinkedAt: identity.CreatedAt})
	}
	return linked, nil
}

// UnlinkIdentity removes one identity from the requesting account. The caller
// has already verified a fresh factor (the current local password); this
// method owns the rules that do not depend on the transport:
//
//   - the identity id is combined with the session's user id in the delete
//     itself, so another owner's id reads as not-found;
//   - the account must keep a way in: removing its only identity is refused
//     unless local password sign-in is both set up and allowed on this
//     instance (OIDC_LOGIN_MODE=hybrid);
//   - the delete bumps AuthSessionVersion in the same write, so every session
//     issued before the unlink — including one a removed identity minted —
//     is revoked;
//   - the delete is written only from the session version user carries (the
//     one the request was authenticated with): an account revoked by another
//     write in between is left as it was and the result is
//     ErrAuthSessionVersionChanged.
//
// On success it returns the session version the delete left the account at; a
// caller re-issuing this device's session does so only while a fresh read
// still shows it.
func (service *OIDCLoginService) UnlinkIdentity(ctx context.Context, user models.User, identityID uint) (int, error) {
	if !service.Enabled() {
		return 0, ErrOIDCDisabled
	}
	if user.ID == 0 || identityID == 0 {
		return 0, ErrOIDCIdentityNotFound
	}
	identities, err := service.identities.ListByUser(ctx, user.ID)
	if err != nil {
		return 0, ErrOIDCIdentityResolveFailed
	}
	owned := false
	for _, identity := range identities {
		if identity.ID == identityID && identity.UserID == user.ID {
			owned = true
			break
		}
	}
	if !owned {
		return 0, ErrOIDCIdentityNotFound
	}
	localSignInOpen := service.LocalPublicAuthEnabled()
	localSignInAvailable := user.LocalAuthEnabled &&
		strings.TrimSpace(user.PasswordHash) != "" &&
		localSignInOpen
	if len(identities) <= 1 && !localSignInAvailable {
		return 0, ErrOIDCUnlinkLastSignIn
	}
	// The read above answers the common case; the store re-checks the same rule
	// inside the delete transaction, which is what holds against a concurrent
	// unlink of the account's other identity.
	expectedSessionVersion := NormalizeAuthSessionVersion(user.AuthSessionVersion)
	deleted, err := service.identities.DeleteForUserAndRevokeSessions(ctx, user.ID, identityID, expectedSessionVersion, localSignInOpen)
	if errors.Is(err, ErrOIDCUnlinkLastSignIn) {
		return 0, ErrOIDCUnlinkLastSignIn
	}
	if errors.Is(err, ErrAuthSessionVersionChanged) {
		return 0, ErrAuthSessionVersionChanged
	}
	if err != nil {
		return 0, ErrOIDCIdentityResolveFailed
	}
	if !deleted {
		return 0, ErrOIDCIdentityNotFound
	}
	return expectedSessionVersion + 1, nil
}

// CompleteIdentityLinkReauth authorises a NEW OIDC identity link from an
// already-authenticated settings session: it runs a fresh code exchange,
// requires the same freshness proof as ValidateReauthExchange (prompt=login +
// max_age enforced via reauthFreshnessVerdict), and then persists the link via
// ConfirmAndLinkIdentity.
//
// It deliberately does NOT reuse ValidateReauthExchange: that helper requires
// the returned (issuer, subject) to already be linked to expectedUserID,
// which is exactly backwards for linking — the whole point here is a pair
// that has never been linked to anyone. Reusing it would make first-time
// linking impossible; the freshness check is the part worth sharing, the
// "already linked" check is not.
//
// expectedSessionVersion is the version of the session that started the
// step-up; see ConfirmAndLinkIdentity, whose returned session version this
// passes back (an already-linked no-op leaves expectedSessionVersion).
func (service *OIDCLoginService) CompleteIdentityLinkReauth(ctx context.Context, code string, codeVerifier string, expectedNonce string, targetUserID uint, expectedSessionVersion int, maxAuthAge time.Duration, now time.Time) (int, error) {
	if !service.Enabled() {
		return 0, ErrOIDCDisabled
	}
	if targetUserID == 0 {
		return 0, ErrOIDCLinkFailed
	}
	if strings.TrimSpace(code) == "" || strings.TrimSpace(codeVerifier) == "" || strings.TrimSpace(expectedNonce) == "" {
		return 0, ErrOIDCCallbackInvalid
	}

	exchange, err := service.client.ExchangeCode(ctx, code, codeVerifier, expectedNonce)
	if err != nil {
		return 0, ErrOIDCAuthenticationFailed
	}

	if err := reauthFreshnessVerdict(exchange.Claims, maxAuthAge, now); err != nil {
		return 0, err
	}

	return service.ConfirmAndLinkIdentity(ctx, targetUserID, expectedSessionVersion, exchange.Claims, now)
}

func (service *OIDCLoginService) authenticateLinkedIdentity(ctx context.Context, exchange security.OIDCExchangeResult, loginTime time.Time) (OIDCLoginResult, bool, error) {
	identity, found, err := service.identities.FindByIssuerSubject(ctx, exchange.Claims.Issuer, exchange.Claims.Subject)
	if err != nil {
		return OIDCLoginResult{}, false, ErrOIDCIdentityResolveFailed
	}
	if !found {
		return OIDCLoginResult{}, false, nil
	}

	user, err := service.users.FindByID(ctx, identity.UserID)
	if err != nil {
		return OIDCLoginResult{}, true, ErrOIDCIdentityResolveFailed
	}
	if err := ValidateSupportedWebUser(&user); err != nil {
		return OIDCLoginResult{}, true, ErrOIDCAccountUnavailable
	}
	_ = service.identities.TouchLastUsed(ctx, identity.ID, identity.UserID, loginTime)
	return OIDCLoginResult{User: user}, true, nil
}

func (service *OIDCLoginService) resolveUserForClaims(ctx context.Context, claims security.OIDCClaims, loginTime time.Time) (models.User, bool, error) {
	normalizedEmail := NormalizeAuthEmail(claims.Email)
	if !claims.EmailVerified || normalizedEmail == "" {
		return models.User{}, false, ErrOIDCAccountUnavailable
	}
	return service.findOrProvisionUser(ctx, normalizedEmail, loginTime)
}

func (service *OIDCLoginService) findOrProvisionUser(ctx context.Context, normalizedEmail string, loginTime time.Time) (models.User, bool, error) {
	user, found, err := service.users.FindByNormalizedEmailOptional(ctx, normalizedEmail)
	if err != nil {
		return models.User{}, false, ErrOIDCIdentityResolveFailed
	}
	if found {
		if err := ValidateSupportedWebUser(&user); err != nil {
			return models.User{}, false, ErrOIDCAccountUnavailable
		}
		return user, false, nil
	}
	if !service.config.AllowsAutoProvision(normalizedEmail) || service.provisioner == nil {
		return models.User{}, false, ErrOIDCAccountUnavailable
	}
	return service.autoProvisionOrLookupUser(ctx, normalizedEmail, loginTime)
}

func (service *OIDCLoginService) autoProvisionOrLookupUser(ctx context.Context, normalizedEmail string, loginTime time.Time) (models.User, bool, error) {
	user, err := service.provisioner.AutoProvisionOwnerAccount(ctx, normalizedEmail, loginTime)
	if err == nil {
		return user, true, nil
	}
	if !errors.Is(err, ErrAuthEmailExists) {
		return models.User{}, false, ErrOIDCProvisionFailed
	}

	user, found, lookupErr := service.users.FindByNormalizedEmailOptional(ctx, normalizedEmail)
	if lookupErr != nil {
		return models.User{}, false, ErrOIDCIdentityResolveFailed
	}
	if !found {
		return models.User{}, false, ErrOIDCProvisionFailed
	}
	if err := ValidateSupportedWebUser(&user); err != nil {
		return models.User{}, false, ErrOIDCAccountUnavailable
	}
	return user, false, nil
}

func (service *OIDCLoginService) linkIdentity(ctx context.Context, userID uint, claims security.OIDCClaims, linkTime time.Time) error {
	if userID == 0 || !hasIdentityKey(claims) {
		return ErrOIDCLinkFailed
	}
	identity := newOIDCIdentityRecord(userID, claims, linkTime)
	if err := service.identities.Create(ctx, &identity); err != nil {
		return ErrOIDCLinkFailed
	}
	return nil
}

func newOIDCIdentityRecord(userID uint, claims security.OIDCClaims, linkTime time.Time) models.OIDCIdentity {
	linkTime = effectiveOIDCLoginTime(linkTime)
	return models.OIDCIdentity{
		UserID:     userID,
		Issuer:     strings.TrimSpace(claims.Issuer),
		Subject:    strings.TrimSpace(claims.Subject),
		CreatedAt:  linkTime,
		LastUsedAt: &linkTime,
	}
}

// hasIdentityKey reports whether the claims carry the (issuer, subject) pair
// every identity is keyed on. A blank either half identifies nobody, so it is
// refused rather than looked up or bound (the claims parser refuses it first;
// this is the service's own half of that rule, for any other claims source).
func hasIdentityKey(claims security.OIDCClaims) bool {
	return strings.TrimSpace(claims.Issuer) != "" && strings.TrimSpace(claims.Subject) != ""
}

func effectiveOIDCLoginTime(now time.Time) time.Time {
	if now.IsZero() {
		now = time.Now().UTC()
	}
	return now.UTC()
}

// buildLogoutState assembles the provider-logout material for the account the
// callback just resolved. userID is that account's id and is required: the
// state is persisted as a row account erasure reaches by user_id, so building
// one without an owner would put a row beyond the reach of the erasure it must
// obey (`docs/SECURITY_INVARIANTS.md`). Returning nil here is the same
// outcome as a provider that offers no end-session endpoint — the caller
// simply issues no logout state.
func (service *OIDCLoginService) buildLogoutState(session security.OIDCSession, userID uint) *OIDCLogoutState {
	if !service.ProviderLogoutEnabled() || userID == 0 {
		return nil
	}

	endSessionEndpoint := strings.TrimSpace(session.EndSessionEndpoint)
	idTokenHint := strings.TrimSpace(session.IDTokenHint)
	postLogoutRedirectURL := strings.TrimSpace(service.config.ResolvedPostLogoutRedirectURL())
	if endSessionEndpoint == "" || idTokenHint == "" || postLogoutRedirectURL == "" {
		return nil
	}

	return &OIDCLogoutState{
		UserID:                userID,
		EndSessionEndpoint:    endSessionEndpoint,
		IDTokenHint:           idTokenHint,
		PostLogoutRedirectURL: postLogoutRedirectURL,
	}
}
