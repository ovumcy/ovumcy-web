package i18n

import (
	"regexp"
	"strings"
	"testing"
)

// One name for the predicted fertile days in every locale: the legend, the
// estimate caption and the dashboard badges all say what dashboard.fertile_window says.
func TestFertileWindowSurfacesShareTheCanonicalName(t *testing.T) {
	keys := []string{
		"calendar.legend.fertility",
		"calendar.fertility_estimate",
		"dashboard.fertility_badge_warning",
		"dashboard.fertility_badge_positive",
	}
	for language, messages := range mustLoadAllLocaleMessages(t) {
		canonical := strings.ToLower(messages["dashboard.fertile_window"])
		if canonical == "" {
			t.Fatalf("%s dashboard.fertile_window is missing", language)
		}
		for _, key := range keys {
			if !strings.Contains(strings.ToLower(messages[key]), canonical) {
				t.Errorf("%s %s = %q does not use %q", language, key, messages[key], messages["dashboard.fertile_window"])
			}
		}
	}
}

// A prediction is an estimate on every surface, whatever the data confidence:
// the fertility chips, the legend and the next-period days carry the qualifier
// in their own wording, not only beside an Approximate cycle hero.
func TestPredictionSurfacesCarryAnEstimateQualifier(t *testing.T) {
	qualifiers := map[string]*regexp.Regexp{
		LangEN: regexp.MustCompile(`(?i)\bestimate`),
		LangRU: regexp.MustCompile(`(?i)оценк|ориентировочн`),
		LangDE: regexp.MustCompile(`(?i)geschätzt|schätzung`),
		LangES: regexp.MustCompile(`(?i)estimad|estimación`),
		LangFR: regexp.MustCompile(`(?i)estimé|estimation`),
		LangIT: regexp.MustCompile(`(?i)stimat|stima\b`),
	}
	keys := []string{
		"dashboard.high_fertility_badge",
		"dashboard.fertility_badge_warning",
		"dashboard.fertility_badge_positive",
		"dashboard.fertile_window",
		"calendar.legend.fertility",
		"calendar.fertility_estimate",
		"dashboard.next_period_days",
		"dashboard.next_period_start_window",
	}
	all := mustLoadAllLocaleMessages(t)
	if len(all) != len(qualifiers) {
		t.Fatalf("loaded %d locales, the qualifier table covers %d", len(all), len(qualifiers))
	}
	for language, messages := range all {
		qualifier := qualifiers[language]
		if qualifier == nil {
			t.Fatalf("no estimate qualifier pattern for locale %s", language)
		}
		for _, key := range keys {
			if !qualifier.MatchString(messages[key]) {
				t.Errorf("%s %s = %q carries no estimate qualifier", language, key, messages[key])
			}
		}
	}
}

// The product addresses the owner without a gendered form; feminine wording is
// reserved for copy about the cycle itself.
func TestItalianDirectAddressIsNotGendered(t *testing.T) {
	gendered := regexp.MustCompile(`(?i)\b(sicura|connessa|connesso|pronta|pronto|iscritta|iscritto)\b`)
	messages := mustLoadAllLocaleMessages(t)[LangIT]
	for _, key := range []string{"confirm.delete_entry", "auth.remember_me"} {
		if gendered.MatchString(messages[key]) {
			t.Errorf("it %s addresses the reader with a gendered form: %q", key, messages[key])
		}
	}
	for key, value := range messages {
		if strings.HasPrefix(key, "auth.") || strings.HasPrefix(key, "confirm.") {
			if gendered.MatchString(value) {
				t.Errorf("it %s addresses the reader with a gendered form: %q", key, value)
			}
		}
	}
}
