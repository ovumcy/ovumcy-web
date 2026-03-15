package services

import (
	"sort"

	"github.com/terraincognita07/ovumcy/internal/models"
)

func RankSymptomsForEntryPicker(symptoms []models.SymptomType, logs []models.DailyLog) []models.SymptomType {
	if len(symptoms) < 2 || len(logs) == 0 {
		result := make([]models.SymptomType, len(symptoms))
		copy(result, symptoms)
		return result
	}

	counts := make(map[uint]int, len(symptoms))
	for _, logEntry := range logs {
		for _, symptomID := range logEntry.SymptomIDs {
			counts[symptomID]++
		}
	}

	ranked := make([]models.SymptomType, len(symptoms))
	copy(ranked, symptoms)
	sort.SliceStable(ranked, func(i, j int) bool {
		return counts[ranked[i].ID] > counts[ranked[j].ID]
	})
	return ranked
}

func SplitSymptomsForCollapsedPicker(symptoms []models.SymptomType, selectedIDs map[uint]bool, primaryLimit int) ([]models.SymptomType, []models.SymptomType) {
	if primaryLimit <= 0 || len(symptoms) == 0 {
		result := make([]models.SymptomType, len(symptoms))
		copy(result, symptoms)
		return result, []models.SymptomType{}
	}

	primary := make([]models.SymptomType, 0, primaryLimit)
	custom := make([]models.SymptomType, 0, len(symptoms))
	remaining := make([]models.SymptomType, 0, len(symptoms))
	deferred := make([]models.SymptomType, 0, len(symptoms))

	for _, symptom := range symptoms {
		if selectedIDs[symptom.ID] {
			primary = append(primary, symptom)
			continue
		}
		if shouldDeferSymptomFromPrimaryPath(symptom) {
			deferred = append(deferred, symptom)
			continue
		}
		if !symptom.IsBuiltin && symptom.IsActive() {
			custom = append(custom, symptom)
			continue
		}
		remaining = append(remaining, symptom)
	}

	for _, symptom := range custom {
		if len(primary) < primaryLimit {
			primary = append(primary, symptom)
			continue
		}
		remaining = append(remaining, symptom)
	}

	extra := make([]models.SymptomType, 0)
	for _, symptom := range remaining {
		if len(primary) < primaryLimit {
			primary = append(primary, symptom)
			continue
		}
		extra = append(extra, symptom)
	}
	extra = append(extra, deferred...)

	return primary, extra
}

func shouldDeferSymptomFromPrimaryPath(symptom models.SymptomType) bool {
	return symptom.IsBuiltin && BuiltinSymptomTranslationKey(symptom.Name) == "symptoms.mood_swings"
}
