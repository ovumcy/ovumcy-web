package db

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/ovumcy/ovumcy-web/internal/models"
	"gorm.io/gorm"
)

type UserRepository struct {
	database *gorm.DB
}

func NewUserRepository(database *gorm.DB) *UserRepository {
	return &UserRepository{database: database}
}

func (repo *UserRepository) CountUsers(ctx context.Context) (int64, error) {
	var count int64
	if err := repo.database.WithContext(ctx).Model(&models.User{}).Count(&count).Error; err != nil {
		return 0, err
	}
	return count, nil
}

func (repo *UserRepository) ListOperatorUserSummaries(ctx context.Context) ([]models.OperatorUserSummary, error) {
	summaries := make([]models.OperatorUserSummary, 0)
	if err := repo.database.WithContext(ctx).
		Model(&models.User{}).
		Select("id", "display_name", "email", "role", "onboarding_completed", "created_at").
		Order("created_at ASC").
		Order("id ASC").
		Find(&summaries).Error; err != nil {
		return nil, err
	}
	return summaries, nil
}

func (repo *UserRepository) FindByID(ctx context.Context, userID uint) (models.User, error) {
	var user models.User
	if err := repo.database.WithContext(ctx).First(&user, userID).Error; err != nil {
		return models.User{}, err
	}
	return user, nil
}

func (repo *UserRepository) FindByNormalizedEmail(ctx context.Context, email string) (models.User, error) {
	var user models.User
	if err := repo.database.WithContext(ctx).Where("lower(trim(email)) = ?", email).First(&user).Error; err != nil {
		return models.User{}, err
	}
	return user, nil
}

func (repo *UserRepository) FindByNormalizedEmailOptional(ctx context.Context, email string) (models.User, bool, error) {
	var user models.User
	if err := repo.database.WithContext(ctx).Where("lower(trim(email)) = ?", email).First(&user).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return models.User{}, false, nil
		}
		return models.User{}, false, err
	}
	return user, true, nil
}

func (repo *UserRepository) ExistsByNormalizedEmail(ctx context.Context, email string) (bool, error) {
	var matched int64
	if err := repo.database.WithContext(ctx).Model(&models.User{}).
		Where("lower(trim(email)) = ?", email).
		Count(&matched).Error; err != nil {
		return false, err
	}
	return matched > 0, nil
}

func (repo *UserRepository) Create(ctx context.Context, user *models.User) error {
	err := repo.database.WithContext(ctx).Create(user).Error
	return classifyUserCreateError(err)
}

func (repo *UserRepository) CreateUserWithSymptoms(ctx context.Context, user *models.User, symptoms []models.SymptomType) error {
	return repo.database.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		if err := classifyUserCreateError(tx.Create(user).Error); err != nil {
			return err
		}
		if len(symptoms) == 0 {
			return nil
		}

		prepared := make([]models.SymptomType, len(symptoms))
		copy(prepared, symptoms)
		for index := range prepared {
			prepared[index].UserID = user.ID
		}

		if err := tx.Create(&prepared).Error; err != nil {
			return &SymptomSeedError{Err: err}
		}
		return nil
	})
}

func (repo *UserRepository) Save(ctx context.Context, user *models.User) error {
	return repo.database.WithContext(ctx).Save(user).Error
}

func (repo *UserRepository) UpdateDisplayName(ctx context.Context, userID uint, displayName string) error {
	return repo.database.WithContext(ctx).Model(&models.User{}).Where("id = ?", userID).Update("display_name", displayName).Error
}

// UpdateUserTimezone persists the owner's IANA timezone name (e.g.
// "Europe/Belgrade"), scoped strictly to userID. The caller (the settings
// service) is responsible for passing only a value that has already cleared the
// request-timezone validator; this method never validates and only writes the
// single column. It touches no security-posture field, so it deliberately does
// not bump auth_session_version.
func (repo *UserRepository) UpdateUserTimezone(ctx context.Context, userID uint, timezone string) error {
	return repo.database.WithContext(ctx).Model(&models.User{}).Where("id = ?", userID).Update("timezone", timezone).Error
}

