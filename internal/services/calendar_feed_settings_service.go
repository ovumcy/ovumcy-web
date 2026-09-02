package services

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/ovumcy/ovumcy-web/internal/models"
	"github.com/ovumcy/ovumcy-web/internal/security"
)

// Calendar (.ics) feed subscription — settings lifecycle (slice 4).
//
// CalendarFeedSettingsService owns the owner-driven lifecycle of the feed
// bearer token: GENERATE (enable), ROTATE (mint a fresh token; the previous URL
// dies immediately), and REVOKE (disable; the columns are NULLed and the feed
// 404s). It is the business-logic seam the settings api layer calls, so the api
// layer stays transport-only: it never mints a token, touches a repository, or
// splits the token itself.
//
// It is deliberately SEPARATE from CalendarFeedService (which resolves a token
// to an owner and renders the read-only .ics): that service authenticates by
// the path token alone and needs no writer, while this one holds the write path
// and is only ever reached behind an authenticated OwnerOnly + CSRF settings
// request scoped to the session user id. This mirrors the webhook split
// (WebhookSettingsService writes; WebhookNotifyService delivers).
//
// SECRET HANDLING. GenerateCalendarFeedToken returns the shown-once full token
// plus the storables (plaintext selector + keyed verifier MAC + bcrypt verifier
// hash); this service persists only the storables and hands the full token
// straight back to the caller for a ONE-TIME reveal (the same shown-once model as
// recovery codes). The full token is never re-derivable afterward — it is not
// persisted, and the settings view only ever renders configured/not-configured
// status, never the token or the URL. Nothing here logs the token.

// ErrCalendarFeedTokenGenerate wraps a token-generation failure (crypto/rand or
// bcrypt), kept distinct from the persistence error so the handler can map each.
var ErrCalendarFeedTokenGenerate = errors.New("calendar feed token generate")

// ErrCalendarFeedTokenPersist wraps a repository write/clear failure.
var ErrCalendarFeedTokenPersist = errors.New("calendar feed token persist")

// CalendarFeedSettingsRepository is the narrow persistence seam the feed
// settings lifecycle needs. SaveCalendarFeedToken writes (creates or rotates)
// the two feed-token columns; ClearCalendarFeedToken NULLs them (revoke).
// Neither bumps auth_session_version — a feed capability is per-surface, not an
// account credential.
type CalendarFeedSettingsRepository interface {
	SaveCalendarFeedToken(ctx context.Context, userID uint, columns models.CalendarFeedTokenColumns) error
	ClearCalendarFeedToken(ctx context.Context, userID uint) error
	// LoadSettingsByID is the single controlled settings projection, and the
	// status read below goes through it rather than through a whole-row read so
	// the two claim watermarks never enter memory on a render path at all.
	// Growing that projection is pinned by
	// TestLoadSettingsProjectionSelectsExactlyTheseColumns.
	LoadSettingsByID(ctx context.Context, userID uint) (models.User, error)
	// ClaimCalendarFeedReveal atomically consumes the owner's one-time reveal of
	// the subscribe URL, returning true only for the call that consumed it.
	// SaveCalendarFeedToken re-arms it in the same statement that mints a token
	// (migration 036).
	ClaimCalendarFeedReveal(ctx context.Context, userID uint, revealedAt time.Time) (bool, error)
}

// CalendarFeedSettingsService is the write-side seam for the feed lifecycle.
type CalendarFeedSettingsService struct {
	users     CalendarFeedSettingsRepository
	secretKey []byte
}

// NewCalendarFeedSettingsService wires the lifecycle service from the user
// repository and the application secret. secretKey derives the keyed verifier MAC
// stored with every minted token (same injection shape as
// NewWebhookSettingsService / NewTOTPService). The repository is required in
// production; tests may pass a stub.
func NewCalendarFeedSettingsService(users CalendarFeedSettingsRepository, secretKey []byte) *CalendarFeedSettingsService {
	return &CalendarFeedSettingsService{users: users, secretKey: secretKey}
}

// GenerateFeedToken mints a fresh feed token for the owner, persists the
// selector + verifier hash (scoped strictly to userID), and returns the
// shown-once full token for a one-time reveal. It is used for BOTH the initial
// enable and rotation: a rotate is just a second GenerateFeedToken, and because
// SaveCalendarFeedToken overwrites the previous (selector, verifierHash) pair,
// the previous token stops verifying the instant this write commits (its
// verifier no longer matches the new hash and its selector no longer resolves).
//
// The returned token is a secret: the caller must reveal it exactly once
// (sealed one-time cookie) and never render it into an HTML value on a later
// settings load. This method never logs the token.
func (service *CalendarFeedSettingsService) GenerateFeedToken(ctx context.Context, userID uint) (string, error) {
	fullToken, columns, err := GenerateCalendarFeedToken(service.secretKey)
	if err != nil {
		return "", fmt.Errorf("%w: %v", ErrCalendarFeedTokenGenerate, err)
	}
	if err := service.users.SaveCalendarFeedToken(ctx, userID, columns); err != nil {
		return "", fmt.Errorf("%w: %v", ErrCalendarFeedTokenPersist, err)
	}
	return fullToken, nil
}

