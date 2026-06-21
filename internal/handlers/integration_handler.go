package handlers

import (
	"strconv"

	"github.com/automax/backend/internal/models"
	"github.com/automax/backend/internal/repository"
	"github.com/automax/backend/internal/services"
	"github.com/automax/backend/pkg/constants"
	"github.com/automax/backend/pkg/i18n"
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
		return utils.ErrorResponse(c, fiber.StatusBadRequest, i18n.T(c.UserContext(), "invalid_request_body"))
	}
	userID := c.Locals(constants.ContextKeys.UserID).(uuid.UUID)
	resp, err := h.svc.CreateVariable(c.UserContext(), &req, userID)
	if err != nil {
		return utils.ErrorResponse(c, fiber.StatusInternalServerError, i18n.T(c.UserContext(), "failed_to_create_variable"))
	}
	return utils.SuccessResponse(c, fiber.StatusCreated, i18n.T(c.UserContext(), "variable_created"), resp)
}

func (h *IntegrationHandler) ListVariables(c *fiber.Ctx) error {
	list, err := h.svc.ListVariables(c.UserContext())
	if err != nil {
		return utils.ErrorResponse(c, fiber.StatusInternalServerError, i18n.T(c.UserContext(), "failed_to_list_variables"))
	}
	return utils.SuccessResponse(c, fiber.StatusOK, i18n.T(c.UserContext(), "variables_retrieved"), list)
}

func (h *IntegrationHandler) DeleteVariable(c *fiber.Ctx) error {
	id, err := uuid.Parse(c.Params("id"))
	if err != nil {
		return utils.ErrorResponse(c, fiber.StatusBadRequest, i18n.T(c.UserContext(), "invalid_variable_id"))
	}
	if err := h.svc.DeleteVariable(c.UserContext(), id); err != nil {
		return utils.ErrorResponse(c, fiber.StatusInternalServerError, i18n.T(c.UserContext(), "failed_to_delete_variable"))
	}
	return utils.SuccessResponse(c, fiber.StatusOK, i18n.T(c.UserContext(), "variable_deleted"), nil)
}

// ---- Scripts ----

func (h *IntegrationHandler) CreateScript(c *fiber.Ctx) error {
	var req models.IntegrationScriptRequest
	if err := c.BodyParser(&req); err != nil {
		return utils.ErrorResponse(c, fiber.StatusBadRequest, i18n.T(c.UserContext(), "invalid_request_body"))
	}
	userID := c.Locals(constants.ContextKeys.UserID).(uuid.UUID)
	resp, err := h.svc.CreateScript(c.UserContext(), &req, userID)
	if err != nil {
		return utils.ErrorResponse(c, fiber.StatusInternalServerError, i18n.T(c.UserContext(), "failed_to_create_script"))
	}
	return utils.SuccessResponse(c, fiber.StatusCreated, i18n.T(c.UserContext(), "script_created"), resp)
}

func (h *IntegrationHandler) GetScript(c *fiber.Ctx) error {
	id, err := uuid.Parse(c.Params("id"))
	if err != nil {
		return utils.ErrorResponse(c, fiber.StatusBadRequest, i18n.T(c.UserContext(), "invalid_script_id"))
	}
	resp, err := h.svc.GetScript(c.UserContext(), id)
	if err != nil {
		return utils.ErrorResponse(c, fiber.StatusNotFound, i18n.T(c.UserContext(), "script_not_found"))
	}
	return utils.SuccessResponse(c, fiber.StatusOK, i18n.T(c.UserContext(), "script_retrieved"), resp)
}

func (h *IntegrationHandler) ListScripts(c *fiber.Ctx) error {
	activeOnly := c.QueryBool("active_only", false)
	list, err := h.svc.ListScripts(c.UserContext(), activeOnly)
	if err != nil {
		return utils.ErrorResponse(c, fiber.StatusInternalServerError, i18n.T(c.UserContext(), "failed_to_list_scripts"))
	}
	return utils.SuccessResponse(c, fiber.StatusOK, i18n.T(c.UserContext(), "scripts_retrieved"), list)
}

