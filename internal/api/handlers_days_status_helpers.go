package api

import (
	"fmt"
	"time"

	"github.com/gofiber/fiber/v2"
	"github.com/terraincognita07/ovumcy/internal/httpx"
)

func htmxDismissibleSuccessStatusMarkup(messages map[string]string, message string) string {
	closeLabel := translateMessage(messages, "common.close")
	if closeLabel == "" || closeLabel == "common.close" {
		closeLabel = "Close"
	}
	return httpx.DismissibleStatusOKMarkup(message, closeLabel)
}

func (handler *Handler) sendDaySaveStatus(c *fiber.Ctx) error {
	timestamp := time.Now().In(handler.requestLocation(c)).Format("15:04")
	pattern := translateMessage(currentMessages(c), "common.saved_at")
	if pattern == "common.saved_at" {
		pattern = "Saved at %s"
	}
	message := fmt.Sprintf(pattern, timestamp)
	return c.SendString(htmxDismissibleSuccessStatusMarkup(currentMessages(c), message))
}
