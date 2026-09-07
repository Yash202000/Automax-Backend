package handlers

import (
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
	result, err := h.syncService.SyncExternalEntities(c.UserContext())
	if err != nil {
		return utils.ErrorResponse(c, fiber.StatusBadGateway, "MOMRA external entity sync failed: "+err.Error())
	}
	return utils.SuccessResponse(c, fiber.StatusOK, "External entity sync completed", result)
}

// SyncExternalEntityClassifications triggers a MOMRA EE-to-classification link sync
// (3.36). Depends on classifications and external entities already being synced.
func (h *MOMRASyncHandler) SyncExternalEntityClassifications(c *fiber.Ctx) error {
	result, err := h.syncService.SyncExternalEntityClassifications(c.UserContext())
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

	classificationResult, err := h.syncService.SyncClassifications(ctx)
	if err != nil {
		return utils.ErrorResponse(c, fiber.StatusBadGateway, "MOMRA classification sync failed: "+err.Error())
	}

	entityResult, err := h.syncService.SyncExternalEntities(ctx)
	if err != nil {
		return utils.ErrorResponse(c, fiber.StatusBadGateway, "MOMRA external entity sync failed: "+err.Error())
	}

	linkResult, err := h.syncService.SyncExternalEntityClassifications(ctx)
	if err != nil {
		return utils.ErrorResponse(c, fiber.StatusBadGateway, "MOMRA external entity classification sync failed: "+err.Error())
	}

	return utils.SuccessResponse(c, fiber.StatusOK, "MOMRA sync completed", fiber.Map{
		"classifications":                 classificationResult,
		"external_entities":               entityResult,
		"external_entity_classifications": linkResult,
	})
}
