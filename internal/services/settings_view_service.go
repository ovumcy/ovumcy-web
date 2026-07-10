package services

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/ovumcy/ovumcy-web/internal/models"
)

var (
	ErrSettingsViewLoadSettings = errors.New("settings view load settings")
	ErrSettingsViewLoadExport   = errors.New("settings view load export")
	ErrSettingsViewLoadSymptoms = errors.New("settings view load symptoms")
)

type SettingsViewLoader interface {
	LoadSettings(ctx context.Context, userID uint) (models.User, error)
}

type SettingsViewExportBuilder interface {
	BuildSummary(ctx context.Context, userID uint, from *time.Time, to *time.Time, location *time.Location) (ExportSummary, error)
}

type SettingsViewSymptomProvider interface {
	FetchSymptoms(ctx context.Context, userID uint) ([]models.SymptomType, error)
}

// SettingsViewWebhookStatusBuilder is the narrow seam the settings view uses to
// turn a stored webhook_url ciphertext into the render-safe status/host
// projection. *WebhookSettingsService satisfies it. Kept as an interface so the
// view service never holds the secret key directly and tests can stub the
// projection without encryption.
type SettingsViewWebhookStatusBuilder interface {
	BuildWebhookURLDisplay(userID uint, encryptedURL string) WebhookURLDisplay
}

// SettingsViewCalendarFeedStatusBuilder is the narrow seam the settings view
// uses to learn whether an owner's .ics feed is currently configured. It
// returns ONLY a configured/not-configured flag — never the token, selector, or
// a URL — so a normal settings load never surfaces the feed secret.
// *CalendarFeedSettingsService satisfies it.
type SettingsViewCalendarFeedStatusBuilder interface {
	BuildFeedStatus(ctx context.Context, userID uint) CalendarFeedStatus
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
	UnpredictableCycle      bool
	AgeGroup                string
	UsageGoal               string
	ShownPeriodTip          bool
	TrackBBT                bool
	TemperatureUnit         string
	TrackCervicalMucus      bool
	HideSexChip             bool
	HideCycleFactors        bool
	HideNotesField          bool
	ShowHistoricalPhases    bool
	WeekStartsOn            string
	ReminderLeadDays        int
	LastPeriodStart         string
	TodayISO                string
	CycleStartMinISO        string
	Export                  SettingsExportViewData
	Symptoms                SettingsSymptomsViewData
	HasOwnerExportViewState bool
	HasOwnerSymptomsView    bool
	// Webhook notification settings (issue #124). WebhookURLConfigured /
	// WebhookURLHost are the ONLY endpoint projection rendered — the stored URL
	// (a secret) is never surfaced. WebhookURLHost is at most the hostname.
	WebhookEnabled         bool
	WebhookNotifyPeriod    bool
	WebhookNotifyOvulation bool
	WebhookURLConfigured   bool
	WebhookURLHost         string
	// Calendar (.ics) feed subscription (slice 4). Only the configured flag is
	// rendered — never the token or the subscribe URL, which are shown exactly
	// once on generation via a sealed one-time reveal, mirroring recovery codes.
	CalendarFeedConfigured bool
}

type SettingsViewService struct {
	settings           SettingsViewLoader
	export             SettingsViewExportBuilder
	symptoms           SettingsViewSymptomProvider
	webhookStatus      SettingsViewWebhookStatusBuilder
	calendarFeedStatus SettingsViewCalendarFeedStatusBuilder
}

type settingsStatusKeys struct {
	errorKey               string
	changePasswordErrorKey string
	successKey             string
}

func NewSettingsViewService(settings SettingsViewLoader, export SettingsViewExportBuilder, symptoms SettingsViewSymptomProvider, webhookStatus SettingsViewWebhookStatusBuilder, calendarFeedStatus SettingsViewCalendarFeedStatusBuilder) *SettingsViewService {
	return &SettingsViewService{
		settings:           settings,
		export:             export,
		symptoms:           symptoms,
		webhookStatus:      webhookStatus,
		calendarFeedStatus: calendarFeedStatus,
	}
}

func (service *SettingsViewService) BuildSettingsPageViewData(ctx context.Context, user *models.User, language string, input SettingsViewInput, now time.Time, location *time.Location) (SettingsPageViewData, error) {
	statusKeys := service.resolveSettingsStatusKeys(input)
	persisted, err := service.settings.LoadSettings(ctx, user.ID)
	if err != nil {
		return SettingsPageViewData{}, fmt.Errorf("%w: %v", ErrSettingsViewLoadSettings, err)
	}

	_, today := SettingsCycleStartDateBounds(now, location)
	minCycleStart, _ := SettingsCycleStartDateBounds(now, location)
	resolvedUser, lastPeriodStart := buildResolvedSettingsUser(user, persisted, today, location)
	viewData := buildSettingsPageBaseViewData(resolvedUser, lastPeriodStart, today, minCycleStart, statusKeys)

	if resolvedUser.Role != models.RoleOwner {
		return viewData, nil
	}

	return viewData, service.populateOwnerSettingsViewData(ctx, &viewData, language, today, location)
}

