package services

import (
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/terraincognita07/ovumcy/internal/models"
)

var (
	ErrSettingsViewLoadSettings = errors.New("settings view load settings")
	ErrSettingsViewLoadExport   = errors.New("settings view load export")
	ErrSettingsViewLoadSymptoms = errors.New("settings view load symptoms")
)

type SettingsViewLoader interface {
	LoadSettings(userID uint) (models.User, error)
}

type SettingsViewExportBuilder interface {
	BuildSummary(userID uint, from *time.Time, to *time.Time, location *time.Location) (ExportSummary, error)
}

type SettingsViewSymptomProvider interface {
	FetchSymptoms(userID uint) ([]models.SymptomType, error)
}

type SettingsViewInput struct {
	FlashSuccess string
	FlashError   string
}

type SettingsExportViewData struct {
	SummaryTotalEntries    int
	HasData                bool
	SummaryHasData         bool
	SummaryDateFrom        string
	SummaryDateTo          string
	SummaryDateFromDisplay string
	SummaryDateToDisplay   string
	DefaultDateFrom        string
	DefaultDateTo          string
	SelectableDateMin      string
	SelectableDateMax      string
	HasSummaryForOwner     bool
}

type SettingsSymptomsViewData struct {
	ActiveCustomSymptoms   []models.SymptomType
	ArchivedCustomSymptoms []models.SymptomType
	HasCustomSymptoms      bool
	HasArchivedSymptoms    bool
}

type SettingsPageViewData struct {
	CurrentUser             models.User
	ErrorKey                string
	ChangePasswordErrorKey  string
	SuccessKey              string
	CycleLength             int
	PeriodLength            int
	AutoPeriodFill          bool
	IrregularCycle          bool
	TrackBBT                bool
	TrackCervicalMucus      bool
	HideSexChip             bool
	LastPeriodStart         string
	TodayISO                string
	CycleStartMinISO        string
	Export                  SettingsExportViewData
	Symptoms                SettingsSymptomsViewData
	HasOwnerExportViewState bool
	HasOwnerSymptomsView    bool
}

type SettingsViewService struct {
	settings      SettingsViewLoader
	notifications *NotificationService
	export        SettingsViewExportBuilder
	symptoms      SettingsViewSymptomProvider
}

func NewSettingsViewService(settings SettingsViewLoader, notifications *NotificationService, export SettingsViewExportBuilder, symptoms SettingsViewSymptomProvider) *SettingsViewService {
	if notifications == nil {
		notifications = NewNotificationService()
	}
	return &SettingsViewService{
		settings:      settings,
		notifications: notifications,
		export:        export,
		symptoms:      symptoms,
	}
}

