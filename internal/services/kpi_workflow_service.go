package services

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/automax/backend/internal/models"
	"github.com/automax/backend/internal/repository"
	"github.com/automax/backend/pkg/constants"
	"github.com/automax/backend/pkg/i18n"
	"github.com/google/uuid"
	"gorm.io/gorm"
)

type KpiWorkflowService struct {
	db           *gorm.DB
	workflowRepo repository.WorkflowRepository
}

func NewKpiWorkflowService(db *gorm.DB, workflowRepo repository.WorkflowRepository) *KpiWorkflowService {
	return &KpiWorkflowService{
		db:           db,
		workflowRepo: workflowRepo,
	}
}

func (s *KpiWorkflowService) InitiateKpiPerformanceWorkflow(ctx context.Context, performanceID uuid.UUID, userID uuid.UUID) (*models.KpiWorkflowInstance, error) {
	var wf models.Workflow
	if err := s.db.WithContext(ctx).Where("record_type = ? AND is_active = ?", "kpi_performance", true).First(&wf).Error; err != nil {
		return nil, fmt.Errorf("no active kpi_performance workflow found: %w", err)
	}

	initialState, err := s.workflowRepo.GetInitialState(ctx, wf.ID)
	if err != nil {
		return nil, fmt.Errorf("workflow has no initial state: %w", err)
	}

	instance := &models.KpiWorkflowInstance{
		WorkflowID:     wf.ID,
		EntityType:     models.KpiWFEntityPerformance,
		EntityID:       performanceID.String(),
		CurrentStateID: initialState.ID,
		InitiatedByID:  userID,
		Status:         models.KpiWFStatusActive,
	}

	if err := s.db.WithContext(ctx).Create(instance).Error; err != nil {
		return nil, fmt.Errorf("failed to create workflow instance: %w", err)
	}

	if err := s.db.WithContext(ctx).Model(&models.KpiPerformance{}).Where("id = ?", performanceID).
		Update("workflow_instance_id", instance.ID).Error; err != nil {
		return nil, fmt.Errorf("failed to link workflow instance to performance: %w", err)
	}

	return instance, nil
}