func (service *SettingsViewService) resolveSettingsStatusKeys(input SettingsViewInput) settingsStatusKeys {
	status := ResolveSettingsStatus(input.FlashSuccess)
	keys := settingsStatusKeys{
		successKey: SettingsStatusTranslationKey(status),
	}
	if status != "" {
		return keys
	}

	errorSource := ResolveSettingsErrorSource(input.FlashError)
	translatedErrorKey := AuthErrorTranslationKey(errorSource)
	if translatedErrorKey == "" {
		return keys
	}
	if ClassifySettingsErrorSource(errorSource) == SettingsErrorTargetChangePassword {
		keys.changePasswordErrorKey = translatedErrorKey
		return keys
	}
	keys.errorKey = translatedErrorKey
	return keys
}

func buildResolvedSettingsUser(user *models.User, persisted models.User, today time.Time, location *time.Location) (models.User, string) {
	cycleLength, periodLength := ResolveCycleAndPeriodDefaults(persisted.CycleLength, persisted.PeriodLength)

	resolvedUser := *user
	resolvedUser.CycleLength = cycleLength
	resolvedUser.PeriodLength = periodLength
	resolvedUser.AutoPeriodFill = persisted.AutoPeriodFill
	resolvedUser.LocalAuthEnabled = persisted.LocalAuthEnabled
	resolvedUser.IrregularCycle = persisted.IrregularCycle
	resolvedUser.UnpredictableCycle = persisted.UnpredictableCycle
	resolvedUser.AgeGroup = NormalizeAgeGroup(persisted.AgeGroup)
	resolvedUser.UsageGoal = NormalizeUsageGoal(persisted.UsageGoal)
	resolvedUser.ShownPeriodTip = persisted.ShownPeriodTip
	resolvedUser.TrackBBT = persisted.TrackBBT
	resolvedUser.TemperatureUnit = NormalizeTemperatureUnit(persisted.TemperatureUnit)
	resolvedUser.TrackCervicalMucus = persisted.TrackCervicalMucus
	resolvedUser.HideSexChip = persisted.HideSexChip
	resolvedUser.HideCycleFactors = persisted.HideCycleFactors
	resolvedUser.HideNotesField = persisted.HideNotesField
	resolvedUser.ShowHistoricalPhases = persisted.ShowHistoricalPhases
	resolvedUser.WeekStartsOn = NormalizeWeekStart(persisted.WeekStartsOn)
	resolvedUser.ReminderLeadDays = NormalizeReminderLeadDays(persisted.ReminderLeadDays)
	resolvedUser.WebhookEnabled = persisted.WebhookEnabled
	resolvedUser.WebhookURL = persisted.WebhookURL
	resolvedUser.WebhookNotifyPeriod = persisted.WebhookNotifyPeriod
	resolvedUser.WebhookNotifyOvulation = persisted.WebhookNotifyOvulation
	resolvedUser.LastPeriodStart = persisted.LastPeriodStart

	lastPeriodStart := ""
	if persisted.LastPeriodStart != nil {
		sanitizedStart := CalendarDay(*persisted.LastPeriodStart, location)
		if sanitizedStart.After(today) {
			sanitizedStart = today
		}
		resolvedUser.LastPeriodStart = &sanitizedStart
		lastPeriodStart = sanitizedStart.Format("2006-01-02")
	}

	return resolvedUser, lastPeriodStart
}

func buildSettingsPageBaseViewData(user models.User, lastPeriodStart string, today time.Time, minCycleStart time.Time, statusKeys settingsStatusKeys) SettingsPageViewData {
	return SettingsPageViewData{
		CurrentUser:            user,
		ErrorKey:               statusKeys.errorKey,
		ChangePasswordErrorKey: statusKeys.changePasswordErrorKey,
		SuccessKey:             statusKeys.successKey,
		CycleLength:            user.CycleLength,
		PeriodLength:           user.PeriodLength,
		AutoPeriodFill:         user.AutoPeriodFill,
		IrregularCycle:         user.IrregularCycle,
		UnpredictableCycle:     user.UnpredictableCycle,
		AgeGroup:               user.AgeGroup,
		UsageGoal:              user.UsageGoal,
		ShownPeriodTip:         user.ShownPeriodTip,
		TrackBBT:               user.TrackBBT,
		TemperatureUnit:        user.TemperatureUnit,
		TrackCervicalMucus:     user.TrackCervicalMucus,
		HideSexChip:            user.HideSexChip,
		HideCycleFactors:       user.HideCycleFactors,
		HideNotesField:         user.HideNotesField,
		ShowHistoricalPhases:   user.ShowHistoricalPhases,
		WeekStartsOn:           user.WeekStartsOn,
		ReminderLeadDays:       user.ReminderLeadDays,
		LastPeriodStart:        lastPeriodStart,
		TodayISO:               today.Format("2006-01-02"),
		CycleStartMinISO:       minCycleStart.Format("2006-01-02"),
	}
}

