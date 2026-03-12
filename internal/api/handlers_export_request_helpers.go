package api

import (
	"fmt"
	"strings"
	"time"

	"github.com/gofiber/fiber/v2"
	"github.com/terraincognita07/ovumcy/internal/models"
	"github.com/terraincognita07/ovumcy/internal/services"
)

func (handler *Handler) parseExportRange(c *fiber.Ctx) (*time.Time, *time.Time, error) {
	fromRaw, toRaw := exportRangeInputValues(c)
	from, to, err := services.ParseExportRange(fromRaw, toRaw, handler.requestLocation(c))
	if err != nil {
		return nil, nil, err
	}

	return from, to, nil
}

func (handler *Handler) exportUserAndRange(c *fiber.Ctx) (*models.User, *time.Time, *time.Time, *APIErrorSpec) {
	user, ok := currentUser(c)
	if !ok || user == nil {
		spec := unauthorizedErrorSpec()
		handler.logSecurityError(c, "data.export", spec)
		return nil, nil, nil, &spec
	}

	from, to, err := handler.parseExportRange(c)
	if err != nil {
		spec := mapExportRangeError(err)
		handler.logSecurityError(c, "data.export", spec)
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

func exportRangeInputValues(c *fiber.Ctx) (string, string) {
	from := strings.TrimSpace(c.FormValue("from"))
	to := strings.TrimSpace(c.FormValue("to"))
	if from == "" {
		from = strings.TrimSpace(c.Query("from"))
	}
	if to == "" {
		to = strings.TrimSpace(c.Query("to"))
	}
	return from, to
}
