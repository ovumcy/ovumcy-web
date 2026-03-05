package api

import "github.com/gofiber/fiber/v2"

func (handler *Handler) ExportSummary(c *fiber.Ctx) error {
	user, from, to, spec := handler.exportUserAndRange(c)
	if spec != nil {
		return handler.respondMappedError(c, *spec)
	}
	summary, err := handler.exportService.BuildSummary(user.ID, from, to, handler.location)
	if err != nil {
		return handler.respondMappedError(c, exportFetchLogsErrorSpec())
	}

	return c.JSON(fiber.Map{
		"total_entries": summary.TotalEntries,
		"has_data":      summary.HasData,
		"date_from":     summary.DateFrom,
		"date_to":       summary.DateTo,
	})
}
