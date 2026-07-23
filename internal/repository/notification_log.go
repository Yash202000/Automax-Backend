package repository

import (
	"context"
	"fmt"
	"log"
	"strings"
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
	ListForMonitoring(ctx context.Context, filter *models.NotificationMonitoringFilter) ([]models.NotificationLog, int64, error)
	FindByRecipientProviderMessageID(ctx context.Context, providerMessageID string) (*models.NotificationLog, error)
	UpdateRecipientDeliveryStatus(ctx context.Context, logID uuid.UUID, providerMessageID, status, failureCode, errorMessage string) error
	GetStats(ctx context.Context, channel string, sentBy *uuid.UUID, receivedBy *uuid.UUID) ([]models.NotificationChannelStat, error)
	GetStatsBySentBy(ctx context.Context, channel string, sentBy *uuid.UUID, receivedBy *uuid.UUID) ([]models.NotificationUserStatRow, error)
	GetStatsByReceivedBy(ctx context.Context, channel string, sentBy *uuid.UUID, receivedBy *uuid.UUID) ([]models.NotificationUserStatRow, error)
	GetUserNotificationStats(ctx context.Context, channel string, userID uuid.UUID) (*models.NotificationUserStatRow, error)
	Update(ctx context.Context, log *models.NotificationLog) error
	Delete(ctx context.Context, id uuid.UUID) error
	MarkAsRead(ctx context.Context, id uuid.UUID, isRead bool) error
	ToggleStar(ctx context.Context, id uuid.UUID, isStarred bool) error
	MoveToCategory(ctx context.Context, id uuid.UUID, category string) error
	BulkMoveToCategory(ctx context.Context, ids []uuid.UUID, category string) error
	BulkDelete(ctx context.Context, ids []uuid.UUID) error
	MarkOTPVerified(ctx context.Context, sessionID string, verifiedAt time.Time) error
	SetMeta(ctx context.Context, ids []uuid.UUID, meta *models.NotificationMeta) error
	SetArContent(ctx context.Context, ids []uuid.UUID, subjectAr, bodyAr string) error
	SetIncidentID(ctx context.Context, ids []uuid.UUID, incidentID uuid.UUID) error
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
	err := r.db.WithContext(ctx).
		Preload("SentByUser").
		Preload("ReceivedByUser").
		First(&log, "id = ?", id).Error
	if err != nil {
		return nil, err
	}
	return &log, nil
}

// FindByRecipientProviderMessageID locates the NotificationLog containing a
// recipient whose ProviderMessageID matches (Twilio SID / Meta message id),
// so a provider delivery-status webhook can be matched back to the send.
func (r *notificationLogRepository) FindByRecipientProviderMessageID(ctx context.Context, providerMessageID string) (*models.NotificationLog, error) {
	var log models.NotificationLog
	err := r.db.WithContext(ctx).
		Where("recipients @> ?", fmt.Sprintf(`[{"provider_message_id":"%s"}]`, providerMessageID)).
		First(&log).Error
	if err != nil {
		return nil, err
	}
	return &log, nil
}

