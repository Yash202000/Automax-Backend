package models

import (
	"errors"
	"strings"
	"time"

	"github.com/google/uuid"
	"gorm.io/datatypes"
	"gorm.io/gorm"
)

// Config errors. Each message is the i18n key (pkg/i18n/messages.go) of its translated text.
var (
	ErrConfigNotFound           = errors.New("config_not_found")
	ErrConfigExists             = errors.New("config_exists")
	ErrConfigVersionConflict    = errors.New("config_version_conflict")
	ErrConfigNameRequired       = errors.New("config_name_required")
	ErrConfigNameInvalid        = errors.New("config_name_invalid")
	ErrConfigValueNotJSON       = errors.New("config_value_not_json")
	ErrConfigInvalidEnvironment = errors.New("config_invalid_environment")
)

const (
	ConfigEnvDev     = "dev"
	ConfigEnvStaging = "staging"
	ConfigEnvProd    = "prod"

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
}

// ConfigUpdateRequest updates a config in place. environment, module and name are immutable.
// Version is optional; when set it must match the stored version (optimistic locking).
type ConfigUpdateRequest struct {
	Value       datatypes.JSON `json:"value"`
	ValueType   *string        `json:"value_type" validate:"omitempty,oneof=string number bool json array"`
	Description *string        `json:"description"`
	IsSecret    *bool          `json:"is_secret"`
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
	Version     int            `json:"version"`
	CreatedAt   time.Time      `json:"created_at"`
	UpdatedAt   time.Time      `json:"updated_at"`
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
