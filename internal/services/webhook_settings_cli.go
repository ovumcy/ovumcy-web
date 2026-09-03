package services

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"github.com/ovumcy/ovumcy-web/internal/models"
)

// ErrWebhookOwnerNotFound is returned by the CLI-facing webhook settings
// orchestration when no owner matches the supplied email. It never carries the
// email so an operator log of the error cannot echo an account identifier.
var ErrWebhookOwnerNotFound = errors.New("webhook owner not found")

// WebhookOwnerReader is the narrow read surface the CLI orchestration needs to
// resolve the target owner (by normalized email) and load its CURRENT webhook
// settings before applying a partial change. It is deliberately separate from
// WebhookSettingsRepository (the slice-1 write surface) so the save path's
// interface stays minimal; *db.UserRepository satisfies both.
type WebhookOwnerReader interface {
	FindByNormalizedEmailOptional(ctx context.Context, email string) (models.User, bool, error)
}

// WebhookSettingsView is the SAFE, transport-free projection of an owner's
// webhook settings for operator surfaces (the CLI show/set output). It carries
// the toggle state, the lead window, and — for the endpoint — only the HOST,
// never the full URL, path, query, or userinfo (which can embed an ntfy/Gotify
// token). Readability reports what this instance can say about the stored ciphertext.
type WebhookSettingsView struct {
	// Readability is the same three-valued answer the web surface renders --
	// absent, unreadable, readable -- and for the same reason: an endpoint this
	// instance can no longer open is not the same fact as one it can, and the
	// operator is the person who has to act on the difference.
	Readability WebhookURLReadability
	Enabled     bool
	// Host is the destination hostname only (e.g. "ntfy.example.io"), or "" when
	// no endpoint is configured. It is the single form of a webhook URL that may
	// appear in operator output — never the scheme/path/query/token.
	Host            string
	NotifyPeriod    bool
	NotifyOvulation bool
	// ReminderLeadDays is the lead window IN FORCE — always clamped through
	// NormalizeReminderLeadDays, the same value DecideDueReminders applies —
	// never the raw stored column. Both builders of this struct clamp, so `show`
	// and `set` cannot report two different readings of one window.
	ReminderLeadDays int
}

// webhookURLAction selects how a patch treats the stored endpoint URL.
type webhookURLAction int

const (
	// webhookURLKeep leaves the stored endpoint unchanged (only toggles/lead
	// change). This is the default so a flag-only edit never disturbs the URL.
	webhookURLKeep webhookURLAction = iota
	// webhookURLSet replaces the endpoint with a new plaintext URL (validated +
	// encrypted by the reused SaveWebhookSettings path).
	webhookURLSet
	// webhookURLClear removes any stored endpoint (stored as empty ciphertext).
	webhookURLClear
)

// WebhookSettingsPatch is the transport-free partial update the CLI builds from
// its flags. Every field is tri-state: a nil pointer means "leave as-is", so a
// single flag toggles exactly one setting without resetting the others. URLAction
// governs the secret endpoint; NewURL is the plaintext URL only when
// URLAction == webhookURLSet.
type WebhookSettingsPatch struct {
	Enabled          *bool
	NotifyPeriod     *bool
	NotifyOvulation  *bool
	ReminderLeadDays *int
	URLAction        webhookURLAction
	// NewURL is the plaintext endpoint to store when URLAction is webhookURLSet.
	// It is a secret (can embed a token) and is never logged or echoed; the
	// service encrypts it via the reused SaveWebhookSettings path.
	NewURL string
}

// SetURL records a plaintext endpoint to store. The value is a secret and must
// never be echoed by the caller.
func (patch *WebhookSettingsPatch) SetURL(rawURL string) {
	patch.URLAction = webhookURLSet
	patch.NewURL = rawURL
}

// ClearURL records that any stored endpoint should be removed.
func (patch *WebhookSettingsPatch) ClearURL() {
	patch.URLAction = webhookURLClear
	patch.NewURL = ""
}

// WebhookSettingsCLIService orchestrates the operator CLI webhook-settings flow:
// resolve an owner by email, project the current settings as a host-only status
// view, and apply a partial change by merging a patch onto the current state and
// persisting it through the SAME slice-1 SaveWebhookSettings path (validation,
// encryption at rest, lead-day clamp, no auth_session_version bump). It adds no
// new persistence: reads go through WebhookOwnerReader, writes and decryption
// through the embedded WebhookSettingsService.
type WebhookSettingsCLIService struct {
	reader   WebhookOwnerReader
	settings *WebhookSettingsService
}

