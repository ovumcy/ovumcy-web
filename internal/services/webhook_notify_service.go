package services

import (
	"context"
	"fmt"
	"log"
	"net/url"
	"strings"
	"time"

	"github.com/ovumcy/ovumcy-web/internal/models"
)

// Webhook notify pass (issue #124, slice 3). This is the request-free
// orchestration that ties the slices together: it lists every owner, decrypts
// each owner's webhook URL, asks the pure decision layer (slice 2) which
// reminders are due, delivers them through the hardened egress client, and — ON
// SUCCESS ONLY — advances the per-kind watermark so the same reminder is never
// sent twice.
//
// Cross-owner isolation: every step is scoped to a single owner's record — the
// URL decrypted with that owner's aad, the decision built from that owner's user
// + logs, the delivery aimed at that owner's URL, the watermark written to that
// owner's row. One owner's health data is therefore never carried into a request
// aimed at another owner.
//
// Idempotency: the watermark is CLAIMED before the POST and released when the
// POST fails. A pass takes the claim by compare-and-set on the same watermark
// column the decision reads, so at most one pass can own a given (owner, kind,
// cycle anchor); a failed POST hands the claim back, leaving the watermark where
// the pass found it, so the next pass (a same-day re-run or a concurrent
// operator cron) still retries it, and a succeeded POST leaves the claim
// standing, so a re-run skips it.
//
// Claiming BEFORE delivery rather than writing after it is what makes the pass
// safe against OVERLAP. The due-set is computed from the record snapshot taken by
// ListAllForNotify, so two passes that both list before either writes — the
// scheduler goroutine and an operator `ovumcy notify` process, which is a
// separate OS process and therefore beyond any in-process lock — each see an
// uncovered anchor and each decide the reminder is due. With the write deferred
// to after Deliver, both delivered. The claim is the coordination point with
// slice-1's watermark columns; it is the only mutual exclusion the pass has.
//
// The claim carries a second job the watermark cannot do: REVOCATION. The
// snapshot is minutes old by the time a POST leaves, and in that window the
// owner can disable delivery, replace or remove the endpoint, narrow the shared
// reminder lead window, or clear their data — none of which moves a watermark (a
// settings save deliberately leaves them alone) and one of which, clear-data,
// NULLs them and so re-opens the first-ever-claim branch. Each of those writes
// advances the owner's webhook_config_version in the same statement, and the
// claim pins the epoch its own snapshot carried, so a revoked configuration can
// no longer win a claim. The line this holds is "no NEW request begins under a
// revoked configuration"; a request already on the wire when the revocation
// lands cannot be recalled, which is documented for operators rather than
// papered over here.
//
// The cost, stated plainly: claiming first turns a returned delivery error into a
// retry but a HARD KILL between claim and release into a permanent skip. A pass
// that dies mid-POST — host reboot, OOM kill, container eviction, an interrupted
// `ovumcy notify` — leaves the claim standing with nothing delivered, and no
// counter records it (the process that would have counted it is gone). That is
// at-most-once for this window, deliberately: a missed reminder is a convenience
// lost, a duplicate reminder about health data at an owner's endpoint is not.
// Narrowing it further needs a claim that is distinguishable from a completed
// send, which the single date column cannot express — a schema decision, not a
// change to make here. Stated for operators in docs/notifications.md.
//
// No-secret discipline: the pass logs counts and at most owner ids — never the
// URL, token, decrypted health specifics, or payload.

