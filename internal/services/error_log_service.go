package services

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/csv"
	"encoding/hex"
	"fmt"
	"log"
	"strings"
	"time"

	"github.com/automax/backend/internal/models"
	"github.com/automax/backend/internal/repository"
	"github.com/automax/backend/pkg/utils"
	"github.com/google/uuid"
)

// DefaultErrorLog is the shared Error Log service. It is set once at startup so any
// module can record an error with LogError(...) without extra wiring.
var DefaultErrorLog *ErrorLogService

// LogError records an error in the Error Log. It never blocks, never panics and is a
// no-op if the service is not initialised, so it is always safe to call.
func LogError(entry models.ErrorLogEntry) {
	if DefaultErrorLog != nil {
		DefaultErrorLog.Log(entry)
	}
}

type ErrorLogService struct {
	repo    repository.ErrorLogRepository
	alerter *ErrorLogAlerter
}

// SetAlerter enables in-app alerts to admins for Critical errors.
func (s *ErrorLogService) SetAlerter(a *ErrorLogAlerter) { s.alerter = a }

func NewErrorLogService(repo repository.ErrorLogRepository) *ErrorLogService {
	return &ErrorLogService{repo: repo}
}

// Log saves an error in the background. Any failure here is swallowed so the
// user's action is never affected (FR-1 / AC-4).
func (s *ErrorLogService) Log(entry models.ErrorLogEntry) {
	if s == nil || s.repo == nil {
		return
	}
	occurredAt := time.Now()
	go func() {
		defer func() {
			if r := recover(); r != nil {
				log.Printf("[ErrorLog] recovered while logging: %v", r)
			}
		}()
		e := BuildErrorLog(entry, occurredAt)
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		if err := s.repo.Create(ctx, e); err != nil {
			log.Printf("[ErrorLog] failed to save error log: %v", err)
			return
		}
		if e.Severity == models.ErrorSeverityCritical && s.alerter != nil {
			s.alerter.Alert(e)
		}
	}()
}

func BuildErrorLog(in models.ErrorLogEntry, occurredAt time.Time) *models.ErrorLog {
	severity := in.Severity
	if severity == "" {
		severity = models.ErrorSeverityMedium
	}
	errType := in.ErrorType
	if errType == "" {
		errType = models.ErrorTypeUnexpected
	}
	userName := strings.TrimSpace(in.UserName)
	if userName == "" {
		userName = "System"
	}
	sum := sha256.Sum256([]byte(in.Module + "|" + errType + "|" + in.Action))

	return &models.ErrorLog{
		OccurredAt:       occurredAt,
		Module:           in.Module,
		Action:           in.Action,
		ErrorType:        errType,
		Severity:         severity,
		Message:          Truncate(utils.SanitizeErrorText(in.Message), 2000),
		TechnicalDetails: Truncate(utils.SanitizeErrorText(in.TechnicalDetails), 8000),
		UserID:           in.UserID,
		UserName:         userName,
		RelatedType:      in.RelatedType,
		RelatedID:        in.RelatedID,
		RelatedLabel:     Truncate(utils.SanitizeErrorText(in.RelatedLabel), 255),
		Status:           models.ErrorStatusNew,
		RetryCount:       in.RetryCount,
		Fingerprint:      hex.EncodeToString(sum[:]),
	}
}

func Truncate(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n]
}

func (s *ErrorLogService) GetByID(ctx context.Context, id uuid.UUID) (*models.ErrorLog, error) {
	return s.repo.FindByID(ctx, id)
}

func (s *ErrorLogService) List(ctx context.Context, f models.ErrorLogFilter) ([]models.ErrorLog, int64, error) {
	return s.repo.List(ctx, f)
}

// UpdateStatus changes an error's status and stores the note + who/when in its history.
func (s *ErrorLogService) UpdateStatus(ctx context.Context, id uuid.UUID, req models.UpdateErrorLogStatusRequest, userID *uuid.UUID, userName string) error {
	return s.repo.UpdateStatus(ctx, id, req.Status, strings.TrimSpace(req.Note), userID, userName)
}

