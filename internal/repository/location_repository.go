package repository

import (
	"context"
	"fmt"

	"github.com/automax/backend/internal/models"
	"github.com/google/uuid"
	"gorm.io/gorm"
)

type LocationRepository interface {
	Create(ctx context.Context, location *models.Location) error
	FindByID(ctx context.Context, id uuid.UUID) (*models.Location, error)
	FindByIDs(ctx context.Context, id []uuid.UUID) (*[]models.Location, error)
	FindByNameAndParent(ctx context.Context, name string, parentID *uuid.UUID) (*models.Location, error)
	FindByNameOrNameArAndParent(ctx context.Context, name string, nameAr string, parentID *uuid.UUID) (*models.Location, error)
	FindByNameOrNameAr(ctx context.Context, name string, nameAr string) (*models.Location, error)
	FindByExternalID(ctx context.Context, externalID string) (*models.Location, error)
	Update(ctx context.Context, location *models.Location) error
	Delete(ctx context.Context, id uuid.UUID) error
	List(ctx context.Context) ([]models.Location, error)
	GetTree(ctx context.Context) ([]models.Location, error)
	GetChildren(ctx context.Context, parentID uuid.UUID) ([]models.Location, error)
	GetByParentID(ctx context.Context, parentID *uuid.UUID) ([]models.Location, error)
	GetByType(ctx context.Context, locationType string) ([]models.Location, error)
	GetTreeWithStats(ctx context.Context, recordType string) ([]models.LocationWithStats, error)
	FetchLocationFullPaths(ctx context.Context, locationIDs []string) (map[string]string, error)
	FetchLocationFullPathByID(ctx context.Context, locationID uuid.UUID) (string, error)
}

type locationRepository struct {
	db *gorm.DB
}

func NewLocationRepository(db *gorm.DB) LocationRepository {
	return &locationRepository{db: db}
}

func (r *locationRepository) Create(ctx context.Context, location *models.Location) error {
	if location.ParentID != nil {
		var parent models.Location
		if err := r.db.WithContext(ctx).First(&parent, "id = ?", location.ParentID).Error; err != nil {
			return fmt.Errorf("parent location not found")
		}
		location.Level = parent.Level + 1
		location.Path = parent.Path + "/" + location.ID.String()
	} else {
		location.Level = 0
		location.Path = location.ID.String()
	}
	return r.db.WithContext(ctx).Create(location).Error
}

func (r *locationRepository) FindByID(ctx context.Context, id uuid.UUID) (*models.Location, error) {
	var location models.Location
	err := r.db.WithContext(ctx).Preload("Children").First(&location, "id = ?", id).Error
	if err != nil {
		return nil, err
	}
	return &location, nil
}
func (r *locationRepository) FindByIDs(ctx context.Context, ids []uuid.UUID) (*[]models.Location, error) {
	var locations []models.Location
	err := r.db.WithContext(ctx).
		Find(&locations, "id IN ?", ids).Error
	if err != nil {
		return nil, err
	}
	return &locations, nil
}
func (r *locationRepository) FindByNameAndParent(ctx context.Context, name string, parentID *uuid.UUID) (*models.Location, error) {
	var location models.Location
	query := r.db.WithContext(ctx).Where("name = ?", name)
	if parentID == nil {
		query = query.Where("parent_id IS NULL")
	} else {
		query = query.Where("parent_id = ?", parentID)
	}
	err := query.First(&location).Error
	if err != nil {
		return nil, err
	}
	return &location, nil
}

func (r *locationRepository) FindByNameOrNameArAndParent(ctx context.Context, name string, nameAr string, parentID *uuid.UUID) (*models.Location, error) {
	var location models.Location
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
	err := query.First(&location).Error
	if err != nil {
		return nil, err
	}
	return &location, nil
}

func (r *locationRepository) FindByNameOrNameAr(ctx context.Context, name string, nameAr string) (*models.Location, error) {
	var location models.Location
	query := r.db.WithContext(ctx)
	if nameAr != "" && nameAr != name {
		query = query.Where("name = ? OR name_ar = ?", name, nameAr)
	} else {
		query = query.Where("name = ?", name)
	}
	err := query.First(&location).Error
	if err != nil {
		return nil, err
	}
	return &location, nil
}

func (r *locationRepository) FindByExternalID(ctx context.Context, externalID string) (*models.Location, error) {
	var location models.Location
	err := r.db.WithContext(ctx).First(&location, "external_id = ?", externalID).Error
	if err != nil {
		return nil, err
	}
	return &location, nil
}

func (r *locationRepository) Update(ctx context.Context, location *models.Location) error {
	return r.db.WithContext(ctx).Save(location).Error
}

func (r *locationRepository) Delete(ctx context.Context, id uuid.UUID) error {
	return r.db.WithContext(ctx).Delete(&models.Location{}, "id = ?", id).Error
}

func (r *locationRepository) List(ctx context.Context) ([]models.Location, error) {
	var locations []models.Location
	err := r.db.WithContext(ctx).Order("sort_order, name").Find(&locations).Error
	return locations, err
}

func (r *locationRepository) GetTree(ctx context.Context) ([]models.Location, error) {
	var roots []models.Location
	err := r.db.WithContext(ctx).
		Where("parent_id IS NULL").
		Preload("Children", func(db *gorm.DB) *gorm.DB {
			return db.Order("sort_order, name")
		}).
		Preload("Children.Children", func(db *gorm.DB) *gorm.DB {
			return db.Order("sort_order, name")
		}).
		Preload("Children.Children.Children").
		Preload("Children.Children.Children.Children").
		Order("sort_order, name").
		Find(&roots).Error
	return roots, err
}

