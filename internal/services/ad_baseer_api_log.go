package services

import (
	"bytes"
	"encoding/json"
	"log"
	"strings"
	"time"
)

// Troubleshooting log of every AD-Baseer API call (AD_BASEER_LOG_API,
// default on). Logging only — it never changes a request, a response or how
// the login handles it. Lines:
//
//	[AD-BASEER-API] request  POST https://host/API/Authentication/GetToken step=GetToken bearer=no body={...}
//	[AD-BASEER-API] response POST https://host/API/Authentication/GetToken step=GetToken status=200 took=212ms content-type="application/json" body={...}
//	[AD-BASEER-API] failed   POST https://host/api/ad/IsValidOTP step=IsValidOTP took=15001ms error="... request failed: ... timeout"
//	[AD-BASEER-API] result   StartLogin (GetToken + ValidateCredentialsOTP) user="jdoe" took=640ms outcome=failed error="..."
//
// Masked in bodies: Service, Key, Password, accessToken / tokens and the OTP
// ("***"); employeeInfo values except email, status and statusDesc
// ("[redacted]"). The configured Service / Key are also masked anywhere in
// the text. Email (the username), requestID, isValid, detailedCodeID,
// businessErrorMessage, exceptionMessage, errorMessage and authorizedModules
// are logged as received.

const adBaseerAPILogMaxBody = 8000

var adBaseerLogSecretKeys = map[string]bool{
	"service": true, "key": true, "password": true, "otp": true,
	"accesstoken": true, "access_token": true, "token": true,
	"refreshtoken": true, "refresh_token": true, "authorization": true,
}

// adBaseerLogPersonalKeys hold a real person's details; inside them only
// adBaseerLogPersonalKeep is logged.
var adBaseerLogPersonalKeys = map[string]bool{"employeeinfo": true}

var adBaseerLogPersonalKeep = map[string]bool{"email": true, "status": true, "statusdesc": true}

// adBaseerLogStep names the AD-Baseer API behind a path.
func adBaseerLogStep(path string) string {
	switch path {
	case adBaseerTokenPath:
		return "GetToken"
	case adBaseerValidatePath:
		return "ValidateCredentialsOTP"
	case adBaseerIsValidPath:
		return "IsValidOTP"
	}
	return path
}

func (c *adBaseerClient) logAPIRequest(path string, bearer bool, body []byte) {
	if !c.cfg.LogAPI {
		return
	}
	b := "no"
	if bearer {
		b = "yes"
	}
	log.Printf("[AD-BASEER-API] request  POST %s%s step=%s bearer=%s body=%s",
		c.baseURL, path, adBaseerLogStep(path), b, c.logBody(body))
}

func (c *adBaseerClient) logAPIResponse(path string, status int, took time.Duration, contentType string, body []byte, readErr error) {
	if !c.cfg.LogAPI {
		return
	}
	readNote := ""
	if readErr != nil {
		readNote = " read-error=" + quoteLog(c.redact(readErr.Error()))
	}
	log.Printf("[AD-BASEER-API] response POST %s%s step=%s status=%d took=%dms content-type=%q%s body=%s",
		c.baseURL, path, adBaseerLogStep(path), status, took.Milliseconds(), contentType, readNote, c.logBody(body))
}

// logAPITransportError logs a call that got no HTTP response (timeout, DNS,
// TLS, connection refused...).
func (c *adBaseerClient) logAPITransportError(path string, took time.Duration, err error) {
	if !c.cfg.LogAPI {
		return
	}
	log.Printf("[AD-BASEER-API] failed   POST %s%s step=%s took=%dms error=%s",
		c.baseURL, path, adBaseerLogStep(path), took.Milliseconds(), quoteLog(c.redact(err.Error())))
}

// logAPIOutcome logs how a login step ended; on failure the error names the
// API and the exact check that failed (status, parse, isValid, missing field).
func (c *adBaseerClient) logAPIOutcome(step, username string, took time.Duration, err error) {
	if !c.cfg.LogAPI {
		return
	}
	user := ""
	if username != "" {
		user = " user=" + quoteLog(username)
	}
	if err != nil {
		log.Printf("[AD-BASEER-API] result   %s%s took=%dms outcome=failed error=%s", step, user, took.Milliseconds(), quoteLog(c.redact(err.Error())))
		return
	}
	log.Printf("[AD-BASEER-API] result   %s%s took=%dms outcome=success", step, user, took.Milliseconds())
}

// logBody returns body masked for the log: JSON field by field, anything
// else (e.g. an HTML error page) as one line of text.
func (c *adBaseerClient) logBody(body []byte) string {
	if len(bytes.TrimSpace(body)) == 0 {
		return "(empty)"
	}
	var out string
	dec := json.NewDecoder(bytes.NewReader(body))
	dec.UseNumber()
	var v any
	if err := dec.Decode(&v); err == nil {
		if masked, err := json.Marshal(maskADBaseerLogValue(v, false)); err == nil {
			out = string(masked)
		}
	}
	if out == "" {
		out = strings.Join(strings.Fields(string(body)), " ")
	}
	out = c.redact(out)
	if len(out) > adBaseerAPILogMaxBody {
		out = out[:adBaseerAPILogMaxBody] + "…(truncated)"
	}
	return out
}

// maskADBaseerLogValue masks one decoded JSON value; personal is true inside
// employeeInfo.
func maskADBaseerLogValue(v any, personal bool) any {
	switch t := v.(type) {
	case map[string]any:
		out := make(map[string]any, len(t))
		for k, val := range t {
			lk := strings.ToLower(k)
			switch {
			case val == nil:
				out[k] = nil
			case adBaseerLogSecretKeys[lk]:
				out[k] = "***"
			case personal && adBaseerLogPersonalKeep[lk]:
				out[k] = val
			default:
				out[k] = maskADBaseerLogValue(val, personal || adBaseerLogPersonalKeys[lk])
			}
		}
		return out
	case []any:
		out := make([]any, len(t))
		for i, val := range t {
			out[i] = maskADBaseerLogValue(val, personal)
		}
		return out
	}
	if personal {
		return "[redacted]"
	}
	return v
}

func quoteLog(s string) string {
	b, _ := json.Marshal(s)
	return string(b)
}
