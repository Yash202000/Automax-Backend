package handlers

import (
	"errors"

	"github.com/automax/backend/internal/repository"
	"github.com/automax/backend/pkg/utils"
	"github.com/gofiber/fiber/v2"
	"github.com/google/uuid"
	"gorm.io/gorm"
)

type AIQualityFeedbackHandler struct {
	repo repository.AIQualityFeedbackRepository
}

func NewAIQualityFeedbackHandler(
	repo repository.AIQualityFeedbackRepository,
) *AIQualityFeedbackHandler {
	return &AIQualityFeedbackHandler{
		repo: repo,
	}
}

// GetAll returns all AI quality feedback records with incident info.
// GET /api/v1/ai-quality
func (h *AIQualityFeedbackHandler) GetAll(c *fiber.Ctx) error {
	feedbacks, err := h.repo.ListAllWithIncident(c.UserContext())
	if err != nil {
		return utils.ErrorResponse(c, fiber.StatusInternalServerError, "Failed to fetch AI quality feedback")
	}
	return utils.SuccessResponse(c, fiber.StatusOK, "AI quality feedback retrieved", feedbacks)
}

// GetByIncident returns the AI quality feedback record for a given incident.
// GET /api/v1/incidents/:id/ai-quality
func (h *AIQualityFeedbackHandler) GetByIncident(c *fiber.Ctx) error {
	incidentID, err := uuid.Parse(c.Params("id"))
	if err != nil {
		return utils.ErrorResponse(c, fiber.StatusBadRequest, "Invalid incident ID")
	}

	feedback, err := h.repo.FindByIncidentID(c.UserContext(), incidentID)
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return utils.ErrorResponse(c, fiber.StatusNotFound, "No AI quality feedback found for this incident")
		}
		return utils.ErrorResponse(c, fiber.StatusInternalServerError, "Failed to fetch AI quality feedback")
	}

	return utils.SuccessResponse(c, fiber.StatusOK, "AI quality feedback retrieved", feedback)
}
