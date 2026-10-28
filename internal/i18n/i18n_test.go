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

// TestIsSupportedLanguageSeparatesUnknownFromDefault pins the distinction
// NormalizeLanguage cannot make. A stored language code (users.interface_language)
// is only usable if this build actually ships its catalogue: normalization
// answers with the default for an unknown code, which a caller reading a stored
// value would take for a deliberate choice of that default.
func TestIsSupportedLanguageSeparatesUnknownFromDefault(t *testing.T) {
	manager := newTestI18nManager(t, LangEN)

	for _, supported := range []string{LangRU, "ru-RU", "RU", LangEN} {
		if !manager.IsSupportedLanguage(supported) {
			t.Fatalf("expected %q to be recognized as a shipped locale", supported)
		}
	}
	for _, unsupported := range []string{"", "   ", "zz", "pt-BR"} {
		if manager.IsSupportedLanguage(unsupported) {
			t.Fatalf("expected %q to be rejected as unsupported, while NormalizeLanguage answers %q",
				unsupported, manager.NormalizeLanguage(unsupported))
		}
	}
}

// TestMatchAcceptLanguageTellsANegotiatedLanguageFromNone pins the no-fallback
// half onboarding stores from: ok only for an entry naming a shipped locale,
// and never for one the client refused with q=0.
func TestMatchAcceptLanguageTellsANegotiatedLanguageFromNone(t *testing.T) {
	manager := newTestI18nManager(t, LangEN)

	cases := []struct {
		header string
		want   string
		wantOK bool
	}{
		{header: "fr-CA,fr;q=0.9", want: LangFR, wantOK: true},
		{header: "ja, it;q=0.5", want: LangIT, wantOK: true},
		{header: "EN-us", want: LangEN, wantOK: true},
		{header: "de;q=0, ru;q=0.3", want: LangRU, wantOK: true},
		{header: "de; Q=0.000", want: "", wantOK: false},
		{header: "es;q=bogus", want: LangES, wantOK: true},
		{header: "de;level=1;q=0, it;flag", want: LangIT, wantOK: true},
		{header: "ja, pt-BR", want: "", wantOK: false},
		{header: "", want: "", wantOK: false},
		{header: " , ;q=1", want: "", wantOK: false},
	}
	for _, tc := range cases {
		got, ok := manager.MatchAcceptLanguage(tc.header)
		if got != tc.want || ok != tc.wantOK {
			t.Fatalf("MatchAcceptLanguage(%q) = (%q, %v), want (%q, %v)", tc.header, got, ok, tc.want, tc.wantOK)
		}
	}
	if got := manager.DetectFromAcceptLanguage("de;q=0"); got != LangEN {
		t.Fatalf("expected a refused-only header to fall back to %q, got %q", LangEN, got)
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
