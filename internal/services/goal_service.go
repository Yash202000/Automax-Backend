package services

import (
	"bytes"
	"context"
	"encoding/csv"
	"fmt"
	"log"
	"math"
	"strconv"
	"strings"
	"time"

	"github.com/automax/backend/internal/config"
	"github.com/automax/backend/internal/models"
	"github.com/automax/backend/internal/repository"
	"github.com/automax/backend/internal/storage"
	"github.com/google/uuid"
	"github.com/xuri/excelize/v2"
)

// ════════════════════════════════════════════════════
// GoalService Interface
// ════════════════════════════════════════════════════

type GoalService interface {
	// Goal CRUD
	CreateGoal(ctx context.Context, req *models.GoalCreateRequest, userID uuid.UUID) (*models.GoalResponse, error)
	GetGoal(ctx context.Context, id uuid.UUID) (*models.GoalResponse, error)
	ListGoals(ctx context.Context, filter *models.GoalFilter) ([]models.GoalResponse, int64, error)
	UpdateGoal(ctx context.Context, id uuid.UUID, req *models.GoalUpdateRequest, userID uuid.UUID) (*models.GoalResponse, error)
	DeleteGoal(ctx context.Context, id uuid.UUID, userID uuid.UUID) error

	// Status Transitions
	TransitionGoalStatus(ctx context.Context, id uuid.UUID, newStatus string, userID uuid.UUID) (*models.GoalResponse, error)

	// Collaborators
	AddCollaborator(ctx context.Context, goalID uuid.UUID, req *models.CollaboratorAddRequest, userID uuid.UUID) error
	RemoveCollaborator(ctx context.Context, goalID uuid.UUID, collaboratorUserID uuid.UUID, userID uuid.UUID) error

	// Metrics
	CreateMetric(ctx context.Context, goalID uuid.UUID, req *models.GoalMetricCreateRequest, userID uuid.UUID) (*models.GoalMetricResponse, error)
	UpdateMetric(ctx context.Context, id uuid.UUID, req *models.GoalMetricUpdateRequest, userID uuid.UUID) (*models.GoalMetricResponse, error)
	DeleteMetric(ctx context.Context, id uuid.UUID, userID uuid.UUID) error
	UpdateMetricValue(ctx context.Context, id uuid.UUID, req *models.MetricValueUpdateRequest, userID uuid.UUID) (*models.GoalMetricResponse, error)
	GetMetricHistory(ctx context.Context, metricID uuid.UUID, page int, limit int) ([]models.MetricHistoryResponse, int64, error)

	// Evidence
	CreateEvidence(ctx context.Context, goalID uuid.UUID, title string, evidenceType string, comment string, metricID *uuid.UUID, fileName string, fileSize int64, mimeType string, fileData []byte, userID uuid.UUID) (*models.EvidenceResponse, error)
	DeleteEvidence(ctx context.Context, id uuid.UUID, userID uuid.UUID) error
	ListEvidences(ctx context.Context, goalID uuid.UUID, filter *models.EvidenceFilter) ([]models.EvidenceResponse, int64, error)
	GetEvidence(ctx context.Context, id uuid.UUID) (*models.EvidenceResponse, error)
	GetEvidencePreview(ctx context.Context, evidenceID uuid.UUID) (string, error)
	GetEvidenceDownloadURL(ctx context.Context, evidenceID uuid.UUID) (string, error)

	// Evidence Workflow Transitions
	GetAvailableEvidenceTransitions(ctx context.Context, evidenceID uuid.UUID, userID uuid.UUID) ([]models.AvailableTransitionResponse, error)
	ExecuteEvidenceTransition(ctx context.Context, evidenceID uuid.UUID, req *models.EvidenceTransitionRequest, userID uuid.UUID) (*models.EvidenceResponse, error)
	GetEvidenceTransitionHistory(ctx context.Context, evidenceID uuid.UUID) ([]models.EvidenceTransitionHistoryResponse, error)
	ListPendingApprovals(ctx context.Context, userID uuid.UUID, page int, limit int) ([]models.ApprovalListResponse, int64, error)
	ListCompletedApprovals(ctx context.Context, userID uuid.UUID, page int, limit int) ([]models.ApprovalListResponse, int64, error)

	// Export
	ExportGoalsCSV(ctx context.Context, filter *models.GoalFilter) ([]byte, error)
	ExportGoalsJSON(ctx context.Context, filter *models.GoalFilter) ([]models.GoalResponse, error)

	// Clone
	CloneGoal(ctx context.Context, id uuid.UUID, req *models.GoalCloneRequest, userID uuid.UUID) (*models.GoalResponse, error)

	// Import
	ImportGoals(ctx context.Context, fileData []byte, fileName string, dryRun bool, userID uuid.UUID) (*models.GoalImportResponse, error)

	// Bulk Operations
	BulkAction(ctx context.Context, req *models.BulkActionRequest, userID uuid.UUID) (*models.BulkActionResponse, error)

	// Progress
	RecalculateProgress(ctx context.Context, goalID uuid.UUID) error
}

// ════════════════════════════════════════════════════
// Private Implementation
// ════════════════════════════════════════════════════

type goalService struct {
	goalRepo            repository.GoalRepository
	workflowRepo        repository.WorkflowRepository
	userRepo            repository.UserRepository
	departmentRepo      repository.DepartmentRepository
	documentaClient     storage.DocumentaClient
	notificationService *NotificationService
	actionLogService    ActionLogService
	cfg                 *config.Config
}

func NewGoalService(
	goalRepo repository.GoalRepository,
	workflowRepo repository.WorkflowRepository,
	userRepo repository.UserRepository,
	departmentRepo repository.DepartmentRepository,
	documentaClient storage.DocumentaClient,
	notificationService *NotificationService,
	actionLogService ActionLogService,
	cfg *config.Config,
) GoalService {
	return &goalService{
		goalRepo:            goalRepo,
		workflowRepo:        workflowRepo,
		userRepo:            userRepo,
		departmentRepo:      departmentRepo,
		documentaClient:     documentaClient,
		notificationService: notificationService,
		actionLogService:    actionLogService,
		cfg:                 cfg,
	}
}

// ──────────────────────────────────────────────────
// 1. CreateGoal
// ──────────────────────────────────────────────────

func (s *goalService) CreateGoal(ctx context.Context, req *models.GoalCreateRequest, userID uuid.UUID) (*models.GoalResponse, error) {
	// Validate dates
	if err := req.Validate(); err != nil {
		return nil, err
	}

	// Create Documenta folder for this goal
	folderID, err := s.documentaClient.CreateFolder(ctx, s.cfg.Documenta.WorkspaceName, req.Title)
	if err != nil {
		return nil, fmt.Errorf("failed to create documenta folder: %w", err)
	}

	// Default metadata to empty JSON object
	metadata := req.Metadata
	if strings.TrimSpace(metadata) == "" {
		metadata = "{}"
	}

	goal := &models.Goal{
		Title:             req.Title,
		Description:       req.Description,
		Category:          req.Category,
		Priority:          req.Priority,
		Status:            models.GoalStatusDraft,
		OwnerID:           req.OwnerID,
		DepartmentID:      req.DepartmentID,
		StartDate:         req.StartDate,
		TargetDate:        req.TargetDate,
		ReviewDate:        req.ReviewDate,
		DocumentaFolderID: folderID,
		Metadata:          metadata,
		CreatedByID:       userID,
	}

	if err := s.goalRepo.Create(ctx, goal); err != nil {
		return nil, fmt.Errorf("failed to create goal: %w", err)
	}

	// Audit log
	s.actionLogService.LogAction(ctx, &LogActionParams{
		UserID:      userID,
		Action:      "create",
		Module:      "goals",
		ResourceID:  goal.ID.String(),
		Description: "Created goal: " + goal.Title,
		Status:      "success",
	})

	// Fetch back with relations
	goalWithRelations, err := s.goalRepo.FindByIDWithRelations(ctx, goal.ID)
	if err != nil {
		return nil, fmt.Errorf("failed to fetch created goal: %w", err)
	}

	resp := goalWithRelations.ToResponse()
	return &resp, nil
}

// ──────────────────────────────────────────────────
// 2. GetGoal
// ──────────────────────────────────────────────────

func (s *goalService) GetGoal(ctx context.Context, id uuid.UUID) (*models.GoalResponse, error) {
	goal, err := s.goalRepo.FindByIDWithRelations(ctx, id)
	if err != nil {
		return nil, fmt.Errorf("goal not found: %w", err)
	}

	resp := goal.ToResponse()
	return &resp, nil
}

// ──────────────────────────────────────────────────
// 3. ListGoals
// ──────────────────────────────────────────────────

func (s *goalService) ListGoals(ctx context.Context, filter *models.GoalFilter) ([]models.GoalResponse, int64, error) {
	if filter.Page < 1 {
		filter.Page = 1
	}
	if filter.Limit < 1 {
		filter.Limit = 10
	}

	goals, total, err := s.goalRepo.List(ctx, filter)
	if err != nil {
		return nil, 0, fmt.Errorf("failed to list goals: %w", err)
	}

	responses := make([]models.GoalResponse, len(goals))
	for i, g := range goals {
		responses[i] = g.ToResponse()
	}

	return responses, total, nil
}

