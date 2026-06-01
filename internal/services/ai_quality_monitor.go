package services

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"mime/multipart"
	"net/http"
	"sort"
	"time"

	"github.com/automax/backend/internal/config"
	"github.com/automax/backend/internal/models"
	"github.com/automax/backend/internal/repository"
	"github.com/automax/backend/internal/storage"
	"github.com/automax/backend/pkg/utils"
	"github.com/google/uuid"
)

// AIQualityMonitor watches for incidents that have is_ai_verified=true and whose current workflow
// state has is_ai_qa=true. For each qualifying incident (processed exactly once) it:
//  1. Downloads the two most relevant attachments from MinIO (oldest = before, newest = after).
//  2. POSTs a multipart/form-data request to the configured AI quality API.
//  3. Parses the JSON response and saves an AIQualityFeedback record.
//  4. Updates the incident with the AI-derived summary and resolution status.
type AIQualityMonitor interface {
	Start(ctx context.Context)
	Stop()
	ProcessAIQualityChecks(ctx context.Context) error
}

type aiQualityMonitor struct {
	feedbackRepo repository.AIQualityFeedbackRepository
	incidentRepo repository.IncidentRepository
	storage      *storage.MinIOStorage
	cfg          config.AIQualityConfig
	jwtManager   *utils.JWTManager
	httpClient   *http.Client
	interval     time.Duration
	stopChan     chan struct{}
	running      bool
}

// NewAIQualityMonitor creates and returns a new AIQualityMonitor.
func NewAIQualityMonitor(
	feedbackRepo repository.AIQualityFeedbackRepository,
	incidentRepo repository.IncidentRepository,
	minioStorage *storage.MinIOStorage,
	cfg config.AIQualityConfig,
	// jwtManager *utils.JWTManager,
) AIQualityMonitor {
	interval := time.Duration(cfg.CheckIntervalMinutes) * time.Minute
	if interval == 0 {
		interval = 10 * time.Minute
	}
	return &aiQualityMonitor{
		feedbackRepo: feedbackRepo,
		incidentRepo: incidentRepo,
		storage:      minioStorage,
		cfg:          cfg,
		// jwtManager:   jwtManager,
		httpClient: &http.Client{Timeout: 120 * time.Second},
		interval:   interval,
		stopChan:   make(chan struct{}),
	}
}

// Start begins background AI quality monitoring.
func (m *aiQualityMonitor) Start(ctx context.Context) {
	if m.running {
		return
	}
	m.running = true

	go func() {
		if err := m.ProcessAIQualityChecks(ctx); err != nil {
			log.Printf("[AIQualityMonitor] Initial check error: %v", err)
		}

		ticker := time.NewTicker(m.interval)
		defer ticker.Stop()

		for {
			select {
			case <-ticker.C:
				if err := m.ProcessAIQualityChecks(ctx); err != nil {
					log.Printf("[AIQualityMonitor] Periodic check error: %v", err)
				}
			case <-m.stopChan:
				log.Println("[AIQualityMonitor] Stopped")
				return
			case <-ctx.Done():
				log.Println("[AIQualityMonitor] Context cancelled")
				return
			}
		}
	}()
}

// Stop halts the background monitoring.
func (m *aiQualityMonitor) Stop() {
	if !m.running {
		return
	}
	m.running = false
	close(m.stopChan)
}

// ProcessAIQualityChecks finds all eligible incidents and runs the AI quality check on each.
func (m *aiQualityMonitor) ProcessAIQualityChecks(ctx context.Context) error {
	if m.cfg.APIEndpoint == "" {
		log.Println("[AIQualityMonitor] API endpoint not configured — skipping")
		return nil
	}

	incidents, err := m.feedbackRepo.FindPendingIncidents(ctx)
	if err != nil {
		return fmt.Errorf("find pending incidents: %w", err)
	}

	if len(incidents) == 0 {
		log.Println("[AIQualityMonitor] No pending incidents")
		return nil
	}

	for i := range incidents {
		inc := &incidents[i]
		if err := m.processIncident(ctx, inc); err != nil {
			log.Printf("[AIQualityMonitor] ERROR — incident %s: %v", inc.IncidentNumber, err)
			// Continue with the next incident; one failure must not block others.
		}
	}
	return nil
}

// ---- API response types ----------------------------------------------------

