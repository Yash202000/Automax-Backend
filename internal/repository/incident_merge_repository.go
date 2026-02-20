package repository

import (
	"context"
	"time"

	"github.com/automax/backend/internal/models"
	"github.com/google/uuid"
	"gorm.io/gorm"
)

type IncidentMergeRepository interface {
	// Merge operations - creates NEW master incident and links all incidents
	CreateMergeWithNewMaster(ctx context.Context, incidentIDs []uuid.UUID, masterIncident *models.Incident, userID uuid.UUID, notes string) (*models.Incident, error)
	
	// Link existing incidents to a master (alternative approach)
	LinkIncidentsToMaster(ctx context.Context, masterIncidentID uuid.UUID, incidentIDs []uuid.UUID, userID uuid.UUID, notes string) error
	
	// Unmerge operations
	UnmergeIncident(ctx context.Context, incidentID uuid.UUID, userID uuid.UUID, notes string) error
	UnmergeAllFromMaster(ctx context.Context, masterIncidentID uuid.UUID, userID uuid.UUID) error
	
	// Query operations
	GetMergedIncidents(ctx context.Context, masterIncidentID uuid.UUID) ([]models.Incident, error)
	GetMasterIncident(ctx context.Context, incidentID uuid.UUID) (*models.Incident, error)
	GetMergeRecords(ctx context.Context, masterIncidentID uuid.UUID) ([]models.IncidentMerge, error)
	IsIncidentMerged(ctx context.Context, incidentID uuid.UUID) (bool, error)
	IsIncidentMaster(ctx context.Context, incidentID uuid.UUID) (bool, error)
	HasMergedIncidents(ctx context.Context, incidentID uuid.UUID) (bool, error)

	// Status sync
	SyncStatusToMergedIncidents(ctx context.Context, masterIncidentID uuid.UUID, newStateID uuid.UUID) error
	CloseMergedIncidents(ctx context.Context, masterIncidentID uuid.UUID) error

	// Auto operations
	AutoUnmergeOnClose(ctx context.Context, masterIncidentID uuid.UUID) error
	AutoUnmergeOnReopen(ctx context.Context, incidentID uuid.UUID) error

	// WithTx for transaction support
	WithTx(tx *gorm.DB) IncidentMergeRepository
}

type incidentMergeRepository struct {
	db *gorm.DB
}

func NewIncidentMergeRepository(db *gorm.DB) IncidentMergeRepository {
	return &incidentMergeRepository{db: db}
}

func (r *incidentMergeRepository) WithTx(tx *gorm.DB) IncidentMergeRepository {
	return &incidentMergeRepository{db: tx}
}

// CreateMergeWithNewMaster creates a NEW master incident and links all selected incidents to it
func (r *incidentMergeRepository) CreateMergeWithNewMaster(ctx context.Context, incidentIDs []uuid.UUID, masterIncident *models.Incident, userID uuid.UUID, notes string) (*models.Incident, error) {
	now := time.Now()

	// Create the new master incident
	if err := r.db.WithContext(ctx).Create(masterIncident).Error; err != nil {
		return nil, err
	}

	// Create merge records linking all incidents to the new master
	for _, incidentID := range incidentIDs {
		merge := &models.IncidentMerge{
			MasterIncidentID:  masterIncident.ID,
			RelatedIncidentID: incidentID,
			MergedByUserID:    &userID,
			MergedAt:          &now,
			IsActive:          true,
			Notes:             notes,
		}
		if err := r.db.WithContext(ctx).Create(merge).Error; err != nil {
			return nil, err
		}

		// Update the related incident to mark it as merged
		if err := r.db.WithContext(ctx).Model(&models.Incident{}).
			Where("id = ?", incidentID).
			Updates(map[string]interface{}{
				"master_incident_id": masterIncident.ID,
				"is_merged":          true,
				"merged_at":          &now,
				"merged_by_user_id":  userID,
			}).Error; err != nil {
			return nil, err
		}
	}

	return masterIncident, nil
}

// LinkIncidentsToMaster links existing incidents to a master (when master is one of the selected)
func (r *incidentMergeRepository) LinkIncidentsToMaster(ctx context.Context, masterIncidentID uuid.UUID, incidentIDs []uuid.UUID, userID uuid.UUID, notes string) error {
	now := time.Now()

	for _, incidentID := range incidentIDs {
		// Skip if this is the master
		if incidentID == masterIncidentID {
			continue
		}

		// Check if merge record already exists
		var existing models.IncidentMerge
		err := r.db.WithContext(ctx).
			Where("master_incident_id = ? AND related_incident_id = ?", masterIncidentID, incidentID).
			First(&existing).Error

		if err == gorm.ErrRecordNotFound {
			// Create new merge record
			merge := &models.IncidentMerge{
				MasterIncidentID:  masterIncidentID,
				RelatedIncidentID: incidentID,
				MergedByUserID:    &userID,
				MergedAt:          &now,
				IsActive:          true,
				Notes:             notes,
			}
			if err := r.db.WithContext(ctx).Create(merge).Error; err != nil {
				return err
			}
		} else if err != nil {
			return err
		}

		// Update the related incident
		if err := r.db.WithContext(ctx).Model(&models.Incident{}).
			Where("id = ?", incidentID).
			Updates(map[string]interface{}{
				"master_incident_id": masterIncidentID,
				"is_merged":          true,
				"merged_at":          &now,
				"merged_by_user_id":  userID,
			}).Error; err != nil {
			return err
		}
	}

	return nil
}