// ──────────────────────────────────────────────────
// 4. UpdateGoal
// ──────────────────────────────────────────────────

func (s *goalService) UpdateGoal(ctx context.Context, id uuid.UUID, req *models.GoalUpdateRequest, userID uuid.UUID) (*models.GoalResponse, error) {
	goal, err := s.goalRepo.FindByID(ctx, id)
	if err != nil {
		return nil, fmt.Errorf("goal not found: %w", err)
	}

	// Apply non-nil fields
	if req.Title != nil {
		goal.Title = *req.Title
	}
	if req.Description != nil {
		goal.Description = *req.Description
	}
	if req.Category != nil {
		goal.Category = *req.Category
	}
	if req.Priority != nil {
		goal.Priority = *req.Priority
	}
	if req.DepartmentID != nil {
		goal.DepartmentID = req.DepartmentID
	}
	if req.OwnerID != nil {
		goal.OwnerID = *req.OwnerID
	}
	if req.StartDate != nil {
		goal.StartDate = req.StartDate
	}
	if req.TargetDate != nil {
		goal.TargetDate = req.TargetDate
	}
	if req.ReviewDate != nil {
		goal.ReviewDate = req.ReviewDate
	}
	if req.Metadata != nil {
		goal.Metadata = *req.Metadata
	}

	if err := s.goalRepo.Update(ctx, goal); err != nil {
		return nil, fmt.Errorf("failed to update goal: %w", err)
	}

	// Audit log
	s.actionLogService.LogAction(ctx, &LogActionParams{
		UserID:      userID,
		Action:      "update",
		Module:      "goals",
		ResourceID:  goal.ID.String(),
		Description: "Updated goal: " + goal.Title,
		Status:      "success",
	})

	// Fetch back with relations
	goalWithRelations, err := s.goalRepo.FindByIDWithRelations(ctx, goal.ID)
	if err != nil {
		return nil, fmt.Errorf("failed to fetch updated goal: %w", err)
	}

	resp := goalWithRelations.ToResponse()
	return &resp, nil
}

// ──────────────────────────────────────────────────
// 5. DeleteGoal
// ──────────────────────────────────────────────────

func (s *goalService) DeleteGoal(ctx context.Context, id uuid.UUID, userID uuid.UUID) error {
	goal, err := s.goalRepo.FindByID(ctx, id)
	if err != nil {
		return fmt.Errorf("goal not found: %w", err)
	}

	if err := s.goalRepo.Delete(ctx, id); err != nil {
		return fmt.Errorf("failed to delete goal: %w", err)
	}

	// Audit log
	s.actionLogService.LogAction(ctx, &LogActionParams{
		UserID:      userID,
		Action:      "delete",
		Module:      "goals",
		ResourceID:  goal.ID.String(),
		Description: "Deleted goal: " + goal.Title,
		Status:      "success",
	})

	return nil
}

// ──────────────────────────────────────────────────
// 6. TransitionGoalStatus
// ──────────────────────────────────────────────────

func (s *goalService) TransitionGoalStatus(ctx context.Context, id uuid.UUID, newStatus string, userID uuid.UUID) (*models.GoalResponse, error) {
	goal, err := s.goalRepo.FindByID(ctx, id)
	if err != nil {
		return nil, fmt.Errorf("goal not found: %w", err)
	}

	if !models.IsValidGoalTransition(goal.Status, newStatus) {
		return nil, fmt.Errorf("invalid status transition from %s to %s", goal.Status, newStatus)
	}

	oldStatus := goal.Status
	goal.Status = newStatus

	if err := s.goalRepo.Update(ctx, goal); err != nil {
		return nil, fmt.Errorf("failed to update goal status: %w", err)
	}

	// Audit log
	s.actionLogService.LogAction(ctx, &LogActionParams{
		UserID:      userID,
		Action:      "transition",
		Module:      "goals",
		ResourceID:  goal.ID.String(),
		Description: fmt.Sprintf("Transitioned goal '%s' from %s to %s", goal.Title, oldStatus, newStatus),
		Status:      "success",
	})

	// Fetch back with relations
	goalWithRelations, err := s.goalRepo.FindByIDWithRelations(ctx, goal.ID)
	if err != nil {
		return nil, fmt.Errorf("failed to fetch updated goal: %w", err)
	}

	resp := goalWithRelations.ToResponse()
	return &resp, nil
}

// ──────────────────────────────────────────────────
// 7. AddCollaborator
// ──────────────────────────────────────────────────

func (s *goalService) AddCollaborator(ctx context.Context, goalID uuid.UUID, req *models.CollaboratorAddRequest, userID uuid.UUID) error {
	// Verify goal exists
	goal, err := s.goalRepo.FindByID(ctx, goalID)
	if err != nil {
		return fmt.Errorf("goal not found: %w", err)
	}

	// Verify user exists
	user, err := s.userRepo.FindByID(ctx, req.UserID)
	if err != nil {
		return fmt.Errorf("user not found: %w", err)
	}

	collaborator := &models.GoalCollaborator{
		GoalID: goalID,
		UserID: req.UserID,
		Role:   req.Role,
	}

	if err := s.goalRepo.AddCollaborator(ctx, collaborator); err != nil {
		return fmt.Errorf("failed to add collaborator: %w", err)
	}

	// Send in-app notification
	s.notificationService.SendNotification(
		ctx,
		"notification",
		nil,
		"en",
		[]string{user.Email},
		nil,
		nil,
		"Goal Assignment",
		"You have been assigned as "+req.Role+" to goal: "+goal.Title,
		nil,
		nil,
		&userID,
		nil,
	)

	// Audit log
	s.actionLogService.LogAction(ctx, &LogActionParams{
		UserID:      userID,
		Action:      "create",
		Module:      "goals",
		ResourceID:  goalID.String(),
		Description: fmt.Sprintf("Added collaborator %s (%s) to goal: %s", user.Email, req.Role, goal.Title),
		Status:      "success",
	})

	return nil
}

// ──────────────────────────────────────────────────
// 8. RemoveCollaborator
// ──────────────────────────────────────────────────

func (s *goalService) RemoveCollaborator(ctx context.Context, goalID uuid.UUID, collaboratorUserID uuid.UUID, userID uuid.UUID) error {
	if err := s.goalRepo.RemoveCollaborator(ctx, goalID, collaboratorUserID); err != nil {
		return fmt.Errorf("failed to remove collaborator: %w", err)
	}

	// Audit log
	s.actionLogService.LogAction(ctx, &LogActionParams{
		UserID:      userID,
		Action:      "delete",
		Module:      "goals",
		ResourceID:  goalID.String(),
		Description: fmt.Sprintf("Removed collaborator %s from goal %s", collaboratorUserID.String(), goalID.String()),
		Status:      "success",
	})

	return nil
}

// ──────────────────────────────────────────────────
// 9. CreateMetric
// ──────────────────────────────────────────────────

func (s *goalService) CreateMetric(ctx context.Context, goalID uuid.UUID, req *models.GoalMetricCreateRequest, userID uuid.UUID) (*models.GoalMetricResponse, error) {
	// Verify goal exists
	goal, err := s.goalRepo.FindByID(ctx, goalID)
	if err != nil {
		return nil, fmt.Errorf("goal not found: %w", err)
	}

	// Validate metric type
	if !models.IsValidMetricType(req.MetricType) {
		return nil, fmt.Errorf("invalid metric type: %s", req.MetricType)
	}

	weight := req.Weight
	if weight == 0 {
		weight = 1.0
	}

	metric := &models.GoalMetric{
		GoalID:        goalID,
		Name:          req.Name,
		MetricType:    req.MetricType,
		Unit:          req.Unit,
		BaselineValue: req.BaselineValue,
		CurrentValue:  req.CurrentValue,
		TargetValue:   req.TargetValue,
		Weight:        weight,
	}

	if err := s.goalRepo.CreateMetric(ctx, metric); err != nil {
		return nil, fmt.Errorf("failed to create metric: %w", err)
	}

	// Recalculate goal progress
	if err := s.RecalculateProgress(ctx, goalID); err != nil {
		return nil, fmt.Errorf("failed to recalculate progress: %w", err)
	}

	// Audit log
	s.actionLogService.LogAction(ctx, &LogActionParams{
		UserID:      userID,
		Action:      "create",
		Module:      "goals",
		ResourceID:  metric.ID.String(),
		Description: fmt.Sprintf("Created metric '%s' for goal: %s", metric.Name, goal.Title),
		Status:      "success",
	})

	resp := metric.ToResponse()
	return &resp, nil
}

// ──────────────────────────────────────────────────
// 10. UpdateMetric
// ──────────────────────────────────────────────────

