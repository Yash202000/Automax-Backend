package services

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"time"

	"github.com/automax/backend/internal/config"
)

// MOMRAClassificationItem is the shared shape returned by 3.21/3.22/3.23 — the parent
// reference field name differs per level (ClassificationID on sub, SubClassificationID
// on special), so callers read ParentRef rather than a level-specific field.
type MOMRAClassificationItem struct {
	ID        string
	Name      string
	ParentRef string
}

// MOMRAExternalEntity is 3.35's result shape.
type MOMRAExternalEntity struct {
	EEName   string
	EECode   string
	EENameAR string
}

// MOMRALinkedClassification is one entry of 3.36's LinkedClassifications.
type MOMRALinkedClassification struct {
	ClassificationCode string
	ClassificationName string
}

// MOMRAExternalEntityClassifications is 3.36's per-EE result shape.
type MOMRAExternalEntityClassifications struct {
	EEName                string
	EECode                string
	EENameAR              string
	LinkedClassifications []MOMRALinkedClassification
}

// MOMRAStatusUpdateRequest is 3.14's request body. EECode/EEName/EENotesFromMUN are
// only meaningful (and, per MOMRA's contract, required) when CaseStatusID == "004".
type MOMRAStatusUpdateRequest struct {
	// CRMCaseID isn't listed in the Integration Guide's own §3.14.3 Fields Description
	// table, but it is present in that same section's §3.14.4 sample request body —
	// another instance of MOMRA's docs disagreeing with themselves. Set to the same
	// value as the URL's {CRMID} path segment (MOMRA's own incident number), matching
	// the field's literal name.
	CRMCaseID        string                  `json:"CRMCaseID,omitempty"`
	CaseStatusID     string                  `json:"CaseStatusID,omitempty"`
	ClosureFlag      string                  `json:"ClosureFlag,omitempty"`
	AmanaID          string                  `json:"AmanaID,omitempty"`
	AmanaIncidentID  string                  `json:"AmanaIncidentID,omitempty"`
	OperatorName     string                  `json:"OperatorName,omitempty"`
	OperatorID       string                  `json:"OperatorID,omitempty"`
	CompletionTime   string                  `json:"CompletionTime,omitempty"`
	OperatorComments string                  `json:"OperatorComments,omitempty"`
	Attachments      []MOMRAStatusAttachment `json:"Attachments,omitempty"`
	EECode           string                  `json:"EECode,omitempty"`
	EEName           string                  `json:"EEName,omitempty"`
	EENotesFromMUN   string                  `json:"EENotesFromMUN,omitempty"`
}

// momraCompletionTimeLayout matches the Integration Guide §3.14.4 sample's
// CompletionTime value exactly ("9/26/2023 4:23:09 PM" — legacy US-style, no leading
// zeros on month/day/hour), not ISO 8601/RFC3339. TFIS v1.0 itself flags this exact
// field as an unresolved format question (OD-014), but the real captured example from
// MOMRA's own Integration Guide is the more authoritative source here.
const momraCompletionTimeLayout = "1/2/2006 3:04:05 PM"

type MOMRAStatusAttachment struct {
	AttachmentCategory string `json:"AttachmentCategory,omitempty"`
	AttachmentName     string `json:"AttachmentName,omitempty"`
	AttachmentURL      string `json:"AttachmentURL,omitempty"`
}

// MOMRAResponseEnvelope is the common {statusDetails, data} shape every MOMRA CRM
// service returns (see docs/MOMRA_Outbound_Integration_Spec_v1.0.md §3.1's note that
// MOMRA only ever distinguishes success/business-error via data.responseCode "1"/"2" —
// richer error handling must key off the HTTP status code instead).
type MOMRAResponseEnvelope struct {
	StatusDetails struct {
		Code    int    `json:"code"`
		Message string `json:"message"`
	} `json:"statusDetails"`
	Data struct {
		ResponseCode    string          `json:"responseCode"`
		ResponseMessage string          `json:"responseMessage"`
		ResponseID      string          `json:"responseId"`
		Result          json.RawMessage `json:"result"`
	} `json:"data"`
}