// NotifyUserRepository is the narrow persistence surface the notify pass needs.
// It lists the per-owner notify projection and advances a per-kind watermark.
type NotifyUserRepository interface {
	// ListAllForNotify returns the per-owner notify projection (webhook_url is
	// CIPHERTEXT). Ordered by id for a deterministic pass.
	ListAllForNotify(ctx context.Context) ([]models.WebhookNotifyRecord, error)
	// ClaimWebhookWatermark takes an exclusive claim on one (owner, kind, cycle
	// anchor) send by compare-and-set on the per-kind watermark, and reports
	// whether this caller won it. previous is the watermark the caller's record
	// snapshot carried and is the value the set is compared against, so a claim is
	// lost whenever the column moved at all since that snapshot — not only when it
	// moved to this same anchor. false means another pass owns the send.
	// cycleAnchor is canonicalized to UTC-midnight by the repo.
	//
	// configVersion is the revocation epoch the SAME snapshot carried, and the
	// claim is lost when the stored epoch has moved since. That is the thing a
	// watermark cannot express: an owner disabling delivery, replacing or
	// removing the endpoint, narrowing the shared reminder lead window, or
	// clearing their data moves none of the values above, yet must stop a pass
	// that read the old configuration from reaching the old endpoint. false
	// therefore means "another pass owns this send OR the configuration this pass
	// read is no longer the owner's" — the caller treats both the same way, by
	// not delivering.
	ClaimWebhookWatermark(ctx context.Context, userID uint, reminderType string, cycleAnchor time.Time, previous *time.Time, configVersion int) (bool, error)
	// ReleaseWebhookWatermark restores the watermark to previous after the delivery
	// a claim covered failed, conditional on the column still holding cycleAnchor.
	// previous must be the same value handed to ClaimWebhookWatermark, which is
	// what that claim replaced.
	ReleaseWebhookWatermark(ctx context.Context, userID uint, reminderType string, cycleAnchor time.Time, previous *time.Time) error
}

// NotifyLogReader is the narrow read surface for an owner's day logs (the
// prediction input). It is a subset of *db.DailyLogRepository.ListByUser.
type NotifyLogReader interface {
	ListByUser(ctx context.Context, userID uint) ([]models.DailyLog, error)
}

// WebhookURLDecryptor decrypts a stored webhook_url ciphertext for one owner. It
// is satisfied by *WebhookSettingsService.DecryptWebhookURL, kept as an interface
// so the notify pass depends only on the decrypt seam (and tests can inject a
// deterministic decryptor without a real SECRET_KEY).
type WebhookURLDecryptor interface {
	DecryptWebhookURL(userID uint, encryptedURL string) (string, error)
}

// DisclaimerProvider yields the medical-safety disclaimer for a language. It is
// satisfied by an i18n adapter; the egress surfaces use it so every payload
// carries the owner-localized "estimates, not medical advice" string without
// importing the whole i18n Manager.
type DisclaimerProvider interface {
	Disclaimer(language string) string
}

// NotifyCopyProvider is the whole localized-copy seam of the notify pass: the
// medical-safety disclaimer plus any catalogue entry by key. The reminder
// headline and sentence are catalogue entries rather than Go literals precisely
// so they follow the same language the disclaimer does — a payload written half
// in the owner's language and half in the server's is what the split produced.
// Both halves resolve at the owner's users.interface_language.
type NotifyCopyProvider interface {
	DisclaimerProvider
	// Message returns the catalogue entry for key at language, or "" when the key
	// is unknown. An empty or unsupported language yields the server default (the
	// i18n Manager merges the default over the target).
	Message(language string, key string) string
}

// The reminder copy keys. They are whole literals on purpose: the locale
// reachability sweep recognises a key only by the literal that names it, so a
// key assembled from parts would read as unreachable and be deleted by the next
// catalogue cleanup.
const (
	reminderPeriodTitleKey      = "webhook.reminder.period.title"
	reminderPeriodMessageKey    = "webhook.reminder.period.message"
	reminderOvulationTitleKey   = "webhook.reminder.ovulation.title"
	reminderOvulationMessageKey = "webhook.reminder.ovulation.message"
)

