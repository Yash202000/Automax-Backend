package services

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"log"
	"mime/multipart"
	"slices"
	"strings"
	"time"

	"github.com/automax/backend/internal/config"
	"github.com/automax/backend/internal/models"
	"github.com/automax/backend/internal/repository"
	"github.com/automax/backend/pkg/configcache"
	"github.com/automax/backend/pkg/utils"
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

	// SetAttachmentStore enables attachment configs (module "<x>-attachments", value = attachment
	// ids): reads expand the ids into views with generated URLs and Delete removes the files.
	SetAttachmentStore(store AttachmentStore)
	// GetRaw reads one config of the server's environment exactly as stored (unmasked, ids not
	// expanded). Internal use by the attachment service.
	GetRaw(ctx context.Context, module, name string) (*models.Config, error)
	// CreateAttachmentsConfig creates an empty, public attachment config. The normal Create
	// refuses "-attachments" modules; this is the only way to make one.
	CreateAttachmentsConfig(ctx context.Context, module, name string) (*models.Config, error)
	// SetValue replaces the value of the server-environment config module.name through the normal
	// update path (version bump, cache refresh, diff). expectedVersion nil means "whatever is stored".
	SetValue(ctx context.Context, module, name string, value datatypes.JSON, expectedVersion *int) (*models.ConfigResponse, []models.ConfigChange, error)

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
	GetCached(ctx context.Context, module, name string) (*models.ConfigResponse, bool)
	ListCached(ctx context.Context) []models.ConfigResponse

	// Anonymous reads: only configs with visibility "public" that are not secret.
	GetPublic(ctx context.Context, module, name string) (*models.ConfigPublicResponse, bool)
	ListPublic(ctx context.Context) []models.ConfigPublicResponse
	// ListPublicByModule returns name, value and timestamps of a module's public configs.
	ListPublicByModule(ctx context.Context, module string) []models.ConfigBriefResponse
}

// configEnvAll is the list filter value that disables the environment filter.
const configEnvAll = "all"

// normalizeConfigPart trims and lowercases a module or name.
func normalizeConfigPart(s string) string { return strings.ToLower(strings.TrimSpace(s)) }

// AttachmentStore is what attachment configs need from the public attachment storage.
// *SystemAttachmentService implements it.
type AttachmentStore interface {
	UploadImages(ctx context.Context, module string, files []*multipart.FileHeader, userID uuid.UUID) (uuid.UUID, []models.SystemAttachment, error)
	Resolve(ctx context.Context, ids []uuid.UUID) (map[uuid.UUID]models.ConfigAttachmentView, error)
	DeleteByIDs(ctx context.Context, ids []uuid.UUID) error
}

type configService struct {
	repo        repository.ConfigRepository
	cfg         *config.Config
	cache       *configcache.Cache // in-memory snapshot, see pkg/configcache
	attachments AttachmentStore    // optional; see SetAttachmentStore
}

func (s *configService) SetAttachmentStore(store AttachmentStore) {
	s.attachments = store
	s.cache.SetResolver(store)
}

// responses builds API responses, expanding the attachment ids of attachment configs into
// views (one query for the whole batch). Without a store the stored value is returned as is.
func (s *configService) responses(ctx context.Context, cfgs []models.Config) []models.ConfigResponse {
	out := make([]models.ConfigResponse, len(cfgs))
	for i := range cfgs {
		out[i] = models.ToConfigResponse(&cfgs[i])
	}
	if s.attachments == nil {
		return out
	}
	var ids []uuid.UUID
	for i := range cfgs {
		if cfgs[i].IsAttachments() {
			if got, ok := models.AttachmentIDs(cfgs[i].Value); ok {
				ids = append(ids, got...)
			}
		}
	}
	if len(ids) == 0 {
		return out
	}
	views, err := s.attachments.Resolve(ctx, ids)
	if err != nil {
		log.Printf("[ConfigService] resolving attachments failed: %v", err)
		views = nil
	}
	for i := range cfgs {
		if cfgs[i].IsAttachments() {
			out[i].Value = absolutize(ctx, cfgs[i].ValueType, models.ExpandAttachmentValue(cfgs[i].Value, views))
		}
	}
	return out
}

