package repository

import (
	"context"
	"errors"
	"fmt"

	"github.com/automax/backend/internal/models"
	"github.com/google/uuid"
	"gorm.io/gorm"
)

type GoalRepository interface {
	// Goal CRUD
	Create(ctx context.Context, goal *models.Goal) error
	FindByID(ctx context.Context, id uuid.UUID) (*models.Goal, error)
	FindByIDWithRelations(ctx context.Context, id uuid.UUID) (*models.Goal, error)
	List(ctx context.Context, filter *models.GoalFilter) ([]models.Goal, int64, error)
	ListForExport(ctx context.Context, filter *models.GoalFilter) ([]models.Goal, error)
	Update(ctx context.Context, goal *models.Goal) error
	Delete(ctx context.Context, id uuid.UUID) error
	FindGoalByTitleAndOwner(ctx context.Context, title string, ownerID uuid.UUID) (*models.Goal, error)

	// Collaborators
	AddCollaborator(ctx context.Context, collaborator *models.GoalCollaborator) error
	RemoveCollaborator(ctx context.Context, goalID uuid.UUID, userID uuid.UUID) error
	ListCollaborators(ctx context.Context, goalID uuid.UUID) ([]models.GoalCollaborator, error)

	// Metrics
	CreateMetric(ctx context.Context, metric *models.GoalMetric) error
	FindMetricByID(ctx context.Context, id uuid.UUID) (*models.GoalMetric, error)
	UpdateMetric(ctx context.Context, metric *models.GoalMetric) error
	DeleteMetric(ctx context.Context, id uuid.UUID) error
	ListMetricsByGoalID(ctx context.Context, goalID uuid.UUID) ([]models.GoalMetric, error)

	// Metric History
	CreateMetricHistory(ctx context.Context, history *models.MetricHistory) error
	ListMetricHistory(ctx context.Context, metricID uuid.UUID, page int, limit int) ([]models.MetricHistory, int64, error)

	// Metric Workflow / Approval Gate
	FindMetricByIDWithRelations(ctx context.Context, id uuid.UUID) (*models.GoalMetric, error)
	FindMetricByIDForUpdate(ctx context.Context, tx *gorm.DB, id uuid.UUID) (*models.GoalMetric, error)
	UpdateMetricInTx(ctx context.Context, tx *gorm.DB, metric *models.GoalMetric) error
	CreateMetricTransitionHistory(ctx context.Context, tx *gorm.DB, h *models.MetricTransitionHistory) error
	ListMetricTransitionHistory(ctx context.Context, metricID uuid.UUID) ([]models.MetricTransitionHistory, error)
	ListPendingMetricApprovals(ctx context.Context, userID uuid.UUID, page int, limit int) ([]models.GoalMetric, int64, error)

	// Metric Value Change
	CreateMetricValueChange(ctx context.Context, change *models.GoalMetricValueChange) error
	FindMetricValueChangeByIDWithRelations(ctx context.Context, id uuid.UUID) (*models.GoalMetricValueChange, error)
	FindMetricValueChangeByIDForUpdate(ctx context.Context, tx *gorm.DB, id uuid.UUID) (*models.GoalMetricValueChange, error)
	UpdateMetricValueChangeInTx(ctx context.Context, tx *gorm.DB, change *models.GoalMetricValueChange) error
	ListMetricValueChangesByMetricID(ctx context.Context, metricID uuid.UUID) ([]models.GoalMetricValueChange, error)
	CreateMetricValueChangeTransitionHistory(ctx context.Context, tx *gorm.DB, h *models.MetricValueChangeTransitionHistory) error
	ListPendingMetricValueChangeApprovals(ctx context.Context, userID uuid.UUID, page int, limit int) ([]models.GoalMetricValueChange, int64, error)

	// Evidence
	CreateEvidence(ctx context.Context, evidence *models.Evidence) error
	FindEvidenceByID(ctx context.Context, id uuid.UUID) (*models.Evidence, error)
	FindEvidenceByIDWithRelations(ctx context.Context, id uuid.UUID) (*models.Evidence, error)
	FindEvidenceByIDForUpdate(ctx context.Context, tx *gorm.DB, id uuid.UUID) (*models.Evidence, error)
	UpdateEvidence(ctx context.Context, evidence *models.Evidence) error
	UpdateEvidenceInTx(ctx context.Context, tx *gorm.DB, evidence *models.Evidence) error
	DeleteEvidence(ctx context.Context, id uuid.UUID) error
	ListEvidencesByGoalID(ctx context.Context, goalID uuid.UUID, filter *models.EvidenceFilter) ([]models.Evidence, int64, error)

	// Evidence Transition History
	CreateTransitionHistory(ctx context.Context, tx *gorm.DB, history *models.EvidenceTransitionHistory) error
	ListTransitionHistory(ctx context.Context, evidenceID uuid.UUID) ([]models.EvidenceTransitionHistory, error)
	ListPendingApprovals(ctx context.Context, userID uuid.UUID, page int, limit int) ([]models.Evidence, int64, error)
	ListCompletedApprovals(ctx context.Context, userID uuid.UUID, page int, limit int) ([]models.EvidenceTransitionHistory, int64, error)

	// Hierarchy
	FindChildren(ctx context.Context, parentID uuid.UUID) ([]models.Goal, error)
	FindDescendantIDs(ctx context.Context, goalPath string) ([]uuid.UUID, error)

	// Check-ins
	CreateCheckIn(ctx context.Context, checkIn *models.GoalCheckIn) error
	ListCheckIns(ctx context.Context, goalID uuid.UUID, page int, limit int) ([]models.GoalCheckIn, int64, error)
	FindCheckInByID(ctx context.Context, id uuid.UUID) (*models.GoalCheckIn, error)
	DeleteCheckIn(ctx context.Context, id uuid.UUID) error

	// Metric Import Batches
	CreateMetricImportBatch(ctx context.Context, batch *models.MetricImportBatch) error
	FindMetricImportBatchByID(ctx context.Context, id uuid.UUID) (*models.MetricImportBatch, error)
	FindMetricImportBatchByIDWithRelations(ctx context.Context, id uuid.UUID) (*models.MetricImportBatch, error)
	FindMetricImportBatchByIDForUpdate(ctx context.Context, tx *gorm.DB, id uuid.UUID) (*models.MetricImportBatch, error)
	UpdateMetricImportBatchInTx(ctx context.Context, tx *gorm.DB, batch *models.MetricImportBatch) error
	ListMetricImportBatches(ctx context.Context, filter *models.MetricImportBatchFilter) ([]models.MetricImportBatch, int64, error)
	DeleteMetricImportBatch(ctx context.Context, id uuid.UUID) error
	CreateMetricImportItems(ctx context.Context, tx *gorm.DB, items []models.MetricImportItem) error
	ListMetricImportItemsByBatchID(ctx context.Context, batchID uuid.UUID) ([]models.MetricImportItem, error)
	MarkMetricImportItemsApplied(ctx context.Context, tx *gorm.DB, batchID uuid.UUID) error
	CreateMetricBatchTransitionHistory(ctx context.Context, tx *gorm.DB, h *models.MetricImportBatchTransitionHistory) error
	ListMetricBatchTransitionHistory(ctx context.Context, batchID uuid.UUID) ([]models.MetricImportBatchTransitionHistory, error)

	// Comments
	CreateComment(ctx context.Context, comment *models.GoalComment) error
	ListComments(ctx context.Context, goalID uuid.UUID, page, limit int) ([]models.GoalComment, int64, error)
	DeleteComment(ctx context.Context, id uuid.UUID) error
	FindCommentByID(ctx context.Context, id uuid.UUID) (*models.GoalComment, error)

	// Transaction support
	BeginTx(ctx context.Context) *gorm.DB
}

