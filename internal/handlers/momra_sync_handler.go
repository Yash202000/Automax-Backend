package handlers

import (
	"strings"

	"github.com/automax/backend/internal/services"
	"github.com/automax/backend/pkg/utils"
	"github.com/gofiber/fiber/v2"
)

// MOMRASyncHandler exposes admin-triggered sync endpoints for MOMRA's classification
// and external-entity master data (docs/MOMRA_Outbound_Integration_Spec_v1.0.md §4-6).
// Manual triggers today; a scheduled refresh can call the same service methods later.
type MOMRASyncHandler struct {
	syncService services.MOMRASyncService
}

func NewMOMRASyncHandler(syncService services.MOMRASyncService) *MOMRASyncHandler {
	return &MOMRASyncHandler{syncService: syncService}
}

// SyncClassifications triggers a MOMRA Main/Sub/Special classification sync (3.21-3.23).
func (h *MOMRASyncHandler) SyncClassifications(c *fiber.Ctx) error {
	result, err := h.syncService.SyncClassifications(c.UserContext())
	if err != nil {
		return utils.ErrorResponse(c, fiber.StatusBadGateway, "MOMRA classification sync failed: "+err.Error())
	}
	return utils.SuccessResponse(c, fiber.StatusOK, "Classification sync completed", result)
}

// SyncExternalEntities triggers a MOMRA External Entities master sync (3.35).
func (h *MOMRASyncHandler) SyncExternalEntities(c *fiber.Ctx) error {
	req, err := parseMomraSyncRequest(c)
	if err != nil {
		return utils.ErrorResponse(c, fiber.StatusBadRequest, "Invalid request body")
	}
	result, err := h.syncService.SyncExternalEntities(c.UserContext(), req.RequestID)
	if err != nil {
		return utils.ErrorResponse(c, fiber.StatusBadGateway, "MOMRA external entity sync failed: "+err.Error())
	}
	return utils.SuccessResponse(c, fiber.StatusOK, "External entity sync completed", result)
}

// momraSyncRequest is the optional body of the sync endpoints. Empty request
// IDs mean the service default.
//   - RequestID: the CRM940 requestid for the single-sync endpoints
//     (GetExternalEntities / GetExternalEntitiesClassifications).
//   - ExternalEntitiesRequestID / EEClassificationsRequestID: SyncAll's
//     per-step requestids, since each step has its own.
type momraSyncRequest struct {
	RequestID                  string `json:"request_id"`
	ExternalEntitiesRequestID  string `json:"external_entities_request_id"`
	EEClassificationsRequestID string `json:"external_entity_classifications_request_id"`
}

// parseMomraSyncRequest reads the optional JSON body; an empty body is fine.
func parseMomraSyncRequest(c *fiber.Ctx) (momraSyncRequest, error) {
	var req momraSyncRequest
	if len(c.Body()) == 0 {
		return req, nil
	}
	if err := c.BodyParser(&req); err != nil {
		return req, err
	}
	req.RequestID = strings.TrimSpace(req.RequestID)
	req.ExternalEntitiesRequestID = strings.TrimSpace(req.ExternalEntitiesRequestID)
	req.EEClassificationsRequestID = strings.TrimSpace(req.EEClassificationsRequestID)
	return req, nil
}

// SyncExternalEntityClassifications triggers a MOMRA EE-to-classification link sync
// (3.36). Depends on classifications and external entities already being synced.
func (h *MOMRASyncHandler) SyncExternalEntityClassifications(c *fiber.Ctx) error {
	req, err := parseMomraSyncRequest(c)
	if err != nil {
		return utils.ErrorResponse(c, fiber.StatusBadRequest, "Invalid request body")
	}
	result, err := h.syncService.SyncExternalEntityClassifications(c.UserContext(), req.RequestID)
	if err != nil {
		return utils.ErrorResponse(c, fiber.StatusBadGateway, "MOMRA external entity classification sync failed: "+err.Error())
	}
	return utils.SuccessResponse(c, fiber.StatusOK, "External entity classification sync completed", result)
}

// SyncAll runs all three syncs in dependency order (classifications, then external
// entities, then the links between them) — the sequence the effort estimate and
// integration spec both call out as required.
func (h *MOMRASyncHandler) SyncAll(c *fiber.Ctx) error {
	ctx := c.UserContext()
	req, err := parseMomraSyncRequest(c)
	if err != nil {
		return utils.ErrorResponse(c, fiber.StatusBadRequest, "Invalid request body")
	}

	classificationResult, err := h.syncService.SyncClassifications(ctx)
	if err != nil {
		return utils.ErrorResponse(c, fiber.StatusBadGateway, "MOMRA classification sync failed: "+err.Error())
	}

	entityResult, err := h.syncService.SyncExternalEntities(ctx, req.ExternalEntitiesRequestID)
	if err != nil {
		return utils.ErrorResponse(c, fiber.StatusBadGateway, "MOMRA external entity sync failed: "+err.Error())
	}

	linkResult, err := h.syncService.SyncExternalEntityClassifications(ctx, req.EEClassificationsRequestID)
	if err != nil {
		return utils.ErrorResponse(c, fiber.StatusBadGateway, "MOMRA external entity classification sync failed: "+err.Error())
	}

	return utils.SuccessResponse(c, fiber.StatusOK, "MOMRA sync completed", fiber.Map{
		"classifications":                 classificationResult,
		"external_entities":               entityResult,
		"external_entity_classifications": linkResult,
	})
}