// coordinatesCheck is the nested object in the AI API response.
// BeforeCoordinates and AfterCoordinates are json.RawMessage because the API
// may return either a plain string or a nested object for these fields.
type coordinatesCheck struct {
	BeforeCoordinates json.RawMessage `json:"before_coordinates"`
	AfterCoordinates  json.RawMessage `json:"after_coordinates"`
	DistanceMeters    *float64        `json:"distance_meters"` // nullable
	IsMismatch        *bool           `json:"is_mismatch"`     // nullable
	Note              string          `json:"note"`
}

// coordinateDiff captures the coordinate comparison block returned by the API.
type coordinateDiff struct {
	Verdict string `json:"verdict"`
}

// sameSceneCheck captures the same-location analysis block returned by the API.
type sameSceneCheck struct {
	IsSameLocation bool     `json:"is_same_location"`
	Confidence     float64  `json:"confidence"`
	Evidence       []string `json:"evidence"`
}

// incidentAssessment captures the before/after assessment blocks returned by the API.
type incidentAssessment struct {
	IssuePresent bool   `json:"issue_present"`
	Severity     string `json:"severity"`
	Summary      string `json:"summary"`
}

// aiQualityAPIResponse mirrors the JSON returned by the verify-incident endpoint.
// It covers both the legacy v1 format and the new v2 format in a single struct so that
// a single json.Unmarshal handles either response. Use the helper methods
// (resolvedResolutionStatus, resolvedChangeSummary, resolvedDistanceMeters) to read
// values — they auto-detect the format and return the correct field.
type aiQualityAPIResponse struct {
	// ── v1 (legacy) fields ────────────────────────────────────────────────────
	ChangeSummary    string             `json:"change_summary"`
	ResolutionStatus string             `json:"resolution_status"`
	Confidence       float64            `json:"confidence"`
	ReasoningPoints  []string           `json:"reasoning_points"`
	RiskFlags        []string           `json:"risk_flags"`
	CoordinatesCheck coordinatesCheck   `json:"coordinates_check"`
	CoordinateDiff   coordinateDiff     `json:"coordinate_diff"`
	IncidentType     string             `json:"incident_type"`
	SameScene        sameSceneCheck     `json:"same_scene"`
	BeforeAssessment incidentAssessment `json:"before_assessment"`
	AfterAssessment  incidentAssessment `json:"after_assessment"`

	// ── v2 (new) fields ───────────────────────────────────────────────────────
	AuditID          string             `json:"audit_id"`
	AuditDecision    v2AuditDecision    `json:"audit_decision"`
	RiskFindings     v2RiskFindings     `json:"risk_findings"`
	EvidenceFindings v2EvidenceFindings `json:"evidence_findings"`
}

// isV2 returns true when the response uses the new v2 schema,
// identified by the presence of a non-empty audit_decision.audit_result.
func (r *aiQualityAPIResponse) isV2() bool {
	return r.AuditDecision.AuditResult != ""
}

// resolvedResolutionStatus returns the resolution status from whichever
// response format is present.
//
//	v2: audit_decision.audit_result
//	v1: resolution_status
func (r *aiQualityAPIResponse) resolvedResolutionStatus() string {
	if r.isV2() {
		return r.AuditDecision.AuditResult
	}
	return r.ResolutionStatus // v1
}

// resolvedChangeSummary returns the human-readable change summary from
// whichever response format is present.
//
//	v2: risk_findings.second_opinion.summary
//	v1: change_summary
func (r *aiQualityAPIResponse) resolvedChangeSummary() string {
	summary := r.ChangeSummary
	if r.isV2() {
		summary = r.RiskFindings.SecondOpinion.Summary
	}
	if verdict := r.CoordinateDiff.Verdict; verdict != "" {
		summary = summary + " | " + verdict
	}
	return summary
}

// resolvedDistanceMeters returns the GPS drift distance from whichever
// response format is present.
//
//	v2: evidence_findings.geospatial.distance_meters
//	v1: coordinates_check.distance_meters (nullable — nil treated as 0)
func (r *aiQualityAPIResponse) resolvedDistanceMeters() float64 {
	if r.isV2() {
		return r.EvidenceFindings.Geospatial.DistanceMeters
	}
	// v1: nullable pointer
	if r.CoordinatesCheck.DistanceMeters != nil {
		return *r.CoordinatesCheck.DistanceMeters
	}
	return 0.0
}

// ── v2 nested types ──────────────────────────────────────────────────────────