type goalRepository struct {
	db *gorm.DB
}

func NewGoalRepository(db *gorm.DB) GoalRepository {
	return &goalRepository{db: db}
}

// ──────────────────────────────────────────────────
// Goal CRUD
// ──────────────────────────────────────────────────

func (r *goalRepository) Create(ctx context.Context, goal *models.Goal) error {
	return r.db.WithContext(ctx).Create(goal).Error
}

func (r *goalRepository) FindByID(ctx context.Context, id uuid.UUID) (*models.Goal, error) {
	var goal models.Goal
	err := r.db.WithContext(ctx).
		First(&goal, "id = ?", id).Error
	if err != nil {
		return nil, err
	}
	return &goal, nil
}

func (r *goalRepository) FindByIDWithRelations(ctx context.Context, id uuid.UUID) (*models.Goal, error) {
	var goal models.Goal
	err := r.db.WithContext(ctx).
		Preload("Owner").
		Preload("CreatedBy").
		Preload("Department").
		Preload("CategoryRef").
		Preload("ParentGoal").
		Preload("Children").
		Preload("Collaborators").
		Preload("Collaborators.User").
		Preload("Metrics").
		Preload("Evidences").
		Preload("Evidences.UploadedBy").
		Preload("Evidences.CurrentState").
		Preload("Evidences.AssignedTo").
		First(&goal, "id = ?", id).Error
	if err != nil {
		return nil, err
	}
	return &goal, nil
}

