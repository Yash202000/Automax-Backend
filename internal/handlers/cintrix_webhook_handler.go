package handlers

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"log"
	"strings"
	"time"

	"github.com/automax/backend/internal/models"
	"github.com/automax/backend/internal/repository"
	"github.com/automax/backend/pkg/i18n"
	"github.com/automax/backend/pkg/utils"
	"github.com/gofiber/fiber/v2"
	"github.com/google/uuid"
	"gorm.io/datatypes"
	"gorm.io/gorm"
)

// CintrixWebhookHandler ingests signed call-event webhooks from Cintrix (the
// contact-center/CTI system) and persists them as CallLog/CallParticipant rows.
type CintrixWebhookHandler struct {
	webhookSecret string
	callLogRepo   repository.CallLogRepository
	userRepo      repository.UserRepository
}

func NewCintrixWebhookHandler(webhookSecret string, callLogRepo repository.CallLogRepository, userRepo repository.UserRepository) *CintrixWebhookHandler {
	return &CintrixWebhookHandler{
		webhookSecret: webhookSecret,
		callLogRepo:   callLogRepo,
		userRepo:      userRepo,
	}
}

// cintrixCallEventPayload mirrors the exact JSON the Cintrix notifier sends
// (and signs) — see docs/superpowers/specs §6.2 in the Cintrix repo.
type cintrixCallEventPayload struct {
	Event        string  `json:"event"`
	CallUuid     string  `json:"call_uuid"`
	Direction    string  `json:"direction"`
	CallerNumber string  `json:"caller_number"`
	DID          string  `json:"did"`
	Queue        *string `json:"queue"`
	AgentEmail   *string `json:"agent_email"`
	Contact      *struct {
		ID   int    `json:"id"`
		Name string `json:"name"`
	} `json:"contact"`
	StartedAt       *time.Time `json:"started_at"`
	AnsweredAt      *time.Time `json:"answered_at"`
	EndedAt         *time.Time `json:"ended_at"`
	DurationSeconds int        `json:"duration_seconds"`
	Outcome         string     `json:"outcome"`
	RecordingURL    *string    `json:"recording_url"`
}

