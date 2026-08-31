package db

import (
	"context"
	"testing"
	"time"

	"github.com/ovumcy/ovumcy-web/internal/models"
)

// Revocation half of the webhook watermark claim (finding PRIV-1 / SEC-01).
//
// The notify pass reads every owner's configuration once, at the start of the
// pass, and only claims each send minutes later, just before the POST. The
// watermark compare-and-set cannot see what happens in that window: a settings
// save deliberately leaves the watermarks alone, and a clear-data wipe NULLs
// them, which re-opens the claim's first-ever-send branch outright. So an owner
// could disable delivery, replace or remove the endpoint, or erase their data,
// be told it succeeded, and still have a pass already in flight POST their
// predicted health data to the endpoint they had just revoked.
//
// webhook_config_version is the monotonic revocation epoch that closes it. Every
// write to an owner's webhook configuration advances it in the same statement,
// and the claim pins the value the claiming pass's snapshot carried. The tests
// below are the three revocation triggers, each as an explicit interleaving —
// snapshot, then revoke, then claim — plus the monotonicity the whole thing
// rests on and the two fail-closed pins that hold even when the epoch does not
// move.

// armWebhookForClaimTest turns delivery on for an owner with both reminder kinds
// opted in — the configuration a claim now requires — and returns the revocation
// epoch that save produced, which is the value a notify pass's snapshot of this
// row would carry. Claims pin webhook_enabled, the kind's opt-in and this epoch,
// so a watermark test running against a row that never armed delivery would lose
// every claim for a reason that has nothing to do with what it is testing.
func armWebhookForClaimTest(t *testing.T, repo *UserRepository, userID uint) int {
	t.Helper()
	if err := repo.SaveWebhookSettings(context.Background(), userID, models.WebhookSettingsColumns{
		Enabled:          true,
		EncryptedURL:     "opaque-ciphertext-stand-in",
		NotifyPeriod:     true,
		NotifyOvulation:  true,
		ReminderLeadDays: models.DefaultReminderLeadDays,
	}); err != nil {
		t.Fatalf("arm webhook delivery for owner %d: %v", userID, err)
	}
	return reloadUserForWebhook(t, repo, userID).WebhookConfigVersion
}

// TestSaveWebhookSettingsAdvancesTheRevocationEpoch proves the settings save —
// the one write behind enable, disable, endpoint replacement and endpoint
// removal alike — moves the epoch every time, in the same statement that stores
// the settings. Without this every test below is vacuous: the claim would keep
// matching because nothing ever invalidated the snapshot.
func TestSaveWebhookSettingsAdvancesTheRevocationEpoch(t *testing.T) {
	repo := openWebhookRepoForTest(t)
	user := createUserForTimezoneTest(t, repo, "wh-epoch-save@example.com")

	fresh := reloadUserForWebhook(t, repo, user.ID)
	if fresh.WebhookConfigVersion != 0 {
		t.Fatalf("expected a fresh owner at epoch 0, got %d", fresh.WebhookConfigVersion)
	}

	first := armWebhookForClaimTest(t, repo, user.ID)
	if first <= fresh.WebhookConfigVersion {
		t.Fatalf("arming delivery must advance the epoch: before=%d after=%d", fresh.WebhookConfigVersion, first)
	}

	// Disabling is the same write, and owes the same advance — it is the
	// revocation the owner is most likely to make.
	if err := repo.SaveWebhookSettings(context.Background(), user.ID, models.WebhookSettingsColumns{
		Enabled:          false,
		EncryptedURL:     "opaque-ciphertext-stand-in",
		NotifyPeriod:     true,
		NotifyOvulation:  true,
		ReminderLeadDays: models.DefaultReminderLeadDays,
	}); err != nil {
		t.Fatalf("disable delivery: %v", err)
	}
	second := reloadUserForWebhook(t, repo, user.ID).WebhookConfigVersion
	if second <= first {
		t.Fatalf("disabling delivery must advance the epoch: before=%d after=%d", first, second)
	}
}

