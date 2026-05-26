package handlers

import (
	"github.com/automax/backend/internal/services"
	"github.com/automax/backend/pkg/utils"
	"github.com/gofiber/fiber/v2"
)

type GISHandler struct {
	svc services.GISService
}

func NewGISHandler(svc services.GISService) *GISHandler {
	return &GISHandler{svc: svc}
}

type gisIdentifyRequest struct {
	X float64 `json:"x"`
	Y float64 `json:"y"`
}

func (h *GISHandler) Identify(c *fiber.Ctx) error {
	var req gisIdentifyRequest
	if err := c.BodyParser(&req); err != nil {
		return utils.ErrorResponse(c, fiber.StatusBadRequest, "Invalid request body")
	}
	if req.X == 0 || req.Y == 0 {
		return utils.ErrorResponse(c, fiber.StatusBadRequest, "x and y coordinates are required")
	}

	result, err := h.svc.Identify(c.UserContext(), req.X, req.Y)
	if err != nil {
		return utils.ErrorResponse(c, fiber.StatusInternalServerError, err.Error())
	}

	c.Set("Content-Type", "application/json")
	return c.Send(result)
}
