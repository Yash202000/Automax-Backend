package services

import (
	"context"
	"fmt"
	"log"
	"net/url"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/automax/backend/internal/models"
	"github.com/automax/backend/internal/repository"
	pkgutils "github.com/automax/backend/pkg/utils"
	"github.com/google/uuid"
)

const smsFeedbackMaxRetries = 3

// GetFeedbackTokenDuration returns the feedback link TTL from SMS_FEEDBACK_TOKEN_EXPIRY_MINUTES,
// defaulting to 1440 minutes (24 hours) if unset or invalid.
func GetFeedbackTokenDuration() time.Duration {
	minutes := 1440
	if v := os.Getenv("SMS_FEEDBACK_TOKEN_EXPIRY_MINUTES"); v != "" {
		if parsed, err := strconv.Atoi(v); err == nil && parsed > 0 {
			minutes = parsed
		}
	}
	return time.Duration(minutes) * time.Minute
}

// SmsFeedbackService processes deferred SMS feedback notifications.
// On a final-close transition the SMS is not sent immediately; instead a pending
// record is created. This service (called every SLA monitor tick) checks whether
// the WhatsApp feedback session is still pending (via the feedback-session API)
// once the configured delay window has elapsed.
// If the session is gone (feedback already submitted) → skip SMS. If it's still
// PENDING → send SMS with the feedback link.
type SmsFeedbackService struct {
	pendingRepo    repository.SmsFeedbackPendingRepository
	incidentRepo   repository.IncidentRepository
	notification   *NotificationService
	sessionService *FinalCloseWhatsAppFeedbackSessionService
}

func NewSmsFeedbackService(
	pendingRepo repository.SmsFeedbackPendingRepository,
	incidentRepo repository.IncidentRepository,
	notification *NotificationService,
	sessionService *FinalCloseWhatsAppFeedbackSessionService,
) *SmsFeedbackService {
	return &SmsFeedbackService{
		pendingRepo:    pendingRepo,
		incidentRepo:   incidentRepo,
		notification:   notification,
		sessionService: sessionService,
	}
}

// ProcessPending is called on every SLA monitor tick. It finds all overdue pending
// records and either sends the SMS or skips it based on WhatsApp chatbot activity.
func (s *SmsFeedbackService) ProcessPending(ctx context.Context) error {
	records, err := s.pendingRepo.FindDue(ctx)
	if err != nil {
		return fmt.Errorf("[SmsFeedback] failed to fetch due records: %w", err)
	}
	if len(records) == 0 {
		return nil
	}

	log.Printf("[SmsFeedback] processing %d due record(s)", len(records))
	for i := range records {
		s.processOne(ctx, &records[i])
	}
	return nil
}

