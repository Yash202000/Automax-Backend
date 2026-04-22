package handlers

import (
	"io"
	"log"

	"github.com/automax/backend/internal/models"
	"github.com/automax/backend/internal/services"
	"github.com/automax/backend/internal/storage"
	"github.com/automax/backend/pkg/constants"
	"github.com/automax/backend/pkg/utils"
	"github.com/gofiber/fiber/v2"
)

type DocumentHandler struct {
	service       services.DocumentService
	authzService  services.DocumentAuthzService
	workspaceName string
}

// NewDocumentHandler constructs a DocumentHandler. The authzService is required
// — every handler method runs through it to scope access to only the goals the
// caller owns or collaborates on (unless the caller is a super admin / holds
// goals:delete / goals:approve).
func NewDocumentHandler(service services.DocumentService, authzService services.DocumentAuthzService, workspaceName string) *DocumentHandler {
	return &DocumentHandler{
		service:       service,
		authzService:  authzService,
		workspaceName: workspaceName,
	}
}

// ──────────────────────────────────────────────────
// Internal helpers
// ──────────────────────────────────────────────────

func (h *DocumentHandler) getUserEmail(c *fiber.Ctx) string {
	if email, ok := c.Locals(constants.ContextKeys.Email).(string); ok && email != "" {
		return email
	}
	return "system@automax.local"
}

// getUser pulls the *models.User that the auth middleware stashed on the request.
// RequirePermission always sets it; if for any reason it's missing we treat the
// request as unauthenticated rather than silently falling back to unrestricted.
func (h *DocumentHandler) getUser(c *fiber.Ctx) *models.User {
	if user, ok := c.Locals(constants.ContextKeys.User).(*models.User); ok {
		return user
	}
	return nil
}

// resolveScope builds the request-scoped DocumentScope. Returns (nil, error-written)
// when the caller is unauthenticated — the handler must stop processing.
func (h *DocumentHandler) resolveScope(c *fiber.Ctx) (*services.DocumentScope, error) {
	user := h.getUser(c)
	if user == nil {
		_ = utils.ErrorResponse(c, fiber.StatusUnauthorized, "user context missing")
		return nil, fiber.ErrUnauthorized
	}
	scope, err := h.authzService.ResolveDocumentScope(c.UserContext(), user)
	if err != nil {
		_ = utils.ErrorResponse(c, fiber.StatusInternalServerError, "failed to resolve document scope")
		return nil, err
	}
	return scope, nil
}

// enforceNode runs CheckFolderAccess and writes a 403 response on denial.
// Returns true if the caller may proceed.
func (h *DocumentHandler) enforceNode(c *fiber.Ctx, scope *services.DocumentScope, nodeID string) bool {
	email := h.getUserEmail(c)
	allowed, err := h.authzService.CheckFolderAccess(c.UserContext(), scope, nodeID, email)
	if err != nil {
		log.Printf("[document_handler] CheckFolderAccess error node=%s: %v", nodeID, err)
		_ = utils.ErrorResponse(c, fiber.StatusInternalServerError, "failed to authorize document access")
		return false
	}
	if !allowed {
		_ = utils.ErrorResponse(c, fiber.StatusForbidden, "access denied")
		return false
	}
	return true
}

// ══════════════════════════════════════════════════
// Handlers
// ══════════════════════════════════════════════════

