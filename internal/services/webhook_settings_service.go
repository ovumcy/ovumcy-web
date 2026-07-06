package services

import (
	"context"
	"errors"
	"fmt"
	"net/url"
	"strings"

	"github.com/ovumcy/ovumcy-web/internal/models"
	"github.com/ovumcy/ovumcy-web/internal/security"
)

const (
	// MinReminderLeadDays / MaxReminderLeadDays bound the SHARED per-owner lead
	// window (banner + webhooks). 0 means "only on the day itself"; 14 is a
	// generous upper limit that still keeps a reminder actionable. Values
	// outside the range are clamped, never rejected — this is a numeric
	// preference, not a security input.
	MinReminderLeadDays = 0
	MaxReminderLeadDays = 14
	// DefaultReminderLeadDays mirrors the column default and
	// DashboardReminderBannerWindowDays; re-exported from models so callers in
	// this package have it alongside the bound constants.
	DefaultReminderLeadDays = models.DefaultReminderLeadDays
)

// ErrWebhookURLInvalid is returned by SaveWebhookSettings when webhook delivery
// is enabled but the supplied URL is empty, unparseable, or uses a scheme other
// than http/https. It never carries the offending URL so the value cannot leak
// through an error string into a log or response.
var ErrWebhookURLInvalid = errors.New("webhook url invalid")

// aadForWebhookURL returns the additional-authenticated-data binding an
// encrypted webhook URL to a single user's row. It parallels aadForTOTPSecret
// (a deliberately separate helper, not a shared one): including the user id
// prevents an attacker with database write access from swapping one owner's
// webhook_url ciphertext into another owner's row and having DecryptField open
// it under aad "ovumcy.field.webhook_url:<other id>".
func aadForWebhookURL(userID uint) []byte {
	return []byte(fmt.Sprintf("ovumcy.field.webhook_url:%d", userID))
}

// WebhookSettingsUpdate is the transport-free input to SaveWebhookSettings. URL
// is the plaintext endpoint as entered by the owner; the service encrypts it
// before it reaches persistence.
type WebhookSettingsUpdate struct {
	Enabled          bool
	URL              string
	NotifyPeriod     bool
	NotifyOvulation  bool
	ReminderLeadDays int
}

// WebhookSettingsFormUpdate is the transport-free input to
// SaveWebhookSettingsFromForm, the write-only-field save path used by the
// settings UI (issue #124). The URL field on the settings page is write-only —
// it renders blank and the stored secret is never echoed — so a save that omits
// the URL means "leave the endpoint unchanged", while a distinct remove
// affordance clears it. The transport layer sets these flags; the semantics
// (keep / replace / remove) live entirely in SaveWebhookSettingsFromForm.
type WebhookSettingsFormUpdate struct {
	Enabled         bool
	NotifyPeriod    bool
	NotifyOvulation bool
	// URL is the newly-entered plaintext endpoint. Empty means the owner left
	// the write-only field blank.
	URL string
	// URLProvided is true when the owner typed a non-blank URL this submission.
	// When false, the stored endpoint is preserved as-is.
	URLProvided bool
	// RemoveURL is the explicit "remove endpoint" affordance: it clears the
	// stored URL and forces delivery off, taking precedence over URLProvided.
	RemoveURL bool
}

// WebhookSettingsRepository is the narrow persistence surface the webhook
// settings service needs. SaveWebhookSettings writes the settings columns
// (with webhook_url already ciphertext); it deliberately does NOT bump
// auth_session_version — changing a notification preference is not a change to
// the account's security posture, so no active session should be revoked.
// LoadSettingsByID lets the write-only form save read the currently-stored
// ciphertext so a blank URL submission can mean "leave the endpoint unchanged"
// without the endpoint ever round-tripping through transport.
type WebhookSettingsRepository interface {
	SaveWebhookSettings(ctx context.Context, userID uint, settings models.WebhookSettingsColumns) error
	LoadSettingsByID(ctx context.Context, userID uint) (models.User, error)
}

// WebhookSettingsService owns the business logic for persisting an owner's
// webhook notification settings: URL validation, encryption at rest, and
// lead-day clamping. It holds the application secretKey to encrypt the URL,
// mirroring TOTPService.
type WebhookSettingsService struct {
	users     WebhookSettingsRepository
	secretKey []byte
}

// NewWebhookSettingsService creates a WebhookSettingsService. secretKey is used
// to encrypt the webhook URL before it is written to the database (the same key
// that encrypts TOTP secrets).
func NewWebhookSettingsService(users WebhookSettingsRepository, secretKey []byte) *WebhookSettingsService {
	return &WebhookSettingsService{users: users, secretKey: secretKey}
}

