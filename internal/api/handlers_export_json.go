package api

import (
	"encoding/json"
	"time"

	"github.com/gofiber/fiber/v2"
)

func (handler *Handler) ExportJSON(c *fiber.Ctx) error {
	user, from, to, spec := handler.exportUserAndRange(c)
	if spec != nil {
		return handler.respondMappedError(c, *spec)
	}
	entries, err := handler.exportService.BuildJSONEntries(user.ID, from, to, handler.location)
	if err != nil {
		return handler.respondMappedError(c, exportFetchLogsErrorSpec())
	}
	now := time.Now().In(handler.location)

	payload := fiber.Map{
		"exported_at": now.Format(time.RFC3339),
		"entries":     entries,
	}

	serialized, err := json.MarshalIndent(payload, "", "  ")
	if err != nil {
		return handler.respondMappedError(c, exportBuildErrorSpec())
	}

	setExportAttachmentHeaders(c, fiber.MIMEApplicationJSON, buildExportFilename(now, "json"))
	return c.Send(serialized)
}
