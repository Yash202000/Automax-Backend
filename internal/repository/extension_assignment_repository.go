package repository

import (
	"context"
	"errors"

	"github.com/automax/backend/internal/models"
	"github.com/google/uuid"
	"gorm.io/gorm"
)

// ExtensionAssignmentRepository owns the CURRENT-state table (extension_assignments)
// — one active row per extension / per user — plus the append-only history log
// (extension_assignment_history). It is the sole source of truth for which user
// holds which PBX extension; the users table no longer stores extensions.
type ExtensionAssignmentRepository interface {
	// Current-state
	GetByExtension(ctx context.Context, extension string) (*models.ExtensionAssignment, error)
	GetByUserID(ctx context.Context, userID uuid.UUID) (*models.ExtensionAssignment, error)
	ListActive(ctx context.Context) ([]models.ExtensionAssignment, error)
	AssignTx(tx *gorm.DB, row *models.ExtensionAssignment) error
	DeleteByExtensionTx(tx *gorm.DB, extension string) error
	DeleteByUserTx(tx *gorm.DB, userID uuid.UUID) error

	// History
	CreateHistory(ctx context.Context, row *models.ExtensionAssignmentHistory) error
	CreateHistoryTx(tx *gorm.DB, row *models.ExtensionAssignmentHistory) error
	HistoryByExtension(ctx context.Context, extension string, limit int) ([]models.ExtensionAssignmentHistory, error)
	HistoryByUser(ctx context.Context, userID uuid.UUID, limit int) ([]models.ExtensionAssignmentHistory, error)
}

type extensionAssignmentRepository struct {
	db *gorm.DB
}

func NewExtensionAssignmentRepository(db *gorm.DB) ExtensionAssignmentRepository {
	return &extensionAssignmentRepository{db: db}
}

// --- Current-state ---

func (r *extensionAssignmentRepository) GetByExtension(ctx context.Context, extension string) (*models.ExtensionAssignment, error) {
	var row models.ExtensionAssignment
	err := r.db.WithContext(ctx).Preload("User").Where("extension = ?", extension).First(&row).Error
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, nil
		}
		return nil, err
	}
	return &row, nil
}

func (r *extensionAssignmentRepository) GetByUserID(ctx context.Context, userID uuid.UUID) (*models.ExtensionAssignment, error) {
	var row models.ExtensionAssignment
	err := r.db.WithContext(ctx).Preload("User").Where("user_id = ?", userID).First(&row).Error
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, nil
		}
		return nil, err
	}
	return &row, nil
}

func (r *extensionAssignmentRepository) ListActive(ctx context.Context) ([]models.ExtensionAssignment, error) {
	var rows []models.ExtensionAssignment
	err := r.db.WithContext(ctx).Preload("User").Find(&rows).Error
	return rows, err
}

func (r *extensionAssignmentRepository) AssignTx(tx *gorm.DB, row *models.ExtensionAssignment) error {
	return tx.Create(row).Error
}

func (r *extensionAssignmentRepository) DeleteByExtensionTx(tx *gorm.DB, extension string) error {
	return tx.Where("extension = ?", extension).Delete(&models.ExtensionAssignment{}).Error
}

func (r *extensionAssignmentRepository) DeleteByUserTx(tx *gorm.DB, userID uuid.UUID) error {
	return tx.Where("user_id = ?", userID).Delete(&models.ExtensionAssignment{}).Error
}

// --- History ---

func (r *extensionAssignmentRepository) CreateHistory(ctx context.Context, row *models.ExtensionAssignmentHistory) error {
	return r.db.WithContext(ctx).Create(row).Error
}

func (r *extensionAssignmentRepository) CreateHistoryTx(tx *gorm.DB, row *models.ExtensionAssignmentHistory) error {
	return tx.Create(row).Error
}

func (r *extensionAssignmentRepository) HistoryByExtension(ctx context.Context, extension string, limit int) ([]models.ExtensionAssignmentHistory, error) {
	var rows []models.ExtensionAssignmentHistory
	q := r.db.WithContext(ctx).
		Preload("User").
		Preload("PreviousUser").
		Preload("AssignedByUser").
		Where("extension = ?", extension).
		Order("created_at DESC")
	if limit > 0 {
		q = q.Limit(limit)
	}
	err := q.Find(&rows).Error
	return rows, err
}

func (r *extensionAssignmentRepository) HistoryByUser(ctx context.Context, userID uuid.UUID, limit int) ([]models.ExtensionAssignmentHistory, error) {
	var rows []models.ExtensionAssignmentHistory
	q := r.db.WithContext(ctx).
		Preload("User").
		Preload("PreviousUser").
		Preload("AssignedByUser").
		Where("user_id = ? OR previous_user_id = ?", userID, userID).
		Order("created_at DESC")
	if limit > 0 {
		q = q.Limit(limit)
	}
	err := q.Find(&rows).Error
	return rows, err
}
