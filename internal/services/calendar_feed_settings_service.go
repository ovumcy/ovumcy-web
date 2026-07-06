package services

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"github.com/ovumcy/ovumcy-web/internal/models"
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
// plus the two storables (plaintext selector + bcrypt verifier hash); this
// service persists only the storables and hands the full token straight back to
// the caller for a ONE-TIME reveal (the same shown-once model as recovery
// codes). The full token is never re-derivable afterward — it is not persisted,
// and the settings view only ever renders configured/not-configured status,
// never the token or the URL. Nothing here logs the token.

// ErrCalendarFeedTokenGenerate wraps a token-generation failure (crypto/rand or
// bcrypt), kept distinct from the persistence error so the handler can map each.
var ErrCalendarFeedTokenGenerate = errors.New("calendar feed token generate")

// ErrCalendarFeedTokenPersist wraps a repository write/clear failure.
var ErrCalendarFeedTokenPersist = errors.New("calendar feed token persist")

// CalendarFeedSettingsRepository is the narrow persistence seam the feed
// settings lifecycle needs. SaveCalendarFeedToken writes (creates or rotates)
// the two feed-token columns; ClearCalendarFeedToken NULLs them (revoke).
// Neither bumps auth_session_version — a feed capability is per-surface, not an
// account credential. FindByID lets the status projection read the current
// selector without exposing any verifier plaintext (none is stored).
type CalendarFeedSettingsRepository interface {
	SaveCalendarFeedToken(ctx context.Context, userID uint, columns models.CalendarFeedTokenColumns) error
	ClearCalendarFeedToken(ctx context.Context, userID uint) error
	FindByID(ctx context.Context, userID uint) (models.User, error)
}

// CalendarFeedSettingsService is the write-side seam for the feed lifecycle.
type CalendarFeedSettingsService struct {
	users CalendarFeedSettingsRepository
}

// NewCalendarFeedSettingsService wires the lifecycle service from the user
// repository. The repository is required in production; tests may pass a stub.
func NewCalendarFeedSettingsService(users CalendarFeedSettingsRepository) *CalendarFeedSettingsService {
	return &CalendarFeedSettingsService{users: users}
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
	fullToken, selector, verifierHash, err := GenerateCalendarFeedToken()
	if err != nil {
		return "", fmt.Errorf("%w: %v", ErrCalendarFeedTokenGenerate, err) // codecov:ignore -- crypto/rand or bcrypt failure, not reachable in tests
	}
	if err := service.users.SaveCalendarFeedToken(ctx, userID, models.CalendarFeedTokenColumns{
		Selector:     selector,
		VerifierHash: verifierHash,
	}); err != nil {
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

// CalendarFeedStatus is the render-safe projection the settings view uses. It
// reports ONLY whether a feed is currently configured — never the token, the
// selector, or a URL — so a normal settings load can show "configured" without
// any secret ever reaching the page.
type CalendarFeedStatus struct {
	Configured bool
}

// BuildFeedStatus derives the configured/not-configured projection for an
// owner's stored feed selector, scoped to userID. A non-empty selector means a
// feed is armed. The verifier plaintext is never stored and never read here, so
// this seam cannot leak the token. On a load error it reports not-configured so
// the settings page still renders.
func (service *CalendarFeedSettingsService) BuildFeedStatus(ctx context.Context, userID uint) CalendarFeedStatus {
	user, err := service.users.FindByID(ctx, userID)
	if err != nil {
		return CalendarFeedStatus{}
	}
	return CalendarFeedStatus{Configured: strings.TrimSpace(user.CalendarFeedSelector) != ""}
}