// UnmergeIncident detaches an incident from its master
func (r *incidentMergeRepository) UnmergeIncident(ctx context.Context, incidentID uuid.UUID, userID uuid.UUID, notes string) error {
	now := time.Now()

	// Get the merge record
	var merge models.IncidentMerge
	err := r.db.WithContext(ctx).
		Where("related_incident_id = ? AND is_active = TRUE", incidentID).
		First(&merge).Error
	if err == nil {
		// Mark merge record as inactive
		r.db.WithContext(ctx).Model(&merge).Updates(map[string]interface{}{
			"is_active":          false,
			"unmerged_by_user_id": userID,
			"unmerged_at":        now,
			"notes":              notes,
		})
	}

	// Update the incident
	return r.db.WithContext(ctx).Model(&models.Incident{}).
		Where("id = ?", incidentID).
		Updates(map[string]interface{}{
			"master_incident_id": nil,
			"is_merged":          false,
			"merged_at":          nil,
			"merged_by_user_id":  nil,
		}).Error
}

// UnmergeAllFromMaster unmerges all incidents from a master
func (r *incidentMergeRepository) UnmergeAllFromMaster(ctx context.Context, masterIncidentID uuid.UUID, userID uuid.UUID) error {
	now := time.Now()

	// Mark all merge records as inactive
	r.db.WithContext(ctx).Model(&models.IncidentMerge{}).
		Where("master_incident_id = ? AND is_active = TRUE", masterIncidentID).
		Updates(map[string]interface{}{
			"is_active":      false,
			"unmerged_by_id": userID,
			"unmerged_at":    now,
		})

	// Update all merged incidents
	return r.db.WithContext(ctx).Model(&models.Incident{}).
		Where("master_incident_id = ?", masterIncidentID).
		Updates(map[string]interface{}{
			"master_incident_id": nil,
			"is_merged":          false,
			"merged_at":          nil,
			"merged_by_user_id":  nil,
		}).Error
}

// AutoUnmergeOnClose automatically unmerges all incidents when master reaches final closed state
func (r *incidentMergeRepository) AutoUnmergeOnClose(ctx context.Context, masterIncidentID uuid.UUID) error {
	now := time.Now()

	// Mark all merge records as inactive
	r.db.WithContext(ctx).Model(&models.IncidentMerge{}).
		Where("master_incident_id = ? AND is_active = TRUE", masterIncidentID).
		Updates(map[string]interface{}{
			"is_active":   false,
			"unmerged_at": now,
		})

	// Unmerge all child incidents
	err := r.db.WithContext(ctx).Model(&models.Incident{}).
		Where("master_incident_id = ?", masterIncidentID).
		Updates(map[string]interface{}{
			"master_incident_id": nil,
			"is_merged":          false,
			"merged_at":          nil,
			"merged_by_user_id":  nil,
		}).Error
	if err != nil {
		return err
	}

	// Also clear the master's merge status
	return r.db.WithContext(ctx).Model(&models.Incident{}).
		Where("id = ?", masterIncidentID).
		Updates(map[string]interface{}{
			"is_merged": false,
		}).Error
}

// AutoUnmergeOnReopen removes merge relationships when a closed incident is reopened
func (r *incidentMergeRepository) AutoUnmergeOnReopen(ctx context.Context, incidentID uuid.UUID) error {
	// First, get the incident to check if it's a master or child
	var incident models.Incident
	if err := r.db.WithContext(ctx).First(&incident, "id = ?", incidentID).Error; err != nil {
		return err
	}

	// If this incident is a child, unmerge it from its master
	if incident.IsMerged && incident.MasterIncidentID != nil {
		r.db.WithContext(ctx).Model(&models.Incident{}).
			Where("id = ?", incidentID).
			Updates(map[string]interface{}{
				"master_incident_id": nil,
				"is_merged":          false,
				"merged_at":          nil,
				"merged_by_user_id":  nil,
			})

		// Mark merge record as inactive
		r.db.WithContext(ctx).Model(&models.IncidentMerge{}).
			Where("related_incident_id = ? AND is_active = TRUE", incidentID).
			Updates(map[string]interface{}{
				"is_active":   false,
				"unmerged_at": time.Now(),
			})
	}

	// If this incident is a master, unmerge all its children
	var children []models.Incident
	if err := r.db.WithContext(ctx).Where("master_incident_id = ?", incidentID).Find(&children).Error; err != nil {
		return err
	}

	for _, child := range children {
		r.db.WithContext(ctx).Model(&models.Incident{}).
			Where("id = ?", child.ID).
			Updates(map[string]interface{}{
				"master_incident_id": nil,
				"is_merged":          false,
				"merged_at":          nil,
				"merged_by_user_id":  nil,
			})
	}

	// Mark all merge records as inactive
	r.db.WithContext(ctx).Model(&models.IncidentMerge{}).
		Where("master_incident_id = ? AND is_active = TRUE", incidentID).
		Updates(map[string]interface{}{
			"is_active":   false,
			"unmerged_at": time.Now(),
		})

	// Clear master's merge status
	return r.db.WithContext(ctx).Model(&models.Incident{}).
		Where("id = ?", incidentID).
		Updates(map[string]interface{}{
			"is_merged": false,
		}).Error
}

