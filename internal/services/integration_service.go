package services

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/automax/backend/internal/models"
	"github.com/automax/backend/internal/repository"
	"github.com/automax/backend/pkg/utils"
	"github.com/google/uuid"
)

type IntegrationService interface {
	// Variables
	CreateVariable(ctx context.Context, req *models.IntegrationVariableCreateRequest, createdByID uuid.UUID) (*models.IntegrationVariableResponse, error)
	ListVariables(ctx context.Context) ([]models.IntegrationVariableResponse, error)
	DeleteVariable(ctx context.Context, id uuid.UUID) error
	// ResolveVariables returns a map of variable name → plaintext value for use by the executor.
	ResolveVariables(ctx context.Context) (map[string]string, error)

	// Scripts
	CreateScript(ctx context.Context, req *models.IntegrationScriptRequest, createdByID uuid.UUID) (*models.IntegrationScriptResponse, error)
	GetScript(ctx context.Context, id uuid.UUID) (*models.IntegrationScriptResponse, error)
	ListScripts(ctx context.Context, activeOnly bool) ([]models.IntegrationScriptResponse, error)
	UpdateScript(ctx context.Context, id uuid.UUID, req *models.IntegrationScriptRequest) (*models.IntegrationScriptResponse, error)
	DeleteScript(ctx context.Context, id uuid.UUID) error

	// State triggers
	CreateStateTrigger(ctx context.Context, stateID uuid.UUID, req *models.WorkflowStateTriggerRequest) (*models.WorkflowStateTrigger, error)
	ListStateTriggers(ctx context.Context, stateID uuid.UUID) ([]models.WorkflowStateTrigger, error)
	UpdateStateTrigger(ctx context.Context, id uuid.UUID, req *models.WorkflowStateTriggerRequest) (*models.WorkflowStateTrigger, error)
	DeleteStateTrigger(ctx context.Context, id uuid.UUID) error

	// Transition triggers
	CreateTransitionTrigger(ctx context.Context, transitionID uuid.UUID, req *models.WorkflowTransitionTriggerRequest) (*models.WorkflowTransitionTrigger, error)
	ListTransitionTriggers(ctx context.Context, transitionID uuid.UUID) ([]models.WorkflowTransitionTrigger, error)
	UpdateTransitionTrigger(ctx context.Context, id uuid.UUID, req *models.WorkflowTransitionTriggerRequest) (*models.WorkflowTransitionTrigger, error)
	DeleteTransitionTrigger(ctx context.Context, id uuid.UUID) error

	// Execution logs
	ListLogsByScript(ctx context.Context, scriptID uuid.UUID, limit, offset int) ([]models.IntegrationExecutionLogResponse, int64, error)
	ListLogsByIncident(ctx context.Context, incidentID uuid.UUID, limit, offset int) ([]models.IntegrationExecutionLogResponse, int64, error)

	// Incident bridges
	CreateBridge(ctx context.Context, b *models.IncidentBridge) error
	ListBridgesByIncident(ctx context.Context, incidentID uuid.UUID) ([]models.IncidentBridgeResponse, error)
	CloseBridgesForIncident(ctx context.Context, incidentID uuid.UUID) error

	// Webhook callback configs
	CreateWebhookConfig(ctx context.Context, req *models.WebhookCallbackConfigRequest) (*models.WebhookCallbackConfigResponse, error)
	GetWebhookConfig(ctx context.Context, id uuid.UUID) (*models.WebhookCallbackConfigResponse, error)
	ListWebhookConfigs(ctx context.Context) ([]models.WebhookCallbackConfigResponse, error)
	UpdateWebhookConfig(ctx context.Context, id uuid.UUID, req *models.WebhookCallbackConfigRequest) (*models.WebhookCallbackConfigResponse, error)
	DeleteWebhookConfig(ctx context.Context, id uuid.UUID) error
	// FindTransitionForCallback validates the bearer secret against all active webhook configs
	// and resolves payload.RemoteStateCode / payload.Action to a local transition UUID.
	FindTransitionForCallback(ctx context.Context, secret string, payload *models.AutomaxCallbackPayload) (uuid.UUID, error)
}

type integrationService struct {
	repo       repository.IntegrationRepository
	secretsKey string // 64-char hex AES-256 key from INTEGRATION_SECRETS_KEY env var
}

