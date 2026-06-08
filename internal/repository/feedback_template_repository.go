package repository

import (
	"context"

	"github.com/automax/backend/internal/models"
	"github.com/google/uuid"
	"gorm.io/gorm"
)

type FeedbackTemplateRepository interface {
	Create(ctx context.Context, feedbackTemplate *models.FeedbackTemplate) error
	FindByID(ctx context.Context, id uuid.UUID) (*models.FeedbackTemplate, error)
	FindByWorkflowTransitionID(ctx context.Context, workflowTransitionID uuid.UUID) ([]models.FeedbackTemplate, error)
	FeedbackTemplateExist(ctx context.Context, feedback string, workflowTransitionID uuid.UUID) (bool, error)
	List(ctx context.Context, includeInactive bool) ([]models.FeedbackTemplate, error)
	Update(ctx context.Context, feedbackTemplate *models.FeedbackTemplate) error
	Delete(ctx context.Context, id uuid.UUID) error
}

type feedbackTemplateRepository struct {
	db *gorm.DB
}

func NewFeedbackTemplateRepository(db *gorm.DB) FeedbackTemplateRepository {
	return &feedbackTemplateRepository{db: db}
}

func (r *feedbackTemplateRepository) Create(ctx context.Context, feedbackTemplate *models.FeedbackTemplate) error {
	return r.db.WithContext(ctx).Create(feedbackTemplate).Error
}

func (r *feedbackTemplateRepository) FindByID(ctx context.Context, id uuid.UUID) (*models.FeedbackTemplate, error) {
	var feedbackTemplate models.FeedbackTemplate
	err := r.db.WithContext(ctx).
		Preload("WorkflowTransition").
		First(&feedbackTemplate, "id = ?", id).Error
	if err != nil {
		return nil, err
	}
	return &feedbackTemplate, nil
}

func (r *feedbackTemplateRepository) FindByWorkflowTransitionID(ctx context.Context, workflowTransitionID uuid.UUID) ([]models.FeedbackTemplate, error) {
	var feedbackTemplates []models.FeedbackTemplate
	err := r.db.WithContext(ctx).
		Preload("WorkflowTransition").
		Where("workflow_transition_id = ? AND is_active = ?", workflowTransitionID, true).
		Find(&feedbackTemplates).Error
	return feedbackTemplates, err
}

func (r *feedbackTemplateRepository) FeedbackTemplateExist(ctx context.Context, feedback string, workflowTransitionID uuid.UUID) (bool, error) {
	var count int64
	err := r.db.WithContext(ctx).Model(&models.FeedbackTemplate{}).
		Where("feedback_text = ? AND workflow_transition_id = ?", feedback, workflowTransitionID).
		Count(&count).Error
	if err != nil {
		return false, err
	}
	return count > 0, nil
}

func (r *feedbackTemplateRepository) List(ctx context.Context, includeInactive bool) ([]models.FeedbackTemplate, error) {
	var feedbackTemplates []models.FeedbackTemplate
	query := r.db.WithContext(ctx).
		Preload("WorkflowTransition")

	if !includeInactive {
		query = query.Where("is_active = ?", true)
	}

	err := query.Find(&feedbackTemplates).Error
	return feedbackTemplates, err
}

func (r *feedbackTemplateRepository) Update(ctx context.Context, feedbackTemplate *models.FeedbackTemplate) error {
	return r.db.WithContext(ctx).Save(feedbackTemplate).Error
}

func (r *feedbackTemplateRepository) Delete(ctx context.Context, id uuid.UUID) error {
	return r.db.WithContext(ctx).Delete(&models.FeedbackTemplate{}, "id = ?", id).Error
}
