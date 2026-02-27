package services

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"strings"
	"time"

	"github.com/automax/backend/internal/config"
	"github.com/automax/backend/internal/models"
	"github.com/automax/backend/internal/repository"
	"github.com/google/uuid"
	"gorm.io/gorm"
)

// ReadyToCloseService handles all logic for the "Ready to Close" expiry feature.
type ReadyToCloseService interface {
	// CreateEntry records a new Ready-to-Close expiry entry when an incident enters the ready_to_close state. It also updates the incident's ReadyToCloseExpiresAt field.
	CreateEntry(ctx context.Context, incidentID uuid.UUID, duration string, comment string, enteredBy uuid.UUID) error

	// DeactivateForIncident deactivates any active Ready-to-Close entry for an incident(called when the incident transitions away from the ready_to_close state).
	DeactivateForIncident(ctx context.Context, incidentID uuid.UUID) error

	// ProcessExpiries checks for incidents whose Ready-to-Close window has expired and automatically reverts them to the configured revert state (e.g. Under Resolution).
	ProcessExpiries(ctx context.Context) error

	// ProcessPreExpiryNotifications sends a single in-app notification to the assignee when an incident is within PreExpiryNotificationHours of its expiry.
	ProcessPreExpiryNotifications(ctx context.Context) error

	// GetDurationOptionsForState returns the duration options for a given workflow state.It uses the state's DurationOptions if set, otherwise falls back to the global config.
	GetDurationOptionsForState(state *models.WorkflowStateResponse) []string

	// ParseDuration converts a human-readable duration label (e.g. "1 Week") to a time.Duration.
	ParseDuration(label string) (time.Duration, error)
}

type readyToCloseService struct {
	repo         repository.ReadyToCloseRepository
	incidentRepo repository.IncidentRepository
	workflowRepo repository.WorkflowRepository
	notifService *NotificationService
	cfg          config.ReadyToCloseConfig
	db           *gorm.DB
}

func NewReadyToCloseService(
	repo repository.ReadyToCloseRepository,
	incidentRepo repository.IncidentRepository,
	workflowRepo repository.WorkflowRepository,
	notifService *NotificationService,
	cfg config.ReadyToCloseConfig,
	db *gorm.DB,
) ReadyToCloseService {
	return &readyToCloseService{
		repo:         repo,
		incidentRepo: incidentRepo,
		workflowRepo: workflowRepo,
		notifService: notifService,
		cfg:          cfg,
		db:           db,
	}
}

// CreateEntry records a Ready-to-Close entry and updates the incident's expiry fields.
func (s *readyToCloseService) CreateEntry(ctx context.Context, incidentID uuid.UUID, duration, comment string, enteredBy uuid.UUID) error {
	dur, err := s.ParseDuration(duration)
	if err != nil {
		return fmt.Errorf("invalid ready_to_close duration %q: %w", duration, err)
	}

	now := time.Now()
	expiresAt := now.Add(dur)

	entry := &models.IncidentReadyToCloseEntry{
		IncidentID:  incidentID,
		EnteredByID: enteredBy,
		EnteredAt:   now,
		ExpiresAt:   expiresAt,
		Duration:    duration,
		Comment:     comment,
		IsActive:    true,
	}

	if err := s.repo.CreateEntry(ctx, entry); err != nil {
		return err
	}

	// Update the incident's expiry fields for quick lookup
	return s.db.WithContext(ctx).
		Model(&models.Incident{}).
		Where("id = ?", incidentID).
		Updates(map[string]interface{}{
			"ready_to_close_expires_at": expiresAt,
			"ready_to_close_duration":   duration,
			"ready_to_close_notified":   false,
		}).Error
}

// DeactivateForIncident deactivates active entries and clears the incident's expiry fields.
func (s *readyToCloseService) DeactivateForIncident(ctx context.Context, incidentID uuid.UUID) error {
	if err := s.repo.DeactivateForIncident(ctx, incidentID); err != nil {
		return err
	}
	return s.db.WithContext(ctx).
		Model(&models.Incident{}).
		Where("id = ?", incidentID).
		Updates(map[string]interface{}{
			"ready_to_close_expires_at": nil,
			"ready_to_close_duration":   "",
			"ready_to_close_notified":   false,
		}).Error
}

// ProcessExpiries auto-reverts incidents that have exceeded their Ready-to-Close window.
func (s *readyToCloseService) ProcessExpiries(ctx context.Context) error {
	entries, err := s.repo.GetExpiredActiveEntries(ctx)
	if err != nil {
		return fmt.Errorf("get expired ready_to_close entries: %w", err)
	}
	if len(entries) == 0 {
		return nil
	}

	log.Printf("[ReadyToClose] Processing %d expired entries", len(entries))

	for _, entry := range entries {
		if err := s.revertIncident(ctx, &entry); err != nil {
			log.Printf("[ReadyToClose] Failed to revert incident %s: %v", entry.IncidentID, err)
			// Continue processing other entries
		}
	}
	return nil
}

