package models

import (
	"time"

	"github.com/google/uuid"
	"gorm.io/gorm"
)

// IntegrationVariable stores an encrypted key-value secret used by integration scripts.
type IntegrationVariable struct {
	ID          uuid.UUID `gorm:"type:uuid;primary_key" json:"id"`
	Name        string    `gorm:"size:100;not null;uniqueIndex" json:"name"`
	Description string    `gorm:"size:500" json:"description"`
	// EncryptedValue and EncryptedNonce store the AES-256-GCM ciphertext.
	// The plaintext value is NEVER returned in API responses.
	EncryptedValue string `gorm:"type:text;not null" json:"-"`
	EncryptedNonce string `gorm:"type:text;not null" json:"-"`
	IsSecret       bool   `gorm:"default:true" json:"is_secret"`
	CreatedByID    *uuid.UUID `gorm:"type:uuid" json:"created_by_id"`
	CreatedBy      *User      `gorm:"foreignKey:CreatedByID" json:"created_by,omitempty"`
	CreatedAt      time.Time      `json:"created_at"`
	UpdatedAt      time.Time      `json:"updated_at"`
	DeletedAt      gorm.DeletedAt `gorm:"index" json:"-"`
}

func (v *IntegrationVariable) BeforeCreate(tx *gorm.DB) error {
	if v.ID == uuid.Nil {
		v.ID = uuid.New()
	}
	return nil
}

// IntegrationScript defines a reusable script that can be triggered from workflow states or transitions.
// ScriptType is either "http_request" (JSON template) or "javascript" (goja sandbox).
type IntegrationScript struct {
	ID            uuid.UUID `gorm:"type:uuid;primary_key" json:"id"`
	Name          string    `gorm:"size:100;not null" json:"name"`
	Description   string    `gorm:"size:500" json:"description"`
	ScriptType    string    `gorm:"size:20;not null" json:"script_type"` // http_request, javascript
	ScriptContent string    `gorm:"type:text;not null" json:"script_content"`
	// AuthConfig is a JSON blob describing how to authenticate outbound requests.
	// Supported types: none, api_key, bearer, basic, oauth2_client_credentials
	AuthConfig string `gorm:"type:text" json:"auth_config"`
	// BridgeConfig is optional JSON (ScriptBridgeConfig). When set on an http_request script,
	// the executor creates an IncidentBridge record after a successful run.
	BridgeConfig string `gorm:"type:text" json:"bridge_config"`
	IsActive     bool   `gorm:"default:true" json:"is_active"`
	CreatedByID *uuid.UUID `gorm:"type:uuid" json:"created_by_id"`
	CreatedBy   *User      `gorm:"foreignKey:CreatedByID" json:"created_by,omitempty"`
	CreatedAt   time.Time      `json:"created_at"`
	UpdatedAt   time.Time      `json:"updated_at"`
	DeletedAt   gorm.DeletedAt `gorm:"index" json:"-"`
}

func (s *IntegrationScript) BeforeCreate(tx *gorm.DB) error {
	if s.ID == uuid.Nil {
		s.ID = uuid.New()
	}
	return nil
}

