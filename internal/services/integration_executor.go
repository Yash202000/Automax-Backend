package services

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/automax/backend/internal/models"
	"github.com/automax/backend/internal/repository"
	"github.com/dop251/goja"
	"github.com/google/uuid"
)

// IntegrationExecutor runs integration scripts triggered by state or transition events.
type IntegrationExecutor interface {
	// RunStateTriggers fires all active triggers on the given state for the given event ("enter" or "exit").
	RunStateTriggers(ctx context.Context, incident *models.Incident, stateID uuid.UUID, stateName string, event string)
	// RunTransitionTriggers fires all active triggers on the given transition.
	RunTransitionTriggers(ctx context.Context, incident *models.Incident, transitionID uuid.UUID, transitionName string)
	// RunScriptForTest executes a script once against a real incident for dry-run/testing.
	RunScriptForTest(ctx context.Context, script *models.IntegrationScript, incident *models.Incident, fieldMappings string) *models.IntegrationExecutionLog
}

type integrationExecutor struct {
	integrationRepo repository.IntegrationRepository
	integrationSvc  IntegrationService
	httpClient      *http.Client
}

func NewIntegrationExecutor(integrationRepo repository.IntegrationRepository, integrationSvc IntegrationService) IntegrationExecutor {
	return &integrationExecutor{
		integrationRepo: integrationRepo,
		integrationSvc:  integrationSvc,
		httpClient: &http.Client{
			Timeout: 30 * time.Second,
		},
	}
}

func (e *integrationExecutor) RunStateTriggers(ctx context.Context, incident *models.Incident, stateID uuid.UUID, stateName string, event string) {
	triggers, err := e.integrationRepo.ListActiveStateTriggersForEvent(ctx, stateID, event)
	if err != nil {
		log.Printf("[integration] failed to load state triggers for state %s: %v", stateID, err)
		return
	}
	for _, trigger := range triggers {
		if trigger.IntegrationScript == nil || !trigger.IntegrationScript.IsActive {
			continue
		}
		if !e.classificationMatches(incident, trigger.Classifications) {
			continue
		}
		triggerType := "state_" + event
		triggerName := fmt.Sprintf("%s (%s)", stateName, event)
		t := trigger // capture loop var
		if t.IsAsync {
			go e.execute(context.Background(), incident, t.IntegrationScript, t.FieldMappings, triggerType, stateID, triggerName)
		} else {
			e.execute(ctx, incident, t.IntegrationScript, t.FieldMappings, triggerType, stateID, triggerName)
		}
	}
}

func (e *integrationExecutor) RunTransitionTriggers(ctx context.Context, incident *models.Incident, transitionID uuid.UUID, transitionName string) {
	triggers, err := e.integrationRepo.ListActiveTransitionTriggers(ctx, transitionID)
	if err != nil {
		log.Printf("[integration] failed to load transition triggers for transition %s: %v", transitionID, err)
		return
	}
	for _, trigger := range triggers {
		if trigger.IntegrationScript == nil || !trigger.IntegrationScript.IsActive {
			continue
		}
		if !e.classificationMatches(incident, trigger.Classifications) {
			continue
		}
		t := trigger
		if t.IsAsync {
			go e.execute(context.Background(), incident, t.IntegrationScript, t.FieldMappings, "transition", transitionID, transitionName)
		} else {
			e.execute(ctx, incident, t.IntegrationScript, t.FieldMappings, "transition", transitionID, transitionName)
		}
	}
}

func (e *integrationExecutor) RunScriptForTest(ctx context.Context, script *models.IntegrationScript, incident *models.Incident, fieldMappings string) *models.IntegrationExecutionLog {
	return e.execute(ctx, incident, script, fieldMappings, "test", uuid.Nil, "test")
}