func (s *configService) response(ctx context.Context, cfg *models.Config) models.ConfigResponse {
	return s.responses(ctx, []models.Config{*cfg})[0]
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
	cfg, err := s.createConfig(ctx, req, false)
	if err != nil {
		return nil, err
	}
	resp := s.response(ctx, cfg)
	return &resp, nil
}

func (s *configService) CreateAttachmentsConfig(ctx context.Context, module, name string) (*models.Config, error) {
	return s.createConfig(ctx, &models.ConfigCreateRequest{
		Module:     module,
		Name:       name,
		Value:      models.AttachmentIDsValue(nil),
		ValueType:  models.ConfigValueTypeAttachments,
		Visibility: models.ConfigVisibilityPublic,
	}, true)
}

// createConfig validates and inserts a config. Attachment configs ("-attachments" modules, value
// type "attachments") are server-managed: only internal callers may create them.
func (s *configService) createConfig(ctx context.Context, req *models.ConfigCreateRequest, internal bool) (*models.Config, error) {
	module := normalizeConfigPart(req.Module)
	name := normalizeConfigPart(req.Name)
	if module == "" || name == "" {
		return nil, models.ErrConfigNameRequired
	}
	if !internal && (models.IsAttachmentsModule(module) || req.ValueType == models.ConfigValueTypeAttachments) {
		return nil, models.ErrConfigAttachmentsManaged
	}
	// Same rules as the attachment endpoints. The one hyphen allowed is the "-attachments" suffix the
	// server adds to an attachment config's module, so it is validated without it.
	keyModule, maxModule := module, models.ConfigKeyMaxLen
	if internal {
		keyModule, maxModule = strings.TrimSuffix(module, models.ConfigAttachmentsSuffix), models.ConfigAttachmentBaseMaxLen
	}
	if !models.ValidConfigModule(keyModule, maxModule) || !models.ValidConfigName(name, models.ConfigKeyMaxLen) {
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

	// No visibility given: public, except that a secret config is never public.
	visibility := strings.ToLower(strings.TrimSpace(req.Visibility))
	if visibility == "" {
		visibility = models.ConfigVisibilityPublic
		if req.IsSecret {
			visibility = models.ConfigVisibilityAuthenticated
		}
	}
	if visibility == models.ConfigVisibilityPublic && req.IsSecret {
		return nil, models.ErrConfigPublicSecret
	}

	cfg := &models.Config{
		Environment: env,
		Module:      module,
		Name:        name,
		Value:       req.Value,
		ValueType:   req.ValueType,
		Description: req.Description,
		IsSecret:    req.IsSecret,
		Visibility:  visibility,
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
	return cfg, nil
}

func (s *configService) Get(ctx context.Context, id uuid.UUID) (*models.ConfigResponse, error) {
	cfg, err := s.repo.FindByID(ctx, id)
	if err != nil {
		return nil, err
	}
	resp := s.response(ctx, cfg)
	return &resp, nil
}

func (s *configService) GetByName(ctx context.Context, module, name string) (*models.ConfigResponse, error) {
	cfg, err := s.repo.FindByEnvModuleName(ctx, s.Environment(), normalizeConfigPart(module), normalizeConfigPart(name))
	if err != nil {
		return nil, err
	}
	resp := s.response(ctx, cfg)
	return &resp, nil
}

func (s *configService) GetRaw(ctx context.Context, module, name string) (*models.Config, error) {
	return s.repo.FindByEnvModuleName(ctx, s.Environment(), normalizeConfigPart(module), normalizeConfigPart(name))
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
	return s.responses(ctx, cfgs), total, nil
}

func (s *configService) Update(ctx context.Context, id uuid.UUID, req *models.ConfigUpdateRequest) (*models.ConfigResponse, []models.ConfigChange, error) {
	cfg, err := s.repo.FindByID(ctx, id)
	if err != nil {
		return nil, nil, err
	}
	// The value of an attachment config is a list of attachment ids kept in step with the stored
	// files, so it can only change through the attachment endpoints. Visibility and description
	// can still be edited here.
	if cfg.IsAttachments() || models.IsAttachmentsModule(cfg.Module) {
		if len(req.Value) > 0 || req.ValueType != nil {
			return nil, nil, models.ErrConfigAttachmentsManaged
		}
	}
	return s.update(ctx, cfg, req)
}

// update applies req to cfg and saves it (version check, cache refresh, diff). It has no
// attachment guard: it is also the path the attachment service writes through.
func (s *configService) update(ctx context.Context, cfg *models.Config, req *models.ConfigUpdateRequest) (*models.ConfigResponse, []models.ConfigChange, error) {
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
	if req.Visibility != nil {
		cfg.Visibility = strings.ToLower(strings.TrimSpace(*req.Visibility))
	}
	if cfg.Visibility == models.ConfigVisibilityPublic && cfg.IsSecret {
		return nil, nil, models.ErrConfigPublicSecret
	}
	if cfg.IsAttachments() && cfg.IsSecret {
		return nil, nil, models.ErrConfigAttachmentType // attachments are public files, never secrets
	}

	if err := s.repo.Update(ctx, cfg, expected); err != nil {
		return nil, nil, err
	}
	s.refreshCache(ctx)
	resp := s.response(ctx, cfg)
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
	if before.Visibility != after.Visibility {
		changes = append(changes, models.ConfigChange{Field: "visibility", Old: before.Visibility, New: after.Visibility})
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

func (s *configService) SetValue(ctx context.Context, module, name string, value datatypes.JSON, expectedVersion *int) (*models.ConfigResponse, []models.ConfigChange, error) {
	cfg, err := s.GetRaw(ctx, module, name)
	if err != nil {
		return nil, nil, err
	}
	return s.update(ctx, cfg, &models.ConfigUpdateRequest{Value: value, Version: expectedVersion})
}

// Delete removes a config. For an attachment config the stored files are deleted as well.
func (s *configService) Delete(ctx context.Context, id uuid.UUID) error {
	cfg, err := s.repo.FindByID(ctx, id)
	if err != nil {
		return err
	}
	if err := s.repo.Delete(ctx, id); err != nil {
		return err
	}
	if cfg.IsAttachments() && s.attachments != nil {
		if ids, ok := models.AttachmentIDs(cfg.Value); ok {
			if err := s.attachments.DeleteByIDs(ctx, ids); err != nil {
				log.Printf("[ConfigService] deleting files of config %s/%s failed: %v", cfg.Module, cfg.Name, err)
			}
		}
	}
	s.refreshCache(ctx)
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

// Cache-backed reads. The cache keeps attachment URLs relative (it is shared by every request);
// each response gets the host of the request that asked for it.

// absolutize adds the request's protocol://hostname to the attachment URLs of an attachment
// config value; other values pass through untouched.
func absolutize(ctx context.Context, valueType string, v datatypes.JSON) datatypes.JSON {
	if valueType != models.ConfigValueTypeAttachments {
		return v
	}
	return models.AbsolutizeAttachmentValue(v, utils.RequestBaseURL(ctx))
}

func (s *configService) GetCached(ctx context.Context, module, name string) (*models.ConfigResponse, bool) {
	resp, ok := s.cache.Get(module, name)
	if ok {
		resp.Value = absolutize(ctx, resp.ValueType, resp.Value)
	}
	return resp, ok
}

func (s *configService) ListCached(ctx context.Context) []models.ConfigResponse {
	out := s.cache.List()
	for i := range out {
		out[i].Value = absolutize(ctx, out[i].ValueType, out[i].Value)
	}
	return out
}

func (s *configService) GetPublic(ctx context.Context, module, name string) (*models.ConfigPublicResponse, bool) {
	resp, ok := s.cache.GetPublic(module, name)
	if ok {
		resp.Value = absolutize(ctx, resp.ValueType, resp.Value)
	}
	return resp, ok
}

func (s *configService) ListPublic(ctx context.Context) []models.ConfigPublicResponse {
	out := s.cache.ListPublic()
	for i := range out {
		out[i].Value = absolutize(ctx, out[i].ValueType, out[i].Value)
	}
	return out
}

func (s *configService) ListPublicByModule(ctx context.Context, module string) []models.ConfigBriefResponse {
	out := s.cache.ListPublicByModule(module)
	for i := range out {
		out[i].Value = absolutize(ctx, out[i].ValueType, out[i].Value)
	}
	return out
}
