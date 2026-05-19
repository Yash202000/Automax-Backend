package handlers

import (
	"strconv"

	"github.com/automax/backend/internal/models"
	"github.com/automax/backend/internal/repository"
	"github.com/automax/backend/internal/services"
	"github.com/automax/backend/pkg/constants"
	"github.com/automax/backend/pkg/utils"
	"github.com/gofiber/fiber/v2"
	"github.com/google/uuid"
)

type IntegrationHandler struct {
	svc      services.IntegrationService
	executor services.IntegrationExecutor
	repo     repository.IntegrationRepository
	// incidentRepo is used for test-run lookups
	incidentRepo repository.IncidentRepository
}

func NewIntegrationHandler(
	svc services.IntegrationService,
	executor services.IntegrationExecutor,
	repo repository.IntegrationRepository,
	incidentRepo repository.IncidentRepository,
) *IntegrationHandler {
	return &IntegrationHandler{svc: svc, executor: executor, repo: repo, incidentRepo: incidentRepo}
}

// ---- Variables ----

func (h *IntegrationHandler) CreateVariable(c *fiber.Ctx) error {
	var req models.IntegrationVariableCreateRequest
	if err := c.BodyParser(&req); err != nil {
		return utils.ErrorResponse(c, fiber.StatusBadRequest, "Invalid request body")
	}
	userID := c.Locals(constants.ContextKeys.UserID).(uuid.UUID)
	resp, err := h.svc.CreateVariable(c.UserContext(), &req, userID)
	if err != nil {
		return utils.ErrorResponse(c, fiber.StatusInternalServerError, "Failed to create variable")
	}
	return utils.SuccessResponse(c, fiber.StatusCreated, "Variable created", resp)
}

func (h *IntegrationHandler) ListVariables(c *fiber.Ctx) error {
	list, err := h.svc.ListVariables(c.UserContext())
	if err != nil {
		return utils.ErrorResponse(c, fiber.StatusInternalServerError, "Failed to list variables")
	}
	return utils.SuccessResponse(c, fiber.StatusOK, "Variables retrieved", list)
}

func (h *IntegrationHandler) DeleteVariable(c *fiber.Ctx) error {
	id, err := uuid.Parse(c.Params("id"))
	if err != nil {
		return utils.ErrorResponse(c, fiber.StatusBadRequest, "Invalid variable ID")
	}
	if err := h.svc.DeleteVariable(c.UserContext(), id); err != nil {
		return utils.ErrorResponse(c, fiber.StatusInternalServerError, "Failed to delete variable")
	}
	return utils.SuccessResponse(c, fiber.StatusOK, "Variable deleted", nil)
}

// ---- Scripts ----

func (h *IntegrationHandler) CreateScript(c *fiber.Ctx) error {
	var req models.IntegrationScriptRequest
	if err := c.BodyParser(&req); err != nil {
		return utils.ErrorResponse(c, fiber.StatusBadRequest, "Invalid request body")
	}
	userID := c.Locals(constants.ContextKeys.UserID).(uuid.UUID)
	resp, err := h.svc.CreateScript(c.UserContext(), &req, userID)
	if err != nil {
		return utils.ErrorResponse(c, fiber.StatusInternalServerError, "Failed to create script")
	}
	return utils.SuccessResponse(c, fiber.StatusCreated, "Script created", resp)
}

func (h *IntegrationHandler) GetScript(c *fiber.Ctx) error {
	id, err := uuid.Parse(c.Params("id"))
	if err != nil {
		return utils.ErrorResponse(c, fiber.StatusBadRequest, "Invalid script ID")
	}
	resp, err := h.svc.GetScript(c.UserContext(), id)
	if err != nil {
		return utils.ErrorResponse(c, fiber.StatusNotFound, "Script not found")
	}
	return utils.SuccessResponse(c, fiber.StatusOK, "Script retrieved", resp)
}

func (h *IntegrationHandler) ListScripts(c *fiber.Ctx) error {
	activeOnly := c.QueryBool("active_only", false)
	list, err := h.svc.ListScripts(c.UserContext(), activeOnly)
	if err != nil {
		return utils.ErrorResponse(c, fiber.StatusInternalServerError, "Failed to list scripts")
	}
	return utils.SuccessResponse(c, fiber.StatusOK, "Scripts retrieved", list)
}