func (s *goalService) UpdateMetric(ctx context.Context, id uuid.UUID, req *models.GoalMetricUpdateRequest, userID uuid.UUID) (*models.GoalMetricResponse, error) {
	metric, err := s.goalRepo.FindMetricByID(ctx, id)
	if err != nil {
		return nil, fmt.Errorf("metric not found: %w", err)
	}

	// Apply non-nil fields
	if req.Name != nil {
		metric.Name = *req.Name
	}
	if req.MetricType != nil {
		metric.MetricType = *req.MetricType
	}
	if req.Unit != nil {
		metric.Unit = *req.Unit
	}
	if req.BaselineValue != nil {
		metric.BaselineValue = *req.BaselineValue
	}
	if req.TargetValue != nil {
		metric.TargetValue = *req.TargetValue
	}
	if req.Weight != nil {
		metric.Weight = *req.Weight
	}

	if err := s.goalRepo.UpdateMetric(ctx, metric); err != nil {
		return nil, fmt.Errorf("failed to update metric: %w", err)
	}

	// Recalculate goal progress
	if err := s.RecalculateProgress(ctx, metric.GoalID); err != nil {
		return nil, fmt.Errorf("failed to recalculate progress: %w", err)
	}

	// Audit log
	s.actionLogService.LogAction(ctx, &LogActionParams{
		UserID:      userID,
		Action:      "update",
		Module:      "goals",
		ResourceID:  metric.ID.String(),
		Description: fmt.Sprintf("Updated metric: %s", metric.Name),
		Status:      "success",
	})

	resp := metric.ToResponse()
	return &resp, nil
}

// ──────────────────────────────────────────────────
// 11. DeleteMetric
// ──────────────────────────────────────────────────

func (s *goalService) DeleteMetric(ctx context.Context, id uuid.UUID, userID uuid.UUID) error {
	// Find metric to get goal ID
	metric, err := s.goalRepo.FindMetricByID(ctx, id)
	if err != nil {
		return fmt.Errorf("metric not found: %w", err)
	}

	goalID := metric.GoalID

	if err := s.goalRepo.DeleteMetric(ctx, id); err != nil {
		return fmt.Errorf("failed to delete metric: %w", err)
	}

	// Recalculate goal progress
	if err := s.RecalculateProgress(ctx, goalID); err != nil {
		return fmt.Errorf("failed to recalculate progress: %w", err)
	}

	// Audit log
	s.actionLogService.LogAction(ctx, &LogActionParams{
		UserID:      userID,
		Action:      "delete",
		Module:      "goals",
		ResourceID:  id.String(),
		Description: fmt.Sprintf("Deleted metric: %s", metric.Name),
		Status:      "success",
	})

	return nil
}

// ──────────────────────────────────────────────────
// 12. UpdateMetricValue
// ──────────────────────────────────────────────────

func (s *goalService) UpdateMetricValue(ctx context.Context, id uuid.UUID, req *models.MetricValueUpdateRequest, userID uuid.UUID) (*models.GoalMetricResponse, error) {
	metric, err := s.goalRepo.FindMetricByID(ctx, id)
	if err != nil {
		return nil, fmt.Errorf("metric not found: %w", err)
	}

	// Create history entry
	history := &models.MetricHistory{
		MetricID:    metric.ID,
		OldValue:    metric.CurrentValue,
		NewValue:    req.Value,
		ChangedByID: userID,
		Comment:     req.Comment,
	}

	if err := s.goalRepo.CreateMetricHistory(ctx, history); err != nil {
		return nil, fmt.Errorf("failed to create metric history: %w", err)
	}

	// Update metric current value
	metric.CurrentValue = req.Value

	if err := s.goalRepo.UpdateMetric(ctx, metric); err != nil {
		return nil, fmt.Errorf("failed to update metric value: %w", err)
	}

	// Recalculate goal progress
	if err := s.RecalculateProgress(ctx, metric.GoalID); err != nil {
		return nil, fmt.Errorf("failed to recalculate progress: %w", err)
	}

	// Audit log
	s.actionLogService.LogAction(ctx, &LogActionParams{
		UserID:      userID,
		Action:      "update",
		Module:      "goals",
		ResourceID:  metric.ID.String(),
		Description: fmt.Sprintf("Updated metric value for '%s': %.2f -> %.2f", metric.Name, history.OldValue, req.Value),
		Status:      "success",
	})

	resp := metric.ToResponse()
	return &resp, nil
}

// ──────────────────────────────────────────────────
// 13. GetMetricHistory
// ──────────────────────────────────────────────────

func (s *goalService) GetMetricHistory(ctx context.Context, metricID uuid.UUID, page int, limit int) ([]models.MetricHistoryResponse, int64, error) {
	if page < 1 {
		page = 1
	}
	if limit < 1 {
		limit = 20
	}

	histories, total, err := s.goalRepo.ListMetricHistory(ctx, metricID, page, limit)
	if err != nil {
		return nil, 0, fmt.Errorf("failed to list metric history: %w", err)
	}

	responses := make([]models.MetricHistoryResponse, len(histories))
	for i, h := range histories {
		responses[i] = h.ToResponse()
	}

	return responses, total, nil
}

// ──────────────────────────────────────────────────
// 14. CreateEvidence
// ──────────────────────────────────────────────────

func (s *goalService) CreateEvidence(ctx context.Context, goalID uuid.UUID, title string, evidenceType string, comment string, metricID *uuid.UUID, fileName string, fileSize int64, mimeType string, fileData []byte, userID uuid.UUID) (*models.EvidenceResponse, error) {
	// Validate comment is non-empty
	if strings.TrimSpace(comment) == "" {
		return nil, fmt.Errorf("comment is mandatory")
	}

	// Find goal to get DocumentaFolderID
	goal, err := s.goalRepo.FindByID(ctx, goalID)
	if err != nil {
		return nil, fmt.Errorf("goal not found: %w", err)
	}

	// Upload file to Documenta
	metadata := map[string]string{
		"goal_title":    goal.Title,
		"evidence_type": evidenceType,
	}

	fileReader := bytes.NewReader(fileData)
	documentaFileID, err := s.documentaClient.UploadFile(ctx, goal.DocumentaFolderID, fileName, fileReader, fileSize, metadata)
	if err != nil {
		return nil, fmt.Errorf("failed to upload file to documenta: %w", err)
	}

	// Resolve the evidence approval workflow and its initial state
	workflows, wfErr := s.workflowRepo.ListByRecordType(ctx, "evidence", true)
	if wfErr != nil || len(workflows) == 0 {
		return nil, fmt.Errorf("no active evidence approval workflow found")
	}
	wf := workflows[0]
	initialState, stateErr := s.workflowRepo.GetInitialState(ctx, wf.ID)
	if stateErr != nil || initialState == nil {
		return nil, fmt.Errorf("evidence workflow has no initial state")
	}

	evidence := &models.Evidence{
		GoalID:          goalID,
		MetricID:        metricID,
		Title:           title,
		EvidenceType:    evidenceType,
		Comment:         comment,
		Status:          models.EvidenceStatusDraft,
		DocumentaFileID: documentaFileID,
		FileName:        fileName,
		FileSize:        fileSize,
		MimeType:        mimeType,
		UploadedByID:    userID,
		WorkflowID:      &wf.ID,
		CurrentStateID:  &initialState.ID,
		Version:         1,
	}

	if err := s.goalRepo.CreateEvidence(ctx, evidence); err != nil {
		return nil, fmt.Errorf("failed to create evidence: %w", err)
	}

	// Audit log
	s.actionLogService.LogAction(ctx, &LogActionParams{
		UserID:      userID,
		Action:      "create",
		Module:      "goals",
		ResourceID:  evidence.ID.String(),
		Description: fmt.Sprintf("Created evidence '%s' for goal: %s", evidence.Title, goal.Title),
		Status:      "success",
	})

	// Reload with relations for response
	evidenceWithRelations, reloadErr := s.goalRepo.FindEvidenceByIDWithRelations(ctx, evidence.ID)
	if reloadErr != nil {
		resp := evidence.ToResponse()
		return &resp, nil
	}
	resp := evidenceWithRelations.ToResponse()
	return &resp, nil
}

// ──────────────────────────────────────────────────
// 15. ListEvidences
// ──────────────────────────────────────────────────

func (s *goalService) ListEvidences(ctx context.Context, goalID uuid.UUID, filter *models.EvidenceFilter) ([]models.EvidenceResponse, int64, error) {
	if filter.Page < 1 {
		filter.Page = 1
	}
	if filter.Limit < 1 {
		filter.Limit = 10
	}

	evidences, total, err := s.goalRepo.ListEvidencesByGoalID(ctx, goalID, filter)
	if err != nil {
		return nil, 0, fmt.Errorf("failed to list evidences: %w", err)
	}

	responses := make([]models.EvidenceResponse, len(evidences))
	for i, e := range evidences {
		responses[i] = e.ToResponse()
	}

	return responses, total, nil
}

// ──────────────────────────────────────────────────
// 16. GetEvidence
// ──────────────────────────────────────────────────

func (s *goalService) GetEvidence(ctx context.Context, id uuid.UUID) (*models.EvidenceResponse, error) {
	evidence, err := s.goalRepo.FindEvidenceByIDWithRelations(ctx, id)
	if err != nil {
		return nil, fmt.Errorf("evidence not found: %w", err)
	}

	resp := evidence.ToResponse()
	return &resp, nil
}