func (s *KpiWorkflowService) TransitionKpiPerformance(ctx context.Context, performanceID uuid.UUID, req *models.MetricTransitionRequest, userID uuid.UUID) (*models.KpiPerformanceResponse, error) {
	transitionID, err := uuid.Parse(req.TransitionID)
	if err != nil {
		return nil, fmt.Errorf("%s: %w", i18n.T(ctx, "invalid_transition_id_svc"), err)
	}

	tx := s.db.Begin()
	defer func() {
		if r := recover(); r != nil {
			tx.Rollback()
		}
	}()

	var perf models.KpiPerformance
	if err := tx.WithContext(ctx).Set("gorm:query_option", "FOR UPDATE").First(&perf, performanceID).Error; err != nil {
		tx.Rollback()
		return nil, fmt.Errorf("performance not found: %w", err)
	}

	if perf.WorkflowInstanceID == nil {
		var wf models.Workflow
		if err := tx.WithContext(ctx).Where("record_type = ? AND is_active = ?", "kpi_performance", true).First(&wf).Error; err != nil {
			tx.Rollback()
			return nil, fmt.Errorf("no active kpi_performance workflow found: %w", err)
		}
		initialState, err := s.workflowRepo.GetInitialState(ctx, wf.ID)
		if err != nil {
			tx.Rollback()
			return nil, fmt.Errorf("workflow has no initial state: %w", err)
		}
		instance := &models.KpiWorkflowInstance{
			WorkflowID:     wf.ID,
			EntityType:     models.KpiWFEntityPerformance,
			EntityID:       performanceID.String(),
			CurrentStateID: initialState.ID,
			InitiatedByID:  userID,
			Status:         models.KpiWFStatusActive,
		}
		if err := tx.WithContext(ctx).Create(instance).Error; err != nil {
			tx.Rollback()
			return nil, fmt.Errorf("failed to create workflow instance: %w", err)
		}
		perf.WorkflowInstanceID = &instance.ID
		if err := tx.WithContext(ctx).Model(&perf).Update("workflow_instance_id", instance.ID).Error; err != nil {
			tx.Rollback()
			return nil, fmt.Errorf("failed to link workflow instance: %w", err)
		}
	}

	var wfInstance models.KpiWorkflowInstance
	if err := tx.WithContext(ctx).First(&wfInstance, *perf.WorkflowInstanceID).Error; err != nil {
		tx.Rollback()
		return nil, fmt.Errorf("workflow instance not found: %w", err)
	}

	transition, err := s.workflowRepo.FindTransitionByIDWithRelations(ctx, transitionID)
	if err != nil {
		tx.Rollback()
		return nil, fmt.Errorf("%s: %w", i18n.T(ctx, "transition_not_found"), err)
	}

	if transition.WorkflowID != wfInstance.WorkflowID {
		tx.Rollback()
		return nil, fmt.Errorf("transition does not belong to the performance's workflow")
	}
	if transition.FromStateID != wfInstance.CurrentStateID {
		tx.Rollback()
		return nil, fmt.Errorf("%s", i18n.T(ctx, "transition_invalid_from_state"))
	}

	// Service-level permission check — not just route-level
	if !s.userHasPermission(ctx, transitionPermissionCode(transition.Code)) {
		tx.Rollback()
		return nil, fmt.Errorf("insufficient permissions for transition '%s'", transition.Code)
	}

	fromStateID := wfInstance.CurrentStateID

	requirements, _ := s.workflowRepo.GetTransitionRequirements(ctx, transitionID)
	for _, r := range requirements {
		if r.RequirementType == "comment" && r.IsMandatory != nil && *r.IsMandatory {
			if strings.TrimSpace(req.Comment) == "" {
				tx.Rollback()
				errMsg := r.ErrorMessage
				if errMsg == "" {
					errMsg = "Comment is required for this transition"
				}
				return nil, fmt.Errorf("%s", errMsg)
			}
		}
	}

	action := &models.KpiWorkflowAction{
		WorkflowInstanceID: wfInstance.ID,
		TransitionID:       transitionID,
		FromStateID:        fromStateID,
		ToStateID:          transition.ToStateID,
		PerformedByID:      userID,
		Comment:            req.Comment,
		PerformedAt:        time.Now(),
	}
	if err := tx.WithContext(ctx).Create(action).Error; err != nil {
		tx.Rollback()
		return nil, fmt.Errorf("failed to create workflow action: %w", err)
	}

	wfInstance.CurrentStateID = transition.ToStateID
	if err := tx.WithContext(ctx).Save(&wfInstance).Error; err != nil {
		tx.Rollback()
		return nil, fmt.Errorf("failed to update workflow instance state: %w", err)
	}

	perf.Status = transition.ToState.Code

	switch transition.Code {
	case "submit":
		perf.SubmittedByID = &userID
	case "approve", "approve_l1", "approve_l2", "approve_l1_final":
		perf.ApprovedByID = &userID
	}

	updateFields := map[string]interface{}{
		"status": perf.Status,
	}
	if perf.SubmittedByID != nil {
		updateFields["submitted_by_id"] = *perf.SubmittedByID
	}
	if perf.ApprovedByID != nil {
		updateFields["approved_by_id"] = *perf.ApprovedByID
	}
	if err := tx.WithContext(ctx).Model(&perf).Updates(updateFields).Error; err != nil {
		tx.Rollback()
		return nil, fmt.Errorf("failed to update performance: %w", err)
	}

	if err := tx.Commit().Error; err != nil {
		return nil, fmt.Errorf("failed to commit transaction: %w", err)
	}

	var reloaded models.KpiPerformance
	s.db.WithContext(ctx).Preload("SubmittedBy").Preload("ApprovedBy").First(&reloaded, performanceID)
	resp := reloaded.ToResponse()
	return &resp, nil
}

func (s *KpiWorkflowService) ensureEntryWorkflowInstance(ctx context.Context, tx *gorm.DB, entry *models.KpiEntry, userID uuid.UUID) error {
	if entry.WorkflowInstanceID != nil {
		return nil
	}
	var wf models.Workflow
	if err := tx.WithContext(ctx).Where("record_type = ? AND is_active = ?", "kpi_entry", true).First(&wf).Error; err != nil {
		return fmt.Errorf("no active kpi_entry workflow found: %w", err)
	}
	initialState, err := s.workflowRepo.GetInitialState(ctx, wf.ID)
	if err != nil {
		return fmt.Errorf("workflow has no initial state: %w", err)
	}
	instance := &models.KpiWorkflowInstance{
		WorkflowID:     wf.ID,
		EntityType:     models.KpiWFEntityEntry,
		EntityID:       entry.ID.String(),
		CurrentStateID: initialState.ID,
		InitiatedByID:  userID,
		Status:         models.KpiWFStatusActive,
	}
	if err := tx.WithContext(ctx).Create(instance).Error; err != nil {
		return fmt.Errorf("failed to create workflow instance: %w", err)
	}
	entry.WorkflowInstanceID = &instance.ID
	return tx.WithContext(ctx).Model(entry).Update("workflow_instance_id", instance.ID).Error
}

