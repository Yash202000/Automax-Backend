package services

import (
	"context"
	"fmt"
	"time"

	"github.com/automax/backend/internal/models"
	"github.com/automax/backend/pkg/i18n"
	"github.com/google/uuid"
	"gorm.io/gorm"
)

// kpiDictionaryWorkflowRecordType is the Workflow.RecordType this service
// looks up ("KPI Workflow" in the seed, kpi_dictionary_workflow_seed.go).
const kpiDictionaryWorkflowRecordType = "kpi_dictionary"

// loadDictionaryKpiState reads just the two columns this service needs
// (activation_status, workflow_instance_id) from whichever KPI dictionary
// table kpiType names, without loading the rest of the (large) row.
func loadDictionaryKpiState(db *gorm.DB, kpiType string, kpiID uuid.UUID) (activationStatus string, workflowInstanceID *uuid.UUID, err error) {
	switch kpiType {
	case models.KPITypeOperational:
		var k models.OperationalKPI
		err = db.Select("activation_status", "workflow_instance_id").Where("id = ?", kpiID).First(&k).Error
		return k.ActivationStatus, k.WorkflowInstanceID, err
	case models.KPITypeAward:
		var k models.AwardKPI
		err = db.Select("activation_status", "workflow_instance_id").Where("id = ?", kpiID).First(&k).Error
		return k.ActivationStatus, k.WorkflowInstanceID, err
	default:
		var k models.StrategicKPI
		err = db.Select("activation_status", "workflow_instance_id").Where("id = ?", kpiID).First(&k).Error
		return k.ActivationStatus, k.WorkflowInstanceID, err
	}
}

// updateDictionaryKpiWorkflow writes the workflow_instance_id + activation_status
// (denormalized mirror of the workflow instance's current state) back onto
// the KPI dictionary row.
func updateDictionaryKpiWorkflow(db *gorm.DB, kpiType string, kpiID uuid.UUID, instanceID uuid.UUID, stateCode string) error {
	updates := map[string]interface{}{"workflow_instance_id": instanceID, "activation_status": stateCode}
	switch kpiType {
	case models.KPITypeOperational:
		return db.Model(&models.OperationalKPI{}).Where("id = ?", kpiID).Updates(updates).Error
	case models.KPITypeAward:
		return db.Model(&models.AwardKPI{}).Where("id = ?", kpiID).Updates(updates).Error
	default:
		return db.Model(&models.StrategicKPI{}).Where("id = ?", kpiID).Updates(updates).Error
	}
}

// InitiateKpiDictionaryWorkflow eagerly assigns the "KPI Workflow" to a
// newly created KPI, per the requirement that every new KPI automatically
// starts the Draft→Reviewed→Approved→Active→Closed lifecycle — unlike
// KpiEntry/KpiPerformance, which only get a workflow instance lazily on
// first transition attempt.
func (s *KpiWorkflowService) InitiateKpiDictionaryWorkflow(ctx context.Context, kpiType string, kpiID uuid.UUID, userID uuid.UUID) error {
	var wf models.Workflow
	if err := s.db.WithContext(ctx).Where("record_type = ? AND is_active = ?", kpiDictionaryWorkflowRecordType, true).First(&wf).Error; err != nil {
		return fmt.Errorf("no active kpi_dictionary workflow found: %w", err)
	}
	initialState, err := s.workflowRepo.GetInitialState(ctx, wf.ID)
	if err != nil {
		return fmt.Errorf("workflow has no initial state: %w", err)
	}

	instance := &models.KpiWorkflowInstance{
		WorkflowID:     wf.ID,
		EntityType:     models.KpiWFEntityDictionary,
		EntityID:       kpiID.String(),
		CurrentStateID: initialState.ID,
		InitiatedByID:  userID,
		Status:         models.KpiWFStatusActive,
	}
	if err := s.db.WithContext(ctx).Create(instance).Error; err != nil {
		return fmt.Errorf("failed to create workflow instance: %w", err)
	}

	return updateDictionaryKpiWorkflow(s.db.WithContext(ctx), kpiType, kpiID, instance.ID, initialState.Code)
}

