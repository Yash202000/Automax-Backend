package handlers

import (
	"github.com/automax/backend/internal/models"
	"github.com/automax/backend/internal/services"
	"github.com/automax/backend/pkg/constants"
	"github.com/automax/backend/pkg/utils"
	"github.com/automax/backend/pkg/validation"
	"github.com/gofiber/fiber/v2"
	"github.com/google/uuid"
)

type CallerSentimentHandler struct {
	service services.CallerSentimentService
}

func NewCallerSentimentHandler(service services.CallerSentimentService) *CallerSentimentHandler {
	return &CallerSentimentHandler{
		service: service,
	}
}

func (h *CallerSentimentHandler) Create(c *fiber.Ctx) error {
	var req models.CreateSentimentRequest

	if err := c.BodyParser(&req); err != nil {
		return utils.ErrorResponse(c, fiber.StatusBadRequest, "Invalid request body")
	}

	if validationErrors := validation.ValidateStruct(c.UserContext(), &req); len(validationErrors) != 0 {
		return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{
			"success": false,
			"errors":  validationErrors,
		})
	}

	//Caller (from token)
	CallerID, ok := c.Locals(constants.ContextKeys.UserID).(uuid.UUID)
	if !ok {
		return utils.ErrorResponse(c, fiber.StatusUnauthorized, "User not authenticated")
	}
	sentiment := &models.CallerSentiment{
		ID:        uuid.New(),
		CallerID:  CallerID.String(), // caller
		CalleeID:  req.CalleeID,
		CallUUID:  req.CallUUID,
		Sentiment: req.Sentiment,
		Feedback:  req.Feedback,
	}

	if err := h.service.Create(c.UserContext(), sentiment); err != nil {
		return utils.ErrorResponse(c, fiber.StatusInternalServerError, err.Error())
	}

	return c.JSON(fiber.Map{
		"success": true,
		"message": "sentiment recorded successfully",
	})
}

func (h *CallerSentimentHandler) GetCallerSentiments(c *fiber.Ctx) error {
	callerID := c.Params("caller_id")
	if callerID == "" {
		return utils.ErrorResponse(c, fiber.StatusBadRequest, "caller_id is required")
	}

	res, err := h.service.GetCallerSentiments(c.UserContext(), callerID)
	if err != nil {
		return utils.ErrorResponse(c, fiber.StatusInternalServerError, err.Error())
	}

	return c.JSON(res)
}

func (h *CallerSentimentHandler) GetAllCallerSentiments(c *fiber.Ctx) error {
	res, err := h.service.GetAllCallerSentiments(c.UserContext())
	if err != nil {
		return utils.ErrorResponse(c, fiber.StatusInternalServerError, err.Error())
	}

	return c.JSON(res)
}