func (r *goalRepository) List(ctx context.Context, filter *models.GoalFilter) ([]models.Goal, int64, error) {
	var goals []models.Goal
	var total int64

	query := r.db.WithContext(ctx).Model(&models.Goal{})

	// Apply filters
	if filter.Status != "" {
		query = query.Where("status = ?", filter.Status)
	}
	if filter.Priority != "" {
		query = query.Where("priority = ?", filter.Priority)
	}
	if filter.OwnerID != nil {
		query = query.Where("owner_id = ?", *filter.OwnerID)
	}
	if filter.DepartmentID != nil {
		query = query.Where("department_id = ?", *filter.DepartmentID)
	}
	if filter.ParentGoalID != nil {
		query = query.Where("parent_goal_id = ?", *filter.ParentGoalID)
	}
	if filter.RootOnly {
		query = query.Where("parent_goal_id IS NULL")
	}
	if filter.Category != "" {
		query = query.Where("category = ?", filter.Category)
	}
	if filter.Search != "" {
		searchPattern := "%" + filter.Search + "%"
		query = query.Where("title ILIKE ? OR description ILIKE ?", searchPattern, searchPattern)
	}
	if filter.StartFrom != nil {
		query = query.Where("start_date >= ?", *filter.StartFrom)
	}
	if filter.StartTo != nil {
		query = query.Where("start_date <= ?", *filter.StartTo)
	}
	if filter.TargetFrom != nil {
		query = query.Where("target_date >= ?", *filter.TargetFrom)
	}
	if filter.TargetTo != nil {
		query = query.Where("target_date <= ?", *filter.TargetTo)
	}
	if filter.UserID != nil {
		// User can see goals they own, collaborate on, or belong to their department
		query = query.Where(
			"owner_id = ? OR id IN (SELECT goal_id FROM goal_collaborators WHERE user_id = ?) OR department_id IN (SELECT department_id FROM users WHERE id = ? AND department_id IS NOT NULL)",
			*filter.UserID, *filter.UserID, *filter.UserID,
		)
	}
	// scope=mine narrows the listing to owner-or-collaborator, ignoring
	// department-wide visibility. Used by the "My Goals" view.
	if filter.Scope == "mine" && filter.UserID != nil {
		query = query.Where(
			"owner_id = ? OR id IN (SELECT goal_id FROM goal_collaborators WHERE user_id = ?)",
			*filter.UserID, *filter.UserID,
		)
	}

	// Count total
	if err := query.Count(&total).Error; err != nil {
		return nil, 0, err
	}

	// Apply pagination
	if filter.Page < 1 {
		filter.Page = 1
	}
	if filter.Limit < 1 || filter.Limit > 100 {
		filter.Limit = 10
	}
	offset := (filter.Page - 1) * filter.Limit

	// Apply sorting
	orderClause := "created_at DESC"
	if filter.SortBy != "" {
		direction := "ASC"
		if filter.SortOrder == "desc" {
			direction = "DESC"
		}
		orderClause = fmt.Sprintf("%s %s", filter.SortBy, direction)
	}

	err := query.
		Preload("Owner").
		Preload("Department").
		Order(orderClause).
		Offset(offset).
		Limit(filter.Limit).
		Find(&goals).Error
	if err != nil {
		return nil, 0, err
	}

	return goals, total, nil
}

func (r *goalRepository) ListForExport(ctx context.Context, filter *models.GoalFilter) ([]models.Goal, error) {
	var goals []models.Goal

	query := r.db.WithContext(ctx).Model(&models.Goal{})

	if filter.Status != "" {
		query = query.Where("status = ?", filter.Status)
	}
	if filter.Priority != "" {
		query = query.Where("priority = ?", filter.Priority)
	}
	if filter.OwnerID != nil {
		query = query.Where("owner_id = ?", *filter.OwnerID)
	}
	if filter.DepartmentID != nil {
		query = query.Where("department_id = ?", *filter.DepartmentID)
	}
	if filter.Category != "" {
		query = query.Where("category = ?", filter.Category)
	}
	if filter.Search != "" {
		searchPattern := "%" + filter.Search + "%"
		query = query.Where("title ILIKE ? OR description ILIKE ?", searchPattern, searchPattern)
	}

	err := query.
		Preload("Owner").
		Preload("Department").
		Preload("CategoryRef").
		Preload("Metrics").
		Preload("Collaborators.User").
		Order("created_at DESC").
		Limit(10000).
		Find(&goals).Error
	if err != nil {
		return nil, err
	}

	return goals, nil
}

func (r *goalRepository) Update(ctx context.Context, goal *models.Goal) error {
	return r.db.WithContext(ctx).Save(goal).Error
}

func (r *goalRepository) Delete(ctx context.Context, id uuid.UUID) error {
	return r.db.WithContext(ctx).Delete(&models.Goal{}, "id = ?", id).Error
}

// ──────────────────────────────────────────────────
// Collaborators
// ──────────────────────────────────────────────────

func (r *goalRepository) AddCollaborator(ctx context.Context, collaborator *models.GoalCollaborator) error {
	return r.db.WithContext(ctx).Create(collaborator).Error
}

func (r *goalRepository) RemoveCollaborator(ctx context.Context, goalID uuid.UUID, userID uuid.UUID) error {
	return r.db.WithContext(ctx).
		Where("goal_id = ? AND user_id = ?", goalID, userID).
		Delete(&models.GoalCollaborator{}).Error
}

func (r *goalRepository) ListCollaborators(ctx context.Context, goalID uuid.UUID) ([]models.GoalCollaborator, error) {
	var collaborators []models.GoalCollaborator
	err := r.db.WithContext(ctx).
		Preload("User").
		Where("goal_id = ?", goalID).
		Find(&collaborators).Error
	return collaborators, err
}

// ──────────────────────────────────────────────────
// Metrics
// ──────────────────────────────────────────────────

func (r *goalRepository) CreateMetric(ctx context.Context, metric *models.GoalMetric) error {
	return r.db.WithContext(ctx).Create(metric).Error
}

func (r *goalRepository) FindMetricByID(ctx context.Context, id uuid.UUID) (*models.GoalMetric, error) {
	var metric models.GoalMetric
	err := r.db.WithContext(ctx).
		First(&metric, "id = ?", id).Error
	if err != nil {
		return nil, err
	}
	return &metric, nil
}

func (r *goalRepository) UpdateMetric(ctx context.Context, metric *models.GoalMetric) error {
	return r.db.WithContext(ctx).Save(metric).Error
}

func (r *goalRepository) DeleteMetric(ctx context.Context, id uuid.UUID) error {
	return r.db.WithContext(ctx).Delete(&models.GoalMetric{}, "id = ?", id).Error
}