// UpdateReminderLeadDays persists the owner's shared reminder lead window
// (users.reminder_lead_days, issue #123) scoped strictly to userID. The caller
// (SettingsService) is responsible for passing an already-clamped value
// (services.NormalizeReminderLeadDays); this method writes the single column
// only. Like the webhook-settings save path it deliberately does NOT bump
// auth_session_version — a reminder preference is not a change to the account's
// security posture, so no active session should be revoked.
func (repo *UserRepository) UpdateReminderLeadDays(ctx context.Context, userID uint, leadDays int) error {
	return repo.database.WithContext(ctx).Model(&models.User{}).Where("id = ?", userID).Update("reminder_lead_days", leadDays).Error
}

// SaveWebhookSettings persists an owner's webhook notification settings
// (issue #124), scoped strictly to userID. The webhook_url value MUST already
// be ciphertext — the caller (WebhookSettingsService) encrypts the plaintext
// endpoint before this method runs, so persistence never writes a plaintext
// URL. It touches only the notification-settings columns, deliberately NOT
// bumping auth_session_version: a notification-preference change is not a change
// to the account's security posture, so no active session should be revoked.
// It does not clear the *_last_sent_cycle_start watermarks — those are owned by
// the future notify pass, not by a settings edit.
func (repo *UserRepository) SaveWebhookSettings(ctx context.Context, userID uint, settings models.WebhookSettingsColumns) error {
	return repo.database.WithContext(ctx).Model(&models.User{}).Where("id = ?", userID).Updates(map[string]any{
		"webhook_enabled":          settings.Enabled,
		"webhook_url":              settings.EncryptedURL,
		"webhook_notify_period":    settings.NotifyPeriod,
		"webhook_notify_ovulation": settings.NotifyOvulation,
		"reminder_lead_days":       settings.ReminderLeadDays,
	}).Error
}

// ListAllForNotify returns the per-owner projection a future request-free batch
// pass needs to decide and send webhook reminders (issue #124). It selects a
// deliberately narrow column whitelist — cycle-prediction inputs, the webhook
// settings, the per-kind watermarks, the encrypted URL, and the timezone — and
// nothing else, so the batch query never over-reads sensitive per-account data.
// This is a dedicated method, NOT an overload of LoadSettingsByID (which stays
// the single settings whitelist). webhook_url is returned as CIPHERTEXT;
// decrypt via WebhookSettingsService.DecryptWebhookURL.
func (repo *UserRepository) ListAllForNotify(ctx context.Context) ([]models.WebhookNotifyRecord, error) {
	records := make([]models.WebhookNotifyRecord, 0)
	if err := repo.database.WithContext(ctx).
		Model(&models.User{}).
		Select(
			"id",
			"cycle_length",
			"period_length",
			"luteal_phase",
			"irregular_cycle",
			"unpredictable_cycle",
			"last_period_start",
			"timezone",
			"webhook_enabled",
			"webhook_url",
			"webhook_notify_period",
			"webhook_notify_ovulation",
			"reminder_lead_days",
			"webhook_period_last_sent_cycle_start",
			"webhook_ovulation_last_sent_cycle_start",
		).
		Order("id ASC").
		Find(&records).Error; err != nil {
		return nil, err
	}
	return records, nil
}

// webhookWatermarkColumns maps a reminder kind to its watermark column. Only
// these two kinds have a watermark; any other value is rejected so a typo can
// never write an unexpected column.
var webhookWatermarkColumns = map[string]string{
	models.WebhookReminderTypePeriod:    "webhook_period_last_sent_cycle_start",
	models.WebhookReminderTypeOvulation: "webhook_ovulation_last_sent_cycle_start",
}

// UpdateWebhookWatermark advances the per-kind "last sent" watermark for one
// owner after a SUCCESSFUL webhook delivery (issue #124, slice 3), scoped
// strictly to userID. reminderType selects the column (period/ovulation);
// cycleAnchor is the cycle-start the reminder covered.
//
// cycleAnchor is canonicalized to UTC-midnight HERE because this write uses
// Updates(map[string]any{...}), which bypasses the model's BeforeSave hook that
// normally does it — so a raw location-bearing time would otherwise be stored
// verbatim and could compare unequal to a UTC-midnight anchor on the next pass,
// breaking idempotency. It touches only the one watermark column: NOT
// auth_session_version (advancing a send watermark is not a security-posture
// change) and NOT any other setting.
func (repo *UserRepository) UpdateWebhookWatermark(ctx context.Context, userID uint, reminderType string, cycleAnchor time.Time) error {
	column, ok := webhookWatermarkColumns[reminderType]
	if !ok {
		return fmt.Errorf("unknown webhook reminder type %q", reminderType)
	}
	year, month, day := cycleAnchor.Date()
	anchorUTC := time.Date(year, month, day, 0, 0, 0, 0, time.UTC)
	return repo.database.WithContext(ctx).Model(&models.User{}).Where("id = ?", userID).Update(column, anchorUTC).Error
}

