package services

import (
	"context"
	"errors"
	"fmt"
	"math"
	"strings"
	"time"

	"github.com/automax/backend/internal/models"
	"github.com/automax/backend/pkg/i18n"
	"github.com/google/uuid"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

// Errors returned by the KPI Workflow service so handlers can answer with
// the right status: ErrKpiDictionaryNotFound -> 404,
// ErrKpiTransitionForbidden -> 403, ErrKpiTransitionInvalid -> 400 (unknown
// transition, transition from another state/workflow, BR-07 gate).
var (
	ErrKpiDictionaryNotFound  = errors.New("kpi not found")
	ErrKpiTransitionForbidden = errors.New("kpi transition not allowed for this user")
	ErrKpiTransitionInvalid   = errors.New("invalid kpi transition")
)

// kpiWorkflowError wraps a sentinel with the user-facing message.
type kpiWorkflowError struct {
	kind error
	msg  string
}

func (e *kpiWorkflowError) Error() string { return e.msg }
func (e *kpiWorkflowError) Unwrap() error { return e.kind }

func kpiWFErr(kind error, format string, args ...interface{}) error {
	return &kpiWorkflowError{kind: kind, msg: fmt.Sprintf(format, args...)}
}

// kpiTableFor returns the dictionary table of a KPI type.
func kpiTableFor(kpiType string) string {
	switch kpiType {
	case models.KPITypeOperational:
		return "operational_kpis"
	case models.KPITypeAward:
		return "award_kpis"
	default:
		return "strategic_kpis"
	}
}

// lockDictionaryKpi takes a row lock on the (non-deleted) KPI so concurrent
// workflow operations on the same KPI serialize. Returns
// ErrKpiDictionaryNotFound when the KPI does not exist.
func lockDictionaryKpi(tx *gorm.DB, kpiType string, kpiID uuid.UUID) error {
	var row struct{ ID uuid.UUID }
	if err := tx.Table(kpiTableFor(kpiType)).Select("id").
		Where("id = ? AND deleted_at IS NULL", kpiID).
		Clauses(clause.Locking{Strength: "UPDATE"}).
		Limit(1).Scan(&row).Error; err != nil {
		return err
	}
	if row.ID == uuid.Nil {
		return kpiWFErr(ErrKpiDictionaryNotFound, "KPI not found")
	}
	return nil
}

// The KPI Workflow ("Draft->Reviewed->Approved->Active->Closed") is looked
// up by KpiWorkflowService.kpiDictionaryWorkflowCode (a Workflow.Code,
// configurable via KPI_DICTIONARY_WORKFLOW_CODE — see config.go), not by
// RecordType. RecordType is a plain editable field on the generic Workflow
// admin designer, whose own record-type dropdown doesn't offer
// "kpi_dictionary" as an option, so opening and saving this workflow there
// silently resets RecordType to whatever that UI's default is (observed in
// practice: "all") and breaks a record_type-based lookup — Code isn't
// exposed to that same reset.

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

// GetKpiActivationStatus returns a KPI dictionary row's current
// activation_status (its KPI Workflow state code) — exported for callers
// outside this package (e.g. metric-entry creation) that need to gate an
// action on the KPI's current lifecycle state without pulling in the rest of
// the workflow-transition machinery.
func GetKpiActivationStatus(db *gorm.DB, kpiType string, kpiID uuid.UUID) (string, error) {
	status, _, err := loadDictionaryKpiState(db, kpiType, kpiID)
	return status, err
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
	if err := s.db.WithContext(ctx).Where("code = ? AND is_active = ?", s.kpiDictionaryWorkflowCode, true).First(&wf).Error; err != nil {
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
//
// The caller must hold the KPI's row lock (lockDictionaryKpi) in tx, so two
// concurrent first calls cannot both create an instance; use
// getOrCreateDictionaryWorkflowInstance outside a transaction.
func (s *KpiWorkflowService) ensureDictionaryWorkflowInstance(ctx context.Context, tx *gorm.DB, kpiType string, kpiID uuid.UUID, userID uuid.UUID) (*models.KpiWorkflowInstance, error) {
	activationStatus, workflowInstanceID, err := loadDictionaryKpiState(tx.WithContext(ctx), kpiType, kpiID)
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, kpiWFErr(ErrKpiDictionaryNotFound, "KPI not found")
	}
	if err != nil {
		return nil, fmt.Errorf("loading kpi: %w", err)
	}
	if workflowInstanceID != nil {
		var instance models.KpiWorkflowInstance
		if err := tx.WithContext(ctx).Clauses(clause.Locking{Strength: "UPDATE"}).First(&instance, *workflowInstanceID).Error; err != nil {
			return nil, fmt.Errorf("workflow instance not found: %w", err)
		}
		return &instance, nil
	}

	var wf models.Workflow
	if err := tx.WithContext(ctx).Where("code = ? AND is_active = ?", s.kpiDictionaryWorkflowCode, true).First(&wf).Error; err != nil {
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

// getOrCreateDictionaryWorkflowInstance returns the KPI's workflow
// instance, creating it (self-heal for KPIs that predate the KPI Workflow)
// under the KPI's row lock so concurrent readers never create duplicates.
// The common case — the instance already exists — takes no lock and writes
// nothing.
func (s *KpiWorkflowService) getOrCreateDictionaryWorkflowInstance(ctx context.Context, kpiType string, kpiID uuid.UUID, userID uuid.UUID) (*models.KpiWorkflowInstance, error) {
	_, workflowInstanceID, err := loadDictionaryKpiState(s.db.WithContext(ctx), kpiType, kpiID)
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, kpiWFErr(ErrKpiDictionaryNotFound, "KPI not found")
	}
	if err != nil {
		return nil, err
	}
	if workflowInstanceID != nil {
		var instance models.KpiWorkflowInstance
		if err := s.db.WithContext(ctx).First(&instance, *workflowInstanceID).Error; err == nil {
			return &instance, nil
		}
	}
	var instance *models.KpiWorkflowInstance
	err = s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		if err := lockDictionaryKpi(tx, kpiType, kpiID); err != nil {
			return err
		}
		var err error
		instance, err = s.ensureDictionaryWorkflowInstance(ctx, tx, kpiType, kpiID, userID)
		return err
	})
	return instance, err
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
	instance, err := s.getOrCreateDictionaryWorkflowInstance(ctx, kpiType, kpiID, userID)
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
// states, and role authorization via the transition's configured
// AllowedRoles, and the BR-07 gate on entering Active (at least one Active
// metric, weights summing to 100%). Returns the new activation_status
// (= the target state's code) on success.
func (s *KpiWorkflowService) TransitionKpiDictionary(ctx context.Context, kpiType string, kpiID uuid.UUID, req *models.MetricTransitionRequest, userID uuid.UUID) (string, error) {
	transitionID, err := uuid.Parse(req.TransitionID)
	if err != nil {
		return "", kpiWFErr(ErrKpiTransitionInvalid, "%s", i18n.T(ctx, "invalid_transition_id_svc"))
	}

	var newStatus string
	err = s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		// Lock the KPI row first: concurrent transitions (or a transition
		// racing the lazy instance creation) on the same KPI serialize here,
		// and each re-reads the instance's current state after the lock, so
		// N identical requests produce exactly one state change.
		if err := lockDictionaryKpi(tx, kpiType, kpiID); err != nil {
			return err
		}
		instance, err := s.ensureDictionaryWorkflowInstance(ctx, tx, kpiType, kpiID, userID)
		if err != nil {
			return err
		}

		transition, err := s.workflowRepo.FindTransitionByIDWithRelations(ctx, transitionID)
		if err != nil {
			return kpiWFErr(ErrKpiTransitionInvalid, "%s", i18n.T(ctx, "transition_not_found"))
		}
		if transition.WorkflowID != instance.WorkflowID {
			return kpiWFErr(ErrKpiTransitionInvalid, "transition does not belong to this KPI's workflow")
		}
		if transition.FromStateID != instance.CurrentStateID {
			return kpiWFErr(ErrKpiTransitionInvalid, "%s", i18n.T(ctx, "transition_invalid_from_state"))
		}
		if !transition.IsActive {
			return kpiWFErr(ErrKpiTransitionInvalid, "%s", i18n.T(ctx, "transition_not_found"))
		}

		user, err := s.loadUserForRoleCheck(ctx, userID)
		if err != nil {
			return fmt.Errorf("failed to load user: %w", err)
		}
		if !userHasAnyAllowedRole(user, transition.AllowedRoles) {
			return kpiWFErr(ErrKpiTransitionForbidden, "you do not have a role authorized to perform the '%s' transition", transition.Name)
		}

		// BR-07: matched by the resolved TO state's code, not a hardcoded
		// transition code, so it stays correct if "Activate" is ever renamed.
		if transition.ToState != nil && transition.ToState.Code == models.KPIStatusActive {
			count, sum, err := ActiveMetricWeightSum(tx, kpiID, kpiType)
			if err != nil {
				return fmt.Errorf("failed to validate metric weights: %w", err)
			}
			if count == 0 {
				return kpiWFErr(ErrKpiTransitionInvalid, "%s", i18n.T(ctx, "kpi_activate_no_metrics"))
			}
			if math.Abs(sum-100) >= 0.01 {
				return kpiWFErr(ErrKpiTransitionInvalid, "%s", i18n.Tf(ctx, "kpi_activate_weight_not_100", sum))
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
		if err := tx.Create(action).Error; err != nil {
			return fmt.Errorf("failed to create workflow action: %w", err)
		}

		updates := map[string]interface{}{"current_state_id": transition.ToStateID}
		if transition.ToState != nil && transition.ToState.StateType == "terminal" {
			updates["status"] = models.KpiWFStatusCompleted
		}
		if err := tx.Model(&models.KpiWorkflowInstance{}).Where("id = ?", instance.ID).Updates(updates).Error; err != nil {
			return fmt.Errorf("failed to update workflow instance state: %w", err)
		}

		newStatus = transition.ToState.Code
		if err := updateDictionaryKpiWorkflow(tx, kpiType, kpiID, instance.ID, newStatus); err != nil {
			return fmt.Errorf("failed to update kpi status: %w", err)
		}
		return nil
	})
	if err != nil {
		return "", err
	}
	return newStatus, nil
}

// CancelKpiDictionaryWorkflow marks a deleted KPI's workflow instance
// cancelled so it no longer shows as an active lifecycle. Pass the
// transaction the KPI is deleted in.
func CancelKpiDictionaryWorkflow(tx *gorm.DB, kpiID uuid.UUID) error {
	return tx.Model(&models.KpiWorkflowInstance{}).
		Where("entity_type = ? AND entity_id = ? AND status = ?", models.KpiWFEntityDictionary, kpiID.String(), models.KpiWFStatusActive).
		Update("status", models.KpiWFStatusCancelled).Error
}

// CanEditKpiDefinition reports whether the user may edit (or delete) a KPI's
// definition — its fields and metric configuration — in the KPI's current
// KPI Workflow state, per that state's configured Editable Roles:
//   - the Super Admin always may;
//   - a terminal state (Closed) is read-only for everyone but the Administrator role;
//   - otherwise the user needs one of the state's Editable Roles (seeded: every
//     role in Draft/Reviewed/Approved, Administrator only in Active);
//   - a state with no Editable Roles configured does not restrict editing.
//
// KPIs without a matching workflow state (legacy activation_status values)
// are editable while draft/reviewed and admin-only otherwise. Returns the
// state code for the error message.
func (s *KpiWorkflowService) CanEditKpiDefinition(ctx context.Context, kpiType string, kpiID uuid.UUID, userID uuid.UUID) (bool, string, error) {
	db := s.db.WithContext(ctx)
	activationStatus, workflowInstanceID, err := loadDictionaryKpiState(db, kpiType, kpiID)
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return false, "", kpiWFErr(ErrKpiDictionaryNotFound, "KPI not found")
	}
	if err != nil {
		return false, "", err
	}
	user, err := s.loadUserForRoleCheck(ctx, userID)
	if err != nil {
		return false, activationStatus, err
	}
	if user.IsSuperAdmin {
		return true, activationStatus, nil
	}

	var state models.WorkflowState
	found := false
	if workflowInstanceID != nil {
		var instance models.KpiWorkflowInstance
		if db.Select("current_state_id").First(&instance, *workflowInstanceID).Error == nil &&
			db.Preload("EditableRoles").First(&state, instance.CurrentStateID).Error == nil {
			found = true
		}
	}
	if !found && activationStatus != "" {
		var wf models.Workflow
		if db.Where("code = ? AND is_active = ?", s.kpiDictionaryWorkflowCode, true).First(&wf).Error == nil &&
			db.Preload("EditableRoles").Where("workflow_id = ? AND code = ?", wf.ID, activationStatus).First(&state).Error == nil {
			found = true
		}
	}
	isAdmin := user.HasRole("admin")
	if !found {
		st := strings.ToLower(activationStatus)
		return st == "" || st == models.KPIStatusDraft || st == models.KPIStatusReviewed || isAdmin, activationStatus, nil
	}
	if state.StateType == "terminal" || state.Code == models.KPIStatusClosed {
		return isAdmin, state.Code, nil
	}
	if len(state.EditableRoles) == 0 {
		return true, state.Code, nil
	}
	for _, r := range state.EditableRoles {
		if user.HasRole(r.Code) {
			return true, state.Code, nil
		}
	}
	return false, state.Code, nil
}