func (r *goalRepository) ListMetricsByGoalID(ctx context.Context, goalID uuid.UUID) ([]models.GoalMetric, error) {
	var metrics []models.GoalMetric
	err := r.db.WithContext(ctx).
		Preload("CurrentState").
		Preload("AssignedTo").
		Where("goal_id = ?", goalID).
		Find(&metrics).Error
	return metrics, err
}

// ──────────────────────────────────────────────────
// Metric History
// ──────────────────────────────────────────────────

func (r *goalRepository) CreateMetricHistory(ctx context.Context, history *models.MetricHistory) error {
	return r.db.WithContext(ctx).Create(history).Error
}

func (r *goalRepository) ListMetricHistory(ctx context.Context, metricID uuid.UUID, page int, limit int) ([]models.MetricHistory, int64, error) {
	var histories []models.MetricHistory
	var total int64

	query := r.db.WithContext(ctx).Model(&models.MetricHistory{}).Where("metric_id = ?", metricID)

	if err := query.Count(&total).Error; err != nil {
		return nil, 0, err
	}

	if page < 1 {
		page = 1
	}
	if limit < 1 || limit > 100 {
		limit = 10
	}
	offset := (page - 1) * limit

	err := query.
		Preload("ChangedBy").
		Order("created_at DESC").
		Offset(offset).
		Limit(limit).
		Find(&histories).Error
	if err != nil {
		return nil, 0, err
	}

	return histories, total, nil
}

// ──────────────────────────────────────────────────
// Metric Workflow / Approval Gate
// ──────────────────────────────────────────────────

func (r *goalRepository) FindMetricByIDWithRelations(ctx context.Context, id uuid.UUID) (*models.GoalMetric, error) {
	var metric models.GoalMetric
	err := r.db.WithContext(ctx).
		Preload("Goal").
		Preload("CurrentState").
		Preload("AssignedTo").
		First(&metric, "id = ?", id).Error
	if err != nil {
		return nil, err
	}
	return &metric, nil
}

func (r *goalRepository) FindMetricByIDForUpdate(ctx context.Context, tx *gorm.DB, id uuid.UUID) (*models.GoalMetric, error) {
	var metric models.GoalMetric
	err := tx.WithContext(ctx).
		Set("gorm:query_option", "FOR UPDATE").
		Preload("CurrentState").
		Preload("AssignedTo").
		First(&metric, "id = ?", id).Error
	if err != nil {
		return nil, err
	}
	return &metric, nil
}

func (r *goalRepository) UpdateMetricInTx(ctx context.Context, tx *gorm.DB, metric *models.GoalMetric) error {
	return tx.WithContext(ctx).
		Select("current_state_id", "assigned_to_id", "current_value", "version", "updated_at").
		Save(metric).Error
}

func (r *goalRepository) CreateMetricTransitionHistory(ctx context.Context, tx *gorm.DB, h *models.MetricTransitionHistory) error {
	return tx.WithContext(ctx).Create(h).Error
}

func (r *goalRepository) ListMetricTransitionHistory(ctx context.Context, metricID uuid.UUID) ([]models.MetricTransitionHistory, error) {
	var histories []models.MetricTransitionHistory
	err := r.db.WithContext(ctx).
		Preload("FromState").
		Preload("ToState").
		Preload("Transition").
		Preload("PerformedBy").
		Where("metric_id = ?", metricID).
		Order("transitioned_at DESC").
		Find(&histories).Error
	return histories, err
}

func (r *goalRepository) ListPendingMetricApprovals(ctx context.Context, userID uuid.UUID, page int, limit int) ([]models.GoalMetric, int64, error) {
	var metrics []models.GoalMetric
	var total int64

	base := r.db.WithContext(ctx).Model(&models.GoalMetric{}).
		Where("goal_metrics.assigned_to_id = ?", userID).
		Joins("JOIN workflow_states ON workflow_states.id = goal_metrics.current_state_id").
		Where("workflow_states.state_type NOT IN ?", []string{"initial", "terminal"})

	if err := base.Count(&total).Error; err != nil {
		return nil, 0, err
	}

	if page < 1 {
		page = 1
	}
	if limit < 1 || limit > 100 {
		limit = 10
	}
	offset := (page - 1) * limit

	err := r.db.WithContext(ctx).
		Preload("Goal", func(db *gorm.DB) *gorm.DB { return db.Unscoped() }).
		Preload("CurrentState").
		Preload("AssignedTo").
		Where("goal_metrics.assigned_to_id = ?", userID).
		Joins("JOIN workflow_states ON workflow_states.id = goal_metrics.current_state_id").
		Where("workflow_states.state_type NOT IN ?", []string{"initial", "terminal"}).
		Order("goal_metrics.updated_at DESC").
		Offset(offset).
		Limit(limit).
		Find(&metrics).Error
	if err != nil {
		return nil, 0, err
	}

	return metrics, total, nil
}

// ──────────────────────────────────────────────────
// Metric Value Change
// ──────────────────────────────────────────────────

func (r *goalRepository) CreateMetricValueChange(ctx context.Context, change *models.GoalMetricValueChange) error {
	return r.db.WithContext(ctx).Create(change).Error
}

