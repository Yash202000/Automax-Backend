package handlers

import (
	"github.com/automax/backend/internal/models"
	"github.com/automax/backend/internal/services"
	"github.com/gofiber/fiber/v2"
	"github.com/google/uuid"
)

type ApplicationLinkHandler struct {
	linkService services.ApplicationLinkService
}

func NewApplicationLinkHandler(linkService services.ApplicationLinkService) *ApplicationLinkHandler {
	return &ApplicationLinkHandler{
		linkService: linkService,
	}
}

// CreateLink creates a new application link
func (h *ApplicationLinkHandler) CreateLink(c *fiber.Ctx) error {
	var req models.ApplicationLinkCreateRequest
	if err := c.BodyParser(&req); err != nil {
		return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{
			"success": false,
			"error":   "Invalid request body",
		})
	}

	link, err := h.linkService.CreateLink(c.Context(), &req)
	if err != nil {
		return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{
			"success": false,
			"error":   err.Error(),
		})
	}

	return c.Status(fiber.StatusCreated).JSON(fiber.Map{
		"success": true,
		"data":    link,
	})
}

// GetLink retrieves a single application link by ID
func (h *ApplicationLinkHandler) GetLink(c *fiber.Ctx) error {
	idParam := c.Params("id")
	id, err := uuid.Parse(idParam)
	if err != nil {
		return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{
			"success": false,
			"error":   "Invalid ID format",
		})
	}

	link, err := h.linkService.GetLink(c.Context(), id)
	if err != nil {
		return c.Status(fiber.StatusNotFound).JSON(fiber.Map{
			"success": false,
			"error":   "Application link not found",
		})
	}

	return c.JSON(fiber.Map{
		"success": true,
		"data":    link,
	})
}

// ListLinks lists all application links
func (h *ApplicationLinkHandler) ListLinks(c *fiber.Ctx) error {
	links, err := h.linkService.ListLinks(c.Context())
	if err != nil {
		return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{
			"success": false,
			"error":   err.Error(),
		})
	}

	return c.JSON(fiber.Map{
		"success": true,
		"data":    links,
	})
}

// ListActiveLinks lists only active application links (for dashboard display)
func (h *ApplicationLinkHandler) ListActiveLinks(c *fiber.Ctx) error {
	links, err := h.linkService.ListActiveLinks(c.Context())
	if err != nil {
		return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{
			"success": false,
			"error":   err.Error(),
		})
	}

	return c.JSON(fiber.Map{
		"success": true,
		"data":    links,
	})
}

// UpdateLink updates an existing application link
func (h *ApplicationLinkHandler) UpdateLink(c *fiber.Ctx) error {
	idParam := c.Params("id")
	id, err := uuid.Parse(idParam)
	if err != nil {
		return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{
			"success": false,
			"error":   "Invalid ID format",
		})
	}

	var req models.ApplicationLinkUpdateRequest
	if err := c.BodyParser(&req); err != nil {
		return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{
			"success": false,
			"error":   "Invalid request body",
		})
	}

	link, err := h.linkService.UpdateLink(c.Context(), id, &req)
	if err != nil {
		return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{
			"success": false,
			"error":   err.Error(),
		})
	}

	return c.JSON(fiber.Map{
		"success": true,
		"data":    link,
	})
}

// DeleteLink deletes an application link
func (h *ApplicationLinkHandler) DeleteLink(c *fiber.Ctx) error {
	idParam := c.Params("id")
	id, err := uuid.Parse(idParam)
	if err != nil {
		return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{
			"success": false,
			"error":   "Invalid ID format",
		})
	}

	if err := h.linkService.DeleteLink(c.Context(), id); err != nil {
		return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{
			"success": false,
			"error":   err.Error(),
		})
	}

	return c.JSON(fiber.Map{
		"success": true,
		"message": "Application link deleted successfully",
	})
}
