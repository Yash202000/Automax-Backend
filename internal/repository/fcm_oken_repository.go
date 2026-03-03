package repository

import (
	"errors"

	"github.com/automax/backend/internal/models"
	"github.com/google/uuid"
	"gorm.io/gorm"
)

type DeviceTokenRepository struct {
	db *gorm.DB
}

func NewDeviceTokenRepository(db *gorm.DB) *DeviceTokenRepository {
	return &DeviceTokenRepository{db: db}
}

func (r *DeviceTokenRepository) GetByToken1(token string) (*models.DeviceToken, error) {
	var device models.DeviceToken
	err := r.db.Where("device_token = ?", token).First(&device).Error
	if err != nil {
		return nil, err
	}
	return &device, nil
}

func (r *DeviceTokenRepository) GetByToken(token string) (*models.DeviceToken, error) {
	var device models.DeviceToken

	err := r.db.Where("device_token = ?", token).First(&device).Error
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, nil
		}
		return nil, err
	}

	return &device, nil
}

func (r *DeviceTokenRepository) Create(device *models.DeviceToken) error {
	return r.db.Create(device).Error
}

func (r *DeviceTokenRepository) Update(device *models.DeviceToken) error {
	return r.db.Save(device).Error
}

func (r *DeviceTokenRepository) GetByUserID(userID uuid.UUID) ([]models.DeviceToken, error) {

	var tokens []models.DeviceToken

	err := r.db.
		Where("user_id = ?", userID).
		Find(&tokens).
		Error

	return tokens, err
}

func (r *DeviceTokenRepository) DeleteByUserAndToken(userID uuid.UUID, token string) error {
	return r.db.
		Where("user_id = ? AND device_token = ?", userID, token).
		Delete(&models.DeviceToken{}).
		Error
}
