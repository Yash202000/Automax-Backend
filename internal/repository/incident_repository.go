package repository

import (
	"context"
	"errors"
	"fmt"
	"log"
	"time"

	"github.com/automax/backend/internal/models"
	"github.com/google/uuid"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

var ErrVersionMismatch = errors.New("incident was modified by another user")

type IncidentRepository interface {
	// Incident CRUD
	Create(ctx context.Context, incident *models.Incident) error
	FindByID(ctx context.Context, id uuid.UUID) (*models.Incident, error)
	FindByIDWithRelations(ctx context.Context, id uuid.UUID) (*models.Incident, error)
	FindByIncidentNumber(ctx context.Context, number string) (*models.Incident, error)
	FindByIDs(ctx context.Context, ids []uuid.UUID) ([]models.Incident, error)
	List(ctx context.Context, filter *models.IncidentFilter) ([]models.Incident, int64, error)
	Update(ctx context.Context, incident *models.Incident) error
	UpdateFields(ctx context.Context, id uuid.UUID, updates map[string]interface{}) error
	UpdateFieldsWithVersion(ctx context.Context, id uuid.UUID, updates map[string]interface{}, expectedVersion int) error
	Delete(ctx context.Context, id uuid.UUID) error
	WithTx(tx *gorm.DB) IncidentRepository
	LockForUpdate(ctx context.Context, tx *gorm.DB, id uuid.UUID) (*models.Incident, error)
	FindUserOpenIncidentsForDuplicateCheck(ctx context.Context, reporterID uuid.UUID) ([]models.Incident, error)
	FindByIDWithLast6DigitValidation(ctx context.Context, req *models.IncidentUpdateIVRRequest) (*models.Incident, error)
	// Incident number generation
	GenerateIncidentNumber(ctx context.Context) (string, error)
	GenerateRequestNumber(ctx context.Context) (string, error)
	GenerateComplaintNumber(ctx context.Context) (string, error)
	GenerateQueryNumber(ctx context.Context) (string, error)

	// State transitions
	UpdateState(ctx context.Context, incidentID, newStateID uuid.UUID) error
	CreateTransitionHistory(ctx context.Context, history *models.IncidentTransitionHistory) error
	GetTransitionHistory(ctx context.Context, incidentID uuid.UUID) ([]models.IncidentTransitionHistory, error)

	// Comments
	CreateComment(ctx context.Context, comment *models.IncidentComment) error
	FindCommentByID(ctx context.Context, id uuid.UUID) (*models.IncidentComment, error)
	ListComments(ctx context.Context, incidentID uuid.UUID) ([]models.IncidentComment, error)
	UpdateComment(ctx context.Context, comment *models.IncidentComment) error
	DeleteComment(ctx context.Context, id uuid.UUID) error

	// Attachments
	CreateAttachment(ctx context.Context, attachment *models.IncidentAttachment) error
	FindAttachmentByID(ctx context.Context, id uuid.UUID) (*models.IncidentAttachment, error)
	ListAttachments(ctx context.Context, incidentID uuid.UUID) ([]models.IncidentAttachment, error)
	DeleteAttachment(ctx context.Context, id uuid.UUID) error
	LinkAttachmentsToTransition(ctx context.Context, attachmentIDs []uuid.UUID, transitionHistoryID uuid.UUID) error

	// Assignment
	AssignIncident(ctx context.Context, incidentID, assigneeID uuid.UUID) error
	SetAssignees(ctx context.Context, incidentID uuid.UUID, userIDs []uuid.UUID) error
	ClearAssignees(ctx context.Context, incidentID uuid.UUID) error
	SetLookupValues(ctx context.Context, incidentID uuid.UUID, lookupValues []models.LookupValue) error

	// Stats
	GetStats(ctx context.Context, filter *models.IncidentFilter) (*models.IncidentStatsResponse, error)
	GetStatsV2(ctx context.Context, filter *models.IncidentFilter) (*models.IncidentStatsResponseV2, error)
	GetPriorityCounts(ctx context.Context, filter *models.IncidentFilter) (map[string]int64, error)
	GetSLABreachedIncidents(ctx context.Context) ([]models.Incident, error)
	UpdateSLABreached(ctx context.Context, incidentID uuid.UUID, breached bool) error
	MarkSLABreached(ctx context.Context) (int64, error)
	GetBreachedByFilter(ctx context.Context, locationID uuid.UUID, classificationID uuid.UUID) ([]models.Incident, error)
	GetIncidentsExceedingStateSLA(ctx context.Context) ([]models.Incident, error)
	GetNewlyBreachedIncidents(ctx context.Context) ([]models.Incident, error)

	// User-specific queries
	GetAssignedToUser(ctx context.Context, userID uuid.UUID, recordType string, page, limit int) ([]models.Incident, int64, error)
	GetReportedByUser(ctx context.Context, userID uuid.UUID, recordType string, page, limit int) ([]models.Incident, int64, error)

	// Revisions
	CreateRevision(ctx context.Context, revision *models.IncidentRevision) error
	ListRevisions(ctx context.Context, filter *models.IncidentRevisionFilter) ([]models.IncidentRevision, int64, error)
	GetNextRevisionNumber(ctx context.Context, incidentID uuid.UUID) (int, error)

	// Feedback
	CreateFeedback(ctx context.Context, feedback *models.IncidentFeedback) error
	FindFeedbackByID(ctx context.Context, id uuid.UUID) (*models.IncidentFeedback, error)
	ListFeedback(ctx context.Context, incidentID uuid.UUID) ([]models.IncidentFeedback, error)
	ListAllFeedback(ctx context.Context) ([]models.IncidentFeedback, error)
	LinkFeedbackToTransition(ctx context.Context, feedbackID uuid.UUID, transitionHistoryID uuid.UUID) error

	// Notifications
	CreateNotification(ctx context.Context, notification *models.NotificationLog) error

	// Complaint-specific
	IncrementEvaluationCount(ctx context.Context, id uuid.UUID) error
}

type incidentRepository struct {
	db *gorm.DB
}

func NewIncidentRepository(db *gorm.DB) IncidentRepository {
	return &incidentRepository{db: db}
}

// Incident CRUD

func (r *incidentRepository) Create(ctx context.Context, incident *models.Incident) error {
	return r.db.WithContext(ctx).Create(incident).Error
}

func (r *incidentRepository) FindByID(ctx context.Context, id uuid.UUID) (*models.Incident, error) {
	var incident models.Incident
	err := r.db.WithContext(ctx).
		Preload("CurrentState").
		Preload("Workflow").
		First(&incident, "id = ?", id).Error
	if err != nil {
		return nil, err
	}
	return &incident, nil
}

func (r *incidentRepository) FindByIDWithRelations(ctx context.Context, id uuid.UUID) (*models.Incident, error) {
	var incident models.Incident
	err := r.db.WithContext(ctx).Session(&gorm.Session{}).
		Preload("Classification").
		Preload("Workflow").
		Preload("Workflow.States", func(db *gorm.DB) *gorm.DB {
			return db.Order("sort_order ASC")
		}).
		Preload("Workflow.Transitions").
		Preload("Workflow.Transitions.AllowedRoles").
		Preload("CurrentState").
		Preload("CurrentState.EditableRoles").
		Preload("Assignee").
		Preload("Assignees").
		Preload("Department").
		Preload("Location").
		Preload("LookupValues.Category").
		Preload("Reporter").
		Preload("SourceIncident").
		Preload("ConvertedRequest").
		Preload("Comments", func(db *gorm.DB) *gorm.DB {
			return db.Order("created_at DESC")
		}).
		Preload("Comments.Author").
		Preload("Attachments", func(db *gorm.DB) *gorm.DB {
			return db.Order("created_at DESC")
		}).
		Preload("Attachments.UploadedBy").
		Preload("TransitionHistory", func(db *gorm.DB) *gorm.DB {
			return db.Order("transitioned_at DESC")
		}).
		Preload("TransitionHistory.FromState").
		Preload("TransitionHistory.ToState").
		Preload("TransitionHistory.PerformedBy").
		Preload("TransitionHistory.Transition").
		First(&incident, "id = ?", id).Error
	if err != nil {
		return nil, err
	}
	return &incident, nil
}

func (r *incidentRepository) FindByIncidentNumber(ctx context.Context, number string) (*models.Incident, error) {
	var incident models.Incident
	err := r.db.WithContext(ctx).
		Preload("CurrentState").
		Where("incident_number = ?", number).
		First(&incident).Error
	if err != nil {
		return nil, err
	}
	return &incident, nil
}

func (r *incidentRepository) FindByIDs(ctx context.Context, ids []uuid.UUID) ([]models.Incident, error) {
	var incidents []models.Incident
	err := r.db.WithContext(ctx).
		Preload("CurrentState").
		Preload("Workflow").
		Preload("Classification").
		Preload("Assignee").
		Preload("Department").
		Preload("Location").
		Preload("Reporter").
		Where("id IN ?", ids).
		Find(&incidents).Error
	return incidents, err
}

func (r *incidentRepository) List(ctx context.Context, filter *models.IncidentFilter) ([]models.Incident, int64, error) {
	var incidents []models.Incident
	var total int64

	query := r.db.WithContext(ctx).Model(&models.Incident{})

	// Apply filters
	if len(filter.WorkflowID) != 0 {
		query = query.Where("workflow_id IN ?", filter.WorkflowID)
	}
	if len(filter.CurrentStateID) != 0 {
		query = query.Where("current_state_id IN ?", filter.CurrentStateID)
	}
	if len(filter.ClassificationID) != 0 {
		query = query.Where("classification_id IN ?", filter.ClassificationID)
	}
	if filter.Priority != nil {
		query = query.Where(`incidents.id IN (
			SELECT ilv.incident_id FROM incident_lookup_values ilv
			INNER JOIN lookup_values lv ON lv.id = ilv.lookup_value_id
			INNER JOIN lookup_categories lc ON lc.id = lv.category_id
			WHERE lc.code = 'PRIORITY' AND lv.sort_order = ?
		)`, *filter.Priority)
	}
	if len(filter.AssigneeID) != 0 {
		if len(filter.AssigneeID) == 1 {
			// Check both primary assignee and multiple assignees
			query = query.Where("assignee_id = ? OR id IN (SELECT incident_id FROM incident_assignees WHERE user_id = ?)", filter.AssigneeID[0], filter.AssigneeID[0])
		} else {
			// Multiple assignees
			query = query.Where("assignee_id IN ? ", filter.AssigneeID)
		}
	}
	if len(filter.DepartmentID) != 0 {
		query = query.Where("department_id IN ?", filter.DepartmentID)
	}
	// fix bleow filter for location and reporter to handle multiple values

	if len(filter.LocationID) != 0 {
		query = query.Where("location_id IN ?", filter.LocationID)
	}
	if len(filter.ReporterID) != 0 {
		query = query.Where("reporter_id IN ?", filter.ReporterID)
	}
	if filter.SLABreached != nil {
		query = query.Where("sla_breached = ?", *filter.SLABreached)
	}
	if filter.RecordType != nil {
		query = query.Where("record_type = ?", *filter.RecordType)
	}
	if filter.Channel != nil && *filter.Channel != "" {
		query = query.Where("channel = ?", *filter.Channel)
	}
	if filter.Source != nil && *filter.Source != "" {
		query = query.Where("source = ?", *filter.Source)
	}
	if filter.ConvertedToRequest != nil {
		if *filter.ConvertedToRequest {
			query = query.Where("converted_request_id IS NOT NULL")
		} else {
			query = query.Where("converted_request_id IS NULL")
		}
	}
	if filter.StartDate != nil {
		query = query.Where("created_at >= ?", *filter.StartDate)
	}
	if filter.EndDate != nil {
		query = query.Where("created_at <= ?", *filter.EndDate)
	}
	if filter.Search != "" {
		searchPattern := "%" + filter.Search + "%"
		query = query.Where("incident_number ILIKE ? OR title ILIKE ? OR description ILIKE ?", searchPattern, searchPattern, searchPattern)
	}
	if filter.CustomFieldKey != "" && filter.CustomFieldValue != "" {
		query = query.Where("NULLIF(custom_fields, '')::jsonb -> ? ->> 'value' ILIKE ?", filter.CustomFieldKey, "%"+filter.CustomFieldValue+"%")
	}
	if filter.TaskID != "" {
		query = query.Where("NULLIF(custom_fields, '')::jsonb -> 'lookup:TASK ID' ->> 'value' ILIKE ?", "%"+filter.TaskID+"%")
	}

	// Transition filters — JOIN only when needed
	if filter.TransitionID != nil || filter.FromStateID != nil || filter.ToStateID != nil {
		// Build a subquery to get matching incident IDs
		subQuery := r.db.WithContext(ctx).
			Table("incident_transition_histories ith").
			Select("DISTINCT ith.incident_id").
			Joins("JOIN workflow_transitions wt ON wt.id = ith.transition_id")

		if filter.TransitionID != nil {
			subQuery = subQuery.Where("wt.id = ?", *filter.TransitionID)
		}
		if filter.FromStateID != nil {
			subQuery = subQuery.Where("wt.from_state_id = ?", *filter.FromStateID)
		}
		if filter.ToStateID != nil {
			subQuery = subQuery.Where("wt.to_state_id = ?", *filter.ToStateID)
		}

		// Use IN subquery on main query — no JOIN, no DISTINCT needed
		query = query.Where("incidents.id IN (?)", subQuery)
	}

	// Single unified count — works for all cases
	if err := query.Count(&total).Error; err != nil {
		return nil, 0, err
	}
	// Apply pagination
	// Apply pagination
	if filter.Page < 1 {
		filter.Page = 1
	}
	if filter.Limit < 1 || filter.Limit > 100 {
		filter.Limit = 20
	}
	offset := (filter.Page - 1) * filter.Limit

	err := query.
		Preload("Classification").
		Preload("TransitionHistory").
		Preload("TransitionHistory.PerformedBy").
		Preload("Workflow").
		Preload("CurrentState").
		Preload("Assignee").
		Preload("Department").
		Preload("Location").
		Preload("LookupValues.Category").
		Preload("TransitionHistory.Transition").
		Order("created_at DESC").
		Offset(offset).
		Limit(filter.Limit).
		Find(&incidents).Error
	if err != nil {
		return nil, 0, err
	}

	return incidents, total, nil
}

func (r *incidentRepository) FindByIDWithLast6DigitValidation(
	ctx context.Context,
	req *models.IncidentUpdateIVRRequest,
) (*models.Incident, error) {

	var incident models.Incident

	err := r.db.WithContext(ctx).Debug().
		Preload("CurrentState").
		Preload("Workflow").
		Joins("JOIN users ON users.id = incidents.reporter_id").
		Where("incidents.id = ?", req.IncidentID).
		Where("RIGHT(users.phone, 6) = ?", req.LastPhoneDigits).
		First(&incident).Error

	if err != nil && !errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, err
	}

	return &incident, nil
}
func (r *incidentRepository) Update(ctx context.Context, incident *models.Incident) error {
	return r.db.WithContext(ctx).Save(incident).Error
}

