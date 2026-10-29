package api

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"golang.org/x/net/html"
)

// The enrollment seed is a 32-character run that is hard to transcribe by eye:
// it is shown in groups of four and can be copied in one click. The display
// itself is the sanctioned exception and stays.
func TestTOTPSetupPageGroupsTheSecretAndOffersACopyButton(t *testing.T) {
	ctx := newTOTPSettingsContext(t, "totp-secret-display@example.com")

	req := httptest.NewRequest(http.MethodGet, "/settings/2fa", nil)
	req.Header.Set("Accept-Language", "en")
	req.Header.Set("Cookie", ctx.authCookie)
	resp, err := ctx.app.Test(req, testConfigNoTimeout)
	if err != nil {
		t.Fatalf("GET /settings/2fa: %v", err)
	}
	defer func() { _ = resp.Body.Close() }()

	document := mustParseHTMLDocument(t, mustReadBodyString(t, resp.Body))
	secret := htmlFindElement(document, func(node *html.Node) bool {
		return node.Type == html.ElementNode && htmlHasAttr(node, "data-totp-manual-secret")
	})
	if secret == nil {
		t.Fatal("expected the manual secret element")
	}

	groups := strings.Fields(htmlNodeText(secret))
	if len(groups) < 4 {
		t.Fatalf("expected the secret in several groups, got %q", htmlNodeText(secret))
	}
	for _, group := range groups[:len(groups)-1] {
		if len(group) != 4 {
			t.Fatalf("expected full groups of four, got %q in %v", group, groups)
		}
	}

	if !htmlHasAttr(secret, "data-recovery-code-value") {
		t.Fatal("expected the secret to be the copy source (data-recovery-code-value)")
	}
	tools := htmlFindElement(document, func(node *html.Node) bool {
		return node.Type == html.ElementNode && htmlHasAttr(node, "data-recovery-code-tools")
	})
	if tools == nil {
		t.Fatal("expected a copy-tools root around the secret")
	}
	copyButton := htmlFindElement(tools, func(node *html.Node) bool {
		return node.Type == html.ElementNode && node.Data == "button" && htmlAttr(node, "data-recovery-action") == "copy"
	})
	if copyButton == nil {
		t.Fatal("expected a copy button inside the copy-tools root")
	}
	if htmlFindElement(tools, func(node *html.Node) bool { return node == secret }) == nil {
		t.Fatal("expected the secret inside the copy-tools root")
	}
}