// NewWebhookSettingsCLIService builds the CLI orchestration from an owner reader
// and the slice-1 settings service (which holds the secretKey for encrypt/
// decrypt). Both collaborate over the same *db.UserRepository in production.
func NewWebhookSettingsCLIService(reader WebhookOwnerReader, settings *WebhookSettingsService) *WebhookSettingsCLIService {
	return &WebhookSettingsCLIService{reader: reader, settings: settings}
}

// ResolveWebhookSettings loads the current webhook settings for the owner with
// the given email and returns a host-only status view. It returns
// ErrWebhookOwnerNotFound when no owner matches and ErrOperatorUserEmail* when
// the email is blank/invalid. The stored endpoint ciphertext is decrypted only
// to derive the host; the plaintext URL never leaves this method.
func (service *WebhookSettingsCLIService) ResolveWebhookSettings(ctx context.Context, email string) (WebhookSettingsView, error) {
	_, view, _, err := service.resolveOwner(ctx, email)
	if err != nil {
		return WebhookSettingsView{}, err
	}
	return view, nil
}

// ApplyWebhookSettings resolves the owner, merges the patch onto the current
// settings, and — unless dryRun — persists the result through the reused
// SaveWebhookSettings path (which validates the URL scheme/shape, encrypts it
// aad-bound to the owner id, and clamps the lead days). It returns the resulting
// status view (host-only). On dryRun it validates the same way (so an invalid
// URL still errors) but writes nothing. An invalid/other-scheme URL is rejected
// with ErrWebhookURLInvalid and nothing is persisted.
func (service *WebhookSettingsCLIService) ApplyWebhookSettings(ctx context.Context, email string, patch WebhookSettingsPatch, dryRun bool) (WebhookSettingsView, error) {
	// resolveOwner already decrypts the stored endpoint once (to derive the host)
	// and returns the plaintext, so a URL-keeping patch re-persists the existing
	// endpoint unchanged without a second decrypt. The plaintext is confined to
	// this method; only the derived host is ever surfaced.
	owner, current, currentURL, err := service.resolveOwner(ctx, email)
	if err != nil {
		return WebhookSettingsView{}, err
	}

	update := WebhookSettingsUpdate{
		Enabled:          boolPatch(patch.Enabled, current.Enabled),
		NotifyPeriod:     boolPatch(patch.NotifyPeriod, current.NotifyPeriod),
		NotifyOvulation:  boolPatch(patch.NotifyOvulation, current.NotifyOvulation),
		ReminderLeadDays: intPatch(patch.ReminderLeadDays, current.ReminderLeadDays),
	}
	switch patch.URLAction {
	case webhookURLSet:
		update.URL = patch.NewURL
	case webhookURLClear:
		update.URL = ""
	default:
		// Keep — and any action added later without a case of its own, which
		// lands here for the same reason: preserving the endpoint is the safe
		// default. The two arms share one body deliberately. A keep-merge is the
		// one action that needs the plaintext, and on a row this instance cannot
		// open there is nothing to keep: re-persisting the empty string would
		// silently delete the endpoint the operator asked to leave alone. Guarding
		// only the named case would have left the fallback doing exactly that,
		// while its comment claimed it failed safe.
		if current.Readability == WebhookURLUnreadable {
			return WebhookSettingsView{}, ErrWebhookURLUnreadable
		}
		update.URL = currentURL
	}

	// Validate the resulting URL the same way on both paths so a dry-run reports
	// the same rejection a real save would. SaveWebhookSettings performs this
	// validation itself; for dry-run we invoke the shared validator directly and
	// skip persistence.
	if dryRun {
		if err := validateWebhookUpdateForDryRun(update); err != nil {
			return WebhookSettingsView{}, err
		}
	} else if err := service.settings.SaveWebhookSettings(ctx, owner.ID, update); err != nil {
		return WebhookSettingsView{}, err
	}

	return viewFromUpdate(update), nil
}