// WorkflowStateTrigger attaches an IntegrationScript to a WorkflowState.
// The script fires when an incident enters or exits the state (controlled by TriggerOn).
// ClassificationIDs (via join table) filters which incident classifications trigger this script;
// an empty list means all classifications match.
type WorkflowStateTrigger struct {
	ID                  uuid.UUID      `gorm:"type:uuid;primary_key" json:"id"`
	WorkflowStateID     uuid.UUID      `gorm:"type:uuid;index;not null" json:"workflow_state_id"`
	WorkflowState       *WorkflowState `gorm:"foreignKey:WorkflowStateID" json:"workflow_state,omitempty"`
	IntegrationScriptID uuid.UUID      `gorm:"type:uuid;index;not null" json:"integration_script_id"`
	IntegrationScript   *IntegrationScript `gorm:"foreignKey:IntegrationScriptID" json:"integration_script,omitempty"`
	// TriggerOn: "enter", "exit", or "both"
	TriggerOn string `gorm:"size:10;not null;default:'enter'" json:"trigger_on"`
	// FieldMappings is a JSON array of {source, target} objects.
	// source is a dot-path into the incident (e.g. "title", "incident_number", "classification.name")
	// target is the key name sent in the script context / payload
	FieldMappings  string `gorm:"type:text" json:"field_mappings"`
	ExecutionOrder int    `gorm:"default:0" json:"execution_order"`
	IsAsync        bool   `gorm:"default:true" json:"is_async"`
	IsActive       bool   `gorm:"default:true" json:"is_active"`
	// Classifications filters which incident classifications trigger this script.
	// Empty = all classifications match.
	Classifications []Classification `gorm:"many2many:workflow_state_trigger_classifications;" json:"classifications,omitempty"`
	CreatedAt time.Time      `json:"created_at"`
	UpdatedAt time.Time      `json:"updated_at"`
	DeletedAt gorm.DeletedAt `gorm:"index" json:"-"`
}

func (t *WorkflowStateTrigger) BeforeCreate(tx *gorm.DB) error {
	if t.ID == uuid.Nil {
		t.ID = uuid.New()
	}
	return nil
}

// WorkflowTransitionTrigger attaches an IntegrationScript to a WorkflowTransition.
type WorkflowTransitionTrigger struct {
	ID                    uuid.UUID           `gorm:"type:uuid;primary_key" json:"id"`
	WorkflowTransitionID  uuid.UUID           `gorm:"type:uuid;index;not null" json:"workflow_transition_id"`
	WorkflowTransition    *WorkflowTransition `gorm:"foreignKey:WorkflowTransitionID" json:"workflow_transition,omitempty"`
	IntegrationScriptID   uuid.UUID           `gorm:"type:uuid;index;not null" json:"integration_script_id"`
	IntegrationScript     *IntegrationScript  `gorm:"foreignKey:IntegrationScriptID" json:"integration_script,omitempty"`
	FieldMappings         string              `gorm:"type:text" json:"field_mappings"`
	ExecutionOrder        int                 `gorm:"default:0" json:"execution_order"`
	IsAsync               bool                `gorm:"default:true" json:"is_async"`
	IsActive              bool                `gorm:"default:true" json:"is_active"`
	Classifications       []Classification    `gorm:"many2many:workflow_transition_trigger_classifications;" json:"classifications,omitempty"`
	CreatedAt time.Time      `json:"created_at"`
	UpdatedAt time.Time      `json:"updated_at"`
	DeletedAt gorm.DeletedAt `gorm:"index" json:"-"`
}

func (t *WorkflowTransitionTrigger) BeforeCreate(tx *gorm.DB) error {
	if t.ID == uuid.Nil {
		t.ID = uuid.New()
	}
	return nil
}

// IntegrationExecutionLog records every execution attempt of an IntegrationScript.
// This is immutable — never updated after creation, only inserted.
type IntegrationExecutionLog struct {
	ID                  uuid.UUID  `gorm:"type:uuid;primary_key" json:"id"`
	IntegrationScriptID uuid.UUID  `gorm:"type:uuid;index;not null" json:"integration_script_id"`
	IntegrationScript   *IntegrationScript `gorm:"foreignKey:IntegrationScriptID" json:"integration_script,omitempty"`
	IncidentID          uuid.UUID  `gorm:"type:uuid;index;not null" json:"incident_id"`
	IncidentNumber      string     `gorm:"size:50" json:"incident_number"`
	// TriggerType: "state_enter", "state_exit", "transition"
	TriggerType   string    `gorm:"size:20;not null" json:"trigger_type"`
	TriggerRefID  uuid.UUID `gorm:"type:uuid" json:"trigger_ref_id"` // state or transition UUID
	TriggerRefName string   `gorm:"size:200" json:"trigger_ref_name"` // human-readable name for display
	// Status: pending, running, success, failed, timeout
	Status         string `gorm:"size:20;not null;default:'pending'" json:"status"`
	RequestPayload string `gorm:"type:text" json:"request_payload"`
	ResponseBody   string `gorm:"type:text" json:"response_body"`
	StatusCode     int    `json:"status_code"`
	ErrorMessage   string `gorm:"type:text" json:"error_message"`
	DurationMs     int64  `json:"duration_ms"`
	ExecutedAt     time.Time  `json:"executed_at"`
	CompletedAt    *time.Time `json:"completed_at"`
}