// NotifyReport is the transport-free result of one notify pass. It never carries
// a URL, token, or payload — a destination appears as a HOST at most.
// OwnerIDsFailed lets an operator see WHICH owners failed (an id is not a secret)
// without exposing why in a way that leaks the endpoint.
//
// On a dry run DryRunPreview additionally carries each reminder's type and
// estimated date. Those ARE health specifics about an identified owner, so a
// caller that renders the report keeps them behind an explicit opt-in (the CLI's
// --show-health-details) instead of printing them by default; everything else in
// the report is safe to print unconditionally.
type NotifyReport struct {
	// OwnersScanned is the number of owner records the pass examined.
	OwnersScanned int
	// Due is the number of reminders the decision layer reported as due across all
	// owners (before delivery).
	Due int
	// Sent is the number of reminders successfully delivered (2xx). Always 0 on a
	// dry run.
	Sent int
	// SkippedIdempotent is the number of due reminders skipped because a watermark
	// already covered them (they were sent on an earlier — or an overlapping —
	// pass). Two moments contribute: the pure decision layer excludes reminders
	// whose incoming watermark covers them, and a claim lost to a concurrent pass
	// is skipped at delivery time. A lost claim is a skip, never a failure: the
	// reminder IS being delivered, by the pass that won it.
	SkippedIdempotent int
	// Failed is the number of reminders that could not be delivered: the POST
	// failed (non-2xx, timeout, refused redirect, bad scheme) and its claim was
	// released, or the claim itself could not be taken because the watermark write
	// errored. Either way the watermark is left where the pass found it, so the
	// next pass retries.
	Failed int
	// DryRun records whether this pass computed-only (no delivery, no watermark).
	DryRun bool
	// OwnerIDsFailed lists the ids of owners with at least one failed delivery.
	OwnerIDsFailed []uint
	// DryRunPreview lists, on a dry run only, what WOULD be sent: one line per due
	// reminder with its type, estimated date, and destination HOST — never the
	// full URL, query, or token. Empty on a real delivery pass.
	DryRunPreview []NotifyPreviewLine
}

// NotifyPreviewLine is a single "would send" entry for a dry run. It carries the
// reminder type, the estimated event date, and the destination HOST ONLY — the
// same minimized, secret-free shape the delivered payload uses, so a dry run is
// auditable without leaking the URL or token.
type NotifyPreviewLine struct {
	OwnerID   uint
	Type      string
	EventDate string
	Host      string
}

// WebhookNotifyService runs the notify pass. It holds only narrow seams so it is
// fully unit-testable with stubs and never reaches for a real socket or clock.
type WebhookNotifyService struct {
	users     NotifyUserRepository
	logs      NotifyLogReader
	decryptor WebhookURLDecryptor
	deliverer WebhookDeliverer
	localized NotifyCopyProvider
}

// NewWebhookNotifyService assembles the notify service from its collaborators.
func NewWebhookNotifyService(
	users NotifyUserRepository,
	logs NotifyLogReader,
	decryptor WebhookURLDecryptor,
	deliverer WebhookDeliverer,
	localized NotifyCopyProvider,
) *WebhookNotifyService {
	return &WebhookNotifyService{
		users:     users,
		logs:      logs,
		decryptor: decryptor,
		deliverer: deliverer,
		localized: localized,
	}
}

// RunOnce executes one notify pass. now and location are injected (never
// time.Now() inside the decision path); per owner, the owner's persisted
// timezone is preferred and location is the fallback. When dryRun is true it
// computes what WOULD be sent but makes NO outbound request and writes NO
// watermark.
//
// A pass-level failure (listing owners) returns an error with a zero-ish report.
// A per-owner failure (decrypt error, delivery error) is contained: it is
// counted, logged host-only, and the pass continues to the next owner — one bad
// endpoint never aborts every other owner's reminders.
func (service *WebhookNotifyService) RunOnce(ctx context.Context, now time.Time, location *time.Location, dryRun bool) (NotifyReport, error) {
	report := NotifyReport{DryRun: dryRun}

	records, err := service.users.ListAllForNotify(ctx)
	if err != nil {
		return report, fmt.Errorf("list owners for notify: %w", err)
	}

	for i := range records {
		record := records[i]
		report.OwnersScanned++
		service.processOwner(ctx, record, now, location, dryRun, &report)
	}
	return report, nil
}