// TransitionKpiEntry moves a KpiEntry through the kpi_entry workflow, mirroring
// TransitionKpiPerformance: row-locks the entry, auto-creates a workflow
// instance on first use, validates the transition against the current state,
// enforces mandatory-comment requirements, checks permissions, and records an
// audit action. Reuses the same perf:* permission codes as KpiPerformance
// (submit/review/approve/reject/publish/request_changes) so approvers don't
// need a second set of permissions provisioned for entries.
func (s *KpiWorkflowService) TransitionKpiEntry(ctx context.Context, entryID uuid.UUID, req *models.MetricTransitionRequest, userID uuid.UUID) (*models.KpiEntry, error) {
	transitionID, err := uuid.Parse(req.TransitionID)
	if err != nil {
		return nil, fmt.Errorf("%s: %w", i18n.T(ctx, "invalid_transition_id_svc"), err)
	}

	tx := s.db.Begin()
	defer func() {
		if r := recover(); r != nil {
			tx.Rollback()
		}
	}()

	var entry models.KpiEntry
	if err := tx.WithContext(ctx).Set("gorm:query_option", "FOR UPDATE").First(&entry, entryID).Error; err != nil {
		tx.Rollback()
		return nil, fmt.Errorf("entry not found: %w", err)
	}

	if err := s.ensureEntryWorkflowInstance(ctx, tx, &entry, userID); err != nil {
		tx.Rollback()
		return nil, err
	}

	var wfInstance models.KpiWorkflowInstance
	if err := tx.WithContext(ctx).First(&wfInstance, *entry.WorkflowInstanceID).Error; err != nil {
		tx.Rollback()
		return nil, fmt.Errorf("workflow instance not found: %w", err)
	}

	transition, err := s.workflowRepo.FindTransitionByIDWithRelations(ctx, transitionID)
	if err != nil {
		tx.Rollback()
		return nil, fmt.Errorf("%s: %w", i18n.T(ctx, "transition_not_found"), err)
	}

	if transition.WorkflowID != wfInstance.WorkflowID {
		tx.Rollback()
		return nil, fmt.Errorf("transition does not belong to the entry's workflow")
	}
	if transition.FromStateID != wfInstance.CurrentStateID {
		tx.Rollback()
		return nil, fmt.Errorf("%s", i18n.T(ctx, "transition_invalid_from_state"))
	}

	if !s.userHasPermission(ctx, transitionPermissionCode(transition.Code)) {
		tx.Rollback()
		return nil, fmt.Errorf("insufficient permissions for transition '%s'", transition.Code)
	}

	fromStateID := wfInstance.CurrentStateID

	requirements, _ := s.workflowRepo.GetTransitionRequirements(ctx, transitionID)
	for _, r := range requirements {
		if r.RequirementType == "comment" && r.IsMandatory != nil && *r.IsMandatory {
			if strings.TrimSpace(req.Comment) == "" {
				tx.Rollback()
				errMsg := r.ErrorMessage
				if errMsg == "" {
					errMsg = "Comment is required for this transition"
				}
				return nil, fmt.Errorf("%s", errMsg)
			}
		}
	}

	action := &models.KpiWorkflowAction{
		WorkflowInstanceID: wfInstance.ID,
		TransitionID:       transitionID,
		FromStateID:        fromStateID,
		ToStateID:          transition.ToStateID,
		PerformedByID:      userID,
		Comment:            req.Comment,
		PerformedAt:        time.Now(),
	}
	if err := tx.WithContext(ctx).Create(action).Error; err != nil {
		tx.Rollback()
		return nil, fmt.Errorf("failed to create workflow action: %w", err)
	}

	wfInstance.CurrentStateID = transition.ToStateID
	if err := tx.WithContext(ctx).Save(&wfInstance).Error; err != nil {
		tx.Rollback()
		return nil, fmt.Errorf("failed to update workflow instance state: %w", err)
	}

	entry.Status = transition.ToState.Code
	updateFields := map[string]interface{}{"status": entry.Status}
	switch transition.Code {
	case "submit":
		entry.SubmittedByID = &userID
		updateFields["submitted_by_id"] = userID
	case "approve", "approve_l1", "approve_l2", "approve_l1_final":
		entry.ApprovedByID = &userID
		updateFields["approved_by_id"] = userID
	}
	if err := tx.WithContext(ctx).Model(&entry).Updates(updateFields).Error; err != nil {
		tx.Rollback()
		return nil, fmt.Errorf("failed to update entry: %w", err)
	}

	if err := tx.Commit().Error; err != nil {
		return nil, fmt.Errorf("failed to commit transaction: %w", err)
	}

	var reloaded models.KpiEntry
	s.db.WithContext(ctx).Preload("Metric").Preload("SubmittedBy").Preload("ApprovedBy").First(&reloaded, entryID)
	return &reloaded, nil
}

