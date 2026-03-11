package handlers

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"strings"
	"time"

	"github.com/automax/backend/internal/models"
	"github.com/automax/backend/internal/services"
	"github.com/automax/backend/pkg/constants"
	"github.com/automax/backend/pkg/utils"
	"github.com/automax/backend/pkg/validation"
	"github.com/go-playground/validator/v10"
	"github.com/gofiber/fiber/v2"
	"github.com/google/uuid"
)

type WorkflowHandler struct {
	service          services.WorkflowService
	actionLogService services.ActionLogService
	validator        *validator.Validate
}

func NewWorkflowHandler(service services.WorkflowService, actionLogService services.ActionLogService) *WorkflowHandler {
	return &WorkflowHandler{
		service:          service,
		actionLogService: actionLogService,
		validator:        validator.New(),
	}
}

// Workflow CRUD

func (h *WorkflowHandler) CreateWorkflow(c *fiber.Ctx) error {
	var req models.WorkflowCreateRequest
	if err := c.BodyParser(&req); err != nil {
		return utils.ErrorResponse(c, fiber.StatusBadRequest, "Invalid request body")
	}

	if validationErrors := validation.ValidateStruct(c.UserContext(), &req); len(validationErrors) != 0 {
		return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{
			"success": false,
			"errors":  validationErrors,
		})
	}

	// Get user ID from context
	userID := c.Locals(constants.ContextKeys.UserID).(uuid.UUID)

	workflow, err := h.service.CreateWorkflow(c.UserContext(), &req, userID)
	if err != nil {
		return utils.ErrorResponse(c, fiber.StatusInternalServerError, err.Error())
	}

	// Log the creation with workflow details
	ipAddress, _ := c.Locals(constants.ContextKeys.IP_ADDRESS).(string)
	userAgent, _ := c.Locals(constants.ContextKeys.USER_AGENT).(string)
	go func() {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()

		newValue := map[string]interface{}{
			"name":        workflow.Name,
			"code":        workflow.Code,
			"description": workflow.Description,
			"record_type": workflow.RecordType,
			"is_active":   workflow.IsActive,
			"is_default":  workflow.IsDefault,
		}

		_ = h.actionLogService.LogAction(ctx, &services.LogActionParams{
			UserID:      userID,
			Action:      "create",
			Module:      "workflows",
			ResourceID:  workflow.ID.String(),
			Description: fmt.Sprintf("Created new workflow '%s'", workflow.Name),
			NewValue:    newValue,
			IPAddress:   ipAddress,
			UserAgent:   userAgent,
			Status:      "success",
		})
	}()

	return utils.SuccessResponse(c, fiber.StatusCreated, "Workflow created", workflow)
}

func (h *WorkflowHandler) GetWorkflow(c *fiber.Ctx) error {
	idStr := c.Params("id")
	id, err := uuid.Parse(idStr)
	if err != nil {
		return utils.ErrorResponse(c, fiber.StatusBadRequest, "Invalid ID")
	}

	workflow, err := h.service.GetWorkflow(c.UserContext(), id)
	if err != nil {
		return utils.ErrorResponse(c, fiber.StatusNotFound, "Workflow not found")
	}

	return utils.SuccessResponse(c, fiber.StatusOK, "Workflow retrieved", workflow)
}

func (h *WorkflowHandler) ListWorkflows(c *fiber.Ctx) error {
	activeOnly := c.Query("active_only") == "true"
	recordType := c.Query("record_type")

	var workflows []models.WorkflowResponse
	var err error

	if recordType != "" {
		workflows, err = h.service.ListWorkflowsByRecordType(c.UserContext(), recordType, activeOnly)
	} else {
		workflows, err = h.service.ListWorkflows(c.UserContext(), activeOnly)
	}
	if err != nil {
		return utils.ErrorResponse(c, fiber.StatusInternalServerError, err.Error())
	}

	return utils.SuccessResponse(c, fiber.StatusOK, "Workflows retrieved", workflows)
}

