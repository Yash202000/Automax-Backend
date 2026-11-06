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
	"strings"
	"time"

	"github.com/automax/backend/internal/config"
)

// AD-Baseer login errors. Callers map them to user-facing responses; the
// wrapped detail is for server logs only and never contains secrets.
var (
	// ErrADBaseerInvalidCredentials: wrong username/password (or GetToken
	// rejected the employee).
	ErrADBaseerInvalidCredentials = errors.New("ad-baseer: invalid credentials")
	// ErrADBaseerInvalidOTP: IsValidOTP returned isValid=false (wrong, expired
	// or already-used code — the contract does not distinguish them).
	ErrADBaseerInvalidOTP = errors.New("ad-baseer: invalid or expired otp")
	// ErrADBaseerTokenRejected: the access token was refused (401) by a
	// ValidateCredentialsOTP / IsValidOTP call. The login must restart.
	ErrADBaseerTokenRejected = errors.New("ad-baseer: access token rejected")
	// ErrADBaseerUnavailable: timeout, transport failure, 5xx, non-JSON or an
	// unexpected response. Not the user's fault.
	ErrADBaseerUnavailable = errors.New("ad-baseer: service unavailable")
)

// adBaseerMockHeader is set by ad-baseer-simulator on every response.
const adBaseerMockHeader = "X-AD-Baseer-Mock"

const (
	adBaseerTokenPath    = "/API/Authentication/GetToken"
	adBaseerValidatePath = "/api/ad/ValidateCredentialsOTP"
	adBaseerIsValidPath  = "/api/ad/IsValidOTP"
	adBaseerModuleAD     = "AD"
)

// ADBaseerChallenge is the state between the credential and OTP steps.
// AccessToken is AD-Baseer's token — never returned to the browser.
type ADBaseerChallenge struct {
	AccessToken string
	RequestID   string
}

// ADBaseerEmployee is IsValidOTP's employeeInfo.
type ADBaseerEmployee struct {
	FirstName          string        `json:"firstName"`
	FatherName         string        `json:"fatherName"`
	ThirdName          string        `json:"thirdName"`
	LastName           string        `json:"lastName"`
	EmployeeNumber     ADBaseerValue `json:"employeeNumber"`
	FullNameAr         string        `json:"fullNameAr"`
	FullNameEn         string        `json:"fullNameEn"`
	DepartmentName     string        `json:"departmentName"`
	Email              string        `json:"email"`
	DepartmentID       ADBaseerValue `json:"departmentID"`
	MainDepartmentID   ADBaseerValue `json:"mainDepartmentID"`
	MainDepartmentName string        `json:"mainDepartmentName"`
	Status             ADBaseerValue `json:"status"`
	StatusDesc         string        `json:"statusDesc"`
}

// ADBaseerValue is an employeeInfo field the contract shows as "..." (type
// unknown): it accepts a JSON string, number or null.
type ADBaseerValue string

func (v *ADBaseerValue) UnmarshalJSON(b []byte) error {
	trimmed := bytes.TrimSpace(b)
	if bytes.Equal(trimmed, []byte("null")) {
		*v = ""
		return nil
	}
	var str string
	if err := json.Unmarshal(trimmed, &str); err == nil {
		*v = ADBaseerValue(strings.TrimSpace(str))
		return nil
	}
	var num json.Number
	if err := json.Unmarshal(trimmed, &num); err != nil {
		return fmt.Errorf("expected string or number, got %s", trimmed)
	}
	*v = ADBaseerValue(num.String())
	return nil
}

// ADBaseerClient calls the client's AD-Baseer authentication APIs.
type ADBaseerClient interface {
	// StartLogin runs GetToken + ValidateCredentialsOTP, which makes AD-Baseer
	// send the OTP to the employee's registered mobile.
	StartLogin(ctx context.Context, username, password string) (*ADBaseerChallenge, error)
	// VerifyOTP runs IsValidOTP and returns the verified employee.
	VerifyOTP(ctx context.Context, challenge *ADBaseerChallenge, otp string) (*ADBaseerEmployee, error)
}

type adBaseerClient struct {
	cfg        config.ADBaseerConfig
	baseURL    string
	httpClient *http.Client
}

