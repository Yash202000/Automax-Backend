package handlers

import (
	"log"

	"github.com/automax/backend/internal/services"
	"github.com/automax/backend/pkg/i18n"
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
	log.Printf("[GIS-WRAPPER] request %s %s body=%s", c.Method(), c.OriginalURL(), utils.SanitizeErrorText(string(c.Body())))
	var req gisIdentifyRequest
	if err := c.BodyParser(&req); err != nil {
		return utils.ErrorResponse(c, fiber.StatusBadRequest, i18n.T(c.UserContext(), "invalid_request_body"))
	}
	if req.X == 0 || req.Y == 0 {
		return utils.ErrorResponse(c, fiber.StatusBadRequest, i18n.T(c.UserContext(), "x_y_coordinates_required"))
	}

	result, err := h.svc.Identify(c.UserContext(), req.X, req.Y)
	if err != nil {
		log.Printf("[GIS-WRAPPER] response %s %s status=500 error=%s", c.Method(), c.OriginalURL(), utils.SanitizeErrorText(err.Error()))
		return utils.ErrorResponse(c, fiber.StatusInternalServerError, err.Error())
	}

	log.Printf("[GIS-WRAPPER] response %s %s status=200 body=%s", c.Method(), c.OriginalURL(), utils.SanitizeErrorText(string(result)))
	c.Set("Content-Type", "application/json")
	return c.Send(result)
}
