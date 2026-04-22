package repository

import (
	"context"
	"fmt"

	"github.com/automax/backend/internal/models"
	"github.com/google/uuid"
	"gorm.io/gorm"
)

// CategoryRepository is the data access contract for the hierarchical
// Category system parameter used by Goals (and, potentially, other modules).
type CategoryRepository interface {
	Create(ctx context.Context, category *models.Category) error
	FindByID(ctx context.Context, id uuid.UUID) (*models.Category, error)
	FindByCode(ctx context.Context, code string) (*models.Category, error)
	List(ctx context.Context, includeInactive bool) ([]models.Category, error)
	GetTree(ctx context.Context) ([]models.Category, error)
	GetChildren(ctx context.Context, parentID uuid.UUID) ([]models.Category, error)
	Update(ctx context.Context, category *models.Category) error
	Delete(ctx context.Context, id uuid.UUID) error
	CountGoalsUsingCategory(ctx context.Context, id uuid.UUID) (int64, error)
	CountChildren(ctx context.Context, id uuid.UUID) (int64, error)
}

type categoryRepository struct {
	db *gorm.DB
}

func NewCategoryRepository(db *gorm.DB) CategoryRepository {
	return &categoryRepository{db: db}
}

func (r *categoryRepository) Create(ctx context.Context, category *models.Category) error {
	return r.db.WithContext(ctx).Create(category).Error
}

func (r *categoryRepository) FindByID(ctx context.Context, id uuid.UUID) (*models.Category, error) {
	var category models.Category
	err := r.db.WithContext(ctx).
		Preload("Parent").
		First(&category, "id = ?", id).Error
	if err != nil {
		return nil, err
	}
	return &category, nil
}

func (r *categoryRepository) FindByCode(ctx context.Context, code string) (*models.Category, error) {
	var category models.Category
	err := r.db.WithContext(ctx).
		First(&category, "code = ?", code).Error
	if err != nil {
		return nil, err
	}
	return &category, nil
}

func (r *categoryRepository) List(ctx context.Context, includeInactive bool) ([]models.Category, error) {
	var categories []models.Category
	query := r.db.WithContext(ctx).Order("level, sort_order, name")
	if !includeInactive {
		query = query.Where("is_active = ?", true)
	}
	err := query.Find(&categories).Error
	return categories, err
}

// GetTree loads every category in one query and stitches them into a nested
// tree by ParentID. Two-pass: load flat, then recursively build from an
// index-by-parent-ID map so ordering from SQL (sort_order, name) is preserved.
func (r *categoryRepository) GetTree(ctx context.Context) ([]models.Category, error) {
	var flat []models.Category
	if err := r.db.WithContext(ctx).
		Order("level, sort_order, name").
		Find(&flat).Error; err != nil {
		return nil, err
	}

	// Map parent-id → indexes of children in `flat`. Preserves SQL order.
	childrenByParent := make(map[uuid.UUID][]int)
	rootIndexes := make([]int, 0)
	for i := range flat {
		flat[i].Children = nil
		if flat[i].ParentID == nil {
			rootIndexes = append(rootIndexes, i)
		} else {
			childrenByParent[*flat[i].ParentID] = append(childrenByParent[*flat[i].ParentID], i)
		}
	}

	var build func(i int) models.Category
	build = func(i int) models.Category {
		node := flat[i]
		for _, ci := range childrenByParent[node.ID] {
			node.Children = append(node.Children, build(ci))
		}
		return node
	}

	roots := make([]models.Category, 0, len(rootIndexes))
	for _, ri := range rootIndexes {
		roots = append(roots, build(ri))
	}
	return roots, nil
}

func (r *categoryRepository) GetChildren(ctx context.Context, parentID uuid.UUID) ([]models.Category, error) {
	var children []models.Category
	err := r.db.WithContext(ctx).
		Where("parent_id = ?", parentID).
		Order("sort_order, name").
		Find(&children).Error
	return children, err
}

func (r *categoryRepository) Update(ctx context.Context, category *models.Category) error {
	return r.db.WithContext(ctx).Save(category).Error
}

// Delete soft-deletes the category after ensuring it has no live children
// and that no goals reference it.
func (r *categoryRepository) Delete(ctx context.Context, id uuid.UUID) error {
	childCount, err := r.CountChildren(ctx, id)
	if err != nil {
		return fmt.Errorf("failed to count children: %w", err)
	}
	if childCount > 0 {
		return fmt.Errorf("cannot delete category: %d child categor(y/ies) still exist", childCount)
	}

	goalCount, err := r.CountGoalsUsingCategory(ctx, id)
	if err != nil {
		return fmt.Errorf("failed to count referencing goals: %w", err)
	}
	if goalCount > 0 {
		return fmt.Errorf("cannot delete category: %d goal(s) still reference it", goalCount)
	}

	return r.db.WithContext(ctx).Delete(&models.Category{}, "id = ?", id).Error
}

func (r *categoryRepository) CountGoalsUsingCategory(ctx context.Context, id uuid.UUID) (int64, error) {
	var count int64
	err := r.db.WithContext(ctx).
		Table("goals").
		Where("category_id = ? AND deleted_at IS NULL", id).
		Count(&count).Error
	return count, err
}

func (r *categoryRepository) CountChildren(ctx context.Context, id uuid.UUID) (int64, error) {
	var count int64
	err := r.db.WithContext(ctx).
		Model(&models.Category{}).
		Where("parent_id = ?", id).
		Count(&count).Error
	return count, err
}