// resolveOwner looks up the owner by normalized email and returns the record, a
// host-only status view of the stored settings, and the DECRYPTED plaintext
// endpoint URL (empty when none is configured). It centralizes the email
// normalization + not-found mapping + the single URL decrypt shared by resolve
// and apply. The plaintext URL is an internal return: callers surface only the
// host, never the URL itself.
func (service *WebhookSettingsCLIService) resolveOwner(ctx context.Context, email string) (models.User, WebhookSettingsView, string, error) {
	normalizedEmail, err := normalizeOperatorUserEmail(email)
	if err != nil {
		return models.User{}, WebhookSettingsView{}, "", err
	}

	owner, found, err := service.reader.FindByNormalizedEmailOptional(ctx, normalizedEmail)
	if err != nil {
		return models.User{}, WebhookSettingsView{}, "", fmt.Errorf("%w: %v", ErrOperatorUserLookupFailed, err)
	}
	if !found {
		return models.User{}, WebhookSettingsView{}, "", ErrWebhookOwnerNotFound
	}

	// One projection rule for both surfaces. This path used to fail outright on a
	// ciphertext it could not open, which left the operator unable to SEE the
	// broken row and unable to clear it -- the one situation the CLI exists for.
	// It now reports the same three-valued readability the settings page renders;
	// only the merge that genuinely needs the plaintext refuses, below.
	display := service.settings.BuildWebhookURLDisplay(owner.ID, owner.WebhookURL)
	plaintextURL := ""
	if display.Readability == WebhookURLReadable {
		// Decrypt once more only where the plaintext is actually needed: a
		// URL-keeping merge re-persists the existing endpoint unchanged. The
		// display above deliberately discards it.
		plaintext, decryptErr := service.settings.DecryptWebhookURL(owner.ID, owner.WebhookURL)
		// codecov:ignore:start -- unreachable: this arm runs only when the
		// projection above already opened the same ciphertext under the same key
		// and owner id. Kept because the alternative is discarding an error, and
		// the day the two stop sharing one decrypt this fails closed.
		if decryptErr != nil {
			return models.User{}, WebhookSettingsView{}, "", ErrWebhookURLUnreadable
		}
		// codecov:ignore:end
		plaintextURL = plaintext
	}

	view := WebhookSettingsView{
		Readability:     display.Readability,
		Enabled:         owner.WebhookEnabled,
		Host:            display.Host,
		NotifyPeriod:    owner.WebhookNotifyPeriod,
		NotifyOvulation: owner.WebhookNotifyOvulation,
		// Clamped, exactly as viewFromUpdate and DecideDueReminders clamp it: the
		// view reports the window IN FORCE, never the raw stored column. A row
		// outside the bound — hand-edited, restored from a backup, or written
		// before the bound existed — otherwise made `show` print a window no
		// reminder can fire on, and a no-op `set` look like a change.
		ReminderLeadDays: NormalizeReminderLeadDays(owner.ReminderLeadDays),
	}
	return owner, view, plaintextURL, nil
}

// validateWebhookUpdateForDryRun applies the same URL scheme/shape guard
// SaveWebhookSettings enforces, without persisting. It mirrors the enabled/
// disabled+URL cases so a dry-run rejects exactly what a real save would.
func validateWebhookUpdateForDryRun(update WebhookSettingsUpdate) error {
	trimmedURL := strings.TrimSpace(update.URL)
	if !update.Enabled && trimmedURL == "" {
		return nil
	}
	if _, err := ValidateWebhookURL(trimmedURL); err != nil {
		return err
	}
	return nil
}

// viewFromUpdate projects the merged update into a host-only status view. It
// re-derives the host from the plaintext URL (never storing it) so the returned
// view mirrors what a subsequent ResolveWebhookSettings would report.
func viewFromUpdate(update WebhookSettingsUpdate) WebhookSettingsView {
	trimmedURL := strings.TrimSpace(update.URL)
	readability := WebhookURLAbsent
	if trimmedURL != "" {
		readability = WebhookURLReadable
	}
	return WebhookSettingsView{
		Readability:      readability,
		Enabled:          update.Enabled,
		Host:             hostOnly(trimmedURL),
		NotifyPeriod:     update.NotifyPeriod,
		NotifyOvulation:  update.NotifyOvulation,
		ReminderLeadDays: NormalizeReminderLeadDays(update.ReminderLeadDays),
	}
}

// boolPatch returns the override when non-nil, else the current value.
func boolPatch(override *bool, current bool) bool {
	if override != nil {
		return *override
	}
	return current
}

// intPatch returns the override when non-nil, else the current value.
func intPatch(override *int, current int) int {
	if override != nil {
		return *override
	}
	return current
}
