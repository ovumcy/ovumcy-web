package i18n

import (
	"path/filepath"
	"runtime"
	"slices"
	"testing"
)

func TestManagerSupportsGermanLocale(t *testing.T) {
	manager := newTestI18nManager(t, LangDE)

	supported := manager.SupportedLanguages()
	if !slices.Equal(supported, []string{LangDE, LangEN, LangES, LangFR, LangRU}) {
		t.Fatalf("expected sorted supported languages [de en es fr ru], got %#v", supported)
	}
	if manager.DefaultLanguage() != LangDE {
		t.Fatalf("expected default language %q, got %q", LangDE, manager.DefaultLanguage())
	}
	if got := manager.NormalizeLanguage("de-DE"); got != LangDE {
		t.Fatalf("expected de-DE to normalize to %q, got %q", LangDE, got)
	}
	if got := manager.DetectFromAcceptLanguage("de-DE,de;q=0.9,en;q=0.8"); got != LangDE {
		t.Fatalf("expected Accept-Language to detect %q, got %q", LangDE, got)
	}
}

func TestManagerFallsBackToDefaultForUnsupportedLanguage(t *testing.T) {
	manager := newTestI18nManager(t, LangEN)

	if got := manager.NormalizeLanguage("it-IT"); got != LangEN {
		t.Fatalf("expected unsupported language to fall back to %q, got %q", LangEN, got)
	}
	if got := manager.DetectFromAcceptLanguage("it-IT,it;q=0.9"); got != LangEN {
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
