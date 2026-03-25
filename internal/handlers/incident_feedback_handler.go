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
	incidentRepo repository.IncidentRepository
}

func NewIncidentFeedbackHandler(incidentRepo repository.IncidentRepository) *IncidentFeedbackHandler {
	return &IncidentFeedbackHandler{incidentRepo: incidentRepo}
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

	if incident.ReporterID == nil || *incident.ReporterID != userID {
		return utils.ErrorResponse(c, fiber.StatusForbidden, "Only the incident reporter can submit feedback")
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
