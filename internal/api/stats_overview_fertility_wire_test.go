package api

import (
	"encoding/json"
	"go/ast"
	"go/parser"
	"go/token"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/ovumcy/ovumcy-web/internal/models"
	"github.com/ovumcy/ovumcy-web/internal/services"
	"gorm.io/gorm"
)

// The fertility status, its basis and the out-of-date verdict, read on the
// wire and against the published schema in each of the values they can take.
// The response-schema guard drives this operation once, for an owner whose
// status is unknown, so on its own it never validated a "projection" or a
// "confirmed" basis or a true cycle_data_stale.

const statsOverviewOperation = "GET /api/v1/stats/overview"

func TestStatsOverviewBodiesMatchTheSchemaInEveryFertilityBasis(t *testing.T) {
	schemas, declared := loadOpenAPISchemaGuardDocument(t)
	success, ok := declared[statsOverviewOperation]
	if !ok {
		t.Fatalf("%s declares no JSON success body", statsOverviewOperation)
	}

	for _, tc := range []struct {
		name      string
		seed      func(t *testing.T, database *gorm.DB, user models.User, today time.Time)
		wantBasis any // the decoded wire value: a string, or nil for null
		wantStale bool
	}{
		{
			name: "projection",
			seed: func(t *testing.T, database *gorm.DB, user models.User, today time.Time) {
				seedStatsOverviewCycleHistory(t, database, user, today, 90, 62, 34, 6)
			},
			wantBasis: services.FertilityBasisProjection,
		},
		{
			// The confirmed-shift owner of
			// TestStatsOverviewPublishesTheConfirmedOvulationDayNotTheModelsProjection:
			// cycle day 21, the shift confirmed on today-4.
			name: "confirmed",
			seed: func(t *testing.T, database *gorm.DB, user models.User, today time.Time) {
				// The onboarding anchor six days back would stay active and
				// open a cycle no shift lies in.
				updateStatsOverviewUser(t, database, user, map[string]any{
					"track_bbt":         true,
					"last_period_start": services.AddCalendarDays(today, -20, time.UTC),
				})
				seedStatsOverviewCycleHistory(t, database, user, today, 104, 76, 48, 20)
				for _, offset := range []int{9, 8, 7, 6, 5, 4} {
					seedStatsOverviewLog(t, database, models.DailyLog{UserID: user.ID, Date: services.AddCalendarDays(today, -offset, time.UTC), BBT: new(dashboardConfirmedOvulationLowBBT)})
				}
				for _, offset := range []int{3, 2, 1} {
					seedStatsOverviewLog(t, database, models.DailyLog{UserID: user.ID, Date: services.AddCalendarDays(today, -offset, time.UTC), BBT: new(dashboardConfirmedOvulationHighBBT)})
				}
			},
			wantBasis: services.FertilityBasisConfirmed,
		},
		{
			name: "out of date",
			seed: func(t *testing.T, database *gorm.DB, user models.User, today time.Time) {
				seedStatsOverviewCycleHistory(t, database, user, today, 114, 86, 58, 30)
				updateStatsOverviewUser(t, database, user, map[string]any{
					"last_period_start": services.AddCalendarDays(today, -30, time.UTC),
				})
			},
			wantBasis: nil,
			wantStale: true,
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			app, database, _ := newOnboardingTestAppWithLocation(t, time.UTC)
			user, authCookie, today := newStatsOverviewOwner(t, app, database, "wire-"+strings.ReplaceAll(tc.name, " ", "-")+"@example.com")
			tc.seed(t, database, user, today)

			body, _ := fetchStatsOverview(t, app, authCookie)
			var decoded any
			if err := json.Unmarshal([]byte(body), &decoded); err != nil {
				t.Fatalf("decode: %v", err)
			}
			if found := schemas.violations(decoded, success.schema, "body", 0); len(found) > 0 {
				t.Fatalf("the body departs from the declared schema:\n  %s\nbody: %s", strings.Join(found, "\n  "), body)
			}

			wire, _ := decoded.(map[string]any)
			basis, present := wire["fertility_basis"]
			if !present {
				t.Fatal("fertility_basis is absent from the wire; the schema requires it, null included")
			}
			if basis != tc.wantBasis {
				t.Fatalf("fertility_basis = %#v on the wire, want %#v", basis, tc.wantBasis)
			}
			if stale, _ := wire["cycle_data_stale"].(bool); stale != tc.wantStale {
				t.Fatalf("cycle_data_stale = %#v on the wire, want %v", wire["cycle_data_stale"], tc.wantStale)
			}
			// Null exactly when the status is unknown, read off the wire.
			if (basis == nil) != (wire["current_fertility"] == services.FertilityStatusUnknown) {
				t.Fatalf("fertility_basis %#v beside current_fertility %#v — null exactly when the status is unknown", basis, wire["current_fertility"])
			}
		})
	}
}