// ListFiles lists files and folders in a workspace/parent folder.
//
// For scoped (non-admin) callers:
//   - parent="" (workspace root) → synthetic list with ONLY the "Goal Management"
//     folder, so viewers cannot enumerate other workspace folders.
//   - parent=<Goal Management UUID> → real list, post-filtered so only goal
//     folders the caller may access are returned.
//   - parent=<other UUID> → standard node access check.
//
// GET /documents/files?parent=<uuid>
func (h *DocumentHandler) ListFiles(c *fiber.Ctx) error {
	scope, err := h.resolveScope(c)
	if err != nil {
		return nil
	}
	parentID := c.Query("parent", "")
	email := h.getUserEmail(c)

	// Unrestricted callers: pass straight through.
	if scope.IsUnrestricted {
		result, err := h.service.ListFiles(c.UserContext(), parentID, email)
		if err != nil {
			return utils.ErrorResponse(c, fiber.StatusInternalServerError, "Failed to list files: "+err.Error())
		}
		return utils.SuccessResponse(c, fiber.StatusOK, "Files listed", result)
	}

	// Scoped caller hitting the workspace root — return a synthetic list with
	// only the Goal Management master folder, so viewers cannot see siblings.
	if parentID == "" {
		gmID, gmErr := h.authzService.GoalManagementFolderID(c.UserContext(), email)
		if gmErr != nil {
			return utils.ErrorResponse(c, fiber.StatusInternalServerError, "Failed to resolve Goal Management folder: "+gmErr.Error())
		}
		synth := &storage.DmsListResult{
			Files: []storage.DmsFile{
				{UUID: gmID, Name: "Goal Management", Type: "folder"},
			},
		}
		return utils.SuccessResponse(c, fiber.StatusOK, "Files listed", synth)
	}

	// Scoped caller hitting the Goal Management folder — pull the real listing
	// but filter it down to goal folders this user may access.
	if parentID == scope.GoalManagementFolderID {
		result, err := h.service.ListFiles(c.UserContext(), parentID, email)
		if err != nil {
			return utils.ErrorResponse(c, fiber.StatusInternalServerError, "Failed to list files: "+err.Error())
		}
		filtered := make([]storage.DmsFile, 0, len(result.Files))
		for _, f := range result.Files {
			if _, ok := scope.AccessibleGoalFolderIDs[f.UUID]; ok {
				filtered = append(filtered, f)
			}
		}
		return utils.SuccessResponse(c, fiber.StatusOK, "Files listed", &storage.DmsListResult{Files: filtered})
	}

	// Scoped caller hitting a deeper node — must live under a goal they can access.
	if !h.enforceNode(c, scope, parentID) {
		return nil
	}
	result, err := h.service.ListFiles(c.UserContext(), parentID, email)
	if err != nil {
		return utils.ErrorResponse(c, fiber.StatusInternalServerError, "Failed to list files: "+err.Error())
	}
	return utils.SuccessResponse(c, fiber.StatusOK, "Files listed", result)
}

// SearchFiles searches for files across the workspace, filtered to goals the
// caller can access for scoped users. Implementation choice: MyDocs' /search
// endpoint does not expose a folder-id filter, so we post-filter results by
// resolving each hit's owning goal via the DB-first / breadcrumb-fallback
// path in FindGoalForDMSNode. This costs up to one MyDocs round-trip per
// result that isn't a direct goal-folder UUID match — acceptable for the
// UI result set sizes (default page size is small) and keeps the MyDocs API
// surface unchanged. If search volume grows, the right next step is a
// workspace-wide tag filter indexed on goal_id (which evidences already tag).
//
// POST /documents/search { "query": "...", "tags": {"key": "value"} }
func (h *DocumentHandler) SearchFiles(c *fiber.Ctx) error {
	scope, err := h.resolveScope(c)
	if err != nil {
		return nil
	}
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

	var raw *storage.DmsSearchResult
	if len(body.Tags) > 0 {
		raw, err = h.service.SearchFilesWithTags(c.UserContext(), body.Query, body.Tags, email)
	} else {
		raw, err = h.service.SearchFiles(c.UserContext(), body.Query, email)
	}
	if err != nil {
		return utils.ErrorResponse(c, fiber.StatusInternalServerError, "Failed to search files: "+err.Error())
	}

	// Unrestricted callers see everything.
	if scope.IsUnrestricted || raw == nil {
		return utils.SuccessResponse(c, fiber.StatusOK, "Search results", raw)
	}

	filtered := make([]storage.DmsFile, 0, len(raw.Files))
	for _, f := range raw.Files {
		// Direct goal-folder hit — always cheap.
		if _, ok := scope.AccessibleGoalFolderIDs[f.UUID]; ok {
			filtered = append(filtered, f)
			continue
		}
		// Skip the master folder and anything above it.
		if f.UUID == scope.GoalManagementFolderID {
			continue
		}
		// Fall back to walking up.
		goal, err := h.authzService.FindGoalForDMSNode(c.UserContext(), f.UUID, email)
		if err != nil {
			log.Printf("[document_handler] SearchFiles filter: resolve %s failed: %v", f.UUID, err)
			continue
		}
		if goal == nil {
			continue
		}
		if _, ok := scope.AccessibleGoalFolderIDs[goal.DocumentaFolderID]; ok {
			filtered = append(filtered, f)
		}
	}
	return utils.SuccessResponse(c, fiber.StatusOK, "Search results", &storage.DmsSearchResult{
		Files: filtered,
		Total: len(filtered),
	})
}

