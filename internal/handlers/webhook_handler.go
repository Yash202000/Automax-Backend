package handlers

import (
	"log"
	"strings"
	"time"

	"github.com/automax/backend/internal/models"
	"github.com/automax/backend/internal/services"
	"github.com/automax/backend/pkg/i18n"
	"github.com/automax/backend/pkg/utils"
	"github.com/gofiber/fiber/v2"
	"github.com/google/uuid"
)

type WebhookHandler struct {
	integrationSvc services.IntegrationService
	incidentSvc    services.IncidentService
}

func NewWebhookHandler(integrationSvc services.IntegrationService, incidentSvc services.IncidentService) *WebhookHandler {
	return &WebhookHandler{integrationSvc: integrationSvc, incidentSvc: incidentSvc}
}

// HandleAutomaxCallback processes inbound callbacks from remote Automax systems.
// Route: POST /webhooks/automax-callback (public, auth via shared secret in Authorization header)
func (h *WebhookHandler) HandleAutomaxCallback(c *fiber.Ctx) error {
	// Extract bearer token
	authHeader := c.Get("Authorization")
	secret := strings.TrimPrefix(authHeader, "Bearer ")
	if secret == authHeader || secret == "" {
		return utils.ErrorResponse(c, fiber.StatusUnauthorized, i18n.T(c.UserContext(), "authorization_bearer_required"))
	}

	var payload models.AutomaxCallbackPayload
	if err := c.BodyParser(&payload); err != nil {
		return utils.ErrorResponse(c, fiber.StatusBadRequest, i18n.T(c.UserContext(), "invalid_payload"))
	}
	if payload.SourceIncidentID == "" {
		return utils.ErrorResponse(c, fiber.StatusBadRequest, i18n.T(c.UserContext(), "source_incident_id_required"))
	}
	if payload.Action == "" && payload.RemoteStateCode == "" {
		return utils.ErrorResponse(c, fiber.StatusBadRequest, i18n.T(c.UserContext(), "action_or_remote_state_required"))
	}

	incidentID, err := uuid.Parse(payload.SourceIncidentID)
	if err != nil {
		return utils.ErrorResponse(c, fiber.StatusBadRequest, i18n.T(c.UserContext(), "invalid_source_incident_id"))
	}

	// Validate secret and resolve the transition to execute
	transitionID, err := h.integrationSvc.FindTransitionForCallback(c.UserContext(), secret, &payload)
	if err != nil {
		log.Printf("[webhook] callback rejected: %v", err)
		return utils.ErrorResponse(c, fiber.StatusUnauthorized, i18n.T(c.UserContext(), "no_matching_webhook_config"))
	}

	// Execute the transition as system (no user context)
	req := &models.IncidentTransitionRequest{TransitionID: transitionID.String()}
	if _, err = h.incidentSvc.ExecuteTransition(c.UserContext(), incidentID, req, uuid.Nil, []uuid.UUID{}); err != nil {
		log.Printf("[webhook] failed to execute transition %s on incident %s: %v", transitionID, incidentID, err)
		return utils.ErrorResponse(c, fiber.StatusUnprocessableEntity, i18n.T(c.UserContext(), "failed_to_execute_transition"))
	}

	// Close open outbound bridges when remote system signals closure
	if payload.Action == "close" {
		if closeErr := h.integrationSvc.CloseBridgesForIncident(c.UserContext(), incidentID); closeErr != nil {
			log.Printf("[webhook] failed to close bridges for incident %s: %v", incidentID, closeErr)
		}
	}

	// Record an inbound bridge entry for audit trail
	remoteName := payload.RemoteSystemName
	if remoteName == "" {
		remoteName = "Remote Automax"
	}
	now := time.Now()
	bridge := &models.IncidentBridge{
		LocalIncidentID:      incidentID,
		RemoteSystemName:     remoteName,
		RemoteIncidentNumber: payload.RemoteRef,
		Direction:            "inbound",
		Status:               "open",
	}
	if payload.Action == "close" {
		bridge.Status = "closed"
		bridge.ClosedAt = &now
	}
	if createErr := h.integrationSvc.CreateBridge(c.UserContext(), bridge); createErr != nil {
		log.Printf("[webhook] failed to record inbound bridge for incident %s: %v", incidentID, createErr)
	}

	return utils.SuccessResponse(c, fiber.StatusOK, i18n.T(c.UserContext(), "callback_processed"), nil)
}
