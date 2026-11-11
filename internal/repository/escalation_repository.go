package repository

import (
	"context"
	"database/sql"

	"github.com/automax/backend/internal/models"
	"github.com/google/uuid"
	"gorm.io/gorm"
)

type EscalationRepository interface {
	// Create stores a new SLA breach notification record
	Create(ctx context.Context, e *models.EscalationSLA) error
	// Update updates an existing SLA breach notification record
	Update(ctx context.Context, e *models.EscalationSLA) error

	// GetAll returns all breach notification records (most recent first)
	GetAll(ctx context.Context) ([]models.EscalationSLA, error)

	// GetByIncidentID returns all breach notifications for a specific incident
	GetByIncidentID(ctx context.Context, incidentID uuid.UUID) ([]models.EscalationSLA, error)

	// SearchLogs returns a filtered page and the total matching count.
	SearchLogs(ctx context.Context, filter *models.EscalationLogFilter) ([]models.EscalationSLA, int64, error)

	// SearchLogsAll returns all filtered rows for export.
	SearchLogsAll(ctx context.Context, filter *models.EscalationLogFilter) ([]models.EscalationSLA, error)

	// GetPreviousFiredLevels returns the previous fired step for each escalation row.
	GetPreviousFiredLevels(ctx context.Context, rowIDs []uuid.UUID) (map[uuid.UUID]*int, error)

	// HasBeenNotified checks whether a user was already notified for the given incident+state
	// combination (prevents duplicate notifications within the same SLA breach period)
	HasBeenNotified(ctx context.Context, incidentID, stateID, userID uuid.UUID) (bool, error)

	// HasGlobalBreachNotification checks whether a global SLA deadline breach notification
	// (state_id IS NULL) has already been sent to the given user for this incident.
	HasGlobalBreachNotification(ctx context.Context, incidentID, userID uuid.UUID) (bool, error)

	// HasBatchNotificationToday checks whether a batch SLA grouped summary notification
	// (incident_id IS NULL AND state_id IS NULL) was already sent to this user today.
	// Used to prevent duplicate daily summary notifications.
	HasBatchNotificationToday(ctx context.Context, userID uuid.UUID) (bool, error)
}

type escalationRepository struct {
	db *gorm.DB
}

func NewEscalationRepository(db *gorm.DB) EscalationRepository {
	return &escalationRepository{db: db}
}

func (r *escalationRepository) Create(ctx context.Context, e *models.EscalationSLA) error {
	return r.db.WithContext(ctx).Create(e).Error
}

func (r *escalationRepository) Update(ctx context.Context, e *models.EscalationSLA) error {
	return r.db.WithContext(ctx).Save(e).Error
}

func (r *escalationRepository) GetAll(ctx context.Context) ([]models.EscalationSLA, error) {
	var records []models.EscalationSLA
	err := r.db.WithContext(ctx).
		Preload("Incident").
		Preload("State").
		Preload("Transition").
		Preload("NotifiedUser").
		Order("notified_at DESC").
		Find(&records).Error
	return records, err
}

func (r *escalationRepository) GetByIncidentID(ctx context.Context, incidentID uuid.UUID) ([]models.EscalationSLA, error) {
	var records []models.EscalationSLA
	err := r.db.WithContext(ctx).
		Preload("Incident").
		Preload("State").
		Preload("Transition").
		Preload("NotifiedUser").
		Where("incident_id = ?", incidentID).
		Order("notified_at DESC NULLS LAST").
		Find(&records).Error
	return records, err
}