// TestUpdateReminderLeadDaysAdvancesTheRevocationEpoch covers the third writer,
// which is the one easy to leave out: reminder_lead_days is shared with the
// in-app dashboard banner, so it does not read like webhook configuration — and
// it is, because ListAllForNotify projects it and the notify decision uses it to
// place the reminder. An owner narrowing the window from seven days to zero is
// saying "not this early", and a pass holding the seven-day snapshot must not
// deliver against it. A rule applied at two of its three write sites is what
// leaves the third reachable.
func TestUpdateReminderLeadDaysAdvancesTheRevocationEpoch(t *testing.T) {
	repo := openWebhookRepoForTest(t)
	user := createUserForTimezoneTest(t, repo, "wh-epoch-leaddays@example.com")

	snapshotEpoch := armWebhookForClaimTest(t, repo, user.ID)
	anchor := time.Date(2026, time.August, 20, 0, 0, 0, 0, time.UTC)

	if err := repo.UpdateReminderLeadDays(context.Background(), user.ID, 0); err != nil {
		t.Fatalf("narrow the lead window: %v", err)
	}

	after := reloadUserForWebhook(t, repo, user.ID)
	if after.WebhookConfigVersion <= snapshotEpoch {
		t.Fatalf("a lead-window change must advance the epoch: before=%d after=%d", snapshotEpoch, after.WebhookConfigVersion)
	}
	if after.ReminderLeadDays != 0 {
		t.Fatalf("the lead window itself must still be written, got %d", after.ReminderLeadDays)
	}

	claimed, err := repo.ClaimWebhookWatermark(context.Background(), user.ID, models.WebhookReminderTypePeriod, anchor, nil, snapshotEpoch)
	if err != nil {
		t.Fatalf("a lost claim is a normal outcome, not an error: %v", err)
	}
	if claimed {
		t.Fatal("a pass holding the owner's previous lead window must lose its claim")
	}

	// Positive anchor: a pass that read the narrowed window still delivers what
	// that window admits.
	claimed, err = repo.ClaimWebhookWatermark(context.Background(), user.ID, models.WebhookReminderTypePeriod, anchor, nil, after.WebhookConfigVersion)
	if err != nil {
		t.Fatalf("current-window claim: %v", err)
	}
	if !claimed {
		t.Fatal("a pass holding the owner's current lead window must win its claim")
	}
}

// TestSaveWebhookSettingsEpochIsScopedToTheOwner proves the advance is a
// per-owner value, not a shared counter: one owner's save must not move another
// owner's epoch, or every household member's in-flight pass would lose its claim
// whenever any of them touched their settings.
func TestSaveWebhookSettingsEpochIsScopedToTheOwner(t *testing.T) {
	repo := openWebhookRepoForTest(t)
	owner := createUserForTimezoneTest(t, repo, "wh-epoch-scope-a@example.com")
	other := createUserForTimezoneTest(t, repo, "wh-epoch-scope-b@example.com")

	otherBefore := reloadUserForWebhook(t, repo, other.ID).WebhookConfigVersion
	armWebhookForClaimTest(t, repo, owner.ID)

	otherAfter := reloadUserForWebhook(t, repo, other.ID).WebhookConfigVersion
	if otherAfter != otherBefore {
		t.Fatalf("owner A's save moved owner B's epoch from %d to %d", otherBefore, otherAfter)
	}
}