// NormalizeReminderLeadDays clamps a lead-day value into [MinReminderLeadDays,
// MaxReminderLeadDays]. Exposed so the settings render/view layer can present
// the same bounded value the service will persist.
func NormalizeReminderLeadDays(value int) int {
	if value < MinReminderLeadDays {
		return MinReminderLeadDays
	}
	if value > MaxReminderLeadDays {
		return MaxReminderLeadDays
	}
	return value
}

// ValidateWebhookURL trims and parses a candidate webhook URL, returning the
// cleaned value on success. It accepts ONLY absolute http/https URLs with a
// host; every other scheme (file, gopher, javascript, data, ftp, …) and any
// relative or hostless value is rejected with ErrWebhookURLInvalid. The error
// never embeds the candidate, so an invalid URL cannot leak into logs.
//
// This is a save-time scheme/shape guard only. Outbound SSRF defenses
// (blocking loopback/link-local/metadata targets at delivery time) belong to
// the later delivery slice, not here.
func ValidateWebhookURL(raw string) (string, error) {
	candidate := strings.TrimSpace(raw)
	if candidate == "" {
		return "", ErrWebhookURLInvalid
	}
	// Reject CR/LF defensively so a crafted URL can never be smuggled into a
	// later request line or header when the delivery slice consumes it.
	if strings.ContainsAny(candidate, "\r\n") {
		return "", ErrWebhookURLInvalid
	}
	parsed, err := url.Parse(candidate)
	if err != nil {
		return "", ErrWebhookURLInvalid
	}
	scheme := strings.ToLower(parsed.Scheme)
	if scheme != "http" && scheme != "https" {
		return "", ErrWebhookURLInvalid
	}
	if parsed.Host == "" {
		return "", ErrWebhookURLInvalid
	}
	return candidate, nil
}

// SaveWebhookSettings validates and persists an owner's webhook notification
// settings, scoped to userID.
//
//   - reminder_lead_days is clamped into the sane bound (never errors).
//   - When Enabled is true the URL must be a valid http/https URL; otherwise the
//     save is refused with ErrWebhookURLInvalid so a webhook can never be armed
//     without a deliverable target.
//   - The URL is encrypted with security.EncryptField, aad-bound to userID, and
//     only the ciphertext is handed to persistence. An empty URL (disabled with
//     no endpoint) is stored as an empty string, not encrypted.
//
// It does not bump auth_session_version: a notification-preference change is not
// a security-posture change.
func (service *WebhookSettingsService) SaveWebhookSettings(ctx context.Context, userID uint, update WebhookSettingsUpdate) error {
	columns := models.WebhookSettingsColumns{
		Enabled:          update.Enabled,
		NotifyPeriod:     update.NotifyPeriod,
		NotifyOvulation:  update.NotifyOvulation,
		ReminderLeadDays: NormalizeReminderLeadDays(update.ReminderLeadDays),
	}

	trimmedURL := strings.TrimSpace(update.URL)
	switch {
	case update.Enabled:
		validated, err := ValidateWebhookURL(trimmedURL)
		if err != nil {
			return err
		}
		ciphertext, err := security.EncryptField(validated, service.secretKey, aadForWebhookURL(userID))
		if err != nil {
			return fmt.Errorf("webhook url encrypt failed: %w", err)
		}
		columns.EncryptedURL = ciphertext
	case trimmedURL == "":
		// Disabled and no endpoint supplied: clear any stored ciphertext.
		columns.EncryptedURL = ""
	default:
		// Disabled but an endpoint was supplied (owner turned delivery off but
		// kept the URL in the form): still validate the shape and persist the
		// ciphertext so re-enabling later does not need re-entry, but never
		// store an unparseable/other-scheme value.
		validated, err := ValidateWebhookURL(trimmedURL)
		if err != nil {
			return err
		}
		ciphertext, err := security.EncryptField(validated, service.secretKey, aadForWebhookURL(userID))
		if err != nil {
			return fmt.Errorf("webhook url encrypt failed: %w", err)
		}
		columns.EncryptedURL = ciphertext
	}

	return service.users.SaveWebhookSettings(ctx, userID, columns)
}

