package i18n

import (
	"fmt"
	"sort"
	"strings"
	"testing"
)

// Seven validator sentences are carried twice: once under `onboarding.step2.`
// for the onboarding form and once under `settings.cycle.` for the cycle
// settings card. They are the same two validators reading the same two fields,
// their English text is identical to the character, and both surfaces render a
// plain template literal — so a wording change costs twelve edits and nothing
// reports a half-applied one. This class has already drifted once:
// `onboarding.step1.field` and `settings.cycle.last_period_start` agree in
// English and disagree in Italian ("ultimo ciclo" vs "ultimo periodo").
//
// The registry is EXPLICIT and short on purpose. "Same suffix under these two
// prefixes ⇒ same text" is not true as a rule: `onboarding.step2.auto_period_fill`
// ("Auto-mark period days") and `settings.cycle.auto_period_fill` ("Auto-fill
// period days") differ deliberately, as do `onboarding.step2.irregular_cycle`
// ("My cycle is usually irregular") and `settings.cycle.irregular_cycle` ("I
// have an irregular cycle") — a form label and a settings toggle addressing the
// reader differently. Widening this to a pattern would demand those be made
// identical, which is a copy regression dressed as consistency. Adding a suffix
// here is a claim that the two surfaces must say the same thing; make it
// deliberately.
var pairedOnboardingAndSettingsSuffixes = []string{
	"auto_period_fill_hint",
	"error_incompatible",
	"info_adjusted",
	"info_cycle_short",
	"info_period_long",
	"irregular_cycle_hint",
	"warning_approximate",
}

const (
	pairedOnboardingPrefix = "onboarding.step2."
	pairedSettingsPrefix   = "settings.cycle."
)

func TestPairedOnboardingAndSettingsStringsAgreeInEveryLocale(t *testing.T) {
	locales := mustLoadAllLocaleMessages(t)

	languages := make([]string, 0, len(locales))
	for language := range locales {
		languages = append(languages, language)
	}
	sort.Strings(languages)

	if len(languages) != len(requiredLocales) {
		t.Fatalf("loaded %d locale(s), expected %d; this check would pass by simply not reading the catalogues", len(languages), len(requiredLocales))
	}

	var failures []string
	for _, suffix := range pairedOnboardingAndSettingsSuffixes {
		onboardingKey := pairedOnboardingPrefix + suffix
		settingsKey := pairedSettingsPrefix + suffix

		for _, language := range languages {
			messages := locales[language]

			// A missing half is a failure, not a skip. Deleting one side is
			// exactly the one-sided edit this check exists to catch, and a
			// check that skips what it cannot find goes quiet instead of red.
			onboarding, onboardingPresent := messages[onboardingKey]
			settings, settingsPresent := messages[settingsKey]
			switch {
			case !onboardingPresent && !settingsPresent:
				failures = append(failures, fmt.Sprintf("  %s: neither %q nor %q is defined", language, onboardingKey, settingsKey))
				continue
			case !onboardingPresent:
				failures = append(failures, fmt.Sprintf("  %s: %q is defined but %q is not", language, settingsKey, onboardingKey))
				continue
			case !settingsPresent:
				failures = append(failures, fmt.Sprintf("  %s: %q is defined but %q is not", language, onboardingKey, settingsKey))
				continue
			}

			if onboarding != settings {
				failures = append(failures, fmt.Sprintf("  %s: %q and %q disagree\n      %s: %q\n      %s: %q",
					language, onboardingKey, settingsKey, pairedOnboardingPrefix, onboarding, pairedSettingsPrefix, settings))
			}
		}
	}

	if len(failures) == 0 {
		return
	}
	t.Fatalf("%d paired string(s) out of step:\n%s\n"+
		"These seven sentences are the same two validators on the same two fields, rendered on two surfaces. "+
		"Change both halves in every locale, or drop the suffix from pairedOnboardingAndSettingsSuffixes and say "+
		"in the same change why the two surfaces are now meant to diverge.",
		len(failures), strings.Join(failures, "\n"))
}

// The registry must name pairs that exist, or a typo in it would be a check
// that runs on nothing — and the "missing half" branch above would report the
// typo as a product defect rather than as its own mistake.
func TestPairedSuffixRegistryNamesRealKeys(t *testing.T) {
	locales := mustLoadAllLocaleMessages(t)
	reference, ok := locales[LangEN]
	if !ok {
		t.Fatalf("reference locale %q is missing", LangEN)
	}

	seen := map[string]bool{}
	for _, suffix := range pairedOnboardingAndSettingsSuffixes {
		if seen[suffix] {
			t.Errorf("%q is listed twice in pairedOnboardingAndSettingsSuffixes", suffix)
		}
		seen[suffix] = true

		for _, key := range []string{pairedOnboardingPrefix + suffix, pairedSettingsPrefix + suffix} {
			if strings.TrimSpace(reference[key]) == "" {
				t.Errorf("pairedOnboardingAndSettingsSuffixes names %q, which the English catalogue does not answer; the registry is stale", key)
			}
		}
	}
}
