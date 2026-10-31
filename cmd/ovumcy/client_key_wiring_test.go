package main

import (
	"io"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gofiber/fiber/v3"
	"github.com/ovumcy/ovumcy-web/internal/api"
)

// TestConfigureFiberMiddlewarePublishesTheEdgeClientKey pins WEB-395 L3. Behind
// a trusted proxy with PROXY_HEADER=X-Forwarded-For, c.IP() is the leftmost,
// client-supplied entry; keyed on it, the per-client auth-attempt buckets let a
// stranger name a victim's address and spend its login budget (eight junk
// requests lock that address out). The handlers key on api.RequestClientKey,
// so the composition root must publish the edge limiters' own key there.
func TestConfigureFiberMiddlewarePublishesTheEdgeClientKey(t *testing.T) {
	proxy := proxySettings{Enabled: true, Header: "X-Forwarded-For", TrustedProxies: []string{"0.0.0.0"}}
	app := fiber.New(fiberConfig(proxy, nil))
	configureFiberMiddleware(app, runtimeConfig{Proxy: proxy}, newRateLimitTestHandler(t))
	app.Get("/probe", func(c fiber.Ctx) error {
		return c.SendString(api.RequestClientKey(c))
	})

	request := httptest.NewRequest(http.MethodGet, "/probe", nil)
	request.Header.Set("X-Forwarded-For", "198.51.100.1, 203.0.113.7")
	response, err := app.Test(request, testConfigNoTimeout)
	if err != nil {
		t.Fatalf("probe request: %v", err)
	}
	defer func() { _ = response.Body.Close() }()
	body, err := io.ReadAll(response.Body)
	if err != nil {
		t.Fatalf("read probe body: %v", err)
	}
	if got := string(body); got != "203.0.113.7" {
		t.Fatalf("expected the per-client key to be the rightmost untrusted hop 203.0.113.7, got %q (the spoofable leftmost entry is 198.51.100.1)", got)
	}
}