func (h *WorkflowHandler) UpdateWorkflow(c *fiber.Ctx) error {
	idStr := c.Params("id")
	id, err := uuid.Parse(idStr)
	if err != nil {
		return utils.ErrorResponse(c, fiber.StatusBadRequest, "Invalid ID")
	}

	var req models.WorkflowUpdateRequest
	if err := c.BodyParser(&req); err != nil {
		return utils.ErrorResponse(c, fiber.StatusBadRequest, "Invalid request body")
	}

	// Get old workflow for action logging
	oldWorkflow, err := h.service.GetWorkflow(c.UserContext(), id)
	if err != nil {
		return utils.ErrorResponse(c, fiber.StatusInternalServerError, err.Error())
	}

	workflow, err := h.service.UpdateWorkflow(c.UserContext(), id, &req)
	if err != nil {
		return utils.ErrorResponse(c, fiber.StatusInternalServerError, err.Error())
	}

	// Capture values before async logging
	userID := c.Locals(constants.ContextKeys.UserID).(uuid.UUID)
	ipAddress, _ := c.Locals(constants.ContextKeys.IP_ADDRESS).(string)
	userAgent, _ := c.Locals(constants.ContextKeys.USER_AGENT).(string)

	// Build comprehensive old and new values
	oldValue := map[string]interface{}{
		"name":                     oldWorkflow.Name,
		"code":                     oldWorkflow.Code,
		"description":              oldWorkflow.Description,
		"record_type":              oldWorkflow.RecordType,
		"is_active":                oldWorkflow.IsActive,
		"is_default":               oldWorkflow.IsDefault,
		"sources":                  oldWorkflow.Sources,
		"priorities":               oldWorkflow.Priorities,
		"canvas_layout":            oldWorkflow.CanvasLayout,
		"required_fields":          oldWorkflow.RequiredFields,
		"classifications":          oldWorkflow.Classifications,
		"locations":                oldWorkflow.Locations,
		"convert_to_request_roles": oldWorkflow.ConvertToRequestRoles,
		"merge_allowed_roles":      oldWorkflow.MergeAllowedRoles,
	}
	newValue := map[string]interface{}{
		"name":                     workflow.Name,
		"code":                     workflow.Code,
		"description":              workflow.Description,
		"record_type":              workflow.RecordType,
		"is_active":                workflow.IsActive,
		"is_default":               workflow.IsDefault,
		"sources":                  workflow.Sources,
		"priorities":               workflow.Priorities,
		"canvas_layout":            workflow.CanvasLayout,
		"required_fields":          workflow.RequiredFields,
		"classifications":          workflow.Classifications,
		"locations":                workflow.Locations,
		"convert_to_request_roles": workflow.ConvertToRequestRoles,
		"merge_allowed_roles":      workflow.MergeAllowedRoles,
	}

	// Build detailed change description
	var changeDetails []string

	// Check each field for changes
	fieldLabels := map[string]string{
		"name": "Name", "code": "Code", "description": "Description",
		"record_type": "Record Type", "is_active": "Is Active", "is_default": "Is Default",
		"sources": "Sources", "priorities": "Priorities", "canvas_layout": "Canvas Layout",
		"required_fields": "Required Fields", "classifications": "Classifications",
		"locations": "Locations", "convert_to_request_roles": "Convert to Request Roles",
		"merge_allowed_roles": "Merge Allowed Roles",
	}

	for key, label := range fieldLabels {
		oldVal := oldValue[key]
		newVal := newValue[key]
		if fmt.Sprintf("%v", oldVal) != fmt.Sprintf("%v", newVal) {
			changeDetails = append(changeDetails, label)
		}
	}

	description := fmt.Sprintf("Updated workflow '%s'", workflow.Name)
	if len(changeDetails) > 0 {
		description += fmt.Sprintf(" - changed: %s", strings.Join(changeDetails, ", "))
	}

	// Log the update with old and new values
	go func() {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()

		_ = h.actionLogService.LogAction(ctx, &services.LogActionParams{
			UserID:      userID,
			Action:      "update",
			Module:      "workflows",
			ResourceID:  id.String(),
			Description: description,
			OldValue:    oldValue,
			NewValue:    newValue,
			IPAddress:   ipAddress,
			UserAgent:   userAgent,
			Status:      "success",
		})
	}()

	return utils.SuccessResponse(c, fiber.StatusOK, "Workflow updated", workflow)
}

func (h *WorkflowHandler) DeleteWorkflow(c *fiber.Ctx) error {
	idStr := c.Params("id")
	id, err := uuid.Parse(idStr)
	if err != nil {
		return utils.ErrorResponse(c, fiber.StatusBadRequest, "Invalid ID")
	}

	// Get workflow details for action logging
	oldWorkflow, err := h.service.GetWorkflow(c.UserContext(), id)
	if err != nil {
		return utils.ErrorResponse(c, fiber.StatusInternalServerError, err.Error())
	}

	if err := h.service.DeleteWorkflow(c.UserContext(), id); err != nil {
		return utils.ErrorResponse(c, fiber.StatusInternalServerError, err.Error())
	}

	// Log the deletion
	userID := c.Locals(constants.ContextKeys.UserID).(uuid.UUID)
	ipAddress, _ := c.Locals(constants.ContextKeys.IP_ADDRESS).(string)
	userAgent, _ := c.Locals(constants.ContextKeys.USER_AGENT).(string)
	go func() {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()

		oldValue := map[string]interface{}{
			"name":        oldWorkflow.Name,
			"code":        oldWorkflow.Code,
			"description": oldWorkflow.Description,
		}

		_ = h.actionLogService.LogAction(ctx, &services.LogActionParams{
			UserID:      userID,
			Action:      "delete",
			Module:      "workflows",
			ResourceID:  id.String(),
			Description: fmt.Sprintf("Deleted workflow '%s'", oldWorkflow.Name),
			OldValue:    oldValue,
			IPAddress:   ipAddress,
			UserAgent:   userAgent,
			Status:      "success",
		})
	}()

	return utils.SuccessResponse(c, fiber.StatusOK, "Workflow deleted", nil)
}

func (h *WorkflowHandler) ListDeletedWorkflows(c *fiber.Ctx) error {
	workflows, err := h.service.ListDeletedWorkflows(c.UserContext())
	if err != nil {
		return utils.ErrorResponse(c, fiber.StatusInternalServerError, err.Error())
	}

	return utils.SuccessResponse(c, fiber.StatusOK, "Deleted workflows retrieved", workflows)
}

func (h *WorkflowHandler) PermanentDeleteWorkflow(c *fiber.Ctx) error {
	idStr := c.Params("id")
	id, err := uuid.Parse(idStr)
	if err != nil {
		return utils.ErrorResponse(c, fiber.StatusBadRequest, "Invalid ID")
	}

	if err := h.service.PermanentDeleteWorkflow(c.UserContext(), id); err != nil {
		return utils.ErrorResponse(c, fiber.StatusInternalServerError, err.Error())
	}

	return utils.SuccessResponse(c, fiber.StatusOK, "Workflow permanently deleted", nil)
}

func (h *WorkflowHandler) RestoreWorkflow(c *fiber.Ctx) error {
	idStr := c.Params("id")
	id, err := uuid.Parse(idStr)
	if err != nil {
		return utils.ErrorResponse(c, fiber.StatusBadRequest, "Invalid ID")
	}

	if err := h.service.RestoreWorkflow(c.UserContext(), id); err != nil {
		return utils.ErrorResponse(c, fiber.StatusInternalServerError, err.Error())
	}

	return utils.SuccessResponse(c, fiber.StatusOK, "Workflow restored", nil)
}

func (h *WorkflowHandler) DuplicateWorkflow(c *fiber.Ctx) error {
	idStr := c.Params("id")
	id, err := uuid.Parse(idStr)
	if err != nil {
		return utils.ErrorResponse(c, fiber.StatusBadRequest, "Invalid ID")
	}

	userID := c.Locals(constants.ContextKeys.UserID).(uuid.UUID)

	workflow, err := h.service.DuplicateWorkflow(c.UserContext(), id, userID)
	if err != nil {
		return utils.ErrorResponse(c, fiber.StatusInternalServerError, err.Error())
	}

	return utils.SuccessResponse(c, fiber.StatusCreated, "Workflow duplicated", workflow)
}