func NewIntegrationService(repo repository.IntegrationRepository, secretsKey string) IntegrationService {
	return &integrationService{repo: repo, secretsKey: secretsKey}
}

// ---- Variables ----

func (s *integrationService) CreateVariable(ctx context.Context, req *models.IntegrationVariableCreateRequest, createdByID uuid.UUID) (*models.IntegrationVariableResponse, error) {
	if s.secretsKey == "" {
		return nil, errors.New("INTEGRATION_SECRETS_KEY is not configured")
	}
	encrypted, nonce, err := utils.EncryptLicenseKey(req.Value, s.secretsKey)
	if err != nil {
		return nil, fmt.Errorf("failed to encrypt variable value: %w", err)
	}
	isSecret := true
	if req.IsSecret != nil {
		isSecret = *req.IsSecret
	}
	v := &models.IntegrationVariable{
		Name:           req.Name,
		Description:    req.Description,
		EncryptedValue: encrypted,
		EncryptedNonce: nonce,
		IsSecret:       isSecret,
		CreatedByID:    &createdByID,
	}
	if err := s.repo.CreateVariable(ctx, v); err != nil {
		return nil, err
	}
	return toVariableResponse(v), nil
}

func (s *integrationService) ListVariables(ctx context.Context) ([]models.IntegrationVariableResponse, error) {
	list, err := s.repo.ListVariables(ctx)
	if err != nil {
		return nil, err
	}
	resp := make([]models.IntegrationVariableResponse, len(list))
	for i, v := range list {
		resp[i] = *toVariableResponse(&v)
	}
	return resp, nil
}

func (s *integrationService) DeleteVariable(ctx context.Context, id uuid.UUID) error {
	return s.repo.DeleteVariable(ctx, id)
}

func (s *integrationService) ResolveVariables(ctx context.Context) (map[string]string, error) {
	if s.secretsKey == "" {
		return map[string]string{}, nil
	}
	list, err := s.repo.ListVariables(ctx)
	if err != nil {
		return nil, err
	}
	result := make(map[string]string, len(list))
	for _, v := range list {
		plaintext, err := utils.DecryptLicenseKey(v.EncryptedValue, v.EncryptedNonce, s.secretsKey)
		if err != nil {
			return nil, fmt.Errorf("failed to decrypt variable %q: %w", v.Name, err)
		}
		result[v.Name] = plaintext
	}
	return result, nil
}

// ---- Scripts ----

func (s *integrationService) CreateScript(ctx context.Context, req *models.IntegrationScriptRequest, createdByID uuid.UUID) (*models.IntegrationScriptResponse, error) {
	isActive := true
	if req.IsActive != nil {
		isActive = *req.IsActive
	}
	script := &models.IntegrationScript{
		Name:          req.Name,
		Description:   req.Description,
		ScriptType:    req.ScriptType,
		ScriptContent: req.ScriptContent,
		AuthConfig:    req.AuthConfig,
		BridgeConfig:  req.BridgeConfig,
		IsActive:      isActive,
		CreatedByID:   &createdByID,
	}
	if err := s.repo.CreateScript(ctx, script); err != nil {
		return nil, err
	}
	return toScriptResponse(script), nil
}

func (s *integrationService) GetScript(ctx context.Context, id uuid.UUID) (*models.IntegrationScriptResponse, error) {
	script, err := s.repo.FindScriptByID(ctx, id)
	if err != nil {
		return nil, err
	}
	return toScriptResponse(script), nil
}

func (s *integrationService) ListScripts(ctx context.Context, activeOnly bool) ([]models.IntegrationScriptResponse, error) {
	list, err := s.repo.ListScripts(ctx, activeOnly)
	if err != nil {
		return nil, err
	}
	resp := make([]models.IntegrationScriptResponse, len(list))
	for i, sc := range list {
		resp[i] = *toScriptResponse(&sc)
	}
	return resp, nil
}

func (s *integrationService) UpdateScript(ctx context.Context, id uuid.UUID, req *models.IntegrationScriptRequest) (*models.IntegrationScriptResponse, error) {
	script, err := s.repo.FindScriptByID(ctx, id)
	if err != nil {
		return nil, errors.New("script not found")
	}
	script.Name = req.Name
	script.Description = req.Description
	script.ScriptType = req.ScriptType
	script.ScriptContent = req.ScriptContent
	script.AuthConfig = req.AuthConfig
	script.BridgeConfig = req.BridgeConfig
	if req.IsActive != nil {
		script.IsActive = *req.IsActive
	}
	if err := s.repo.UpdateScript(ctx, script); err != nil {
		return nil, err
	}
	return toScriptResponse(script), nil
}