func (r *goalRepository) FindMetricValueChangeByIDWithRelations(ctx context.Context, id uuid.UUID) (*models.GoalMetricValueChange, error) {
	var change models.GoalMetricValueChange
	err := r.db.WithContext(ctx).
		Preload("Metric").
		Preload("Metric.Goal").
		Preload("SubmittedBy").
		Preload("CurrentState").
		Preload("AssignedTo").
		First(&change, "id = ?", id).Error
	if err != nil {
		return nil, err
	}
	return &change, nil
}

func (r *goalRepository) FindMetricValueChangeByIDForUpdate(ctx context.Context, tx *gorm.DB, id uuid.UUID) (*models.GoalMetricValueChange, error) {
	var change models.GoalMetricValueChange
	err := tx.WithContext(ctx).
		Set("gorm:query_option", "FOR UPDATE").
		Preload("CurrentState").
		Preload("AssignedTo").
		First(&change, "id = ?", id).Error
	if err != nil {
		return nil, err
	}
	return &change, nil
}

func (r *goalRepository) UpdateMetricValueChangeInTx(ctx context.Context, tx *gorm.DB, change *models.GoalMetricValueChange) error {
	return tx.WithContext(ctx).
		Select("current_state_id", "assigned_to_id", "applied_at", "version", "updated_at").
		Save(change).Error
}

func (r *goalRepository) ListMetricValueChangesByMetricID(ctx context.Context, metricID uuid.UUID) ([]models.GoalMetricValueChange, error) {
	var changes []models.GoalMetricValueChange
	err := r.db.WithContext(ctx).
		Preload("SubmittedBy").
		Preload("CurrentState").
		Preload("AssignedTo").
		Where("metric_id = ?", metricID).
		Order("created_at DESC").
		Find(&changes).Error
	return changes, err
}

func (r *goalRepository) CreateMetricValueChangeTransitionHistory(ctx context.Context, tx *gorm.DB, h *models.MetricValueChangeTransitionHistory) error {
	return tx.WithContext(ctx).Create(h).Error
}

func (r *goalRepository) ListPendingMetricValueChangeApprovals(ctx context.Context, userID uuid.UUID, page int, limit int) ([]models.GoalMetricValueChange, int64, error) {
	var changes []models.GoalMetricValueChange
	var total int64

	base := r.db.WithContext(ctx).Model(&models.GoalMetricValueChange{}).
		Where("goal_metric_value_changes.assigned_to_id = ?", userID).
		Joins("JOIN workflow_states ON workflow_states.id = goal_metric_value_changes.current_state_id").
		Where("workflow_states.state_type NOT IN ?", []string{"initial", "terminal"})

	if err := base.Count(&total).Error; err != nil {
		return nil, 0, err
	}

	if page < 1 {
		page = 1
	}
	if limit < 1 || limit > 100 {
		limit = 10
	}
	offset := (page - 1) * limit

	err := r.db.WithContext(ctx).
		Preload("Metric", func(db *gorm.DB) *gorm.DB { return db.Unscoped() }).
		Preload("Metric.Goal", func(db *gorm.DB) *gorm.DB { return db.Unscoped() }).
		Preload("SubmittedBy").
		Preload("CurrentState").
		Preload("AssignedTo").
		Where("goal_metric_value_changes.assigned_to_id = ?", userID).
		Joins("JOIN workflow_states ON workflow_states.id = goal_metric_value_changes.current_state_id").
		Where("workflow_states.state_type NOT IN ?", []string{"initial", "terminal"}).
		Order("goal_metric_value_changes.updated_at DESC").
		Offset(offset).
		Limit(limit).
		Find(&changes).Error
	if err != nil {
		return nil, 0, err
	}

	return changes, total, nil
}

// ──────────────────────────────────────────────────
// Evidence
// ──────────────────────────────────────────────────

func (r *goalRepository) CreateEvidence(ctx context.Context, evidence *models.Evidence) error {
	return r.db.WithContext(ctx).Create(evidence).Error
}

func (r *goalRepository) FindEvidenceByID(ctx context.Context, id uuid.UUID) (*models.Evidence, error) {
	var evidence models.Evidence
	err := r.db.WithContext(ctx).
		First(&evidence, "id = ?", id).Error
	if err != nil {
		return nil, err
	}
	return &evidence, nil
}

func (r *goalRepository) FindEvidenceByIDWithRelations(ctx context.Context, id uuid.UUID) (*models.Evidence, error) {
	var evidence models.Evidence
	err := r.db.WithContext(ctx).
		Preload("Goal").
		Preload("Metric").
		Preload("UploadedBy").
		Preload("CurrentState").
		Preload("AssignedTo").
		First(&evidence, "id = ?", id).Error
	if err != nil {
		return nil, err
	}
	return &evidence, nil
}

func (r *goalRepository) FindEvidenceByIDForUpdate(ctx context.Context, tx *gorm.DB, id uuid.UUID) (*models.Evidence, error) {
	var evidence models.Evidence
	err := tx.WithContext(ctx).
		Set("gorm:query_option", "FOR UPDATE").
		Preload("CurrentState").
		Preload("AssignedTo").
		First(&evidence, "id = ?", id).Error
	if err != nil {
		return nil, err
	}
	return &evidence, nil
}

func (r *goalRepository) UpdateEvidenceInTx(ctx context.Context, tx *gorm.DB, evidence *models.Evidence) error {
	// Use Select to update only scalar columns, avoiding GORM re-associating
	// preloaded relations (e.g., CurrentState) which can overwrite foreign keys.
	return tx.WithContext(ctx).
		Select("current_state_id", "status", "assigned_to_id", "version", "updated_at").
		Save(evidence).Error
}

