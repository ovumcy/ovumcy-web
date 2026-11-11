package db

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/ovumcy/ovumcy-web/internal/models"
)

func openCycleDayReminderReposForTest(t *testing.T) (*UserRepository, *CycleDayReminderRepository) {
	t.Helper()
	users := openWebhookRepoForTest(t)
	return users, NewCycleDayReminderRepository(users.database)
}

func cycleDayReminderInputForTest(day int, hour int) models.CycleDayReminderInput {
	return models.CycleDayReminderInput{CycleDay: day, RemindAtHour: hour, Label: "vitamin", SendLabel: false}
}

func createCycleDayRuleForTest(t *testing.T, rules *CycleDayReminderRepository, userID uint, day int, hour int) models.CycleDayReminder {
	t.Helper()
	ctx := context.Background()
	if err := rules.Create(ctx, userID, cycleDayReminderInputForTest(day, hour)); err != nil {
		t.Fatalf("create rule day=%d hour=%d: %v", day, hour, err)
	}
	listed, err := rules.ListByUser(ctx, userID)
	if err != nil {
		t.Fatalf("list rules: %v", err)
	}
	for _, rule := range listed {
		if rule.CycleDay == day && rule.RemindAtHour == hour {
			return rule
		}
	}
	t.Fatalf("created rule day=%d hour=%d not listed", day, hour)
	return models.CycleDayReminder{}
}

func enableWebhookForCycleDayTest(t *testing.T, users *UserRepository, userID uint) int {
	t.Helper()
	// The pinned save is refused once a rule write moved the epoch, so delivery is
	// switched on directly: the claim only reads the flag and the epoch.
	if err := users.database.Model(&models.User{}).Where("id = ?", userID).
		Updates(map[string]any{"webhook_enabled": true, "webhook_url": "sealed-endpoint"}).Error; err != nil {
		t.Fatalf("enable webhook: %v", err)
	}
	return reloadUserForWebhook(t, users, userID).WebhookConfigVersion
}

func TestCycleDayReminderWritesAdvanceTheWebhookConfigVersion(t *testing.T) {
	users, rules := openCycleDayReminderReposForTest(t)
	user := createUserForTimezoneTest(t, users, "cdr-epoch@example.com")
	ctx := context.Background()

	before := reloadUserForWebhook(t, users, user.ID).WebhookConfigVersion
	rule := createCycleDayRuleForTest(t, rules, user.ID, 5, 9)
	afterCreate := reloadUserForWebhook(t, users, user.ID).WebhookConfigVersion
	if afterCreate <= before {
		t.Fatalf("Create must advance webhook_config_version: %d -> %d", before, afterCreate)
	}

	if err := rules.Update(ctx, user.ID, rule.ID, cycleDayReminderInputForTest(6, 10)); err != nil {
		t.Fatalf("Update: %v", err)
	}
	afterUpdate := reloadUserForWebhook(t, users, user.ID).WebhookConfigVersion
	if afterUpdate <= afterCreate {
		t.Fatalf("Update must advance webhook_config_version: %d -> %d", afterCreate, afterUpdate)
	}

	if err := rules.Delete(ctx, user.ID, rule.ID); err != nil {
		t.Fatalf("Delete: %v", err)
	}
	afterDelete := reloadUserForWebhook(t, users, user.ID).WebhookConfigVersion
	if afterDelete <= afterUpdate {
		t.Fatalf("Delete must advance webhook_config_version: %d -> %d", afterUpdate, afterDelete)
	}
}

func TestCycleDayReminderRefusesAnotherOwnersRule(t *testing.T) {
	users, rules := openCycleDayReminderReposForTest(t)
	owner := createUserForTimezoneTest(t, users, "cdr-owner@example.com")
	other := createUserForTimezoneTest(t, users, "cdr-other@example.com")
	ctx := context.Background()
	foreign := createCycleDayRuleForTest(t, rules, other.ID, 7, 8)

	if err := rules.Update(ctx, owner.ID, foreign.ID, cycleDayReminderInputForTest(20, 20)); !errors.Is(err, models.ErrCycleDayReminderNotFound) {
		t.Fatalf("Update of another owner's rule: want ErrCycleDayReminderNotFound, got %v", err)
	}
	if err := rules.Delete(ctx, owner.ID, foreign.ID); !errors.Is(err, models.ErrCycleDayReminderNotFound) {
		t.Fatalf("Delete of another owner's rule: want ErrCycleDayReminderNotFound, got %v", err)
	}
	if err := rules.Delete(ctx, owner.ID, foreign.ID+1000); !errors.Is(err, models.ErrCycleDayReminderNotFound) {
		t.Fatalf("Delete of a never-existing rule: want ErrCycleDayReminderNotFound, got %v", err)
	}

	listed, err := rules.ListByUser(ctx, other.ID)
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	if len(listed) != 1 || listed[0].ID != foreign.ID || listed[0].CycleDay != 7 || listed[0].RemindAtHour != 8 || listed[0].Label != "vitamin" {
		t.Fatalf("the other owner's rule must be untouched, got %+v", listed)
	}
	if mine, err := rules.ListByUser(ctx, owner.ID); err != nil || len(mine) != 0 {
		t.Fatalf("the owner must see none of the other owner's rules, got %+v err=%v", mine, err)
	}
}

