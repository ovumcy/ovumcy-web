package api

import (
	"bytes"
	"encoding/csv"
	"time"

	"github.com/gofiber/fiber/v3"
	"github.com/ovumcy/ovumcy-web/internal/services"
)

// exportCSVEgress tags the CSV download as audited health-data egress: the
// action operators already filter on, the scope that left the instance, and the
// format, declared once for every branch of the handler.
var exportCSVEgress = healthEgressKind{
	action: dataExportAction,
	target: exportEgressTarget,
	detail: securityEventField("export_format", "csv"),
}

func (handler *Handler) ExportCSV(c fiber.Ctx) error {
	user, from, to, spec := handler.exportUserAndRange(c, exportCSVEgress)
	if spec != nil {
		return handler.respondMappedError(c, *spec)
	}
	location := handler.requestLocation(c)
	rows, err := handler.exportService.BuildCSVRows(c.Context(), user.ID, from, to, location)
	if err != nil {
		return handler.failEgress(c, exportCSVEgress, exportFetchLogsErrorSpec())
	}
	now := time.Now().In(location)

	var output bytes.Buffer
	writer := csv.NewWriter(&output)
	// The three build branches below are defensive: this csv.Writer writes into a
	// bytes.Buffer, whose Write never returns an error, so no request reaches them.
	if err := writer.Write(services.ExportCSVHeaders()); err != nil {
		return handler.failEgress(c, exportCSVEgress, exportBuildErrorSpec()) // codecov:ignore -- unreachable: bytes.Buffer writes cannot fail
	}

	for _, row := range rows {
		if err := writer.Write(row.Columns()); err != nil {
			return handler.failEgress(c, exportCSVEgress, exportBuildErrorSpec()) // codecov:ignore -- unreachable: bytes.Buffer writes cannot fail
		}
	}

	writer.Flush()
	if err := writer.Error(); err != nil {
		return handler.failEgress(c, exportCSVEgress, exportBuildErrorSpec()) // codecov:ignore -- unreachable: bytes.Buffer writes cannot fail
	}

	setExportAttachmentHeaders(c, "text/csv", buildExportFilename(now, "csv"))
	handler.logEgressSuccess(c, exportCSVEgress)
	return c.Send(output.Bytes())
}