func (r *incidentRepository) UpdateFields(ctx context.Context, id uuid.UUID, updates map[string]interface{}) error {
	return r.db.WithContext(ctx).Model(&models.Incident{}).Where("id = ?", id).Updates(updates).Error
}

func (r *incidentRepository) Delete(ctx context.Context, id uuid.UUID) error {
	return r.db.WithContext(ctx).Delete(&models.Incident{}, "id = ?", id).Error
}

// Incident number generation - uses MAX to get the highest sequence number

func (r *incidentRepository) GenerateIncidentNumber(ctx context.Context) (string, error) {
	year := time.Now().Year()
	prefix := fmt.Sprintf("INC-%d-", year)
	var maxNumber *string
	err := r.db.WithContext(ctx).Unscoped().Model(&models.Incident{}).
		Select("MAX(incident_number)").
		Where("incident_number LIKE ? ", prefix+"%").
		Scan(&maxNumber).Error
	if err != nil {
		return "", err
	}
	nextSeq := 1
	if maxNumber != nil && *maxNumber != "" {
		// Extract sequence number from "INC-2026-000021"
		var seq int
		fmt.Sscanf(*maxNumber, "INC-%d-%d", &year, &seq)
		nextSeq = seq + 1
	}
	return fmt.Sprintf("INC-%d-%06d", year, nextSeq), nil
}

