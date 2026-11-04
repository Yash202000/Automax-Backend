package services

import (
	"context"
	"encoding/json"
	"errors"
	"log"
	"strconv"
	"strings"

	"github.com/automax/backend/internal/models"
	"github.com/automax/backend/internal/repository"
	"github.com/google/uuid"
	"gorm.io/gorm"
)

type NotificationTemplateService interface {
	Create(ctx context.Context, req *models.NotificationTemplateCreateRequest) (*models.NotificationTemplate, error)
	GetByID(ctx context.Context, id uuid.UUID) (*models.NotificationTemplate, error)
	List(ctx context.Context, filter models.NotificationTemplateFilter) ([]models.NotificationTemplate, int64, error)
	Update(ctx context.Context, id uuid.UUID, req *models.NotificationTemplateUpdateRequest) (*models.NotificationTemplate, error)
	ToggleActive(ctx context.Context, id uuid.UUID) (*models.NotificationTemplate, error)
	Delete(ctx context.Context, id uuid.UUID) error
	GetByCode(ctx context.Context, code, channel string) ([]models.NotificationTemplate, error)
	GetByTransitionID(ctx context.Context, transitionID uuid.UUID) ([]models.NotificationTemplate, error)
}

type notificationTemplateService struct {
	repo repository.NotificationTemplateRepository
	db   *gorm.DB
}

func NewNotificationTemplateService(repo repository.NotificationTemplateRepository, db *gorm.DB) NotificationTemplateService {
	return &notificationTemplateService{repo: repo, db: db}
}

// Create stores a bilingual template as a single DB row.
func (s *notificationTemplateService) Create(ctx context.Context, req *models.NotificationTemplateCreateRequest) (*models.NotificationTemplate, error) {
	if req.BodyEN == "" && req.BodyAR == "" {
		return nil, errors.New("At least one of body_en or body_ar is required")
	}
	if req.Code == "" || req.Channel == "" {
		return nil, errors.New("code and channel are required")
	}

	if err := validateProviderTemplateID(req.Channel, req.ProviderTemplateID); err != nil {
		log.Printf("[SMS] stage=template_config action=create REJECTED code=%q provider_template_id=%q: %v", req.Code, req.ProviderTemplateID, err)
		return nil, err
	}

	exists, err := s.repo.ExistsByCodeAndChannel(ctx, req.Code, req.Channel, nil)
	if err != nil {
		return nil, err
	}
	if exists {
		return nil, errors.New("a template with this code and channel already exists")
	}

	tpl := &models.NotificationTemplate{
		Name:               req.Name,
		Code:               req.Code,
		Channel:            req.Channel,
		ModuleType:         req.ModuleType,
		ActionType:         req.ActionType,
		SubjectEN:          req.SubjectEN,
		BodyEN:             req.BodyEN,
		SubjectAR:          req.SubjectAR,
		BodyAR:             req.BodyAR,
		Variables:          ResolveTemplateVariables(req.Variables, req.BodyEN, req.BodyAR),
		ProviderTemplateID: req.ProviderTemplateID,
		TransitionID:       req.TransitionID,
		IsActive:           req.IsActive,
	}

	if err := s.repo.Create(ctx, tpl); err != nil {
		if tpl.Channel == models.TemplateChannelSMS {
			log.Printf("[SMS] stage=template_config action=create FAILED code=%q: %v", tpl.Code, err)
		}
		return nil, err
	}
	logSMSTemplateConfig("create", tpl, "")
	return tpl, nil
}

func (s *notificationTemplateService) GetByID(ctx context.Context, id uuid.UUID) (*models.NotificationTemplate, error) {
	return s.repo.FindByID(ctx, id)
}

func (s *notificationTemplateService) List(ctx context.Context, filter models.NotificationTemplateFilter) ([]models.NotificationTemplate, int64, error) {
	return s.repo.ListWithFilters(ctx, filter)
}