// UpdateRecipientDeliveryStatus updates the matching recipient's Status/FailureCode/
// ErrorMessage inside the Recipients JSONB array, keyed by ProviderMessageID. For
// single-recipient sends (the common case for SMS/WhatsApp) it also mirrors the
// new status onto the parent log's top-level Status so list/filter views pick it
// up without inspecting each recipient individually.
func (r *notificationLogRepository) UpdateRecipientDeliveryStatus(ctx context.Context, logID uuid.UUID, providerMessageID, status, failureCode, errorMessage string) error {
	var notifLog models.NotificationLog
	if err := r.db.WithContext(ctx).First(&notifLog, "id = ?", logID).Error; err != nil {
		return err
	}

	found := false
	for i := range notifLog.Recipients {
		if notifLog.Recipients[i].ProviderMessageID == providerMessageID {
			notifLog.Recipients[i].Status = status
			if failureCode != "" {
				notifLog.Recipients[i].FailureCode = failureCode
			}
			if errorMessage != "" {
				notifLog.Recipients[i].ErrorMessage = errorMessage
			}
			found = true
			break
		}
	}
	if !found {
		return fmt.Errorf("recipient with provider_message_id %s not found on notification log %s", providerMessageID, logID)
	}

	updates := map[string]interface{}{"recipients": notifLog.Recipients}
	if len(notifLog.Recipients) == 1 {
		updates["status"] = status
		if failureCode != "" {
			updates["failure_code"] = failureCode
		}
	}

	return r.db.WithContext(ctx).Model(&models.NotificationLog{}).
		Where("id = ?", logID).
		Updates(updates).Error
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

	if filter.IncidentID != nil {
		// Incident communication history: keep failed sends visible (audit trail requires it)
		// and do not apply the personal inbox sent_by/received_by scoping below.
		// Default to SMS/Email only (the requirement covers those two channels), but if the
		// caller explicitly asked for a specific channel (e.g. ?channel=notification), trust
		// that instead of overriding it — the `filter.Channel` check further below applies it.
		log.Printf("[Notifications Repository] Filtering by incident_id: %s", filter.IncidentID.String())
		query = query.Where("incident_id = ?", *filter.IncidentID)
		if filter.Channel == "" {
			query = query.Where("channel IN ('sms', 'email')")
		}
	} else {
		query = query.Where("NOT (channel IN ('sms', 'email') AND status = 'failed')")

		// Apply user filtering - show emails where user is either sender OR receiver
		if filter.UserID != nil {
			log.Printf("[Notifications Repository] Filtering by user_id: %s (sent_by OR received_by)", filter.UserID.String())
			query = query.Where("sent_by = ? OR received_by = ?", *filter.UserID, *filter.UserID)
		}
	}

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
	order := "created_at DESC"
	if filter.IncidentID != nil {
		order = "created_at ASC" // audit trail: chronological order
	}
	err := query.
		Preload("SentByUser").
		Preload("ReceivedByUser").
		Order(order).
		Offset(offset).
		Limit(filter.Limit).
		Find(&logs).Error
	if err != nil {
		return nil, 0, err
	}

	log.Printf("[Notifications Repository] Found %d notifications (total: %d) for user filter", len(logs), total)

	return logs, total, nil
}

