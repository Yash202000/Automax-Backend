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

// ListVersions returns all versions of a file.
// GET /documents/files/:id/versions
func (h *DocumentHandler) ListVersions(c *fiber.Ctx) error {
	fileID := c.Params("id")
	email := h.getUserEmail(c)

	versions, err := h.service.ListVersions(c.UserContext(), fileID, email)
	if err != nil {
		return utils.ErrorResponse(c, fiber.StatusInternalServerError, "Failed to list versions: "+err.Error())
	}

	return utils.SuccessResponse(c, fiber.StatusOK, "Versions listed", versions)
}

// UploadVersion uploads a new version of a file.
// POST /documents/files/:id/versions (multipart form: file + description)
func (h *DocumentHandler) UploadVersion(c *fiber.Ctx) error {
	fileID := c.Params("id")
	description := c.FormValue("description", "")

	fileHeader, err := c.FormFile("file")
	if err != nil {
		return utils.ErrorResponse(c, fiber.StatusBadRequest, "File is required")
	}

	file, err := fileHeader.Open()
	if err != nil {
		return utils.ErrorResponse(c, fiber.StatusInternalServerError, "Failed to open uploaded file")
	}
	defer file.Close()

	email := h.getUserEmail(c)
	version, err := h.service.UploadVersion(c.UserContext(), fileID, fileHeader.Filename, file, fileHeader.Size, description, email)
	if err != nil {
		return utils.ErrorResponse(c, fiber.StatusInternalServerError, "Failed to upload version: "+err.Error())
	}

	return utils.SuccessResponse(c, fiber.StatusCreated, "Version uploaded", version)
}

// DownloadVersion streams a specific version's content.
// GET /documents/versions/:vid/download
func (h *DocumentHandler) DownloadVersion(c *fiber.Ctx) error {
	versionUUID := c.Params("vid")
	email := h.getUserEmail(c)

	reader, contentType, err := h.service.DownloadVersion(c.UserContext(), versionUUID, email)
	if err != nil {
		return utils.ErrorResponse(c, fiber.StatusInternalServerError, "Failed to download version: "+err.Error())
	}
	defer reader.Close()

	c.Set("Content-Type", contentType)
	c.Set("Content-Disposition", "attachment; filename=\"version-"+versionUUID+"\"")

	return c.SendStream(reader)
}

// RollbackVersion restores a previous version of a file.
// POST /documents/files/:id/versions/rollback { "version_uuid": "..." }
func (h *DocumentHandler) RollbackVersion(c *fiber.Ctx) error {
	fileID := c.Params("id")
	var body struct {
		VersionUUID string `json:"version_uuid"`
	}
	if err := c.BodyParser(&body); err != nil {
		return utils.ErrorResponse(c, fiber.StatusBadRequest, "Invalid request body")
	}
	if body.VersionUUID == "" {
		return utils.ErrorResponse(c, fiber.StatusBadRequest, "version_uuid is required")
	}

	email := h.getUserEmail(c)
	version, err := h.service.RollbackVersion(c.UserContext(), fileID, body.VersionUUID, email)
	if err != nil {
		return utils.ErrorResponse(c, fiber.StatusInternalServerError, "Failed to rollback version: "+err.Error())
	}

	return utils.SuccessResponse(c, fiber.StatusOK, "Version rolled back", version)
}
