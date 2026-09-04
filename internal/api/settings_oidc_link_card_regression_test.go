package api

import (
	"net/http"
	"net/http/httptest"
	"testing"
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

	t.Run("shown when OIDC is enabled", func(t *testing.T) {
		t.Parallel()
		fixture := newOIDCStepupFixture(t, "settings-oidc-link-card-shown@example.com")
		fixture.oidcStub.enabled = true

		request := httptest.NewRequest(http.MethodGet, "/settings", nil)
		request.Header.Set("Accept-Language", "en")
		request.Header.Set("Cookie", fixture.authCookie)
		response := mustAppResponse(t, fixture.app, request)
		assertStatusCode(t, response, http.StatusOK)

		rendered := mustReadBodyString(t, response.Body)
		assertBodyContainsAll(t, rendered,
			bodyStringMatch{fragment: `action="/api/v1/users/current/oidc/link/step-up"`, message: "expected the OIDC link card when OIDC is enabled"},
		)
	})
}
