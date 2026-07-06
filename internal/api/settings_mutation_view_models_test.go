package api

import (
	"testing"

	"github.com/ovumcy/ovumcy-web/internal/models"
)

// These are behavior-asserting unit tests over the settings/onboarding view-model
// helpers, added to close mutation-testing survivors in the SETTINGS/ONBOARDING
// cluster (settings_symptom_view_models.go, language_switch_view_models.go). Each
// test pins an observable output of a boolean/equality decision so that negating
// the operator makes the assertion fail (kills the mutant).
//
// Documented EQUIVALENT mutant (intentionally NOT killed): the ARITHMETIC_BASE
// mutant on settings_symptom_view_models.go:81 changes the slice-capacity hint
// `make([]settingsSymptomIconOption, 0, len(settingsSymptomIconCatalog)+1)` from
// `+1` to `-1`. Capacity is only a pre-allocation hint; `append` grows the slice
// as needed, so the returned length and contents are identical either way. No
// test can distinguish the two without asserting on cap(), which is not observable
// behavior — killing it would be a brittle, meaningless test.

// --- buildSettingsSymptomIconOptions ---

// findIconOption returns the option carrying the given value, or nil.
func findIconOption(options []settingsSymptomIconOption, value string) *settingsSymptomIconOption {
	for i := range options {
		if options[i].Value == value {
			return &options[i]
		}
	}
	return nil
}

// TestBuildSettingsSymptomIconOptions_CatalogIconSelectsExactlyThatOption pins the
// `value == selected` decision (settings_symptom_view_models.go:92): only the icon
// equal to the current selection is Selected; every other catalog icon is not.
// Negating to `!=` would flip Selected on all the wrong options.
func TestBuildSettingsSymptomIconOptions_CatalogIconSelectsExactlyThatOption(t *testing.T) {
	options := buildSettingsSymptomIconOptions("🔥")

	// No custom option is prepended for a catalog icon (settingsSymptomIconInCatalog
	// must report true here), so the returned set is exactly the catalog.
	if len(options) != len(settingsSymptomIconCatalog) {
		t.Fatalf("expected %d options for a catalog icon, got %d", len(settingsSymptomIconCatalog), len(options))
	}

	selectedCount := 0
	for _, option := range options {
		if option.IsCustom {
			t.Errorf("catalog icon %q must not produce a custom option, got value %q", "🔥", option.Value)
		}
		if option.Selected {
			selectedCount++
			if option.Value != "🔥" {
				t.Errorf("only the current icon should be selected; got Selected on %q", option.Value)
			}
		}
	}
	if selectedCount != 1 {
		t.Fatalf("exactly one option should be selected, got %d", selectedCount)
	}

	fire := findIconOption(options, "🔥")
	if fire == nil || !fire.Selected {
		t.Fatalf("current icon 🔥 must be marked Selected")
	}
	// A different catalog icon must NOT be selected — this is what dies if
	// `value == selected` is negated.
	if spark := findIconOption(options, "✨"); spark == nil || spark.Selected {
		t.Fatalf("non-selected catalog icon ✨ must have Selected=false")
	}
}

// TestBuildSettingsSymptomIconOptions_CustomIconIsPrependedAsCustom pins the
// `option == value` membership decision inside settingsSymptomIconInCatalog
// (settings_symptom_view_models.go:100). A non-catalog icon must be reported as
// NOT in the catalog, so a custom option is prepended at index 0. Negating to
// `!=` makes the first differing catalog entry match, so the icon is wrongly
// treated as in-catalog and no custom option is emitted.
func TestBuildSettingsSymptomIconOptions_CustomIconIsPrependedAsCustom(t *testing.T) {
	const customIcon = "🦄" // deliberately absent from settingsSymptomIconCatalog

	options := buildSettingsSymptomIconOptions(customIcon)

	if len(options) != len(settingsSymptomIconCatalog)+1 {
		t.Fatalf("custom icon should add exactly one option, want %d got %d",
			len(settingsSymptomIconCatalog)+1, len(options))
	}
	if options[0].Value != customIcon {
		t.Fatalf("custom icon must be the leading option, got %q", options[0].Value)
	}
	if !options[0].IsCustom {
		t.Fatalf("leading custom option must have IsCustom=true")
	}
	if !options[0].Selected {
		t.Fatalf("leading custom option must be Selected")
	}
	// No catalog icon should be selected when the selection is a custom icon.
	for _, option := range options[1:] {
		if option.Selected {
			t.Errorf("catalog option %q must not be selected when a custom icon is active", option.Value)
		}
		if option.IsCustom {
			t.Errorf("catalog option %q must not be flagged custom", option.Value)
		}
	}
}

