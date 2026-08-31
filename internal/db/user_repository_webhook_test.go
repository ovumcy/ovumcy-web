package db

import (
	"context"
	"path/filepath"
	"testing"
	"time"

	"github.com/ovumcy/ovumcy-web/internal/models"
)

func openWebhookRepoForTest(t *testing.T) *UserRepository {
	t.Helper()
	database := openSQLiteForMigrationBootstrapTest(t, filepath.Join(t.TempDir(), "webhook.db"))
	return NewUserRepository(database)
}

func reloadUserForWebhook(t *testing.T, repo *UserRepository, userID uint) models.User {
	t.Helper()
	var reloaded models.User
	if err := repo.database.First(&reloaded, userID).Error; err != nil {
		t.Fatalf("reload user %d: %v", userID, err)
	}
	return reloaded
}

// TestSaveWebhookSettingsPersistsColumns proves the narrow write stores exactly
// the settings columns handed to it (webhook_url as the opaque ciphertext the
// service produced) and leaves auth_session_version untouched — a notification
// preference change is not a security-posture change.
func TestSaveWebhookSettingsPersistsColumns(t *testing.T) {
	repo := openWebhookRepoForTest(t)
	user := createUserForTimezoneTest(t, repo, "wh-persist@example.com")

	before := reloadUserForWebhook(t, repo, user.ID)
	if before.WebhookEnabled {
		t.Fatal("expected webhook_enabled=false on a fresh user")
	}
	if before.ReminderLeadDays != models.DefaultReminderLeadDays {
		t.Fatalf("expected fresh reminder_lead_days=%d, got %d", models.DefaultReminderLeadDays, before.ReminderLeadDays)
	}

	const opaqueCiphertext = "opaque-ciphertext-stand-in"
	if err := repo.SaveWebhookSettings(context.Background(), user.ID, models.WebhookSettingsColumns{
		Enabled:          true,
		EncryptedURL:     opaqueCiphertext,
		NotifyPeriod:     true,
		NotifyOvulation:  false,
		ReminderLeadDays: 7,
	}); err != nil {
		t.Fatalf("SaveWebhookSettings: %v", err)
	}

	after := reloadUserForWebhook(t, repo, user.ID)
	if !after.WebhookEnabled {
		t.Fatal("expected webhook_enabled=true after save")
	}
	if after.WebhookURL != opaqueCiphertext {
		t.Fatalf("expected webhook_url to store the ciphertext verbatim, got %q", after.WebhookURL)
	}
	if !after.WebhookNotifyPeriod {
		t.Fatal("expected webhook_notify_period=true after save")
	}
	if after.WebhookNotifyOvulation {
		t.Fatal("expected webhook_notify_ovulation=false after save")
	}
	if after.ReminderLeadDays != 7 {
		t.Fatalf("expected reminder_lead_days=7 after save, got %d", after.ReminderLeadDays)
	}
	if after.AuthSessionVersion != before.AuthSessionVersion {
		t.Fatalf("SaveWebhookSettings must not bump auth_session_version: before=%d after=%d", before.AuthSessionVersion, after.AuthSessionVersion)
	}
}

