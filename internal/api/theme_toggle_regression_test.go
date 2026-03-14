package api

import (
	"io"
	"net/http"
	"net/http/httptest"
	"regexp"
	"testing"
)

func TestBaseTemplateAvoidsInlineScriptsUnderStrictCSP(t *testing.T) {
	app, _ := newOnboardingTestApp(t)

	request := httptest.NewRequest(http.MethodGet, "/login", nil)
	request.Header.Set("Accept-Language", "en")

	response, err := app.Test(request, -1)
	if err != nil {
		t.Fatalf("login request failed: %v", err)
	}
	defer response.Body.Close()

	if response.StatusCode != http.StatusOK {
		t.Fatalf("expected status 200, got %d", response.StatusCode)
	}

	body, err := io.ReadAll(response.Body)
	if err != nil {
		t.Fatalf("read response body: %v", err)
	}
	rendered := string(body)

	scriptTagPattern := regexp.MustCompile(`(?is)<script\b[^>]*>`)
	for _, tag := range scriptTagPattern.FindAllString(rendered, -1) {
		if regexp.MustCompile(`(?is)\bsrc\s*=`).MatchString(tag) {
			continue
		}
		t.Fatalf("expected login page to avoid inline script tags under strict CSP, found %q", tag)
	}
}
