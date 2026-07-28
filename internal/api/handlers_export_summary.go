package api

import "github.com/gofiber/fiber/v3"

// exportSummaryEgress tags the summary read as audited health-data egress. It
// carries counts and the covered range rather than the entries themselves, but
// it is still tracked data answering a question about the owner's health, and
// it travels the same action as the two file downloads.
var exportSummaryEgress = healthEgressKind{
	action: dataExportAction,
	target: exportEgressTarget,
	detail: securityEventField("export_format", "summary"),
}

func (handler *Handler) ExportSummary(c fiber.Ctx) error {
	user, from, to, spec := handler.exportUserAndRange(c, exportSummaryEgress)
	if spec != nil {
		return handler.respondMappedError(c, *spec)
	}
	location := handler.requestLocation(c)
	summary, err := handler.exportService.BuildSummary(c.Context(), user.ID, from, to, location)
	if err != nil {
		return handler.failEgress(c, exportSummaryEgress, exportFetchLogsErrorSpec())
	}

	handler.logEgressSuccess(c, exportSummaryEgress)
	return c.JSON(fiber.Map{
		"total_entries": summary.TotalEntries,
		"has_data":      summary.HasData,
		"date_from":     summary.DateFrom,
		"date_to":       summary.DateTo,
	})
}
