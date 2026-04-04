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
)

type OIDCProviderClient interface {
	Enabled() bool
	LocalPublicAuthEnabled() bool
	Config() security.OIDCConfig
	AuthCodeURL(ctx context.Context, state string, nonce string, codeVerifier string) (string, error)
	ExchangeCode(ctx context.Context, code string, codeVerifier string, expectedNonce string) (security.OIDCExchangeResult, error)
}

type OIDCIdentityStore interface {
	FindByIssuerSubject(issuer string, subject string) (models.OIDCIdentity, bool, error)
	Create(identity *models.OIDCIdentity) error
	TouchLastUsed(identityID uint, usedAt time.Time) error
}

type OIDCUserStore interface {
	FindByID(userID uint) (models.User, error)
	FindByNormalizedEmailOptional(email string) (models.User, bool, error)
}

type OIDCAutoProvisioner interface {
	AutoProvisionOwnerAccount(email string, createdAt time.Time) (models.User, error)
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
	if !service.Enabled() {
		return "", ErrOIDCDisabled
	}
	if strings.TrimSpace(state) == "" || strings.TrimSpace(nonce) == "" || strings.TrimSpace(codeVerifier) == "" {
		return "", ErrOIDCCallbackInvalid
	}
	url, err := service.client.AuthCodeURL(ctx, state, nonce, codeVerifier)
	if err != nil {
		return "", ErrOIDCUnavailable
	}
	return url, nil
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

	result, err := service.authenticateExchange(exchange, effectiveOIDCLoginTime(now))
	if err != nil {
		return OIDCLoginResult{}, err
	}
	result.Logout = service.buildLogoutState(exchange.Session)
	return result, nil
}

func (service *OIDCLoginService) authenticateExchange(exchange security.OIDCExchangeResult, loginTime time.Time) (OIDCLoginResult, error) {
	if result, found, err := service.authenticateLinkedIdentity(exchange, loginTime); found || err != nil {
		return result, err
	}

	user, autoProvisioned, err := service.resolveUserForClaims(exchange.Claims, loginTime)
	if err != nil {
		return OIDCLoginResult{}, err
	}
	if err := service.linkIdentity(user.ID, exchange.Claims, loginTime); err != nil {
		return OIDCLoginResult{}, err
	}

	return OIDCLoginResult{
		User:            user,
		NewlyLinked:     true,
		AutoProvisioned: autoProvisioned,
	}, nil
}

func (service *OIDCLoginService) authenticateLinkedIdentity(exchange security.OIDCExchangeResult, loginTime time.Time) (OIDCLoginResult, bool, error) {
	identity, found, err := service.identities.FindByIssuerSubject(exchange.Claims.Issuer, exchange.Claims.Subject)
	if err != nil {
		return OIDCLoginResult{}, false, ErrOIDCIdentityResolveFailed
	}
	if !found {
		return OIDCLoginResult{}, false, nil
	}

	user, err := service.users.FindByID(identity.UserID)
	if err != nil {
		return OIDCLoginResult{}, true, ErrOIDCIdentityResolveFailed
	}
	if err := ValidateSupportedWebUser(&user); err != nil {
		return OIDCLoginResult{}, true, ErrOIDCAccountUnavailable
	}
	_ = service.identities.TouchLastUsed(identity.ID, loginTime)
	return OIDCLoginResult{User: user}, true, nil
}

func (service *OIDCLoginService) resolveUserForClaims(claims security.OIDCClaims, loginTime time.Time) (models.User, bool, error) {
	normalizedEmail := NormalizeAuthEmail(claims.Email)
	if !claims.EmailVerified || normalizedEmail == "" {
		return models.User{}, false, ErrOIDCAccountUnavailable
	}
	return service.findOrProvisionUser(normalizedEmail, loginTime)
}

func (service *OIDCLoginService) findOrProvisionUser(normalizedEmail string, loginTime time.Time) (models.User, bool, error) {
	user, found, err := service.users.FindByNormalizedEmailOptional(normalizedEmail)
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
	return service.autoProvisionOrLookupUser(normalizedEmail, loginTime)
}

func (service *OIDCLoginService) autoProvisionOrLookupUser(normalizedEmail string, loginTime time.Time) (models.User, bool, error) {
	user, err := service.provisioner.AutoProvisionOwnerAccount(normalizedEmail, loginTime)
	if err == nil {
		return user, true, nil
	}
	if !errors.Is(err, ErrAuthEmailExists) {
		return models.User{}, false, ErrOIDCProvisionFailed
	}

	user, found, lookupErr := service.users.FindByNormalizedEmailOptional(normalizedEmail)
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

func (service *OIDCLoginService) linkIdentity(userID uint, claims security.OIDCClaims, linkTime time.Time) error {
	identity := models.OIDCIdentity{
		UserID:     userID,
		Issuer:     strings.TrimSpace(claims.Issuer),
		Subject:    strings.TrimSpace(claims.Subject),
		CreatedAt:  linkTime,
		LastUsedAt: &linkTime,
	}
	if err := service.identities.Create(&identity); err != nil {
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
