package handlers

import (
	"errors"
	"strings"

	"github.com/automax/backend/internal/middleware"
	"github.com/automax/backend/internal/models"
	"github.com/automax/backend/internal/services"
	"github.com/automax/backend/pkg/constants"
	"github.com/automax/backend/pkg/utils"
	"github.com/automax/backend/pkg/validation"
	"github.com/gofiber/fiber/v2"
	"github.com/google/uuid"
)

type IncidentPublicFeedbackHandler struct {
	service      services.IncidentPublicFeedbackService
	actionLogSvc services.ActionLogService
}

func NewIncidentPublicFeedbackHandler(
	service services.IncidentPublicFeedbackService,
	actionLogSvc services.ActionLogService,
) *IncidentPublicFeedbackHandler {
	return &IncidentPublicFeedbackHandler{service: service, actionLogSvc: actionLogSvc}
}

// Create handles POST /api/v1/public-feedback/:incidentID (authenticated).
func (h *IncidentPublicFeedbackHandler) Create(c *fiber.Ctx) error {
	incidentID, err := uuid.Parse(c.Params("incidentID"))
	if err != nil {
		return utils.ErrorResponse(c, fiber.StatusBadRequest, "Invalid incident ID")
	}

	var req models.IncidentPublicFeedbackCreateRequest
	if err := c.BodyParser(&req); err != nil {
		return utils.ErrorResponse(c, fiber.StatusBadRequest, "Invalid request body")
	}

	if validationErrors := validation.ValidateStruct(c.UserContext(), &req); len(validationErrors) != 0 {
		return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{
			"success": false,
			"errors":  validationErrors,
		})
	}

	resp, err := h.service.Create(c.UserContext(), incidentID, &req)
	if err != nil {
		return utils.ErrorResponse(c, fiber.StatusInternalServerError, err.Error())
	}

	middleware.LogAction(c, h.actionLogSvc, &services.LogActionParams{
		Action:      "create",
		Module:      "incident_public_feedback",
		ResourceID:  resp.ID.String(),
		Description: "Incident public feedback created for incident " + incidentID.String(),
	})

	return utils.SuccessResponse(c, fiber.StatusCreated, "Public feedback created", resp)
}

// Submit handles PUT /api/v1/public-feedback/:incidentID/submit?signed_token=xxx (public, no auth).
func (h *IncidentPublicFeedbackHandler) Submit(c *fiber.Ctx) error {
	incidentID, err := uuid.Parse(c.Params("incidentID"))
	if err != nil {
		return utils.ErrorResponse(c, fiber.StatusBadRequest, "Invalid incident ID")
	}

	token := c.Query("signed_token")
	if token == "" {
		return utils.ErrorResponse(c, fiber.StatusBadRequest, "Missing signed token")
	}

	// Extract feedbackID from the raw token (format: fb|feedbackID|incidentID|expiresAt|hmac).
	// ValidateFeedbackToken will re-verify the HMAC over the full payload including feedbackID,
	// so any tampering causes ErrInvalid regardless of what we read here.
	rawParts := strings.Split(token, "|")
	if len(rawParts) != 5 {
		return utils.ErrorResponse(c, fiber.StatusBadRequest, "Malformed token")
	}
	feedbackIDStr := rawParts[1]

	feedbackID, err := uuid.Parse(feedbackIDStr)
	if err != nil {
		return utils.ErrorResponse(c, fiber.StatusBadRequest, "Malformed token")
	}

	if _, err := utils.ValidateFeedbackToken(token, feedbackIDStr, incidentID.String()); err != nil {
		switch {
		case errors.Is(err, utils.ErrExpired):
			return utils.ErrorResponse(c, fiber.StatusUnauthorized, "Token has expired")
		case errors.Is(err, utils.ErrIDMismatch):
			return utils.ErrorResponse(c, fiber.StatusUnauthorized, "Token does not match this incident")
		default:
			return utils.ErrorResponse(c, fiber.StatusUnauthorized, "Invalid token")
		}
	}

	var req models.IncidentPublicFeedbackSubmitRequest
	if err := c.BodyParser(&req); err != nil {
		return utils.ErrorResponse(c, fiber.StatusBadRequest, "Invalid request body")
	}

	if validationErrors := validation.ValidateStruct(c.UserContext(), &req); len(validationErrors) != 0 {
		return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{
			"success": false,
			"errors":  validationErrors,
		})
	}

	resp, err := h.service.Submit(c.UserContext(), incidentID, feedbackID, &req)
	if err != nil {
		switch err.Error() {
		case "feedback record not found":
			return utils.ErrorResponse(c, fiber.StatusNotFound, err.Error())
		case "feedback has already been submitted":
			return utils.ErrorResponse(c, fiber.StatusConflict, err.Error())
		default:
			return utils.ErrorResponse(c, fiber.StatusInternalServerError, err.Error())
		}
	}

	// Only log activity when a user identity is present (authenticated callers).
	if userID, ok := c.Locals(constants.ContextKeys.UserID).(uuid.UUID); ok && userID != uuid.Nil {
		middleware.LogAction(c, h.actionLogSvc, &services.LogActionParams{
			Action:      "update",
			Module:      "incident_public_feedback",
			ResourceID:  resp.ID.String(),
			Description: "Incident public feedback submitted for incident " + incidentID.String(),
		})
	}

	return utils.SuccessResponse(c, fiber.StatusOK, "Feedback submitted successfully", resp)
}

// ListAll handles GET /api/v1/public-feedback/ (authenticated, incidents:view).
func (h *IncidentPublicFeedbackHandler) ListAll(c *fiber.Ctx) error {
	items, err := h.service.List(c.UserContext())
	if err != nil {
		return utils.ErrorResponse(c, fiber.StatusInternalServerError, err.Error())
	}
	return utils.SuccessResponse(c, fiber.StatusOK, "Public feedback retrieved", items)
}

// ListByIncident handles GET /api/v1/public-feedback/:incidentID (authenticated, incidents:view).
func (h *IncidentPublicFeedbackHandler) ListByIncident(c *fiber.Ctx) error {
	incidentID, err := uuid.Parse(c.Params("incidentID"))
	if err != nil {
		return utils.ErrorResponse(c, fiber.StatusBadRequest, "Invalid incident ID")
	}

	items, err := h.service.ListByIncident(c.UserContext(), incidentID)
	if err != nil {
		return utils.ErrorResponse(c, fiber.StatusInternalServerError, err.Error())
	}
	return utils.SuccessResponse(c, fiber.StatusOK, "Public feedback retrieved", items)
}
