package services

import (
	"sort"
	"strings"
	"time"

	"github.com/terraincognita07/ovumcy/internal/models"
)

const exportDateLayout = "2006-01-02"

var ExportCSVHeaders = []string{
	"Date",
	"Period",
	"Flow",
	"Cramps",
	"Headache",
	"Acne",
	"Mood",
	"Bloating",
	"Fatigue",
	"Breast tenderness",
	"Back pain",
	"Nausea",
	"Spotting",
	"Irritability",
	"Insomnia",
	"Food cravings",
	"Diarrhea",
	"Constipation",
	"Other",
	"Notes",
}

var exportSymptomColumnsByName = map[string]string{
	"cramps":            "cramps",
	"headache":          "headache",
	"acne":              "acne",
	"mood":              "mood",
	"mood swings":       "mood",
	"bloating":          "bloating",
	"fatigue":           "fatigue",
	"breast tenderness": "breast_tenderness",
	"back pain":         "back_pain",
	"nausea":            "nausea",
	"spotting":          "spotting",
	"irritability":      "irritability",
	"insomnia":          "insomnia",
	"food cravings":     "food_cravings",
	"diarrhea":          "diarrhea",
	"constipation":      "constipation",
}

var exportSymptomFlagSetters = map[string]func(*ExportSymptomFlags){
	"cramps": func(flags *ExportSymptomFlags) {
		flags.Cramps = true
	},
	"headache": func(flags *ExportSymptomFlags) {
		flags.Headache = true
	},
	"acne": func(flags *ExportSymptomFlags) {
		flags.Acne = true
	},
	"mood": func(flags *ExportSymptomFlags) {
		flags.Mood = true
	},
	"bloating": func(flags *ExportSymptomFlags) {
		flags.Bloating = true
	},
	"fatigue": func(flags *ExportSymptomFlags) {
		flags.Fatigue = true
	},
	"breast_tenderness": func(flags *ExportSymptomFlags) {
		flags.BreastTenderness = true
	},
	"back_pain": func(flags *ExportSymptomFlags) {
		flags.BackPain = true
	},
	"nausea": func(flags *ExportSymptomFlags) {
		flags.Nausea = true
	},
	"spotting": func(flags *ExportSymptomFlags) {
		flags.Spotting = true
	},
	"irritability": func(flags *ExportSymptomFlags) {
		flags.Irritability = true
	},
	"insomnia": func(flags *ExportSymptomFlags) {
		flags.Insomnia = true
	},
	"food_cravings": func(flags *ExportSymptomFlags) {
		flags.FoodCravings = true
	},
	"diarrhea": func(flags *ExportSymptomFlags) {
		flags.Diarrhea = true
	},
	"constipation": func(flags *ExportSymptomFlags) {
		flags.Constipation = true
	},
}

type ExportDayReader interface {
	FetchLogsForOptionalRange(userID uint, from *time.Time, to *time.Time, location *time.Location) ([]models.DailyLog, error)
}

type ExportSymptomReader interface {
	FetchSymptoms(userID uint) ([]models.SymptomType, error)
}

type ExportService struct {
	days     ExportDayReader
	symptoms ExportSymptomReader
}

type ExportSummary struct {
	TotalEntries int
	HasData      bool
	DateFrom     string
	DateTo       string
}

type ExportSymptomFlags struct {
	Cramps           bool `json:"cramps"`
	Headache         bool `json:"headache"`
	Acne             bool `json:"acne"`
	Mood             bool `json:"mood"`
	Bloating         bool `json:"bloating"`
	Fatigue          bool `json:"fatigue"`
	BreastTenderness bool `json:"breast_tenderness"`
	BackPain         bool `json:"back_pain"`
	Nausea           bool `json:"nausea"`
	Spotting         bool `json:"spotting"`
	Irritability     bool `json:"irritability"`
	Insomnia         bool `json:"insomnia"`
	FoodCravings     bool `json:"food_cravings"`
	Diarrhea         bool `json:"diarrhea"`
	Constipation     bool `json:"constipation"`
}

type ExportJSONEntry struct {
	Date          string             `json:"date"`
	Period        bool               `json:"period"`
	Flow          string             `json:"flow"`
	Symptoms      ExportSymptomFlags `json:"symptoms"`
	OtherSymptoms []string           `json:"other_symptoms"`
	Notes         string             `json:"notes"`
}

type ExportCSVRow struct {
	Date          string
	Period        bool
	Flow          string
	Symptoms      ExportSymptomFlags
	OtherSymptoms []string
	Notes         string
}

func NewExportService(days ExportDayReader, symptoms ExportSymptomReader) *ExportService {
	return &ExportService{
		days:     days,
		symptoms: symptoms,
	}
}

func (service *ExportService) LoadDataForRange(userID uint, from *time.Time, to *time.Time, location *time.Location) ([]models.DailyLog, map[uint]string, error) {
	logs, err := service.days.FetchLogsForOptionalRange(userID, from, to, location)
	if err != nil {
		return nil, nil, err
	}

	symptoms, err := service.symptoms.FetchSymptoms(userID)
	if err != nil {
		return nil, nil, err
	}

	symptomNames := make(map[uint]string, len(symptoms))
	for _, symptom := range symptoms {
		symptomNames[symptom.ID] = symptom.Name
	}

	return logs, symptomNames, nil
}

