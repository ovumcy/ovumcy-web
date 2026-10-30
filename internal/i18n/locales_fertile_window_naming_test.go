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
