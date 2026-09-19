package api

import (
	"testing"

	"github.com/ovumcy/ovumcy-web/internal/services"
)

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
