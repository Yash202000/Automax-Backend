package services

import (
	"context"
	"errors"

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

	exists, err := s.repo.ExistsByCodeAndChannel(ctx, req.Code, req.Channel, nil)
	if err != nil {
		return nil, err
	}
	if exists {
		return nil, errors.New("a template with this code and channel already exists")
	}

	tpl := &models.NotificationTemplate{
		Name:         req.Name,
		Code:         req.Code,
		Channel:      req.Channel,
		ModuleType:   req.ModuleType,
		ActionType:   req.ActionType,
		SubjectEN:    req.SubjectEN,
		BodyEN:       req.BodyEN,
		SubjectAR:    req.SubjectAR,
		BodyAR:       req.BodyAR,
		Variables:    req.Variables,
		TransitionID: req.TransitionID,
		IsActive:     req.IsActive,
	}

	if err := s.repo.Create(ctx, tpl); err != nil {
		return nil, err
	}
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

	if req.Name != nil {
		tpl.Name = *req.Name
	}
	if req.SubjectEN != nil {
		tpl.SubjectEN = *req.SubjectEN
	}
	if req.BodyEN != nil {
		if *req.BodyEN == "" {
			return nil, errors.New("body_en cannot be empty")
		}
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
	if req.TransitionID != nil {
		tpl.TransitionID = req.TransitionID
	}
	if req.IsActive != nil {
		tpl.IsActive = *req.IsActive
	}

	if err := s.repo.Update(ctx, tpl); err != nil {
		return nil, err
	}
	return tpl, nil
}

func (s *notificationTemplateService) ToggleActive(ctx context.Context, id uuid.UUID) (*models.NotificationTemplate, error) {
	tpl, err := s.repo.FindByID(ctx, id)
	if err != nil {
		return nil, err
	}
	tpl.IsActive = !tpl.IsActive
	if err := s.repo.Update(ctx, tpl); err != nil {
		return nil, err
	}
	return tpl, nil
}

func (s *notificationTemplateService) Delete(ctx context.Context, id uuid.UUID) error {
	return s.repo.Delete(ctx, id)
}

func (s *notificationTemplateService) GetByCode(ctx context.Context, code, channel string) ([]models.NotificationTemplate, error) {
	return s.repo.FindAllByCode(ctx, code, channel)
}

func (s *notificationTemplateService) GetByTransitionID(ctx context.Context, transitionID uuid.UUID) ([]models.NotificationTemplate, error) {
	return s.repo.FindByTransitionID(ctx, transitionID)
}
