package api

import (
	"net/http"
	"net/http/httptest"
	neturl "net/url"
	"strings"
	"testing"

	"github.com/gofiber/fiber/v2"
)

func newExportRequestForTest(t *testing.T, target string, authCookie string) *http.Request {
	t.Helper()

	parsed, err := neturl.Parse(target)
	if err != nil {
		t.Fatalf("parse export target %q: %v", target, err)
	}

	form := parsed.Query()
	request := httptest.NewRequest(http.MethodPost, parsed.Path, strings.NewReader(form.Encode()))
	request.Header.Set("Content-Type", fiber.MIMEApplicationForm)
	request.Header.Set("Cookie", authCookie)
	return request
}
