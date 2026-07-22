package services

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"net/url"
	"strings"
	"time"
)

// WhatsAppFeedbackSessionStatus mirrors the GET {baseURL}?incident_id=...&phone=...
// response body from the feedback-session API.
type WhatsAppFeedbackSessionStatus struct {
	IncidentID     string `json:"incident_id"`
	Phone          string `json:"phone"`
	FeedbackStatus string `json:"feedback_status"`
}

// FinalCloseWhatsAppFeedbackSessionService deletes a still-active WhatsApp
type FinalCloseWhatsAppFeedbackSessionService struct {
	baseURL    string
	httpClient *http.Client
}

func NewFinalCloseWhatsAppFeedbackSessionService(baseURL string) *FinalCloseWhatsAppFeedbackSessionService {
	trimmed := strings.TrimRight(strings.TrimSpace(baseURL), "/")
	if trimmed == "" {
		log.Printf("[FinalCloseWhatsAppFeedbackSession] FINAL_CLOSE_WHATSAPP_FEEDBACK_SESSION_BASE_URL not configured — session cleanup disabled")
	} else {
		log.Printf("[FinalCloseWhatsAppFeedbackSession] configured with base URL %s", trimmed)
	}
	return &FinalCloseWhatsAppFeedbackSessionService{
		baseURL:    trimmed,
		httpClient: &http.Client{Timeout: 20 * time.Second},
	}
}

// DeleteSession calls DELETE {baseURL}?incident_id={incidentNumber}&phone={mobileNo}
func (s *FinalCloseWhatsAppFeedbackSessionService) DeleteSession(ctx context.Context, incidentNumber string, mobileNo string) error {
	if s == nil {
		return fmt.Errorf("[FinalCloseWhatsAppFeedbackSession] session: service not initialized")
	}
	if ctx == nil {
		ctx = context.Background()
	}
	if s.baseURL == "" {
		log.Printf("[FinalCloseWhatsAppFeedbackSession] skipped for %q: base URL not configured", mobileNo)
		return nil
	}

	incidentNumber = strings.TrimSpace(incidentNumber)
	if incidentNumber == "" {
		log.Printf("[FinalCloseWhatsAppFeedbackSession] skipped: incident number is empty")
		return fmt.Errorf("final-close whatsapp feedback session: incident number is empty")
	}

	mobileNo = strings.TrimSpace(mobileNo)
	if mobileNo == "" {
		log.Printf("[FinalCloseWhatsAppFeedbackSession] skipped: mobile number is empty")
		return fmt.Errorf("final-close whatsapp feedback session: mobile number is empty")
	}
	phone := strings.TrimPrefix(mobileNo, "+")

	query := url.Values{}
	query.Set("incident_id", incidentNumber)
	query.Set("phone", phone)
	reqURL := fmt.Sprintf("%s?%s", s.baseURL, query.Encode())
	log.Printf("[FinalCloseWhatsAppFeedbackSession] deleting session for incident=%s phone=%s", incidentNumber, phone)

	req, err := http.NewRequestWithContext(ctx, http.MethodDelete, reqURL, nil)
	if err != nil {
		log.Printf("[FinalCloseWhatsAppFeedbackSession] failed to build delete request for incident=%s phone=%s: %v", incidentNumber, phone, err)
		return fmt.Errorf("final-close whatsapp feedback session: build request for %s: %w", phone, err)
	}
	req.Header.Set("Accept", "application/json")

	resp, err := s.httpClient.Do(req)
	if err != nil {
		log.Printf("[FinalCloseWhatsAppFeedbackSession] delete request for %s failed: %v", phone, err)
		return fmt.Errorf("final-close whatsapp feedback session: delete request for %s failed: %w", phone, err)
	}
	if resp == nil {
		log.Printf("[FinalCloseWhatsAppFeedbackSession] delete request for %s returned no response", phone)
		return fmt.Errorf("final-close whatsapp feedback session: delete request for %s returned no response", phone)
	}
	defer resp.Body.Close()

	var bodyText string
	if resp.Body != nil {
		if raw, readErr := io.ReadAll(resp.Body); readErr == nil {
			bodyText = string(raw)
		} else {
			log.Printf("[FinalCloseWhatsAppFeedbackSession] failed to read response body for %s: %v", phone, readErr)
		}
	}

	switch {
	case resp.StatusCode == http.StatusNotFound:
		log.Printf("[FinalCloseWhatsAppFeedbackSession] no active session for %s — nothing to delete", phone)
		return nil
	case resp.StatusCode >= 200 && resp.StatusCode < 300:
		log.Printf("[FinalCloseWhatsAppFeedbackSession] session deleted for %s (status %d): %s", phone, resp.StatusCode, bodyText)
		return nil
	default:
		log.Printf("[FinalCloseWhatsAppFeedbackSession] delete for %s failed (status %d): %s", phone, resp.StatusCode, bodyText)
		return fmt.Errorf("final-close whatsapp feedback session: delete for %s failed (%d): %s", phone, resp.StatusCode, bodyText)
	}
}

