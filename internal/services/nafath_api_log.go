package services

import (
	"bytes"
	"encoding/json"
	"log"
	"regexp"
	"strings"
	"time"
)

// Evidence log of every Amana gateway call made for Nafath login
// (NAFATH_LOG_API, default on). One line per request and one per response:
//
//	[NAFATH-API] request  POST /api/NIC/NafathCheckRequest bearer=yes body={...}
//	[NAFATH-API] response POST /api/NIC/NafathCheckRequest status=200 took=212ms body={...}
//
// Bodies are logged with secrets and personal data masked:
//   - Service, Key, tokens: "***"
//   - National IDs (10 digits starting 1 or 2), anywhere: "******1234"
//   - userInfo / person / employeeList / userDelegations: field names kept,
//     values "[redacted]" (names, emails, employee data of a real person)
//
// checkStatus, errorMessage, businessErrorMessage, exceptionMessage,
// detailedCodeID, transID and random are logged as received.

const nafathAPILogMaxBody = 8000

var (
	nafathLogIDPattern     = regexp.MustCompile(`^[12][0-9]{9}$`)
	nafathLogIDInTextRegex = regexp.MustCompile(`\b[12][0-9]{9}\b`)
)

var nafathLogSecretKeys = map[string]bool{
	"service": true, "key": true, "accesstoken": true, "access_token": true,
	"token": true, "refreshtoken": true, "refresh_token": true, "authorization": true, "password": true,
}

var nafathLogPersonalKeys = map[string]bool{
	"userinfo": true, "person": true, "employeelist": true, "userdelegations": true,
}

func (c *nafathClient) logAPIRequest(path string, bearer bool, body []byte) {
	if !c.cfg.LogAPI {
		return
	}
	b := "no"
	if bearer {
		b = "yes"
	}
	log.Printf("[NAFATH-API] request  POST %s bearer=%s body=%s", path, b, c.logBody(body))
}

func (c *nafathClient) logAPIResponse(path string, status int, took time.Duration, body []byte, err error) {
	if !c.cfg.LogAPI {
		return
	}
	ms := took.Milliseconds()
	if err != nil {
		log.Printf("[NAFATH-API] response POST %s error=%q took=%dms", path, c.redact(err.Error()), ms)
		return
	}
	log.Printf("[NAFATH-API] response POST %s status=%d took=%dms body=%s", path, status, ms, c.logBody(body))
}

// logBody returns the body masked for the log: JSON is masked field by
// field; anything else (e.g. an HTML error page) is logged as text with
// secrets and National IDs masked.
func (c *nafathClient) logBody(body []byte) string {
	var out string
	dec := json.NewDecoder(bytes.NewReader(body))
	dec.UseNumber()
	var v any
	if err := dec.Decode(&v); err == nil {
		if masked, err := json.Marshal(maskNafathLogValue(v, false)); err == nil {
			out = string(masked)
		}
	}
	if out == "" {
		out = strings.Join(strings.Fields(string(body)), " ")
		out = nafathLogIDInTextRegex.ReplaceAllStringFunc(out, maskNafathLogID)
	}
	out = c.redact(out)
	if len(out) > nafathAPILogMaxBody {
		out = out[:nafathAPILogMaxBody] + "…(truncated)"
	}
	return out
}

// maskNafathLogValue masks one decoded JSON value. personal is true inside
// userInfo / person / employeeList / userDelegations.
func maskNafathLogValue(v any, personal bool) any {
	switch t := v.(type) {
	case map[string]any:
		out := make(map[string]any, len(t))
		for k, val := range t {
			lk := strings.ToLower(k)
			switch {
			case val == nil:
				out[k] = nil
			case nafathLogSecretKeys[lk]:
				out[k] = "***"
			case lk == "responsejson":
				out[k] = maskNafathLogEmbedded(val, personal)
			default:
				out[k] = maskNafathLogValue(val, personal || nafathLogPersonalKeys[lk])
			}
		}
		return out
	case []any:
		out := make([]any, len(t))
		for i, val := range t {
			out[i] = maskNafathLogValue(val, personal)
		}
		return out
	case json.Number:
		if nafathLogIDPattern.MatchString(t.String()) {
			return maskNafathLogID(t.String())
		}
		if personal {
			return "[redacted]"
		}
		return t
	case string:
		if nafathLogIDPattern.MatchString(strings.TrimSpace(t)) {
			return maskNafathLogID(strings.TrimSpace(t))
		}
		if personal {
			return "[redacted]"
		}
		return t
	case bool:
		if personal {
			return "[redacted]"
		}
		return t
	}
	return v
}

// maskNafathLogEmbedded masks responseJSON, which the gateway may send as a
// JSON document inside a string.
func maskNafathLogEmbedded(v any, personal bool) any {
	s, ok := v.(string)
	if !ok {
		return maskNafathLogValue(v, personal)
	}
	dec := json.NewDecoder(strings.NewReader(s))
	dec.UseNumber()
	var inner any
	if err := dec.Decode(&inner); err != nil {
		return maskNafathLogValue(s, personal)
	}
	return maskNafathLogValue(inner, personal)
}

func maskNafathLogID(id string) string {
	return "******" + id[len(id)-4:]
}
