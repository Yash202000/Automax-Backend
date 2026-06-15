package repository

import (
	"context"
	"fmt"

	"github.com/automax/backend/internal/models"
	"github.com/google/uuid"
	"gorm.io/gorm"
)

// typeExistsWhere returns a GORM WHERE condition and args for filtering classifications
// by the given types slice, using an EXISTS subquery to avoid duplicate rows.
//
//	nil / empty / ["all"] → no condition (all classifications)
//	["both"]              → exists with type IN ('incident','request')
//	["incident","mobile"] → exists with type IN ('incident','mobile')
//	["incident"]          → exists with type = 'incident'
func typeExistsWhere(types []string) (condition string, args []interface{}) {
	if len(types) == 0 {
		return "", nil
	}
	// single magic values
	if len(types) == 1 {
		switch types[0] {
		case "", "all":
			return "", nil
		case "both":
			return "EXISTS (SELECT 1 FROM classification_types WHERE classification_id = classifications.id AND type IN ('incident','request'))", nil
		}
	}
	// collect resolved types, handling magic values in a multi-value list
	resolved := make([]string, 0, len(types))
	for _, t := range types {
		switch t {
		case "", "all":
			// "all" anywhere in the list means no filter
			return "", nil
		case "both":
			resolved = append(resolved, "incident", "request")
		default:
			resolved = append(resolved, t)
		}
	}
	// deduplicate
	seen := make(map[string]struct{}, len(resolved))
	unique := resolved[:0]
	for _, t := range resolved {
		if _, ok := seen[t]; !ok {
			seen[t] = struct{}{}
			unique = append(unique, t)
		}
	}
	if len(unique) == 1 {
		return "EXISTS (SELECT 1 FROM classification_types WHERE classification_id = classifications.id AND type = ?)", []interface{}{unique[0]}
	}
	return "EXISTS (SELECT 1 FROM classification_types WHERE classification_id = classifications.id AND type IN ?)", []interface{}{unique}
}

