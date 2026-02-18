package handlers

import (
	"github.com/automax/backend/internal/models"
	"github.com/automax/backend/internal/repository"
	"github.com/automax/backend/internal/services"
	"github.com/automax/backend/pkg/utils"
	"github.com/go-playground/validator/v10"
	"github.com/gofiber/fiber/v2"
	"github.com/google/uuid"
)

type IncidentMergeHandler struct {
	service  services.IncidentMergeService
	userRepo repository.UserRepository
	validator *validator.Validate
}

func NewIncidentMergeHandler(service services.IncidentMergeService, userRepo repository.UserRepository) *IncidentMergeHandler {
	return &IncidentMergeHandler{
		service:   service,
		userRepo:  userRepo,
		validator: validator.New(),
	}
}

// getUserRoleIDs extracts role IDs from the Fiber context
func (h *IncidentMergeHandler) getUserRoleIDs(c *fiber.Ctx) []uuid.UUID {
	userID, ok := c.Locals("user_id").(uuid.UUID)
	if !ok {
		return []uuid.UUID{}
	}

	// Get user roles from repository
	roles, err := h.userRepo.GetUserRoles(c.Context(), userID)
	if err != nil {
		return []uuid.UUID{}
	}

	roleIDs := make([]uuid.UUID, len(roles))
	for i, role := range roles {
		roleIDs[i] = role.ID
	}
	return roleIDs
}

// ValidateMerge validates if selected incidents can be merged
func (h *IncidentMergeHandler) ValidateMerge(c *fiber.Ctx) error {
	var req models.IncidentMergeValidationRequest
	if err := c.BodyParser(&req); err != nil {
		return utils.ErrorResponse(c, fiber.StatusBadRequest, "Invalid request body")
	}

	if err := h.validator.Struct(&req); err != nil {
		return utils.FormatValidationError(c, err)
	}

	response, err := h.service.ValidateMerge(c.Context(), req.IncidentIDs)
	if err != nil {
		return utils.ErrorResponse(c, fiber.StatusInternalServerError, err.Error())
	}

	return utils.SuccessResponse(c, fiber.StatusOK, "Merge validation completed", response)
}

// MergeIncidents merges multiple incidents into one master incident
func (h *IncidentMergeHandler) MergeIncidents(c *fiber.Ctx) error {
	var req models.IncidentMergeRequest
	if err := c.BodyParser(&req); err != nil {
		return utils.ErrorResponse(c, fiber.StatusBadRequest, "Invalid request body")
	}

	if err := h.validator.Struct(&req); err != nil {
		return utils.FormatValidationError(c, err)
	}

	userID := c.Locals("user_id").(uuid.UUID)
	roleIDs := h.getUserRoleIDs(c)

	response, err := h.service.MergeIncidents(c.Context(), &req, userID, roleIDs)
	if err != nil {
		return utils.ErrorResponse(c, fiber.StatusBadRequest, err.Error())
	}

	return utils.SuccessResponse(c, fiber.StatusOK, "Incidents merged successfully", response)
}

// UnmergeIncident detaches an incident from its master
func (h *IncidentMergeHandler) UnmergeIncident(c *fiber.Ctx) error {
	var req models.IncidentUnmergeRequest
	if err := c.BodyParser(&req); err != nil {
		return utils.ErrorResponse(c, fiber.StatusBadRequest, "Invalid request body")
	}

	if err := h.validator.Struct(&req); err != nil {
		return utils.FormatValidationError(c, err)
	}

	userID := c.Locals("user_id").(uuid.UUID)
	roleIDs := h.getUserRoleIDs(c)

	response, err := h.service.UnmergeIncident(c.Context(), &req, userID, roleIDs)
	if err != nil {
		return utils.ErrorResponse(c, fiber.StatusBadRequest, err.Error())
	}

	return utils.SuccessResponse(c, fiber.StatusOK, "Incident unmerged successfully", response)
}

// BulkUnmergeIncidents detaches multiple incidents from their masters
func (h *IncidentMergeHandler) BulkUnmergeIncidents(c *fiber.Ctx) error {
	var req models.IncidentBulkUnmergeRequest
	if err := c.BodyParser(&req); err != nil {
		return utils.ErrorResponse(c, fiber.StatusBadRequest, "Invalid request body")
	}

	if err := h.validator.Struct(&req); err != nil {
		return utils.FormatValidationError(c, err)
	}

	userID := c.Locals("user_id").(uuid.UUID)
	roleIDs := h.getUserRoleIDs(c)

	response, err := h.service.BulkUnmergeIncidents(c.Context(), &req, userID, roleIDs)
	if err != nil {
		return utils.ErrorResponse(c, fiber.StatusBadRequest, err.Error())
	}

	return utils.SuccessResponse(c, fiber.StatusOK, "Incidents bulk unmerged successfully", response)
}

// GetMergedIncidents returns all incidents merged into a master incident
func (h *IncidentMergeHandler) GetMergedIncidents(c *fiber.Ctx) error {
	masterIncidentID := c.Params("id")
	if masterIncidentID == "" {
		return utils.ErrorResponse(c, fiber.StatusBadRequest, "Master incident ID is required")
	}

	// Validate UUID
	if _, err := uuid.Parse(masterIncidentID); err != nil {
		return utils.ErrorResponse(c, fiber.StatusBadRequest, "Invalid master incident ID")
	}

	incidents, err := h.service.GetMergedIncidents(c.Context(), masterIncidentID)
	if err != nil {
		return utils.ErrorResponse(c, fiber.StatusInternalServerError, err.Error())
	}

	return utils.SuccessResponse(c, fiber.StatusOK, "Merged incidents retrieved", incidents)
}

// CanMerge checks if the current user has permission to merge incidents
func (h *IncidentMergeHandler) CanMerge(c *fiber.Ctx) error {
	roleIDs := h.getUserRoleIDs(c)
	canMerge := h.service.CanUserMerge(c.Context(), roleIDs)

	return utils.SuccessResponse(c, fiber.StatusOK, "Permission check completed", fiber.Map{
		"can_merge": canMerge,
	})
}
