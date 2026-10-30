package i18n

import (
	"regexp"
	"strings"
	"testing"
)

// Rate-limit windows are configurable (RATE_LIMIT_*_WINDOW), so no catalogue
// may promise a fixed duration.
func TestRateLimitCopyStatesNoFixedDuration(t *testing.T) {
	digits := regexp.MustCompile(`\d`)
	locales := mustLoadAllLocaleMessages(t)
	checked := 0
	for language, messages := range locales {
		for key, value := range messages {
			if !strings.HasPrefix(key, "auth.error.too_many_") {
				continue
			}
			checked++
			if digits.MatchString(value) {
				t.Errorf("%s %s states a fixed duration: %q", language, key, value)
			}
		}
	}
	if checked < len(requiredLocales) {
		t.Fatalf("matched only %d rate-limit strings; the guard would pass by reading nothing", checked)
	}
}

// Every predicted-day surface keeps an estimate word in every locale
// (constitution: medical safety).
func TestPredictedDayCopyKeepsAnEstimateWord(t *testing.T) {
	stems := map[string][]string{
		LangEN: {"estimated", "likely"},
		LangRU: {"ориентиров", "вероятн"},
		LangDE: {"geschätzt", "voraussichtlich"},
		LangFR: {"estimé", "probable"},
		LangES: {"estimad", "probablemente", "estima"},
		LangIT: {"stimat", "probabil"},
	}
	keys := []string{
		"dashboard.next_period_start_window",
		"dashboard.reminder_banner_period_today",
		"dashboard.reminder_banner_period_tomorrow",
		"dashboard.reminder_banner_ovulation_today",
		"dashboard.reminder_banner_ovulation_tomorrow",
		"webhook.reminder.period.message",
		"webhook.reminder.period.message_range",
		"webhook.reminder.ovulation.message",
		"webhook.reminder.ovulation.message_range",
	}
	locales := mustLoadAllLocaleMessages(t)
	for language, words := range stems {
		messages, ok := locales[language]
		if !ok {
			t.Fatalf("locale %q not loaded", language)
		}
		for _, key := range keys {
			value := strings.ToLower(messages[key])
			if value == "" {
				t.Errorf("%s %s is missing", language, key)
				continue
			}
			found := false
			for _, word := range words {
				if strings.Contains(value, word) {
					found = true
					break
				}
			}
			if !found {
				t.Errorf("%s %s carries no estimate word (%v): %q", language, key, words, messages[key])
			}
		}
	}
}

func TestItalianCopyRollup(t *testing.T) {
	messages := mustLoadAllLocaleMessages(t)[LangIT]
	for _, key := range []string{"stats.cycle_chart_summary", "stats.cycle_chart_summary_no_baseline"} {
		if !strings.Contains(messages[key], "da %d a %d") {
			t.Errorf("it %s lacks \"da %%d a %%d\": %q", key, messages[key])
		}
	}
	if !strings.Contains(messages["settings.export_summary_range"], "da %s a %s") {
		t.Errorf("it settings.export_summary_range lacks \"da %%s a %%s\"")
	}
	if strings.Contains(messages["auth.register_subtitle"], "Prima avvio") {
		t.Errorf("it auth.register_subtitle still reads \"Prima avvio\"")
	}
}

// The downloaded recovery file is built in JS from these keys, so it follows
// the interface language.
func TestRecoveryFileCopyIsLocalised(t *testing.T) {
	locales := mustLoadAllLocaleMessages(t)
	for language, messages := range locales {
		for _, key := range []string{"auth.code_file_title", "auth.code_file_footer"} {
			if strings.TrimSpace(messages[key]) == "" {
				t.Errorf("%s %s is missing", language, key)
			}
		}
	}
}
