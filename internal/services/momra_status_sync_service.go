package services

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"time"

	"github.com/automax/backend/internal/models"
	"github.com/automax/backend/internal/repository"
	"github.com/google/uuid"
	"gorm.io/gorm"
)

// momraStatusSyncScriptName is the Name of a placeholder IntegrationScript row used only
// as the required (not-null, FK'd) IntegrationScriptID anchor on IntegrationExecutionLog
// rows created by the MOMRA status sync — see Story B in
// docs/MOMRA_Outbound_Integration_Spec_v1.0.md: "reuse integration_executor.go's
// HTTP client/auth/execution-log pattern rather than building a parallel client."
// This script is never actually executed by integrationExecutor; it exists purely so
// admins can see MOMRA status-sync attempts via the existing
// GET /admin/integration-scripts/:id/logs endpoint, no new UI needed.
const momraStatusSyncScriptName = "MOMRA Status Sync (system)"

const (
	momraSyncMaxAttempts = 3
	momraSyncBaseBackoff = 2 * time.Second
)

// MOMRAStatusSyncService pushes Automax incident status changes to MOMRA via 3.14
// Update Status Version 2 (docs/MOMRA_Outbound_Integration_Spec_v1.0.md §3).
type MOMRAStatusSyncService interface {
	// SyncIncidentStatus looks up the mapping for newStateID and, if found, pushes the
	// status change to MOMRA with retry/backoff, logging the attempt. Safe to call in
	// a goroutine — it never returns an error to a caller that can't act on it; all
	// outcomes are recorded via IntegrationExecutionLog instead. input carries the
	// transition's actor, comment and attachments (see MOMRAStatusSyncInput).
	SyncIncidentStatus(ctx context.Context, incident *models.Incident, newStateID uuid.UUID, input MOMRAStatusSyncInput)
	// RetryFailedSync re-attempts a previously failed sync from its logged request
	// payload, for the manual-retry acceptance criterion in Story C.
	RetryFailedSync(ctx context.Context, logID uuid.UUID) error
	// LogScriptID returns the placeholder IntegrationScript ID execution logs are
	// anchored to, so admin UIs can list them without needing to know it independently.
	LogScriptID() uuid.UUID
}

// MOMRAStatusSyncInput is the transition-time data UpdateStatusV2 needs that
// isn't on the incident itself. Resolved by the caller before the async sync.
type MOMRAStatusSyncInput struct {
	OperatorName string // user who performed the transition
	OperatorID   string
	// Comment is the transition comment, sent as both OperatorComments and
	// EENotesFromMUN.
	Comment string
	// Attachments are the files added on this transition, with URLs MOMRA can
	// download.
	Attachments []MOMRAStatusAttachment
}

type momraStatusSyncService struct {
	client          MOMRAClient
	mappingRepo     repository.MOMRAStatusMappingRepository
	integrationRepo repository.IntegrationRepository
	amanaID         string

	logScriptID uuid.UUID
}

// NewMOMRAStatusSyncService find-or-creates the placeholder IntegrationScript used to
// anchor execution logs, then returns the service. Call once at startup.
func NewMOMRAStatusSyncService(
	ctx context.Context,
	client MOMRAClient,
	mappingRepo repository.MOMRAStatusMappingRepository,
	integrationRepo repository.IntegrationRepository,
	amanaID string,
) (MOMRAStatusSyncService, error) {
	scriptID, err := findOrCreateMOMRALogScript(ctx, integrationRepo)
	if err != nil {
		return nil, fmt.Errorf("momra status sync: failed to prepare log anchor script: %w", err)
	}
	return &momraStatusSyncService{
		client:          client,
		mappingRepo:     mappingRepo,
		integrationRepo: integrationRepo,
		amanaID:         amanaID,
		logScriptID:     scriptID,
	}, nil
}

func findOrCreateMOMRALogScript(ctx context.Context, integrationRepo repository.IntegrationRepository) (uuid.UUID, error) {
	scripts, err := integrationRepo.ListScripts(ctx, false)
	if err != nil {
		return uuid.Nil, err
	}
	for _, s := range scripts {
		if s.Name == momraStatusSyncScriptName {
			return s.ID, nil
		}
	}

	script := &models.IntegrationScript{
		Name:          momraStatusSyncScriptName,
		Description:   "Internal placeholder — anchors execution logs for the MOMRA outbound status sync (3.14). Not user-runnable.",
		ScriptType:    "http_request",
		ScriptContent: "{}",
		IsActive:      false,
	}
	if err := integrationRepo.CreateScript(ctx, script); err != nil {
		return uuid.Nil, err
	}
	return script.ID, nil
}

func (s *momraStatusSyncService) SyncIncidentStatus(ctx context.Context, incident *models.Incident, newStateID uuid.UUID, input MOMRAStatusSyncInput) {
	mapping, err := s.mappingRepo.FindActiveByState(ctx, newStateID)
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			log.Printf("[momra-status-sync] no active CaseStatusID mapping for state %s (incident %s) — skipping, not a failure", newStateID, incident.IncidentNumber)
			return
		}
		log.Printf("[momra-status-sync] failed to look up mapping for state %s: %v", newStateID, err)
		return
	}

	crmID := extractMomraIncidentNo(incident.CustomFields)
	if crmID == "" {
		log.Printf("[momra-status-sync] incident %s has no momra_incident_no in custom_fields — not a MOMRA-sourced incident, skipping", incident.IncidentNumber)
		return
	}

	req := buildMOMRAStatusUpdateRequest(incident, mapping, crmID, s.amanaID, input)
	s.attemptWithRetry(ctx, incident, crmID, req)
}