// TestSettingsSymptomIconInCatalog_ReportsMembership guards the membership helper
// directly on both branches so the equality check keeps its meaning.
func TestSettingsSymptomIconInCatalog_ReportsMembership(t *testing.T) {
	if !settingsSymptomIconInCatalog("💧") {
		t.Errorf("💧 is a catalog icon and must be reported as present")
	}
	if settingsSymptomIconInCatalog("🦄") {
		t.Errorf("🦄 is not a catalog icon and must be reported as absent")
	}
}

// --- buildSettingsSymptomRows ---

// TestBuildSettingsSymptomRows_DraftAppliesOnlyToTargetedSymptom pins the
// `rowState.SymptomID == symptom.ID` decision (settings_symptom_view_models.go:53,
// second operand). Draft form values must override only the row whose ID matches
// rowState.SymptomID; every other row keeps its persisted values. Negating the
// equality would apply the draft to the wrong rows.
func TestBuildSettingsSymptomRows_DraftAppliesOnlyToTargetedSymptom(t *testing.T) {
	symptoms := []models.SymptomType{
		{ID: 11, Name: "Cramps", Icon: "🔥"},
		{ID: 22, Name: "Bloating", Icon: "💧"},
	}
	rowState := settingsSymptomRowState{
		SymptomID:      11,
		UseDraftValues: true,
		Draft:          symptomPayload{Name: "Edited draft", Icon: "⚡"},
	}
	identity := func(s string) string { return s }

	rows := buildSettingsSymptomRows(symptoms, rowState, identity, identity)
	if len(rows) != 2 {
		t.Fatalf("expected 2 rows, got %d", len(rows))
	}

	// Targeted row (ID 11) reflects the draft.
	if rows[0].FormName != "Edited draft" {
		t.Errorf("targeted row FormName = %q, want draft %q", rows[0].FormName, "Edited draft")
	}
	if rows[0].FormIcon != "⚡" {
		t.Errorf("targeted row FormIcon = %q, want draft %q", rows[0].FormIcon, "⚡")
	}
	// Untargeted row (ID 22) keeps its persisted values — dies if == is negated.
	if rows[1].FormName != "Bloating" {
		t.Errorf("untargeted row FormName = %q, want persisted %q", rows[1].FormName, "Bloating")
	}
	if rows[1].FormIcon != "💧" {
		t.Errorf("untargeted row FormIcon = %q, want persisted %q", rows[1].FormIcon, "💧")
	}
}

// TestBuildSettingsSymptomRows_ZeroRowStateIDNeverUsesDraft pins the
// `rowState.SymptomID != 0` guard (settings_symptom_view_models.go:53, first
// operand). A zero rowState.SymptomID means "no row is being edited", so even a
// symptom whose ID is the zero value must render its persisted values, not the
// draft. Negating the guard to `== 0` would make the draft leak onto a zero-ID
// symptom.
func TestBuildSettingsSymptomRows_ZeroRowStateIDNeverUsesDraft(t *testing.T) {
	symptoms := []models.SymptomType{
		{ID: 0, Name: "Persisted name", Icon: "🌙"},
	}
	rowState := settingsSymptomRowState{
		SymptomID:      0,
		UseDraftValues: true,
		Draft:          symptomPayload{Name: "Draft name", Icon: "⚡"},
	}
	identity := func(s string) string { return s }

	rows := buildSettingsSymptomRows(symptoms, rowState, identity, identity)
	if len(rows) != 1 {
		t.Fatalf("expected 1 row, got %d", len(rows))
	}
	if rows[0].FormName != "Persisted name" {
		t.Errorf("zero-ID row FormName = %q, want persisted %q (draft must not apply)", rows[0].FormName, "Persisted name")
	}
	if rows[0].FormIcon != "🌙" {
		t.Errorf("zero-ID row FormIcon = %q, want persisted %q (draft must not apply)", rows[0].FormIcon, "🌙")
	}
}

