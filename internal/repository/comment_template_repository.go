package repository

import (
	"context"

	"github.com/automax/backend/internal/models"
	"github.com/google/uuid"
	"gorm.io/gorm"
)

type CommentTemplateRepository interface {
	Create(ctx context.Context, commentTemplate *models.CommentTemplate) error
	FindByID(ctx context.Context, id uuid.UUID) (*models.CommentTemplate, error)
	FindByWorkflowTransitionID(ctx context.Context, workflowTransitionID uuid.UUID) ([]models.CommentTemplate, error)
	List(ctx context.Context, includeInactive bool) ([]models.CommentTemplate, error)
	Update(ctx context.Context, commentTemplate *models.CommentTemplate) error
	Delete(ctx context.Context, id uuid.UUID) error
}

type commentTemplateRepository struct {
	db *gorm.DB
}

func NewCommentTemplateRepository(db *gorm.DB) CommentTemplateRepository {
	return &commentTemplateRepository{db: db}
}

func (r *commentTemplateRepository) Create(ctx context.Context, commentTemplate *models.CommentTemplate) error {
	return r.db.WithContext(ctx).Create(commentTemplate).Error
}

func (r *commentTemplateRepository) FindByID(ctx context.Context, id uuid.UUID) (*models.CommentTemplate, error) {
	var commentTemplate models.CommentTemplate
	err := r.db.WithContext(ctx).
		Preload("WorkflowTransition").
		First(&commentTemplate, "id = ?", id).Error
	if err != nil {
		return nil, err
	}
	return &commentTemplate, nil
}

func (r *commentTemplateRepository) FindByWorkflowTransitionID(ctx context.Context, workflowTransitionID uuid.UUID) ([]models.CommentTemplate, error) {
	var commentTemplates []models.CommentTemplate
	err := r.db.WithContext(ctx).
		Preload("WorkflowTransition").
		Where("workflow_transition_id = ? AND is_active = ?", workflowTransitionID, true).
		Find(&commentTemplates).Error
	return commentTemplates, err
}

func (r *commentTemplateRepository) List(ctx context.Context, includeInactive bool) ([]models.CommentTemplate, error) {
	var commentTemplates []models.CommentTemplate
	query := r.db.WithContext(ctx).
		Preload("WorkflowTransition")

	if !includeInactive {
		query = query.Where("is_active = ?", true)
	}

	err := query.Find(&commentTemplates).Error
	return commentTemplates, err
}

func (r *commentTemplateRepository) Update(ctx context.Context, commentTemplate *models.CommentTemplate) error {
	return r.db.WithContext(ctx).Save(commentTemplate).Error
}

func (r *commentTemplateRepository) Delete(ctx context.Context, id uuid.UUID) error {
	return r.db.WithContext(ctx).Delete(&models.CommentTemplate{}, "id = ?", id).Error
}
