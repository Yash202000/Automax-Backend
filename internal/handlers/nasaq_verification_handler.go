package handlers

import (
	"encoding/json"
	"log"

	"github.com/automax/backend/internal/models"
	"github.com/automax/backend/internal/repository"
	"github.com/automax/backend/internal/services"
	"github.com/automax/backend/pkg/constants"
	"github.com/automax/backend/pkg/i18n"
	"github.com/automax/backend/pkg/utils"
	"github.com/gofiber/fiber/v2"
	"github.com/google/uuid"
)

// NasaqVerificationHandler implements the manual "Verify Nasaq" action on an
// Incident: POST triggers a fresh verification against the Nasaq Integration
// Microservice and stores it as a new immutable snapshot; GET returns the
// latest stored snapshot without triggering a new call. Phase 1 rule: this
// is only ever triggered by a user click, never automatically.
type NasaqVerificationHandler struct {
	incidentRepo     repository.IncidentRepository
	verificationRepo repository.NasaqVerificationRepository
	client           services.NasaqClient
}

func NewNasaqVerificationHandler(incidentRepo repository.IncidentRepository, verificationRepo repository.NasaqVerificationRepository, client services.NasaqClient) *NasaqVerificationHandler {
	return &NasaqVerificationHandler{
		incidentRepo:     incidentRepo,
		verificationRepo: verificationRepo,
		client:           client,
	}
}

// nasaqRawPayload is what's marshaled into NasaqVerification.RawResponse -
// kept as its own type so both Verify and GetLatest can unmarshal it back
// out the same way when building a response.
type nasaqRawPayload struct {
	NasaqData  *services.NasaqData       `json:"nasaqData,omitempty"`
	Candidates []services.NasaqCandidate `json:"candidates,omitempty"`
	Message    string                    `json:"message,omitempty"`
}

// Verify handles POST /api/v1/incidents/:id/nasaq/verify.
func (h *NasaqVerificationHandler) Verify(c *fiber.Ctx) error {
	incidentID, err := uuid.Parse(c.Params("id"))
	if err != nil {
		return utils.ErrorResponse(c, fiber.StatusBadRequest, i18n.T(c.UserContext(), "invalid_incident_id"))
	}

	incident, err := h.incidentRepo.FindByIDWithRelations(c.UserContext(), incidentID)
	if err != nil {
		return utils.ErrorResponse(c, fiber.StatusNotFound, i18n.T(c.UserContext(), "incident_not_found"))
	}

	if incident.Classification == nil || !incident.Classification.IsNasaq {
		return utils.ErrorResponse(c, fiber.StatusBadRequest, i18n.T(c.UserContext(), "incident_not_nasaq_eligible"))
	}
	if incident.Latitude == nil || incident.Longitude == nil {
		return utils.ErrorResponse(c, fiber.StatusBadRequest, i18n.T(c.UserContext(), "incident_missing_coordinates"))
	}

	var requestedBy *uuid.UUID
	if userID, ok := c.Locals(constants.ContextKeys.UserID).(uuid.UUID); ok {
		requestedBy = &userID
	}

	snapshot := &models.NasaqVerification{
		IncidentID:       incident.ID,
		ClassificationID: *incident.ClassificationID,
		RequestedByID:    requestedBy,
	}

	// pointX/pointY per the Nasaq Integration Guide are longitude/latitude
	// respectively.
	result, callErr := h.client.Verify(c.UserContext(), incident.IncidentNumber, *incident.Longitude, *incident.Latitude)
	if callErr != nil {
		log.Printf("nasaq verify: incident %s: %v", incident.IncidentNumber, callErr)
		snapshot.Status = models.NasaqVerificationError
		snapshot.ErrorMessage = callErr.Error()
		if err := h.verificationRepo.Create(c.UserContext(), snapshot); err != nil {
			log.Printf("nasaq verify: failed to store error snapshot for incident %s: %v", incident.IncidentNumber, err)
		}
		// A failed Nasaq call is a verification outcome (ERROR), not an
		// Incident API failure - respond 200 so the Incident Details page
		// can show it inline and offer retry, same as NO_MATCH/REVIEW_REQUIRED.
		return utils.SuccessResponse(c, fiber.StatusOK, i18n.T(c.UserContext(), "nasaq_verification_completed"), toNasaqVerificationResponse(snapshot))
	}

	snapshot.Status = models.NasaqVerificationStatus(result.VerificationStatus)
	if result.PermitNumber != 0 {
		snapshot.PermitNumber = &result.PermitNumber
	}
	snapshot.Distance = result.Distance
	snapshot.ResponseID = result.ResponseID

	raw, err := json.Marshal(nasaqRawPayload{NasaqData: result.NasaqData, Candidates: result.Candidates, Message: result.Message})
	if err != nil {
		log.Printf("nasaq verify: failed to marshal raw payload for incident %s: %v", incident.IncidentNumber, err)
	} else {
		snapshot.RawResponse = raw
	}

	if err := h.verificationRepo.Create(c.UserContext(), snapshot); err != nil {
		return utils.InternalErrorResponse(c, err, i18n.T(c.UserContext(), "internal_server_error"))
	}

	return utils.SuccessResponse(c, fiber.StatusOK, i18n.T(c.UserContext(), "nasaq_verification_completed"), toNasaqVerificationResponse(snapshot))
}

// GetLatest handles GET /api/v1/incidents/:id/nasaq/verify - returns the most
// recent stored verification for the incident without triggering a new call,
// so the Incident Details page can show the last known result on load.
func (h *NasaqVerificationHandler) GetLatest(c *fiber.Ctx) error {
	incidentID, err := uuid.Parse(c.Params("id"))
	if err != nil {
		return utils.ErrorResponse(c, fiber.StatusBadRequest, i18n.T(c.UserContext(), "invalid_incident_id"))
	}

	latest, err := h.verificationRepo.GetLatestByIncident(c.UserContext(), incidentID)
	if err != nil {
		return utils.InternalErrorResponse(c, err, i18n.T(c.UserContext(), "internal_server_error"))
	}
	if latest == nil {
		return utils.SuccessResponse(c, fiber.StatusOK, i18n.T(c.UserContext(), "nasaq_verification_retrieved"), nil)
	}

	return utils.SuccessResponse(c, fiber.StatusOK, i18n.T(c.UserContext(), "nasaq_verification_retrieved"), toNasaqVerificationResponse(latest))
}

func toNasaqVerificationResponse(v *models.NasaqVerification) fiber.Map {
	var payload nasaqRawPayload
	if len(v.RawResponse) > 0 {
		if err := json.Unmarshal(v.RawResponse, &payload); err != nil {
			log.Printf("nasaq verify: failed to unmarshal stored payload for snapshot %s: %v", v.ID, err)
		}
	}

	return fiber.Map{
		"id":           v.ID,
		"incidentId":   v.IncidentID,
		"status":       v.Status,
		"permitNumber": v.PermitNumber,
		"distance":     v.Distance,
		"responseId":   v.ResponseID,
		"retrievedAt":  v.CreatedAt,
		"message":      payload.Message,
		"nasaqData":    payload.NasaqData,
		"candidates":   payload.Candidates,
		"errorMessage": v.ErrorMessage,
	}
}
