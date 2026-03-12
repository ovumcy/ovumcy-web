package services

import (
	"errors"
	"fmt"
	"sort"
	"time"

	"github.com/terraincognita07/ovumcy/internal/models"
)

var ErrInvalidSymptomID = errors.New("invalid symptom id")

var (
	ErrSymptomNameRequired          = errors.New("symptom name is required")
	ErrSymptomNameTooLong           = errors.New("symptom name is too long")
	ErrSymptomNameInvalidCharacters = errors.New("symptom name contains invalid characters")
	ErrInvalidSymptomColor          = errors.New("invalid symptom color")
	ErrSymptomNameAlreadyExists     = errors.New("symptom name already exists")
	ErrSymptomNotFound              = errors.New("symptom not found")
	ErrBuiltinSymptomEditForbidden  = errors.New("built-in symptom cannot be edited")
	ErrBuiltinSymptomHideForbidden  = errors.New("built-in symptom cannot be hidden")
	ErrBuiltinSymptomShowForbidden  = errors.New("built-in symptom cannot be restored")
	ErrCreateSymptomFailed          = errors.New("create symptom failed")
	ErrUpdateSymptomFailed          = errors.New("update symptom failed")
	ErrArchiveSymptomFailed         = errors.New("archive symptom failed")
	ErrRestoreSymptomFailed         = errors.New("restore symptom failed")
)

type SymptomRepository interface {
	CountBuiltinByUser(userID uint) (int64, error)
	CountByUserAndIDs(userID uint, ids []uint) (int64, error)
	ListByUser(userID uint) ([]models.SymptomType, error)
	Create(symptom *models.SymptomType) error
	CreateBatch(symptoms []models.SymptomType) error
	FindByIDForUser(symptomID uint, userID uint) (models.SymptomType, error)
	Update(symptom *models.SymptomType) error
}

type SymptomService struct {
	symptoms         SymptomRepository
	reservedNameKeys map[string]struct{}
}

var legacyEntryPickerHiddenSymptoms = map[string]struct{}{
	"moodswings":   {},
	"fatigue":      {},
	"irritability": {},
	"insomnia":     {},
}

type SymptomFrequency struct {
	Name      string
	Icon      string
	Count     int
	TotalDays int
}

func NewSymptomService(symptoms SymptomRepository, reservedBuiltinNames ...string) *SymptomService {
	return &SymptomService{
		symptoms:         symptoms,
		reservedNameKeys: builtinSymptomReservedNameKeys(reservedBuiltinNames),
	}
}

func (service *SymptomService) CreateSymptomForUser(userID uint, name string, icon string, color string) (models.SymptomType, error) {
	normalized, err := service.normalizeCustomSymptomInput(userID, 0, name, icon, color, defaultSymptomColor)
	if err != nil {
		if isSymptomValidationError(err) {
			return models.SymptomType{}, err
		}
		return models.SymptomType{}, fmt.Errorf("%w: %v", ErrCreateSymptomFailed, err)
	}

	if err := service.symptoms.Create(&normalized); err != nil {
		return models.SymptomType{}, fmt.Errorf("%w: %v", ErrCreateSymptomFailed, err)
	}
	return normalized, nil
}

func (service *SymptomService) UpdateSymptomForUser(userID uint, symptomID uint, name string, icon string, color string) (models.SymptomType, error) {
	symptom, err := service.symptoms.FindByIDForUser(symptomID, userID)
	if err != nil {
		return models.SymptomType{}, fmt.Errorf("%w: %v", ErrSymptomNotFound, err)
	}
	if symptom.IsBuiltin {
		return models.SymptomType{}, ErrBuiltinSymptomEditForbidden
	}

	normalized, err := service.normalizeCustomSymptomInput(userID, symptom.ID, name, icon, color, symptom.Color)
	if err != nil {
		if isSymptomValidationError(err) {
			return models.SymptomType{}, err
		}
		return models.SymptomType{}, fmt.Errorf("%w: %v", ErrUpdateSymptomFailed, err)
	}

	symptom.Name = normalized.Name
	symptom.Icon = normalized.Icon
	symptom.Color = normalized.Color
	if err := service.symptoms.Update(&symptom); err != nil {
		return models.SymptomType{}, fmt.Errorf("%w: %v", ErrUpdateSymptomFailed, err)
	}
	return symptom, nil
}

func (service *SymptomService) ArchiveSymptomForUser(userID uint, symptomID uint, archivedAt time.Time) error {
	symptom, err := service.symptoms.FindByIDForUser(symptomID, userID)
	if err != nil {
		return fmt.Errorf("%w: %v", ErrSymptomNotFound, err)
	}
	if symptom.IsBuiltin {
		return ErrBuiltinSymptomHideForbidden
	}
	if symptom.ArchivedAt != nil {
		return nil
	}

	archivedAt = archivedAt.UTC()
	symptom.ArchivedAt = &archivedAt
	if err := service.symptoms.Update(&symptom); err != nil {
		return fmt.Errorf("%w: %v", ErrArchiveSymptomFailed, err)
	}
	return nil
}

