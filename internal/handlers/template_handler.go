package handlers

import (
	"errors"

	"github.com/gofiber/fiber/v2"
	"github.com/google/uuid"
	"gorm.io/gorm"

	"github.com/automax/backend/internal/models"
	"github.com/automax/backend/internal/services"
)

type NotificationTemplateHandler struct {
	svc services.NotificationTemplateService
}

func NewNotificationTemplateHandler(svc services.NotificationTemplateService) *NotificationTemplateHandler {
	return &NotificationTemplateHandler{svc: svc}
}

// POST /admin/notification-templates
// Creates a bilingual template (EN + AR) as a single DB row.
func (h *NotificationTemplateHandler) Create(c *fiber.Ctx) error {
	var req models.NotificationTemplateCreateRequest
	if err := c.BodyParser(&req); err != nil {
		return fiber.NewError(fiber.StatusBadRequest, "invalid request body")
	}

	tpl, err := h.svc.Create(c.UserContext(), &req)
	if err != nil {
		return fiber.NewError(fiber.StatusUnprocessableEntity, err.Error())
	}

	return c.Status(fiber.StatusCreated).JSON(fiber.Map{
		"success": true,
		"data":    models.ToNotificationTemplateResponse(tpl),
	})
}

// GET /admin/notification-templates
// Query params: channel, module_type, action_type, is_active (true/false), code, search, page, limit
func (h *NotificationTemplateHandler) List(c *fiber.Ctx) error {
	filter := models.NotificationTemplateFilter{
		Channel:    c.Query("channel"),
		ModuleType: c.Query("module_type"),
		ActionType: c.Query("action_type"),
		Code:       c.Query("code"),
		Search:     c.Query("search"),
		Page:       c.QueryInt("page", 1),
		Limit:      c.QueryInt("limit", 20),
	}

	if v := c.Query("is_active"); v == "true" {
		t := true
		filter.IsActive = &t
	} else if v == "false" {
		f := false
		filter.IsActive = &f
	}

	if v := c.Query("transition_id"); v != "" {
		if tid, err := uuid.Parse(v); err == nil {
			filter.TransitionID = &tid
		}
	}

	list, total, err := h.svc.List(c.UserContext(), filter)
	if err != nil {
		return err
	}

	responses := make([]models.NotificationTemplateResponse, len(list))
	for i, t := range list {
		responses[i] = models.ToNotificationTemplateResponse(&t)
	}

	page := filter.Page
	limit := filter.Limit
	if page < 1 {
		page = 1
	}
	if limit < 1 || limit > 100 {
		limit = 20
	}

	return c.JSON(fiber.Map{
		"success": true,
		"data":    responses,
		"total":   total,
		"page":    page,
		"limit":   limit,
	})
}

// GET /admin/notification-templates/:id
func (h *NotificationTemplateHandler) GetByID(c *fiber.Ctx) error {
	id, err := uuid.Parse(c.Params("id"))
	if err != nil {
		return fiber.NewError(fiber.StatusBadRequest, "invalid id")
	}

	tpl, err := h.svc.GetByID(c.UserContext(), id)
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return fiber.NewError(fiber.StatusNotFound, "template not found")
		}
		return err
	}

	return c.JSON(fiber.Map{
		"success": true,
		"data":    models.ToNotificationTemplateResponse(tpl),
	})
}

// GET /admin/notification-templates/by-code/:code
// Query param: channel (optional)
func (h *NotificationTemplateHandler) GetByCode(c *fiber.Ctx) error {
	code := c.Params("code")
	if code == "" {
		return fiber.NewError(fiber.StatusBadRequest, "code is required")
	}

	list, err := h.svc.GetByCode(c.UserContext(), code, c.Query("channel"))
	if err != nil {
		return err
	}

	responses := make([]models.NotificationTemplateResponse, len(list))
	for i, t := range list {
		responses[i] = models.ToNotificationTemplateResponse(&t)
	}

	return c.JSON(fiber.Map{
		"success": true,
		"data":    responses,
	})
}

// GET /admin/notification-templates/by-transition/:transitionId
func (h *NotificationTemplateHandler) GetByTransition(c *fiber.Ctx) error {
	transitionID, err := uuid.Parse(c.Params("transitionId"))
	if err != nil {
		return fiber.NewError(fiber.StatusBadRequest, "invalid transitionId")
	}

	list, err := h.svc.GetByTransitionID(c.UserContext(), transitionID)
	if err != nil {
		return err
	}

	responses := make([]models.NotificationTemplateResponse, len(list))
	for i, t := range list {
		responses[i] = models.ToNotificationTemplateResponse(&t)
	}

	return c.JSON(fiber.Map{
		"success": true,
		"data":    responses,
	})
}

// PUT /admin/notification-templates/:id
func (h *NotificationTemplateHandler) Update(c *fiber.Ctx) error {
	id, err := uuid.Parse(c.Params("id"))
	if err != nil {
		return fiber.NewError(fiber.StatusBadRequest, "invalid id")
	}

	var req models.NotificationTemplateUpdateRequest
	if err := c.BodyParser(&req); err != nil {
		return fiber.NewError(fiber.StatusBadRequest, "invalid request body")
	}

	tpl, err := h.svc.Update(c.UserContext(), id, &req)
	if err != nil {
		return fiber.NewError(fiber.StatusUnprocessableEntity, err.Error())
	}

	return c.JSON(fiber.Map{
		"success": true,
		"data":    models.ToNotificationTemplateResponse(tpl),
	})
}

// PATCH /admin/notification-templates/:id/toggle
func (h *NotificationTemplateHandler) Toggle(c *fiber.Ctx) error {
	id, err := uuid.Parse(c.Params("id"))
	if err != nil {
		return fiber.NewError(fiber.StatusBadRequest, "invalid id")
	}

	tpl, err := h.svc.ToggleActive(c.UserContext(), id)
	if err != nil {
		return err
	}

	return c.JSON(fiber.Map{
		"success":   true,
		"is_active": tpl.IsActive,
		"data":      models.ToNotificationTemplateResponse(tpl),
	})
}

// DELETE /admin/notification-templates/:id
func (h *NotificationTemplateHandler) Delete(c *fiber.Ctx) error {
	id, err := uuid.Parse(c.Params("id"))
	if err != nil {
		return fiber.NewError(fiber.StatusBadRequest, "invalid id")
	}

	if err := h.svc.Delete(c.UserContext(), id); err != nil {
		return err
	}

	return c.JSON(fiber.Map{"success": true})
}

// GET /admin/notification-templates/available-variables
// Returns the map of action_type → available variable names that can be used in template bodies.
func (h *NotificationTemplateHandler) GetAvailableVariables(c *fiber.Ctx) error {
	return c.JSON(fiber.Map{
		"success": true,
		"data":    models.AvailableVariablesByActionType,
		"syntax":  "Use {{variable_name}} in body/subject, e.g. Dear {{first_name}}, incident {{incident_number}}",
	})
}