// TestSaveWebhookSettingsScopedToUser proves the write is strictly scoped to the
// target user id: saving owner A's webhook settings never touches owner B's row
// (the household-multi-owner isolation boundary).
func TestSaveWebhookSettingsScopedToUser(t *testing.T) {
	repo := openWebhookRepoForTest(t)
	owner := createUserForTimezoneTest(t, repo, "wh-owner@example.com")
	other := createUserForTimezoneTest(t, repo, "wh-other@example.com")

	if err := repo.SaveWebhookSettings(context.Background(), other.ID, models.WebhookSettingsColumns{
		Enabled:          true,
		EncryptedURL:     "other-ciphertext",
		NotifyPeriod:     true,
		NotifyOvulation:  true,
		ReminderLeadDays: 5,
	}); err != nil {
		t.Fatalf("seed other owner webhook settings: %v", err)
	}

	if err := repo.SaveWebhookSettings(context.Background(), owner.ID, models.WebhookSettingsColumns{
		Enabled:          false,
		EncryptedURL:     "owner-ciphertext",
		NotifyPeriod:     false,
		NotifyOvulation:  false,
		ReminderLeadDays: 1,
	}); err != nil {
		t.Fatalf("SaveWebhookSettings owner: %v", err)
	}

	gotOther := reloadUserForWebhook(t, repo, other.ID)
	if gotOther.WebhookURL != "other-ciphertext" || !gotOther.WebhookEnabled || gotOther.ReminderLeadDays != 5 {
		t.Fatalf("other owner row was mutated by owner save: %+v", gotOther)
	}
	gotOwner := reloadUserForWebhook(t, repo, owner.ID)
	if gotOwner.WebhookURL != "owner-ciphertext" || gotOwner.WebhookEnabled || gotOwner.ReminderLeadDays != 1 {
		t.Fatalf("owner row not persisted as expected: %+v", gotOwner)
	}
}

// TestListAllForNotifyReturnsWhitelistedColumns proves the notify projection
// returns the webhook settings, watermarks, and cycle inputs for every owner,
// carrying webhook_url as the stored ciphertext.
func TestListAllForNotifyReturnsWhitelistedColumns(t *testing.T) {
	repo := openWebhookRepoForTest(t)
	first := createUserForTimezoneTest(t, repo, "notify-1@example.com")
	second := createUserForTimezoneTest(t, repo, "notify-2@example.com")

	if err := repo.UpdateUserTimezone(context.Background(), first.ID, "Europe/Belgrade"); err != nil {
		t.Fatalf("seed timezone: %v", err)
	}
	if err := repo.SaveWebhookSettings(context.Background(), first.ID, models.WebhookSettingsColumns{
		Enabled:          true,
		EncryptedURL:     "cipher-first",
		NotifyPeriod:     true,
		NotifyOvulation:  false,
		ReminderLeadDays: 4,
	}); err != nil {
		t.Fatalf("seed first webhook settings: %v", err)
	}

	// Set a watermark directly to confirm it round-trips through the projection.
	anchor := time.Date(2026, time.March, 1, 0, 0, 0, 0, time.UTC)
	if err := repo.database.Model(&models.User{}).Where("id = ?", first.ID).
		Update("webhook_period_last_sent_cycle_start", anchor).Error; err != nil {
		t.Fatalf("seed watermark: %v", err)
	}

	records, err := repo.ListAllForNotify(context.Background())
	if err != nil {
		t.Fatalf("ListAllForNotify: %v", err)
	}
	if len(records) != 2 {
		t.Fatalf("expected 2 notify records, got %d", len(records))
	}

	byID := make(map[uint]models.WebhookNotifyRecord, len(records))
	for _, record := range records {
		byID[record.ID] = record
	}

	got := byID[first.ID]
	if !got.WebhookEnabled {
		t.Fatal("expected first record webhook_enabled=true")
	}
	if got.WebhookURL != "cipher-first" {
		t.Fatalf("expected first record webhook_url ciphertext 'cipher-first', got %q", got.WebhookURL)
	}
	if !got.WebhookNotifyPeriod || got.WebhookNotifyOvulation {
		t.Fatalf("first record notify flags mismatch: period=%v ovulation=%v", got.WebhookNotifyPeriod, got.WebhookNotifyOvulation)
	}
	if got.ReminderLeadDays != 4 {
		t.Fatalf("expected first record reminder_lead_days=4, got %d", got.ReminderLeadDays)
	}
	// The revocation epoch has to ride the projection, because the claim pins
	// the value the snapshot carried. A projection that dropped it would hand
	// every claim a zero, and the revocation check would agree with every stale
	// snapshot on a fresh instance.
	storedEpoch := reloadUserForWebhook(t, repo, first.ID).WebhookConfigVersion
	if storedEpoch == 0 {
		t.Fatal("fixture precondition: the settings save above is expected to have advanced the revocation epoch")
	}
	if got.WebhookConfigVersion != storedEpoch {
		t.Fatalf("expected first record webhook_config_version=%d, got %d", storedEpoch, got.WebhookConfigVersion)
	}
	if got.Timezone != "Europe/Belgrade" {
		t.Fatalf("expected first record timezone Europe/Belgrade, got %q", got.Timezone)
	}
	if got.CycleLength != 28 || got.PeriodLength != 5 {
		t.Fatalf("expected cycle inputs to load, got cycle=%d period=%d", got.CycleLength, got.PeriodLength)
	}
	if got.WebhookPeriodLastSentCycleStart == nil || !got.WebhookPeriodLastSentCycleStart.Equal(anchor) {
		t.Fatalf("expected first record period watermark %s, got %v", anchor, got.WebhookPeriodLastSentCycleStart)
	}

	// The untouched second owner is present with column defaults and a nil
	// watermark (no reminder ever sent).
	other := byID[second.ID]
	if other.WebhookEnabled {
		t.Fatal("expected second record webhook_enabled=false")
	}
	if other.ReminderLeadDays != models.DefaultReminderLeadDays {
		t.Fatalf("expected second record default reminder_lead_days=%d, got %d", models.DefaultReminderLeadDays, other.ReminderLeadDays)
	}
	if other.WebhookPeriodLastSentCycleStart != nil {
		t.Fatalf("expected second record nil period watermark, got %v", other.WebhookPeriodLastSentCycleStart)
	}
}

