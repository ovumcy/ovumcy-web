package services

import (
	"context"
	"errors"
	"fmt"
	"net/url"
	"strconv"
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

// ErrWebhookURLUnreadable is returned when an operation needs the plaintext of a
// stored endpoint that this instance can no longer open -- the usual cause being
// a rotated or replaced SECRET_KEY. It is deliberately NOT raised by the paths
// that REMOVE or REPLACE the endpoint: a destination the instance cannot read is
// still one the owner may withdraw, and refusing there would leave the row
// permanently stuck. It never carries the ciphertext or the decrypt error text.
var ErrWebhookURLUnreadable = errors.New("webhook url unreadable")

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
	// KeepStoredURL asks the save to leave the stored endpoint column untouched
	// instead of writing URL. It is set only where there is no honest value to
	// write: the stored ciphertext will not open, so the plaintext this save
	// would re-encrypt does not exist. Enabled must be false alongside it -- an
	// endpoint the instance cannot read cannot be armed -- and the service
	// refuses the combination rather than trusting the caller.
	KeepStoredURL bool
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
	// RemoveWebhookDestination withdraws the endpoint WITHOUT touching the
	// per-kind opt-ins or the shared lead window, which SaveWebhookSettings
	// cannot express: it writes all three unconditionally.
	RemoveWebhookDestination(ctx context.Context, userID uint) error
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
// cleaned value on success. It accepts ONLY absolute http/https URLs naming a
// host and, when one is given, an in-range port; every other scheme (file,
// gopher, javascript, data, ftp, …), any relative, opaque or hostless value, and
// any port outside 1..65535 is rejected with ErrWebhookURLInvalid. The error
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
	if err := validateWebhookAuthority(parsed); err != nil {
		return "", err
	}
	return candidate, nil
}

// validateWebhookAuthority rejects an authority that parses but names no host to
// connect to. It is shared with the delivery boundary, which re-runs it: save-time
// validation never revisits a URL already in the database, so a row stored before
// this check existed would otherwise keep being delivered.
//
// A non-empty Host is NOT enough. url.Parse gives "http://:8080/" a Host of
// ":8080" with an empty Hostname(), so a port-only authority passes a Host != ""
// test; Go's dialer then reads the empty host as the unspecified address and
// connects to the local machine. Likewise Parse accepts any digits as a port, so
// the range is checked here rather than left to a late transport error.
func validateWebhookAuthority(parsed *url.URL) error {
	if parsed.Opaque != "" || parsed.Hostname() == "" {
		return ErrWebhookURLInvalid
	}
	if port := parsed.Port(); port != "" {
		number, err := strconv.Atoi(port)
		if err != nil || number < 1 || number > 65535 {
			return ErrWebhookURLInvalid
		}
	}
	return nil
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
//   - The delivery mark's fate travels with the write: unless this save can
//     prove the destination is unchanged, it asks the same UPDATE to NULL
//     webhook_last_delivered_at. See destinationNotProvablyUnchanged.
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

	if update.KeepStoredURL {
		// Nothing to validate and nothing to encrypt. ClearLastDeliveredAt stays
		// false deliberately rather than by omission: the mark's rule is that it
		// may not outlive the endpoint it describes, and this save leaves that
		// endpoint exactly where it is, so there is nothing for the rule to do.
		if update.Enabled {
			return fmt.Errorf("%w: delivery cannot be enabled over an endpoint this instance cannot read", ErrWebhookURLUnreadable)
		}
		columns.KeepEncryptedURL = true
		return service.users.SaveWebhookSettings(ctx, userID, columns)
	}

	trimmedURL := strings.TrimSpace(update.URL)
	// destination is the PLAINTEXT this save will store, in the same validated
	// form the previous save stored its own, so the two are comparable below.
	destination := ""
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
		destination = validated
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
		destination = validated
	}
	columns.ClearLastDeliveredAt = service.destinationNotProvablyUnchanged(ctx, userID, destination)

	return service.users.SaveWebhookSettings(ctx, userID, columns)
}

