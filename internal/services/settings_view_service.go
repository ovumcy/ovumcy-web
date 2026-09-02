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

// SettingsViewExportBuilder is the seam the settings page reads its export
// figures through. It asks for the owner's whole history and the window ending
// today in ONE call because the two used to be two reads of the same rows.
type SettingsViewExportBuilder interface {
	BuildSummaryHistoryAndWindow(ctx context.Context, userID uint, through time.Time, location *time.Location) (ExportSummary, ExportSummary, error)
}

type SettingsViewSymptomProvider interface {
	FetchSymptoms(ctx context.Context, userID uint) ([]models.SymptomType, error)
}

// SettingsViewEgressLedgerBuilder is the single seam the settings view reads the
// egress ledger through. It replaced two — a webhook status builder and a feed
// status builder — because the heading state is an automaton over BOTH paths and
// cannot be assembled from two independently rendered halves. *EgressLedgerService
// satisfies it. Kept as an interface so the view service holds neither the secret
// key nor the runtime configuration.
type SettingsViewEgressLedgerBuilder interface {
	BuildEgressLedger(ctx context.Context, user models.User) EgressLedger
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
	CurrentUser            models.User
	ErrorKey               string
	ChangePasswordErrorKey string
	SuccessKey             string
	CycleLength            int
	PeriodLength           int
	AutoPeriodFill         bool
	IrregularCycle         bool
	UnpredictableCycle     bool
	AgeGroup               string
	UsageGoal              string
	ShownPeriodTip         bool
	TrackBBT               bool
	TemperatureUnit        string
	TrackCervicalMucus     bool
	// The three section toggles are rendered positively; the stored columns are
	// inverted and are converted exactly once, in tracking_visibility.go.
	ShowSexChip             bool
	ShowCycleFactors        bool
	ShowNotesField          bool
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
	// Egress is the owner-only account of the webhook and .ics paths — their
	// states, the one timestamp each can prove, and what each carries off the
	// instance. It rides the optional-block convention: a session that is not
	// this row's owner leaves HasOwnerEgressLedger false and Egress zero, and the
	// api layer projects NOTHING from it, so the absence reaches the page as an
	// absent key rather than as a false one.
	Egress               EgressLedger
	HasOwnerEgressLedger bool
}

type SettingsViewService struct {
	settings SettingsViewLoader
	export   SettingsViewExportBuilder
	symptoms SettingsViewSymptomProvider
	egress   SettingsViewEgressLedgerBuilder
}

type settingsStatusKeys struct {
	errorKey               string
	changePasswordErrorKey string
	successKey             string
}

func NewSettingsViewService(settings SettingsViewLoader, export SettingsViewExportBuilder, symptoms SettingsViewSymptomProvider, egressLedger SettingsViewEgressLedgerBuilder) *SettingsViewService {
	return &SettingsViewService{
		settings: settings,
		export:   export,
		symptoms: symptoms,
		egress:   egressLedger,
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
	TrackingVisibilityForUser(&persisted).ApplyToUser(&resolvedUser)
	resolvedUser.ShowHistoricalPhases = persisted.ShowHistoricalPhases
	resolvedUser.WeekStartsOn = NormalizeWeekStart(persisted.WeekStartsOn)
	resolvedUser.ReminderLeadDays = NormalizeReminderLeadDays(persisted.ReminderLeadDays)
	resolvedUser.WebhookEnabled = persisted.WebhookEnabled
	resolvedUser.WebhookURL = persisted.WebhookURL
	resolvedUser.WebhookNotifyPeriod = persisted.WebhookNotifyPeriod
	resolvedUser.WebhookNotifyOvulation = persisted.WebhookNotifyOvulation
	// The delivery mark rides the SAME projection as the columns above it: the
	// session user is a copy taken at sign-in and would answer "no delivery has
	// been recorded" for every owner whose delivery happened afterwards.
	resolvedUser.WebhookLastDeliveredAt = persisted.WebhookLastDeliveredAt
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
	visibility := TrackingVisibilityForUser(&user)
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
		ShowSexChip:            visibility.ShowSexChip,
		ShowCycleFactors:       visibility.ShowCycleFactors,
		ShowNotesField:         visibility.ShowNotesField,
		ShowHistoricalPhases:   user.ShowHistoricalPhases,
		WeekStartsOn:           user.WeekStartsOn,
		ReminderLeadDays:       user.ReminderLeadDays,
		LastPeriodStart:        lastPeriodStart,
		TodayISO:               today.Format("2006-01-02"),
		CycleStartMinISO:       minCycleStart.Format("2006-01-02"),
	}
}

