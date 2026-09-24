package api

import (
	"context"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"

	"github.com/ovumcy/ovumcy-web/internal/db"
	"github.com/ovumcy/ovumcy-web/internal/models"
	"github.com/ovumcy/ovumcy-web/internal/security"
	"github.com/ovumcy/ovumcy-web/internal/services"
	"gorm.io/gorm"
)

// enabledOIDCProviderForLinkTest is the smallest provider the real
// OIDCLoginService accepts as enabled. The link-confirm path never talks to
// the provider, so the exchange methods are never reached.
type enabledOIDCProviderForLinkTest struct{}

func (enabledOIDCProviderForLinkTest) Enabled() bool                { return true }
func (enabledOIDCProviderForLinkTest) LocalPublicAuthEnabled() bool { return true }
func (enabledOIDCProviderForLinkTest) Config() security.OIDCConfig {
	return security.OIDCConfig{Enabled: true, LoginMode: security.OIDCLoginModeHybrid}
}
func (enabledOIDCProviderForLinkTest) AuthCodeURL(context.Context, string, string, string, map[string]string) (string, error) {
	panic("the link-confirm path never starts a provider round trip")
}
func (enabledOIDCProviderForLinkTest) ExchangeCode(context.Context, string, string, string) (security.OIDCExchangeResult, error) {
	panic("the link-confirm path never exchanges a code")
}

// realLinkOIDCWorkflowService keeps the stub for every workflow method this
// test does not exercise and routes ConfirmAndLinkIdentity through the real
// service and repository, so the link's AuthSessionVersion bump lands in the
// same database the session check reads. It is bound after the app exists,
// because the database is created by the app helper.
//
// beforeLink and afterLink commit a concurrent write on either side of the
// link; linkFromStoredVersion hands the link the version stored at that moment
// instead of the one the handler verified, which is what the link did before
// it took a version at all.
type realLinkOIDCWorkflowService struct {
	*stubOIDCWorkflowService
	real *services.OIDCLoginService

	beforeLink            func()
	afterLink             func()
	linkFromStoredVersion bool
	storedVersion         func() int
}

func (service *realLinkOIDCWorkflowService) ConfirmAndLinkIdentity(ctx context.Context, targetUserID uint, expectedSessionVersion int, claims security.OIDCClaims, linkTime time.Time) (int, error) {
	if service.beforeLink != nil {
		service.beforeLink()
	}
	if service.linkFromStoredVersion {
		expectedSessionVersion = service.storedVersion()
	}
	linkedVersion, err := service.real.ConfirmAndLinkIdentity(ctx, targetUserID, expectedSessionVersion, claims, linkTime)
	if service.afterLink != nil {
		service.afterLink()
	}
	return linkedVersion, err
}

// A confirmed link bumps AuthSessionVersion in the same write, so the session
// the confirmation mints must carry the bumped version: one minted from the
// account as read before the link is revoked on its first use, and the owner
// who just proved her password lands back on /login.
func TestCompleteOIDCLinkConfirmationMintsASessionThatSurvivesTheLinksRevocation(t *testing.T) {
	t.Parallel()

	workflow := &realLinkOIDCWorkflowService{stubOIDCWorkflowService: newStubOIDCWorkflowService(true)}
	app, database := newOnboardingTestAppWithOptions(t, onboardingTestAppOptions{
		cookieSecure: true,
		oidcService:  workflow,
	})
	repositories := db.NewRepositories(database)
	workflow.real = services.NewOIDCLoginService(enabledOIDCProviderForLinkTest{}, repositories.OIDCIdentities, repositories.Users, nil)

	user := createOnboardingTestUser(t, database, "link-version@example.com", "StrongPass1", true)
	versionBefore := storedAuthSessionVersion(t, database, user.ID)

	pendingPayload, err := newOIDCLinkPendingPayload(time.Now().UTC(), user.ID, "https://idp.example", "subject-version", user.Email)
	if err != nil {
		t.Fatalf("newOIDCLinkPendingPayload: %v", err)
	}
	postRequest := httptest.NewRequest(http.MethodPost, oidcLinkConfirmPath, strings.NewReader(url.Values{
		"password": {"StrongPass1"},
	}.Encode()))
	postRequest.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	postRequest.Header.Set("Cookie", oidcLinkPendingCookieName+"="+sealLinkPendingCookieForTest(t, pendingPayload))

	response := mustAppResponse(t, app, postRequest)
	assertStatusCode(t, response, http.StatusSeeOther)
	if location := response.Header.Get("Location"); location != "/dashboard" {
		t.Fatalf("expected redirect to /dashboard after the link, got %q", location)
	}
	if got := storedAuthSessionVersion(t, database, user.ID); got != versionBefore+1 {
		t.Fatalf("expected the real link to bump the session version from %d to %d, got %d", versionBefore, versionBefore+1, got)
	}
	authCookie := responseCookie(response.Cookies(), authCookieName)
	if authCookie == nil || strings.TrimSpace(authCookie.Value) == "" {
		t.Fatal("expected an auth cookie after the confirmed link")
	}

	dashboardRequest := httptest.NewRequest(http.MethodGet, "/dashboard", nil)
	dashboardRequest.Header.Set("Accept-Language", "en")
	dashboardRequest.Header.Set("Cookie", cookiePair(authCookie))
	dashboardResponse := mustAppResponse(t, app, dashboardRequest)
	if dashboardResponse.StatusCode != http.StatusOK {
		t.Fatalf("expected the freshly minted session to open /dashboard, got %d (Location %q)", dashboardResponse.StatusCode, dashboardResponse.Header.Get("Location"))
	}
}

func storedAuthSessionVersion(t *testing.T, database *gorm.DB, userID uint) int {
	t.Helper()
	var user models.User
	if err := database.First(&user, userID).Error; err != nil {
		t.Fatalf("load user %d: %v", userID, err)
	}
	return user.AuthSessionVersion
}