// ──────────────────────────────────────────────────
// 17. DeleteEvidence
// ──────────────────────────────────────────────────

func (s *goalService) DeleteEvidence(ctx context.Context, id uuid.UUID, userID uuid.UUID) error {
	evidence, err := s.goalRepo.FindEvidenceByIDWithRelations(ctx, id)
	if err != nil {
		return fmt.Errorf("evidence not found: %w", err)
	}

	// Only allow deletion when evidence is in Draft state
	if evidence.CurrentState == nil || evidence.CurrentState.Code != "draft" {
		return fmt.Errorf("evidence can only be deleted when in Draft state")
	}

	if err := s.goalRepo.DeleteEvidence(ctx, id); err != nil {
		return fmt.Errorf("failed to delete evidence: %w", err)
	}

	s.actionLogService.LogAction(ctx, &LogActionParams{
		UserID:      userID,
		Action:      "delete",
		Module:      "goals",
		ResourceID:  id.String(),
		Description: fmt.Sprintf("Deleted evidence '%s'", evidence.Title),
		Status:      "success",
	})

	return nil
}

// ──────────────────────────────────────────────────
// 18. GetAvailableEvidenceTransitions
// ──────────────────────────────────────────────────

func (s *goalService) GetAvailableEvidenceTransitions(ctx context.Context, evidenceID uuid.UUID, userID uuid.UUID) ([]models.AvailableTransitionResponse, error) {
	evidence, err := s.goalRepo.FindEvidenceByIDWithRelations(ctx, evidenceID)
	if err != nil {
		return nil, fmt.Errorf("evidence not found: %w", err)
	}

	if evidence.CurrentStateID == nil || evidence.WorkflowID == nil {
		return nil, fmt.Errorf("evidence has no workflow assigned")
	}

	// Get all transitions from current state
	transitions, err := s.workflowRepo.ListTransitionsFromState(ctx, *evidence.CurrentStateID)
	if err != nil {
		return nil, fmt.Errorf("failed to get transitions: %w", err)
	}

	// Get collaborators to determine L2 reviewer existence
	collaborators, err := s.goalRepo.ListCollaborators(ctx, evidence.GoalID)
	if err != nil {
		return nil, fmt.Errorf("failed to list collaborators: %w", err)
	}

	var hasL2Reviewer bool
	for _, c := range collaborators {
		if c.Role == models.CollaboratorRoleReviewerL2 {
			hasL2Reviewer = true
			break
		}
	}

	var results []models.AvailableTransitionResponse
	for _, t := range transitions {
		canExecute := true
		reason := ""

		// Load requirements
		requirements, _ := s.workflowRepo.GetTransitionRequirements(ctx, t.ID)

		switch t.Code {
		case "submit", "resubmit":
			// Only the evidence uploader or goal owner can submit/resubmit
			goal, _ := s.goalRepo.FindByID(ctx, evidence.GoalID)
			if evidence.UploadedByID != userID && (goal == nil || goal.OwnerID != userID) {
				canExecute = false
				reason = "Only the uploader or goal owner can submit"
			}
		case "approve_l1":
			// Only show if L2 reviewer exists; only assigned reviewer can execute
			if !hasL2Reviewer {
				continue // skip this transition, show approve_l1_final instead
			}
			if evidence.AssignedToID == nil || *evidence.AssignedToID != userID {
				canExecute = false
				reason = "Only the assigned reviewer can approve"
			}
		case "approve_l1_final":
			// Only show if NO L2 reviewer; only assigned reviewer can execute
			if hasL2Reviewer {
				continue // skip this transition, show approve_l1 instead
			}
			if evidence.AssignedToID == nil || *evidence.AssignedToID != userID {
				canExecute = false
				reason = "Only the assigned reviewer can approve"
			}
		case "request_changes_l1", "reject_l1", "request_changes_l2", "reject_l2":
			// Only assigned reviewer can reject/request changes
			if evidence.AssignedToID == nil || *evidence.AssignedToID != userID {
				canExecute = false
				reason = "Only the assigned reviewer can perform this action"
			}
		case "approve_l2":
			if evidence.AssignedToID == nil || *evidence.AssignedToID != userID {
				canExecute = false
				reason = "Only the assigned L2 reviewer can approve"
			}
		case "assign_l1":
			// System transition — not available to users
			continue
		}

		// Build requirements response
		reqResponses := make([]models.TransitionRequirementResponse, len(requirements))
		for i, r := range requirements {
			reqResponses[i] = models.ToTransitionRequirementResponse(&r)
		}

		results = append(results, models.AvailableTransitionResponse{
			Transition:   models.ToWorkflowTransitionResponse(&t),
			CanExecute:   canExecute,
			Requirements: reqResponses,
			Reason:       reason,
		})
	}

	return results, nil
}

// ──────────────────────────────────────────────────
// 19. ExecuteEvidenceTransition
// ──────────────────────────────────────────────────

