package i18n

import (
	"reflect"
	"testing"
)

// TestMessagesServesTheMergedCatalogueWithoutCopying (WEB-289): every request
// reads its catalogue through Messages, a refused one included, so the call
// must hand back the one merged map rather than rebuild it, while still laying
// the language over the default for a key it lacks.
func TestMessagesServesTheMergedCatalogueWithoutCopying(t *testing.T) {
	manager, err := NewManager(LangEN)
	if err != nil {
		t.Fatalf("init i18n manager: %v", err)
	}

	if reflect.ValueOf(manager.Messages(LangDE)).Pointer() != reflect.ValueOf(manager.Messages("de-DE")).Pointer() {
		t.Fatal("Messages returned a different map per call: it rebuilds the catalogue per request")
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
