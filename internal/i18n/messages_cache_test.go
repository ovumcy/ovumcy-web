package i18n

import "testing"

// TestMessagesServesTheMergedCatalogueWithoutCopying (WEB-289): every request
// reads its catalogue through Messages, a refused one included, so the call
// must not rebuild the merged map — it allocates nothing — while still laying
// the language over the default for a key it lacks.
func TestMessagesServesTheMergedCatalogueWithoutCopying(t *testing.T) {
	manager, err := NewManager(LangEN)
	if err != nil {
		t.Fatalf("init i18n manager: %v", err)
	}

	if allocs := testing.AllocsPerRun(20, func() { _ = manager.Messages(LangDE) }); allocs != 0 {
		t.Fatalf("Messages allocated %.0f times per call, want 0: it rebuilds the catalogue per request", allocs)
	}

	german := manager.Messages("de-DE")
	for key, value := range manager.locales[LangDE] {
		if german[key] != value {
			t.Fatalf("Messages(de-DE)[%q] = %q, want the German %q", key, german[key], value)
		}
	}
	for key, value := range manager.locales[LangEN] {
		if _, ok := manager.locales[LangDE][key]; !ok && german[key] != value {
			t.Fatalf("Messages(de-DE)[%q] = %q, want the default's %q for a key German lacks", key, german[key], value)
		}
	}
	if fallback := manager.Messages("xx"); len(fallback) != len(manager.locales[LangEN]) {
		t.Fatalf("Messages(xx) has %d keys, want the default catalogue's %d", len(fallback), len(manager.locales[LangEN]))
	}
}