type v2AuditReasons struct {
	FailedChecks        []string `json:"failed_checks"`
	VerifiedChecks      []string `json:"verified_checks"`
	ManualReviewReasons []string `json:"manual_review_reasons"`
}

type v2AuditDecision struct {
	AuditResult       string         `json:"audit_result"`
	TrustScore        float64        `json:"trust_score"`
	RecommendedAction string         `json:"recommended_action"`
	Reasons           v2AuditReasons `json:"reasons"`
}

type v2SecondOpinion struct {
	Used              bool    `json:"used"`
	Summary           string  `json:"summary"`
	Provider          string  `json:"provider"`
	Confidence        float64 `json:"confidence"`
	ResolutionStatus  string  `json:"resolution_status"`
	AgreesWithPrimary bool    `json:"agrees_with_primary"`
}

type v2RiskFindings struct {
	RiskFlags     []string        `json:"risk_flags"`
	SecondOpinion v2SecondOpinion `json:"second_opinion"`
}

type v2Geospatial struct {
	DistanceMeters float64 `json:"distance_meters"`
	GPSFound       bool    `json:"gps_found"`
	GPSMatch       bool    `json:"gps_match"`
	Trust          string  `json:"trust"`
}

type v2EvidenceFindings struct {
	Geospatial v2Geospatial `json:"geospatial"`
}

// ---- core processing -------------------------------------------------------

