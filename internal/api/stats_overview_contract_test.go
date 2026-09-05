package api

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/gofiber/fiber/v3"
	"github.com/ovumcy/ovumcy-web/internal/i18n"
	"github.com/ovumcy/ovumcy-web/internal/models"
	"github.com/ovumcy/ovumcy-web/internal/services"
	"golang.org/x/net/html"
	"gorm.io/gorm"
)

// The contract matrix for GET /api/v1/stats/overview.
//
// The endpoint used to end in `return c.JSON(stats)` — a direct serialization of
// the domain CycleStats, forward-looking fields and all — so every ovulation
// date, fertile window and next-period estimate that /stats and the dashboard
// withhold left the instance as JSON. Suppression is the floor, and a floor one
// surface stands in front of is not one (docs/SECURITY_INVARIANTS.md -> medical
// safety).
//
// Each state below turns on one suppression signal through the real request
// path — an owner in the database, an authenticated GET, the rendered response —
// rather than through a constructed CycleStats, because the defect was in what
// the TRANSPORT published, not in what the derivation computed.

type statsOverviewState struct {
	name string
	// seed puts the owner into the state and returns nothing: the endpoint reads
	// the same database the page does.
	seed func(t *testing.T, database *gorm.DB, user models.User, today time.Time)
	// history is the cycle starts to record, in days back from today. A state
	// with none is the zero-completed-cycle tier; every other state carries real
	// history, because the stats page renders its empty state instead of its stat
	// cards without one — and a parity check against a page that rendered nothing
	// is a comparison that never happened.
	history []int
	// wantReasons are the reasons the payload must NAME. Several signals can hold
	// at once (a paused owner with no completed cycle names two), so this is a
	// containment check, never an equality one.
	wantReasons       []string
	wantPredictions   bool
	wantFertility     bool
	wantNextPeriodSet bool
	// wantsFertilityHook says the stats page renders data-fertility-status in
	// this tier, so the parity subtest knows whether an absent hook is the page
	// answering on another branch or the comparison silently not happening. It is
	// asserted per state rather than counted across them: a counter read after
	// t.Run would be both unsynchronised and read too early the day a subtest
	// takes t.Parallel.
	wantsFertilityHook bool
}

func statsOverviewStates() []statsOverviewState {
	return []statsOverviewState{
		{
			// The projected next period survives this tier: its anchor is a start
			// the owner recorded and only the length falls back to the setting.
			name:              "zero completed cycles",
			seed:              func(*testing.T, *gorm.DB, models.User, time.Time) {},
			wantReasons:       []string{"awaiting_first_cycle"},
			wantPredictions:   false,
			wantFertility:     true,
			wantNextPeriodSet: true,
			// No history, so the page renders its empty state instead of the cards.
			wantsFertilityHook: false,
		},
		{
			name:    "active pregnancy pause",
			history: []int{62, 34, 6},
			seed: func(t *testing.T, database *gorm.DB, user models.User, today time.Time) {
				seedStatsOverviewLog(t, database, models.DailyLog{
					UserID:        user.ID,
					Date:          today.AddDate(0, 0, -2),
					PregnancyTest: models.PregnancyTestPositive,
				})
			},
			wantReasons:     []string{"pregnancy_pause"},
			wantPredictions: true,
			wantFertility:   true,
			// A pause reports PredictionDisabled through the cycle context, so the
			// page takes the same facts-only branch unpredictable mode does.
			wantsFertilityHook: false,
		},
		{
			name:    "unpredictable cycle mode",
			history: []int{62, 34, 6},
			seed: func(t *testing.T, database *gorm.DB, user models.User, today time.Time) {
				updateStatsOverviewUser(t, database, user, map[string]any{"unpredictable_cycle": true})
			},
			wantReasons:     []string{"unpredictable_cycle"},
			wantPredictions: true,
			wantFertility:   true,
			// Unpredictable mode is the page's facts-only tier: it publishes no
			// fertility status at all, which is the stricter answer.
			wantsFertilityHook: false,
		},
		{
			// The history is itself overdue: the anchor is the latest recorded
			// start, so a recent one would end the state this case is about.
			name:    "cycle overdue past its own reference length",
			history: []int{90, 62, 45},
			seed: func(t *testing.T, database *gorm.DB, user models.User, today time.Time) {
				updateStatsOverviewUser(t, database, user, map[string]any{
					"last_period_start": today.AddDate(0, 0, -45),
				})
			},
			wantReasons:     []string{"cycle_overdue"},
			wantPredictions: true,
			wantFertility:   true,
			// The overdue tier is the one that suppresses the projection and still
			// renders a status, so it is where the two surfaces must agree on the
			// value rather than both on silence.
			wantsFertilityHook: true,
		},
	}
}