func (s *integrationService) DeleteScript(ctx context.Context, id uuid.UUID) error {
	return s.repo.DeleteScript(ctx, id)
}

// ---- State triggers ----

func (s *integrationService) CreateStateTrigger(ctx context.Context, stateID uuid.UUID, req *models.WorkflowStateTriggerRequest) (*models.WorkflowStateTrigger, error) {
	scriptID, err := uuid.Parse(req.IntegrationScriptID)
	if err != nil {
		return nil, errors.New("invalid integration_script_id")
	}
	isAsync := true
	if req.IsAsync != nil {
		isAsync = *req.IsAsync
	}
	isActive := true
	if req.IsActive != nil {
		isActive = *req.IsActive
	}
	t := &models.WorkflowStateTrigger{
		WorkflowStateID:     stateID,
		IntegrationScriptID: scriptID,
		TriggerOn:           req.TriggerOn,
		FieldMappings:       req.FieldMappings,
		ExecutionOrder:      req.ExecutionOrder,
		IsAsync:             isAsync,
		IsActive:            isActive,
	}
	if err := s.repo.CreateStateTrigger(ctx, t); err != nil {
		return nil, err
	}
	if len(req.ClassificationIDs) > 0 {
		classUUIDs, err := parseUUIDs(req.ClassificationIDs)
		if err != nil {
			return nil, err
		}
		if err := s.repo.ReplaceStateTriggerClassifications(ctx, t.ID, classUUIDs); err != nil {
			return nil, err
		}
	}
	return s.repo.FindStateTriggerByID(ctx, t.ID)
}

func (s *integrationService) ListStateTriggers(ctx context.Context, stateID uuid.UUID) ([]models.WorkflowStateTrigger, error) {
	return s.repo.ListStateTriggers(ctx, stateID)
}

func (s *integrationService) UpdateStateTrigger(ctx context.Context, id uuid.UUID, req *models.WorkflowStateTriggerRequest) (*models.WorkflowStateTrigger, error) {
	t, err := s.repo.FindStateTriggerByID(ctx, id)
	if err != nil {
		return nil, errors.New("state trigger not found")
	}
	scriptID, err := uuid.Parse(req.IntegrationScriptID)
	if err != nil {
		return nil, errors.New("invalid integration_script_id")
	}
	t.IntegrationScriptID = scriptID
	t.TriggerOn = req.TriggerOn
	t.FieldMappings = req.FieldMappings
	t.ExecutionOrder = req.ExecutionOrder
	if req.IsAsync != nil {
		t.IsAsync = *req.IsAsync
	}
	if req.IsActive != nil {
		t.IsActive = *req.IsActive
	}
	if err := s.repo.UpdateStateTrigger(ctx, t); err != nil {
		return nil, err
	}
	classUUIDs, err := parseUUIDs(req.ClassificationIDs)
	if err != nil {
		return nil, err
	}
	if err := s.repo.ReplaceStateTriggerClassifications(ctx, id, classUUIDs); err != nil {
		return nil, err
	}
	return s.repo.FindStateTriggerByID(ctx, id)
}

func (s *integrationService) DeleteStateTrigger(ctx context.Context, id uuid.UUID) error {
	return s.repo.DeleteStateTrigger(ctx, id)
}

// ---- Transition triggers ----

func (s *integrationService) CreateTransitionTrigger(ctx context.Context, transitionID uuid.UUID, req *models.WorkflowTransitionTriggerRequest) (*models.WorkflowTransitionTrigger, error) {
	scriptID, err := uuid.Parse(req.IntegrationScriptID)
	if err != nil {
		return nil, errors.New("invalid integration_script_id")
	}
	isAsync := true
	if req.IsAsync != nil {
		isAsync = *req.IsAsync
	}
	isActive := true
	if req.IsActive != nil {
		isActive = *req.IsActive
	}
	t := &models.WorkflowTransitionTrigger{
		WorkflowTransitionID: transitionID,
		IntegrationScriptID:  scriptID,
		FieldMappings:        req.FieldMappings,
		ExecutionOrder:       req.ExecutionOrder,
		IsAsync:              isAsync,
		IsActive:             isActive,
	}
	if err := s.repo.CreateTransitionTrigger(ctx, t); err != nil {
		return nil, err
	}
	if len(req.ClassificationIDs) > 0 {
		classUUIDs, err := parseUUIDs(req.ClassificationIDs)
		if err != nil {
			return nil, err
		}
		if err := s.repo.ReplaceTransitionTriggerClassifications(ctx, t.ID, classUUIDs); err != nil {
			return nil, err
		}
	}
	return s.repo.FindTransitionTriggerByID(ctx, t.ID)
}