func (h *IntegrationHandler) UpdateScript(c *fiber.Ctx) error {
	id, err := uuid.Parse(c.Params("id"))
	if err != nil {
		return utils.ErrorResponse(c, fiber.StatusBadRequest, "Invalid script ID")
	}
	var req models.IntegrationScriptRequest
	if err := c.BodyParser(&req); err != nil {
		return utils.ErrorResponse(c, fiber.StatusBadRequest, "Invalid request body")
	}
	resp, err := h.svc.UpdateScript(c.UserContext(), id, &req)
	if err != nil {
		return utils.ErrorResponse(c, fiber.StatusInternalServerError, "Failed to update script")
	}
	return utils.SuccessResponse(c, fiber.StatusOK, "Script updated", resp)
}

func (h *IntegrationHandler) DeleteScript(c *fiber.Ctx) error {
	id, err := uuid.Parse(c.Params("id"))
	if err != nil {
		return utils.ErrorResponse(c, fiber.StatusBadRequest, "Invalid script ID")
	}
	if err := h.svc.DeleteScript(c.UserContext(), id); err != nil {
		return utils.ErrorResponse(c, fiber.StatusInternalServerError, "Failed to delete script")
	}
	return utils.SuccessResponse(c, fiber.StatusOK, "Script deleted", nil)
}

// TestScript runs a script against a real incident for validation purposes.
func (h *IntegrationHandler) TestScript(c *fiber.Ctx) error {
	scriptID, err := uuid.Parse(c.Params("id"))
	if err != nil {
		return utils.ErrorResponse(c, fiber.StatusBadRequest, "Invalid script ID")
	}
	var req models.IntegrationScriptTestRequest
	if err := c.BodyParser(&req); err != nil {
		return utils.ErrorResponse(c, fiber.StatusBadRequest, "Invalid request body")
	}
	incidentID, err := uuid.Parse(req.IncidentID)
	if err != nil {
		return utils.ErrorResponse(c, fiber.StatusBadRequest, "Invalid incident_id")
	}
	script, err := h.repo.FindScriptByID(c.UserContext(), scriptID)
	if err != nil {
		return utils.ErrorResponse(c, fiber.StatusNotFound, "Script not found")
	}
	incident, err := h.incidentRepo.FindByIDWithRelations(c.UserContext(), incidentID)
	if err != nil {
		return utils.ErrorResponse(c, fiber.StatusNotFound, "Incident not found")
	}
	result := h.executor.RunScriptForTest(c.UserContext(), script, incident, "")
	return utils.SuccessResponse(c, fiber.StatusOK, "Test executed", result)
}

// ---- State triggers ----

func (h *IntegrationHandler) CreateStateTrigger(c *fiber.Ctx) error {
	stateID, err := uuid.Parse(c.Params("stateId"))
	if err != nil {
		return utils.ErrorResponse(c, fiber.StatusBadRequest, "Invalid state ID")
	}
	var req models.WorkflowStateTriggerRequest
	if err := c.BodyParser(&req); err != nil {
		return utils.ErrorResponse(c, fiber.StatusBadRequest, "Invalid request body")
	}
	trigger, err := h.svc.CreateStateTrigger(c.UserContext(), stateID, &req)
	if err != nil {
		return utils.ErrorResponse(c, fiber.StatusInternalServerError, "Failed to create state trigger")
	}
	return utils.SuccessResponse(c, fiber.StatusCreated, "State trigger created", trigger)
}

func (h *IntegrationHandler) ListStateTriggers(c *fiber.Ctx) error {
	stateID, err := uuid.Parse(c.Params("stateId"))
	if err != nil {
		return utils.ErrorResponse(c, fiber.StatusBadRequest, "Invalid state ID")
	}
	list, err := h.svc.ListStateTriggers(c.UserContext(), stateID)
	if err != nil {
		return utils.ErrorResponse(c, fiber.StatusInternalServerError, "Failed to list state triggers")
	}
	return utils.SuccessResponse(c, fiber.StatusOK, "State triggers retrieved", list)
}

func (h *IntegrationHandler) UpdateStateTrigger(c *fiber.Ctx) error {
	triggerID, err := uuid.Parse(c.Params("id"))
	if err != nil {
		return utils.ErrorResponse(c, fiber.StatusBadRequest, "Invalid trigger ID")
	}
	var req models.WorkflowStateTriggerRequest
	if err := c.BodyParser(&req); err != nil {
		return utils.ErrorResponse(c, fiber.StatusBadRequest, "Invalid request body")
	}
	trigger, err := h.svc.UpdateStateTrigger(c.UserContext(), triggerID, &req)
	if err != nil {
		return utils.ErrorResponse(c, fiber.StatusInternalServerError, "Failed to update state trigger")
	}
	return utils.SuccessResponse(c, fiber.StatusOK, "State trigger updated", trigger)
}