// TestBuildSettingsSymptomRows_LocalizesMessagesOnlyForTargetedRow verifies the
// localizers are applied to the matching row's messages, exercising the
// row-match branch that populates Error/Success.
func TestBuildSettingsSymptomRows_LocalizesMessagesOnlyForTargetedRow(t *testing.T) {
	symptoms := []models.SymptomType{
		{ID: 5, Name: "Headache", Icon: "🤕"},
		{ID: 6, Name: "Fatigue", Icon: "🌀"},
	}
	rowState := settingsSymptomRowState{
		SymptomID:     5,
		ErrorMessage:  "boom",
		SuccessStatus: "yay",
	}
	statusLoc := func(s string) string {
		if s == "" {
			return ""
		}
		return "status:" + s
	}
	errorLoc := func(s string) string {
		if s == "" {
			return ""
		}
		return "error:" + s
	}

	rows := buildSettingsSymptomRows(symptoms, rowState, statusLoc, errorLoc)
	if rows[0].ErrorMessage != "error:boom" {
		t.Errorf("matched row ErrorMessage = %q, want %q", rows[0].ErrorMessage, "error:boom")
	}
	if rows[0].SuccessMessage != "status:yay" {
		t.Errorf("matched row SuccessMessage = %q, want %q", rows[0].SuccessMessage, "status:yay")
	}
	if rows[1].ErrorMessage != "" || rows[1].SuccessMessage != "" {
		t.Errorf("non-matched row must have empty messages, got err=%q success=%q", rows[1].ErrorMessage, rows[1].SuccessMessage)
	}
}

// --- language switch view models ---

// TestBuildLanguageSwitchOptions_MarksOnlyCurrentActive pins the
// `normalizedCode == currentLanguage` decision (language_switch_view_models.go:24):
// exactly the current language option is Active. Negating to `!=` would mark every
// other language active and the current one inactive.
func TestBuildLanguageSwitchOptions_MarksOnlyCurrentActive(t *testing.T) {
	messages := map[string]string{}
	supported := []string{"en", "ru", "es"}

	options := buildLanguageSwitchOptions(messages, "ru", supported)
	if len(options) != 3 {
		t.Fatalf("expected 3 options, got %d", len(options))
	}

	activeCodes := map[string]bool{}
	for _, option := range options {
		if option.Active {
			activeCodes[option.Code] = true
		}
	}
	if len(activeCodes) != 1 || !activeCodes["ru"] {
		t.Fatalf("exactly the current language ru should be active, got active set %v", activeCodes)
	}
}

// TestBuildLanguageSwitchOptions_SkipsBlankCodes covers the empty-code continue
// (language_switch_view_models.go:18): blank/whitespace codes are dropped.
func TestBuildLanguageSwitchOptions_SkipsBlankCodes(t *testing.T) {
	options := buildLanguageSwitchOptions(map[string]string{}, "en", []string{"en", "  ", "", "es"})
	if len(options) != 2 {
		t.Fatalf("blank codes must be skipped, want 2 options got %d", len(options))
	}
	for _, option := range options {
		if option.Code != "en" && option.Code != "es" {
			t.Errorf("unexpected option code %q", option.Code)
		}
	}
}

// TestLocalizedLanguageSwitchLabel_UsesTranslationWhenPresent pins the
// `localized == key` fallback decision (language_switch_view_models.go:33, first
// operand): when the message catalog carries a real label for lang.<code>, that
// label is returned verbatim; only a missing translation falls back to the
// uppercased code. Negating `==` to `!=` would discard a present translation and
// wrongly uppercase the code.
func TestLocalizedLanguageSwitchLabel_UsesTranslationWhenPresent(t *testing.T) {
	messages := map[string]string{"lang.ru": "Русский"}

	if got := localizedLanguageSwitchLabel(messages, "ru"); got != "Русский" {
		t.Fatalf("present translation must be used, got %q want %q", got, "Русский")
	}
	// Missing translation falls back to the uppercased code.
	if got := localizedLanguageSwitchLabel(messages, "es"); got != "ES" {
		t.Fatalf("missing translation must fall back to uppercased code, got %q want %q", got, "ES")
	}
}