func TestStatsOverviewWithholdsEveryProjectionItsGatesRefuse(t *testing.T) {
	for _, state := range statsOverviewStates() {
		t.Run(state.name, func(t *testing.T) {
			app, database, _ := newOnboardingTestAppWithLocation(t, time.UTC)
			user, authCookie, today := newStatsOverviewOwner(t, app, database, "overview-"+strings.ReplaceAll(state.name, " ", "-")+"@example.com")
			seedStatsOverviewCycleHistory(t, database, user, today, state.history...)
			state.seed(t, database, user, today)

			body, payload := fetchStatsOverview(t, app, authCookie)

			if payload.Suppression.Predictions != state.wantPredictions {
				t.Fatalf("suppression.predictions = %v, want %v (reasons %v)", payload.Suppression.Predictions, state.wantPredictions, payload.Suppression.Reasons)
			}
			if payload.Suppression.Fertility != state.wantFertility {
				t.Fatalf("suppression.fertility = %v, want %v (reasons %v)", payload.Suppression.Fertility, state.wantFertility, payload.Suppression.Reasons)
			}
			for _, reason := range state.wantReasons {
				if !containsString(payload.Suppression.Reasons, reason) {
					t.Fatalf("suppression.reasons = %v, want it to name %q — a withheld date with no machine-readable cause is a client's guess", payload.Suppression.Reasons, reason)
				}
			}

			// The fertility half, refused in every state here.
			for field, value := range map[string]*string{
				"ovulation_date":         payload.OvulationDate,
				"fertility_window_start": payload.FertilityWindowStart,
				"fertility_window_end":   payload.FertilityWindowEnd,
			} {
				if value != nil {
					t.Fatalf("%s published as %q while suppression.fertility is true", field, *value)
				}
			}
			if payload.CurrentFertility != services.FertilityStatusUnknown {
				t.Fatalf("current_fertility = %q, want %q while the fertility gate holds", payload.CurrentFertility, services.FertilityStatusUnknown)
			}
			if payload.OvulationExact {
				t.Fatal("ovulation_exact is true beside a null ovulation_date")
			}
			if payload.OvulationConfirmed {
				t.Fatal("ovulation_confirmed is true beside a null ovulation_date")
			}
			if (payload.NextPeriodStart != nil) != state.wantNextPeriodSet {
				t.Fatalf("next_period_start present = %v, want %v", payload.NextPeriodStart != nil, state.wantNextPeriodSet)
			}

			// Recorded history is fact, not projection: it survives every tier, or
			// the endpoint has answered "suppressed" by publishing nothing at all.
			if payload.LastPeriodStart == nil {
				t.Fatal("last_period_start is null — a recorded anchor is not a projection and no gate withholds it")
			}

			// current_phase is the axis orthogonal to fertility (#416), so it is
			// published in every tier and is NOT withheld with the dates — it is
			// pinned here against the spec's enum so a suppressed payload cannot
			// answer with something no client is told to expect.
			if !containsString([]string{"menstrual", "follicular", "ovulation", "luteal", "unknown"}, payload.CurrentPhase) {
				t.Fatalf("current_phase = %q, which the published enum does not name", payload.CurrentPhase)
			}

			assertStatsOverviewFraming(t, payload)
			assertNoInstantTimestamps(t, body)
		})
	}
}