// SaveCalendarFeedToken sets (creates or rotates) the calendar-feed token
// columns for one owner, scoped strictly to userID. The VerifierHash MUST
// already be a bcrypt hash — the caller (a later slice) hashes the secret
// verifier via services.GenerateCalendarFeedToken before this method runs, so
// persistence never writes the verifier plaintext. Selector is the non-secret,
// UNIQUE-indexed lookup id.
//
// Rotation reuses this method: writing a fresh (selector, verifierHash) pair
// overwrites the previous one, so the old token stops verifying (its verifier no
// longer matches the new hash, and its selector no longer resolves). It touches
// only the two feed-token columns and deliberately does NOT bump
// auth_session_version: a feed token is a per-surface capability, not an account
// credential, so rotating it must not revoke the owner's login sessions.
func (repo *UserRepository) SaveCalendarFeedToken(ctx context.Context, userID uint, columns models.CalendarFeedTokenColumns) error {
	return repo.database.WithContext(ctx).Model(&models.User{}).Where("id = ?", userID).Updates(map[string]any{
		"calendar_feed_selector":      columns.Selector,
		"calendar_feed_verifier_hash": columns.VerifierHash,
	}).Error
}

// ClearCalendarFeedToken revokes an owner's calendar feed by NULLing both feed
// token columns, scoped strictly to userID. After this the feed URL 404s (its
// selector resolves no row). Like SaveCalendarFeedToken it does not bump
// auth_session_version — revoking a per-surface capability is not a change to the
// account's login security posture. Uses a typed nil so the columns become SQL
// NULL (feed off), matching the "both NULL = off" default.
func (repo *UserRepository) ClearCalendarFeedToken(ctx context.Context, userID uint) error {
	return repo.database.WithContext(ctx).Model(&models.User{}).Where("id = ?", userID).Updates(map[string]any{
		"calendar_feed_selector":      nil,
		"calendar_feed_verifier_hash": nil,
	}).Error
}

// FindByCalendarFeedSelector resolves the single owner whose calendar_feed_selector
// equals selector, for the by-selector feed lookup (a later slice). It returns
// (user, true, nil) on a hit and (zero, false, nil) when no row matches — the
// same not-found shape as FindByNormalizedEmailOptional — so the caller can keep
// a missing selector and a wrong verifier observationally identical (no oracle).
//
// An empty selector is treated as an immediate miss and never hits the database:
// a feed-off row stores NULL (not the empty string), and an equality match on the
// empty string would never match a NULL column anyway, but the guard makes the
// intent explicit and avoids a pointless query. The returned user carries
// CalendarFeedVerifierHash so the caller can constant-time-verify the verifier
// half without a second read.
func (repo *UserRepository) FindByCalendarFeedSelector(ctx context.Context, selector string) (models.User, bool, error) {
	if selector == "" {
		return models.User{}, false, nil
	}
	var user models.User
	if err := repo.database.WithContext(ctx).Where("calendar_feed_selector = ?", selector).First(&user).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return models.User{}, false, nil
		}
		return models.User{}, false, err
	}
	return user, true, nil
}

// UpdateRecoveryCodeHashAndRevokeSessions rotates the recovery-code hash and
// bumps auth_session_version in one atomic update (recovery-code regeneration).
//
// It ALSO force-clears the calendar-feed token in the SAME Updates() — the
// compromise arm of the approved force-rotate-on-recovery rule. A feed token is
// a long-lived bearer capability that outlives login sessions, so regenerating
// the recovery code (a security-posture reset the owner performs when they
// suspect the account is compromised) must also disarm any feed URL that may
// have leaked. Clearing (not silently re-minting) is deliberate: the old URL
// dies immediately and the owner re-generates a fresh one afterward from
// settings, so a compromise never leaves a working feed behind. It lives in the
// same Updates() as the version bump so a partial failure can never revoke
// sessions while leaving the feed armed (or vice versa).
func (repo *UserRepository) UpdateRecoveryCodeHashAndRevokeSessions(ctx context.Context, userID uint, recoveryHash string) error {
	return repo.database.WithContext(ctx).Model(&models.User{}).Where("id = ?", userID).Updates(map[string]any{
		"recovery_code_hash":          recoveryHash,
		"calendar_feed_selector":      nil,
		"calendar_feed_verifier_hash": nil,
		"auth_session_version":        gorm.Expr("auth_session_version + 1"),
	}).Error
}