// revertIncident moves a single incident from ready_to_close back to the configured revert state.
func (s *readyToCloseService) revertIncident(ctx context.Context, entry *models.IncidentReadyToCloseEntry) error {
	// Load incident with current state
	incident, err := s.incidentRepo.FindByIDWithRelations(ctx, entry.IncidentID)
	if err != nil {
		return fmt.Errorf("incident not found: %w", err)
	}

	// Verify the incident is still in a ready_to_close state
	if incident.CurrentState == nil || !incident.CurrentState.IsReadyToClose {
		// Already moved away — just deactivate the entry
		return s.repo.DeactivateEntry(ctx, entry.ID)
	}

	// Find the target revert state in the same workflow
	revertState, err := s.workflowRepo.FindStateByCode(ctx, incident.WorkflowID, s.cfg.RevertStateCode)
	if err != nil || revertState == nil {
		return fmt.Errorf("revert state %q not found in workflow %s", s.cfg.RevertStateCode, incident.WorkflowID)
	}

	now := time.Now()
	systemUserID := uuid.Nil // system-triggered action

	// Build history comment
	histComment := fmt.Sprintf(
		"Automatic reversion: incident expired in Ready to Close state after %s (entered at %s, expired at %s).",
		entry.Duration,
		entry.EnteredAt.Format("2006-01-02 15:04:05"),
		entry.ExpiresAt.Format("2006-01-02 15:04:05"),
	)

	// Create transition history record (no workflow transition ID — system action)
	history := &models.IncidentTransitionHistory{
		IncidentID:     entry.IncidentID,
		TransitionID:   nil, // system-triggered — no configured transition
		FromStateID:    incident.CurrentStateID,
		ToStateID:      revertState.ID,
		PerformedByID:  systemUserID,
		Comment:        histComment,
		IsSystemAction: true,
		TransitionedAt: now,
	}

	// Use a transaction for atomicity
	return s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		// Insert transition history
		if err := tx.Create(history).Error; err != nil {
			return err
		}

		// Deactivate the Ready-to-Close entry
		if err := tx.Model(&models.IncidentReadyToCloseEntry{}).
			Where("id = ?", entry.ID).
			Update("is_active", false).Error; err != nil {
			return err
		}

		// Update incident state and clear expiry fields
		updates := map[string]interface{}{
			"current_state_id":          revertState.ID,
			"ready_to_close_expires_at": nil,
			"ready_to_close_duration":   "",
			"ready_to_close_notified":   false,
			"updated_at":                now,
		}
		if err := tx.Model(&models.Incident{}).
			Where("id = ?", entry.IncidentID).
			Updates(updates).Error; err != nil {
			return err
		}

		// Create revision log
		oldStateName := incident.CurrentState.Name
		newStateName := revertState.Name
		changes := []models.IncidentFieldChange{
			{
				FieldName:  "current_state_id",
				FieldLabel: "Status",
				OldValue:   &oldStateName,
				NewValue:   &newStateName,
			},
		}
		changesBytes, _ := json.Marshal(changes)

		revNum := 1
		tx.Raw("SELECT COALESCE(MAX(revision_number), 0) + 1 FROM incident_revisions WHERE incident_id = ?", entry.IncidentID).Scan(&revNum)

		revision := &models.IncidentRevision{
			IncidentID:          entry.IncidentID,
			RevisionNumber:      revNum,
			ActionType:          models.RevisionActionStatusChanged,
			ActionDescription:   fmt.Sprintf("Automatic reversion: status changed from %s to %s (Ready to Close expired)", oldStateName, newStateName),
			Changes:             string(changesBytes),
			PerformedByID:       systemUserID,
			PerformedByRoles:    `["system"]`,
			TransitionHistoryID: &history.ID,
			CreatedAt:           now,
		}
		if err := tx.Create(revision).Error; err != nil {
			return err
		}

		log.Printf("[ReadyToClose] Reverted incident %s from %s to %s (entry %s expired)",
			entry.IncidentID, oldStateName, newStateName, entry.ID)
		return nil
	})
}

// ProcessPreExpiryNotifications sends one-time 24-hour pre-expiry in-app notifications.
func (s *readyToCloseService) ProcessPreExpiryNotifications(ctx context.Context) error {
	entries, err := s.repo.GetPreExpiryEntries(ctx, s.cfg.PreExpiryNotificationHours)
	if err != nil {
		return fmt.Errorf("get pre-expiry ready_to_close entries: %w", err)
	}
	if len(entries) == 0 {
		return nil
	}

	log.Printf("[ReadyToClose] Sending pre-expiry notifications for %d entries", len(entries))

	for _, entry := range entries {
		if err := s.sendPreExpiryNotification(ctx, &entry); err != nil {
			log.Printf("[ReadyToClose] Pre-expiry notification failed for incident %s: %v", entry.IncidentID, err)
		}
	}
	return nil
}

