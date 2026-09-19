package api

import (
	"net/url"
	"testing"

	"github.com/ovumcy/ovumcy-web/internal/services"
)

// The post-logout return address is taken from the current configuration at
// redirect time, never from the stored row: a row written under an earlier
// configuration, or carrying a foreign address, would otherwise tell the
// provider where to send the browser after sign-out. With no configured
// address there is no provider redirect at all.
func TestProviderLogoutRedirectTakesThePostLogoutAddressFromTheConfiguration(t *testing.T) {
	t.Parallel()

	handler := &Handler{oidcService: newStubOIDCWorkflowService(true)}
	for _, stored := range []string{
		"https://evil.example/landing",
		"https://old-ovumcy.example.com/login",
		testOIDCPostLogoutRedirectURL,
	} {
		state := services.OIDCLogoutState{
			EndSessionEndpoint:    testOIDCIssuerURL + "/logout",
			IDTokenHint:           "id-token",
			PostLogoutRedirectURL: stored,
		}
		got := handler.providerLogoutRedirectURLFromState(state)
		location, err := url.Parse(got)
		if got == "" || err != nil {
			t.Fatalf("stored post-logout address %q: expected a provider redirect, got %q (%v)", stored, got, err)
		}
		if redirect := location.Query().Get("post_logout_redirect_uri"); redirect != testOIDCPostLogoutRedirectURL {
			t.Fatalf("stored post-logout address %q: provider redirect carried %q, want the configured %q", stored, redirect, testOIDCPostLogoutRedirectURL)
		}
	}

	unconfigured := newStubOIDCWorkflowService(true)
	unconfigured.postLogoutRedirectURL = ""
	state := services.OIDCLogoutState{
		EndSessionEndpoint:    testOIDCIssuerURL + "/logout",
		IDTokenHint:           "id-token",
		PostLogoutRedirectURL: testOIDCPostLogoutRedirectURL,
	}
	if got := (&Handler{oidcService: unconfigured}).providerLogoutRedirectURLFromState(state); got != "" {
		t.Fatalf("no configured post-logout address: produced a redirect from the stored one: %q", got)
	}
	if got := (&Handler{}).oidcPostLogoutRedirectURL(); got != "" {
		t.Fatalf("a handler with no OIDC service reported a post-logout address: %q", got)
	}
}

// The stored end-session endpoint is pinned to the configured issuer origin
// with the same comparison discovery applies: a row carrying a foreign origin —
// written before the pin, or under an issuer since replaced — would otherwise
// hand the id_token_hint to a host the issuer does not own. The pin is observed
// on the path that composes the Location, through a handler whose OIDC service
// reports the issuer.
func TestProviderLogoutRedirectPinsTheStoredEndSessionEndpointToTheIssuerOrigin(t *testing.T) {
	t.Parallel()

	handler := &Handler{oidcService: newStubOIDCWorkflowService(true)}
	valid := services.OIDCLogoutState{
		EndSessionEndpoint:    testOIDCIssuerURL + "/logout",
		IDTokenHint:           "id-token",
		PostLogoutRedirectURL: "https://ovumcy.example/",
	}
	if got := handler.providerLogoutRedirectURLFromState(valid); got == "" {
		t.Fatal("positive control: a same-origin end_session_endpoint produced no redirect")
	}

	for _, foreign := range []string{
		"https://evil.example/logout",
		"https://logout.id.example.com/logout",
		"https://id.example.com:8443/logout",
	} {
		state := valid
		state.EndSessionEndpoint = foreign
		if got := handler.providerLogoutRedirectURLFromState(state); got != "" {
			t.Fatalf("off-origin end_session_endpoint %q produced a redirect: %q", foreign, got)
		}
	}

	// An absent issuer is invalid input, never a reason to skip the pin: a
	// handler whose OIDC service names no issuer, or that has none, composes no
	// provider redirect at all.
	noIssuer := newStubOIDCWorkflowService(true)
	noIssuer.issuerURL = ""
	for name, candidate := range map[string]*Handler{
		"service names no issuer": {oidcService: noIssuer},
		"no oidc service":         {},
	} {
		if got := candidate.providerLogoutRedirectURLFromState(valid); got != "" {
			t.Fatalf("%s: produced a redirect: %q", name, got)
		}
	}
}
