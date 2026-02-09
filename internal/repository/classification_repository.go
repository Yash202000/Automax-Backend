package repository

import (
	"context"
	"fmt"

	"github.com/automax/backend/internal/models"
	"github.com/google/uuid"
	"gorm.io/gorm"
)

type ClassificationRepository interface {
	Create(ctx context.Context, classification *models.Classification) error
	FindByID(ctx context.Context, id uuid.UUID) (*models.Classification, error)
	FindByNameAndParent(ctx context.Context, name string, parentID *uuid.UUID) (*models.Classification, error)
	Update(ctx context.Context, classification *models.Classification) error
	Delete(ctx context.Context, id uuid.UUID) error
	List(ctx context.Context) ([]models.Classification, error)
	ListByType(ctx context.Context, classType string) ([]models.Classification, error)
	GetTree(ctx context.Context) ([]models.Classification, error)
	GetTreeByType(ctx context.Context, classType string) ([]models.Classification, error)
	GetChildren(ctx context.Context, parentID uuid.UUID) ([]models.Classification, error)
	GetByParentID(ctx context.Context, parentID *uuid.UUID) ([]models.Classification, error)
	GetTreeWithStats(ctx context.Context, recordType string) ([]models.ClassificationWithStats, error)
}

type classificationRepository struct {
	db *gorm.DB
}

func NewClassificationRepository(db *gorm.DB) ClassificationRepository {
	return &classificationRepository{db: db}
}

func (r *classificationRepository) Create(ctx context.Context, classification *models.Classification) error {
	if classification.ParentID != nil {
		var parent models.Classification
		if err := r.db.WithContext(ctx).First(&parent, "id = ?", classification.ParentID).Error; err != nil {
			return fmt.Errorf("parent classification not found")
		}
		classification.Level = parent.Level + 1
		classification.Path = parent.Path + "/" + classification.ID.String()
	} else {
		classification.Level = 0
		classification.Path = classification.ID.String()
	}
	return r.db.WithContext(ctx).Create(classification).Error
}

func (r *classificationRepository) FindByID(ctx context.Context, id uuid.UUID) (*models.Classification, error) {
	var classification models.Classification
	err := r.db.WithContext(ctx).Preload("Children").First(&classification, "id = ?", id).Error
	if err != nil {
		return nil, err
	}
	return &classification, nil
}

func (r *classificationRepository) FindByNameAndParent(ctx context.Context, name string, parentID *uuid.UUID) (*models.Classification, error) {
	var classification models.Classification
	query := r.db.WithContext(ctx).Where("name = ?", name)
	if parentID == nil {
		query = query.Where("parent_id IS NULL")
	} else {
		query = query.Where("parent_id = ?", parentID)
	}
	err := query.First(&classification).Error
	if err != nil {
		return nil, err
	}
	return &classification, nil
}

func (r *classificationRepository) Update(ctx context.Context, classification *models.Classification) error {
	return r.db.WithContext(ctx).Save(classification).Error
}

func (r *classificationRepository) Delete(ctx context.Context, id uuid.UUID) error {
	return r.db.WithContext(ctx).Delete(&models.Classification{}, "id = ?", id).Error
}

func (r *classificationRepository) List(ctx context.Context) ([]models.Classification, error) {
	var classifications []models.Classification
	err := r.db.WithContext(ctx).Order("sort_order, name").Find(&classifications).Error
	return classifications, err
}

func (r *classificationRepository) ListByType(ctx context.Context, classType string) ([]models.Classification, error) {
	var classifications []models.Classification
	query := r.db.WithContext(ctx)
	if classType != "" {
		// Support 'all' type which matches any type, 'both' matches incident/request
		query = query.Where("type = ? OR type = 'both' OR type = 'all'", classType)
	}
	err := query.Order("sort_order, name").Find(&classifications).Error
	return classifications, err
}

func (r *classificationRepository) GetTree(ctx context.Context) ([]models.Classification, error) {
	var roots []models.Classification
	err := r.db.WithContext(ctx).
		Where("parent_id IS NULL").
		Preload("Children", func(db *gorm.DB) *gorm.DB {
			return db.Order("sort_order, name")
		}).
		Preload("Children.Children", func(db *gorm.DB) *gorm.DB {
			return db.Order("sort_order, name")
		}).
		Preload("Children.Children.Children").
		Order("sort_order, name").
		Find(&roots).Error
	return roots, err
}

