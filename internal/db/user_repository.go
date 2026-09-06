package db

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/ovumcy/ovumcy-web/internal/models"
	"gorm.io/gorm"
)

// CalendarFeedFence records, OUTSIDE the database, that the set of armed
// calendar feeds just changed. Every write below that arms, rotates or removes
// a feed calls it, in the same shape and for the same reason the webhook
// revocation epoch is advanced by every writer of a delivery configuration: a
// revocation that exists only inside the database is undone by restoring a
// backup taken before it, and the restored rows carry no sign that it happened.
//
// Implemented by services.CalendarFeedRestoreFence; declared here so this layer
// depends on the behaviour rather than on the layer above it.
type CalendarFeedFence interface {
	Advance(ctx context.Context) error
}

type UserRepository struct {
	database *gorm.DB
	// calendarFeedFence is nil in tests and in any binary that provably never
	// changes feed state; the advance is then a no-op. Production wiring goes
	// through bootstrap.BuildRepositories, which always attaches one.
	calendarFeedFence CalendarFeedFence
}

func NewUserRepository(database *gorm.DB) *UserRepository {
	return &UserRepository{database: database}
}

// advanceCalendarFeedFence is called by every write that changes which calendar
// feeds are armed. Where it sits relative to that write is decided by which
// mistake costs more, and the two cases genuinely differ:
//
//   - SaveCalendarFeedToken and ClearCalendarFeedToken advance FIRST. There the
//     write IS the revocation, and the rule is that the fence may never be
//     BEHIND the row state. Advancing after leaves a window in which the row
//     says revoked and the fence does not, and a restore across that window
//     revives the feed — the defect this whole mechanism exists for. Advancing
//     first cannot produce it: a crash in the window leaves the fence ahead of
//     a row that never changed, which is a still-armed feed and a revocation
//     the owner never saw succeed, so they retry it.
//
//     Note what this does NOT rest on. After an advance the two halves AGREE,
//     so no later boot disarms on its account; the safety comes from the
//     ordering itself, never from a boot-time backstop.
//
//     What it does not close is a backup taken INSIDE the window. The advance
//     and the row write are separate transactions, so a consistent snapshot
//     between them holds the new token beside a row that is still armed, and
//     restoring that snapshot compares equal and revives the feed. The window
//     is sub-millisecond and the runbook takes backups with the app stopped,
//     which is why it is named rather than engineered away; the reverse order
//     trades it for the strictly worse crash window described above.
//
//   - Everything else advances AFTER. There the feed clear is a side effect of
//     a credential rotation, a clear-data wipe or an erasure, and advancing
//     first would refuse the whole operation whenever the fence's own write
//     fails — a password change blocked by a calendar-feed marker. That trade
//     is not worth closing a window these paths only touch incidentally, and
//     their containment does not rest on the feed (the same statement bumps
//     auth_session_version). The window is named as a residual risk rather
//     than engineered away.
//
//     Every "advances AFTER" caller drops this call's error (`_ =`) rather
//     than returning it, because by the time it runs their own write has
//     already committed: reporting a completed password reset, recovery-code
//     rotation or clear-data as failed would be a bigger lie than the missing
//     fence record. The cost is bounded either way the fence can actually
//     fail — a write that moved the file but not app_state leaves the halves
//     disagreeing, and a write that moved neither leaves the fence unanchored
//     — and Enforce answers both by disarming every armed feed on the next
//     boot, never by losing a revocation silently. This reasoning is the
//     server's own; it holds only for a write acting on the account's OWN
//     feed, never for the operator CLI acting on someone else's, which is why
//     AdvanceConfirmed (internal/services) exists and refuses instead of
//     degrading.
//
// Deliberately NOT called by the two boot-time bulk disarms: the restore fence
// records its own token immediately after its disarm, and the key-rotation
// sentinel's disarm is answered by the key epoch, which a restore brings back
// together with the rows it belongs to. Nor by BackfillCalendarFeedVerifierMAC,
// which neither grants nor removes access — it rewrites a derived column for a
// token that already verified, on a read path taken by every first poll.
func (repo *UserRepository) advanceCalendarFeedFence(ctx context.Context) error {
	if repo.calendarFeedFence == nil {
		return nil
	}
	return repo.calendarFeedFence.Advance(ctx)
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

func (repo *UserRepository) FindByIDOptional(ctx context.Context, userID uint) (models.User, bool, error) {
	var user models.User
	if err := repo.database.WithContext(ctx).First(&user, userID).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return models.User{}, false, nil
		}
		return models.User{}, false, err
	}
	return user, true, nil
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

