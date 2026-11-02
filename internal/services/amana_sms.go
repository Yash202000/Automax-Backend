package services

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"strings"
	"time"

	"github.com/automax/backend/internal/config"
	"github.com/google/uuid"
)

// amanaSMSSendPath is Amana's template-based SMS endpoint on MOMRAConfig.BaseURL.
const amanaSMSSendPath = "/api/Notifications/SendSMSByTemplateID"

// AmanaSMSResult is what Amana answered for one send; kept for the audit trail in notification_logs.
type AmanaSMSResult struct {
	MessageID  string
	StatusCode int
	Response   string // trimmed, secret-safe excerpt of the response body
}

// SMSTemplateSender sends an SMS through an Amana-hosted template.
type SMSTemplateSender interface {
	SendSMSByTemplateID(ctx context.Context, mobile string, templateID int, variables []string) (string, error)
	// SendSMSByTemplateIDDetailed is SendSMSByTemplateID plus the provider status/response for auditing.
	// The result is non-nil whenever Amana was actually reached, even if the call failed.
	SendSMSByTemplateIDDetailed(ctx context.Context, mobile string, templateID int, variables []string) (*AmanaSMSResult, error)
}

// smsLogIDKey carries the notification_logs row id of the current send so every log line can be correlated.
type smsLogIDKey struct{}

func withSMSLogID(ctx context.Context, id uuid.UUID) context.Context {
	return context.WithValue(ctx, smsLogIDKey{}, id)
}

// smsLogTag renders "log_id=<uuid>" for log lines ("-" outside a tracked send).
func smsLogTag(ctx context.Context) string {
	if id, ok := ctx.Value(smsLogIDKey{}).(uuid.UUID); ok {
		return "log_id=" + id.String()
	}
	return "log_id=-"
}

// NewAmanaSMSSender reuses the MOMRA base URL, credentials and token flow (own token cache).
func NewAmanaSMSSender(cfg config.MOMRAConfig) SMSTemplateSender {
	return NewMOMRAClient(cfg).(*momraClient)
}

// SendSMSByTemplateID posts {MobileNumber, TemplateID, Variables} and returns the provider message id when present.
func (c *momraClient) SendSMSByTemplateID(ctx context.Context, mobile string, templateID int, variables []string) (string, error) {
	res, err := c.SendSMSByTemplateIDDetailed(ctx, mobile, templateID, variables)
	if res == nil {
		return "", err
	}
	return res.MessageID, err
}

// SendSMSByTemplateIDDetailed posts the template SMS and returns message id, HTTP status and response excerpt.
// On a 401 the cached token is dropped and the call is retried once.
func (c *momraClient) SendSMSByTemplateIDDetailed(ctx context.Context, mobile string, templateID int, variables []string) (*AmanaSMSResult, error) {
	if variables == nil {
		variables = []string{}
	}
	payload, err := json.Marshal(map[string]any{
		"MobileNumber": mobile,
		"TemplateID":   templateID,
		"Variables":    variables,
	})
	if err != nil {
		return nil, err
	}

	maskedMobile := maskMobile(mobile)
	tag := smsLogTag(ctx)
	for attempt := 0; attempt < 2; attempt++ {
		token, err := c.getToken(ctx)
		if err != nil {
			log.Printf("[SMS] provider=amana stage=token FAILED %s to=%s template_id=%d: %v", tag, maskedMobile, templateID, err)
			return nil, err
		}
		req, err := http.NewRequestWithContext(ctx, http.MethodPost, strings.TrimRight(c.cfg.BaseURL, "/")+amanaSMSSendPath, bytes.NewReader(payload))
		if err != nil {
			return nil, err
		}
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("Authorization", "Bearer "+token)

		log.Printf("[SMS] provider=amana stage=request %s POST %s to=%s template_id=%d variables=%d attempt=%d", tag, amanaSMSSendPath, maskedMobile, templateID, len(variables), attempt+1)
		start := time.Now()
		resp, err := c.httpClient.Do(req)
		if err != nil {
			log.Printf("[SMS] provider=amana stage=request FAILED %s to=%s template_id=%d after %s: %v", tag, maskedMobile, templateID, time.Since(start), err)
			return nil, fmt.Errorf("amana sms request failed: %w", err)
		}
		body, _ := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
		resp.Body.Close()
		res := &AmanaSMSResult{StatusCode: resp.StatusCode, Response: c.safeExcerpt(body)}
		log.Printf("[SMS] provider=amana stage=response %s status=%d in %s to=%s template_id=%d", tag, resp.StatusCode, time.Since(start), maskedMobile, templateID)

		if resp.StatusCode == http.StatusUnauthorized && attempt == 0 {
			log.Printf("[SMS] provider=amana stage=response %s 401 unauthorized — dropping cached token and retrying once", tag)
			c.tokenMu.Lock()
			c.cachedToken = ""
			c.tokenMu.Unlock()
			continue
		}
		if resp.StatusCode < 200 || resp.StatusCode > 299 {
			log.Printf("[SMS] provider=amana stage=response FAILED %s status=%d to=%s template_id=%d body=%s", tag, resp.StatusCode, maskedMobile, templateID, res.Response)
			return res, fmt.Errorf("amana sms request returned %d: %s", resp.StatusCode, res.Response)
		}
		id, err := parseAmanaSMSResponse(body)
		if err != nil {
			log.Printf("[SMS] provider=amana stage=response REJECTED %s to=%s template_id=%d: %v", tag, maskedMobile, templateID, err)
			return res, err
		}
		res.MessageID = id
		log.Printf("[SMS] provider=amana stage=done SENT OK %s to=%s template_id=%d message_id=%q", tag, maskedMobile, templateID, id)
		return res, nil
	}
	return nil, fmt.Errorf("amana sms request unauthorized")
}

