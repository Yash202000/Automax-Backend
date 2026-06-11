package services

import (
	"context"
	"fmt"
	"log"
	"net/url"
	"os"
	"strings"
	"time"

	"github.com/automax/backend/internal/models"
	"github.com/automax/backend/internal/repository"
	pkgutils "github.com/automax/backend/pkg/utils"
)

const smsFeedbackTokenDuration = 72 * time.Hour
const smsFeedbackMaxRetries = 3

// SmsFeedbackService processes deferred SMS feedback notifications.
// On a final-close transition the SMS is not sent immediately; instead a pending
// record is created. This service (called every SLA monitor tick) checks whether
// the WhatsApp chatbot received a response within the configured window.
// If yes → skip SMS. If no → send SMS with the feedback link.
type SmsFeedbackService struct {
	pendingRepo  repository.SmsFeedbackPendingRepository
	incidentRepo repository.IncidentRepository
	notification *NotificationService
}

func NewSmsFeedbackService(
	pendingRepo repository.SmsFeedbackPendingRepository,
	incidentRepo repository.IncidentRepository,
	notification *NotificationService,
) *SmsFeedbackService {
	return &SmsFeedbackService{
		pendingRepo:  pendingRepo,
		incidentRepo: incidentRepo,
		notification: notification,
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
	// Check if WhatsApp chatbot already received a response for this incident.
	hasWhatsApp, err := s.pendingRepo.HasWhatsAppFeedback(ctx, p.IncidentID, p.ClosedAt)
	if err != nil {
		log.Printf("[SmsFeedback] incident=%s — error checking WhatsApp feedback: %v", p.IncidentID, err)
		p.RetryCount++
		p.Log = fmt.Sprintf("error checking WhatsApp feedback: %v", err)
		s.save(ctx, p)
		return
	}

	now := time.Now()
	p.ProcessedAt = &now

	if hasWhatsApp {
		p.Skipped = true
		p.Log = "WhatsApp chatbot received feedback — SMS not sent"
		log.Printf("[SmsFeedback] incident=%s — WhatsApp feedback found, SMS skipped", p.IncidentID)
		s.save(ctx, p)
		return
	}

	// No WhatsApp response — send SMS fallback.
	if p.MobileNo == "" {
		p.Skipped = true
		p.Log = "SMS skipped — no mobile number on record"
		log.Printf("[SmsFeedback] incident=%s — no mobile number, SMS skipped", p.IncidentID)
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

	incident, err := s.incidentRepo.FindByIDWithRelations(ctx, p.IncidentID)
	if err != nil {
		p.RetryCount++
		p.Log = fmt.Sprintf("failed to load incident: %v", err)
		log.Printf("[SmsFeedback] incident=%s — failed to load incident: %v", p.IncidentID, err)
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
		feedbackToken := pkgutils.GenerateFeedbackToken(p.FeedbackID.String(), p.IncidentID.String(), smsFeedbackTokenDuration)
		vars["feedback_url"] = fmt.Sprintf("%s/feedback/%s?signed_token=%s",
			smsPortalBase, p.IncidentID.String(), url.QueryEscape(feedbackToken))
		vars["feedback_link"] = fmt.Sprintf(`<a href="%s">تقييم الخدمة</a>`, vars["feedback_url"])
	}

	lang := p.Language
	if lang == "" {
		lang = "ar"
	}
	templateCode := p.TemplateCode

	log.Printf("[SmsFeedback] incident=%s — no WhatsApp feedback after delay, sending SMS to %s (template=%s)", p.IncidentID, p.MobileNo, templateCode)

	_, sendErr := s.notification.SendNotification(
		ctx, "sms", &templateCode, lang,
		[]string{p.MobileNo}, nil, nil,
		"", "", vars, nil, nil, nil,
	)
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
	p.Log = "SMS sent — no WhatsApp feedback received within delay window"
	log.Printf("[SmsFeedback] incident=%s — SMS sent successfully to %s", p.IncidentID, p.MobileNo)
	s.save(ctx, p)
}

func (s *SmsFeedbackService) save(ctx context.Context, p *models.SmsFeedbackPending) {
	if err := s.pendingRepo.Update(ctx, p); err != nil {
		log.Printf("SMS fallback scheduled at  failed to update pending record %s: %v", p.ID, err)
	}
}