// Classification assignment

func (h *WorkflowHandler) AssignClassifications(c *fiber.Ctx) error {
	idStr := c.Params("id")
	id, err := uuid.Parse(idStr)
	if err != nil {
		return utils.ErrorResponse(c, fiber.StatusBadRequest, "Invalid ID")
	}

	var req struct {
		ClassificationIDs []string `json:"classification_ids"`
	}
	if err := c.BodyParser(&req); err != nil {
		return utils.ErrorResponse(c, fiber.StatusBadRequest, "Invalid request body")
	}

	classIDs := make([]uuid.UUID, 0, len(req.ClassificationIDs))
	for _, idStr := range req.ClassificationIDs {
		classID, err := uuid.Parse(idStr)
		if err != nil {
			continue
		}
		classIDs = append(classIDs, classID)
	}

	if err := h.service.AssignClassifications(c.UserContext(), id, classIDs); err != nil {
		return utils.ErrorResponse(c, fiber.StatusInternalServerError, err.Error())
	}

	// Fetch updated workflow
	workflow, err := h.service.GetWorkflow(c.UserContext(), id)
	if err != nil {
		return utils.ErrorResponse(c, fiber.StatusInternalServerError, err.Error())
	}

	// Log the classification assignment
	userID := c.Locals(constants.ContextKeys.UserID).(uuid.UUID)
	ipAddress, _ := c.Locals(constants.ContextKeys.IP_ADDRESS).(string)
	userAgent, _ := c.Locals(constants.ContextKeys.USER_AGENT).(string)
	go func() {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()

		// Extract classification IDs from classifications
		var classIDs []string
		for _, c := range workflow.Classifications {
			classIDs = append(classIDs, c.ID.String())
		}

		newValue := map[string]interface{}{
			"classifications": classIDs,
		}

		_ = h.actionLogService.LogAction(ctx, &services.LogActionParams{
			UserID:      userID,
			Action:      "update",
			Module:      "workflows",
			ResourceID:  id.String(),
			Description: fmt.Sprintf("Updated classifications for workflow '%s'", workflow.Name),
			NewValue:    newValue,
			IPAddress:   ipAddress,
			UserAgent:   userAgent,
			Status:      "success",
		})
	}()

	return utils.SuccessResponse(c, fiber.StatusOK, "Classifications assigned", workflow)
}

func (h *WorkflowHandler) GetWorkflowByClassification(c *fiber.Ctx) error {
	idStr := c.Params("classification_id")
	id, err := uuid.Parse(idStr)
	if err != nil {
		return utils.ErrorResponse(c, fiber.StatusBadRequest, "Invalid classification ID")
	}

	workflow, err := h.service.GetWorkflowByClassification(c.UserContext(), id)
	if err != nil {
		return utils.ErrorResponse(c, fiber.StatusNotFound, err.Error())
	}

	return utils.SuccessResponse(c, fiber.StatusOK, "Workflow retrieved", workflow)
}

// State management

func (h *WorkflowHandler) CreateState(c *fiber.Ctx) error {
	workflowIDStr := c.Params("id")
	workflowID, err := uuid.Parse(workflowIDStr)
	if err != nil {
		return utils.ErrorResponse(c, fiber.StatusBadRequest, "Invalid workflow ID")
	}

	var req models.WorkflowStateCreateRequest
	if err := c.BodyParser(&req); err != nil {
		return utils.ErrorResponse(c, fiber.StatusBadRequest, "Invalid request body")
	}

	if validationErrors := validation.ValidateStruct(c.UserContext(), &req); len(validationErrors) != 0 {
		return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{
			"success": false,
			"errors":  validationErrors,
		})
	}

	state, err := h.service.CreateState(c.UserContext(), workflowID, &req)
	if err != nil {
		return utils.ErrorResponse(c, fiber.StatusInternalServerError, err.Error())
	}

	// Log the creation
	userID := c.Locals(constants.ContextKeys.UserID).(uuid.UUID)
	ipAddress, _ := c.Locals(constants.ContextKeys.IP_ADDRESS).(string)
	userAgent, _ := c.Locals(constants.ContextKeys.USER_AGENT).(string)
	go func() {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()

		// Extract viewable role IDs
		var viewableRoleIDs []string
		for _, r := range state.ViewableRoles {
			viewableRoleIDs = append(viewableRoleIDs, r.ID.String())
		}

		newValue := map[string]interface{}{
			"name":           state.Name,
			"code":           state.Code,
			"description":    state.Description,
			"state_type":     state.StateType,
			"color":          state.Color,
			"sla_hours":      state.SLAHours,
			"sort_order":     state.SortOrder,
			"position_x":     state.PositionX,
			"position_y":     state.PositionY,
			"is_active":      state.IsActive,
			"viewable_roles": viewableRoleIDs,
		}

		_ = h.actionLogService.LogAction(ctx, &services.LogActionParams{
			UserID:      userID,
			Action:      "create",
			Module:      "workflow_states",
			ResourceID:  state.ID.String(),
			Description: fmt.Sprintf("Created workflow state '%s' in workflow", state.Name),
			NewValue:    newValue,
			IPAddress:   ipAddress,
			UserAgent:   userAgent,
			Status:      "success",
		})
	}()

	return utils.SuccessResponse(c, fiber.StatusCreated, "State created", state)
}

func (h *WorkflowHandler) ListStates(c *fiber.Ctx) error {
	workflowIDStr := c.Params("id")
	workflowID, err := uuid.Parse(workflowIDStr)
	if err != nil {
		return utils.ErrorResponse(c, fiber.StatusBadRequest, "Invalid workflow ID")
	}

	states, err := h.service.ListStates(c.UserContext(), workflowID)
	if err != nil {
		return utils.ErrorResponse(c, fiber.StatusInternalServerError, err.Error())
	}

	return utils.SuccessResponse(c, fiber.StatusOK, "States retrieved", states)
}