func TestCycleDayReminderRefusesAZeroOwner(t *testing.T) {
	_, rules := openCycleDayReminderReposForTest(t)
	ctx := context.Background()
	day := time.Date(2026, time.March, 7, 0, 0, 0, 0, time.UTC)

	if _, err := rules.ListByUser(ctx, 0); !errors.Is(err, ErrUserOwnerRequired) {
		t.Fatalf("ListByUser(0): %v", err)
	}
	if err := rules.Create(ctx, 0, cycleDayReminderInputForTest(5, 9)); !errors.Is(err, ErrUserOwnerRequired) {
		t.Fatalf("Create(0): %v", err)
	}
	if err := rules.Update(ctx, 0, 1, cycleDayReminderInputForTest(5, 9)); !errors.Is(err, ErrUserOwnerRequired) {
		t.Fatalf("Update(0): %v", err)
	}
	if err := rules.Delete(ctx, 0, 1); !errors.Is(err, ErrUserOwnerRequired) {
		t.Fatalf("Delete(0): %v", err)
	}
	if _, err := rules.ClaimSend(ctx, 0, 1, day, nil, 0); !errors.Is(err, ErrUserOwnerRequired) {
		t.Fatalf("ClaimSend(0): %v", err)
	}
	if err := rules.ReleaseSend(ctx, 0, 1, day, nil); !errors.Is(err, ErrUserOwnerRequired) {
		t.Fatalf("ReleaseSend(0): %v", err)
	}
}

func TestCycleDayReminderCreateCapsAtTenAndRefusesDuplicates(t *testing.T) {
	users, rules := openCycleDayReminderReposForTest(t)
	user := createUserForTimezoneTest(t, users, "cdr-cap@example.com")
	ctx := context.Background()

	createCycleDayRuleForTest(t, rules, user.ID, 1, 9)
	err := rules.Create(ctx, user.ID, cycleDayReminderInputForTest(1, 9))
	var unique *UniqueConstraintError
	if !errors.As(err, &unique) {
		t.Fatalf("a duplicate (user, day, hour) must be a UniqueConstraintError, got %v", err)
	}

	for day := 2; day <= models.CycleDayReminderMaxPerOwner; day++ {
		createCycleDayRuleForTest(t, rules, user.ID, day, 9)
	}
	if err := rules.Create(ctx, user.ID, cycleDayReminderInputForTest(50, 9)); !errors.Is(err, models.ErrCycleDayReminderLimit) {
		t.Fatalf("the eleventh rule must be refused with ErrCycleDayReminderLimit, got %v", err)
	}
	listed, err := rules.ListByUser(ctx, user.ID)
	if err != nil || len(listed) != models.CycleDayReminderMaxPerOwner {
		t.Fatalf("expected exactly %d rules, got %d (err=%v)", models.CycleDayReminderMaxPerOwner, len(listed), err)
	}

	// The cap is per owner.
	other := createUserForTimezoneTest(t, users, "cdr-cap-other@example.com")
	createCycleDayRuleForTest(t, rules, other.ID, 50, 9)
}

