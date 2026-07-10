package api

import "testing"

// TestOIDCResponseModeQueryNilGuards covers the defensive nil guards in
// oidcResponseModeQuery: a nil handler and a handler without an OIDC service
// both must report form_post (not query), so route registration and the
// callback source default safely when OIDC is disabled.
func TestOIDCResponseModeQueryNilGuards(t *testing.T) {
	var nilHandler *Handler
	if nilHandler.oidcResponseModeQuery() {
		t.Fatal("nil handler must not report query response mode")
	}
	if (&Handler{}).oidcResponseModeQuery() {
		t.Fatal("handler without an OIDC service must not report query response mode")
	}
}