// processOwner runs the decision-and-delivery for a single owner, mutating the
// aggregate report. All per-owner errors are contained here so the pass survives
// a single bad endpoint.
func (service *WebhookNotifyService) processOwner(
	ctx context.Context,
	record models.WebhookNotifyRecord,
	now time.Time,
	location *time.Location,
	dryRun bool,
	report *NotifyReport,
) {
	if !record.WebhookEnabled {
		return
	}

	ownerLocation := resolveOwnerLocation(record.Timezone, location)

	decryptedURL, err := service.decryptor.DecryptWebhookURL(record.ID, record.WebhookURL)
	if err != nil {
		// Fail safe: skip this owner rather than deliver to a garbage target after a
		// decrypt failure (e.g. SECRET_KEY rotation). Log the owner id only — the
		// error may not carry the URL, but we never risk it.
		log.Printf("webhook notify: decrypt failed, skipping owner id=%d", record.ID)
		return
	}
	if strings.TrimSpace(decryptedURL) == "" {
		// Enabled but no endpoint stored: nothing deliverable.
		return
	}

	dayLogs, err := service.logs.ListByUser(ctx, record.ID)
	if err != nil {
		log.Printf("webhook notify: load logs failed, skipping owner id=%d", record.ID)
		return
	}

	user := userFromNotifyRecord(record)
	settings := WebhookReminderSettingsFromNotifyRecord(record)
	// One traversal yields both the authoritative due set and the idempotency
	// counter: the decision already knows which reminders its own watermark
	// withheld, so the Report can prove "sent once, then skipped" without the pass
	// deciding a second time. That second decision was pure and I/O-free but not
	// cheap — it rebuilt the owner's cycle statistics from their whole logged
	// history, so the counter cost as much as the work it counted.
	due, watermarkSuppressed := decideDueReminders(&user, settings, dayLogs, now, ownerLocation)
	report.SkippedIdempotent += watermarkSuppressed

	// Destination HOST only, for the dry-run preview and any host-scoped logging.
	// Never keep or print more of the URL than this.
	host := hostOnly(decryptedURL)

	for _, reminder := range due {
		report.Due++
		payload := service.buildPayload(reminder, record.InterfaceLanguage)

		if dryRun {
			// Compute-only: no outbound request, no watermark. Record what WOULD be
			// sent (type + date + destination HOST only) so the CLI can print an
			// auditable preview without leaking the URL or token.
			report.DryRunPreview = append(report.DryRunPreview, NotifyPreviewLine{
				OwnerID:   record.ID,
				Type:      reminder.Type,
				EventDate: reminder.EventDate.Format("2006-01-02"),
				Host:      host,
			})
			continue
		}

		// Claim the send BEFORE the request leaves. previousWatermark is the value
		// this pass's snapshot carried: it is what the claim compares against — so a
		// pass whose snapshot has been overtaken loses rather than writing the column
		// backwards — and it is what a failed delivery restores.
		//
		// record.WebhookConfigVersion is pinned alongside it, and it is the SAME
		// snapshot's value on purpose: everything this loop is about to send was
		// decided from that snapshot — the enabled flag, the per-kind opt-in, the
		// decrypted URL above — so the claim has to ask whether THAT configuration
		// is still the owner's, not whether some configuration is. Re-reading the
		// row here to get a fresher epoch would defeat the check by agreeing with
		// whatever the revocation just wrote.
		previousWatermark := watermarkForReminderType(settings, reminder.Type)
		claimed, err := service.users.ClaimWebhookWatermark(ctx, record.ID, reminder.Type, reminder.CycleAnchor, previousWatermark, record.WebhookConfigVersion)
		if err != nil {
			// Owner id only: the reminder type is a health specific, and this line
			// lands in whatever log the pass was started from. Nothing was delivered
			// and no watermark moved, so the reminder is still pending — counted as
			// failed so an operator sees it, and retried by the next pass.
			log.Printf("webhook notify: watermark claim failed, owner id=%d", record.ID)
			report.Failed++
			report.OwnerIDsFailed = appendUniqueID(report.OwnerIDsFailed, record.ID)
			continue
		}
		if !claimed {
			// Either a concurrent pass owns this send — the reminder is being
			// delivered, just not by us — or the owner revoked the configuration this
			// pass read, and there is nothing left to deliver to. Both are a skip
			// rather than a failure, and in both cases sending anyway is exactly the
			// egress the claim exists to prevent: a duplicate in the first case, a
			// POST to a revoked endpoint in the second. The pass deliberately does not
			// distinguish them, because the log line it could write would have to name
			// the owner whose endpoint was revoked, and the correct action is
			// identical.
			report.SkippedIdempotent++
			continue
		}

		if err := service.deliverer.Deliver(ctx, decryptedURL, payload); err != nil {
			// Delivery already logged host-only inside Deliver. Give the claim back so
			// the watermark is left where this pass found it and the next pass retries;
			// a claim kept after a failed POST would read exactly like a successful send
			// and suppress the reminder for good.
			report.Failed++
			report.OwnerIDsFailed = appendUniqueID(report.OwnerIDsFailed, record.ID)
			if releaseErr := service.users.ReleaseWebhookWatermark(ctx, record.ID, reminder.Type, reminder.CycleAnchor, previousWatermark); releaseErr != nil {
				log.Printf("webhook notify: watermark release failed after a failed delivery, owner id=%d", record.ID)
			}
			continue
		}

		// Success: the claim stands as the watermark for this kind, so a re-run
		// skips it. No second write is needed — the claim WAS the write.
		report.Sent++
	}
}

