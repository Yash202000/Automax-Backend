package handlers

import (
	"fmt"

	"github.com/automax/backend/internal/config"
	"github.com/automax/backend/internal/services"
	"github.com/automax/backend/internal/storage"
	"github.com/automax/backend/pkg/i18n"
	"github.com/automax/backend/pkg/utils"
	"github.com/gofiber/fiber/v2"
)

// KpiDocumentaHandler exposes the "KPI Evidence Folder Configuration"
// endpoints used by the KPI create/edit forms: resolving the chosen root
// folder, browsing folders under it, and creating new ones. Unlike
// KpiEngagementHandler, these endpoints are stateless/category-parameterized
// rather than KPI-id-parameterized — they must work before a KPI is ever
// saved.
type KpiDocumentaHandler struct {
	documentaClient storage.DocumentaClient
	documentaCfg    config.DocumentaConfig
}

func NewKpiDocumentaHandler(documentaClient storage.DocumentaClient, documentaCfg config.DocumentaConfig) *KpiDocumentaHandler {
	return &KpiDocumentaHandler{
		documentaClient: documentaClient,
		documentaCfg:    documentaCfg,
	}
}

// KpiDocumentaAnchorResponse is the resolved (and ensured-to-exist) root
// folder for a chosen category, or a reason it couldn't be resolved.
type KpiDocumentaAnchorResponse struct {
	Resolvable     bool     `json:"resolvable"`
	AnchorFolderID string   `json:"anchor_folder_id"`
	AnchorPath     []string `json:"anchor_path"`
	Reason         string   `json:"reason"`
}

// ResolveAnchor resolves and ensures the Documenta root folder for one of
// the seven fixed KPI master-data categories (entity_type=pillar|enabler|
// objective|initiative|domain|award_criterion|award_sub_criterion) — the
// root folder IS the category itself (e.g. "Pillars"), a free pick
// independent of whichever taxonomy the KPI being configured actually
// belongs to. Always returns 200 — even when not resolvable — since "not
// resolvable yet" is a normal state the frontend renders inline, not a
// request error.
func (h *KpiDocumentaHandler) ResolveAnchor(c *fiber.Ctx) error {
	category := c.Query("entity_type")

	anchor, err := services.ResolveMasterDataCategoryAnchor(
		c.UserContext(), h.documentaClient, h.documentaCfg.WorkspaceName, category,
	)
	if err != nil {
		return utils.ErrorResponse(c, fiber.StatusInternalServerError, i18n.T(c.UserContext(), "failed_to_upload_file"))
	}
	if anchor == nil {
		return utils.SuccessResponse(c, fiber.StatusOK, "", KpiDocumentaAnchorResponse{
			Resolvable: false,
			AnchorPath: []string{},
			Reason:     "Unknown root folder category.",
		})
	}

	return utils.SuccessResponse(c, fiber.StatusOK, "", KpiDocumentaAnchorResponse{
		Resolvable:     true,
		AnchorFolderID: anchor.FolderID,
		AnchorPath:     anchor.Path,
	})
}

// KpiDocumentaFolder is one folder entry returned by ListFolders/CreateFolder.
type KpiDocumentaFolder struct {
	UUID string `json:"uuid"`
	Name string `json:"name"`
	Path string `json:"path"`
}

// GetFolderInfo looks up a single folder's display name and full path by id
// — used by the KPI edit form to show the actual configured Evidence Folder
// path (e.g. "/Pillars/Roads/Q1 Reports") instead of a generic placeholder.
func (h *KpiDocumentaHandler) GetFolderInfo(c *fiber.Ctx) error {
	folderID := c.Params("id")
	if folderID == "" {
		return utils.ErrorResponse(c, fiber.StatusBadRequest, i18n.T(c.UserContext(), "invalid_id"))
	}

	info, err := h.documentaClient.GetFileInfo(c.UserContext(), folderID)
	if err != nil {
		return utils.ErrorResponse(c, fiber.StatusNotFound, i18n.T(c.UserContext(), "not_found"))
	}

	return utils.SuccessResponse(c, fiber.StatusOK, "", KpiDocumentaFolder{
		UUID: info.UUID,
		Name: info.Name,
		Path: info.Path,
	})
}