func (r *goalRepository) UpdateEvidence(ctx context.Context, evidence *models.Evidence) error {
	return r.db.WithContext(ctx).Save(evidence).Error
}

func (r *goalRepository) DeleteEvidence(ctx context.Context, id uuid.UUID) error {
	return r.db.WithContext(ctx).Delete(&models.Evidence{}, "id = ?", id).Error
}

func (r *goalRepository) ListEvidencesByGoalID(ctx context.Context, goalID uuid.UUID, filter *models.EvidenceFilter) ([]models.Evidence, int64, error) {
	var evidences []models.Evidence
	var total int64

	query := r.db.WithContext(ctx).Model(&models.Evidence{}).Where("evidences.goal_id = ?", goalID)

	if filter.Status != "" {
		query = query.Where("evidences.status = ?", filter.Status)
	}
	if filter.Search != "" {
		searchPattern := "%" + filter.Search + "%"
		query = query.Where("(evidences.title ILIKE ? OR evidences.file_name ILIKE ?)", searchPattern, searchPattern)
	}
	if filter.EvidenceType != "" {
		query = query.Where("evidences.evidence_type = ?", filter.EvidenceType)
	}
	if filter.StartDate != "" {
		query = query.Where("evidences.created_at >= ?", filter.StartDate)
	}
	if filter.EndDate != "" {
		query = query.Where("evidences.created_at <= ?", filter.EndDate+"T23:59:59Z")
	}

	// ApprovedOnly: restrict to evidences in a terminal state (Approved/Rejected).
	// Set by the service layer for view-only callers. Legacy rows without a
	// current_state_id are excluded — they were created before the gate so
	// view-only callers should not see drafts.
	if filter.ApprovedOnly {
		query = query.
			Joins("JOIN workflow_states ON workflow_states.id = evidences.current_state_id").
			Where("workflow_states.state_type = ?", "terminal")
	}

	if err := query.Count(&total).Error; err != nil {
		return nil, 0, err
	}

	page := filter.Page
	limit := filter.Limit
	if page < 1 {
		page = 1
	}
	if limit < 1 || limit > 100 {
		limit = 10
	}
	offset := (page - 1) * limit

	err := query.
		Preload("UploadedBy").
		Preload("CurrentState").
		Preload("AssignedTo").
		Order("evidences.created_at DESC").
		Offset(offset).
		Limit(limit).
		Find(&evidences).Error
	if err != nil {
		return nil, 0, err
	}

	return evidences, total, nil
}

// ──────────────────────────────────────────────────
// Evidence Transition History
// ──────────────────────────────────────────────────

func (r *goalRepository) CreateTransitionHistory(ctx context.Context, tx *gorm.DB, history *models.EvidenceTransitionHistory) error {
	return tx.WithContext(ctx).Create(history).Error
}

func (r *goalRepository) ListTransitionHistory(ctx context.Context, evidenceID uuid.UUID) ([]models.EvidenceTransitionHistory, error) {
	var histories []models.EvidenceTransitionHistory
	err := r.db.WithContext(ctx).
		Preload("FromState").
		Preload("ToState").
		Preload("Transition").
		Preload("PerformedBy").
		Where("evidence_id = ?", evidenceID).
		Order("transitioned_at DESC").
		Find(&histories).Error
	return histories, err
}

func (r *goalRepository) ListPendingApprovals(ctx context.Context, userID uuid.UUID, page int, limit int) ([]models.Evidence, int64, error) {
	var evidences []models.Evidence
	var total int64

	// Pending approvals = evidences assigned to this user that are in a non-terminal review state
	query := r.db.WithContext(ctx).Model(&models.Evidence{}).
		Where("assigned_to_id = ?", userID).
		Joins("JOIN workflow_states ON workflow_states.id = evidences.current_state_id").
		Where("workflow_states.state_type NOT IN ?", []string{"initial", "terminal"})

	if err := query.Count(&total).Error; err != nil {
		return nil, 0, err
	}

	if page < 1 {
		page = 1
	}
	if limit < 1 || limit > 100 {
		limit = 10
	}
	offset := (page - 1) * limit

	err := r.db.WithContext(ctx).
		Preload("Goal", func(db *gorm.DB) *gorm.DB { return db.Unscoped() }).
		Preload("UploadedBy").
		Preload("CurrentState").
		Preload("AssignedTo").
		Where("assigned_to_id = ?", userID).
		Joins("JOIN workflow_states ON workflow_states.id = evidences.current_state_id").
		Where("workflow_states.state_type NOT IN ?", []string{"initial", "terminal"}).
		Order("evidences.updated_at DESC").
		Offset(offset).
		Limit(limit).
		Find(&evidences).Error
	if err != nil {
		return nil, 0, err
	}

	return evidences, total, nil
}