// TestClearAllDataResetsWebhookColumns proves a clear-data wipe disarms
// webhook delivery, clears the encrypted endpoint, resets the shared lead
// window and per-kind opt-ins to defaults, and clears both watermarks so no
// stale reminder can fire against the freshly emptied account.
func TestClearAllDataResetsWebhookColumns(t *testing.T) {
	repo := openWebhookRepoForTest(t)
	user := createUserForTimezoneTest(t, repo, "wh-clear@example.com")

	if err := repo.SaveWebhookSettings(context.Background(), user.ID, models.WebhookSettingsColumns{
		Enabled:          true,
		EncryptedURL:     "cipher-to-wipe",
		NotifyPeriod:     false,
		NotifyOvulation:  false,
		ReminderLeadDays: 10,
	}); err != nil {
		t.Fatalf("seed webhook settings: %v", err)
	}
	anchor := time.Date(2026, time.February, 10, 0, 0, 0, 0, time.UTC)
	if err := repo.database.Model(&models.User{}).Where("id = ?", user.ID).Updates(map[string]any{
		"webhook_period_last_sent_cycle_start":    anchor,
		"webhook_ovulation_last_sent_cycle_start": anchor,
	}).Error; err != nil {
		t.Fatalf("seed watermarks: %v", err)
	}

	if err := repo.ClearAllDataAndResetSettings(context.Background(), user.ID); err != nil {
		t.Fatalf("ClearAllDataAndResetSettings: %v", err)
	}

	got := reloadUserForWebhook(t, repo, user.ID)
	if got.WebhookEnabled {
		t.Fatal("expected webhook_enabled=false after clear-data")
	}
	if got.WebhookURL != "" {
		t.Fatalf("expected webhook_url cleared after clear-data, got %q", got.WebhookURL)
	}
	if !got.WebhookNotifyPeriod || !got.WebhookNotifyOvulation {
		t.Fatalf("expected per-kind opt-ins reset to true, got period=%v ovulation=%v", got.WebhookNotifyPeriod, got.WebhookNotifyOvulation)
	}
	if got.ReminderLeadDays != models.DefaultReminderLeadDays {
		t.Fatalf("expected reminder_lead_days reset to %d, got %d", models.DefaultReminderLeadDays, got.ReminderLeadDays)
	}
	if got.WebhookPeriodLastSentCycleStart != nil || got.WebhookOvulationLastSentCycleStart != nil {
		t.Fatalf("expected watermarks cleared after clear-data, got period=%v ovulation=%v", got.WebhookPeriodLastSentCycleStart, got.WebhookOvulationLastSentCycleStart)
	}
}