func TestCycleDayReminderClaimSendIsACompareAndSet(t *testing.T) {
	users, rules := openCycleDayReminderReposForTest(t)
	user := createUserForTimezoneTest(t, users, "cdr-claim@example.com")
	ctx := context.Background()
	rule := createCycleDayRuleForTest(t, rules, user.ID, 10, 9)
	version := enableWebhookForCycleDayTest(t, users, user.ID)
	day := time.Date(2026, time.March, 7, 0, 0, 0, 0, time.UTC)

	won, err := rules.ClaimSend(ctx, user.ID, rule.ID, day, nil, version)
	if err != nil || !won {
		t.Fatalf("the first claim must win, won=%t err=%v", won, err)
	}
	again, err := rules.ClaimSend(ctx, user.ID, rule.ID, day, nil, version)
	if err != nil || again {
		t.Fatalf("a second claim against the same previous must lose, won=%t err=%v", again, err)
	}

	// ReleaseSend restores previous (NULL here), so the claim can be taken again.
	if err := rules.ReleaseSend(ctx, user.ID, rule.ID, day, nil); err != nil {
		t.Fatalf("ReleaseSend: %v", err)
	}
	listed, _ := rules.ListByUser(ctx, user.ID)
	if len(listed) != 1 || listed[0].LastSentOn != nil {
		t.Fatalf("release must restore last_sent_on to NULL, got %+v", listed)
	}

	// A non-NULL previous: claim day1, then claim day2 against day1, release back.
	if won, err := rules.ClaimSend(ctx, user.ID, rule.ID, day, nil, version); err != nil || !won {
		t.Fatalf("re-claim after release: won=%t err=%v", won, err)
	}
	next := day.AddDate(0, 0, 28)
	if won, err := rules.ClaimSend(ctx, user.ID, rule.ID, next, &day, version); err != nil || !won {
		t.Fatalf("a claim against the stored previous must win: won=%t err=%v", won, err)
	}
	if err := rules.ReleaseSend(ctx, user.ID, rule.ID, next, &day); err != nil {
		t.Fatalf("ReleaseSend to a date: %v", err)
	}
	listed, _ = rules.ListByUser(ctx, user.ID)
	if len(listed) != 1 || listed[0].LastSentOn == nil || listed[0].LastSentOn.Format("2006-01-02") != "2026-03-07" {
		t.Fatalf("release must restore last_sent_on to the previous date, got %+v", listed)
	}
}

func TestCycleDayReminderClaimSendLosesAfterTheConfigurationMoved(t *testing.T) {
	users, rules := openCycleDayReminderReposForTest(t)
	user := createUserForTimezoneTest(t, users, "cdr-claim-epoch@example.com")
	ctx := context.Background()
	rule := createCycleDayRuleForTest(t, rules, user.ID, 10, 9)
	version := enableWebhookForCycleDayTest(t, users, user.ID)
	day := time.Date(2026, time.March, 7, 0, 0, 0, 0, time.UTC)

	// Another rule created after the pass read the configuration moves the epoch.
	createCycleDayRuleForTest(t, rules, user.ID, 11, 9)
	if won, err := rules.ClaimSend(ctx, user.ID, rule.ID, day, nil, version); err != nil || won {
		t.Fatalf("a claim pinned to the pre-create epoch must lose, won=%t err=%v", won, err)
	}
	current := reloadUserForWebhook(t, users, user.ID).WebhookConfigVersion
	if won, err := rules.ClaimSend(ctx, user.ID, rule.ID, day, nil, current); err != nil || !won {
		t.Fatalf("control: a claim pinned to the current epoch wins, won=%t err=%v", won, err)
	}

	// Delivery disabled: the claim loses even at the stored epoch.
	disabled := createUserForTimezoneTest(t, users, "cdr-claim-disabled@example.com")
	disabledRule := createCycleDayRuleForTest(t, rules, disabled.ID, 10, 9)
	disabledVersion := reloadUserForWebhook(t, users, disabled.ID).WebhookConfigVersion
	if reloadUserForWebhook(t, users, disabled.ID).WebhookEnabled {
		t.Fatal("precondition: a fresh owner has webhook_enabled=false")
	}
	if won, err := rules.ClaimSend(ctx, disabled.ID, disabledRule.ID, day, nil, disabledVersion); err != nil || won {
		t.Fatalf("a claim with delivery disabled must lose, won=%t err=%v", won, err)
	}

	// Another owner's rule cannot be claimed under this owner's id.
	if won, err := rules.ClaimSend(ctx, user.ID, disabledRule.ID, day, nil, current); err != nil || won {
		t.Fatalf("a claim on another owner's rule must lose, won=%t err=%v", won, err)
	}
}

func TestClearAllDataDeletesTheCycleDayReminders(t *testing.T) {
	users, rules := openCycleDayReminderReposForTest(t)
	user := createUserForTimezoneTest(t, users, "cdr-clear@example.com")
	other := createUserForTimezoneTest(t, users, "cdr-clear-other@example.com")
	ctx := context.Background()
	createCycleDayRuleForTest(t, rules, user.ID, 3, 9)
	createCycleDayRuleForTest(t, rules, user.ID, 4, 9)
	createCycleDayRuleForTest(t, rules, other.ID, 3, 9)

	if err := users.ClearAllDataAndResetSettings(ctx, user.ID, storedSessionVersionForTest(t, users, user.ID)); err != nil {
		t.Fatalf("ClearAllDataAndResetSettings: %v", err)
	}
	if listed, err := rules.ListByUser(ctx, user.ID); err != nil || len(listed) != 0 {
		t.Fatalf("clear-data must delete the owner's rules, got %+v err=%v", listed, err)
	}
	if listed, err := rules.ListByUser(ctx, other.ID); err != nil || len(listed) != 1 {
		t.Fatalf("clear-data must leave the other owner's rule, got %+v err=%v", listed, err)
	}
}