func (m *aiQualityMonitor) processIncident(ctx context.Context, incident *models.Incident) error {
	// Filter to image-only attachments, then sort oldest-first.
	// Slot 0 → before_image, last slot → after_image.
	// If only one image exists it is used as before_image only.
	imageAttachments := imageOnly(incident.Attachments)
	sort.Slice(imageAttachments, func(i, j int) bool {
		return imageAttachments[i].CreatedAt.Before(imageAttachments[j].CreatedAt)
	})

	if len(imageAttachments) == 0 {
		return fmt.Errorf("no image attachments found — before_image is required")
	}

	if len(imageAttachments) < 2 {
		return fmt.Errorf("less than 2 image attachments found — before_image is required")
	}

	// Derive user_comment from the incident description (reporter's original text).
	userComment := incident.Description

	// Derive resolver_comment from the most recent comment (Comments are ordered newest-first).
	resolverComment := ""
	if len(incident.Comments) > 0 {
		resolverComment = incident.Comments[0].Content
	}

	if resolverComment == "" {
		resolverComment = incident.Title
	}

	// Format coordinates as "lat,lng" strings (empty string when not set).
	beforeCoords := formatCoords(incident.Latitude, incident.Longitude)
	afterCoords := beforeCoords // same location reference; override if resolver coords are stored elsewhere

	// Build multipart body.
	body := &bytes.Buffer{}
	writer := multipart.NewWriter(body)

	// Text fields.
	for field, value := range map[string]string{
		"user_comment":       userComment,
		"resolver_comment":   resolverComment,
		"before_coordinates": beforeCoords,
		"after_coordinates":  afterCoords,
	} {
		if err := writer.WriteField(field, value); err != nil {
			writer.Close()
			return fmt.Errorf("write field %q: %w", field, err)
		}
	}

	// before_image — oldest image attachment.
	beforeAtt := imageAttachments[0]
	if err := m.writeFileField(ctx, writer, "before_image", beforeAtt); err != nil {
		writer.Close()
		return fmt.Errorf("before_image (%s): %w", beforeAtt.FileName, err)
	}

	// after_image — newest image attachment (only when there are at least two images).
	if len(imageAttachments) >= 2 {
		afterAtt := imageAttachments[len(imageAttachments)-1]
		if err := m.writeFileField(ctx, writer, "after_image", afterAtt); err != nil {
			writer.Close()
			return fmt.Errorf("after_image (%s): %w", afterAtt.FileName, err)
		}

	} else {
		log.Printf("[AIQualityMonitor] incident=%s only one image attachment — after_image not sent", incident.IncidentNumber)
	}

	writer.Close() // must close before reading body

	if len(imageAttachments) >= 2 {
		afterAtt := imageAttachments[len(imageAttachments)-1]
		log.Printf("[AIQualityMonitor]   after_image:  id=%s name=%s mime=%s path=%s size=%d",
			afterAtt.ID, afterAtt.FileName, afterAtt.MimeType, afterAtt.FilePath, afterAtt.FileSize)
	}

	// Call the AI API.
	apiResp, rawBody, err := m.callAIAPI(ctx, body, writer.FormDataContentType())
	if err != nil {
		return fmt.Errorf("AI API call: %w", err)
	}

	log.Printf("[AIQualityMonitor] incident=%s API response (format=%s) — resolution_status=%q change_summary=%q distance_meters=%.4f",
		incident.IncidentNumber, map[bool]string{true: "v2", false: "v1"}[apiResp.isV2()],
		apiResp.resolvedResolutionStatus(), truncate(apiResp.resolvedChangeSummary(), 120), apiResp.resolvedDistanceMeters())

	if apiResp.BeforeAssessment.Summary != "" {
		log.Printf("[AIQualityMonitor] incident=%s before_assessment: severity=%q summary=%q",
			incident.IncidentNumber, apiResp.BeforeAssessment.Severity, apiResp.BeforeAssessment.Summary)
	}
	if apiResp.AfterAssessment.Summary != "" {
		log.Printf("[AIQualityMonitor] incident=%s after_assessment: severity=%q summary=%q",
			incident.IncidentNumber, apiResp.AfterAssessment.Severity, apiResp.AfterAssessment.Summary)
	}
	if len(apiResp.SameScene.Evidence) > 0 {
		log.Printf("[AIQualityMonitor] incident=%s same_scene_evidence: %v", incident.IncidentNumber, apiResp.SameScene.Evidence)
	}
	if len(apiResp.ReasoningPoints) > 0 {
		log.Printf("[AIQualityMonitor] incident=%s reasoning_points:", incident.IncidentNumber)
		for i, point := range apiResp.ReasoningPoints {
			log.Printf("[AIQualityMonitor]   [%d] %s", i+1, point)
		}
	}
	if apiResp.CoordinatesCheck.Note != "" {
		log.Printf("[AIQualityMonitor] incident=%s coordinates_note: %s",
			incident.IncidentNumber, apiResp.CoordinatesCheck.Note)
	}

	// Resolve the three stored fields using format-aware helpers.
	// To revert to v1-only extraction, replace the three helper calls below with:
	//   resolutionStatus = apiResp.ResolutionStatus
	//   changeSummary    = apiResp.ChangeSummary
	//   distanceMeters   = 0.0; if apiResp.CoordinatesCheck.DistanceMeters != nil { distanceMeters = *apiResp.CoordinatesCheck.DistanceMeters }
	resolutionStatus := apiResp.resolvedResolutionStatus()
	changeSummary := apiResp.resolvedChangeSummary()
	distanceMeters := apiResp.resolvedDistanceMeters()

	// Persist AIQualityFeedback.
	feedback := &models.AIQualityFeedback{
		IncidentID:       incident.ID,
		ChangedSummary:   changeSummary,
		ResolutionStatus: resolutionStatus,
		DistanceMeters:   distanceMeters,
		RawResponse:      rawBody,
	}
	if err := m.feedbackRepo.Create(ctx, feedback); err != nil {
		return fmt.Errorf("save AIQualityFeedback: %w", err)
	}

	// Update the incident: mark as AI-verified and apply the AI-derived change summary.
	updates := map[string]interface{}{
		"is_ai_verified": true,
	}
	// NOTE: ChangeSummary is already stored in the AIQualityFeedback record.
	// Do NOT write it to the incident's description — that would overwrite
	// the original reporter-provided description.
	if err := m.incidentRepo.UpdateFields(ctx, incident.ID, updates); err != nil {
		// Non-fatal — the feedback record was already persisted.
		log.Printf("[AIQualityMonitor] incident=%s WARNING: could not update incident fields: %v",
			incident.IncidentNumber, err)
	} else {
		log.Printf("[AIQualityMonitor] incident=%s updated incident fields: %v", incident.IncidentNumber, updates)
	}

	log.Printf("[AIQualityMonitor] incident=%s DONE — status=%q distance=%.4fm",
		incident.IncidentNumber, resolutionStatus, distanceMeters)
	return nil
}

