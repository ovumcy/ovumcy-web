package api

import (
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"net/url"
	"reflect"
	"strings"
	"testing"

	"github.com/gofiber/fiber/v3"
	"github.com/ovumcy/ovumcy-web/internal/models"
	"github.com/ovumcy/ovumcy-web/internal/services"
)

func patchCycleSettingsJSON(t *testing.T, app *fiber.App, authCookie string, body string) *http.Response {
	t.Helper()

	request := httptest.NewRequest(http.MethodPatch, "/api/v1/users/current/cycle", strings.NewReader(body))
	request.Header.Set("Content-Type", "application/json")
	request.Header.Set("Accept", "application/json")
	request.Header.Set("Cookie", authCookie)

	response, err := app.Test(request, testConfigNoTimeout)
	if err != nil {
		t.Fatalf("cycle settings request failed: %v", err)
	}
	return response
}

// TestCycleSettingsPatchKeepsWhatTheBodyDidNotCarry is the finding itself: a
// JSON save of the cycle geometry alone used to rewrite every member it did not
// mention to that member's zero value. The costly one is `usage_goal`: an owner
// tracking to avoid pregnancy was silently moved to general health tracking,
// which reframes the fertile window, the badges and the summaries on every
// surface — a mode the product only ever changes on an explicit owner action.
// `age_group` and the three flags went the same way.
func TestCycleSettingsPatchKeepsWhatTheBodyDidNotCarry(t *testing.T) {
	app, database := newOnboardingTestApp(t)
	user := createOnboardingTestUser(t, database, "cycle-patch-keeps@example.com", "StrongPass1", true)
	if err := database.Model(&models.User{}).Where("id = ?", user.ID).Updates(map[string]any{
		"cycle_length":        30,
		"period_length":       6,
		"usage_goal":          models.UsageGoalAvoid,
		"age_group":           models.AgeGroup40To45,
		"irregular_cycle":     true,
		"auto_period_fill":    true,
		"unpredictable_cycle": false,
	}).Error; err != nil {
		t.Fatalf("set initial cycle values: %v", err)
	}
	authCookie := loginAndExtractAuthCookie(t, app, user.Email, "StrongPass1")

	response := patchCycleSettingsJSON(t, app, authCookie, `{"cycle_length":29,"period_length":5}`)
	defer func() { _ = response.Body.Close() }()
	if response.StatusCode != http.StatusOK {
		t.Fatalf("expected status 200, got %d", response.StatusCode)
	}

	persisted := models.User{}
	if err := database.First(&persisted, user.ID).Error; err != nil {
		t.Fatalf("load persisted user: %v", err)
	}
	if persisted.CycleLength != 29 || persisted.PeriodLength != 5 {
		t.Fatalf("expected the submitted lengths, got cycle %d period %d", persisted.CycleLength, persisted.PeriodLength)
	}
	if persisted.UsageGoal != models.UsageGoalAvoid {
		t.Fatalf("a save that never named the goal changed it to %q", persisted.UsageGoal)
	}
	if persisted.AgeGroup != models.AgeGroup40To45 {
		t.Fatalf("a save that never named the age bracket changed it to %q", persisted.AgeGroup)
	}
	if !persisted.IrregularCycle || !persisted.AutoPeriodFill {
		t.Fatalf("a save that named neither flag cleared them: irregular=%v auto_fill=%v", persisted.IrregularCycle, persisted.AutoPeriodFill)
	}
}

