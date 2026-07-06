package api

import (
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/gofiber/fiber/v3"
)

// runDayStatusHelperCtx drives fn inside a real Fiber request context with the
// given localized message set installed, returning fn's result plus the
// response header set (so header-only helpers can be asserted). It is the seam
// the day/settings status-markup helper tests use to exercise the
// translate-and-fallback branches those helpers own.
func runDayStatusHelperCtx(t *testing.T, messages map[string]string, fn func(handler *Handler, c fiber.Ctx) string) (string, http.Header) {
	t.Helper()

	handler := &Handler{}
	app := fiber.New()
	var result string
	var header http.Header
	app.Get("/*", func(c fiber.Ctx) error {
		if messages != nil {
			c.Locals(contextMessagesKey, messages)
		}
		result = fn(handler, c)
		// Snapshot response headers set by the helper before the handler returns.
		header = http.Header{}
		for key, value := range c.Response().Header.All() {
			header.Add(string(key), string(value))
		}
		return c.SendStatus(http.StatusOK)
	})
	request := httptest.NewRequest(http.MethodGet, "/settings", nil)
	response, err := app.Test(request, testConfigNoTimeout)
	if err != nil {
		t.Fatalf("day status helper request failed: %v", err)
	}
	_ = response.Body.Close()
	return result, header
}

// TestLocalizedStatusDismissLabel pins the close-label fallback (survivor: the
// `closeLabel == "" || closeLabel == "common.close"` guard): a real translation
// is used verbatim, and a missing translation falls back to "Close" rather than
// leaking the raw key into the toast's aria-label.
func TestLocalizedStatusDismissLabel(t *testing.T) {
	t.Parallel()

	if got := localizedStatusDismissLabel(map[string]string{"common.close": "Dismiss"}); got != "Dismiss" {
		t.Fatalf("expected translated close label, got %q", got)
	}
	if got := localizedStatusDismissLabel(map[string]string{}); got != "Close" {
		t.Fatalf("expected fallback close label 'Close', got %q", got)
	}
}

// TestHtmxSettingsSuccessMarkupFallsBackToDefault pins the success-message
// fallback (survivor: the `message == "" || message == messageKey` guard): when
// a status has no translation the provided default message is rendered into the
// toast; when it does, the translation is used instead.
func TestHtmxSettingsSuccessMarkupFallsBackToDefault(t *testing.T) {
	t.Parallel()

	// "profile_updated" resolves to a settings status translation key; with no
	// message map that key is untranslated, so the default message must appear.
	markup, _ := runDayStatusHelperCtx(t, nil, func(handler *Handler, c fiber.Ctx) string {
		return htmxSettingsSuccessMarkup(c, "profile_updated", "Saved profile")
	})
	if !strings.Contains(markup, "Saved profile") {
		t.Fatalf("expected default message in success markup, got %q", markup)
	}

	// With the status key translated, the translation wins over the default.
	statusKey := "settings.success.profile_updated"
	markupTranslated, _ := runDayStatusHelperCtx(t, map[string]string{statusKey: "Profile updated"}, func(handler *Handler, c fiber.Ctx) string {
		return htmxSettingsSuccessMarkup(c, "profile_updated", "Saved profile")
	})
	if strings.Contains(markupTranslated, "Profile updated") == strings.Contains(markupTranslated, "Saved profile") {
		t.Fatalf("expected exactly one of translation/default, got %q", markupTranslated)
	}
	if !strings.Contains(markupTranslated, "Profile updated") {
		t.Fatalf("expected translated message to win, got %q", markupTranslated)
	}
}

// TestSetEncodedResponseNoticeSkipsBlank pins the blank guard in
// setEncodedResponseNotice (survivor: the `trimmed == ""` conditional): a
// non-empty message is URL-encoded into the X-Ovumcy-Notice header, and a blank
// message sets no header at all.
func TestSetEncodedResponseNoticeSkipsBlank(t *testing.T) {
	t.Parallel()

	_, header := runDayStatusHelperCtx(t, nil, func(handler *Handler, c fiber.Ctx) string {
		setEncodedResponseNotice(c, "cycle saved")
		return ""
	})
	if got := header.Get("X-Ovumcy-Notice"); got != "cycle+saved" && got != "cycle%20saved" {
		t.Fatalf("expected url-encoded notice header, got %q", got)
	}

	_, blankHeader := runDayStatusHelperCtx(t, nil, func(handler *Handler, c fiber.Ctx) string {
		setEncodedResponseNotice(c, "   ")
		return ""
	})
	if got := blankHeader.Get("X-Ovumcy-Notice"); got != "" {
		t.Fatalf("expected no notice header for a blank message, got %q", got)
	}
}

// TestSendDaySaveStatusPatternSelection pins the pattern/key branches in
// sendDaySaveStatus (survivors: the `patternKey == ""` default, the
// `pattern == "" || pattern == patternKey` fallback, and the two
// `patternKey == "common.saved_at"` checks that decide the "Saved at %s" vs
// "Saved." shape and whether the timestamp is formatted in). The response body
// is the rendered toast markup.
func TestSendDaySaveStatusPatternSelection(t *testing.T) {
	t.Parallel()

	render := func(messages map[string]string, messageKey string) string {
		handler := &Handler{}
		app := fiber.New()
		app.Get("/*", func(c fiber.Ctx) error {
			if messages != nil {
				c.Locals(contextMessagesKey, messages)
			}
			return handler.sendDaySaveStatus(c, messageKey)
		})
		request := httptest.NewRequest(http.MethodGet, "/settings", nil)
		response, err := app.Test(request, testConfigNoTimeout)
		if err != nil {
			t.Fatalf("sendDaySaveStatus request failed: %v", err)
		}
		defer func() { _ = response.Body.Close() }()
		body, err := io.ReadAll(response.Body)
		if err != nil {
			t.Fatalf("read sendDaySaveStatus body: %v", err)
		}
		return string(body)
	}

	// Empty key -> "common.saved_at" default -> "Saved at HH:MM": the %s verb
	// must be REPLACED by the formatted timestamp (asserting the absence of a
	// literal %s distinguishes the format-in branch from a skipped Sprintf).
	defaultBody := render(nil, "")
	if !strings.Contains(defaultBody, "Saved at ") {
		t.Fatalf("expected default 'Saved at' pattern, got %q", defaultBody)
	}
	if strings.Contains(defaultBody, "%s") {
		t.Fatalf("expected the timestamp verb to be formatted in, got raw pattern %q", defaultBody)
	}

	// A non-saved_at key with no translation -> plain "Saved." (no timestamp).
	otherBody := render(nil, "common.saved")
	if !strings.Contains(otherBody, "Saved.") {
		t.Fatalf("expected plain 'Saved.' for a non-saved_at key, got %q", otherBody)
	}
	if strings.Contains(otherBody, "Saved at ") {
		t.Fatalf("did not expect a timestamped message for a non-saved_at key, got %q", otherBody)
	}

	// A translated saved_at pattern is formatted with the timestamp argument,
	// so no literal %s verb survives into the rendered toast.
	translatedBody := render(map[string]string{"common.saved_at": "Stored %s"}, "common.saved_at")
	if !strings.Contains(translatedBody, "Stored ") {
		t.Fatalf("expected translated saved_at pattern to be formatted, got %q", translatedBody)
	}
	if strings.Contains(translatedBody, "%s") {
		t.Fatalf("expected translated saved_at verb to be formatted in, got %q", translatedBody)
	}
}