func (service *SymptomService) RestoreSymptomForUser(userID uint, symptomID uint) error {
	symptom, err := service.symptoms.FindByIDForUser(symptomID, userID)
	if err != nil {
		return fmt.Errorf("%w: %v", ErrSymptomNotFound, err)
	}
	if symptom.IsBuiltin {
		return ErrBuiltinSymptomShowForbidden
	}
	if symptom.ArchivedAt == nil {
		return nil
	}
	if err := service.ensureSymptomNameAvailable(userID, symptom.ID, symptom.Name); err != nil {
		if isSymptomValidationError(err) {
			return err
		}
		return fmt.Errorf("%w: %v", ErrRestoreSymptomFailed, err)
	}

	symptom.ArchivedAt = nil
	if err := service.symptoms.Update(&symptom); err != nil {
		return fmt.Errorf("%w: %v", ErrRestoreSymptomFailed, err)
	}
	return nil
}

func isSymptomValidationError(err error) bool {
	return errors.Is(err, ErrSymptomNameRequired) ||
		errors.Is(err, ErrSymptomNameTooLong) ||
		errors.Is(err, ErrSymptomNameInvalidCharacters) ||
		errors.Is(err, ErrInvalidSymptomColor) ||
		errors.Is(err, ErrSymptomNameAlreadyExists) ||
		errors.Is(err, ErrBuiltinSymptomEditForbidden)
}

func (service *SymptomService) FindSymptomForUser(symptomID uint, userID uint) (models.SymptomType, error) {
	return service.symptoms.FindByIDForUser(symptomID, userID)
}

func (service *SymptomService) CalculateFrequencies(userID uint, logs []models.DailyLog) ([]SymptomFrequency, error) {
	if len(logs) == 0 {
		return []SymptomFrequency{}, nil
	}
	totalDays := len(logs)

	counts := make(map[uint]int)
	for _, logEntry := range logs {
		for _, id := range logEntry.SymptomIDs {
			counts[id]++
		}
	}
	if len(counts) == 0 {
		return []SymptomFrequency{}, nil
	}

	symptoms, err := service.FetchSymptoms(userID)
	if err != nil {
		return nil, err
	}

	symptomByID := make(map[uint]models.SymptomType, len(symptoms))
	for _, symptom := range symptoms {
		symptomByID[symptom.ID] = symptom
	}

	result := make([]SymptomFrequency, 0, len(counts))
	for id, count := range counts {
		if symptom, ok := symptomByID[id]; ok {
			result = append(result, SymptomFrequency{
				Name:      symptom.Name,
				Icon:      symptom.Icon,
				Count:     count,
				TotalDays: totalDays,
			})
		}
	}

	sort.Slice(result, func(i, j int) bool {
		if result[i].Count == result[j].Count {
			return result[i].Name < result[j].Name
		}
		return result[i].Count > result[j].Count
	})

	return result, nil
}

func (service *SymptomService) SeedBuiltinSymptoms(userID uint) error {
	count, err := service.symptoms.CountBuiltinByUser(userID)
	if err != nil {
		return err
	}
	if count > 0 {
		return nil
	}
	return service.symptoms.CreateBatch(BuiltinSymptomRecordsForUser(userID))
}

func (service *SymptomService) EnsureBuiltinSymptoms(userID uint) error {
	existing, err := service.symptoms.ListByUser(userID)
	if err != nil {
		return err
	}
	existingByName := make(map[string]struct{}, len(existing))
	for _, symptom := range existing {
		key := normalizeSymptomNameKey(symptom.Name)
		if key != "" {
			existingByName[key] = struct{}{}
		}
	}

	missing := MissingBuiltinSymptomsForUser(userID, existingByName)
	if len(missing) == 0 {
		return nil
	}
	return service.symptoms.CreateBatch(missing)
}

func (service *SymptomService) FetchSymptoms(userID uint) ([]models.SymptomType, error) {
	if err := service.EnsureBuiltinSymptoms(userID); err != nil {
		return nil, err
	}
	symptoms, err := service.symptoms.ListByUser(userID)
	if err != nil {
		return nil, err
	}
	SortSymptomsByBuiltinAndName(symptoms)
	return symptoms, nil
}

func (service *SymptomService) FetchPickerSymptoms(userID uint, selectedIDs []uint) ([]models.SymptomType, error) {
	symptoms, err := service.FetchSymptoms(userID)
	if err != nil {
		return nil, err
	}

	selected := make(map[uint]struct{}, len(selectedIDs))
	for _, id := range selectedIDs {
		selected[id] = struct{}{}
	}

	filtered := make([]models.SymptomType, 0, len(symptoms))
	for _, symptom := range symptoms {
		_, isSelected := selected[symptom.ID]
		if shouldHideSymptomFromEntryPicker(symptom) && !isSelected {
			continue
		}
		if symptom.IsActive() {
			filtered = append(filtered, symptom)
			continue
		}
		if isSelected {
			filtered = append(filtered, symptom)
		}
	}
	return filtered, nil
}

