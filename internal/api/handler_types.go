package api

import (
	"html/template"
	"time"

	"github.com/terraincognita07/ovumcy/internal/i18n"
	"github.com/terraincognita07/ovumcy/internal/models"
	"github.com/terraincognita07/ovumcy/internal/services"
)

type RegistrationWorkflowService interface {
	RegisterOwnerAccount(email string, rawPassword string, confirmPassword string, createdAt time.Time) (models.User, string, error)
}

type LoginWorkflowService interface {
	Authenticate(secretKey []byte, clientKey string, email string, password string, resetTokenTTL time.Duration, now time.Time) (services.LoginResult, error)
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
}

type CalendarDay struct {
	Date         time.Time
	DateString   string
	Day          int
	InMonth      bool
	IsToday      bool
	IsPeriod     bool
	IsPredicted  bool
	IsPreFertile bool
	IsFertility  bool
	IsOvulation  bool
	HasData      bool
	HasSex       bool
	CellClass    string
	TextClass    string
	BadgeClass   string
	StateKey     string
	OvulationDot bool
}

type FlashPayload struct {
	AuthError       string `json:"auth_error,omitempty"`
	SettingsError   string `json:"settings_error,omitempty"`
	SettingsSuccess string `json:"settings_success,omitempty"`
	LoginEmail      string `json:"login_email,omitempty"`
	RegisterEmail   string `json:"register_email,omitempty"`
	ForgotEmail     string `json:"forgot_password_email,omitempty"`
}

const (
	defaultAuthTokenTTL  = 7 * 24 * time.Hour
	rememberAuthTokenTTL = 30 * 24 * time.Hour
)