func (r *locationRepository) GetChildren(ctx context.Context, parentID uuid.UUID) ([]models.Location, error) {
	var children []models.Location
	err := r.db.WithContext(ctx).
		Where("parent_id = ?", parentID).
		Order("sort_order, name").
		Find(&children).Error
	return children, err
}

func (r *locationRepository) GetByParentID(ctx context.Context, parentID *uuid.UUID) ([]models.Location, error) {
	var locations []models.Location
	query := r.db.WithContext(ctx)
	if parentID == nil {
		query = query.Where("parent_id IS NULL")
	} else {
		query = query.Where("parent_id = ?", parentID)
	}
	err := query.Order("sort_order, name").Find(&locations).Error
	return locations, err
}

func (r *locationRepository) GetByType(ctx context.Context, locationType string) ([]models.Location, error) {
	var locations []models.Location
	err := r.db.WithContext(ctx).
		Where("type = ?", locationType).
		Order("sort_order, name").
		Find(&locations).Error
	return locations, err
}

func (r *locationRepository) GetTreeWithStats(ctx context.Context, recordType string) ([]models.LocationWithStats, error) {
	// Get all locations
	var locations []models.Location
	if err := r.db.WithContext(ctx).Order("sort_order, name").Find(&locations).Error; err != nil {
		return nil, err
	}

	// Get counts for each location
	type CountResult struct {
		LocationID uuid.UUID
		Count      int64
	}

	var counts []CountResult
	countQuery := r.db.WithContext(ctx).
		Table("incidents").
		Select("location_id, COUNT(*) as count").
		Where("location_id IS NOT NULL AND deleted_at IS NULL")

	if recordType != "" && recordType != "all" {
		countQuery = countQuery.Where("record_type = ?", recordType)
	}

	if err := countQuery.Group("location_id").Scan(&counts).Error; err != nil {
		return nil, err
	}

	// Build count map
	countMap := make(map[uuid.UUID]int64)
	for _, c := range counts {
		countMap[c.LocationID] = c.Count
	}

	// Build tree structure with stats
	statsMap := make(map[uuid.UUID]*models.LocationWithStats)
	var roots []models.LocationWithStats

	// First pass: create all nodes
	for _, loc := range locations {
		stats := models.LocationWithStats{
			ID:          loc.ID,
			Name:        loc.Name,
			Code:        loc.Code,
			Description: loc.Description,
			Type:        loc.Type,
			ParentID:    loc.ParentID,
			Level:       loc.Level,
			Path:        loc.Path,
			Address:     loc.Address,
			Latitude:    loc.Latitude,
			Longitude:   loc.Longitude,
			IsActive:    loc.IsActive,
			SortOrder:   loc.SortOrder,
			Count:       countMap[loc.ID],
			Children:    []models.LocationWithStats{},
			CreatedAt:   loc.CreatedAt,
		}
		statsMap[loc.ID] = &stats
	}

	// Second pass: build tree
	for _, loc := range locations {
		if loc.ParentID == nil {
			roots = append(roots, *statsMap[loc.ID])
		} else if parent, exists := statsMap[*loc.ParentID]; exists {
			parent.Children = append(parent.Children, *statsMap[loc.ID])
		}
	}

	// Third pass: calculate total counts (including children)
	var calculateTotalCount func(*models.LocationWithStats) int64
	calculateTotalCount = func(node *models.LocationWithStats) int64 {
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

// full path queries

// ── full path queries ───────────────────────────────────────────────────
func (r *locationRepository) FetchLocationFullPaths(
	ctx context.Context,
	locationIDs []string,
) (paths map[string]string, err error) {
	paths = map[string]string{}
	if len(locationIDs) == 0 {
		return
	}

	query := `
		WITH RECURSIVE location_hierarchy AS (
			SELECT
				id,
				name,
				parent_id,
				name::TEXT AS full_path
			FROM locations
			WHERE parent_id IS NULL

			UNION ALL

			SELECT
				l.id,
				l.name,
				l.parent_id,
				lh.full_path || ' > ' || l.name
			FROM locations l
			INNER JOIN location_hierarchy lh ON l.parent_id = lh.id
		)
		SELECT
			id::text,
			full_path
		FROM location_hierarchy
		WHERE id::text IN (?)
	`

	rows, qerr := r.db.WithContext(ctx).Raw(query, locationIDs).Rows()
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

// @Todo: Add filter in temp table to only fetch paths for locations that are relevant to the incident list query (e.g. only fetch paths for locations that have incidents matching the filter criteria). This will reduce the number of paths we need to fetch and improve performance.
func (r *locationRepository) FetchLocationFullPathByID(
	ctx context.Context,
	locationID uuid.UUID,
) (string, error) {
	const query = `
		WITH RECURSIVE location_hierarchy AS (
			SELECT
				id,
				name,
				parent_id,
				name::TEXT AS full_path
			FROM locations
			WHERE parent_id IS NULL

			UNION ALL

			SELECT
				l.id,
				l.name,
				l.parent_id,
				lh.full_path || ' > ' || l.name
			FROM locations l
			INNER JOIN location_hierarchy lh ON l.parent_id = lh.id
		)
		SELECT full_path
		FROM location_hierarchy
		WHERE id = $1
	`

	var fullPath string
	err := r.db.WithContext(ctx).Raw(query, locationID).Scan(&fullPath).Error
	if err != nil {
		return "", fmt.Errorf("fetchLocationFullPathByID: %w", err)
	}
	if fullPath == "" {
		return "", fmt.Errorf("fetchLocationFullPathByID: location %s not found", locationID)
	}

	return fullPath, nil
}