func (r *incidentRepository) GenerateRequestNumber(ctx context.Context) (string, error) {
	year := time.Now().Year()
	prefix := fmt.Sprintf("REQ-%d-", year)
	var maxNumber *string
	// Unscoped() to include soft-deleted rows in the MAX calculation
	err := r.db.WithContext(ctx).Unscoped().Model(&models.Incident{}).
		Select("MAX(incident_number)").Where("incident_number LIKE ?", prefix+"%").
		Scan(&maxNumber).Error
	if err != nil {
		return "", err
	}
	nextSeq := 1
	if maxNumber != nil && *maxNumber != "" {
		var seq int
		fmt.Sscanf(*maxNumber, "REQ-%d-%d", &year, &seq)
		nextSeq = seq + 1
	}
	return fmt.Sprintf("REQ-%d-%06d", year, nextSeq), nil
}

func (r *incidentRepository) GenerateComplaintNumber(ctx context.Context) (string, error) {
	year := time.Now().Year()
	prefix := fmt.Sprintf("COMP-%d-", year)
	var maxNumber *string
	err := r.db.WithContext(ctx).Unscoped().Model(&models.Incident{}).
		Select("MAX(incident_number)").
		Where("incident_number LIKE ?", prefix+"%").
		Scan(&maxNumber).Error
	if err != nil {
		return "", err
	}
	nextSeq := 1
	if maxNumber != nil && *maxNumber != "" {
		var seq int
		fmt.Sscanf(*maxNumber, "COMP-%d-%d", &year, &seq)
		nextSeq = seq + 1
	}
	return fmt.Sprintf("COMP-%d-%06d", year, nextSeq), nil
}

func (r *incidentRepository) GenerateQueryNumber(ctx context.Context) (string, error) {
	year := time.Now().Year()
	prefix := fmt.Sprintf("QRY-%d-", year)
	var maxNumber *string
	err := r.db.WithContext(ctx).Unscoped().Model(&models.Incident{}).
		Select("MAX(incident_number)").
		Where("incident_number LIKE ?", prefix+"%").
		Scan(&maxNumber).Error
	if err != nil {
		return "", err
	}
	nextSeq := 1
	if maxNumber != nil && *maxNumber != "" {
		var seq int
		fmt.Sscanf(*maxNumber, "QRY-%d-%d", &year, &seq)
		nextSeq = seq + 1
	}
	return fmt.Sprintf("QRY-%d-%06d", year, nextSeq), nil
}

// State transitions

func (r *incidentRepository) UpdateState(ctx context.Context, incidentID, newStateID uuid.UUID) error {
	return r.db.WithContext(ctx).
		Model(&models.Incident{}).
		Where("id = ?", incidentID).
		Update("current_state_id", newStateID).Error
}

func (r *incidentRepository) CreateTransitionHistory(ctx context.Context, history *models.IncidentTransitionHistory) error {
	return r.db.WithContext(ctx).Create(history).Error
}

func (r *incidentRepository) GetTransitionHistory(ctx context.Context, incidentID uuid.UUID) ([]models.IncidentTransitionHistory, error) {
	var history []models.IncidentTransitionHistory
	err := r.db.WithContext(ctx).Debug().
		Preload("Transition").
		Preload("FromState").
		Preload("ToState").
		Preload("PerformedBy").
		Preload("PerformedBy.Departments").
		Preload("Feedbacks").
		Preload("Feedbacks.CreatedBy").
		Where("incident_id = ?", incidentID).
		Order("transitioned_at DESC").
		Find(&history).Error
	return history, err
}

// Comments

func (r *incidentRepository) CreateComment(ctx context.Context, comment *models.IncidentComment) error {
	return r.db.WithContext(ctx).Create(comment).Error
}

func (r *incidentRepository) FindCommentByID(ctx context.Context, id uuid.UUID) (*models.IncidentComment, error) {
	var comment models.IncidentComment
	err := r.db.WithContext(ctx).
		Preload("Author").
		First(&comment, "id = ?", id).Error
	if err != nil {
		return nil, err
	}
	return &comment, nil
}

func (r *incidentRepository) ListComments(ctx context.Context, incidentID uuid.UUID) ([]models.IncidentComment, error) {
	var comments []models.IncidentComment
	err := r.db.WithContext(ctx).
		Preload("Author").
		Where("incident_id = ?", incidentID).
		Order("created_at DESC").
		Find(&comments).Error
	return comments, err
}

func (r *incidentRepository) UpdateComment(ctx context.Context, comment *models.IncidentComment) error {
	return r.db.WithContext(ctx).Save(comment).Error
}

func (r *incidentRepository) DeleteComment(ctx context.Context, id uuid.UUID) error {
	return r.db.WithContext(ctx).Delete(&models.IncidentComment{}, "id = ?", id).Error
}

// Attachments

func (r *incidentRepository) CreateAttachment(ctx context.Context, attachment *models.IncidentAttachment) error {
	return r.db.WithContext(ctx).Create(attachment).Error
}

