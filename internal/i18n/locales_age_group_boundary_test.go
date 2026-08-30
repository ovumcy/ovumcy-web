package i18n

import "testing"

// TestAgeGroupLabelsDoNotOverlapAt45 pins P2-02: the "40-45" and "45+" labels
// both literally covered age 45, so a woman who is exactly 45 fit either
// radio button even though they are mutually exclusive, and the only reader
// of the bracket (services.shouldShowStatsPerimenopauseHint) treats them as
// disjoint. The stored value (age_40_45) is unchanged — this is copy only.
func TestAgeGroupLabelsDoNotOverlapAt45(t *testing.T) {
	want := map[string]struct {
		fortyTo45 string
		fortyFive string
	}{
		LangEN: {"40-44", "45+"},
		LangRU: {"40-44", "45+"},
		LangDE: {"40-44", "45+"},
		LangES: {"40-44", "45+"},
		LangIT: {"40-44", "45+"},
		LangFR: {"40–44 ans", "45 ans et plus"},
	}

	locales := mustLoadAllLocaleMessages(t)
	for language, expect := range want {
		messages, ok := locales[language]
		if !ok {
			t.Fatalf("locale %q not loaded", language)
		}
		if got := messages["settings.age_group.40_to_45"]; got != expect.fortyTo45 {
			t.Errorf("%s settings.age_group.40_to_45 = %q, want %q (must exclude 45 so it doesn't double-cover the 45+ bracket)", language, got, expect.fortyTo45)
		}
		if got := messages["settings.age_group.45_plus"]; got != expect.fortyFive {
			t.Errorf("%s settings.age_group.45_plus = %q, want %q", language, got, expect.fortyFive)
		}
	}
}