func (h *IntegrationHandler) UpdateScript(c *fiber.Ctx) error {
	id, err := uuid.Parse(c.Params("id"))
	if err != nil {
		return utils.ErrorResponse(c, fiber.StatusBadRequest, i18n.T(c.UserContext(), "invalid_script_id"))
	}
	var req models.IntegrationScriptRequest
	if err := c.BodyParser(&req); err != nil {
		return utils.ErrorResponse(c, fiber.StatusBadRequest, i18n.T(c.UserContext(), "invalid_request_body"))
	}
	resp, err := h.svc.UpdateScript(c.UserContext(), id, &req)
	if err != nil {
		return utils.ErrorResponse(c, fiber.StatusInternalServerError, i18n.T(c.UserContext(), "failed_to_update_script"))
	}
	return utils.SuccessResponse(c, fiber.StatusOK, i18n.T(c.UserContext(), "script_updated"), resp)
}

func (h *IntegrationHandler) DeleteScript(c *fiber.Ctx) error {
	id, err := uuid.Parse(c.Params("id"))
	if err != nil {
		return utils.ErrorResponse(c, fiber.StatusBadRequest, i18n.T(c.UserContext(), "invalid_script_id"))
	}
	if err := h.svc.DeleteScript(c.UserContext(), id); err != nil {
		return utils.ErrorResponse(c, fiber.StatusInternalServerError, i18n.T(c.UserContext(), "failed_to_delete_script"))
	}
	return utils.SuccessResponse(c, fiber.StatusOK, i18n.T(c.UserContext(), "script_deleted"), nil)
}

// TestScript runs a script against a real incident for validation purposes.
func (h *IntegrationHandler) TestScript(c *fiber.Ctx) error {
	scriptID, err := uuid.Parse(c.Params("id"))
	if err != nil {
		return utils.ErrorResponse(c, fiber.StatusBadRequest, i18n.T(c.UserContext(), "invalid_script_id"))
	}
	var req models.IntegrationScriptTestRequest
	if err := c.BodyParser(&req); err != nil {
		return utils.ErrorResponse(c, fiber.StatusBadRequest, i18n.T(c.UserContext(), "invalid_request_body"))
	}
	incidentID, err := uuid.Parse(req.IncidentID)
	if err != nil {
		return utils.ErrorResponse(c, fiber.StatusBadRequest, i18n.T(c.UserContext(), "invalid_incident_id_param2"))
	}
	script, err := h.repo.FindScriptByID(c.UserContext(), scriptID)
	if err != nil {
		return utils.ErrorResponse(c, fiber.StatusNotFound, i18n.T(c.UserContext(), "script_not_found"))
	}
	incident, err := h.incidentRepo.FindByIDWithRelations(c.UserContext(), incidentID)
	if err != nil {
		return utils.ErrorResponse(c, fiber.StatusNotFound, i18n.T(c.UserContext(), "incident_not_found"))
	}
	result := h.executor.RunScriptForTest(c.UserContext(), script, incident, "")
	return utils.SuccessResponse(c, fiber.StatusOK, i18n.T(c.UserContext(), "test_executed"), result)
}

// ---- State triggers ----

func (h *IntegrationHandler) CreateStateTrigger(c *fiber.Ctx) error {
	stateID, err := uuid.Parse(c.Params("stateId"))
	if err != nil {
		return utils.ErrorResponse(c, fiber.StatusBadRequest, i18n.T(c.UserContext(), "invalid_state_id2"))
	}
	var req models.WorkflowStateTriggerRequest
	if err := c.BodyParser(&req); err != nil {
		return utils.ErrorResponse(c, fiber.StatusBadRequest, i18n.T(c.UserContext(), "invalid_request_body"))
	}
	trigger, err := h.svc.CreateStateTrigger(c.UserContext(), stateID, &req)
	if err != nil {
		return utils.ErrorResponse(c, fiber.StatusInternalServerError, i18n.T(c.UserContext(), "failed_to_create_state_trigger"))
	}
	return utils.SuccessResponse(c, fiber.StatusCreated, i18n.T(c.UserContext(), "state_trigger_created"), trigger)
}