func (s *integrationService) ListTransitionTriggers(ctx context.Context, transitionID uuid.UUID) ([]models.WorkflowTransitionTrigger, error) {
	return s.repo.ListTransitionTriggers(ctx, transitionID)
}

func (s *integrationService) UpdateTransitionTrigger(ctx context.Context, id uuid.UUID, req *models.WorkflowTransitionTriggerRequest) (*models.WorkflowTransitionTrigger, error) {
	t, err := s.repo.FindTransitionTriggerByID(ctx, id)
	if err != nil {
		return nil, errors.New("transition trigger not found")
	}
	scriptID, err := uuid.Parse(req.IntegrationScriptID)
	if err != nil {
		return nil, errors.New("invalid integration_script_id")
	}
	t.IntegrationScriptID = scriptID
	t.FieldMappings = req.FieldMappings
	t.ExecutionOrder = req.ExecutionOrder
	if req.IsAsync != nil {
		t.IsAsync = *req.IsAsync
	}
	if req.IsActive != nil {
		t.IsActive = *req.IsActive
	}
	if err := s.repo.UpdateTransitionTrigger(ctx, t); err != nil {
		return nil, err
	}
	classUUIDs, err := parseUUIDs(req.ClassificationIDs)
	if err != nil {
		return nil, err
	}
	if err := s.repo.ReplaceTransitionTriggerClassifications(ctx, id, classUUIDs); err != nil {
		return nil, err
	}
	return s.repo.FindTransitionTriggerByID(ctx, id)
}

func (s *integrationService) DeleteTransitionTrigger(ctx context.Context, id uuid.UUID) error {
	return s.repo.DeleteTransitionTrigger(ctx, id)
}

// ---- Logs ----

func (s *integrationService) ListLogsByScript(ctx context.Context, scriptID uuid.UUID, limit, offset int) ([]models.IntegrationExecutionLogResponse, int64, error) {
	list, total, err := s.repo.ListExecutionLogsByScript(ctx, scriptID, limit, offset)
	if err != nil {
		return nil, 0, err
	}
	return toLogResponses(list), total, nil
}

func (s *integrationService) ListLogsByIncident(ctx context.Context, incidentID uuid.UUID, limit, offset int) ([]models.IntegrationExecutionLogResponse, int64, error) {
	list, total, err := s.repo.ListExecutionLogsByIncident(ctx, incidentID, limit, offset)
	if err != nil {
		return nil, 0, err
	}
	return toLogResponses(list), total, nil
}

// ---- helpers ----

func toVariableResponse(v *models.IntegrationVariable) *models.IntegrationVariableResponse {
	return &models.IntegrationVariableResponse{
		ID:          v.ID,
		Name:        v.Name,
		Description: v.Description,
		IsSecret:    v.IsSecret,
		CreatedAt:   v.CreatedAt,
		UpdatedAt:   v.UpdatedAt,
		CreatedByID: v.CreatedByID,
	}
}

func toScriptResponse(s *models.IntegrationScript) *models.IntegrationScriptResponse {
	return &models.IntegrationScriptResponse{
		ID:            s.ID,
		Name:          s.Name,
		Description:   s.Description,
		ScriptType:    s.ScriptType,
		ScriptContent: s.ScriptContent,
		AuthConfig:    s.AuthConfig,
		BridgeConfig:  s.BridgeConfig,
		IsActive:      s.IsActive,
		CreatedByID:   s.CreatedByID,
		CreatedAt:     s.CreatedAt,
		UpdatedAt:     s.UpdatedAt,
	}
}

