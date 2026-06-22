package handlers

import (
	"errors"
	"strings"

	"github.com/automax/backend/internal/middleware"
	"github.com/automax/backend/internal/models"
	"github.com/automax/backend/internal/services"
	"github.com/automax/backend/pkg/constants"
	"github.com/automax/backend/pkg/i18n"
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
		return utils.ErrorResponse(c, fiber.StatusBadRequest, i18n.T(c.UserContext(), "invalid_incident_id"))
	}

	var req models.IncidentPublicFeedbackCreateRequest
	if err := c.BodyParser(&req); err != nil {
		return utils.ErrorResponse(c, fiber.StatusBadRequest, i18n.T(c.UserContext(), "invalid_request_body"))
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

	return utils.SuccessResponse(c, fiber.StatusCreated, i18n.T(c.UserContext(), "public_feedback_created"), resp)
}

// Init handles GET /api/v1/public-feedback/:incidentID/init?signed_token=xxx (public, no auth).
// It validates the 3-part incident token embedded in the {{feedback_url}} template variable,
// creates or reuses a pending feedback record, and returns a proper fb-format signed token
// the frontend can use to call the Submit endpoint.
func (h *IncidentPublicFeedbackHandler) Init(c *fiber.Ctx) error {
	incidentID, err := uuid.Parse(c.Params("incidentID"))
	if err != nil {
		return utils.ErrorResponse(c, fiber.StatusBadRequest, i18n.T(c.UserContext(), "invalid_incident_id"))
	}

	token := c.Query("signed_token")
	if token == "" {
		return utils.ErrorResponse(c, fiber.StatusBadRequest, i18n.T(c.UserContext(), "missing_signed_token"))
	}

	resp, err := h.service.Init(c.UserContext(), incidentID, token)
	if err != nil {
		switch err.Error() {
		case "token has expired":
			return utils.ErrorResponse(c, fiber.StatusUnauthorized, err.Error())
		case "token does not match this incident", "invalid token":
			return utils.ErrorResponse(c, fiber.StatusUnauthorized, err.Error())
		default:
			return utils.ErrorResponse(c, fiber.StatusInternalServerError, err.Error())
		}
	}

	return utils.SuccessResponse(c, fiber.StatusOK, i18n.T(c.UserContext(), "feedback_initialized"), resp)
}

// Submit handles PUT /api/v1/public-feedback/:incidentID/submit (public, no auth).
// Accepts either ?signed_token=<fb-token> or ?feedback_id=<uuid>.
// When feedback_id is used, ownership is validated by the DB in the service layer.
func (h *IncidentPublicFeedbackHandler) Submit(c *fiber.Ctx) error {
	incidentID, err := uuid.Parse(c.Params("incidentID"))
	if err != nil {
		return utils.ErrorResponse(c, fiber.StatusBadRequest, i18n.T(c.UserContext(), "invalid_incident_id"))
	}

	var feedbackID uuid.UUID

	token := c.Query("signed_token")
	directFeedbackID := c.Query("feedback_id")

	switch {
	case token != "":
		// If signed_token is a plain UUID, treat it directly as feedback_id (no HMAC validation).
		// Otherwise expect a full fb-format token (fb|feedbackID|incidentID|expiresAt|hmac).
		if parsed, uuidErr := uuid.Parse(token); uuidErr == nil {
			feedbackID = parsed
		} else {
			rawParts := strings.Split(token, "|")
			if len(rawParts) != 5 {
				return utils.ErrorResponse(c, fiber.StatusBadRequest, i18n.T(c.UserContext(), "malformed_token"))
			}
			feedbackIDStr := rawParts[1]
			feedbackID, err = uuid.Parse(feedbackIDStr)
			if err != nil {
				return utils.ErrorResponse(c, fiber.StatusBadRequest, i18n.T(c.UserContext(), "malformed_token"))
			}
			if _, err := utils.ValidateFeedbackToken(token, feedbackIDStr, incidentID.String()); err != nil {
				switch {
				case errors.Is(err, utils.ErrExpired):
					return utils.ErrorResponse(c, fiber.StatusUnauthorized, i18n.T(c.UserContext(), "token_expired"))
				case errors.Is(err, utils.ErrIDMismatch):
					return utils.ErrorResponse(c, fiber.StatusUnauthorized, i18n.T(c.UserContext(), "token_incident_mismatch"))
				default:
					return utils.ErrorResponse(c, fiber.StatusUnauthorized, "Invalid token")
				}
			}
		}
	case directFeedbackID != "":
		feedbackID, err = uuid.Parse(directFeedbackID)
		if err != nil {
			return utils.ErrorResponse(c, fiber.StatusBadRequest, i18n.T(c.UserContext(), "invalid_feedback_id"))
		}
	default:
		return utils.ErrorResponse(c, fiber.StatusBadRequest, i18n.T(c.UserContext(), "missing_token_or_feedback_id"))
	}

	var req models.IncidentPublicFeedbackSubmitRequest
	if err := c.BodyParser(&req); err != nil {
		return utils.ErrorResponse(c, fiber.StatusBadRequest, i18n.T(c.UserContext(), "invalid_request_body"))
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
		case "feedback has already been submitted",
			"feedback has already been submitted for this incident",
			"feedback has already been submitted for this incident via WhatsApp":
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

	return utils.SuccessResponse(c, fiber.StatusOK, i18n.T(c.UserContext(), "feedback_submitted"), resp)
}

// ListAll handles GET /api/v1/public-feedback/ (authenticated, incidents:view).
func (h *IncidentPublicFeedbackHandler) ListAll(c *fiber.Ctx) error {
	items, err := h.service.List(c.UserContext())
	if err != nil {
		return utils.ErrorResponse(c, fiber.StatusInternalServerError, err.Error())
	}
	return utils.SuccessResponse(c, fiber.StatusOK, i18n.T(c.UserContext(), "public_feedback_retrieved"), items)
}

// ListByIncident handles GET /api/v1/public-feedback/:incidentID (authenticated, incidents:view).
func (h *IncidentPublicFeedbackHandler) ListByIncident(c *fiber.Ctx) error {
	incidentID, err := uuid.Parse(c.Params("incidentID"))
	if err != nil {
		return utils.ErrorResponse(c, fiber.StatusBadRequest, i18n.T(c.UserContext(), "invalid_incident_id"))
	}

	items, err := h.service.ListByIncident(c.UserContext(), incidentID)
	if err != nil {
		return utils.ErrorResponse(c, fiber.StatusInternalServerError, err.Error())
	}
	return utils.SuccessResponse(c, fiber.StatusOK, i18n.T(c.UserContext(), "public_feedback_retrieved"), items)
}