func (s *notificationTemplateService) Update(ctx context.Context, id uuid.UUID, req *models.NotificationTemplateUpdateRequest) (*models.NotificationTemplate, error) {
	tpl, err := s.repo.FindByID(ctx, id)
	if err != nil {
		return nil, err
	}

	oldProviderID := tpl.ProviderTemplateID
	if req.Name != nil {
		tpl.Name = *req.Name
	}
	if req.SubjectEN != nil {
		tpl.SubjectEN = *req.SubjectEN
	}

	if req.BodyEN != nil && *req.BodyEN == "" && req.BodyAR != nil && *req.BodyAR == "" {
		return nil, errors.New("body cannot be empty")
	}

	if req.BodyEN != nil {
		tpl.BodyEN = *req.BodyEN
	}

	if req.SubjectAR != nil {
		tpl.SubjectAR = *req.SubjectAR
	}
	if req.BodyAR != nil {
		tpl.BodyAR = *req.BodyAR
	}
	if req.ModuleType != nil {
		tpl.ModuleType = *req.ModuleType
	}
	if req.ActionType != nil {
		tpl.ActionType = *req.ActionType
	}
	if req.Variables != nil {
		tpl.Variables = *req.Variables
	}
	// Keep the variables column filled from the body when it is empty (never overrides an explicit list).
	tpl.Variables = ResolveTemplateVariables(tpl.Variables, tpl.BodyEN, tpl.BodyAR)
	if req.ProviderTemplateID != nil {
		tpl.ProviderTemplateID = *req.ProviderTemplateID
	}
	if req.TransitionID != nil {
		tpl.TransitionID = req.TransitionID
	}
	if req.IsActive != nil {
		tpl.IsActive = *req.IsActive
	}
	if req.Channel != "" && req.Channel != tpl.Channel {
		exists, err := s.repo.ExistsByCodeAndChannel(ctx, tpl.Code, req.Channel, &tpl.ID)
		if err != nil {
			return nil, err
		}
		if exists {
			return nil, errors.New("a template with this code and channel already exists")
		}
		tpl.Channel = req.Channel
	}
	if err := validateProviderTemplateID(tpl.Channel, tpl.ProviderTemplateID); err != nil {
		log.Printf("[SMS] stage=template_config action=update REJECTED id=%s code=%q provider_template_id=%q: %v", tpl.ID, tpl.Code, tpl.ProviderTemplateID, err)
		return nil, err
	}

	if err := s.repo.Update(ctx, tpl); err != nil {
		if tpl.Channel == models.TemplateChannelSMS {
			log.Printf("[SMS] stage=template_config action=update FAILED id=%s code=%q: %v", tpl.ID, tpl.Code, err)
		}
		return nil, err
	}
	logSMSTemplateConfig("update", tpl, oldProviderID)
	return tpl, nil
}

func (s *notificationTemplateService) ToggleActive(ctx context.Context, id uuid.UUID) (*models.NotificationTemplate, error) {
	tpl, err := s.repo.FindByID(ctx, id)
	if err != nil {
		return nil, err
	}
	tpl.IsActive = !tpl.IsActive
	if err := s.repo.Update(ctx, tpl); err != nil {
		if tpl.Channel == models.TemplateChannelSMS {
			log.Printf("[SMS] stage=template_config action=toggle FAILED id=%s code=%q: %v", tpl.ID, tpl.Code, err)
		}
		return nil, err
	}
	logSMSTemplateConfig("toggle", tpl, tpl.ProviderTemplateID)
	return tpl, nil
}

func (s *notificationTemplateService) Delete(ctx context.Context, id uuid.UUID) error {
	tpl, _ := s.repo.FindByID(ctx, id) // only for the log line; the delete itself does not depend on it
	if err := s.repo.Delete(ctx, id); err != nil {
		log.Printf("[SMS] stage=template_config action=delete FAILED id=%s: %v", id, err)
		return err
	}
	if tpl != nil && tpl.Channel == models.TemplateChannelSMS {
		log.Printf("[SMS] stage=template_config action=delete OK id=%s code=%q provider_template_id=%q — SMS using this code will now be refused (template not found)", tpl.ID, tpl.Code, tpl.ProviderTemplateID)
	}
	return nil
}

