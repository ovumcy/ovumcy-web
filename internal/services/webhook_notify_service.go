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
// Idempotency: the watermark is written only after a 2xx delivery. A failed POST
// leaves the watermark unchanged, so the next pass (a same-day re-run or a
// concurrent operator cron) retries it; a succeeded POST advances the watermark,
// so a re-run skips it. This write-on-success is the coordination point with
// slice-1's watermark columns.
//
// No-secret discipline: the pass logs counts and at most owner ids — never the
// URL, token, decrypted health specifics, or payload.

// NotifyUserRepository is the narrow persistence surface the notify pass needs.
// It lists the per-owner notify projection and advances a per-kind watermark.
type NotifyUserRepository interface {
	// ListAllForNotify returns the per-owner notify projection (webhook_url is
	// CIPHERTEXT). Ordered by id for a deterministic pass.
	ListAllForNotify(ctx context.Context) ([]models.WebhookNotifyRecord, error)
	// UpdateWebhookWatermark advances the per-kind watermark for one owner after a
	// successful send. cycleAnchor is canonicalized to UTC-midnight by the repo.
	UpdateWebhookWatermark(ctx context.Context, userID uint, reminderType string, cycleAnchor time.Time) error
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
// satisfied by an i18n adapter; the notify pass uses it so every payload carries
// the owner-localized (or server-default) "estimates, not medical advice"
// string without importing the whole i18n Manager.
type DisclaimerProvider interface {
	Disclaimer(language string) string
}

// NotifyReport is the transport-free result of one notify pass. It carries only
// aggregate counts — never a URL, token, health specific, or payload — so it is
// safe to print from the CLI. OwnerIDsFailed lets an operator see WHICH owners
// failed (an id is not a secret) without exposing why in a way that leaks the
// endpoint.
type NotifyReport struct {
	// OwnersScanned is the number of owner records the pass examined.
	OwnersScanned int
	// Due is the number of reminders the decision layer reported as due across all
	// owners (before delivery).
	Due int
	// Sent is the number of reminders successfully delivered (2xx). Always 0 on a
	// dry run.
	Sent int
	// SkippedIdempotent is the number of due reminders skipped because the incoming
	// watermark already covered them (they were sent on an earlier pass). The pure
	// decision layer excludes these, so this counts owners whose decision returned
	// nothing purely due to watermarks — surfaced for observability.
	SkippedIdempotent int
	// Failed is the number of reminders whose delivery failed (non-2xx, timeout,
	// refused redirect, bad scheme). Their watermark was NOT advanced.
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
	users      NotifyUserRepository
	logs       NotifyLogReader
	decryptor  WebhookURLDecryptor
	deliverer  WebhookDeliverer
	disclaimer DisclaimerProvider
}

// NewWebhookNotifyService assembles the notify service from its collaborators.
func NewWebhookNotifyService(
	users NotifyUserRepository,
	logs NotifyLogReader,
	decryptor WebhookURLDecryptor,
	deliverer WebhookDeliverer,
	disclaimer DisclaimerProvider,
) *WebhookNotifyService {
	return &WebhookNotifyService{
		users:      users,
		logs:       logs,
		decryptor:  decryptor,
		deliverer:  deliverer,
		disclaimer: disclaimer,
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
	due := DecideDueReminders(&user, settings, dayLogs, now, ownerLocation)

	// Idempotency observability: the pure decision already hides reminders whose
	// watermark covers them, so re-run the SAME decision with the watermarks
	// cleared to learn how many candidates existed, and attribute the difference
	// (candidates that were suppressed purely by an existing watermark) to
	// SkippedIdempotent. This is a second pure, I/O-free decision call — no store,
	// no clock — kept so the Report can prove "sent once, then skipped" without
	// changing the authoritative (watermarked) decision above.
	report.SkippedIdempotent += countWatermarkSuppressed(&user, settings, dayLogs, now, ownerLocation, due)

	// Destination HOST only, for the dry-run preview and any host-scoped logging.
	// Never keep or print more of the URL than this.
	host := hostOnly(decryptedURL)

	for _, reminder := range due {
		report.Due++
		payload := service.buildPayload(reminder)

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

		if err := service.deliverer.Deliver(ctx, decryptedURL, payload); err != nil {
			// Delivery already logged host-only inside Deliver. Leave the watermark
			// unchanged so the next pass retries.
			report.Failed++
			report.OwnerIDsFailed = appendUniqueID(report.OwnerIDsFailed, record.ID)
			continue
		}

		// Success: advance the watermark for this kind so a re-run skips it. A
		// watermark write failure is logged but not counted as a delivery failure —
		// the reminder WAS delivered; a stuck watermark would at worst re-send next
		// pass, never lose data.
		if err := service.users.UpdateWebhookWatermark(ctx, record.ID, reminder.Type, reminder.CycleAnchor); err != nil {
			log.Printf("webhook notify: watermark write failed after send, owner id=%d type=%s", record.ID, reminder.Type)
		}
		report.Sent++
	}
}

// buildPayload turns a due reminder into the transport-free notification body.
// It resolves the disclaimer at the server-default language (no per-owner
// language is persisted) and minimizes health specifics to type + estimated date
// + lead days.
func (service *WebhookNotifyService) buildPayload(reminder DueReminder) WebhookPayload {
	disclaimer := service.disclaimer.Disclaimer("")
	title, message := reminderCopy(reminder)
	return WebhookPayload{
		Title:      title,
		Message:    message,
		Disclaimer: disclaimer,
		Type:       reminder.Type,
		EventDate:  reminder.EventDate.Format("2006-01-02"),
		LeadDays:   reminder.LeadDays,
	}
}

// reminderCopy returns a neutral, secret-free title and message for a reminder.
// The copy is intentionally minimal and English-neutral machine text; the
// disclaimer field carries the localized medical-safety string.
func reminderCopy(reminder DueReminder) (string, string) {
	date := reminder.EventDate.Format("2006-01-02")
	switch reminder.Type {
	case DueReminderTypeOvulation:
		return "Ovulation reminder", fmt.Sprintf("Estimated ovulation around %s.", date)
	default:
		return "Period reminder", fmt.Sprintf("Estimated next period around %s.", date)
	}
}

// countWatermarkSuppressed reports how many reminders WOULD be due for this
// owner if no watermark existed but are absent from due (i.e. were suppressed
// purely because an incoming watermark already covered them). It re-runs the
// pure decision with both watermarks cleared and subtracts, by type, the
// reminders that actually surfaced. It performs no I/O.
func countWatermarkSuppressed(
	user *models.User,
	settings WebhookReminderSettings,
	logs []models.DailyLog,
	now time.Time,
	location *time.Location,
	due []DueReminder,
) int {
	unwatermarked := settings
	unwatermarked.PeriodWatermark = nil
	unwatermarked.OvulationWatermark = nil
	candidates := DecideDueReminders(user, unwatermarked, logs, now, location)

	dueTypes := make(map[string]bool, len(due))
	for _, reminder := range due {
		dueTypes[reminder.Type] = true
	}

	suppressed := 0
	for _, candidate := range candidates {
		if !dueTypes[candidate.Type] {
			suppressed++
		}
	}
	return suppressed
}

// hostOnly returns the hostname of a URL and nothing else — no scheme, path,
// query, or userinfo (which may carry a notification token). It is the only form
// of a webhook URL that may appear in a preview or log. Returns "" when the URL
// cannot be parsed.
func hostOnly(rawURL string) string {
	parsed, err := url.Parse(strings.TrimSpace(rawURL))
	if err != nil {
		// codecov:ignore -- unreachable from the notify path: processOwner only
		// reaches here for a URL the decryptor returned, which slice-1 save-time
		// validation already parsed; kept as a fail-safe so a malformed value yields
		// an empty host rather than leaking anything.
		return ""
	}
	return parsed.Hostname()
}

// resolveOwnerLocation prefers the owner's persisted IANA timezone and falls
// back to the injected server location when it is empty or invalid. It never
// calls time.Now(); it only resolves a zone.
func resolveOwnerLocation(timezone string, fallback *time.Location) *time.Location {
	name := strings.TrimSpace(timezone)
	if name == "" {
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
