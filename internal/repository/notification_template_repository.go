package repository

import (
	"context"

	"github.com/automax/backend/internal/models"
	"github.com/google/uuid"
	"gorm.io/gorm"
)

type NotificationTemplateRepository interface {
	Create(ctx context.Context, tpl *models.NotificationTemplate) error
	FindByID(ctx context.Context, id uuid.UUID) (*models.NotificationTemplate, error)
	// FindByCode / FindByCodeChannelLanguage kept for notification service compatibility.
	FindByCode(ctx context.Context, code, channel, language string) (*models.NotificationTemplate, error)
	FindByCodeChannelLanguage(ctx context.Context, code, channel, language string) (*models.NotificationTemplate, error)
	Update(ctx context.Context, tpl *models.NotificationTemplate) error
	Delete(ctx context.Context, id uuid.UUID) error
	List(ctx context.Context) ([]models.NotificationTemplate, error)
	// New query methods
	ListWithFilters(ctx context.Context, filter models.NotificationTemplateFilter) ([]models.NotificationTemplate, int64, error)
	FindAllByCode(ctx context.Context, code, channel string) ([]models.NotificationTemplate, error)
	FindByTransitionID(ctx context.Context, transitionID uuid.UUID) ([]models.NotificationTemplate, error)
}

type notificationTemplateRepository struct {
	db *gorm.DB
}

func NewNotificationTemplateRepository(db *gorm.DB) NotificationTemplateRepository {
	return &notificationTemplateRepository{db: db}
}

func (r *notificationTemplateRepository) Create(ctx context.Context, tpl *models.NotificationTemplate) error {
	return r.db.WithContext(ctx).Create(tpl).Error
}

func (r *notificationTemplateRepository) FindByID(ctx context.Context, id uuid.UUID) (*models.NotificationTemplate, error) {
	var tpl models.NotificationTemplate
	if err := r.db.WithContext(ctx).First(&tpl, "id = ?", id).Error; err != nil {
		return nil, err
	}
	return &tpl, nil
}

func (r *notificationTemplateRepository) FindByCode(ctx context.Context, code, channel, language string) (*models.NotificationTemplate, error) {
	var tpl models.NotificationTemplate
	err := r.db.WithContext(ctx).
		Where("code = ? AND channel = ? AND language = ? AND is_active = true", code, channel, language).
		First(&tpl).Error
	if err != nil {
		return nil, err
	}
	return &tpl, nil
}

func (r *notificationTemplateRepository) FindByCodeChannelLanguage(ctx context.Context, code, channel, language string) (*models.NotificationTemplate, error) {
	return r.FindByCode(ctx, code, channel, language)
}

func (r *notificationTemplateRepository) Update(ctx context.Context, tpl *models.NotificationTemplate) error {
	return r.db.WithContext(ctx).Save(tpl).Error
}

func (r *notificationTemplateRepository) Delete(ctx context.Context, id uuid.UUID) error {
	return r.db.WithContext(ctx).Delete(&models.NotificationTemplate{}, "id = ?", id).Error
}

func (r *notificationTemplateRepository) List(ctx context.Context) ([]models.NotificationTemplate, error) {
	var list []models.NotificationTemplate
	err := r.db.WithContext(ctx).Order("created_at DESC").Find(&list).Error
	return list, err
}

func (r *notificationTemplateRepository) ListWithFilters(ctx context.Context, filter models.NotificationTemplateFilter) ([]models.NotificationTemplate, int64, error) {
	q := r.db.WithContext(ctx).Model(&models.NotificationTemplate{})

	if filter.Channel != "" {
		q = q.Where("channel = ?", filter.Channel)
	}
	if filter.Language != "" {
		q = q.Where("language = ?", filter.Language)
	}
	if filter.ModuleType != "" {
		q = q.Where("module_type = ?", filter.ModuleType)
	}
	if filter.ActionType != "" {
		q = q.Where("action_type = ?", filter.ActionType)
	}
	if filter.IsActive != nil {
		q = q.Where("is_active = ?", *filter.IsActive)
	}
	if filter.Code != "" {
		q = q.Where("code = ?", filter.Code)
	}
	if filter.Search != "" {
		like := "%" + filter.Search + "%"
		q = q.Where("name ILIKE ? OR description ILIKE ?", like, like)
	}
	if filter.TransitionID != nil {
		q = q.Where("transition_id = ?", *filter.TransitionID)
	}

	var total int64
	if err := q.Count(&total).Error; err != nil {
		return nil, 0, err
	}

	page := filter.Page
	limit := filter.Limit
	if page < 1 {
		page = 1
	}
	if limit < 1 || limit > 100 {
		limit = 20
	}
	offset := (page - 1) * limit

	var list []models.NotificationTemplate
	err := q.Order("code ASC, language ASC").Offset(offset).Limit(limit).Find(&list).Error
	return list, total, err
}

func (r *notificationTemplateRepository) FindAllByCode(ctx context.Context, code, channel string) ([]models.NotificationTemplate, error) {
	var list []models.NotificationTemplate
	q := r.db.WithContext(ctx).Where("code = ?", code)
	if channel != "" {
		q = q.Where("channel = ?", channel)
	}
	err := q.Order("language ASC").Find(&list).Error
	return list, err
}

func (r *notificationTemplateRepository) FindByTransitionID(ctx context.Context, transitionID uuid.UUID) ([]models.NotificationTemplate, error) {
	var list []models.NotificationTemplate
	err := r.db.WithContext(ctx).
		Where("transition_id = ?", transitionID).
		Order("code ASC, language ASC").
		Find(&list).Error
	return list, err
}