func (repo *UserRepository) UpdatePasswordAndRevokeSessions(ctx context.Context, userID uint, passwordHash string, mustChangePassword bool) error {
	return repo.database.WithContext(ctx).Model(&models.User{}).Where("id = ?", userID).Updates(map[string]any{
		"password_hash":        passwordHash,
		"must_change_password": mustChangePassword,
		"local_auth_enabled":   true,
		"auth_session_version": gorm.Expr("auth_session_version + 1"),
	}).Error
}

// ForceResetPasswordAndRevokeSessions is the operator-driven variant of
// UpdatePasswordAndRevokeSessions (the CLI `ovumcy reset` path). It rewrites the
// password hash, forces a change-on-next-login, bumps auth_session_version, AND
// force-clears the calendar-feed token — all in one atomic Updates().
//
// It is deliberately SEPARATE from UpdatePasswordAndRevokeSessions because that
// method is ALSO the routine authenticated password-change path
// (SettingsService.ChangePassword), which must NOT disarm the feed: a routine
// change is not a compromise event, and the owner keeps a manual rotate control.
// A forced operator reset, by contrast, is used to recover a compromised or
// locked-out account, so it is the operator-reset arm of the approved
// force-rotate-on-recovery rule: any feed URL that may have leaked is cleared in
// the same write that resets the credential and revokes sessions.
func (repo *UserRepository) ForceResetPasswordAndRevokeSessions(ctx context.Context, userID uint, passwordHash string) error {
	return repo.database.WithContext(ctx).Model(&models.User{}).Where("id = ?", userID).Updates(map[string]any{
		"password_hash":               passwordHash,
		"must_change_password":        true,
		"local_auth_enabled":          true,
		"calendar_feed_selector":      nil,
		"calendar_feed_verifier_hash": nil,
		"auth_session_version":        gorm.Expr("auth_session_version + 1"),
	}).Error
}

// UpdatePasswordHashOnly rewrites only the password_hash column without bumping
// auth_session_version and without touching must_change_password or
// local_auth_enabled. It exists for the transparent bcrypt-cost upgrade the
// auth service performs after a successful login (mirrors
// UpdateTOTPSecretCiphertext for the TOTP secret): the account's security
// posture is unchanged — same password, stronger hash — so no active session
// should be revoked by what is an internal storage upgrade.
func (repo *UserRepository) UpdatePasswordHashOnly(ctx context.Context, userID uint, passwordHash string) error {
	return repo.database.WithContext(ctx).Model(&models.User{}).Where("id = ?", userID).Update("password_hash", passwordHash).Error
}

func (repo *UserRepository) UpdatePasswordRecoveryCodeAndRevokeSessions(ctx context.Context, userID uint, passwordHash string, recoveryHash string, mustChangePassword bool) error {
	return repo.database.WithContext(ctx).Model(&models.User{}).Where("id = ?", userID).Updates(map[string]any{
		"password_hash":        passwordHash,
		"recovery_code_hash":   recoveryHash,
		"must_change_password": mustChangePassword,
		"local_auth_enabled":   true,
		"auth_session_version": gorm.Expr("auth_session_version + 1"),
	}).Error
}

