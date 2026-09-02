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

// MOMRAStatusMappingHandler exposes admin CRUD for the WorkflowState -> MOMRA
// CaseStatusID mapping (Story A) plus the manual-retry endpoint for failed syncs
// (Story C), per docs/MOMRA_Outbound_Integration_Spec_v1.0.md §3.
type MOMRAStatusMappingHandler struct {
	repo            repository.MOMRAStatusMappingRepository
	syncService     services.MOMRAStatusSyncService
	integrationRepo repository.IntegrationRepository
	validator       *validator.Validate
}

func NewMOMRAStatusMappingHandler(repo repository.MOMRAStatusMappingRepository, syncService services.MOMRAStatusSyncService, integrationRepo repository.IntegrationRepository) *MOMRAStatusMappingHandler {
	return &MOMRAStatusMappingHandler{
		repo:            repo,
		syncService:     syncService,
		integrationRepo: integrationRepo,
		validator:       validator.New(),
	}
}

// ListStatusSyncLogs returns MOMRA status-sync execution attempts, so the admin UI
// doesn't need to know the internal placeholder script ID they're anchored to.
func (h *MOMRAStatusMappingHandler) ListStatusSyncLogs(c *fiber.Ctx) error {
	limit := c.QueryInt("limit", 50)
	offset := c.QueryInt("offset", 0)
	logs, total, err := h.integrationRepo.ListExecutionLogsByScript(c.UserContext(), h.syncService.LogScriptID(), limit, offset)
	if err != nil {
		return utils.ErrorResponse(c, fiber.StatusInternalServerError, err.Error())
	}
	return utils.SuccessResponse(c, fiber.StatusOK, "Status sync logs retrieved", fiber.Map{
		"logs":   logs,
		"total":  total,
		"limit":  limit,
		"offset": offset,
	})
}

func (h *MOMRAStatusMappingHandler) Create(c *fiber.Ctx) error {
	var req models.MOMRAStatusMappingRequest
	if err := c.BodyParser(&req); err != nil {
		return utils.ErrorResponse(c, fiber.StatusBadRequest, "invalid request body")
	}
	if err := h.validator.Struct(&req); err != nil {
		return utils.ErrorResponse(c, fiber.StatusBadRequest, err.Error())
	}

	isActive := true
	if req.IsActive != nil {
		isActive = *req.IsActive
	}
	mapping := &models.MOMRAStatusMapping{
		WorkflowID:      req.WorkflowID,
		StateID:         req.StateID,
		CaseStatusID:    req.CaseStatusID,
		IsClosureStatus: req.IsClosureStatus,
		IsActive:        isActive,
	}
	if err := h.repo.Create(c.UserContext(), mapping); err != nil {
		return utils.ErrorResponse(c, fiber.StatusInternalServerError, err.Error())
	}
	return utils.SuccessResponse(c, fiber.StatusCreated, "Status mapping created", mapping)
}

func (h *MOMRAStatusMappingHandler) Update(c *fiber.Ctx) error {
	id, err := uuid.Parse(c.Params("id"))
	if err != nil {
		return utils.ErrorResponse(c, fiber.StatusBadRequest, "invalid id")
	}
	existing, err := h.repo.FindByID(c.UserContext(), id)
	if err != nil {
		return utils.ErrorResponse(c, fiber.StatusNotFound, "status mapping not found")
	}

	var req models.MOMRAStatusMappingRequest
	if err := c.BodyParser(&req); err != nil {
		return utils.ErrorResponse(c, fiber.StatusBadRequest, "invalid request body")
	}
	if err := h.validator.Struct(&req); err != nil {
		return utils.ErrorResponse(c, fiber.StatusBadRequest, err.Error())
	}

	existing.WorkflowID = req.WorkflowID
	existing.StateID = req.StateID
	existing.CaseStatusID = req.CaseStatusID
	existing.IsClosureStatus = req.IsClosureStatus
	if req.IsActive != nil {
		existing.IsActive = *req.IsActive
	}

	if err := h.repo.Update(c.UserContext(), existing); err != nil {
		return utils.ErrorResponse(c, fiber.StatusInternalServerError, err.Error())
	}
	return utils.SuccessResponse(c, fiber.StatusOK, "Status mapping updated", existing)
}

func (h *MOMRAStatusMappingHandler) Delete(c *fiber.Ctx) error {
	id, err := uuid.Parse(c.Params("id"))
	if err != nil {
		return utils.ErrorResponse(c, fiber.StatusBadRequest, "invalid id")
	}
	if err := h.repo.Delete(c.UserContext(), id); err != nil {
		return utils.ErrorResponse(c, fiber.StatusInternalServerError, err.Error())
	}
	return utils.SuccessResponse(c, fiber.StatusOK, "Status mapping deleted", nil)
}

func (h *MOMRAStatusMappingHandler) ListByWorkflow(c *fiber.Ctx) error {
	workflowID, err := uuid.Parse(c.Query("workflow_id"))
	if err != nil {
		return utils.ErrorResponse(c, fiber.StatusBadRequest, "workflow_id query parameter is required and must be a valid UUID")
	}
	list, err := h.repo.ListByWorkflow(c.UserContext(), workflowID)
	if err != nil {
		return utils.ErrorResponse(c, fiber.StatusInternalServerError, err.Error())
	}
	return utils.SuccessResponse(c, fiber.StatusOK, "Status mappings retrieved", list)
}

// RetryFailedSync re-attempts a failed MOMRA status sync from its logged request
// payload — the manual-retry acceptance criterion for Story C.
func (h *MOMRAStatusMappingHandler) RetryFailedSync(c *fiber.Ctx) error {
	logID, err := uuid.Parse(c.Params("logId"))
	if err != nil {
		return utils.ErrorResponse(c, fiber.StatusBadRequest, "invalid log id")
	}
	if err := h.syncService.RetryFailedSync(c.UserContext(), logID); err != nil {
		return utils.ErrorResponse(c, fiber.StatusBadGateway, "retry failed: "+err.Error())
	}
	return utils.SuccessResponse(c, fiber.StatusOK, "Retry attempted — check the log for the new outcome", nil)
}
