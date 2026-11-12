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
	return findCycleDayRuleForTest(t, rules, userID, day, hour)
}

func findCycleDayRuleForTest(t *testing.T, rules *CycleDayReminderRepository, userID uint, day int, hour int) models.CycleDayReminder {
	t.Helper()
	listed, err := rules.ListByUser(context.Background(), userID)
	if err != nil {
		t.Fatalf("list rules: %v", err)
	}
	for _, rule := range listed {
		if rule.CycleDay == day && rule.RemindAtHour == hour {
			return rule
		}
	}
	t.Fatalf("rule day=%d hour=%d not listed", day, hour)
	return models.CycleDayReminder{}
}

func enableWebhookForCycleDayTest(t *testing.T, users *UserRepository, userID uint) int {
	t.Helper()
	// Delivery is switched on directly: the claim only reads the flag and the epoch.
	if err := users.database.Model(&models.User{}).Where("id = ?", userID).
		Updates(map[string]any{"webhook_enabled": true, "webhook_url": "sealed-endpoint"}).Error; err != nil {
		t.Fatalf("enable webhook: %v", err)
	}
	return reloadUserForWebhook(t, users, userID).WebhookConfigVersion
}

func TestCycleDayReminderWritesDoNotAdvanceTheWebhookConfigVersion(t *testing.T) {
	users, rules := openCycleDayReminderReposForTest(t)
	user := createUserForTimezoneTest(t, users, "cdr-epoch@example.com")
	ctx := context.Background()

	before := reloadUserForWebhook(t, users, user.ID).WebhookConfigVersion
	rule := createCycleDayRuleForTest(t, rules, user.ID, 5, 9)
	if err := rules.Update(ctx, user.ID, rule.ID, cycleDayReminderInputForTest(6, 10)); err != nil {
		t.Fatalf("Update: %v", err)
	}
	if err := rules.Delete(ctx, user.ID, rule.ID); err != nil {
		t.Fatalf("Delete: %v", err)
	}
	// A rule write moving the epoch would void a running pass's period and
	// ovulation claims; the rule's own claim is pinned to the rule instead.
	if after := reloadUserForWebhook(t, users, user.ID).WebhookConfigVersion; after != before {
		t.Fatalf("rule writes must not advance webhook_config_version: %d -> %d", before, after)
	}
}

