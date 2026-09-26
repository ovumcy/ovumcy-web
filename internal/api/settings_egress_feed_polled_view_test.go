package api

import (
	"testing"
	"time"

	"github.com/gofiber/fiber/v3"
	"github.com/ovumcy/ovumcy-web/internal/services"
	"github.com/valyala/fasthttp"
)

// TestSettingsEgressViewOmitsThePolledLineInTheUnknownState pins the view's own
// gate on the polled line for the unknown feed state. The ledger already drops
// the date there (TestEgressLedgerFeedUnknownCarriesNoLastPolledMark), but the
// LINE is chosen here: were unknown treated as a stored link, the card would say
// "No calendar check is recorded for this link." about a row it could not read.
// The mark is passed in anyway so the gate is tested on its own, not through the
// ledger's suppression. The issued-current-key view is the control.
func TestSettingsEgressViewOmitsThePolledLineInTheUnknownState(t *testing.T) {
	t.Parallel()

	app := fiber.New()
	c := app.AcquireCtx(&fasthttp.RequestCtx{})
	defer app.ReleaseCtx(c)

	polledOn := time.Date(2026, 9, 20, 0, 0, 0, 0, time.UTC)
	build := func(state services.EgressFeedState) settingsEgressView {
		ledger := services.EgressLedger{}
		ledger.Feed.State = state
		ledger.Feed.LastPolledOn = &polledOn
		return buildSettingsEgressView(c, ledger, time.UTC)
	}

	if control := build(services.EgressFeedIssuedCurrentKey); control.Feed.PolledKey == "" {
		t.Fatal("control: expected a polled line for a stored link; the absence check below would pass vacuously")
	}
	if view := build(services.EgressFeedUnknown); view.Feed.PolledKey != "" {
		t.Fatalf("expected no polled line in the unknown state, got key %q", view.Feed.PolledKey)
	}
}