// GetAvailableKpiEntryTransitions lists the transitions available from an
// entry's current workflow state, auto-initiating the workflow instance (in
// its own transaction) if the entry hasn't entered one yet.
func (s *KpiWorkflowService) GetAvailableKpiEntryTransitions(ctx context.Context, entryID uuid.UUID, userID uuid.UUID) ([]models.WorkflowTransition, error) {
	var entry models.KpiEntry
	if err := s.db.WithContext(ctx).First(&entry, entryID).Error; err != nil {
		return nil, fmt.Errorf("entry not found: %w", err)
	}

	if entry.WorkflowInstanceID == nil {
		if err := s.ensureEntryWorkflowInstance(ctx, s.db, &entry, userID); err != nil {
			return nil, err
		}
	}

	var wfInstance models.KpiWorkflowInstance
	if err := s.db.WithContext(ctx).First(&wfInstance, *entry.WorkflowInstanceID).Error; err != nil {
		return nil, fmt.Errorf("workflow instance not found: %w", err)
	}

	var transitions []models.WorkflowTransition
	if err := s.db.WithContext(ctx).
		Preload("FromState").
		Preload("ToState").
		Preload("Requirements").
		Where("from_state_id = ? AND workflow_id = ? AND is_active = ?", wfInstance.CurrentStateID, wfInstance.WorkflowID, true).
		Order("sort_order, name").
		Find(&transitions).Error; err != nil {
		return nil, err
	}

	return transitions, nil
}

// transitionPermissionCode maps a workflow transition code to a granular permission
func transitionPermissionCode(code string) string {
	switch code {
	case "submit":
		return "perf:submit"
	case "review":
		return "perf:review"
	case "approve", "approve_l1", "approve_l2", "approve_l1_final":
		return "perf:approve"
	case "reject":
		return "perf:reject"
	case "publish":
		return "perf:publish"
	case "request_changes":
		return "perf:request_changes"
	default:
		return "perf:review"
	}
}

func (s *KpiWorkflowService) userHasPermission(ctx context.Context, code string) bool {
	user, ok := ctx.Value(constants.ContextKeys.User).(*models.User)
	if !ok || user == nil {
		return false
	}
	return user.HasPermission(code)
}

func (s *KpiWorkflowService) ensureWorkflowInstance(ctx context.Context, perf *models.KpiPerformance, userID uuid.UUID) error {
	if perf.WorkflowInstanceID != nil {
		return nil
	}
	instance, err := s.InitiateKpiPerformanceWorkflow(ctx, perf.ID, userID)
	if err != nil {
		return err
	}
	perf.WorkflowInstanceID = &instance.ID
	return nil
}

func (s *KpiWorkflowService) GetAvailableKpiPerformanceTransitions(ctx context.Context, performanceID uuid.UUID, userID uuid.UUID) ([]models.WorkflowTransition, error) {
	var perf models.KpiPerformance
	if err := s.db.WithContext(ctx).First(&perf, performanceID).Error; err != nil {
		return nil, fmt.Errorf("performance not found: %w", err)
	}

	if err := s.ensureWorkflowInstance(ctx, &perf, userID); err != nil {
		return nil, err
	}

	var wfInstance models.KpiWorkflowInstance
	if err := s.db.WithContext(ctx).First(&wfInstance, *perf.WorkflowInstanceID).Error; err != nil {
		return nil, fmt.Errorf("workflow instance not found: %w", err)
	}

	var transitions []models.WorkflowTransition
	if err := s.db.WithContext(ctx).
		Preload("FromState").
		Preload("ToState").
		Preload("Requirements").
		Where("from_state_id = ? AND workflow_id = ? AND is_active = ?", wfInstance.CurrentStateID, wfInstance.WorkflowID, true).
		Order("sort_order, name").
		Find(&transitions).Error; err != nil {
		return nil, err
	}

	return transitions, nil
}
