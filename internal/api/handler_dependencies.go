package api

import (
	"github.com/terraincognita07/ovumcy/internal/db"
	"github.com/terraincognita07/ovumcy/internal/services"
	"gorm.io/gorm"
)

func (handler *Handler) withDependencies(database *gorm.DB) *Handler {
	handler.repositories = db.NewRepositories(database)
	handler.authService = services.NewAuthService(handler.repositories.Users)
	attemptLimiter := services.NewAttemptLimiter()
	handler.passwordResetSvc = services.NewPasswordResetService(handler.authService, attemptLimiter)
	handler.dayService = services.NewDayService(handler.repositories.DailyLogs, handler.repositories.Users)
	handler.symptomService = services.NewSymptomService(handler.repositories.Symptoms, handler.repositories.DailyLogs)
	handler.registrationService = services.NewRegistrationService(handler.authService, handler.symptomService)
	handler.viewerService = services.NewViewerService(handler.dayService, handler.symptomService)
	handler.statsService = services.NewStatsService(handler.dayService, handler.symptomService)
	handler.calendarViewService = services.NewCalendarViewService(handler.dayService, handler.statsService)
	handler.dashboardViewService = services.NewDashboardViewService(handler.statsService, handler.viewerService, handler.dayService)
	handler.exportService = services.NewExportService(handler.dayService, handler.symptomService)
	handler.settingsService = services.NewSettingsService(handler.repositories.Users)
	notificationService := services.NewNotificationService()
	handler.settingsViewService = services.NewSettingsViewService(handler.settingsService, notificationService, handler.exportService)
	handler.onboardingSvc = services.NewOnboardingService(handler.repositories.Users)
	handler.setupService = services.NewSetupService(handler.repositories.Users)
	return handler
}
