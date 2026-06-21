package services

import (
	"context"
	"errors"
	"fmt"

	"github.com/automax/backend/internal/models"
	"github.com/automax/backend/internal/repository"
	"github.com/automax/backend/pkg/constants"
	"github.com/automax/backend/pkg/i18n"
	"github.com/gofiber/fiber/v2/log"
	"github.com/google/uuid"
	"gorm.io/gorm"
)

type CommentTemplateService interface {
	Create(ctx context.Context, req *models.CommentTemplateCreateRequest) (*models.CommentTemplateResponse, error)
	Update(ctx context.Context, id uuid.UUID, req *models.CommentTemplateUpdateRequest) (*models.CommentTemplateResponse, error)
	Get(ctx context.Context, id uuid.UUID) (*models.CommentTemplateResponse, error)
	GetByWorkflowTransitionID(ctx context.Context, workflowTransitionID uuid.UUID) ([]models.CommentTemplateResponse, error)
	CommentTemplateExist(ctx context.Context, comment string, workflowTransitionID uuid.UUID) (bool, error)
	List(ctx context.Context, includeInactive bool) ([]models.CommentTemplateResponse, error)
	Delete(ctx context.Context, id uuid.UUID) error
}

type commentTemplateService struct {
	repo repository.CommentTemplateRepository
}

func NewCommentTemplateService(repo repository.CommentTemplateRepository) CommentTemplateService {
	return &commentTemplateService{repo: repo}
}

func (s *commentTemplateService) Create(ctx context.Context, req *models.CommentTemplateCreateRequest) (*models.CommentTemplateResponse, error) {
	if req.CommentText == "" {
		return nil, errors.New(i18n.T(ctx, "comment_text_required"))
	}

	username, ok := ctx.Value(constants.ContextKeys.UserName).(string)
	if !ok {
		log.Warn("UserName not found in context, using user id as fallback")
		return nil, errors.New(i18n.T(ctx, "user_context_missing"))
	}

	commentTemplate := &models.CommentTemplate{
		CommentText:          req.CommentText,
		WorkflowTransitionID: req.WorkflowTransitionID,
		UpdatedBy:            username,
		IsActive:             true,
	}

	if err := s.repo.Create(ctx, commentTemplate); err != nil {
		return nil, fmt.Errorf("failed to create comment template: %w", err)
	}

	// Fetch the created comment template with relations
	created, err := s.repo.FindByID(ctx, commentTemplate.ID)
	if err != nil {
		return nil, fmt.Errorf("failed to fetch created comment template: %w", err)
	}

	response := models.ToCommentTemplateResponse(created)
	return &response, nil
}

func (s *commentTemplateService) Update(ctx context.Context, id uuid.UUID, req *models.CommentTemplateUpdateRequest) (*models.CommentTemplateResponse, error) {
	commentTemplate, err := s.repo.FindByID(ctx, id)
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, errors.New(i18n.T(ctx, "comment_template_not_found"))
		}
		return nil, fmt.Errorf("failed to find comment template: %w", err)
	}

	if req.CommentText != nil {
		commentTemplate.CommentText = *req.CommentText
	}
	if req.IsActive != nil {
		commentTemplate.IsActive = *req.IsActive
	}

	if err := s.repo.Update(ctx, commentTemplate); err != nil {
		return nil, fmt.Errorf("failed to update comment template: %w", err)
	}

	// Fetch the updated comment template with relations
	updated, err := s.repo.FindByID(ctx, commentTemplate.ID)
	if err != nil {
		return nil, fmt.Errorf("failed to fetch updated comment template: %w", err)
	}

	response := models.ToCommentTemplateResponse(updated)
	return &response, nil
}

func (s *commentTemplateService) Get(ctx context.Context, id uuid.UUID) (*models.CommentTemplateResponse, error) {
	commentTemplate, err := s.repo.FindByID(ctx, id)
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, errors.New(i18n.T(ctx, "comment_template_not_found"))
		}
		return nil, fmt.Errorf("failed to find comment template: %w", err)
	}

	response := models.ToCommentTemplateResponse(commentTemplate)
	return &response, nil
}

func (s *commentTemplateService) GetByWorkflowTransitionID(ctx context.Context, workflowTransitionID uuid.UUID) ([]models.CommentTemplateResponse, error) {
	commentTemplates, err := s.repo.FindByWorkflowTransitionID(ctx, workflowTransitionID)
	if err != nil {
		return nil, fmt.Errorf("failed to find comment templates: %w", err)
	}

	responses := make([]models.CommentTemplateResponse, len(commentTemplates))
	for i, ct := range commentTemplates {
		responses[i] = models.ToCommentTemplateResponse(&ct)
	}

	return responses, nil
}

func (s *commentTemplateService) CommentTemplateExist(ctx context.Context, comment string, workflowTransitionID uuid.UUID) (bool, error) {
	return s.repo.CommentTemplateExist(ctx, comment, workflowTransitionID)
}

func (s *commentTemplateService) List(ctx context.Context, includeInactive bool) ([]models.CommentTemplateResponse, error) {
	commentTemplates, err := s.repo.List(ctx, includeInactive)
	if err != nil {
		return nil, fmt.Errorf("failed to list comment templates: %w", err)
	}

	responses := make([]models.CommentTemplateResponse, len(commentTemplates))
	for i, ct := range commentTemplates {
		responses[i] = models.ToCommentTemplateResponse(&ct)
	}

	return responses, nil
}

func (s *commentTemplateService) Delete(ctx context.Context, id uuid.UUID) error {
	// Check if comment template exists
	_, err := s.repo.FindByID(ctx, id)
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return errors.New(i18n.T(ctx, "comment_template_not_found"))
		}
		return fmt.Errorf("failed to find comment template: %w", err)
	}

	if err := s.repo.Delete(ctx, id); err != nil {
		return fmt.Errorf("failed to delete comment template: %w", err)
	}

	return nil
}