// parseAmanaSMSResponse surfaces explicit failure flags and extracts a message id if one is returned.
func parseAmanaSMSResponse(body []byte) (string, error) {
	var m map[string]any
	if json.Unmarshal(body, &m) != nil {
		return "", nil
	}
	id := ""
	for k, v := range m {
		switch strings.ToLower(k) {
		case "issuccess", "success":
			if ok, isBool := v.(bool); isBool && !ok {
				return "", fmt.Errorf("amana sms rejected: %s", string(body))
			}
		case "messageid", "smsid":
			id = fmt.Sprint(v)
		}
	}
	return id, nil
}

// ParseTemplateVariables turns a template's Variables column into an ordered list of names.
// Accepts a JSON array (["a","b"]) or a comma/semicolon/newline separated list; {{ }} wrappers are stripped.
func ParseTemplateVariables(raw string) []string {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return nil
	}
	var names []string
	if strings.HasPrefix(raw, "[") && json.Unmarshal([]byte(raw), &names) != nil {
		names = nil
	}
	if names == nil {
		names = strings.FieldsFunc(raw, func(r rune) bool { return r == ',' || r == '\n' || r == ';' })
	}
	out := make([]string, 0, len(names))
	for _, n := range names {
		n = strings.TrimSpace(n)
		n = strings.TrimSpace(strings.TrimSuffix(strings.TrimPrefix(n, "{{"), "}}"))
		n = strings.TrimPrefix(n, ".")
		if n != "" {
			out = append(out, n)
		}
	}
	return out
}

// BuildAmanaVariables orders values by the template's variable list and fails if any is missing or empty.
func BuildAmanaVariables(names []string, vars map[string]string) ([]string, error) {
	lower := make(map[string]string, len(vars))
	for k, v := range vars {
		lower[strings.ToLower(k)] = v
	}
	out := make([]string, 0, len(names))
	var missing []string
	for _, n := range names {
		v, ok := lower[strings.ToLower(n)]
		if !ok || strings.TrimSpace(v) == "" {
			missing = append(missing, n)
			continue
		}
		out = append(out, v)
	}
	if len(missing) > 0 {
		return nil, fmt.Errorf("missing required SMS template variables: %s", strings.Join(missing, ", "))
	}
	return out, nil
}

// ExtractPlaceholders lists {{placeholders}} of a template body in order of appearance
// (duplicates kept), used as the Amana variable order when the template has no explicit variables list.
func ExtractPlaceholders(body string) []string {
	matches := placeholderRe.FindAllStringSubmatch(body, -1)
	out := make([]string, 0, len(matches))
	for _, m := range matches {
		out = append(out, m[1])
	}
	return out
}

// maskMobile keeps only the last 3 digits so logs never carry a full phone number.
func maskMobile(m string) string {
	if len(m) <= 3 {
		return "***"
	}
	return strings.Repeat("*", len(m)-3) + m[len(m)-3:]
}

// maskPhones applies maskMobile to each number for safe logging.
func maskPhones(phones []string) []string {
	out := make([]string, len(phones))
	for i, p := range phones {
		out[i] = maskMobile(p)
	}
	return out
}
