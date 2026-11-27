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
	// ErrNafathUnavailable: Nafath gave no answer at all (timeout,
	// connection failure) or the configuration is wrong. Whenever Nafath
	// answers, its own answer is returned as a NafathRefusal instead.
	ErrNafathUnavailable = errors.New("nafath: service unavailable")
)

// NafathRefusal carries the gateway's own message for ErrNafathRefused.
type NafathRefusal struct {
	Message string
	Detail  string
	// TokenRejected: the gateway answered 401 with this message, so the
	// error also matches ErrNafathTokenRejected (get a new token, retry).
	TokenRejected bool
}

func (e *NafathRefusal) Error() string { return "nafath: request refused: " + e.Detail }
func (e *NafathRefusal) Unwrap() []error {
	if e.TokenRejected {
		return []error{ErrNafathRefused, ErrNafathTokenRejected}
	}
	return []error{ErrNafathRefused}
}

// nafathAnswer is Nafath's own answer as a refusal, shown to the user as it
// is: its message (errorMessage / businessErrorMessage / exceptionMessage),
// else its body when that is plain text, else its HTTP status line (e.g.
// "400 Bad Request"). Automax adds no text of its own.
func (c *nafathClient) nafathAnswer(path string, status int, body []byte, msg string, tokenRejected bool) *NafathRefusal {
	text := strings.TrimSpace(msg)
	if text == "" {
		// A gateway-level answer such as {"statusCode":401,"message":"Invalid Token."}.
		var plain struct {
			Message json.RawMessage `json:"message"`
			Title   json.RawMessage `json:"title"`
		}
		if json.Unmarshal(body, &plain) == nil {
			if text = rawMessageText(plain.Message); text == "" {
				text = rawMessageText(plain.Title)
			}
		}
	}
	if text == "" {
		if b := strings.TrimSpace(string(body)); b != "" && !strings.HasPrefix(b, "{") && !strings.HasPrefix(b, "[") && !strings.HasPrefix(b, "<") {
			text = b
		}
	}
	if text == "" {
		text = strings.TrimSpace(fmt.Sprintf("%d %s", status, http.StatusText(status)))
	}
	text = c.redact(text)
	return &NafathRefusal{Message: text, Detail: fmt.Sprintf("%s returned %d: %s", path, status, text), TokenRejected: tokenRejected}
}

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
	// Message is the gateway's own text sent with the status (shown to the
	// user for REJECTED / EXPIRED), empty if none.
	Message string
	Person  *NafathPerson
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
	if cfg.TimeoutSeconds < 1 || cfg.PollSeconds < 1 {
		return nil, errors.New("NAFATH_TIMEOUT_SECONDS and NAFATH_POLL_SECONDS must be >= 1")
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
	for _, raw := range []json.RawMessage{e.BusinessErrorMessage, e.ErrorMessage, e.ExceptionMessage} {
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

	c.logAPIRequest(path, bearer != "", raw)
	started := time.Now()
	resp, err := c.httpClient.Do(req)
	if err != nil {
		// url.Error includes the URL only — the body is never part of it.
		err = fmt.Errorf("%w: %s request failed: %v", ErrNafathUnavailable, path, err)
		c.logAPIResponse(path, 0, time.Since(started), nil, err)
		return 0, nil, err
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(io.LimitReader(resp.Body, 256*1024))
	c.logAPIResponse(path, resp.StatusCode, time.Since(started), body, nil)

	if resp.Header.Get(nafathMockHeader) != "" && !c.cfg.IsMock() {
		return 0, nil, fmt.Errorf("%w: %s answered by nafath-simulator but NAFATH_MODE=real — check NAFATH_BASE_URL", ErrNafathUnavailable, path)
	}
	// Nafath answered with an error status or a body Automax cannot read:
	// pass on what Nafath said (its message, text or HTTP status).
	if resp.StatusCode >= 500 || isNonJSONBody(resp.Header.Get("Content-Type"), body) || len(bytes.TrimSpace(body)) == 0 {
		var env nafathEnvelope
		_ = json.Unmarshal(body, &env)
		return resp.StatusCode, nil, c.nafathAnswer(path, resp.StatusCode, body, env.errorText(), false)
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
		return "", c.nafathAnswer(nafathTokenPath, status, body, "", false)
	}
	// The gateway's own message is shown to the user as it is; without one,
	// its HTTP status (a 401 here usually means a wrong Service/Key).
	if msg := rawMessageText(resp.ErrorMessage); msg != "" || status != http.StatusOK || resp.AccessToken == nil || strings.TrimSpace(*resp.AccessToken) == "" {
		return "", c.nafathAnswer(nafathTokenPath, status, body, msg, false)
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
	var env nafathEnvelope
	if status == http.StatusUnauthorized {
		// Keep the gateway's message (e.g. "token expired") so it can be
		// shown if a new token does not help either.
		_ = json.Unmarshal(body, &env)
		return nil, c.nafathAnswer(path, status, body, env.errorText(), true)
	}
	if err := json.Unmarshal(body, &env); err != nil {
		return nil, c.nafathAnswer(path, status, body, "", false)
	}
	// A checkStatus is Nafath's answer whatever the HTTP status: the UAT
	// gateway sends WAITING with 400 while the user has not approved yet.
	// The caller (CheckRequest) decides from checkStatus.
	if strings.TrimSpace(string(env.CheckStatus)) != "" {
		return &env, nil
	}
	if msg := env.errorText(); msg != "" || status != http.StatusOK {
		return &env, c.nafathAnswer(path, status, body, msg, false)
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
		// Nafath answered without a request number: show what it answered.
		return nil, c.nafathAnswer(nafathSendPath, http.StatusOK, nil, env.errorText(), false)
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
	// checkStatus decides. A message next to a status (e.g. WAITING with
	// "request pending") is information, not a failure. A message with no
	// status is Nafath's refusal and is returned as one.
	var refusal *NafathRefusal
	if errors.As(err, &refusal) && env != nil && strings.TrimSpace(string(env.CheckStatus)) != "" {
		err = nil
	}
	if err != nil {
		return nil, err
	}

	raw := strings.ToUpper(strings.TrimSpace(string(env.CheckStatus)))
	result := &NafathCheckResult{RawStatus: raw, Message: c.redact(env.errorText())}
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