// TestListAllForNotifyReturnsErrorOnQueryFailure exercises the error-return
// branch of ListAllForNotify: when the underlying SELECT fails (here because the
// users table has been dropped), the method must propagate the error and return
// a nil slice rather than report empty success. Mirrors the drop-table technique
// the account-erasure tests use to reach their own error branches.
func TestListAllForNotifyReturnsErrorOnQueryFailure(t *testing.T) {
	repo := openWebhookRepoForTest(t)

	if err := repo.database.Exec("DROP TABLE users").Error; err != nil {
		t.Fatalf("drop users table: %v", err)
	}

	records, err := repo.ListAllForNotify(context.Background())
	if err == nil {
		t.Fatal("expected ListAllForNotify to error when the users table is missing")
	}
	if records != nil {
		t.Fatalf("expected nil records on error, got %v", records)
	}
}

// TestClaimWebhookWatermarkCanonicalizesToUTCMidnight proves the watermark claim
// (issue #124, slice 3) stores the cycle anchor as UTC-midnight even when handed
// a location-bearing time — the write uses Update/Updates, which bypasses the
// model BeforeSave hook, so the method must canonicalize itself. A drifted
// (non-midnight, non-UTC) stored value would compare unequal to a UTC-midnight
// anchor on the next pass and break both idempotency and the claim predicate,
// which is exact equality on the stored value.
func TestClaimWebhookWatermarkCanonicalizesToUTCMidnight(t *testing.T) {
	repo := openWebhookRepoForTest(t)
	user := createUserForTimezoneTest(t, repo, "wh-watermark@example.com")
	epoch := armWebhookForClaimTest(t, repo, user.ID)

	before := reloadUserForWebhook(t, repo, user.ID)

	// A New York afternoon: 2026-03-14 15:00 -04:00 is still 2026-03-14 locally,
	// and its UTC instant is 2026-03-14 19:00Z — canonicalization must key on the
	// wall-clock calendar day of the supplied value (14 March), stored at UTC
	// midnight.
	loc, err := time.LoadLocation("America/New_York")
	if err != nil {
		t.Fatalf("load location: %v", err)
	}
	anchor := time.Date(2026, time.March, 14, 15, 0, 0, 0, loc)

	claimed, err := repo.ClaimWebhookWatermark(context.Background(), user.ID, models.WebhookReminderTypePeriod, anchor, nil, epoch)
	if err != nil {
		t.Fatalf("ClaimWebhookWatermark: %v", err)
	}
	if !claimed {
		t.Fatal("the first claim on an empty watermark must be won")
	}

	after := reloadUserForWebhook(t, repo, user.ID)
	if after.WebhookPeriodLastSentCycleStart == nil {
		t.Fatal("expected the period watermark to be set")
	}
	got := after.WebhookPeriodLastSentCycleStart.UTC()
	want := time.Date(2026, time.March, 14, 0, 0, 0, 0, time.UTC)
	if !got.Equal(want) {
		t.Fatalf("expected watermark stored as %s, got %s", want, got)
	}
	// The ovulation watermark must be untouched.
	if after.WebhookOvulationLastSentCycleStart != nil {
		t.Fatalf("period write must not touch the ovulation watermark, got %v", after.WebhookOvulationLastSentCycleStart)
	}
	// Advancing a send watermark is not a security-posture change.
	if after.AuthSessionVersion != before.AuthSessionVersion {
		t.Fatalf("ClaimWebhookWatermark must not bump auth_session_version: before=%d after=%d", before.AuthSessionVersion, after.AuthSessionVersion)
	}
}