func (h *IntegrationHandler) DeleteStateTrigger(c *fiber.Ctx) error {
	triggerID, err := uuid.Parse(c.Params("id"))
	if err != nil {
		return utils.ErrorResponse(c, fiber.StatusBadRequest, "Invalid trigger ID")
	}
	if err := h.svc.DeleteStateTrigger(c.UserContext(), triggerID); err != nil {
		return utils.ErrorResponse(c, fiber.StatusInternalServerError, "Failed to delete state trigger")
	}
	return utils.SuccessResponse(c, fiber.StatusOK, "State trigger deleted", nil)
}

// ---- Transition triggers ----

func (h *IntegrationHandler) CreateTransitionTrigger(c *fiber.Ctx) error {
	transitionID, err := uuid.Parse(c.Params("transitionId"))
	if err != nil {
		return utils.ErrorResponse(c, fiber.StatusBadRequest, "Invalid transition ID")
	}
	var req models.WorkflowTransitionTriggerRequest
	if err := c.BodyParser(&req); err != nil {
		return utils.ErrorResponse(c, fiber.StatusBadRequest, "Invalid request body")
	}
	trigger, err := h.svc.CreateTransitionTrigger(c.UserContext(), transitionID, &req)
	if err != nil {
		return utils.ErrorResponse(c, fiber.StatusInternalServerError, "Failed to create transition trigger")
	}
	return utils.SuccessResponse(c, fiber.StatusCreated, "Transition trigger created", trigger)
}

func (h *IntegrationHandler) ListTransitionTriggers(c *fiber.Ctx) error {
	transitionID, err := uuid.Parse(c.Params("transitionId"))
	if err != nil {
		return utils.ErrorResponse(c, fiber.StatusBadRequest, "Invalid transition ID")
	}
	list, err := h.svc.ListTransitionTriggers(c.UserContext(), transitionID)
	if err != nil {
		return utils.ErrorResponse(c, fiber.StatusInternalServerError, "Failed to list transition triggers")
	}
	return utils.SuccessResponse(c, fiber.StatusOK, "Transition triggers retrieved", list)
}

func (h *IntegrationHandler) UpdateTransitionTrigger(c *fiber.Ctx) error {
	triggerID, err := uuid.Parse(c.Params("id"))
	if err != nil {
		return utils.ErrorResponse(c, fiber.StatusBadRequest, "Invalid trigger ID")
	}
	var req models.WorkflowTransitionTriggerRequest
	if err := c.BodyParser(&req); err != nil {
		return utils.ErrorResponse(c, fiber.StatusBadRequest, "Invalid request body")
	}
	trigger, err := h.svc.UpdateTransitionTrigger(c.UserContext(), triggerID, &req)
	if err != nil {
		return utils.ErrorResponse(c, fiber.StatusInternalServerError, "Failed to update transition trigger")
	}
	return utils.SuccessResponse(c, fiber.StatusOK, "Transition trigger updated", trigger)
}

func (h *IntegrationHandler) DeleteTransitionTrigger(c *fiber.Ctx) error {
	triggerID, err := uuid.Parse(c.Params("id"))
	if err != nil {
		return utils.ErrorResponse(c, fiber.StatusBadRequest, "Invalid trigger ID")
	}
	if err := h.svc.DeleteTransitionTrigger(c.UserContext(), triggerID); err != nil {
		return utils.ErrorResponse(c, fiber.StatusInternalServerError, "Failed to delete transition trigger")
	}
	return utils.SuccessResponse(c, fiber.StatusOK, "Transition trigger deleted", nil)
}

// ---- Execution logs ----

func (h *IntegrationHandler) ListLogsByScript(c *fiber.Ctx) error {
	scriptID, err := uuid.Parse(c.Params("id"))
	if err != nil {
		return utils.ErrorResponse(c, fiber.StatusBadRequest, "Invalid script ID")
	}
	limit, _ := strconv.Atoi(c.Query("limit", "50"))
	offset, _ := strconv.Atoi(c.Query("offset", "0"))
	list, total, err := h.svc.ListLogsByScript(c.UserContext(), scriptID, limit, offset)
	if err != nil {
		return utils.ErrorResponse(c, fiber.StatusInternalServerError, "Failed to list logs")
	}
	return utils.SuccessResponse(c, fiber.StatusOK, "Logs retrieved", fiber.Map{
		"logs":   list,
		"total":  total,
		"limit":  limit,
		"offset": offset,
	})
}

