package services

import (
	"context"
	"fmt"

	"github.com/automax/backend/internal/models"
	"github.com/automax/backend/internal/repository"
	"github.com/google/uuid"
)

// EscalationPolicyService handles CRUD for escalation policies and provides a
// shared helper for resolving the target user list from dept+role+exclusions.
type EscalationPolicyService struct {
	repo     repository.EscalationPolicyRepository
	userRepo repository.UserRepository
}

func NewEscalationPolicyService(repo repository.EscalationPolicyRepository, userRepo repository.UserRepository) *EscalationPolicyService {
	return &EscalationPolicyService{repo: repo, userRepo: userRepo}
}

// ─── CRUD ─────────────────────────────────────────────────────────────────────

func (s *EscalationPolicyService) Create(ctx context.Context, req *models.CreateEscalationPolicyRequest) (*models.EscalationPolicyResponse, error) {
	isActive := true
	if req.IsActive != nil {
		isActive = *req.IsActive
	}

	policy := &models.EscalationPolicy{
		Name:        req.Name,
		Description: req.Description,
		IsActive:    isActive,
	}

	if err := s.repo.Create(ctx, policy); err != nil {
		return nil, err
	}

	if len(req.Steps) > 0 {
		steps, err := buildSteps(req.Steps)
		if err != nil {
			return nil, err
		}
		if err := s.repo.SetSteps(ctx, policy.ID, steps); err != nil {
			return nil, err
		}
	}

	created, err := s.repo.FindByID(ctx, policy.ID)
	if err != nil {
		return nil, err
	}
	resp := models.ToEscalationPolicyResponse(created)
	return &resp, nil
}

func (s *EscalationPolicyService) Update(ctx context.Context, id uuid.UUID, req *models.UpdateEscalationPolicyRequest) (*models.EscalationPolicyResponse, error) {
	policy, err := s.repo.FindByID(ctx, id)
	if err != nil {
		return nil, fmt.Errorf("escalation policy not found")
	}

	if req.Name != nil {
		policy.Name = *req.Name
	}
	if req.Description != nil {
		policy.Description = *req.Description
	}
	if req.IsActive != nil {
		policy.IsActive = *req.IsActive
	}

	if err := s.repo.Update(ctx, policy); err != nil {
		return nil, err
	}

	// nil Steps slice = no change; non-nil (even empty) = full replacement
	if req.Steps != nil {
		steps, err := buildSteps(req.Steps)
		if err != nil {
			return nil, err
		}
		if err := s.repo.SetSteps(ctx, id, steps); err != nil {
			return nil, err
		}
	}

	updated, err := s.repo.FindByID(ctx, id)
	if err != nil {
		return nil, err
	}
	resp := models.ToEscalationPolicyResponse(updated)
	return &resp, nil
}

func (s *EscalationPolicyService) Delete(ctx context.Context, id uuid.UUID) error {
	if _, err := s.repo.FindByID(ctx, id); err != nil {
		return fmt.Errorf("escalation policy not found")
	}
	return s.repo.Delete(ctx, id)
}

func (s *EscalationPolicyService) GetByID(ctx context.Context, id uuid.UUID) (*models.EscalationPolicyResponse, error) {
	policy, err := s.repo.FindByID(ctx, id)
	if err != nil {
		return nil, fmt.Errorf("escalation policy not found")
	}
	resp := models.ToEscalationPolicyResponse(policy)
	return &resp, nil
}

func (s *EscalationPolicyService) List(ctx context.Context) ([]models.EscalationPolicyResponse, error) {
	policies, err := s.repo.List(ctx)
	if err != nil {
		return nil, err
	}
	result := make([]models.EscalationPolicyResponse, len(policies))
	for i, p := range policies {
		result[i] = models.ToEscalationPolicyResponse(&p)
	}
	return result, nil
}

// ResolveUsers returns the active users for a given dept+role combination,
// minus any explicitly excluded user IDs. Used by both the escalation group
// service and the policy-driven escalation engine.
func (s *EscalationPolicyService) ResolveUsers(ctx context.Context, departmentID, roleID *uuid.UUID, excludedUserIDs []string) ([]models.User, error) {
	users, err := s.userRepo.FindByDepartmentAndRole(ctx, departmentID, roleID)
	if err != nil {
		return nil, err
	}
	if len(excludedUserIDs) == 0 {
		return users, nil
	}

	excluded := make(map[string]struct{}, len(excludedUserIDs))
	for _, id := range excludedUserIDs {
		excluded[id] = struct{}{}
	}

	filtered := users[:0]
	for _, u := range users {
		if _, skip := excluded[u.ID.String()]; !skip {
			filtered = append(filtered, u)
		}
	}
	return filtered, nil
}

// ResolveStepTargetUsers resolves the full user list for a policy step by
// iterating over all targets, calling ResolveUsers for each, and deduplicating.
func (s *EscalationPolicyService) ResolveStepTargetUsers(ctx context.Context, targets []models.EscalationPolicyStepTarget) ([]models.User, error) {
	seen := make(map[uuid.UUID]struct{})
	var result []models.User

	for _, t := range targets {
		users, err := s.ResolveUsers(ctx, t.DepartmentID, t.RoleID, []string(t.ExcludedUserIDs))
		if err != nil {
			return nil, err
		}
		for _, u := range users {
			if _, ok := seen[u.ID]; !ok {
				seen[u.ID] = struct{}{}
				result = append(result, u)
			}
		}
	}
	return result, nil
}

// ResolveGroupTargetUsers resolves users for an EscalationGroup's Targets.
func (s *EscalationPolicyService) ResolveGroupTargetUsers(ctx context.Context, targets []models.EscalationGroupTarget) ([]models.User, error) {
	seen := make(map[uuid.UUID]struct{})
	var result []models.User

	for _, t := range targets {
		users, err := s.ResolveUsers(ctx, t.DepartmentID, t.RoleID, []string(t.ExcludedUserIDs))
		if err != nil {
			return nil, err
		}
		for _, u := range users {
			if _, ok := seen[u.ID]; !ok {
				seen[u.ID] = struct{}{}
				result = append(result, u)
			}
		}
	}
	return result, nil
}

// ─── helpers ─────────────────────────────────────────────────────────────────

// buildSteps converts the API request steps into model structs.
func buildSteps(reqs []models.EscalationPolicyStepRequest) ([]models.EscalationPolicyStep, error) {
	steps := make([]models.EscalationPolicyStep, 0, len(reqs))
	for _, sr := range reqs {
		step := models.EscalationPolicyStep{
			StepOrder:  sr.StepOrder,
			DelayHours: sr.DelayHours,
			Channel:    sr.Channel,
		}
		for _, tr := range sr.Targets {
			target := models.EscalationPolicyStepTarget{
				ExcludedUserIDs: tr.ExcludedUserIDs,
			}
			if target.ExcludedUserIDs == nil {
				target.ExcludedUserIDs = []string{}
			}
			if tr.DepartmentID != nil {
				deptID, err := uuid.Parse(*tr.DepartmentID)
				if err != nil {
					return nil, fmt.Errorf("invalid department_id: %w", err)
				}
				target.DepartmentID = &deptID
			}
			if tr.RoleID != nil {
				roleID, err := uuid.Parse(*tr.RoleID)
				if err != nil {
					return nil, fmt.Errorf("invalid role_id: %w", err)
				}
				target.RoleID = &roleID
			}
			step.Targets = append(step.Targets, target)
		}
		steps = append(steps, step)
	}
	return steps, nil
}
