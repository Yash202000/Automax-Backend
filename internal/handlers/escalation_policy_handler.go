package handlers

import (
	"github.com/automax/backend/internal/models"
	"github.com/automax/backend/internal/repository"
	"github.com/automax/backend/internal/services"
	"github.com/automax/backend/pkg/utils"
	"github.com/automax/backend/pkg/validation"
	"github.com/gofiber/fiber/v2"
	"github.com/google/uuid"
)

type EscalationPolicyHandler struct {
	service      *services.EscalationPolicyService
	userRepo     repository.UserRepository
	deptRepo     repository.DepartmentRepository
}

func NewEscalationPolicyHandler(service *services.EscalationPolicyService, userRepo repository.UserRepository, deptRepo repository.DepartmentRepository) *EscalationPolicyHandler {
	return &EscalationPolicyHandler{service: service, userRepo: userRepo, deptRepo: deptRepo}
}

// List — GET /api/v1/admin/escalation-policies
func (h *EscalationPolicyHandler) List(c *fiber.Ctx) error {
	policies, err := h.service.List(c.UserContext())
	if err != nil {
		return utils.ErrorResponse(c, fiber.StatusInternalServerError, err.Error())
	}
	return utils.SuccessResponse(c, fiber.StatusOK, "Escalation policies retrieved", policies)
}

// GetByID — GET /api/v1/admin/escalation-policies/:id
func (h *EscalationPolicyHandler) GetByID(c *fiber.Ctx) error {
	id, err := uuid.Parse(c.Params("id"))
	if err != nil {
		return utils.ErrorResponse(c, fiber.StatusBadRequest, "Invalid ID")
	}
	policy, err := h.service.GetByID(c.UserContext(), id)
	if err != nil {
		return utils.ErrorResponse(c, fiber.StatusNotFound, err.Error())
	}
	return utils.SuccessResponse(c, fiber.StatusOK, "Escalation policy retrieved", policy)
}

// Create — POST /api/v1/admin/escalation-policies
func (h *EscalationPolicyHandler) Create(c *fiber.Ctx) error {
	var req models.CreateEscalationPolicyRequest
	if err := c.BodyParser(&req); err != nil {
		return utils.ErrorResponse(c, fiber.StatusBadRequest, "Invalid request body")
	}
	if errs := validation.ValidateStruct(c.UserContext(), &req); len(errs) != 0 {
		return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{"success": false, "errors": errs})
	}

	policy, err := h.service.Create(c.UserContext(), &req)
	if err != nil {
		return utils.ErrorResponse(c, fiber.StatusInternalServerError, err.Error())
	}
	return utils.SuccessResponse(c, fiber.StatusCreated, "Escalation policy created", policy)
}

// Update — PUT /api/v1/admin/escalation-policies/:id
func (h *EscalationPolicyHandler) Update(c *fiber.Ctx) error {
	id, err := uuid.Parse(c.Params("id"))
	if err != nil {
		return utils.ErrorResponse(c, fiber.StatusBadRequest, "Invalid ID")
	}

	var req models.UpdateEscalationPolicyRequest
	if err := c.BodyParser(&req); err != nil {
		return utils.ErrorResponse(c, fiber.StatusBadRequest, "Invalid request body")
	}
	if errs := validation.ValidateStruct(c.UserContext(), &req); len(errs) != 0 {
		return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{"success": false, "errors": errs})
	}

	policy, err := h.service.Update(c.UserContext(), id, &req)
	if err != nil {
		return utils.ErrorResponse(c, fiber.StatusInternalServerError, err.Error())
	}
	return utils.SuccessResponse(c, fiber.StatusOK, "Escalation policy updated", policy)
}

// Delete — DELETE /api/v1/admin/escalation-policies/:id
func (h *EscalationPolicyHandler) Delete(c *fiber.Ctx) error {
	id, err := uuid.Parse(c.Params("id"))
	if err != nil {
		return utils.ErrorResponse(c, fiber.StatusBadRequest, "Invalid ID")
	}
	if err := h.service.Delete(c.UserContext(), id); err != nil {
		return utils.ErrorResponse(c, fiber.StatusNotFound, err.Error())
	}
	return utils.SuccessResponse(c, fiber.StatusOK, "Escalation policy deleted", nil)
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
		return utils.ErrorResponse(c, fiber.StatusBadRequest, "Invalid request body")
	}

	var deptID, roleID *uuid.UUID
	if req.DepartmentID != nil && *req.DepartmentID != "" {
		id, err := uuid.Parse(*req.DepartmentID)
		if err != nil {
			return utils.ErrorResponse(c, fiber.StatusBadRequest, "Invalid department_id")
		}
		deptID = &id
	}
	if req.RoleID != nil && *req.RoleID != "" {
		id, err := uuid.Parse(*req.RoleID)
		if err != nil {
			return utils.ErrorResponse(c, fiber.StatusBadRequest, "Invalid role_id")
		}
		roleID = &id
	}

	users, err := h.service.ResolveUsers(c.UserContext(), deptID, roleID, req.ExcludedUserIDs)
	if err != nil {
		return utils.ErrorResponse(c, fiber.StatusInternalServerError, err.Error())
	}

	responses := make([]models.UserResponse, len(users))
	for i, u := range users {
		responses[i] = models.ToUserResponse(&u)
	}
	return utils.SuccessResponse(c, fiber.StatusOK, "Users resolved", responses)
}