// writeFileField downloads the attachment from MinIO and writes it as a multipart file field.
// If the MinIO object is not found it falls back to downloading via the attachment preview API.
func (m *aiQualityMonitor) writeFileField(ctx context.Context, writer *multipart.Writer, field string, att models.IncidentAttachment) error {

	part, err := writer.CreateFormFile(field, att.FileName)
	if err != nil {
		return fmt.Errorf("create form file: %w", err)
	}

	rc, storageErr := m.storage.GetFile(ctx, att.FilePath)
	if storageErr != nil {
		log.Printf("[AIQualityMonitor] MinIO download failed (%v) — falling back to API URL for attachment %s", storageErr, att.ID)
		return m.writeFileFieldViaURL(ctx, part, att)
	}
	defer rc.Close()

	if _, err := io.Copy(part, rc); err != nil {
		return fmt.Errorf("copy file data: %w", err)
	}
	return nil
}

// writeFileFieldViaURL downloads the attachment through the internal attachment preview API
// and copies it into the multipart part. Used as a fallback when MinIO storage is unavailable.
// It uses APP_TOKEN from config when set; otherwise it self-generates a short-lived system JWT.
func (m *aiQualityMonitor) writeFileFieldViaURL(ctx context.Context, part io.Writer, att models.IncidentAttachment) error {
	token := m.cfg.AppToken
	if token == "" && m.jwtManager != nil {
		// Self-generate a short-lived system token so no static APP_TOKEN env var is required.
		var err error
		token, err = m.jwtManager.GenerateToken(uuid.Nil, "system@internal", "admin")
		if err != nil {
			return fmt.Errorf("generate system token for attachment fallback: %w", err)
		}
	}

	attachURL := utils.GenerateAttachmentURL(m.cfg.AppProtocol, m.cfg.AppHost, att.ID.String(), token)
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, attachURL, nil)
	if err != nil {
		return fmt.Errorf("build fallback request: %w", err)
	}

	resp, err := m.httpClient.Do(req)
	if err != nil {
		return fmt.Errorf("fallback HTTP request: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return fmt.Errorf("fallback API returned HTTP %d for attachment %s", resp.StatusCode, att.ID)
	}

	if _, err := io.Copy(part, resp.Body); err != nil {
		return fmt.Errorf("copy fallback file data: %w", err)
	}
	return nil
}

// callAIAPI sends the multipart body to the AI API and parses the JSON response.
func (m *aiQualityMonitor) callAIAPI(ctx context.Context, body *bytes.Buffer, contentType string) (*aiQualityAPIResponse, []byte, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, m.cfg.APIEndpoint, body)
	if err != nil {
		return nil, nil, fmt.Errorf("build request: %w", err)
	}
	req.Header.Set("Content-Type", contentType)
	req.Header.Set("Accept", "application/json")
	if m.cfg.APIKey != "" {
		req.Header.Set("Authorization", "Bearer "+m.cfg.APIKey)
	}

	resp, err := m.httpClient.Do(req)
	if err != nil {
		return nil, nil, fmt.Errorf("http request: %w", err)
	}
	defer resp.Body.Close()

	rawBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, nil, fmt.Errorf("read response body: %w", err)
	}

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return nil, nil, fmt.Errorf("API returned HTTP %d: %s", resp.StatusCode, string(rawBody))
	}

	var apiResp aiQualityAPIResponse
	if err := json.Unmarshal(rawBody, &apiResp); err != nil {
		return nil, nil, fmt.Errorf("decode response JSON: %w", err)
	}
	return &apiResp, rawBody, nil
}

// ---- helpers ---------------------------------------------------------------

// imageOnly returns only the attachments whose MIME type starts with "image/".
func imageOnly(attachments []models.IncidentAttachment) []models.IncidentAttachment {
	out := make([]models.IncidentAttachment, 0, len(attachments))
	for _, a := range attachments {
		if len(a.MimeType) >= 6 && a.MimeType[:6] == "image/" {
			out = append(out, a)
		}
	}
	return out
}

// formatCoords returns "lat,lng" when both are non-nil, otherwise an empty string.
func formatCoords(lat, lng *float64) string {
	if lat == nil || lng == nil {
		return ""
	}
	return fmt.Sprintf("%f,%f", *lat, *lng)
}

// truncate caps a string at maxLen characters for safe log output.
func truncate(s string, maxLen int) string {
	if len(s) <= maxLen {
		return s
	}
	return s[:maxLen] + "…"
}

// nilableFloat renders a *float64 as a string for logging.
func nilableFloat(f *float64) string {
	if f == nil {
		return "null"
	}
	return fmt.Sprintf("%.4f", *f)
}
