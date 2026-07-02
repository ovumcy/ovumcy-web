package api

import (
	"time"

	"github.com/ovumcy/ovumcy-web/internal/models"
	"github.com/ovumcy/ovumcy-web/internal/services"
)

// dayResponse is the transport DTO for a persisted day record on the public
// /api/v1/days surface. It mirrors the `DailyLog` schema in docs/openapi.yaml
// (snake_case keys, `date` as a calendar date-only string) so the wire format
// conforms to the published contract. models.DailyLog stays transport-free:
// serialization lives in the api layer, not on the model.
type dayResponse struct {
	ID              uint      `json:"id"`
	UserID          uint      `json:"user_id"`
	Date            string    `json:"date"`
	IsPeriod        bool      `json:"is_period"`
	CycleStart      bool      `json:"cycle_start"`
	IsUncertain     bool      `json:"is_uncertain"`
	Flow            string    `json:"flow"`
	Mood            int       `json:"mood"`
	SexActivity     string    `json:"sex_activity"`
	BBT             *float64  `json:"bbt,omitempty"`
	CervicalMucus   string    `json:"cervical_mucus"`
	PregnancyTest   string    `json:"pregnancy_test"`
	CycleFactorKeys []string  `json:"cycle_factor_keys"`
	SymptomIDs      []uint    `json:"symptom_ids"`
	Notes           string    `json:"notes"`
	CreatedAt       time.Time `json:"created_at"`
	UpdatedAt       time.Time `json:"updated_at"`
}

// newDayResponse maps a raw GORM day model onto its transport DTO. The stored
// calendar date is emitted verbatim via services.CalendarDayKey (no timezone
// shift) so the wire `date` matches docs/openapi.yaml `format: date`.
func newDayResponse(entry models.DailyLog) dayResponse {
	return dayResponse{
		ID:              entry.ID,
		UserID:          entry.UserID,
		Date:            services.CalendarDayKey(entry.Date),
		IsPeriod:        entry.IsPeriod,
		CycleStart:      entry.CycleStart,
		IsUncertain:     entry.IsUncertain,
		Flow:            entry.Flow,
		Mood:            entry.Mood,
		SexActivity:     entry.SexActivity,
		BBT:             entry.BBT,
		CervicalMucus:   entry.CervicalMucus,
		PregnancyTest:   entry.PregnancyTest,
		CycleFactorKeys: entry.CycleFactorKeys,
		SymptomIDs:      entry.SymptomIDs,
		Notes:           entry.Notes,
		CreatedAt:       entry.CreatedAt,
		UpdatedAt:       entry.UpdatedAt,
	}
}

// newDayResponses maps a slice of day models to their transport DTOs, preserving
// order. A nil input yields an empty (non-nil) slice so the list endpoint always
// serializes a JSON array.
func newDayResponses(entries []models.DailyLog) []dayResponse {
	out := make([]dayResponse, 0, len(entries))
	for _, entry := range entries {
		out = append(out, newDayResponse(entry))
	}
	return out
}

// symptomResponse is the transport DTO for a symptom type on the public
// /api/v1/symptoms surface. It mirrors the `Symptom` schema in
// docs/openapi.yaml (snake_case keys, nullable RFC3339 archived_at).
type symptomResponse struct {
	ID         uint       `json:"id"`
	UserID     uint       `json:"user_id"`
	Name       string     `json:"name"`
	Icon       string     `json:"icon"`
	Color      string     `json:"color"`
	IsBuiltin  bool       `json:"is_builtin"`
	ArchivedAt *time.Time `json:"archived_at"`
}

// newSymptomResponse maps a raw GORM symptom model onto its transport DTO.
func newSymptomResponse(symptom models.SymptomType) symptomResponse {
	return symptomResponse{
		ID:         symptom.ID,
		UserID:     symptom.UserID,
		Name:       symptom.Name,
		Icon:       symptom.Icon,
		Color:      symptom.Color,
		IsBuiltin:  symptom.IsBuiltin,
		ArchivedAt: symptom.ArchivedAt,
	}
}

// newSymptomResponses maps a slice of symptom models to their transport DTOs,
// preserving order. A nil input yields an empty (non-nil) slice.
func newSymptomResponses(symptoms []models.SymptomType) []symptomResponse {
	out := make([]symptomResponse, 0, len(symptoms))
	for _, symptom := range symptoms {
		out = append(out, newSymptomResponse(symptom))
	}
	return out
}
