package api

import (
	"html/template"
	"sync"
	"time"

	"github.com/ovumcy/ovumcy-web/internal/apideps"
	"github.com/ovumcy/ovumcy-web/internal/i18n"
	"github.com/ovumcy/ovumcy-web/internal/services"
)

// The handler's dependency-port interfaces live in internal/apideps (see the
// package doc); these aliases keep the api.* names in handler/test code.
type (
	RegistrationWorkflowService = apideps.RegistrationWorkflowService
	LoginWorkflowService        = apideps.LoginWorkflowService
	RegisterPickupTokenStore    = apideps.RegisterPickupTokenStore
	OIDCWorkflowService         = apideps.OIDCWorkflowService
)

type Handler struct {
	secretKey            []byte
	cookieCodecOnce      sync.Once
	cookieCodecCached    *secureCookieCodec
	cookieCodecErr       error
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