// TestStatsOverviewPublishesAWholeProjectionWhenNothingSuppressesIt is the other
// end of the matrix. Without it a handler that answered `null` to everything
// would satisfy every assertion above.
func TestStatsOverviewPublishesAWholeProjectionWhenNothingSuppressesIt(t *testing.T) {
	app, database, _ := newOnboardingTestAppWithLocation(t, time.UTC)
	user, authCookie, today := newStatsOverviewOwner(t, app, database, "overview-unsuppressed@example.com")
	seedStatsOverviewCycleHistory(t, database, user, today, 62, 34, 6)

	_, payload := fetchStatsOverview(t, app, authCookie)

	if payload.Suppression.Predictions || payload.Suppression.Fertility {
		t.Fatalf("an owner with observed cycles got suppression %+v", payload.Suppression)
	}
	if len(payload.Suppression.Reasons) != 0 {
		t.Fatalf("reasons = %v, want none", payload.Suppression.Reasons)
	}
	if payload.OvulationDate == nil || payload.FertilityWindowStart == nil || payload.FertilityWindowEnd == nil || payload.NextPeriodStart == nil {
		t.Fatalf("an unsuppressed owner is missing projected dates: %+v", payload)
	}
	if payload.CompletedCycleCount < 1 {
		t.Fatalf("completed_cycle_count = %d — the fixture did not build the state it claims", payload.CompletedCycleCount)
	}
	assertStatsOverviewFraming(t, payload)
}

// TestStatsOverviewAgreesWithTheStatsPageOnFertility is the parity half: the two
// owner surfaces publish through one adapter, so they cannot differ on what a
// suppressed tier may carry. The page is read at its rendered hook rather than
// at a view flag — a context field is not a rendered value, and a page can
// answer on a sibling branch first.
//
// The page renders that hook only in the tiers that have a fertility status —
// unpredictable-cycle mode takes the facts-only branch instead — so a missing
// hook cannot be a failure everywhere. It also cannot be allowed to read as
// agreement: each state DECLARES whether the page publishes one, so a renamed
// attribute or a widened facts-only branch reds the states that must render it
// rather than turning every subtest into a green report about nothing.
func TestStatsOverviewAgreesWithTheStatsPageOnFertility(t *testing.T) {
	for _, state := range statsOverviewStates() {
		t.Run(state.name, func(t *testing.T) {
			app, database, _ := newOnboardingTestAppWithLocation(t, time.UTC)
			user, authCookie, today := newStatsOverviewOwner(t, app, database, "parity-"+strings.ReplaceAll(state.name, " ", "-")+"@example.com")
			seedStatsOverviewCycleHistory(t, database, user, today, state.history...)
			state.seed(t, database, user, today)

			_, payload := fetchStatsOverview(t, app, authCookie)
			document := fetchStatsPageDocument(t, app, authCookie)

			node := findHTMLNodeWithAttr(document, "data-fertility-status")
			if (node != nil) != state.wantsFertilityHook {
				t.Fatalf("the page rendered data-fertility-status = %v, want %v — with no hook this test compares nothing and would pass the same way if the two surfaces disagreed", node != nil, state.wantsFertilityHook)
			}
			if node != nil {
				if rendered := htmlAttr(node, "data-fertility-status"); rendered != payload.CurrentFertility {
					t.Fatalf("the page renders fertility %q while the API publishes %q", rendered, payload.CurrentFertility)
				}
			}
			if payload.Suppression.Fertility && findHTMLNodeWithAttr(document, "data-fertile-window") != nil {
				t.Fatal("the page names a fertile window while the API suppresses the fertility half")
			}
		})
	}
}

