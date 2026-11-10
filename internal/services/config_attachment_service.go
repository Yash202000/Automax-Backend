package services

import (
	"context"
	"errors"
	"mime/multipart"
	"slices"
	"strings"

	"github.com/automax/backend/internal/models"
	"github.com/google/uuid"
)

// ConfigAttachmentService manages attachment configs: configs in module "<module>-attachments"
// whose value is the list of ids of public attachments (uploaded images). Like incident
// attachments, only the id is stored; URLs are generated from it when the config is read.
//
// The caller passes the base module ("settings") and the name ("logo"); the service adds the
// "-attachments" suffix and creates the config on the first upload. Writes go through
// ConfigService.SetValue, so versioning, optimistic locking, the cache refresh and the audit
// diff work exactly as for any other config update.
type ConfigAttachmentService struct {
	configs ConfigService
	store   AttachmentStore
}

func NewConfigAttachmentService(configs ConfigService, store AttachmentStore) *ConfigAttachmentService {
	return &ConfigAttachmentService{configs: configs, store: store}
}

// attachmentsKey validates the base module and name (lowercase letters and digits only) and
// returns the stored module and the name.
func attachmentsKey(base, name string) (module, key string, err error) {
	base, key = strings.ToLower(strings.TrimSpace(base)), strings.ToLower(strings.TrimSpace(name))
	if !models.ConfigAttachmentKeyRe.MatchString(base) || !models.ConfigAttachmentKeyRe.MatchString(key) {
		return "", "", models.ErrConfigAttachmentKeyBad
	}
	if models.IsReservedAttachmentModule(base) {
		return "", "", models.ErrConfigAttachmentModuleReserved
	}
	return models.AttachmentsModule(base), key, nil
}

// load returns the attachment config and its current ids. With create set, a missing config is
// created empty (and a lost create race falls back to reading the winner).
func (s *ConfigAttachmentService) load(ctx context.Context, module, name string, create bool) (*models.Config, []uuid.UUID, error) {
	cfg, err := s.configs.GetRaw(ctx, module, name)
	if errors.Is(err, models.ErrConfigNotFound) && create {
		cfg, err = s.configs.CreateAttachmentsConfig(ctx, module, name)
		if errors.Is(err, models.ErrConfigExists) {
			cfg, err = s.configs.GetRaw(ctx, module, name)
		}
	}
	if err != nil {
		return nil, nil, err
	}
	if !cfg.IsAttachments() || cfg.IsSecret {
		return nil, nil, models.ErrConfigAttachmentType
	}
	ids, ok := models.AttachmentIDs(cfg.Value)
	if !ok {
		return nil, nil, models.ErrConfigAttachmentType
	}
	return cfg, ids, nil
}

// Add uploads images and appends them to the config (created on first use).
func (s *ConfigAttachmentService) Add(ctx context.Context, base, name string, files []*multipart.FileHeader, userID uuid.UUID) (*models.ConfigResponse, []models.ConfigChange, error) {
	return s.save(ctx, base, name, files, userID, false)
}

// Replace uploads images and makes them the whole set; the previous files are deleted once the
// config has been updated.
func (s *ConfigAttachmentService) Replace(ctx context.Context, base, name string, files []*multipart.FileHeader, userID uuid.UUID) (*models.ConfigResponse, []models.ConfigChange, error) {
	return s.save(ctx, base, name, files, userID, true)
}

func (s *ConfigAttachmentService) save(ctx context.Context, base, name string, files []*multipart.FileHeader, userID uuid.UUID, replace bool) (*models.ConfigResponse, []models.ConfigChange, error) {
	module, key, err := attachmentsKey(base, name)
	if err != nil {
		return nil, nil, err
	}
	cfg, current, err := s.load(ctx, module, key, true)
	if err != nil {
		return nil, nil, err
	}

	_, records, err := s.store.UploadImages(ctx, strings.TrimSuffix(module, models.ConfigAttachmentsSuffix), files, userID) // the normalised base module
	if err != nil {
		return nil, nil, err
	}
	added := make([]uuid.UUID, len(records))
	for i := range records {
		added[i] = records[i].ID
	}

	next := added
	if !replace {
		next = append(slices.Clone(current), added...)
	}

	version := cfg.Version
	resp, changes, err := s.configs.SetValue(ctx, module, key, models.AttachmentIDsValue(next), &version)
	if err != nil {
		_ = s.store.DeleteByIDs(ctx, added) // don't leave the new files orphaned
		return nil, nil, err
	}
	if replace {
		_ = s.store.DeleteByIDs(ctx, current) // best effort, after the config no longer points at them
	}
	return resp, changes, nil
}

// Remove drops one attachment from the config and deletes its file.
func (s *ConfigAttachmentService) Remove(ctx context.Context, base, name string, attachmentID uuid.UUID) (*models.ConfigResponse, []models.ConfigChange, error) {
	module, key, err := attachmentsKey(base, name)
	if err != nil {
		return nil, nil, err
	}
	cfg, current, err := s.load(ctx, module, key, false)
	if err != nil {
		return nil, nil, err
	}
	if !slices.Contains(current, attachmentID) {
		return nil, nil, models.ErrConfigAttachmentNotFound
	}
	next := slices.DeleteFunc(slices.Clone(current), func(id uuid.UUID) bool { return id == attachmentID })

	version := cfg.Version
	resp, changes, err := s.configs.SetValue(ctx, module, key, models.AttachmentIDsValue(next), &version)
	if err != nil {
		return nil, nil, err
	}
	_ = s.store.DeleteByIDs(ctx, []uuid.UUID{attachmentID})
	return resp, changes, nil
}