// sendPreExpiryNotification sends an in-app notification and logs it in incident_revisions.
func (s *readyToCloseService) sendPreExpiryNotification(ctx context.Context, entry *models.IncidentReadyToCloseEntry) error {
	incident, err := s.incidentRepo.FindByIDWithRelations(ctx, entry.IncidentID)
	if err != nil {
		return fmt.Errorf("incident not found: %w", err)
	}

	// Determine recipient — prefer the assignee
	if incident.AssigneeID == nil || incident.Assignee == nil || incident.Assignee.Email == "" {
		log.Printf("[ReadyToClose] Incident %s has no assignee; skipping pre-expiry notification", entry.IncidentID)
		// Mark as notified to avoid repeated attempts
		return s.repo.MarkExpiryNotified(ctx, entry.ID)
	}

	remaining := time.Until(entry.ExpiresAt)
	hours := int(remaining.Hours())
	minutes := int(remaining.Minutes()) % 60
	timeStr := fmt.Sprintf("%dh %dm", hours, minutes)

	subject := fmt.Sprintf("Incident %s: Ready to Close Expiring Soon", incident.IncidentNumber)
	body := fmt.Sprintf(
		"This incident will automatically move back to '%s' if not closed within %s.\n\nIncident: %s\nTitle: %s\nExpiry: %s",
		strings.Title(strings.ReplaceAll(s.cfg.RevertStateCode, "_", " ")),
		timeStr,
		incident.IncidentNumber,
		incident.Title,
		entry.ExpiresAt.Format("2006-01-02 15:04:05"),
	)

	if _, err := s.notifService.SendNotification(
		ctx,
		"notification", // in-app channel — delivers to user's inbox
		nil,            // no template
		"en",
		[]string{incident.Assignee.Email}, // email used as lookup key for inbox creation
		nil, nil,
		subject, body,
		map[string]string{
			"incident_number": incident.IncidentNumber,
			"incident_title":  incident.Title,
			"expires_at":      entry.ExpiresAt.Format("2006-01-02 15:04:05"),
			"remaining_time":  timeStr,
		},
		nil,
		nil, nil,
	); err != nil {
		log.Printf("[ReadyToClose] Notification delivery failed for incident %s: %v", entry.IncidentID, err)
		// Still mark as notified to prevent infinite retry
	}

	// Mark entry as notified and update incident flag
	if err := s.repo.MarkExpiryNotified(ctx, entry.ID); err != nil {
		return err
	}

	// Update incident-level flag
	if err := s.db.WithContext(ctx).
		Model(&models.Incident{}).
		Where("id = ?", entry.IncidentID).
		Update("ready_to_close_notified", true).Error; err != nil {
		return err
	}

	// Create revision log entry for audit
	now := time.Now()
	revNum := 1
	s.db.WithContext(ctx).Raw(
		"SELECT COALESCE(MAX(revision_number), 0) + 1 FROM incident_revisions WHERE incident_id = ?",
		entry.IncidentID,
	).Scan(&revNum)

	revision := &models.IncidentRevision{
		IncidentID:        entry.IncidentID,
		RevisionNumber:    revNum,
		ActionType:        models.RevisionActionStatusChanged,
		ActionDescription: fmt.Sprintf("Pre-expiry notification sent: Ready to Close expires in %s (at %s)", timeStr, entry.ExpiresAt.Format("2006-01-02 15:04:05")),
		Changes:           `[]`,
		PerformedByID:     uuid.Nil,
		PerformedByRoles:  `["system"]`,
		CreatedAt:         now,
	}
	s.db.WithContext(ctx).Create(revision)

	log.Printf("[ReadyToClose] Pre-expiry notification sent for incident %s (expires %s)", entry.IncidentID, entry.ExpiresAt.Format(time.RFC3339))
	return nil
}

// GetDurationOptionsForState returns the ordered duration options for a state.
// State-level options (DurationOptions field) take precedence over the global config.
func (s *readyToCloseService) GetDurationOptionsForState(state *models.WorkflowStateResponse) []string {
	if state != nil && len(state.DurationOptions) > 0 {
		return state.DurationOptions
	}
	return s.cfg.DefaultDurationOptions
}

// ParseDuration converts a human-readable duration label into a time.Duration.
// Supported formats: "N Day(s)", "N Week(s)", "N Month(s)", "N Hour(s)".
func (s *readyToCloseService) ParseDuration(label string) (time.Duration, error) {
	label = strings.TrimSpace(label)
	lower := strings.ToLower(label)

	var n int
	var unit string
	if _, err := fmt.Sscanf(lower, "%d %s", &n, &unit); err != nil {
		return 0, fmt.Errorf("cannot parse duration %q: expected format like '1 Day' or '2 Weeks'", label)
	}
	if n <= 0 {
		return 0, fmt.Errorf("duration must be positive, got %d", n)
	}

	unit = strings.TrimRight(unit, "s") // normalise plural: "days" -> "day", "weeks" -> "week"
	switch unit {
	case "hour":
		return time.Duration(n) * time.Hour, nil
	case "day":
		return time.Duration(n) * 24 * time.Hour, nil
	case "week":
		return time.Duration(n) * 7 * 24 * time.Hour, nil
	case "month":
		// Approximate month as 30 days
		return time.Duration(n) * 30 * 24 * time.Hour, nil
	default:
		return 0, fmt.Errorf("unrecognised duration unit %q in %q", unit, label)
	}
}
