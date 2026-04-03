package services

import (
	"context"
	"fmt"

	"github.com/automax/backend/internal/models"
	"github.com/automax/backend/internal/repository"
	"github.com/google/uuid"
)

type GoalTemplateService interface {
	Create(ctx context.Context, req *models.GoalTemplateCreateRequest, userID uuid.UUID) (*models.GoalTemplateResponse, error)
	GetByID(ctx context.Context, id uuid.UUID) (*models.GoalTemplateResponse, error)
	List(ctx context.Context, filter *models.GoalTemplateFilter) ([]models.GoalTemplateResponse, int64, error)
	ListActive(ctx context.Context) ([]models.GoalTemplateResponse, error)
	Update(ctx context.Context, id uuid.UUID, req *models.GoalTemplateUpdateRequest) (*models.GoalTemplateResponse, error)
	Delete(ctx context.Context, id uuid.UUID) error
}

type goalTemplateService struct {
	repo repository.GoalTemplateRepository
}

func NewGoalTemplateService(repo repository.GoalTemplateRepository) GoalTemplateService {
	return &goalTemplateService{repo: repo}
}

func (s *goalTemplateService) Create(ctx context.Context, req *models.GoalTemplateCreateRequest, userID uuid.UUID) (*models.GoalTemplateResponse, error) {
	template := &models.GoalTemplate{
		Name:                 req.Name,
		Description:          req.Description,
		Category:             req.Category,
		Priority:             req.Priority,
		DefaultMetrics:       req.DefaultMetrics,
		DefaultCollaborators: req.DefaultCollaborators,
		WorkflowID:           req.WorkflowID,
		IsActive:             req.IsActive,
		CreatedByID:          userID,
	}

	if err := s.repo.Create(ctx, template); err != nil {
		return nil, fmt.Errorf("failed to create goal template: %w", err)
	}

	created, err := s.repo.FindByID(ctx, template.ID)
	if err != nil {
		return nil, fmt.Errorf("failed to fetch created template: %w", err)
	}

	resp := created.ToResponse()
	return &resp, nil
}

func (s *goalTemplateService) GetByID(ctx context.Context, id uuid.UUID) (*models.GoalTemplateResponse, error) {
	template, err := s.repo.FindByID(ctx, id)
	if err != nil {
		return nil, fmt.Errorf("goal template not found: %w", err)
	}

	resp := template.ToResponse()
	return &resp, nil
}

func (s *goalTemplateService) List(ctx context.Context, filter *models.GoalTemplateFilter) ([]models.GoalTemplateResponse, int64, error) {
	templates, total, err := s.repo.List(ctx, filter)
	if err != nil {
		return nil, 0, fmt.Errorf("failed to list goal templates: %w", err)
	}

	responses := make([]models.GoalTemplateResponse, len(templates))
	for i, t := range templates {
		responses[i] = t.ToResponse()
	}

	return responses, total, nil
}

func (s *goalTemplateService) ListActive(ctx context.Context) ([]models.GoalTemplateResponse, error) {
	templates, err := s.repo.ListActive(ctx)
	if err != nil {
		return nil, fmt.Errorf("failed to list active templates: %w", err)
	}

	responses := make([]models.GoalTemplateResponse, len(templates))
	for i, t := range templates {
		responses[i] = t.ToResponse()
	}

	return responses, nil
}

func (s *goalTemplateService) Update(ctx context.Context, id uuid.UUID, req *models.GoalTemplateUpdateRequest) (*models.GoalTemplateResponse, error) {
	template, err := s.repo.FindByID(ctx, id)
	if err != nil {
		return nil, fmt.Errorf("goal template not found: %w", err)
	}

	if req.Name != nil {
		template.Name = *req.Name
	}
	if req.Description != nil {
		template.Description = *req.Description
	}
	if req.Category != nil {
		template.Category = *req.Category
	}
	if req.Priority != nil {
		template.Priority = *req.Priority
	}
	if req.DefaultMetrics != nil {
		template.DefaultMetrics = *req.DefaultMetrics
	}
	if req.DefaultCollaborators != nil {
		template.DefaultCollaborators = *req.DefaultCollaborators
	}
	if req.WorkflowID != nil {
		template.WorkflowID = req.WorkflowID
	}
	if req.IsActive != nil {
		template.IsActive = *req.IsActive
	}

	if err := s.repo.Update(ctx, template); err != nil {
		return nil, fmt.Errorf("failed to update goal template: %w", err)
	}

	updated, err := s.repo.FindByID(ctx, id)
	if err != nil {
		return nil, fmt.Errorf("failed to fetch updated template: %w", err)
	}

	resp := updated.ToResponse()
	return &resp, nil
}

func (s *goalTemplateService) Delete(ctx context.Context, id uuid.UUID) error {
	if _, err := s.repo.FindByID(ctx, id); err != nil {
		return fmt.Errorf("goal template not found: %w", err)
	}

	if err := s.repo.Delete(ctx, id); err != nil {
		return fmt.Errorf("failed to delete goal template: %w", err)
	}

	return nil
}