// MOMRABusinessError is returned when MOMRA's HTTP call succeeds but
// data.responseCode signals a business error ("2") rather than success ("1").
type MOMRABusinessError struct {
	ResponseCode    string
	ResponseMessage string
}

func (e *MOMRABusinessError) Error() string {
	return fmt.Sprintf("MOMRA business error %s: %s", e.ResponseCode, e.ResponseMessage)
}

// MOMRAHTTPError carries the HTTP status code from a failed MOMRA call so callers can
// decide retryability per TFIS v1.0 §16.3/16.4: 502/503/504/timeout are retryable,
// 400/401/403/409 are not. StatusCode is 0 for a transport-level failure (no response
// at all), which is also treated as retryable.
type MOMRAHTTPError struct {
	StatusCode int
	Body       string
}

func (e *MOMRAHTTPError) Error() string {
	return fmt.Sprintf("momra request returned %d: %s", e.StatusCode, e.Body)
}

// Retryable reports whether this HTTP-level failure should be retried with backoff.
func (e *MOMRAHTTPError) Retryable() bool {
	return e.StatusCode == 0 || e.StatusCode == 502 || e.StatusCode == 503 || e.StatusCode == 504
}

// MOMRAClient is Automax's outbound client for the MOMRA CRM services Automax must
// call (3.1 auth, 3.14 status sync, 3.21-3.23 classifications, 3.35/3.36 external
// entities). This is the reverse direction from epm_incident_handler.go.
type MOMRAClient interface {
	GetMainClassifications(ctx context.Context, classType, municipalityID string) ([]MOMRAClassificationItem, error)
	GetSubClassifications(ctx context.Context, mainClassificationID string) ([]MOMRAClassificationItem, error)
	GetSpecialClassifications(ctx context.Context, subClassificationID string) ([]MOMRAClassificationItem, error)
	GetExternalEntities(ctx context.Context, requestID string) ([]MOMRAExternalEntity, error)
	GetExternalEntityClassifications(ctx context.Context, requestID, municipalityID, eeCode string) ([]MOMRAExternalEntityClassifications, error)
	UpdateIncidentStatus(ctx context.Context, crmID string, req MOMRAStatusUpdateRequest) error
}

type momraClient struct {
	cfg        config.MOMRAConfig
	httpClient *http.Client

	tokenMu     sync.Mutex
	cachedToken string
	tokenExpiry time.Time
}

func NewMOMRAClient(cfg config.MOMRAConfig) MOMRAClient {
	return &momraClient{
		cfg:        cfg,
		httpClient: &http.Client{Timeout: 30 * time.Second},
	}
}

// momraTokenPath is the Amana token API's path on MOMRAConfig.BaseURL.
const momraTokenPath = "/API/Authentication/GetToken"

// momraDefaultTokenTTL applies only when the token response omits expiresIn
// (or sends a non-positive value). 1800s matches the expiresIn in the client's
// sample GetToken response; erring short just means an earlier refresh.
const momraDefaultTokenTTL = 1800 * time.Second

// momraTokenResponse is the Amana GetToken response. expiresIn is a JSON
// number in the client's sample; json.Number also accepts a numeric string.
type momraTokenResponse struct {
	AccessToken  string      `json:"accessToken"`
	TokenType    string      `json:"tokenType"`
	ExpiresIn    json.Number `json:"expiresIn"`
	ErrorMessage *string     `json:"errorMessage"`
}

func (c *momraClient) tokenURL() string {
	return strings.TrimRight(c.cfg.BaseURL, "/") + momraTokenPath
}

