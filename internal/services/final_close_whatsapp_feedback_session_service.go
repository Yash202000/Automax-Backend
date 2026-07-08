package services

import (
	"context"
	"fmt"
	"io"
	"log"
	"net/http"
	"strings"
	"time"
)

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
		httpClient: &http.Client{Timeout: 15 * time.Second},
	}
}

// DeleteSession calls DELETE {baseURL}/{mobileNo} to invalidate the WhatsApp
// feedback session for the given number. baseURL is expected to already
// include the "/session" path segment (see FINAL_CLOSE_WHATSAPP_FEEDBACK_SESSION_BASE_URL),
// so it's not hardcoded here. A 404 ("no active session") means there was
// nothing to clean up and is treated as success, not an error.
func (s *FinalCloseWhatsAppFeedbackSessionService) DeleteSession(ctx context.Context, mobileNo string) error {
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

	mobileNo = strings.TrimSpace(mobileNo)
	if mobileNo == "" {
		log.Printf("[FinalCloseWhatsAppFeedbackSession] skipped: mobile number is empty")
		return fmt.Errorf("final-close whatsapp feedback session: mobile number is empty")
	}
	phone := strings.TrimPrefix(mobileNo, "+")

	url := fmt.Sprintf("%s/%s", s.baseURL, phone)
	log.Printf("[FinalCloseWhatsAppFeedbackSession] deleting session for %s", phone)

	req, err := http.NewRequestWithContext(ctx, http.MethodDelete, url, nil)
	if err != nil {
		log.Printf("[FinalCloseWhatsAppFeedbackSession] failed to build delete request for %s: %v", phone, err)
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