func TestCycleDayReminderUpdateResetsLastSentOnOnlyWhenTheDayMoves(t *testing.T) {
	users, rules := openCycleDayReminderReposForTest(t)
	user := createUserForTimezoneTest(t, users, "cdr-reset@example.com")
	ctx := context.Background()
	rule := createCycleDayRuleForTest(t, rules, user.ID, 10, 9)
	version := enableWebhookForCycleDayTest(t, users, user.ID)
	day := time.Date(2026, time.March, 7, 0, 0, 0, 0, time.UTC)
	if won, err := rules.ClaimSend(ctx, user.ID, rule, day, nil, version); err != nil || !won {
		t.Fatalf("claim: won=%t err=%v", won, err)
	}

	// Hour, label and opt-in edits keep the send: today's edit must not re-arm it.
	sameDay := models.CycleDayReminderInput{CycleDay: 10, RemindAtHour: 20, Label: "iron", SendLabel: true}
	if err := rules.Update(ctx, user.ID, rule.ID, sameDay); err != nil {
		t.Fatalf("Update on the same day: %v", err)
	}
	kept := findCycleDayRuleForTest(t, rules, user.ID, 10, 20)
	if kept.LastSentOn == nil || kept.LastSentOn.Format("2006-01-02") != "2026-03-07" {
		t.Fatalf("an edit that keeps the cycle day must keep last_sent_on, got %+v", kept)
	}

	// A rule moved to another day is reset, or the old day's send silences it.
	if err := rules.Update(ctx, user.ID, rule.ID, cycleDayReminderInputForTest(12, 20)); err != nil {
		t.Fatalf("Update to another day: %v", err)
	}
	if moved := findCycleDayRuleForTest(t, rules, user.ID, 12, 20); moved.LastSentOn != nil {
		t.Fatalf("a rule moved to another cycle day must reset last_sent_on, got %+v", moved)
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
	if _, err := rules.ClaimSend(ctx, 0, models.CycleDayReminder{ID: 1}, day, nil, 0); !errors.Is(err, ErrUserOwnerRequired) {
		t.Fatalf("ClaimSend(0): %v", err)
	}
	if err := rules.ReleaseSend(ctx, 0, 1, day, nil); !errors.Is(err, ErrUserOwnerRequired) {
		t.Fatalf("ReleaseSend(0): %v", err)
	}
}

func TestCycleDayReminderCreateRefusesAnAbsentOwner(t *testing.T) {
	_, rules := openCycleDayReminderReposForTest(t)
	if err := rules.Create(context.Background(), 999999, cycleDayReminderInputForTest(5, 9)); !errors.Is(err, ErrUserOwnerRequired) {
		t.Fatalf("Create for an absent owner row: want ErrUserOwnerRequired, got %v", err)
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

	won, err := rules.ClaimSend(ctx, user.ID, rule, day, nil, version)
	if err != nil || !won {
		t.Fatalf("the first claim must win, won=%t err=%v", won, err)
	}
	again, err := rules.ClaimSend(ctx, user.ID, rule, day, nil, version)
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
	if won, err := rules.ClaimSend(ctx, user.ID, rule, day, nil, version); err != nil || !won {
		t.Fatalf("re-claim after release: won=%t err=%v", won, err)
	}
	next := day.AddDate(0, 0, 28)
	if won, err := rules.ClaimSend(ctx, user.ID, rule, next, &day, version); err != nil || !won {
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

	// The rule edited after the pass read it: the stale snapshot loses.
	edited := models.CycleDayReminderInput{CycleDay: 10, RemindAtHour: 9, Label: "vitamin", SendLabel: true}
	if err := rules.Update(ctx, user.ID, rule.ID, edited); err != nil {
		t.Fatalf("Update: %v", err)
	}
	if won, err := rules.ClaimSend(ctx, user.ID, rule, day, nil, version); err != nil || won {
		t.Fatalf("a claim pinned to the pre-edit rule must lose, won=%t err=%v", won, err)
	}

	// An unrelated rule created meanwhile voids nothing: the fresh snapshot wins
	// at the same epoch.
	createCycleDayRuleForTest(t, rules, user.ID, 11, 9)
	current := findCycleDayRuleForTest(t, rules, user.ID, 10, 9)
	if won, err := rules.ClaimSend(ctx, user.ID, current, day, nil, version); err != nil || !won {
		t.Fatalf("an unrelated create must not void the claim, won=%t err=%v", won, err)
	}
	if err := rules.ReleaseSend(ctx, user.ID, current.ID, day, nil); err != nil {
		t.Fatalf("ReleaseSend: %v", err)
	}

	// A delivery-settings write advances the epoch: the claim pinned before it loses.
	if err := users.UpdateReminderLeadDays(ctx, user.ID, 0); err != nil {
		t.Fatalf("UpdateReminderLeadDays: %v", err)
	}
	if won, err := rules.ClaimSend(ctx, user.ID, current, day, nil, version); err != nil || won {
		t.Fatalf("a claim pinned to the pre-settings-write epoch must lose, won=%t err=%v", won, err)
	}
	advanced := reloadUserForWebhook(t, users, user.ID).WebhookConfigVersion

	// A removed rule matches no row.
	if err := rules.Delete(ctx, user.ID, current.ID); err != nil {
		t.Fatalf("Delete: %v", err)
	}
	if won, err := rules.ClaimSend(ctx, user.ID, current, day, nil, advanced); err != nil || won {
		t.Fatalf("a claim on a removed rule must lose, won=%t err=%v", won, err)
	}

	// Delivery disabled: the claim loses even at the stored epoch.
	disabled := createUserForTimezoneTest(t, users, "cdr-claim-disabled@example.com")
	disabledRule := createCycleDayRuleForTest(t, rules, disabled.ID, 10, 9)
	disabledVersion := reloadUserForWebhook(t, users, disabled.ID).WebhookConfigVersion
	if reloadUserForWebhook(t, users, disabled.ID).WebhookEnabled {
		t.Fatal("precondition: a fresh owner has webhook_enabled=false")
	}
	if won, err := rules.ClaimSend(ctx, disabled.ID, disabledRule, day, nil, disabledVersion); err != nil || won {
		t.Fatalf("a claim with delivery disabled must lose, won=%t err=%v", won, err)
	}

	// Another owner's rule cannot be claimed under this owner's id.
	if won, err := rules.ClaimSend(ctx, user.ID, disabledRule, day, nil, advanced); err != nil || won {
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

func TestCycleDayReminderRefusesAZeroRuleID(t *testing.T) {
	users, rules := openCycleDayReminderReposForTest(t)
	user := createUserForTimezoneTest(t, users, "cdr-zero-rule@example.com")
	ctx := context.Background()
	day := time.Date(2026, time.March, 7, 0, 0, 0, 0, time.UTC)

	if err := rules.Update(ctx, user.ID, 0, cycleDayReminderInputForTest(5, 9)); !errors.Is(err, models.ErrCycleDayReminderNotFound) {
		t.Fatalf("Update(rule 0): want ErrCycleDayReminderNotFound, got %v", err)
	}
	if err := rules.Delete(ctx, user.ID, 0); !errors.Is(err, models.ErrCycleDayReminderNotFound) {
		t.Fatalf("Delete(rule 0): want ErrCycleDayReminderNotFound, got %v", err)
	}
	if won, err := rules.ClaimSend(ctx, user.ID, models.CycleDayReminder{}, day, nil, 0); won || !errors.Is(err, models.ErrCycleDayReminderNotFound) {
		t.Fatalf("ClaimSend(rule 0): want a lost claim with ErrCycleDayReminderNotFound, got won=%t err=%v", won, err)
	}
}

// refuseCycleDayReminderStatement makes every event (UPDATE or DELETE) on a
// cycle_day_reminders row fail, so a statement error is not mistaken for a
// zero-row outcome.
func refuseCycleDayReminderStatement(t *testing.T, users *UserRepository, event string) {
	t.Helper()
	if err := users.database.Exec(`CREATE TRIGGER refuse_cycle_day_reminder_` + event + ` BEFORE ` + event +
		` ON cycle_day_reminders BEGIN SELECT RAISE(ABORT, 'cycle-day reminder ` + event + ` refused'); END`).Error; err != nil {
		t.Fatalf("create the %s trigger: %v", event, err)
	}
}

func TestCycleDayReminderWritesReturnStatementErrors(t *testing.T) {
	users, rules := openCycleDayReminderReposForTest(t)
	user := createUserForTimezoneTest(t, users, "cdr-statement-errors@example.com")
	ctx := context.Background()
	rule := createCycleDayRuleForTest(t, rules, user.ID, 5, 9)
	version := enableWebhookForCycleDayTest(t, users, user.ID)
	day := time.Date(2026, time.March, 7, 0, 0, 0, 0, time.UTC)

	refuseCycleDayReminderStatement(t, users, "UPDATE")
	err := rules.Update(ctx, user.ID, rule.ID, cycleDayReminderInputForTest(6, 9))
	var unique *UniqueConstraintError
	if err == nil || errors.As(err, &unique) || errors.Is(err, models.ErrCycleDayReminderNotFound) {
		t.Fatalf("a refused UPDATE must surface as the statement error, got %v", err)
	}
	if won, err := rules.ClaimSend(ctx, user.ID, rule, day, nil, version); won || err == nil {
		t.Fatalf("a refused claim UPDATE must surface as an error, got won=%t err=%v", won, err)
	}

	refuseCycleDayReminderStatement(t, users, "DELETE")
	if err := rules.Delete(ctx, user.ID, rule.ID); err == nil || errors.Is(err, models.ErrCycleDayReminderNotFound) {
		t.Fatalf("a refused DELETE must surface as the statement error, got %v", err)
	}
	if err := users.ClearAllDataAndResetSettings(ctx, user.ID, storedSessionVersionForTest(t, users, user.ID)); err == nil {
		t.Fatal("clear-data must fail when the rules cannot be deleted")
	}
	if err := users.DeleteAccountAndRelatedData(ctx, user.ID); err == nil {
		t.Fatal("account erasure must fail when the rules cannot be deleted")
	}
	if listed := findCycleDayRuleForTest(t, rules, user.ID, 5, 9); listed.ID != rule.ID {
		t.Fatalf("every refused write must leave the rule in place, got %+v", listed)
	}
}

func TestCycleDayReminderReadsReturnQueryErrors(t *testing.T) {
	users, rules := openCycleDayReminderReposForTest(t)
	user := createUserForTimezoneTest(t, users, "cdr-query-errors@example.com")
	ctx := context.Background()

	if err := users.database.Exec(`ALTER TABLE cycle_day_reminders RENAME TO cycle_day_reminders_gone`).Error; err != nil {
		t.Fatalf("rename the rules table: %v", err)
	}
	if listed, err := rules.ListByUser(ctx, user.ID); err == nil || listed != nil {
		t.Fatalf("ListByUser over a missing table must fail, got %+v err=%v", listed, err)
	}
	// The owner lock passes; the per-owner count is the statement that fails.
	if err := rules.Create(ctx, user.ID, cycleDayReminderInputForTest(5, 9)); err == nil || errors.Is(err, ErrUserOwnerRequired) {
		t.Fatalf("Create must surface the count error, got %v", err)
	}

	if err := users.database.Exec(`ALTER TABLE users RENAME TO users_gone`).Error; err != nil {
		t.Fatalf("rename the users table: %v", err)
	}
	if err := rules.Create(ctx, user.ID, cycleDayReminderInputForTest(5, 9)); err == nil || errors.Is(err, ErrUserOwnerRequired) {
		t.Fatalf("Create must surface the owner-lock error, got %v", err)
	}
}
