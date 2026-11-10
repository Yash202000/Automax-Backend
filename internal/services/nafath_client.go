package services

import (
	"bytes"
	"context"
	"crypto/tls"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	"github.com/automax/backend/internal/config"
)

// Nafath login errors. Callers map them to user-facing responses; the wrapped
// detail is for server logs only and never contains secrets.
var (
	// ErrNafathRefused: the gateway refused the request for a business
	// reason (e.g. the ID is not registered in Nafath, too many requests).
	// Wrapped in NafathRefusal, whose Message may be shown to the user.
	ErrNafathRefused = errors.New("nafath: request refused")
	// ErrNafathTokenRejected: the gateway access token was refused (401). A
	// new token must be requested.
	ErrNafathTokenRejected = errors.New("nafath: access token rejected")
	// ErrNafathUnknownRequest: NafathCheckRequest no longer knows the
	// transID (expired and forgotten by the gateway).
	ErrNafathUnknownRequest = errors.New("nafath: request not found")
	// ErrNafathUnavailable: timeout, transport failure, 5xx, non-JSON or an
	// unexpected response. Not the user's fault.
	ErrNafathUnavailable = errors.New("nafath: service unavailable")
)

// NafathRefusal carries the gateway's own message for ErrNafathRefused.
type NafathRefusal struct {
	Message string
	Detail  string
}

func (e *NafathRefusal) Error() string { return "nafath: request refused: " + e.Detail }
func (e *NafathRefusal) Unwrap() error { return ErrNafathRefused }

// nafathMockHeader is set by nafath-simulator on every response.
const nafathMockHeader = "X-Nafath-Mock"

const (
	nafathTokenPath = "/API/Authentication/GetAccessTokenByIdNumber"
	nafathSendPath  = "/api/NIC/NafathSendRequest"
	nafathCheckPath = "/api/NIC/NafathCheckRequest"
)

// Nafath request states. COMPLETED is confirmed by the client's sample; the
// others are Elm's Nafath names (UNCONFIRMED). Any other value is treated as
// still waiting.
const (
	NafathStatusWaiting   = "WAITING"
	NafathStatusCompleted = "COMPLETED"
	NafathStatusRejected  = "REJECTED"
	NafathStatusExpired   = "EXPIRED"
)

// NafathRequest is one NafathSendRequest: the number the person must pick in
// the Nafath app.
type NafathRequest struct {
	TransID string `json:"trans_id"`
	Random  string `json:"random"`
}

// NafathPerson is the identity NafathCheckRequest returns once approved.
type NafathPerson struct {
	IDNumber   string
	FullNameAr string
	FullNameEn string
}

// NafathCheckResult is one NafathCheckRequest answer. Person is set only for
// COMPLETED (and may still be nil if the gateway sent no userInfo).
type NafathCheckResult struct {
	Status    string
	RawStatus string
	Person    *NafathPerson
}

// NafathClient calls the Nafath APIs on the client's Amana gateway.
type NafathClient interface {
	// GetToken runs GetAccessTokenByIdNumber for the National ID.
	GetToken(ctx context.Context, nationalID string) (string, error)
	// SendRequest runs NafathSendRequest: the person gets a Nafath app
	// request showing the returned random number.
	SendRequest(ctx context.Context, token, nationalID string) (*NafathRequest, error)
	// CheckRequest runs NafathCheckRequest for one earlier request.
	CheckRequest(ctx context.Context, token, nationalID string, req NafathRequest) (*NafathCheckResult, error)
}

type nafathClient struct {
	cfg        config.NafathConfig
	baseURL    string
	httpClient *http.Client
}

