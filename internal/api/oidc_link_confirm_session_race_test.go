package api

import (
	"context"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"

	"github.com/gofiber/fiber/v3"
	"github.com/ovumcy/ovumcy-web/internal/db"
	"github.com/ovumcy/ovumcy-web/internal/models"
	"github.com/ovumcy/ovumcy-web/internal/services"
	"github.com/pquerna/otp/totp"
	"gorm.io/gorm"
)

// A TOTP re-enrollment revokes every session of the account. When it commits
// while link-confirm is between its factor checks and the session it mints,
// that session must not survive the revocation: the link refuses when the
// re-enrollment lands before its write, and the mint refuses when it lands
// after. The controls pin that the refusal comes from the version check —
// the re-enrollment really ran, and with the link taking whatever version is
// stored the same race hands out a live session — and that an undisturbed
// confirmation still signs the owner in.
func TestCompleteOIDCLinkConfirmationRefusesASessionARevocationCommittedMidwayWouldNotReach(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name                  string
		alreadyLinked         bool
		reenrollBeforeLink    bool
		reenrollAfterLink     bool
		linkFromStoredVersion bool
		wantSession           bool
		wantLinked            bool
	}{
		{name: "re-enrollment before the link write", reenrollBeforeLink: true, wantLinked: false},
		// The already-linked pair writes nothing, so no compare-and-set runs:
		// only the check after the re-read stands between the race and a session.
		{name: "re-enrollment before an already-linked confirmation", alreadyLinked: true, reenrollBeforeLink: true, wantLinked: true},
		{name: "re-enrollment after the link write", reenrollAfterLink: true, wantLinked: true},
		{name: "negative control: link from the stored version", reenrollBeforeLink: true, linkFromStoredVersion: true, wantSession: true, wantLinked: true},
		{name: "positive control: no concurrent write", wantSession: true, wantLinked: true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			workflow := &realLinkOIDCWorkflowService{stubOIDCWorkflowService: newStubOIDCWorkflowService(true)}
			app, database := newOnboardingTestAppWithOptions(t, onboardingTestAppOptions{
				cookieSecure: true,
				oidcService:  workflow,
			})
			repositories := db.NewRepositories(database)
			workflow.real = services.NewOIDCLoginService(enabledOIDCProviderForLinkTest{}, repositories.OIDCIdentities, repositories.Users, nil)

			user := createOnboardingTestUser(t, database, "link-race@example.com", "StrongPass1", true)
			rawSecret := setupTOTPForUser(t, database, user.ID, []byte(testHandlerSecretKey))
			const issuer, subject = "https://idp.example", "subject-race"
			if tc.alreadyLinked {
				identity := models.OIDCIdentity{UserID: user.ID, Issuer: issuer, Subject: subject, CreatedAt: time.Now().UTC()}
				if err := repositories.OIDCIdentities.CreateAndRevokeSessions(context.Background(), &identity, storedAuthSessionVersion(t, database, user.ID)); err != nil {
					t.Fatalf("pre-link the identity: %v", err)
				}
			}
			versionBefore := storedAuthSessionVersion(t, database, user.ID)
			secretBefore := storedTOTPSecret(t, database, user.ID)

			reenroll := func() {
				totpService := services.NewTOTPService(repositories.Users, []byte(testHandlerSecretKey), nil)
				key, err := totpService.GenerateSetupKey("Ovumcy", user.Email)
				if err != nil {
					t.Errorf("GenerateSetupKey: %v", err)
					return
				}
				// Another device's re-enrollment: it was authenticated at the
				// version the account holds right now.
				var current models.User
				if err := database.First(&current, user.ID).Error; err != nil {
					t.Errorf("load the account before re-enrolling: %v", err)
					return
				}
				if err := totpService.EnableTOTP(context.Background(), user.ID, current.AuthSessionVersion, key.Secret()); err != nil {
					t.Errorf("EnableTOTP: %v", err)
				}
			}
			if tc.reenrollBeforeLink {
				workflow.beforeLink = reenroll
			}
			if tc.reenrollAfterLink {
				workflow.afterLink = reenroll
			}
			workflow.linkFromStoredVersion = tc.linkFromStoredVersion
			// The hooks run on the request's goroutine, where t.Fatalf is not allowed.
			workflow.storedVersion = func() int {
				var stored models.User
				if err := database.First(&stored, user.ID).Error; err != nil {
					t.Errorf("load user %d: %v", user.ID, err)
				}
				return stored.AuthSessionVersion
			}

			pendingPayload, err := newOIDCLinkPendingPayload(time.Now().UTC(), user.ID, issuer, subject, user.Email)
			if err != nil {
				t.Fatalf("newOIDCLinkPendingPayload: %v", err)
			}
			code, err := totp.GenerateCode(rawSecret, time.Now())
			if err != nil {
				t.Fatalf("GenerateCode: %v", err)
			}
			postRequest := httptest.NewRequest(http.MethodPost, oidcLinkConfirmPath, strings.NewReader(url.Values{
				"password":  {"StrongPass1"},
				"totp_code": {code},
			}.Encode()))
			postRequest.Header.Set("Content-Type", "application/x-www-form-urlencoded")
			postRequest.Header.Set("Cookie", oidcLinkPendingCookieName+"="+sealLinkPendingCookieForTest(t, pendingPayload))

			response := mustAppResponse(t, app, postRequest)
			assertStatusCode(t, response, http.StatusSeeOther)

			reenrolled := tc.reenrollBeforeLink || tc.reenrollAfterLink
			if reenrolled && storedTOTPSecret(t, database, user.ID) == secretBefore {
				t.Fatal("the re-enrollment did not replace the TOTP secret: the race under test never ran")
			}
			wantVersion := versionBefore
			if reenrolled {
				wantVersion++
			}
			if tc.wantLinked && !tc.alreadyLinked {
				wantVersion++
			}
			if got := storedAuthSessionVersion(t, database, user.ID); got != wantVersion {
				t.Fatalf("expected the stored session version %d, got %d", wantVersion, got)
			}
			if _, linked, err := repositories.OIDCIdentities.FindByIssuerSubject(context.Background(), issuer, subject); err != nil || linked != tc.wantLinked {
				t.Fatalf("expected linked=%v, got linked=%v err=%v", tc.wantLinked, linked, err)
			}

			location := response.Header.Get("Location")
			opens := linkConfirmSessionOpensDashboard(t, app, response)
			if tc.wantSession {
				if location != "/dashboard" || !opens {
					t.Fatalf("expected a session that opens /dashboard, got Location %q opens=%v", location, opens)
				}
				return
			}
			if location != "/login" || opens {
				t.Fatalf("expected a refusal to /login and no usable session, got Location %q opens=%v", location, opens)
			}
		})
	}
}

// linkConfirmSessionOpensDashboard reports whether the auth cookie the
// response set, if any, is accepted on the next request.
func linkConfirmSessionOpensDashboard(t *testing.T, app *fiber.App, response *http.Response) bool {
	t.Helper()
	authCookie := responseCookie(response.Cookies(), authCookieName)
	if authCookie == nil || strings.TrimSpace(authCookie.Value) == "" {
		return false
	}
	request := httptest.NewRequest(http.MethodGet, "/dashboard", nil)
	request.Header.Set("Accept-Language", "en")
	request.Header.Set("Cookie", cookiePair(authCookie))
	return mustAppResponse(t, app, request).StatusCode == http.StatusOK
}

func storedTOTPSecret(t *testing.T, database *gorm.DB, userID uint) string {
	t.Helper()
	var user models.User
	if err := database.First(&user, userID).Error; err != nil {
		t.Fatalf("load user %d: %v", userID, err)
	}
	return user.TOTPSecret
}
