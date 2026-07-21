package repository

import (
	"context"
	"fmt"

	"github.com/automax/backend/internal/models"
	"github.com/google/uuid"
	"gorm.io/gorm"
)

type EscalationPolicyRepository interface {
	Create(ctx context.Context, policy *models.EscalationPolicy) error
	Update(ctx context.Context, policy *models.EscalationPolicy) error
	Delete(ctx context.Context, id uuid.UUID) error
	FindByID(ctx context.Context, id uuid.UUID) (*models.EscalationPolicy, error)
	List(ctx context.Context) ([]models.EscalationPolicy, error)
	ListActive(ctx context.Context) ([]models.EscalationPolicy, error)

	// Step management — full replacement of all steps for a policy
	SetSteps(ctx context.Context, policyID uuid.UUID, steps []models.EscalationPolicyStep) error

	// HasStepBeenFired checks if an EscalationSLA record already exists for the
	// given incident + state + policy step (prevents duplicate notifications).
	HasStepBeenFired(ctx context.Context, incidentID, stateID, stepID uuid.UUID) (bool, error)

	// UpdateTargetExcludedUsers replaces excluded_user_ids for one step target.
	// Returns an error if the target does not belong to the given policy.
	UpdateTargetExcludedUsers(ctx context.Context, policyID, targetID uuid.UUID, excludedUserIDs []string) error
}

type escalationPolicyRepository struct {
	db *gorm.DB
}

func NewEscalationPolicyRepository(db *gorm.DB) EscalationPolicyRepository {
	return &escalationPolicyRepository{db: db}
}

func (r *escalationPolicyRepository) Create(ctx context.Context, policy *models.EscalationPolicy) error {
	return r.db.WithContext(ctx).Create(policy).Error
}

func (r *escalationPolicyRepository) Update(ctx context.Context, policy *models.EscalationPolicy) error {
	return r.db.WithContext(ctx).Save(policy).Error
}

func (r *escalationPolicyRepository) Delete(ctx context.Context, id uuid.UUID) error {
	return r.db.WithContext(ctx).Delete(&models.EscalationPolicy{}, "id = ?", id).Error
}

func (r *escalationPolicyRepository) FindByID(ctx context.Context, id uuid.UUID) (*models.EscalationPolicy, error) {
	var policy models.EscalationPolicy
	err := r.db.WithContext(ctx).
		Preload("Steps", func(db *gorm.DB) *gorm.DB {
			return db.Order("step_order ASC")
		}).
		Preload("Steps.Targets").
		Preload("Steps.Targets.Department").
		Preload("Steps.Targets.Role").
		First(&policy, "id = ?", id).Error
	if err != nil {
		return nil, err
	}
	return &policy, nil
}

func (r *escalationPolicyRepository) List(ctx context.Context) ([]models.EscalationPolicy, error) {
	var policies []models.EscalationPolicy
	err := r.db.WithContext(ctx).
		Preload("Steps", func(db *gorm.DB) *gorm.DB {
			return db.Order("step_order ASC")
		}).
		Preload("Steps.Targets").
		Preload("Steps.Targets.Department").
		Preload("Steps.Targets.Role").
		Order("created_at DESC").
		Find(&policies).Error
	return policies, err
}

func (r *escalationPolicyRepository) ListActive(ctx context.Context) ([]models.EscalationPolicy, error) {
	var policies []models.EscalationPolicy
	err := r.db.WithContext(ctx).
		Where("is_active = true").
		Preload("Steps", func(db *gorm.DB) *gorm.DB {
			return db.Order("step_order ASC")
		}).
		Preload("Steps.Targets").
		Preload("Steps.Targets.Department").
		Preload("Steps.Targets.Role").
		Order("created_at DESC").
		Find(&policies).Error
	return policies, err
}

// SetSteps atomically replaces all steps (and their targets) for a policy.
func (r *escalationPolicyRepository) SetSteps(ctx context.Context, policyID uuid.UUID, steps []models.EscalationPolicyStep) error {
	return r.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		// Load existing steps so we can preserve their IDs (matched by step_order).
		// Preserving IDs is critical for deduplication — escalation_slas references step IDs
		// and must remain valid across policy edits.
		var existing []models.EscalationPolicyStep
		if err := tx.Where("policy_id = ?", policyID).Find(&existing).Error; err != nil {
			return err
		}
		existingByOrder := make(map[int]uuid.UUID, len(existing))
		existingIDs := make([]uuid.UUID, 0, len(existing))
		for _, s := range existing {
			existingByOrder[s.StepOrder] = s.ID
			existingIDs = append(existingIDs, s.ID)
		}

		// Explicitly delete targets first. ON DELETE CASCADE is defined in the GORM
		// model tag but may not be enforced on the existing table if the constraint
		// was added after the table was created. Deleting explicitly guarantees no
		// stale targets survive when steps are recreated with the same UUID.
		if len(existingIDs) > 0 {
			if err := tx.Where("step_id IN ?", existingIDs).Delete(&models.EscalationPolicyStepTarget{}).Error; err != nil {
				return err
			}
		}

		// Delete all existing steps.
		if err := tx.Where("policy_id = ?", policyID).Delete(&models.EscalationPolicyStep{}).Error; err != nil {
			return err
		}

		// Re-insert steps, reusing the old UUID when step_order matches so breach log
		// deduplication (HasStepBeenFired) continues to work after a policy edit.
		for i := range steps {
			steps[i].PolicyID = policyID
			if oldID, ok := existingByOrder[steps[i].StepOrder]; ok {
				steps[i].ID = oldID
			}
			if err := tx.Create(&steps[i]).Error; err != nil {
				return err
			}
		}
		return nil
	})
}

func (r *escalationPolicyRepository) HasStepBeenFired(ctx context.Context, incidentID, stateID, stepID uuid.UUID) (bool, error) {
	var count int64
	err := r.db.WithContext(ctx).
		Model(&models.EscalationSLA{}).
		Where("incident_id = ? AND state_id = ? AND escalation_policy_step_id = ?", incidentID, stateID, stepID).
		Count(&count).Error
	return count > 0, err
}

func (r *escalationPolicyRepository) UpdateTargetExcludedUsers(ctx context.Context, policyID, targetID uuid.UUID, excludedUserIDs []string) error {
	// Verify the target belongs to a step that belongs to this policy.
	var count int64
	err := r.db.WithContext(ctx).
		Table("escalation_policy_step_targets t").
		Joins("JOIN escalation_policy_steps s ON s.id = t.step_id").
		Where("t.id = ? AND s.policy_id = ?", targetID, policyID).
		Count(&count).Error
	if err != nil {
		return err
	}
	if count == 0 {
		return fmt.Errorf("target %s not found under policy %s", targetID, policyID)
	}

	ids := models.TextArray(excludedUserIDs)
	if ids == nil {
		ids = models.TextArray{}
	}
	return r.db.WithContext(ctx).
		Model(&models.EscalationPolicyStepTarget{}).
		Where("id = ?", targetID).
		Update("excluded_user_ids", ids).Error
}
