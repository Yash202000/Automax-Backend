package repository

import (
	"context"
	"fmt"
	"time"

	"github.com/automax/backend/internal/models"
	"github.com/google/uuid"
	"gorm.io/gorm"
)

type NotificationLogRepository interface {
	Create(ctx context.Context, log *models.NotificationLog) error
	FindByID(ctx context.Context, id uuid.UUID) (*models.NotificationLog, error)
	FindByAttachmentID(ctx context.Context, attachmentID string) ([]*models.NotificationLog, error)
	List(ctx context.Context, filter *models.NotificationLogFilter) ([]models.NotificationLog, int64, error)
	Update(ctx context.Context, log *models.NotificationLog) error
	Delete(ctx context.Context, id uuid.UUID) error
	MarkAsRead(ctx context.Context, id uuid.UUID, isRead bool) error
	ToggleStar(ctx context.Context, id uuid.UUID, isStarred bool) error
	MoveToCategory(ctx context.Context, id uuid.UUID, category string) error
	BulkMoveToCategory(ctx context.Context, ids []uuid.UUID, category string) error
	BulkDelete(ctx context.Context, ids []uuid.UUID) error
	MarkOTPVerified(ctx context.Context, sessionID string, verifiedAt time.Time) error
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

func (r *notificationLogRepository) FindByAttachmentID(ctx context.Context, attachmentID string) ([]*models.NotificationLog, error) {
	var logs []*models.NotificationLog
	// Query JSONB field for attachment with matching ID
	err := r.db.WithContext(ctx).
		Where("attachments @> ?", `[{"id":"`+attachmentID+`"}]`).
		Find(&logs).Error
	if err != nil {
		return nil, err
	}
	return logs, nil
}

func (r *notificationLogRepository) List(ctx context.Context, filter *models.NotificationLogFilter) ([]models.NotificationLog, int64, error) {
	var logs []models.NotificationLog
	var total int64

	query := r.db.WithContext(ctx).Model(&models.NotificationLog{})

	// Apply filters
	if filter.Channel != "" {
		query = query.Where("channel = ?", filter.Channel)
	}
	if filter.Direction != "" {
		query = query.Where("direction = ?", filter.Direction)
	}
	if filter.Category != "" {
		query = query.Where("category = ?", filter.Category)
	}
	if filter.Status != "" {
		query = query.Where("status = ?", filter.Status)
	}
	if filter.IsRead != nil {
		query = query.Where("is_read = ?", *filter.IsRead)
	}
	if filter.IsStarred != nil {
		query = query.Where("is_starred = ?", *filter.IsStarred)
	}
	if filter.SentBy != nil {
		query = query.Where("sent_by = ?", *filter.SentBy)
	}
	if filter.ReceivedBy != nil {
		query = query.Where("received_by = ?", *filter.ReceivedBy)
	}
	if filter.TemplateCode != "" {
		query = query.Where("template_code = ?", filter.TemplateCode)
	}
	if filter.ThreadID != nil {
		query = query.Where("thread_id = ?", *filter.ThreadID)
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
			"subject ILIKE ? OR body ILIKE ? OR body_html ILIKE ? OR "+
				"\"from\" ILIKE ? OR recipients::text ILIKE ? OR "+
				"cc::text ILIKE ? OR bcc::text ILIKE ? OR template_code ILIKE ?",
			searchPattern, searchPattern, searchPattern, searchPattern,
			searchPattern, searchPattern, searchPattern, searchPattern,
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

func (r *notificationLogRepository) Update(ctx context.Context, log *models.NotificationLog) error {
	return r.db.WithContext(ctx).Save(log).Error
}

func (r *notificationLogRepository) Delete(ctx context.Context, id uuid.UUID) error {
	return r.db.WithContext(ctx).Delete(&models.NotificationLog{}, "id = ?", id).Error
}

func (r *notificationLogRepository) MarkAsRead(ctx context.Context, id uuid.UUID, isRead bool) error {
	return r.db.WithContext(ctx).Model(&models.NotificationLog{}).
		Where("id = ?", id).
		Update("is_read", isRead).Error
}

func (r *notificationLogRepository) ToggleStar(ctx context.Context, id uuid.UUID, isStarred bool) error {
	return r.db.WithContext(ctx).Model(&models.NotificationLog{}).
		Where("id = ?", id).
		Update("is_starred", isStarred).Error
}

func (r *notificationLogRepository) MoveToCategory(ctx context.Context, id uuid.UUID, category string) error {
	return r.db.WithContext(ctx).Model(&models.NotificationLog{}).
		Where("id = ?", id).
		Update("category", category).Error
}

func (r *notificationLogRepository) BulkMoveToCategory(ctx context.Context, ids []uuid.UUID, category string) error {
	return r.db.WithContext(ctx).Model(&models.NotificationLog{}).
		Where("id IN ?", ids).
		Update("category", category).Error
}

func (r *notificationLogRepository) BulkDelete(ctx context.Context, ids []uuid.UUID) error {
	return r.db.WithContext(ctx).Delete(&models.NotificationLog{}, "id IN ?", ids).Error
}

//OTP Verification store inside NotificationLog

func (r *notificationLogRepository) MarkOTPVerified(
	ctx context.Context,
	sessionID string,
	now time.Time,
) error {

	result := r.db.WithContext(ctx).
		Model(&models.NotificationLog{}).
		Where("otp_session_id = ? AND otp_verified = false", sessionID).
		Updates(map[string]interface{}{
			"otp_verified":    true,
			"otp_verified_at": now,
		})

	if result.RowsAffected == 0 {
		return fmt.Errorf("already verified or not found")
	}

	return result.Error
}