// SaveWebhookSettingsFromForm applies a write-only-field save from the settings
// UI, scoped to userID. It resolves the endpoint the owner intends before
// delegating to SaveWebhookSettings (which owns validation + encryption), so the
// keep/replace/remove policy lives in one place and the handler never decrypts,
// re-encrypts, or re-implements the scheme rules:
//
//   - RemoveURL: clear the stored endpoint and force delivery off. Takes
//     precedence over everything else.
//   - URLProvided: use the newly-entered URL (validated + re-encrypted).
//   - neither: preserve the currently-stored endpoint. It is decrypted from the
//     owner's row and re-supplied to SaveWebhookSettings so enabling delivery on
//     a previously-saved URL needs no re-entry. A stored ciphertext that no
//     longer opens (e.g. after SECRET_KEY rotation) surfaces as
//     ErrWebhookURLInvalid when Enabled — the owner must re-enter the URL.
//
// Like SaveWebhookSettings it does not bump auth_session_version.
func (service *WebhookSettingsService) SaveWebhookSettingsFromForm(ctx context.Context, userID uint, form WebhookSettingsFormUpdate) error {
	update := WebhookSettingsUpdate{
		NotifyPeriod:    form.NotifyPeriod,
		NotifyOvulation: form.NotifyOvulation,
		// Slice-1 scope: the settings UI does not edit reminder_lead_days here
		// (PR #168 owns that control). Preserve the persisted value so this save
		// never clobbers it back to a default.
		ReminderLeadDays: DefaultReminderLeadDays,
	}

	current, err := service.users.LoadSettingsByID(ctx, userID)
	if err != nil {
		return err
	}
	update.ReminderLeadDays = current.ReminderLeadDays

	switch {
	case form.RemoveURL:
		update.Enabled = false
		update.URL = ""
	case form.URLProvided:
		update.Enabled = form.Enabled
		update.URL = form.URL
	default:
		update.Enabled = form.Enabled
		storedURL, decryptErr := service.DecryptWebhookURL(userID, current.WebhookURL)
		if decryptErr != nil {
			// The stored ciphertext will not open. If the owner is enabling
			// delivery we must fail loudly (SaveWebhookSettings rejects an empty
			// URL when enabled) so they re-enter it; if disabling, an empty URL
			// is fine and clears the un-openable value.
			update.URL = ""
			break
		}
		update.URL = storedURL
	}

	return service.SaveWebhookSettings(ctx, userID, update)
}

// DecryptWebhookURL opens a stored webhook_url ciphertext for the given user,
// returning the plaintext endpoint. It is the read-side counterpart to the
// encryption in SaveWebhookSettings and will be used by the future notify pass.
// An empty stored value yields an empty URL with no error (webhook configured
// with no endpoint / disabled). A ciphertext that fails to open — e.g. after a
// SECRET_KEY rotation — returns an error so the caller can fail safe and skip
// that owner rather than deliver to a garbage target.
func (service *WebhookSettingsService) DecryptWebhookURL(userID uint, encryptedURL string) (string, error) {
	if strings.TrimSpace(encryptedURL) == "" {
		return "", nil
	}
	plaintext, _, err := security.DecryptField(encryptedURL, service.secretKey, aadForWebhookURL(userID))
	if err != nil {
		return "", err
	}
	return plaintext, nil
}

// WebhookURLDisplay is the ONLY webhook-endpoint projection the settings surface
// may render. The stored URL is a secret (it can embed an ntfy/Gotify token), so
// it is never echoed back into an HTML value/attribute: Configured says whether a
// deliverable endpoint exists, and Host carries at most the hostname
// (u.Hostname() — never scheme, path, query, or userinfo). A ciphertext that
// fails to open still counts as Configured=true (an endpoint is stored) but with
// an empty Host, so the UI shows "configured" without leaking or fabricating a
// host.
type WebhookURLDisplay struct {
	Configured bool
	Host       string
}

// BuildWebhookURLDisplay derives the render-safe status/host projection for a
// stored webhook_url ciphertext, scoped to userID (the AAD binds the ciphertext
// to the owner). It decrypts only to extract the hostname and deliberately
// discards the rest of the URL, so no caller can obtain the full secret through
// this seam. An empty stored value yields the zero value (not configured). A
// ciphertext that fails to open is reported as configured-but-hostless rather
// than as an error: the settings page must still render, and the owner can
// re-save to restore a decryptable endpoint.
func (service *WebhookSettingsService) BuildWebhookURLDisplay(userID uint, encryptedURL string) WebhookURLDisplay {
	if strings.TrimSpace(encryptedURL) == "" {
		return WebhookURLDisplay{}
	}
	plaintext, err := service.DecryptWebhookURL(userID, encryptedURL)
	if err != nil {
		return WebhookURLDisplay{Configured: true}
	}
	return WebhookURLDisplay{Configured: true, Host: webhookURLHost(plaintext)}
}

// webhookURLHost returns the hostname component of a stored webhook URL and
// nothing else — no scheme, port, path, query, or userinfo — so a token embedded
// anywhere but the host can never reach a render surface. An unparseable value
// yields an empty host (the caller still shows "configured").
func webhookURLHost(raw string) string {
	parsed, err := url.Parse(strings.TrimSpace(raw))
	if err != nil {
		return ""
	}
	return parsed.Hostname()
}