// classificationMatches returns true if:
//   - the trigger has no classification filter (all pass), OR
//   - the incident's classification exactly matches one of the trigger's classifications, OR
//   - the incident's classification path contains one of the trigger's classification IDs
//     (hierarchical: selecting a parent covers all its children)
func (e *integrationExecutor) classificationMatches(incident *models.Incident, filterClasses []models.Classification) bool {
	if len(filterClasses) == 0 {
		return true
	}
	if incident.ClassificationID == nil {
		return false
	}
	incidentClassPath := ""
	if incident.Classification != nil {
		incidentClassPath = incident.Classification.Path
	}
	for _, fc := range filterClasses {
		// Exact match
		if *incident.ClassificationID == fc.ID {
			return true
		}
		// Hierarchical match: incident's path starts with the filter classification's ID segment
		if incidentClassPath != "" {
			idStr := fc.ID.String()
			if strings.Contains(incidentClassPath, idStr) {
				return true
			}
		}
	}
	return false
}

func (e *integrationExecutor) execute(
	ctx context.Context,
	incident *models.Incident,
	script *models.IntegrationScript,
	fieldMappings string,
	triggerType string,
	triggerRefID uuid.UUID,
	triggerRefName string,
) *models.IntegrationExecutionLog {
	logEntry := &models.IntegrationExecutionLog{
		IntegrationScriptID: script.ID,
		IncidentID:          incident.ID,
		IncidentNumber:      incident.IncidentNumber,
		TriggerType:         triggerType,
		TriggerRefID:        triggerRefID,
		TriggerRefName:      triggerRefName,
		Status:              "running",
		ExecutedAt:          time.Now(),
	}
	_ = e.integrationRepo.CreateExecutionLog(ctx, logEntry)

	start := time.Now()
	var execErr error
	var requestPayload, responseBody string
	var statusCode int

	vars, err := e.integrationSvc.ResolveVariables(ctx)
	if err != nil {
		log.Printf("[integration] failed to resolve variables: %v", err)
		vars = map[string]string{}
	}

	incidentCtx := e.buildIncidentContext(incident, fieldMappings)

	switch script.ScriptType {
	case "http_request":
		requestPayload, responseBody, statusCode, execErr = e.executeHTTP(ctx, script, incidentCtx, vars)
	case "javascript":
		requestPayload, responseBody, execErr = e.executeJS(script, incidentCtx, vars)
	default:
		execErr = fmt.Errorf("unknown script_type: %s", script.ScriptType)
	}

	durationMs := time.Since(start).Milliseconds()
	now := time.Now()
	logEntry.DurationMs = durationMs
	logEntry.CompletedAt = &now
	logEntry.RequestPayload = requestPayload
	logEntry.ResponseBody = responseBody
	logEntry.StatusCode = statusCode

	if execErr != nil {
		logEntry.Status = "failed"
		logEntry.ErrorMessage = execErr.Error()
		log.Printf("[integration] script %s (%s) failed for incident %s: %v", script.Name, script.ID, incident.IncidentNumber, execErr)
	} else {
		logEntry.Status = "success"
		// Auto-create bridge record if the script has bridge_config set (http_request only).
		if script.ScriptType == "http_request" && script.BridgeConfig != "" {
			go e.saveBridgeFromResponse(context.Background(), incident, script, responseBody)
		}
	}

	_ = e.integrationRepo.UpdateExecutionLog(ctx, logEntry)
	return logEntry
}

// saveBridgeFromResponse parses the HTTP response and creates an IncidentBridge record.
func (e *integrationExecutor) saveBridgeFromResponse(ctx context.Context, incident *models.Incident, script *models.IntegrationScript, responseBody string) {
	var cfg models.ScriptBridgeConfig
	if err := json.Unmarshal([]byte(script.BridgeConfig), &cfg); err != nil {
		log.Printf("[integration] invalid bridge_config on script %s: %v", script.ID, err)
		return
	}
	var respMap map[string]interface{}
	if err := json.Unmarshal([]byte(responseBody), &respMap); err != nil {
		log.Printf("[integration] bridge: response is not valid JSON for script %s", script.ID)
		return
	}
	remoteID := extractField(respMap, cfg.ResponseIDField)
	remoteNumber := extractField(respMap, cfg.ResponseNumberField)
	scriptID := script.ID
	bridge := &models.IncidentBridge{
		LocalIncidentID:      incident.ID,
		RemoteSystemName:     cfg.RemoteSystemName,
		RemoteSystemURL:      cfg.RemoteSystemURL,
		RemoteIncidentID:     remoteID,
		RemoteIncidentNumber: remoteNumber,
		Direction:            "outbound",
		Status:               "open",
		IntegrationScriptID:  &scriptID,
	}
	if err := e.integrationSvc.CreateBridge(ctx, bridge); err != nil {
		log.Printf("[integration] failed to save bridge for incident %s: %v", incident.IncidentNumber, err)
	}
}

