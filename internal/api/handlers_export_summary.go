package api

import "github.com/gofiber/fiber/v2"

func (handler *Handler) ExportSummary(c *fiber.Ctx) error {
	user, from, to, spec := handler.exportUserAndRange(c)
	if spec != nil {
		return handler.respondMappedError(c, *spec)
	}
	summary, err := handler.exportService.BuildSummary(user.ID, from, to, handler.location)
	if err != nil {
		spec := exportFetchLogsErrorSpec()
		handler.logSecurityError(c, "data.export", spec, securityEventField("export_format", "summary"))
		return handler.respondMappedError(c, spec)
	}

	handler.logSecurityEvent(c, "data.export", "success", securityEventField("export_format", "summary"))
	return c.JSON(fiber.Map{
		"total_entries": summary.TotalEntries,
		"has_data":      summary.HasData,
		"date_from":     summary.DateFrom,
		"date_to":       summary.DateTo,
	})
}
