package handlers

import (
	"github.com/automax/backend/internal/models"
	"github.com/automax/backend/internal/services"
	"github.com/automax/backend/internal/storage"
	"github.com/gofiber/fiber/v2"
	"github.com/google/uuid"
)

type ApplicationLinkHandler struct {
	linkService services.ApplicationLinkService
	storage     *storage.MinIOStorage
}

func NewApplicationLinkHandler(linkService services.ApplicationLinkService, storage *storage.MinIOStorage) *ApplicationLinkHandler {
	return &ApplicationLinkHandler{
		linkService: linkService,
		storage:     storage,
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

	link, err := h.linkService.CreateLink(c.UserContext(), &req)
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

	link, err := h.linkService.GetLink(c.UserContext(), id)
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
	links, err := h.linkService.ListLinks(c.UserContext())
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
	links, err := h.linkService.ListActiveLinks(c.UserContext())
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

	link, err := h.linkService.UpdateLink(c.UserContext(), id, &req)
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

	if err := h.linkService.DeleteLink(c.UserContext(), id); err != nil {
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

// UploadImage uploads a logo image for an application link
func (h *ApplicationLinkHandler) UploadImage(c *fiber.Ctx) error {
	idParam := c.Params("id")
	id, err := uuid.Parse(idParam)
	if err != nil {
		return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{
			"success": false,
			"error":   "Invalid ID format",
		})
	}

	// Verify the link exists
	_, err = h.linkService.GetLink(c.UserContext(), id)
	if err != nil {
		return c.Status(fiber.StatusNotFound).JSON(fiber.Map{
			"success": false,
			"error":   "Application link not found",
		})
	}

	// Get uploaded file
	file, err := c.FormFile("image")
	if err != nil {
		return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{
			"success": false,
			"error":   "No file uploaded",
		})
	}

	// Validate file size (max 5MB)
	if file.Size > 5*1024*1024 {
		return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{
			"success": false,
			"error":   "File size exceeds 5MB limit",
		})
	}

	// Validate file type
	contentType := file.Header.Get("Content-Type")
	if contentType != "image/jpeg" && contentType != "image/png" && contentType != "image/gif" && contentType != "image/webp" && contentType != "image/svg+xml" {
		return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{
			"success": false,
			"error":   "Invalid file type. Only JPEG, PNG, GIF, WebP, and SVG are allowed",
		})
	}

	// Open file
	src, err := file.Open()
	if err != nil {
		return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{
			"success": false,
			"error":   "Failed to open file",
		})
	}
	defer src.Close()

	// Upload to storage
	url, err := h.storage.UploadFile(c.UserContext(), src, file, "application-links")
	if err != nil {
		return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{
			"success": false,
			"error":   "Failed to upload image",
		})
	}

	// Update the link with the image URL
	updateReq := &models.ApplicationLinkUpdateRequest{
		ImageURL: url,
	}

	link, err := h.linkService.UpdateLink(c.UserContext(), id, updateReq)
	if err != nil {
		return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{
			"success": false,
			"error":   "Failed to update application link with image URL",
		})
	}

	return c.JSON(fiber.Map{
		"success": true,
		"message": "Image uploaded successfully",
		"data": fiber.Map{
			"image_url": url,
			"link":      link,
		},
	})
}

// RemoveImage removes the logo image from an application link
func (h *ApplicationLinkHandler) RemoveImage(c *fiber.Ctx) error {
	idParam := c.Params("id")
	id, err := uuid.Parse(idParam)
	if err != nil {
		return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{
			"success": false,
			"error":   "Invalid ID format",
		})
	}

	// Get the link to retrieve the current image path for storage cleanup
	existing, err := h.linkService.GetLink(c.UserContext(), id)
	if err != nil {
		return c.Status(fiber.StatusNotFound).JSON(fiber.Map{
			"success": false,
			"error":   "Application link not found",
		})
	}

	// Best-effort delete from MinIO; don't fail the request if it errors
	if existing.ImageURL != "" {
		_ = h.storage.DeleteFile(c.UserContext(), existing.ImageURL)
	}

	link, err := h.linkService.RemoveImage(c.UserContext(), id)
	if err != nil {
		return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{
			"success": false,
			"error":   "Failed to remove image",
		})
	}

	return c.JSON(fiber.Map{
		"success": true,
		"message": "Logo removed successfully",
		"data":    link,
	})
}