// extractField extracts a string value from a nested map using dot notation (e.g. "data.id").
func extractField(m map[string]interface{}, path string) string {
	if path == "" || m == nil {
		return ""
	}
	dot := strings.Index(path, ".")
	if dot == -1 {
		if val, ok := m[path]; ok {
			return fmt.Sprintf("%v", val)
		}
		return ""
	}
	key, rest := path[:dot], path[dot+1:]
	nested, ok := m[key].(map[string]interface{})
	if !ok {
		return ""
	}
	return extractField(nested, rest)
}

// buildIncidentContext builds a flat map from selected field mappings.
// If no mappings are provided, all basic incident fields are included.
func (e *integrationExecutor) buildIncidentContext(incident *models.Incident, fieldMappingsJSON string) map[string]interface{} {
	ctx := map[string]interface{}{
		"id":              incident.ID.String(),
		"incident_number": incident.IncidentNumber,
		"title":           incident.Title,
		"description":     incident.Description,
		"record_type":     incident.RecordType,
	}

	if incident.Classification != nil {
		ctx["classification_name"] = incident.Classification.Name
		ctx["classification_id"] = incident.ClassificationID.String()
	}
	if incident.CurrentState != nil {
		ctx["current_state"] = incident.CurrentState.Name
		ctx["current_state_id"] = incident.CurrentStateID.String()
	}
	if incident.Department != nil {
		ctx["department"] = incident.Department.Name
	}
	if incident.Assignee != nil {
		ctx["assignee_name"] = incident.Assignee.FirstName + " " + incident.Assignee.LastName
		ctx["assignee_email"] = incident.Assignee.Email
	}
	if incident.Reporter != nil {
		ctx["reporter_name"] = incident.Reporter.FirstName + " " + incident.Reporter.LastName
		ctx["reporter_email"] = incident.Reporter.Email
	}
	if incident.DueDate != nil {
		ctx["due_date"] = incident.DueDate.Format(time.RFC3339)
	}
	ctx["sla_breached"] = incident.SLABreached
	ctx["created_at"] = incident.CreatedAt.Format(time.RFC3339)
	ctx["updated_at"] = incident.UpdatedAt.Format(time.RFC3339)

	// If explicit field mappings were given, build a remapped context.
	if fieldMappingsJSON == "" {
		return ctx
	}
	var mappings []models.FieldMapping
	if err := json.Unmarshal([]byte(fieldMappingsJSON), &mappings); err != nil || len(mappings) == 0 {
		return ctx
	}
	mapped := make(map[string]interface{}, len(mappings))
	for _, m := range mappings {
		if val, ok := ctx[m.Source]; ok {
			mapped[m.Target] = val
		}
	}
	return mapped
}