// TestStatsOverviewAgreesWithTheStatsPageOnAPublishedProjection is the parity
// case with something to publish: both surfaces name a fertility status, and the
// page carries the fertile-window line the API's own status implies. The
// suppressed states above can be satisfied by two surfaces that publish nothing;
// this one cannot.
func TestStatsOverviewAgreesWithTheStatsPageOnAPublishedProjection(t *testing.T) {
	app, database, _ := newOnboardingTestAppWithLocation(t, time.UTC)
	user, authCookie, today := newStatsOverviewOwner(t, app, database, "parity-unsuppressed@example.com")
	seedStatsOverviewCycleHistory(t, database, user, today, 62, 34, 6)

	_, payload := fetchStatsOverview(t, app, authCookie)
	document := fetchStatsPageDocument(t, app, authCookie)

	node := findHTMLNodeWithAttr(document, "data-fertility-status")
	if node == nil {
		t.Fatal("the page rendered no fertility status for an owner whose projection is published")
	}
	if rendered := htmlAttr(node, "data-fertility-status"); rendered != payload.CurrentFertility {
		t.Fatalf("the page renders fertility %q while the API publishes %q", rendered, payload.CurrentFertility)
	}
	if payload.Suppression.Fertility {
		t.Fatalf("an owner with observed cycles got the fertility gate: %v", payload.Suppression.Reasons)
	}

	// The window line is the page's own consequence of that status, so the two
	// must not disagree about whether today is inside the fertile window.
	renderedWindow := findHTMLNodeWithAttr(document, "data-fertile-window") != nil
	if renderedWindow != (payload.CurrentFertility == services.FertilityStatusFertile) {
		t.Fatalf("the page renders the fertile-window line = %v while the API publishes fertility %q", renderedWindow, payload.CurrentFertility)
	}
}

// TestStatsOverviewPublishesTheConfirmedOvulationDayNotTheModelsProjection is
// the JSON-API half of the confirmed-ovulation substitution the calendar's
// solid marker, the dashboard's ovulation line and four other surfaces
// already apply (services.ConfirmedCurrentCycleOvulation,
// dashboard_confirmed_ovulation_test.go). The model and the measured day are
// made to differ deliberately, the way MED-2's trace does: a median-cycle
// projection lands on cycle day 14, while a recorded BBT shift confirms cycle
// day 17. Before this fix the endpoint answered the model's superseded day;
// it must now answer the measured one, with ovulation_confirmed reporting
// true for it — the same "confirmed, not modeled" bit the dashboard carries
// as DisplayOvulationConfirmed beside its own DisplayOvulationExact
// (dashboard_cycle.go). This fixture's luteal phase is not clamped, so
// ovulation_exact is true here too; the case that pulls the two apart is
// TestStatsOverviewConfirmedOvulationIsIndependentOfOvulationExact below.
func TestStatsOverviewPublishesTheConfirmedOvulationDayNotTheModelsProjection(t *testing.T) {
	app, database, _ := newOnboardingTestAppWithLocation(t, time.UTC)
	user := createOnboardingTestUser(t, database, "overview-confirmed-ovulation@example.com", "StrongPass1", true)
	authCookie := loginAndExtractAuthCookie(t, app, user.Email, "StrongPass1")
	today := services.DateAtLocation(time.Now().In(time.UTC), time.UTC)
	updateStatsOverviewUser(t, database, user, map[string]any{"track_bbt": true})

	// Three prior 28-day cycles fix the median at 28 and lift the
	// zero-completed-cycle floor; the fourth start opens the CURRENT cycle at
	// today-20 (cycle day 21 today). The median projects ovulation on cycle
	// day 14 (cycleLen 28 - luteal 14), i.e. today-7.
	seedStatsOverviewCycleHistory(t, database, user, today, 104, 76, 48, 20)

	// Undisturbed temperatures fill the shared 3-over-6 detector's 6-day
	// coverline window (cycle days 12-17 = today-9..today-4); three elevated
	// days follow (cycle days 18-20 = today-3..today-1). The detector confirms
	// ovulation the day BEFORE the shift: cycle day 17 = today-4 — three days
	// off the model's today-7, so the two dates cannot pass by coincidence.
	for _, offset := range []int{9, 8, 7, 6, 5, 4} {
		seedStatsOverviewLog(t, database, models.DailyLog{UserID: user.ID, Date: today.AddDate(0, 0, -offset), BBT: new(dashboardConfirmedOvulationLowBBT)})
	}
	for _, offset := range []int{3, 2, 1} {
		seedStatsOverviewLog(t, database, models.DailyLog{UserID: user.ID, Date: today.AddDate(0, 0, -offset), BBT: new(dashboardConfirmedOvulationHighBBT)})
	}

	_, payload := fetchStatsOverview(t, app, authCookie)

	wantConfirmed := today.AddDate(0, 0, -4).Format(statsOverviewDateLayout)
	wantModel := today.AddDate(0, 0, -7).Format(statsOverviewDateLayout)

	if payload.OvulationDate == nil {
		t.Fatal("ovulation_date is null for an owner with a published projection")
	}
	if *payload.OvulationDate == wantModel {
		t.Fatalf("ovulation_date = %s — the model's projection the owner's own temperatures already superseded; want the confirmed %s", *payload.OvulationDate, wantConfirmed)
	}
	if *payload.OvulationDate != wantConfirmed {
		t.Fatalf("ovulation_date = %s, want the BBT-confirmed %s", *payload.OvulationDate, wantConfirmed)
	}
	if !payload.OvulationConfirmed {
		t.Fatal("ovulation_confirmed = false beside a BBT-confirmed ovulation_date — the JSON view must name the substitution the calendar and dashboard already applied")
	}
	if !payload.OvulationExact {
		t.Fatal("ovulation_exact = false: this fixture's luteal phase (14, cycle length 28) is not clamped, so the model's own fit is exact independent of the confirmation")
	}
	if payload.Suppression.Fertility {
		t.Fatalf("suppression.fertility = true for an owner with observed cycles and a confirmed shift: %v", payload.Suppression.Reasons)
	}
}