// UpdatePasswordRecoveryCodeAndRevokeSessionsCAS is the single-use variant
// used by the password-reset flow. It adds a CAS predicate — `WHERE id = ?
// AND password_hash = <oldHash>` — so concurrent or replayed redeems of the
// same reset token both race to write the new hash and only one wins.
//
// The token embeds a fingerprint of the password_hash at issuance time
// (IsPasswordStateFingerprintMatch). If two requests arrive simultaneously
// with the same valid token, the first UPDATE wins (RowsAffected == 1) and
// writes the new hash; the second finds password_hash != oldHash and affects
// 0 rows, returning ErrResetTokenAlreadyConsumed.
//
// Returns ErrResetTokenAlreadyConsumed when RowsAffected == 0 (token was
// already redeemed or the password state changed since the token was issued).
//
// It ALSO force-clears the calendar-feed token in the SAME Updates() — the
// password-reset arm of the approved force-rotate-on-recovery rule. A reset via
// recovery code is a compromise-recovery event (the owner lost the password), so
// any feed URL that may have leaked alongside the password is disarmed in the
// same atomic write that rotates the credential and revokes sessions; the owner
// re-generates a fresh feed afterward. Because the clear rides the same CAS
// UPDATE, a replayed/concurrent redeem that loses the race (RowsAffected == 0)
// neither rotates the credential nor clears the feed — both stay consistent.
func (repo *UserRepository) UpdatePasswordRecoveryCodeAndRevokeSessionsCAS(ctx context.Context, userID uint, oldPasswordHash string, newPasswordHash string, recoveryHash string) error {
	result := repo.database.WithContext(ctx).Model(&models.User{}).
		Where("id = ? AND password_hash = ?", userID, oldPasswordHash).
		Updates(map[string]any{
			"password_hash":               newPasswordHash,
			"recovery_code_hash":          recoveryHash,
			"must_change_password":        false,
			"local_auth_enabled":          true,
			"calendar_feed_selector":      nil,
			"calendar_feed_verifier_hash": nil,
			"auth_session_version":        gorm.Expr("auth_session_version + 1"),
		})
	if result.Error != nil {
		return result.Error // codecov:ignore -- DB-layer error on the CAS UPDATE; not reachable in unit tests
	}
	if result.RowsAffected == 0 {
		return ErrResetTokenAlreadyConsumed
	}
	return nil
}

func (repo *UserRepository) BumpAuthSessionVersion(ctx context.Context, userID uint) error {
	return repo.database.WithContext(ctx).Model(&models.User{}).Where("id = ?", userID).UpdateColumn("auth_session_version", gorm.Expr("auth_session_version + 1")).Error
}

// UpdateTOTPFieldsAndRevokeSessions atomically rewrites the TOTP-related
// columns and increments auth_session_version, so every active auth cookie
// for the user is invalidated in the same transaction. Both 2FA enable and
// disable change the account's auth posture and therefore must invalidate
// any session that was issued before the change.
func (repo *UserRepository) UpdateTOTPFieldsAndRevokeSessions(ctx context.Context, userID uint, encryptedSecret string, enabled bool) error {
	return repo.database.WithContext(ctx).Model(&models.User{}).Where("id = ?", userID).Updates(map[string]any{
		"totp_secret":          encryptedSecret,
		"totp_enabled":         enabled,
		"totp_last_used_step":  0,
		"auth_session_version": gorm.Expr("auth_session_version + 1"),
	}).Error
}

// UpdateTOTPSecretCiphertext rewrites only the encrypted TOTP secret column
// without bumping auth_session_version and without touching totp_enabled or
// totp_last_used_step. It exists for transparent re-encryption of legacy
// (pre-aad-binding) ciphertexts under the current aad-bound format: the
// account's security posture has not changed, so no active session should
// be revoked by what is otherwise an internal storage upgrade.
func (repo *UserRepository) UpdateTOTPSecretCiphertext(ctx context.Context, userID uint, encryptedSecret string) error {
	return repo.database.WithContext(ctx).Model(&models.User{}).Where("id = ?", userID).Update("totp_secret", encryptedSecret).Error
}

// ClaimTOTPStep atomically claims a TOTP step for the given user. Returns true
// iff the row was updated, i.e. the persisted totp_last_used_step was strictly
// less than `step` at the moment of the UPDATE. Replays and concurrent losers
// observe RowsAffected == 0 and get false.
func (repo *UserRepository) ClaimTOTPStep(ctx context.Context, userID uint, step int64) (bool, error) {
	result := repo.database.WithContext(ctx).Model(&models.User{}).
		Where("id = ? AND totp_last_used_step < ?", userID, step).
		Update("totp_last_used_step", step)
	if result.Error != nil {
		return false, result.Error
	}
	return result.RowsAffected == 1, nil
}

func (repo *UserRepository) UpdateByID(ctx context.Context, userID uint, updates map[string]any) error {
	return repo.database.WithContext(ctx).Model(&models.User{}).Where("id = ?", userID).Updates(updates).Error
}

