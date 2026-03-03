package repository

import (
	"context"
	"encoding/json"

	"github.com/automax/backend/internal/models"
	"github.com/google/uuid"
	"gorm.io/gorm"
	"fmt"
)

type RejectionLogRepository interface {
	Create(ctx context.Context, log *models.IncidentRejectionLog) error
	GetByIncident(ctx context.Context, incidentID uuid.UUID) ([]models.IncidentRejectionLog, error)
	CountByIncident(ctx context.Context, incidentID uuid.UUID) (int, error)
	List(ctx context.Context, filter *models.IncidentRejectionLogFilter) ([]models.IncidentRejectionLog, int64, error)
	// GetLastTransitionIntoState finds the most recent transition that moved an incident INTO a given state.
	// Used to calculate ReceivedAt (when the incident was received in the current state before rejection).
	GetLastTransitionIntoState(ctx context.Context, incidentID uuid.UUID, stateID uuid.UUID) (*models.IncidentTransitionHistory, error)
	// ExecuteRejectionLogQuery executes a report query against the rejection log table.
	ExecuteRejectionLogQuery(ctx context.Context, filters []models.ReportFilterConfig, sorting *models.ReportSortConfig, page, limit int) ([]map[string]interface{}, int64, error)
}

type rejectionLogRepository struct {
	db *gorm.DB
}

func NewRejectionLogRepository(db *gorm.DB) RejectionLogRepository {
	return &rejectionLogRepository{db: db}
}

func (r *rejectionLogRepository) Create(ctx context.Context, log *models.IncidentRejectionLog) error {
	return r.db.WithContext(ctx).Create(log).Error
}

func (r *rejectionLogRepository) GetByIncident(ctx context.Context, incidentID uuid.UUID) ([]models.IncidentRejectionLog, error) {
	var logs []models.IncidentRejectionLog
	err := r.db.WithContext(ctx).
		Preload("FromState").
		Preload("ToState").
		Preload("RejectedBy").
		Where("incident_id = ?", incidentID).
		Order("rejection_sequence ASC").
		Find(&logs).Error
	return logs, err
}

func (r *rejectionLogRepository) CountByIncident(ctx context.Context, incidentID uuid.UUID) (int, error) {
	var count int64
	err := r.db.WithContext(ctx).
		Model(&models.IncidentRejectionLog{}).
		Where("incident_id = ?", incidentID).
		Count(&count).Error
	return int(count), err
}

func (r *rejectionLogRepository) List(ctx context.Context, filter *models.IncidentRejectionLogFilter) ([]models.IncidentRejectionLog, int64, error) {
	var logs []models.IncidentRejectionLog
	var total int64

	query := r.db.WithContext(ctx).Model(&models.IncidentRejectionLog{})

	if filter.IncidentID != nil {
		query = query.Where("incident_id = ?", *filter.IncidentID)
	}
	if filter.RejectedByID != nil {
		query = query.Where("rejected_by_id = ?", *filter.RejectedByID)
	}
	if filter.RecordType != nil && *filter.RecordType != "" {
		query = query.Where("record_type = ?", *filter.RecordType)
	}
	if filter.DepartmentID != nil {
		query = query.Where("department_id = ?", *filter.DepartmentID)
	}
	if filter.ClassificationID != nil {
		query = query.Where("classification_id = ?", *filter.ClassificationID)
	}
	if filter.SLAStatus != nil && *filter.SLAStatus != "" {
		query = query.Where("sla_status = ?", *filter.SLAStatus)
	}
	if filter.StartDate != nil {
		query = query.Where("rejected_at >= ?", *filter.StartDate)
	}
	if filter.EndDate != nil {
		query = query.Where("rejected_at <= ?", *filter.EndDate)
	}

	if err := query.Count(&total).Error; err != nil {
		return nil, 0, err
	}

	page := filter.Page
	if page < 1 {
		page = 1
	}
	limit := filter.Limit
	if limit < 1 || limit > 100 {
		limit = 20
	}
	offset := (page - 1) * limit

	err := query.
		Preload("FromState").
		Preload("ToState").
		Preload("RejectedBy").
		Order("rejected_at DESC").
		Offset(offset).
		Limit(limit).
		Find(&logs).Error

	return logs, total, err
}

func (r *rejectionLogRepository) GetLastTransitionIntoState(ctx context.Context, incidentID uuid.UUID, stateID uuid.UUID) (*models.IncidentTransitionHistory, error) {
	var history models.IncidentTransitionHistory
	err := r.db.WithContext(ctx).
		Where("incident_id = ? AND to_state_id = ?", incidentID, stateID).
		Order("transitioned_at DESC").
		First(&history).Error
	if err != nil {
		return nil, err
	}
	return &history, nil
}

