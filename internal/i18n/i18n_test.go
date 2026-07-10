package i18n

import (
	"slices"
	"sort"
	"testing"
)

func TestManagerSupportsGermanLocale(t *testing.T) {
	manager := newTestI18nManager(t, LangDE)

	supported := manager.SupportedLanguages()
	if !slices.Equal(supported, []string{LangDE, LangEN, LangES, LangFR, LangIT, LangRU}) {
		t.Fatalf("expected sorted supported languages [de en es fr it ru], got %#v", supported)
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

	if got := manager.NormalizeLanguage("pt-BR"); got != LangEN {
		t.Fatalf("expected unsupported language to fall back to %q, got %q", LangEN, got)
	}
	if got := manager.DetectFromAcceptLanguage("pt-BR,pt;q=0.9"); got != LangEN {
		t.Fatalf("expected unsupported Accept-Language to fall back to %q, got %q", LangEN, got)
	}
}

func TestRequiredLocalesCoversAllSupportedLanguages(t *testing.T) {
	want := []string{LangDE, LangEN, LangES, LangFR, LangIT, LangRU}

	got := slices.Clone(requiredLocales)
	sort.Strings(got)

	if !slices.Equal(got, want) {
		t.Fatalf("expected requiredLocales to cover exactly %#v, got %#v", want, got)
	}
}

func newTestI18nManager(t *testing.T, defaultLanguage string) *Manager {
	t.Helper()

	manager, err := NewManager(defaultLanguage)
	if err != nil {
		t.Fatalf("NewManager() unexpected error: %v", err)
	}
	return manager
}