// GetSessionStatus calls GET {baseURL}?incident_id={incidentNumber}&phone={mobileNo}
// to check whether a WhatsApp feedback session is still pending for the given
// incident/reporter. Returns (nil, nil) when the API reports no active session
// (i.e. feedback was already submitted or the session never existed) — this is
// not treated as an error.
func (s *FinalCloseWhatsAppFeedbackSessionService) GetSessionStatus(ctx context.Context, incidentNumber string, mobileNo string) (*WhatsAppFeedbackSessionStatus, error) {
	if s == nil {
		return nil, fmt.Errorf("[FinalCloseWhatsAppFeedbackSession] session: service not initialized")
	}
	if ctx == nil {
		ctx = context.Background()
	}
	if s.baseURL == "" {
		return nil, fmt.Errorf("final-close whatsapp feedback session: base URL not configured")
	}

	incidentNumber = strings.TrimSpace(incidentNumber)
	if incidentNumber == "" {
		return nil, fmt.Errorf("final-close whatsapp feedback session: incident number is empty")
	}

	mobileNo = strings.TrimSpace(mobileNo)
	if mobileNo == "" {
		return nil, fmt.Errorf("final-close whatsapp feedback session: mobile number is empty")
	}
	phone := strings.TrimPrefix(mobileNo, "+")

	query := url.Values{}
	query.Set("incident_id", incidentNumber)
	query.Set("phone", phone)
	reqURL := fmt.Sprintf("%s?%s", s.baseURL, query.Encode())

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, reqURL, nil)
	if err != nil {
		return nil, fmt.Errorf("final-close whatsapp feedback session: build status request for %s: %w", phone, err)
	}
	req.Header.Set("Accept", "application/json")

	resp, err := s.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("final-close whatsapp feedback session: status request for %s failed: %w", phone, err)
	}
	if resp == nil {
		return nil, fmt.Errorf("final-close whatsapp feedback session: status request for %s returned no response", phone)
	}
	defer resp.Body.Close()

	raw, readErr := io.ReadAll(resp.Body)
	if readErr != nil {
		return nil, fmt.Errorf("final-close whatsapp feedback session: read status response for %s: %w", phone, readErr)
	}

	switch {
	case resp.StatusCode == http.StatusNotFound:
		log.Printf("[FinalCloseWhatsAppFeedbackSession] no active session for incident=%s phone=%s", incidentNumber, phone)
		return nil, nil
	case resp.StatusCode >= 200 && resp.StatusCode < 300:
		var status WhatsAppFeedbackSessionStatus
		if err := json.Unmarshal(raw, &status); err != nil {
			return nil, fmt.Errorf("final-close whatsapp feedback session: parse status response for %s: %w", phone, err)
		}
		log.Printf("[FinalCloseWhatsAppFeedbackSession] session status for incident=%s phone=%s: %s", incidentNumber, phone, status.FeedbackStatus)
		return &status, nil
	default:
		return nil, fmt.Errorf("final-close whatsapp feedback session: status check for %s failed (%d): %s", phone, resp.StatusCode, string(raw))
	}
}