// GetMergedIncidents returns all incidents merged into the master
func (r *incidentMergeRepository) GetMergedIncidents(ctx context.Context, masterIncidentID uuid.UUID) ([]models.Incident, error) {
	var incidents []models.Incident
	err := r.db.WithContext(ctx).
		Preload("CurrentState").
		Preload("Assignee").
		Preload("Reporter").
		Preload("Department").
		Preload("Location").
		Preload("Classification").
		Where("master_incident_id = ?", masterIncidentID).
		Find(&incidents).Error
	return incidents, err
}

// GetMasterIncident returns the master incident for a given incident
func (r *incidentMergeRepository) GetMasterIncident(ctx context.Context, incidentID uuid.UUID) (*models.Incident, error) {
	var incident models.Incident
	err := r.db.WithContext(ctx).
		Preload("MasterIncident").
		Preload("MasterIncident.CurrentState").
		First(&incident, "id = ?", incidentID).Error
	if err != nil {
		return nil, err
	}
	return incident.MasterIncident, nil
}

// GetMergeRecords returns all merge records for a master incident
func (r *incidentMergeRepository) GetMergeRecords(ctx context.Context, masterIncidentID uuid.UUID) ([]models.IncidentMerge, error) {
	var merges []models.IncidentMerge
	err := r.db.WithContext(ctx).
		Preload("RelatedIncident").
		Preload("MergedBy").
		Where("master_incident_id = ? AND is_active = TRUE", masterIncidentID).
		Find(&merges).Error
	return merges, err
}

// IsIncidentMaster checks if an incident is a master (has merged incidents)
func (r *incidentMergeRepository) IsIncidentMaster(ctx context.Context, incidentID uuid.UUID) (bool, error) {
	var count int64
	err := r.db.WithContext(ctx).Model(&models.IncidentMerge{}).
		Where("master_incident_id = ? AND is_active = TRUE", incidentID).
		Count(&count).Error
	return count > 0, err
}

// IsIncidentMerged checks if an incident is merged into another
func (r *incidentMergeRepository) IsIncidentMerged(ctx context.Context, incidentID uuid.UUID) (bool, error) {
	var incident models.Incident
	err := r.db.WithContext(ctx).Select("is_merged, master_incident_id").First(&incident, "id = ?", incidentID).Error
	if err != nil {
		return false, err
	}
	return incident.IsMerged && incident.MasterIncidentID != nil, nil
}

// HasMergedIncidents checks if an incident has other incidents merged into it
func (r *incidentMergeRepository) HasMergedIncidents(ctx context.Context, incidentID uuid.UUID) (bool, error) {
	var count int64
	err := r.db.WithContext(ctx).Model(&models.Incident{}).
		Where("master_incident_id = ? AND is_merged = ?", incidentID, true).
		Count(&count).Error
	return count > 0, err
}

// SyncStatusToMergedIncidents updates the status of all merged incidents to match the master
func (r *incidentMergeRepository) SyncStatusToMergedIncidents(ctx context.Context, masterIncidentID uuid.UUID, newStateID uuid.UUID) error {
	return r.db.WithContext(ctx).Model(&models.Incident{}).
		Where("master_incident_id = ?", masterIncidentID).
		Updates(map[string]interface{}{
			"current_state_id": newStateID,
		}).Error
}

// CloseMergedIncidents closes all merged incidents when the master is closed
func (r *incidentMergeRepository) CloseMergedIncidents(ctx context.Context, masterIncidentID uuid.UUID) error {
	// First get the master incident's current state and timestamps
	var master models.Incident
	if err := r.db.WithContext(ctx).Select("current_state_id, closed_at, resolved_at").
		First(&master, "id = ?", masterIncidentID).Error; err != nil {
		return err
	}

	now := time.Now()
	updates := map[string]interface{}{
		"current_state_id": master.CurrentStateID,
		"closed_at":        now,
	}
	if master.ResolvedAt != nil {
		updates["resolved_at"] = master.ResolvedAt
	}

	return r.db.WithContext(ctx).Model(&models.Incident{}).
		Where("master_incident_id = ?", masterIncidentID).
		Updates(updates).Error
}