// logQuery builds the filtered escalation-log query.
func (r *escalationRepository) logQuery(ctx context.Context, filter *models.EscalationLogFilter) *gorm.DB {
	// Exclude batch digests, which are not incident escalations.
	q := r.db.WithContext(ctx).
		Model(&models.EscalationSLA{}).
		Where("escalation_slas.incident_id IS NOT NULL").
		Joins("LEFT JOIN incidents i ON i.id = escalation_slas.incident_id").
		Joins("LEFT JOIN users u ON u.id = escalation_slas.notified_user_id").
		Joins("LEFT JOIN escalation_policy_steps eps ON eps.id = escalation_slas.escalation_policy_step_id")

	if filter.IncidentID != nil {
		q = q.Where("escalation_slas.incident_id = ?", *filter.IncidentID)
	}
	if filter.IncidentNumber != "" {
		q = q.Where("i.incident_number ILIKE ?", "%"+filter.IncidentNumber+"%")
	}
	if filter.Level != nil {
		q = q.Where("eps.step_order = ?", *filter.Level)
	}
	if filter.ClassificationID != nil {
		q = q.Where("i.classification_id = ?", *filter.ClassificationID)
	}
	if filter.DateFrom != nil {
		q = q.Where("COALESCE(escalation_slas.notified_at, escalation_slas.created_at) >= ?", *filter.DateFrom)
	}
	if filter.DateTo != nil {
		q = q.Where("COALESCE(escalation_slas.notified_at, escalation_slas.created_at) <= ?", *filter.DateTo)
	}
	if filter.Person != "" {
		like := "%" + filter.Person + "%"
		q = q.Where(
			`(u.first_name ILIKE ? OR u.last_name ILIKE ? OR u.email ILIKE ? OR escalation_slas.email ILIKE ? OR escalation_slas.phone ILIKE ?)`,
			like, like, like, like, like,
		)
	}
	if filter.EscalationType != "" {
		// Identify legacy global rows by their empty state and step.
		switch filter.EscalationType {
		case "global_sla":
			q = q.Where(
				"(escalation_slas.escalation_type = ? OR (escalation_slas.state_id IS NULL AND escalation_slas.escalation_policy_step_id IS NULL))",
				"global_sla",
			)
		case "state_sla":
			q = q.Where("escalation_slas.escalation_type = ?", "state_sla").
				Where("NOT (escalation_slas.state_id IS NULL AND escalation_slas.escalation_policy_step_id IS NULL)")
		default:
			q = q.Where("escalation_slas.escalation_type = ?", filter.EscalationType)
		}
	}

	return q
}

func (r *escalationRepository) SearchLogs(ctx context.Context, filter *models.EscalationLogFilter) ([]models.EscalationSLA, int64, error) {
	if filter == nil {
		filter = &models.EscalationLogFilter{}
	}

	var total int64
	if err := r.logQuery(ctx, filter).Count(&total).Error; err != nil {
		return nil, 0, err
	}

	page := filter.Page
	if page < 1 {
		page = 1
	}
	limit := filter.Limit
	if limit < 1 {
		limit = 10
	}
	if limit > 100 {
		limit = 100
	}

	var records []models.EscalationSLA
	err := r.logQuery(ctx, filter).
		Preload("Incident").
		Preload("Incident.Classification").
		Preload("Incident.CurrentState").
		Preload("State").
		Preload("Transition").
		Preload("NotifiedUser").
		Preload("EscalationPolicyStep").
		Order("COALESCE(escalation_slas.notified_at, escalation_slas.created_at) DESC, escalation_slas.created_at DESC, escalation_slas.id").
		Offset((page - 1) * limit).
		Limit(limit).
		Find(&records).Error
	if err != nil {
		return nil, 0, err
	}
	return records, total, nil
}

func (r *escalationRepository) SearchLogsAll(ctx context.Context, filter *models.EscalationLogFilter) ([]models.EscalationSLA, error) {
	if filter == nil {
		filter = &models.EscalationLogFilter{}
	}

	var records []models.EscalationSLA
	err := r.logQuery(ctx, filter).
		Preload("Incident").
		Preload("Incident.Classification").
		Preload("Incident.CurrentState").
		Preload("State").
		Preload("Transition").
		Preload("NotifiedUser").
		Preload("EscalationPolicyStep").
		Order("COALESCE(escalation_slas.notified_at, escalation_slas.created_at) DESC, escalation_slas.created_at DESC, escalation_slas.id").
		Find(&records).Error
	return records, err
}