// TestClearAllDataAdvancesTheWebhookRevocationEpoch is the monotonicity claim
// the whole mechanism rests on. Clear-data resets every other webhook column to
// what a fresh account carries; the epoch is the one that must ADVANCE instead.
// Resetting it to zero would hand a snapshot taken before the wipe its own value
// back, and since the wipe also NULLs both watermarks — re-opening the claim's
// first-ever-send branch — that snapshot would win its claim and POST to the
// endpoint the wipe had just erased.
func TestClearAllDataAdvancesTheWebhookRevocationEpoch(t *testing.T) {
	repo := openWebhookRepoForTest(t)
	user := createUserForTimezoneTest(t, repo, "wh-epoch-clear@example.com")

	epoch := armWebhookForClaimTest(t, repo, user.ID)

	if err := repo.ClearAllDataAndResetSettings(context.Background(), user.ID); err != nil {
		t.Fatalf("ClearAllDataAndResetSettings: %v", err)
	}

	after := reloadUserForWebhook(t, repo, user.ID)
	if after.WebhookConfigVersion <= epoch {
		t.Fatalf("clear-data must ADVANCE the revocation epoch, never reset it: before=%d after=%d", epoch, after.WebhookConfigVersion)
	}
	// The rest of the block still resets, so the advance is not a settings write
	// in disguise.
	if after.WebhookEnabled || after.WebhookURL != "" {
		t.Fatalf("clear-data must still disarm delivery and clear the endpoint, got enabled=%v url=%q", after.WebhookEnabled, after.WebhookURL)
	}
}

// TestClaimWebhookWatermarkIsLostAfterTheOwnerDisabledDelivery is trigger one of
// three, written as the interleaving it defends against: the pass snapshots an
// armed owner, the owner turns delivery off and is told it succeeded, and only
// then does the pass try to claim its send. The claim must LOSE. Before the
// epoch it won — the disable is a settings save, and a settings save
// deliberately leaves the watermark exactly where the snapshot found it.
func TestClaimWebhookWatermarkIsLostAfterTheOwnerDisabledDelivery(t *testing.T) {
	repo := openWebhookRepoForTest(t)
	user := createUserForTimezoneTest(t, repo, "wh-revoke-disable@example.com")

	// The pass's snapshot: delivery armed, no reminder ever sent.
	snapshotEpoch := armWebhookForClaimTest(t, repo, user.ID)
	anchor := time.Date(2026, time.March, 26, 0, 0, 0, 0, time.UTC)

	// The owner disables delivery while the pass is still working through the
	// owner list.
	if err := repo.SaveWebhookSettings(context.Background(), user.ID, models.WebhookSettingsColumns{
		Enabled:          false,
		EncryptedURL:     "opaque-ciphertext-stand-in",
		NotifyPeriod:     true,
		NotifyOvulation:  true,
		ReminderLeadDays: models.DefaultReminderLeadDays,
	}); err != nil {
		t.Fatalf("disable delivery: %v", err)
	}

	claimed, err := repo.ClaimWebhookWatermark(context.Background(), user.ID, models.WebhookReminderTypePeriod, anchor, nil, snapshotEpoch)
	if err != nil {
		t.Fatalf("a lost claim is a normal outcome, not an error: %v", err)
	}
	if claimed {
		t.Fatal("a pass holding the configuration the owner just disabled must lose its claim — winning it delivers to a revoked endpoint")
	}
	after := reloadUserForWebhook(t, repo, user.ID)
	if after.WebhookPeriodLastSentCycleStart != nil {
		t.Fatalf("a lost claim must write no watermark, got %v", after.WebhookPeriodLastSentCycleStart)
	}

	// Positive anchor: the refusal is about THIS snapshot, not a predicate that
	// now refuses everything. Re-arming delivery yields a fresh epoch, and a pass
	// carrying that one wins.
	rearmed := armWebhookForClaimTest(t, repo, user.ID)
	claimed, err = repo.ClaimWebhookWatermark(context.Background(), user.ID, models.WebhookReminderTypePeriod, anchor, nil, rearmed)
	if err != nil {
		t.Fatalf("re-armed claim: %v", err)
	}
	if !claimed {
		t.Fatal("a pass holding the owner's current configuration must still win its claim")
	}
}

