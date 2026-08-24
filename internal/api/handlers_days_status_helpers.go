package api

import (
	"fmt"
	"net/url"
	"strings"
	"time"

	"github.com/gofiber/fiber/v3"
	"github.com/ovumcy/ovumcy-web/internal/httpx"
	"github.com/ovumcy/ovumcy-web/internal/services"
)

func htmxDismissibleSuccessStatusMarkup(messages map[string]string, message string) string {
	return httpx.DismissibleStatusOKMarkup(message, localizedStatusDismissLabel(messages))
}

// htmxSettingsSuccessMarkup resolves a settings success status into the
// localized dismissible status markup, falling back to defaultMessage when
// the status has no translation. The missing-translation policy for
// settings HTMX success responses lives here once; handlers must not
// re-implement the translate-and-fallback dance inline.
func htmxSettingsSuccessMarkup(c fiber.Ctx, status string, defaultMessage string) string {
	messages := currentMessages(c)
	messageKey := services.SettingsStatusTranslationKey(status)
	message, translated := lookupMessage(messages, messageKey)
	if !translated {
		message = defaultMessage
	}
	return htmxDismissibleSuccessStatusMarkup(messages, message)
}

func localizedStatusDismissLabel(messages map[string]string) string {
	closeLabel, translated := lookupMessage(messages, "common.close")
	if !translated {
		return "Close"
	}
	return closeLabel
}

// setEncodedResponseNotice carries a day-save toast to the client out of band,
// in a header, because the response body is the saved entry rather than a page.
//
// It emits the rendered sentence AND its catalogue key. The sentence is what
// the toast shows; the key is what anything asserting on the notice keys off,
// so a test does not have to re-type localized copy the catalogue owns — the
// same split the rendered surfaces express as data-explainer-key next to their
// text. A blank message emits neither header, so "no notice" stays observable
// as the absence of both.
func setEncodedResponseNotice(c fiber.Ctx, noticeKey string, message string) {
	trimmed := strings.TrimSpace(message)
	if trimmed == "" {
		return
	}
	c.Set("X-Ovumcy-Notice", url.QueryEscape(trimmed))
	if key := strings.TrimSpace(noticeKey); key != "" {
		c.Set("X-Ovumcy-Notice-Key", key)
	}
}

func (handler *Handler) sendDaySaveStatus(c fiber.Ctx, messageKey string) error {
	timestamp := time.Now().In(handler.requestLocation(c)).Format("15:04")
	patternKey := messageKey
	if patternKey == "" {
		patternKey = "common.saved_at"
	}
	pattern, translated := lookupMessage(currentMessages(c), patternKey)
	if !translated {
		if patternKey == "common.saved_at" {
			pattern = "Saved at %s"
		} else {
			pattern = "Saved."
		}
	}
	message := pattern
	if patternKey == "common.saved_at" {
		message = fmt.Sprintf(pattern, timestamp)
	}
	return c.SendString(htmxDismissibleSuccessStatusMarkup(currentMessages(c), message))
}