// getToken returns a cached Bearer token, refreshing it via the Amana token
// API (POST {BaseURL}/API/Authentication/GetToken with JSON {"Service","Key"}) when missing or within 60s
// of expiry. tokenMu is held across the refresh so concurrent callers wait for
// the one in-flight request and then share its token.
func (c *momraClient) getToken(ctx context.Context) (string, error) {
	c.tokenMu.Lock()
	defer c.tokenMu.Unlock()

	if c.cachedToken != "" && time.Now().Before(c.tokenExpiry.Add(-60*time.Second)) {
		return c.cachedToken, nil
	}

	if c.cfg.AuthService == "" || c.cfg.AuthKey == "" {
		return "", fmt.Errorf("momra token request not attempted: MOMRA_AUTH_SERVICE / MOMRA_AUTH_KEY not configured")
	}

	payload, err := json.Marshal(struct {
		Service string `json:"Service"`
		Key     string `json:"Key"`
	}{Service: c.cfg.AuthService, Key: c.cfg.AuthKey})
	if err != nil {
		return "", err
	}

	tokenURL := c.tokenURL()
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, tokenURL, bytes.NewReader(payload))
	if err != nil {
		return "", err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "application/json")

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return "", fmt.Errorf("momra token request to %s failed: %w", tokenURL, err)
	}
	defer resp.Body.Close()

	body, _ := io.ReadAll(io.LimitReader(resp.Body, 16*1024))
	if resp.StatusCode < 200 || resp.StatusCode > 299 {
		return "", fmt.Errorf("momra token request to %s returned %d: %s",
			tokenURL, resp.StatusCode, c.safeExcerpt(body))
	}

	// An HTML body (login page, proxy/WAF error, IIS default page) means the
	// URL or request shape is wrong even when the status is 200 — report that
	// rather than a bare "invalid character '<'" JSON error.
	if isNonJSONBody(resp.Header.Get("Content-Type"), body) {
		return "", fmt.Errorf("momra token request to %s returned non-JSON response (status %d, content-type %q) — check MOMRA_BASE_URL: %s",
			tokenURL, resp.StatusCode, resp.Header.Get("Content-Type"), c.safeExcerpt(body))
	}

	var tokenResp momraTokenResponse
	if err := json.Unmarshal(body, &tokenResp); err != nil {
		return "", fmt.Errorf("momra token response parse failed (status %d): %w: %s",
			resp.StatusCode, err, c.safeExcerpt(body))
	}
	if tokenResp.ErrorMessage != nil && strings.TrimSpace(*tokenResp.ErrorMessage) != "" {
		return "", fmt.Errorf("momra token request rejected: %s", c.redact(*tokenResp.ErrorMessage))
	}
	if tokenResp.AccessToken == "" {
		return "", fmt.Errorf("momra token response contained no accessToken")
	}
	// Downstream CRM calls send "Authorization: Bearer <token>"; fail loudly if
	// the API ever issues a different token type rather than send a wrong header.
	if tokenResp.TokenType != "" && !strings.EqualFold(tokenResp.TokenType, "Bearer") {
		return "", fmt.Errorf("momra token response has unsupported tokenType %q", tokenResp.TokenType)
	}

	ttl := momraDefaultTokenTTL
	if tokenResp.ExpiresIn != "" {
		secs, err := tokenResp.ExpiresIn.Int64()
		if err != nil {
			return "", fmt.Errorf("momra token response has invalid expiresIn %q", tokenResp.ExpiresIn)
		}
		if secs > 0 {
			ttl = time.Duration(secs) * time.Second
		}
	}

	c.cachedToken = tokenResp.AccessToken
	c.tokenExpiry = time.Now().Add(ttl)
	return c.cachedToken, nil
}

// isNonJSONBody reports whether a response is clearly not JSON: an HTML/XML
// content type, or a body that starts with '<'.
func isNonJSONBody(contentType string, body []byte) bool {
	ct := strings.ToLower(contentType)
	if strings.Contains(ct, "html") || strings.Contains(ct, "xml") {
		return true
	}
	trimmed := bytes.TrimSpace(body)
	return len(trimmed) > 0 && trimmed[0] == '<'
}

