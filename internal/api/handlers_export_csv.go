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
		return handler.respondMappedError(c, exportFetchLogsErrorSpec())
	}
	now := time.Now().In(handler.location)

	var output bytes.Buffer
	writer := csv.NewWriter(&output)
	if err := writer.Write(services.ExportCSVHeaders); err != nil {
		return handler.respondMappedError(c, exportBuildErrorSpec())
	}

	for _, row := range rows {
		if err := writer.Write(row.Columns()); err != nil {
			return handler.respondMappedError(c, exportBuildErrorSpec())
		}
	}

	writer.Flush()
	if err := writer.Error(); err != nil {
		return handler.respondMappedError(c, exportBuildErrorSpec())
	}

	setExportAttachmentHeaders(c, "text/csv", buildExportFilename(now, "csv"))
	return c.Send(output.Bytes())
}