// ListForMonitoring is the admin-wide equivalent of List: no sent_by/received_by
// scoping and no exclusion of failed sms/email rows, since the monitoring
// dashboard's whole purpose is to surface delivery failures across every user.
func (r *notificationLogRepository) ListForMonitoring(ctx context.Context, filter *models.NotificationMonitoringFilter) ([]models.NotificationLog, int64, error) {
	var logs []models.NotificationLog
	var total int64

	query := r.db.WithContext(ctx).Model(&models.NotificationLog{}).
		Where("direction = ?", models.DirectionOutbound)

	if filter.Channel != "" {
		rawChannels := strings.Split(filter.Channel, ",")
		channels := make([]string, 0, len(rawChannels))
		for _, ch := range rawChannels {
			if ch = strings.TrimSpace(ch); ch != "" {
				channels = append(channels, ch)
			}
		}
		if len(channels) == 1 {
			query = query.Where("channel = ?", channels[0])
		} else if len(channels) > 1 {
			query = query.Where("channel IN ?", channels)
		}
	}
	if filter.Status != "" {
		query = query.Where("status = ?", filter.Status)
	}
	if filter.FailureCode != "" {
		query = query.Where("failure_code = ?", filter.FailureCode)
	}
	if filter.Recipient != "" {
		query = query.Where("recipients::text ILIKE ?", "%"+filter.Recipient+"%")
	}
	if filter.RecordType != "" {
		query = query.Where(
			"template_code IN (SELECT code FROM notification_templates WHERE module_type = ?)",
			filter.RecordType,
		)
	}
	if filter.StartDate != nil {
		query = query.Where("created_at >= ?", *filter.StartDate)
	}
	if filter.EndDate != nil {
		query = query.Where("created_at <= ?", *filter.EndDate)
	}

	if err := query.Count(&total).Error; err != nil {
		return nil, 0, err
	}

	if filter.Page <= 0 {
		filter.Page = 1
	}
	if filter.Limit <= 0 || filter.Limit > 100 {
		filter.Limit = 20
	}
	offset := (filter.Page - 1) * filter.Limit

	err := query.
		Preload("SentByUser").
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
	fmt.Println("Marking OTP as verified", sessionID)
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

func (r *notificationLogRepository) SetMeta(ctx context.Context, ids []uuid.UUID, meta *models.NotificationMeta) error {
	if len(ids) == 0 || meta == nil {
		return nil
	}
	return r.db.WithContext(ctx).
		Model(&models.NotificationLog{}).
		Where("id IN ?", ids).
		Update("meta", meta).Error
}

func (r *notificationLogRepository) SetIncidentID(ctx context.Context, ids []uuid.UUID, incidentID uuid.UUID) error {
	if len(ids) == 0 {
		return nil
	}
	return r.db.WithContext(ctx).
		Model(&models.NotificationLog{}).
		Where("id IN ?", ids).
		Update("incident_id", incidentID).Error
}

func (r *notificationLogRepository) SetArContent(ctx context.Context, ids []uuid.UUID, subjectAr, bodyAr string) error {
	if len(ids) == 0 {
		return nil
	}
	return r.db.WithContext(ctx).
		Model(&models.NotificationLog{}).
		Where("id IN ?", ids).
		Updates(map[string]interface{}{
			"subject_ar": subjectAr,
			"body_ar":    bodyAr,
		}).Error
}

func (r *notificationLogRepository) GetStats(ctx context.Context, channel string, sentBy *uuid.UUID, receivedBy *uuid.UUID) ([]models.NotificationChannelStat, error) {
	type row struct {
		Channel  string
		Status   string
		Category string
		Count    int64
	}

	query := r.db.WithContext(ctx).
		Model(&models.NotificationLog{}).
		Select("channel, status, category, COUNT(*) as count").
		Group("channel, status, category")

	if channel != "" {
		query = query.Where("channel = ?", channel)
	}
	if sentBy != nil {
		query = query.Where("sent_by = ?", *sentBy)
	}
	if receivedBy != nil {
		query = query.Where("received_by = ?", *receivedBy)
	}

	var rows []row
	if err := query.Scan(&rows).Error; err != nil {
		return nil, err
	}

	statsMap := make(map[string]*models.NotificationChannelStat)
	for _, row := range rows {
		s, ok := statsMap[row.Channel]
		if !ok {
			s = &models.NotificationChannelStat{Channel: row.Channel}
			statsMap[row.Channel] = s
		}
		s.Total += row.Count
		switch row.Status {
		case "sent":
			s.Sent += row.Count
		case "failed":
			s.Failed += row.Count
		}
		switch row.Category {
		case models.CategoryInbox:
			s.Inbox += row.Count
		case models.CategoryDraft:
			s.Draft += row.Count
		case models.CategoryOutbox:
			s.Outbox += row.Count
		case models.CategoryTrash:
			s.Trash += row.Count
		case models.CategorySpam:
			s.Spam += row.Count
		}
	}

	result := make([]models.NotificationChannelStat, 0, len(statsMap))
	for _, s := range statsMap {
		result = append(result, *s)
	}
	return result, nil
}

func (r *notificationLogRepository) GetStatsBySentBy(ctx context.Context, channel string, sentBy *uuid.UUID, receivedBy *uuid.UUID) ([]models.NotificationUserStatRow, error) {
	query := r.db.WithContext(ctx).
		Model(&models.NotificationLog{}).
		Select("sent_by AS user_id, COUNT(*) AS total, " +
			"SUM(CASE WHEN status = 'sent' THEN 1 ELSE 0 END) AS sent, " +
			"SUM(CASE WHEN status = 'failed' THEN 1 ELSE 0 END) AS failed, " +
			"SUM(CASE WHEN category = 'draft' THEN 1 ELSE 0 END) AS draft, " +
			"SUM(CASE WHEN category = 'trash' THEN 1 ELSE 0 END) AS trash, " +
			"SUM(CASE WHEN category = 'inbox' THEN 1 ELSE 0 END) AS inbox, " +
			"SUM(CASE WHEN category = 'outbox' THEN 1 ELSE 0 END) AS outbox, " +
			"SUM(CASE WHEN category = 'spam' THEN 1 ELSE 0 END) AS spam").
		Where("sent_by IS NOT NULL").
		Where("category = 'sent'").
		Group("sent_by")

	if channel != "" {
		query = query.Where("channel = ?", channel)
	}
	if sentBy != nil {
		query = query.Where("sent_by = ?", *sentBy)
	}
	if receivedBy != nil {
		query = query.Where("received_by = ?", *receivedBy)
	}

	var rows []models.NotificationUserStatRow
	if err := query.Scan(&rows).Error; err != nil {
		return nil, err
	}
	return rows, nil
}

func (r *notificationLogRepository) GetStatsByReceivedBy(ctx context.Context, channel string, sentBy *uuid.UUID, receivedBy *uuid.UUID) ([]models.NotificationUserStatRow, error) {
	query := r.db.WithContext(ctx).
		Model(&models.NotificationLog{}).
		Select("received_by AS user_id, COUNT(*) AS total, " +
			"SUM(CASE WHEN status = 'sent' THEN 1 ELSE 0 END) AS sent, " +
			"SUM(CASE WHEN status = 'failed' THEN 1 ELSE 0 END) AS failed, " +
			"SUM(CASE WHEN category = 'draft' THEN 1 ELSE 0 END) AS draft, " +
			"SUM(CASE WHEN category = 'trash' THEN 1 ELSE 0 END) AS trash, " +
			"SUM(CASE WHEN category = 'inbox' THEN 1 ELSE 0 END) AS inbox, " +
			"SUM(CASE WHEN category = 'outbox' THEN 1 ELSE 0 END) AS outbox, " +
			"SUM(CASE WHEN category = 'spam' THEN 1 ELSE 0 END) AS spam").
		Where("received_by IS NOT NULL").
		Group("received_by")

	if channel != "" {
		query = query.Where("channel = ?", channel)
	}
	if sentBy != nil {
		query = query.Where("sent_by = ?", *sentBy)
	}
	if receivedBy != nil {
		query = query.Where("received_by = ?", *receivedBy)
	}

	var rows []models.NotificationUserStatRow
	if err := query.Scan(&rows).Error; err != nil {
		return nil, err
	}
	return rows, nil
}

// GetUserNotificationStats returns aggregated counts for all notifications .where the user is either the sender or the receiver, in a single query.

func (r *notificationLogRepository) GetUserNotificationStats(ctx context.Context, channel string, userID uuid.UUID) (*models.NotificationUserStatRow, error) {
	rawSQL := `
		SELECT
			COUNT(*)                                                                                                    AS total,
			SUM(CASE WHEN category = 'sent'   AND direction = 'outbound' AND sent_by     = @userID THEN 1 ELSE 0 END) AS sent,
			SUM(CASE WHEN status  = 'failed'                             AND sent_by     = @userID THEN 1 ELSE 0 END) AS failed,
			SUM(CASE WHEN category = 'inbox'  AND direction = 'inbound'  AND received_by = @userID THEN 1 ELSE 0 END) AS inbox,
			SUM(CASE WHEN category = 'draft'                             AND sent_by     = @userID THEN 1 ELSE 0 END) AS draft,
			SUM(CASE WHEN category = 'outbox'                            AND sent_by     = @userID THEN 1 ELSE 0 END) AS outbox,
			SUM(CASE WHEN category = 'trash'                                                       THEN 1 ELSE 0 END) AS trash,
			SUM(CASE WHEN category = 'spam'                                                        THEN 1 ELSE 0 END) AS spam
		FROM notification_logs
		WHERE (sent_by = @userID OR received_by = @userID)
		AND deleted_at IS NULL
		AND NOT (channel IN ('sms', 'email') AND status = 'failed')
		AND (@channel = '' OR channel = @channel)
	`

	var row models.NotificationUserStatRow
	if err := r.db.WithContext(ctx).Raw(rawSQL, map[string]interface{}{
		"userID":  userID,
		"channel": channel,
	}).Scan(&row).Error; err != nil {
		return nil, err
	}
	return &row, nil
}
