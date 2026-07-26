package api

import (
	"bytes"
	"fmt"

	"github.com/gofiber/fiber/v3"
)

// Health reports process liveness only. It does NOT query the database or
// check any downstream dependency, so a 200 here means the process is alive,
// not that it is ready to serve traffic. That split is deliberate and load
// bearing: the container healthcheck probes this endpoint, so a storage blip
// must not be able to restart the process. Ready (/readyz) is the probe that
// answers the storage question.
func (handler *Handler) Health(c fiber.Ctx) error {
	return c.JSON(fiber.Map{"status": "ok"})
}

// Ready reports whether the process can serve traffic, not merely that it is
// running: it answers 200 only when the storage layer responds to a trivial
// probe, and 503 when it does not. It is the readiness half of the probe pair
// — an operator or load balancer drains on 503, while the container
// healthcheck stays on Health so a transient failure does not become a restart
// loop.
//
// Both bodies are fixed constants that mirror Health's shape. The endpoint is
// unauthenticated by nature, so nothing about the failure — driver, database
// path, error text — may travel in the response: the caller learns exactly the
// one bit a readiness probe exists to carry. Nothing is logged from here
// either; the request logger already records the 503 with its method and path,
// and an anonymous caller must not be able to drive log volume or emit driver
// detail into the operator's logs on demand.
func (handler *Handler) Ready(c fiber.Ctx) error {
	if err := handler.readinessService.CheckStorage(c.Context()); err != nil {
		return c.Status(fiber.StatusServiceUnavailable).JSON(fiber.Map{"status": "unavailable"})
	}
	return c.JSON(fiber.Map{"status": "ok"})
}

func (handler *Handler) render(c fiber.Ctx, name string, data fiber.Map) error {
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

func (handler *Handler) renderPartial(c fiber.Ctx, name string, data fiber.Map) error {
	output, err := handler.renderPartialString(c, name, data)
	if err != nil {
		return respondGlobalMappedError(c, partialRenderErrorSpec())
	}
	c.Type("html", "utf-8")
	return c.SendString(output)
}

func (handler *Handler) renderPartialString(c fiber.Ctx, name string, data fiber.Map) (string, error) {
	tmpl, ok := handler.partials[name]
	if !ok {
		return "", fmt.Errorf("partial template %q not found", name)
	}
	payload := handler.withTemplateDefaults(c, data)
	var output bytes.Buffer
	if err := tmpl.ExecuteTemplate(&output, name, payload); err != nil {
		return "", fmt.Errorf("execute partial template %q: %w", name, err)
	}
	return output.String(), nil
}