func (h *WorkflowHandler) UpdateState(c *fiber.Ctx) error {
	stateIDStr := c.Params("state_id")
	stateID, err := uuid.Parse(stateIDStr)
	if err != nil {
		return utils.ErrorResponse(c, fiber.StatusBadRequest, "Invalid state ID")
	}

	var req models.WorkflowStateUpdateRequest
	if err := c.BodyParser(&req); err != nil {
		return utils.ErrorResponse(c, fiber.StatusBadRequest, "Invalid request body")
	}

	// Get old state for action logging
	oldState, err := h.service.GetState(c.UserContext(), stateID)
	if err != nil {
		return utils.ErrorResponse(c, fiber.StatusInternalServerError, err.Error())
	}

	state, err := h.service.UpdateState(c.UserContext(), stateID, &req)
	if err != nil {
		return utils.ErrorResponse(c, fiber.StatusInternalServerError, err.Error())
	}

	// Capture values before async logging
	userID := c.Locals(constants.ContextKeys.UserID).(uuid.UUID)
	ipAddress, _ := c.Locals(constants.ContextKeys.IP_ADDRESS).(string)
	userAgent, _ := c.Locals(constants.ContextKeys.USER_AGENT).(string)

	// Log the update with old and new values
	go func() {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()

		// Extract viewable role IDs
		var oldViewableRoleIDs, newViewableRoleIDs []string
		for _, r := range oldState.ViewableRoles {
			oldViewableRoleIDs = append(oldViewableRoleIDs, r.ID.String())
		}
		for _, r := range state.ViewableRoles {
			newViewableRoleIDs = append(newViewableRoleIDs, r.ID.String())
		}

		oldValue := map[string]interface{}{
			"name":           oldState.Name,
			"code":           oldState.Code,
			"description":    oldState.Description,
			"state_type":     oldState.StateType,
			"color":          oldState.Color,
			"sla_hours":      oldState.SLAHours,
			"sort_order":     oldState.SortOrder,
			"is_active":      oldState.IsActive,
			"position_x":     oldState.PositionX,
			"position_y":     oldState.PositionY,
			"viewable_roles": oldViewableRoleIDs,
		}
		newValue := map[string]interface{}{
			"name":           state.Name,
			"code":           state.Code,
			"description":    state.Description,
			"state_type":     state.StateType,
			"color":          state.Color,
			"sla_hours":      state.SLAHours,
			"sort_order":     state.SortOrder,
			"is_active":      state.IsActive,
			"position_x":     state.PositionX,
			"position_y":     state.PositionY,
			"viewable_roles": newViewableRoleIDs,
		}

		// Build detailed change description
		var changeDetails []string
		fieldLabels := map[string]string{
			"name": "Name", "code": "Code", "description": "Description",
			"state_type": "State Type", "color": "Color", "sla_hours": "SLA Hours",
			"sort_order": "Sort Order", "is_active": "Is Active",
			"position_x": "Position X", "position_y": "Position Y",
			"viewable_roles": "Viewable Roles",
		}

		for key, label := range fieldLabels {
			if fmt.Sprintf("%v", oldValue[key]) != fmt.Sprintf("%v", newValue[key]) {
				changeDetails = append(changeDetails, label)
			}
		}

		desc := fmt.Sprintf("Updated workflow state '%s'", state.Name)
		if len(changeDetails) > 0 {
			desc += fmt.Sprintf(" - changed: %s", strings.Join(changeDetails, ", "))
		}

		_ = h.actionLogService.LogAction(ctx, &services.LogActionParams{
			UserID:      userID,
			Action:      "update",
			Module:      "workflow_states",
			ResourceID:  stateID.String(),
			Description: desc,
			OldValue:    oldValue,
			NewValue:    newValue,
			IPAddress:   ipAddress,
			UserAgent:   userAgent,
			Status:      "success",
		})
	}()

	return utils.SuccessResponse(c, fiber.StatusOK, "State updated", state)
}

func (h *WorkflowHandler) DeleteState(c *fiber.Ctx) error {
	stateIDStr := c.Params("state_id")
	stateID, err := uuid.Parse(stateIDStr)
	if err != nil {
		return utils.ErrorResponse(c, fiber.StatusBadRequest, "Invalid state ID")
	}

	// Get state details for action logging
	oldState, err := h.service.GetState(c.UserContext(), stateID)
	if err != nil {
		return utils.ErrorResponse(c, fiber.StatusInternalServerError, err.Error())
	}

	if err := h.service.DeleteState(c.UserContext(), stateID); err != nil {
		return utils.ErrorResponse(c, fiber.StatusInternalServerError, err.Error())
	}

	// Log the deletion
	userID := c.Locals(constants.ContextKeys.UserID).(uuid.UUID)
	ipAddress, _ := c.Locals(constants.ContextKeys.IP_ADDRESS).(string)
	userAgent, _ := c.Locals(constants.ContextKeys.USER_AGENT).(string)
	go func() {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()

		// Extract viewable role IDs
		var viewableRoleIDs []string
		for _, r := range oldState.ViewableRoles {
			viewableRoleIDs = append(viewableRoleIDs, r.ID.String())
		}

		oldValue := map[string]interface{}{
			"name":           oldState.Name,
			"code":           oldState.Code,
			"description":    oldState.Description,
			"state_type":     oldState.StateType,
			"color":          oldState.Color,
			"sla_hours":      oldState.SLAHours,
			"sort_order":     oldState.SortOrder,
			"position_x":     oldState.PositionX,
			"position_y":     oldState.PositionY,
			"is_active":      oldState.IsActive,
			"viewable_roles": viewableRoleIDs,
		}

		_ = h.actionLogService.LogAction(ctx, &services.LogActionParams{
			UserID:      userID,
			Action:      "delete",
			Module:      "workflow_states",
			ResourceID:  stateID.String(),
			Description: fmt.Sprintf("Deleted workflow state '%s'", oldState.Name),
			OldValue:    oldValue,
			IPAddress:   ipAddress,
			UserAgent:   userAgent,
			Status:      "success",
		})
	}()

	return utils.SuccessResponse(c, fiber.StatusOK, "State deleted", nil)
}

