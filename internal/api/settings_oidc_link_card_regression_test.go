package api

import (
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/ovumcy/ovumcy-web/internal/services"
)

// TestSettingsPageShowsOIDCLinkCardOnlyWhenOIDCIsEnabled pins the display
// decision in buildSettingsViewData/settings_account.html: the "link an OIDC
// identity" step-up entry point (issue #701) is a service to the owner only
// when the provider is actually configured — StartOIDCIdentityLinkStepup
// refuses immediately otherwise, so showing the control anyway would be a
// button that can only ever fail.
func TestSettingsPageShowsOIDCLinkCardOnlyWhenOIDCIsEnabled(t *testing.T) {
	t.Parallel()

	t.Run("hidden when OIDC is disabled", func(t *testing.T) {
		t.Parallel()
		fixture := newOIDCStepupFixture(t, "settings-oidc-link-card-hidden@example.com")
		fixture.oidcStub.enabled = false

		request := httptest.NewRequest(http.MethodGet, "/settings", nil)
		request.Header.Set("Accept-Language", "en")
		request.Header.Set("Cookie", fixture.authCookie)
		response := mustAppResponse(t, fixture.app, request)
		assertStatusCode(t, response, http.StatusOK)

		rendered := mustReadBodyString(t, response.Body)
		assertBodyNotContainsAll(t, rendered,
			bodyStringMatch{fragment: `action="/api/v1/users/current/oidc/link/step-up"`, message: "did not expect the OIDC link card when OIDC is disabled"},
		)
	})

	t.Run("shown with the password field and the linked list for a password account", func(t *testing.T) {
		t.Parallel()
		fixture := newOIDCStepupFixture(t, "settings-oidc-link-card-shown@example.com")
		fixture.oidcStub.enabled = true
		giveLinkFixtureAPassword(t, fixture)
		fixture.oidcStub.linkedIdentities = []services.LinkedOIDCIdentity{
			{ID: 41, Issuer: "https://id.example.com", LinkedAt: time.Date(2026, 3, 4, 10, 0, 0, 0, time.UTC)},
		}

		rendered := renderSettingsPageForOIDCCard(t, fixture)
		assertBodyContainsAll(t, rendered,
			bodyStringMatch{fragment: `action="/api/v1/users/current/oidc/link/step-up"`, message: "expected the OIDC link card when OIDC is enabled"},
			bodyStringMatch{fragment: `id="settings-oidc-link-password"`, message: "expected the link form to ask for the current password"},
			bodyStringMatch{fragment: `https://id.example.com`, message: "expected the linked identity's issuer in the list"},
			bodyStringMatch{fragment: `hx-delete="/api/v1/users/current/oidc/identities/41"`, message: "expected an unlink form addressing the listed identity"},
			bodyStringMatch{fragment: `id="settings-oidc-unlink-password-41"`, message: "expected the unlink form to ask for the current password"},
			bodyStringMatch{fragment: `hx-confirm="`, message: "expected the unlink form to confirm before removing"},
		)
	})

	t.Run("an account without a password is told to set one and offered no form", func(t *testing.T) {
		t.Parallel()
		fixture := newOIDCStepupFixture(t, "settings-oidc-link-card-no-password@example.com")
		fixture.oidcStub.enabled = true
		fixture.oidcStub.linkedIdentities = []services.LinkedOIDCIdentity{
			{ID: 42, Issuer: "https://id.example.com", LinkedAt: time.Date(2026, 3, 4, 10, 0, 0, 0, time.UTC)},
		}

		rendered := renderSettingsPageForOIDCCard(t, fixture)
		assertBodyContainsAll(t, rendered,
			bodyStringMatch{fragment: `data-oidc-link-needs-password`, message: "expected the set-a-password hint in place of the link form"},
			bodyStringMatch{fragment: `https://id.example.com`, message: "expected the linked identity to be listed"},
		)
		assertBodyNotContainsAll(t, rendered,
			bodyStringMatch{fragment: `action="/api/v1/users/current/oidc/link/step-up"`, message: "did not expect a link form the server would refuse"},
			bodyStringMatch{fragment: `hx-delete="/api/v1/users/current/oidc/identities/`, message: "did not expect an unlink form the server would refuse"},
		)
	})
}

func renderSettingsPageForOIDCCard(t *testing.T, fixture *oidcStepupFixture) string {
	t.Helper()
	request := httptest.NewRequest(http.MethodGet, "/settings", nil)
	request.Header.Set("Accept-Language", "en")
	request.Header.Set("Cookie", fixture.authCookie)
	response := mustAppResponse(t, fixture.app, request)
	assertStatusCode(t, response, http.StatusOK)
	return mustReadBodyString(t, response.Body)
}