func (service *SettingsViewService) populateOwnerSettingsViewData(ctx context.Context, viewData *SettingsPageViewData, language string, today time.Time, location *time.Location) error {
	service.populateOwnerEgressLedger(ctx, viewData)

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

// populateOwnerEgressLedger builds the owner's egress account and marks the
// optional block present. Nothing from the ledger — no state, no timestamp, no
// host — reaches the view data unless this runs, and this runs only past the
// owner gate. Without a ledger builder the flag stays false and the block is
// absent rather than empty, which is what keeps a partially wired instance from
// rendering "nothing is configured" about paths it never looked at.
func (service *SettingsViewService) populateOwnerEgressLedger(ctx context.Context, viewData *SettingsPageViewData) {
	if service.egress == nil {
		return
	}
	viewData.Egress = service.egress.BuildEgressLedger(ctx, viewData.CurrentUser)
	viewData.HasOwnerEgressLedger = true
}

func (service *SettingsViewService) buildOwnerExportViewData(ctx context.Context, userID uint, language string, today time.Time, location *time.Location) (SettingsExportViewData, error) {
	// One read, two aggregates: the panel's selectable bounds come from
	// everything the owner has, its default window ends today. The default
	// window's lower bound is the owner's earliest entry, so the window IS the
	// history up to today and needs no second query to describe it.
	availableSummary, defaultSummary, err := service.export.BuildSummaryHistoryAndWindow(ctx, userID, today, location)
	if err != nil {
		return SettingsExportViewData{}, fmt.Errorf("%w: %v", ErrSettingsViewLoadExport, err)
	}
	if !availableSummary.HasData {
		return SettingsExportViewData{HasSummaryForOwner: true}, nil
	}

	defaultFrom, defaultTo, selectableMin, selectableMax := resolveOwnerExportDateBounds(availableSummary, today)

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

// compareISODate orders two date-only ISO strings. Both operands are trimmed
// ONCE into locals and every arm reads those: the equality arm used to trim
// while the ordering arm compared the raw strings, so a padded value was
// "not equal" and then ordered by its leading space — 0x20 sorts below every
// digit — and a later date reported as the earlier one.
func compareISODate(left string, right string) int {
	leftDate := strings.TrimSpace(left)
	rightDate := strings.TrimSpace(right)
	switch {
	case leftDate == rightDate:
		return 0
	case leftDate < rightDate:
		return -1
	default:
		return 1
	}
}

// ErrSettingsViewLoadEgress wraps a failure to re-read the owner's settings row
// while rebuilding the egress block.
var ErrSettingsViewLoadEgress = errors.New("settings view load egress")

// BuildSettingsEgressViewData rebuilds the owner's egress ledger from a FRESH
// read of the settings row, and it exists so a mutation's response can be built
// from what the database now holds rather than from what the request intended.
// The distinction is the whole point: a response assembled from the caller's
// intent renders a proposition the write it is reporting has just falsified, and
// it does so most convincingly on success.
//
// It re-reads deliberately. Handing it the caller's stale user would make the
// read-count identical to a response built from intent, which is exactly the
// difference the guard exists to see.
//
// A non-owner receives the zero ledger and no error: the gate is the role, and
// this method is reachable from a route that already refused a non-owner.
// Regression: TestEveryEgressMutationRebuildsItsBlockFromAReadAfterTheWrite.
func (service *SettingsViewService) BuildSettingsEgressViewData(ctx context.Context, user *models.User) (EgressLedger, error) {
	if user == nil || user.Role != models.RoleOwner || service.egress == nil {
		return EgressLedger{}, nil
	}

	reloaded, err := service.settings.LoadSettings(ctx, user.ID)
	if err != nil {
		return EgressLedger{}, fmt.Errorf("%w: %v", ErrSettingsViewLoadEgress, err)
	}
	reloaded.ID = user.ID
	reloaded.Role = user.Role

	return service.egress.BuildEgressLedger(ctx, reloaded), nil
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
