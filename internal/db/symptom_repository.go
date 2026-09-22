package db

import (
	"context"
	"errors"

	"github.com/ovumcy/ovumcy-web/internal/models"
	"gorm.io/gorm"
)

// ErrSymptomOwnerRequired is returned by every SymptomRepository write that
// would otherwise accept a symptom naming no owner: Update, Create and
// CreateBatch, and by requireSymptomOwners for the seed rows written inside
// UserRepository.CreateUserWithSymptoms.
//
// The two writes refuse a zero UserID for different reasons, and both are
// invalid input rather than a wildcard. Update scopes its write by
// symptom.UserID in the query itself (see its doc comment), where matching
// Where("user_id = ?", 0) would ordinarily just match zero rows and return
// nil — a silent no-op indistinguishable from success. Create and CreateBatch
// would instead succeed, and leave behind a row that no owner-scoped read and
// no account erasure can ever address again.
var ErrSymptomOwnerRequired = errors.New("symptom owner is required")

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

// classifySymptomWriteError names the one unique constraint symptom_types
// carries besides its primary key: migration 037's per-owner index on
// (user_id, lower(name)). Every symptom write goes through it, so the service
// sees the database's refusal of a duplicate name as a unique-constraint error
// it can map onto its own "name already exists", rather than as a driver
// message it would report as an internal failure.
func classifySymptomWriteError(err error) error {
	return classifyUniqueConstraintError(err, "symptom_types.user_id_name")
}

// Create inserts a new custom or builtin symptom row. A zero symptom.UserID is
// refused rather than written: an owner-scoped read or the account-erasure
// sweep can never address a row with no owner, so it would sit unreachable
// forever instead of failing loudly at write time.
func (repo *SymptomRepository) Create(ctx context.Context, symptom *models.SymptomType) error {
	if symptom.UserID == 0 {
		return ErrSymptomOwnerRequired
	}
	return classifySymptomWriteError(repo.database.WithContext(ctx).Create(symptom).Error)
}

// requireSymptomOwners refuses a batch in which any row names no owner, for
// the reason given on ErrSymptomOwnerRequired. It is the shared check for
// every multi-row symptom insert in this package: CreateBatch below, and the
// seed rows UserRepository.CreateUserWithSymptoms writes through its own
// transaction handle rather than through this repository. A second insert
// site that skipped it would write exactly the unreachable rows the first one
// refuses, so both call this rather than spelling the loop out twice.
func requireSymptomOwners(symptoms []models.SymptomType) error {
	for _, symptom := range symptoms {
		if symptom.UserID == 0 {
			return ErrSymptomOwnerRequired
		}
	}
	return nil
}

// CreateBatch refuses the whole batch when any symptom carries a zero
// UserID, for the same reason as Create.
func (repo *SymptomRepository) CreateBatch(ctx context.Context, symptoms []models.SymptomType) error {
	if len(symptoms) == 0 {
		return nil
	}
	if err := requireSymptomOwners(symptoms); err != nil {
		return err
	}
	return classifySymptomWriteError(repo.database.WithContext(ctx).Create(&symptoms).Error)
}

func (repo *SymptomRepository) FindByIDForUser(ctx context.Context, symptomID uint, userID uint) (models.SymptomType, error) {
	symptom := models.SymptomType{}
	if err := repo.database.WithContext(ctx).Where("id = ? AND user_id = ?", symptomID, userID).First(&symptom).Error; err != nil {
		return models.SymptomType{}, err
	}
	return symptom, nil
}

// Update persists a full update of an already-owned custom symptom. Like
// DailyLogRepository.Save it is scoped by user_id and uses
// Model(...).Select("*").Updates (UPDATE-only, no insert fallback) rather than
// gorm.Save: a mutated symptom.UserID can never reassign or overwrite another
// owner's row (defense-in-depth for the household multi-owner boundary), and
// Select("*") still writes zero-value fields so RestoreSymptomForUser's
// ArchivedAt=nil clears the column. Every caller sources symptom from
// FindByIDForUser first, so a legitimate write always matches its row.
func (repo *SymptomRepository) Update(ctx context.Context, symptom *models.SymptomType) error {
	if symptom.UserID == 0 {
		return ErrSymptomOwnerRequired
	}
	return classifySymptomWriteError(repo.database.WithContext(ctx).
		Model(symptom).
		Where("user_id = ?", symptom.UserID).
		Select("*").
		Updates(symptom).Error)
}