func (s *goalService) ExecuteEvidenceTransition(ctx context.Context, evidenceID uuid.UUID, req *models.EvidenceTransitionRequest, userID uuid.UUID) (*models.EvidenceResponse, error) {
	transitionID, err := uuid.Parse(req.TransitionID)
	if err != nil {
		return nil, fmt.Errorf("invalid transition_id: %w", err)
	}

	// Begin transaction
	tx := s.goalRepo.BeginTx(ctx)
	defer func() {
		if r := recover(); r != nil {
			tx.Rollback()
		}
	}()

	// Pessimistic lock on evidence
	evidence, err := s.goalRepo.FindEvidenceByIDForUpdate(ctx, tx, evidenceID)
	if err != nil {
		tx.Rollback()
		return nil, fmt.Errorf("evidence not found: %w", err)
	}

	// Optimistic concurrency check
	if evidence.Version != req.Version {
		tx.Rollback()
		return nil, fmt.Errorf("evidence has been modified by another user (expected version %d, got %d)", evidence.Version, req.Version)
	}

	if evidence.CurrentStateID == nil || evidence.WorkflowID == nil {
		tx.Rollback()
		return nil, fmt.Errorf("evidence has no workflow assigned")
	}

	// Fetch transition with relations
	transition, err := s.workflowRepo.FindTransitionByIDWithRelations(ctx, transitionID)
	if err != nil {
		tx.Rollback()
		return nil, fmt.Errorf("transition not found: %w", err)
	}

	// Validate transition belongs to evidence's workflow
	if transition.WorkflowID != *evidence.WorkflowID {
		tx.Rollback()
		return nil, fmt.Errorf("transition does not belong to evidence's workflow")
	}

	// Validate transition's from_state matches evidence's current state
	if transition.FromStateID != *evidence.CurrentStateID {
		tx.Rollback()
		return nil, fmt.Errorf("transition is not valid from current state")
	}

	// Authorization check
	switch transition.Code {
	case "submit", "resubmit":
		goal, _ := s.goalRepo.FindByID(ctx, evidence.GoalID)
		if evidence.UploadedByID != userID && (goal == nil || goal.OwnerID != userID) {
			tx.Rollback()
			return nil, fmt.Errorf("only the uploader or goal owner can submit evidence")
		}
	case "approve_l1", "approve_l1_final", "request_changes_l1", "reject_l1", "approve_l2", "request_changes_l2", "reject_l2":
		if evidence.AssignedToID == nil || *evidence.AssignedToID != userID {
			tx.Rollback()
			return nil, fmt.Errorf("only the assigned reviewer can perform this action")
		}
	}

	// Validate requirements (comment required for reject/request_changes)
	requirements, _ := s.workflowRepo.GetTransitionRequirements(ctx, transitionID)
	for _, r := range requirements {
		if r.RequirementType == "comment" && r.IsMandatory != nil && *r.IsMandatory {
			if strings.TrimSpace(req.Comment) == "" {
				tx.Rollback()
				errMsg := r.ErrorMessage
				if errMsg == "" {
					errMsg = "Comment is required for this transition"
				}
				return nil, fmt.Errorf(errMsg)
			}
		}
	}

	fromStateID := *evidence.CurrentStateID

	// Create transition history record
	now := time.Now()
	history := &models.EvidenceTransitionHistory{
		EvidenceID:     evidenceID,
		TransitionID:   &transitionID,
		FromStateID:    fromStateID,
		ToStateID:      transition.ToStateID,
		PerformedByID:  userID,
		Comment:        req.Comment,
		IsSystemAction: false,
		TransitionedAt: now,
	}
	if err := s.goalRepo.CreateTransitionHistory(ctx, tx, history); err != nil {
		tx.Rollback()
		return nil, fmt.Errorf("failed to create transition history: %w", err)
	}

	// Update evidence state
	evidence.CurrentStateID = &transition.ToStateID
	evidence.Version++

	// Get the target state to update the Status string for backward compat
	targetState, _ := s.workflowRepo.FindStateByID(ctx, transition.ToStateID)
	if targetState != nil {
		switch targetState.Code {
		case "draft":
			evidence.Status = models.EvidenceStatusDraft
		case "submitted":
			evidence.Status = models.EvidenceStatusSubmitted
		case "l1_review", "l2_review":
			evidence.Status = models.EvidenceStatusInReview
		case "approved":
			evidence.Status = models.EvidenceStatusApproved
		case "rejected":
			evidence.Status = models.EvidenceStatusRejected
		case "changes_requested":
			evidence.Status = models.EvidenceStatusChangesRequested
		}
	}

	// Assignment logic based on target state
	collaborators, _ := s.goalRepo.ListCollaborators(ctx, evidence.GoalID)

	switch transition.Code {
	case "submit", "resubmit":
		// Auto-transition to l1_review: find L1 reviewer and assign
		var reviewerL1 *models.GoalCollaborator
		for i := range collaborators {
			if collaborators[i].Role == models.CollaboratorRoleReviewerL1 {
				reviewerL1 = &collaborators[i]
				break
			}
		}
		if reviewerL1 == nil {
			tx.Rollback()
			return nil, fmt.Errorf("no L1 reviewer assigned to this goal")
		}

		// Find the l1_review state to auto-advance
		l1State, _ := s.workflowRepo.FindStateByCode(ctx, *evidence.WorkflowID, "l1_review")
		if l1State != nil {
			// Create system transition history for the auto-advance
			autoHistory := &models.EvidenceTransitionHistory{
				EvidenceID:     evidenceID,
				FromStateID:    transition.ToStateID,
				ToStateID:      l1State.ID,
				PerformedByID:  userID,
				Comment:        "Auto-assigned to L1 reviewer",
				IsSystemAction: true,
				TransitionedAt: now,
			}
			s.goalRepo.CreateTransitionHistory(ctx, tx, autoHistory)

			evidence.CurrentStateID = &l1State.ID
			evidence.Status = models.EvidenceStatusInReview
		}

		evidence.AssignedToID = &reviewerL1.UserID

	case "approve_l1":
		// L1 approved → move to L2 review
		var reviewerL2 *models.GoalCollaborator
		for i := range collaborators {
			if collaborators[i].Role == models.CollaboratorRoleReviewerL2 {
				reviewerL2 = &collaborators[i]
				break
			}
		}
		if reviewerL2 != nil {
			evidence.AssignedToID = &reviewerL2.UserID
		}

	case "approve_l1_final", "approve_l2":
		// Final approval → clear assignment, recalculate progress
		evidence.AssignedToID = nil

	case "request_changes_l1", "request_changes_l2", "reject_l1", "reject_l2":
		// Clear assignment
		evidence.AssignedToID = nil
	}

	// Save evidence
	if err := s.goalRepo.UpdateEvidenceInTx(ctx, tx, evidence); err != nil {
		tx.Rollback()
		return nil, fmt.Errorf("failed to update evidence: %w", err)
	}

	// Commit
	if err := tx.Commit().Error; err != nil {
		return nil, fmt.Errorf("failed to commit transaction: %w", err)
	}

	// Post-commit actions (notifications, progress recalc)
	goal, _ := s.goalRepo.FindByID(ctx, evidence.GoalID)
	goalTitle := "a goal"
	if goal != nil {
		goalTitle = goal.Title
	}

	switch transition.Code {
	case "submit", "resubmit":
		// Notify reviewer
		if evidence.AssignedToID != nil {
			reviewer, rErr := s.userRepo.FindByID(ctx, *evidence.AssignedToID)
			if rErr == nil && reviewer != nil {
				s.notificationService.SendNotification(ctx, "notification", nil, "en",
					[]string{reviewer.Email}, nil, nil,
					"Evidence Submitted for Review",
					fmt.Sprintf("Evidence '%s' has been submitted for your review on goal: %s", evidence.Title, goalTitle),
					nil, nil, &userID, nil)
			}
		}

	case "approve_l1":
		// Notify L2 reviewer
		if evidence.AssignedToID != nil {
			reviewer, rErr := s.userRepo.FindByID(ctx, *evidence.AssignedToID)
			if rErr == nil && reviewer != nil {
				s.notificationService.SendNotification(ctx, "notification", nil, "en",
					[]string{reviewer.Email}, nil, nil,
					"Evidence Pending L2 Review",
					fmt.Sprintf("Evidence '%s' for goal '%s' requires your L2 review", evidence.Title, goalTitle),
					nil, nil, &userID, nil)
			}
		}

	case "approve_l1_final", "approve_l2":
		// Recalculate goal progress
		s.RecalculateProgress(ctx, evidence.GoalID)
		// Notify submitter
		submitter, sErr := s.userRepo.FindByID(ctx, evidence.UploadedByID)
		if sErr == nil && submitter != nil {
			s.notificationService.SendNotification(ctx, "notification", nil, "en",
				[]string{submitter.Email}, nil, nil,
				"Evidence Approved",
				fmt.Sprintf("Your evidence '%s' for goal '%s' has been approved", evidence.Title, goalTitle),
				nil, nil, &userID, nil)
		}

	case "reject_l1", "reject_l2":
		// Notify submitter
		submitter, sErr := s.userRepo.FindByID(ctx, evidence.UploadedByID)
		if sErr == nil && submitter != nil {
			s.notificationService.SendNotification(ctx, "notification", nil, "en",
				[]string{submitter.Email}, nil, nil,
				"Evidence Rejected",
				fmt.Sprintf("Your evidence '%s' for goal '%s' has been rejected. Comment: %s", evidence.Title, goalTitle, req.Comment),
				nil, nil, &userID, nil)
		}

	case "request_changes_l1", "request_changes_l2":
		// Notify submitter
		submitter, sErr := s.userRepo.FindByID(ctx, evidence.UploadedByID)
		if sErr == nil && submitter != nil {
			s.notificationService.SendNotification(ctx, "notification", nil, "en",
				[]string{submitter.Email}, nil, nil,
				"Evidence Returned for Changes",
				fmt.Sprintf("Your evidence '%s' for goal '%s' has been returned for changes. Comment: %s", evidence.Title, goalTitle, req.Comment),
				nil, nil, &userID, nil)
		}
	}

	// Audit log
	transitionName := transition.Name
	s.actionLogService.LogAction(ctx, &LogActionParams{
		UserID:      userID,
		Action:      "transition",
		Module:      "goals",
		ResourceID:  evidenceID.String(),
		Description: fmt.Sprintf("Executed '%s' on evidence '%s' for goal: %s", transitionName, evidence.Title, goalTitle),
		Status:      "success",
	})

	// Reload with relations
	evidenceWithRelations, reloadErr := s.goalRepo.FindEvidenceByIDWithRelations(ctx, evidenceID)
	if reloadErr != nil {
		resp := evidence.ToResponse()
		return &resp, nil
	}
	resp := evidenceWithRelations.ToResponse()
	return &resp, nil
}

// ──────────────────────────────────────────────────
// 20. GetEvidenceTransitionHistory
// ──────────────────────────────────────────────────

func (s *goalService) GetEvidenceTransitionHistory(ctx context.Context, evidenceID uuid.UUID) ([]models.EvidenceTransitionHistoryResponse, error) {
	histories, err := s.goalRepo.ListTransitionHistory(ctx, evidenceID)
	if err != nil {
		return nil, fmt.Errorf("failed to get transition history: %w", err)
	}

	responses := make([]models.EvidenceTransitionHistoryResponse, len(histories))
	for i, h := range histories {
		responses[i] = h.ToResponse()
	}

	return responses, nil
}

// ──────────────────────────────────────────────────
// 21. ListPendingApprovals
// ──────────────────────────────────────────────────

func (s *goalService) ListPendingApprovals(ctx context.Context, userID uuid.UUID, page int, limit int) ([]models.ApprovalListResponse, int64, error) {
	if page < 1 {
		page = 1
	}
	if limit < 1 {
		limit = 10
	}

	evidences, total, err := s.goalRepo.ListPendingApprovals(ctx, userID, page, limit)
	if err != nil {
		return nil, 0, fmt.Errorf("failed to list pending approvals: %w", err)
	}

	responses := make([]models.ApprovalListResponse, len(evidences))
	for i, ev := range evidences {
		resp := models.ApprovalListResponse{
			ID:              ev.ID,
			EvidenceID:      ev.ID,
			EvidenceTitle:   ev.Title,
			EvidenceVersion: ev.Version,
			Status:          ev.Status,
			AssignedTo:      models.ToUserBriefResponse(ev.AssignedTo),
			SubmittedBy:     models.ToUserBriefResponse(ev.UploadedBy),
			CreatedAt:       ev.CreatedAt,
			UpdatedAt:       ev.UpdatedAt,
		}

		if ev.CurrentState != nil {
			resp.StateName = ev.CurrentState.Name
			resp.StateColor = ev.CurrentState.Color
		}

		if ev.Goal != nil {
			resp.GoalID = ev.Goal.ID
			resp.GoalTitle = ev.Goal.Title
			resp.GoalPriority = ev.Goal.Priority
		}

		responses[i] = resp
	}

	return responses, total, nil
}