// executeHTTP executes an http_request type script.
func (e *integrationExecutor) executeHTTP(
	ctx context.Context,
	script *models.IntegrationScript,
	incidentCtx map[string]interface{},
	vars map[string]string,
) (requestPayload, responseBody string, statusCode int, err error) {
	var cfg models.HTTPRequestConfig
	if err = json.Unmarshal([]byte(script.ScriptContent), &cfg); err != nil {
		return "", "", 0, fmt.Errorf("invalid http_request config: %w", err)
	}
	if cfg.Method == "" {
		cfg.Method = "POST"
	}

	resolve := func(s string) string { return resolvePlaceholders(s, incidentCtx, vars) }

	// Build body
	var bodyBytes []byte
	if cfg.Body != nil {
		bodyJSON, _ := json.Marshal(cfg.Body)
		resolved := resolve(string(bodyJSON))
		bodyBytes = []byte(resolved)
	}
	requestPayload = string(bodyBytes)

	resolvedURL := resolve(cfg.URL)
	req, err := http.NewRequestWithContext(ctx, strings.ToUpper(cfg.Method), resolvedURL, bytes.NewReader(bodyBytes))
	if err != nil {
		return requestPayload, "", 0, fmt.Errorf("failed to create request: %w", err)
	}

	req.Header.Set("Content-Type", "application/json")
	for k, v := range cfg.Headers {
		req.Header.Set(k, resolve(v))
	}

	// Apply auth
	if script.AuthConfig != "" {
		if err = e.applyAuth(req, script.AuthConfig, vars, ctx); err != nil {
			return requestPayload, "", 0, fmt.Errorf("auth error: %w", err)
		}
	}

	resp, err := e.httpClient.Do(req)
	if err != nil {
		return requestPayload, "", 0, fmt.Errorf("request failed: %w", err)
	}
	defer resp.Body.Close()

	respBytes, _ := io.ReadAll(io.LimitReader(resp.Body, 64*1024))
	responseBody = string(respBytes)
	statusCode = resp.StatusCode

	if resp.StatusCode >= 400 {
		return requestPayload, responseBody, statusCode, fmt.Errorf("external API returned %d: %s", resp.StatusCode, integrationTruncate(responseBody, 200))
	}
	return requestPayload, responseBody, statusCode, nil
}

// executeJS executes a javascript type script in a goja sandbox.
func (e *integrationExecutor) executeJS(
	script *models.IntegrationScript,
	incidentCtx map[string]interface{},
	vars map[string]string,
) (requestPayload, responseBody string, err error) {
	vm := goja.New()

	// Inject incident context
	if err = vm.Set("incident", incidentCtx); err != nil {
		return "", "", fmt.Errorf("failed to set incident context: %w", err)
	}
	// Inject vars (read-only map)
	if err = vm.Set("vars", vars); err != nil {
		return "", "", fmt.Errorf("failed to set vars: %w", err)
	}

	// Inject log helper
	logObj := vm.NewObject()
	logObj.Set("info", func(args ...interface{}) { //nolint
		log.Printf("[integration-js] INFO: %v", args)
	})
	logObj.Set("error", func(args ...interface{}) { //nolint
		log.Printf("[integration-js] ERROR: %v", args)
	})
	vm.Set("log", logObj) //nolint

	// Capture outbound HTTP calls made by the script
	var capturedPayload, capturedResponse string
	httpObj := vm.NewObject()
	makeHTTPCall := func(method, targetURL string, body interface{}, headers map[string]interface{}) (map[string]interface{}, error) {
		var bodyBytes []byte
		if body != nil {
			bodyBytes, _ = json.Marshal(body)
		}
		capturedPayload = string(bodyBytes)

		req, err := http.NewRequest(strings.ToUpper(method), targetURL, bytes.NewReader(bodyBytes))
		if err != nil {
			return nil, err
		}
		req.Header.Set("Content-Type", "application/json")
		for k, v := range headers {
			req.Header.Set(k, fmt.Sprintf("%v", v))
		}
		if script.AuthConfig != "" {
			e.applyAuth(req, script.AuthConfig, vars, context.Background()) //nolint
		}
		resp, err := e.httpClient.Do(req)
		if err != nil {
			return nil, err
		}
		defer resp.Body.Close()
		respBytes, _ := io.ReadAll(io.LimitReader(resp.Body, 64*1024))
		capturedResponse = string(respBytes)

		var respJSON interface{}
		json.Unmarshal(respBytes, &respJSON)
		return map[string]interface{}{
			"status": resp.StatusCode,
			"body":   respJSON,
			"raw":    capturedResponse,
		}, nil
	}
	for _, method := range []string{"get", "post", "put", "patch", "delete"} {
		m := strings.ToUpper(method)
		httpObj.Set(method, func(targetURL string, body interface{}, headers map[string]interface{}) map[string]interface{} {
			result, err := makeHTTPCall(m, targetURL, body, headers)
			if err != nil {
				panic(vm.NewGoError(err))
			}
			return result
		})
	}
	vm.Set("http", httpObj) //nolint

	// Timeout: interrupt after 30 seconds
	done := make(chan struct{})
	timer := time.AfterFunc(30*time.Second, func() {
		vm.Interrupt("script execution timeout (30s)")
		close(done)
	})
	defer func() {
		timer.Stop()
		select {
		case <-done:
		default:
		}
	}()

	_, execErr := vm.RunString(script.ScriptContent)
	if execErr != nil {
		return capturedPayload, capturedResponse, fmt.Errorf("JS execution error: %w", execErr)
	}
	return capturedPayload, capturedResponse, nil
}

