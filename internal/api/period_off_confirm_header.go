package api

import (
	"bytes"
	"net/url"
	"strings"

	"github.com/gofiber/fiber/v3"
	"github.com/ovumcy/ovumcy-web/internal/models"
)

// periodOffConfirmHeader carries, on every answered HTMX day save, the question
// the saved day's period toggle asks from then on (its data-period-off-confirm,
// query-escaped like X-Ovumcy-Notice), empty when it asks nothing. Both day
// forms stay open after a save without re-rendering, so without it a tick saved
// in the open form would un-tick later without the question its render lacked.
const periodOffConfirmHeader = "X-Ovumcy-Period-Off-Confirm"

// setPeriodOffConfirmHeader sends no header when the effect cannot be read, so
// the form keeps the question it was rendered with.
func (handler *Handler) setPeriodOffConfirmHeader(c fiber.Ctx, request upsertDayRequest, saved models.DailyLog) {
	if handler.dashboardViewService == nil {
		return
	}
	effect, err := handler.dashboardViewService.PeriodOffEffectAfterSave(c.Context(), request.user, saved, request.day, handler.clockNow().In(request.location), request.location)
	if err != nil {
		return
	}
	question := ""
	if effect.Any() {
		// Every partial tree parses components/*.html, so the editor's holds the
		// question both forms render.
		tmpl, ok := handler.partials["day_editor_partial"]
		if !ok {
			return
		}
		var output bytes.Buffer
		if err := tmpl.ExecuteTemplate(&output, "period_off_confirm", fiber.Map{"Messages": currentMessages(c), "Lang": currentLanguage(c), "Effect": effect}); err != nil {
			return
		}
		question = url.QueryEscape(strings.TrimSpace(output.String()))
	}
	c.Set(periodOffConfirmHeader, question)
}