func (r *classificationRepository) GetTreeByType(ctx context.Context, classType string) ([]models.Classification, error) {
	var roots []models.Classification
	typeFilter := func(db *gorm.DB) *gorm.DB {
		return db.Where("type = ? OR type = 'both' OR type = 'all'", classType).Order("sort_order, name")
	}
	err := r.db.WithContext(ctx).
		Where("parent_id IS NULL").
		Where("type = ? OR type = 'both' OR type = 'all'", classType).
		Preload("Children", typeFilter).
		Preload("Children.Children", typeFilter).
		Preload("Children.Children.Children", typeFilter).
		Order("sort_order, name").
		Find(&roots).Error
	return roots, err
}

func (r *classificationRepository) GetChildren(ctx context.Context, parentID uuid.UUID) ([]models.Classification, error) {
	var children []models.Classification
	err := r.db.WithContext(ctx).
		Where("parent_id = ?", parentID).
		Order("sort_order, name").
		Find(&children).Error
	return children, err
}

func (r *classificationRepository) GetByParentID(ctx context.Context, parentID *uuid.UUID) ([]models.Classification, error) {
	var classifications []models.Classification
	query := r.db.WithContext(ctx)
	if parentID == nil {
		query = query.Where("parent_id IS NULL")
	} else {
		query = query.Where("parent_id = ?", parentID)
	}
	err := query.Order("sort_order, name").Find(&classifications).Error
	return classifications, err
}

func (r *classificationRepository) GetTreeWithStats(ctx context.Context, recordType string) ([]models.ClassificationWithStats, error) {
	// Get all classifications
	var classifications []models.Classification
	query := r.db.WithContext(ctx)

	if recordType != "" && recordType != "all" {
		query = query.Where("type = ? OR type = 'both' OR type = 'all'", recordType)
	}

	if err := query.Order("sort_order, name").Find(&classifications).Error; err != nil {
		return nil, err
	}

	// Get counts for each classification
	type CountResult struct {
		ClassificationID uuid.UUID
		Count            int64
	}

	var counts []CountResult
	countQuery := r.db.WithContext(ctx).
		Table("incidents").
		Select("classification_id, COUNT(*) as count").
		Where("classification_id IS NOT NULL AND deleted_at IS NULL")

	if recordType != "" && recordType != "all" {
		countQuery = countQuery.Where("record_type = ?", recordType)
	}

	if err := countQuery.Group("classification_id").Scan(&counts).Error; err != nil {
		return nil, err
	}

	// Build count map
	countMap := make(map[uuid.UUID]int64)
	for _, c := range counts {
		countMap[c.ClassificationID] = c.Count
	}

	// Build tree structure with stats
	statsMap := make(map[uuid.UUID]*models.ClassificationWithStats)
	var roots []models.ClassificationWithStats

	// First pass: create all nodes
	for _, cls := range classifications {
		stats := models.ClassificationWithStats{
			ID:          cls.ID,
			Name:        cls.Name,
			Description: cls.Description,
			Type:        cls.Type,
			ParentID:    cls.ParentID,
			Level:       cls.Level,
			Path:        cls.Path,
			IsActive:    cls.IsActive,
			SortOrder:   cls.SortOrder,
			Count:       countMap[cls.ID],
			Children:    []models.ClassificationWithStats{},
			CreatedAt:   cls.CreatedAt,
		}
		statsMap[cls.ID] = &stats
	}

	// Second pass: build tree and propagate counts upward
	for _, cls := range classifications {
		if cls.ParentID == nil {
			roots = append(roots, *statsMap[cls.ID])
		} else if parent, exists := statsMap[*cls.ParentID]; exists {
			parent.Children = append(parent.Children, *statsMap[cls.ID])
		}
	}

	// Third pass: calculate total counts (including children)
	var calculateTotalCount func(*models.ClassificationWithStats) int64
	calculateTotalCount = func(node *models.ClassificationWithStats) int64 {
		total := node.Count
		for i := range node.Children {
			total += calculateTotalCount(&node.Children[i])
		}
		return total
	}

	// Update roots with total counts
	for i := range roots {
		roots[i].Count = calculateTotalCount(&roots[i])
	}

	return roots, nil
}
