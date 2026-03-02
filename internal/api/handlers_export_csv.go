package api

import (
	"bytes"
	"encoding/csv"
	"time"

	"github.com/gofiber/fiber/v2"
	"github.com/terraincognita07/ovumcy/internal/services"
)

func (handler *Handler) ExportCSV(c *fiber.Ctx) error {
	user, from, to, status, message := handler.exportUserAndRange(c)
	if status != 0 {
		return apiError(c, status, message)
	}
	rows, err := handler.exportService.BuildCSVRows(user.ID, from, to, handler.location)
	if err != nil {
		return apiError(c, fiber.StatusInternalServerError, "failed to fetch logs")
	}
	now := time.Now().In(handler.location)

	var output bytes.Buffer
	writer := csv.NewWriter(&output)
	if err := writer.Write(services.ExportCSVHeaders); err != nil {
		return apiError(c, fiber.StatusInternalServerError, "failed to build export")
	}

	for _, row := range rows {
		if err := writer.Write(row.Columns()); err != nil {
			return apiError(c, fiber.StatusInternalServerError, "failed to build export")
		}
	}

	writer.Flush()
	if err := writer.Error(); err != nil {
		return apiError(c, fiber.StatusInternalServerError, "failed to build export")
	}

	setExportAttachmentHeaders(c, "text/csv", buildExportFilename(now, "csv"))
	return c.Send(output.Bytes())
}