// watermarkForReminderType returns the incoming watermark of the kind a reminder
// belongs to, as the pass's record snapshot carried it. It is both the value the
// claim's compare-and-set is taken against and the value a released claim
// restores, so "unchanged after a failed delivery" means exactly the value this
// pass decided against.
func watermarkForReminderType(settings WebhookReminderSettings, reminderType string) *time.Time {
	if reminderType == DueReminderTypeOvulation {
		return settings.OvulationWatermark
	}
	return settings.PeriodWatermark
}

// buildPayload turns a due reminder into the transport-free notification body.
// Every localized field — the headline, the sentence, and the medical-safety
// disclaimer — resolves at ONE language: the owner's persisted
// users.interface_language, which the projection carries because a request-free
// pass has no browser to ask. An owner who never chose a language stores "",
// which the provider answers at the server default. Health specifics stay
// minimized to type + estimated date + lead days.
func (service *WebhookNotifyService) buildPayload(reminder DueReminder, language string) WebhookPayload {
	disclaimer := service.localized.Disclaimer(language)
	title, message := service.reminderCopy(reminder, language)
	return WebhookPayload{
		Title:      title,
		Message:    message,
		Disclaimer: disclaimer,
		Type:       reminder.Type,
		EventDate:  reminder.EventDate.Format("2006-01-02"),
		LeadDays:   reminder.LeadDays,
	}
}

// reminderCopy returns the minimal, secret-free title and message for a
// reminder, resolved at language. Both come from the locale catalogue: the copy
// used to be English literals here, which put the reminder body outside the
// six-file locale contract (no translator ever saw a key for it) and sent an
// owner a headline in one language beside a disclaimer in another. The sentence
// template carries a single %s for the ISO event date.
func (service *WebhookNotifyService) reminderCopy(reminder DueReminder, language string) (string, string) {
	titleKey, messageKey := reminderPeriodTitleKey, reminderPeriodMessageKey
	if reminder.Type == DueReminderTypeOvulation {
		titleKey, messageKey = reminderOvulationTitleKey, reminderOvulationMessageKey
	}
	title := service.localized.Message(language, titleKey)
	template := service.localized.Message(language, messageKey)
	if template == "" {
		// No catalogue entry (a provider without the key): send the headline alone
		// rather than a body of formatting-verb residue.
		return title, ""
	}
	return title, fmt.Sprintf(template, reminder.EventDate.Format("2006-01-02"))
}

