package services

import (
	"net/http"
	"testing"
)

// TestWebhookHTTPClientIgnoresProxyEnvironment pins the unsupported-proxy
// contract: a proxied request would dial the proxy, and the guard would vet the
// proxy's address instead of the owner's destination.
func TestWebhookHTTPClientIgnoresProxyEnvironment(t *testing.T) {
	t.Setenv("HTTPS_PROXY", "http://127.0.0.1:3128")
	t.Setenv("HTTP_PROXY", "http://127.0.0.1:3128")

	transport, ok := newWebhookHTTPClient(isMetadataIP, fakeResolver{}).Transport.(*http.Transport)
	if !ok {
		t.Fatal("expected the delivery client to use *http.Transport")
	}
	if transport.Proxy != nil {
		t.Fatal("the delivery transport must not consult a proxy")
	}
}

// TestWebhookDialAttemptTimeoutSplitsThePhaseBudget pins the per-record share
// of the dial phase and its floor.
func TestWebhookDialAttemptTimeoutSplitsThePhaseBudget(t *testing.T) {
	if got := webhookDialAttemptTimeout(1); got != webhookPhaseTimeout {
		t.Fatalf("the last attempt gets the whole phase budget, got %v", got)
	}
	if got := webhookDialAttemptTimeout(2); got != webhookPhaseTimeout/2 {
		t.Fatalf("the first of two attempts gets half, got %v", got)
	}
	if got := webhookDialAttemptTimeout(8); got != webhookDialAttemptMinimum {
		t.Fatalf("a share below the floor is raised to %v, got %v", webhookDialAttemptMinimum, got)
	}
}
