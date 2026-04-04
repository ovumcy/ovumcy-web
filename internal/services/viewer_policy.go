package services

import "github.com/ovumcy/ovumcy-web/internal/models"

func SanitizeRestrictedViewerLog(entry models.DailyLog) models.DailyLog {
	entry.Mood = 0
	entry.SexActivity = models.SexActivityNone
	entry.BBT = 0
	entry.CervicalMucus = models.CervicalMucusNone
	entry.CycleFactorKeys = []string{}
	entry.Notes = ""
	entry.SymptomIDs = []uint{}
	return entry
}

func SanitizeLogForViewer(user *models.User, entry models.DailyLog) models.DailyLog {
	if IsOwnerUser(user) {
		return entry
	}
	return SanitizeRestrictedViewerLog(entry)
}

func SanitizeLogsForViewer(user *models.User, logs []models.DailyLog) {
	if IsOwnerUser(user) {
		return
	}
	for index := range logs {
		logs[index] = SanitizeRestrictedViewerLog(logs[index])
	}
}

func ShouldExposeSymptomsForViewer(user *models.User) bool {
	return IsOwnerUser(user)
}
