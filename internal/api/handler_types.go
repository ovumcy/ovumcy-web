package api

import (
	"context"
	"html/template"
	"time"

	"github.com/ovumcy/ovumcy-web/internal/i18n"
	"github.com/ovumcy/ovumcy-web/internal/models"
	"github.com/ovumcy/ovumcy-web/internal/security"
	"github.com/ovumcy/ovumcy-web/internal/services"
)

type RegistrationWorkflowService interface {
	RegisterOwnerAccount(ctx context.Context, email string, rawPassword string, confirmPassword string, createdAt time.Time) (models.User, string, error)
	RegistrationOpen() bool
}

type LoginWorkflowService interface {
	Authenticate(ctx context.Context, secretKey []byte, clientKey string, email string, password string, resetTokenTTL time.Duration, now time.Time) (services.LoginResult, error)
}

// RegisterPickupTokenStore persists and atomically consumes the nonces that
// back the sealed `ovumcy_register_pickup` cookie. The interface lets tests
// substitute an in-memory implementation without spinning up a database.
type RegisterPickupTokenStore interface {
	Issue(ctx context.Context, nonce string, userID uint, expiresAt time.Time) error
	Consume(ctx context.Context, nonce string, now time.Time) (uint, bool, error)
}

type OIDCWorkflowService interface {
	Enabled() bool
	LocalPublicAuthEnabled() bool
	StartAuth(ctx context.Context, state string, nonce string, codeVerifier string) (string, error)
	StartReauth(ctx context.Context, state string, nonce string, codeVerifier string) (string, error)
	Authenticate(ctx context.Context, code string, codeVerifier string, expectedNonce string, now time.Time) (services.OIDCLoginResult, error)
	ValidateReauthExchange(ctx context.Context, code string, codeVerifier string, expectedNonce string, expectedUserID uint, maxAuthAge time.Duration, now time.Time) error
	ConfirmAndLinkIdentity(ctx context.Context, targetUserID uint, claims security.OIDCClaims, linkTime time.Time) error
}

type Handler struct {
	secretKey            []byte
	location             *time.Location
	cookieSecure         bool
	i18n                 *i18n.Manager
	templates            map[string]*template.Template
	partials             map[string]*template.Template
	authService          *services.AuthService
	registrationService  RegistrationWorkflowService
	passwordResetSvc     *services.PasswordResetService
	loginService         LoginWorkflowService
	oidcService          OIDCWorkflowService
	oidcLogoutStateSvc   *services.OIDCLogoutStateService
	dayService           *services.DayService
	symptomService       *services.SymptomService
	viewerService        *services.ViewerService
	statsService         *services.StatsService
	calendarViewService  *services.CalendarViewService
	dashboardViewService *services.DashboardViewService
	exportService        *services.ExportService
	settingsService      *services.SettingsService
	settingsViewService  *services.SettingsViewService
	onboardingSvc        *services.OnboardingService
	setupService         *services.SetupService
	totpService          *services.TOTPService
	registerPickupTokens RegisterPickupTokenStore
}

type CalendarDay struct {
	Date                   time.Time
	DateString             string
	Day                    int
	InMonth                bool
	IsToday                bool
	OpenEditDirectly       bool
	IsPeriod               bool
	IsPredicted            bool
	IsPreFertile           bool
	IsFertility            bool
	IsFertilityPeak        bool
	IsFertilityEdge        bool
	IsOvulation            bool
	IsTentativeOvulation   bool
	HasData                bool
	HasSex                 bool
	CellClass              string
	TextClass              string
	BadgeClass             string
	StateKey               string
	OvulationDot           bool
	TentativeOvulationMark bool
}

type FlashPayload struct {
	AuthError       string `json:"auth_error,omitempty"`
	SettingsError   string `json:"settings_error,omitempty"`
	SettingsSuccess string `json:"settings_success,omitempty"`
	// ForgotEmail carries the entered address across the two-step password
	// recovery flow (email -> recovery code). It is the only email kept in the
	// flash cookie: the cookie is AEAD-encrypted and the redirect-safe
	// alternatives (URL query param) would expose the address in logs/history.
	// Login/register error prefill deliberately does NOT round-trip the email
	// to keep PII out of the cookie on the common failure paths.
	ForgotEmail string `json:"forgot_password_email,omitempty"`
}

const (
	defaultAuthTokenTTL  = 7 * 24 * time.Hour
	rememberAuthTokenTTL = 30 * 24 * time.Hour
)