// ensureDictionaryWorkflowInstance is the lazy self-heal fallback for KPIs
// that predate this feature (e.g. seed_goal_demo.go's demo KPIs) — mirrors
// ensureEntryWorkflowInstance, but initializes the new instance's state to
// whichever of the 5 KPI Workflow states the KPI's existing activation_status
// already names (all pre-existing demo rows say "active", a valid state-4
// code), rather than always resetting to Draft.
func (s *KpiWorkflowService) ensureDictionaryWorkflowInstance(ctx context.Context, tx *gorm.DB, kpiType string, kpiID uuid.UUID, userID uuid.UUID) (*models.KpiWorkflowInstance, error) {
	activationStatus, workflowInstanceID, err := loadDictionaryKpiState(tx.WithContext(ctx), kpiType, kpiID)
	if err != nil {
		return nil, fmt.Errorf("kpi not found: %w", err)
	}
	if workflowInstanceID != nil {
		var instance models.KpiWorkflowInstance
		if err := tx.WithContext(ctx).First(&instance, *workflowInstanceID).Error; err != nil {
			return nil, fmt.Errorf("workflow instance not found: %w", err)
		}
		return &instance, nil
	}

	var wf models.Workflow
	if err := tx.WithContext(ctx).Where("record_type = ? AND is_active = ?", kpiDictionaryWorkflowRecordType, true).First(&wf).Error; err != nil {
		return nil, fmt.Errorf("no active kpi_dictionary workflow found: %w", err)
	}

	startState, err := s.workflowRepo.GetInitialState(ctx, wf.ID)
	if err != nil {
		return nil, fmt.Errorf("workflow has no initial state: %w", err)
	}
	if activationStatus != "" {
		var matched models.WorkflowState
		if err := tx.WithContext(ctx).Where("workflow_id = ? AND code = ? AND is_active = ?", wf.ID, activationStatus, true).First(&matched).Error; err == nil {
			startState = &matched
		}
	}

	instance := &models.KpiWorkflowInstance{
		WorkflowID:     wf.ID,
		EntityType:     models.KpiWFEntityDictionary,
		EntityID:       kpiID.String(),
		CurrentStateID: startState.ID,
		InitiatedByID:  userID,
		Status:         models.KpiWFStatusActive,
	}
	if err := tx.WithContext(ctx).Create(instance).Error; err != nil {
		return nil, fmt.Errorf("failed to create workflow instance: %w", err)
	}
	if err := updateDictionaryKpiWorkflow(tx.WithContext(ctx), kpiType, kpiID, instance.ID, startState.Code); err != nil {
		return nil, fmt.Errorf("failed to link workflow instance to kpi: %w", err)
	}
	return instance, nil
}

// userDictionaryRoleCodes loads the calling user's active role codes (plus
// whether they're a super admin), for checking against a
// WorkflowTransition's AllowedRoles.
func (s *KpiWorkflowService) loadUserForRoleCheck(ctx context.Context, userID uuid.UUID) (*models.User, error) {
	var user models.User
	if err := s.db.WithContext(ctx).Preload("Roles", "is_active = ?", true).First(&user, "id = ?", userID).Error; err != nil {
		return nil, err
	}
	return &user, nil
}

// userHasAnyAllowedRole reports whether the user holds at least one of a
// transition's AllowedRoles (or is a super admin) — the KPI Workflow's own
// role-gating mechanism, read directly from the configured
// WorkflowTransition.AllowedRoles rather than a hardcoded permission-code
// switch, per the requirement that "transition permissions must be
// validated based on the configured roles."
func userHasAnyAllowedRole(user *models.User, allowedRoles []models.Role) bool {
	if user.IsSuperAdmin {
		return true
	}
	for _, role := range allowedRoles {
		if user.HasRole(role.Code) {
			return true
		}
	}
	return false
}

// GetAvailableKpiDictionaryTransitions lists the transitions the calling
// user may currently execute on this KPI — self-healing the workflow
// instance if needed, then filtering to only transitions where the user
// holds one of the transition's AllowedRoles. This filters server-side
// (unlike the sibling KpiEntry/KpiPerformance endpoints, which return every
// transition valid from the current state regardless of caller) because the
// requirement is explicit that the available button depends on the user's
// role, not just the KPI's state.
func (s *KpiWorkflowService) GetAvailableKpiDictionaryTransitions(ctx context.Context, kpiType string, kpiID uuid.UUID, userID uuid.UUID) ([]models.WorkflowTransition, error) {
	instance, err := s.ensureDictionaryWorkflowInstance(ctx, s.db, kpiType, kpiID, userID)
	if err != nil {
		return nil, err
	}

	var transitions []models.WorkflowTransition
	if err := s.db.WithContext(ctx).
		Preload("FromState").
		Preload("ToState").
		Preload("AllowedRoles").
		Where("from_state_id = ? AND workflow_id = ? AND is_active = ?", instance.CurrentStateID, instance.WorkflowID, true).
		Order("sort_order, name").
		Find(&transitions).Error; err != nil {
		return nil, err
	}

	user, err := s.loadUserForRoleCheck(ctx, userID)
	if err != nil {
		return nil, err
	}

	allowed := make([]models.WorkflowTransition, 0, len(transitions))
	for _, t := range transitions {
		if userHasAnyAllowedRole(user, t.AllowedRoles) {
			allowed = append(allowed, t)
		}
	}
	return allowed, nil
}