func (r *incidentRepository) FindAttachmentByID(ctx context.Context, id uuid.UUID) (*models.IncidentAttachment, error) {
	var attachment models.IncidentAttachment
	err := r.db.WithContext(ctx).
		Preload("UploadedBy").
		First(&attachment, "id = ?", id).Error
	if err != nil {
		return nil, err
	}
	return &attachment, nil
}

func (r *incidentRepository) ListAttachments(ctx context.Context, incidentID uuid.UUID) ([]models.IncidentAttachment, error) {
	var attachments []models.IncidentAttachment
	err := r.db.WithContext(ctx).
		Preload("UploadedBy").
		Preload("UploadedBy.Roles").
		Preload("UploadedBy.Departments").
		Where("incident_id = ?", incidentID).
		Order("created_at DESC").
		Find(&attachments).Error
	return attachments, err
}

func (r *incidentRepository) DeleteAttachment(ctx context.Context, id uuid.UUID) error {
	return r.db.WithContext(ctx).Delete(&models.IncidentAttachment{}, "id = ?", id).Error
}

func (r *incidentRepository) LinkAttachmentsToTransition(ctx context.Context, attachmentIDs []uuid.UUID, transitionHistoryID uuid.UUID) error {
	return r.db.WithContext(ctx).
		Model(&models.IncidentAttachment{}).
		Where("id IN ?", attachmentIDs).
		Update("transition_history_id", transitionHistoryID).Error
}

// Assignment

func (r *incidentRepository) AssignIncident(ctx context.Context, incidentID, assigneeID uuid.UUID) error {
	return r.db.WithContext(ctx).
		Model(&models.Incident{}).
		Where("id = ?", incidentID).
		Update("assignee_id", assigneeID).Error
}

func (r *incidentRepository) SetAssignees(ctx context.Context, incidentID uuid.UUID, userIDs []uuid.UUID) error {
	// Get the incident
	var incident models.Incident
	if err := r.db.WithContext(ctx).First(&incident, "id = ?", incidentID).Error; err != nil {
		return err
	}

	// Get users to assign
	var users []models.User
	if len(userIDs) > 0 {
		if err := r.db.WithContext(ctx).Where("id IN ?", userIDs).Find(&users).Error; err != nil {
			return err
		}
	}

	// Replace the assignees (this clears existing and sets new ones)
	return r.db.WithContext(ctx).Model(&incident).Association("Assignees").Replace(users)
}

func (r *incidentRepository) ClearAssignees(ctx context.Context, incidentID uuid.UUID) error {
	var incident models.Incident
	if err := r.db.WithContext(ctx).First(&incident, "id = ?", incidentID).Error; err != nil {
		return err
	}
	return r.db.WithContext(ctx).Model(&incident).Association("Assignees").Clear()
}

func (r *incidentRepository) SetLookupValues(ctx context.Context, incidentID uuid.UUID, lookupValues []models.LookupValue) error {
	var incident models.Incident
	if err := r.db.WithContext(ctx).First(&incident, "id = ?", incidentID).Error; err != nil {
		return err
	}

	// First fetch the actual LookupValue records from database
	var actualLookupValues []models.LookupValue
	lookupIDs := make([]uuid.UUID, len(lookupValues))
	for i, lv := range lookupValues {
		lookupIDs[i] = lv.ID
	}

	if err := r.db.WithContext(ctx).Where("id IN ?", lookupIDs).Find(&actualLookupValues).Error; err != nil {
		return err
	}

	return r.db.WithContext(ctx).Model(&incident).Association("LookupValues").Replace(actualLookupValues)
}

// Stats

func (r *incidentRepository) GetStats(ctx context.Context, filter *models.IncidentFilter) (*models.IncidentStatsResponse, error) {

	stats := &models.IncidentStatsResponse{
		ByState: make(map[string]int64),
	}

	baseQuery := r.db.WithContext(ctx).Model(&models.Incident{})

	// Apply filters if provided
	if filter != nil {

		if filter.RecordType != nil && *filter.RecordType != "" {
			baseQuery = baseQuery.Where("record_type = ?", *filter.RecordType)
		}

		if len(filter.WorkflowID) != 0 {
			baseQuery = baseQuery.Where("workflow_id IN ?", filter.WorkflowID)
		}

		if len(filter.DepartmentID) != 0 {
			baseQuery = baseQuery.Where("department_id IN ?", filter.DepartmentID)
		}

		if len(filter.AssigneeID) != 0 {
			baseQuery = baseQuery.Where(
				"assignee_id IN ? OR id IN (SELECT incident_id FROM incident_assignees WHERE user_id IN ?)",
				filter.AssigneeID,
				filter.AssigneeID,
			)
		}

		// Created by Me filter
		if filter.FilterType == "created" {
			baseQuery = baseQuery.Where("reporter_id = ?", filter.UserID)
		}

		// Assigned to Me filter
		if filter.FilterType == "assigned" {
			baseQuery = baseQuery.Where(`
				assignee_id = ? OR id IN (
					SELECT incident_id FROM incident_assignees WHERE user_id = ?
				)
			`, filter.UserID, filter.UserID)
		}
	}

	// Total count
	if err := baseQuery.Count(&stats.Total).Error; err != nil {
		return nil, err
	}

	// SLA breached count
	slaQuery := r.db.WithContext(ctx).
		Model(&models.Incident{}).
		Where("sla_breached = ?", true)

	if filter != nil {

		if filter.RecordType != nil && *filter.RecordType != "" {
			slaQuery = slaQuery.Where("record_type = ?", *filter.RecordType)
		}

		// Created by Me filter
		if filter.FilterType == "created" {
			slaQuery = slaQuery.Where("reporter_id = ?", filter.UserID)
		}
		// Assigned to Me filter
		if filter.FilterType == "assigned" {
			slaQuery = slaQuery.Where(`
				assignee_id = ? OR id IN (
					SELECT incident_id FROM incident_assignees WHERE user_id = ?
				)
			`, filter.UserID, filter.UserID)
		}
	}

	if err := slaQuery.Count(&stats.SLABreached).Error; err != nil {
		return nil, err
	}

	// Counts by state
	type stateCount struct {
		StateID   uuid.UUID `gorm:"column:state_id"`
		StateName string    `gorm:"column:state_name"`
		StateCode string    `gorm:"column:state_code"`
		Count     int64     `gorm:"column:count"`
	}

	var stateCounts []stateCount

	stateQuery := r.db.WithContext(ctx).
		Model(&models.Incident{}).
		Select("workflow_states.id as state_id, workflow_states.name as state_name, workflow_states.code as state_code, count(*) as count").
		Joins("JOIN workflow_states ON workflow_states.id = incidents.current_state_id")

	if filter != nil {

		if filter.RecordType != nil && *filter.RecordType != "" {
			stateQuery = stateQuery.Where("incidents.record_type = ?", *filter.RecordType)
		}

		// Created by Me filter
		if filter.FilterType == "created" {
			stateQuery = stateQuery.Where("incidents.reporter_id = ?", filter.UserID)
		}
		// Assigned to Me filter
		if filter.FilterType == "assigned" {
			stateQuery = stateQuery.Where(`
				incidents.assignee_id = ? OR incidents.id IN (
					SELECT incident_id FROM incident_assignees WHERE user_id = ?
				)
			`, filter.UserID, filter.UserID)
		}

		// Filter by role visibility
		if len(filter.UserRoleIDs) > 0 {
			stateQuery = stateQuery.Where(`
				NOT EXISTS (
					SELECT 1 FROM state_viewable_roles 
					WHERE workflow_state_id = workflow_states.id
				)
				OR EXISTS (
					SELECT 1 FROM state_viewable_roles
					WHERE workflow_state_id = workflow_states.id 
					AND role_id IN ?
				)
			`, filter.UserRoleIDs)
		}
	}

	if err := stateQuery.
		Group("workflow_states.id, workflow_states.name").
		Scan(&stateCounts).Error; err != nil {
		return nil, err
	}

	stats.ByStateDetails = make([]models.StateStatDetail, 0, len(stateCounts))

	for _, sc := range stateCounts {

		stats.ByState[sc.StateName] = sc.Count

		stats.ByStateDetails = append(stats.ByStateDetails, models.StateStatDetail{
			ID:    sc.StateID,
			Name:  sc.StateName,
			Count: sc.Count,
		})

		if sc.StateCode == "resolved" {
			stats.Resolved += sc.Count
		}
	}

	// Count by state_type
	type stateTypeCount struct {
		StateType string `gorm:"column:state_type"`
		Count     int64  `gorm:"column:count"`
	}

	var stateTypeCounts []stateTypeCount

	stateTypeQuery := r.db.WithContext(ctx).
		Model(&models.Incident{}).
		Select("workflow_states.state_type as state_type, count(*) as count").
		Joins("JOIN workflow_states ON workflow_states.id = incidents.current_state_id")

	if filter != nil {

		if filter.RecordType != nil && *filter.RecordType != "" {
			stateTypeQuery = stateTypeQuery.Where("incidents.record_type = ?", *filter.RecordType)
		}

		// Created by Me filter
		if filter.FilterType == "created" {
			stateTypeQuery = stateTypeQuery.Where("incidents.reporter_id = ?", filter.UserID)
		}
		// Assigned to Me Filter
		if filter.FilterType == "assigned" {
			stateTypeQuery = stateTypeQuery.Where(`
				incidents.assignee_id = ? OR incidents.id IN (
					SELECT incident_id FROM incident_assignees WHERE user_id = ?
				)
			`, filter.UserID, filter.UserID)
		}
	}

	if err := stateTypeQuery.
		Group("workflow_states.state_type").
		Scan(&stateTypeCounts).Error; err != nil {
		return nil, err
	}

	for _, stc := range stateTypeCounts {
		switch stc.StateType {
		case "initial":
			stats.Open = stc.Count
		case "normal":
			stats.InProgress = stc.Count
		case "terminal":
			stats.Closed = stc.Count
		}
	}

	return stats, nil
}

