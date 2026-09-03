package db

import (
	"context"
	"reflect"
	"testing"
	"time"

	"github.com/ovumcy/ovumcy-web/internal/models"
)

// Withdrawing a delivery endpoint is its own write, and the reason is what these
// guards are about: the shared save cannot express it. SaveWebhookSettings names
// reminder_lead_days and both per-kind opt-ins in every statement it issues, so
// a thin "remove" caller reaching for it writes a zero lead window — and a zero
// window makes a cycle anchor due on exactly one calendar day, with no later
// pass to retry it.

// TestRemoveWebhookDestinationLeavesTheKindsAndLeadWindowAlone is the assertion
// the shortcut would fail. Withdrawing an address is not a request to forget
// which reminders the owner wanted, nor how early they wanted them.
func TestRemoveWebhookDestinationLeavesTheKindsAndLeadWindowAlone(t *testing.T) {
	repo := openWebhookRepoForTest(t)
	user := createUserForTimezoneTest(t, repo, "wh-remove-keeps@example.com")
	ctx := context.Background()

	if err := repo.SaveWebhookSettings(ctx, user.ID, models.WebhookSettingsColumns{
		Enabled:          true,
		EncryptedURL:     "sealed-endpoint",
		NotifyPeriod:     true,
		NotifyOvulation:  true,
		ReminderLeadDays: 5,
	}); err != nil {
		t.Fatalf("seed webhook settings: %v", err)
	}

	if err := repo.RemoveWebhookDestination(ctx, user.ID); err != nil {
		t.Fatalf("RemoveWebhookDestination: %v", err)
	}

	after := reloadUserForWebhook(t, repo, user.ID)
	if after.WebhookURL != "" {
		t.Fatalf("expected the endpoint withdrawn, got %q", after.WebhookURL)
	}
	if after.WebhookEnabled {
		t.Fatal("expected delivery switched off with the endpoint")
	}
	if !after.WebhookNotifyPeriod || !after.WebhookNotifyOvulation {
		t.Fatal("withdrawing the endpoint forgot which reminders the owner had chosen")
	}
	if after.ReminderLeadDays != 5 {
		t.Fatalf("withdrawing the endpoint rewrote the shared lead window to %d; zero lead days make a reminder due for one calendar day with no retry", after.ReminderLeadDays)
	}
}

// TestRemoveWebhookDestinationAdvancesTheRevocationEpoch keeps the new writer
// inside the rule migration 038 states: every write to the delivery
// configuration advances webhook_config_version in the statement that performs
// it, so a pass already holding the previous snapshot loses its claim instead of
// posting to an endpoint the owner has just withdrawn.
func TestRemoveWebhookDestinationAdvancesTheRevocationEpoch(t *testing.T) {
	repo := openWebhookRepoForTest(t)
	user := createUserForTimezoneTest(t, repo, "wh-remove-epoch@example.com")
	ctx := context.Background()

	before := reloadUserForWebhook(t, repo, user.ID).WebhookConfigVersion
	if err := repo.RemoveWebhookDestination(ctx, user.ID); err != nil {
		t.Fatalf("RemoveWebhookDestination: %v", err)
	}
	after := reloadUserForWebhook(t, repo, user.ID).WebhookConfigVersion
	if after != before+1 {
		t.Fatalf("expected the revocation epoch to advance %d -> %d, got %d", before, before+1, after)
	}
}

// TestRemoveWebhookDestinationClearsTheDeliveryMark holds the rule that makes
// the mark renderable at all: it says a delivery to THAT endpoint was accepted,
// so it may not outlive the endpoint by a single read.
func TestRemoveWebhookDestinationClearsTheDeliveryMark(t *testing.T) {
	repo := openWebhookRepoForTest(t)
	user := createUserForTimezoneTest(t, repo, "wh-remove-mark@example.com")
	ctx := context.Background()

	if err := repo.SaveWebhookSettings(ctx, user.ID, models.WebhookSettingsColumns{
		Enabled:      true,
		EncryptedURL: "sealed-endpoint",
		NotifyPeriod: true,
	}); err != nil {
		t.Fatalf("seed webhook settings: %v", err)
	}
	epoch := reloadUserForWebhook(t, repo, user.ID).WebhookConfigVersion
	if err := repo.MarkWebhookDelivered(ctx, user.ID, time.Date(2026, 5, 2, 8, 0, 0, 0, time.UTC), epoch); err != nil {
		t.Fatalf("MarkWebhookDelivered: %v", err)
	}
	if markedDeliveryOf(t, repo, user.ID) == nil {
		t.Fatal("expected a delivery mark to seed the case; without it the assertion below is vacuous")
	}

	if err := repo.RemoveWebhookDestination(ctx, user.ID); err != nil {
		t.Fatalf("RemoveWebhookDestination: %v", err)
	}
	if mark := markedDeliveryOf(t, repo, user.ID); mark != nil {
		t.Fatalf("the delivery mark (%s) outlived the endpoint it was about", mark)
	}
}