// RevokeFeedToken disables the owner's feed by NULLing both token columns,
// scoped strictly to userID. After it commits, any previously-issued feed URL
// 404s (its selector resolves no row). Idempotent: revoking an already-off feed
// is a no-op write that still succeeds.
func (service *CalendarFeedSettingsService) RevokeFeedToken(ctx context.Context, userID uint) error {
	if err := service.users.ClearCalendarFeedToken(ctx, userID); err != nil {
		return fmt.Errorf("%w: %v", ErrCalendarFeedTokenPersist, err)
	}
	return nil
}

// ClaimFeedReveal consumes the owner's one-time reveal of the subscribe URL and
// reports whether THIS call is the one that got it. False means the reveal was
// already spent — a replayed sealed cookie, a second tab, a re-issued request —
// and the caller must render no URL and audit no egress.
//
// Retracting the sealed cookie in the reveal response asks the browser to forget
// the value and cannot bind a client that kept it, which is why the mark is what
// makes "shown exactly once" true.
//
// A zero userID is refused here, before the repository is reached: an absent
// owner id is invalid input, not a claim that applies to whichever row comes
// first. The compare-and-set below would match no row either — the guard makes
// the refusal explicit rather than resting on that.
func (service *CalendarFeedSettingsService) ClaimFeedReveal(ctx context.Context, userID uint, revealedAt time.Time) (bool, error) {
	if userID == 0 {
		return false, fmt.Errorf("%w: calendar feed reveal requires an owner id", ErrCalendarFeedTokenPersist)
	}
	claimed, err := service.users.ClaimCalendarFeedReveal(ctx, userID, revealedAt)
	if err != nil {
		return false, fmt.Errorf("%w: %v", ErrCalendarFeedTokenPersist, err)
	}
	return claimed, nil
}

// CalendarFeedStatus is the render-safe projection the settings view uses. It
// carries no token, no selector and no URL, so a normal settings load can
// describe the feed without any secret reaching the page.
//
// Known is the field a load failure needs. Without it the zero value said
// "no feed configured", which is a claim about the ROW made by a read that never
// saw the row -- and the one claim an owner would act on by generating a second
// token beside a first one that still works.
//
// RevealedAt marks the one-time reveal as CONSUMED. It is emphatically not a
// fetch record: polls of the .ics feed are deliberately unaudited, and no field
// here can say whether anyone ever subscribed.
//
// KeyEpoch is the row's stamp and CurrentKeyEpoch the value this instance
// derives from its running SECRET_KEY. Their comparison is the only honest thing
// that can be said about whether the issued link still resolves: the verifier
// plaintext is not stored and its MAC cannot be recomputed, so the presence of a
// selector proves the row exists and nothing more. CurrentKeyEpoch is empty when
// the epoch could not be derived, and a comparison against an empty value is
// never treated as a match.
type CalendarFeedStatus struct {
	Known           bool
	Configured      bool
	RevealedAt      *time.Time
	KeyEpoch        string
	CurrentKeyEpoch string
}

// BuildFeedStatus derives the render-safe feed projection for an owner, scoped
// to userID. The verifier plaintext is never stored and never read here, so this
// seam cannot leak the token. A load error yields the zero value, whose Known is
// false: the caller must render "unknown", never "not configured".
func (service *CalendarFeedSettingsService) BuildFeedStatus(ctx context.Context, userID uint) CalendarFeedStatus {
	user, err := service.users.LoadSettingsByID(ctx, userID)
	if err != nil {
		return CalendarFeedStatus{}
	}
	// A derivation failure (no SECRET_KEY) leaves CurrentKeyEpoch empty, which the
	// state machine reads as "cannot judge" rather than as a mismatch: telling an
	// owner their link was issued under a superseded key because this process
	// could not derive its own epoch would be a guess wearing a fact's clothes.
	currentEpoch, epochErr := security.CalendarFeedKeyEpoch(service.secretKey)
	if epochErr != nil {
		currentEpoch = ""
	}
	return CalendarFeedStatus{
		Known:           true,
		Configured:      strings.TrimSpace(user.CalendarFeedSelector) != "",
		RevealedAt:      user.CalendarFeedRevealedAt,
		KeyEpoch:        strings.TrimSpace(user.CalendarFeedKeyEpoch),
		CurrentKeyEpoch: currentEpoch,
	}
}
