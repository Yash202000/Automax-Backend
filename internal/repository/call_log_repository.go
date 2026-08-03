package repository

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/automax/backend/internal/models"
	"github.com/google/uuid"
	"gorm.io/gorm"
)

type CallLogRepository interface {
	Create(ctx context.Context, callLog *models.CallLog) error
	FindByID(ctx context.Context, id uuid.UUID) (*models.CallLog, error)
	FindByCallUUID(ctx context.Context, callUUID string) (*models.CallLog, error)
	Update(ctx context.Context, callLog *models.CallLog) error
	UpdateByField(ctx context.Context, id uuid.UUID, fields map[string]interface{}) error
	Delete(ctx context.Context, id uuid.UUID) error
	List(ctx context.Context, filter *models.CallLogFilter) ([]models.CallLog, int64, error)
	ListSummary(ctx context.Context, filter *models.CallLogFilter) ([]models.CallLog, int64, error)
	GetStats(ctx context.Context) (*models.CallLogStats, error)
	FindByPhone(ctx context.Context, phone string, page, limit int) ([]models.CallLog, int64, error)
	CreateWithParticipants(ctx context.Context, callLog *models.CallLog, participants []*models.CallParticipant) error
	CreateParticipant(ctx context.Context, p *models.CallParticipant) error
	FindParticipant(ctx context.Context, callLogID uuid.UUID, phone string) (*models.CallParticipant, error)
	UpdateParticipant(ctx context.Context, p *models.CallParticipant) error
	UpdateParticipantsLeftAt(ctx context.Context, callLogID uuid.UUID, leftAt time.Time) error
	UpdateParticipantJoin(ctx context.Context, callLogID uuid.UUID, phone, joinStatus string, joinedAt *time.Time) error
	CreateAttachment(ctx context.Context, attachment *models.CallLogAttachment) error
	FindAttachmentByID(ctx context.Context, id uuid.UUID) (*models.CallLogAttachment, error)
}

type callLogRepository struct {
	db *gorm.DB
}

func NewCallLogRepository(db *gorm.DB) CallLogRepository {
	return &callLogRepository{db: db}
}

func (r *callLogRepository) Create(ctx context.Context, callLog *models.CallLog) error {
	return r.db.WithContext(ctx).Create(callLog).Error
}

func (r *callLogRepository) FindByID(ctx context.Context, id uuid.UUID) (*models.CallLog, error) {
	var callLog models.CallLog
	err := r.db.WithContext(ctx).
		Preload("Participants").
		Preload("Attachments").
		First(&callLog, "id = ?", id).Error
	if err != nil {
		return nil, err
	}
	return &callLog, nil
}

func (r *callLogRepository) FindByCallUUID(ctx context.Context, callUUID string) (*models.CallLog, error) {
	var callLog models.CallLog
	err := r.db.WithContext(ctx).
		Preload("Participants").
		First(&callLog, "call_uuid = ?", callUUID).Error
	if err != nil {
		return nil, err
	}
	return &callLog, nil
}

func (r *callLogRepository) Update(ctx context.Context, callLog *models.CallLog) error {
	return r.db.WithContext(ctx).Omit("Participants").Save(callLog).Error
}

func (r *callLogRepository) UpdateByField(ctx context.Context, id uuid.UUID, fields map[string]interface{}) error {
	if len(fields) == 0 {
		return errors.New("no fields provided for update")
	}

	result := r.db.WithContext(ctx).
		Model(&models.CallLog{}).
		Where("id = ?", id).
		Updates(fields)

	if result.Error != nil {
		return fmt.Errorf("failed to update call log: %w", result.Error)
	}

	if result.RowsAffected == 0 {
		return fmt.Errorf("call log with id %s not found", id)
	}

	return nil
}

func (r *callLogRepository) Delete(ctx context.Context, id uuid.UUID) error {
	return r.db.WithContext(ctx).Delete(&models.CallLog{}, "id = ?", id).Error
}

func (r *callLogRepository) List(ctx context.Context, filter *models.CallLogFilter) ([]models.CallLog, int64, error) {
	var callLogs []models.CallLog
	var total int64

	query := r.db.WithContext(ctx).Model(&models.CallLog{})

	if filter.Status != "" {
		query = query.Where("status = ?", filter.Status)
	}
	if filter.StartDate != nil {
		query = query.Where("created_at >= ?", *filter.StartDate)
	}
	if filter.EndDate != nil {
		query = query.Where("created_at <= ?", *filter.EndDate)
	}
	if filter.Search != "" {
		searchPattern := "%" + filter.Search + "%"
		query = query.Where("call_uuid ILIKE ?", searchPattern)
	}
	if filter.ParticipantID != nil {
		query = query.Where(
			`id IN (
				SELECT cp.call_log_id FROM call_participants cp
				JOIN users u ON cp.phone_number = u.phone OR cp.phone_number IN (
					SELECT ea.extension FROM extension_assignments ea WHERE ea.user_id = u.id
				)
				WHERE u.id = ?
			)`,
			*filter.ParticipantID,
		)
	}

	if err := query.Count(&total).Error; err != nil {
		return nil, 0, err
	}

	offset := (filter.Page - 1) * filter.Limit
	err := query.
		Preload("Participants").
		Preload("Attachments").
		Order("created_at DESC").
		Offset(offset).
		Limit(filter.Limit).
		Find(&callLogs).Error
	if err != nil {
		return nil, 0, err
	}

	return callLogs, total, nil
}