func (h *IntegrationHandler) ListStateTriggers(c *fiber.Ctx) error {
	stateID, err := uuid.Parse(c.Params("stateId"))
	if err != nil {
		return utils.ErrorResponse(c, fiber.StatusBadRequest, i18n.T(c.UserContext(), "invalid_state_id2"))
	}
	list, err := h.svc.ListStateTriggers(c.UserContext(), stateID)
	if err != nil {
		return utils.ErrorResponse(c, fiber.StatusInternalServerError, i18n.T(c.UserContext(), "failed_to_list_state_triggers"))
	}
	return utils.SuccessResponse(c, fiber.StatusOK, i18n.T(c.UserContext(), "state_triggers_retrieved"), list)
}

func (h *IntegrationHandler) UpdateStateTrigger(c *fiber.Ctx) error {
	triggerID, err := uuid.Parse(c.Params("id"))
	if err != nil {
		return utils.ErrorResponse(c, fiber.StatusBadRequest, i18n.T(c.UserContext(), "invalid_trigger_id"))
	}
	var req models.WorkflowStateTriggerRequest
	if err := c.BodyParser(&req); err != nil {
		return utils.ErrorResponse(c, fiber.StatusBadRequest, i18n.T(c.UserContext(), "invalid_request_body"))
	}
	trigger, err := h.svc.UpdateStateTrigger(c.UserContext(), triggerID, &req)
	if err != nil {
		return utils.ErrorResponse(c, fiber.StatusInternalServerError, i18n.T(c.UserContext(), "failed_to_update_state_trigger"))
	}
	return utils.SuccessResponse(c, fiber.StatusOK, i18n.T(c.UserContext(), "state_trigger_updated"), trigger)
}

func (h *IntegrationHandler) DeleteStateTrigger(c *fiber.Ctx) error {
	triggerID, err := uuid.Parse(c.Params("id"))
	if err != nil {
		return utils.ErrorResponse(c, fiber.StatusBadRequest, i18n.T(c.UserContext(), "invalid_trigger_id"))
	}
	if err := h.svc.DeleteStateTrigger(c.UserContext(), triggerID); err != nil {
		return utils.ErrorResponse(c, fiber.StatusInternalServerError, i18n.T(c.UserContext(), "failed_to_delete_state_trigger"))
	}
	return utils.SuccessResponse(c, fiber.StatusOK, i18n.T(c.UserContext(), "state_trigger_deleted"), nil)
}

// ---- Transition triggers ----

func (h *IntegrationHandler) CreateTransitionTrigger(c *fiber.Ctx) error {
	transitionID, err := uuid.Parse(c.Params("transitionId"))
	if err != nil {
		return utils.ErrorResponse(c, fiber.StatusBadRequest, i18n.T(c.UserContext(), "invalid_transition_id"))
	}
	var req models.WorkflowTransitionTriggerRequest
	if err := c.BodyParser(&req); err != nil {
		return utils.ErrorResponse(c, fiber.StatusBadRequest, i18n.T(c.UserContext(), "invalid_request_body"))
	}
	trigger, err := h.svc.CreateTransitionTrigger(c.UserContext(), transitionID, &req)
	if err != nil {
		return utils.ErrorResponse(c, fiber.StatusInternalServerError, i18n.T(c.UserContext(), "failed_to_create_transition_trigger"))
	}
	return utils.SuccessResponse(c, fiber.StatusCreated, i18n.T(c.UserContext(), "transition_trigger_created"), trigger)
}

func (h *IntegrationHandler) ListTransitionTriggers(c *fiber.Ctx) error {
	transitionID, err := uuid.Parse(c.Params("transitionId"))
	if err != nil {
		return utils.ErrorResponse(c, fiber.StatusBadRequest, i18n.T(c.UserContext(), "invalid_transition_id"))
	}
	list, err := h.svc.ListTransitionTriggers(c.UserContext(), transitionID)
	if err != nil {
		return utils.ErrorResponse(c, fiber.StatusInternalServerError, i18n.T(c.UserContext(), "failed_to_list_transition_triggers"))
	}
	return utils.SuccessResponse(c, fiber.StatusOK, i18n.T(c.UserContext(), "transition_triggers_retrieved"), list)
}

