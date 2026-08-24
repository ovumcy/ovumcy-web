package services

import (
	"context"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/ovumcy/ovumcy-web/internal/i18n"
	"github.com/ovumcy/ovumcy-web/internal/models"
)

// localeCopyProvider is the test twin of the production i18n adapter
// (internal/bootstrap): it answers both seams — the medical-safety disclaimer
// and any catalogue key — out of the REAL locale files, so these guards fail
// both when a surface resolves at the wrong language and when the key it names
// is missing from a catalogue.
//
// internal/i18n is imported by this TEST only; the services layer keeps no
// dependency on it (the same arrangement i18n_policy_test.go documents).
type localeCopyProvider struct{ manager *i18n.Manager }

func newLocaleCopyProvider(t *testing.T) localeCopyProvider {
	t.Helper()
	manager, err := i18n.NewManager(i18n.LangEN)
	if err != nil {
		t.Fatalf("init i18n manager: %v", err)
	}
	return localeCopyProvider{manager: manager}
}

func (provider localeCopyProvider) Disclaimer(language string) string {
	return provider.Message(language, "medical.disclaimer")
}

func (provider localeCopyProvider) Message(language string, key string) string {
	return provider.manager.Messages(language)[key]
}

// titleOnlyCopyProvider answers the reminder TITLE keys and nothing else, which
// is the shape of a provider whose catalogue lost the sentence entry. It exists
// because reminderCopy's empty-template arm is otherwise unreachable from a
// catalogue-backed provider: the sweep above forbids a blank sentence in any
// shipped locale, so the branch that keeps a missing one from rendering as
// formatting residue has no locale that can exercise it.
type titleOnlyCopyProvider struct{}

func (titleOnlyCopyProvider) Disclaimer(string) string { return "" }

func (titleOnlyCopyProvider) Message(_ string, key string) string {
	if strings.HasSuffix(key, ".title") {
		return "Reminder"
	}
	return ""
}

// TestReminderCopyWithoutASentenceSendsTheHeadlineAlone pins the arm that keeps
// a missing catalogue sentence from reaching a webhook consumer as Sprintf
// residue. Without it, reminderCopy would format the empty template and the
// payload body would read "%!(EXTRA string=2026-02-10)".
func TestReminderCopyWithoutASentenceSendsTheHeadlineAlone(t *testing.T) {
	service := NewWebhookNotifyService(nil, nil, nil, nil, titleOnlyCopyProvider{})
	eventDate := time.Date(2026, time.February, 10, 0, 0, 0, 0, time.UTC)

	for _, reminderType := range []string{DueReminderTypePeriod, DueReminderTypeOvulation} {
		title, message := service.reminderCopy(DueReminder{Type: reminderType, EventDate: eventDate}, "en")
		if title != "Reminder" {
			t.Errorf("%s title = %q, want the headline the provider does carry", reminderType, title)
		}
		if message != "" {
			t.Errorf("%s message = %q, want it empty rather than formatted from a missing template", reminderType, message)
		}
	}
}

func (provider localeCopyProvider) messages(t *testing.T, language string) map[string]string {
	t.Helper()
	messages := provider.manager.Messages(language)
	if len(messages) == 0 {
		t.Fatalf("locale %q carries no messages", language)
	}
	return messages
}

// TestNotifyPayloadIsLocalizedToTheOwnersInterfaceLanguage is the R2-0135 /
// R2-0109 regression on the webhook half: the owner's persisted
// users.interface_language is the durable carrier of the language they chose, so
// the notify pass must resolve BOTH payload fields at it — the disclaimer AND
// the reminder headline/sentence, which live in the locale catalogue rather than
// as English literals in Go. Resolving the disclaimer at the server default (the
// empty language) hands an owner on a non-default locale a mandatory safety
// string in a language they never chose, next to an English body.
func TestNotifyPayloadIsLocalizedToTheOwnersInterfaceLanguage(t *testing.T) {
	now := time.Date(2026, 3, 12, 9, 0, 0, 0, time.UTC)
	record := ovulationDueRecord(1, "https://a.example/hook", now)
	record.InterfaceLanguage = i18n.LangRU

	repo := &stubNotifyRepo{records: []models.WebhookNotifyRecord{record}}
	logs := stubLogReader{byUser: map[uint][]models.DailyLog{1: completedCycleStartLogs(1, *record.LastPeriodStart)}}
	deliverer := &stubDeliverer{}
	provider := newLocaleCopyProvider(t)
	service := NewWebhookNotifyService(repo, logs, stubDecryptor{}, deliverer, provider)

	report, err := service.RunOnce(context.Background(), now, time.UTC, false)
	if err != nil {
		t.Fatalf("RunOnce: %v", err)
	}
	deliveries := deliverer.deliveries()
	if len(deliveries) == 0 {
		t.Fatalf("expected at least one delivery (sent=%d, due=%d)", report.Sent, report.Due)
	}

	russian := provider.messages(t, i18n.LangRU)
	english := provider.messages(t, i18n.LangEN)

	for _, delivery := range deliveries {
		payload := delivery.payload

		if payload.Disclaimer != russian["medical.disclaimer"] {
			t.Errorf("disclaimer for a ru owner = %q, want the ru catalogue entry %q", payload.Disclaimer, russian["medical.disclaimer"])
		}
		if payload.Disclaimer == english["medical.disclaimer"] {
			t.Errorf("disclaimer for a ru owner resolved at the server default language: %q", payload.Disclaimer)
		}

		titleKey, messageKey := "webhook.reminder.period.title", "webhook.reminder.period.message"
		if payload.Type == DueReminderTypeOvulation {
			titleKey, messageKey = "webhook.reminder.ovulation.title", "webhook.reminder.ovulation.message"
		}
		wantTitle := russian[titleKey]
		if wantTitle == "" {
			t.Fatalf("locale ru has no entry for %q: the reminder copy is not in the catalogue", titleKey)
		}
		wantMessage := fmt.Sprintf(russian[messageKey], payload.EventDate)
		if payload.Title != wantTitle {
			t.Errorf("%s title = %q, want the ru catalogue entry %q", payload.Type, payload.Title, wantTitle)
		}
		if payload.Message != wantMessage {
			t.Errorf("%s message = %q, want the ru catalogue entry %q", payload.Type, payload.Message, wantMessage)
		}
	}
}

