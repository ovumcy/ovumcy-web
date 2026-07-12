package api

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gofiber/fiber/v3"
)

// Mutation-kill tests for localizedSettingsSymptomError
// (handlers_settings_symptoms.go L95/L96). The helper maps a raw symptom error
// string to its i18n key and returns the localized string only when both the
// mapping exists (`key != ""`, L95) AND the catalog resolves it to something
// other than the key itself (`localized != key`, L96); otherwise it echoes the
// raw source verbatim. Negating either guard (CONDITIONALS_NEGATION) either
// drops a real localization or leaks the raw i18n key. A synthetic catalog pins
// the branch logic without depending on real copy.

// localizedSettingsSymptomErrorForTest drives localizedSettingsSymptomError with
// an injected message catalog via a mounted route (the function reads
// currentMessages(c) from the request Locals), mirroring the status-side helper.
func localizedSettingsSymptomErrorForTest(t *testing.T, source string, messages map[string]string) string {
	t.Helper()

	app := fiber.New()
	app.Get("/localize-error", func(c fiber.Ctx) error {
		c.Locals(contextMessagesKey, messages)
		return c.SendString(localizedSettingsSymptomError(c, source))
	})
	req := httptest.NewRequest(http.MethodGet, "/localize-error", nil)
	resp, err := app.Test(req, testConfigNoTimeout)
	if err != nil {
		t.Fatalf("symptom-error localizer probe failed: %v", err)
	}
	defer func() { _ = resp.Body.Close() }()
	return mustReadBodyString(t, resp.Body)
}

// TestLocalizedSettingsSymptomError pins both decisions in
// localizedSettingsSymptomError:
//   - L95 `AuthErrorTranslationKey(source) != ""`: a mappable source with a
//     present translation is localized; negating skips localization and echoes
//     the raw source.
//   - L96 `translateMessage(...) != key`: a mappable source whose translation is
//     MISSING must echo the raw source, not the bare i18n key; negating returns
//     the key instead.
func TestLocalizedSettingsSymptomError(t *testing.T) {
	const source = "symptom name is too long" // -> settings.symptoms.error.name_too_long
	const i18nKey = "settings.symptoms.error.name_too_long"

	// Mappable source + translation present -> localized (kills L95 and the
	// present-translation arm of L96).
	if got := localizedSettingsSymptomErrorForTest(t, source, map[string]string{i18nKey: "SENTINEL_SYMPTOM_LOCALIZED"}); got != "SENTINEL_SYMPTOM_LOCALIZED" {
		t.Fatalf("mappable+translated symptom error must localize, got %q", got)
	}

	// Mappable source + NO translation -> echoes the raw source, never the key
	// (kills the missing-translation arm of L96).
	if got := localizedSettingsSymptomErrorForTest(t, source, map[string]string{}); got != source {
		t.Fatalf("mappable-but-untranslated symptom error must echo the raw source, got %q", got)
	}

	// Unmappable source -> echoed verbatim (baseline for the L95 false branch).
	if got := localizedSettingsSymptomErrorForTest(t, "totally unmapped symptom error", map[string]string{}); got != "totally unmapped symptom error" {
		t.Fatalf("unmappable symptom error must echo verbatim, got %q", got)
	}

	// Blank source -> empty (short-circuit guard, unaffected by the mutants but
	// pins the documented contract).
	if got := localizedSettingsSymptomErrorForTest(t, "   ", map[string]string{}); got != "" {
		t.Fatalf("blank symptom error must resolve to empty, got %q", got)
	}
}