func (h *IntegrationHandler) UpdateTransitionTrigger(c *fiber.Ctx) error {
	triggerID, err := uuid.Parse(c.Params("id"))
	if err != nil {
		return utils.ErrorResponse(c, fiber.StatusBadRequest, i18n.T(c.UserContext(), "invalid_trigger_id"))
	}
	var req models.WorkflowTransitionTriggerRequest
	if err := c.BodyParser(&req); err != nil {
		return utils.ErrorResponse(c, fiber.StatusBadRequest, i18n.T(c.UserContext(), "invalid_request_body"))
	}
	trigger, err := h.svc.UpdateTransitionTrigger(c.UserContext(), triggerID, &req)
	if err != nil {
		return utils.ErrorResponse(c, fiber.StatusInternalServerError, i18n.T(c.UserContext(), "failed_to_update_transition_trigger"))
	}
	return utils.SuccessResponse(c, fiber.StatusOK, i18n.T(c.UserContext(), "transition_trigger_updated"), trigger)
}

func (h *IntegrationHandler) DeleteTransitionTrigger(c *fiber.Ctx) error {
	triggerID, err := uuid.Parse(c.Params("id"))
	if err != nil {
		return utils.ErrorResponse(c, fiber.StatusBadRequest, i18n.T(c.UserContext(), "invalid_trigger_id"))
	}
	if err := h.svc.DeleteTransitionTrigger(c.UserContext(), triggerID); err != nil {
		return utils.ErrorResponse(c, fiber.StatusInternalServerError, i18n.T(c.UserContext(), "failed_to_delete_transition_trigger"))
	}
	return utils.SuccessResponse(c, fiber.StatusOK, i18n.T(c.UserContext(), "transition_trigger_deleted"), nil)
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
		return utils.ErrorResponse(c, fiber.StatusInternalServerError, i18n.T(c.UserContext(), "failed_to_list_logs"))
	}
	return utils.SuccessResponse(c, fiber.StatusOK, i18n.T(c.UserContext(), "logs_retrieved"), fiber.Map{
		"logs":   list,
		"total":  total,
		"limit":  limit,
		"offset": offset,
	})
}

