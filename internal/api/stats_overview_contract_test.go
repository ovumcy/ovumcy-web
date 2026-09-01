package api

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/gofiber/fiber/v3"
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
	// wantReasons are the reasons the payload must NAME. Several signals can hold
	// at once (a paused owner with no completed cycle names two), so this is a
	// containment check, never an equality one.
	wantReasons       []string
	wantPredictions   bool
	wantFertility     bool
	wantNextPeriodSet bool
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
		},
		{
			name: "active pregnancy pause",
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
		},
		{
			name: "unpredictable cycle mode",
			seed: func(t *testing.T, database *gorm.DB, user models.User, today time.Time) {
				updateStatsOverviewUser(t, database, user, map[string]any{"unpredictable_cycle": true})
			},
			wantReasons:     []string{"unpredictable_cycle"},
			wantPredictions: true,
			wantFertility:   true,
		},
		{
			name: "cycle overdue past its own reference length",
			seed: func(t *testing.T, database *gorm.DB, user models.User, today time.Time) {
				updateStatsOverviewUser(t, database, user, map[string]any{
					"last_period_start": today.AddDate(0, 0, -45),
				})
			},
			wantReasons:     []string{"cycle_overdue"},
			wantPredictions: true,
			wantFertility:   true,
		},
	}
}

func TestStatsOverviewWithholdsEveryProjectionItsGatesRefuse(t *testing.T) {
	for _, state := range statsOverviewStates() {
		t.Run(state.name, func(t *testing.T) {
			app, database, _ := newOnboardingTestAppWithLocation(t, time.UTC)
			user, authCookie, today := newStatsOverviewOwner(t, app, database, "overview-"+strings.ReplaceAll(state.name, " ", "-")+"@example.com")
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
			if (payload.NextPeriodStart != nil) != state.wantNextPeriodSet {
				t.Fatalf("next_period_start present = %v, want %v", payload.NextPeriodStart != nil, state.wantNextPeriodSet)
			}

			// Recorded history is fact, not projection: it survives every tier, or
			// the endpoint has answered "suppressed" by publishing nothing at all.
			if payload.LastPeriodStart == nil {
				t.Fatal("last_period_start is null — a recorded anchor is not a projection and no gate withholds it")
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
	seedStatsOverviewCycleHistory(t, database, user, today)

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
func TestStatsOverviewAgreesWithTheStatsPageOnFertility(t *testing.T) {
	for _, state := range statsOverviewStates() {
		t.Run(state.name, func(t *testing.T) {
			app, database, _ := newOnboardingTestAppWithLocation(t, time.UTC)
			user, authCookie, today := newStatsOverviewOwner(t, app, database, "parity-"+strings.ReplaceAll(state.name, " ", "-")+"@example.com")
			state.seed(t, database, user, today)

			_, payload := fetchStatsOverview(t, app, authCookie)
			document := fetchStatsPageDocument(t, app, authCookie)

			// The page renders the fertility status only in the tier that has one;
			// where it does, the two surfaces must name the same value.
			if node := findHTMLNodeWithAttr(document, "data-fertility-status"); node != nil {
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
func seedStatsOverviewCycleHistory(t *testing.T, database *gorm.DB, user models.User, today time.Time) {
	t.Helper()

	for _, daysBack := range []int{62, 34, 6} {
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
