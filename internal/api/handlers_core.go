package api

import (
	"bytes"

	"github.com/gofiber/fiber/v2"
)

func (handler *Handler) Health(c *fiber.Ctx) error {
	return c.JSON(fiber.Map{"status": "ok"})
}

func (handler *Handler) render(c *fiber.Ctx, name string, data fiber.Map) error {
	tmpl, ok := handler.templates[name]
	if !ok {
		return apiError(c, fiber.StatusInternalServerError, "template not found")
	}
	payload := handler.withTemplateDefaults(c, data)
	var output bytes.Buffer
	if err := tmpl.ExecuteTemplate(&output, "base", payload); err != nil {
		return apiError(c, fiber.StatusInternalServerError, "failed to render template")
	}
	c.Type("html", "utf-8")
	return c.Send(output.Bytes())
}

func (handler *Handler) renderPartial(c *fiber.Ctx, name string, data fiber.Map) error {
	tmpl, ok := handler.partials[name]
	if !ok {
		return apiError(c, fiber.StatusInternalServerError, "partial not found")
	}
	payload := handler.withTemplateDefaults(c, data)
	var output bytes.Buffer
	if err := tmpl.ExecuteTemplate(&output, name, payload); err != nil {
		return apiError(c, fiber.StatusInternalServerError, "failed to render partial")
	}
	c.Type("html", "utf-8")
	return c.Send(output.Bytes())
}