// TestRemoveWebhookDestinationTouchesNoOtherOwnersRow is the isolation
// assertion. An instance hosts more than one independent owner, and a write
// whose predicate is wide enough to reach a second row is a privacy defect
// whatever it was meant to do.
func TestRemoveWebhookDestinationTouchesNoOtherOwnersRow(t *testing.T) {
	repo := openWebhookRepoForTest(t)
	withdrawn := createUserForTimezoneTest(t, repo, "wh-remove-mine@example.com")
	bystander := createUserForTimezoneTest(t, repo, "wh-remove-theirs@example.com")
	ctx := context.Background()

	for _, id := range []uint{withdrawn.ID, bystander.ID} {
		if err := repo.SaveWebhookSettings(ctx, id, models.WebhookSettingsColumns{
			Enabled:          true,
			EncryptedURL:     "sealed-endpoint",
			NotifyPeriod:     true,
			ReminderLeadDays: 3,
		}); err != nil {
			t.Fatalf("seed webhook settings for %d: %v", id, err)
		}
	}
	epochBefore := reloadUserForWebhook(t, repo, bystander.ID).WebhookConfigVersion

	if err := repo.RemoveWebhookDestination(ctx, withdrawn.ID); err != nil {
		t.Fatalf("RemoveWebhookDestination: %v", err)
	}

	untouched := reloadUserForWebhook(t, repo, bystander.ID)
	if untouched.WebhookURL != "sealed-endpoint" || !untouched.WebhookEnabled {
		t.Fatal("withdrawing one owner's endpoint reached another owner's row")
	}
	if untouched.WebhookConfigVersion != epochBefore {
		t.Fatalf("another owner's revocation epoch moved: %d -> %d", epochBefore, untouched.WebhookConfigVersion)
	}
}

// TestLoadSettingsProjectionSelectsExactlyTheseColumns is the boundary the
// egress ledger rests on.
//
// The ledger's honesty argument is that a claim watermark can never be read as a
// delivery time. Watermarks ARE reachable on the settings path — the row exists
// and a whole-row read would return them — so the guarantee is not that they are
// unreachable but that the ONE read this path takes does not select them, and
// that growing that read is a deliberate act rather than a quiet one.
//
// It is derived from the running projection rather than from a hand-written
// mirror of the Select list: every column is seeded non-zero, the projection is
// loaded, and the fields that come back non-zero ARE the column set. A mirror of
// the implementation would agree with it by construction.
func TestLoadSettingsProjectionSelectsExactlyTheseColumns(t *testing.T) {
	repo := openWebhookRepoForTest(t)
	user := createUserForTimezoneTest(t, repo, "settings-projection@example.com")
	ctx := context.Background()

	seedEveryProjectableColumn(t, repo, user.ID)

	loaded, err := repo.LoadSettingsByID(ctx, user.ID)
	if err != nil {
		t.Fatalf("LoadSettingsByID: %v", err)
	}

	// Anti-vacuity anchor, owned by this test rather than by the data it judges:
	// one field that must be selected and one that must not.
	if loaded.WebhookURL == "" {
		t.Fatal("the projection did not return webhook_url; the assertions below would then be measuring nothing")
	}
	for _, forbidden := range []string{"WebhookPeriodLastSentCycleStart", "WebhookOvulationLastSentCycleStart"} {
		field := reflect.ValueOf(loaded).FieldByName(forbidden)
		if !field.IsValid() {
			t.Fatalf("models.User no longer declares %s; this guard names the wrong field", forbidden)
		}
		if !field.IsZero() {
			t.Fatalf("the settings projection now selects %s: a claim watermark is written before a POST and would be read as a delivery time by anything that rendered it", forbidden)
		}
	}

	expected := map[string]struct{}{
		"CycleLength": {}, "PeriodLength": {}, "LutealPhase": {}, "AutoPeriodFill": {},
		"LocalAuthEnabled": {}, "IrregularCycle": {}, "TrackBBT": {}, "TemperatureUnit": {},
		"TrackCervicalMucus": {}, "HideSexChip": {}, "HideCycleFactors": {}, "HideNotesField": {},
		"ShowHistoricalPhases": {}, "WeekStartsOn": {}, "InterfaceLanguage": {}, "ShownPeriodTip": {},
		"AgeGroup": {}, "UsageGoal": {}, "UnpredictableCycle": {}, "LongPeriodWarningCycleStart": {},
		"LastPeriodStart": {}, "ReminderLeadDays": {}, "WebhookEnabled": {}, "WebhookURL": {},
		"WebhookNotifyPeriod": {}, "WebhookNotifyOvulation": {}, "WebhookLastDeliveredAt": {},
		"CalendarFeedKeyEpoch": {}, "CalendarFeedSelector": {}, "CalendarFeedRevealedAt": {},
	}

	value := reflect.ValueOf(loaded)
	structType := value.Type()
	for index := range structType.NumField() {
		name := structType.Field(index).Name
		if value.Field(index).IsZero() {
			if _, wanted := expected[name]; wanted {
				t.Fatalf("the settings projection stopped selecting %s", name)
			}
			continue
		}
		if _, wanted := expected[name]; !wanted {
			t.Fatalf("the settings projection now selects %s. Growing the one controlled settings read is allowed, but it is a deliberate act: add the column here and say why it is safe to render", name)
		}
	}
}

