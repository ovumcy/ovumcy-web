package api

import (
	"bytes"
	"encoding/csv"
	"time"

	"github.com/gofiber/fiber/v2"
	"github.com/terraincognita07/ovumcy/internal/services"
)

func (handler *Handler) ExportCSV(c *fiber.Ctx) error {
	user, from, to, spec := handler.exportUserAndRange(c)
	if spec != nil {
		return handler.respondMappedError(c, *spec)
	}
	rows, err := handler.exportService.BuildCSVRows(user.ID, from, to, handler.location)
	if err != nil {
		spec := exportFetchLogsErrorSpec()
		handler.logSecurityError(c, "data.export", spec, securityEventField("export_format", "csv"))
		return handler.respondMappedError(c, spec)
	}
	now := time.Now().In(handler.location)

	var output bytes.Buffer
	writer := csv.NewWriter(&output)
	if err := writer.Write(services.ExportCSVHeaders); err != nil {
		spec := exportBuildErrorSpec()
		handler.logSecurityError(c, "data.export", spec, securityEventField("export_format", "csv"))
		return handler.respondMappedError(c, spec)
	}

	for _, row := range rows {
		if err := writer.Write(row.Columns()); err != nil {
			spec := exportBuildErrorSpec()
			handler.logSecurityError(c, "data.export", spec, securityEventField("export_format", "csv"))
			return handler.respondMappedError(c, spec)
		}
	}

	writer.Flush()
	if err := writer.Error(); err != nil {
		spec := exportBuildErrorSpec()
		handler.logSecurityError(c, "data.export", spec, securityEventField("export_format", "csv"))
		return handler.respondMappedError(c, spec)
	}

	setExportAttachmentHeaders(c, "text/csv", buildExportFilename(now, "csv"))
	handler.logSecurityEvent(c, "data.export", "success", securityEventField("export_format", "csv"))
	return c.Send(output.Bytes())
}