func (l *IntegrationExecutionLog) BeforeCreate(tx *gorm.DB) error {
	if l.ID == uuid.Nil {
		l.ID = uuid.New()
	}
	return nil
}

// FieldMapping maps a source incident field path to a target key name sent to the external system.
type FieldMapping struct {
	Source string `json:"source"` // dot-path: "title", "incident_number", "classification.name"
	Target string `json:"target"` // key in the script context / payload
}

// AuthConfig describes how to authenticate outbound HTTP requests.
type IntegrationAuthConfig struct {
	Type string `json:"type"` // none, api_key, bearer, basic, oauth2_client_credentials

	// api_key
	APIKeyHeader string `json:"api_key_header,omitempty"` // header name, default "X-API-Key"
	APIKeyValue  string `json:"api_key_value,omitempty"`  // may be a {{vars.NAME}} reference

	// bearer
	BearerToken string `json:"bearer_token,omitempty"` // may be a {{vars.NAME}} reference

	// basic
	BasicUsername string `json:"basic_username,omitempty"`
	BasicPassword string `json:"basic_password,omitempty"` // may be a {{vars.NAME}} reference

	// oauth2_client_credentials
	OAuth2TokenURL    string `json:"oauth2_token_url,omitempty"`
	OAuth2ClientID    string `json:"oauth2_client_id,omitempty"`
	OAuth2ClientSecret string `json:"oauth2_client_secret,omitempty"` // may be a {{vars.NAME}} reference
	OAuth2Scope       string `json:"oauth2_scope,omitempty"`
}

// HTTPRequestConfig is the script content for ScriptType="http_request".
type HTTPRequestConfig struct {
	Method  string            `json:"method"`  // GET, POST, PUT, PATCH
	URL     string            `json:"url"`     // supports {{incident.X}} and {{vars.Y}} placeholders
	Headers map[string]string `json:"headers"` // supports placeholders
	Body    interface{}       `json:"body"`    // JSON object or string; supports placeholders
}

// ScriptBridgeConfig is parsed from IntegrationScript.BridgeConfig.
// When set on an http_request script, the executor creates an IncidentBridge record
// after a successful run by extracting fields from the response body.
type ScriptBridgeConfig struct {
	RemoteSystemName    string `json:"remote_system_name"`    // Display name, e.g. "Automax HQ"
	RemoteSystemURL     string `json:"remote_system_url"`     // Base URL of remote system (for deep-link)
	ResponseIDField     string `json:"response_id_field"`     // Dot-path in response JSON, e.g. "id" or "data.id"
	ResponseNumberField string `json:"response_number_field"` // e.g. "incident_number" or "data.incident_number"
}

// IncidentBridge records a linkage between a local incident and an incident on a remote Automax system.
// Created automatically by the executor when a script has bridge_config set and succeeds (outbound),
// or by the webhook handler when a callback arrives (inbound).
type IncidentBridge struct {
	ID                   uuid.UUID  `gorm:"type:uuid;primary_key" json:"id"`
	LocalIncidentID      uuid.UUID  `gorm:"type:uuid;index;not null" json:"local_incident_id"`
	RemoteSystemName     string     `gorm:"size:200;not null" json:"remote_system_name"`
	RemoteSystemURL      string     `gorm:"size:500" json:"remote_system_url"`
	RemoteIncidentID     string     `gorm:"size:200" json:"remote_incident_id"`
	RemoteIncidentNumber string     `gorm:"size:100" json:"remote_incident_number"`
	// Direction: "outbound" = this system pushed to remote, "inbound" = remote pushed to this system
	Direction           string     `gorm:"size:20;not null;default:'outbound'" json:"direction"`
	// Status: "open", "closed", "error"
	Status              string     `gorm:"size:20;not null;default:'open'" json:"status"`
	IntegrationScriptID *uuid.UUID `gorm:"type:uuid" json:"integration_script_id,omitempty"`
	CreatedAt           time.Time  `json:"created_at"`
	UpdatedAt           time.Time  `json:"updated_at"`
	ClosedAt            *time.Time `json:"closed_at,omitempty"`
}

