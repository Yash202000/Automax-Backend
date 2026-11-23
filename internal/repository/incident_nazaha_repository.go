package repository

import (
	"context"

	"github.com/automax/backend/internal/models"
	"github.com/automax/backend/pkg/utils"
	"github.com/google/uuid"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

// Queries used only by the Nazaha create-incident screen.

// ListByCallerNumber returns the caller's earlier incidents, newest first, for the
// "Previous Reports" (1.3) dropdown. The number matches in any saved format.
func (r *incidentRepository) ListByCallerNumber(ctx context.Context, callerNumber string) ([]models.CallerPreviousReport, error) {
	var reports []models.CallerPreviousReport
	err := r.db.WithContext(ctx).
		Table("incidents AS i").
		Select(`i.id, i.incident_number, i.created_at,
			COALESCE(cl.name, '') AS classification_name, COALESCE(cl.name_ar, '') AS classification_name_ar,
			COALESCE(ws.name, '') AS status_name, COALESCE(ws.name_ar, '') AS status_name_ar`).
		Joins("LEFT JOIN classifications cl ON cl.id = i.classification_id").
		Joins("LEFT JOIN workflow_states ws ON ws.id = i.current_state_id").
		// Match every way the number may have been saved (+966…, 966…, 05…).
		Where("i.caller_number IN ? AND i.deleted_at IS NULL", utils.MobileMatchVariants(callerNumber)).
		Order("i.created_at DESC").
		Scan(&reports).Error
	return reports, err
}

// CreatePreviousReportLinks links an incident to the previous reports selected on
// the create screen. Links that already exist are skipped.
func (r *incidentRepository) CreatePreviousReportLinks(ctx context.Context, incidentID uuid.UUID, previousIDs []uuid.UUID) error {
	if len(previousIDs) == 0 {
		return nil
	}
	links := make([]models.IncidentPreviousReport, 0, len(previousIDs))
	for _, id := range previousIDs {
		links = append(links, models.IncidentPreviousReport{IncidentID: incidentID, PreviousIncidentID: id})
	}
	return r.db.WithContext(ctx).Clauses(clause.OnConflict{DoNothing: true}).Create(&links).Error
}

// LoadNazahaRelations loads the concerned authority, the linked incident and the
// reporter's nationality onto an incident that was already loaded.
func (r *incidentRepository) LoadNazahaRelations(ctx context.Context, incident *models.Incident) error {
	db := r.db.WithContext(ctx)

	if incident.ConcernedAuthorityID != nil {
		authority, err := findLookupValue(db, *incident.ConcernedAuthorityID)
		if err != nil {
			return err
		}
		incident.ConcernedAuthority = authority
	}

	if incident.Reporter != nil && incident.Reporter.NationalityID != nil {
		nationality, err := findLookupValue(db, *incident.Reporter.NationalityID)
		if err != nil {
			return err
		}
		incident.Reporter.Nationality = nationality
	}

	if incident.LinkedIncidentID != nil {
		var linked models.Incident
		if err := db.Select("id", "incident_number").First(&linked, "id = ?", *incident.LinkedIncidentID).Error; err == nil {
			incident.LinkedIncident = &linked
		} else if err != gorm.ErrRecordNotFound {
			return err
		}
	}
	return nil
}

// ListPreviousReports returns the previous reports linked to an incident.
func (r *incidentRepository) ListPreviousReports(ctx context.Context, incidentID uuid.UUID) ([]models.IncidentRef, error) {
	var refs []models.IncidentRef
	err := r.db.WithContext(ctx).
		Table("incident_previous_reports AS p").
		Select("i.id, i.incident_number").
		Joins("JOIN incidents i ON i.id = p.previous_incident_id AND i.deleted_at IS NULL").
		Where("p.incident_id = ?", incidentID).
		Order("i.created_at DESC").
		Scan(&refs).Error
	return refs, err
}

// ReplacePreviousReportLinks sets the previous reports linked to an incident to
// exactly previousIDs (used when the incident is edited).
func (r *incidentRepository) ReplacePreviousReportLinks(ctx context.Context, incidentID uuid.UUID, previousIDs []uuid.UUID) error {
	return r.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		if err := tx.Where("incident_id = ?", incidentID).Delete(&models.IncidentPreviousReport{}).Error; err != nil {
			return err
		}
		if len(previousIDs) == 0 {
			return nil
		}
		links := make([]models.IncidentPreviousReport, 0, len(previousIDs))
		for _, id := range previousIDs {
			links = append(links, models.IncidentPreviousReport{IncidentID: incidentID, PreviousIncidentID: id})
		}
		return tx.Create(&links).Error
	})
}

// findLookupValue loads a master-data value; nil (no error) when it no longer exists.
// Soft-deleted values are still loaded so saved records keep showing their name.
func findLookupValue(db *gorm.DB, id uuid.UUID) (*models.LookupValue, error) {
	var value models.LookupValue
	err := db.Unscoped().First(&value, "id = ?", id).Error
	if err == gorm.ErrRecordNotFound {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return &value, nil
}
