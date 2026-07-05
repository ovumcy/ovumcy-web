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

// WebhookSettingsRepository is the narrow persistence surface the webhook
// settings service needs. SaveWebhookSettings writes the settings columns
// (with webhook_url already ciphertext); it deliberately does NOT bump
// auth_session_version — changing a notification preference is not a change to
// the account's security posture, so no active session should be revoked.
type WebhookSettingsRepository interface {
	SaveWebhookSettings(ctx context.Context, userID uint, settings models.WebhookSettingsColumns) error
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
