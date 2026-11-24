package repository

import (
	"context"
	"strings"

	"github.com/automax/backend/internal/models"
	"github.com/automax/backend/pkg/utils"
	"github.com/google/uuid"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

// Queries used only by Nazaha.

// ListByCallerNumber returns a caller's earlier incidents, newest first.
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

// CreatePreviousReportLinks links an incident to its previous reports.
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

// LoadNazahaRelations loads the authority, linked incident and reporter nationality.
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

// ReplacePreviousReportLinks replaces the previous reports linked to an incident.
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

// findLookupValue loads a lookup value, including deleted ones; nil when missing.
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

// maxNameSearchWords is the most search words matched on the name.
const maxNameSearchWords = 5

// reporterFullName is the contact's full name.
const reporterFullName = "concat_ws(' ', u.first_name, u.middle_name, u.last_name)"

// reporterNameCondition matches incidents whose reporter name has every search
// word. Uncooperative reporters never match.
func reporterNameCondition(search string) clause.Expr {
	words := strings.Fields(search)
	if len(words) == 0 {
		return clause.Expr{SQL: "FALSE"}
	}
	if len(words) > maxNameSearchWords {
		words = words[:maxNameSearchWords]
	}
	args := []interface{}{models.CooperationStatusUncooperative}
	likes := make([]string, len(words))
	for i, w := range words {
		likes[i] = reporterFullName + " ILIKE ?"
		args = append(args, "%"+escapeLike(w)+"%")
	}
	cond := "incidents.cooperation_status <> ? AND incidents.reporter_id IS NOT NULL" +
		" AND (incidents.created_by_id IS NULL OR incidents.created_by_id <> incidents.reporter_id)" +
		" AND EXISTS (SELECT 1 FROM users u WHERE u.id = incidents.reporter_id AND " + strings.Join(likes, " AND ") + ")"
	return clause.Expr{SQL: cond, Vars: args}
}

// MatchReporterName returns the incidents whose reporter name matches the search.
func (r *incidentRepository) MatchReporterName(ctx context.Context, ids []uuid.UUID, search string) (map[uuid.UUID]bool, error) {
	if len(ids) == 0 {
		return nil, nil
	}
	var matched []uuid.UUID
	if err := r.db.WithContext(ctx).Model(&models.Incident{}).
		Where("incidents.id IN ?", ids).
		Where(reporterNameCondition(search)).
		Pluck("incidents.id", &matched).Error; err != nil {
		return nil, err
	}
	result := make(map[uuid.UUID]bool, len(matched))
	for _, id := range matched {
		result[id] = true
	}
	return result, nil
}
