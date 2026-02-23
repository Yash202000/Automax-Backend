package repository

import (
	"context"

	"github.com/automax/backend/internal/models"
	"gorm.io/gorm"
)

type escalationRepository struct {
	db *gorm.DB
}

func NewEscalationRepository(db *gorm.DB) EscalationRepository {
	return &escalationRepository{db: db}
}

type EscalationRepository interface {
	Create(ctx context.Context, e *models.EscalationSLA) error
	GetAll(ctx context.Context) ([]models.EscalationSLA, error)
}

func (r *escalationRepository) Create(ctx context.Context, e *models.EscalationSLA) error {
	return r.db.WithContext(ctx).Create(e).Error
}

func (r *escalationRepository) GetAll(ctx context.Context) ([]models.EscalationSLA, error) {
	var configs []models.EscalationSLA
	err := r.db.WithContext(ctx).Find(&configs).Error
	return configs, err
}
