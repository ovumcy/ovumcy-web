package api

import "github.com/gofiber/fiber/v2"

func (handler *Handler) ExportSummary(c *fiber.Ctx) error {
	user, from, to, status, message := handler.exportUserAndRange(c)
	if status != 0 {
		return apiError(c, status, message)
	}
	summary, err := handler.exportService.BuildSummary(user.ID, from, to, handler.location)
	if err != nil {
		return apiError(c, fiber.StatusInternalServerError, "failed to fetch logs")
	}

	return c.JSON(fiber.Map{
		"total_entries": summary.TotalEntries,
		"has_data":      summary.HasData,
		"date_from":     summary.DateFrom,
		"date_to":       summary.DateTo,
	})
}
