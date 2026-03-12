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
	location := handler.requestLocation(c)
	entries, err := handler.exportService.BuildJSONEntries(user.ID, from, to, location)
	if err != nil {
		spec := exportFetchLogsErrorSpec()
		handler.logSecurityError(c, "data.export", spec, securityEventField("export_format", "json"))
		return handler.respondMappedError(c, spec)
	}
	now := time.Now().In(location)

	payload := fiber.Map{
		"exported_at": now.Format(time.RFC3339),
		"entries":     entries,
	}

	serialized, err := json.MarshalIndent(payload, "", "  ")
	if err != nil {
		spec := exportBuildErrorSpec()
		handler.logSecurityError(c, "data.export", spec, securityEventField("export_format", "json"))
		return handler.respondMappedError(c, spec)
	}

	setExportAttachmentHeaders(c, fiber.MIMEApplicationJSON, buildExportFilename(now, "json"))
	handler.logSecurityEvent(c, "data.export", "success", securityEventField("export_format", "json"))
	return c.Send(serialized)
}
