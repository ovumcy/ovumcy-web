package services

import (
	"errors"
	"fmt"
	"time"

	"github.com/terraincognita07/ovumcy/internal/models"
)

var (
	ErrSettingsViewLoadSettings = errors.New("settings view load settings")
	ErrSettingsViewLoadExport   = errors.New("settings view load export")
)

type SettingsViewLoader interface {
	LoadSettings(userID uint) (models.User, error)
}

type SettingsViewExportBuilder interface {
	BuildSummary(userID uint, from *time.Time, to *time.Time, location *time.Location) (ExportSummary, error)
}

type SettingsViewInput struct {
	FlashSuccess string
	FlashError   string
	QuerySuccess string
	QueryStatus  string
	QueryError   string
}

type SettingsExportViewData struct {
	TotalEntries       int
	HasData            bool
	DateFrom           string
	DateTo             string
	DateFromDisplay    string
	DateToDisplay      string
	HasSummaryForOwner bool
}

type SettingsPageViewData struct {
	CurrentUser             models.User
	ErrorKey                string
	ChangePasswordErrorKey  string
	SuccessKey              string
	CycleLength             int
	PeriodLength            int
	AutoPeriodFill          bool
	LastPeriodStart         string
	TodayISO                string
	CycleStartMinISO        string
	Export                  SettingsExportViewData
	HasOwnerExportViewState bool
}

type SettingsViewService struct {
	settings      SettingsViewLoader
	notifications *NotificationService
	export        SettingsViewExportBuilder
}

func NewSettingsViewService(settings SettingsViewLoader, notifications *NotificationService, export SettingsViewExportBuilder) *SettingsViewService {
	if notifications == nil {
		notifications = NewNotificationService()
	}
	return &SettingsViewService{
		settings:      settings,
		notifications: notifications,
		export:        export,
	}
}

func (service *SettingsViewService) BuildSettingsPageViewData(user *models.User, language string, input SettingsViewInput, now time.Time, location *time.Location) (SettingsPageViewData, error) {
	status := service.notifications.ResolveSettingsStatus(
		input.FlashSuccess,
		input.QuerySuccess,
		input.QueryStatus,
	)

	errorKey := ""
	changePasswordErrorKey := ""
	if status == "" {
		errorSource := service.notifications.ResolveSettingsErrorSource(input.FlashError, input.QueryError)
		translatedErrorKey := AuthErrorTranslationKey(errorSource)
		if translatedErrorKey != "" {
			if service.notifications.ClassifySettingsErrorSource(errorSource) == SettingsErrorTargetChangePassword {
				changePasswordErrorKey = translatedErrorKey
			} else {
				errorKey = translatedErrorKey
			}
		}
	}

	persisted, err := service.settings.LoadSettings(user.ID)
	if err != nil {
		return SettingsPageViewData{}, fmt.Errorf("%w: %v", ErrSettingsViewLoadSettings, err)
	}

	cycleLength, periodLength := ResolveCycleAndPeriodDefaults(persisted.CycleLength, persisted.PeriodLength)
	autoPeriodFill := persisted.AutoPeriodFill

	resolvedUser := *user
	resolvedUser.CycleLength = cycleLength
	resolvedUser.PeriodLength = periodLength
	resolvedUser.AutoPeriodFill = autoPeriodFill
	resolvedUser.LastPeriodStart = persisted.LastPeriodStart

	lastPeriodStart := ""
	if persisted.LastPeriodStart != nil {
		lastPeriodStart = DateAtLocation(*persisted.LastPeriodStart, location).Format("2006-01-02")
	}
	minCycleStart, today := SettingsCycleStartDateBounds(now, location)

	viewData := SettingsPageViewData{
		CurrentUser:            resolvedUser,
		ErrorKey:               errorKey,
		ChangePasswordErrorKey: changePasswordErrorKey,
		SuccessKey:             SettingsStatusTranslationKey(status),
		CycleLength:            cycleLength,
		PeriodLength:           periodLength,
		AutoPeriodFill:         autoPeriodFill,
		LastPeriodStart:        lastPeriodStart,
		TodayISO:               today.Format("2006-01-02"),
		CycleStartMinISO:       minCycleStart.Format("2006-01-02"),
	}

	if resolvedUser.Role != models.RoleOwner {
		return viewData, nil
	}

	if service.export == nil {
		return viewData, nil
	}

	summary, err := service.export.BuildSummary(resolvedUser.ID, nil, nil, location)
	if err != nil {
		return SettingsPageViewData{}, fmt.Errorf("%w: %v", ErrSettingsViewLoadExport, err)
	}

	displayFrom := summary.DateFrom
	if parsedFrom, parseErr := ParseDayDate(summary.DateFrom, location); parseErr == nil {
		displayFrom = LocalizedDateDisplay(language, parsedFrom)
	}
	displayTo := summary.DateTo
	if parsedTo, parseErr := ParseDayDate(summary.DateTo, location); parseErr == nil {
		displayTo = LocalizedDateDisplay(language, parsedTo)
	}

	viewData.Export = SettingsExportViewData{
		TotalEntries:       summary.TotalEntries,
		HasData:            summary.HasData,
		DateFrom:           summary.DateFrom,
		DateTo:             summary.DateTo,
		DateFromDisplay:    displayFrom,
		DateToDisplay:      displayTo,
		HasSummaryForOwner: true,
	}
	viewData.HasOwnerExportViewState = true

	return viewData, nil
}