// safeExcerpt returns a short, single-line, credential-redacted excerpt of a
// response body for error messages.
func (c *momraClient) safeExcerpt(body []byte) string {
	const maxRunes = 300
	text := strings.Join(strings.Fields(string(body)), " ")
	if r := []rune(text); len(r) > maxRunes {
		text = string(r[:maxRunes]) + "…"
	}
	if text == "" {
		return "<empty body>"
	}
	return c.redact(text)
}

// redact masks the configured GetToken credentials in case a response echoes
// them back.
func (c *momraClient) redact(s string) string {
	for _, secret := range []string{c.cfg.AuthKey, c.cfg.AuthService} {
		if secret != "" {
			s = strings.ReplaceAll(s, secret, "[redacted]")
		}
	}
	return s
}

// doGet performs an authenticated GET against MOMRA and unmarshals the common envelope.
func (c *momraClient) doGet(ctx context.Context, relativeURL string) (*MOMRAResponseEnvelope, error) {
	token, err := c.getToken(ctx)
	if err != nil {
		return nil, err
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, c.cfg.BaseURL+relativeURL, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Authorization", "Bearer "+token)

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("momra request failed: %w", err)
	}
	defer resp.Body.Close()

	body, _ := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if resp.StatusCode >= 400 {
		return nil, fmt.Errorf("momra request to %s returned %d: %s", relativeURL, resp.StatusCode, string(body))
	}

	var envelope MOMRAResponseEnvelope
	if err := json.Unmarshal(body, &envelope); err != nil {
		return nil, fmt.Errorf("momra response parse failed: %w", err)
	}
	if envelope.Data.ResponseCode == "2" {
		return &envelope, &MOMRABusinessError{ResponseCode: envelope.Data.ResponseCode, ResponseMessage: envelope.Data.ResponseMessage}
	}
	return &envelope, nil
}

func (c *momraClient) GetMainClassifications(ctx context.Context, classType, municipalityID string) ([]MOMRAClassificationItem, error) {
	q := url.Values{}
	q.Set("type", classType)
	if municipalityID != "" {
		q.Set("municipalityID", municipalityID)
	}
	envelope, err := c.doGet(ctx, "/v1/crm-services/mobile/main-classifications?"+q.Encode())
	if err != nil {
		return nil, err
	}
	var raw []struct {
		ID   string `json:"ID"`
		Name string `json:"Name"`
	}
	if err := json.Unmarshal(envelope.Data.Result, &raw); err != nil {
		return nil, fmt.Errorf("momra main-classifications result parse failed: %w", err)
	}
	items := make([]MOMRAClassificationItem, 0, len(raw))
	for _, r := range raw {
		items = append(items, MOMRAClassificationItem{ID: r.ID, Name: r.Name})
	}
	return items, nil
}

func (c *momraClient) GetSubClassifications(ctx context.Context, mainClassificationID string) ([]MOMRAClassificationItem, error) {
	q := url.Values{}
	q.Set("mainClassificationID", mainClassificationID)
	envelope, err := c.doGet(ctx, "/v1/crm-services/mobile/sub-classifications?"+q.Encode())
	if err != nil {
		return nil, err
	}
	var raw []struct {
		ID               string `json:"ID"`
		Name             string `json:"Name"`
		ClassificationID string `json:"ClassificationID"`
	}
	if err := json.Unmarshal(envelope.Data.Result, &raw); err != nil {
		return nil, fmt.Errorf("momra sub-classifications result parse failed: %w", err)
	}
	items := make([]MOMRAClassificationItem, 0, len(raw))
	for _, r := range raw {
		items = append(items, MOMRAClassificationItem{ID: r.ID, Name: r.Name, ParentRef: r.ClassificationID})
	}
	return items, nil
}