func toLogResponses(list []models.IntegrationExecutionLog) []models.IntegrationExecutionLogResponse {
	resp := make([]models.IntegrationExecutionLogResponse, len(list))
	for i, l := range list {
		scriptName := ""
		if l.IntegrationScript != nil {
			scriptName = l.IntegrationScript.Name
		}
		resp[i] = models.IntegrationExecutionLogResponse{
			ID:                  l.ID,
			IntegrationScriptID: l.IntegrationScriptID,
			ScriptName:          scriptName,
			IncidentID:          l.IncidentID,
			IncidentNumber:      l.IncidentNumber,
			TriggerType:         l.TriggerType,
			TriggerRefID:        l.TriggerRefID,
			TriggerRefName:      l.TriggerRefName,
			Status:              l.Status,
			RequestPayload:      l.RequestPayload,
			ResponseBody:        l.ResponseBody,
			StatusCode:          l.StatusCode,
			ErrorMessage:        l.ErrorMessage,
			DurationMs:          l.DurationMs,
			ExecutedAt:          l.ExecutedAt,
			CompletedAt:         l.CompletedAt,
		}
	}
	return resp
}

func parseUUIDs(strs []string) ([]uuid.UUID, error) {
	ids := make([]uuid.UUID, 0, len(strs))
	for _, s := range strs {
		id, err := uuid.Parse(s)
		if err != nil {
			return nil, fmt.Errorf("invalid UUID %q: %w", s, err)
		}
		ids = append(ids, id)
	}
	return ids, nil
}

// ---- Incident bridges ----

func (s *integrationService) CreateBridge(ctx context.Context, b *models.IncidentBridge) error {
	return s.repo.CreateBridge(ctx, b)
}

func (s *integrationService) ListBridgesByIncident(ctx context.Context, incidentID uuid.UUID) ([]models.IncidentBridgeResponse, error) {
	list, err := s.repo.ListBridgesByIncident(ctx, incidentID)
	if err != nil {
		return nil, err
	}
	resp := make([]models.IncidentBridgeResponse, len(list))
	for i, b := range list {
		resp[i] = toBridgeResponse(&b)
	}
	return resp, nil
}

func (s *integrationService) CloseBridgesForIncident(ctx context.Context, incidentID uuid.UUID) error {
	return s.repo.CloseOpenBridgesForIncident(ctx, incidentID)
}

// ---- Webhook callback configs ----

func (s *integrationService) CreateWebhookConfig(ctx context.Context, req *models.WebhookCallbackConfigRequest) (*models.WebhookCallbackConfigResponse, error) {
	if s.secretsKey == "" {
		return nil, errors.New("INTEGRATION_SECRETS_KEY is not configured")
	}
	encrypted, nonce, err := utils.EncryptLicenseKey(req.SharedSecret, s.secretsKey)
	if err != nil {
		return nil, fmt.Errorf("failed to encrypt shared secret: %w", err)
	}
	isActive := true
	if req.IsActive != nil {
		isActive = *req.IsActive
	}
	cfg := &models.WebhookCallbackConfig{
		Name:                  req.Name,
		Description:           req.Description,
		IsActive:              isActive,
		SharedSecretEncrypted: encrypted,
		SharedSecretNonce:     nonce,
		ActionMappings:        req.ActionMappings,
		StateCodeMappings:     req.StateCodeMappings,
	}
	if err := s.repo.CreateWebhookConfig(ctx, cfg); err != nil {
		return nil, err
	}
	return toWebhookConfigResponse(cfg), nil
}

func (s *integrationService) GetWebhookConfig(ctx context.Context, id uuid.UUID) (*models.WebhookCallbackConfigResponse, error) {
	cfg, err := s.repo.FindWebhookConfigByID(ctx, id)
	if err != nil {
		return nil, err
	}
	return toWebhookConfigResponse(cfg), nil
}

func (s *integrationService) ListWebhookConfigs(ctx context.Context) ([]models.WebhookCallbackConfigResponse, error) {
	list, err := s.repo.ListWebhookConfigs(ctx)
	if err != nil {
		return nil, err
	}
	resp := make([]models.WebhookCallbackConfigResponse, len(list))
	for i, cfg := range list {
		resp[i] = *toWebhookConfigResponse(&cfg)
	}
	return resp, nil
}