func (b *IncidentBridge) BeforeCreate(tx *gorm.DB) error {
	if b.ID == uuid.Nil {
		b.ID = uuid.New()
	}
	return nil
}

// WebhookCallbackConfig defines how to handle inbound callbacks from remote Automax systems.
// The shared secret is validated on each call; matched action/state_code maps to a local transition.
type WebhookCallbackConfig struct {
	ID          uuid.UUID `gorm:"type:uuid;primary_key" json:"id"`
	Name        string    `gorm:"size:200;not null" json:"name"`
	Description string    `gorm:"size:500" json:"description"`
	IsActive    bool      `gorm:"default:true" json:"is_active"`
	// Shared secret stored encrypted (AES-256-GCM, same pattern as IntegrationVariable).
	SharedSecretEncrypted string `gorm:"type:text;not null" json:"-"`
	SharedSecretNonce     string `gorm:"type:text;not null" json:"-"`
	// ActionMappings is JSON: { "close": "<transition_uuid>", "reopen": "<transition_uuid>" }
	ActionMappings string `gorm:"type:text" json:"action_mappings"`
	// StateCodeMappings is JSON: { "RESOLVED": "<transition_uuid>", "REJECTED": "<transition_uuid>" }
	// Evaluated before ActionMappings when remote_state_code is present.
	StateCodeMappings string    `gorm:"type:text" json:"state_code_mappings"`
	CreatedAt         time.Time `json:"created_at"`
	UpdatedAt         time.Time `json:"updated_at"`
}

func (w *WebhookCallbackConfig) BeforeCreate(tx *gorm.DB) error {
	if w.ID == uuid.Nil {
		w.ID = uuid.New()
	}
	return nil
}

// --- Request / Response types ---

type IntegrationVariableCreateRequest struct {
	Name        string `json:"name" validate:"required,min=1,max=100"`
	Description string `json:"description" validate:"max=500"`
	Value       string `json:"value" validate:"required"`
	IsSecret    *bool  `json:"is_secret"`
}

type IntegrationVariableResponse struct {
	ID          uuid.UUID  `json:"id"`
	Name        string     `json:"name"`
	Description string     `json:"description"`
	IsSecret    bool       `json:"is_secret"`
	CreatedAt   time.Time  `json:"created_at"`
	UpdatedAt   time.Time  `json:"updated_at"`
	CreatedByID *uuid.UUID `json:"created_by_id"`
}

type IntegrationScriptRequest struct {
	Name          string `json:"name" validate:"required,min=1,max=100"`
	Description   string `json:"description" validate:"max=500"`
	ScriptType    string `json:"script_type" validate:"required,oneof=http_request javascript"`
	ScriptContent string `json:"script_content" validate:"required"`
	AuthConfig    string `json:"auth_config"`
	BridgeConfig  string `json:"bridge_config"`
	IsActive      *bool  `json:"is_active"`
}

type IntegrationScriptResponse struct {
	ID            uuid.UUID  `json:"id"`
	Name          string     `json:"name"`
	Description   string     `json:"description"`
	ScriptType    string     `json:"script_type"`
	ScriptContent string     `json:"script_content"`
	AuthConfig    string     `json:"auth_config"`
	BridgeConfig  string     `json:"bridge_config"`
	IsActive      bool       `json:"is_active"`
	CreatedByID   *uuid.UUID `json:"created_by_id"`
	CreatedAt     time.Time  `json:"created_at"`
	UpdatedAt     time.Time  `json:"updated_at"`
}

// --- Webhook / Bridge types ---