// Transition management

func (h *WorkflowHandler) CreateTransition(c *fiber.Ctx) error {
	workflowIDStr := c.Params("id")
	workflowID, err := uuid.Parse(workflowIDStr)
	if err != nil {
		return utils.ErrorResponse(c, fiber.StatusBadRequest, "Invalid workflow ID")
	}

	var req models.WorkflowTransitionCreateRequest
	if err := c.BodyParser(&req); err != nil {
		return utils.ErrorResponse(c, fiber.StatusBadRequest, "Invalid request body")
	}

	if validationErrors := validation.ValidateStruct(c.UserContext(), &req); len(validationErrors) != 0 {
		return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{
			"success": false,
			"errors":  validationErrors,
		})
	}

	transition, err := h.service.CreateTransition(c.UserContext(), workflowID, &req)
	if err != nil {
		return utils.ErrorResponse(c, fiber.StatusInternalServerError, err.Error())
	}

	// Log the creation
	userID := c.Locals(constants.ContextKeys.UserID).(uuid.UUID)
	ipAddress, _ := c.Locals(constants.ContextKeys.IP_ADDRESS).(string)
	userAgent, _ := c.Locals(constants.ContextKeys.USER_AGENT).(string)
	go func() {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()

		// Extract allowed role IDs
		var allowedRoleIDs []string
		for _, r := range transition.AllowedRoles {
			allowedRoleIDs = append(allowedRoleIDs, r.ID.String())
		}
		var assignmentRoleIDs []string
		for _, r := range transition.AssignmentRoles {
			assignmentRoleIDs = append(assignmentRoleIDs, r.ID.String())
		}

		newValue := map[string]interface{}{
			"name":                   transition.Name,
			"code":                   transition.Code,
			"description":            transition.Description,
			"from_state_id":          transition.FromStateID.String(),
			"to_state_id":            transition.ToStateID.String(),
			"sort_order":             transition.SortOrder,
			"is_active":              transition.IsActive,
			"allowed_roles":          allowedRoleIDs,
			"assign_department_id":   transition.AssignDepartmentID,
			"auto_detect_department": transition.AutoDetectDepartment,
			"assign_user_id":         transition.AssignUserID,
			"assignment_role_ids":    assignmentRoleIDs,
			"auto_match_user":        transition.AutoMatchUser,
			"manual_select_user":     transition.ManualSelectUser,
		}

		_ = h.actionLogService.LogAction(ctx, &services.LogActionParams{
			UserID:      userID,
			Action:      "create",
			Module:      "workflow_transitions",
			ResourceID:  transition.ID.String(),
			Description: fmt.Sprintf("Created workflow transition '%s' (from state to state)", transition.Name),
			NewValue:    newValue,
			IPAddress:   ipAddress,
			UserAgent:   userAgent,
			Status:      "success",
		})
	}()

	return utils.SuccessResponse(c, fiber.StatusCreated, "Transition created", transition)
}

func (h *WorkflowHandler) ListTransitions(c *fiber.Ctx) error {
	workflowIDStr := c.Params("id")
	workflowID, err := uuid.Parse(workflowIDStr)
	if err != nil {
		return utils.ErrorResponse(c, fiber.StatusBadRequest, "Invalid workflow ID")
	}

	transitions, err := h.service.ListTransitions(c.UserContext(), workflowID)
	if err != nil {
		return utils.ErrorResponse(c, fiber.StatusInternalServerError, err.Error())
	}

	return utils.SuccessResponse(c, fiber.StatusOK, "Transitions retrieved", transitions)
}

