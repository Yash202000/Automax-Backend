package handlers

import (
	"fmt"

	"github.com/automax/backend/internal/middleware"
	"github.com/automax/backend/internal/models"
	"github.com/automax/backend/internal/repository"
	"github.com/automax/backend/internal/services"
	"github.com/automax/backend/pkg/i18n"
	"github.com/automax/backend/pkg/utils"
	"github.com/automax/backend/pkg/validation"
	"github.com/gofiber/fiber/v2"
	"github.com/google/uuid"
)

const escalationPolicyLogModule = "escalation-policies"

type EscalationPolicyHandler struct {
	service      *services.EscalationPolicyService
	userRepo     repository.UserRepository
	deptRepo     repository.DepartmentRepository
	actionLogSvc services.ActionLogService
}

func NewEscalationPolicyHandler(service *services.EscalationPolicyService, userRepo repository.UserRepository, deptRepo repository.DepartmentRepository, actionLogSvc services.ActionLogService) *EscalationPolicyHandler {
	return &EscalationPolicyHandler{service: service, userRepo: userRepo, deptRepo: deptRepo, actionLogSvc: actionLogSvc}
}

func (h *EscalationPolicyHandler) audit(c *fiber.Ctx, action string, id uuid.UUID, description string, oldValue, newValue any) {
	if h.actionLogSvc == nil {
		return
	}
	middleware.LogAction(c, h.actionLogSvc, &services.LogActionParams{
		Action:      action,
		Module:      escalationPolicyLogModule,
		ResourceID:  id.String(),
		Description: description,
		OldValue:    oldValue,
		NewValue:    newValue,
	})
}

// List — GET /api/v1/admin/escalation-policies
func (h *EscalationPolicyHandler) List(c *fiber.Ctx) error {
	policies, err := h.service.List(c.UserContext())
	if err != nil {
		return utils.InternalErrorResponse(c, err, i18n.T(c.UserContext(), "internal_server_error"))
	}
	return utils.SuccessResponse(c, fiber.StatusOK, i18n.T(c.UserContext(), "escalation_policies_retrieved"), policies)
}

// GetByID — GET /api/v1/admin/escalation-policies/:id
func (h *EscalationPolicyHandler) GetByID(c *fiber.Ctx) error {
	id, err := uuid.Parse(c.Params("id"))
	if err != nil {
		return utils.ErrorResponse(c, fiber.StatusBadRequest, i18n.T(c.UserContext(), "invalid_id"))
	}
	policy, err := h.service.GetByID(c.UserContext(), id)
	if err != nil {
		return utils.ErrorResponse(c, fiber.StatusNotFound, err.Error())
	}
	return utils.SuccessResponse(c, fiber.StatusOK, i18n.T(c.UserContext(), "escalation_policy_retrieved"), policy)
}

// Create — POST /api/v1/admin/escalation-policies
func (h *EscalationPolicyHandler) Create(c *fiber.Ctx) error {
	var req models.CreateEscalationPolicyRequest
	if err := c.BodyParser(&req); err != nil {
		return utils.ErrorResponse(c, fiber.StatusBadRequest, i18n.T(c.UserContext(), "invalid_request_body"))
	}
	if errs := validation.ValidateStruct(c.UserContext(), &req); len(errs) != 0 {
		return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{"success": false, "errors": errs})
	}

	policy, err := h.service.Create(c.UserContext(), &req)
	if err != nil {
		return utils.InternalErrorResponse(c, err, i18n.T(c.UserContext(), "internal_server_error"))
	}
	h.audit(c, "create", policy.ID, fmt.Sprintf("Created escalation policy '%s'", policy.Name), nil, policy)
	return utils.SuccessResponse(c, fiber.StatusCreated, i18n.T(c.UserContext(), "escalation_policy_created"), policy)
}

// Update — PUT /api/v1/admin/escalation-policies/:id
func (h *EscalationPolicyHandler) Update(c *fiber.Ctx) error {
	id, err := uuid.Parse(c.Params("id"))
	if err != nil {
		return utils.ErrorResponse(c, fiber.StatusBadRequest, i18n.T(c.UserContext(), "invalid_id"))
	}

	var req models.UpdateEscalationPolicyRequest
	if err := c.BodyParser(&req); err != nil {
		return utils.ErrorResponse(c, fiber.StatusBadRequest, i18n.T(c.UserContext(), "invalid_request_body"))
	}
	if errs := validation.ValidateStruct(c.UserContext(), &req); len(errs) != 0 {
		return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{"success": false, "errors": errs})
	}

	oldPolicy, _ := h.service.GetByID(c.UserContext(), id)
	policy, err := h.service.Update(c.UserContext(), id, &req)
	if err != nil {
		return utils.InternalErrorResponse(c, err, i18n.T(c.UserContext(), "internal_server_error"))
	}
	var oldValue any
	if oldPolicy != nil {
		oldValue = oldPolicy
	}
	h.audit(c, "update", policy.ID, fmt.Sprintf("Updated escalation policy '%s'", policy.Name), oldValue, policy)
	return utils.SuccessResponse(c, fiber.StatusOK, i18n.T(c.UserContext(), "escalation_policy_updated"), policy)
}