func (r *callLogRepository) ListSummary(ctx context.Context, filter *models.CallLogFilter) ([]models.CallLog, int64, error) {
	var callLogs []models.CallLog
	var total int64

	query := r.db.WithContext(ctx).Model(&models.CallLog{})

	if filter.Status != "" {
		query = query.Where("status = ?", filter.Status)
	}
	if filter.StartDate != nil {
		query = query.Where("created_at >= ?", *filter.StartDate)
	}
	if filter.EndDate != nil {
		query = query.Where("created_at <= ?", *filter.EndDate)
	}
	if filter.ParticipantID != nil {
		// Cintrix PBX calls are company call-centre history: their participants
		// are external callers and PBX extensions, not Automax user ids, so a
		// participant join can never match (and missed calls have no agent at
		// all). Show them to everyone this admin endpoint already gates;
		// personal direct/group calls stay participant-scoped.
		query = query.Where(
			`(call_type = 'cintrix' OR id IN (
				SELECT cp.call_log_id FROM call_participants cp
				JOIN users u ON cp.phone_number = u.phone OR cp.phone_number IN (
					SELECT ea.extension FROM extension_assignments ea WHERE ea.user_id = u.id
				)
				WHERE u.id = ?
			))`,
			*filter.ParticipantID,
		)
	}

	if err := query.Count(&total).Error; err != nil {
		return nil, 0, err
	}

	offset := (filter.Page - 1) * filter.Limit
	err := query.
		Select("id, call_uuid, call_type, start_at, end_at, status, meta, created_at").
		Preload("Participants").
		Preload("Attachments").
		Order("created_at DESC").
		Offset(offset).
		Limit(filter.Limit).
		Find(&callLogs).Error

	return callLogs, total, err
}

func (r *callLogRepository) GetStats(ctx context.Context) (*models.CallLogStats, error) {
	stats := &models.CallLogStats{}

	r.db.WithContext(ctx).Model(&models.CallLog{}).Count(&stats.TotalCalls)

	var statusStats []struct {
		Status string
		Count  int64
	}
	r.db.WithContext(ctx).Model(&models.CallLog{}).
		Select("status, count(*) as count").
		Group("status").
		Find(&statusStats)

	stats.CallsByStatus = make(map[string]int64)
	for _, stat := range statusStats {
		stats.CallsByStatus[stat.Status] = stat.Count
	}

	thirtyDaysAgo := time.Now().AddDate(0, 0, -30)
	r.db.WithContext(ctx).Model(&models.CallLog{}).
		Where("created_at >= ?", thirtyDaysAgo).
		Count(&stats.RecentCalls)

	return stats, nil
}

func (r *callLogRepository) FindByPhone(ctx context.Context, phone string, page, limit int) ([]models.CallLog, int64, error) {
	var callLogs []models.CallLog
	var total int64

	query := r.db.WithContext(ctx).Model(&models.CallLog{}).
		Where("id IN (SELECT call_log_id FROM call_participants WHERE phone_number = ?)", phone)

	if err := query.Count(&total).Error; err != nil {
		return nil, 0, err
	}

	offset := (page - 1) * limit
	err := query.
		Preload("Participants").
		Preload("Attachments").
		Order("created_at DESC").
		Offset(offset).
		Limit(limit).
		Find(&callLogs).Error
	if err != nil {
		return nil, 0, err
	}

	return callLogs, total, nil
}

func (r *callLogRepository) CreateWithParticipants(ctx context.Context, callLog *models.CallLog, participants []*models.CallParticipant) error {
	return r.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		if err := tx.Create(callLog).Error; err != nil {
			return err
		}
		for _, p := range participants {
			p.CallLogID = callLog.ID
			if err := tx.Create(p).Error; err != nil {
				return err
			}
		}
		return nil
	})
}

func (r *callLogRepository) CreateParticipant(ctx context.Context, p *models.CallParticipant) error {
	return r.db.WithContext(ctx).Create(p).Error
}

func (r *callLogRepository) FindParticipant(ctx context.Context, callLogID uuid.UUID, phone string) (*models.CallParticipant, error) {
	var p models.CallParticipant
	if err := r.db.WithContext(ctx).
		Where("call_log_id = ? AND phone_number = ?", callLogID, phone).
		First(&p).Error; err != nil {
		return nil, err
	}
	return &p, nil
}

func (r *callLogRepository) UpdateParticipant(ctx context.Context, p *models.CallParticipant) error {
	return r.db.WithContext(ctx).Save(p).Error
}

func (r *callLogRepository) UpdateParticipantsLeftAt(ctx context.Context, callLogID uuid.UUID, leftAt time.Time) error {
	return r.db.WithContext(ctx).
		Model(&models.CallParticipant{}).
		Where("call_log_id = ? AND join_status = ?", callLogID, "joined").
		Update("left_at", leftAt).
		Error
}

// UpdateParticipantJoin marks a participant (by call_log_id + phone) joined at
// the given time. Idempotent: re-running with the same values is a no-op write.
func (r *callLogRepository) UpdateParticipantJoin(ctx context.Context, callLogID uuid.UUID, phone, joinStatus string, joinedAt *time.Time) error {
	return r.db.WithContext(ctx).
		Model(&models.CallParticipant{}).
		Where("call_log_id = ? AND phone_number = ?", callLogID, phone).
		Updates(map[string]interface{}{"join_status": joinStatus, "joined_at": joinedAt}).Error
}

// Attachments

func (r *callLogRepository) CreateAttachment(ctx context.Context, attachment *models.CallLogAttachment) error {
	return r.db.WithContext(ctx).Create(attachment).Error
}

func (r *callLogRepository) FindAttachmentByID(ctx context.Context, id uuid.UUID) (*models.CallLogAttachment, error) {
	var attachment models.CallLogAttachment
	err := r.db.WithContext(ctx).
		Preload("UploadedBy").
		First(&attachment, "id = ?", id).Error
	if err != nil {
		return nil, err
	}
	return &attachment, nil
}