// TestStatsOverviewConfirmedOvulationIsIndependentOfOvulationExact pins the
// boundary TestStatsOverviewPublishesTheConfirmedOvulationDayNotTheModelsProjection
// cannot: ovulation_confirmed and ovulation_exact are two different signals,
// mirroring the dashboard's own DisplayOvulationConfirmed vs
// DisplayOvulationExact (dashboard_cycle.go), and a single collapsed boolean
// would hide whichever one it discarded. The fixture forces the fallback
// 14-day luteal phase to be CLAMPED (a short 18-day median cycle: the shared
// CalcOvulationDay caps a 14-day luteal phase at cycleLen-5=13), so
// ovulation_exact must read false, while the current cycle's own BBT shift
// still confirms an ovulation — a fact ovulation_confirmed must still report
// regardless of the model's own fit.
func TestStatsOverviewConfirmedOvulationIsIndependentOfOvulationExact(t *testing.T) {
	app, database, _ := newOnboardingTestAppWithLocation(t, time.UTC)
	user := createOnboardingTestUser(t, database, "overview-confirmed-not-exact@example.com", "StrongPass1", true)
	authCookie := loginAndExtractAuthCookie(t, app, user.Email, "StrongPass1")
	today := services.DateAtLocation(time.Now().In(time.UTC), time.UTC)
	updateStatsOverviewUser(t, database, user, map[string]any{"track_bbt": true})

	// Three prior 18-day cycles fix the median at 18 (no BBT data recorded in
	// them, so InferUserLutealPhase finds nothing to personalize and the
	// 14-day fallback stays in force); the fourth start opens the CURRENT
	// cycle at today-11 (cycle day 12 today). CalcOvulationDay(18, 14) clamps
	// the fallback to 13 (18-5), landing the model's projection on cycle day
	// 5 = today-7, with ovulation_exact = false.
	seedStatsOverviewCycleHistory(t, database, user, today, 72, 54, 36, 11)

	// Undisturbed temperatures fill the coverline window (cycle days 3-8 =
	// today-9..today-4); three elevated days follow (cycle days 9-11 =
	// today-3..today-1). The detector confirms ovulation the day BEFORE the
	// shift: cycle day 8 = today-4.
	for _, offset := range []int{9, 8, 7, 6, 5, 4} {
		seedStatsOverviewLog(t, database, models.DailyLog{UserID: user.ID, Date: today.AddDate(0, 0, -offset), BBT: new(dashboardConfirmedOvulationLowBBT)})
	}
	for _, offset := range []int{3, 2, 1} {
		seedStatsOverviewLog(t, database, models.DailyLog{UserID: user.ID, Date: today.AddDate(0, 0, -offset), BBT: new(dashboardConfirmedOvulationHighBBT)})
	}

	_, payload := fetchStatsOverview(t, app, authCookie)

	wantConfirmed := today.AddDate(0, 0, -4).Format(statsOverviewDateLayout)

	if payload.OvulationDate == nil || *payload.OvulationDate != wantConfirmed {
		t.Fatalf("ovulation_date = %v, want the BBT-confirmed %s", payload.OvulationDate, wantConfirmed)
	}
	if !payload.OvulationConfirmed {
		t.Fatal("ovulation_confirmed = false beside a BBT-confirmed ovulation_date")
	}
	if payload.OvulationExact {
		t.Fatal("ovulation_exact = true, want false: the fixture's 18-day median clamps the 14-day fallback luteal phase, and a confirmed observation must not silently launder that into an exact model fit")
	}
}

