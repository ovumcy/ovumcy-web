package api

import (
	"fmt"
	"strings"
	"time"

	"github.com/gofiber/fiber/v3"
	"github.com/ovumcy/ovumcy-web/internal/models"
	"github.com/ovumcy/ovumcy-web/internal/services"
)

// dataExportAction is the wire-visible action every export format logs under.
// It is deliberately the same string the three handlers used to write out by
// hand, so an operator filter on action="data.export" keeps matching.
const dataExportAction = "data.export"

// exportEgressTarget names the scope that leaves the instance. It reuses the
// designator the clear-data wipe already uses for the same scope, so one target
// value follows an owner's tracked data across both health audit domains.
const exportEgressTarget = "account_data"

func (handler *Handler) parseExportRange(c fiber.Ctx) (*time.Time, *time.Time, error) {
	fromRaw, toRaw := exportRangeInputValues(c)
	from, to, err := services.ParseExportRange(fromRaw, toRaw, handler.requestLocation(c))
	if err != nil {
		return nil, nil, err
	}

	return from, to, nil
}

// exportUserAndRange is the shared prologue of the three export handlers. It
// takes the caller's egress kind so a request rejected before any data is read
// is still attributed to the format that was asked for: both refusals below
// used to log the action alone, which left an operator unable to tell a refused
// CSV download from a refused JSON one.
func (handler *Handler) exportUserAndRange(c fiber.Ctx, kind healthEgressKind) (*models.User, *time.Time, *time.Time, *APIErrorSpec) {
	user, ok := currentUser(c)
	if !ok || user == nil {
		spec := unauthorizedErrorSpec()
		handler.logEgressError(c, kind, spec)
		return nil, nil, nil, &spec
	}

	from, to, err := handler.parseExportRange(c)
	if err != nil {
		spec := mapExportRangeError(err)
		handler.logEgressError(c, kind, spec)
		return nil, nil, nil, &spec
	}

	return user, from, to, nil
}

func buildExportFilename(now time.Time, extension string) string {
	return fmt.Sprintf("ovumcy-export-%s.%s", now.Format("2006-01-02"), extension)
}

func setExportAttachmentHeaders(c fiber.Ctx, contentType string, filename string) {
	c.Set(fiber.HeaderContentType, contentType)
	c.Set(fiber.HeaderContentDisposition, fmt.Sprintf("attachment; filename=%s", filename))
}

// exportRangeInputValues reads the requested range from wherever the caller put
// it. One lookup covers both: fiber's FormValue searches the query string
// before the body, so a GET's `?from=` and a POST's form field arrive through
// the same call and an empty answer means neither carried a value. An explicit
// c.Query fallback behind it could only reassign the empty string over itself.
// That resolution order belongs to the framework, so it is pinned rather than
// assumed: handlers_export_request_helpers_seam_test.go.
func exportRangeInputValues(c fiber.Ctx) (string, string) {
	return strings.TrimSpace(c.FormValue("from")), strings.TrimSpace(c.FormValue("to"))
}