//stats api v2 (After all fixes  will deprecated Old api)

func (r *incidentRepository) GetStatsV2(ctx context.Context, filter *models.IncidentFilter) (*models.IncidentStatsResponseV2, error) {

	stats := &models.IncidentStatsResponseV2{}

	isAdmin := filter != nil && filter.IsAdmin

	// only shows workflows matching the requested record_type), WorkflowID, DepartmentID,
	applySharedFilters := func(q *gorm.DB) *gorm.DB {
		if filter == nil {
			return q
		}
		if filter.RecordType != nil && *filter.RecordType != "" {
			//q = q.Where("incidents.record_type = ?", *filter.RecordType) // all Workflow counts
			q = q.Where(
				"incidents.record_type = ? AND incidents.workflow_id IN (SELECT id FROM workflows WHERE record_type = ? AND deleted_at IS NULL)",
				*filter.RecordType, *filter.RecordType)
		}
		if len(filter.WorkflowID) > 0 {
			q = q.Where("incidents.workflow_id IN ?", filter.WorkflowID)
		}
		if len(filter.DepartmentID) > 0 {
			q = q.Where("incidents.department_id IN ?", filter.DepartmentID)
		}
		if len(filter.AssigneeID) > 0 {
			q = q.Where(`incidents.assignee_id IN ? OR incidents.id IN (SELECT incident_id FROM incident_assignees WHERE user_id IN ?)`,
				filter.AssigneeID, filter.AssigneeID)
		}
		return q
	}

	// applyBaseFilters: shared filters + FilterType for ALL users
	applyBaseFilters := func(q *gorm.DB) *gorm.DB {
		q = applySharedFilters(q)
		if filter == nil {
			return q
		}
		if filter.FilterType == "created" {
			q = q.Where("incidents.reporter_id = ?", filter.UserID)
		}
		if filter.FilterType == "assigned" {
			q = q.Where(`incidents.assignee_id = ? OR incidents.id IN (SELECT incident_id FROM incident_assignees WHERE user_id = ?)`,
				filter.UserID, filter.UserID)
		}
		return q
	}

	// applyCommonFilters: used only for stateQuery (WorkflowStats/ByStateDetails).
	// Non-admin → applies FilterType + role-visibility (permission-based).
	applyCommonFilters := func(q *gorm.DB) *gorm.DB {
		q = applySharedFilters(q)
		if filter == nil {
			return q
		}
		if !isAdmin {
			if filter.FilterType == "created" {
				q = q.Where("incidents.reporter_id = ?", filter.UserID)
			}
			if filter.FilterType == "assigned" {
				q = q.Where(`incidents.assignee_id = ? OR incidents.id IN (SELECT incident_id FROM incident_assignees WHERE user_id = ?)`,
					filter.UserID, filter.UserID)
			}
			isPersonalFilter := filter.FilterType == "assigned" || filter.FilterType == "created"
			if len(filter.UserRoleIDs) > 0 && !isPersonalFilter {
				q = q.Where(`
					NOT EXISTS (SELECT 1 FROM state_viewable_roles WHERE workflow_state_id = incidents.current_state_id)
					OR EXISTS (SELECT 1 FROM state_viewable_roles WHERE workflow_state_id = incidents.current_state_id AND role_id IN ?)
				`, filter.UserRoleIDs)
			}
		}
		return q
	}

	//  Total
	baseQuery := applyBaseFilters(
		r.db.WithContext(ctx).Model(&models.Incident{}).
			Where("incidents.workflow_id IN (SELECT id FROM workflows WHERE deleted_at IS NULL)"),
	)
	if err := baseQuery.Count(&stats.Total).Error; err != nil {
		return nil, err
	}

	// SLA Breached
	slaQuery := applyBaseFilters(
		r.db.WithContext(ctx).Model(&models.Incident{}).
			Where("sla_breached = ? AND incidents.workflow_id IN (SELECT id FROM workflows WHERE deleted_at IS NULL)", true),
	)
	if err := slaQuery.Count(&stats.SLABreached).Error; err != nil {
		return nil, err
	}

	// Partial Close (incidents currently in a partial_close state)
	pcQuery := applyBaseFilters(
		r.db.WithContext(ctx).Model(&models.Incident{}).
			Joins("JOIN workflow_states ON workflow_states.id = incidents.current_state_id").
			Where("workflow_states.is_partial_close = ? AND incidents.workflow_id IN (SELECT id FROM workflows WHERE deleted_at IS NULL)", true),
	)
	if err := pcQuery.Count(&stats.PartialClose).Error; err != nil {
		// Non-fatal: column may not exist yet on older schema — partial_close stays 0
		log.Printf("[GetStatsV2] partial_close count query failed (schema may need migration): %v", err)
	}

	// Per-state counts (workflow_stats + by_state)
	type stateCount struct {
		WorkflowID   uuid.UUID `gorm:"column:workflow_id"`
		WorkflowName string    `gorm:"column:workflow_name"`
		StateID      uuid.UUID `gorm:"column:state_id"`
		StateName    string    `gorm:"column:state_name"`
		StateCode    string    `gorm:"column:state_code"`
		StateType    string    `gorm:"column:state_type"`
		Count        int64     `gorm:"column:count"`
	}
	var stateCounts []stateCount

	stateQuery := applyCommonFilters(
		r.db.WithContext(ctx).Model(&models.Incident{}).
			Select(`
				workflows.id    as workflow_id,
				workflows.name  as workflow_name,
				workflow_states.id         as state_id,
				workflow_states.name       as state_name,
				workflow_states.code       as state_code,
				workflow_states.state_type as state_type,
				count(*) as count
			`).
			Joins("JOIN workflow_states ON workflow_states.id = incidents.current_state_id").
			Joins("JOIN workflows ON workflows.id = incidents.workflow_id AND workflows.deleted_at IS NULL"),
	)

	if err := stateQuery.
		Group(`workflows.id, workflows.name, workflow_states.id, workflow_states.name, workflow_states.code, workflow_states.state_type`).
		Order("workflows.name ASC, workflow_states.state_type ASC").
		Scan(&stateCounts).Error; err != nil {
		return nil, err
	}

	type stateTypeCount struct {
		StateType string `gorm:"column:state_type"`
		StateCode string `gorm:"column:state_code"`
		Count     int64  `gorm:"column:count"`
	}
	var stateTypeCounts []stateTypeCount

	stateTypeQuery := applyBaseFilters(
		r.db.WithContext(ctx).Model(&models.Incident{}).
			Select("workflow_states.state_type as state_type, workflow_states.code as state_code, count(*) as count").
			Joins("JOIN workflow_states ON workflow_states.id = incidents.current_state_id").
			Where("incidents.workflow_id IN (SELECT id FROM workflows WHERE deleted_at IS NULL)"),
	)

	if err := stateTypeQuery.
		Group("workflow_states.state_type, workflow_states.code").
		Scan(&stateTypeCounts).Error; err != nil {
		return nil, err
	}

	for _, stc := range stateTypeCounts {
		switch stc.StateType {
		case "initial":
			stats.Open += stc.Count
		case "normal":
			stats.InProgress += stc.Count
		case "terminal":
			stats.Closed += stc.Count
		}
		if stc.StateCode == "resolved" {
			stats.Resolved += stc.Count
		}
	}

	//  Build workflow_stats

	filterType := ""
	if filter != nil {
		filterType = filter.FilterType
	}
	hasRecordType := filter != nil && filter.RecordType != nil && *filter.RecordType != ""
	hasTypeFilter := filterType == "created" || filterType == "assigned"

	if hasRecordType || hasTypeFilter {
		workflowMap := make(map[uuid.UUID]*models.WorkflowStats)
		for _, sc := range stateCounts {
			if _, exists := workflowMap[sc.WorkflowID]; !exists {
				workflowMap[sc.WorkflowID] = &models.WorkflowStats{
					WorkflowID:     sc.WorkflowID,
					WorkflowName:   sc.WorkflowName,
					ByState:        make(map[string]int64),
					ByStateDetails: []models.StateStatDetail{},
				}
			}
			wf := workflowMap[sc.WorkflowID]
			wf.ByState[sc.StateName] += sc.Count
			wf.ByStateDetails = append(wf.ByStateDetails, models.StateStatDetail{
				ID:    sc.StateID,
				Name:  sc.StateName,
				Count: sc.Count,
			})
		}
		for _, wf := range workflowMap {
			stats.WorkflowStats = append(stats.WorkflowStats, *wf)
		}
	}

	return stats, nil
}