// TestClaimWebhookWatermarkOvulationColumn proves the ovulation kind writes the
// ovulation column (and not the period one), so the two kinds dedupe
// independently.
func TestClaimWebhookWatermarkOvulationColumn(t *testing.T) {
	repo := openWebhookRepoForTest(t)
	user := createUserForTimezoneTest(t, repo, "wh-ovulation@example.com")
	epoch := armWebhookForClaimTest(t, repo, user.ID)

	anchor := time.Date(2026, time.April, 1, 0, 0, 0, 0, time.UTC)
	claimed, err := repo.ClaimWebhookWatermark(context.Background(), user.ID, models.WebhookReminderTypeOvulation, anchor, nil, epoch)
	if err != nil {
		t.Fatalf("ClaimWebhookWatermark: %v", err)
	}
	if !claimed {
		t.Fatal("the first claim on an empty watermark must be won")
	}

	after := reloadUserForWebhook(t, repo, user.ID)
	if after.WebhookOvulationLastSentCycleStart == nil || !after.WebhookOvulationLastSentCycleStart.UTC().Equal(anchor) {
		t.Fatalf("expected ovulation watermark %s, got %v", anchor, after.WebhookOvulationLastSentCycleStart)
	}
	if after.WebhookPeriodLastSentCycleStart != nil {
		t.Fatalf("ovulation write must not touch the period watermark, got %v", after.WebhookPeriodLastSentCycleStart)
	}
}

// TestClaimWebhookWatermarkRejectsUnknownType proves an unrecognized reminder
// type is rejected (no column write), so a typo can never scribble an unexpected
// column.
func TestClaimWebhookWatermarkRejectsUnknownType(t *testing.T) {
	repo := openWebhookRepoForTest(t)
	user := createUserForTimezoneTest(t, repo, "wh-badtype@example.com")

	claimed, err := repo.ClaimWebhookWatermark(context.Background(), user.ID, "not-a-real-kind", time.Now(), nil, 0)
	if claimed {
		t.Fatal("an unknown reminder type must never report a won claim")
	}
	if err == nil {
		t.Fatal("expected an error for an unknown reminder type")
	}
	after := reloadUserForWebhook(t, repo, user.ID)
	if after.WebhookPeriodLastSentCycleStart != nil || after.WebhookOvulationLastSentCycleStart != nil {
		t.Fatal("a rejected type must not write any watermark column")
	}
}

// TestClaimWebhookWatermarkScopedToUser proves the watermark write is strictly
// scoped to the target user id: advancing owner A's watermark never touches owner
// B's row (the household-multi-owner isolation boundary).
func TestClaimWebhookWatermarkScopedToUser(t *testing.T) {
	repo := openWebhookRepoForTest(t)
	owner := createUserForTimezoneTest(t, repo, "wh-wm-owner@example.com")
	epoch := armWebhookForClaimTest(t, repo, owner.ID)
	other := createUserForTimezoneTest(t, repo, "wh-wm-other@example.com")

	anchor := time.Date(2026, time.May, 20, 0, 0, 0, 0, time.UTC)
	if _, err := repo.ClaimWebhookWatermark(context.Background(), owner.ID, models.WebhookReminderTypePeriod, anchor, nil, epoch); err != nil {
		t.Fatalf("ClaimWebhookWatermark: %v", err)
	}

	otherAfter := reloadUserForWebhook(t, repo, other.ID)
	if otherAfter.WebhookPeriodLastSentCycleStart != nil {
		t.Fatalf("owner B's watermark must be untouched, got %v", otherAfter.WebhookPeriodLastSentCycleStart)
	}
}

