package api

import (
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/gofiber/fiber/v3"
	"github.com/ovumcy/ovumcy-web/internal/models"
	"golang.org/x/net/html"
)

// The egress card's result lands beside the control that triggered it, a refused
// URL comes back in its field, and focus is put somewhere that exists after the
// card replaced itself.

func egressFeedbackAnswer(t *testing.T, seed egressRenderRow, method string, path string, form string) *html.Node {
	t.Helper()

	handler, database, _ := newCountingEgressHandler(t)
	owner := createEgressLedgerUser(t, database, "egress-feedback-"+strings.ToLower(strings.ReplaceAll(method+path, "/", "-"))+"@example.com", models.RoleOwner)
	seedEgressRow(t, database, owner.ID, seed)

	app := fiber.New()
	app.Use(handler.LanguageMiddleware)
	app.Use(func(c fiber.Ctx) error {
		reloaded := reloadEgressUser(t, database, owner.ID)
		c.Locals(contextUserKey, &reloaded)
		return c.Next()
	})
	app.Delete("/api/v1/users/current/webhook", handler.RemoveWebhookDestination)
	app.Delete("/api/v1/users/current/calendar-feed", handler.RevokeCalendarFeed)
	app.Post("/api/v1/users/current/webhook", handler.UpdateWebhookSettings)

	var body io.Reader
	if form != "" {
		body = strings.NewReader(form)
	}
	request := httptest.NewRequest(method, path, body)
	if form != "" {
		request.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	}
	request.Header.Set("HX-Request", "true")
	request.Header.Set("Accept", "text/html")
	response, err := app.Test(request, testConfigNoTimeout)
	if err != nil {
		t.Fatalf("mutation failed: %v", err)
	}
	defer func() { _ = response.Body.Close() }()
	return mustParseHTMLDocument(t, mustReadBodyString(t, response.Body))
}

func mustFindByID(t *testing.T, root *html.Node, id string) *html.Node {
	t.Helper()
	node := htmlElementByID(root, id)
	if node == nil {
		t.Fatalf("the answer carries no #%s", id)
	}
	return node
}

// elementWithAttr finds the first element CARRYING the attribute; the shared
// by-attribute helper treats a missing attribute as the empty value.
func elementWithAttr(root *html.Node, name string) *html.Node {
	return htmlFindElement(root, func(node *html.Node) bool {
		return node.Type == html.ElementNode && htmlHasAttr(node, name)
	})
}

func htmlWithin(root *html.Node, wanted *html.Node) bool {
	for node := wanted; node != nil; node = node.Parent {
		if node == root {
			return true
		}
	}
	return false
}

func hasStatusChild(root *html.Node, class string) bool {
	return htmlFindElement(root, func(node *html.Node) bool {
		return node.Type == html.ElementNode && htmlHasClass(node, class)
	}) != nil
}

const refusedWebhookForm = "webhook_url=ftp%3A%2F%2Fhooks.example.test%2Ftypo-token&webhook_enabled=true&webhook_notify_period=true"

func TestEgressRefusedWebhookURLComesBackInItsFieldAndTakesFocus(t *testing.T) {
	document := egressFeedbackAnswer(t, egressRenderRow{}, http.MethodPost, "/api/v1/users/current/webhook", refusedWebhookForm)

	input := mustFindByID(t, document, "settings-webhook-url")
	if got := htmlAttr(input, "value"); got != "ftp://hooks.example.test/typo-token" {
		t.Fatalf("the refused URL must come back in its field, got %q", got)
	}
	if htmlAttr(input, "aria-invalid") != "true" {
		t.Fatal("the refused field must be marked aria-invalid")
	}
	if !htmlHasAttr(input, "data-focus-after-swap") {
		t.Fatal("the refused field must take focus after the swap")
	}
	if !strings.Contains(htmlAttr(input, "aria-describedby"), "settings-egress-status") {
		t.Fatalf("the field must point at the error that explains it, got %q", htmlAttr(input, "aria-describedby"))
	}
	if htmlAttr(input, "pattern") != "https?://.+" {
		t.Fatalf("the URL field must hint http(s) early, got pattern %q", htmlAttr(input, "pattern"))
	}
}

func TestEgressRefusedWebhookResultSitsInsideTheWebhookForm(t *testing.T) {
	document := egressFeedbackAnswer(t, egressRenderRow{}, http.MethodPost, "/api/v1/users/current/webhook", refusedWebhookForm)

	form := elementWithAttr(document, "data-settings-webhook-form")
	if form == nil {
		t.Fatal("the answer carries no webhook form")
	}
	webhookIsland := mustFindByID(t, document, "settings-egress-status")
	if !htmlWithin(form, webhookIsland) {
		t.Fatal("the webhook result must sit inside the webhook form, beside its Save")
	}
	if !hasStatusChild(webhookIsland, "status-error") {
		t.Fatal("the refusal must be inside the webhook live region")
	}
	if hasStatusChild(mustFindByID(t, document, "settings-egress-feed-status"), "status-error") {
		t.Fatal("a webhook refusal must not appear beside the calendar-feed buttons")
	}
}

func TestEgressFeedRevokeResultSitsBesideTheFeedButtonsAndKeepsFocus(t *testing.T) {
	document := egressFeedbackAnswer(t, egressRenderRow{
		feedSelector:        "sel7777777777777777777777777777",
		useCurrentFeedEpoch: true,
	}, http.MethodDelete, "/api/v1/users/current/calendar-feed", "")

	if !hasStatusChild(mustFindByID(t, document, "settings-egress-feed-status"), "status-ok") {
		t.Fatal("the revoke result must be in the live region beside the feed buttons")
	}
	if hasStatusChild(mustFindByID(t, document, "settings-egress-status"), "status-ok") {
		t.Fatal("the revoke result must not appear at the webhook form")
	}
	marked := htmlFindElements(document, func(node *html.Node) bool {
		return node.Type == html.ElementNode && htmlHasAttr(node, "data-focus-after-swap")
	})
	if len(marked) != 1 {
		t.Fatalf("exactly one control must take focus after the revoke, got %d", len(marked))
	}
	if marked[0].Data != "button" {
		t.Fatalf("focus must land on a button, got <%s>", marked[0].Data)
	}
	generate := elementWithAttr(document, "data-settings-calendar-feed-generate")
	if generate == nil || !htmlWithin(generate, marked[0]) {
		t.Fatal("after a revoke the button that exists is the create button, and it must take focus")
	}
}

func TestEgressSuccessfulWebhookSaveDoesNotEchoTheURL(t *testing.T) {
	document := egressFeedbackAnswer(t, egressRenderRow{}, http.MethodPost, "/api/v1/users/current/webhook",
		"webhook_url=https%3A%2F%2Fhooks.example.test%2Fsecret-token&webhook_enabled=true&webhook_notify_period=true")

	input := mustFindByID(t, document, "settings-webhook-url")
	if htmlHasAttr(input, "value") || htmlHasAttr(input, "aria-invalid") || htmlHasAttr(input, "data-focus-after-swap") {
		t.Fatal("a saved URL must not be echoed back or marked invalid")
	}
	if strings.Contains(htmlDocumentText(document), "secret-token") {
		t.Fatal("the saved URL's token reached the page text")
	}
	save := elementWithAttr(document, "data-settings-webhook-save")
	if save == nil || !htmlHasAttr(save, "data-focus-after-swap") {
		t.Fatal("focus must return to the webhook Save after a saved result")
	}
}
