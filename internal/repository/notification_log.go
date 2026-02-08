package repository

import (
	"context"

	"github.com/automax/backend/internal/models"
	"github.com/google/uuid"
	"gorm.io/gorm"
)

type NotificationLogRepository interface {
	Create(ctx context.Context, log *models.NotificationLog) error
	FindByID(ctx context.Context, id uuid.UUID) (*models.NotificationLog, error)
	List(ctx context.Context, filter *models.NotificationLogFilter) ([]models.NotificationLog, int64, error)
	Delete(ctx context.Context, id uuid.UUID) error
}

type notificationLogRepository struct {
	db *gorm.DB
}

func NewNotificationLogRepository(db *gorm.DB) NotificationLogRepository {
	return &notificationLogRepository{db: db}
}

func (r *notificationLogRepository) Create(
	ctx context.Context,
	log *models.NotificationLog,
) error {
	return r.db.WithContext(ctx).Create(log).Error
}

func (r *notificationLogRepository) FindByID(ctx context.Context, id uuid.UUID) (*models.NotificationLog, error) {
	var log models.NotificationLog
	err := r.db.WithContext(ctx).First(&log, "id = ?", id).Error
	if err != nil {
		return nil, err
	}
	return &log, nil
}

func (r *notificationLogRepository) List(ctx context.Context, filter *models.NotificationLogFilter) ([]models.NotificationLog, int64, error) {
	var logs []models.NotificationLog
	var total int64

	query := r.db.WithContext(ctx).Model(&models.NotificationLog{})

	// Apply filters
	if filter.Channel != "" {
		query = query.Where("channel = ?", filter.Channel)
	}
	if filter.Status != "" {
		query = query.Where("status = ?", filter.Status)
	}
	if filter.SentBy != nil {
		query = query.Where("sent_by = ?", *filter.SentBy)
	}
	if filter.TemplateCode != "" {
		query = query.Where("template_code = ?", filter.TemplateCode)
	}
	if filter.StartDate != nil {
		query = query.Where("created_at >= ?", *filter.StartDate)
	}
	if filter.EndDate != nil {
		query = query.Where("created_at <= ?", *filter.EndDate)
	}

	// Search functionality (like Gmail) - search across multiple fields
	if filter.Search != "" {
		searchPattern := "%" + filter.Search + "%"
		query = query.Where(
			"subject ILIKE ? OR body ILIKE ? OR recipients::text ILIKE ? OR "+
				"cc::text ILIKE ? OR bcc::text ILIKE ? OR template_code ILIKE ?",
			searchPattern, searchPattern, searchPattern,
			searchPattern, searchPattern, searchPattern,
		)
	}

	// Count total
	if err := query.Count(&total).Error; err != nil {
		return nil, 0, err
	}

	// Set defaults for pagination
	if filter.Page <= 0 {
		filter.Page = 1
	}
	if filter.Limit <= 0 || filter.Limit > 100 {
		filter.Limit = 20
	}

	// Apply pagination
	offset := (filter.Page - 1) * filter.Limit
	err := query.
		Order("created_at DESC").
		Offset(offset).
		Limit(filter.Limit).
		Find(&logs).Error
	if err != nil {
		return nil, 0, err
	}

	return logs, total, nil
}

func (r *notificationLogRepository) Delete(ctx context.Context, id uuid.UUID) error {
	return r.db.WithContext(ctx).Delete(&models.NotificationLog{}, "id = ?", id).Error
}
