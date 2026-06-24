package repository

import (
	"context"

	"github.com/automax/backend/internal/models"
	"github.com/google/uuid"
	"gorm.io/gorm"
)

type ApplicationLinkRepository interface {
	Create(ctx context.Context, link *models.ApplicationLink) error
	FindByID(ctx context.Context, id uuid.UUID) (*models.ApplicationLink, error)
	List(ctx context.Context) ([]models.ApplicationLink, error)
	ListActive(ctx context.Context) ([]models.ApplicationLink, error)
	ListActiveWithChildren(ctx context.Context) ([]models.ApplicationLink, error)
	Update(ctx context.Context, link *models.ApplicationLink) error
	Delete(ctx context.Context, id uuid.UUID) error
	OrphanChildren(ctx context.Context, parentID uuid.UUID) error
}

type applicationLinkRepository struct {
	db *gorm.DB
}

func NewApplicationLinkRepository(db *gorm.DB) ApplicationLinkRepository {
	return &applicationLinkRepository{db: db}
}

func (r *applicationLinkRepository) Create(ctx context.Context, link *models.ApplicationLink) error {
	return r.db.WithContext(ctx).Create(link).Error
}

func (r *applicationLinkRepository) FindByID(ctx context.Context, id uuid.UUID) (*models.ApplicationLink, error) {
	var link models.ApplicationLink
	err := r.db.WithContext(ctx).First(&link, "id = ?", id).Error
	if err != nil {
		return nil, err
	}
	return &link, nil
}

func (r *applicationLinkRepository) List(ctx context.Context) ([]models.ApplicationLink, error) {
	var links []models.ApplicationLink
	err := r.db.WithContext(ctx).Order("sort_order ASC, name ASC").Find(&links).Error
	return links, err
}

func (r *applicationLinkRepository) ListActive(ctx context.Context) ([]models.ApplicationLink, error) {
	var links []models.ApplicationLink
	err := r.db.WithContext(ctx).
		Where("is_active = ?", true).
		Order("sort_order ASC, name ASC").
		Find(&links).Error
	return links, err
}

// ListActiveWithChildren returns root-level active links with their active children preloaded.
func (r *applicationLinkRepository) ListActiveWithChildren(ctx context.Context) ([]models.ApplicationLink, error) {
	var links []models.ApplicationLink
	err := r.db.WithContext(ctx).
		Where("is_active = ? AND parent_id IS NULL", true).
		Preload("Children", func(db *gorm.DB) *gorm.DB {
			return db.Where("is_active = ?", true).Order("sort_order ASC, name ASC")
		}).
		Order("sort_order ASC, name ASC").
		Find(&links).Error
	return links, err
}

func (r *applicationLinkRepository) Update(ctx context.Context, link *models.ApplicationLink) error {
	return r.db.WithContext(ctx).Save(link).Error
}

func (r *applicationLinkRepository) Delete(ctx context.Context, id uuid.UUID) error {
	return r.db.WithContext(ctx).Delete(&models.ApplicationLink{}, "id = ?", id).Error
}

// OrphanChildren sets parent_id = NULL for all children of the given parent,
// making them independent root cards before the parent is deleted.
func (r *applicationLinkRepository) OrphanChildren(ctx context.Context, parentID uuid.UUID) error {
	return r.db.WithContext(ctx).
		Model(&models.ApplicationLink{}).
		Where("parent_id = ?", parentID).
		Update("parent_id", nil).Error
}