// isWithinAnchor reports whether parentID is the anchor itself or a
// descendant of it, by walking parentID's breadcrumb (root→...→parent) and
// checking for anchorID among the ancestors. Mirrors
// DocumentAuthzService.FindGoalForDMSNode/CheckFolderAccess's ancestor-walk
// pattern, adapted to a single explicit anchor instead of a DB-backed set.
//
// A breadcrumb lookup failure (parentID doesn't exist, or any other
// upstream error) fails closed — treated as "not within anchor" rather than
// a request error — so a caller-supplied bad or out-of-scope id reliably
// gets a 403, not a 500 that leaks nothing but behaves inconsistently with
// the same-shaped "out of scope" case.
func (h *KpiDocumentaHandler) isWithinAnchor(c *fiber.Ctx, anchorID, parentID string) bool {
	if parentID == "" || parentID == anchorID {
		return true
	}
	chain, err := h.documentaClient.GetFileBreadcrumb(c.UserContext(), parentID)
	if err != nil {
		return false
	}
	for _, entry := range chain {
		if entry.UUID == anchorID {
			return true
		}
	}
	return false
}

// ListFolders lists the sub-folders of parent_id (defaulting to anchor_id),
// after verifying parent_id lies within the anchor's own subtree.
func (h *KpiDocumentaHandler) ListFolders(c *fiber.Ctx) error {
	anchorID := c.Query("anchor_id")
	if anchorID == "" {
		return utils.ErrorResponse(c, fiber.StatusBadRequest, i18n.T(c.UserContext(), "invalid_request_body"))
	}
	parentID := c.Query("parent_id", anchorID)

	if !h.isWithinAnchor(c, anchorID, parentID) {
		return utils.ErrorResponse(c, fiber.StatusForbidden, i18n.T(c.UserContext(), "forbidden"))
	}

	result, err := h.documentaClient.ListFiles(c.UserContext(), h.documentaCfg.WorkspaceName, parentID)
	if err != nil {
		return utils.ErrorResponse(c, fiber.StatusInternalServerError, i18n.T(c.UserContext(), "failed_to_upload_file"))
	}

	folders := make([]KpiDocumentaFolder, 0, len(result.Files))
	for _, f := range result.Files {
		if f.Type != "folder" {
			continue
		}
		folders = append(folders, KpiDocumentaFolder{UUID: f.UUID, Name: f.Name, Path: f.Path})
	}

	return utils.SuccessResponse(c, fiber.StatusOK, "", fiber.Map{"folders": folders})
}

// CreateFolderRequest is the body for POST /kpi/documenta/folders.
type CreateFolderRequest struct {
	AnchorID string `json:"anchor_id" validate:"required"`
	ParentID string `json:"parent_id"`
	Name     string `json:"name" validate:"required"`
}

// CreateFolder creates a new folder under parent_id (defaulting to
// anchor_id), after verifying parent_id lies within the anchor's subtree.
func (h *KpiDocumentaHandler) CreateFolder(c *fiber.Ctx) error {
	var req CreateFolderRequest
	if err := c.BodyParser(&req); err != nil || req.AnchorID == "" || req.Name == "" {
		return utils.ErrorResponse(c, fiber.StatusBadRequest, i18n.T(c.UserContext(), "invalid_request_body"))
	}
	parentID := req.ParentID
	if parentID == "" {
		parentID = req.AnchorID
	}

	if !h.isWithinAnchor(c, req.AnchorID, parentID) {
		return utils.ErrorResponse(c, fiber.StatusForbidden, i18n.T(c.UserContext(), "forbidden"))
	}

	folderID, err := h.documentaClient.CreateFolder(c.UserContext(), h.documentaCfg.WorkspaceName, parentID, req.Name)
	if err != nil {
		return utils.ErrorResponse(c, fiber.StatusInternalServerError, i18n.T(c.UserContext(), "failed_to_upload_file"))
	}

	return utils.SuccessResponse(c, fiber.StatusCreated, "", KpiDocumentaFolder{
		UUID: folderID,
		Name: req.Name,
		Path: fmt.Sprintf("/%s", req.Name),
	})
}