// TestCycleSettingsPatchWritesEveryMemberItDoesCarry is the other half: partial
// must not become inert. A member present in the body still wins over the stored
// value, including one carrying a zero value, which is exactly the case a
// presence check exists to tell apart from absence.
func TestCycleSettingsPatchWritesEveryMemberItDoesCarry(t *testing.T) {
	app, database := newOnboardingTestApp(t)
	user := createOnboardingTestUser(t, database, "cycle-patch-writes@example.com", "StrongPass1", true)
	if err := database.Model(&models.User{}).Where("id = ?", user.ID).Updates(map[string]any{
		"usage_goal":       models.UsageGoalAvoid,
		"age_group":        models.AgeGroup40To45,
		"irregular_cycle":  true,
		"auto_period_fill": true,
	}).Error; err != nil {
		t.Fatalf("set initial cycle values: %v", err)
	}
	authCookie := loginAndExtractAuthCookie(t, app, user.Email, "StrongPass1")

	response := patchCycleSettingsJSON(t, app, authCookie, `{"cycle_length":28,"period_length":5,"usage_goal":"trying_to_conceive","age_group":"","irregular_cycle":false,"auto_period_fill":false,"unpredictable_cycle":true}`)
	defer func() { _ = response.Body.Close() }()
	if response.StatusCode != http.StatusOK {
		t.Fatalf("expected status 200, got %d", response.StatusCode)
	}

	persisted := models.User{}
	if err := database.First(&persisted, user.ID).Error; err != nil {
		t.Fatalf("load persisted user: %v", err)
	}
	if persisted.UsageGoal != models.UsageGoalTrying {
		t.Fatalf("expected the submitted goal, got %q", persisted.UsageGoal)
	}
	if persisted.AgeGroup != models.AgeGroupUnknown {
		t.Fatalf("expected the explicitly cleared age bracket, got %q", persisted.AgeGroup)
	}
	if persisted.IrregularCycle || persisted.AutoPeriodFill {
		t.Fatalf("expected both flags explicitly cleared, got irregular=%v auto_fill=%v", persisted.IrregularCycle, persisted.AutoPeriodFill)
	}
	if !persisted.UnpredictableCycle {
		t.Fatal("expected the submitted unpredictable-cycle flag to be written")
	}
}

// TestCycleSettingsFormSaveRefusesNonNumericLengths covers the form transport's
// own two refusals: it parses the lengths itself rather than binding them, so a
// non-numeric value has to be answered there and not carried into validation as
// a zero.
func TestCycleSettingsFormSaveRefusesNonNumericLengths(t *testing.T) {
	app, database := newOnboardingTestApp(t)
	user := createOnboardingTestUser(t, database, "cycle-form-nonnumeric@example.com", "StrongPass1", true)
	authCookie := loginAndExtractAuthCookie(t, app, user.Email, "StrongPass1")

	for _, form := range []url.Values{
		{"cycle_length": {"twenty-eight"}, "period_length": {"5"}},
		{"cycle_length": {"28"}, "period_length": {"five"}},
	} {
		request := httptest.NewRequest(http.MethodPatch, "/api/v1/users/current/cycle", strings.NewReader(form.Encode()))
		request.Header.Set("Content-Type", "application/x-www-form-urlencoded")
		request.Header.Set("Accept", "application/json")
		request.Header.Set("Cookie", authCookie)

		response, err := app.Test(request, testConfigNoTimeout)
		if err != nil {
			t.Fatalf("cycle settings request failed: %v", err)
		}
		if response.StatusCode != http.StatusBadRequest {
			_ = response.Body.Close()
			t.Fatalf("expected status 400 for %v, got %d", form, response.StatusCode)
		}
		_ = response.Body.Close()
	}
}

// TestCycleSettingsValidationErrorKeyNamesEveryMappedFailure pins the mapper
// including its default arm, which no request can reach: every sentinel the
// policy declares gets its own key, and anything else falls back to the generic
// refusal rather than leaking the error's own text.
func TestCycleSettingsValidationErrorKeyNamesEveryMappedFailure(t *testing.T) {
	for err, want := range map[error]string{
		services.ErrSettingsCycleLengthOutOfRange:          "cycle length must be between 15 and 90",
		services.ErrSettingsPeriodLengthOutOfRange:         "period length must be between 1 and 14",
		services.ErrSettingsPeriodLengthIncompatible:       "period length is incompatible with cycle length",
		services.ErrSettingsCycleStartDateInvalid:          "invalid cycle start date",
		errors.New("an error the policy does not declare"): "invalid settings input",
	} {
		if got := cycleSettingsValidationErrorKey(err); got != want {
			t.Fatalf("expected %q for %v, got %q", want, err, got)
		}
	}
}