func (r *escalationRepository) GetPreviousFiredLevels(ctx context.Context, rowIDs []uuid.UUID) (map[uuid.UUID]*int, error) {
	levels := make(map[uuid.UUID]*int, len(rowIDs))
	const batchSize = 1000
	for start := 0; start < len(rowIDs); start += batchSize {
		end := start + batchSize
		if end > len(rowIDs) {
			end = len(rowIDs)
		}
		if err := r.loadPreviousFiredLevels(ctx, rowIDs[start:end], levels); err != nil {
			return nil, err
		}
	}
	return levels, nil
}

func (r *escalationRepository) loadPreviousFiredLevels(ctx context.Context, rowIDs []uuid.UUID, levels map[uuid.UUID]*int) error {
	if len(rowIDs) == 0 {
		return nil
	}

	var rows []struct {
		ID           uuid.UUID     `gorm:"column:id"`
		PreviousStep sql.NullInt64 `gorm:"column:previous_step"`
	}
	err := r.db.WithContext(ctx).Raw(`
		SELECT cur.id AS id,
		       (
		           SELECT MAX(prev_step.step_order)
		           FROM escalation_slas prev
		           JOIN escalation_policy_steps prev_step
		             ON prev_step.id = prev.escalation_policy_step_id
		           WHERE prev.incident_id = cur.incident_id
		             AND prev.state_id IS NOT DISTINCT FROM cur.state_id
		             AND prev_step.policy_id = cur_step.policy_id
		             AND COALESCE(prev.notified_at, prev.created_at) < COALESCE(cur.notified_at, cur.created_at)
		             AND prev_step.step_order < cur_step.step_order
		       ) AS previous_step
		FROM escalation_slas cur
		JOIN escalation_policy_steps cur_step
		  ON cur_step.id = cur.escalation_policy_step_id
		WHERE cur.id IN ?
		  AND cur.incident_id IS NOT NULL
	`, rowIDs).Scan(&rows).Error
	if err != nil {
		return err
	}

	for _, row := range rows {
		if !row.PreviousStep.Valid {
			continue
		}
		level := int(row.PreviousStep.Int64)
		levels[row.ID] = &level
	}
	return nil
}

func (r *escalationRepository) HasBeenNotified(ctx context.Context, incidentID, stateID, userID uuid.UUID) (bool, error) {
	var count int64
	err := r.db.WithContext(ctx).
		Model(&models.EscalationSLA{}).
		Where("incident_id = ? AND state_id = ? AND notified_user_id = ?",
			incidentID, stateID, userID).
		Count(&count).Error
	return count > 0, err
}

func (r *escalationRepository) HasGlobalBreachNotification(ctx context.Context, incidentID, userID uuid.UUID) (bool, error) {
	var count int64
	err := r.db.WithContext(ctx).
		Model(&models.EscalationSLA{}).
		Where("incident_id = ? AND state_id IS NULL AND notified_user_id = ?", incidentID, userID).
		Count(&count).Error
	return count > 0, err
}

// HasBatchNotificationToday returns true when a batch grouped summary
// (incident_id IS NULL AND state_id IS NULL) was already sent to this user
// today (on or after the current calendar day's midnight).
func (r *escalationRepository) HasBatchNotificationToday(ctx context.Context, userID uuid.UUID) (bool, error) {
	var count int64
	err := r.db.WithContext(ctx).
		Model(&models.EscalationSLA{}).
		Where("incident_id IS NULL AND state_id IS NULL AND notified_user_id = ? AND notified_at >= CURRENT_DATE", userID).
		Count(&count).Error
	return count > 0, err
}