// NewADBaseerClient validates cfg against appEnv and returns the client.
// Misconfiguration (mock or skipped TLS verification in production, missing
// credentials, non-HTTPS real endpoint) is an error so startup fails loudly.
func NewADBaseerClient(cfg config.ADBaseerConfig, appEnv string) (ADBaseerClient, error) {
	production := strings.EqualFold(appEnv, "production") || strings.EqualFold(appEnv, "prod")
	mode := strings.ToLower(strings.TrimSpace(cfg.Mode))
	if mode != "real" && mode != "mock" {
		return nil, fmt.Errorf("AD_BASEER_MODE must be \"real\" or \"mock\", got %q", cfg.Mode)
	}
	if cfg.IsMock() && production {
		return nil, errors.New("AD_BASEER_MODE=mock is not allowed when APP_ENV=production")
	}
	if cfg.InsecureSkipVerify && production {
		return nil, errors.New("AD_BASEER_INSECURE_SKIP_VERIFY=true is not allowed when APP_ENV=production")
	}

	base := strings.TrimRight(strings.TrimSpace(cfg.BaseURL), "/")
	u, err := url.Parse(base)
	if base == "" || err != nil || u.Host == "" || (u.Scheme != "http" && u.Scheme != "https") {
		return nil, fmt.Errorf("AD_BASEER_BASE_URL %q is not a valid http(s) URL", base)
	}
	if !cfg.IsMock() && u.Scheme != "https" {
		return nil, fmt.Errorf("AD_BASEER_BASE_URL must use https, got %q", base)
	}
	var missing []string
	for name, v := range map[string]string{"AD_BASEER_SERVICE": cfg.Service, "AD_BASEER_KEY": cfg.Key, "AD_BASEER_MODULE_ID": cfg.ModuleID} {
		if strings.TrimSpace(v) == "" {
			missing = append(missing, name)
		}
	}
	if len(missing) > 0 {
		return nil, fmt.Errorf("AD-Baseer is enabled but %s not set", strings.Join(missing, ", "))
	}
	if cfg.OTPMaxAttempts < 1 || cfg.OTPTTLSeconds < 30 || cfg.TimeoutSeconds < 1 {
		return nil, errors.New("AD_BASEER_OTP_MAX_ATTEMPTS must be >= 1, AD_BASEER_OTP_TTL_SECONDS >= 30 and AD_BASEER_TIMEOUT_SECONDS >= 1")
	}

	transport := http.DefaultTransport.(*http.Transport).Clone()
	transport.TLSClientConfig = &tls.Config{MinVersion: tls.VersionTLS12, InsecureSkipVerify: cfg.InsecureSkipVerify}
	return &adBaseerClient{
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

// adBaseerResult is the common tail of the /api/ad/* responses.
type adBaseerResult struct {
	DetailedCodeID       int             `json:"detailedCodeID"`
	BusinessErrorMessage json.RawMessage `json:"businessErrorMessage"`
	ExceptionMessage     json.RawMessage `json:"exceptionMessage"`
}

func (r adBaseerResult) describe() string {
	parts := []string{fmt.Sprintf("detailedCodeID %d", r.DetailedCodeID)}
	if m := rawMessageText(r.BusinessErrorMessage); m != "" {
		parts = append(parts, "business: "+m)
	}
	if m := rawMessageText(r.ExceptionMessage); m != "" {
		parts = append(parts, "exception: "+m)
	}
	return strings.Join(parts, ", ")
}

type adBaseerTokenResponse struct {
	AccessToken       *string  `json:"accessToken"`
	AuthorizedModules []string `json:"authorizedModules"`
	ErrorMessage      *string  `json:"errorMessage"`
}

type adBaseerValidateResponse struct {
	IsValid   bool   `json:"isValid"`
	RequestID string `json:"requestID"`
	adBaseerResult
}

type adBaseerIsValidResponse struct {
	IsValid      bool              `json:"isValid"`
	EmployeeInfo *ADBaseerEmployee `json:"employeeInfo"`
	adBaseerResult
}

// post sends a JSON POST and returns status and body. Transport failures,
// non-JSON bodies and (outside mock mode) simulator responses are reported as
// ErrADBaseerUnavailable.
func (c *adBaseerClient) post(ctx context.Context, path, bearer string, payload any) (int, []byte, error) {
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
		return 0, nil, fmt.Errorf("%w: %s request failed: %v", ErrADBaseerUnavailable, path, err)
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(io.LimitReader(resp.Body, 64*1024))

	if resp.Header.Get(adBaseerMockHeader) != "" && !c.cfg.IsMock() {
		return 0, nil, fmt.Errorf("%w: %s answered by ad-baseer-simulator but AD_BASEER_MODE=real — check AD_BASEER_BASE_URL", ErrADBaseerUnavailable, path)
	}
	if resp.StatusCode >= 500 {
		return resp.StatusCode, nil, fmt.Errorf("%w: %s returned %d", ErrADBaseerUnavailable, path, resp.StatusCode)
	}
	if isNonJSONBody(resp.Header.Get("Content-Type"), body) || len(bytes.TrimSpace(body)) == 0 {
		return resp.StatusCode, nil, fmt.Errorf("%w: %s returned a non-JSON response (status %d, content-type %q) — check the AD-Baseer URL",
			ErrADBaseerUnavailable, path, resp.StatusCode, resp.Header.Get("Content-Type"))
	}
	return resp.StatusCode, body, nil
}

func (c *adBaseerClient) StartLogin(ctx context.Context, username, password string) (*ADBaseerChallenge, error) {
	// 1. GetToken. The contract sends the employee's username as "Email".
	status, body, err := c.post(ctx, adBaseerTokenPath, "", map[string]string{
		"Service": c.cfg.Service,
		"Key":     c.cfg.Key,
		"Email":   username,
	})
	if err != nil {
		return nil, err
	}
	var tokenResp adBaseerTokenResponse
	if err := json.Unmarshal(body, &tokenResp); err != nil {
		return nil, fmt.Errorf("%w: GetToken response parse failed (status %d): %v", ErrADBaseerUnavailable, status, err)
	}
	if status == http.StatusUnauthorized {
		// The contract's 401 carries no reason: it is either a wrong
		// Service/Key (configuration) or an employee GetToken won't issue for.
		return nil, fmt.Errorf("%w: GetToken returned 401 (wrong AD_BASEER_SERVICE/KEY, or employee not recognised)", ErrADBaseerInvalidCredentials)
	}
	if status != http.StatusOK {
		msg := ""
		if tokenResp.ErrorMessage != nil {
			msg = c.redact(*tokenResp.ErrorMessage)
		}
		return nil, fmt.Errorf("%w: GetToken returned %d: %s", ErrADBaseerUnavailable, status, msg)
	}
	if tokenResp.ErrorMessage != nil && strings.TrimSpace(*tokenResp.ErrorMessage) != "" {
		return nil, fmt.Errorf("%w: GetToken rejected: %s", ErrADBaseerUnavailable, c.redact(*tokenResp.ErrorMessage))
	}
	if tokenResp.AccessToken == nil || strings.TrimSpace(*tokenResp.AccessToken) == "" {
		return nil, fmt.Errorf("%w: GetToken response contained no accessToken", ErrADBaseerUnavailable)
	}
	if !containsFold(tokenResp.AuthorizedModules, adBaseerModuleAD) {
		return nil, fmt.Errorf("%w: GetToken authorizedModules %v does not include %q", ErrADBaseerUnavailable, tokenResp.AuthorizedModules, adBaseerModuleAD)
	}
	token := strings.TrimSpace(*tokenResp.AccessToken)

	// 2. ValidateCredentialsOTP.
	status, body, err = c.post(ctx, adBaseerValidatePath, token, map[string]string{
		"Email":    username,
		"Password": password,
		"ModuleID": c.cfg.ModuleID,
	})
	if err != nil {
		return nil, err
	}
	if status == http.StatusUnauthorized {
		return nil, fmt.Errorf("%w: ValidateCredentialsOTP refused a fresh token", ErrADBaseerUnavailable)
	}
	var validateResp adBaseerValidateResponse
	if err := json.Unmarshal(body, &validateResp); err != nil {
		return nil, fmt.Errorf("%w: ValidateCredentialsOTP response parse failed (status %d): %v", ErrADBaseerUnavailable, status, err)
	}
	if !validateResp.IsValid {
		if validateResp.DetailedCodeID == 1001 {
			return nil, fmt.Errorf("%w: %s", ErrADBaseerInvalidCredentials, validateResp.describe())
		}
		// e.g. 1000 "Invalid Request data": our request (ModuleID, fields) is
		// wrong, not the user's password.
		return nil, fmt.Errorf("%w: ValidateCredentialsOTP returned %d, %s", ErrADBaseerUnavailable, status, validateResp.describe())
	}
	if status != http.StatusOK || strings.TrimSpace(validateResp.RequestID) == "" {
		return nil, fmt.Errorf("%w: ValidateCredentialsOTP returned %d with isValid=true but no requestID", ErrADBaseerUnavailable, status)
	}
	return &ADBaseerChallenge{AccessToken: token, RequestID: strings.TrimSpace(validateResp.RequestID)}, nil
}

func (c *adBaseerClient) VerifyOTP(ctx context.Context, challenge *ADBaseerChallenge, otp string) (*ADBaseerEmployee, error) {
	status, body, err := c.post(ctx, adBaseerIsValidPath, challenge.AccessToken, map[string]string{
		"otp":       otp,
		"requestID": challenge.RequestID,
	})
	if err != nil {
		return nil, err
	}
	if status == http.StatusUnauthorized {
		return nil, fmt.Errorf("%w: IsValidOTP returned 401", ErrADBaseerTokenRejected)
	}
	var resp adBaseerIsValidResponse
	if err := json.Unmarshal(body, &resp); err != nil {
		return nil, fmt.Errorf("%w: IsValidOTP response parse failed (status %d): %v", ErrADBaseerUnavailable, status, err)
	}
	if !resp.IsValid {
		if status == http.StatusOK || status == http.StatusBadRequest {
			return nil, fmt.Errorf("%w: %s", ErrADBaseerInvalidOTP, resp.describe())
		}
		return nil, fmt.Errorf("%w: IsValidOTP returned %d, %s", ErrADBaseerUnavailable, status, resp.describe())
	}
	if status != http.StatusOK || resp.EmployeeInfo == nil {
		return nil, fmt.Errorf("%w: IsValidOTP returned %d with isValid=true but no employeeInfo", ErrADBaseerUnavailable, status)
	}
	return resp.EmployeeInfo, nil
}

// redact masks the configured credentials in case a response echoes them.
func (c *adBaseerClient) redact(s string) string {
	for _, secret := range []string{c.cfg.Key, c.cfg.Service} {
		if secret != "" {
			s = strings.ReplaceAll(s, secret, "[redacted]")
		}
	}
	return s
}

func containsFold(list []string, want string) bool {
	for _, v := range list {
		if strings.EqualFold(strings.TrimSpace(v), want) {
			return true
		}
	}
	return false
}