func (r *rejectionLogRepository) ExecuteRejectionLogQuery(ctx context.Context, filters []models.ReportFilterConfig, sorting *models.ReportSortConfig, page, limit int) ([]map[string]interface{}, int64, error) {
	var total int64

	query := r.db.WithContext(ctx).Model(&models.IncidentRejectionLog{})
	query = r.applyFilters(query, filters)

	if err := query.Count(&total).Error; err != nil {
		return nil, 0, err
	}

	// Apply sorting
	if sorting != nil && sorting.Field != "" {
		dir := "ASC"
		if sorting.Direction == "desc" {
			dir = "DESC"
		}
		query = query.Order("incident_rejection_logs." + sorting.Field + " " + dir)
	} else {
		query = query.Order("incident_rejection_logs.rejected_at DESC")
	}

	offset := (page - 1) * limit
	rows, err := query.
		Select("incident_rejection_logs.*, "+
			"users.username as rejected_by_username_join, "+
			"users.first_name as rejected_by_first_name, "+
			"users.last_name as rejected_by_last_name, "+
			"from_states.name as from_state_name, "+
			"to_states.name as to_state_name, "+
			"departments.name as department_name, "+
			"classifications.name as classification_name").
		Joins("LEFT JOIN users ON incident_rejection_logs.rejected_by_id = users.id").
		Joins("LEFT JOIN workflow_states AS from_states ON incident_rejection_logs.from_state_id = from_states.id").
		Joins("LEFT JOIN workflow_states AS to_states ON incident_rejection_logs.to_state_id = to_states.id").
		Joins("LEFT JOIN departments ON incident_rejection_logs.department_id = departments.id").
		Joins("LEFT JOIN classifications ON incident_rejection_logs.classification_id = classifications.id").
		Offset(offset).
		Limit(limit).
		Rows()

	if err != nil {
		return nil, 0, err
	}
	defer rows.Close()

	cols, _ := rows.Columns()
	var results []map[string]interface{}

	for rows.Next() {
		columns := make([]interface{}, len(cols))
		columnPointers := make([]interface{}, len(cols))
		for i := range columns {
			columnPointers[i] = &columns[i]
		}
		if err := rows.Scan(columnPointers...); err != nil {
			continue
		}

		row := make(map[string]interface{})
		for i, colName := range cols {
			val := columns[i]
			if b, ok := val.([]byte); ok {
				row[colName] = string(b)
			} else {
				row[colName] = val
			}
		}

		// Convenience computed fields
		if v, ok := row["from_state_name"]; ok {
			row["from_state.name"] = v
		}
		if v, ok := row["to_state_name"]; ok {
			row["to_state.name"] = v
		}
		if v, ok := row["department_name"]; ok {
			row["department.name"] = v
		}
		if v, ok := row["classification_name"]; ok {
			row["classification.name"] = v
		}
		firstName, _ := row["rejected_by_first_name"].(string)
		lastName, _ := row["rejected_by_last_name"].(string)
		fullName := firstName
		if lastName != "" {
			if fullName != "" {
				fullName += " "
			}
			fullName += lastName
		}
		row["rejected_by.full_name"] = fullName

		// Parse roles snapshot to array for BI-friendly output
		if rolesJSON, ok := row["rejected_by_roles_snapshot"].(string); ok && rolesJSON != "" {
			var roles []string
			if json.Unmarshal([]byte(rolesJSON), &roles) == nil {
				row["rejected_by_roles"] = roles
			}
		}

		results = append(results, row)
	}

	return results, total, nil
}

// applyFilters applies ReportFilterConfig slice to the query (same pattern as report_repository).
func (r *rejectionLogRepository) applyFilters(query *gorm.DB, filters []models.ReportFilterConfig) *gorm.DB {
	for _, filter := range filters {
		if filter.Field == "" || filter.Value == "" {
			continue
		}
		col := "incident_rejection_logs." + filter.Field
		switch filter.Operator {
		case "eq":
			query = query.Where(col+" = ?", filter.Value)
		case "neq":
			query = query.Where(col+" != ?", filter.Value)
		// case "contains":
		// 	query = query.Where(col+" ILIKE ?", "%"+filter.Value+"%")
		case "contains":
			strVal := fmt.Sprintf("%v", filter.Value)
			query = query.Where(col+" ILIKE ?", "%"+strVal+"%")
		case "gt":
			query = query.Where(col+" > ?", filter.Value)
		case "lt":
			query = query.Where(col+" < ?", filter.Value)
		case "gte":
			query = query.Where(col+" >= ?", filter.Value)
		case "lte":
			query = query.Where(col+" <= ?", filter.Value)
		}
	}
	return query
}
