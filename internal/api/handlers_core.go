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
		return respondGlobalMappedError(c, templateNotFoundErrorSpec())
	}
	payload := handler.withTemplateDefaults(c, data)
	var output bytes.Buffer
	if err := tmpl.ExecuteTemplate(&output, "base", payload); err != nil {
		return respondGlobalMappedError(c, templateRenderErrorSpec())
	}
	c.Type("html", "utf-8")
	return c.Send(output.Bytes())
}

func (handler *Handler) renderPartial(c *fiber.Ctx, name string, data fiber.Map) error {
	tmpl, ok := handler.partials[name]
	if !ok {
		return respondGlobalMappedError(c, partialNotFoundErrorSpec())
	}
	payload := handler.withTemplateDefaults(c, data)
	var output bytes.Buffer
	if err := tmpl.ExecuteTemplate(&output, name, payload); err != nil {
		return respondGlobalMappedError(c, partialRenderErrorSpec())
	}
	c.Type("html", "utf-8")
	return c.Send(output.Bytes())
}
