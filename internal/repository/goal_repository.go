package repository

import (
	"context"
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

	query := r.db.WithContext(ctx).Model(&models.Evidence{}).Where("goal_id = ?", goalID)

	if filter.Status != "" {
		query = query.Where("status = ?", filter.Status)
	}
	if filter.Search != "" {
		searchPattern := "%" + filter.Search + "%"
		query = query.Where("(title ILIKE ? OR file_name ILIKE ?)", searchPattern, searchPattern)
	}
	if filter.EvidenceType != "" {
		query = query.Where("evidence_type = ?", filter.EvidenceType)
	}
	if filter.StartDate != "" {
		query = query.Where("created_at >= ?", filter.StartDate)
	}
	if filter.EndDate != "" {
		query = query.Where("created_at <= ?", filter.EndDate+"T23:59:59Z")
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
		Order("created_at DESC").
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

	query := r.db.WithContext(ctx).Model(&models.EvidenceTransitionHistory{}).
		Where("performed_by_id = ? AND is_system_action = false", userID)

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