// TestStatsOverviewConfirmsALateShiftAfterTheProjectedNextPeriodStart is the
// JSON-API half of the current-cycle detection window fix
// (services.ConfirmedCurrentCycleOvulation): a thermal shift whose coverline
// window straddles the model's own projected next period start is still an
// event of the CURRENT cycle, and GET /api/v1/stats/overview must confirm it
// like the calendar and the dashboard do. The cohort is seedLateThermalShiftCycle
// (dashboard_confirmed_ovulation_test.go), shared with the rendered half.
func TestStatsOverviewConfirmsALateShiftAfterTheProjectedNextPeriodStart(t *testing.T) {
	app, database, _ := newOnboardingTestAppWithLocation(t, time.UTC)
	user := createOnboardingTestUser(t, database, "overview-late-shift@example.com", "StrongPass1", true)
	authCookie := loginAndExtractAuthCookie(t, app, user.Email, "StrongPass1")
	today := services.DateAtLocation(time.Now().In(time.UTC), time.UTC)
	updateStatsOverviewUser(t, database, user, map[string]any{"track_bbt": true})
	confirmedDay := seedLateThermalShiftCycle(t, database, user, today)

	_, payload := fetchStatsOverview(t, app, authCookie)

	wantConfirmed := confirmedDay.Format(statsOverviewDateLayout)
	if payload.OvulationDate == nil || *payload.OvulationDate != wantConfirmed {
		t.Fatalf("ovulation_date = %v, want the BBT-confirmed %s (a shift recorded after the projected next period start is still this cycle's)", payload.OvulationDate, wantConfirmed)
	}
	if !payload.OvulationConfirmed {
		t.Fatal("ovulation_confirmed = false beside a BBT-confirmed ovulation_date recorded after the projected next period start")
	}
}

// TestStatsOverviewSuppressedOvulationIsNeverReportedConfirmed pins the floor
// PublishedOverviewStats' own doc comment names: ovulation_confirmed is
// derived off the CLEARED stats, never off the ConfirmedCurrentCycleOvulation
// call alone, so a suppressed projection cannot assert a measurement even if a
// real BBT shift exists in the account's history. The fixture is
// TestStatsOverviewPublishesTheConfirmedOvulationDayNotTheModelsProjection's,
// with unpredictable-cycle mode layered on top — the same temperatures that
// confirm a day there must confirm nothing here, matching the suppression
// gate ConfirmedCurrentCycleOvulation already reads for itself
// (FertilityProjectionSuppressed, cycle_signals.go).
func TestStatsOverviewSuppressedOvulationIsNeverReportedConfirmed(t *testing.T) {
	app, database, _ := newOnboardingTestAppWithLocation(t, time.UTC)
	user := createOnboardingTestUser(t, database, "overview-confirmed-suppressed@example.com", "StrongPass1", true)
	authCookie := loginAndExtractAuthCookie(t, app, user.Email, "StrongPass1")
	today := services.DateAtLocation(time.Now().In(time.UTC), time.UTC)
	updateStatsOverviewUser(t, database, user, map[string]any{"track_bbt": true, "unpredictable_cycle": true})

	seedStatsOverviewCycleHistory(t, database, user, today, 104, 76, 48, 20)
	for _, offset := range []int{9, 8, 7, 6, 5, 4} {
		seedStatsOverviewLog(t, database, models.DailyLog{UserID: user.ID, Date: today.AddDate(0, 0, -offset), BBT: new(dashboardConfirmedOvulationLowBBT)})
	}
	for _, offset := range []int{3, 2, 1} {
		seedStatsOverviewLog(t, database, models.DailyLog{UserID: user.ID, Date: today.AddDate(0, 0, -offset), BBT: new(dashboardConfirmedOvulationHighBBT)})
	}

	_, payload := fetchStatsOverview(t, app, authCookie)

	if !payload.Suppression.Fertility {
		t.Fatal("suppression.fertility = false for an unpredictable-cycle account — fixture did not build the state it claims")
	}
	if payload.OvulationDate != nil {
		t.Fatalf("ovulation_date = %s while suppression.fertility is true", *payload.OvulationDate)
	}
	if payload.OvulationConfirmed {
		t.Fatal("ovulation_confirmed = true beside a null ovulation_date — a suppressed projection must never assert a measurement, even with a real BBT shift on record")
	}
}

