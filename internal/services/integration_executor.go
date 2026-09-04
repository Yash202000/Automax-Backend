// i18n note: this file's fmt.Errorf messages surface only in IntegrationExecutionLog.ErrorMessage
// (admin-only script-test/execution-log diagnostics, e.g. "external API returned 500: <raw body>")
// and are not translated — the raw upstream response/protocol detail is the useful part for an
// admin debugging an integration script, and is itself untranslatable (external API text).
package services

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"mime/multipart"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/automax/backend/internal/models"
	"github.com/automax/backend/internal/repository"
	pkgUtils "github.com/automax/backend/pkg/utils"
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
	// HasActiveTransitionTriggers returns true if the given transition has at least one active integration trigger.
	HasActiveTransitionTriggers(ctx context.Context, transitionID uuid.UUID) (bool, error)
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

func (e *integrationExecutor) HasActiveTransitionTriggers(ctx context.Context, transitionID uuid.UUID) (bool, error) {
	triggers, err := e.integrationRepo.ListActiveTransitionTriggers(ctx, transitionID)
	if err != nil {
		return false, err
	}
	return len(triggers) > 0, nil
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
		requestPayload, responseBody, execErr = e.executeJS(script, incident, incidentCtx, vars)
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
// Non-string values (float64, []interface{}) are stored with their native types
// so that JS scripts and the typed HTTP body resolver can use proper JSON types.
func (e *integrationExecutor) buildIncidentContext(incident *models.Incident, fieldMappingsJSON string) map[string]interface{} {
	ctx := map[string]interface{}{
		"id":              incident.ID.String(),
		"incident_number": incident.IncidentNumber,
		"title":           incident.Title,
		"description":     incident.Description,
		"record_type":     incident.RecordType,
		"workflow_id":     incident.WorkflowID.String(),
		"source":          incident.Source,
		"channel":         incident.Channel,
		"address":         incident.Address,
		"city":            incident.City,
		"geo_state":       incident.State,
		"country":         incident.Country,
		"postal_code":     incident.PostalCode,
		"custom_fields":   incident.CustomFields,
		// Direct reporter fields (always present even without a linked user)
		"reporter_email": incident.ReporterEmail,
		"reporter_name":  incident.ReporterName,
		"reporter_phone": pkgUtils.NormalizeMobile(incident.ReporterPhone, pkgUtils.SystemCountryCode()),
		"sla_breached":   incident.SLABreached,
		"created_at":     incident.CreatedAt.Format(time.RFC3339),
		"updated_at":     incident.UpdatedAt.Format(time.RFC3339),
	}

	// For FK + relation pairs, fall back to the loaded relation's ID when the
	// FK pointer is nil (can happen due to GORM join vs. preload differences).
	if incident.ClassificationID != nil {
		ctx["classification_id"] = incident.ClassificationID.String()
	} else if incident.Classification != nil {
		ctx["classification_id"] = incident.Classification.ID.String()
	}
	if incident.Classification != nil {
		ctx["classification_name"] = incident.Classification.Name
	}
	if incident.CurrentState != nil {
		ctx["current_state"] = incident.CurrentState.Name
		ctx["current_state_id"] = incident.CurrentStateID.String()
	}
	if incident.LocationID != nil {
		ctx["location_id"] = incident.LocationID.String()
	} else if incident.Location != nil {
		ctx["location_id"] = incident.Location.ID.String()
	}
	if incident.Location != nil {
		ctx["location_name"] = incident.Location.Name
	}
	if incident.DepartmentID != nil {
		ctx["department_id"] = incident.DepartmentID.String()
	} else if incident.Department != nil {
		ctx["department_id"] = incident.Department.ID.String()
	}
	if incident.Department != nil {
		ctx["department"] = incident.Department.Name
	}
	if incident.AssigneeID != nil {
		ctx["assignee_id"] = incident.AssigneeID.String()
	} else if incident.Assignee != nil {
		ctx["assignee_id"] = incident.Assignee.ID.String()
	}
	if incident.Assignee != nil {
		ctx["assignee_name"] = incident.Assignee.FirstName + " " + incident.Assignee.LastName
		ctx["assignee_email"] = incident.Assignee.Email
	}
	if incident.Reporter != nil {
		if ctx["reporter_email"] == "" && incident.Reporter.Email != "" {
			ctx["reporter_email"] = incident.Reporter.Email
		}
		fullName := strings.TrimSpace(incident.Reporter.FirstName + " " + incident.Reporter.LastName)
		if ctx["reporter_name"] == "" && fullName != "" {
			ctx["reporter_name"] = fullName
		}
		if ctx["reporter_phone"] == "" && incident.Reporter.Phone != "" {
			ctx["reporter_phone"] = pkgUtils.NormalizeMobile(incident.Reporter.Phone, pkgUtils.SystemCountryCode())
		}
		ctx["reporter_id"] = incident.Reporter.ID.String()
		ctx["reporter_username"] = incident.Reporter.Username
		ctx["reporter_first_name"] = incident.Reporter.FirstName
		ctx["reporter_last_name"] = incident.Reporter.LastName
		ctx["reporter_extension"] = incident.Reporter.Extension
	}
	if incident.SourceIncidentID != nil {
		ctx["source_incident_id"] = incident.SourceIncidentID.String()
	}
	if incident.DueDate != nil {
		ctx["due_date"] = incident.DueDate.Format(time.RFC3339)
	}
	if incident.Latitude != nil {
		ctx["latitude"] = *incident.Latitude // float64 — proper type for JS + HTTP body
	}
	if incident.Longitude != nil {
		ctx["longitude"] = *incident.Longitude
	}

	// Lookup value IDs as a proper slice so they serialise as a JSON array.
	if len(incident.LookupValues) > 0 {
		ids := make([]interface{}, len(incident.LookupValues))
		for i, lv := range incident.LookupValues {
			ids[i] = lv.ID.String()
		}
		ctx["lookup_value_ids"] = ids
	} else {
		ctx["lookup_value_ids"] = []interface{}{}
	}

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

	// Build body.  Walk the body object and resolve placeholders with proper types:
	// a string value that is EXACTLY one placeholder (e.g. "{{incident.latitude}}")
	// is replaced by the typed value from the context (float64, []interface{}, etc.)
	// so the outbound JSON contains proper numbers/arrays instead of quoted strings.
	var bodyBytes []byte
	if cfg.Body != nil {
		resolved := resolveBodyTyped(cfg.Body, incidentCtx, vars)
		if b, err2 := json.Marshal(resolved); err2 == nil {
			bodyBytes = b
		} else {
			// Fallback to the legacy string-replace path
			bodyJSON, _ := json.Marshal(cfg.Body)
			bodyBytes = []byte(resolve(string(bodyJSON)))
		}
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
	incident *models.Incident,
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

	// http.getBytes(url, headers) — downloads a file and returns base64-encoded content + mime type.
	// Use this to fetch attachments before re-uploading them elsewhere.
	httpObj.Set("getBytes", func(targetURL string, headers map[string]interface{}) map[string]interface{} { //nolint
		req, err := http.NewRequest("GET", targetURL, nil)
		if err != nil {
			panic(vm.NewGoError(err))
		}
		for k, v := range headers {
			req.Header.Set(k, fmt.Sprintf("%v", v))
		}
		resp, err := e.httpClient.Do(req)
		if err != nil {
			panic(vm.NewGoError(err))
		}
		defer resp.Body.Close()
		fileBytes, _ := io.ReadAll(io.LimitReader(resp.Body, 50*1024*1024)) // 50 MB cap
		return map[string]interface{}{
			"status":   resp.StatusCode,
			"data":     base64.StdEncoding.EncodeToString(fileBytes),
			"mimeType": resp.Header.Get("Content-Type"),
		}
	})

	// http.upload(url, base64Data, fileName, mimeType, headers) — multipart/form-data upload.
	// Pair with getBytes to bridge attachments across Automax instances.
	httpObj.Set("upload", func(targetURL, base64Data, fileName, mimeType string, headers map[string]interface{}) map[string]interface{} { //nolint
		fileBytes, err := base64.StdEncoding.DecodeString(base64Data)
		if err != nil {
			panic(vm.NewGoError(fmt.Errorf("upload: invalid base64 data: %w", err)))
		}
		var buf bytes.Buffer
		writer := multipart.NewWriter(&buf)
		part, err := writer.CreateFormFile("file", fileName)
		if err != nil {
			panic(vm.NewGoError(err))
		}
		if _, err = part.Write(fileBytes); err != nil {
			panic(vm.NewGoError(err))
		}
		writer.Close()

		req, err := http.NewRequest("POST", targetURL, &buf)
		if err != nil {
			panic(vm.NewGoError(err))
		}
		req.Header.Set("Content-Type", writer.FormDataContentType())
		for k, v := range headers {
			req.Header.Set(k, fmt.Sprintf("%v", v))
		}
		resp, err := e.httpClient.Do(req)
		if err != nil {
			panic(vm.NewGoError(err))
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
		}
	})

	vm.Set("http", httpObj) //nolint

	// createBridge(remoteSystemName, remoteSystemUrl, remoteIncidentId, remoteIncidentNumber, direction)
	// Creates a bridge record on the LOCAL instance linking this incident to a remote one.
	vm.Set("createBridge", func(remoteSystemName, remoteSystemURL, remoteIncidentID, remoteIncidentNumber, direction string) { //nolint
		if direction != "inbound" {
			direction = "outbound"
		}
		bridge := &models.IncidentBridge{
			LocalIncidentID:      incident.ID,
			RemoteSystemName:     remoteSystemName,
			RemoteSystemURL:      remoteSystemURL,
			RemoteIncidentID:     remoteIncidentID,
			RemoteIncidentNumber: remoteIncidentNumber,
			Direction:            direction,
			Status:               "open",
		}
		if err := e.integrationSvc.CreateBridge(context.Background(), bridge); err != nil {
			panic(vm.NewGoError(fmt.Errorf("createBridge failed: %w", err)))
		}
	})

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

// resolveBodyTyped walks a JSON body object and resolves {{incident.X}} / {{vars.Y}}
// placeholders while preserving native types.  If a string value is *exactly* one
// placeholder token (nothing else), the value from the context is used as-is
// (float64, []interface{}, bool, etc.) so that the outbound JSON contains proper
// numbers and arrays rather than quoted strings.  Mixed strings (e.g.
// "prefix {{incident.title}} suffix") fall back to string replacement.
func resolveBodyTyped(body interface{}, incidentCtx map[string]interface{}, vars map[string]string) interface{} {
	singlePlaceholderRe := func(s string) (string, bool) {
		trimmed := strings.TrimSpace(s)
		if strings.HasPrefix(trimmed, "{{") && strings.HasSuffix(trimmed, "}}") && strings.Count(trimmed, "{{") == 1 {
			inner := trimmed[2 : len(trimmed)-2]
			return inner, true
		}
		return "", false
	}

	lookupTyped := func(key string) (interface{}, bool) {
		if strings.HasPrefix(key, "incident.") {
			k := key[len("incident."):]
			if v, ok := incidentCtx[k]; ok {
				return v, true
			}
		}
		if strings.HasPrefix(key, "vars.") {
			k := key[len("vars."):]
			if v, ok := vars[k]; ok {
				return v, true
			}
		}
		// Bare key — try incidentCtx then vars
		if v, ok := incidentCtx[key]; ok {
			return v, true
		}
		if v, ok := vars[key]; ok {
			return v, true
		}
		return nil, false
	}

	switch v := body.(type) {
	case map[string]interface{}:
		out := make(map[string]interface{}, len(v))
		for k, mv := range v {
			out[k] = resolveBodyTyped(mv, incidentCtx, vars)
		}
		return out
	case []interface{}:
		out := make([]interface{}, len(v))
		for i, mv := range v {
			out[i] = resolveBodyTyped(mv, incidentCtx, vars)
		}
		return out
	case string:
		if key, ok := singlePlaceholderRe(v); ok {
			if typed, found := lookupTyped(key); found {
				return typed
			}
			// Unresolved single placeholder (field not set on this incident) → null.
			// This prevents the literal "{{incident.X}}" string from reaching the API
			// and failing validation (e.g. treating it as an invalid UUID).
			return nil
		}
		return resolvePlaceholders(v, incidentCtx, vars)
	}
	return body
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
