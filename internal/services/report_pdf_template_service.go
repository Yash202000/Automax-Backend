package services

import (
	"context"
	"errors"
	"log"
	"time"

	"github.com/automax/backend/internal/models"
	"github.com/automax/backend/internal/repository"
	"github.com/google/uuid"
)

type ReportPdfTemplateService interface {
	CreatePdfTemplate(ctx context.Context, req *models.ReportPdfTemplateCreateRequest, userID uuid.UUID) (*models.ReportPdfTemplateResponse, error)
	GetPdfTemplate(ctx context.Context, id uuid.UUID) (*models.ReportPdfTemplateResponse, error)
	ListPdfTemplates(ctx context.Context, filter *models.ReportPdfTemplateFilter) ([]models.ReportPdfTemplateResponse, int64, error)
	UpdatePdfTemplate(ctx context.Context, id uuid.UUID, req *models.ReportPdfTemplateUpdateRequest, userID uuid.UUID) (*models.ReportPdfTemplateResponse, error)
	DeletePdfTemplate(ctx context.Context, id uuid.UUID, userID uuid.UUID) error
	DuplicatePdfTemplate(ctx context.Context, id uuid.UUID, userID uuid.UUID) (*models.ReportPdfTemplateResponse, error)
	SetDefaultPdfTemplate(ctx context.Context, id uuid.UUID) error
	GetDefaultPdfTemplate(ctx context.Context) (*models.ReportPdfTemplateResponse, error)
}

type reportPdfTemplateService struct {
	repo repository.ReportPdfTemplateRepository
}

func NewReportPdfTemplateService(repo repository.ReportPdfTemplateRepository) ReportPdfTemplateService {
	return &reportPdfTemplateService{repo: repo}
}

func (s *reportPdfTemplateService) CreatePdfTemplate(ctx context.Context, req *models.ReportPdfTemplateCreateRequest, userID uuid.UUID) (*models.ReportPdfTemplateResponse, error) {
	template := &models.ReportPdfTemplate{
		Name:        req.Name,
		Description: req.Description,
		HTMLBody:    req.HTMLBody,
		IsPublic:    req.IsPublic,
		CreatedByID: userID,
	}
	if err := s.repo.Create(ctx, template); err != nil {
		log.Printf("[ReportPdfTemplateService] create failed: %v", err)
		return nil, errors.New("failed to create pdf template")
	}
	return s.GetPdfTemplate(ctx, template.ID)
}

func (s *reportPdfTemplateService) GetPdfTemplate(ctx context.Context, id uuid.UUID) (*models.ReportPdfTemplateResponse, error) {
	template, err := s.repo.FindByIDWithRelations(ctx, id)
	if err != nil {
		return nil, err
	}
	return toPdfTemplateResponse(template), nil
}

func (s *reportPdfTemplateService) ListPdfTemplates(ctx context.Context, filter *models.ReportPdfTemplateFilter) ([]models.ReportPdfTemplateResponse, int64, error) {
	templates, total, err := s.repo.List(ctx, filter)
	if err != nil {
		return nil, 0, err
	}
	responses := make([]models.ReportPdfTemplateResponse, len(templates))
	for i, t := range templates {
		responses[i] = *toPdfTemplateResponse(&t)
	}
	return responses, total, nil
}

func (s *reportPdfTemplateService) UpdatePdfTemplate(ctx context.Context, id uuid.UUID, req *models.ReportPdfTemplateUpdateRequest, userID uuid.UUID) (*models.ReportPdfTemplateResponse, error) {
	template, err := s.repo.FindByID(ctx, id)
	if err != nil {
		return nil, errors.New("pdf template not found")
	}
	if template.CreatedByID != userID {
		return nil, errors.New("you can only update your own pdf templates")
	}
	if req.Name != "" {
		template.Name = req.Name
	}
	if req.Description != "" {
		template.Description = req.Description
	}
	if req.HTMLBody != nil {
		template.HTMLBody = *req.HTMLBody
	}
	if req.IsPublic != nil {
		template.IsPublic = *req.IsPublic
	}
	if req.IsDefault != nil {
		template.IsDefault = *req.IsDefault
	}
	if err := s.repo.Update(ctx, template); err != nil {
		log.Printf("[ReportPdfTemplateService] update failed for %s: %v", id, err)
		return nil, errors.New("failed to update pdf template")
	}
	return s.GetPdfTemplate(ctx, id)
}

func (s *reportPdfTemplateService) DeletePdfTemplate(ctx context.Context, id uuid.UUID, userID uuid.UUID) error {
	template, err := s.repo.FindByID(ctx, id)
	if err != nil {
		return errors.New("pdf template not found")
	}
	if template.CreatedByID != userID {
		return errors.New("you can only delete your own pdf templates")
	}
	if err := s.repo.Delete(ctx, id); err != nil {
		log.Printf("[ReportPdfTemplateService] delete failed for %s: %v", id, err)
		return errors.New("failed to delete pdf template")
	}
	return nil
}

func (s *reportPdfTemplateService) DuplicatePdfTemplate(ctx context.Context, id uuid.UUID, userID uuid.UUID) (*models.ReportPdfTemplateResponse, error) {
	original, err := s.repo.FindByID(ctx, id)
	if err != nil {
		return nil, errors.New("pdf template not found")
	}
	duplicate := &models.ReportPdfTemplate{
		Name:        original.Name + " (Copy)",
		Description: original.Description,
		HTMLBody:    original.HTMLBody,
		IsPublic:    original.IsPublic,
		IsDefault:   false,
		CreatedByID: userID,
	}
	if err := s.repo.Create(ctx, duplicate); err != nil {
		log.Printf("[ReportPdfTemplateService] duplicate failed for %s: %v", id, err)
		return nil, errors.New("failed to duplicate pdf template")
	}
	return s.GetPdfTemplate(ctx, duplicate.ID)
}

func (s *reportPdfTemplateService) SetDefaultPdfTemplate(ctx context.Context, id uuid.UUID) error {
	if _, err := s.repo.FindByID(ctx, id); err != nil {
		return errors.New("pdf template not found")
	}
	if err := s.repo.SetDefault(ctx, id); err != nil {
		log.Printf("[ReportPdfTemplateService] set default failed for %s: %v", id, err)
		return errors.New("failed to set default pdf template")
	}
	return nil
}

func (s *reportPdfTemplateService) GetDefaultPdfTemplate(ctx context.Context) (*models.ReportPdfTemplateResponse, error) {
	template, err := s.repo.GetDefault(ctx)
	if err != nil {
		return nil, err
	}
	return toPdfTemplateResponse(template), nil
}

func toPdfTemplateResponse(t *models.ReportPdfTemplate) *models.ReportPdfTemplateResponse {
	resp := &models.ReportPdfTemplateResponse{
		ID:          t.ID.String(),
		Name:        t.Name,
		Description: t.Description,
		HTMLBody:    t.HTMLBody,
		IsDefault:   t.IsDefault,
		IsPublic:    t.IsPublic,
		CreatedAt:   t.CreatedAt.Format(time.RFC3339),
		UpdatedAt:   t.UpdatedAt.Format(time.RFC3339),
	}
	if t.CreatedBy != nil {
		resp.CreatedBy = &models.UserBasicResponse{
			ID:        t.CreatedBy.ID.String(),
			Email:     t.CreatedBy.Email,
			Username:  t.CreatedBy.Username,
			FirstName: t.CreatedBy.FirstName,
			LastName:  t.CreatedBy.LastName,
			Avatar:    t.CreatedBy.Avatar,
		}
	}
	return resp
}