// logSMSTemplateConfig records an SMS template change that affects routing: the Amana ID (old → new),
// the resulting route, and warnings for configurations that will misbehave at send time.
// Non-SMS templates are not logged here.
func logSMSTemplateConfig(action string, tpl *models.NotificationTemplate, oldProviderID string) {
	if tpl == nil || tpl.Channel != models.TemplateChannelSMS {
		return
	}
	newID := strings.TrimSpace(tpl.ProviderTemplateID)
	route := "REFUSED (no provider_template_id)"
	if newID != "" {
		route = "amana template_id=" + newID
	}
	log.Printf("[SMS] stage=template_config action=%s OK id=%s code=%q active=%t provider_template_id=%q→%q route=%s variables=%q",
		action, tpl.ID, tpl.Code, tpl.IsActive, strings.TrimSpace(oldProviderID), newID, route, tpl.Variables)
	for _, w := range smsTemplateWarnings(tpl) {
		log.Printf("[SMS] stage=template_config WARN id=%s code=%q: %s", tpl.ID, tpl.Code, w)
	}
}

// smsTemplateWarnings flags SMS template settings that are valid but risky for sending.
func smsTemplateWarnings(tpl *models.NotificationTemplate) []string {
	var warns []string
	if tpl.IsActive && strings.TrimSpace(tpl.ProviderTemplateID) == "" {
		warns = append(warns, "active SMS template has no provider_template_id — sending it will be refused (template_not_configured)")
	}
	body := tpl.BodyEN
	if body == "" {
		body = tpl.BodyAR
	}
	if declared := ParseTemplateVariables(tpl.Variables); len(declared) > 0 {
		used := map[string]bool{}
		for _, n := range ExtractPlaceholders(body) {
			used[strings.ToLower(n)] = true
		}
		var missing []string
		for _, n := range declared {
			if !used[strings.ToLower(n)] {
				missing = append(missing, n)
			}
		}
		if len(missing) > 0 {
			warns = append(warns, "variables not used in the body: "+strings.Join(missing, ", ")+" — check the order/names expected by the Amana template")
		}
	}
	return warns
}

func (s *notificationTemplateService) GetByCode(ctx context.Context, code, channel string) ([]models.NotificationTemplate, error) {
	return s.repo.FindAllByCode(ctx, code, channel)
}

func (s *notificationTemplateService) GetByTransitionID(ctx context.Context, transitionID uuid.UUID) ([]models.NotificationTemplate, error) {
	return s.repo.FindByTransitionID(ctx, transitionID)
}

// ResolveTemplateVariables returns the variables to store for a template.
// An explicit, non-empty list is kept untouched. Otherwise the {{placeholders}} found in the body
// (English first, Arabic as fallback) are stored as a JSON array in order of appearance, which is
// the same order the SMS sender already falls back to, so sending behaviour does not change.
func ResolveTemplateVariables(explicit, bodyEN, bodyAR string) string {
	if strings.TrimSpace(explicit) != "" {
		return explicit
	}
	body := bodyEN
	if strings.TrimSpace(body) == "" {
		body = bodyAR
	}
	names := ExtractPlaceholders(body)
	if len(names) == 0 {
		return explicit
	}
	encoded, err := json.Marshal(names)
	if err != nil {
		return explicit
	}
	return string(encoded)
}

// validateProviderTemplateID checks that an SMS template's provider (Amana) template id, when set, is a
// positive number. It is optional to save, but an SMS template without one cannot be sent.
func validateProviderTemplateID(channel, id string) error {
	id = strings.TrimSpace(id)
	if channel != models.TemplateChannelSMS || id == "" {
		return nil
	}
	if n, err := strconv.Atoi(id); err != nil || n <= 0 {
		return errors.New("provider_template_id must be a positive number for SMS templates")
	}
	return nil
}
