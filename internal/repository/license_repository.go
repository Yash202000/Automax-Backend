package repository

import (
	"context"

	"github.com/automax/backend/internal/models"
	"github.com/google/uuid"
	"gorm.io/gorm"
)

type LicenseRepository interface {
	GetActive(ctx context.Context) (*models.License, error)
	Upsert(ctx context.Context, license *models.License) error
	Delete(ctx context.Context, id uuid.UUID) error
	CountActiveUsers(ctx context.Context) (int64, error)
}

type licenseRepository struct {
	db *gorm.DB
}

func NewLicenseRepository(db *gorm.DB) LicenseRepository {
	return &licenseRepository{db: db}
}

// GetActive returns the single active (non-deleted) license, or gorm.ErrRecordNotFound.
func (r *licenseRepository) GetActive(ctx context.Context) (*models.License, error) {
	var license models.License
	err := r.db.WithContext(ctx).First(&license).Error
	if err != nil {
		return nil, err
	}
	return &license, nil
}

// Upsert creates or replaces the license. Since this is a single-row table,
// we delete any existing row first, then create the new one.
func (r *licenseRepository) Upsert(ctx context.Context, license *models.License) error {
	return r.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		// Hard-delete all existing license rows (including soft-deleted)
		if err := tx.Unscoped().Where("1 = 1").Delete(&models.License{}).Error; err != nil {
			return err
		}
		return tx.Create(license).Error
	})
}

// Delete soft-deletes the license by ID.
func (r *licenseRepository) Delete(ctx context.Context, id uuid.UUID) error {
	return r.db.WithContext(ctx).Delete(&models.License{}, "id = ?", id).Error
}

// CountActiveUsers returns the count of active, non-deleted users.
func (r *licenseRepository) CountActiveUsers(ctx context.Context) (int64, error) {
	var count int64
	err := r.db.WithContext(ctx).
		Model(&models.User{}).
		Where("is_active = ? AND deleted_at IS NULL", true).
		Count(&count).Error
	return count, err
}