// ──────────────────────────────────────────────────
// 22. ListCompletedApprovals
// ──────────────────────────────────────────────────

func (s *goalService) ListCompletedApprovals(ctx context.Context, userID uuid.UUID, page int, limit int) ([]models.ApprovalListResponse, int64, error) {
	if page < 1 {
		page = 1
	}
	if limit < 1 {
		limit = 10
	}

	histories, total, err := s.goalRepo.ListCompletedApprovals(ctx, userID, page, limit)
	if err != nil {
		return nil, 0, fmt.Errorf("failed to list completed approvals: %w", err)
	}

	responses := make([]models.ApprovalListResponse, len(histories))
	for i, h := range histories {
		resp := models.ApprovalListResponse{
			ID:        h.ID,
			CreatedAt: h.CreatedAt,
			UpdatedAt: h.TransitionedAt,
		}

		if h.PerformedBy != nil {
			resp.SubmittedBy = models.ToUserBriefResponse(h.PerformedBy)
		}

		if h.ToState != nil {
			resp.StateName = h.ToState.Name
			resp.StateColor = h.ToState.Color
			resp.Status = h.ToState.Name
		}

		if h.Evidence != nil {
			resp.EvidenceID = h.Evidence.ID
			resp.EvidenceTitle = h.Evidence.Title
			resp.AssignedTo = models.ToUserBriefResponse(h.Evidence.AssignedTo)

			if h.Evidence.Goal != nil {
				resp.GoalID = h.Evidence.Goal.ID
				resp.GoalTitle = h.Evidence.Goal.Title
				resp.GoalPriority = h.Evidence.Goal.Priority
			}
		}

		responses[i] = resp
	}

	return responses, total, nil
}

// ──────────────────────────────────────────────────
// 21. RecalculateProgress
// ──────────────────────────────────────────────────

func (s *goalService) RecalculateProgress(ctx context.Context, goalID uuid.UUID) error {
	metrics, err := s.goalRepo.ListMetricsByGoalID(ctx, goalID)
	if err != nil {
		return fmt.Errorf("failed to list metrics: %w", err)
	}

	goal, err := s.goalRepo.FindByID(ctx, goalID)
	if err != nil {
		return fmt.Errorf("goal not found: %w", err)
	}

	if len(metrics) == 0 {
		goal.Progress = 0
		return s.goalRepo.Update(ctx, goal)
	}

	var weighted float64
	var totalWeight float64

	for _, m := range metrics {
		var metricProgress float64

		if m.MetricType == models.MetricTypeBoolean {
			// Boolean: if current >= 1 then 1.0 else 0.0
			if m.CurrentValue >= 1 {
				metricProgress = 1.0
			} else {
				metricProgress = 0.0
			}
		} else {
			// Non-boolean: min(current/target, 1.0)
			if m.TargetValue != 0 {
				metricProgress = math.Min(m.CurrentValue/m.TargetValue, 1.0)
			} else {
				metricProgress = 0.0
			}
		}

		weighted += metricProgress * m.Weight
		totalWeight += m.Weight
	}

	// Handle division by zero
	var progress float64
	if totalWeight > 0 {
		progress = (weighted / totalWeight) * 100
	} else {
		progress = 0
	}

	goal.Progress = progress
	return s.goalRepo.Update(ctx, goal)
}

// ──────────────────────────────────────────────────
// 22. GetEvidencePreview
// ──────────────────────────────────────────────────

func (s *goalService) GetEvidencePreview(ctx context.Context, evidenceID uuid.UUID) (string, error) {
	evidence, err := s.goalRepo.FindEvidenceByID(ctx, evidenceID)
	if err != nil {
		return "", fmt.Errorf("evidence not found: %w", err)
	}

	previewURL, err := s.documentaClient.GetPreviewURL(ctx, evidence.DocumentaFileID)
	if err != nil {
		return "", fmt.Errorf("failed to get preview URL: %w", err)
	}

	return previewURL, nil
}

// ──────────────────────────────────────────────────
// 23. GetEvidenceDownloadURL
// ──────────────────────────────────────────────────

func (s *goalService) GetEvidenceDownloadURL(ctx context.Context, evidenceID uuid.UUID) (string, error) {
	evidence, err := s.goalRepo.FindEvidenceByID(ctx, evidenceID)
	if err != nil {
		return "", fmt.Errorf("evidence not found: %w", err)
	}

	downloadURL, err := s.documentaClient.GetDownloadURL(ctx, evidence.DocumentaFileID)
	if err != nil {
		return "", fmt.Errorf("failed to get download URL: %w", err)
	}

	return downloadURL, nil
}

// ──────────────────────────────────────────────────
// Export Goals
// ──────────────────────────────────────────────────

func (s *goalService) ExportGoalsCSV(ctx context.Context, filter *models.GoalFilter) ([]byte, error) {
	goals, err := s.goalRepo.ListForExport(ctx, filter)
	if err != nil {
		return nil, fmt.Errorf("failed to list goals for export: %w", err)
	}

	var buf bytes.Buffer
	writer := csv.NewWriter(&buf)

	// Header row
	header := []string{
		"ID", "Title", "Description", "Category", "Priority", "Status",
		"Owner", "Department", "Start Date", "Target Date", "Review Date",
		"Progress", "Metric Name", "Metric Type", "Metric Unit",
		"Baseline Value", "Current Value", "Target Value", "Weight",
	}
	if err := writer.Write(header); err != nil {
		return nil, fmt.Errorf("failed to write CSV header: %w", err)
	}

	for _, g := range goals {
		if len(g.Metrics) == 0 {
			row := goalToCSVRow(&g, nil)
			if err := writer.Write(row); err != nil {
				return nil, fmt.Errorf("failed to write CSV row: %w", err)
			}
		} else {
			for _, m := range g.Metrics {
				row := goalToCSVRow(&g, &m)
				if err := writer.Write(row); err != nil {
					return nil, fmt.Errorf("failed to write CSV row: %w", err)
				}
			}
		}
	}

	writer.Flush()
	if err := writer.Error(); err != nil {
		return nil, fmt.Errorf("CSV writer error: %w", err)
	}

	return buf.Bytes(), nil
}

func goalToCSVRow(g *models.Goal, m *models.GoalMetric) []string {
	ownerName := ""
	if g.Owner != nil {
		ownerName = g.Owner.Email
	}
	deptName := ""
	if g.Department != nil {
		deptName = g.Department.Name
	}
	startDate := ""
	if g.StartDate != nil {
		startDate = g.StartDate.Format("2006-01-02")
	}
	targetDate := ""
	if g.TargetDate != nil {
		targetDate = g.TargetDate.Format("2006-01-02")
	}
	reviewDate := ""
	if g.ReviewDate != nil {
		reviewDate = g.ReviewDate.Format("2006-01-02")
	}

	row := []string{
		g.ID.String(), g.Title, g.Description, g.Category, g.Priority, g.Status,
		ownerName, deptName, startDate, targetDate, reviewDate,
		fmt.Sprintf("%.1f", g.Progress),
	}

	if m != nil {
		row = append(row,
			m.Name, m.MetricType, m.Unit,
			fmt.Sprintf("%.2f", m.BaselineValue),
			fmt.Sprintf("%.2f", m.CurrentValue),
			fmt.Sprintf("%.2f", m.TargetValue),
			fmt.Sprintf("%.2f", m.Weight),
		)
	} else {
		row = append(row, "", "", "", "", "", "", "")
	}

	return row
}

func (s *goalService) ExportGoalsJSON(ctx context.Context, filter *models.GoalFilter) ([]models.GoalResponse, error) {
	goals, err := s.goalRepo.ListForExport(ctx, filter)
	if err != nil {
		return nil, fmt.Errorf("failed to list goals for export: %w", err)
	}

	responses := make([]models.GoalResponse, len(goals))
	for i, g := range goals {
		responses[i] = g.ToResponse()
	}

	return responses, nil
}

// ──────────────────────────────────────────────────
// Clone Goal
// ──────────────────────────────────────────────────

