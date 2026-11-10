package models

import (
	"encoding/json"
	"errors"
	"regexp"
	"strings"
	"time"

	"github.com/google/uuid"
	"gorm.io/datatypes"
	"gorm.io/gorm"
)

// Config errors. Each message is the i18n key (pkg/i18n/messages.go) of its translated text.
var (
	ErrConfigNotFound                 = errors.New("config_not_found")
	ErrConfigExists                   = errors.New("config_exists")
	ErrConfigVersionConflict          = errors.New("config_version_conflict")
	ErrConfigNameRequired             = errors.New("config_name_required")
	ErrConfigNameInvalid              = errors.New("config_name_invalid")
	ErrConfigValueNotJSON             = errors.New("config_value_not_json")
	ErrConfigInvalidEnvironment       = errors.New("config_invalid_environment")
	ErrConfigPublicSecret             = errors.New("config_public_secret")
	ErrConfigAttachmentType           = errors.New("config_attachment_unsupported")
	ErrConfigAttachmentNotFound       = errors.New("config_attachment_not_found")
	ErrConfigAttachmentKeyBad         = errors.New("config_attachment_key_invalid")
	ErrConfigAttachmentModuleReserved = errors.New("config_attachment_module_reserved")
	ErrConfigAttachmentsManaged       = errors.New("config_attachments_managed")
)

const (
	ConfigEnvDev     = "dev"
	ConfigEnvStaging = "staging"
	ConfigEnvProd    = "prod"

	// Config visibility: who may read a config outside the admin routes.
	ConfigVisibilityAdmin         = "admin"         // admin routes only
	ConfigVisibilityAuthenticated = "authenticated" // any logged-in user (GET /configs)
	ConfigVisibilityPublic        = "public"        // anyone, no login (GET /public/configs)

	// ConfigSecretMask replaces the value of is_secret configs in API responses.
	ConfigSecretMask = "******"
)

// ConfigEnvFromAppEnv maps the APP_ENV value (config.Config.Env: "development" | "staging" |
// "production") to the environment name used in the configs table. ok is false for an
// unrecognised value, in which case dev is returned.
func ConfigEnvFromAppEnv(appEnv string) (env string, ok bool) {
	switch strings.ToLower(strings.TrimSpace(appEnv)) {
	case "development", "dev", "local":
		return ConfigEnvDev, true
	case "staging", "stage":
		return ConfigEnvStaging, true
	case "production", "prod":
		return ConfigEnvProd, true
	}
	return ConfigEnvDev, false
}

// ConfigEnvironments lists the environments a config row may belong to.
var ConfigEnvironments = []string{ConfigEnvDev, ConfigEnvStaging, ConfigEnvProd}

// Config is a named setting scoped to an environment. It is identified by (environment, module, name);
// the joined text "module.name" (see CacheKey) is an internal identifier only and is never stored.
type Config struct {
	ID          uuid.UUID      `gorm:"type:uuid;primary_key" json:"id"`
	Environment string         `gorm:"size:32;not null;uniqueIndex:idx_configs_env_module_name,priority:1;index:idx_configs_env;check:environment IN ('dev','staging','prod')" json:"environment"`
	Module      string         `gorm:"size:100;not null;uniqueIndex:idx_configs_env_module_name,priority:2;index:idx_configs_module" json:"module"`
	Name        string         `gorm:"size:100;not null;uniqueIndex:idx_configs_env_module_name,priority:3" json:"name"`
	Value       datatypes.JSON `gorm:"type:jsonb;not null" json:"value"`
	ValueType   string         `gorm:"size:16;not null;default:json" json:"value_type"` // string | number | bool | json (informational)
	Description *string        `gorm:"type:text" json:"description"`
	IsSecret    bool           `gorm:"not null;default:false" json:"is_secret"`
	Visibility  string         `gorm:"size:16;not null;default:authenticated;check:visibility IN ('admin','authenticated','public')" json:"visibility"`
	Version     int            `gorm:"not null;default:1" json:"version"` // bumped on each update
	CreatedAt   time.Time      `gorm:"not null;default:now()" json:"created_at"`
	UpdatedAt   time.Time      `gorm:"not null;default:now()" json:"updated_at"`
}

func (Config) TableName() string { return "configs" }

// ConfigCacheKey is the internal identifier of a config inside an environment: lowercase "module.name".
func ConfigCacheKey(module, name string) string {
	return strings.ToLower(module + "." + name)
}

// CacheKey returns the config's internal identifier ("module.name").
func (c *Config) CacheKey() string { return ConfigCacheKey(c.Module, c.Name) }

func (c *Config) BeforeCreate(tx *gorm.DB) error {
	if c.ID == uuid.Nil {
		c.ID = uuid.New()
	}
	return nil
}