// applyAuth injects authentication headers into an outbound HTTP request.
func (e *integrationExecutor) applyAuth(req *http.Request, authConfigJSON string, vars map[string]string, ctx context.Context) error {
	var auth models.IntegrationAuthConfig
	if err := json.Unmarshal([]byte(authConfigJSON), &auth); err != nil {
		return fmt.Errorf("invalid auth_config: %w", err)
	}

	resolveVar := func(s string) string {
		return resolvePlaceholders(s, nil, vars)
	}

	switch auth.Type {
	case "none", "":
		// nothing
	case "api_key":
		header := auth.APIKeyHeader
		if header == "" {
			header = "X-API-Key"
		}
		req.Header.Set(header, resolveVar(auth.APIKeyValue))
	case "bearer":
		req.Header.Set("Authorization", "Bearer "+resolveVar(auth.BearerToken))
	case "basic":
		user := resolveVar(auth.BasicUsername)
		pass := resolveVar(auth.BasicPassword)
		encoded := base64.StdEncoding.EncodeToString([]byte(user + ":" + pass))
		req.Header.Set("Authorization", "Basic "+encoded)
	case "oauth2_client_credentials":
		token, err := e.fetchOAuth2Token(ctx, auth, vars)
		if err != nil {
			return fmt.Errorf("oauth2 token fetch failed: %w", err)
		}
		req.Header.Set("Authorization", "Bearer "+token)
	default:
		return fmt.Errorf("unknown auth type: %s", auth.Type)
	}
	return nil
}

// fetchOAuth2Token fetches a client-credentials OAuth2 access token.
func (e *integrationExecutor) fetchOAuth2Token(ctx context.Context, auth models.IntegrationAuthConfig, vars map[string]string) (string, error) {
	resolveVar := func(s string) string { return resolvePlaceholders(s, nil, vars) }

	data := url.Values{}
	data.Set("grant_type", "client_credentials")
	data.Set("client_id", resolveVar(auth.OAuth2ClientID))
	data.Set("client_secret", resolveVar(auth.OAuth2ClientSecret))
	if auth.OAuth2Scope != "" {
		data.Set("scope", auth.OAuth2Scope)
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, auth.OAuth2TokenURL, strings.NewReader(data.Encode()))
	if err != nil {
		return "", err
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")

	resp, err := e.httpClient.Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(io.LimitReader(resp.Body, 8*1024))

	var tokenResp struct {
		AccessToken string `json:"access_token"`
		Error       string `json:"error"`
	}
	if err := json.Unmarshal(body, &tokenResp); err != nil {
		return "", fmt.Errorf("failed to parse token response: %w", err)
	}
	if tokenResp.Error != "" {
		return "", fmt.Errorf("oauth2 error: %s", tokenResp.Error)
	}
	return tokenResp.AccessToken, nil
}

// resolvePlaceholders replaces {{incident.X}}, {{vars.Y}}, and {{X}} in a string.
func resolvePlaceholders(s string, incidentCtx map[string]interface{}, vars map[string]string) string {
	result := s
	// Replace {{vars.NAME}}
	for k, v := range vars {
		result = strings.ReplaceAll(result, "{{vars."+k+"}}", v)
	}
	// Replace {{incident.FIELD}} and bare {{FIELD}}
	for k, v := range incidentCtx {
		val := fmt.Sprintf("%v", v)
		result = strings.ReplaceAll(result, "{{incident."+k+"}}", val)
		result = strings.ReplaceAll(result, "{{"+k+"}}", val)
	}
	return result
}

func integrationTruncate(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n] + "..."
}