type AutomaxCallbackPayload struct {
	SourceIncidentID string `json:"source_incident_id"` // local incident UUID on receiving system
	Action           string `json:"action"`             // "close", "reopen", "update"
	RemoteStateCode  string `json:"remote_state_code"`  // optional; checked before action
	RemoteRef        string `json:"remote_ref"`         // remote incident number for audit
	RemoteSystemName string `json:"remote_system_name"` // optional display name
}

type WebhookCallbackConfigRequest struct {
	Name              string `json:"name" validate:"required,min=1,max=200"`
	Description       string `json:"description" validate:"max=500"`
	IsActive          *bool  `json:"is_active"`
	SharedSecret      string `json:"shared_secret" validate:"required,min=8"`
	ActionMappings    string `json:"action_mappings"`
	StateCodeMappings string `json:"state_code_mappings"`
}

type WebhookCallbackConfigResponse struct {
	ID                uuid.UUID `json:"id"`
	Name              string    `json:"name"`
	Description       string    `json:"description"`
	IsActive          bool      `json:"is_active"`
	ActionMappings    string    `json:"action_mappings"`
	StateCodeMappings string    `json:"state_code_mappings"`
	CreatedAt         time.Time `json:"created_at"`
	UpdatedAt         time.Time `json:"updated_at"`
}

type IncidentBridgeResponse struct {
	ID                   uuid.UUID  `json:"id"`
	LocalIncidentID      uuid.UUID  `json:"local_incident_id"`
	RemoteSystemName     string     `json:"remote_system_name"`
	RemoteSystemURL      string     `json:"remote_system_url"`
	RemoteIncidentID     string     `json:"remote_incident_id"`
	RemoteIncidentNumber string     `json:"remote_incident_number"`
	Direction            string     `json:"direction"`
	Status               string     `json:"status"`
	IntegrationScriptID  *uuid.UUID `json:"integration_script_id,omitempty"`
	CreatedAt            time.Time  `json:"created_at"`
	UpdatedAt            time.Time  `json:"updated_at"`
	ClosedAt             *time.Time `json:"closed_at,omitempty"`
}

type WorkflowStateTriggerRequest struct {
	IntegrationScriptID string   `json:"integration_script_id" validate:"required,uuid"`
	TriggerOn           string   `json:"trigger_on" validate:"required,oneof=enter exit both"`
	FieldMappings       string   `json:"field_mappings"`
	ExecutionOrder      int      `json:"execution_order"`
	IsAsync             *bool    `json:"is_async"`
	IsActive            *bool    `json:"is_active"`
	ClassificationIDs   []string `json:"classification_ids"`
}

type WorkflowTransitionTriggerRequest struct {
	IntegrationScriptID string   `json:"integration_script_id" validate:"required,uuid"`
	FieldMappings       string   `json:"field_mappings"`
	ExecutionOrder      int      `json:"execution_order"`
	IsAsync             *bool    `json:"is_async"`
	IsActive            *bool    `json:"is_active"`
	ClassificationIDs   []string `json:"classification_ids"`
}

type IntegrationScriptTestRequest struct {
	IncidentID string `json:"incident_id" validate:"required,uuid"`
}

type IntegrationExecutionLogResponse struct {
	ID                  uuid.UUID  `json:"id"`
	IntegrationScriptID uuid.UUID  `json:"integration_script_id"`
	ScriptName          string     `json:"script_name,omitempty"`
	IncidentID          uuid.UUID  `json:"incident_id"`
	IncidentNumber      string     `json:"incident_number"`
	TriggerType         string     `json:"trigger_type"`
	TriggerRefID        uuid.UUID  `json:"trigger_ref_id"`
	TriggerRefName      string     `json:"trigger_ref_name"`
	Status              string     `json:"status"`
	RequestPayload      string     `json:"request_payload"`
	ResponseBody        string     `json:"response_body"`
	StatusCode          int        `json:"status_code"`
	ErrorMessage        string     `json:"error_message"`
	DurationMs          int64      `json:"duration_ms"`
	ExecutedAt          time.Time  `json:"executed_at"`
	CompletedAt         *time.Time `json:"completed_at"`
}
