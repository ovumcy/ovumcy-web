package api

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/ovumcy/ovumcy-web/internal/models"
	"github.com/ovumcy/ovumcy-web/internal/services"
	"golang.org/x/net/html"
)

// TestStatsPageBBTSurfacesAgreeOnTheOwnersUnit renders /stats for one stored
// cycle twice, once per temperature unit, and reads the control reading back
// from every surface that shows it: the chart payload (plotted value and the
// coverline the axis and label are drawn from), the crosshair text, the axis
// suffix, the unit beside the heading, the table twin and the text summary.
// The stored readings are Celsius for both owners; 97.7 °F is what a
// Fahrenheit owner typed to store 36.5, so both renders describe one reading.
func TestStatsPageBBTSurfacesAgreeOnTheOwnersUnit(t *testing.T) {
	typed := 97.7
	if stored := services.ConvertDayBBTToStorage(&typed, services.TemperatureUnitFahrenheit); stored == nil || *stored != 36.5 {
		t.Fatalf("premise: 97.7 °F must store as 36.5 °C, got %v", stored)
	}

	cases := []struct {
		unit    string
		symbol  string
		control float64
		text    string
	}{
		{unit: services.TemperatureUnitCelsius, symbol: "°C", control: 36.5, text: "36.5"},
		{unit: services.TemperatureUnitFahrenheit, symbol: "°F", control: 97.7, text: "97.7"},
	}
	for _, tc := range cases {
		t.Run(tc.unit, func(t *testing.T) {
			document := renderStatsBBTUnitPage(t, tc.unit)

			if unit := htmlElementWithAttr(document, "data-bbt-unit"); unit == nil || strings.TrimSpace(htmlNodeText(unit)) != tc.symbol {
				t.Fatalf("expected the BBT heading unit to read %q", tc.symbol)
			}

			chart := htmlElementByID(document, "bbt-chart")
			if chart == nil {
				t.Fatal("expected the BBT chart container")
			}
			if got := htmlAttr(chart, "data-value-suffix"); got != tc.symbol {
				t.Fatalf("axis/crosshair suffix = %q, want %q", got, tc.symbol)
			}
			var payload struct {
				Values     []*float64 `json:"values"`
				ValueTexts []string   `json:"valueTexts"`
				Baseline   float64    `json:"baseline"`
			}
			if err := json.Unmarshal([]byte(htmlAttr(chart, "data-chart")), &payload); err != nil {
				t.Fatalf("decode BBT chart payload: %v", err)
			}
			if len(payload.Values) < 3 || payload.Values[2] == nil || *payload.Values[2] != tc.control {
				t.Fatalf("plotted control reading = %v, want %v", payload.Values, tc.control)
			}
			if len(payload.ValueTexts) < 3 || payload.ValueTexts[2] != tc.text {
				t.Fatalf("crosshair control text = %v, want %q", payload.ValueTexts, tc.text)
			}
			if payload.Baseline != tc.control {
				t.Fatalf("coverline = %v, want %v", payload.Baseline, tc.control)
			}

			row := htmlFindElement(document, htmlNodeAttrEquals("data-cycle-day", "3"))
			if row == nil {
				t.Fatal("expected a table row for cycle day 3")
			}
			if cell := htmlElementWithAttr(row, "data-bbt-table-value"); cell == nil || strings.TrimSpace(htmlNodeText(cell)) != tc.text+tc.symbol {
				t.Fatalf("expected the table twin to read %q for cycle day 3", tc.text+tc.symbol)
			}

			summary := htmlElementByID(document, "stats-bbt-summary")
			if summary == nil {
				t.Fatal("expected the BBT text summary")
			}
			if want := fmt.Sprintf("%.2f %s", tc.control, tc.symbol); !strings.Contains(htmlNodeText(summary), want) {
				t.Fatalf("summary %q does not name the coverline as %q", htmlNodeText(summary), want)
			}
		})
	}
}

func renderStatsBBTUnitPage(t *testing.T, unit string) *html.Node {
	t.Helper()

	app, database := newOnboardingTestApp(t)
	user := createOnboardingTestUser(t, database, "stats-bbt-unit-"+unit+"@example.com", "StrongPass1", true)
	authCookie := loginAndExtractAuthCookie(t, app, user.Email, "StrongPass1")

	now := time.Now().UTC()
	cycleStart := time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, time.UTC).AddDate(0, 0, -11)
	if err := database.Model(&models.User{}).Where("id = ?", user.ID).Updates(map[string]any{
		"last_period_start": cycleStart,
		"track_bbt":         true,
		"temperature_unit":  unit,
	}).Error; err != nil {
		t.Fatalf("update user settings: %v", err)
	}

	// Two completed cycles take the page past its empty state. In the current
	// one, six low readings whose highest is the day-3 control, then three days
	// above it: the detector confirms the shift with the control as coverline.
	logs := []models.DailyLog{
		{UserID: user.ID, Date: cycleStart.AddDate(0, 0, -56), IsPeriod: true},
		{UserID: user.ID, Date: cycleStart.AddDate(0, 0, -28), IsPeriod: true},
	}
	readings := map[int]float64{0: 36.30, 1: 36.40, 2: 36.50, 3: 36.35, 4: 36.45, 5: 36.40, 6: 36.70, 7: 36.75, 8: 36.80}
	for offset, reading := range readings {
		log := models.DailyLog{UserID: user.ID, Date: cycleStart.AddDate(0, 0, offset), IsPeriod: offset == 0, BBT: new(reading)}
		logs = append(logs, log)
	}
	if err := database.Create(&logs).Error; err != nil {
		t.Fatalf("create BBT logs: %v", err)
	}

	request := httptest.NewRequest(http.MethodGet, "/stats", nil)
	request.Header.Set("Accept-Language", "en")
	request.Header.Set("Cookie", authCookie)
	response, err := app.Test(request, testConfigNoTimeout)
	if err != nil {
		t.Fatalf("stats request failed: %v", err)
	}
	defer func() { _ = response.Body.Close() }()
	if response.StatusCode != http.StatusOK {
		t.Fatalf("expected status 200, got %d", response.StatusCode)
	}
	return mustParseHTMLDocument(t, mustReadBodyString(t, response.Body))
}