// TestClaimWebhookWatermarkIsExclusivePerAnchor proves the claim predicate on the
// real engine: the first claim on an anchor is won, a second claim on the SAME
// anchor is lost (zero rows, not an error), and a claim on a DIFFERENT anchor —
// the next cycle — is won again. That is what stops two overlapping notify passes
// from both delivering the same reminder while leaving the next cycle claimable.
func TestClaimWebhookWatermarkIsExclusivePerAnchor(t *testing.T) {
	repo := openWebhookRepoForTest(t)
	user := createUserForTimezoneTest(t, repo, "wh-claim-exclusive@example.com")
	epoch := armWebhookForClaimTest(t, repo, user.ID)

	anchor := time.Date(2026, time.March, 26, 0, 0, 0, 0, time.UTC)

	first, err := repo.ClaimWebhookWatermark(context.Background(), user.ID, models.WebhookReminderTypePeriod, anchor, nil, epoch)
	if err != nil {
		t.Fatalf("first claim: %v", err)
	}
	if !first {
		t.Fatal("the first claim on an empty watermark must be won")
	}

	second, err := repo.ClaimWebhookWatermark(context.Background(), user.ID, models.WebhookReminderTypePeriod, anchor, nil, epoch)
	if err != nil {
		t.Fatalf("second claim must not error, a lost claim is a normal outcome: %v", err)
	}
	if second {
		t.Fatal("a second claim on an anchor already claimed must be lost")
	}

	// A location-bearing spelling of the same calendar day is the same claim: the
	// method canonicalizes before comparing, so a pass running in the owner's zone
	// cannot re-claim what a pass running in UTC already took.
	loc, err := time.LoadLocation("Asia/Tokyo")
	if err != nil {
		t.Fatalf("load location: %v", err)
	}
	sameDay, err := repo.ClaimWebhookWatermark(context.Background(), user.ID, models.WebhookReminderTypePeriod, time.Date(2026, time.March, 26, 9, 0, 0, 0, loc), nil, epoch)
	if err != nil {
		t.Fatalf("same-day claim: %v", err)
	}
	if sameDay {
		t.Fatal("a claim on the same calendar day in another zone must be lost")
	}

	nextCycle := anchor.AddDate(0, 0, 28)
	third, err := repo.ClaimWebhookWatermark(context.Background(), user.ID, models.WebhookReminderTypePeriod, nextCycle, &anchor, epoch)
	if err != nil {
		t.Fatalf("next-cycle claim: %v", err)
	}
	if !third {
		t.Fatal("the next cycle's anchor is a different reminder and must be claimable")
	}
	after := reloadUserForWebhook(t, repo, user.ID)
	if after.WebhookPeriodLastSentCycleStart == nil || !after.WebhookPeriodLastSentCycleStart.UTC().Equal(nextCycle) {
		t.Fatalf("expected the watermark at %s, got %v", nextCycle, after.WebhookPeriodLastSentCycleStart)
	}
}

// TestClaimWebhookWatermarkIsLostWhenTheColumnMovedSinceTheSnapshot proves the
// claim is compared against the value the pass EXPECTS TO REPLACE, not against
// the value it is about to write. A pass works through the owner list holding a
// snapshot; while it does, the owner logs a new cycle start and a second pass
// claims and delivers that cycle's anchor. The first pass must now lose: its
// snapshot is stale, and winning would move the watermark BACKWARDS — delivering
// a reminder for a superseded cycle and, worse, leaving a column that no longer
// covers the newer anchor, so the newer reminder ships a second time on the next
// pass. That is the duplicate egress the claim exists to close, re-entered
// through the back door.
func TestClaimWebhookWatermarkIsLostWhenTheColumnMovedSinceTheSnapshot(t *testing.T) {
	repo := openWebhookRepoForTest(t)
	user := createUserForTimezoneTest(t, repo, "wh-claim-stale@example.com")
	epoch := armWebhookForClaimTest(t, repo, user.ID)

	// The stale pass's snapshot: the watermark is empty.
	var snapshot *time.Time
	stale := time.Date(2026, time.March, 26, 0, 0, 0, 0, time.UTC)

	// Meanwhile a second pass claims and delivers the new cycle's anchor.
	current := time.Date(2026, time.May, 30, 0, 0, 0, 0, time.UTC)
	won, err := repo.ClaimWebhookWatermark(context.Background(), user.ID, models.WebhookReminderTypePeriod, current, nil, epoch)
	if err != nil {
		t.Fatalf("current claim: %v", err)
	}
	if !won {
		t.Fatal("the second pass must win the claim on an empty watermark")
	}

	// The stale pass now tries to claim its own, older anchor against a snapshot
	// that predates the write above.
	claimed, err := repo.ClaimWebhookWatermark(context.Background(), user.ID, models.WebhookReminderTypePeriod, stale, snapshot, epoch)
	if err != nil {
		t.Fatalf("stale claim must not error, a lost claim is a normal outcome: %v", err)
	}
	if claimed {
		t.Fatal("a claim whose snapshot predates the stored watermark must be lost, never won")
	}

	after := reloadUserForWebhook(t, repo, user.ID)
	if after.WebhookPeriodLastSentCycleStart == nil || !after.WebhookPeriodLastSentCycleStart.UTC().Equal(current) {
		t.Fatalf("a lost claim must leave the newer watermark standing, expected %s got %v", current, after.WebhookPeriodLastSentCycleStart)
	}
}