// TransitionKpiDictionary executes a KPI Workflow transition on a KPI
// dictionary row (Strategic/Operational/Award), enforcing: no skipping
// states, role authorization via the transition's configured AllowedRoles,
// and (for the Approved→Active transition specifically, matched by the
// resolved target state's code rather than a hardcoded transition code) the
// existing BR-07 metric-weight-sum gate. Returns the new activation_status
// (= the target state's code) on success.
func (s *KpiWorkflowService) TransitionKpiDictionary(ctx context.Context, kpiType string, kpiID uuid.UUID, req *models.MetricTransitionRequest, userID uuid.UUID) (string, error) {
	transitionID, err := uuid.Parse(req.TransitionID)
	if err != nil {
		return "", fmt.Errorf("%s: %w", i18n.T(ctx, "invalid_transition_id_svc"), err)
	}

	tx := s.db.Begin()
	defer func() {
		if r := recover(); r != nil {
			tx.Rollback()
		}
	}()

	instance, err := s.ensureDictionaryWorkflowInstance(ctx, tx, kpiType, kpiID, userID)
	if err != nil {
		tx.Rollback()
		return "", err
	}

	transition, err := s.workflowRepo.FindTransitionByIDWithRelations(ctx, transitionID)
	if err != nil {
		tx.Rollback()
		return "", fmt.Errorf("%s: %w", i18n.T(ctx, "transition_not_found"), err)
	}

	if transition.WorkflowID != instance.WorkflowID {
		tx.Rollback()
		return "", fmt.Errorf("transition does not belong to this KPI's workflow")
	}
	if transition.FromStateID != instance.CurrentStateID {
		tx.Rollback()
		return "", fmt.Errorf("%s", i18n.T(ctx, "transition_invalid_from_state"))
	}

	user, err := s.loadUserForRoleCheck(ctx, userID)
	if err != nil {
		tx.Rollback()
		return "", fmt.Errorf("failed to load user: %w", err)
	}
	if !userHasAnyAllowedRole(user, transition.AllowedRoles) {
		tx.Rollback()
		return "", fmt.Errorf("you do not have a role authorized to perform the '%s' transition", transition.Name)
	}

	// BR-07: the sum of an Active composite KPI's metric weights must equal
	// 100% before it can move into the Active state — otherwise
	// achievement*weight contributions silently under- or over-count instead
	// of forming one proper composite score. Matched by the resolved TO
	// state's code, not a hardcoded transition code, so it stays correct even
	// if the "Activate" transition is ever renamed.
	if transition.ToState != nil && transition.ToState.Code == models.KPIStatusActive {
		valid, sum, err := MetricWeightSumValid(tx, kpiID, kpiType)
		if err != nil {
			tx.Rollback()
			return "", fmt.Errorf("failed to validate metric weights: %w", err)
		}
		if !valid {
			tx.Rollback()
			return "", fmt.Errorf("metric weights must sum to 100%% before activation (currently %.2f%%)", sum)
		}
	}

	action := &models.KpiWorkflowAction{
		WorkflowInstanceID: instance.ID,
		TransitionID:       transitionID,
		FromStateID:        instance.CurrentStateID,
		ToStateID:          transition.ToStateID,
		PerformedByID:      userID,
		Comment:            req.Comment,
		PerformedAt:        time.Now(),
	}
	if err := tx.WithContext(ctx).Create(action).Error; err != nil {
		tx.Rollback()
		return "", fmt.Errorf("failed to create workflow action: %w", err)
	}

	instance.CurrentStateID = transition.ToStateID
	if err := tx.WithContext(ctx).Save(instance).Error; err != nil {
		tx.Rollback()
		return "", fmt.Errorf("failed to update workflow instance state: %w", err)
	}

	newStatus := transition.ToState.Code
	if err := updateDictionaryKpiWorkflow(tx.WithContext(ctx), kpiType, kpiID, instance.ID, newStatus); err != nil {
		tx.Rollback()
		return "", fmt.Errorf("failed to update kpi status: %w", err)
	}

	if err := tx.Commit().Error; err != nil {
		return "", fmt.Errorf("failed to commit transaction: %w", err)
	}
	return newStatus, nil
}