func (h *WorkflowHandler) UpdateTransition(c *fiber.Ctx) error {
	transitionIDStr := c.Params("transition_id")
	transitionID, err := uuid.Parse(transitionIDStr)
	if err != nil {
		return utils.ErrorResponse(c, fiber.StatusBadRequest, "Invalid transition ID")
	}

	var req models.WorkflowTransitionUpdateRequest
	if err := c.BodyParser(&req); err != nil {
		return utils.ErrorResponse(c, fiber.StatusBadRequest, "Invalid request body")
	}

	// Get old transition for action logging
	oldTransition, err := h.service.GetTransition(c.UserContext(), transitionID)
	if err != nil {
		return utils.ErrorResponse(c, fiber.StatusInternalServerError, err.Error())
	}

	transition, err := h.service.UpdateTransition(c.UserContext(), transitionID, &req)
	if err != nil {
		return utils.ErrorResponse(c, fiber.StatusInternalServerError, err.Error())
	}

	// Capture values before async logging
	userID := c.Locals(constants.ContextKeys.UserID).(uuid.UUID)
	ipAddress, _ := c.Locals(constants.ContextKeys.IP_ADDRESS).(string)
	userAgent, _ := c.Locals(constants.ContextKeys.USER_AGENT).(string)

	// Log the update with old and new values
	go func() {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()

		// Extract allowed role IDs
		var oldAllowedRoleIDs, newAllowedRoleIDs []string
		for _, r := range oldTransition.AllowedRoles {
			oldAllowedRoleIDs = append(oldAllowedRoleIDs, r.ID.String())
		}
		for _, r := range transition.AllowedRoles {
			newAllowedRoleIDs = append(newAllowedRoleIDs, r.ID.String())
		}
		var oldAssignmentRoleIDs, newAssignmentRoleIDs []string
		for _, r := range oldTransition.AssignmentRoles {
			oldAssignmentRoleIDs = append(oldAssignmentRoleIDs, r.ID.String())
		}
		for _, r := range transition.AssignmentRoles {
			newAssignmentRoleIDs = append(newAssignmentRoleIDs, r.ID.String())
		}

		oldValue := map[string]interface{}{
			"name":                   oldTransition.Name,
			"code":                   oldTransition.Code,
			"description":            oldTransition.Description,
			"from_state_id":          oldTransition.FromStateID.String(),
			"to_state_id":            oldTransition.ToStateID.String(),
			"sort_order":             oldTransition.SortOrder,
			"is_active":              oldTransition.IsActive,
			"allowed_roles":          oldAllowedRoleIDs,
			"assign_department_id":   oldTransition.AssignDepartmentID,
			"auto_detect_department": oldTransition.AutoDetectDepartment,
			"assign_user_id":         oldTransition.AssignUserID,
			"assignment_role_ids":    oldAssignmentRoleIDs,
			"auto_match_user":        oldTransition.AutoMatchUser,
			"manual_select_user":     oldTransition.ManualSelectUser,
		}
		newValue := map[string]interface{}{
			"name":                   transition.Name,
			"code":                   transition.Code,
			"description":            transition.Description,
			"from_state_id":          transition.FromStateID.String(),
			"to_state_id":            transition.ToStateID.String(),
			"sort_order":             transition.SortOrder,
			"is_active":              transition.IsActive,
			"allowed_roles":          newAllowedRoleIDs,
			"assign_department_id":   transition.AssignDepartmentID,
			"auto_detect_department": transition.AutoDetectDepartment,
			"assign_user_id":         transition.AssignUserID,
			"assignment_role_ids":    newAssignmentRoleIDs,
			"auto_match_user":        transition.AutoMatchUser,
			"manual_select_user":     transition.ManualSelectUser,
		}

		// Build detailed change description
		var changeDetails []string
		fieldLabels := map[string]string{
			"name": "Name", "code": "Code", "description": "Description",
			"from_state_id": "From State", "to_state_id": "To State",
			"sort_order": "Sort Order", "is_active": "Is Active", "allowed_roles": "Roles",
			"assign_department_id": "Assign Department", "auto_detect_department": "Auto Detect Dept",
			"assign_user_id": "Assign User", "assignment_role_ids": "Assignment Roles",
			"auto_match_user": "Auto Match User", "manual_select_user": "Manual Select User",
		}

		for key, label := range fieldLabels {
			if fmt.Sprintf("%v", oldValue[key]) != fmt.Sprintf("%v", newValue[key]) {
				changeDetails = append(changeDetails, label)
			}
		}

		desc := fmt.Sprintf("Updated workflow transition '%s'", transition.Name)
		if len(changeDetails) > 0 {
			desc += fmt.Sprintf(" - changed: %s", strings.Join(changeDetails, ", "))
		}

		_ = h.actionLogService.LogAction(ctx, &services.LogActionParams{
			UserID:      userID,
			Action:      "update",
			Module:      "workflow_transitions",
			ResourceID:  transitionID.String(),
			Description: desc,
			OldValue:    oldValue,
			NewValue:    newValue,
			IPAddress:   ipAddress,
			UserAgent:   userAgent,
			Status:      "success",
		})
	}()

	return utils.SuccessResponse(c, fiber.StatusOK, "Transition updated", transition)
}

func (h *WorkflowHandler) DeleteTransition(c *fiber.Ctx) error {
	transitionIDStr := c.Params("transition_id")
	transitionID, err := uuid.Parse(transitionIDStr)
	if err != nil {
		return utils.ErrorResponse(c, fiber.StatusBadRequest, "Invalid transition ID")
	}

	// Get transition details for action logging
	oldTransition, err := h.service.GetTransition(c.UserContext(), transitionID)
	if err != nil {
		return utils.ErrorResponse(c, fiber.StatusInternalServerError, err.Error())
	}

	if err := h.service.DeleteTransition(c.UserContext(), transitionID); err != nil {
		return utils.ErrorResponse(c, fiber.StatusInternalServerError, err.Error())
	}

	// Log the deletion
	userID := c.Locals(constants.ContextKeys.UserID).(uuid.UUID)
	ipAddress, _ := c.Locals(constants.ContextKeys.IP_ADDRESS).(string)
	userAgent, _ := c.Locals(constants.ContextKeys.USER_AGENT).(string)
	go func() {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()

		// Extract allowed role IDs
		var allowedRoleIDs []string
		for _, r := range oldTransition.AllowedRoles {
			allowedRoleIDs = append(allowedRoleIDs, r.ID.String())
		}
		var assignmentRoleIDs []string
		for _, r := range oldTransition.AssignmentRoles {
			assignmentRoleIDs = append(assignmentRoleIDs, r.ID.String())
		}

		oldValue := map[string]interface{}{
			"name":                   oldTransition.Name,
			"code":                   oldTransition.Code,
			"description":            oldTransition.Description,
			"from_state_id":          oldTransition.FromStateID.String(),
			"to_state_id":            oldTransition.ToStateID.String(),
			"sort_order":             oldTransition.SortOrder,
			"is_active":              oldTransition.IsActive,
			"allowed_roles":          allowedRoleIDs,
			"assign_department_id":   oldTransition.AssignDepartmentID,
			"auto_detect_department": oldTransition.AutoDetectDepartment,
			"assign_user_id":         oldTransition.AssignUserID,
			"assignment_role_ids":    assignmentRoleIDs,
			"auto_match_user":        oldTransition.AutoMatchUser,
			"manual_select_user":     oldTransition.ManualSelectUser,
		}

		_ = h.actionLogService.LogAction(ctx, &services.LogActionParams{
			UserID:      userID,
			Action:      "delete",
			Module:      "workflow_transitions",
			ResourceID:  transitionID.String(),
			Description: fmt.Sprintf("Deleted workflow transition '%s'", oldTransition.Name),
			OldValue:    oldValue,
			IPAddress:   ipAddress,
			UserAgent:   userAgent,
			Status:      "success",
		})
	}()

	return utils.SuccessResponse(c, fiber.StatusOK, "Transition deleted", nil)
}

// Transition configuration

