package i18n

import (
	"path/filepath"
	"runtime"
	"slices"
	"testing"
)

func TestManagerSupportsSpanishLocale(t *testing.T) {
	manager := newTestI18nManager(t, LangES)

	supported := manager.SupportedLanguages()
	if !slices.Equal(supported, []string{LangEN, LangES, LangFR, LangRU}) {
		t.Fatalf("expected sorted supported languages [en es ru fr], got %#v", supported)
	}
	if manager.DefaultLanguage() != LangES {
		t.Fatalf("expected default language %q, got %q", LangES, manager.DefaultLanguage())
	}
	if got := manager.NormalizeLanguage("es-MX"); got != LangES {
		t.Fatalf("expected es-MX to normalize to %q, got %q", LangES, got)
	}
	if got := manager.DetectFromAcceptLanguage("es-MX,es;q=0.9,en;q=0.8"); got != LangES {
		t.Fatalf("expected Accept-Language to detect %q, got %q", LangES, got)
	}
}

func TestManagerFallsBackToDefaultForUnsupportedLanguage(t *testing.T) {
	manager := newTestI18nManager(t, LangEN)

	if got := manager.NormalizeLanguage("de-DE"); got != LangEN {
		t.Fatalf("expected unsupported language to fall back to %q, got %q", LangEN, got)
	}
	if got := manager.DetectFromAcceptLanguage("de-DE,de;q=0.9"); got != LangEN {
		t.Fatalf("expected unsupported Accept-Language to fall back to %q, got %q", LangEN, got)
	}
}

func newTestI18nManager(t *testing.T, defaultLanguage string) *Manager {
	t.Helper()

	_, thisFile, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("resolve test file path: runtime.Caller failed")
	}
	localesDir := filepath.Join(filepath.Dir(thisFile), "locales")
	manager, err := NewManager(defaultLanguage, localesDir)
	if err != nil {
		t.Fatalf("NewManager() unexpected error: %v", err)
	}
	return manager
}