func (r *goalRepository) ListCompletedApprovals(ctx context.Context, userID uuid.UUID, page int, limit int) ([]models.EvidenceTransitionHistory, int64, error) {
	var histories []models.EvidenceTransitionHistory
	var total int64

	// "Completed approvals" must show only reviewer-side transitions the user
	// performed, not their own evidence submissions. Otherwise the uploader's
	// own "submit"/"resubmit" actions appear in their Completed tab — which
	// confused super admins who saw their evidence uploads listed as if they
	// were approvals they had performed.
	query := r.db.WithContext(ctx).Model(&models.EvidenceTransitionHistory{}).
		Joins("LEFT JOIN workflow_transitions wt ON wt.id = evidence_transition_histories.transition_id").
		Where("evidence_transition_histories.performed_by_id = ?", userID).
		Where("evidence_transition_histories.is_system_action = false").
		Where("wt.code IS NULL OR wt.code NOT IN ?", []string{"submit", "resubmit"})

	if err := query.Count(&total).Error; err != nil {
		return nil, 0, err
	}

	if page < 1 {
		page = 1
	}
	if limit < 1 || limit > 100 {
		limit = 10
	}
	offset := (page - 1) * limit

	err := query.
		Preload("Evidence.Goal", func(db *gorm.DB) *gorm.DB { return db.Unscoped() }).
		Preload("Evidence.CurrentState").
		Preload("FromState").
		Preload("ToState").
		Preload("Transition").
		Preload("PerformedBy").
		Order("transitioned_at DESC").
		Offset(offset).
		Limit(limit).
		Find(&histories).Error
	if err != nil {
		return nil, 0, err
	}

	return histories, total, nil
}

// ──────────────────────────────────────────────────
// Transaction Support
// ──────────────────────────────────────────────────

func (r *goalRepository) BeginTx(ctx context.Context) *gorm.DB {
	return r.db.WithContext(ctx).Begin()
}

// ──────────────────────────────────────────────────
// Hierarchy
// ──────────────────────────────────────────────────

func (r *goalRepository) FindChildren(ctx context.Context, parentID uuid.UUID) ([]models.Goal, error) {
	var children []models.Goal
	err := r.db.WithContext(ctx).
		Preload("Owner").
		Where("parent_goal_id = ?", parentID).
		Order("created_at ASC").
		Find(&children).Error
	return children, err
}

func (r *goalRepository) FindDescendantIDs(ctx context.Context, goalPath string) ([]uuid.UUID, error) {
	var ids []uuid.UUID
	err := r.db.WithContext(ctx).
		Model(&models.Goal{}).
		Where("path LIKE ?", goalPath+".%").
		Pluck("id", &ids).Error
	return ids, err
}

// ──────────────────────────────────────────────────
// Check-ins
// ──────────────────────────────────────────────────

func (r *goalRepository) CreateCheckIn(ctx context.Context, checkIn *models.GoalCheckIn) error {
	return r.db.WithContext(ctx).Create(checkIn).Error
}

func (r *goalRepository) ListCheckIns(ctx context.Context, goalID uuid.UUID, page int, limit int) ([]models.GoalCheckIn, int64, error) {
	var checkIns []models.GoalCheckIn
	var total int64

	query := r.db.WithContext(ctx).Model(&models.GoalCheckIn{}).Where("goal_id = ?", goalID)

	if err := query.Count(&total).Error; err != nil {
		return nil, 0, err
	}

	if page < 1 {
		page = 1
	}
	if limit < 1 || limit > 100 {
		limit = 10
	}
	offset := (page - 1) * limit

	err := query.
		Preload("Author").
		Order("created_at DESC").
		Offset(offset).
		Limit(limit).
		Find(&checkIns).Error
	if err != nil {
		return nil, 0, err
	}

	return checkIns, total, nil
}

func (r *goalRepository) FindCheckInByID(ctx context.Context, id uuid.UUID) (*models.GoalCheckIn, error) {
	var checkIn models.GoalCheckIn
	err := r.db.WithContext(ctx).
		Preload("Author").
		First(&checkIn, "id = ?", id).Error
	if err != nil {
		return nil, err
	}
	return &checkIn, nil
}

func (r *goalRepository) DeleteCheckIn(ctx context.Context, id uuid.UUID) error {
	return r.db.WithContext(ctx).Delete(&models.GoalCheckIn{}, "id = ?", id).Error
}

// ──────────────────────────────────────────────────
// Metric Import Batches
// ──────────────────────────────────────────────────

func (r *goalRepository) CreateMetricImportBatch(ctx context.Context, batch *models.MetricImportBatch) error {
	return r.db.WithContext(ctx).Create(batch).Error
}

func (r *goalRepository) FindMetricImportBatchByID(ctx context.Context, id uuid.UUID) (*models.MetricImportBatch, error) {
	var batch models.MetricImportBatch
	err := r.db.WithContext(ctx).First(&batch, "id = ?", id).Error
	if err != nil {
		return nil, err
	}
	return &batch, nil
}

func (r *goalRepository) FindMetricImportBatchByIDWithRelations(ctx context.Context, id uuid.UUID) (*models.MetricImportBatch, error) {
	var batch models.MetricImportBatch
	err := r.db.WithContext(ctx).
		Preload("ImportedBy").
		Preload("PrimaryGoal").
		Preload("CurrentState").
		Preload("AssignedTo").
		Preload("Items", func(db *gorm.DB) *gorm.DB {
			return db.Order("created_at")
		}).
		Preload("Items.Goal").
		Preload("Items.Metric").
		First(&batch, "id = ?", id).Error
	if err != nil {
		return nil, err
	}
	return &batch, nil
}