func (h *WorkflowHandler) SetTransitionRoles(c *fiber.Ctx) error {
	transitionIDStr := c.Params("id")
	transitionID, err := uuid.Parse(transitionIDStr)
	if err != nil {
		return utils.ErrorResponse(c, fiber.StatusBadRequest, "Invalid transition ID")
	}

	var req struct {
		RoleIDs []string `json:"role_ids"`
	}
	if err := c.BodyParser(&req); err != nil {
		return utils.ErrorResponse(c, fiber.StatusBadRequest, "Invalid request body")
	}

	roleIDs := make([]uuid.UUID, 0, len(req.RoleIDs))
	for _, idStr := range req.RoleIDs {
		roleID, err := uuid.Parse(idStr)
		if err != nil {
			continue
		}
		roleIDs = append(roleIDs, roleID)
	}

	if err := h.service.SetTransitionRoles(c.UserContext(), transitionID, roleIDs); err != nil {
		return utils.ErrorResponse(c, fiber.StatusInternalServerError, err.Error())
	}

	// Log the role assignment
	userID := c.Locals(constants.ContextKeys.UserID).(uuid.UUID)
	ipAddress, _ := c.Locals(constants.ContextKeys.IP_ADDRESS).(string)
	userAgent, _ := c.Locals(constants.ContextKeys.USER_AGENT).(string)
	go func() {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()

		_ = h.actionLogService.LogAction(ctx, &services.LogActionParams{
			UserID:      userID,
			Action:      "update",
			Module:      "workflow_transitions",
			ResourceID:  transitionID.String(),
			Description: fmt.Sprintf("Updated roles for transition (ID: %s)", transitionID.String()),
			NewValue:    map[string]interface{}{"role_ids": req.RoleIDs},
			IPAddress:   ipAddress,
			UserAgent:   userAgent,
			Status:      "success",
		})
	}()

	return utils.SuccessResponse(c, fiber.StatusOK, "Transition roles updated", nil)
}

func (h *WorkflowHandler) SetTransitionRequirements(c *fiber.Ctx) error {
	transitionIDStr := c.Params("id")
	transitionID, err := uuid.Parse(transitionIDStr)
	if err != nil {
		return utils.ErrorResponse(c, fiber.StatusBadRequest, "Invalid transition ID")
	}

	var req struct {
		Requirements []models.TransitionRequirementRequest `json:"requirements"`
	}
	if err := c.BodyParser(&req); err != nil {
		return utils.ErrorResponse(c, fiber.StatusBadRequest, "Invalid request body")
	}

	if err := h.service.SetTransitionRequirements(c.UserContext(), transitionID, req.Requirements); err != nil {
		return utils.ErrorResponse(c, fiber.StatusInternalServerError, err.Error())
	}

	// Log the requirements update
	userID := c.Locals(constants.ContextKeys.UserID).(uuid.UUID)
	ipAddress, _ := c.Locals(constants.ContextKeys.IP_ADDRESS).(string)
	userAgent, _ := c.Locals(constants.ContextKeys.USER_AGENT).(string)
	go func() {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()

		_ = h.actionLogService.LogAction(ctx, &services.LogActionParams{
			UserID:      userID,
			Action:      "update",
			Module:      "workflow_transitions",
			ResourceID:  transitionID.String(),
			Description: fmt.Sprintf("Updated requirements for transition (ID: %s)", transitionID.String()),
			NewValue:    map[string]interface{}{"requirements": req.Requirements},
			IPAddress:   ipAddress,
			UserAgent:   userAgent,
			Status:      "success",
		})
	}()

	return utils.SuccessResponse(c, fiber.StatusOK, "Transition requirements updated", nil)
}

func (h *WorkflowHandler) SetTransitionActions(c *fiber.Ctx) error {
	transitionIDStr := c.Params("id")
	transitionID, err := uuid.Parse(transitionIDStr)
	if err != nil {
		return utils.ErrorResponse(c, fiber.StatusBadRequest, "Invalid transition ID")
	}

	var req struct {
		Actions []models.TransitionActionRequest `json:"actions"`
	}
	if err := c.BodyParser(&req); err != nil {
		return utils.ErrorResponse(c, fiber.StatusBadRequest, "Invalid request body")
	}

	if err := h.service.SetTransitionActions(c.UserContext(), transitionID, req.Actions); err != nil {
		return utils.ErrorResponse(c, fiber.StatusInternalServerError, err.Error())
	}

	// Log the actions update
	userID := c.Locals(constants.ContextKeys.UserID).(uuid.UUID)
	ipAddress, _ := c.Locals(constants.ContextKeys.IP_ADDRESS).(string)
	userAgent, _ := c.Locals(constants.ContextKeys.USER_AGENT).(string)
	go func() {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()

		_ = h.actionLogService.LogAction(ctx, &services.LogActionParams{
			UserID:      userID,
			Action:      "update",
			Module:      "workflow_transitions",
			ResourceID:  transitionID.String(),
			Description: fmt.Sprintf("Updated actions for transition (ID: %s)", transitionID.String()),
			NewValue:    map[string]interface{}{"actions": req.Actions},
			IPAddress:   ipAddress,
			UserAgent:   userAgent,
			Status:      "success",
		})
	}()

	return utils.SuccessResponse(c, fiber.StatusOK, "Transition actions updated", nil)
}