func (s *goalService) CloneGoal(ctx context.Context, id uuid.UUID, req *models.GoalCloneRequest, userID uuid.UUID) (*models.GoalResponse, error) {
	source, err := s.goalRepo.FindByIDWithRelations(ctx, id)
	if err != nil {
		return nil, fmt.Errorf("source goal not found: %w", err)
	}

	// Determine title
	title := "Copy of " + source.Title
	if req.Title != "" {
		title = req.Title
	}

	// Determine owner
	ownerID := source.OwnerID
	if req.OwnerID != nil {
		ownerID = *req.OwnerID
	}

	// Determine dates
	startDate := source.StartDate
	if req.StartDate != nil {
		startDate = req.StartDate
	}
	targetDate := source.TargetDate
	if req.TargetDate != nil {
		targetDate = req.TargetDate
	}
	reviewDate := source.ReviewDate
	if req.ReviewDate != nil {
		reviewDate = req.ReviewDate
	}

	// Create Documenta folder
	folderID, err := s.documentaClient.CreateFolder(ctx, s.cfg.Documenta.WorkspaceName, title)
	if err != nil {
		log.Printf("[GoalService] CloneGoal: failed to create Documenta folder, continuing without it: %v", err)
		folderID = ""
	}

	clone := &models.Goal{
		Title:             title,
		Description:       source.Description,
		Category:          source.Category,
		Priority:          source.Priority,
		Status:            models.GoalStatusDraft,
		OwnerID:           ownerID,
		DepartmentID:      source.DepartmentID,
		StartDate:         startDate,
		TargetDate:        targetDate,
		ReviewDate:        reviewDate,
		Progress:          0,
		DocumentaFolderID: folderID,
		Metadata:          source.Metadata,
		CreatedByID:       userID,
	}

	if err := s.goalRepo.Create(ctx, clone); err != nil {
		return nil, fmt.Errorf("failed to create cloned goal: %w", err)
	}

	// Clone metrics (reset current value to baseline)
	for _, m := range source.Metrics {
		clonedMetric := &models.GoalMetric{
			GoalID:        clone.ID,
			Name:          m.Name,
			MetricType:    m.MetricType,
			Unit:          m.Unit,
			BaselineValue: m.BaselineValue,
			CurrentValue:  m.BaselineValue,
			TargetValue:   m.TargetValue,
			Weight:        m.Weight,
		}
		if err := s.goalRepo.CreateMetric(ctx, clonedMetric); err != nil {
			log.Printf("[GoalService] CloneGoal: failed to clone metric %s: %v", m.Name, err)
		}
	}

	// Clone collaborators
	for _, c := range source.Collaborators {
		clonedCollab := &models.GoalCollaborator{
			GoalID: clone.ID,
			UserID: c.UserID,
			Role:   c.Role,
		}
		if err := s.goalRepo.AddCollaborator(ctx, clonedCollab); err != nil {
			log.Printf("[GoalService] CloneGoal: failed to clone collaborator %s: %v", c.UserID, err)
		}
	}

	// Audit log
	s.actionLogService.LogAction(ctx, &LogActionParams{
		UserID:      userID,
		Action:      "clone",
		Module:      "goals",
		ResourceID:  clone.ID.String(),
		Description: fmt.Sprintf("Cloned goal from %s: %s", source.ID.String(), title),
		Status:      "success",
	})

	cloneWithRelations, err := s.goalRepo.FindByIDWithRelations(ctx, clone.ID)
	if err != nil {
		return nil, fmt.Errorf("failed to fetch cloned goal: %w", err)
	}

	resp := cloneWithRelations.ToResponse()
	return &resp, nil
}

// ──────────────────────────────────────────────────
// ImportGoals
// ──────────────────────────────────────────────────

func (s *goalService) ImportGoals(ctx context.Context, fileData []byte, fileName string, dryRun bool, userID uuid.UUID) (*models.GoalImportResponse, error) {
	ext := strings.ToLower(fileName[strings.LastIndex(fileName, "."):])

	var rows [][]string
	var err error
	switch ext {
	case ".csv":
		rows, err = s.parseCSV(fileData)
	case ".xlsx":
		rows, err = s.parseXLSX(fileData)
	default:
		return nil, fmt.Errorf("unsupported file type: %s", ext)
	}
	if err != nil {
		return nil, fmt.Errorf("failed to parse file: %w", err)
	}

	if len(rows) < 2 {
		return nil, fmt.Errorf("file must contain a header row and at least one data row")
	}

	// Skip header row
	dataRows := rows[1:]

	// Group rows by title for multi-metric goals
	type parsedMetric struct {
		name      string
		mtype     string
		unit      string
		baseline  string
		current   string
		target    string
		weight    string
		rowNumber int
	}
	type parsedGoal struct {
		title       string
		description string
		category    string
		priority    string
		ownerStr    string
		deptStr     string
		startDate   string
		targetDate  string
		reviewDate  string
		metrics     []parsedMetric
		firstRow    int
	}

	goalMap := make(map[string]*parsedGoal)
	var goalOrder []string
	rowResults := make([]models.ImportRowResult, 0, len(dataRows))

	for i, row := range dataRows {
		rowNum := i + 2 // 1-indexed, skip header
		result := models.ImportRowResult{
			RowNumber: rowNum,
			Status:    "valid",
		}

		// Pad row to expected column count (19 columns matching export format)
		for len(row) < 19 {
			row = append(row, "")
		}

		// Columns: 0=ID, 1=Title, 2=Description, 3=Category, 4=Priority, 5=Status,
		// 6=Owner, 7=Department, 8=Start Date, 9=Target Date, 10=Review Date, 11=Progress,
		// 12=Metric Name, 13=Metric Type, 14=Metric Unit, 15=Baseline Value,
		// 16=Current Value, 17=Target Value, 18=Weight
		title := strings.TrimSpace(row[1])
		if title == "" {
			result.Status = "error"
			result.Errors = append(result.Errors, "Title is required")
			result.GoalTitle = "(empty)"
			rowResults = append(rowResults, result)
			continue
		}
		result.GoalTitle = title

		// Validate priority
		priority := strings.TrimSpace(row[4])
		if priority == "" {
			priority = models.GoalPriorityMedium
			result.Warnings = append(result.Warnings, "Priority defaulted to Medium")
		} else if !models.IsValidGoalPriority(priority) {
			result.Status = "error"
			result.Errors = append(result.Errors, fmt.Sprintf("Invalid priority: %s (must be Critical, High, Medium, or Low)", priority))
		}

		// Validate owner
		ownerStr := strings.TrimSpace(row[6])
		if ownerStr != "" {
			if _, uErr := uuid.Parse(ownerStr); uErr != nil {
				// Try as email
				if !strings.Contains(ownerStr, "@") {
					result.Status = "error"
					result.Errors = append(result.Errors, fmt.Sprintf("Owner must be a valid UUID or email address: %s", ownerStr))
				} else {
					user, uErr := s.userRepo.FindByEmail(ctx, ownerStr)
					if uErr != nil || user == nil {
						result.Status = "error"
						result.Errors = append(result.Errors, fmt.Sprintf("Owner not found: %s", ownerStr))
					}
				}
			} else {
				uid, _ := uuid.Parse(ownerStr)
				user, uErr := s.userRepo.FindByID(ctx, uid)
				if uErr != nil || user == nil {
					result.Status = "error"
					result.Errors = append(result.Errors, fmt.Sprintf("Owner not found: %s", ownerStr))
				}
			}
		} else {
			result.Status = "error"
			result.Errors = append(result.Errors, "Owner is required")
		}

		// Validate department
		deptStr := strings.TrimSpace(row[7])
		if deptStr != "" {
			if _, dErr := uuid.Parse(deptStr); dErr != nil {
				// Try as department name
				dept, dErr := s.departmentRepo.FindByNameAndParent(ctx, deptStr, nil)
				if dErr != nil || dept == nil {
					result.Warnings = append(result.Warnings, fmt.Sprintf("Department not found: %s", deptStr))
				}
			} else {
				did, _ := uuid.Parse(deptStr)
				dept, dErr := s.departmentRepo.FindByID(ctx, did)
				if dErr != nil || dept == nil {
					result.Warnings = append(result.Warnings, fmt.Sprintf("Department not found: %s", deptStr))
				}
			}
		}

		// Validate dates
		startDate := strings.TrimSpace(row[8])
		targetDate := strings.TrimSpace(row[9])
		if startDate != "" {
			if _, dErr := time.Parse("2006-01-02", startDate); dErr != nil {
				result.Status = "error"
				result.Errors = append(result.Errors, fmt.Sprintf("Invalid start date format: %s (use YYYY-MM-DD)", startDate))
			}
		}
		if targetDate != "" {
			if _, dErr := time.Parse("2006-01-02", targetDate); dErr != nil {
				result.Status = "error"
				result.Errors = append(result.Errors, fmt.Sprintf("Invalid target date format: %s (use YYYY-MM-DD)", targetDate))
			}
		}
		reviewDate := strings.TrimSpace(row[10])
		if reviewDate != "" {
			if _, dErr := time.Parse("2006-01-02", reviewDate); dErr != nil {
				result.Status = "error"
				result.Errors = append(result.Errors, fmt.Sprintf("Invalid review date format: %s (use YYYY-MM-DD)", reviewDate))
			}
		}

		// Validate metric fields (if present)
		metricName := strings.TrimSpace(row[12])
		metricType := strings.TrimSpace(row[13])
		if metricName != "" {
			if metricType == "" {
				metricType = models.MetricTypeNumeric
				result.Warnings = append(result.Warnings, "Metric type defaulted to Numeric")
			} else if !models.IsValidMetricType(metricType) {
				result.Status = "error"
				result.Errors = append(result.Errors, fmt.Sprintf("Invalid metric type: %s (must be Numeric, Percentage, Currency, or Boolean)", metricType))
			}
			targetVal := strings.TrimSpace(row[17])
			if targetVal != "" {
				if _, pErr := strconv.ParseFloat(targetVal, 64); pErr != nil {
					result.Status = "error"
					result.Errors = append(result.Errors, fmt.Sprintf("Invalid target value: %s", targetVal))
				}
			}
		}

		if len(result.Warnings) > 0 && result.Status == "valid" {
			result.Status = "warning"
		}

		rowResults = append(rowResults, result)

		// Group into goals
		if _, exists := goalMap[title]; !exists {
			goalMap[title] = &parsedGoal{
				title:       title,
				description: strings.TrimSpace(row[2]),
				category:    strings.TrimSpace(row[3]),
				priority:    priority,
				ownerStr:    ownerStr,
				deptStr:     deptStr,
				startDate:   startDate,
				targetDate:  targetDate,
				reviewDate:  reviewDate,
				firstRow:    rowNum,
			}
			goalOrder = append(goalOrder, title)
		}

		if metricName != "" {
			goalMap[title].metrics = append(goalMap[title].metrics, parsedMetric{
				name:      metricName,
				mtype:     metricType,
				unit:      strings.TrimSpace(row[14]),
				baseline:  strings.TrimSpace(row[15]),
				current:   strings.TrimSpace(row[16]),
				target:    strings.TrimSpace(row[17]),
				weight:    strings.TrimSpace(row[18]),
				rowNumber: rowNum,
			})
		}
	}

	// Count results
	errorCount := 0
	warningCount := 0
	validCount := 0
	metricsCount := 0
	for _, r := range rowResults {
		switch r.Status {
		case "error":
			errorCount++
		case "warning":
			warningCount++
		default:
			validCount++
		}
	}
	for _, g := range goalMap {
		metricsCount += len(g.metrics)
	}

	mode := "dry_run"
	var createdGoalIDs []string

	if !dryRun && errorCount == 0 {
		mode = "committed"
		tx := s.goalRepo.BeginTx(ctx)

		for _, title := range goalOrder {
			pg := goalMap[title]

			// Resolve owner
			var ownerID uuid.UUID
			if _, uErr := uuid.Parse(pg.ownerStr); uErr == nil {
				ownerID, _ = uuid.Parse(pg.ownerStr)
			} else {
				user, _ := s.userRepo.FindByEmail(ctx, pg.ownerStr)
				if user != nil {
					ownerID = user.ID
				}
			}

			// Resolve department
			var departmentID *uuid.UUID
			if pg.deptStr != "" {
				if did, dErr := uuid.Parse(pg.deptStr); dErr == nil {
					departmentID = &did
				} else {
					dept, _ := s.departmentRepo.FindByNameAndParent(ctx, pg.deptStr, nil)
					if dept != nil {
						departmentID = &dept.ID
					}
				}
			}

			// Parse dates
			var startDate, targetDate, reviewDate *time.Time
			if pg.startDate != "" {
				t, _ := time.Parse("2006-01-02", pg.startDate)
				startDate = &t
			}
			if pg.targetDate != "" {
				t, _ := time.Parse("2006-01-02", pg.targetDate)
				targetDate = &t
			}
			if pg.reviewDate != "" {
				t, _ := time.Parse("2006-01-02", pg.reviewDate)
				reviewDate = &t
			}

			// Create Documenta folder
			folderID, fErr := s.documentaClient.CreateFolder(ctx, s.cfg.Documenta.WorkspaceName, pg.title)
			if fErr != nil {
				log.Printf("[GoalService] ImportGoals: failed to create Documenta folder for %q: %v", pg.title, fErr)
				folderID = ""
			}

			goal := &models.Goal{
				ID:                uuid.New(),
				Title:             pg.title,
				Description:       pg.description,
				Category:          pg.category,
				Priority:          pg.priority,
				Status:            models.GoalStatusDraft,
				OwnerID:           ownerID,
				DepartmentID:      departmentID,
				StartDate:         startDate,
				TargetDate:        targetDate,
				ReviewDate:        reviewDate,
				Progress:          0,
				DocumentaFolderID: folderID,
				Metadata:          "{}",
				CreatedByID:       userID,
			}

			if err := tx.WithContext(ctx).Create(goal).Error; err != nil {
				tx.Rollback()
				return nil, fmt.Errorf("failed to create goal %q: %w", pg.title, err)
			}

			// Create metrics
			for _, pm := range pg.metrics {
				baseline, _ := strconv.ParseFloat(pm.baseline, 64)
				current, _ := strconv.ParseFloat(pm.current, 64)
				target, _ := strconv.ParseFloat(pm.target, 64)
				weight, _ := strconv.ParseFloat(pm.weight, 64)
				if weight == 0 {
					weight = 1
				}

				metric := &models.GoalMetric{
					ID:            uuid.New(),
					GoalID:        goal.ID,
					Name:          pm.name,
					MetricType:    pm.mtype,
					Unit:          pm.unit,
					BaselineValue: baseline,
					CurrentValue:  current,
					TargetValue:   target,
					Weight:        weight,
				}

				if err := tx.WithContext(ctx).Create(metric).Error; err != nil {
					tx.Rollback()
					return nil, fmt.Errorf("failed to create metric %q for goal %q: %w", pm.name, pg.title, err)
				}
			}

			createdGoalIDs = append(createdGoalIDs, goal.ID.String())
		}

		if err := tx.Commit().Error; err != nil {
			return nil, fmt.Errorf("failed to commit import transaction: %w", err)
		}

		// Recalculate progress for all created goals
		for _, idStr := range createdGoalIDs {
			gid, _ := uuid.Parse(idStr)
			_ = s.RecalculateProgress(ctx, gid)
		}

		// Audit log
		s.actionLogService.LogAction(ctx, &LogActionParams{
			UserID:      userID,
			Action:      "import",
			Module:      "goals",
			ResourceID:  fmt.Sprintf("%d goals", len(createdGoalIDs)),
			Description: fmt.Sprintf("Imported %d goals with %d metrics from %s", len(createdGoalIDs), metricsCount, fileName),
			Status:      "success",
		})
	}

	return &models.GoalImportResponse{
		Mode:           mode,
		TotalRows:      len(dataRows),
		GoalsCount:     len(goalOrder),
		MetricsCount:   metricsCount,
		ValidCount:     validCount,
		ErrorCount:     errorCount,
		WarningCount:   warningCount,
		Rows:           rowResults,
		CreatedGoalIDs: createdGoalIDs,
	}, nil
}