// FindAllByNormalizedEmail returns every row matching the normalized address,
// ordered by id, instead of gorm's arbitrary first match. A legacy database
// can hold more than one: two accounts on one mailbox is exactly the case the
// boot-time email renormalizer leaves standing (RenormalizeUserEmail) when it
// keeps the older row's address and locks the newer one out. A caller acting
// on a single row must use this, not FindByNormalizedEmailOptional, wherever
// picking the wrong one silently would be a mistake worth refusing instead.
func (repo *UserRepository) FindAllByNormalizedEmail(ctx context.Context, email string) ([]models.User, error) {
	var users []models.User
	if err := repo.database.WithContext(ctx).
		Where("lower(trim(email)) = ?", email).
		Order("id ASC").
		Find(&users).Error; err != nil {
		return nil, err
	}
	return users, nil
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

// ExistsByNormalizedEmailExcludingUser reports whether any user OTHER than
// excludeUserID already answers to the normalized email. The boot-time email
// renormalizer needs the self-exclusion: rewriting a row whose stored value
// differs from the canonical form only by case or whitespace would otherwise
// see the row itself as the "conflict" and skip the repair.
func (repo *UserRepository) ExistsByNormalizedEmailExcludingUser(ctx context.Context, email string, excludeUserID uint) (bool, error) {
	var count int64
	err := repo.database.WithContext(ctx).Model(&models.User{}).
		Where("lower(trim(email)) = ? AND id != ?", email, excludeUserID).
		Count(&count).Error
	return count > 0, err
}

// RenormalizeUserEmail rewrites one user's stored email to its canonical form
// under a compare-and-set on the previous value: if the row changed between
// the read and this write, zero rows match and the caller sees changed=false
// instead of clobbering a concurrent update. It deliberately does not bump
// auth_session_version — the mailbox identity is unchanged, only its stored
// spelling; this is a repair, not a credential rotation.
func (repo *UserRepository) RenormalizeUserEmail(ctx context.Context, userID uint, fromEmail string, toEmail string) (bool, error) {
	result := repo.database.WithContext(ctx).Model(&models.User{}).
		Where("id = ? AND email = ?", userID, fromEmail).
		Update("email", toEmail)
	return result.RowsAffected == 1, result.Error
}

// SetUserEmailByIDAndRevokeSessions re-homes ONE account to a new address, and
// unlike RenormalizeUserEmail above it DOES bump auth_session_version, in the
// same statement: the stored email is the login identity — the value both
// AuthenticateCredentials and the OIDC email match resolve an account by — so
// changing it is a change to the account's security posture, and no cookie
// issued against the old identity may keep resolving.
//
// The CAS on the previous value is what makes it safe to run against an
// instance that is up: fromEmail is the exact string the operator was shown by
// `users list`, so a row that moved between that listing and this write matches
// zero rows and the caller reports a conflict instead of overwriting whatever
// stands there now. Uniqueness is not decided here — the unique index on
// lower(trim(email)) is, and its violation surfaces as the error.
//
// It deliberately leaves the calendar-feed columns alone. Re-homing an address
// is a repair of a locked-out account, not a compromise event, so it follows
// the routine-password-change arm of the force-rotate rule: the feed capability
// belongs to the account, not to the address, and the owner keeps the manual
// rotate control.
func (repo *UserRepository) SetUserEmailByIDAndRevokeSessions(ctx context.Context, userID uint, fromEmail string, toEmail string) (bool, error) {
	result := repo.database.WithContext(ctx).Model(&models.User{}).
		Where("id = ? AND email = ?", userID, fromEmail).
		Updates(map[string]any{
			"email":                toEmail,
			"auth_session_version": gorm.Expr("auth_session_version + 1"),
		})
	if result.Error != nil {
		return false, classifyUniqueConstraintError(result.Error, "users.email")
	}
	return result.RowsAffected == 1, nil
}

// requireOwnerRole is the persistence half of the owner-role-only boundary, and
// it runs on both account-creating methods below — the only two writes in this
// package that put a role into the users table.
//
// The column's CHECK still admits 'partner', a role the product does not have:
// no constant declares it, no handler or template reads it, and narrowing the
// constraint would mean rebuilding the users table on SQLite, which has no
// ALTER for a CHECK. That rebuild would DROP the account table, and with
// foreign_keys=ON a DROP TABLE fires every ON DELETE CASCADE hanging off it, so
// the migration that tightened one unused constant would delete every day log
// on the instance. The value is therefore refused where it is written instead
// of where it is stored, which is also the only place any current caller can
// reach: containment used to be web-policy only
// (`docs/SECURITY_INVARIANTS.md` → the owner-role-only privacy boundary), and a
// row carrying another role would be accepted by the database and then read by
// code written on the assumption that owner is the only role.
//
// An empty role is the one value that is not a rejection: it means "take the
// column default", which is owner. It is written out explicitly so the row
// never depends on the gorm tag and the migration default agreeing.
func requireOwnerRole(user *models.User) error {
	if user == nil {
		return ErrUnsupportedUserRole
	}
	if user.Role == "" {
		user.Role = models.RoleOwner
		return nil
	}
	if user.Role != models.RoleOwner {
		return ErrUnsupportedUserRole
	}
	return nil
}

func (repo *UserRepository) Create(ctx context.Context, user *models.User) error {
	if err := requireOwnerRole(user); err != nil {
		return err
	}
	err := repo.database.WithContext(ctx).Create(user).Error
	return classifyUserCreateError(err)
}

func (repo *UserRepository) CreateUserWithSymptoms(ctx context.Context, user *models.User, symptoms []models.SymptomType) error {
	if err := requireOwnerRole(user); err != nil {
		return err
	}
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

// UpdateInterfaceLanguage persists the owner's chosen UI language
// (users.interface_language, migration 034) scoped strictly to userID. The
// caller (SettingsService) passes a code already validated against the shipped
// locale catalogue — this method never validates and writes the single column
// only. It touches no security-posture field, so it deliberately does not bump
// auth_session_version.
//
// It returns whether a row was actually updated. A settings save is the one
// caller, and for it a zero-row outcome means the account is gone: reporting it
// as "saved" would show the owner a success flash for a preference nothing
// stored. The distinction has to come from here, because an UPDATE that matches
// nothing is not an error to the driver.
func (repo *UserRepository) UpdateInterfaceLanguage(ctx context.Context, userID uint, language string) (bool, error) {
	result := repo.database.WithContext(ctx).Model(&models.User{}).
		Where("id = ?", userID).
		Update("interface_language", language)
	if result.Error != nil {
		return false, result.Error
	}
	return result.RowsAffected == 1, nil
}

// UpdateReminderLeadDays persists the owner's shared reminder lead window
// (users.reminder_lead_days, issue #123) scoped strictly to userID. The caller
// (SettingsService) is responsible for passing an already-clamped value
// (services.NormalizeReminderLeadDays); this method writes that column and the
// revocation epoch below, and no other setting. Like the webhook-settings save
// path it deliberately does NOT bump auth_session_version — a reminder
// preference is not a change to the account's security posture, so no active
// session should be revoked.
//
// It DOES advance webhook_config_version, for the same reason SaveWebhookSettings
// does. reminder_lead_days is SHARED: it is a dashboard-banner preference AND one
// of the columns ListAllForNotify projects, and decideDueReminders decides from
// it — narrowing the window from seven days to zero is the owner saying "not this
// early", and a pass holding the seven-day snapshot would otherwise still deliver
// against it. That is a smaller blast radius than a revoked endpoint and the same
// class, so this write joins the epoch rather than being excused from it: a rule
// applied at two of its three write sites is what leaves the third reachable.
//
// The cost of joining it is SaveWebhookSettings' cost, and this is the path that
// can set the lead window to zero, where that cost is worst: read it there before
// changing anything here. Regression:
// TestUpdateReminderLeadDaysAdvancesTheRevocationEpoch.
func (repo *UserRepository) UpdateReminderLeadDays(ctx context.Context, userID uint, leadDays int) error {
	return repo.database.WithContext(ctx).Model(&models.User{}).Where("id = ?", userID).Updates(map[string]any{
		"reminder_lead_days":     leadDays,
		"webhook_config_version": gorm.Expr("webhook_config_version + 1"),
	}).Error
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
//
// It DOES advance webhook_config_version, the revocation epoch, in the same
// statement. This is the one thing a settings save owes the notify pass, and it
// is owed precisely BECAUSE the watermarks are left alone: a save is also how an
// owner disables delivery, replaces the endpoint or removes it, and a pass that
// snapshotted the previous configuration would otherwise still satisfy the
// watermark compare-and-set and POST to the endpoint the owner was just told was
// gone. Riding the same UPDATE is what makes "the save landed" and "no older
// snapshot can still send" one event rather than two. Advancing (not stamping a
// value the caller chose) keeps it monotonic per row, so no epoch is ever handed
// back to a snapshot that already held it. Regression:
// TestSaveWebhookSettingsAdvancesTheRevocationEpoch,
// TestClaimWebhookWatermarkIsLostAfterTheOwnerDisabledDelivery.
//
// The advance is UNCONDITIONAL, including for a save that changes nothing — the
// settings form re-submitted untouched. The alternative is worse: comparing
// against the stored row to decide whether this save "counts" would put a
// revocation behind a value judgement made from a row read a moment earlier, and
// an egress gate errs toward refusing. (SettingsService.SaveReminderLeadDays
// skips its own no-op UPDATE before reaching persistence, so that saving is
// already taken where it can be taken safely.)
//
// The cost, stated at its worst rather than its typical: an in-flight pass loses
// its claim, and the reminder waits for a pass whose due-set still covers the
// anchor. At the default lead window that is the next day. At a lead window of
// ZERO — reachable, MinReminderLeadDays is 0 — the due test is
// "daysUntil >= 0 && daysUntil <= leadDays" (reminderWithinWindow), so the
// anchor is due on exactly one calendar day and there is no later pass to
// retry it: a save landing inside that one pass drops that cycle's reminder.
// Losing a reminder the owner was mid-way through reconfiguring is the
// fail-closed direction, but it is a dropped reminder and not a delay.
//
// It also decides the fate of webhook_last_delivered_at, in this same statement
// and never in a follow-up: a mark saying "a delivery to your endpoint was
// accepted" must not survive the endpoint it was about by even one read. The
// judgement is the caller's (settings.ClearLastDeliveredAt) because it needs the
// plaintext — persistence sees ciphertext, and a re-encryption of the same URL
// differs from it byte for byte. A save that leaves the destination alone leaves
// the mark alone, which is what a toggle-only save is. Regression:
// TestEveryWebhookURLWriterDecidesTheDeliveryMark.
func (repo *UserRepository) SaveWebhookSettings(ctx context.Context, userID uint, settings models.WebhookSettingsColumns) error {
	updates := map[string]any{
		"webhook_enabled":          settings.Enabled,
		"webhook_notify_period":    settings.NotifyPeriod,
		"webhook_notify_ovulation": settings.NotifyOvulation,
		"reminder_lead_days":       settings.ReminderLeadDays,
		"webhook_config_version":   gorm.Expr("webhook_config_version + 1"),
	}
	// A save that keeps the endpoint writes no endpoint. This is the one shape in
	// which this statement is not a webhook_url writer, so it is also the one
	// shape in which the delivery-mark judgement below has nothing to decide:
	// the mark still describes the column the row still holds.
	//
	// It also cannot arm. An endpoint kept because this instance could not read
	// it is one delivery must not run against, and forcing the flag here makes
	// that structural rather than a rule some caller has to remember: the service
	// refuses the combination too, and this is what holds if a second caller ever
	// arrives without it. The epoch still advances either way -- the per-kind
	// opt-ins and the enable flag ARE delivery configuration, so a pass holding
	// the previous snapshot has to lose its claim whether or not the endpoint
	// column moved.
	if settings.KeepEncryptedURL {
		updates["webhook_enabled"] = false
	} else {
		updates["webhook_url"] = settings.EncryptedURL
		if settings.ClearLastDeliveredAt {
			updates["webhook_last_delivered_at"] = nil
		}
	}
	return repo.database.WithContext(ctx).Model(&models.User{}).Where("id = ?", userID).Updates(updates).Error
}

// RemoveWebhookDestination withdraws this owner's delivery endpoint and nothing
// else, scoped strictly to userID. It exists because SaveWebhookSettings cannot
// express "remove the destination" without also writing reminder_lead_days,
// webhook_notify_period and webhook_notify_ovulation: a thin handler calling it
// with a zero-valued update would silently narrow the shared lead window to
// zero, which makes a cycle anchor due on exactly one calendar day with no later
// pass to retry it.
//
// It writes webhook_url and therefore decides the fate of webhook_last_delivered_at
// in the same statement: the mark says a delivery to THAT endpoint was accepted,
// so it cannot outlive it by a single read. It never decrypts the ciphertext it
// clears -- an endpoint the instance can no longer open is still an endpoint the
// owner may withdraw, and requiring a successful decrypt here would make the
// unreadable state unrevokable.
//
// It DOES advance webhook_config_version, the revocation epoch, for the reason
// migration 038 states: this is a write to the DELIVERY CONFIGURATION -- whether
// and where delivery happens -- so a pass already holding the previous snapshot
// loses its claim rather than posting to an endpoint the owner just withdrew.
// The per-kind opt-ins and the lead window are deliberately left where the owner
// set them: withdrawing an address is not a request to forget which reminders
// they wanted. Regression: TestRemoveWebhookDestinationLeavesTheKindsAndLeadWindowAlone,
// TestRemoveWebhookDestinationAdvancesTheRevocationEpoch.
func (repo *UserRepository) RemoveWebhookDestination(ctx context.Context, userID uint) error {
	return repo.database.WithContext(ctx).Model(&models.User{}).Where("id = ?", userID).Updates(map[string]any{
		"webhook_enabled":           false,
		"webhook_url":               "",
		"webhook_last_delivered_at": nil,
		"webhook_config_version":    gorm.Expr("webhook_config_version + 1"),
	}).Error
}

// MarkWebhookDelivered records that a delivery to this owner's endpoint was
// ACCEPTED — a 2xx already returned — scoped strictly to userID. It is the only
// writer of webhook_last_delivered_at that sets a value, and the notify pass
// calls it in exactly one place: after Deliver returns nil, never at the claim.
//
// It is a second write, and the claim is emphatically not a substitute for it.
// The claim happens BEFORE the POST, stores a cycle anchor rather than a clock
// reading, and stands for a send that was never accepted until the failure path
// releases it. Reading a watermark as a delivery time is the exact misreading
// this column was added to make impossible.
//
// configVersion is the revocation epoch the delivering pass's snapshot carried —
// the same value it handed ClaimWebhookWatermark — and pinning it here means a
// configuration revoked while the request was on the wire is not credited with a
// delivery. The pass cannot recall that request, but it can decline to record it
// against a configuration the owner has withdrawn.
//
// It does NOT advance that epoch, and that is deliberate rather than an
// oversight of migration 038's "a later writer owes the same advance": the
// obligation is on writers of the DELIVERY CONFIGURATION — whether, where and
// how early delivery happens — and this write changes none of those. Advancing
// here would also break the pass making the call, because the epoch is pinned
// once per owner outside the reminder loop: a bump after a period delivery would
// lose the ovulation claim of that same pass. Regression:
// TestMarkWebhookDeliveredLeavesTheRevocationEpochAlone.
//
// The stamp is monotonic — the predicate refuses a value not later than the one
// stored — so a late-returning delivery cannot walk the mark backwards over a
// newer one. A refused write affects zero rows and is not an error: the mark is
// already at least as current as this call would make it, or the configuration
// moved on. It writes ONE column: not the epoch it pins, not a watermark, not
// auth_session_version.
func (repo *UserRepository) MarkWebhookDelivered(ctx context.Context, userID uint, deliveredAt time.Time, configVersion int) error {
	stamp := deliveredAt.UTC()
	return repo.database.WithContext(ctx).Model(&models.User{}).
		Where("id = ? AND webhook_config_version = ?", userID, configVersion).
		Where("webhook_last_delivered_at IS NULL OR webhook_last_delivered_at < ?", stamp).
		Update("webhook_last_delivered_at", stamp).Error
}

// ListAllForNotify returns the per-owner projection a future request-free batch
// pass needs to decide and send webhook reminders (issue #124). It selects a
// deliberately narrow column whitelist — cycle-prediction inputs, the webhook
// settings, the per-kind watermarks, the encrypted URL, the timezone, and the
// chosen interface language — and nothing else, so the batch query never
// over-reads sensitive per-account data. The last two are what a request-free
// pass has instead of a browser: they decide which calendar day the owner is on
// and which language the payload is written in.
// This is a dedicated method, NOT an overload of LoadSettingsByID (which stays
// the single settings whitelist). webhook_url is returned as CIPHERTEXT;
// decrypt via WebhookSettingsService.DecryptWebhookURL.
//
// webhook_config_version rides the projection because the snapshot is the whole
// problem: everything below is read once, at the start of the pass, and acted on
// minutes later. The epoch is what lets ClaimWebhookWatermark tell an act-on-it
// pass whose configuration is still current from one whose owner has since
// revoked delivery, so it is not an extra column so much as the timestamp of the
// rest of them.
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
			"interface_language",
			"webhook_enabled",
			"webhook_url",
			"webhook_notify_period",
			"webhook_notify_ovulation",
			"reminder_lead_days",
			"webhook_config_version",
			"webhook_period_last_sent_cycle_start",
			"webhook_ovulation_last_sent_cycle_start",
		).
		Order("id ASC").
		Find(&records).Error; err != nil {
		return nil, err
	}
	return records, nil
}

// ListOwnerLutealPhaseRows returns the per-owner projection the one-shot boot
// recompute of the derived luteal-phase cache walks
// (services.LutealPhaseRecomputer): the id it may update, the stored timezone it
// resolves the owner's calendar day at without a browser request, and the stored
// estimate it compares the recomputed value against. Three columns and nothing
// else — the pass reads day logs through DailyLogRepository, so it never needs a
// sensitive users column.
//
// Scoped to owner rows: luteal_phase is meaningless for any other role, and the
// column CHECK still admits 'partner' even though both account-creating methods
// refuse it. Narrowing the population to one value is safe here for a reason
// worth stating, since a filter that silently drops a row leaves that account on
// its stale estimate forever: the column is NOT NULL and its CHECK admits only
// 'owner' and 'partner', so 'owner' is the whole of the population this pass is
// meant to walk. There is no NULL row, no empty-string row and no third spelling
// for the predicate to exclude by accident.
//
// Ordered by id for a stable listing the projection test can assert against.
// The pass itself does not depend on the order: it derives and writes each row
// independently, with no batching and no resume point, so a retry re-derives
// every row whatever sequence it sees.
func (repo *UserRepository) ListOwnerLutealPhaseRows(ctx context.Context) ([]models.LutealPhaseRecomputeRow, error) {
	rows := make([]models.LutealPhaseRecomputeRow, 0)
	if err := repo.database.WithContext(ctx).
		Model(&models.User{}).
		Select("id", "timezone", "luteal_phase").
		Where("role = ?", models.RoleOwner).
		Order("id ASC").
		Find(&rows).Error; err != nil {
		return nil, err
	}
	return rows, nil
}

// webhookKindColumns names the two per-kind columns a claim needs: the
// watermark it compares and sets, and the per-kind opt-in it pins so a kind the
// owner has switched off cannot be claimed.
type webhookKindColumns struct {
	watermark string
	optIn     string
}

// webhookWatermarkColumns maps a reminder kind to its columns. Only these two
// kinds have a watermark; any other value is rejected so a typo can never write
// an unexpected column. Both columns of a kind live in ONE entry rather than in
// two maps keyed alike: a kind present in one map and missing from the other
// would silently drop half of the predicate below, which is the failure this
// pairing cannot have.
var webhookWatermarkColumns = map[string]webhookKindColumns{
	models.WebhookReminderTypePeriod: {
		watermark: "webhook_period_last_sent_cycle_start",
		optIn:     "webhook_notify_period",
	},
	models.WebhookReminderTypeOvulation: {
		watermark: "webhook_ovulation_last_sent_cycle_start",
		optIn:     "webhook_notify_ovulation",
	},
}

// canonicalWatermarkAnchor reduces a cycle anchor to UTC midnight, the single
// stored form of every watermark value. It is done HERE rather than by the
// model's BeforeSave hook because these writes go through Update/Updates, which
// bypass the hook — a raw location-bearing time would otherwise be stored
// verbatim and compare unequal to a UTC-midnight anchor on the next pass,
// breaking idempotency and, with it, the claim predicate below.
func canonicalWatermarkAnchor(cycleAnchor time.Time) time.Time {
	year, month, day := cycleAnchor.Date()
	return time.Date(year, month, day, 0, 0, 0, 0, time.UTC)
}

// ClaimWebhookWatermark takes an exclusive claim on one (owner, reminder kind,
// cycle anchor) send BEFORE it is delivered, scoped strictly to userID, and
// reports whether this caller won it. reminderType selects the column
// (period/ovulation); cycleAnchor is the cycle-start the reminder covers.
//
// The claim IS the watermark write — there is no second, separate advance after
// delivery. Writing it first is what makes the notify pass safe against overlap:
// the scheduler goroutine and an operator `ovumcy notify` process both compute
// their due-set from a record snapshot, so both can see the same uncovered
// anchor. An unconditional write let both deliver; a conditional one cannot.
//
// The UPDATE is a compare-and-set on the value the caller EXPECTS TO REPLACE —
// previous, the watermark this pass's own record snapshot carried — not on the
// value it is about to write:
//
//   - id = ? scopes the write to one owner, never another row.
//   - "column IS NULL" (previous nil) or "column = previous" is "the column has
//     not moved since I read it". Anything else means another writer got there
//     first, so this pass's due-set was computed against a watermark that no
//     longer exists and its conclusion cannot be trusted.
//
// Keying on the ANCHOR instead ("column <> anchor") is the tempting shorter form
// and it is wrong in one direction that matters: a pass holding a stale snapshot
// would win the claim over a NEWER watermark and move the column BACKWARDS. Its
// own delivery would announce a superseded cycle, and — because the column then
// no longer covers the newer anchor — the newer reminder would ship a second time
// on the next pass. The stale pass must lose. Regression:
// TestClaimWebhookWatermarkIsLostWhenTheColumnMovedSinceTheSnapshot.
//
// The next cycle stays claimable, which is the whole point of comparing against
// previous rather than against a fixed value: a pass that read watermark W and
// decided anchor X is due passes previous=W and wins, moving W to X.
//
// A nil or zero previous expects SQL NULL. A stored zero timestamp is a value
// nothing in this code path writes; were one to exist, every claim against it
// would be lost and the reminder suppressed — the fail-closed direction for an
// egress path, and never a duplicate send.
//
// Exactly one concurrent caller can see one row affected, because the conditional
// UPDATE is evaluated under the row lock: whichever statement runs second reads
// the column the first one already set. Zero rows is a normal outcome, not an
// error — the caller skips the send instead of duplicating it. Same shape as the
// TOTP replay guard's "totp_last_used_step < ?" below.
//
// The watermark alone is NOT the whole predicate, because it cannot see a
// revocation. configVersion is the webhook_config_version the same snapshot
// carried, and it is pinned too: every write to an owner's webhook configuration
// — a settings save, a disable, an endpoint replacement or removal, a change to
// the shared reminder lead window, a clear-data wipe — advances that column in
// the statement that performs it, so a claim presenting the previous epoch
// matches no row. That list is the complete set of writers, not an
// illustration: SaveWebhookSettings, UpdateReminderLeadDays and
// ClearAllDataAndResetSettings are the three, and a later writer owes the same
// advance whenever it CHANGES that configuration — never merely because it
// touched the users row. MarkWebhookDelivered is the counter-example and must
// stay one: it records a delivery that already happened, and advancing there
// would lose this same pass's next claim. It covers WHETHER, WHERE and HOW
// EARLY delivery happens and deliberately not what the reminder would say — a
// cycle-data edit or a timezone capture moves the prediction, which is the
// watermark compare-and-set's own subject, not this column's.
//
// Without the epoch the two revocation
// shapes both survive the watermark check: a settings save deliberately leaves
// the watermarks untouched, so the stale snapshot's compare-and-set still holds,
// and a clear-data wipe NULLs them, which re-opens the first-ever-claim branch
// outright. Either way the pass would POST health data to an endpoint the owner
// had already been told was revoked. The epoch is what makes the constitution's
// "containment survives state transitions" true across this one.
//
// webhook_enabled and the kind's own opt-in are pinned as well, and the honest
// reason is narrower than it first reads: today no reachable path needs them.
// The notify pass returns before the claim for an owner whose snapshot says
// delivery is off, and every write that turns delivery off also advances the
// epoch, so both shapes are already refused a step earlier. They are here as
// this layer's own floor, against the two things that would bypass that step —
// a future caller of this method that does not make the pass's early return,
// and a path that writes those columns without advancing the epoch. An egress
// claim already reading the row is cheap to make state its whole condition and
// expensive to retrofit once something depends on it not doing so. Regression:
// TestClaimWebhookWatermarkIsRefusedWhileDeliveryIsDisabled,
// TestClaimWebhookWatermarkIsRefusedForAKindTheOwnerOptedOutOf.
//
// What this does NOT cover, stated plainly because it cannot be fixed here: a
// request already in flight. The claim precedes the POST, so a revocation that
// lands after the claim won and before the response returns cannot recall a
// request already on the wire. The guarantee is that no NEW request begins under
// a configuration the owner has revoked. Stated for operators in
// docs/notifications.md.
//
// It touches only the one watermark column: NOT auth_session_version (taking a
// send claim is not a security-posture change), NOT the epoch it reads, and NOT
// any other setting.
func (repo *UserRepository) ClaimWebhookWatermark(ctx context.Context, userID uint, reminderType string, cycleAnchor time.Time, previous *time.Time, configVersion int) (bool, error) {
	columns, ok := webhookWatermarkColumns[reminderType]
	if !ok {
		return false, fmt.Errorf("unknown webhook reminder type %q", reminderType)
	}
	anchorUTC := canonicalWatermarkAnchor(cycleAnchor)

	// The opt-in and enabled flags are bound as parameters rather than written as
	// TRUE/1 literals: the two dialects spell a boolean differently, and the
	// driver already knows which.
	claim := repo.database.WithContext(ctx).Model(&models.User{}).
		Where("id = ? AND webhook_config_version = ? AND webhook_enabled = ? AND "+columns.optIn+" = ?",
			userID, configVersion, true, true)

	// Two spellings rather than one expression with a NULL-valued parameter: SQL
	// equality against NULL is never true in any dialect, so "column = ?" with a
	// NULL bind would silently match no row and lose every first-ever claim.
	if previous == nil || previous.IsZero() {
		claim = claim.Where(columns.watermark + " IS NULL")
	} else {
		claim = claim.Where(columns.watermark+" = ?", *previous)
	}

	result := claim.Update(columns.watermark, anchorUTC)
	if result.Error != nil {
		return false, result.Error
	}
	return result.RowsAffected > 0, nil
}

// ReleaseWebhookWatermark gives a claim back after the delivery it covered
// failed, restoring the watermark to the value the claiming pass found, scoped
// strictly to userID. It is what keeps a retryable failure retryable: without it
// a claim taken before a failed POST would read exactly like a successful send
// and permanently suppress the reminder.
//
// The restore is itself a compare-and-set — "column = anchor" — so it can only
// undo THIS pass's own claim. If the column has moved on (a later pass re-claimed
// the anchor, or the owner's row was written by another path), zero rows are
// affected and the newer value stands; rolling back blindly would resurrect a
// reminder somebody else has already sent.
//
// previous must be the SAME value the caller handed ClaimWebhookWatermark, and
// that is exactly what the claim replaced: the claim only succeeds when the
// column still holds previous, so "restore previous" cannot overwrite a value
// this pass never saw. It is written verbatim, not canonicalized — "unchanged"
// means the exact value that was there. A nil previous restores SQL NULL (never
// sent yet). A zero-row outcome is normal and not an error.
//
// It deliberately does NOT pin webhook_config_version the way the claim does,
// and the asymmetry is the point: the claim gates EGRESS, the release only puts
// a column back. A revocation that lands between claim and release must not
// leave the watermark standing on a send that never happened — that would
// suppress the reminder for good on a configuration the owner may re-arm. Where
// the revocation was a clear-data wipe the watermark is already NULL, so the
// "column = anchor" predicate matches nothing and the wipe stands untouched.
func (repo *UserRepository) ReleaseWebhookWatermark(ctx context.Context, userID uint, reminderType string, cycleAnchor time.Time, previous *time.Time) error {
	columns, ok := webhookWatermarkColumns[reminderType]
	if !ok {
		return fmt.Errorf("unknown webhook reminder type %q", reminderType)
	}
	anchorUTC := canonicalWatermarkAnchor(cycleAnchor)
	var restored any
	if previous != nil && !previous.IsZero() {
		restored = *previous
	}
	return repo.database.WithContext(ctx).Model(&models.User{}).
		Where("id = ? AND "+columns.watermark+" = ?", userID, anchorUTC).
		Updates(map[string]any{columns.watermark: restored}).Error
}

// SaveCalendarFeedToken sets (creates or rotates) the calendar-feed token
// columns for one owner, scoped strictly to userID. VerifierHash MUST already be
// a bcrypt hash and VerifierMAC an already-computed keyed authenticator — the
// caller derives both from the secret verifier via
// services.GenerateCalendarFeedToken before this method runs, so persistence
// never writes the verifier plaintext. Selector is the non-secret,
// UNIQUE-indexed lookup id.
//
// Both verifier columns are written on every mint: the MAC is what the feed
// endpoint compares, and the bcrypt hash stays current so a rollback to a binary
// that predates migration 032 keeps verifying freshly minted tokens.
//
// Rotation reuses this method: writing a fresh triple overwrites the previous
// one, so the old token stops verifying (its verifier matches neither the new MAC
// nor the new hash, and its selector no longer resolves). It touches only the
// feed-token columns and deliberately does NOT bump auth_session_version: a feed
// token is a per-surface capability, not an account credential, so rotating it
// must not revoke the owner's login sessions.
//
// It also NULLs calendar_feed_revealed_at in the SAME Updates(): a mint arms a
// fresh one-time reveal of the new subscribe URL, so the consumption mark of the
// previous one must not outlive the token it was about. Riding the same write
// means a mint can never leave a token armed with a stale mark that would refuse
// its own reveal.
//
// calendar_feed_key_epoch (migration 039) rides it for the same reason and is
// the only place that value is ever stamped: it names the verification regime
// this token was minted under, so arming a token and recording what armed it are
// one event. Revoke and both bulk disarms deliberately leave it standing — they
// clear the token, and a stamp without a token asserts nothing. That keeps this
// the sole writer, so the restore-fence completeness guard needs no exemption
// for it.
func (repo *UserRepository) SaveCalendarFeedToken(ctx context.Context, userID uint, columns models.CalendarFeedTokenColumns) error {
	// Fence first: see advanceCalendarFeedFence. A rotation retires the previous
	// token, so this write is a revocation as much as ClearCalendarFeedToken is.
	if err := repo.advanceCalendarFeedFence(ctx); err != nil {
		return err
	}
	return repo.database.WithContext(ctx).Model(&models.User{}).Where("id = ?", userID).Updates(map[string]any{
		"calendar_feed_selector":      columns.Selector,
		"calendar_feed_verifier_hash": columns.VerifierHash,
		"calendar_feed_verifier_mac":  columns.VerifierMAC,
		"calendar_feed_revealed_at":   nil,
		"calendar_feed_key_epoch":     columns.KeyEpoch,
	}).Error
}

// ClaimCalendarFeedReveal atomically claims the one-time reveal of the owner's
// calendar-feed subscribe URL, scoped strictly to userID. It returns true iff
// this call is the one that consumed it — the persisted
// calendar_feed_revealed_at was NULL at the moment of the UPDATE.
//
// The compare-and-set is the whole mechanism: clearing the sealed cookie in the
// reveal response asks the browser to forget the value, while a client that kept
// it can present it again. A replay and a concurrent second reveal both find the
// column already set, affect zero rows, and get false. Shaped after ClaimTOTPStep,
// which claims a TOTP step the same way and for the same reason.
func (repo *UserRepository) ClaimCalendarFeedReveal(ctx context.Context, userID uint, revealedAt time.Time) (bool, error) {
	result := repo.database.WithContext(ctx).Model(&models.User{}).
		Where("id = ? AND calendar_feed_revealed_at IS NULL", userID).
		Update("calendar_feed_revealed_at", revealedAt.UTC())
	if result.Error != nil {
		return false, result.Error
	}
	return result.RowsAffected == 1, nil
}

// ClaimRecoveryCodeReveal atomically claims the one-time reveal of the owner's
// recovery code, scoped strictly to userID, and returns true iff this call is
// the one that consumed it. It is the recovery-code half of
// ClaimCalendarFeedReveal above and carries the same reasoning: both reveal
// surfaces enforced "shown once" by retracting a cookie, which a retained sealed
// value survives.
func (repo *UserRepository) ClaimRecoveryCodeReveal(ctx context.Context, userID uint, revealedAt time.Time) (bool, error) {
	result := repo.database.WithContext(ctx).Model(&models.User{}).
		Where("id = ? AND recovery_code_revealed_at IS NULL", userID).
		Update("recovery_code_revealed_at", revealedAt.UTC())
	if result.Error != nil {
		return false, result.Error
	}
	return result.RowsAffected == 1, nil
}

// BackfillCalendarFeedVerifierMAC writes the keyed verifier authenticator into a
// row minted before migration 032, which carries a bcrypt hash but no MAC. The
// MAC cannot be derived from the hash (that is the point of a hash), so the only
// moment it can be computed is a request that presents the correct verifier
// plaintext — the feed's own verification path calls this after a successful
// bcrypt fallback, and the row then joins the microsecond fast path.
//
// The UPDATE is a compare-and-set, not a blind write, and every predicate earns
// its place:
//
//   - id = ? scopes the write to the resolved owner (never another row).
//   - calendar_feed_selector = ? pins it to the token that was just verified. If
//     the owner rotated or revoked the feed between the read and this write, the
//     selector no longer matches, zero rows are affected, and the stale MAC is
//     NOT written — without this, a backfill racing a rotation could pair the old
//     token's MAC with the new token's hash and break the fresh subscribe URL
//     until the next rotation.
//   - the NULL-or-empty guard keeps it idempotent and one-way: a row that already
//     carries a MAC is never overwritten, so this path can never replace a live
//     authenticator (in particular it can never "repair" a MAC that mismatches
//     because SECRET_KEY was rotated — that case is a deliberate hard refusal).
//
// A zero-row outcome is a normal, expected result, so it is not an error: the
// caller treats a failed backfill as a missed optimization and still serves the
// feed.
func (repo *UserRepository) BackfillCalendarFeedVerifierMAC(ctx context.Context, userID uint, selector string, verifierMAC string) error {
	return repo.database.WithContext(ctx).Model(&models.User{}).
		Where("id = ? AND calendar_feed_selector = ? AND (calendar_feed_verifier_mac IS NULL OR calendar_feed_verifier_mac = '')", userID, selector).
		Update("calendar_feed_verifier_mac", verifierMAC).Error
}

// ClearCalendarFeedToken revokes an owner's calendar feed by NULLing every feed
// token column, scoped strictly to userID. After this the feed URL 404s (its
// selector resolves no row). Like SaveCalendarFeedToken it does not bump
// auth_session_version — revoking a per-surface capability is not a change to the
// account's login security posture. Uses a typed nil so the columns become SQL
// NULL (feed off), matching the "both NULL = off" default.
func (repo *UserRepository) ClearCalendarFeedToken(ctx context.Context, userID uint) error {
	// Fence first: see advanceCalendarFeedFence. This write IS the revocation,
	// so the fence has to be ahead of it — a crash in between then leaves the
	// owner's intent enforced at the next boot instead of lost.
	if err := repo.advanceCalendarFeedFence(ctx); err != nil {
		return err
	}
	return repo.database.WithContext(ctx).Model(&models.User{}).Where("id = ?", userID).Updates(map[string]any{
		"calendar_feed_selector":      nil,
		"calendar_feed_verifier_hash": nil,
		"calendar_feed_verifier_mac":  nil,
	}).Error
}

// DisarmCalendarFeedTokensWithoutMAC clears the feed-token columns of every row
// that is armed (non-empty selector) but carries NO verifier MAC — the rows
// minted before migration 032, whose bcrypt hash verifies independently of
// SECRET_KEY. The boot-time rotation sentinel calls it on two boots: one where
// the calendar-feed key epoch changed, and one where no epoch is stored yet.
// After a rotation the rotated key already turns MAC verification into a hard
// refusal for every other armed row, so this narrow predicate is exactly the
// set that would otherwise SURVIVE it — and be silently re-armed under the new
// key by the first successful bcrypt poll's MAC backfill. On the absent-epoch
// boot no key changed and that inference is unavailable: the same rows are
// cleared because nothing records which key minted them.
//
// Rows that do carry a MAC are deliberately left in place: they fail closed on
// their own, and the narrow predicate keeps the blast radius of a boot with a
// mistyped SECRET_KEY as small as the revocation rule allows (only legacy rows
// are irreversibly disarmed; the operator runbook calls this out).
//
// Like every feed-token write it does not bump auth_session_version: a feed
// capability is not a login credential. The row count feeds the startup log.
func (repo *UserRepository) DisarmCalendarFeedTokensWithoutMAC(ctx context.Context) (int64, error) {
	result := repo.database.WithContext(ctx).Model(&models.User{}).
		Where("calendar_feed_selector IS NOT NULL AND calendar_feed_selector != '' AND (calendar_feed_verifier_mac IS NULL OR calendar_feed_verifier_mac = '')").
		Updates(map[string]any{
			"calendar_feed_selector":      nil,
			"calendar_feed_verifier_hash": nil,
			"calendar_feed_verifier_mac":  nil,
		})
	return result.RowsAffected, result.Error
}

// DisarmAllCalendarFeedTokens clears the feed-token columns of EVERY armed row,
// whatever its verifier generation. The boot-time restore fence calls it when
// the database turns out not to be the one this instance last ran with — a
// backup restore, or a fence that was recreated.
//
// The wider predicate is what the trigger demands, and is the one difference
// from DisarmCalendarFeedTokensWithoutMAC above. That sentinel narrows to
// MAC-less rows because its trigger IS a changed SECRET_KEY, which already
// turns every MAC-bearing row into a hard refusal. A restore changes no key:
// selector, bcrypt hash and MAC all come back valid together, and every armed
// row — legacy or current — would serve its old subscribe URL again. Narrowing
// here would leave exactly the rows the finding is about.
//
// calendar_feed_revealed_at is deliberately left standing, as in every other
// disarm: whether the owner has already been shown a URL is a separate fact
// from whether one is armed. Like every feed-token write it does not bump
// auth_session_version — a feed capability is not a login credential — and the
// row count feeds the startup log.
func (repo *UserRepository) DisarmAllCalendarFeedTokens(ctx context.Context) (int64, error) {
	result := repo.database.WithContext(ctx).Model(&models.User{}).
		Where("calendar_feed_selector IS NOT NULL AND calendar_feed_selector != ''").
		Updates(map[string]any{
			"calendar_feed_selector":      nil,
			"calendar_feed_verifier_hash": nil,
			"calendar_feed_verifier_mac":  nil,
		})
	return result.RowsAffected, result.Error
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
// intent explicit and avoids a pointless query. The returned user carries both
// CalendarFeedVerifierMAC and CalendarFeedVerifierHash so the caller can
// constant-time-verify the verifier half — via the MAC, or via bcrypt for a row
// minted before migration 032 — without a second read.
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
//
// recovery_code_revealed_at is NULLed in that same statement because the fresh
// code arms its own one-time reveal (migration 036). Only a MINT clears a reveal
// mark: the feed columns are cleared here rather than re-minted, so
// calendar_feed_revealed_at is deliberately left standing — re-arming the reveal
// of a token that no longer resolves would only make a retained sealed cookie
// presentable again. Regression: TestEveryRecoveryCodeMintClearsItsRevealMark.
func (repo *UserRepository) UpdateRecoveryCodeHashAndRevokeSessions(ctx context.Context, userID uint, recoveryHash string) error {
	if err := repo.database.WithContext(ctx).Model(&models.User{}).Where("id = ?", userID).Updates(map[string]any{
		"recovery_code_hash":          recoveryHash,
		"recovery_code_revealed_at":   nil,
		"calendar_feed_selector":      nil,
		"calendar_feed_verifier_hash": nil,
		"calendar_feed_verifier_mac":  nil,
		"auth_session_version":        gorm.Expr("auth_session_version + 1"),
	}).Error; err != nil {
		return err
	}
	// Best-effort: see advanceCalendarFeedFence's doc comment for why a
	// caller past this point drops the error instead of returning it.
	_ = repo.advanceCalendarFeedFence(ctx)
	return nil
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
// UpdatePasswordAndRevokeSessions (the CLI `ovumcy reset-password` path). It rewrites the
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
	if err := repo.database.WithContext(ctx).Model(&models.User{}).Where("id = ?", userID).Updates(map[string]any{
		"password_hash":               passwordHash,
		"must_change_password":        true,
		"local_auth_enabled":          true,
		"calendar_feed_selector":      nil,
		"calendar_feed_verifier_hash": nil,
		"calendar_feed_verifier_mac":  nil,
		"auth_session_version":        gorm.Expr("auth_session_version + 1"),
	}).Error; err != nil {
		return err
	}
	// Best-effort: see advanceCalendarFeedFence's doc comment for why a
	// caller past this point drops the error instead of returning it.
	_ = repo.advanceCalendarFeedFence(ctx)
	return nil
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

// UpdatePasswordRecoveryCodeAndRevokeSessions writes a password hash and a fresh
// recovery-code hash in one atomic update, bumping auth_session_version with
// them. It NULLs recovery_code_revealed_at in the same statement: the code it
// writes is about to be revealed once, so its consumption mark starts unset
// (migration 036). Regression: TestEveryRecoveryCodeMintClearsItsRevealMark.
func (repo *UserRepository) UpdatePasswordRecoveryCodeAndRevokeSessions(ctx context.Context, userID uint, passwordHash string, recoveryHash string, mustChangePassword bool) error {
	return repo.database.WithContext(ctx).Model(&models.User{}).Where("id = ?", userID).Updates(map[string]any{
		"password_hash":             passwordHash,
		"recovery_code_hash":        recoveryHash,
		"recovery_code_revealed_at": nil,
		"must_change_password":      mustChangePassword,
		"local_auth_enabled":        true,
		"auth_session_version":      gorm.Expr("auth_session_version + 1"),
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
//
// recovery_code_revealed_at rides that CAS as well, NULLed because the fresh
// code arms its own one-time reveal (migration 036) — and for the same
// consistency reason: the redeem that loses the race must not re-arm a reveal it
// minted no code for. Regression: TestEveryRecoveryCodeMintClearsItsRevealMark.
func (repo *UserRepository) UpdatePasswordRecoveryCodeAndRevokeSessionsCAS(ctx context.Context, userID uint, oldPasswordHash string, newPasswordHash string, recoveryHash string) error {
	result := repo.database.WithContext(ctx).Model(&models.User{}).
		Where("id = ? AND password_hash = ?", userID, oldPasswordHash).
		Updates(map[string]any{
			"password_hash":               newPasswordHash,
			"recovery_code_hash":          recoveryHash,
			"recovery_code_revealed_at":   nil,
			"must_change_password":        false,
			"local_auth_enabled":          true,
			"calendar_feed_selector":      nil,
			"calendar_feed_verifier_hash": nil,
			"calendar_feed_verifier_mac":  nil,
			"auth_session_version":        gorm.Expr("auth_session_version + 1"),
		})
	if result.Error != nil {
		return result.Error // codecov:ignore -- DB-layer error on the CAS UPDATE; not reachable in unit tests
	}
	if result.RowsAffected == 0 {
		return ErrResetTokenAlreadyConsumed
	}
	// Best-effort: see advanceCalendarFeedFence's doc comment for why a
	// caller past this point drops the error instead of returning it.
	_ = repo.advanceCalendarFeedFence(ctx)
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
			"interface_language",
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
			// The two marks migration 039 added. They are here and the two
			// *_last_sent_cycle_start watermarks are deliberately NOT: a watermark
			// is a claim taken before a POST and would be read as a delivery time
			// by anything that rendered it, which is the misreading these two
			// columns exist to replace.
			"webhook_last_delivered_at",
			"calendar_feed_key_epoch",
			// The feed columns the egress ledger reads. The selector says a
			// token exists; calendar_feed_revealed_at marks the one-time reveal
			// as CONSUMED and is not a record that anyone fetched the feed --
			// polls are deliberately unaudited. No verifier lands here: the
			// plaintext is never stored and the hash and MAC are not renderable.
			"calendar_feed_selector",
			"calendar_feed_revealed_at",
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

// SaveOnboardingStep2 writes the columns the second onboarding step owns.
// age_group is not among them — onboarding no longer collects it, and the
// column is written by the settings cycle form only.
func (repo *UserRepository) SaveOnboardingStep2(ctx context.Context, userID uint, cycleLength int, periodLength int, autoPeriodFill bool, irregularCycle bool, usageGoal string) error {
	return repo.database.WithContext(ctx).Model(&models.User{}).Where("id = ?", userID).Updates(map[string]any{
		"cycle_length":     cycleLength,
		"period_length":    periodLength,
		"luteal_phase":     14,
		"auto_period_fill": autoPeriodFill,
		"irregular_cycle":  irregularCycle,
		"usage_goal":       usageGoal,
	}).Error
}

func (repo *UserRepository) ClearAllDataAndResetSettings(ctx context.Context, userID uint) error {
	if err := repo.database.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		if err := tx.Where("user_id = ?", userID).Delete(&models.DailyLog{}).Error; err != nil {
			return err
		}
		if err := tx.Where("user_id = ? AND is_builtin = ?", userID, false).Delete(&models.SymptomType{}).Error; err != nil {
			return err
		}
		return tx.Model(&models.User{}).Where("id = ?", userID).Updates(map[string]any{
			"cycle_length":  models.DefaultCycleLength,
			"period_length": models.DefaultPeriodLength,
			"luteal_phase":  14,
			// auto_period_fill resets to models.DefaultAutoPeriodFill (off), not
			// to the value the account carried. Auto-fill MANUFACTURES period
			// days the owner never logged, so leaving it armed would answer an
			// erasure gesture by restarting the generator that produced part of
			// what was just erased — containment has to survive the transition,
			// and the wiped account is a fresh account for this setting.
			"auto_period_fill":                models.DefaultAutoPeriodFill,
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
			// users.timezone is the owner's last observed IANA zone, written from
			// the request (api.UpdateTimezone) and read by the request-free
			// reminder pass. It is a coarse location signal inferred from the
			// owner's browser, so a clear-data wipe must not leave it standing
			// when every other preference resets. Empty string is the value a
			// fresh account carries: migration 026 adds the column with no
			// DEFAULT and models.User.Timezone is a plain string with no gorm
			// default, so the zero value is what both the schema and a new row
			// agree on. The next request re-detects the zone and persists it
			// again, so the reset costs nothing but the stale value.
			"timezone": "",
			// users.interface_language is deliberately ABSENT from this map. It
			// is not health data and not a location signal: it is the language
			// the owner reads the interface in, and resetting it would answer a
			// "wipe my records" gesture by switching the UI back to English
			// mid-session. It survives the wipe exactly as email, password hash
			// and display name do; account deletion removes the row anyway.
			// Regression: TestClearAllDataPreservesInterfaceLanguage.
			//
			// Webhook notification settings (issue #124) are owner data: a
			// clear-data wipe disarms delivery, clears the encrypted endpoint,
			// resets the shared lead window to its default, and clears the
			// per-kind watermarks so no stale reminder fires against the freshly
			// emptied account. The per-kind opt-ins return to their column
			// defaults (both true) to match a fresh account.
			//
			// webhook_config_version is the one webhook column that ADVANCES
			// instead of resetting, and it has to: NULLing the watermarks is
			// precisely what re-opens the claim predicate's first-ever-send
			// branch, so a notify pass that snapshotted this owner before the
			// wipe would find its claim satisfied again and POST to the endpoint
			// this statement just erased. Advancing the epoch in the same
			// UPDATE is what makes the erasure hold against a pass already in
			// flight; resetting it to zero would hand that snapshot its own
			// value back and reopen the window from the other side. Regression:
			// TestClaimWebhookWatermarkIsLostAfterClearData,
			// TestClearAllDataAdvancesTheWebhookRevocationEpoch.
			"webhook_enabled":                         false,
			"webhook_url":                             "",
			"webhook_notify_period":                   true,
			"webhook_notify_ovulation":                true,
			"webhook_period_last_sent_cycle_start":    nil,
			"webhook_ovulation_last_sent_cycle_start": nil,
			// The delivery mark (migration 039) goes with them, and
			// unconditionally: this statement erases the endpoint, so a mark
			// saying a delivery there was accepted would outlive the only thing
			// that gave it meaning. Unlike the reveal marks below, NULL here is
			// not an armed state — it is "no delivery recorded", which is exactly
			// true of an account whose endpoint this write just cleared.
			"webhook_last_delivered_at": nil,
			"reminder_lead_days":        models.DefaultReminderLeadDays,
			"webhook_config_version":    gorm.Expr("webhook_config_version + 1"),
			// Calendar (.ics) feed token: a clear-data wipe revokes the feed by
			// NULLing all three columns (selector plus both verifier columns —
			// the MAC arrived with migration 032, after this comment was
			// written), so any previously-issued feed URL 404s against
			// the freshly emptied account (its selector no longer resolves). This
			// is the data-reset arm of the approved force-rotate-on-recovery rule;
			// the password-reset / operator-reset / recovery-regen force-rotate
			// hooks are a later slice.
			"calendar_feed_selector":      nil,
			"calendar_feed_verifier_hash": nil,
			"calendar_feed_verifier_mac":  nil,
			// The two shown-once reveal marks (migration 036) are deliberately
			// ABSENT from this map. They are not preferences and hold no health
			// data — each records that a secret was already displayed — and NULL
			// is the ARMED value, so resetting them here would answer a wipe by
			// re-arming a reveal for a sealed cookie the client may still hold.
			// Only a mint clears a mark, and the mints this wipe leaves possible
			// (generate a feed, regenerate the code) each clear their own in the
			// same write. Account deletion removes the row and the marks with it.
			// Bump auth_session_version inside the same transaction so a
			// successful clear-data wipe also revokes every auth cookie that
			// existed before the wipe. Without this bump a stolen session that
			// was used to trigger the wipe would retain authenticated access
			// to the freshly-empty account, and a legitimate "panic clear"
			// gesture would not actually sign other devices out.
			"auth_session_version": gorm.Expr("auth_session_version + 1"),
		}).Error
	}); err != nil {
		return err
	}
	// Dropped like the purge error above, and for the same reason: the wipe has
	// committed, and reporting it as a failure would tell an owner their data
	// is still there. Dropping it costs no containment on either route an error
	// actually arrives on. The file half was written and the database half was
	// not, so the halves already disagree and the next boot disarms. Or neither
	// was written, which means the fence file is unwritable — and an unwritable
	// fence sends every later boot down Enforce's unanchored path, which
	// disarms every armed feed on each start. (A failed token mint also reaches
	// here with neither half written and a writable fence, which WOULD lose the
	// record; it takes an OS-level entropy fault, and Advance marks it
	// unreachable for the same reason.)
	//
	// That reasoning holds for THIS caller — the server process, whose anchor is
	// the fence file it was booted with, acting on the owner's own request.
	// There is no operator-CLI counterpart to wiping data this way: clear-data
	// is reached only from SettingsService, itself reached only from the
	// owner-authenticated, step-up-gated danger-zone endpoint.
	_ = repo.advanceCalendarFeedFence(ctx)
	return nil
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
		// oidc_logout_states rows carry the owner's user_id (migration 031), so
		// erase them explicitly here alongside the other user-scoped tables. The
		// rows written before 031 had a NULL user_id that this predicate could
		// never match, leaving an id_token_hint behind for up to the state TTL
		// after erasure — migration 033 deleted them, so every row in the table is
		// now attributable and this delete covers all of them.
		if err := tx.Where("user_id = ?", userID).Delete(&models.OIDCLogoutState{}).Error; err != nil {
			return err
		}
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
	// The erased account's feed left with its row, which is a removal like any
	// other: a restore that brings the account back brings its subscribe URL
	// back with it. The error is dropped for the same reason the purge above
	// drops its own — the erasure has committed, and an account that no longer
	// exists must not be reported as one that failed to be deleted, least of
	// all to an owner who can no longer sign in to retry. It costs no
	// containment on either route an error actually arrives on: a written file
	// half with no database half leaves the two disagreeing, and neither half
	// written means the fence file is unwritable, which sends every later boot
	// down Enforce's unanchored path. Both disarm. See ClearAllDataAndResetSettings
	// for the third, unreachable route.
	//
	// That reasoning holds for THIS caller — the server process, whose anchor is
	// the fence file it was booted with, acting on the owner's own request. The
	// operator CLI's `users delete` does not reach this best-effort call as its
	// only protection: it confirms and advances the same fence through
	// AdvanceConfirmed before it ever calls this method, and refuses the whole
	// deletion when it cannot (internal/cli).
	_ = repo.advanceCalendarFeedFence(ctx)
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
