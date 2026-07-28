package api

import (
	"encoding/json"
	"time"

	"github.com/gofiber/fiber/v3"
)

// exportJSONEgress tags the JSON download as audited health-data egress; see
// exportCSVEgress for the shape.
var exportJSONEgress = healthEgressKind{
	action: dataExportAction,
	target: exportEgressTarget,
	detail: securityEventField("export_format", "json"),
}

func (handler *Handler) ExportJSON(c fiber.Ctx) error {
	user, from, to, spec := handler.exportUserAndRange(c, exportJSONEgress)
	if spec != nil {
		return handler.respondMappedError(c, *spec)
	}
	location := handler.requestLocation(c)
	entries, err := handler.exportService.BuildJSONEntries(c.Context(), user.ID, from, to, location)
	if err != nil {
		return handler.failEgress(c, exportJSONEgress, exportFetchLogsErrorSpec())
	}
	now := time.Now().In(location)

	payload := fiber.Map{
		"exported_at": now.Format(time.RFC3339),
		"entries":     entries,
	}

	serialized, err := json.MarshalIndent(payload, "", "  ")
	if err != nil {
		// Defensive: the payload holds only strings and export DTOs, none of which
		// carry a channel, a func, or a cyclic reference, so marshalling cannot fail.
		return handler.failEgress(c, exportJSONEgress, exportBuildErrorSpec()) // codecov:ignore -- unreachable: the export payload is always marshalable
	}

	setExportAttachmentHeaders(c, fiber.MIMEApplicationJSON, buildExportFilename(now, "json"))
	handler.logEgressSuccess(c, exportJSONEgress)
	return c.Send(serialized)
}