// TestClaimWebhookWatermarkAcceptsTheValueItReadBack closes the round trip the
// compare-and-set rests on: the expected prior value a pass holds comes from a
// read of this same column, so it must compare equal to what is stored. If the
// driver's write and read spellings ever diverged, every claim past the first
// would be lost forever and reminders would stop silently.
func TestClaimWebhookWatermarkAcceptsTheValueItReadBack(t *testing.T) {
	repo := openWebhookRepoForTest(t)
	user := createUserForTimezoneTest(t, repo, "wh-claim-roundtrip@example.com")
	epoch := armWebhookForClaimTest(t, repo, user.ID)

	first := time.Date(2026, time.March, 26, 0, 0, 0, 0, time.UTC)
	if _, err := repo.ClaimWebhookWatermark(context.Background(), user.ID, models.WebhookReminderTypePeriod, first, nil, epoch); err != nil {
		t.Fatalf("first claim: %v", err)
	}

	stored := reloadUserForWebhook(t, repo, user.ID).WebhookPeriodLastSentCycleStart
	if stored == nil {
		t.Fatal("expected the first claim to have written the watermark")
	}

	next := first.AddDate(0, 0, 28)
	claimed, err := repo.ClaimWebhookWatermark(context.Background(), user.ID, models.WebhookReminderTypePeriod, next, stored, epoch)
	if err != nil {
		t.Fatalf("next-cycle claim: %v", err)
	}
	if !claimed {
		t.Fatal("a claim carrying the value read back from the column must be won")
	}
}

// TestClaimWebhookWatermarkReturnsErrorOnWriteFailure exercises the error-return
// branch of the claim: a storage failure must surface as an error and report the
// claim NOT won, never as a quiet "somebody else has it". The two outcomes lead
// the notify pass in opposite directions — a lost claim is a silent skip, an
// errored claim is counted and flagged for the operator — so conflating them
// would hide a broken database behind a normal-looking pass.
func TestClaimWebhookWatermarkReturnsErrorOnWriteFailure(t *testing.T) {
	repo := openWebhookRepoForTest(t)
	user := createUserForTimezoneTest(t, repo, "wh-claim-writefail@example.com")

	if err := repo.database.Exec("DROP TABLE users").Error; err != nil {
		t.Fatalf("drop users table: %v", err)
	}

	claimed, err := repo.ClaimWebhookWatermark(context.Background(), user.ID, models.WebhookReminderTypePeriod, time.Now(), nil, 0)
	if err == nil {
		t.Fatal("expected ClaimWebhookWatermark to error when the users table is missing")
	}
	if claimed {
		t.Fatal("a failed write must never report a won claim")
	}
}