// TestClaimWebhookWatermarkIsLostAfterTheOwnerChangedTheEndpoint is trigger two:
// delivery stays on, but the endpoint the snapshot carries is no longer the one
// the owner wants their health data sent to. The decrypted URL the pass is about
// to POST to came out of that snapshot, so a claim that ignored the change would
// deliver to the replaced endpoint — the removal case is the same write with an
// empty URL, and is covered by the same epoch.
func TestClaimWebhookWatermarkIsLostAfterTheOwnerChangedTheEndpoint(t *testing.T) {
	repo := openWebhookRepoForTest(t)
	user := createUserForTimezoneTest(t, repo, "wh-revoke-endpoint@example.com")

	snapshotEpoch := armWebhookForClaimTest(t, repo, user.ID)
	anchor := time.Date(2026, time.April, 23, 0, 0, 0, 0, time.UTC)

	if err := repo.SaveWebhookSettings(context.Background(), user.ID, models.WebhookSettingsColumns{
		Enabled:          true,
		EncryptedURL:     "a-different-opaque-ciphertext",
		NotifyPeriod:     true,
		NotifyOvulation:  true,
		ReminderLeadDays: models.DefaultReminderLeadDays,
	}); err != nil {
		t.Fatalf("replace the endpoint: %v", err)
	}

	claimed, err := repo.ClaimWebhookWatermark(context.Background(), user.ID, models.WebhookReminderTypePeriod, anchor, nil, snapshotEpoch)
	if err != nil {
		t.Fatalf("a lost claim is a normal outcome, not an error: %v", err)
	}
	if claimed {
		t.Fatal("a pass holding the replaced endpoint must lose its claim — the URL it would POST to is the one the owner removed")
	}

	// Positive anchor: an owner whose endpoint is current still gets reminders.
	current := reloadUserForWebhook(t, repo, user.ID).WebhookConfigVersion
	claimed, err = repo.ClaimWebhookWatermark(context.Background(), user.ID, models.WebhookReminderTypePeriod, anchor, nil, current)
	if err != nil {
		t.Fatalf("current-endpoint claim: %v", err)
	}
	if !claimed {
		t.Fatal("a pass holding the owner's current endpoint must win its claim")
	}
}

// TestClaimWebhookWatermarkIsLostAfterClearData is trigger three, and the one
// the watermark alone cannot survive in either direction: the wipe sets both
// watermarks back to NULL, so the stale snapshot's "the column has not moved
// since I read it" predicate — nil expecting NULL — is satisfied AGAIN. The
// epoch is the only thing standing between an erasure gesture and a POST of the
// erased account's predictions to the endpoint the erasure removed.
func TestClaimWebhookWatermarkIsLostAfterClearData(t *testing.T) {
	repo := openWebhookRepoForTest(t)
	user := createUserForTimezoneTest(t, repo, "wh-revoke-clear@example.com")

	snapshotEpoch := armWebhookForClaimTest(t, repo, user.ID)
	anchor := time.Date(2026, time.May, 30, 0, 0, 0, 0, time.UTC)

	if err := repo.ClearAllDataAndResetSettings(context.Background(), user.ID); err != nil {
		t.Fatalf("ClearAllDataAndResetSettings: %v", err)
	}

	wiped := reloadUserForWebhook(t, repo, user.ID)
	if wiped.WebhookPeriodLastSentCycleStart != nil {
		t.Fatalf("fixture precondition: clear-data is expected to NULL the watermark, got %v", wiped.WebhookPeriodLastSentCycleStart)
	}

	claimed, err := repo.ClaimWebhookWatermark(context.Background(), user.ID, models.WebhookReminderTypePeriod, anchor, nil, snapshotEpoch)
	if err != nil {
		t.Fatalf("a lost claim is a normal outcome, not an error: %v", err)
	}
	if claimed {
		t.Fatal("a pass snapshotted before a clear-data wipe must lose its claim — the NULLed watermark otherwise re-opens the first-ever-send branch for it")
	}

	// Positive anchor: the account is usable again after the wipe. An owner who
	// re-arms delivery gets reminders from a pass holding the new configuration.
	rearmed := armWebhookForClaimTest(t, repo, user.ID)
	claimed, err = repo.ClaimWebhookWatermark(context.Background(), user.ID, models.WebhookReminderTypePeriod, anchor, nil, rearmed)
	if err != nil {
		t.Fatalf("re-armed claim: %v", err)
	}
	if !claimed {
		t.Fatal("an owner who re-arms delivery after a wipe must still receive reminders")
	}
}