func (s *integrationService) UpdateWebhookConfig(ctx context.Context, id uuid.UUID, req *models.WebhookCallbackConfigRequest) (*models.WebhookCallbackConfigResponse, error) {
	cfg, err := s.repo.FindWebhookConfigByID(ctx, id)
	if err != nil {
		return nil, errors.New("webhook config not found")
	}
	// Re-encrypt secret only if a new value is provided
	if req.SharedSecret != "" {
		if s.secretsKey == "" {
			return nil, errors.New("INTEGRATION_SECRETS_KEY is not configured")
		}
		encrypted, nonce, err := utils.EncryptLicenseKey(req.SharedSecret, s.secretsKey)
		if err != nil {
			return nil, fmt.Errorf("failed to encrypt shared secret: %w", err)
		}
		cfg.SharedSecretEncrypted = encrypted
		cfg.SharedSecretNonce = nonce
	}
	cfg.Name = req.Name
	cfg.Description = req.Description
	cfg.ActionMappings = req.ActionMappings
	cfg.StateCodeMappings = req.StateCodeMappings
	if req.IsActive != nil {
		cfg.IsActive = *req.IsActive
	}
	if err := s.repo.UpdateWebhookConfig(ctx, cfg); err != nil {
		return nil, err
	}
	return toWebhookConfigResponse(cfg), nil
}

func (s *integrationService) DeleteWebhookConfig(ctx context.Context, id uuid.UUID) error {
	return s.repo.DeleteWebhookConfig(ctx, id)
}

func (s *integrationService) FindTransitionForCallback(ctx context.Context, secret string, payload *models.AutomaxCallbackPayload) (uuid.UUID, error) {
	configs, err := s.repo.ListActiveWebhookConfigs(ctx)
	if err != nil {
		return uuid.Nil, fmt.Errorf("failed to load webhook configs: %w", err)
	}
	for _, cfg := range configs {
		plain, err := utils.DecryptLicenseKey(cfg.SharedSecretEncrypted, cfg.SharedSecretNonce, s.secretsKey)
		if err != nil || plain != secret {
			continue
		}
		// Matched — resolve state_code first, then action
		if payload.RemoteStateCode != "" && cfg.StateCodeMappings != "" {
			var scMap map[string]string
			if json.Unmarshal([]byte(cfg.StateCodeMappings), &scMap) == nil {
				if transStr, ok := scMap[payload.RemoteStateCode]; ok {
					if id, err := uuid.Parse(transStr); err == nil {
						return id, nil
					}
				}
			}
		}
		if payload.Action != "" && cfg.ActionMappings != "" {
			var aMap map[string]string
			if json.Unmarshal([]byte(cfg.ActionMappings), &aMap) == nil {
				if transStr, ok := aMap[payload.Action]; ok {
					if id, err := uuid.Parse(transStr); err == nil {
						return id, nil
					}
				}
			}
		}
		return uuid.Nil, fmt.Errorf("no transition mapping for action=%q state_code=%q", payload.Action, payload.RemoteStateCode)
	}
	return uuid.Nil, errors.New("no matching webhook config found for provided secret")
}

// ---- helpers ----

func toBridgeResponse(b *models.IncidentBridge) models.IncidentBridgeResponse {
	return models.IncidentBridgeResponse{
		ID:                   b.ID,
		LocalIncidentID:      b.LocalIncidentID,
		RemoteSystemName:     b.RemoteSystemName,
		RemoteSystemURL:      b.RemoteSystemURL,
		RemoteIncidentID:     b.RemoteIncidentID,
		RemoteIncidentNumber: b.RemoteIncidentNumber,
		Direction:            b.Direction,
		Status:               b.Status,
		IntegrationScriptID:  b.IntegrationScriptID,
		CreatedAt:            b.CreatedAt,
		UpdatedAt:            b.UpdatedAt,
		ClosedAt:             b.ClosedAt,
	}
}

func toWebhookConfigResponse(cfg *models.WebhookCallbackConfig) *models.WebhookCallbackConfigResponse {
	return &models.WebhookCallbackConfigResponse{
		ID:                cfg.ID,
		Name:              cfg.Name,
		Description:       cfg.Description,
		IsActive:          cfg.IsActive,
		ActionMappings:    cfg.ActionMappings,
		StateCodeMappings: cfg.StateCodeMappings,
		CreatedAt:         cfg.CreatedAt,
		UpdatedAt:         cfg.UpdatedAt,
	}
}

// suppress unused import if time is only used in helpers
var _ = time.Now