// ExportCSV returns the filtered list as CSV, using the same masking as the screen.
func (s *ErrorLogService) ExportCSV(ctx context.Context, f models.ErrorLogFilter, withTechnical bool) ([]byte, error) {
	logs, err := s.repo.ListForExport(ctx, f, 10000)
	if err != nil {
		return nil, err
	}
	var buf bytes.Buffer
	buf.WriteString("\xEF\xBB\xBF") // UTF-8 BOM so Excel opens Arabic text correctly
	w := csv.NewWriter(&buf)
	header := []string{"Error ID", "Date/Time", "Module", "Action", "Error Type", "Severity", "Message", "User", "Related Record", "Status", "Retry Count"}
	if withTechnical {
		header = append(header, "Technical Details")
	}
	_ = w.Write(header)
	for i := range logs {
		e := &logs[i]
		row := []string{
			e.ErrorCode, e.OccurredAt.Format("2006-01-02 15:04:05"), e.Module, e.Action, e.ErrorType, e.Severity,
			utils.SanitizeErrorText(e.Message), e.UserName, utils.SanitizeErrorText(e.RelatedLabel), e.Status, fmt.Sprint(e.RetryCount),
		}
		if withTechnical {
			row = append(row, utils.SanitizeErrorText(e.TechnicalDetails))
		}
		_ = w.Write(row)
	}
	w.Flush()
	return buf.Bytes(), w.Error()
}

// PurgeExpired deletes logs older than the retention period set in System Settings.
func (s *ErrorLogService) PurgeExpired(ctx context.Context, settings *models.Settings) (int64, error) {
	value := settings.ErrorLogRetentionValue
	if value < 1 {
		value = 12
	}
	now := time.Now()
	cutoff := now.AddDate(0, -value, 0)
	if settings.ErrorLogRetentionUnit == "days" {
		cutoff = now.AddDate(0, 0, -value)
	}
	return s.repo.DeleteOlderThan(ctx, cutoff)
}

// LogAssignmentFailure records a failed (auto-)assignment of an incident.
func LogAssignmentFailure(incidentID uuid.UUID, incidentNumber string, err error) {
	if err == nil {
		return
	}
	LogError(models.ErrorLogEntry{
		Module:           models.ErrorModuleIncident,
		Action:           "Auto-assign incident",
		ErrorType:        models.ErrorTypeAssignmentFailed,
		Severity:         models.ErrorSeverityHigh,
		Message:          "Incident could not be assigned automatically",
		TechnicalDetails: err.Error(),
		RelatedType:      "incident",
		RelatedID:        incidentID.String(),
		RelatedLabel:     incidentNumber,
	})
}

// LogIntegrationFailure records a failed call to an external system (timeout, not
// responding, invalid response). The error type and message are chosen from the failure.
func LogIntegrationFailure(scriptName, scriptID string, incidentID uuid.UUID, incidentNumber string, statusCode int, err error) {
	if err == nil {
		return
	}
	text := strings.ToLower(err.Error())
	errType, message := models.ErrorTypeUnexpected, "Integration script failed"
	switch {
	case strings.Contains(text, "timeout") || strings.Contains(text, "deadline exceeded"):
		errType, message = models.ErrorTypeTimeout, "External system did not respond in time"
	case strings.Contains(text, "request failed"):
		errType, message = models.ErrorTypeConnectionFailed, "External system is not responding"
	case strings.Contains(text, "external api returned"):
		errType, message = models.ErrorTypeInvalidResponse, fmt.Sprintf("External system returned an error (HTTP %d)", statusCode)
	}
	severity := models.ErrorSeverityHigh
	if errType == models.ErrorTypeConnectionFailed {
		severity = models.ErrorSeverityCritical // external system is down
	}
	LogError(models.ErrorLogEntry{
		Module:           models.ErrorModuleIntegration,
		Action:           "Run integration: " + scriptName,
		ErrorType:        errType,
		Severity:         severity,
		Message:          message,
		TechnicalDetails: fmt.Sprintf("script=%s (%s) http_status=%d\n%s", scriptName, scriptID, statusCode, err.Error()),
		RelatedType:      "incident",
		RelatedID:        incidentID.String(),
		RelatedLabel:     incidentNumber,
	})
}

// LogContactFailure records a failure to find, create or update the contact (reporter /
// citizen record) of an incident. The phone number is masked by the logger.
func LogContactFailure(action, contactID, phone string, err error) {
	if err == nil {
		return
	}
	LogError(models.ErrorLogEntry{
		Module:           models.ErrorModuleContact,
		Action:           action,
		ErrorType:        models.ErrorTypeSaveFailed,
		Severity:         models.ErrorSeverityHigh,
		Message:          "Contact could not be saved from the incident",
		TechnicalDetails: err.Error(),
		RelatedType:      "contact",
		RelatedID:        contactID,
		RelatedLabel:     phone,
	})
}

// ListUsers returns the users that appear in the error log (for the User filter).
func (s *ErrorLogService) ListUsers(ctx context.Context) ([]models.ErrorLogUser, error) {
	return s.repo.ListUsers(ctx)
}