func (repo *UserRepository) LoadSettingsByID(ctx context.Context, userID uint) (models.User, error) {
	var user models.User
	if err := repo.database.WithContext(ctx).
		Select(
			"cycle_length",
			"period_length",
			"luteal_phase",
			"auto_period_fill",
			"local_auth_enabled",
			"irregular_cycle",
			"track_bbt",
			"temperature_unit",
			"track_cervical_mucus",
			"hide_sex_chip",
			"hide_cycle_factors",
			"hide_notes_field",
			"show_historical_phases",
			"week_starts_on",
			"shown_period_tip",
			"age_group",
			"usage_goal",
			"unpredictable_cycle",
			"long_period_warning_cycle_start",
			"last_period_start",
			"reminder_lead_days",
			// Webhook notification settings (issue #124) load here so the
			// settings page can render the write-only URL field's status
			// (configured + host) and the enable/notify toggles. webhook_url is
			// CIPHERTEXT — the settings view decrypts only to extract the host.
			"webhook_enabled",
			"webhook_url",
			"webhook_notify_period",
			"webhook_notify_ovulation",
		).
		First(&user, userID).Error; err != nil {
		return models.User{}, err
	}
	return user, nil
}

func (repo *UserRepository) SaveOnboardingStep1(ctx context.Context, userID uint, start time.Time) error {
	return repo.database.WithContext(ctx).Model(&models.User{}).Where("id = ?", userID).Updates(map[string]any{
		"last_period_start": start,
	}).Error
}

func (repo *UserRepository) SaveOnboardingStep2(ctx context.Context, userID uint, cycleLength int, periodLength int, autoPeriodFill bool, irregularCycle bool, ageGroup string, usageGoal string) error {
	return repo.database.WithContext(ctx).Model(&models.User{}).Where("id = ?", userID).Updates(map[string]any{
		"cycle_length":     cycleLength,
		"period_length":    periodLength,
		"luteal_phase":     14,
		"auto_period_fill": autoPeriodFill,
		"irregular_cycle":  irregularCycle,
		"age_group":        ageGroup,
		"usage_goal":       usageGoal,
	}).Error
}

func (repo *UserRepository) ClearAllDataAndResetSettings(ctx context.Context, userID uint) error {
	return repo.database.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		if err := tx.Where("user_id = ?", userID).Delete(&models.DailyLog{}).Error; err != nil {
			return err
		}
		if err := tx.Where("user_id = ? AND is_builtin = ?", userID, false).Delete(&models.SymptomType{}).Error; err != nil {
			return err
		}
		return tx.Model(&models.User{}).Where("id = ?", userID).Updates(map[string]any{
			"cycle_length":                    models.DefaultCycleLength,
			"period_length":                   models.DefaultPeriodLength,
			"luteal_phase":                    14,
			"auto_period_fill":                true,
			"irregular_cycle":                 false,
			"track_bbt":                       false,
			"temperature_unit":                "c",
			"track_cervical_mucus":            false,
			"hide_sex_chip":                   false,
			"hide_cycle_factors":              false,
			"hide_notes_field":                false,
			"show_historical_phases":          false,
			"week_starts_on":                  models.DefaultWeekStart,
			"shown_period_tip":                false,
			"age_group":                       models.AgeGroupUnknown,
			"usage_goal":                      models.UsageGoalHealth,
			"unpredictable_cycle":             false,
			"long_period_warning_cycle_start": nil,
			"last_period_start":               nil,
			// Webhook notification settings (issue #124) are owner data: a
			// clear-data wipe disarms delivery, clears the encrypted endpoint,
			// resets the shared lead window to its default, and clears the
			// per-kind watermarks so no stale reminder fires against the freshly
			// emptied account. The per-kind opt-ins return to their column
			// defaults (both true) to match a fresh account.
			"webhook_enabled":                         false,
			"webhook_url":                             "",
			"webhook_notify_period":                   true,
			"webhook_notify_ovulation":                true,
			"webhook_period_last_sent_cycle_start":    nil,
			"webhook_ovulation_last_sent_cycle_start": nil,
			"reminder_lead_days":                      models.DefaultReminderLeadDays,
			// Calendar (.ics) feed token: a clear-data wipe revokes the feed by
			// NULLing both columns, so any previously-issued feed URL 404s against
			// the freshly emptied account (its selector no longer resolves). This
			// is the data-reset arm of the approved force-rotate-on-recovery rule;
			// the password-reset / operator-reset / recovery-regen force-rotate
			// hooks are a later slice.
			"calendar_feed_selector":      nil,
			"calendar_feed_verifier_hash": nil,
			// Bump auth_session_version inside the same transaction so a
			// successful clear-data wipe also revokes every auth cookie that
			// existed before the wipe. Without this bump a stolen session that
			// was used to trigger the wipe would retain authenticated access
			// to the freshly-empty account, and a legitimate "panic clear"
			// gesture would not actually sign other devices out.
			"auth_session_version": gorm.Expr("auth_session_version + 1"),
		}).Error
	})
}