type ConfigCreateRequest struct {
	Environment string         `json:"environment" validate:"omitempty,oneof=dev staging prod"`
	Module      string         `json:"module" validate:"required,max=100"`
	Name        string         `json:"name" validate:"required,max=100"` // trimmed + lowercased, stored as-is
	Value       datatypes.JSON `json:"value" validate:"required"`
	ValueType   string         `json:"value_type" validate:"omitempty,oneof=string number bool json array"`
	Description *string        `json:"description"`
	IsSecret    bool           `json:"is_secret"`
	Visibility  string         `json:"visibility" validate:"omitempty,oneof=admin authenticated public"` // default public (authenticated if is_secret)
}

// ConfigUpdateRequest updates a config in place. environment, module and name are immutable.
// Version is optional; when set it must match the stored version (optimistic locking).
type ConfigUpdateRequest struct {
	Value       datatypes.JSON `json:"value"`
	ValueType   *string        `json:"value_type" validate:"omitempty,oneof=string number bool json array"`
	Description *string        `json:"description"`
	IsSecret    *bool          `json:"is_secret"`
	Visibility  *string        `json:"visibility" validate:"omitempty,oneof=admin authenticated public"`
	Version     *int           `json:"version"`
}

type ConfigListFilter struct {
	Environment string // empty = the server's environment, "all" = every environment (resolved by the service)
	Module      string // exact match, lowercase
	Search      string // matches name or module (case-insensitive substring)
	Page        int
	Limit       int
}

// MaskedValue returns the value to expose in API responses.
func (c *Config) MaskedValue() datatypes.JSON {
	if c.IsSecret {
		return datatypes.JSON(`"` + ConfigSecretMask + `"`)
	}
	return c.Value
}

type ConfigResponse struct {
	ID          uuid.UUID      `json:"id"`
	Environment string         `json:"environment"`
	Module      string         `json:"module"`
	Name        string         `json:"name"`
	Value       datatypes.JSON `json:"value"`
	ValueType   string         `json:"value_type"`
	Description *string        `json:"description"`
	IsSecret    bool           `json:"is_secret"`
	Visibility  string         `json:"visibility"`
	Version     int            `json:"version"`
	CreatedAt   time.Time      `json:"created_at"`
	UpdatedAt   time.Time      `json:"updated_at"`
}

// ConfigPublicResponse is what anonymous callers see: no id, environment, description,
// secret flag or version.
type ConfigPublicResponse struct {
	Module    string         `json:"module"`
	Name      string         `json:"name"`
	Value     datatypes.JSON `json:"value"`
	ValueType string         `json:"value_type"`
	UpdatedAt time.Time      `json:"updated_at"`
}

// Attachment configs: a config whose module ends in "-attachments" and whose value_type is
// "attachments" stores a JSON array of public attachment IDs. Reads expand the ids into
// ConfigAttachmentView objects with generated URLs; only the attachment endpoints write them.
const (
	ConfigAttachmentsSuffix    = "-attachments"
	ConfigValueTypeAttachments = "attachments"
)

// ConfigAttachmentKeyRe is what the module and name passed to the attachment endpoints may be:
// lowercase letters and digits only. The server adds ConfigAttachmentsSuffix to the module itself.
var ConfigAttachmentKeyRe = regexp.MustCompile(`^[a-z0-9]{1,50}$`)

// reservedAttachmentModules can't be used as the module of an attachment config: "system" tags
// uploads that belong to no config module, and "attachment(s)" would only repeat the suffix.
var reservedAttachmentModules = []string{SystemAttachmentDefaultModule, "public", "attachment", "attachments"}

// IsReservedAttachmentModule reports whether base (already lowercased) is a reserved module name.
func IsReservedAttachmentModule(base string) bool {
	for _, r := range reservedAttachmentModules {
		if base == r {
			return true
		}
	}
	return false
}

// AttachmentsModule returns the stored module for an attachment config of base module base.
func AttachmentsModule(base string) string { return base + ConfigAttachmentsSuffix }

// IsAttachmentsModule reports whether module is reserved for attachment configs.
func IsAttachmentsModule(module string) bool {
	return strings.HasSuffix(strings.ToLower(module), ConfigAttachmentsSuffix)
}

// IsAttachments reports whether this config stores attachment ids.
func (c *Config) IsAttachments() bool { return c.ValueType == ConfigValueTypeAttachments }

// ConfigAttachmentView is one attachment as shown inside an attachment config's value.
// The URLs are generated from the stored id on every read, never stored.
type ConfigAttachmentView struct {
	ID         uuid.UUID `json:"id"`
	FileName   string    `json:"file_name"`
	FileSize   int64     `json:"file_size"`
	MimeType   string    `json:"mime_type"`
	URL        string    `json:"url"`
	PreviewURL string    `json:"preview_url"`
}