func (h *IntegrationHandler) ListLogsByIncident(c *fiber.Ctx) error {
	incidentID, err := uuid.Parse(c.Params("incidentId"))
	if err != nil {
		return utils.ErrorResponse(c, fiber.StatusBadRequest, i18n.T(c.UserContext(), "invalid_incident_id"))
	}
	limit, _ := strconv.Atoi(c.Query("limit", "50"))
	offset, _ := strconv.Atoi(c.Query("offset", "0"))
	list, total, err := h.svc.ListLogsByIncident(c.UserContext(), incidentID, limit, offset)
	if err != nil {
		return utils.ErrorResponse(c, fiber.StatusInternalServerError, i18n.T(c.UserContext(), "failed_to_list_logs"))
	}
	return utils.SuccessResponse(c, fiber.StatusOK, i18n.T(c.UserContext(), "logs_retrieved"), fiber.Map{
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
		return utils.ErrorResponse(c, fiber.StatusBadRequest, i18n.T(c.UserContext(), "invalid_incident_id"))
	}
	list, err := h.svc.ListBridgesByIncident(c.UserContext(), incidentID)
	if err != nil {
		return utils.ErrorResponse(c, fiber.StatusInternalServerError, i18n.T(c.UserContext(), "failed_to_list_bridges"))
	}
	return utils.SuccessResponse(c, fiber.StatusOK, i18n.T(c.UserContext(), "bridges_retrieved"), list)
}

func (h *IntegrationHandler) CreateBridgeForIncident(c *fiber.Ctx) error {
	incidentID, err := uuid.Parse(c.Params("incidentId"))
	if err != nil {
		return utils.ErrorResponse(c, fiber.StatusBadRequest, i18n.T(c.UserContext(), "invalid_incident_id"))
	}
	var req struct {
		RemoteSystemName     string `json:"remote_system_name"`
		RemoteSystemURL      string `json:"remote_system_url"`
		RemoteIncidentID     string `json:"remote_incident_id"`
		RemoteIncidentNumber string `json:"remote_incident_number"`
		Direction            string `json:"direction"`
	}
	if err := c.BodyParser(&req); err != nil {
		return utils.ErrorResponse(c, fiber.StatusBadRequest, i18n.T(c.UserContext(), "invalid_request_body"))
	}
	if req.Direction != "inbound" {
		req.Direction = "outbound"
	}
	bridge := &models.IncidentBridge{
		LocalIncidentID:      incidentID,
		RemoteSystemName:     req.RemoteSystemName,
		RemoteSystemURL:      req.RemoteSystemURL,
		RemoteIncidentID:     req.RemoteIncidentID,
		RemoteIncidentNumber: req.RemoteIncidentNumber,
		Direction:            req.Direction,
		Status:               "open",
	}
	if err := h.svc.CreateBridge(c.UserContext(), bridge); err != nil {
		return utils.ErrorResponse(c, fiber.StatusInternalServerError, i18n.T(c.UserContext(), "failed_to_create_bridge"))
	}
	return utils.SuccessResponse(c, fiber.StatusCreated, i18n.T(c.UserContext(), "bridge_created"), bridge)
}

// ---- Webhook callback configs ----

func (h *IntegrationHandler) CreateWebhookConfig(c *fiber.Ctx) error {
	var req models.WebhookCallbackConfigRequest
	if err := c.BodyParser(&req); err != nil {
		return utils.ErrorResponse(c, fiber.StatusBadRequest, i18n.T(c.UserContext(), "invalid_request_body"))
	}
	resp, err := h.svc.CreateWebhookConfig(c.UserContext(), &req)
	if err != nil {
		return utils.ErrorResponse(c, fiber.StatusInternalServerError, i18n.T(c.UserContext(), "failed_to_create_webhook"))
	}
	return utils.SuccessResponse(c, fiber.StatusCreated, i18n.T(c.UserContext(), "webhook_created"), resp)
}

func (h *IntegrationHandler) ListWebhookConfigs(c *fiber.Ctx) error {
	list, err := h.svc.ListWebhookConfigs(c.UserContext())
	if err != nil {
		return utils.ErrorResponse(c, fiber.StatusInternalServerError, i18n.T(c.UserContext(), "failed_to_list_webhooks"))
	}
	return utils.SuccessResponse(c, fiber.StatusOK, i18n.T(c.UserContext(), "webhooks_retrieved"), list)
}

func (h *IntegrationHandler) UpdateWebhookConfig(c *fiber.Ctx) error {
	id, err := uuid.Parse(c.Params("id"))
	if err != nil {
		return utils.ErrorResponse(c, fiber.StatusBadRequest, i18n.T(c.UserContext(), "invalid_config_id"))
	}
	var req models.WebhookCallbackConfigRequest
	if err := c.BodyParser(&req); err != nil {
		return utils.ErrorResponse(c, fiber.StatusBadRequest, i18n.T(c.UserContext(), "invalid_request_body"))
	}
	resp, err := h.svc.UpdateWebhookConfig(c.UserContext(), id, &req)
	if err != nil {
		return utils.ErrorResponse(c, fiber.StatusInternalServerError, i18n.T(c.UserContext(), "failed_to_update_webhook"))
	}
	return utils.SuccessResponse(c, fiber.StatusOK, i18n.T(c.UserContext(), "webhook_updated"), resp)
}

func (h *IntegrationHandler) DeleteWebhookConfig(c *fiber.Ctx) error {
	id, err := uuid.Parse(c.Params("id"))
	if err != nil {
		return utils.ErrorResponse(c, fiber.StatusBadRequest, i18n.T(c.UserContext(), "invalid_config_id"))
	}
	if err := h.svc.DeleteWebhookConfig(c.UserContext(), id); err != nil {
		return utils.ErrorResponse(c, fiber.StatusInternalServerError, i18n.T(c.UserContext(), "failed_to_delete_webhook"))
	}
	return utils.SuccessResponse(c, fiber.StatusOK, i18n.T(c.UserContext(), "webhook_deleted"), nil)
}