func (s *goalService) parseCSV(data []byte) ([][]string, error) {
	reader := csv.NewReader(bytes.NewReader(data))
	reader.FieldsPerRecord = -1 // Allow variable fields
	return reader.ReadAll()
}

func (s *goalService) parseXLSX(data []byte) ([][]string, error) {
	f, err := excelize.OpenReader(bytes.NewReader(data))
	if err != nil {
		return nil, err
	}
	defer f.Close()

	sheetName := f.GetSheetName(0)
	if sheetName == "" {
		return nil, fmt.Errorf("no sheets found in XLSX file")
	}

	xlRows, err := f.GetRows(sheetName)
	if err != nil {
		return nil, err
	}

	return xlRows, nil
}

// ──────────────────────────────────────────────────
// BulkAction
// ──────────────────────────────────────────────────

func (s *goalService) BulkAction(ctx context.Context, req *models.BulkActionRequest, userID uuid.UUID) (*models.BulkActionResponse, error) {
	results := make([]models.BulkActionItemResult, len(req.GoalIDs))
	successCount := 0
	failureCount := 0

	for i, goalID := range req.GoalIDs {
		var actionErr error

		switch req.Action {
		case "transition":
			if req.NewStatus == "" {
				actionErr = fmt.Errorf("new_status is required for transition action")
			} else {
				_, actionErr = s.TransitionGoalStatus(ctx, goalID, req.NewStatus, userID)
			}

		case "reassign":
			if req.NewOwnerID == nil {
				actionErr = fmt.Errorf("new_owner_id is required for reassign action")
			} else {
				ownerID := *req.NewOwnerID
				_, actionErr = s.UpdateGoal(ctx, goalID, &models.GoalUpdateRequest{
					OwnerID: &ownerID,
				}, userID)
			}

		case "close":
			_, actionErr = s.TransitionGoalStatus(ctx, goalID, models.GoalStatusClosed, userID)

		default:
			actionErr = fmt.Errorf("unknown action: %s", req.Action)
		}

		result := models.BulkActionItemResult{
			GoalID:  goalID,
			Success: actionErr == nil,
		}
		if actionErr != nil {
			result.Error = actionErr.Error()
			failureCount++
		} else {
			successCount++
		}
		results[i] = result
	}

	// Audit log
	s.actionLogService.LogAction(ctx, &LogActionParams{
		UserID:      userID,
		Action:      "bulk_" + req.Action,
		Module:      "goals",
		ResourceID:  fmt.Sprintf("%d goals", len(req.GoalIDs)),
		Description: fmt.Sprintf("Bulk %s: %d succeeded, %d failed", req.Action, successCount, failureCount),
		Status:      "success",
	})

	return &models.BulkActionResponse{
		TotalRequested: len(req.GoalIDs),
		SuccessCount:   successCount,
		FailureCount:   failureCount,
		Results:        results,
	}, nil
}