// NewNafathClient validates cfg against appEnv and returns the client.
// Misconfiguration (mock or skipped TLS verification in production, missing
// credentials, non-HTTPS real endpoint) is an error so startup fails loudly.
func NewNafathClient(cfg config.NafathConfig, appEnv string) (NafathClient, error) {
	production := strings.EqualFold(appEnv, "production") || strings.EqualFold(appEnv, "prod")
	mode := strings.ToLower(strings.TrimSpace(cfg.Mode))
	if mode != "real" && mode != "mock" {
		return nil, fmt.Errorf("NAFATH_MODE must be \"real\" or \"mock\", got %q", cfg.Mode)
	}
	if cfg.IsMock() && production {
		return nil, errors.New("NAFATH_MODE=mock is not allowed when APP_ENV=production")
	}
	if cfg.InsecureSkipVerify && production {
		return nil, errors.New("NAFATH_INSECURE_SKIP_VERIFY=true is not allowed when APP_ENV=production")
	}

	base := strings.TrimRight(strings.TrimSpace(cfg.BaseURL), "/")
	u, err := url.Parse(base)
	if base == "" || err != nil || u.Host == "" || (u.Scheme != "http" && u.Scheme != "https") {
		return nil, fmt.Errorf("NAFATH_BASE_URL %q is not a valid http(s) URL", base)
	}
	if !cfg.IsMock() && u.Scheme != "https" {
		return nil, fmt.Errorf("NAFATH_BASE_URL must use https, got %q", base)
	}
	var missing []string
	for _, f := range []struct{ name, value string }{
		{"NAFATH_SERVICE (or AD_BASEER_SERVICE)", cfg.Service},
		{"NAFATH_KEY (or AD_BASEER_KEY)", cfg.Key},
		{"NAFATH_APP_ID", cfg.AppID},
		{"NAFATH_CHECK_APP_ID", cfg.CheckAppID},
	} {
		if strings.TrimSpace(f.value) == "" {
			missing = append(missing, f.name)
		}
	}
	if len(missing) > 0 {
		return nil, fmt.Errorf("Nafath is enabled but %s not set", strings.Join(missing, ", "))
	}
	for _, id := range []string{cfg.AppID, cfg.CheckAppID} {
		if _, err := strconv.Atoi(strings.TrimSpace(id)); err != nil {
			return nil, fmt.Errorf("NAFATH_APP_ID / NAFATH_CHECK_APP_ID must be numbers, got %q", id)
		}
	}
	if cfg.TimeoutSeconds < 1 || cfg.RotateSeconds < 5 || cfg.LoginTimeoutSeconds < cfg.RotateSeconds {
		return nil, errors.New("NAFATH_TIMEOUT_SECONDS must be >= 1, NAFATH_ROTATE_SECONDS >= 5 and NAFATH_LOGIN_TIMEOUT_SECONDS >= NAFATH_ROTATE_SECONDS")
	}

	transport := http.DefaultTransport.(*http.Transport).Clone()
	transport.TLSClientConfig = &tls.Config{MinVersion: tls.VersionTLS12, InsecureSkipVerify: cfg.InsecureSkipVerify}
	return &nafathClient{
		cfg:     cfg,
		baseURL: base,
		httpClient: &http.Client{
			Timeout:   time.Duration(cfg.TimeoutSeconds) * time.Second,
			Transport: transport,
			// Never follow redirects: they would re-send credentials elsewhere.
			CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse },
		},
	}, nil
}

// nafathEnvelope is the gateway's /api/NIC/* wrapper (field names from the
// client's NafathCheckRequest sample). TransID / Random / ResponseJSON carry
// NafathSendRequest's answer, whose shape is UNCONFIRMED: both the top-level
// fields and Elm's raw answer as a JSON string in responseJSON are accepted.
type nafathEnvelope struct {
	ErrorMessage         json.RawMessage `json:"errorMessage"`
	CheckStatus          ADBaseerValue   `json:"checkStatus"`
	ResponseJSON         json.RawMessage `json:"responseJSON"`
	UserInfo             json.RawMessage `json:"userInfo"`
	DetailedCodeID       int             `json:"detailedCodeID"`
	BusinessErrorMessage json.RawMessage `json:"businessErrorMessage"`
	ExceptionMessage     json.RawMessage `json:"exceptionMessage"`

	TransID      ADBaseerValue `json:"transID"`
	TransIDLower ADBaseerValue `json:"transId"`
	Random       ADBaseerValue `json:"random"`
}

func (e nafathEnvelope) errorText() string {
	for _, raw := range []json.RawMessage{e.BusinessErrorMessage, e.ErrorMessage} {
		if m := rawMessageText(raw); m != "" {
			return m
		}
	}
	return ""
}

func (e nafathEnvelope) describe() string {
	parts := []string{fmt.Sprintf("detailedCodeID %d", e.DetailedCodeID)}
	if m := rawMessageText(e.ErrorMessage); m != "" {
		parts = append(parts, "error: "+m)
	}
	if m := rawMessageText(e.BusinessErrorMessage); m != "" {
		parts = append(parts, "business: "+m)
	}
	if m := rawMessageText(e.ExceptionMessage); m != "" {
		parts = append(parts, "exception: "+m)
	}
	return strings.Join(parts, ", ")
}