func (h *IntegrationHandler) ListLogsByIncident(c *fiber.Ctx) error {
	incidentID, err := uuid.Parse(c.Params("incidentId"))
	if err != nil {
		return utils.ErrorResponse(c, fiber.StatusBadRequest, "Invalid incident ID")
	}
	limit, _ := strconv.Atoi(c.Query("limit", "50"))
	offset, _ := strconv.Atoi(c.Query("offset", "0"))
	list, total, err := h.svc.ListLogsByIncident(c.UserContext(), incidentID, limit, offset)
	if err != nil {
		return utils.ErrorResponse(c, fiber.StatusInternalServerError, "Failed to list logs")
	}
	return utils.SuccessResponse(c, fiber.StatusOK, "Logs retrieved", fiber.Map{
		"logs":   list,
		"total":  total,
		"limit":  limit,
		"offset": offset,
	})
}

// ---- Incident bridges ----

func (h *IntegrationHandler) ListBridgesByIncident(c *fiber.Ctx) error {
	incidentID, err := uuid.Parse(c.Params("incidentId"))
	if err != nil {
		return utils.ErrorResponse(c, fiber.StatusBadRequest, "Invalid incident ID")
	}
	list, err := h.svc.ListBridgesByIncident(c.UserContext(), incidentID)
	if err != nil {
		return utils.ErrorResponse(c, fiber.StatusInternalServerError, "Failed to list bridges")
	}
	return utils.SuccessResponse(c, fiber.StatusOK, "Bridges retrieved", list)
}

// ---- Webhook callback configs ----

func (h *IntegrationHandler) CreateWebhookConfig(c *fiber.Ctx) error {
	var req models.WebhookCallbackConfigRequest
	if err := c.BodyParser(&req); err != nil {
		return utils.ErrorResponse(c, fiber.StatusBadRequest, "Invalid request body")
	}
	resp, err := h.svc.CreateWebhookConfig(c.UserContext(), &req)
	if err != nil {
		return utils.ErrorResponse(c, fiber.StatusInternalServerError, "Failed to create webhook config")
	}
	return utils.SuccessResponse(c, fiber.StatusCreated, "Webhook config created", resp)
}

func (h *IntegrationHandler) ListWebhookConfigs(c *fiber.Ctx) error {
	list, err := h.svc.ListWebhookConfigs(c.UserContext())
	if err != nil {
		return utils.ErrorResponse(c, fiber.StatusInternalServerError, "Failed to list webhook configs")
	}
	return utils.SuccessResponse(c, fiber.StatusOK, "Webhook configs retrieved", list)
}

func (h *IntegrationHandler) UpdateWebhookConfig(c *fiber.Ctx) error {
	id, err := uuid.Parse(c.Params("id"))
	if err != nil {
		return utils.ErrorResponse(c, fiber.StatusBadRequest, "Invalid config ID")
	}
	var req models.WebhookCallbackConfigRequest
	if err := c.BodyParser(&req); err != nil {
		return utils.ErrorResponse(c, fiber.StatusBadRequest, "Invalid request body")
	}
	resp, err := h.svc.UpdateWebhookConfig(c.UserContext(), id, &req)
	if err != nil {
		return utils.ErrorResponse(c, fiber.StatusInternalServerError, "Failed to update webhook config")
	}
	return utils.SuccessResponse(c, fiber.StatusOK, "Webhook config updated", resp)
}

func (h *IntegrationHandler) DeleteWebhookConfig(c *fiber.Ctx) error {
	id, err := uuid.Parse(c.Params("id"))
	if err != nil {
		return utils.ErrorResponse(c, fiber.StatusBadRequest, "Invalid config ID")
	}
	if err := h.svc.DeleteWebhookConfig(c.UserContext(), id); err != nil {
		return utils.ErrorResponse(c, fiber.StatusInternalServerError, "Failed to delete webhook config")
	}
	return utils.SuccessResponse(c, fiber.StatusOK, "Webhook config deleted", nil)
}
