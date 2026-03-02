package api

import (
	"encoding/json"
	"time"

	"github.com/gofiber/fiber/v2"
)

func (handler *Handler) ExportJSON(c *fiber.Ctx) error {
	user, from, to, status, message := handler.exportUserAndRange(c)
	if status != 0 {
		return apiError(c, status, message)
	}
	entries, err := handler.exportService.BuildJSONEntries(user.ID, from, to, handler.location)
	if err != nil {
		return apiError(c, fiber.StatusInternalServerError, "failed to fetch logs")
	}
	now := time.Now().In(handler.location)

	payload := fiber.Map{
		"exported_at": now.Format(time.RFC3339),
		"entries":     entries,
	}

	serialized, err := json.MarshalIndent(payload, "", "  ")
	if err != nil {
		return apiError(c, fiber.StatusInternalServerError, "failed to build export")
	}

	setExportAttachmentHeaders(c, fiber.MIMEApplicationJSON, buildExportFilename(now, "json"))
	return c.Send(serialized)
}
