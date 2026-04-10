package repository

import (
	"context"
	"time"

	"github.com/automax/backend/internal/models"
	"github.com/google/uuid"
	"gorm.io/gorm"
)

type EscalationGroupRepository interface {
	Create(ctx context.Context, group *models.EscalationGroup) error
	Update(ctx context.Context, group *models.EscalationGroup) error
	Delete(ctx context.Context, id uuid.UUID) error
	FindByID(ctx context.Context, id uuid.UUID) (*models.EscalationGroup, error)
	List(ctx context.Context) ([]models.EscalationGroup, error)

	// FindAllActive returns all active groups with targets, users, and classifications preloaded.
	FindAllActive(ctx context.Context) ([]models.EscalationGroup, error)

	// UpdateLastNotifiedAt records when a group last dispatched its notification batch.
	UpdateLastNotifiedAt(ctx context.Context, id uuid.UUID, t time.Time) error

	// SetUsers replaces the legacy user association for the given group.
	SetUsers(ctx context.Context, groupID uuid.UUID, userIDs []uuid.UUID) error

	// SetTargets atomically replaces all EscalationGroupTargets for a group.
	SetTargets(ctx context.Context, groupID uuid.UUID, targets []models.EscalationGroupTarget) error

	// SetClassifications replaces the many2many classifications for a group.
	SetClassifications(ctx context.Context, groupID uuid.UUID, classificationIDs []uuid.UUID) error
}

type escalationGroupRepository struct {
	db *gorm.DB
}

func NewEscalationGroupRepository(db *gorm.DB) EscalationGroupRepository {
	return &escalationGroupRepository{db: db}
}

func (r *escalationGroupRepository) Create(ctx context.Context, group *models.EscalationGroup) error {
	return r.db.WithContext(ctx).Create(group).Error
}

func (r *escalationGroupRepository) Update(ctx context.Context, group *models.EscalationGroup) error {
	return r.db.WithContext(ctx).Save(group).Error
}

func (r *escalationGroupRepository) Delete(ctx context.Context, id uuid.UUID) error {
	return r.db.WithContext(ctx).Delete(&models.EscalationGroup{}, "id = ?", id).Error
}

func (r *escalationGroupRepository) FindByID(ctx context.Context, id uuid.UUID) (*models.EscalationGroup, error) {
	var group models.EscalationGroup
	err := r.db.WithContext(ctx).
		Preload("Classification").
		Preload("Classifications").
		Preload("Targets").
		Preload("Targets.Department").
		Preload("Targets.Role").
		Preload("Users").
		First(&group, "id = ?", id).Error
	if err != nil {
		return nil, err
	}
	return &group, nil
}

func (r *escalationGroupRepository) List(ctx context.Context) ([]models.EscalationGroup, error) {
	var groups []models.EscalationGroup
	err := r.db.WithContext(ctx).
		Preload("Classification").
		Preload("Classifications").
		Preload("Targets").
		Preload("Targets.Department").
		Preload("Targets.Role").
		Preload("Users").
		Order("created_at DESC").
		Find(&groups).Error
	return groups, err
}

func (r *escalationGroupRepository) FindAllActive(ctx context.Context) ([]models.EscalationGroup, error) {
	var groups []models.EscalationGroup
	err := r.db.WithContext(ctx).
		Where("is_active = ?", true).
		Preload("Classification").
		Preload("Classifications").
		Preload("Targets").
		Preload("Targets.Department").
		Preload("Targets.Role").
		Preload("Users").
		Find(&groups).Error
	return groups, err
}

// SetClassifications replaces the many2many classification associations for a group.
func (r *escalationGroupRepository) SetClassifications(ctx context.Context, groupID uuid.UUID, classificationIDs []uuid.UUID) error {
	classifications := make([]models.Classification, len(classificationIDs))
	for i, id := range classificationIDs {
		classifications[i] = models.Classification{ID: id}
	}
	group := models.EscalationGroup{ID: groupID}
	return r.db.WithContext(ctx).Model(&group).Association("Classifications").Replace(classifications)
}

func (r *escalationGroupRepository) UpdateLastNotifiedAt(ctx context.Context, id uuid.UUID, t time.Time) error {
	return r.db.WithContext(ctx).
		Model(&models.EscalationGroup{}).
		Where("id = ?", id).
		Update("last_notified_at", t).Error
}

func (r *escalationGroupRepository) SetUsers(ctx context.Context, groupID uuid.UUID, userIDs []uuid.UUID) error {
	users := make([]models.User, len(userIDs))
	for i, id := range userIDs {
		users[i] = models.User{ID: id}
	}
	group := models.EscalationGroup{ID: groupID}
	return r.db.WithContext(ctx).Model(&group).Association("Users").Replace(users)
}

// SetTargets atomically replaces all targets for a group.
func (r *escalationGroupRepository) SetTargets(ctx context.Context, groupID uuid.UUID, targets []models.EscalationGroupTarget) error {
	return r.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		if err := tx.Where("escalation_group_id = ?", groupID).Delete(&models.EscalationGroupTarget{}).Error; err != nil {
			return err
		}
		for i := range targets {
			targets[i].EscalationGroupID = groupID
			if err := tx.Create(&targets[i]).Error; err != nil {
				return err
			}
		}
		return nil
	})
}