// HandleCallEvent processes inbound call.answered / call.ended webhooks from Cintrix.
// Route: POST /webhooks/cintrix/call-event (public, auth via shared secret + HMAC signature).
func (h *CintrixWebhookHandler) HandleCallEvent(c *fiber.Ctx) error {
	if h.webhookSecret == "" {
		return utils.ErrorResponse(c, fiber.StatusServiceUnavailable, i18n.T(c.UserContext(), "cintrix_integration_not_configured"))
	}

	// Extract bearer token.
	authHeader := c.Get("Authorization")
	token := strings.TrimPrefix(authHeader, "Bearer ")
	if token == authHeader || token == "" {
		return utils.ErrorResponse(c, fiber.StatusUnauthorized, i18n.T(c.UserContext(), "authorization_bearer_required"))
	}
	if !hmac.Equal([]byte(token), []byte(h.webhookSecret)) {
		return utils.ErrorResponse(c, fiber.StatusUnauthorized, i18n.T(c.UserContext(), "authorization_bearer_required"))
	}

	// Verify HMAC-SHA256(secret, raw body) against X-Cintrix-Signature, over the
	// exact raw bytes — must be read/verified before any body parsing.
	rawBody := c.Body()
	mac := hmac.New(sha256.New, []byte(h.webhookSecret))
	mac.Write(rawBody)
	expectedSig := hex.EncodeToString(mac.Sum(nil))
	providedSig := c.Get("X-Cintrix-Signature")
	if providedSig == "" || !hmac.Equal([]byte(expectedSig), []byte(providedSig)) {
		return utils.ErrorResponse(c, fiber.StatusUnauthorized, i18n.T(c.UserContext(), "invalid_signature"))
	}

	var payload cintrixCallEventPayload
	if err := json.Unmarshal(rawBody, &payload); err != nil {
		return utils.ErrorResponse(c, fiber.StatusBadRequest, i18n.T(c.UserContext(), "invalid_payload"))
	}
	if payload.CallUuid == "" || payload.Event == "" {
		return utils.ErrorResponse(c, fiber.StatusBadRequest, i18n.T(c.UserContext(), "call_uuid_and_event_required"))
	}

	ctx := c.UserContext()

	// Resolve the agent participant's identifier (extension preferred, else phone)
	// by email lookup. Missing/unresolvable agent is not an error — many events
	// (e.g. missed calls) never carry one.
	var agentIdentifier string
	var agentUserID *uuid.UUID
	if payload.AgentEmail != nil && *payload.AgentEmail != "" {
		if user, err := h.userRepo.FindByEmail(ctx, *payload.AgentEmail); err == nil && user != nil {
			if user.Extension != "" {
				agentIdentifier = user.Extension
			} else {
				agentIdentifier = user.Phone
			}
			agentUserID = &user.ID
		} else if err != nil && err != gorm.ErrRecordNotFound {
			log.Printf("[cintrix-webhook] agent lookup failed for %s: %v", *payload.AgentEmail, err)
		}
	}

	// Also attribute the DIALED target (payload.DID) when it resolves to an
	// internal extension — so an UNANSWERED agent-to-agent call still records
	// the callee as a participant and appears in *their* call history. An
	// unanswered call carries no agent_email, so without this the callee has no
	// participant row and never sees a call they missed. For IVR/queue calls the
	// DID is a service/queue number that resolves to no user, so this is a no-op
	// there (an unanswered IVR call has no per-agent significance — correct).
	var dialedIdentifier string
	if payload.DID != "" {
		if du, derr := h.userRepo.FindByExtension(ctx, payload.DID); derr == nil && du != nil {
			if du.Extension != "" {
				dialedIdentifier = du.Extension
			} else {
				dialedIdentifier = du.Phone
			}
		}
	}

	meta := datatypes.JSON(append([]byte{}, rawBody...))

	// StartAt prefers answered_at, falling back to ended_at (covers missed calls
	// that arrive only via call.ended, with no answered_at at all).
	startAt := payload.AnsweredAt
	if startAt == nil {
		startAt = payload.EndedAt
	}

	callerName := ""
	if payload.Contact != nil {
		callerName = payload.Contact.Name
	}

	existing, err := h.callLogRepo.FindByCallUUID(ctx, payload.CallUuid)
	if err != nil && err != gorm.ErrRecordNotFound {
		log.Printf("[cintrix-webhook] lookup failed for call_uuid %s: %v", payload.CallUuid, err)
		return utils.ErrorResponse(c, fiber.StatusInternalServerError, i18n.T(ctx, "internal_server_error"))
	}

	if existing == nil {
		callLog := &models.CallLog{
			// CreatedBy stays nil when the agent doesn't resolve to a user
			// (e.g. missed calls) — this is a machine-ingested row, not the
			// work of a real actor, so NULL is the truthful value.
			CreatedBy: agentUserID,
			CallUuid:  payload.CallUuid,
			CallType:  "cintrix",
			Status:    normalizeOutcome(payload.Outcome),
			StartAt:   startAt,
			EndAt:     payload.EndedAt,
			Meta:      meta,
		}

		participants := make([]*models.CallParticipant, 0, 2)
		if payload.CallerNumber != "" {
			participants = append(participants, &models.CallParticipant{
				PhoneNumber: payload.CallerNumber,
				Role:        "initiator",
				DisplayName: callerName,
			})
		}
		if agentIdentifier != "" {
			rp := &models.CallParticipant{PhoneNumber: agentIdentifier, Role: "recipient"}
			if payload.Event == "call.answered" && payload.AnsweredAt != nil {
				rp.JoinStatus = "joined"
				rp.JoinedAt = payload.AnsweredAt
			}
			participants = append(participants, rp)
		}
		// Dialed callee for an unanswered/direct call — dedup against the caller
		// and the (already-added) answered agent.
		if dialedIdentifier != "" && dialedIdentifier != agentIdentifier && dialedIdentifier != payload.CallerNumber {
			participants = append(participants, &models.CallParticipant{PhoneNumber: dialedIdentifier, Role: "recipient"})
		}

		if err := h.callLogRepo.CreateWithParticipants(ctx, callLog, participants); err != nil {
			log.Printf("[cintrix-webhook] failed to create call log for %s: %v", payload.CallUuid, err)
			return utils.ErrorResponse(c, fiber.StatusInternalServerError, i18n.T(ctx, "internal_server_error"))
		}

		return utils.SuccessResponse(c, fiber.StatusOK, i18n.T(ctx, "cintrix_call_event_processed"), nil)
	}

	// Existing call log — update in place. Retries of the same event resend the
	// same values, so this is idempotent; call.ended completes a log started by
	// call.answered (or creates the terminal state directly for missed calls).
	fields := map[string]interface{}{
		"meta": meta,
	}
	if existing.CreatedBy == nil && agentUserID != nil {
		fields["created_by"] = agentUserID
	}
	if payload.Outcome != "" {
		fields["status"] = normalizeOutcome(payload.Outcome)
	}
	if payload.EndedAt != nil {
		fields["end_at"] = payload.EndedAt
	}
	if existing.StartAt == nil && startAt != nil {
		fields["start_at"] = startAt
	}

	if err := h.callLogRepo.UpdateByField(ctx, existing.ID, fields); err != nil {
		log.Printf("[cintrix-webhook] failed to update call log %s: %v", payload.CallUuid, err)
		return utils.ErrorResponse(c, fiber.StatusInternalServerError, i18n.T(ctx, "internal_server_error"))
	}

	// Ensure participants exist without duplicating them on retries (check
	// before insert — matched by call_log_id + phone_number, which is stable
	// per call across retries/events).
	if payload.CallerNumber != "" {
		if _, err := h.callLogRepo.FindParticipant(ctx, existing.ID, payload.CallerNumber); err == gorm.ErrRecordNotFound {
			if cerr := h.callLogRepo.CreateParticipant(ctx, &models.CallParticipant{
				CallLogID:   existing.ID,
				PhoneNumber: payload.CallerNumber,
				Role:        "initiator",
				DisplayName: callerName,
			}); cerr != nil {
				log.Printf("[cintrix-webhook] failed to add caller participant for %s: %v", payload.CallUuid, cerr)
			}
		} else if err != nil {
			log.Printf("[cintrix-webhook] caller participant lookup failed for %s: %v", payload.CallUuid, err)
		}
	}
	if agentIdentifier != "" {
		if _, err := h.callLogRepo.FindParticipant(ctx, existing.ID, agentIdentifier); err == gorm.ErrRecordNotFound {
			if cerr := h.callLogRepo.CreateParticipant(ctx, &models.CallParticipant{
				CallLogID:   existing.ID,
				PhoneNumber: agentIdentifier,
				Role:        "recipient",
			}); cerr != nil {
				log.Printf("[cintrix-webhook] failed to add agent participant for %s: %v", payload.CallUuid, cerr)
			}
		} else if err != nil {
			log.Printf("[cintrix-webhook] agent participant lookup failed for %s: %v", payload.CallUuid, err)
		}
	}
	// Dialed callee (unanswered/direct call) — same dedup as the create path.
	if dialedIdentifier != "" && dialedIdentifier != agentIdentifier && dialedIdentifier != payload.CallerNumber {
		if _, err := h.callLogRepo.FindParticipant(ctx, existing.ID, dialedIdentifier); err == gorm.ErrRecordNotFound {
			if cerr := h.callLogRepo.CreateParticipant(ctx, &models.CallParticipant{
				CallLogID:   existing.ID,
				PhoneNumber: dialedIdentifier,
				Role:        "recipient",
			}); cerr != nil {
				log.Printf("[cintrix-webhook] failed to add dialed participant for %s: %v", payload.CallUuid, cerr)
			}
		} else if err != nil {
			log.Printf("[cintrix-webhook] dialed participant lookup failed for %s: %v", payload.CallUuid, err)
		}
	}

	// Join/leave fields: event ordering isn't guaranteed for fire-and-forget
	// pushes, so handle either call.answered or call.ended arriving first.
	if payload.Event == "call.answered" && payload.AnsweredAt != nil && agentIdentifier != "" {
		if err := h.callLogRepo.UpdateParticipantJoin(ctx, existing.ID, agentIdentifier, "joined", payload.AnsweredAt); err != nil {
			log.Printf("[cintrix-webhook] join update failed for %s: %v", payload.CallUuid, err)
		}
	}
	if payload.Event == "call.ended" && payload.EndedAt != nil {
		if err := h.callLogRepo.UpdateParticipantsLeftAt(ctx, existing.ID, *payload.EndedAt); err != nil {
			log.Printf("[cintrix-webhook] left_at update failed for %s: %v", payload.CallUuid, err)
		}
	}

	return utils.SuccessResponse(c, fiber.StatusOK, i18n.T(ctx, "cintrix_call_event_processed"), nil)
}
