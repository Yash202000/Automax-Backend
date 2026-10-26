package services

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"regexp"
	"slices"
	"strings"
	"time"

	"github.com/automax/backend/internal/config"
	"github.com/automax/backend/internal/models"
	"github.com/automax/backend/internal/repository"
	"github.com/automax/backend/pkg/configcache"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgconn"
	"gorm.io/datatypes"
)

type ConfigService interface {
	Create(ctx context.Context, req *models.ConfigCreateRequest) (*models.ConfigResponse, error)
	Get(ctx context.Context, id uuid.UUID) (*models.ConfigResponse, error)
	// GetByName reads one config of the server's environment (APP_ENV) from the database.
	GetByName(ctx context.Context, module, name string) (*models.ConfigResponse, error)
	List(ctx context.Context, filter *models.ConfigListFilter) ([]models.ConfigResponse, int64, error)
	Update(ctx context.Context, id uuid.UUID, req *models.ConfigUpdateRequest) (*models.ConfigResponse, []models.ConfigChange, error)
	Delete(ctx context.Context, id uuid.UUID) error
	// SeedDefaults inserts the built-in default configs for every environment, skipping keys that already exist.
	SeedDefaults(ctx context.Context) error

	// LoadCache rebuilds the in-memory snapshot from the database. Writes through this service
	// refresh it automatically; StartCacheRefresh additionally reloads it on an interval.
	LoadCache(ctx context.Context) error
	StartCacheRefresh(ctx context.Context, interval time.Duration)

	// Cached, unmasked reads for internal consumers, always for the current environment
	// (derived from APP_ENV). The getters return def when the key is missing or holds a
	// value of another type.
	Environment() string
	GetValue(key string) (datatypes.JSON, bool)
	GetString(key, def string) string
	GetBool(key string, def bool) bool
	GetInt(key string, def int) int

	// Cache-backed API reads (current environment, non-secret only, no defaults).
	GetCached(module, name string) (*models.ConfigResponse, bool)
	ListCached() []models.ConfigResponse
}

// configEnvAll is the list filter value that disables the environment filter.
const configEnvAll = "all"

// configPartRe is what a module or name may contain after trimming and lowercasing.
var configPartRe = regexp.MustCompile(`^[a-z0-9_-]+$`)

// normalizeConfigPart trims and lowercases a module or name.
func normalizeConfigPart(s string) string { return strings.ToLower(strings.TrimSpace(s)) }

type configService struct {
	repo  repository.ConfigRepository
	cfg   *config.Config
	cache *configcache.Cache // in-memory snapshot, see pkg/configcache
}

// NewConfigService creates the service. cfg.Env (APP_ENV) decides which environment the cached
// getters read, and the rest of cfg supplies the values for the seeded defaults.
func NewConfigService(repo repository.ConfigRepository, cfg *config.Config) ConfigService {
	env, ok := models.ConfigEnvFromAppEnv(cfg.Env)
	if !ok {
		log.Printf("[ConfigService] unrecognised APP_ENV %q, reading %q configs", cfg.Env, env)
	}
	return &configService{repo: repo, cfg: cfg, cache: configcache.New(repo, env)}
}

func (s *configService) Create(ctx context.Context, req *models.ConfigCreateRequest) (*models.ConfigResponse, error) {
	module := normalizeConfigPart(req.Module)
	name := normalizeConfigPart(req.Name)
	if module == "" || name == "" {
		return nil, models.ErrConfigNameRequired
	}
	if !configPartRe.MatchString(module) || !configPartRe.MatchString(name) {
		return nil, models.ErrConfigNameInvalid
	}
	if !json.Valid(req.Value) {
		return nil, models.ErrConfigValueNotJSON
	}

	// The environment is optional: blank means the server's own environment (APP_ENV).
	env := strings.ToLower(strings.TrimSpace(req.Environment))
	if env == "" {
		env = s.Environment()
	}
	if !slices.Contains(models.ConfigEnvironments, env) {
		return nil, models.ErrConfigInvalidEnvironment
	}

	// Check first so a duplicate never reaches the unique index as an SQL error. The 23505 handling
	// below only covers two creates racing between this check and the insert.
	if exists, err := s.repo.Exists(ctx, env, module, name); err != nil {
		return nil, err
	} else if exists {
		return nil, models.ErrConfigExists
	}

	cfg := &models.Config{
		Environment: env,
		Module:      module,
		Name:        name,
		Value:       req.Value,
		ValueType:   req.ValueType,
		Description: req.Description,
		IsSecret:    req.IsSecret,
		Version:     1,
	}

	if cfg.ValueType == "" {
		cfg.ValueType = "json"
	}

	if err := s.repo.Create(ctx, cfg); err != nil {
		var pgErr *pgconn.PgError
		if errors.As(err, &pgErr) && pgErr.Code == "23505" {
			return nil, models.ErrConfigExists
		}
		return nil, err
	}
	s.refreshCache(ctx)
	resp := models.ToConfigResponse(cfg)
	return &resp, nil
}

func (s *configService) Get(ctx context.Context, id uuid.UUID) (*models.ConfigResponse, error) {
	cfg, err := s.repo.FindByID(ctx, id)
	if err != nil {
		return nil, err
	}
	resp := models.ToConfigResponse(cfg)
	return &resp, nil
}

func (s *configService) GetByName(ctx context.Context, module, name string) (*models.ConfigResponse, error) {
	cfg, err := s.repo.FindByEnvModuleName(ctx, s.Environment(), normalizeConfigPart(module), normalizeConfigPart(name))
	if err != nil {
		return nil, err
	}
	resp := models.ToConfigResponse(cfg)
	return &resp, nil
}

