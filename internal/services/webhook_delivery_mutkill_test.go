package services

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
)

// TestWebhookDeliveryTreats300AsFailure kills the webhook_delivery.go:294
// CONDITIONALS_BOUNDARY survivor on the 2xx success check:
//
//	if response.StatusCode < 200 || response.StatusCode >= 300 {
//
// Relaxing `>= 300` to `> 300` would accept a 300 Multiple Choices as a
// successful delivery. A 300 is not a delivered notification, and (unlike
// 301/302) Go's http client does not auto-follow it, so the response reaches this
// status check directly. The existing tests only exercise 200 (success) and 500
// (failure), leaving the exact 300 boundary unguarded — which is why it survived.
func TestWebhookDeliveryTreats300AsFailure(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, _ *http.Request) {
		writer.WriteHeader(http.StatusMultipleChoices) // 300
	}))
	defer server.Close()

	deliverer := NewWebhookDeliverer(false)
	err := deliverer.Deliver(context.Background(), server.URL, samplePayload())
	if err == nil {
		t.Fatal("a 300 (Multiple Choices) response must be treated as a delivery failure, got nil error")
	}
}