func (service *SettingsViewService) populateOwnerSettingsViewData(ctx context.Context, viewData *SettingsPageViewData, language string, today time.Time, location *time.Location) error {
	service.populateOwnerWebhookViewData(viewData)
	service.populateOwnerCalendarFeedViewData(ctx, viewData)

	if service.symptoms != nil {
		symptomsViewData, err := service.BuildSettingsSymptomsViewData(ctx, &viewData.CurrentUser)
		if err != nil {
			return err
		}
		viewData.Symptoms = symptomsViewData
		viewData.HasOwnerSymptomsView = true
	}

	if service.export == nil {
		return nil
	}

	exportViewData, err := service.buildOwnerExportViewData(ctx, viewData.CurrentUser.ID, language, today, location)
	if err != nil {
		return err
	}
	viewData.Export = exportViewData
	viewData.HasOwnerExportViewState = true
	return nil
}

// populateOwnerWebhookViewData sets the webhook toggles and the render-safe
// URL status (configured + host) on the owner's settings view. The stored URL
// is a secret, so only the projection from BuildWebhookURLDisplay is copied —
// never the ciphertext or plaintext URL. Without a webhook status builder the
// toggles still populate but the URL status stays "not configured".
func (service *SettingsViewService) populateOwnerWebhookViewData(viewData *SettingsPageViewData) {
	viewData.WebhookEnabled = viewData.CurrentUser.WebhookEnabled
	viewData.WebhookNotifyPeriod = viewData.CurrentUser.WebhookNotifyPeriod
	viewData.WebhookNotifyOvulation = viewData.CurrentUser.WebhookNotifyOvulation

	if service.webhookStatus == nil {
		return
	}
	display := service.webhookStatus.BuildWebhookURLDisplay(viewData.CurrentUser.ID, viewData.CurrentUser.WebhookURL)
	viewData.WebhookURLConfigured = display.Configured
	viewData.WebhookURLHost = display.Host
}

// populateOwnerCalendarFeedViewData sets the render-safe .ics feed status
// (configured vs not) on the owner's settings view. It surfaces ONLY the
// boolean — never the token or the subscribe URL — so a normal settings load
// (or any revisit) can never re-render the secret; the URL is shown exactly once
// on generation via a sealed one-time reveal. Without a status builder the flag
// stays false (not configured).
func (service *SettingsViewService) populateOwnerCalendarFeedViewData(ctx context.Context, viewData *SettingsPageViewData) {
	if service.calendarFeedStatus == nil {
		return
	}
	viewData.CalendarFeedConfigured = service.calendarFeedStatus.BuildFeedStatus(ctx, viewData.CurrentUser.ID).Configured
}

func (service *SettingsViewService) buildOwnerExportViewData(ctx context.Context, userID uint, language string, today time.Time, location *time.Location) (SettingsExportViewData, error) {
	availableSummary, err := service.export.BuildSummary(ctx, userID, nil, nil, location)
	if err != nil {
		return SettingsExportViewData{}, fmt.Errorf("%w: %v", ErrSettingsViewLoadExport, err)
	}
	if !availableSummary.HasData {
		return SettingsExportViewData{HasSummaryForOwner: true}, nil
	}

	defaultFrom, defaultTo, selectableMin, selectableMax := resolveOwnerExportDateBounds(availableSummary, today)
	defaultSummary, err := service.export.BuildSummary(
		ctx,
		userID,
		exportSummaryBound(defaultFrom, location),
		exportSummaryBound(defaultTo, location),
		location,
	)
	if err != nil {
		return SettingsExportViewData{}, fmt.Errorf("%w: %v", ErrSettingsViewLoadExport, err)
	}

	return SettingsExportViewData{
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
	}, nil
}

func resolveOwnerExportDateBounds(availableSummary ExportSummary, today time.Time) (string, string, string, string) {
	todayISO := today.Format(exportDateLayout)
	selectableMin := todayISO
	if compareISODate(availableSummary.DateFrom, selectableMin) < 0 {
		selectableMin = availableSummary.DateFrom
	}
	selectableMax := todayISO
	if compareISODate(availableSummary.DateTo, selectableMax) > 0 {
		selectableMax = availableSummary.DateTo
	}
	return selectableMin, todayISO, selectableMin, selectableMax
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

func (service *SettingsViewService) BuildSettingsSymptomsViewData(ctx context.Context, user *models.User) (SettingsSymptomsViewData, error) {
	if user == nil || user.Role != models.RoleOwner || service.symptoms == nil {
		return SettingsSymptomsViewData{}, nil
	}

	symptoms, err := service.symptoms.FetchSymptoms(ctx, user.ID)
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