func (service *SettingsViewService) BuildSettingsPageViewData(user *models.User, language string, input SettingsViewInput, now time.Time, location *time.Location) (SettingsPageViewData, error) {
	status := service.notifications.ResolveSettingsStatus(input.FlashSuccess)

	errorKey := ""
	changePasswordErrorKey := ""
	if status == "" {
		errorSource := service.notifications.ResolveSettingsErrorSource(input.FlashError)
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
	resolvedUser.IrregularCycle = persisted.IrregularCycle
	resolvedUser.TrackBBT = persisted.TrackBBT
	resolvedUser.TrackCervicalMucus = persisted.TrackCervicalMucus
	resolvedUser.HideSexChip = persisted.HideSexChip
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
		IrregularCycle:         resolvedUser.IrregularCycle,
		TrackBBT:               resolvedUser.TrackBBT,
		TrackCervicalMucus:     resolvedUser.TrackCervicalMucus,
		HideSexChip:            resolvedUser.HideSexChip,
		LastPeriodStart:        lastPeriodStart,
		TodayISO:               today.Format("2006-01-02"),
		CycleStartMinISO:       minCycleStart.Format("2006-01-02"),
	}

	if resolvedUser.Role != models.RoleOwner {
		return viewData, nil
	}

	if service.symptoms != nil {
		symptomsViewData, err := service.BuildSettingsSymptomsViewData(&resolvedUser)
		if err != nil {
			return SettingsPageViewData{}, err
		}
		viewData.Symptoms = symptomsViewData
		viewData.HasOwnerSymptomsView = true
	}

	if service.export == nil {
		return viewData, nil
	}

	availableSummary, err := service.export.BuildSummary(resolvedUser.ID, nil, nil, location)
	if err != nil {
		return SettingsPageViewData{}, fmt.Errorf("%w: %v", ErrSettingsViewLoadExport, err)
	}

	if !availableSummary.HasData {
		viewData.Export = SettingsExportViewData{HasSummaryForOwner: true}
		viewData.HasOwnerExportViewState = true
		return viewData, nil
	}

	todayISO := today.Format(exportDateLayout)
	selectableMin := todayISO
	if compareISODate(availableSummary.DateFrom, selectableMin) < 0 {
		selectableMin = availableSummary.DateFrom
	}
	selectableMax := todayISO
	if compareISODate(availableSummary.DateTo, selectableMax) > 0 {
		selectableMax = availableSummary.DateTo
	}

	defaultFrom := selectableMin
	defaultTo := todayISO
	defaultSummary, err := service.export.BuildSummary(
		resolvedUser.ID,
		exportSummaryBound(defaultFrom, location),
		exportSummaryBound(defaultTo, location),
		location,
	)
	if err != nil {
		return SettingsPageViewData{}, fmt.Errorf("%w: %v", ErrSettingsViewLoadExport, err)
	}

	viewData.Export = SettingsExportViewData{
		SummaryTotalEntries:    defaultSummary.TotalEntries,
		HasData:                availableSummary.HasData,
		SummaryHasData:         defaultSummary.HasData,
		SummaryDateFrom:        defaultSummary.DateFrom,
		SummaryDateTo:          defaultSummary.DateTo,
		SummaryDateFromDisplay: localizedExportSummaryDate(language, defaultSummary.DateFrom, location),
		SummaryDateToDisplay:   localizedExportSummaryDate(language, defaultSummary.DateTo, location),
		DefaultDateFrom:        defaultFrom,
		DefaultDateTo:          defaultTo,
		SelectableDateMin:      selectableMin,
		SelectableDateMax:      selectableMax,
		HasSummaryForOwner:     true,
	}
	viewData.HasOwnerExportViewState = true

	return viewData, nil
}

func localizedExportSummaryDate(language string, raw string, location *time.Location) string {
	trimmed := strings.TrimSpace(raw)
	if trimmed == "" {
		return ""
	}

	parsed, err := ParseDayDate(trimmed, location)
	if err != nil {
		return trimmed
	}
	return LocalizedDateDisplay(language, parsed)
}

func exportSummaryBound(raw string, location *time.Location) *time.Time {
	trimmed := strings.TrimSpace(raw)
	if trimmed == "" {
		return nil
	}

	parsed, err := ParseDayDate(trimmed, location)
	if err != nil {
		return nil
	}
	return &parsed
}

func compareISODate(left string, right string) int {
	switch {
	case strings.TrimSpace(left) == strings.TrimSpace(right):
		return 0
	case left < right:
		return -1
	default:
		return 1
	}
}

func (service *SettingsViewService) BuildSettingsSymptomsViewData(user *models.User) (SettingsSymptomsViewData, error) {
	if user == nil || user.Role != models.RoleOwner || service.symptoms == nil {
		return SettingsSymptomsViewData{}, nil
	}

	symptoms, err := service.symptoms.FetchSymptoms(user.ID)
	if err != nil {
		return SettingsSymptomsViewData{}, fmt.Errorf("%w: %v", ErrSettingsViewLoadSymptoms, err)
	}

	viewData := SettingsSymptomsViewData{
		ActiveCustomSymptoms:   make([]models.SymptomType, 0),
		ArchivedCustomSymptoms: make([]models.SymptomType, 0),
	}
	for _, symptom := range symptoms {
		if symptom.IsBuiltin {
			continue
		}
		if symptom.IsActive() {
			viewData.ActiveCustomSymptoms = append(viewData.ActiveCustomSymptoms, symptom)
			continue
		}
		viewData.ArchivedCustomSymptoms = append(viewData.ArchivedCustomSymptoms, symptom)
	}

	viewData.HasCustomSymptoms = len(viewData.ActiveCustomSymptoms) > 0
	viewData.HasArchivedSymptoms = len(viewData.ArchivedCustomSymptoms) > 0
	return viewData, nil
}
