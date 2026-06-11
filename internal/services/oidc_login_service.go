package services

import (
	"context"
	"errors"
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
	// but auth_time / iat are older than the requested max age — the user did
	// not actually re-authenticate, the provider answered from a cached SSO
	// session despite prompt=login + max_age=0.
	ErrOIDCReauthStale = errors.New("oidc reauth stale")
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
	TouchLastUsed(ctx context.Context, identityID uint, usedAt time.Time) error
}

type OIDCUserStore interface {
	FindByID(ctx context.Context, userID uint) (models.User, error)
	FindByNormalizedEmailOptional(ctx context.Context, email string) (models.User, bool, error)
}

type OIDCAutoProvisioner interface {
	AutoProvisionOwnerAccount(ctx context.Context, email string, createdAt time.Time) (models.User, error)
}

type OIDCLogoutState struct {
	EndSessionEndpoint    string
	IDTokenHint           string
	PostLogoutRedirectURL string
}

type OIDCLoginResult struct {
	User            models.User
	NewlyLinked     bool
	AutoProvisioned bool
	Logout          *OIDCLogoutState
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

func (service *OIDCLoginService) Enabled() bool {
	return service != nil && service.client != nil && service.client.Enabled()
}

func (service *OIDCLoginService) LocalPublicAuthEnabled() bool {
	if service == nil {
		return true
	}
	return service.config.LocalPublicAuthEnabled()
}

func (service *OIDCLoginService) OIDCOnly() bool {
	return service.Enabled() && !service.LocalPublicAuthEnabled()
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
//     authentication. We trust auth_time when present (REQUIRED by the spec
//     whenever max_age was sent); otherwise we fall back to iat, which is
//     bounded by token lifetime but still useful when auth_time is omitted.
//   - the freshness signal must lie within maxAuthAge of now. A small
//     forward-tolerance handles modest clock skew.
//
// All deviations collapse into ErrOIDCReauthStale or
// ErrOIDCReauthIdentityMismatch so the handler can log them with distinct
// security events while returning a uniform user-facing error.
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

	if !reauthClaimsFresh(exchange.Claims, maxAuthAge, now) {
		return ErrOIDCReauthStale
	}

	_ = service.identities.TouchLastUsed(ctx, identity.ID, effectiveOIDCLoginTime(now))
	return nil
}

func reauthClaimsFresh(claims security.OIDCClaims, maxAuthAge time.Duration, now time.Time) bool {
	if maxAuthAge <= 0 {
		return false
	}
	reference := claims.AuthTime
	if reference.IsZero() {
		reference = claims.IssuedAt
	}
	if reference.IsZero() {
		return false
	}
	if reference.After(now.Add(1 * time.Minute)) {
		// Clock skew tolerance in one direction only — clearly future-dated
		// timestamps look forged or like provider misconfiguration.
		return false
	}
	return now.Sub(reference) <= maxAuthAge
}

func (service *OIDCLoginService) Authenticate(ctx context.Context, code string, codeVerifier string, expectedNonce string, now time.Time) (OIDCLoginResult, error) {
	if !service.Enabled() {
		return OIDCLoginResult{}, ErrOIDCDisabled
	}
	if strings.TrimSpace(code) == "" || strings.TrimSpace(codeVerifier) == "" || strings.TrimSpace(expectedNonce) == "" {
		return OIDCLoginResult{}, ErrOIDCCallbackInvalid
	}

	exchange, err := service.client.ExchangeCode(ctx, code, codeVerifier, expectedNonce)
	if err != nil {
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
	result.Logout = service.buildLogoutState(exchange.Session)
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
func (service *OIDCLoginService) ConfirmAndLinkIdentity(ctx context.Context, targetUserID uint, claims security.OIDCClaims, linkTime time.Time) error {
	if !service.Enabled() {
		return ErrOIDCDisabled
	}
	if targetUserID == 0 {
		return ErrOIDCLinkFailed
	}

	existing, found, err := service.identities.FindByIssuerSubject(ctx, claims.Issuer, claims.Subject)
	if err != nil {
		return ErrOIDCIdentityResolveFailed
	}
	if found {
		if existing.UserID != targetUserID {
			// (issuer, subject) was claimed by somebody else between the OIDC
			// callback that issued the pending-link cookie and this
			// confirmation. Fail closed.
			return ErrOIDCLinkFailed
		}
		_ = service.identities.TouchLastUsed(ctx, existing.ID, effectiveOIDCLoginTime(linkTime)) // codecov:ignore -- best-effort last-used touch; error intentionally ignored
		return nil
	}

	return service.linkIdentity(ctx, targetUserID, claims, linkTime)
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
	_ = service.identities.TouchLastUsed(ctx, identity.ID, loginTime)
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
	identity := models.OIDCIdentity{
		UserID:     userID,
		Issuer:     strings.TrimSpace(claims.Issuer),
		Subject:    strings.TrimSpace(claims.Subject),
		CreatedAt:  linkTime,
		LastUsedAt: &linkTime,
	}
	if err := service.identities.Create(ctx, &identity); err != nil {
		return ErrOIDCLinkFailed
	}
	return nil
}

func effectiveOIDCLoginTime(now time.Time) time.Time {
	if now.IsZero() {
		now = time.Now().UTC()
	}
	return now.UTC()
}

func (service *OIDCLoginService) buildLogoutState(session security.OIDCSession) *OIDCLogoutState {
	if !service.config.ProviderLogoutEnabled() {
		return nil
	}

	endSessionEndpoint := strings.TrimSpace(session.EndSessionEndpoint)
	idTokenHint := strings.TrimSpace(session.IDTokenHint)
	postLogoutRedirectURL := strings.TrimSpace(service.config.ResolvedPostLogoutRedirectURL())
	if endSessionEndpoint == "" || idTokenHint == "" || postLogoutRedirectURL == "" {
		return nil
	}

	return &OIDCLogoutState{
		EndSessionEndpoint:    endSessionEndpoint,
		IDTokenHint:           idTokenHint,
		PostLogoutRedirectURL: postLogoutRedirectURL,
	}
}