func (h *WorkflowHandler) SetTransitionFieldChanges(c *fiber.Ctx) error {
	transitionIDStr := c.Params("id")
	transitionID, err := uuid.Parse(transitionIDStr)
	if err != nil {
		return utils.ErrorResponse(c, fiber.StatusBadRequest, "Invalid transition ID")
	}

	var req struct {
		FieldChanges []models.TransitionFieldChangeRequest `json:"field_changes"`
	}
	if err := c.BodyParser(&req); err != nil {
		return utils.ErrorResponse(c, fiber.StatusBadRequest, "Invalid request body")
	}

	if err := h.service.SetTransitionFieldChanges(c.UserContext(), transitionID, req.FieldChanges); err != nil {
		return utils.ErrorResponse(c, fiber.StatusInternalServerError, err.Error())
	}

	// Log the field changes update
	userID := c.Locals(constants.ContextKeys.UserID).(uuid.UUID)
	ipAddress, _ := c.Locals(constants.ContextKeys.IP_ADDRESS).(string)
	userAgent, _ := c.Locals(constants.ContextKeys.USER_AGENT).(string)
	go func() {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()

		_ = h.actionLogService.LogAction(ctx, &services.LogActionParams{
			UserID:      userID,
			Action:      "update",
			Module:      "workflow_transitions",
			ResourceID:  transitionID.String(),
			Description: fmt.Sprintf("Updated field changes for transition (ID: %s)", transitionID.String()),
			NewValue:    map[string]interface{}{"field_changes": req.FieldChanges},
			IPAddress:   ipAddress,
			UserAgent:   userAgent,
			Status:      "success",
		})
	}()

	return utils.SuccessResponse(c, fiber.StatusOK, "Transition field changes updated", nil)
}

// Helper endpoints

func (h *WorkflowHandler) GetTransitionsFromState(c *fiber.Ctx) error {
	stateIDStr := c.Params("state_id")
	stateID, err := uuid.Parse(stateIDStr)
	if err != nil {
		return utils.ErrorResponse(c, fiber.StatusBadRequest, "Invalid state ID")
	}

	transitions, err := h.service.GetTransitionsFromState(c.UserContext(), stateID)
	if err != nil {
		return utils.ErrorResponse(c, fiber.StatusInternalServerError, err.Error())
	}

	return utils.SuccessResponse(c, fiber.StatusOK, "Transitions retrieved", transitions)
}

func (h *WorkflowHandler) GetInitialState(c *fiber.Ctx) error {
	workflowIDStr := c.Params("id")
	workflowID, err := uuid.Parse(workflowIDStr)
	if err != nil {
		return utils.ErrorResponse(c, fiber.StatusBadRequest, "Invalid workflow ID")
	}

	state, err := h.service.GetInitialState(c.UserContext(), workflowID)
	if err != nil {
		return utils.ErrorResponse(c, fiber.StatusNotFound, "Initial state not found")
	}

	return utils.SuccessResponse(c, fiber.StatusOK, "Initial state retrieved", state)
}

// MatchWorkflow finds a workflow based on incident criteria and returns form configuration
// This endpoint is designed for mobile apps and other clients to get:
// 1. The matched workflow based on classification, location, source, etc.
// 2. The required fields for incident creation
// 3. All form fields with their labels and descriptions
func (h *WorkflowHandler) MatchWorkflow(c *fiber.Ctx) error {
	var req models.WorkflowMatchRequest
	if err := c.BodyParser(&req); err != nil {
		// If no body provided, use empty request (returns default workflow)
		req = models.WorkflowMatchRequest{}
	}

	result, err := h.service.MatchWorkflow(c.UserContext(), &req)
	if err != nil {
		return utils.ErrorResponse(c, fiber.StatusInternalServerError, err.Error())
	}

	return utils.SuccessResponse(c, fiber.StatusOK, "Workflow matched", result)
}

// ExportWorkflow exports a workflow as a JSON file
func (h *WorkflowHandler) ExportWorkflow(c *fiber.Ctx) error {
	idStr := c.Params("id")
	id, err := uuid.Parse(idStr)
	if err != nil {
		return utils.ErrorResponse(c, fiber.StatusBadRequest, "Invalid ID")
	}

	jsonBytes, filename, err := h.service.ExportWorkflow(c.UserContext(), id)
	if err != nil {
		return utils.ErrorResponse(c, fiber.StatusInternalServerError, err.Error())
	}

	// Set headers for file download
	c.Set("Content-Type", "application/json")
	c.Set("Content-Disposition", "attachment; filename=\""+filename+"\"")

	return c.Send(jsonBytes)
}

// ImportWorkflow imports a workflow from a JSON file
func (h *WorkflowHandler) ImportWorkflow(c *fiber.Ctx) error {
	// Get file from multipart form
	file, err := c.FormFile("file")
	if err != nil {
		return utils.ErrorResponse(c, fiber.StatusBadRequest, "No file uploaded")
	}

	// Validate file size (10MB limit)
	const maxFileSize = 10 * 1024 * 1024 // 10MB
	if file.Size > maxFileSize {
		return utils.ErrorResponse(c, fiber.StatusRequestEntityTooLarge, "File size exceeds 10MB limit")
	}

	// Open and read file
	fileContent, err := file.Open()
	if err != nil {
		return utils.ErrorResponse(c, fiber.StatusInternalServerError, "Failed to read file")
	}
	defer fileContent.Close()

	// Read file content
	buf, err := io.ReadAll(fileContent)
	if err != nil {
		return utils.ErrorResponse(c, fiber.StatusInternalServerError, "Failed to read file content")
	}

	// Parse JSON
	var importData models.WorkflowImportData
	if err := json.Unmarshal(buf, &importData); err != nil {
		return utils.ErrorResponse(c, fiber.StatusBadRequest, "Invalid JSON format: "+err.Error())
	}

	// Get user ID from context
	userID := c.Locals(constants.ContextKeys.UserID).(uuid.UUID)

	// Import workflow
	workflow, warnings, err := h.service.ImportWorkflow(c.UserContext(), &importData, userID)
	if err != nil {
		return utils.ErrorResponse(c, fiber.StatusInternalServerError, err.Error())
	}

	// Build response
	response := models.WorkflowImportResponse{
		Workflow: *workflow,
		Warnings: warnings,
	}

	return utils.SuccessResponse(c, fiber.StatusCreated, "Workflow imported successfully", response)
}

// getChangedFields returns a comma-separated list of field names that changed
func getChangedFields(changes map[string]interface{}) string {
	var fields []string
	for key := range changes {
		fields = append(fields, strings.ReplaceAll(key, "_", " "))
	}
	if len(fields) == 0 {
		return "none"
	}
	return strings.Join(fields, ", ")
}
