package api

import (
	"testing"

	"github.com/ovumcy/ovumcy-web/internal/services"
)

// The stored end-session material is composed into a Location carrying the
// id_token_hint, so the state validator is the last gate before a browser hop.
// `https://:8443/logout` and the unspecified addresses parse as absolute https
// URLs with no fragment and dial this machine; the discovery sanitizer refuses
// that shape, and rows written before it did are still readable here.
func TestValidOIDCLogoutStateRefusesHostsThatDialThisMachine(t *testing.T) {
	t.Parallel()

	valid := services.OIDCLogoutState{
		EndSessionEndpoint:    "https://id.example.com/logout",
		IDTokenHint:           "id-token",
		PostLogoutRedirectURL: "https://ovumcy.example/",
	}
	if !validOIDCLogoutState(valid) {
		t.Fatal("positive control: a well-formed logout state was refused")
	}

	for _, hostile := range []string{"https://:8443/logout", "https://0.0.0.0:8443/logout", "https://[::]:8443/logout"} {
		state := valid
		state.EndSessionEndpoint = hostile
		if validOIDCLogoutState(state) {
			t.Fatalf("end_session_endpoint %q passed the state validator", hostile)
		}
		if got := (&Handler{}).providerLogoutRedirectURLFromState(state); got != "" {
			t.Fatalf("end_session_endpoint %q produced a redirect: %q", hostile, got)
		}

		state = valid
		state.PostLogoutRedirectURL = hostile
		if validOIDCLogoutState(state) {
			t.Fatalf("post_logout_redirect_uri %q passed the state validator", hostile)
		}
	}
}