// Delete — DELETE /api/v1/admin/escalation-policies/:id
func (h *EscalationPolicyHandler) Delete(c *fiber.Ctx) error {
	id, err := uuid.Parse(c.Params("id"))
	if err != nil {
		return utils.ErrorResponse(c, fiber.StatusBadRequest, i18n.T(c.UserContext(), "invalid_id"))
	}
	oldPolicy, _ := h.service.GetByID(c.UserContext(), id)
	if err := h.service.Delete(c.UserContext(), id); err != nil {
		return utils.ErrorResponse(c, fiber.StatusNotFound, err.Error())
	}
	var oldValue any
	name := id.String()
	if oldPolicy != nil {
		name = oldPolicy.Name
		oldValue = oldPolicy
	}
	h.audit(c, "delete", id, fmt.Sprintf("Deleted escalation policy '%s'", name), oldValue, nil)
	return utils.SuccessResponse(c, fiber.StatusOK, i18n.T(c.UserContext(), "escalation_policy_deleted"), nil)
}

// UpdateTargetExcludedUsers — PUT /api/v1/admin/escalation-policies/:id/targets/:target_id/excluded-users
// Body: { "excluded_user_ids": ["uuid1", "uuid2"] }
// Replaces the excluded_user_ids for a single step target in-place without
// requiring the frontend to re-send the entire policy/steps structure.
func (h *EscalationPolicyHandler) UpdateTargetExcludedUsers(c *fiber.Ctx) error {
	policyID, err := uuid.Parse(c.Params("id"))
	if err != nil {
		return utils.ErrorResponse(c, fiber.StatusBadRequest, i18n.T(c.UserContext(), "invalid_policy_id"))
	}
	targetID, err := uuid.Parse(c.Params("target_id"))
	if err != nil {
		return utils.ErrorResponse(c, fiber.StatusBadRequest, i18n.T(c.UserContext(), "invalid_target_id"))
	}

	var req struct {
		ExcludedUserIDs []string `json:"excluded_user_ids" validate:"omitempty,dive,uuid"`
	}
	if err := c.BodyParser(&req); err != nil {
		return utils.ErrorResponse(c, fiber.StatusBadRequest, i18n.T(c.UserContext(), "invalid_request_body"))
	}
	if req.ExcludedUserIDs == nil {
		req.ExcludedUserIDs = []string{}
	}

	oldPolicy, _ := h.service.GetByID(c.UserContext(), policyID)
	if err := h.service.UpdateTargetExcludedUsers(c.UserContext(), policyID, targetID, req.ExcludedUserIDs); err != nil {
		return utils.ErrorResponse(c, fiber.StatusBadRequest, err.Error())
	}
	newPolicy, _ := h.service.GetByID(c.UserContext(), policyID)

	var oldValue, newValue any
	name := policyID.String()
	if oldPolicy != nil {
		name = oldPolicy.Name
		oldValue = oldPolicy
	}
	if newPolicy != nil {
		newValue = newPolicy
	}
	h.audit(c, "update", policyID, fmt.Sprintf("Updated escalation policy target exclusions for '%s'", name), oldValue, newValue)
	return utils.SuccessResponse(c, fiber.StatusOK, i18n.T(c.UserContext(), "excluded_users_updated"), nil)
}

// ResolveTargetUsers — POST /api/v1/admin/escalation-policies/resolve-users
// Body: { "department_id": "...", "role_id": "...", "excluded_user_ids": [...] }
// Returns the list of users that would be notified given this target config.
func (h *EscalationPolicyHandler) ResolveTargetUsers(c *fiber.Ctx) error {
	var req struct {
		DepartmentID    *string  `json:"department_id"`
		RoleID          *string  `json:"role_id"`
		ExcludedUserIDs []string `json:"excluded_user_ids"`
	}
	if err := c.BodyParser(&req); err != nil {
		return utils.ErrorResponse(c, fiber.StatusBadRequest, i18n.T(c.UserContext(), "invalid_request_body"))
	}

	var deptID, roleID *uuid.UUID
	if req.DepartmentID != nil && *req.DepartmentID != "" {
		id, err := uuid.Parse(*req.DepartmentID)
		if err != nil {
			return utils.ErrorResponse(c, fiber.StatusBadRequest, i18n.T(c.UserContext(), "invalid_department_id"))
		}
		deptID = &id
	}
	if req.RoleID != nil && *req.RoleID != "" {
		id, err := uuid.Parse(*req.RoleID)
		if err != nil {
			return utils.ErrorResponse(c, fiber.StatusBadRequest, i18n.T(c.UserContext(), "invalid_role_id"))
		}
		roleID = &id
	}

	users, err := h.service.ResolveUsers(c.UserContext(), deptID, roleID, req.ExcludedUserIDs)
	if err != nil {
		return utils.InternalErrorResponse(c, err, i18n.T(c.UserContext(), "internal_server_error"))
	}

	responses := make([]models.UserResponse, len(users))
	for i, u := range users {
		responses[i] = models.ToUserResponse(&u)
	}
	return utils.SuccessResponse(c, fiber.StatusOK, i18n.T(c.UserContext(), "users_resolved"), responses)
}