func (service *ExportService) BuildSummary(userID uint, from *time.Time, to *time.Time, location *time.Location) (ExportSummary, error) {
	logs, err := service.days.FetchLogsForOptionalRange(userID, from, to, location)
	if err != nil {
		return ExportSummary{}, err
	}
	if len(logs) == 0 {
		return ExportSummary{}, nil
	}

	first := logs[0].Date
	last := logs[0].Date
	for _, logEntry := range logs[1:] {
		if logEntry.Date.Before(first) {
			first = logEntry.Date
		}
		if logEntry.Date.After(last) {
			last = logEntry.Date
		}
	}

	return ExportSummary{
		TotalEntries: len(logs),
		HasData:      true,
		DateFrom:     DateAtLocation(first, location).Format(exportDateLayout),
		DateTo:       DateAtLocation(last, location).Format(exportDateLayout),
	}, nil
}

func (service *ExportService) BuildJSONEntries(userID uint, from *time.Time, to *time.Time, location *time.Location) ([]ExportJSONEntry, error) {
	logs, symptomNames, err := service.LoadDataForRange(userID, from, to, location)
	if err != nil {
		return nil, err
	}

	entries := make([]ExportJSONEntry, 0, len(logs))
	for _, logEntry := range logs {
		flags, other := buildExportSymptomFlags(logEntry.SymptomIDs, symptomNames)
		entries = append(entries, ExportJSONEntry{
			Date:          DateAtLocation(logEntry.Date, location).Format(exportDateLayout),
			Period:        logEntry.IsPeriod,
			Flow:          normalizeExportFlow(logEntry.Flow),
			Symptoms:      flags,
			OtherSymptoms: other,
			Notes:         logEntry.Notes,
		})
	}
	return entries, nil
}

func (service *ExportService) BuildCSVRows(userID uint, from *time.Time, to *time.Time, location *time.Location) ([]ExportCSVRow, error) {
	logs, symptomNames, err := service.LoadDataForRange(userID, from, to, location)
	if err != nil {
		return nil, err
	}

	rows := make([]ExportCSVRow, 0, len(logs))
	for _, logEntry := range logs {
		flags, other := buildExportSymptomFlags(logEntry.SymptomIDs, symptomNames)
		rows = append(rows, ExportCSVRow{
			Date:          DateAtLocation(logEntry.Date, location).Format(exportDateLayout),
			Period:        logEntry.IsPeriod,
			Flow:          csvFlowLabel(logEntry.Flow),
			Symptoms:      flags,
			OtherSymptoms: other,
			Notes:         logEntry.Notes,
		})
	}
	return rows, nil
}

func (row ExportCSVRow) Columns() []string {
	return []string{
		row.Date,
		csvYesNo(row.Period),
		row.Flow,
		csvYesNo(row.Symptoms.Cramps),
		csvYesNo(row.Symptoms.Headache),
		csvYesNo(row.Symptoms.Acne),
		csvYesNo(row.Symptoms.Mood),
		csvYesNo(row.Symptoms.Bloating),
		csvYesNo(row.Symptoms.Fatigue),
		csvYesNo(row.Symptoms.BreastTenderness),
		csvYesNo(row.Symptoms.BackPain),
		csvYesNo(row.Symptoms.Nausea),
		csvYesNo(row.Symptoms.Spotting),
		csvYesNo(row.Symptoms.Irritability),
		csvYesNo(row.Symptoms.Insomnia),
		csvYesNo(row.Symptoms.FoodCravings),
		csvYesNo(row.Symptoms.Diarrhea),
		csvYesNo(row.Symptoms.Constipation),
		strings.Join(row.OtherSymptoms, "; "),
		row.Notes,
	}
}

func buildExportSymptomFlags(symptomIDs []uint, symptomNames map[uint]string) (ExportSymptomFlags, []string) {
	flags := ExportSymptomFlags{}
	otherSet := make(map[string]struct{})

	for _, symptomID := range symptomIDs {
		name, ok := symptomNames[symptomID]
		if !ok {
			continue
		}

		if setExportSymptomFlag(&flags, exportSymptomColumn(name)) {
			continue
		}

		trimmed := strings.TrimSpace(name)
		if trimmed != "" {
			otherSet[trimmed] = struct{}{}
		}
	}

	other := make([]string, 0, len(otherSet))
	for name := range otherSet {
		other = append(other, name)
	}
	sort.Strings(other)

	return flags, other
}

func setExportSymptomFlag(flags *ExportSymptomFlags, column string) bool {
	setter, ok := exportSymptomFlagSetters[column]
	if !ok {
		return false
	}
	setter(flags)
	return true
}

func exportSymptomColumn(name string) string {
	normalized := strings.ToLower(strings.TrimSpace(name))
	if column, ok := exportSymptomColumnsByName[normalized]; ok {
		return column
	}
	return "other"
}

func csvYesNo(value bool) string {
	if value {
		return "Yes"
	}
	return "No"
}

func csvFlowLabel(flow string) string {
	switch strings.ToLower(strings.TrimSpace(flow)) {
	case models.FlowLight:
		return "Light"
	case models.FlowMedium:
		return "Medium"
	case models.FlowHeavy:
		return "Heavy"
	default:
		return "None"
	}
}

func normalizeExportFlow(flow string) string {
	switch strings.ToLower(strings.TrimSpace(flow)) {
	case models.FlowLight:
		return models.FlowLight
	case models.FlowMedium:
		return models.FlowMedium
	case models.FlowHeavy:
		return models.FlowHeavy
	default:
		return models.FlowNone
	}
}