type ClassificationRepository interface {
	Create(ctx context.Context, classification *models.Classification) error
	FindByID(ctx context.Context, id uuid.UUID) (*models.Classification, error)
	FindByIDs(ctx context.Context, id []uuid.UUID) (*[]models.Classification, error)
	FindByNameAndParent(ctx context.Context, name string, parentID *uuid.UUID) (*models.Classification, error)
	FindByNameOrNameArAndParent(ctx context.Context, name string, nameAr string, parentID *uuid.UUID) (*models.Classification, error)
	FindByNameOrNameAr(ctx context.Context, name string, nameAr string) (*models.Classification, error)
	FindByExternalID(ctx context.Context, externalID string) (*models.Classification, error)
	Update(ctx context.Context, classification *models.Classification) error
	Delete(ctx context.Context, id uuid.UUID) error
	List(ctx context.Context) ([]models.Classification, error)
	ListByType(ctx context.Context, types []string) ([]models.Classification, error)
	GetTree(ctx context.Context) ([]models.Classification, error)
	GetTreeByType(ctx context.Context, types []string) ([]models.Classification, error)
	GetChildren(ctx context.Context, parentID uuid.UUID) ([]models.Classification, error)
	GetByParentID(ctx context.Context, parentID *uuid.UUID) ([]models.Classification, error)
	GetTreeWithStats(ctx context.Context, types []string) ([]models.ClassificationWithStats, error)
	// Type management
	SetTypes(ctx context.Context, classificationID uuid.UUID, types []string) error
	// Classification Criticality methods
	CreateCriticality(ctx context.Context, criticality *models.ClassificationCriticality) error
	UpdateCriticality(ctx context.Context, criticality *models.ClassificationCriticality) error
	DeleteCriticality(ctx context.Context, id uuid.UUID) error
	GetCriticalitiesByClassificationID(ctx context.Context, classificationID uuid.UUID) ([]models.ClassificationCriticality, error)
	GetCriticalityByID(ctx context.Context, id uuid.UUID) (*models.ClassificationCriticality, error)
	GetCriticalityByClassificationAndCriticalityID(ctx context.Context, classificationID, criticalityID uuid.UUID) (*models.ClassificationCriticality, error)
	GetCriticalityByClassificationAndPriorityCode(ctx context.Context, classificationID uuid.UUID, priorityCode string) (*models.ClassificationCriticality, error)
	FetchClassificationFullPaths(ctx context.Context, classificationIDs []uuid.UUID) (map[string]string, error)
	FetchClassificationFullPathByID(ctx context.Context, classificationID uuid.UUID) (string, error)
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
	err := r.db.WithContext(ctx).
		Preload("Types").
		Preload("Children").
		Preload("Criticalities.Criticality").
		First(&classification, "id = ?", id).Error
	if err != nil {
		return nil, err
	}
	return &classification, nil
}
func (r *classificationRepository) FindByIDs(ctx context.Context, ids []uuid.UUID) (*[]models.Classification, error) {
	var classifications []models.Classification
	err := r.db.WithContext(ctx).
		Find(&classifications, "id IN ?", ids).Error // Find not First
	if err != nil {
		return nil, err
	}
	return &classifications, nil
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

func (r *classificationRepository) FindByNameOrNameArAndParent(ctx context.Context, name string, nameAr string, parentID *uuid.UUID) (*models.Classification, error) {
	var classification models.Classification
	query := r.db.WithContext(ctx)
	if nameAr != "" && nameAr != name {
		query = query.Where("name = ? OR name_ar = ?", name, nameAr)
	} else {
		query = query.Where("name = ?", name)
	}
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

func (r *classificationRepository) FindByNameOrNameAr(ctx context.Context, name string, nameAr string) (*models.Classification, error) {
	var classification models.Classification
	query := r.db.WithContext(ctx)
	if nameAr != "" && nameAr != name {
		query = query.Where("name = ? OR name_ar = ?", name, nameAr)
	} else {
		query = query.Where("name = ?", name)
	}
	err := query.First(&classification).Error
	if err != nil {
		return nil, err
	}
	return &classification, nil
}

func (r *classificationRepository) FindByExternalID(ctx context.Context, externalID string) (*models.Classification, error) {
	var classification models.Classification
	err := r.db.WithContext(ctx).First(&classification, "external_id = ?", externalID).Error
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
	err := r.db.WithContext(ctx).
		Preload("Types").
		Preload("Criticalities.Criticality").
		Order("sort_order, name").
		Find(&classifications).Error
	return classifications, err
}

func (r *classificationRepository) ListByType(ctx context.Context, types []string) ([]models.Classification, error) {
	var classifications []models.Classification
	query := r.db.WithContext(ctx).
		Preload("Types").
		Preload("Criticalities.Criticality").
		Order("sort_order, name")

	if cond, args := typeExistsWhere(types); cond != "" {
		query = query.Where(cond, args...)
	}

	err := query.Find(&classifications).Error
	return classifications, err
}

func (r *classificationRepository) GetTree(ctx context.Context) ([]models.Classification, error) {
	var roots []models.Classification
	err := r.db.WithContext(ctx).
		Where("parent_id IS NULL").
		Preload("Types").
		Preload("Criticalities.Criticality").
		Preload("Children.Types").
		Preload("Children.Criticalities.Criticality").
		Preload("Children.Children.Types").
		Preload("Children.Children.Criticalities.Criticality").
		Preload("Children", func(db *gorm.DB) *gorm.DB {
			return db.Order("sort_order, name")
		}).
		Preload("Children.Children", func(db *gorm.DB) *gorm.DB {
			return db.Order("sort_order, name")
		}).
		Preload("Children.Children.Children").
		Preload("Children.Children.Children.Types").
		Order("sort_order, name").
		Find(&roots).Error
	return roots, err
}

func (r *classificationRepository) GetTreeByType(ctx context.Context, types []string) ([]models.Classification, error) {
	cond, args := typeExistsWhere(types)
	// no filter means return full tree
	if cond == "" {
		return r.GetTree(ctx)
	}

	var roots []models.Classification
	typeFilter := func(db *gorm.DB) *gorm.DB {
		return db.Where(cond, args...).Order("sort_order, name")
	}
	err := r.db.WithContext(ctx).
		Where("parent_id IS NULL").
		Where(cond, args...).
		Preload("Types").
		Preload("Criticalities.Criticality").
		Preload("Children.Types").
		Preload("Children.Criticalities.Criticality").
		Preload("Children.Children.Types").
		Preload("Children.Children.Criticalities.Criticality").
		Preload("Children", typeFilter).
		Preload("Children.Children", typeFilter).
		Preload("Children.Children.Children", typeFilter).
		Preload("Children.Children.Children.Types").
		Order("sort_order, name").
		Find(&roots).Error
	return roots, err
}

func (r *classificationRepository) GetChildren(ctx context.Context, parentID uuid.UUID) ([]models.Classification, error) {
	var children []models.Classification
	err := r.db.WithContext(ctx).
		Preload("Types").
		Where("parent_id = ?", parentID).
		Order("sort_order, name").
		Find(&children).Error
	return children, err
}

func (r *classificationRepository) GetByParentID(ctx context.Context, parentID *uuid.UUID) ([]models.Classification, error) {
	var classifications []models.Classification
	query := r.db.WithContext(ctx).Preload("Types")
	if parentID == nil {
		query = query.Where("parent_id IS NULL")
	} else {
		query = query.Where("parent_id = ?", parentID)
	}
	err := query.Order("sort_order, name").Find(&classifications).Error
	return classifications, err
}

func (r *classificationRepository) GetTreeWithStats(ctx context.Context, types []string) ([]models.ClassificationWithStats, error) {
	var classifications []models.Classification
	query := r.db.WithContext(ctx).Preload("Types")

	if cond, args := typeExistsWhere(types); cond != "" {
		query = query.Where(cond, args...)
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

	// resolve types for counting — reuse same logic as classification filter
	resolved := make([]string, 0, len(types))
	for _, t := range types {
		switch t {
		case "", "all":
			resolved = resolved[:0] // no filter
			goto countDone
		case "both":
			resolved = append(resolved, "incident", "request")
		default:
			resolved = append(resolved, t)
		}
	}
	if len(resolved) == 1 {
		countQuery = countQuery.Where("record_type = ?", resolved[0])
	} else if len(resolved) > 1 {
		countQuery = countQuery.Where("record_type IN ?", resolved)
	}
countDone:

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
		typeStrings := make([]string, len(cls.Types))
		for i, t := range cls.Types {
			typeStrings[i] = t.Type
		}
		stats := models.ClassificationWithStats{
			ID:          cls.ID,
			Name:        cls.Name,
			Description: cls.Description,
			Types:       typeStrings,
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

	// Second pass: build tree
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

	for i := range roots {
		roots[i].Count = calculateTotalCount(&roots[i])
	}

	return roots, nil
}

// SetTypes replaces all type entries for a classification atomically
func (r *classificationRepository) SetTypes(ctx context.Context, classificationID uuid.UUID, types []string) error {
	return r.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		// Delete all existing types for this classification
		if err := tx.Where("classification_id = ?", classificationID).Delete(&models.ClassificationType{}).Error; err != nil {
			return err
		}
		// Insert new types
		for _, t := range types {
			ct := &models.ClassificationType{
				ClassificationID: classificationID,
				Type:             t,
			}
			if err := tx.Create(ct).Error; err != nil {
				return err
			}
		}
		return nil
	})
}

// Classification Criticality methods

func (r *classificationRepository) CreateCriticality(ctx context.Context, criticality *models.ClassificationCriticality) error {
	return r.db.WithContext(ctx).Create(criticality).Error
}

func (r *classificationRepository) UpdateCriticality(ctx context.Context, criticality *models.ClassificationCriticality) error {
	return r.db.WithContext(ctx).Save(criticality).Error
}

func (r *classificationRepository) DeleteCriticality(ctx context.Context, id uuid.UUID) error {
	return r.db.WithContext(ctx).Delete(&models.ClassificationCriticality{}, "id = ?", id).Error
}

func (r *classificationRepository) GetCriticalitiesByClassificationID(ctx context.Context, classificationID uuid.UUID) ([]models.ClassificationCriticality, error) {
	var criticalities []models.ClassificationCriticality
	err := r.db.WithContext(ctx).
		Preload("Criticality").
		Where("classification_id = ?", classificationID).
		Find(&criticalities).Error
	return criticalities, err
}

func (r *classificationRepository) GetCriticalityByID(ctx context.Context, id uuid.UUID) (*models.ClassificationCriticality, error) {
	var criticality models.ClassificationCriticality
	err := r.db.WithContext(ctx).
		Preload("Criticality").
		First(&criticality, "id = ?", id).Error
	if err != nil {
		return nil, err
	}
	return &criticality, nil
}

func (r *classificationRepository) GetCriticalityByClassificationAndCriticalityID(ctx context.Context, classificationID, criticalityID uuid.UUID) (*models.ClassificationCriticality, error) {
	var criticality models.ClassificationCriticality
	err := r.db.WithContext(ctx).
		Preload("Criticality").
		Where("classification_id = ? AND criticality_id = ?", classificationID, criticalityID).
		First(&criticality).Error
	if err != nil {
		return nil, err
	}
	return &criticality, nil
}

// GetCriticalityByClassificationAndPriorityCode gets the classification criticality by classification ID and priority code (e.g., "CRITICAL", "HIGH")
func (r *classificationRepository) GetCriticalityByClassificationAndPriorityCode(ctx context.Context, classificationID uuid.UUID, priorityCode string) (*models.ClassificationCriticality, error) {
	var criticality models.ClassificationCriticality
	err := r.db.WithContext(ctx).
		Joins("JOIN lookup_values ON lookup_values.id = classification_criticalities.criticality_id").
		Joins("JOIN lookup_categories ON lookup_categories.id = lookup_values.category_id").
		Where("classification_criticalities.classification_id = ? AND lookup_categories.code = ? AND lookup_values.code = ?", classificationID, "PRIORITY", priorityCode).
		Preload("Criticality").
		First(&criticality).Error
	if err != nil {
		return nil, err
	}
	return &criticality, nil
}

// full path queries

// ── full path queries ───────────────────────────────────────────────────
func (r *classificationRepository) FetchClassificationFullPaths(
	ctx context.Context,
	classificationIDs []uuid.UUID,
) (paths map[string]string, err error) {
	paths = map[string]string{}
	if len(classificationIDs) == 0 {
		return
	}

	query := `
		WITH RECURSIVE classification_hierarchy AS (
			SELECT
				id,
				name,
				parent_id,
				name::TEXT AS full_path
			FROM classifications
			WHERE parent_id IS NULL

			UNION ALL

			SELECT
				l.id,
				l.name,
				l.parent_id,
				lh.full_path || ' > ' || l.name
			FROM classifications l
			INNER JOIN classification_hierarchy lh ON l.parent_id = lh.id
		)
		SELECT
			id::text,
			full_path
		FROM classification_hierarchy
		WHERE id::text IN (?)
	`

	rows, qerr := r.db.WithContext(ctx).Raw(query, classificationIDs).Rows()
	if qerr != nil {
		err = fmt.Errorf("fetchLocationPaths: %w", qerr)
		return
	}
	defer rows.Close()

	for rows.Next() {
		var locationID, fullPath string
		if serr := rows.Scan(&locationID, &fullPath); serr != nil {
			continue
		}
		paths[locationID] = fullPath
	}
	return
}

func (r *classificationRepository) FetchClassificationFullPathByID(
	ctx context.Context,
	classificationID uuid.UUID,
) (string, error) {
	const query = `
		WITH RECURSIVE classification_hierarchy AS (
			SELECT
				id,
				name,
				parent_id,
				name::TEXT AS full_path
			FROM classifications
			WHERE parent_id IS NULL

			UNION ALL

			SELECT
				l.id,
				l.name,
				l.parent_id,
				lh.full_path || ' > ' || l.name
			FROM classifications l
			INNER JOIN classification_hierarchy lh ON l.parent_id = lh.id
		)
		SELECT full_path
		FROM classification_hierarchy
		WHERE id = $1
	`

	var fullPath string
	err := r.db.WithContext(ctx).Raw(query, classificationID).Scan(&fullPath).Error
	if err != nil {
		return "", fmt.Errorf("fetchLocationFullPathByID: %w", err)
	}
	if fullPath == "" {
		return "", fmt.Errorf("fetchLocationFullPathByID: location %s not found", classificationID)
	}

	return fullPath, nil
}
