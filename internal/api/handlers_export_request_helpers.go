package api

import (
	"fmt"
	"time"

	"github.com/gofiber/fiber/v2"
	"github.com/terraincognita07/ovumcy/internal/models"
	"github.com/terraincognita07/ovumcy/internal/services"
)

func (handler *Handler) parseExportRange(c *fiber.Ctx) (*time.Time, *time.Time, string) {
	from, to, err := services.ParseExportRange(c.Query("from"), c.Query("to"), handler.location)
	if err != nil {
		switch services.ClassifyExportRangeError(err) {
		case services.ExportRangeErrorFromInvalid:
			return nil, nil, "invalid from date"
		case services.ExportRangeErrorToInvalid:
			return nil, nil, "invalid to date"
		case services.ExportRangeErrorInvalid:
			return nil, nil, "invalid range"
		default:
			return nil, nil, "invalid range"
		}
	}

	return from, to, ""
}

func (handler *Handler) exportUserAndRange(c *fiber.Ctx) (*models.User, *time.Time, *time.Time, int, string) {
	user, ok := currentUser(c)
	if !ok || user == nil {
		return nil, nil, nil, fiber.StatusUnauthorized, "unauthorized"
	}

	from, to, rangeError := handler.parseExportRange(c)
	if rangeError != "" {
		return nil, nil, nil, fiber.StatusBadRequest, rangeError
	}

	return user, from, to, 0, ""
}

func buildExportFilename(now time.Time, extension string) string {
	return fmt.Sprintf("ovumcy-export-%s.%s", now.Format("2006-01-02"), extension)
}

func setExportAttachmentHeaders(c *fiber.Ctx, contentType string, filename string) {
	c.Set(fiber.HeaderContentType, contentType)
	c.Set(fiber.HeaderContentDisposition, fmt.Sprintf("attachment; filename=%s", filename))
}