// AttachmentIDs reads the id list out of a stored attachment config value. ok is false when
// the value is not a JSON array of UUID strings.
func AttachmentIDs(value datatypes.JSON) (ids []uuid.UUID, ok bool) {
	var raw []string
	if len(value) == 0 {
		return nil, true
	}
	if err := json.Unmarshal(value, &raw); err != nil {
		return nil, false
	}
	for _, s := range raw {
		id, err := uuid.Parse(s)
		if err != nil {
			return nil, false
		}
		ids = append(ids, id)
	}
	return ids, true
}

// AttachmentIDsValue builds the stored value for ids ("[]" when empty).
func AttachmentIDsValue(ids []uuid.UUID) datatypes.JSON {
	strs := make([]string, len(ids))
	for i, id := range ids {
		strs[i] = id.String()
	}
	b, _ := json.Marshal(strs)
	return datatypes.JSON(b)
}

// ExpandAttachmentValue turns a stored id list into the list of views, in stored order.
// Ids without a view (deleted files) are skipped; a malformed value yields an empty list.
func ExpandAttachmentValue(value datatypes.JSON, views map[uuid.UUID]ConfigAttachmentView) datatypes.JSON {
	out := []ConfigAttachmentView{}
	if ids, ok := AttachmentIDs(value); ok {
		for _, id := range ids {
			if v, found := views[id]; found {
				out = append(out, v)
			}
		}
	}
	b, _ := json.Marshal(out)
	return datatypes.JSON(b)
}

// AbsolutizeAttachmentValue prefixes the relative url and preview_url of the views in an expanded
// attachment config value with baseURL ("protocol://hostname" of the request). Use it only on
// values of attachment configs. With an empty baseURL, or a value that isn't a view list, the
// value is returned unchanged.
func AbsolutizeAttachmentValue(value datatypes.JSON, baseURL string) datatypes.JSON {
	if baseURL == "" || len(value) == 0 {
		return value
	}
	var views []ConfigAttachmentView
	if json.Unmarshal(value, &views) != nil {
		return value
	}
	for i := range views {
		if strings.HasPrefix(views[i].URL, "/") {
			views[i].URL = baseURL + views[i].URL
		}
		if strings.HasPrefix(views[i].PreviewURL, "/") {
			views[i].PreviewURL = baseURL + views[i].PreviewURL
		}
	}
	b, err := json.Marshal(views)
	if err != nil {
		return value
	}
	return datatypes.JSON(b)
}

// ConfigSettingsAttachmentsModule is the config module whose public entries are returned
// inside the settings response (settings_attachments).
const ConfigSettingsAttachmentsModule = "settings-attachments"

// ConfigBriefResponse is the minimal view of a config: name, value and timestamps. It is the internal
// shape ConfigService.ListPublicByModule returns; the settings response reduces it to name -> URL.
type ConfigBriefResponse struct {
	ValueType string         `json:"-"` // internal: lets callers add the host to attachment URLs
	Name      string         `json:"name"`
	Value     datatypes.JSON `json:"value"`
	CreatedAt time.Time      `json:"created_at"`
	UpdatedAt time.Time      `json:"updated_at"`
}

// ToConfigPublicResponse builds the anonymous view. Callers must have checked IsPublic.
func ToConfigPublicResponse(c *Config) ConfigPublicResponse {
	return ConfigPublicResponse{Module: c.Module, Name: c.Name, Value: c.Value, ValueType: c.ValueType, UpdatedAt: c.UpdatedAt}
}

// IsPublic reports whether anonymous callers may read this config. Secrets never are.
func (c *Config) IsPublic() bool {
	return c.Visibility == ConfigVisibilityPublic && !c.IsSecret
}

func ToConfigResponse(c *Config) ConfigResponse {
	return ConfigResponse{
		ID:          c.ID,
		Environment: c.Environment,
		Module:      c.Module,
		Name:        c.Name,
		Value:       c.MaskedValue(),
		ValueType:   c.ValueType,
		Description: c.Description,
		IsSecret:    c.IsSecret,
		Visibility:  c.Visibility,
		Version:     c.Version,
		CreatedAt:   c.CreatedAt,
		UpdatedAt:   c.UpdatedAt,
	}
}

// ConfigChange describes one field changed by an update. Old/New are already masked
// when the config is (or was) a secret, so it is safe to write to audit logs.
type ConfigChange struct {
	Field string `json:"field"`
	Old   any    `json:"old"`
	New   any    `json:"new"`
}
