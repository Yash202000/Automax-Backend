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

type FeedbackTemplateService interface {
	Create(ctx context.Context, req *models.FeedbackTemplateCreateRequest) (*models.FeedbackTemplateResponse, error)
	Update(ctx context.Context, id uuid.UUID, req *models.FeedbackTemplateUpdateRequest) (*models.FeedbackTemplateResponse, error)
	Get(ctx context.Context, id uuid.UUID) (*models.FeedbackTemplateResponse, error)
	GetByWorkflowTransitionID(ctx context.Context, workflowTransitionID uuid.UUID) ([]models.FeedbackTemplateResponse, error)
	FeedbackTemplateExist(ctx context.Context, feedback string, workflowTransitionID uuid.UUID) (bool, error)
	List(ctx context.Context, includeInactive bool) ([]models.FeedbackTemplateResponse, error)
	Delete(ctx context.Context, id uuid.UUID) error
}

type feedbackTemplateService struct {
	repo repository.FeedbackTemplateRepository
}

func NewFeedbackTemplateService(repo repository.FeedbackTemplateRepository) FeedbackTemplateService {
	return &feedbackTemplateService{repo: repo}
}

func (s *feedbackTemplateService) Create(ctx context.Context, req *models.FeedbackTemplateCreateRequest) (*models.FeedbackTemplateResponse, error) {
	if req.FeedbackText == "" {
		return nil, errors.New("feedback text is required")
	}

	username, ok := ctx.Value(constants.ContextKeys.UserName).(string)
	if !ok {
		log.Warn("UserName not found in context, using user id as fallback")
		return nil, errors.New(i18n.T(ctx, "user_context_missing"))
	}

	feedbackTemplate := &models.FeedbackTemplate{
		FeedbackText:         req.FeedbackText,
		WorkflowTransitionID: req.WorkflowTransitionID,
		UpdatedBy:            username,
		IsActive:             true,
	}

	if err := s.repo.Create(ctx, feedbackTemplate); err != nil {
		return nil, fmt.Errorf("failed to create feedback template: %w", err)
	}

	// Fetch the created feedback template with relations
	created, err := s.repo.FindByID(ctx, feedbackTemplate.ID)
	if err != nil {
		return nil, fmt.Errorf("failed to fetch created feedback template: %w", err)
	}

	response := models.ToFeedbackTemplateResponse(created)
	return &response, nil
}

func (s *feedbackTemplateService) Update(ctx context.Context, id uuid.UUID, req *models.FeedbackTemplateUpdateRequest) (*models.FeedbackTemplateResponse, error) {
	feedbackTemplate, err := s.repo.FindByID(ctx, id)
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, errors.New(i18n.T(ctx, "feedback_template_not_found"))
		}
		return nil, fmt.Errorf("failed to find feedback template: %w", err)
	}

	if req.FeedbackText != nil {
		feedbackTemplate.FeedbackText = *req.FeedbackText
	}
	if req.IsActive != nil {
		feedbackTemplate.IsActive = *req.IsActive
	}

	if err := s.repo.Update(ctx, feedbackTemplate); err != nil {
		return nil, fmt.Errorf("failed to update feedback template: %w", err)
	}

	// Fetch the updated feedback template with relations
	updated, err := s.repo.FindByID(ctx, feedbackTemplate.ID)
	if err != nil {
		return nil, fmt.Errorf("failed to fetch updated feedback template: %w", err)
	}

	response := models.ToFeedbackTemplateResponse(updated)
	return &response, nil
}

func (s *feedbackTemplateService) Get(ctx context.Context, id uuid.UUID) (*models.FeedbackTemplateResponse, error) {
	feedbackTemplate, err := s.repo.FindByID(ctx, id)
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, errors.New(i18n.T(ctx, "feedback_template_not_found"))
		}
		return nil, fmt.Errorf("failed to find feedback	 template: %w", err)
	}

	response := models.ToFeedbackTemplateResponse(feedbackTemplate)
	return &response, nil
}

func (s *feedbackTemplateService) GetByWorkflowTransitionID(ctx context.Context, workflowTransitionID uuid.UUID) ([]models.FeedbackTemplateResponse, error) {
	feedbackTemplates, err := s.repo.FindByWorkflowTransitionID(ctx, workflowTransitionID)
	if err != nil {
		return nil, fmt.Errorf("failed to find feedback templates: %w", err)
	}

	responses := make([]models.FeedbackTemplateResponse, len(feedbackTemplates))
	for i, ct := range feedbackTemplates {
		responses[i] = models.ToFeedbackTemplateResponse(&ct)
	}

	return responses, nil
}

func (s *feedbackTemplateService) FeedbackTemplateExist(ctx context.Context, feedback string, workflowTransitionID uuid.UUID) (bool, error) {
	exist, err := s.repo.FeedbackTemplateExist(ctx, feedback, workflowTransitionID)
	if err != nil {
		return false, fmt.Errorf("failed to check feedback template existence: %w", err)
	}
	return exist, nil
}

func (s *feedbackTemplateService) List(ctx context.Context, includeInactive bool) ([]models.FeedbackTemplateResponse, error) {
	feedbackTemplates, err := s.repo.List(ctx, includeInactive)
	if err != nil {
		return nil, fmt.Errorf("failed to list feedback templates: %w", err)
	}

	responses := make([]models.FeedbackTemplateResponse, len(feedbackTemplates))
	for i, ct := range feedbackTemplates {
		responses[i] = models.ToFeedbackTemplateResponse(&ct)
	}

	return responses, nil
}

func (s *feedbackTemplateService) Delete(ctx context.Context, id uuid.UUID) error {
	// Check if feedback template exists
	_, err := s.repo.FindByID(ctx, id)
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return errors.New(i18n.T(ctx, "feedback_template_not_found"))
		}
		return fmt.Errorf("failed to find feedback template: %w", err)
	}

	if err := s.repo.Delete(ctx, id); err != nil {
		return fmt.Errorf("failed to delete feedback template: %w", err)
	}

	return nil
}