func (service *SymptomService) ValidateSymptomIDs(userID uint, ids []uint) ([]uint, error) {
	if len(ids) == 0 {
		return []uint{}, nil
	}

	unique := make(map[uint]struct{}, len(ids))
	for _, id := range ids {
		unique[id] = struct{}{}
	}
	filtered := make([]uint, 0, len(unique))
	for id := range unique {
		filtered = append(filtered, id)
	}

	matched, err := service.symptoms.CountByUserAndIDs(userID, filtered)
	if err != nil {
		return nil, err
	}
	if int(matched) != len(filtered) {
		return nil, ErrInvalidSymptomID
	}
	sort.Slice(filtered, func(i, j int) bool { return filtered[i] < filtered[j] })
	return filtered, nil
}

func (service *SymptomService) normalizeCustomSymptomInput(userID uint, excludeID uint, name string, icon string, color string, fallbackColor string) (models.SymptomType, error) {
	normalizedName, err := normalizeSymptomNameInput(name)
	if err != nil {
		return models.SymptomType{}, err
	}
	normalizedColor, err := resolveSymptomColorInput(color, fallbackColor)
	if err != nil {
		return models.SymptomType{}, err
	}
	normalizedIcon := normalizeSymptomIconInput(icon)

	if err := service.ensureSymptomNameAvailable(userID, excludeID, normalizedName); err != nil {
		return models.SymptomType{}, err
	}

	return models.SymptomType{
		UserID:    userID,
		Name:      normalizedName,
		Icon:      normalizedIcon,
		Color:     normalizedColor,
		IsBuiltin: false,
	}, nil
}

func (service *SymptomService) ensureSymptomNameAvailable(userID uint, excludeID uint, name string) error {
	if _, reserved := service.reservedNameKeys[normalizeSymptomNameKey(name)]; reserved {
		return ErrSymptomNameAlreadyExists
	}

	symptoms, err := service.symptoms.ListByUser(userID)
	if err != nil {
		return err
	}

	targetKey := normalizeSymptomNameKey(name)
	for _, symptom := range symptoms {
		if excludeID != 0 && symptom.ID == excludeID {
			continue
		}
		if normalizeSymptomNameKey(symptom.Name) == targetKey {
			return ErrSymptomNameAlreadyExists
		}
	}

	return nil
}

func builtinSymptomReservedNameKeys(extra []string) map[string]struct{} {
	keys := make(map[string]struct{})
	for _, symptom := range models.DefaultBuiltinSymptoms() {
		keys[normalizeSymptomNameKey(symptom.Name)] = struct{}{}
	}
	for _, name := range extra {
		key := normalizeSymptomNameKey(name)
		if key == "" {
			continue
		}
		keys[key] = struct{}{}
	}
	return keys
}

func shouldHideSymptomFromEntryPicker(symptom models.SymptomType) bool {
	if !symptom.IsBuiltin {
		return false
	}
	_, hidden := legacyEntryPickerHiddenSymptoms[normalizeSymptomNameKey(symptom.Name)]
	return hidden
}

func BuiltinSymptomRecordsForUser(userID uint) []models.SymptomType {
	builtin := models.DefaultBuiltinSymptoms()
	records := make([]models.SymptomType, 0, len(builtin))
	for _, symptom := range builtin {
		records = append(records, models.SymptomType{
			UserID:    userID,
			Name:      symptom.Name,
			Icon:      symptom.Icon,
			Color:     symptom.Color,
			IsBuiltin: true,
		})
	}
	return records
}

func MissingBuiltinSymptomsForUser(userID uint, existingByName map[string]struct{}) []models.SymptomType {
	missing := make([]models.SymptomType, 0)
	for _, symptom := range models.DefaultBuiltinSymptoms() {
		key := normalizeSymptomNameKey(symptom.Name)
		if _, ok := existingByName[key]; ok {
			continue
		}
		missing = append(missing, models.SymptomType{
			UserID:    userID,
			Name:      symptom.Name,
			Icon:      symptom.Icon,
			Color:     symptom.Color,
			IsBuiltin: true,
		})
	}
	return missing
}

func SortSymptomsByBuiltinAndName(symptoms []models.SymptomType) {
	builtinOrder := builtinSymptomOrderMap()

	sort.Slice(symptoms, func(i, j int) bool {
		left := symptoms[i]
		right := symptoms[j]
		if left.IsBuiltin != right.IsBuiltin {
			return left.IsBuiltin
		}
		if left.IsBuiltin && right.IsBuiltin {
			leftIndex, leftHas := builtinOrder[normalizeSymptomNameKey(left.Name)]
			rightIndex, rightHas := builtinOrder[normalizeSymptomNameKey(right.Name)]
			switch {
			case leftHas && rightHas && leftIndex != rightIndex:
				return leftIndex < rightIndex
			case leftHas != rightHas:
				return leftHas
			}
		}
		return normalizeSymptomNameKey(left.Name) < normalizeSymptomNameKey(right.Name)
	})
}

func builtinSymptomOrderMap() map[string]int {
	order := make(map[string]int)
	for index, symptom := range models.DefaultBuiltinSymptoms() {
		order[normalizeSymptomNameKey(symptom.Name)] = index
	}
	return order
}