// buildMOMRAStatusUpdateRequest builds the UpdateStatusV2 body with every field
// of the client's request contract: case status/closure from the mapping, the
// MOMRA incident number (also the URL's {CRMID}) as CRMCaseID, AmanaID, the
// Automax incident number, the acting operator, completion time, the
// transition comment and attachments, and the incident's external entity when
// it is assigned to one.
func buildMOMRAStatusUpdateRequest(incident *models.Incident, mapping *models.MOMRAStatusMapping, crmID, amanaID string, input MOMRAStatusSyncInput) MOMRAStatusUpdateRequest {
	closureFlag := "No"
	if mapping.IsClosureStatus {
		closureFlag = "Yes"
	}

	req := MOMRAStatusUpdateRequest{
		CRMCaseID:        crmID,
		CaseStatusID:     mapping.CaseStatusID,
		ClosureFlag:      closureFlag,
		AmanaID:          amanaID,
		AmanaIncidentID:  incident.IncidentNumber,
		CompletionTime:   time.Now().Format(momraCompletionTimeLayout),
		OperatorName:     input.OperatorName,
		OperatorID:       input.OperatorID,
		OperatorComments: input.Comment,
		Attachments:      input.Attachments,
	}

	// Department{Type:"external"} is Automax's model for MOMRA External Entities
	// (see internal/services/momra_sync_service.go) — Department.Code stores EECode.
	if incident.Department != nil && incident.Department.Type == externalEntityDepartmentType {
		req.EECode = incident.Department.Code
		req.EEName = incident.Department.Name
		req.EENotesFromMUN = input.Comment
	}

	return req
}

func (s *momraStatusSyncService) attemptWithRetry(ctx context.Context, incident *models.Incident, crmID string, req MOMRAStatusUpdateRequest) {
	requestPayload, _ := json.Marshal(struct {
		CRMID string `json:"CRMID"`
		MOMRAStatusUpdateRequest
	}{CRMID: crmID, MOMRAStatusUpdateRequest: req})

	logEntry := &models.IntegrationExecutionLog{
		IntegrationScriptID: s.logScriptID,
		IncidentID:          incident.ID,
		IncidentNumber:      incident.IncidentNumber,
		TriggerType:         "momra_status_sync",
		Status:              "running",
		RequestPayload:      string(requestPayload),
		ExecutedAt:          time.Now(),
	}
	_ = s.integrationRepo.CreateExecutionLog(ctx, logEntry)

	start := time.Now()
	var lastErr error
	for attempt := 1; attempt <= momraSyncMaxAttempts; attempt++ {
		lastErr = s.client.UpdateIncidentStatus(ctx, crmID, req)
		if lastErr == nil {
			break
		}

		var httpErr *MOMRAHTTPError
		retryable := errors.As(lastErr, &httpErr) && httpErr.Retryable()
		if !retryable || attempt == momraSyncMaxAttempts {
			break
		}

		backoff := momraSyncBaseBackoff * time.Duration(1<<(attempt-1)) // 2s, 4s, 8s...
		log.Printf("[momra-status-sync] attempt %d/%d for incident %s failed (%v), retrying in %s", attempt, momraSyncMaxAttempts, incident.IncidentNumber, lastErr, backoff)
		time.Sleep(backoff)
	}

	now := time.Now()
	logEntry.CompletedAt = &now
	logEntry.DurationMs = time.Since(start).Milliseconds()

	if lastErr != nil {
		logEntry.Status = "failed"
		logEntry.ErrorMessage = lastErr.Error()
		var httpErr *MOMRAHTTPError
		if errors.As(lastErr, &httpErr) {
			logEntry.StatusCode = httpErr.StatusCode
		}
		log.Printf("[momra-status-sync] incident %s: all attempts failed, marked failed for manual retry: %v", incident.IncidentNumber, lastErr)
	} else {
		logEntry.Status = "success"
		logEntry.StatusCode = 200
	}
	_ = s.integrationRepo.UpdateExecutionLog(ctx, logEntry)
}

func (s *momraStatusSyncService) RetryFailedSync(ctx context.Context, logID uuid.UUID) error {
	original, err := s.integrationRepo.FindExecutionLogByID(ctx, logID)
	if err != nil {
		return fmt.Errorf("find log: %w", err)
	}
	if original.Status != "failed" {
		return fmt.Errorf("log %s is not in a failed state (status=%s)", logID, original.Status)
	}

	var parsed struct {
		CRMID string `json:"CRMID"`
		MOMRAStatusUpdateRequest
	}
	if err := json.Unmarshal([]byte(original.RequestPayload), &parsed); err != nil {
		return fmt.Errorf("parse original request payload: %w", err)
	}
	if parsed.CRMID == "" {
		return fmt.Errorf("original request payload has no CRMID")
	}

	incident := &models.Incident{ID: original.IncidentID, IncidentNumber: original.IncidentNumber}
	s.attemptWithRetry(ctx, incident, parsed.CRMID, parsed.MOMRAStatusUpdateRequest)
	return nil
}

func (s *momraStatusSyncService) LogScriptID() uuid.UUID {
	return s.logScriptID
}

// extractMomraIncidentNo reads custom_fields.momra_incident_no, matching the same
// pattern epm_incident_handler.go already uses to correlate MOMRA-sourced incidents.
func extractMomraIncidentNo(customFieldsJSON string) string {
	if customFieldsJSON == "" {
		return ""
	}
	var fields map[string]interface{}
	if err := json.Unmarshal([]byte(customFieldsJSON), &fields); err != nil {
		return ""
	}
	momraNo, _ := fields["momra_incident_no"].(string)
	return momraNo
}