// TestReleaseWebhookWatermarkRestoresOnlyItsOwnClaim proves the release side: it
// puts back the value the claiming pass found (including SQL NULL, so a first-ever
// reminder stays retryable), and it refuses to roll back a watermark that has
// moved on — a claim somebody else now owns must not be resurrected.
func TestReleaseWebhookWatermarkRestoresOnlyItsOwnClaim(t *testing.T) {
	repo := openWebhookRepoForTest(t)
	user := createUserForTimezoneTest(t, repo, "wh-release@example.com")
	epoch := armWebhookForClaimTest(t, repo, user.ID)

	anchor := time.Date(2026, time.March, 26, 0, 0, 0, 0, time.UTC)
	if _, err := repo.ClaimWebhookWatermark(context.Background(), user.ID, models.WebhookReminderTypePeriod, anchor, nil, epoch); err != nil {
		t.Fatalf("claim: %v", err)
	}
	if err := repo.ReleaseWebhookWatermark(context.Background(), user.ID, models.WebhookReminderTypePeriod, anchor, nil); err != nil {
		t.Fatalf("release to NULL: %v", err)
	}
	after := reloadUserForWebhook(t, repo, user.ID)
	if after.WebhookPeriodLastSentCycleStart != nil {
		t.Fatalf("a released first-ever claim must leave the column NULL, got %v", after.WebhookPeriodLastSentCycleStart)
	}
	// Released means retryable: the same anchor can be claimed again.
	reclaimed, err := repo.ClaimWebhookWatermark(context.Background(), user.ID, models.WebhookReminderTypePeriod, anchor, nil, epoch)
	if err != nil {
		t.Fatalf("reclaim: %v", err)
	}
	if !reclaimed {
		t.Fatal("a released anchor must be claimable again, otherwise a failed delivery is a permanent skip")
	}

	// Now the previous-value path: a pass that found the March anchor claims April
	// and fails, so the column must go back to March exactly.
	previous := anchor
	april := time.Date(2026, time.April, 23, 0, 0, 0, 0, time.UTC)
	if _, err := repo.ClaimWebhookWatermark(context.Background(), user.ID, models.WebhookReminderTypePeriod, april, &previous, epoch); err != nil {
		t.Fatalf("april claim: %v", err)
	}
	if err := repo.ReleaseWebhookWatermark(context.Background(), user.ID, models.WebhookReminderTypePeriod, april, &previous); err != nil {
		t.Fatalf("release to previous: %v", err)
	}
	after = reloadUserForWebhook(t, repo, user.ID)
	if after.WebhookPeriodLastSentCycleStart == nil || !after.WebhookPeriodLastSentCycleStart.UTC().Equal(previous) {
		t.Fatalf("expected the watermark restored to %s, got %v", previous, after.WebhookPeriodLastSentCycleStart)
	}

	// A stale release — the column no longer holds the anchor this pass wrote — is
	// a no-op, not an error, and leaves the newer value standing.
	if err := repo.ReleaseWebhookWatermark(context.Background(), user.ID, models.WebhookReminderTypePeriod, april, nil); err != nil {
		t.Fatalf("stale release must not error: %v", err)
	}
	after = reloadUserForWebhook(t, repo, user.ID)
	if after.WebhookPeriodLastSentCycleStart == nil || !after.WebhookPeriodLastSentCycleStart.UTC().Equal(previous) {
		t.Fatalf("a stale release must leave the newer watermark alone, got %v", after.WebhookPeriodLastSentCycleStart)
	}
}

// TestReleaseWebhookWatermarkRejectsUnknownType mirrors the claim's type guard:
// an unrecognized reminder kind is rejected before any column is touched.
func TestReleaseWebhookWatermarkRejectsUnknownType(t *testing.T) {
	repo := openWebhookRepoForTest(t)
	user := createUserForTimezoneTest(t, repo, "wh-release-badtype@example.com")

	if err := repo.ReleaseWebhookWatermark(context.Background(), user.ID, "not-a-real-kind", time.Now(), nil); err == nil {
		t.Fatal("expected an error for an unknown reminder type")
	}
	after := reloadUserForWebhook(t, repo, user.ID)
	if after.WebhookPeriodLastSentCycleStart != nil || after.WebhookOvulationLastSentCycleStart != nil {
		t.Fatal("a rejected type must not write any watermark column")
	}
}