// seedEveryProjectableColumn writes a non-zero value into every column the
// settings projection could return plus the two it must not, so the load below
// separates "selected" from "absent" by observation.
func seedEveryProjectableColumn(t *testing.T, repo *UserRepository, userID uint) {
	t.Helper()

	stamp := time.Date(2026, 6, 1, 7, 0, 0, 0, time.UTC)
	period := time.Date(2026, 5, 20, 0, 0, 0, 0, time.UTC)
	updates := map[string]any{
		"cycle_length": 30, "period_length": 6, "luteal_phase": 13, "auto_period_fill": true,
		"local_auth_enabled": true, "irregular_cycle": true, "track_bbt": true,
		"temperature_unit": "f", "track_cervical_mucus": true, "hide_sex_chip": true,
		"hide_cycle_factors": true, "hide_notes_field": true, "show_historical_phases": true,
		"week_starts_on": "sunday", "interface_language": "de", "shown_period_tip": true,
		"age_group": "25_34", "usage_goal": "conceive", "unpredictable_cycle": true,
		"long_period_warning_cycle_start": period, "last_period_start": period,
		"reminder_lead_days": 4, "webhook_enabled": true, "webhook_url": "sealed-endpoint",
		"webhook_notify_period": true, "webhook_notify_ovulation": true,
		"webhook_last_delivered_at": stamp,
		"calendar_feed_selector":    "selector-value", "calendar_feed_revealed_at": stamp,
		"calendar_feed_key_epoch": "epoch-value",
		// The two that must not come back.
		"webhook_period_last_sent_cycle_start":    period,
		"webhook_ovulation_last_sent_cycle_start": period,
	}
	if err := repo.database.WithContext(context.Background()).Model(&models.User{}).Where("id = ?", userID).Updates(updates).Error; err != nil {
		t.Fatalf("seed every projectable column: %v", err)
	}
}

// TestSaveWebhookSettingsKeepingTheEndpointCannotArmIt makes the exclusivity
// structural rather than a rule a caller has to remember. An endpoint kept
// BECAUSE this instance could not read it is one delivery must never run
// against, and the service refuses that combination - but the service is not the
// only thing that can reach this method.
func TestSaveWebhookSettingsKeepingTheEndpointCannotArmIt(t *testing.T) {
	repo := openWebhookRepoForTest(t)
	user := createUserForTimezoneTest(t, repo, "wh-keep-cannot-arm@example.com")
	ctx := context.Background()

	if err := repo.SaveWebhookSettings(ctx, user.ID, models.WebhookSettingsColumns{
		Enabled:          true,
		EncryptedURL:     "sealed-endpoint",
		NotifyPeriod:     true,
		ReminderLeadDays: 3,
	}); err != nil {
		t.Fatalf("seed webhook settings: %v", err)
	}

	// A caller asking to keep the endpoint AND to arm delivery over it.
	if err := repo.SaveWebhookSettings(ctx, user.ID, models.WebhookSettingsColumns{
		Enabled:          true,
		KeepEncryptedURL: true,
		NotifyPeriod:     true,
		ReminderLeadDays: 3,
	}); err != nil {
		t.Fatalf("keeping save: %v", err)
	}

	after := reloadUserForWebhook(t, repo, user.ID)
	if after.WebhookEnabled {
		t.Fatal("a save that kept an unreadable endpoint armed delivery over it")
	}
	if after.WebhookURL != "sealed-endpoint" {
		t.Fatalf("the kept endpoint was rewritten to %q", after.WebhookURL)
	}
	if !after.WebhookNotifyPeriod {
		t.Fatal("the owner's reminder-kind choice did not survive the keeping save")
	}
}