// TestStatsOverviewCarriesTheDisclaimerWithoutTheLanguageMiddleware pins the
// framing against its own wiring.
//
// The disclaimer is resolved from the per-request catalogue, which
// LanguageMiddleware fills. Every other test here mounts that middleware, so
// none of them can see what happens without it — and the fallback in
// translateMessage renders a miss as the KEY, which would publish the literal
// "medical.disclaimer" as the owner-visible safety text with the whole suite
// green. Here the resolver runs against a request that carries no catalogue at
// all, which is what a route reached ahead of that middleware would hand it.
func TestStatsOverviewCarriesTheDisclaimerWithoutTheLanguageMiddleware(t *testing.T) {
	manager, err := i18n.NewManager("en")
	if err != nil {
		t.Fatalf("init i18n: %v", err)
	}
	probe := func(t *testing.T, handler *Handler) string {
		t.Helper()

		app := fiber.New()
		app.Get("/probe", func(c fiber.Ctx) error {
			return c.SendString(handler.medicalDisclaimer(c))
		})

		response := mustAppResponse(t, app, httptest.NewRequest(http.MethodGet, "/probe", nil))
		assertStatusCode(t, response, http.StatusOK)
		return mustReadBodyString(t, response.Body)
	}

	t.Run("falls back to the server's default language", func(t *testing.T) {
		disclaimer := probe(t, &Handler{i18n: manager})

		if disclaimer == medicalDisclaimerMessageKey || strings.TrimSpace(disclaimer) == "" {
			t.Fatalf("disclaimer = %q with no request catalogue — the payload must fall back to the server's default language, not to the key", disclaimer)
		}
	})

	// A handler with no catalogue at all is never production — NewHandler takes a
	// manager — only a partially wired test one. It answers empty rather than
	// panicking, matching what the egress adapter does with a nil manager, and
	// never the key: an empty string is visibly missing framing, while the key
	// would read as framing that happens to be untranslated.
	t.Run("a handler with no catalogue answers empty, never the key", func(t *testing.T) {
		if disclaimer := probe(t, &Handler{}); disclaimer != "" {
			t.Fatalf("disclaimer = %q with no i18n manager, want empty", disclaimer)
		}
	})
}

func newStatsOverviewOwner(t *testing.T, app *fiber.App, database *gorm.DB, email string) (models.User, string, time.Time) {
	t.Helper()

	user := createOnboardingTestUser(t, database, email, "StrongPass1", true)
	authCookie := loginAndExtractAuthCookie(t, app, user.Email, "StrongPass1")
	today := services.DateAtLocation(time.Now().In(time.UTC), time.UTC)
	updateStatsOverviewUser(t, database, user, map[string]any{
		"cycle_length":      28,
		"period_length":     5,
		"last_period_start": today.AddDate(0, 0, -6),
	})
	return user, authCookie, today
}

// seedStatsOverviewCycleHistory records three explicit cycle starts, which is
// what lifts the zero-completed-cycle floor: the projection then rests on
// observed lengths rather than on the onboarding setting.
func seedStatsOverviewCycleHistory(t *testing.T, database *gorm.DB, user models.User, today time.Time, daysBackList ...int) {
	t.Helper()

	if len(daysBackList) == 0 {
		return
	}
	for _, daysBack := range daysBackList {
		seedStatsOverviewLog(t, database, models.DailyLog{
			UserID:     user.ID,
			Date:       today.AddDate(0, 0, -daysBack),
			IsPeriod:   true,
			Flow:       models.FlowMedium,
			CycleStart: true,
		})
	}
}