// TestStatsOverviewSpecEnumsMatchTheFertilityConstants pins the two published
// enums to the constants the services write. The constants are enumerated from
// the services package source by name prefix, so a value added there reds this
// until the spec names it too.
func TestStatsOverviewSpecEnumsMatchTheFertilityConstants(t *testing.T) {
	document := parseOpenAPIDocument(t, filepath.Join("..", "..", "docs", "openapi.yaml"))
	properties := document.get("components").get("schemas").get("StatsOverview").get("properties")

	constants := servicesStringConstants(t)
	for _, field := range []struct {
		property, prefix, loadBearing, loadBearingValue string
		nullable                                        bool
	}{
		{"current_fertility", "FertilityStatus", "FertilityStatusOutsideEstimatedWindow", services.FertilityStatusOutsideEstimatedWindow, false},
		{"fertility_basis", "FertilityBasis", "FertilityBasisConfirmed", services.FertilityBasisConfirmed, true},
	} {
		if got := constants[field.loadBearing]; got != field.loadBearingValue {
			t.Fatalf("the source walk read %s as %q, want %q — the walk is not reading the constants the code uses", field.loadBearing, got, field.loadBearingValue)
		}
		var want []string
		for name, value := range constants {
			if strings.HasPrefix(name, field.prefix) {
				want = append(want, value)
			}
		}
		if field.nullable {
			want = append(want, "null")
		}
		sort.Strings(want)

		enum := properties.get(field.property).get("enum")
		if enum == nil || len(enum.items) == 0 {
			t.Fatalf("%s declares no enum", field.property)
		}
		got := make([]string, 0, len(enum.items))
		for _, item := range enum.items {
			got = append(got, item.text())
		}
		sort.Strings(got)
		if strings.Join(got, ",") != strings.Join(want, ",") {
			t.Fatalf("%s enum = %v, the services' %s* constants give %v", field.property, got, field.prefix, want)
		}
	}
}

// servicesStringConstants reads every string constant declared in the
// services package's production source.
func servicesStringConstants(t *testing.T) map[string]string {
	t.Helper()

	files, err := filepath.Glob(filepath.Join("..", "services", "*.go"))
	if err != nil || len(files) == 0 {
		t.Fatalf("list the services package: %v (%d files)", err, len(files))
	}
	constants := map[string]string{}
	fileSet := token.NewFileSet()
	for _, path := range files {
		if strings.HasSuffix(path, "_test.go") {
			continue
		}
		parsed, err := parser.ParseFile(fileSet, path, nil, parser.SkipObjectResolution)
		if err != nil {
			t.Fatalf("parse %s: %v", path, err)
		}
		for _, declaration := range parsed.Decls {
			general, ok := declaration.(*ast.GenDecl)
			if !ok || general.Tok != token.CONST {
				continue
			}
			for _, spec := range general.Specs {
				valueSpec := spec.(*ast.ValueSpec)
				for index, name := range valueSpec.Names {
					if index >= len(valueSpec.Values) {
						continue
					}
					literal, ok := valueSpec.Values[index].(*ast.BasicLit)
					if !ok || literal.Kind != token.STRING {
						continue
					}
					value, err := strconv.Unquote(literal.Value)
					if err != nil {
						t.Fatalf("%s: unquote %s: %v", path, name.Name, err)
					}
					constants[name.Name] = value
				}
			}
		}
	}
	return constants
}

// TestCalendarPaintsOneAnswerWhereTheRangeWindowOutrunsTheNextPeriod: in the
// irregular range mode the fertile window runs to the longest recent cycle's
// ovulation (cycle day 31 for cycles of 25, 45 and 28 days) and the next period
// is projected on day 29. The window is not clamped there; the shared cells
// resolve through the ladder's existing rung for a start-window day the
// fertile window also covers, so each paints one state that keeps both facts.
func TestCalendarPaintsOneAnswerWhereTheRangeWindowOutrunsTheNextPeriod(t *testing.T) {
	user := &models.User{Role: models.RoleOwner, CycleLength: 28, PeriodLength: 5, IrregularCycle: true}
	var logs []models.DailyLog
	for _, start := range []string{"2026-01-23", "2026-02-17", "2026-04-03", "2026-05-01"} {
		first, err := time.Parse("2006-01-02", start)
		if err != nil {
			t.Fatal(err)
		}
		for offset := range 5 {
			logs = append(logs, models.DailyLog{Date: first.AddDate(0, 0, offset), IsPeriod: true, CycleStart: offset == 0, Flow: models.FlowMedium})
		}
	}
	today := time.Date(2026, 5, 30, 0, 0, 0, 0, time.UTC)
	stats := services.BuildCycleStatsFromLogs(user, logs, today, time.UTC)
	if services.CalendarDayKey(stats.NextPeriodStart) != "2026-05-29" || services.CalendarDayKey(stats.FertilityWindowEnd) != "2026-05-31" {
		t.Fatalf("fixture: next period %s, window end %s, want 2026-05-29 and 2026-05-31", services.CalendarDayKey(stats.NextPeriodStart), services.CalendarDayKey(stats.FertilityWindowEnd))
	}

	states := services.BuildCalendarDayStates(user, time.Date(2026, 5, 1, 0, 0, 0, 0, time.UTC), logs, stats, today, time.UTC)
	keys := map[string]string{}
	for _, day := range (&Handler{}).buildCalendarDays(states) {
		keys[day.DateString] = day.StateKey
	}
	for _, day := range []string{"2026-05-29", "2026-05-30", "2026-05-31"} {
		if keys[day] != "predicted-start-window-in-fertile-window" {
			t.Fatalf("%s paints %q, want the one rung that keeps the start window and the fertile window together", day, keys[day])
		}
	}
	if keys["2026-06-01"] != "predicted-start-window" {
		t.Fatalf("2026-06-01 paints %q, want the start window alone past the fertile window", keys["2026-06-01"])
	}
}