func (r *incidentRepository) GetPriorityCounts(ctx context.Context, filter *models.IncidentFilter) (map[string]int64, error) {
	// Query to count incidents grouped by priority
	type priorityCount struct {
		PriorityName string `gorm:"column:priority_name"`
		SortOrder    int    `gorm:"column:sort_order"`
		Count        int64  `gorm:"column:count"`
	}

	query := r.db.WithContext(ctx).Model(&models.Incident{}).
		Select("lookup_values.name as priority_name, lookup_values.sort_order as sort_order, count(*) as count").
		Joins("INNER JOIN incident_lookup_values ON incident_lookup_values.incident_id = incidents.id").
		Joins("INNER JOIN lookup_values ON lookup_values.id = incident_lookup_values.lookup_value_id").
		Joins("INNER JOIN lookup_categories ON lookup_categories.id = lookup_values.category_id").
		Where("lookup_categories.code = ?", "PRIORITY")

	// Apply filters if provided
	if filter != nil {
		if filter.RecordType != nil && *filter.RecordType != "" {
			query = query.Where("incidents.record_type = ?", *filter.RecordType)
		}

		if len(filter.WorkflowID) != 0 {
			query = query.Where("incidents.workflow_id IN ?", filter.WorkflowID)
		}

		if len(filter.DepartmentID) != 0 {
			query = query.Where("incidents.department_id IN ?", filter.DepartmentID)
		}

		if len(filter.AssigneeID) != 0 {
			if len(filter.AssigneeID) == 1 {
				// Check both primary assignee and multiple assignees
				query = query.Where("incidents.assignee_id = ? OR incidents.id IN (SELECT incident_id FROM incident_assignees WHERE user_id = ?)", filter.AssigneeID[0], filter.AssigneeID[0])
			} else {
				// Multiple assignees
				query = query.Where("incidents.assignee_id IN ?", filter.AssigneeID)
			}
		}

		// Filter by state visibility based on user roles
		if filter.UserRoleIDs != nil && len(filter.UserRoleIDs) > 0 {
			query = query.Joins("JOIN workflow_states ON workflow_states.id = incidents.current_state_id").
				Where(`
					NOT EXISTS (SELECT 1 FROM state_viewable_roles WHERE workflow_state_id = workflow_states.id)
					OR EXISTS (SELECT 1 FROM state_viewable_roles
					           WHERE workflow_state_id = workflow_states.id AND role_id IN ?)
				`, filter.UserRoleIDs)
		}
	}

	var counts []priorityCount
	if err := query.Group("lookup_values.id, lookup_values.name, lookup_values.sort_order").
		Order("lookup_values.sort_order ASC").
		Scan(&counts).Error; err != nil {
		return nil, err
	}

	// Build result map with priority names as keys
	result := make(map[string]int64)
	for _, pc := range counts {
		result[pc.PriorityName] = pc.Count
	}

	return result, nil
}

