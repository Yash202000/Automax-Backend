package services

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"time"

	"github.com/automax/backend/internal/config"
)

// NasaqData is the Phase 1 permit information returned by the Nasaq
// Integration Microservice on a MATCHED result.
type NasaqData struct {
	PolygonPathID                      string `json:"polygonPathId,omitempty"`
	PermitTypeName                     string `json:"permitTypeName"`
	PermitStatusName                   string `json:"permitStatusName"`
	PermitExpiryDate                   string `json:"permitExpiryDate"`
	PermitWarrantyExpiryDate           string `json:"permitWarrantyExpiryDate,omitempty"`
	WarrantyStatus                     string `json:"warrantyStatus"`
	MainContractorName                 string `json:"mainContractorName"`
	MainContractorProjectManagerName   string `json:"mainContractorProjectManagerName"`
	MainContractorProjectManagerMobile string `json:"mainContractorProjectManagerMobile"`
}

// NasaqCandidate is one permit in a REVIEW_REQUIRED result.
type NasaqCandidate struct {
	PermitNumber     int64   `json:"permitNumber"`
	Distance         float64 `json:"distance"`
	PermitTypeName   string  `json:"permitTypeName"`
	PermitStatusName string  `json:"permitStatusName"`
	PermitExpiryDate string  `json:"permitExpiryDate"`
}

// NasaqVerifyResult is the Nasaq Integration Microservice's normalized
// response to POST /api/v1/verify.
type NasaqVerifyResult struct {
	IncidentID         string           `json:"incidentId"`
	VerificationStatus string           `json:"verificationStatus"`
	Message            string           `json:"message,omitempty"`
	PermitNumber       int64            `json:"permitNumber,omitempty"`
	Distance           *float64         `json:"distance,omitempty"`
	RetrievedAt        string           `json:"retrievedAt"`
	ResponseID         string           `json:"responseId,omitempty"`
	BufferUsed         int              `json:"bufferUsed"`
	NasaqData          *NasaqData       `json:"nasaqData,omitempty"`
	Candidates         []NasaqCandidate `json:"candidates,omitempty"`
}

// NasaqClientError wraps a non-2xx response from the Nasaq Integration
// Microservice (e.g. its own upstream Nasaq auth/rate-limit/service
// failures) - it is always a genuine failure to complete the verification,
// never a business outcome (NO_MATCH/REVIEW_REQUIRED come back as normal
// 200 results, not errors).
type NasaqClientError struct {
	StatusCode int
	Code       string
	Message    string
	Retryable  bool
}

func (e *NasaqClientError) Error() string {
	return fmt.Sprintf("nasaq integration service: %s (http=%d code=%s)", e.Message, e.StatusCode, e.Code)
}

type NasaqClient interface {
	// Verify calls the Nasaq Integration Microservice for one Incident.
	// pointX/pointY are longitude/latitude respectively, per the Nasaq
	// Integration Guide's coordinate naming.
	Verify(ctx context.Context, incidentID string, pointX, pointY float64) (*NasaqVerifyResult, error)
}

type nasaqClient struct {
	cfg        config.NasaqConfig
	httpClient *http.Client
}

func NewNasaqClient(cfg config.NasaqConfig) NasaqClient {
	timeout := time.Duration(cfg.TimeoutSeconds) * time.Second
	if timeout <= 0 {
		timeout = 20 * time.Second
	}
	return &nasaqClient{
		cfg:        cfg,
		httpClient: &http.Client{Timeout: timeout},
	}
}

type nasaqVerifyRequestBody struct {
	IncidentID string  `json:"incidentId"`
	PointX     float64 `json:"pointX"`
	PointY     float64 `json:"pointY"`
}

type nasaqErrorBody struct {
	Error     string `json:"error"`
	Code      string `json:"code"`
	Retryable bool   `json:"retryable"`
}

func (c *nasaqClient) Verify(ctx context.Context, incidentID string, pointX, pointY float64) (*NasaqVerifyResult, error) {
	if c.cfg.BaseURL == "" {
		return nil, &NasaqClientError{Message: "NASAQ_SERVICE_BASE_URL is not configured", Retryable: false}
	}

	reqBody, err := json.Marshal(nasaqVerifyRequestBody{IncidentID: incidentID, PointX: pointX, PointY: pointY})
	if err != nil {
		return nil, fmt.Errorf("nasaq client: encode request: %w", err)
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.cfg.BaseURL+"/api/v1/verify", bytes.NewReader(reqBody))
	if err != nil {
		return nil, fmt.Errorf("nasaq client: build request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "application/json")
	if c.cfg.APIKey != "" {
		req.Header.Set("X-Api-Key", c.cfg.APIKey)
	}

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, &NasaqClientError{Message: err.Error(), Retryable: true}
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if err != nil {
		return nil, fmt.Errorf("nasaq client: read response: %w", err)
	}

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		var errBody nasaqErrorBody
		_ = json.Unmarshal(body, &errBody)
		msg := errBody.Error
		if msg == "" {
			msg = "nasaq integration service call failed"
		}
		return nil, &NasaqClientError{StatusCode: resp.StatusCode, Code: errBody.Code, Message: msg, Retryable: errBody.Retryable}
	}

	var result NasaqVerifyResult
	if err := json.Unmarshal(body, &result); err != nil {
		return nil, fmt.Errorf("nasaq client: decode response: %w", err)
	}
	return &result, nil
}