// GetFileInfo returns metadata for a single file.
// GET /documents/files/:id/info
func (h *DocumentHandler) GetFileInfo(c *fiber.Ctx) error {
	scope, err := h.resolveScope(c)
	if err != nil {
		return nil
	}
	fileID := c.Params("id")
	if !h.enforceNode(c, scope, fileID) {
		return nil
	}

	email := h.getUserEmail(c)
	result, err := h.service.GetFileInfo(c.UserContext(), fileID, email)
	if err != nil {
		return utils.ErrorResponse(c, fiber.StatusInternalServerError, "Failed to get file info: "+err.Error())
	}
	return utils.SuccessResponse(c, fiber.StatusOK, "File info", result)
}

// GetFileBreadcrumb returns the folder chain (root → parent) for a file.
// GET /documents/files/:id/breadcrumb
func (h *DocumentHandler) GetFileBreadcrumb(c *fiber.Ctx) error {
	scope, err := h.resolveScope(c)
	if err != nil {
		return nil
	}
	fileID := c.Params("id")
	if !h.enforceNode(c, scope, fileID) {
		return nil
	}

	email := h.getUserEmail(c)
	chain, err := h.service.GetFileBreadcrumb(c.UserContext(), fileID, email)
	if err != nil {
		return utils.ErrorResponse(c, fiber.StatusInternalServerError, "Failed to resolve breadcrumb: "+err.Error())
	}
	if chain == nil {
		chain = []storage.DmsBreadcrumbEntry{}
	}
	return utils.SuccessResponse(c, fiber.StatusOK, "File breadcrumb", fiber.Map{"breadcrumb": chain})
}

// GetPreviewURL returns a preview URL for a file.
// GET /documents/files/:id/preview
func (h *DocumentHandler) GetPreviewURL(c *fiber.Ctx) error {
	scope, err := h.resolveScope(c)
	if err != nil {
		return nil
	}
	fileID := c.Params("id")
	if !h.enforceNode(c, scope, fileID) {
		return nil
	}

	email := h.getUserEmail(c)
	url, err := h.service.GetPreviewURL(c.UserContext(), fileID, email)
	if err != nil {
		return utils.ErrorResponse(c, fiber.StatusInternalServerError, "Failed to get preview URL: "+err.Error())
	}
	return utils.SuccessResponse(c, fiber.StatusOK, "Preview URL", fiber.Map{"url": url})
}