func (c *momraClient) GetSpecialClassifications(ctx context.Context, subClassificationID string) ([]MOMRAClassificationItem, error) {
	q := url.Values{}
	q.Set("subClassificationID", subClassificationID)
	envelope, err := c.doGet(ctx, "/v1/crm-services/mobile/get-special-classifications?"+q.Encode())
	if err != nil {
		return nil, err
	}
	var raw []struct {
		ID                  string `json:"ID"`
		Name                string `json:"Name"`
		SubClassificationID string `json:"SubClassificationID"`
	}
	if err := json.Unmarshal(envelope.Data.Result, &raw); err != nil {
		return nil, fmt.Errorf("momra special-classifications result parse failed: %w", err)
	}
	items := make([]MOMRAClassificationItem, 0, len(raw))
	for _, r := range raw {
		items = append(items, MOMRAClassificationItem{ID: r.ID, Name: r.Name, ParentRef: r.SubClassificationID})
	}
	return items, nil
}

func (c *momraClient) GetExternalEntities(ctx context.Context, requestID string) ([]MOMRAExternalEntity, error) {
	q := url.Values{}
	q.Set("RequestID", requestID)
	envelope, err := c.doGet(ctx, "/v1/crm-services/external-entities?"+q.Encode())
	if err != nil {
		return nil, err
	}
	var items []MOMRAExternalEntity
	if err := json.Unmarshal(envelope.Data.Result, &items); err != nil {
		return nil, fmt.Errorf("momra external-entities result parse failed: %w", err)
	}
	return items, nil
}

func (c *momraClient) GetExternalEntityClassifications(ctx context.Context, requestID, municipalityID, eeCode string) ([]MOMRAExternalEntityClassifications, error) {
	q := url.Values{}
	q.Set("RequestID", requestID)
	q.Set("MunicipalityID", municipalityID)
	if eeCode != "" {
		q.Set("EECode", eeCode)
	}
	envelope, err := c.doGet(ctx, "/v1/crm-services/external-entity-classifications?"+q.Encode())
	if err != nil {
		// When eeCode is set and MOMRA doesn't recognize it, this comes back as a
		// *MOMRABusinessError (responseCode "2") rather than an empty result — callers
		// syncing a single EE should check errors.As(err, &MOMRABusinessError{}) and
		// treat that as "skip this EE," not a fatal sync failure.
		return nil, err
	}
	var items []MOMRAExternalEntityClassifications
	if err := json.Unmarshal(envelope.Data.Result, &items); err != nil {
		return nil, fmt.Errorf("momra external-entity-classifications result parse failed: %w", err)
	}
	return items, nil
}

func (c *momraClient) UpdateIncidentStatus(ctx context.Context, crmID string, statusReq MOMRAStatusUpdateRequest) error {
	token, err := c.getToken(ctx)
	if err != nil {
		return err
	}

	body, err := json.Marshal(statusReq)
	if err != nil {
		return err
	}

	relativeURL := fmt.Sprintf("/v1/crm-services/incidents/%s/status", url.PathEscape(crmID))
	req, err := http.NewRequestWithContext(ctx, http.MethodPut, c.cfg.BaseURL+relativeURL, bytes.NewReader(body))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+token)

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return &MOMRAHTTPError{StatusCode: 0, Body: err.Error()}
	}
	defer resp.Body.Close()

	respBody, _ := io.ReadAll(io.LimitReader(resp.Body, 64*1024))
	if resp.StatusCode >= 400 {
		return &MOMRAHTTPError{StatusCode: resp.StatusCode, Body: string(respBody)}
	}

	var envelope MOMRAResponseEnvelope
	if err := json.Unmarshal(respBody, &envelope); err != nil {
		return fmt.Errorf("momra status update response parse failed: %w", err)
	}
	if envelope.Data.ResponseCode == "2" {
		return &MOMRABusinessError{ResponseCode: envelope.Data.ResponseCode, ResponseMessage: envelope.Data.ResponseMessage}
	}
	return nil
}
