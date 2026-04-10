package handlers

import (
	"errors"

	"github.com/automax/backend/internal/models"
	"github.com/automax/backend/internal/repository"
	"github.com/automax/backend/internal/services"
	"github.com/automax/backend/pkg/constants"
	"github.com/automax/backend/pkg/utils"
	"github.com/gofiber/fiber/v2"
	"github.com/google/uuid"
	"gorm.io/gorm"
)

type AIQualityFeedbackHandler struct {
	repo        repository.AIQualityFeedbackRepository
	incidentSvc services.IncidentService
	userRepo    repository.UserRepository
}

func NewAIQualityFeedbackHandler(
	repo repository.AIQualityFeedbackRepository,
	incidentSvc services.IncidentService,
	userRepo repository.UserRepository,
) *AIQualityFeedbackHandler {
	return &AIQualityFeedbackHandler{
		repo:        repo,
		incidentSvc: incidentSvc,
		userRepo:    userRepo,
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

// ReopenIncident executes the first available transition for an incident.
// POST /api/v1/incidents/:id/reopen
func (h *AIQualityFeedbackHandler) ReopenIncident(c *fiber.Ctx) error {
	incidentID, err := uuid.Parse(c.Params("id"))
	if err != nil {
		return utils.ErrorResponse(c, fiber.StatusBadRequest, "Invalid incident ID")
	}

	userID, ok := c.Locals(constants.ContextKeys.UserID).(uuid.UUID)
	if !ok {
		return utils.ErrorResponse(c, fiber.StatusUnauthorized, "User not authenticated")
	}

	// Resolve caller's role IDs
	roles, err := h.userRepo.GetUserRoles(c.UserContext(), userID)
	if err != nil {
		roles = nil
	}
	roleIDs := make([]uuid.UUID, len(roles))
	for i, r := range roles {
		roleIDs[i] = r.ID
	}

	// Get available transitions
	transitions, err := h.incidentSvc.GetAvailableTransitions(c.UserContext(), incidentID, roleIDs)
	if err != nil {
		return utils.ErrorResponse(c, fiber.StatusBadRequest, "Failed to get available transitions: "+err.Error())
	}

	// Pick the first executable transition
	var targetTransitionID string
	for _, t := range transitions {
		if t.CanExecute {
			targetTransitionID = t.Transition.ID.String()
			break
		}
	}
	if targetTransitionID == "" {
		return utils.ErrorResponse(c, fiber.StatusBadRequest, "No available transitions for this incident")
	}

	req := &models.IncidentTransitionRequest{
		TransitionID: targetTransitionID,
		Comment:      "Reopened via Quality Audit",
	}

	incident, err := h.incidentSvc.ExecuteTransition(c.UserContext(), incidentID, req, userID, roleIDs)
	if err != nil {
		return utils.ErrorResponse(c, fiber.StatusBadRequest, err.Error())
	}

	return utils.SuccessResponse(c, fiber.StatusOK, "Incident reopened", incident)
}
