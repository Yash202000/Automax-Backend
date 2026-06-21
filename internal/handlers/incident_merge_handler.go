package handlers

import (
	"github.com/automax/backend/internal/models"
	"github.com/automax/backend/internal/repository"
	"github.com/automax/backend/internal/services"
	"github.com/automax/backend/pkg/constants"
	"github.com/automax/backend/pkg/i18n"
	"github.com/automax/backend/pkg/utils"
	"github.com/go-playground/validator/v10"
	"github.com/gofiber/fiber/v2"
	"github.com/google/uuid"
)

type IncidentMergeHandler struct {
	service   services.IncidentMergeService
	userRepo  repository.UserRepository
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
	userID, ok := c.Locals(constants.ContextKeys.UserID).(uuid.UUID)
	if !ok {
		return []uuid.UUID{}
	}

	// Get user roles from repository
	roles, err := h.userRepo.GetUserRoles(c.UserContext(), userID)
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
		return utils.ErrorResponse(c, fiber.StatusBadRequest, i18n.T(c.UserContext(), "invalid_request_body"))
	}

	if err := h.validator.Struct(&req); err != nil {
		return utils.FormatValidationError(c, err)
	}

	response, err := h.service.ValidateMerge(c.UserContext(), req.IncidentIDs)
	if err != nil {
		return utils.ErrorResponse(c, fiber.StatusInternalServerError, err.Error())
	}

	// If validation found errors, return 400 with proper message
	if !response.CanMerge && len(response.Errors) > 0 {
		return utils.ErrorResponse(c, fiber.StatusBadRequest, response.Errors[0])
	}

	return utils.SuccessResponse(c, fiber.StatusOK, i18n.T(c.UserContext(), "merge_validation_completed"), response)
}

// MergeIncidents merges multiple incidents into one master incident
func (h *IncidentMergeHandler) MergeIncidents(c *fiber.Ctx) error {
	var req models.IncidentMergeRequest
	if err := c.BodyParser(&req); err != nil {
		return utils.ErrorResponse(c, fiber.StatusBadRequest, i18n.T(c.UserContext(), "invalid_request_body"))
	}

	if err := h.validator.Struct(&req); err != nil {
		return utils.FormatValidationError(c, err)
	}

	userID := c.Locals(constants.ContextKeys.UserID).(uuid.UUID)
	roleIDs := h.getUserRoleIDs(c)

	response, err := h.service.MergeIncidents(c.UserContext(), &req, userID, roleIDs)
	if err != nil {
		return utils.ErrorResponse(c, fiber.StatusBadRequest, err.Error())
	}

	return utils.SuccessResponse(c, fiber.StatusOK, i18n.T(c.UserContext(), "incidents_merged"), response)
}

// UnmergeIncident detaches an incident from its master
func (h *IncidentMergeHandler) UnmergeIncident(c *fiber.Ctx) error {
	var req models.IncidentUnmergeRequest
	if err := c.BodyParser(&req); err != nil {
		return utils.ErrorResponse(c, fiber.StatusBadRequest, i18n.T(c.UserContext(), "invalid_request_body"))
	}

	if err := h.validator.Struct(&req); err != nil {
		return utils.FormatValidationError(c, err)
	}

	userID := c.Locals(constants.ContextKeys.UserID).(uuid.UUID)
	roleIDs := h.getUserRoleIDs(c)

	response, err := h.service.UnmergeIncident(c.UserContext(), &req, userID, roleIDs)
	if err != nil {
		return utils.ErrorResponse(c, fiber.StatusBadRequest, err.Error())
	}

	return utils.SuccessResponse(c, fiber.StatusOK, i18n.T(c.UserContext(), "incident_unmerged"), response)
}

// BulkUnmergeIncidents detaches multiple incidents from their masters
func (h *IncidentMergeHandler) BulkUnmergeIncidents(c *fiber.Ctx) error {
	var req models.IncidentBulkUnmergeRequest
	if err := c.BodyParser(&req); err != nil {
		return utils.ErrorResponse(c, fiber.StatusBadRequest, i18n.T(c.UserContext(), "invalid_request_body"))
	}

	if err := h.validator.Struct(&req); err != nil {
		return utils.FormatValidationError(c, err)
	}

	userID := c.Locals(constants.ContextKeys.UserID).(uuid.UUID)
	roleIDs := h.getUserRoleIDs(c)

	response, err := h.service.BulkUnmergeIncidents(c.UserContext(), &req, userID, roleIDs)
	if err != nil {
		return utils.ErrorResponse(c, fiber.StatusBadRequest, err.Error())
	}

	return utils.SuccessResponse(c, fiber.StatusOK, i18n.T(c.UserContext(), "incidents_bulk_unmerged"), response)
}

// GetMergedIncidents returns all incidents merged into a master incident
func (h *IncidentMergeHandler) GetMergedIncidents(c *fiber.Ctx) error {
	masterIncidentID := c.Params("id")
	if masterIncidentID == "" {
		return utils.ErrorResponse(c, fiber.StatusBadRequest, i18n.T(c.UserContext(), "master_incident_id_required"))
	}

	// Validate UUID
	if _, err := uuid.Parse(masterIncidentID); err != nil {
		return utils.ErrorResponse(c, fiber.StatusBadRequest, i18n.T(c.UserContext(), "invalid_master_incident_id"))
	}

	incidents, err := h.service.GetMergedIncidents(c.UserContext(), masterIncidentID)
	if err != nil {
		return utils.ErrorResponse(c, fiber.StatusInternalServerError, err.Error())
	}

	return utils.SuccessResponse(c, fiber.StatusOK, i18n.T(c.UserContext(), "merged_incidents_retrieved"), incidents)
}

// CanMerge checks if the current user has permission to merge incidents for a specific workflow
func (h *IncidentMergeHandler) CanMerge(c *fiber.Ctx) error {
	workflowIDStr := c.Query("workflow_id")
	if workflowIDStr == "" {
		return utils.ErrorResponse(c, fiber.StatusBadRequest, i18n.T(c.UserContext(), "workflow_id_required"))
	}

	workflowID, err := uuid.Parse(workflowIDStr)
	if err != nil {
		return utils.ErrorResponse(c, fiber.StatusBadRequest, i18n.T(c.UserContext(), "invalid_workflow_id_lower"))
	}

	roleIDs := h.getUserRoleIDs(c)
	canMerge, err := h.service.CanUserMerge(c.UserContext(), workflowID, roleIDs)
	if err != nil {
		return utils.ErrorResponse(c, fiber.StatusInternalServerError, err.Error())
	}

	return utils.SuccessResponse(c, fiber.StatusOK, i18n.T(c.UserContext(), "permission_check_completed"), fiber.Map{
		"can_merge": canMerge,
	})
}