// List returns configs of the server's environment by default; Environment "all" lists every environment.
func (s *configService) List(ctx context.Context, filter *models.ConfigListFilter) ([]models.ConfigResponse, int64, error) {
	f := *filter
	switch f.Environment {
	case "":
		f.Environment = s.Environment()
	case configEnvAll:
		f.Environment = ""
	}
	cfgs, total, err := s.repo.List(ctx, &f)
	if err != nil {
		return nil, 0, err
	}
	out := make([]models.ConfigResponse, len(cfgs))
	for i := range cfgs {
		out[i] = models.ToConfigResponse(&cfgs[i])
	}
	return out, total, nil
}

func (s *configService) Update(ctx context.Context, id uuid.UUID, req *models.ConfigUpdateRequest) (*models.ConfigResponse, []models.ConfigChange, error) {
	cfg, err := s.repo.FindByID(ctx, id)
	if err != nil {
		return nil, nil, err
	}
	before := *cfg

	expected := cfg.Version
	if req.Version != nil {
		if *req.Version != cfg.Version {
			return nil, nil, models.ErrConfigVersionConflict
		}
		expected = *req.Version
	}

	if len(req.Value) > 0 {
		if !json.Valid(req.Value) {
			return nil, nil, models.ErrConfigValueNotJSON
		}
		cfg.Value = req.Value
	}
	if req.ValueType != nil {
		cfg.ValueType = *req.ValueType
	}
	if req.Description != nil {
		cfg.Description = req.Description
	}
	if req.IsSecret != nil {
		cfg.IsSecret = *req.IsSecret
	}

	if err := s.repo.Update(ctx, cfg, expected); err != nil {
		return nil, nil, err
	}
	s.refreshCache(ctx)
	resp := models.ToConfigResponse(cfg)
	return &resp, diffConfig(&before, cfg), nil
}

// diffConfig lists the fields that differ between before and after. Values are masked
// if the config was or is a secret, so the result never leaks a secret.
func diffConfig(before, after *models.Config) []models.ConfigChange {
	var changes []models.ConfigChange
	secret := before.IsSecret || after.IsSecret

	if !jsonEqual(before.Value, after.Value) {
		c := models.ConfigChange{Field: "value", Old: json.RawMessage(before.Value), New: json.RawMessage(after.Value)}
		if secret {
			c.Old, c.New = models.ConfigSecretMask, models.ConfigSecretMask
		}
		changes = append(changes, c)
	}
	if before.ValueType != after.ValueType {
		changes = append(changes, models.ConfigChange{Field: "value_type", Old: before.ValueType, New: after.ValueType})
	}
	if deref(before.Description) != deref(after.Description) {
		changes = append(changes, models.ConfigChange{Field: "description", Old: deref(before.Description), New: deref(after.Description)})
	}
	if before.IsSecret != after.IsSecret {
		changes = append(changes, models.ConfigChange{Field: "is_secret", Old: before.IsSecret, New: after.IsSecret})
	}
	return changes
}

func jsonEqual(a, b []byte) bool {
	var ca, cb bytes.Buffer
	if json.Compact(&ca, a) != nil || json.Compact(&cb, b) != nil {
		return string(a) == string(b)
	}
	return ca.String() == cb.String()
}

func (s *configService) Delete(ctx context.Context, id uuid.UUID) error {
	if err := s.repo.Delete(ctx, id); err != nil {
		return err
	}
	s.refreshCache(ctx)
	return nil
}

func (s *configService) SeedDefaults(ctx context.Context) error {
	defaults := buildConfigDefaults(s.cfg)
	rows := make([]models.Config, 0, len(defaults)*len(models.ConfigEnvironments))
	for _, env := range models.ConfigEnvironments {
		for _, d := range defaults {
			value, err := marshalConfigValue(d.Value)
			if err != nil {
				return fmt.Errorf("seed default config %q: %w", d.Key, err)
			}
			desc := d.Description
			rows = append(rows, models.Config{
				Environment: env,
				Module:      configModule(d.Key),
				Name:        configName(d.Key),
				Value:       datatypes.JSON(value),
				ValueType:   d.ValueType,
				Description: &desc,
				Version:     1,
			})
		}
	}
	if err := s.repo.CreateIfMissing(ctx, rows); err != nil {
		return fmt.Errorf("seed default configs: %w", err)
	}
	s.refreshCache(ctx)
	log.Printf("Default configs ensured (%d keys x %d environments)", len(defaults), len(models.ConfigEnvironments))
	return nil
}

// Cache access. The snapshot lives in pkg/configcache; these delegate to it so handlers and
// other services only depend on ConfigService.

func (s *configService) LoadCache(ctx context.Context) error { return s.cache.Load(ctx) }

func (s *configService) StartCacheRefresh(ctx context.Context, interval time.Duration) {
	s.cache.StartRefresh(ctx, interval)
}

// refreshCache is called after every successful write. A failure is only logged: the write
// already succeeded and the periodic reload will repair the snapshot.
func (s *configService) refreshCache(ctx context.Context) {
	if err := s.cache.Load(ctx); err != nil {
		log.Printf("[ConfigService] cache reload after write failed: %v", err)
	}
}

func (s *configService) Environment() string { return s.cache.Environment() }

func (s *configService) GetValue(key string) (datatypes.JSON, bool) { return s.cache.Value(key) }

func (s *configService) GetString(key, def string) string { return s.cache.String(key, def) }

func (s *configService) GetBool(key string, def bool) bool { return s.cache.Bool(key, def) }

func (s *configService) GetInt(key string, def int) int { return s.cache.Int(key, def) }

func (s *configService) GetCached(module, name string) (*models.ConfigResponse, bool) {
	return s.cache.Get(module, name)
}

func (s *configService) ListCached() []models.ConfigResponse { return s.cache.List() }
