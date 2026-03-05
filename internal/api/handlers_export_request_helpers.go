package api

import (
	"fmt"
	"time"

	"github.com/gofiber/fiber/v2"
	"github.com/terraincognita07/ovumcy/internal/models"
	"github.com/terraincognita07/ovumcy/internal/services"
)

func (handler *Handler) parseExportRange(c *fiber.Ctx) (*time.Time, *time.Time, error) {
	from, to, err := services.ParseExportRange(c.Query("from"), c.Query("to"), handler.location)
	if err != nil {
		return nil, nil, err
	}

	return from, to, nil
}

func (handler *Handler) exportUserAndRange(c *fiber.Ctx) (*models.User, *time.Time, *time.Time, *APIErrorSpec) {
	user, ok := currentUser(c)
	if !ok || user == nil {
		spec := unauthorizedErrorSpec()
		return nil, nil, nil, &spec
	}

	from, to, err := handler.parseExportRange(c)
	if err != nil {
		spec := mapExportRangeError(err)
		return nil, nil, nil, &spec
	}

	return user, from, to, nil
}

func buildExportFilename(now time.Time, extension string) string {
	return fmt.Sprintf("ovumcy-export-%s.%s", now.Format("2006-01-02"), extension)
}

func setExportAttachmentHeaders(c *fiber.Ctx, contentType string, filename string) {
	c.Set(fiber.HeaderContentType, contentType)
	c.Set(fiber.HeaderContentDisposition, fmt.Sprintf("attachment; filename=%s", filename))
}
