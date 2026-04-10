package repository

import (
	"context"

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
		Order("name ASC").
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
		Order("name ASC").
		Find(&policies).Error
	return policies, err
}

// SetSteps atomically replaces all steps (and their targets) for a policy.
func (r *escalationPolicyRepository) SetSteps(ctx context.Context, policyID uuid.UUID, steps []models.EscalationPolicyStep) error {
	return r.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		// Delete existing steps (cascade deletes targets)
		if err := tx.Where("policy_id = ?", policyID).Delete(&models.EscalationPolicyStep{}).Error; err != nil {
			return err
		}
		// Insert new steps
		for i := range steps {
			steps[i].PolicyID = policyID
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
