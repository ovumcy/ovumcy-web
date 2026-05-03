package db

import (
	"errors"
	"time"

	"github.com/ovumcy/ovumcy-web/internal/models"
	"gorm.io/gorm"
)

type UserRepository struct {
	database *gorm.DB
}

func NewUserRepository(database *gorm.DB) *UserRepository {
	return &UserRepository{database: database}
}

func (repo *UserRepository) CountUsers() (int64, error) {
	var count int64
	if err := repo.database.Model(&models.User{}).Count(&count).Error; err != nil {
		return 0, err
	}
	return count, nil
}

func (repo *UserRepository) ListOperatorUserSummaries() ([]models.OperatorUserSummary, error) {
	summaries := make([]models.OperatorUserSummary, 0)
	if err := repo.database.
		Model(&models.User{}).
		Select("id", "display_name", "email", "role", "onboarding_completed", "created_at").
		Order("created_at ASC").
		Order("id ASC").
		Find(&summaries).Error; err != nil {
		return nil, err
	}
	return summaries, nil
}

func (repo *UserRepository) FindByID(userID uint) (models.User, error) {
	var user models.User
	if err := repo.database.First(&user, userID).Error; err != nil {
		return models.User{}, err
	}
	return user, nil
}

func (repo *UserRepository) FindByNormalizedEmail(email string) (models.User, error) {
	var user models.User
	if err := repo.database.Where("lower(trim(email)) = ?", email).First(&user).Error; err != nil {
		return models.User{}, err
	}
	return user, nil
}

func (repo *UserRepository) FindByNormalizedEmailOptional(email string) (models.User, bool, error) {
	var user models.User
	if err := repo.database.Where("lower(trim(email)) = ?", email).First(&user).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return models.User{}, false, nil
		}
		return models.User{}, false, err
	}
	return user, true, nil
}

func (repo *UserRepository) ExistsByNormalizedEmail(email string) (bool, error) {
	var matched int64
	if err := repo.database.Model(&models.User{}).
		Where("lower(trim(email)) = ?", email).
		Count(&matched).Error; err != nil {
		return false, err
	}
	return matched > 0, nil
}

func (repo *UserRepository) Create(user *models.User) error {
	err := repo.database.Create(user).Error
	return classifyUserCreateError(err)
}

func (repo *UserRepository) CreateUserWithSymptoms(user *models.User, symptoms []models.SymptomType) error {
	return repo.database.Transaction(func(tx *gorm.DB) error {
		if err := classifyUserCreateError(tx.Create(user).Error); err != nil {
			return err
		}
		if len(symptoms) == 0 {
			return nil
		}

		prepared := make([]models.SymptomType, len(symptoms))
		copy(prepared, symptoms)
		for index := range prepared {
			prepared[index].UserID = user.ID
		}

		if err := tx.Create(&prepared).Error; err != nil {
			return &SymptomSeedError{Err: err}
		}
		return nil
	})
}

func (repo *UserRepository) Save(user *models.User) error {
	return repo.database.Save(user).Error
}

func (repo *UserRepository) UpdateDisplayName(userID uint, displayName string) error {
	return repo.database.Model(&models.User{}).Where("id = ?", userID).Update("display_name", displayName).Error
}

func (repo *UserRepository) UpdateRecoveryCodeHash(userID uint, recoveryHash string) error {
	return repo.database.Model(&models.User{}).Where("id = ?", userID).Update("recovery_code_hash", recoveryHash).Error
}

func (repo *UserRepository) UpdatePassword(userID uint, passwordHash string, mustChangePassword bool) error {
	return repo.database.Model(&models.User{}).Where("id = ?", userID).Updates(map[string]any{
		"password_hash":        passwordHash,
		"must_change_password": mustChangePassword,
		"local_auth_enabled":   true,
	}).Error
}

func (repo *UserRepository) UpdatePasswordAndRevokeSessions(userID uint, passwordHash string, mustChangePassword bool) error {
	return repo.database.Model(&models.User{}).Where("id = ?", userID).Updates(map[string]any{
		"password_hash":        passwordHash,
		"must_change_password": mustChangePassword,
		"local_auth_enabled":   true,
		"auth_session_version": gorm.Expr("auth_session_version + 1"),
	}).Error
}

func (repo *UserRepository) UpdatePasswordRecoveryCodeAndRevokeSessions(userID uint, passwordHash string, recoveryHash string, mustChangePassword bool) error {
	return repo.database.Model(&models.User{}).Where("id = ?", userID).Updates(map[string]any{
		"password_hash":        passwordHash,
		"recovery_code_hash":   recoveryHash,
		"must_change_password": mustChangePassword,
		"local_auth_enabled":   true,
		"auth_session_version": gorm.Expr("auth_session_version + 1"),
	}).Error
}

func (repo *UserRepository) BumpAuthSessionVersion(userID uint) error {
	return repo.database.Model(&models.User{}).Where("id = ?", userID).UpdateColumn("auth_session_version", gorm.Expr("auth_session_version + 1")).Error
}

func (repo *UserRepository) UpdateTOTPFields(userID uint, encryptedSecret string, enabled bool) error {
	return repo.database.Model(&models.User{}).Where("id = ?", userID).Updates(map[string]any{
		"totp_secret":  encryptedSecret,
		"totp_enabled": enabled,
	}).Error
}