func (r *incidentRepository) GetSLABreachedIncidents(ctx context.Context) ([]models.Incident, error) {
	var incidents []models.Incident
	err := r.db.WithContext(ctx).
		Preload("CurrentState").
		Preload("Assignee").
		Where("sla_breached = ? OR (sla_deadline IS NOT NULL AND sla_deadline < ? AND sla_breached = ?)", true, time.Now(), false).
		Find(&incidents).Error
	return incidents, err
}

func (r *incidentRepository) GetBreachedByFilter(ctx context.Context, locationID uuid.UUID, classificationID uuid.UUID) ([]models.Incident, error) {

	var incidents []models.Incident

	query := r.db.WithContext(ctx).
		Where("sla_breached = ?", true).
		Where("current_state_id NOT IN (SELECT id FROM workflow_states WHERE state_type = 'terminal')")

	// Only filter by location if a non-zero UUID is provided
	if locationID != (uuid.UUID{}) {
		query = query.Where("location_id = ?", locationID)
	}

	// Only filter by classification if a non-zero UUID is provided
	if classificationID != (uuid.UUID{}) {
		query = query.Where("classification_id = ?", classificationID)
	}

	err := query.Preload("Location").Preload("Classification").Preload("CurrentState").Preload("Assignee").Find(&incidents).Error
	return incidents, err
}

func (r *incidentRepository) UpdateSLABreached(ctx context.Context, incidentID uuid.UUID, breached bool) error {
	return r.db.WithContext(ctx).
		Model(&models.Incident{}).
		Where("id = ?", incidentID).
		Update("sla_breached", breached).Error
}

func (r *incidentRepository) MarkSLABreached(ctx context.Context) (int64, error) {
	// Find and update all incidents that have passed their SLA deadline
	// but aren't marked as breached yet, and are not in a terminal state
	result := r.db.WithContext(ctx).
		Model(&models.Incident{}).
		Where("sla_deadline IS NOT NULL").
		Where("sla_deadline < ?", time.Now()).
		Where("sla_breached = ?", false).
		Where("current_state_id NOT IN (SELECT id FROM workflow_states WHERE state_type = 'terminal')").
		Update("sla_breached", true)

	return result.RowsAffected, result.Error
}

// User-specific queries

func (r *incidentRepository) GetAssignedToUser(ctx context.Context, userID uuid.UUID, recordType string, page, limit int) ([]models.Incident, int64, error) {
	var incidents []models.Incident
	var total int64

	const maxLimit = 100
	page = max(page, 1)
	if limit < 1 || limit > maxLimit {
		limit = 20
	}
	offset := (page - 1) * limit

	baseQuery := r.db.WithContext(ctx).
		Model(&models.Incident{}).
		Where("assignee_id = ? OR id IN (SELECT incident_id FROM incident_assignees WHERE user_id = ?)", userID, userID)

	if recordType != "" {
		baseQuery = baseQuery.Where("record_type = ?", recordType)
	}

	// Clone for count (critical fix)
	countQuery := baseQuery.Session(&gorm.Session{})
	if err := countQuery.Count(&total).Error; err != nil {
		return nil, 0, err
	}

	// Main query with preloads
	if err := baseQuery.
		Preload("Classification").
		Preload("TransitionHistory").
		Preload("TransitionHistory.PerformedBy").
		Preload("Workflow").
		Preload("CurrentState").
		Preload("Assignee").
		Preload("Department").
		Preload("Location").
		Preload("LookupValues.Category").
		Preload("TransitionHistory.Transition").
		Order("created_at DESC").
		Offset(offset).
		Limit(limit).
		Find(&incidents).Error; err != nil {
		return nil, 0, err
	}

	return incidents, total, nil
}

func (r *incidentRepository) GetAssignedToUser1(ctx context.Context, userID uuid.UUID, recordType string, page, limit int) ([]models.Incident, int64, error) {
	var incidents []models.Incident
	var total int64

	page = max(page, 1)
	if limit < 1 || limit > 100 {
		limit = 20
	}
	offset := (page - 1) * limit

	// Check both primary assignee (assignee_id) AND multiple assignees (incident_assignees table)
	baseQuery := r.db.WithContext(ctx).Model(&models.Incident{}).
		Where("assignee_id = ? OR id IN (SELECT incident_id FROM incident_assignees WHERE user_id = ?)", userID, userID)

	// Filter by record_type if provided
	if recordType != "" {
		baseQuery = baseQuery.Where("record_type = ?", recordType)
	}

	if err := baseQuery.Count(&total).Error; err != nil {
		return nil, 0, err
	}
	err := baseQuery.
		Preload("Classification").
		Preload("TransitionHistory").
		Preload("TransitionHistory.PerformedBy").
		Preload("Workflow").
		Preload("CurrentState").
		Preload("Assignee").
		Preload("Department").
		Preload("Location").
		Preload("LookupValues.Category").
		Preload("TransitionHistory.Transition").
		Order("created_at DESC").
		Offset(offset).
		Limit(limit).
		Find(&incidents).Error
	if err != nil {
		return nil, 0, err
	}

	return incidents, total, nil
}

func (r *incidentRepository) GetReportedByUser(ctx context.Context, userID uuid.UUID, recordType string, page, limit int) ([]models.Incident, int64, error) {
	var incidents []models.Incident
	var total int64

	page = max(page, 1)
	if limit < 1 || limit > 100 {
		limit = 20
	}
	offset := (page - 1) * limit

	baseQuery := r.db.WithContext(ctx).Model(&models.Incident{}).Where("reporter_id = ?", userID)

	// Filter by record_type if provided
	if recordType != "" {
		baseQuery = baseQuery.Where("record_type = ?", recordType)
	}

	if err := baseQuery.Count(&total).Error; err != nil {
		return nil, 0, err
	}

	err := baseQuery.
		Preload("Classification").
		Preload("TransitionHistory").
		Preload("TransitionHistory.PerformedBy").
		Preload("CurrentState").
		Preload("Workflow").
		Preload("Assignee").
		Preload("Department").
		Preload("Location").
		Preload("LookupValues.Category").
		Preload("TransitionHistory.Transition").
		Order("created_at DESC").
		Offset(offset).
		Limit(limit).
		Find(&incidents).Error
	if err != nil {
		return nil, 0, err
	}

	return incidents, total, nil
}

// Revisions

func (r *incidentRepository) CreateRevision(ctx context.Context, revision *models.IncidentRevision) error {
	return r.db.WithContext(ctx).Create(revision).Error
}

