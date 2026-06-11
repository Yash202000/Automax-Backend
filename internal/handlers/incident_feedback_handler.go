package handlers

import (
	"log"

	"github.com/automax/backend/internal/models"
	"github.com/automax/backend/internal/repository"
	"github.com/automax/backend/pkg/constants"
	"github.com/automax/backend/pkg/utils"
	"github.com/automax/backend/pkg/validation"
	"github.com/gofiber/fiber/v2"
	"github.com/google/uuid"
)

type IncidentFeedbackHandler struct {
	incidentRepo       repository.IncidentRepository
	publicFeedbackRepo repository.IncidentPublicFeedbackRepository
}

func NewIncidentFeedbackHandler(incidentRepo repository.IncidentRepository, publicFeedbackRepo repository.IncidentPublicFeedbackRepository) *IncidentFeedbackHandler {
	return &IncidentFeedbackHandler{incidentRepo: incidentRepo, publicFeedbackRepo: publicFeedbackRepo}
}

// CreateFeedback creates a feedback entry for an incident.
// POST /api/v1/incidents/:id/feedback
func (h *IncidentFeedbackHandler) CreateFeedback(c *fiber.Ctx) error {
	incidentID, err := uuid.Parse(c.Params("id"))
	if err != nil {
		return utils.ErrorResponse(c, fiber.StatusBadRequest, "Invalid incident ID")
	}

	var req models.IncidentFeedbackRequest
	if err := c.BodyParser(&req); err != nil {
		return utils.ErrorResponse(c, fiber.StatusBadRequest, "Invalid request body")
	}

	if validationErrors := validation.ValidateStruct(c.UserContext(), &req); len(validationErrors) != 0 {
		return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{
			"success": false,
			"errors":  validationErrors,
		})
	}

	userID, ok := c.Locals(constants.ContextKeys.UserID).(uuid.UUID)
	if !ok {
		return utils.ErrorResponse(c, fiber.StatusUnauthorized, "User not authenticated")
	}

	incident, err := h.incidentRepo.FindByID(c.UserContext(), incidentID)
	if err != nil {
		return utils.ErrorResponse(c, fiber.StatusNotFound, "Incident not found")
	}

	// Allow feedback from the reporter OR any user with incidents:transition permission (e.g., chatbot service account)
	isReporter := incident.ReporterID != nil && *incident.ReporterID == userID
	if !isReporter {
		if user, ok := c.Locals(constants.ContextKeys.User).(*models.User); ok && user != nil {
			hasPermission := false
			for _, role := range user.Roles {
				for _, perm := range role.Permissions {
					if perm.Code == "incidents:transition" {
						hasPermission = true
						break
					}
				}
				if hasPermission {
					break
				}
			}
			if !hasPermission {
				return utils.ErrorResponse(c, fiber.StatusForbidden, "Only the incident reporter or authorized users can submit feedback")
			}
		} else {
			return utils.ErrorResponse(c, fiber.StatusForbidden, "Only the incident reporter or authorized users can submit feedback")
		}
	}

	// Block duplicate WhatsApp chatbot submissions (transition_history_id IS NULL).
	// A regular agent feedback always has transition_history_id set, so this only
	// fires when the chatbot tries to submit a second time for the same incident.
	alreadySubmitted, err := h.incidentRepo.HasWhatsAppFeedback(c.UserContext(), incidentID)
	if err != nil {
		return utils.ErrorResponse(c, fiber.StatusInternalServerError, "Failed to validate feedback status")
	}
	if alreadySubmitted {
		return utils.ErrorResponse(c, fiber.StatusConflict, "Feedback has already been submitted for this incident via WhatsApp")
	}

	// Block if public SMS feedback has already been submitted for this incident.
	if h.publicFeedbackRepo != nil {
		publicSubmitted, err := h.publicFeedbackRepo.HasSubmittedFeedback(c.UserContext(), incidentID)
		if err != nil {
			return utils.ErrorResponse(c, fiber.StatusInternalServerError, "Failed to validate feedback status")
		}
		if publicSubmitted {
			return utils.ErrorResponse(c, fiber.StatusConflict, "Feedback has already been submitted for this incident")
		}
	}

	feedback := &models.IncidentFeedback{
		IncidentID:  incidentID,
		Rating:      req.Rating,
		Comment:     req.Comment,
		CreatedByID: userID,
	}

	if err := h.incidentRepo.CreateFeedback(c.UserContext(), feedback); err != nil {
		return utils.ErrorResponse(c, fiber.StatusInternalServerError, "Failed to create feedback")
	}

	// Re-fetch with CreatedBy preloaded
	created, err := h.incidentRepo.FindFeedbackByID(c.UserContext(), feedback.ID)
	if err != nil {
		return utils.SuccessResponse(c, fiber.StatusCreated, "Feedback created", models.ToIncidentFeedbackResponse(feedback))
	}

	return utils.SuccessResponse(c, fiber.StatusCreated, "Feedback created", models.ToIncidentFeedbackResponse(created))
}

// ListAllFeedback returns all feedback entries across all incidents.
// GET /api/v1/incidents/feedback
func (h *IncidentFeedbackHandler) ListAllFeedback(c *fiber.Ctx) error {
	log.Println("hello feedback")
	feedbackList, err := h.incidentRepo.ListAllFeedback(c.UserContext())
	if err != nil {
		return utils.ErrorResponse(c, fiber.StatusInternalServerError, "Failed to fetch feedback")
	}

	responses := make([]models.IncidentFeedbackResponse, len(feedbackList))
	for i, f := range feedbackList {
		responses[i] = models.ToIncidentFeedbackResponse(&f)
	}

	return utils.SuccessResponse(c, fiber.StatusOK, "Feedback retrieved", responses)
}

// ListFeedback returns all feedback entries for an incident.
// GET /api/v1/incidents/:id/feedback
func (h *IncidentFeedbackHandler) ListFeedback(c *fiber.Ctx) error {
	incidentID, err := uuid.Parse(c.Params("id"))
	if err != nil {
		return utils.ErrorResponse(c, fiber.StatusBadRequest, "Invalid incident ID")
	}

	feedbackList, err := h.incidentRepo.ListFeedback(c.UserContext(), incidentID)
	if err != nil {
		return utils.ErrorResponse(c, fiber.StatusInternalServerError, "Failed to fetch feedback")
	}

	responses := make([]models.IncidentFeedbackResponse, len(feedbackList))
	for i, f := range feedbackList {
		responses[i] = models.ToIncidentFeedbackResponse(&f)
	}

	return utils.SuccessResponse(c, fiber.StatusOK, "Feedback retrieved", responses)
}
