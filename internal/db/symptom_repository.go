package db

import (
	"context"

	"github.com/ovumcy/ovumcy-web/internal/models"
	"gorm.io/gorm"
)

type SymptomRepository struct {
	database *gorm.DB
}

func NewSymptomRepository(database *gorm.DB) *SymptomRepository {
	return &SymptomRepository{database: database}
}

func (repo *SymptomRepository) CountBuiltinByUser(ctx context.Context, userID uint) (int64, error) {
	var count int64
	if err := repo.database.WithContext(ctx).Model(&models.SymptomType{}).
		Where("user_id = ? AND is_builtin = ?", userID, true).
		Count(&count).Error; err != nil {
		return 0, err
	}
	return count, nil
}

func (repo *SymptomRepository) CountByUserAndIDs(ctx context.Context, userID uint, ids []uint) (int64, error) {
	var count int64
	if err := repo.database.WithContext(ctx).Model(&models.SymptomType{}).
		Where("user_id = ? AND id IN ?", userID, ids).
		Count(&count).Error; err != nil {
		return 0, err
	}
	return count, nil
}

func (repo *SymptomRepository) ListByUser(ctx context.Context, userID uint) ([]models.SymptomType, error) {
	symptoms := make([]models.SymptomType, 0)
	if err := repo.database.WithContext(ctx).Where("user_id = ?", userID).Find(&symptoms).Error; err != nil {
		return nil, err
	}
	return symptoms, nil
}

func (repo *SymptomRepository) Create(ctx context.Context, symptom *models.SymptomType) error {
	return repo.database.WithContext(ctx).Create(symptom).Error
}

func (repo *SymptomRepository) CreateBatch(ctx context.Context, symptoms []models.SymptomType) error {
	if len(symptoms) == 0 {
		return nil
	}
	return repo.database.WithContext(ctx).Create(&symptoms).Error
}

func (repo *SymptomRepository) FindByIDForUser(ctx context.Context, symptomID uint, userID uint) (models.SymptomType, error) {
	symptom := models.SymptomType{}
	if err := repo.database.WithContext(ctx).Where("id = ? AND user_id = ?", symptomID, userID).First(&symptom).Error; err != nil {
		return models.SymptomType{}, err
	}
	return symptom, nil
}

func (repo *SymptomRepository) Update(ctx context.Context, symptom *models.SymptomType) error {
	return repo.database.WithContext(ctx).Save(symptom).Error
}