// hostOnly returns the hostname of a URL and nothing else — no scheme, port,
// path, query, or userinfo (which may carry a notification token). It is the
// only form of a webhook URL that may appear in a preview, a settings page, or a
// log, and it is the package's ONLY implementation of that rule: the notify
// pass, the CLI status view, and BuildWebhookURLDisplay all print through it, so
// a hardening of "what is safe to show" reaches every surface at once. Returns
// "" when the URL cannot be parsed, which the settings display renders as
// configured-but-hostless rather than as an error.
func hostOnly(rawURL string) string {
	parsed, err := url.Parse(strings.TrimSpace(rawURL))
	if err != nil {
		return ""
	}
	return parsed.Hostname()
}

// resolveOwnerLocation prefers the owner's persisted IANA timezone and falls
// back to the injected location when it is empty or unusable. It never calls
// time.Now(); it only resolves a zone.
//
// It is the single owner-timezone resolver for every request-free pass — the two
// egress passes (the webhook notify pass and the .ics calendar feed) and the
// one-shot boot recompute of the derived luteal-phase cache
// (LutealPhaseRecomputer) — so none of them can disagree about which calendar day
// an owner is on. None carries a request timezone worth trusting (a cron pass has
// no request at all, a calendar client sends neither the timezone header nor the
// cookie, and a boot pass runs before a listener exists), which is exactly the
// case users.timezone is persisted for.
//
// The "Local" token is rejected by INPUT, mirroring api.parseRequestTimezone:
// time.LoadLocation("Local") returns the server's own zone (time.Local) with no
// error, so a stored "Local" would silently pin an owner to the server's
// timezone instead of falling back. Only validated IANA names are ever written to
// the column, so this is defense in depth against a hand-edited or restored row —
// and it is checked on the input, not on the loaded zone's String(), because
// time.Local stringifies to its real name (e.g. "UTC") whenever TZ is set.
func resolveOwnerLocation(timezone string, fallback *time.Location) *time.Location {
	name := strings.TrimSpace(timezone)
	if name == "" {
		return fallback
	}
	if strings.EqualFold(name, "Local") {
		return fallback
	}
	loaded, err := time.LoadLocation(name)
	if err != nil {
		return fallback
	}
	return loaded
}

// userFromNotifyRecord builds the minimal *models.User the pure decision layer
// needs from the notify projection. It copies ONLY the cycle-prediction inputs,
// plus Role: every account is RoleOwner by invariant (there is no other role),
// so it is a constant here, never read from the projection. ApplyUserCycleBaseline
// gates on user.Role, so omitting it would silently skip the owner cycle baseline
// (last-period anchor, cycle-length bootstrap, inferred luteal phase) that the
// dashboard and .ics feed both apply. No credential, security-posture, or
// webhook-secret field is set, so the decision can never read one.
func userFromNotifyRecord(record models.WebhookNotifyRecord) models.User {
	return models.User{
		ID:                 record.ID,
		Role:               models.RoleOwner,
		CycleLength:        record.CycleLength,
		PeriodLength:       record.PeriodLength,
		LutealPhase:        record.LutealPhase,
		IrregularCycle:     record.IrregularCycle,
		UnpredictableCycle: record.UnpredictableCycle,
		LastPeriodStart:    record.LastPeriodStart,
	}
}

// appendUniqueID appends id to ids only if not already present, keeping the
// failed-owner list free of duplicates when an owner has multiple failing
// reminders.
func appendUniqueID(ids []uint, id uint) []uint {
	for _, existing := range ids {
		if existing == id {
			return ids
		}
	}
	return append(ids, id)
}