// TestUsageGoalSwitchKeepsWhatRidesBesideIt pins the goal-only shortcut's own
// edge. It used to admit any body without the two lengths, so a body carrying
// the goal AND a flag took the one-column path and dropped the flag — the same
// silent partial save, arriving from the other side.
func TestUsageGoalSwitchKeepsWhatRidesBesideIt(t *testing.T) {
	app, database := newOnboardingTestApp(t)
	user := createOnboardingTestUser(t, database, "cycle-goal-plus-flag@example.com", "StrongPass1", true)
	authCookie := loginAndExtractAuthCookie(t, app, user.Email, "StrongPass1")

	response := patchCycleSettingsJSON(t, app, authCookie, `{"usage_goal":"avoid_pregnancy","irregular_cycle":true}`)
	defer func() { _ = response.Body.Close() }()
	if response.StatusCode != http.StatusOK {
		t.Fatalf("expected status 200, got %d", response.StatusCode)
	}

	persisted := models.User{}
	if err := database.First(&persisted, user.ID).Error; err != nil {
		t.Fatalf("load persisted user: %v", err)
	}
	if persisted.UsageGoal != models.UsageGoalAvoid {
		t.Fatalf("expected the submitted goal, got %q", persisted.UsageGoal)
	}
	if !persisted.IrregularCycle {
		t.Fatalf("the flag beside the goal was dropped")
	}
}

// TestUsageGoalOnlySaveAnswersTheDeclaredOkShape pins the response against the
// schema that describes it: `OkResponse` declares `additionalProperties: false`,
// so the echoed `usage_goal` made this the one save whose success body a client
// validating against docs/openapi.yaml rejects.
func TestUsageGoalOnlySaveAnswersTheDeclaredOkShape(t *testing.T) {
	app, database := newOnboardingTestApp(t)
	user := createOnboardingTestUser(t, database, "cycle-goal-only-shape@example.com", "StrongPass1", true)
	authCookie := loginAndExtractAuthCookie(t, app, user.Email, "StrongPass1")

	response := patchCycleSettingsJSON(t, app, authCookie, `{"usage_goal":"avoid_pregnancy"}`)
	defer func() { _ = response.Body.Close() }()
	if response.StatusCode != http.StatusOK {
		t.Fatalf("expected status 200, got %d", response.StatusCode)
	}

	body := map[string]any{}
	if err := json.NewDecoder(response.Body).Decode(&body); err != nil {
		t.Fatalf("decode response body: %v", err)
	}
	if len(body) != 1 {
		t.Fatalf("expected exactly the declared `ok` member, got %v", body)
	}
	if ok, present := body["ok"].(bool); !present || !ok {
		t.Fatalf("expected `ok: true`, got %v", body["ok"])
	}

	persisted := models.User{}
	if err := database.First(&persisted, user.ID).Error; err != nil {
		t.Fatalf("load persisted user: %v", err)
	}
	if persisted.UsageGoal != models.UsageGoalAvoid {
		t.Fatalf("expected the goal-only save to persist, got %q", persisted.UsageGoal)
	}
}

// TestCycleSettingsFormSaveStaysAFullSnapshot is the counterweight to the three
// above: presence is only meaningful where the transport can express it. The
// settings form submits every control it owns, and an unchecked box submits
// nothing at all, so a form body has no absent members to honour — reading one
// as a patch would leave an owner unable to switch a toggle off.
func TestCycleSettingsFormSaveStaysAFullSnapshot(t *testing.T) {
	app, database := newOnboardingTestApp(t)
	user := createOnboardingTestUser(t, database, "cycle-form-snapshot@example.com", "StrongPass1", true)
	if err := database.Model(&models.User{}).Where("id = ?", user.ID).Updates(map[string]any{
		"irregular_cycle":  true,
		"auto_period_fill": true,
	}).Error; err != nil {
		t.Fatalf("set initial cycle values: %v", err)
	}
	authCookie := loginAndExtractAuthCookie(t, app, user.Email, "StrongPass1")

	request := httptest.NewRequest(http.MethodPatch, "/api/v1/users/current/cycle", strings.NewReader(url.Values{
		"cycle_length":  {"28"},
		"period_length": {"5"},
		"usage_goal":    {models.UsageGoalHealth},
		"age_group":     {""},
	}.Encode()))
	request.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	request.Header.Set("Accept", "application/json")
	request.Header.Set("Cookie", authCookie)

	response, err := app.Test(request, testConfigNoTimeout)
	if err != nil {
		t.Fatalf("cycle settings request failed: %v", err)
	}
	defer func() { _ = response.Body.Close() }()
	if response.StatusCode != http.StatusOK {
		t.Fatalf("expected status 200, got %d", response.StatusCode)
	}

	persisted := models.User{}
	if err := database.First(&persisted, user.ID).Error; err != nil {
		t.Fatalf("load persisted user: %v", err)
	}
	if persisted.IrregularCycle || persisted.AutoPeriodFill {
		t.Fatalf("an unchecked box did not clear its flag: irregular=%v auto_fill=%v", persisted.IrregularCycle, persisted.AutoPeriodFill)
	}
}

