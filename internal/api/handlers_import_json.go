package api

import (
	"strconv"

	"github.com/gofiber/fiber/v3"
	"github.com/ovumcy/ovumcy-web/internal/services"
)

// dayImportMutation tags the restore as an audited health-data mutation. The
// audit line carries only counts/outcome — never the imported records.
var dayImportMutation = healthMutationKind{action: "data.import", target: "day_entry"}

// ImportJSON restores an owner's prior JSON export. Transport-only: it resolves
// the session owner, hands the raw body to the service, and maps the typed
// outcome. Owner scoping, validation, and the additive (skip-existing) contract
// all live in services.ImportService. CSRF is enforced by global middleware;
// OwnerOnly is declared explicitly on the route.
func (handler *Handler) ImportJSON(c fiber.Ctx) error {
	user, ok := currentUser(c)
	// codecov:ignore:start -- defensive: AuthRequired middleware guarantees a session user on this route, so this guard is unreachable via routing
	if !ok || user == nil {
		return handler.failMutation(c, dayImportMutation, unauthorizedErrorSpec())
	}
	// codecov:ignore:end

	result, err := handler.importService.ImportJSON(
		c.Context(),
		user.ID,
		c.Body(),
		handler.requestLocation(c),
	)
	if err != nil {
		return handler.failMutation(c, dayImportMutation, mapImportError(err))
	}

	handler.logSecurityEvent(
		c,
		dayImportMutation.action,
		"success",
		securityEventField("domain", "health_data"),
		securityEventField("target", dayImportMutation.target),
		securityEventField("added", strconv.Itoa(result.Added)),
		securityEventField("skipped", strconv.Itoa(result.Skipped)),
		securityEventField("rejected", strconv.Itoa(result.Rejected)),
	)
	return handler.respondImportSuccess(c, result)
}

func (handler *Handler) respondImportSuccess(c fiber.Ctx, result services.ImportResult) error {
	if isHTMX(c) {
		return c.SendString(htmxSettingsSuccessMarkup(c, "data_imported", "Restored your data."))
	}
	if acceptsJSON(c) {
		return c.JSON(fiber.Map{
			"ok":       true,
			"added":    result.Added,
			"skipped":  result.Skipped,
			"rejected": result.Rejected,
		})
	}
	return redirectOrJSON(c, "/settings")
}
