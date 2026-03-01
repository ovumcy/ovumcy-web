package api

import (
	"html/template"
	"time"

	"github.com/terraincognita07/ovumcy/internal/db"
	"github.com/terraincognita07/ovumcy/internal/i18n"
	"github.com/terraincognita07/ovumcy/internal/services"
	"gorm.io/gorm"
)

type Handler struct {
	// db is kept for backward compatibility in tests that still construct
	// Handler literals directly. Runtime logic uses repositories/services.
	db                  *gorm.DB
	secretKey           []byte
	location            *time.Location
	cookieSecure        bool
	i18n                *i18n.Manager
	templates           map[string]*template.Template
	partials            map[string]*template.Template
	recoveryLimiter     *services.AttemptLimiter
	repositories        *db.Repositories
	authService         *services.AuthService
	passwordResetSvc    *services.PasswordResetService
	dayService          *services.DayService
	symptomService      *services.SymptomService
	statsService        *services.StatsService
	exportService       *services.ExportService
	settingsService     *services.SettingsService
	notificationService *services.NotificationService
	onboardingSvc       *services.OnboardingService
	setupService        *services.SetupService
}

type CalendarDay struct {
	Date         time.Time
	DateString   string
	Day          int
	InMonth      bool
	IsToday      bool
	IsPeriod     bool
	IsPredicted  bool
	IsFertility  bool
	IsOvulation  bool
	HasData      bool
	CellClass    string
	TextClass    string
	BadgeClass   string
	OvulationDot bool
}

type SymptomCount struct {
	Name             string
	Icon             string
	Count            int
	TotalDays        int
	FrequencySummary string
}

type FlashPayload struct {
	AuthError       string `json:"auth_error,omitempty"`
	SettingsError   string `json:"settings_error,omitempty"`
	SettingsSuccess string `json:"settings_success,omitempty"`
	LoginEmail      string `json:"login_email,omitempty"`
	RegisterEmail   string `json:"register_email,omitempty"`
}

const (
	defaultAuthTokenTTL  = 7 * 24 * time.Hour
	rememberAuthTokenTTL = 30 * 24 * time.Hour
)