// TestNotifyPayloadCopyResolvesInEveryLocale keeps the four reminder-copy keys
// inside the six-file locale contract: the notify pass renders them for whatever
// language the owner chose, so a key present in one catalogue and absent from
// another would send that owner an empty title or an unrendered sentence.
func TestNotifyPayloadCopyResolvesInEveryLocale(t *testing.T) {
	provider := newLocaleCopyProvider(t)
	keys := []string{
		"webhook.reminder.period.title",
		"webhook.reminder.period.message",
		"webhook.reminder.ovulation.title",
		"webhook.reminder.ovulation.message",
	}
	for _, language := range provider.manager.SupportedLanguages() {
		messages := provider.messages(t, language)
		for _, key := range keys {
			value := messages[key]
			if value == "" {
				t.Errorf("locale %q has no entry for %q: the reminder would go out empty", language, key)
				continue
			}
			// Exactly one verb, not merely one present: reminderCopy passes a
			// single date, so a second %%s renders as %%!s(MISSING) in a payload
			// that has already left the instance. Both directions are one
			// translator edit away and neither is visible from the catalogue.
			if verbs := strings.Count(value, "%s"); strings.HasSuffix(key, ".message") && verbs != 1 {
				t.Errorf("locale %q entry %q = %q: the reminder sentence must carry exactly one %%s date placeholder, found %d", language, key, value, verbs)
			}
		}
	}
}

// TestCalendarFeedDisclaimerIsLocalizedToTheOwnersInterfaceLanguage is the
// R2-0135 regression on the .ics half. The feed is the second surface that
// carries predictions off the instance, and it repeated the webhook pass's stale
// justification verbatim ("no per-owner language is persisted"), so the fix has
// to reach both or the next egress surface inherits the wrong reason.
func TestCalendarFeedDisclaimerIsLocalizedToTheOwnersInterfaceLanguage(t *testing.T) {
	user, token := armedFeedUser(t, 42, "2026-03-02")
	user.InterfaceLanguage = i18n.LangRU

	users := &stubFeedUserStore{selector: user.CalendarFeedSelector, user: user}
	days := &stubFeedDayReader{logs: predictableFeedLogs(t)}
	provider := newLocaleCopyProvider(t)
	service := NewCalendarFeedService(users, days, provider, []byte(calendarFeedTestSecretKey))

	body, ok, err := service.ResolveFeed(context.Background(), token, mustParseDashboardDay(t, "2026-03-20"), time.UTC)
	if err != nil {
		t.Fatalf("ResolveFeed: %v", err)
	}
	if !ok {
		t.Fatal("expected the armed feed to resolve")
	}

	// RFC 5545 folds a content line at 75 octets; unfold before matching so a
	// long localized sentence is compared as one string.
	text := strings.ReplaceAll(string(body), "\r\n ", "")

	// Comma-free fragments: escapeICSText backslash-escapes a comma, so the
	// assertion stays on the part of each sentence the escaping never touches.
	if !strings.Contains(text, "медицинский совет") {
		t.Errorf("the .ics body does not carry the ru medical disclaimer:\n%s", text)
	}
	if strings.Contains(text, "not medical advice") {
		t.Errorf("the .ics body carries the server-default (English) disclaimer for a ru owner:\n%s", text)
	}
}