// destinationNotProvablyUnchanged decides the fate of the delivery mark for the
// save about to run: true asks persistence to NULL webhook_last_delivered_at in
// the same UPDATE that writes the new webhook_url.
//
// The question is deliberately asked in the negative. The mark says "a delivery
// to your endpoint was accepted", so keeping it is a claim about one specific
// destination and may only survive where this instance can PROVE the destination
// is the one the mark was about. Everything else clears it: a replaced URL, a
// removed one, a row that could not be read, and a stored ciphertext that no
// longer opens — after a SECRET_KEY rotation the previous destination is not
// merely different, it is unknowable, and the ledger must not assert about it.
//
// The comparison is on PLAINTEXT because ciphertext cannot answer it: every save
// re-encrypts under a fresh nonce, so a toggle-only save that re-stores the very
// same endpoint yields bytes that differ from the stored ones. Keeping the mark
// across exactly that save is the whole reason this comparison exists instead of
// a blanket clear on every write of the column.
//
// It reads the row itself rather than taking the verdict from its callers: this
// is the one choke point every webhook save passes through, and a rule placed in
// the callers would hold at the two that exist today and be missed by the third.
// A read error clears rather than fails the save — an owner's save is not
// refused over the fate of a display mark.
func (service *WebhookSettingsService) destinationNotProvablyUnchanged(ctx context.Context, userID uint, destination string) bool {
	current, err := service.users.LoadSettingsByID(ctx, userID)
	if err != nil {
		return true
	}
	stored, err := service.DecryptWebhookURL(userID, current.WebhookURL)
	if err != nil {
		return true
	}
	return stored != destination
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
			// The stored ciphertext will not open, and a blank field means "keep
			// the stored endpoint" -- so there is no plaintext to re-encrypt. This
			// used to substitute the empty string, which DELETED an endpoint the
			// request had asked to keep, and it did so on the branch that looks
			// least destructive: an owner reading "this instance can no longer
			// read it" and switching delivery off.
			//
			// Keeping the column is the answer that costs nothing else. The
			// toggles on the same form still save, the endpoint waits for a
			// deliberate withdrawal, and arming it is refused below because an
			// endpoint the instance cannot read cannot deliver.
			update.KeepStoredURL = true
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

// WebhookURLReadability names what this instance can honestly say about a stored
// endpoint ciphertext, and it is a THREE-valued answer because the row admits
// three genuinely different situations. Collapsing the last two into one boolean
// is what let an endpoint the instance can no longer open render beside the word
// "configured": the owner reads a capability the instance does not have.
type WebhookURLReadability string

const (
	// WebhookURLAbsent -- no ciphertext is stored.
	WebhookURLAbsent WebhookURLReadability = "absent"
	// WebhookURLUnreadable -- a ciphertext is stored and this instance cannot
	// open it. The usual cause is a rotated or replaced SECRET_KEY. Delivery
	// cannot happen and no host can be named.
	WebhookURLUnreadable WebhookURLReadability = "unreadable"
	// WebhookURLReadable -- the ciphertext opened. Host carries the result, and
	// an EMPTY Host here is its own fact (a stored value that names no host), not
	// the same fact as an unopenable ciphertext.
	WebhookURLReadable WebhookURLReadability = "readable"
)

// WebhookURLDisplay is the ONLY webhook-endpoint projection any surface may
// render. The stored URL is a secret (it can embed an ntfy/Gotify token), so it
// is never echoed back into an HTML value/attribute, a JSON body, or operator
// output: Readability says what the instance knows about the stored value, and
// Host carries at most the hostname (u.Hostname() -- never scheme, path, query,
// or userinfo).
type WebhookURLDisplay struct {
	Readability WebhookURLReadability
	Host        string
}

// BuildWebhookURLDisplay derives the render-safe projection for a stored
// webhook_url ciphertext, scoped to userID (the AAD binds the ciphertext to the
// owner). It decrypts only to extract the hostname and deliberately discards the
// rest of the URL, so no caller can obtain the full secret through this seam.
//
// A ciphertext that fails to open reports WebhookURLUnreadable rather than an
// error: the settings page must still render, and the owner needs to be told
// which of the two situations they are in -- re-save to restore a decryptable
// endpoint, or withdraw it. The failure is never surfaced as an error VALUE
// either, because the decrypt error's text is not something a page or an
// operator log may carry.
func (service *WebhookSettingsService) BuildWebhookURLDisplay(userID uint, encryptedURL string) WebhookURLDisplay {
	if strings.TrimSpace(encryptedURL) == "" {
		return WebhookURLDisplay{Readability: WebhookURLAbsent}
	}
	plaintext, err := service.DecryptWebhookURL(userID, encryptedURL)
	if err != nil {
		return WebhookURLDisplay{Readability: WebhookURLUnreadable}
	}
	// hostOnly is the package's single URL-hostname redaction rule -- the same one
	// the notify pass and the CLI print through. Keeping one implementation is
	// what makes a hardening of "what is safe to show" reach every surface at
	// once; this display used to carry its own byte-identical copy.
	return WebhookURLDisplay{Readability: WebhookURLReadable, Host: hostOnly(plaintext)}
}

// RemoveWebhookDestination withdraws the owner's endpoint and leaves the
// per-kind opt-ins and the shared lead window exactly where the owner set them.
// It never decrypts the ciphertext it clears, which is what keeps an unreadable
// endpoint revocable, and it is the only write path that expresses "remove the
// destination" on its own -- SaveWebhookSettings always writes the kinds and the
// lead window too, so a thin caller reaching for it would silently narrow the
// window to zero.
func (service *WebhookSettingsService) RemoveWebhookDestination(ctx context.Context, userID uint) error {
	return service.users.RemoveWebhookDestination(ctx, userID)
}