// nafathUserInfo is the part of NafathCheckRequest's userInfo Automax uses.
type nafathUserInfo struct {
	ID         ADBaseerValue `json:"id"`
	FullNameAr string        `json:"full_namear"`
	FullNameEn string        `json:"full_nameen"`
}

type nafathTokenResponse struct {
	AccessToken  *string         `json:"accessToken"`
	ErrorMessage json.RawMessage `json:"errorMessage"`
}

// post sends a JSON POST and returns status and body. Transport failures,
// non-JSON bodies and (outside mock mode) simulator responses are reported as
// ErrNafathUnavailable.
func (c *nafathClient) post(ctx context.Context, path, bearer string, payload any) (int, []byte, error) {
	raw, err := json.Marshal(payload)
	if err != nil {
		return 0, nil, err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.baseURL+path, bytes.NewReader(raw))
	if err != nil {
		return 0, nil, err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "application/json")
	if bearer != "" {
		req.Header.Set("Authorization", "Bearer "+bearer)
	}

	resp, err := c.httpClient.Do(req)
	if err != nil {
		// url.Error includes the URL only — the body is never part of it.
		return 0, nil, fmt.Errorf("%w: %s request failed: %v", ErrNafathUnavailable, path, err)
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(io.LimitReader(resp.Body, 256*1024))

	if resp.Header.Get(nafathMockHeader) != "" && !c.cfg.IsMock() {
		return 0, nil, fmt.Errorf("%w: %s answered by nafath-simulator but NAFATH_MODE=real — check NAFATH_BASE_URL", ErrNafathUnavailable, path)
	}
	if resp.StatusCode >= 500 {
		return resp.StatusCode, nil, fmt.Errorf("%w: %s returned %d", ErrNafathUnavailable, path, resp.StatusCode)
	}
	if isNonJSONBody(resp.Header.Get("Content-Type"), body) || len(bytes.TrimSpace(body)) == 0 {
		return resp.StatusCode, nil, fmt.Errorf("%w: %s returned a non-JSON response (status %d, content-type %q) — check NAFATH_BASE_URL",
			ErrNafathUnavailable, path, resp.StatusCode, resp.Header.Get("Content-Type"))
	}
	return resp.StatusCode, body, nil
}

// idNumber returns the National ID as the JSON number the document sends.
func idNumber(nationalID string) (json.Number, error) {
	if _, err := strconv.ParseInt(nationalID, 10, 64); err != nil {
		return "", fmt.Errorf("invalid national ID")
	}
	return json.Number(nationalID), nil
}

func (c *nafathClient) GetToken(ctx context.Context, nationalID string) (string, error) {
	id, err := idNumber(nationalID)
	if err != nil {
		return "", err
	}
	status, body, err := c.post(ctx, nafathTokenPath, "", map[string]any{
		"Service":  c.cfg.Service,
		"Key":      c.cfg.Key,
		"IdNumber": id,
	})
	if err != nil {
		return "", err
	}
	var resp nafathTokenResponse
	if err := json.Unmarshal(body, &resp); err != nil {
		return "", fmt.Errorf("%w: GetAccessTokenByIdNumber response parse failed (status %d): %v", ErrNafathUnavailable, status, err)
	}
	if status == http.StatusUnauthorized {
		// No reason is given: most likely a wrong Service/Key (configuration).
		return "", fmt.Errorf("%w: GetAccessTokenByIdNumber returned 401 (check NAFATH_SERVICE/NAFATH_KEY)", ErrNafathUnavailable)
	}
	if msg := rawMessageText(resp.ErrorMessage); msg != "" {
		return "", &NafathRefusal{Message: c.redact(msg), Detail: fmt.Sprintf("GetAccessTokenByIdNumber returned %d: %s", status, c.redact(msg))}
	}
	if status != http.StatusOK || resp.AccessToken == nil || strings.TrimSpace(*resp.AccessToken) == "" {
		return "", fmt.Errorf("%w: GetAccessTokenByIdNumber returned %d with no accessToken", ErrNafathUnavailable, status)
	}
	return strings.TrimSpace(*resp.AccessToken), nil
}

// nicCall posts to a /api/NIC/* endpoint and decodes the envelope, mapping
// 401 and business errors.
func (c *nafathClient) nicCall(ctx context.Context, path, token string, payload any) (*nafathEnvelope, error) {
	status, body, err := c.post(ctx, path, token, payload)
	if err != nil {
		return nil, err
	}
	if status == http.StatusUnauthorized {
		return nil, fmt.Errorf("%w: %s returned 401", ErrNafathTokenRejected, path)
	}
	var env nafathEnvelope
	if err := json.Unmarshal(body, &env); err != nil {
		return nil, fmt.Errorf("%w: %s response parse failed (status %d): %v", ErrNafathUnavailable, path, status, err)
	}
	if msg := env.errorText(); msg != "" || status != http.StatusOK {
		detail := fmt.Sprintf("%s returned %d, %s", path, status, c.redact(env.describe()))
		if msg == "" {
			return nil, fmt.Errorf("%w: %s", ErrNafathUnavailable, detail)
		}
		return &env, &NafathRefusal{Message: c.redact(msg), Detail: detail}
	}
	return &env, nil
}

func (c *nafathClient) SendRequest(ctx context.Context, token, nationalID string) (*NafathRequest, error) {
	id, err := idNumber(nationalID)
	if err != nil {
		return nil, err
	}
	env, err := c.nicCall(ctx, nafathSendPath, token, map[string]any{
		"appID":    json.Number(strings.TrimSpace(c.cfg.AppID)),
		"idNumber": id,
	})
	if err != nil {
		return nil, err
	}
	req := NafathRequest{TransID: string(env.TransID), Random: string(env.Random)}
	if req.TransID == "" {
		req.TransID = string(env.TransIDLower)
	}
	if req.TransID == "" || req.Random == "" {
		// Elm's raw answer, {"transId": "...", "random": "53"}, as a string.
		var inner struct {
			TransID      ADBaseerValue `json:"transID"`
			TransIDLower ADBaseerValue `json:"transId"`
			Random       ADBaseerValue `json:"random"`
		}
		if text := rawMessageText(env.ResponseJSON); text != "" && json.Unmarshal([]byte(text), &inner) == nil {
			req = NafathRequest{TransID: string(inner.TransID), Random: string(inner.Random)}
			if req.TransID == "" {
				req.TransID = string(inner.TransIDLower)
			}
		}
	}
	if req.TransID == "" || !validRandom(req.Random) {
		return nil, fmt.Errorf("%w: NafathSendRequest returned no usable transID/random (%s)", ErrNafathUnavailable, env.describe())
	}
	return &req, nil
}

// validRandom accepts the 1-3 digit number the Nafath app shows.
func validRandom(r string) bool {
	if len(r) == 0 || len(r) > 3 {
		return false
	}
	_, err := strconv.Atoi(r)
	return err == nil
}

func (c *nafathClient) CheckRequest(ctx context.Context, token, nationalID string, req NafathRequest) (*NafathCheckResult, error) {
	id, err := idNumber(nationalID)
	if err != nil {
		return nil, err
	}
	env, err := c.nicCall(ctx, nafathCheckPath, token, map[string]any{
		"random":   json.Number(req.Random),
		"transID":  req.TransID,
		"idNumber": id,
		"appID":    json.Number(strings.TrimSpace(c.cfg.CheckAppID)),
	})
	if err != nil {
		var refusal *NafathRefusal
		if errors.As(err, &refusal) {
			// A request the gateway no longer knows is not the person's
			// refusal; the caller treats it as expired.
			return nil, fmt.Errorf("%w: %s", ErrNafathUnknownRequest, refusal.Detail)
		}
		return nil, err
	}

	raw := strings.ToUpper(strings.TrimSpace(string(env.CheckStatus)))
	result := &NafathCheckResult{RawStatus: raw}
	switch raw {
	case NafathStatusCompleted, NafathStatusRejected, NafathStatusExpired:
		result.Status = raw
	default:
		result.Status = NafathStatusWaiting
	}
	if result.Status == NafathStatusCompleted {
		var info nafathUserInfo
		if text := bytes.TrimSpace(env.UserInfo); len(text) > 0 && !bytes.Equal(text, []byte("null")) {
			if err := json.Unmarshal(text, &info); err != nil {
				return nil, fmt.Errorf("%w: NafathCheckRequest userInfo parse failed: %v", ErrNafathUnavailable, err)
			}
			result.Person = &NafathPerson{
				IDNumber:   strings.TrimSpace(string(info.ID)),
				FullNameAr: strings.TrimSpace(info.FullNameAr),
				FullNameEn: strings.TrimSpace(info.FullNameEn),
			}
		}
	}
	return result, nil
}

// redact masks the configured credentials in case a response echoes them.
func (c *nafathClient) redact(s string) string {
	for _, secret := range []string{c.cfg.Key, c.cfg.Service} {
		if secret != "" {
			s = strings.ReplaceAll(s, secret, "[redacted]")
		}
	}
	return s
}