func (r *incidentRepository) ListRevisions(ctx context.Context, filter *models.IncidentRevisionFilter) ([]models.IncidentRevision, int64, error) {
	var revisions []models.IncidentRevision
	var total int64

	page := filter.Page
	limit := filter.Limit
	page = max(page, 1)
	if limit < 1 || limit > 100 {
		limit = 20
	}
	offset := (page - 1) * limit

	query := r.db.WithContext(ctx).Model(&models.IncidentRevision{}).Where("incident_id = ?", filter.IncidentID)

	if filter.ActionType != nil {
		query = query.Where("action_type = ?", *filter.ActionType)
	}
	if filter.PerformedByID != nil {
		query = query.Where("performed_by_id = ?", *filter.PerformedByID)
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

	err := query.
		Preload("PerformedBy").
		Preload("PerformedBy.Roles").
		Preload("PerformedBy.Departments").
		Preload("TransitionHistory").
		Preload("TransitionHistory.Transition").
		Preload("TransitionHistory.FromState").
		Preload("TransitionHistory.ToState").
		Order("revision_number DESC").
		Offset(offset).
		Limit(limit).
		Find(&revisions).Error
	if err != nil {
		return nil, 0, err
	}

	return revisions, total, nil
}

func (r *incidentRepository) GetNextRevisionNumber(ctx context.Context, incidentID uuid.UUID) (int, error) {
	var maxNum int
	err := r.db.WithContext(ctx).
		Model(&models.IncidentRevision{}).
		Select("COALESCE(MAX(revision_number), 0)").
		Where("incident_id = ?", incidentID).
		Scan(&maxNum).Error
	if err != nil {
		return 0, err
	}
	return maxNum + 1, nil
}

// Feedback

func (r *incidentRepository) CreateFeedback(ctx context.Context, feedback *models.IncidentFeedback) error {
	return r.db.WithContext(ctx).Create(feedback).Error
}

func (r *incidentRepository) CreateNotification(ctx context.Context, notification *models.NotificationLog) error {
	return r.db.WithContext(ctx).Create(notification).Error
}

func (r *incidentRepository) FindFeedbackByID(ctx context.Context, id uuid.UUID) (*models.IncidentFeedback, error) {
	var feedback models.IncidentFeedback
	err := r.db.WithContext(ctx).
		Preload("CreatedBy").
		First(&feedback, "id = ?", id).Error
	if err != nil {
		return nil, err
	}
	return &feedback, nil
}

func (r *incidentRepository) ListFeedback(ctx context.Context, incidentID uuid.UUID) ([]models.IncidentFeedback, error) {
	var feedback []models.IncidentFeedback
	err := r.db.WithContext(ctx).
		Preload("CreatedBy").
		Where("incident_id = ?", incidentID).
		Order("created_at DESC").
		Find(&feedback).Error
	return feedback, err
}

func (r *incidentRepository) ListAllFeedback(ctx context.Context) ([]models.IncidentFeedback, error) {
	var feedback []models.IncidentFeedback
	err := r.db.WithContext(ctx).Debug().
		Preload("CreatedBy").
		Preload("Incident").
		Order("created_at DESC").
		Find(&feedback).Error
	return feedback, err
}

func (r *incidentRepository) LinkFeedbackToTransition(ctx context.Context, feedbackID uuid.UUID, transitionHistoryID uuid.UUID) error {
	return r.db.WithContext(ctx).
		Model(&models.IncidentFeedback{}).
		Where("id = ?", feedbackID).
		Update("transition_history_id", transitionHistoryID).Error
}

// Complaint-specific

func (r *incidentRepository) IncrementEvaluationCount(ctx context.Context, id uuid.UUID) error {
	return r.db.WithContext(ctx).
		Model(&models.Incident{}).
		Where("id = ?", id).
		Where("record_type = 'complaint'").
		Update("evaluation_count", gorm.Expr("evaluation_count + 1")).Error
}

// Concurrency Control

// UpdateFieldsWithVersion performs optimistic lock field update
func (r *incidentRepository) UpdateFieldsWithVersion(ctx context.Context, id uuid.UUID, updates map[string]interface{}, expectedVersion int) error {
	updates["version"] = expectedVersion + 1
	updates["updated_at"] = time.Now()

	result := r.db.WithContext(ctx).
		Model(&models.Incident{}).
		Where("id = ? AND version = ?", id, expectedVersion).
		Updates(updates)

	if result.Error != nil {
		return result.Error
	}

	if result.RowsAffected == 0 {
		return ErrVersionMismatch
	}

	return nil
}

// WithTx returns repository instance using the provided transaction
func (r *incidentRepository) WithTx(tx *gorm.DB) IncidentRepository {
	return &incidentRepository{db: tx}
}

// LockForUpdate acquires a pessimistic lock (SELECT FOR UPDATE)
func (r *incidentRepository) LockForUpdate(ctx context.Context, tx *gorm.DB, id uuid.UUID) (*models.Incident, error) {
	var incident models.Incident

	// Set lock timeout to 10 seconds to prevent indefinite waiting
	if err := tx.Exec("SET LOCAL lock_timeout = '10s'").Error; err != nil {
		return nil, err
	}

	err := tx.WithContext(ctx).
		Clauses(clause.Locking{Strength: "UPDATE"}).
		Preload("Classification").
		Preload("Workflow").
		Preload("CurrentState").
		Where("id = ?", id).
		First(&incident).Error

	if err != nil {
		return nil, err
	}

	return &incident, nil
}

func (r *incidentRepository) FindUserOpenIncidentsForDuplicateCheck(ctx context.Context, reporterID uuid.UUID) ([]models.Incident, error) {

	var incidents []models.Incident

	err := r.db.WithContext(ctx).
		Where("reporter_id = ?", reporterID).
		Where("closed_at IS NULL").
		Where("latitude IS NOT NULL").
		Where("longitude IS NOT NULL").
		Find(&incidents).Error

	return incidents, err
}

// GetIncidentsExceedingStateSLA returns incidents where the time spent in the current state

func (r *incidentRepository) GetIncidentsExceedingStateSLA(ctx context.Context) ([]models.Incident, error) {
	var incidents []models.Incident

	err := r.db.WithContext(ctx).
		Select("incidents.*").
		Joins("JOIN workflow_states ws ON incidents.current_state_id = ws.id").
		Where("ws.sla_hours IS NOT NULL AND ws.sla_hours > 0").
		Where("ws.state_type != 'terminal'").
		Where("ws.deleted_at IS NULL").
		Where(`
			EXTRACT(EPOCH FROM (NOW() - COALESCE(
				(SELECT MAX(ith.transitioned_at)
				 FROM incident_transition_histories ith
				 WHERE ith.incident_id = incidents.id
				   AND ith.to_state_id = incidents.current_state_id),
				incidents.created_at
			))) >
			CASE COALESCE(ws.sla_unit, 'hours')
				WHEN 'minutes' THEN ws.sla_hours * 60
				WHEN 'days'    THEN ws.sla_hours * 86400
				WHEN 'months'  THEN ws.sla_hours * 2592000
				ELSE ws.sla_hours * 3600
			END
		`).
		Preload("CurrentState").
		Preload("Workflow").
		Preload("Location").
		Preload("Classification").
		Preload("Assignee").
		Find(&incidents).Error

	return incidents, err
}

// GetNewlyBreachedIncidents returns incidents with sla_breached = true that have no

func (r *incidentRepository) GetNewlyBreachedIncidents(ctx context.Context) ([]models.Incident, error) {
	var incidents []models.Incident
	err := r.db.WithContext(ctx).
		Where("sla_breached = ?", true).
		Where(`current_state_id NOT IN (
			SELECT id FROM workflow_states WHERE state_type = 'terminal'
		)`).
		Where(`NOT EXISTS (
			SELECT 1 FROM escalation_slas es
			WHERE es.incident_id = incidents.id
			  AND es.state_id IS NULL
		)`).
		Preload("Assignee").
		Preload("Reporter").
		Preload("CurrentState").
		Find(&incidents).Error
	return incidents, err
}