// TestCycleSettingsPatchRefusesABodyThatNamesNothing covers the empty answer.
// Before the save became partial an empty body was refused as an out-of-range
// cycle_length; accepting it now would answer 200 and write a mutation-log entry
// for a cycle-settings change that did not happen.
func TestCycleSettingsPatchRefusesABodyThatNamesNothing(t *testing.T) {
	app, database := newOnboardingTestApp(t)
	user := createOnboardingTestUser(t, database, "cycle-patch-empty@example.com", "StrongPass1", true)
	authCookie := loginAndExtractAuthCookie(t, app, user.Email, "StrongPass1")

	for _, body := range []string{`{}`, `{"unrelated":1}`} {
		response := patchCycleSettingsJSON(t, app, authCookie, body)
		if response.StatusCode != http.StatusBadRequest {
			_ = response.Body.Close()
			t.Fatalf("expected status 400 for %s, got %d", body, response.StatusCode)
		}
		_ = response.Body.Close()
	}
}

// TestCycleSettingsPatchRefusesAMalformedBody pins the other refusal the member
// probe can produce: a body that does not decode at all is a request problem,
// not a goal-only save, and must not fall through to either path.
func TestCycleSettingsPatchRefusesAMalformedBody(t *testing.T) {
	app, database := newOnboardingTestApp(t)
	user := createOnboardingTestUser(t, database, "cycle-patch-malformed@example.com", "StrongPass1", true)
	authCookie := loginAndExtractAuthCookie(t, app, user.Email, "StrongPass1")

	response := patchCycleSettingsJSON(t, app, authCookie, `{"usage_goal":`)
	defer func() { _ = response.Body.Close() }()

	if response.StatusCode != http.StatusBadRequest {
		t.Fatalf("expected status 400 for a malformed body, got %d", response.StatusCode)
	}

	persisted := models.User{}
	if err := database.First(&persisted, user.ID).Error; err != nil {
		t.Fatalf("load persisted user: %v", err)
	}
	if persisted.UsageGoal != models.UsageGoalHealth {
		t.Fatalf("a malformed body changed the stored goal to %q", persisted.UsageGoal)
	}
}

// TestGoalOnlyShortcutYieldsToEveryOtherMember is the no-allowlist sweep behind
// the goal-only shortcut: for EVERY member the wire shape declares other than
// the goal itself, a body carrying that member beside the goal must leave the
// one-column path. It derives its roster from cycleSettingsPatchProbe rather
// than a list, so a member added to the shape and forgotten in the predicate
// fails here by name instead of being silently dropped by the shortcut.
func TestGoalOnlyShortcutYieldsToEveryOtherMember(t *testing.T) {
	goal := models.UsageGoalAvoid
	probeType := reflect.TypeOf(cycleSettingsPatchProbe{})

	for index := range probeType.NumField() {
		field := probeType.Field(index)
		if field.Name == "UsageGoal" {
			continue
		}

		patch := services.CycleSettingsPatch{UsageGoal: &goal}
		member := reflect.ValueOf(&patch).Elem().FieldByName(field.Name)
		if !member.IsValid() {
			t.Fatalf("%s is declared on the wire shape but not on services.CycleSettingsPatch", field.Name)
		}
		member.Set(reflect.New(member.Type().Elem()))

		if patchCarriesOnlyUsageGoal(patch) {
			t.Fatalf("a body carrying %s beside the goal still took the one-column path, which drops it", field.Tag.Get("json"))
		}
	}
}
