package api

import (
	"net/url"
	"strings"

	"github.com/gofiber/fiber/v3"
	"github.com/ovumcy/ovumcy-web/internal/services"
)

// periodOffConfirmHeader carries, on every answered HTMX day save, the question
// the saved day's period toggle asks from then on (its data-period-off-confirm,
// query-escaped like X-Ovumcy-Notice), empty when it asks nothing. Both day
// forms stay open after a save without re-rendering, so without it a tick saved
// in the open form would un-tick later without the question its render lacked.
// periodOffConfirmKeyHeader is the locale key of that question
// (data-period-off-confirm-key), as X-Ovumcy-Notice-Key is the notice's; the two
// go out together, both empty when nothing is asked.
const (
	periodOffConfirmHeader    = "X-Ovumcy-Period-Off-Confirm"
	periodOffConfirmKeyHeader = "X-Ovumcy-Period-Off-Confirm-Key"
)

// setPeriodOffConfirmHeader sends neither header when the question cannot be
// rendered, so the form keeps the one it was rendered with.
func (handler *Handler) setPeriodOffConfirmHeader(c fiber.Ctx, effect services.PeriodOffEffect) {
	question := ""
	if effect.Any() {
		rendered, err := handler.renderComponentString("period_off_confirm", fiber.Map{"Messages": currentMessages(c), "Lang": currentLanguage(c), "Effect": effect})
		if err != nil {
			return // codecov:ignore -- the components tree parses components/*.html at NewHandler; TestDaySaveAnswersWithTheQuestionTheSavedToggleAsks renders it
		}
		question = url.QueryEscape(strings.TrimSpace(rendered))
	}
	c.Set(periodOffConfirmHeader, question)
	c.Set(periodOffConfirmKeyHeader, effect.MessageKey())
}