// TestReleaseWebhookWatermarkStillRestoresAfterARevocation pins the asymmetry
// the release's docstring claims: it deliberately does NOT pin the epoch. The
// claim gates egress and must fail closed; the release only puts a column back,
// and refusing it would leave a watermark standing for a send that never
// happened — suppressing that cycle's reminder for good if the owner re-arms
// delivery. Without this case, "complete the symmetry" is a one-line edit that
// nothing goes red for.
func TestReleaseWebhookWatermarkStillRestoresAfterARevocation(t *testing.T) {
	repo := openWebhookRepoForTest(t)
	user := createUserForTimezoneTest(t, repo, "wh-release-revoked@example.com")

	epoch := armWebhookForClaimTest(t, repo, user.ID)
	anchor := time.Date(2026, time.September, 3, 0, 0, 0, 0, time.UTC)
	if claimed, err := repo.ClaimWebhookWatermark(context.Background(), user.ID, models.WebhookReminderTypePeriod, anchor, nil, epoch); err != nil || !claimed {
		t.Fatalf("fixture precondition: the claim must be won, got claimed=%v err=%v", claimed, err)
	}

	// The delivery fails, and the owner disables the webhook before the release
	// lands. The release must still happen: nothing was delivered.
	if err := repo.SaveWebhookSettings(context.Background(), user.ID, models.WebhookSettingsColumns{
		Enabled:          false,
		EncryptedURL:     "opaque-ciphertext-stand-in",
		NotifyPeriod:     true,
		NotifyOvulation:  true,
		ReminderLeadDays: models.DefaultReminderLeadDays,
	}); err != nil {
		t.Fatalf("disable delivery: %v", err)
	}
	if err := repo.ReleaseWebhookWatermark(context.Background(), user.ID, models.WebhookReminderTypePeriod, anchor, nil); err != nil {
		t.Fatalf("release after a revocation: %v", err)
	}
	after := reloadUserForWebhook(t, repo, user.ID)
	if after.WebhookPeriodLastSentCycleStart != nil {
		t.Fatalf("the release must still undo a claim whose delivery failed, got %v", after.WebhookPeriodLastSentCycleStart)
	}
}

// TestReleaseWebhookWatermarkLeavesAClearDataWipeAlone is the other half of the
// same docstring: where the revocation was a clear-data wipe, the watermark is
// already NULL, so the release's own "column = anchor" compare-and-set matches
// nothing and the wipe stands. A release that wrote unconditionally would put a
// value back onto a row the owner had just emptied.
func TestReleaseWebhookWatermarkLeavesAClearDataWipeAlone(t *testing.T) {
	repo := openWebhookRepoForTest(t)
	user := createUserForTimezoneTest(t, repo, "wh-release-wiped@example.com")

	epoch := armWebhookForClaimTest(t, repo, user.ID)
	first := time.Date(2026, time.September, 3, 0, 0, 0, 0, time.UTC)
	second := first.AddDate(0, 0, 28)
	if claimed, err := repo.ClaimWebhookWatermark(context.Background(), user.ID, models.WebhookReminderTypePeriod, first, nil, epoch); err != nil || !claimed {
		t.Fatalf("fixture precondition: the first claim must be won, got claimed=%v err=%v", claimed, err)
	}
	// A second claim so the release carries a NON-NULL previous: a release that
	// ignored its predicate would write that value back, which is observable.
	if claimed, err := repo.ClaimWebhookWatermark(context.Background(), user.ID, models.WebhookReminderTypePeriod, second, &first, epoch); err != nil || !claimed {
		t.Fatalf("fixture precondition: the second claim must be won, got claimed=%v err=%v", claimed, err)
	}

	if err := repo.ClearAllDataAndResetSettings(context.Background(), user.ID); err != nil {
		t.Fatalf("ClearAllDataAndResetSettings: %v", err)
	}
	if err := repo.ReleaseWebhookWatermark(context.Background(), user.ID, models.WebhookReminderTypePeriod, second, &first); err != nil {
		t.Fatalf("release against a wiped row: %v", err)
	}

	after := reloadUserForWebhook(t, repo, user.ID)
	if after.WebhookPeriodLastSentCycleStart != nil {
		t.Fatalf("a release must not put a watermark back onto a row clear-data emptied, got %v", after.WebhookPeriodLastSentCycleStart)
	}
}