func (s *SmsFeedbackService) processOne(ctx context.Context, p *models.SmsFeedbackPending) {
	if p.MobileNo == "" {
		now := time.Now()
		p.ProcessedAt = &now
		p.Skipped = true
		p.Log = "SMS skipped — no mobile number on record"
		log.Printf("[SmsFeedback] incident=%s — no mobile number, SMS skipped", p.IncidentID)
		s.save(ctx, p)
		return
	}

	incident, err := s.incidentRepo.FindByIDWithRelations(ctx, p.IncidentID)
	if err != nil {
		p.RetryCount++
		p.Log = fmt.Sprintf("failed to load incident: %v", err)
		log.Printf("[SmsFeedback] incident=%s — failed to load incident: %v", p.IncidentID, err)
		s.save(ctx, p)
		return
	}

	// Check the WhatsApp feedback-session API to see whether the reporter's
	// session is still pending. No active session (feedback already submitted,
	// or session never existed) → skip. Still PENDING after the delay window → send SMS.
	status, err := s.sessionService.GetSessionStatus(ctx, incident.IncidentNumber, p.MobileNo)
	if err != nil {
		p.RetryCount++
		p.Log = fmt.Sprintf("error checking WhatsApp feedback session (attempt %d): %v", p.RetryCount, err)
		log.Printf("[SmsFeedback] incident=%s — error checking WhatsApp feedback session (attempt %d/%d): %v",
			p.IncidentID, p.RetryCount, smsFeedbackMaxRetries, err)
		if p.RetryCount >= smsFeedbackMaxRetries {
			p.Skipped = true
			p.Log = fmt.Sprintf("SMS skipped after %d failed session checks: %v", smsFeedbackMaxRetries, err)
			log.Printf("[SmsFeedback] incident=%s — max retries reached checking session status, marking skipped", p.IncidentID)
		}
		s.save(ctx, p)
		return
	}

	now := time.Now()
	p.ProcessedAt = &now

	if status == nil || !strings.EqualFold(status.FeedbackStatus, "PENDING") {
		p.Skipped = true
		p.Log = "WhatsApp feedback session resolved (no longer pending) — SMS not sent"
		log.Printf("[SmsFeedback] incident=%s — WhatsApp feedback session not pending, SMS skipped", p.IncidentID)
		s.save(ctx, p)
		return
	}

	if p.TemplateCode == "" {
		p.Skipped = true
		p.Log = "SMS skipped — no template_code configured on the final-close SMS action"
		log.Printf("[SmsFeedback] incident=%s — no template_code, SMS skipped", p.IncidentID)
		s.save(ctx, p)
		return
	}

	vars := BuildIncidentVariables(incident, nil, nil)

	// Override feedback_url with a properly signed token tied to the pre-created feedback record.
	smsPortalBase := strings.TrimRight(os.Getenv("SMS_PORTAL_URL"), "/")
	if smsPortalBase == "" {
		smsPortalBase = strings.TrimRight(os.Getenv("FRONTEND_URL"), "/")
	}
	if smsPortalBase != "" {
		feedbackToken := pkgutils.GenerateFeedbackToken(p.FeedbackID.String(), p.IncidentID.String(), GetFeedbackTokenDuration())
		vars["feedback_url"] = fmt.Sprintf("%s/feedback/%s?signed_token=%s",
			smsPortalBase, p.IncidentID.String(), url.QueryEscape(feedbackToken))
		vars["feedback_link"] = fmt.Sprintf(`<a href="%s">تقييم الخدمة</a>`, vars["feedback_url"])
	}

	lang := p.Language
	if lang == "" {
		lang = "ar"
	}
	templateCode := p.TemplateCode

	log.Printf("[SmsFeedback] incident=%s — WhatsApp feedback session still pending after delay, sending SMS to %s (template=%s)", p.IncidentID, p.MobileNo, templateCode)

	sendResult, sendErr := s.notification.SendNotification(
		ctx, "sms", &templateCode, lang,
		[]string{p.MobileNo}, nil, nil,
		"", "", vars, nil, nil, nil,
	)
	if sendResult != nil && sendResult.SentLog != nil {
		_ = s.notification.SetIncidentIDOnLogs(ctx, []uuid.UUID{sendResult.SentLog.ID}, incident.ID)
	}
	if sendErr != nil {
		p.RetryCount++
		p.Log = fmt.Sprintf("SMS send failed (attempt %d): %v", p.RetryCount, sendErr)
		log.Printf("[SmsFeedback] incident=%s — SMS send failed (attempt %d/%d): %v",
			p.IncidentID, p.RetryCount, smsFeedbackMaxRetries, sendErr)
		if p.RetryCount >= smsFeedbackMaxRetries {
			p.Skipped = true
			p.Log = fmt.Sprintf("SMS skipped after %d failed attempts: %v", smsFeedbackMaxRetries, sendErr)
			log.Printf("[SmsFeedback] incident=%s — max retries reached, marking skipped", p.IncidentID)
		}
		s.save(ctx, p)
		return
	}

	p.Sent = true
	p.Log = "SMS sent — WhatsApp feedback session still pending within delay window"
	log.Printf("[SmsFeedback] incident=%s — SMS sent successfully to %s", p.IncidentID, p.MobileNo)
	s.save(ctx, p)
}

func (s *SmsFeedbackService) save(ctx context.Context, p *models.SmsFeedbackPending) {
	if err := s.pendingRepo.Update(ctx, p); err != nil {
		log.Printf("SMS fallback scheduled at  failed to update pending record %s: %v", p.ID, err)
	}
}