func seedStatsOverviewLog(t *testing.T, database *gorm.DB, log models.DailyLog) {
	t.Helper()

	if err := database.Create(&log).Error; err != nil {
		t.Fatalf("seed daily log: %v", err)
	}
}

func updateStatsOverviewUser(t *testing.T, database *gorm.DB, user models.User, updates map[string]any) {
	t.Helper()

	if err := database.Model(&models.User{}).Where("id = ?", user.ID).Updates(updates).Error; err != nil {
		t.Fatalf("update stats overview owner: %v", err)
	}
}

func fetchStatsOverview(t *testing.T, app *fiber.App, authCookie string) (string, StatsOverviewResponse) {
	t.Helper()

	request := httptest.NewRequest(http.MethodGet, "/api/v1/stats/overview", nil)
	request.Header.Set("Accept", "application/json")
	request.Header.Set("Accept-Language", "en")
	request.Header.Set("Cookie", joinCookieHeader(authCookie, timezoneCookieName+"=UTC"))
	request.Header.Set(timezoneHeaderName, "UTC")

	response := mustAppResponse(t, app, request)
	assertStatusCode(t, response, http.StatusOK)
	body := mustReadBodyString(t, response.Body)

	payload := StatsOverviewResponse{}
	if err := json.Unmarshal([]byte(body), &payload); err != nil {
		t.Fatalf("decode stats overview payload: %v\n%s", err, body)
	}
	return body, payload
}

func fetchStatsPageDocument(t *testing.T, app *fiber.App, authCookie string) *html.Node {
	t.Helper()

	request := httptest.NewRequest(http.MethodGet, "/stats", nil)
	request.Header.Set("Accept-Language", "en")
	request.Header.Set("Cookie", joinCookieHeader(authCookie, timezoneCookieName+"=UTC"))
	request.Header.Set(timezoneHeaderName, "UTC")

	response := mustAppResponse(t, app, request)
	assertStatusCode(t, response, http.StatusOK)
	return mustParseHTMLDocument(t, mustReadBodyString(t, response.Body))
}

// assertStatsOverviewFraming pins the safety framing every state carries: the
// localized disclaimer AND the stable key beside it, so a client branches on the
// key and a test cannot go quiet when the wording changes.
func assertStatsOverviewFraming(t *testing.T, payload StatsOverviewResponse) {
	t.Helper()

	if payload.DisclaimerKey != medicalDisclaimerMessageKey {
		t.Fatalf("disclaimer_key = %q, want %q", payload.DisclaimerKey, medicalDisclaimerMessageKey)
	}
	if strings.TrimSpace(payload.Disclaimer) == "" || payload.Disclaimer == medicalDisclaimerMessageKey {
		t.Fatalf("disclaimer = %q — the payload carries no resolved safety framing", payload.Disclaimer)
	}
	if payload.Suppression.Reasons == nil {
		t.Fatal("suppression.reasons is null — an absent list and an empty one must not mean the same thing to a client")
	}
}

// assertNoInstantTimestamps refuses the shape the endpoint used to publish: the
// domain struct's time.Time fields serialize as RFC 3339 instants, which name a
// timezone the owner never chose and carry a zero date as a real one.
func assertNoInstantTimestamps(t *testing.T, body string) {
	t.Helper()

	if strings.Contains(body, "T00:00:00") {
		t.Fatalf("the payload carries an instant, not a calendar day:\n%s", body)
	}
	if strings.Contains(body, "0001-01-01") {
		t.Fatalf("the payload publishes a zero date instead of null:\n%s", body)
	}
}

func findHTMLNodeWithAttr(document *html.Node, attribute string) *html.Node {
	return htmlFindElement(document, func(node *html.Node) bool {
		return node.Type == html.ElementNode && htmlHasAttr(node, attribute)
	})
}

func containsString(values []string, want string) bool {
	for _, value := range values {
		if value == want {
			return true
		}
	}
	return false
}