func (repo *UserRepository) DeleteAccountAndRelatedData(ctx context.Context, userID uint) error {
	err := repo.database.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		if err := tx.Where("user_id = ?", userID).Delete(&models.DailyLog{}).Error; err != nil {
			return err
		}
		if err := tx.Where("user_id = ?", userID).Delete(&models.SymptomType{}).Error; err != nil {
			return err
		}
		// register_pickup_tokens carries no foreign key, and oidc_identities
		// relies on ON DELETE CASCADE. Delete both explicitly so account
		// erasure stays complete (no orphaned auth-linkage rows) and does not
		// depend on foreign_keys being enforced — GDPR erasure must hold even
		// if the FK pragma is ever disabled.
		if err := tx.Where("user_id = ?", userID).Delete(&models.RegisterPickupToken{}).Error; err != nil {
			return err
		}
		if err := tx.Where("user_id = ?", userID).Delete(&models.OIDCIdentity{}).Error; err != nil {
			return err
		}
		// oidc_logout_states has NO user_id column — rows are keyed only by
		// session_id (a random nonce) and carry an id_token_hint JWT that
		// contains the user's OIDC sub/email. We cannot identify which rows
		// belong to the deleted user from inside this transaction.
		//
		// Residual limitation: any unexpired oidc_logout_state rows minted
		// for the deleted user's sessions survive until their own TTL
		// expires — up to the full logout-state TTL (currently 7 days, see
		// services.defaultOIDCLogoutStateTTL). This is accepted: the rows
		// are inaccessible without the original session cookie and carry no
		// PII beyond what the OIDC provider already holds.
		return tx.Delete(&models.User{}, userID).Error
	})
	if err != nil {
		return err
	}
	// Best-effort housekeeping after the erasure has committed: purge all
	// globally expired logout-state rows so the table does not accumulate
	// stale data. This must NOT run inside the erasure transaction — on
	// Postgres any errored statement poisons the transaction (SQLSTATE
	// 25P02), so an "ignored" purge failure would abort the erasure itself.
	// The error is intentionally dropped here: a purge failure must not turn
	// a completed erasure into a reported failure.
	_ = repo.database.WithContext(ctx).Where("expires_at <= ?", time.Now().UTC()).Delete(&models.OIDCLogoutState{}).Error
	return nil
}

func (repo *UserRepository) CompleteOnboarding(ctx context.Context, userID uint, startDay time.Time, periodLength int, autoPeriodFill bool) error {
	if periodLength <= 0 {
		return errors.New("invalid period length")
	}
	endDay := startDay.AddDate(0, 0, periodLength-1)
	if endDay.Before(startDay) {
		return errors.New("invalid onboarding range")
	}

	return repo.database.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		if autoPeriodFill {
			for cursor := startDay; !cursor.After(endDay); cursor = cursor.AddDate(0, 0, 1) {
				dayStart := cursor
				dayEnd := dayStart.AddDate(0, 0, 1)

				var entry models.DailyLog
				result := tx.
					Where("user_id = ? AND date >= ? AND date < ?", userID, dayStart, dayEnd).
					Order("date DESC, id DESC").
					First(&entry)
				if errors.Is(result.Error, gorm.ErrRecordNotFound) {
					entry = models.DailyLog{
						UserID:        userID,
						Date:          dayStart,
						IsPeriod:      true,
						Flow:          models.FlowNone,
						SexActivity:   models.SexActivityNone,
						CervicalMucus: models.CervicalMucusNone,
						PregnancyTest: models.PregnancyTestNone,
						SymptomIDs:    []uint{},
					}
					if err := tx.Create(&entry).Error; err != nil {
						return err
					}
					continue
				}
				if result.Error != nil {
					return result.Error
				}

				if err := tx.Model(&entry).Updates(map[string]any{
					"is_period": true,
					"flow":      models.FlowNone,
				}).Error; err != nil {
					return err
				}
			}
		}

		return tx.Model(&models.User{}).Where("id = ?", userID).Updates(map[string]any{
			"last_period_start":    startDay,
			"onboarding_completed": true,
		}).Error
	})
}
