package handlers

import (
	"github.com/automax/backend/internal/services"
	"github.com/automax/backend/pkg/constants"
	"github.com/automax/backend/pkg/utils"
	"github.com/gofiber/fiber/v2"
)

type DocumentHandler struct {
	service services.DocumentService
}

func NewDocumentHandler(service services.DocumentService) *DocumentHandler {
	return &DocumentHandler{service: service}
}

func (h *DocumentHandler) getUserEmail(c *fiber.Ctx) string {
	if email, ok := c.Locals(constants.ContextKeys.Email).(string); ok && email != "" {
		return email
	}
	return "system@automax.local"
}

// ListFiles lists files and folders in a workspace/parent folder.
// GET /documents/files?parent=<uuid>
func (h *DocumentHandler) ListFiles(c *fiber.Ctx) error {
	parentID := c.Query("parent", "")
	email := h.getUserEmail(c)

	result, err := h.service.ListFiles(c.UserContext(), parentID, email)
	if err != nil {
		return utils.ErrorResponse(c, fiber.StatusInternalServerError, "Failed to list files: "+err.Error())
	}

	return utils.SuccessResponse(c, fiber.StatusOK, "Files listed", result)
}

// SearchFiles searches for files across the workspace.
// POST /documents/search { "query": "...", "tags": {"key": "value"} }
func (h *DocumentHandler) SearchFiles(c *fiber.Ctx) error {
	var body struct {
		Query string            `json:"query"`
		Tags  map[string]string `json:"tags,omitempty"`
	}
	if err := c.BodyParser(&body); err != nil {
		return utils.ErrorResponse(c, fiber.StatusBadRequest, "Invalid request body")
	}
	if body.Query == "" && len(body.Tags) == 0 {
		return utils.ErrorResponse(c, fiber.StatusBadRequest, "Query or tags are required")
	}

	email := h.getUserEmail(c)

	if len(body.Tags) > 0 {
		result, err := h.service.SearchFilesWithTags(c.UserContext(), body.Query, body.Tags, email)
		if err != nil {
			return utils.ErrorResponse(c, fiber.StatusInternalServerError, "Failed to search files: "+err.Error())
		}
		return utils.SuccessResponse(c, fiber.StatusOK, "Search results", result)
	}

	result, err := h.service.SearchFiles(c.UserContext(), body.Query, email)
	if err != nil {
		return utils.ErrorResponse(c, fiber.StatusInternalServerError, "Failed to search files: "+err.Error())
	}

	return utils.SuccessResponse(c, fiber.StatusOK, "Search results", result)
}

// GetFileInfo returns metadata for a single file.
// GET /documents/files/:id/info
func (h *DocumentHandler) GetFileInfo(c *fiber.Ctx) error {
	fileID := c.Params("id")
	email := h.getUserEmail(c)

	result, err := h.service.GetFileInfo(c.UserContext(), fileID, email)
	if err != nil {
		return utils.ErrorResponse(c, fiber.StatusInternalServerError, "Failed to get file info: "+err.Error())
	}

	return utils.SuccessResponse(c, fiber.StatusOK, "File info", result)
}

// GetPreviewURL returns a preview URL for a file.
// GET /documents/files/:id/preview
func (h *DocumentHandler) GetPreviewURL(c *fiber.Ctx) error {
	fileID := c.Params("id")
	email := h.getUserEmail(c)

	url, err := h.service.GetPreviewURL(c.UserContext(), fileID, email)
	if err != nil {
		return utils.ErrorResponse(c, fiber.StatusInternalServerError, "Failed to get preview URL: "+err.Error())
	}

	return utils.SuccessResponse(c, fiber.StatusOK, "Preview URL", fiber.Map{"url": url})
}

// GetDownloadURL returns a download URL for a file.
// GET /documents/files/:id/download
func (h *DocumentHandler) GetDownloadURL(c *fiber.Ctx) error {
	fileID := c.Params("id")
	email := h.getUserEmail(c)

	url, err := h.service.GetDownloadURL(c.UserContext(), fileID, email)
	if err != nil {
		return utils.ErrorResponse(c, fiber.StatusInternalServerError, "Failed to get download URL: "+err.Error())
	}

	return utils.SuccessResponse(c, fiber.StatusOK, "Download URL", fiber.Map{"url": url})
}

// GetComments returns comments on a file.
// GET /documents/files/:id/comments
func (h *DocumentHandler) GetComments(c *fiber.Ctx) error {
	fileID := c.Params("id")
	email := h.getUserEmail(c)

	comments, err := h.service.GetComments(c.UserContext(), fileID, email)
	if err != nil {
		return utils.ErrorResponse(c, fiber.StatusInternalServerError, "Failed to get comments: "+err.Error())
	}

	return utils.SuccessResponse(c, fiber.StatusOK, "Comments", comments)
}

// AddComment adds a comment to a file.
// POST /documents/files/:id/comments { "content": "..." }
func (h *DocumentHandler) AddComment(c *fiber.Ctx) error {
	fileID := c.Params("id")
	var body struct {
		Content string `json:"content"`
	}
	if err := c.BodyParser(&body); err != nil {
		return utils.ErrorResponse(c, fiber.StatusBadRequest, "Invalid request body")
	}
	if body.Content == "" {
		return utils.ErrorResponse(c, fiber.StatusBadRequest, "Comment content is required")
	}

	email := h.getUserEmail(c)
	if err := h.service.AddComment(c.UserContext(), fileID, body.Content, email); err != nil {
		return utils.ErrorResponse(c, fiber.StatusInternalServerError, "Failed to add comment: "+err.Error())
	}

	return utils.SuccessResponse(c, fiber.StatusCreated, "Comment added", nil)
}

// GetTags returns the metadata tags on a file.
// GET /documents/files/:id/tags
func (h *DocumentHandler) GetTags(c *fiber.Ctx) error {
	fileID := c.Params("id")
	email := h.getUserEmail(c)

	tags, err := h.service.GetTags(c.UserContext(), fileID, email)
	if err != nil {
		return utils.ErrorResponse(c, fiber.StatusInternalServerError, "Failed to get tags: "+err.Error())
	}

	return utils.SuccessResponse(c, fiber.StatusOK, "Tags", tags)
}

// SetTags sets metadata tags on a file.
// PUT /documents/files/:id/tags { "tags": { "key": "value" } }
func (h *DocumentHandler) SetTags(c *fiber.Ctx) error {
	fileID := c.Params("id")
	var body struct {
		Tags map[string]string `json:"tags"`
	}
	if err := c.BodyParser(&body); err != nil {
		return utils.ErrorResponse(c, fiber.StatusBadRequest, "Invalid request body")
	}

	email := h.getUserEmail(c)
	if err := h.service.SetTags(c.UserContext(), fileID, body.Tags, email); err != nil {
		return utils.ErrorResponse(c, fiber.StatusInternalServerError, "Failed to set tags: "+err.Error())
	}

	return utils.SuccessResponse(c, fiber.StatusOK, "Tags updated", nil)
}