func (repo *UserRepository) UpdateByID(userID uint, updates map[string]any) error {
	return repo.database.Model(&models.User{}).Where("id = ?", userID).Updates(updates).Error
}

func (repo *UserRepository) LoadSettingsByID(userID uint) (models.User, error) {
	var user models.User
	if err := repo.database.
		Select(
			"cycle_length",
			"period_length",
			"luteal_phase",
			"auto_period_fill",
			"local_auth_enabled",
			"irregular_cycle",
			"track_bbt",
			"temperature_unit",
			"track_cervical_mucus",
			"hide_sex_chip",
			"hide_cycle_factors",
			"hide_notes_field",
			"shown_period_tip",
			"age_group",
			"usage_goal",
			"unpredictable_cycle",
			"long_period_warning_cycle_start",
			"last_period_start",
		).
		First(&user, userID).Error; err != nil {
		return models.User{}, err
	}
	return user, nil
}

func (repo *UserRepository) SaveOnboardingStep1(userID uint, start time.Time) error {
	return repo.database.Model(&models.User{}).Where("id = ?", userID).Updates(map[string]any{
		"last_period_start": start,
	}).Error
}

func (repo *UserRepository) SaveOnboardingStep2(userID uint, cycleLength int, periodLength int, autoPeriodFill bool, irregularCycle bool, ageGroup string, usageGoal string) error {
	return repo.database.Model(&models.User{}).Where("id = ?", userID).Updates(map[string]any{
		"cycle_length":     cycleLength,
		"period_length":    periodLength,
		"luteal_phase":     14,
		"auto_period_fill": autoPeriodFill,
		"irregular_cycle":  irregularCycle,
		"age_group":        ageGroup,
		"usage_goal":       usageGoal,
	}).Error
}

func (repo *UserRepository) ClearAllDataAndResetSettings(userID uint) error {
	return repo.database.Transaction(func(tx *gorm.DB) error {
		if err := tx.Where("user_id = ?", userID).Delete(&models.DailyLog{}).Error; err != nil {
			return err
		}
		if err := tx.Where("user_id = ? AND is_builtin = ?", userID, false).Delete(&models.SymptomType{}).Error; err != nil {
			return err
		}
		return tx.Model(&models.User{}).Where("id = ?", userID).Updates(map[string]any{
			"cycle_length":                    models.DefaultCycleLength,
			"period_length":                   models.DefaultPeriodLength,
			"luteal_phase":                    14,
			"auto_period_fill":                true,
			"irregular_cycle":                 false,
			"track_bbt":                       false,
			"temperature_unit":                "c",
			"track_cervical_mucus":            false,
			"hide_sex_chip":                   false,
			"hide_cycle_factors":              false,
			"hide_notes_field":                false,
			"shown_period_tip":                false,
			"age_group":                       models.AgeGroupUnknown,
			"usage_goal":                      models.UsageGoalHealth,
			"unpredictable_cycle":             false,
			"long_period_warning_cycle_start": nil,
			"last_period_start":               nil,
		}).Error
	})
}

func (repo *UserRepository) DeleteAccountAndRelatedData(userID uint) error {
	return repo.database.Transaction(func(tx *gorm.DB) error {
		if err := tx.Where("user_id = ?", userID).Delete(&models.DailyLog{}).Error; err != nil {
			return err
		}
		if err := tx.Where("user_id = ?", userID).Delete(&models.SymptomType{}).Error; err != nil {
			return err
		}
		return tx.Delete(&models.User{}, userID).Error
	})
}

func (repo *UserRepository) CompleteOnboarding(userID uint, startDay time.Time, periodLength int, autoPeriodFill bool) error {
	if periodLength <= 0 {
		return errors.New("invalid period length")
	}
	endDay := startDay.AddDate(0, 0, periodLength-1)
	if endDay.Before(startDay) {
		return errors.New("invalid onboarding range")
	}

	return repo.database.Transaction(func(tx *gorm.DB) error {
		if autoPeriodFill {
			for cursor := startDay; !cursor.After(endDay); cursor = cursor.AddDate(0, 0, 1) {
				dayStart := cursor
				dayEnd := dayStart.AddDate(0, 0, 1)

				var entry models.DailyLog
				result := tx.
					Where("user_id = ? AND date >= ? AND date < ?", userID, dayStart, dayEnd).
					Order("date DESC, id DESC").
					First(&entry)
				if errors.Is(result.Error, gorm.ErrRecordNotFound) {
					entry = models.DailyLog{
						UserID:        userID,
						Date:          dayStart,
						IsPeriod:      true,
						Flow:          models.FlowNone,
						SexActivity:   models.SexActivityNone,
						CervicalMucus: models.CervicalMucusNone,
						SymptomIDs:    []uint{},
					}
					if err := tx.Create(&entry).Error; err != nil {
						return err
					}
					continue
				}
				if result.Error != nil {
					return result.Error
				}

				if err := tx.Model(&entry).Updates(map[string]any{
					"is_period": true,
					"flow":      models.FlowNone,
				}).Error; err != nil {
					return err
				}
			}
		}

		return tx.Model(&models.User{}).Where("id = ?", userID).Updates(map[string]any{
			"last_period_start":    startDay,
			"onboarding_completed": true,
		}).Error
	})
}