// TestClaimWebhookWatermarkIsRefusedWhileDeliveryIsDisabled pins the fail-closed
// floor under the epoch. A row can carry a revocation the epoch never saw: every
// row existing before migration 038 starts at epoch 0, so an owner who disabled
// delivery before the upgrade has a disarmed row at the same epoch the first
// post-upgrade pass will read. webhook_enabled is pinned for exactly that case —
// and it would also hold if some later path ever wrote the flag without
// advancing the epoch.
func TestClaimWebhookWatermarkIsRefusedWhileDeliveryIsDisabled(t *testing.T) {
	repo := openWebhookRepoForTest(t)
	user := createUserForTimezoneTest(t, repo, "wh-revoke-preepoch@example.com")

	// Disarm the row WITHOUT going through the save path, so the epoch stays
	// exactly where the pass's snapshot found it: the pre-038 revoked row.
	if err := repo.database.Model(&models.User{}).Where("id = ?", user.ID).
		Update("webhook_enabled", false).Error; err != nil {
		t.Fatalf("seed a disarmed row: %v", err)
	}
	stale := reloadUserForWebhook(t, repo, user.ID)

	claimed, err := repo.ClaimWebhookWatermark(context.Background(), user.ID, models.WebhookReminderTypePeriod,
		time.Date(2026, time.June, 4, 0, 0, 0, 0, time.UTC), nil, stale.WebhookConfigVersion)
	if err != nil {
		t.Fatalf("a lost claim is a normal outcome, not an error: %v", err)
	}
	if claimed {
		t.Fatal("a claim against a row with delivery disabled must be refused even when the epoch matches")
	}
}

// TestClaimWebhookWatermarkIsRefusedForAKindTheOwnerOptedOutOf pins the same
// floor per reminder kind: an owner who keeps the webhook but silences one kind
// has revoked that kind, and the claim for it must be refused on the row's own
// columns rather than only on a decision made upstream from a snapshot.
func TestClaimWebhookWatermarkIsRefusedForAKindTheOwnerOptedOutOf(t *testing.T) {
	repo := openWebhookRepoForTest(t)
	user := createUserForTimezoneTest(t, repo, "wh-revoke-kind@example.com")

	if err := repo.SaveWebhookSettings(context.Background(), user.ID, models.WebhookSettingsColumns{
		Enabled:          true,
		EncryptedURL:     "opaque-ciphertext-stand-in",
		NotifyPeriod:     true,
		NotifyOvulation:  false,
		ReminderLeadDays: models.DefaultReminderLeadDays,
	}); err != nil {
		t.Fatalf("save settings with ovulation silenced: %v", err)
	}
	epoch := reloadUserForWebhook(t, repo, user.ID).WebhookConfigVersion
	anchor := time.Date(2026, time.July, 9, 0, 0, 0, 0, time.UTC)

	claimed, err := repo.ClaimWebhookWatermark(context.Background(), user.ID, models.WebhookReminderTypeOvulation, anchor, nil, epoch)
	if err != nil {
		t.Fatalf("a lost claim is a normal outcome, not an error: %v", err)
	}
	if claimed {
		t.Fatal("a claim for a reminder kind the owner switched off must be refused")
	}

	// Positive anchor: the kind the owner kept is unaffected, so the pin is
	// per-kind and not a blanket refusal.
	claimed, err = repo.ClaimWebhookWatermark(context.Background(), user.ID, models.WebhookReminderTypePeriod, anchor, nil, epoch)
	if err != nil {
		t.Fatalf("period claim: %v", err)
	}
	if !claimed {
		t.Fatal("silencing the ovulation reminder must not withhold the period reminder")
	}
}