// DownloadFile streams the raw bytes of a file back to the caller. Replaces the
// previous "return a JSON URL that points back to this same endpoint" behaviour
// that caused an infinite loop in the browser.
//
// GET /documents/files/:id/download
func (h *DocumentHandler) DownloadFile(c *fiber.Ctx) error {
	scope, err := h.resolveScope(c)
	if err != nil {
		return nil
	}
	fileID := c.Params("id")
	if !h.enforceNode(c, scope, fileID) {
		return nil
	}

	email := h.getUserEmail(c)
	reader, info, err := h.service.DownloadFile(c.UserContext(), fileID, email)
	if err != nil {
		return utils.ErrorResponse(c, fiber.StatusInternalServerError, "Failed to download file: "+err.Error())
	}

	// Read the full body before returning. Fiber's SendStream runs after the
	// handler returns, so deferring reader.Close() would close the stream before
	// any bytes are copied and the client would see an empty reply. For the
	// typical evidence-file size (MB range), buffering in memory is acceptable.
	payload, readErr := io.ReadAll(reader)
	reader.Close()
	if readErr != nil {
		return utils.ErrorResponse(c, fiber.StatusInternalServerError, "Failed to read file bytes: "+readErr.Error())
	}

	// Pull a human-friendly filename from either the download headers or the
	// file metadata. Fall back to the UUID so we always emit a valid Content-
	// Disposition attachment.
	filename := ""
	if info != nil {
		filename = info.Name
	}
	if filename == "" {
		if fi, fiErr := h.service.GetFileInfo(c.UserContext(), fileID, email); fiErr == nil && fi != nil {
			filename = fi.Name
		}
	}
	if filename == "" {
		filename = fileID
	}

	contentType := "application/octet-stream"
	if info != nil && info.MimeType != "" {
		contentType = info.MimeType
	}

	c.Set("Content-Type", contentType)
	c.Set("Content-Disposition", "attachment; filename=\""+filename+"\"")
	return c.Send(payload)
}

// GetComments returns comments on a file.
// GET /documents/files/:id/comments
func (h *DocumentHandler) GetComments(c *fiber.Ctx) error {
	scope, err := h.resolveScope(c)
	if err != nil {
		return nil
	}
	fileID := c.Params("id")
	if !h.enforceNode(c, scope, fileID) {
		return nil
	}

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
	scope, err := h.resolveScope(c)
	if err != nil {
		return nil
	}
	fileID := c.Params("id")
	if !h.enforceNode(c, scope, fileID) {
		return nil
	}

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
	scope, err := h.resolveScope(c)
	if err != nil {
		return nil
	}
	fileID := c.Params("id")
	if !h.enforceNode(c, scope, fileID) {
		return nil
	}

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
	scope, err := h.resolveScope(c)
	if err != nil {
		return nil
	}
	fileID := c.Params("id")
	if !h.enforceNode(c, scope, fileID) {
		return nil
	}

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
	scope, err := h.resolveScope(c)
	if err != nil {
		return nil
	}
	fileID := c.Params("id")
	if !h.enforceNode(c, scope, fileID) {
		return nil
	}

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
	scope, err := h.resolveScope(c)
	if err != nil {
		return nil
	}
	fileID := c.Params("id")
	if !h.enforceNode(c, scope, fileID) {
		return nil
	}

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
//
// NOTE: version UUIDs aren't goal-folder UUIDs, so we can't CheckFolderAccess
// directly. We resolve the version → its owning node via GetFileInfo would
// require an extra endpoint we don't have, so we defer to the scope's goal-folder
// list by enforcing the scope on the resolved file via the breadcrumb path:
// FindGoalForDMSNode walks ancestors on the node UUID returned by the version
// payload, but we don't have it cheaply here. Simplest correct answer: block
// version download for scoped callers unless they're unrestricted OR the caller
// can prove access through another enforced endpoint (they can't bypass the
// gate on versions listing since that is enforced). In practice the frontend
// always reaches this via ListVersions first, which is scope-checked; a direct
// call with a random version UUID by a scoped user is denied.
func (h *DocumentHandler) DownloadVersion(c *fiber.Ctx) error {
	scope, err := h.resolveScope(c)
	if err != nil {
		return nil
	}
	if !scope.IsUnrestricted {
		return utils.ErrorResponse(c, fiber.StatusForbidden, "access denied")
	}
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
	scope, err := h.resolveScope(c)
	if err != nil {
		return nil
	}
	fileID := c.Params("id")
	if !h.enforceNode(c, scope, fileID) {
		return nil
	}

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