func (r *goalRepository) FindMetricImportBatchByIDForUpdate(ctx context.Context, tx *gorm.DB, id uuid.UUID) (*models.MetricImportBatch, error) {
	var batch models.MetricImportBatch
	err := tx.WithContext(ctx).
		Set("gorm:query_option", "FOR UPDATE").
		First(&batch, "id = ?", id).Error
	if err != nil {
		return nil, err
	}
	return &batch, nil
}

func (r *goalRepository) UpdateMetricImportBatchInTx(ctx context.Context, tx *gorm.DB, batch *models.MetricImportBatch) error {
	return tx.WithContext(ctx).Save(batch).Error
}

func (r *goalRepository) ListMetricImportBatches(ctx context.Context, filter *models.MetricImportBatchFilter) ([]models.MetricImportBatch, int64, error) {
	var batches []models.MetricImportBatch
	var total int64

	query := r.db.WithContext(ctx).
		Preload("ImportedBy").
		Preload("PrimaryGoal").
		Preload("CurrentState").
		Preload("AssignedTo")

	if filter.Status != "" {
		query = query.Where("status = ?", filter.Status)
	}

	query.Model(&models.MetricImportBatch{}).Count(&total)

	page := filter.Page
	if page < 1 {
		page = 1
	}
	limit := filter.Limit
	if limit < 1 || limit > 100 {
		limit = 20
	}
	offset := (page - 1) * limit

	err := query.Offset(offset).Limit(limit).Order("updated_at DESC").Find(&batches).Error
	return batches, total, err
}

func (r *goalRepository) DeleteMetricImportBatch(ctx context.Context, id uuid.UUID) error {
	return r.db.WithContext(ctx).Delete(&models.MetricImportBatch{}, "id = ?", id).Error
}

func (r *goalRepository) CreateMetricImportItems(ctx context.Context, tx *gorm.DB, items []models.MetricImportItem) error {
	if len(items) == 0 {
		return nil
	}
	return tx.WithContext(ctx).Create(&items).Error
}

func (r *goalRepository) ListMetricImportItemsByBatchID(ctx context.Context, batchID uuid.UUID) ([]models.MetricImportItem, error) {
	var items []models.MetricImportItem
	err := r.db.WithContext(ctx).
		Preload("Goal").
		Preload("Metric").
		Where("batch_id = ?", batchID).
		Order("created_at").
		Find(&items).Error
	return items, err
}

func (r *goalRepository) MarkMetricImportItemsApplied(ctx context.Context, tx *gorm.DB, batchID uuid.UUID) error {
	return tx.WithContext(ctx).
		Model(&models.MetricImportItem{}).
		Where("batch_id = ?", batchID).
		Update("applied", true).Error
}

// ──────────────────────────────────────────────────
// Comments
// ──────────────────────────────────────────────────

func (r *goalRepository) CreateComment(ctx context.Context, comment *models.GoalComment) error {
	return r.db.WithContext(ctx).Create(comment).Error
}

func (r *goalRepository) ListComments(ctx context.Context, goalID uuid.UUID, page, limit int) ([]models.GoalComment, int64, error) {
	var comments []models.GoalComment
	var total int64

	query := r.db.WithContext(ctx).Model(&models.GoalComment{}).Where("goal_id = ?", goalID)

	if err := query.Count(&total).Error; err != nil {
		return nil, 0, err
	}

	if page < 1 {
		page = 1
	}
	if limit < 1 || limit > 100 {
		limit = 20
	}
	offset := (page - 1) * limit

	err := query.
		Preload("Author").
		Order("created_at DESC").
		Offset(offset).
		Limit(limit).
		Find(&comments).Error
	if err != nil {
		return nil, 0, err
	}

	return comments, total, nil
}

func (r *goalRepository) DeleteComment(ctx context.Context, id uuid.UUID) error {
	return r.db.WithContext(ctx).Delete(&models.GoalComment{}, "id = ?", id).Error
}

func (r *goalRepository) FindCommentByID(ctx context.Context, id uuid.UUID) (*models.GoalComment, error) {
	var comment models.GoalComment
	err := r.db.WithContext(ctx).Preload("Author").First(&comment, "id = ?", id).Error
	if err != nil {
		return nil, err
	}
	return &comment, nil
}

func (r *goalRepository) CreateMetricBatchTransitionHistory(ctx context.Context, tx *gorm.DB, h *models.MetricImportBatchTransitionHistory) error {
	return tx.WithContext(ctx).Create(h).Error
}

func (r *goalRepository) ListMetricBatchTransitionHistory(ctx context.Context, batchID uuid.UUID) ([]models.MetricImportBatchTransitionHistory, error) {
	var history []models.MetricImportBatchTransitionHistory
	err := r.db.WithContext(ctx).
		Preload("Transition").
		Preload("FromState").
		Preload("ToState").
		Preload("PerformedBy").
		Where("batch_id = ?", batchID).
		Order("transitioned_at ASC").
		Find(&history).Error
	return history, err
}

func (r *goalRepository) FindGoalByTitleAndOwner(ctx context.Context, title string, ownerID uuid.UUID) (*models.Goal, error) {
	var goal models.Goal
	err := r.db.WithContext(ctx).
		Where("LOWER(title) = LOWER(?) AND owner_id = ?", title, ownerID).
		First(&goal).Error

	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, nil
		}
		return nil, err
	}
	return &goal, nil
}
