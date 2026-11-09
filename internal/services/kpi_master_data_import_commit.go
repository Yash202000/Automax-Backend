package services

// Commit plan for master-data imports: creates insert, updates modify in place
// (IDs stay valid), blank optional cells keep existing values, and record codes
// are never written.

import (
	"fmt"
	"sort"

	"github.com/automax/backend/internal/models"
	"github.com/google/uuid"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

func (imp *masterDataImporter) commit(tx *gorm.DB) error {
	if err := imp.revalidateCommit(tx); err != nil {
		return err
	}
	for _, r := range imp.recs {
		if r.Rejected {
			continue
		}
		switch r.action {
		case kpiImportActionCreate:
			if err := imp.createRec(tx, r); err != nil {
				return err
			}
		case kpiImportActionUpdate:
			if r.Existing == nil {
				return fmt.Errorf("%s %q has no existing record to update", imp.entityType, r.recordID())
			}
			if err := imp.updateRec(tx, r); err != nil {
				return err
			}
		}
	}
	return nil
}

// revalidateCommit serializes concurrent imports on the identities they write,
// then re-checks stored records inside the transaction: creates that now exist
// are skipped or updated, and updates whose identity was taken are rejected
// instead of overwriting or duplicating another record.
func (imp *masterDataImporter) revalidateCommit(tx *gorm.DB) error {
	keys := map[string]bool{}
	for _, r := range imp.recs {
		if r.Rejected || (r.action != kpiImportActionCreate && r.action != kpiImportActionUpdate) {
			continue
		}
		for _, key := range imp.identityKeys(r) {
			keys[key] = true
		}
	}
	if len(keys) == 0 {
		return nil
	}

	// Acquire advisory locks in a stable order so overlapping imports cannot deadlock.
	ordered := make([]string, 0, len(keys))
	for key := range keys {
		ordered = append(ordered, key)
	}
	sort.Strings(ordered)
	for _, key := range ordered {
		if err := tx.Exec("SELECT pg_advisory_xact_lock(hashtextextended(?, 0))", "kpi-master-import|"+key).Error; err != nil {
			return fmt.Errorf("locking %s identity %q: %w", imp.entityType, key, err)
		}
	}

	// Re-read now that the locks are held; concurrent writes become visible.
	if err := imp.loadExisting(tx); err != nil {
		return fmt.Errorf("re-reading %s records for the commit: %w", imp.entityType, err)
	}

	claimed := map[uuid.UUID]int{}
	for _, r := range imp.recs {
		if r.Rejected || r.action != kpiImportActionUpdate || r.Existing == nil {
			continue
		}
		claimed[r.Existing.ID] = r.Row
	}
	changed := false
	for _, r := range imp.recs {
		if r.Rejected || (r.action != kpiImportActionCreate && r.action != kpiImportActionUpdate) {
			continue
		}
		found, conflict := imp.lookupExisting(r)
		if conflict {
			r.reject("Name (English)", "Name and Code identify different existing records. Correct one of them.")
			changed = true
			continue
		}
		if r.action == kpiImportActionUpdate {
			if r.Existing == nil || (found != nil && found.ID != r.Existing.ID) {
				r.reject("Name (English)", "Another record now uses this name or code. Reload and retry.")
				changed = true
			}
			continue
		}
		if found == nil {
			continue
		}
		if row, dup := claimed[found.ID]; dup {
			r.reject("Name (English)", fmt.Sprintf("Duplicate record in this file — row %d already matches the same existing record.", row))
		} else {
			r.Existing = found
			claimed[found.ID] = r.Row
		}
		changed = true
	}
	if !changed {
		return nil
	}

	// Rebuild actions: creates that now match become skips or updates.
	imp.res.Items = []KpiImportItem{}
	imp.res.Errors = []KpiImportIssue{}
	imp.finalize()
	if imp.opts.ErrorMode == KpiImportErrorsAbort && len(imp.res.Errors) > 0 {
		return fmt.Errorf("the master-data re-check rejected rows; nothing was committed")
	}
	return nil
}

func (imp *masterDataImporter) createRec(tx *gorm.DB, r *masterImportRec) error {
	v := &r.Payload
	create := tx.Omit(clause.Associations)
	wrap := func(label string, err error) error {
		if err != nil {
			return fmt.Errorf("creating %s %q: %w", label, r.recordID(), err)
		}
		return nil
	}

	switch r.EntityType {
	case KpiMasterPillar:
		item := &models.Pillar{NameEn: v.NameEn, NameAr: v.NameAr, OwnerID: v.OwnerID, IsActive: v.IsActive}
		if err := wrap("pillar", create.Create(item).Error); err != nil {
			return err
		}
		return imp.forceInactive(tx, &models.Pillar{}, item.ID, v.IsActive)
	case KpiMasterEnabler:
		item := &models.Enabler{NameEn: v.NameEn, NameAr: v.NameAr, OwnerID: v.OwnerID, IsActive: v.IsActive}
		if err := wrap("enabler", create.Create(item).Error); err != nil {
			return err
		}
		return imp.forceInactive(tx, &models.Enabler{}, item.ID, v.IsActive)
	case KpiMasterOperationalObjective:
		item := &models.OperationalObjective{
			NameEn: v.NameEn, NameAr: v.NameAr, GoalID: v.GoalID,
			PillarID: v.PillarID, EnablerID: v.EnablerID, IsActive: v.IsActive,
		}
		if err := wrap("operational objective", create.Create(item).Error); err != nil {
			return err
		}
		return imp.forceInactive(tx, &models.OperationalObjective{}, item.ID, v.IsActive)
	case KpiMasterProcess:
		item := &models.Process{
			NameEn: v.NameEn, NameAr: v.NameAr,
			OperationalObjectiveID: *v.OperationalObjectiveID,
			GoalID:                 v.GoalID,
			PillarID:               v.PillarID,
			EnablerID:              v.EnablerID,
			DepartmentID:           v.DepartmentID,
			Unit:                   r.Values["unit"],
			IsActive:               v.IsActive,
		}
		if err := wrap("process", create.Create(item).Error); err != nil {
			return err
		}
		return imp.forceInactive(tx, &models.Process{}, item.ID, v.IsActive)
	case KpiMasterInitiative:
		status := v.Status
		if status == "" {
			status = models.InitiativeStatusDraft
		}
		item := &models.Initiative{
			NameEn: v.NameEn, NameAr: v.NameAr, GoalID: v.GoalID,
			ObjectiveID: v.ObjectiveID, PillarID: v.PillarID,
			EnablerID: v.EnablerID, OwnerID: v.OwnerID, Status: status,
		}
		return wrap("initiative", create.Create(item).Error)
	case KpiMasterDomain:
		// Code is intentionally left empty so Postgres assigns it.
		item := &models.Domain{NameEn: v.NameEn, NameAr: v.NameAr, Type: v.Type, IsActive: v.IsActive}
		if err := wrap("domain", create.Create(item).Error); err != nil {
			return err
		}
		return imp.forceInactive(tx, &models.Domain{}, item.ID, v.IsActive)
	case KpiMasterAwardCriterion:
		item := &models.AwardCriterion{
			CriterionNo: v.CriterionNo, NameEn: v.NameEn, NameAr: v.NameAr, IsActive: v.IsActive,
		}
		if err := wrap("award criterion", create.Create(item).Error); err != nil {
			return err
		}
		return imp.forceInactive(tx, &models.AwardCriterion{}, item.ID, v.IsActive)
	case KpiMasterAwardSubCriterion:
		item := &models.AwardSubCriterion{
			AwardCriterionID: *v.AwardCriterionID, SubNo: v.SubNo,
			NameEn: v.NameEn, NameAr: v.NameAr, IsActive: v.IsActive,
		}
		if err := wrap("award sub-criterion", create.Create(item).Error); err != nil {
			return err
		}
		return imp.forceInactive(tx, &models.AwardSubCriterion{}, item.ID, v.IsActive)
	case KpiMasterAwardKpiMeasure:
		item := &models.AwardKpiMeasure{NameEn: v.NameEn, NameAr: v.NameAr, Version: 1}
		return wrap("award KPI measure", create.Create(item).Error)
	case KpiMasterDataSource:
		item := &models.KpiDataSource{NameEn: v.NameEn, NameAr: v.NameAr, IsActive: v.IsActive}
		if err := wrap("data source", create.Create(item).Error); err != nil {
			return err
		}
		return imp.forceInactive(tx, &models.KpiDataSource{}, item.ID, v.IsActive)
	case KpiMasterSegmentationDimension:
		item := &models.KpiSegmentationDimension{NameEn: v.NameEn, NameAr: v.NameAr, IsActive: v.IsActive}
		if err := wrap("segmentation dimension", create.Create(item).Error); err != nil {
			return err
		}
		return imp.forceInactive(tx, &models.KpiSegmentationDimension{}, item.ID, v.IsActive)
	case KpiMasterOrganization:
		item := &models.KpiOrganization{
			NameEn: v.NameEn, NameAr: v.NameAr, ContactInfo: v.ContactInfo, IsActive: v.IsActive,
		}
		if err := wrap("organization", create.Create(item).Error); err != nil {
			return err
		}
		return imp.forceInactive(tx, &models.KpiOrganization{}, item.ID, v.IsActive)
	}
	return fmt.Errorf("unsupported master-data entity type %q", r.EntityType)
}

// forceInactive writes false explicitly because GORM treats a false zero value
// as "use the default".
func (imp *masterDataImporter) forceInactive(tx *gorm.DB, model interface{}, id uuid.UUID, active bool) error {
	if active {
		return nil
	}
	if err := tx.Model(model).Where("id = ?", id).Update("is_active", false).Error; err != nil {
		return fmt.Errorf("setting is_active=false: %w", err)
	}
	return nil
}

func (imp *masterDataImporter) updateRec(tx *gorm.DB, r *masterImportRec) error {
	v := &r.Payload
	id := r.Existing.ID
	updates := map[string]interface{}{"name_en": v.NameEn}
	set := r.Set
	if set["name_ar"] {
		updates["name_ar"] = v.NameAr
	}
	if set["is_active"] {
		updates["is_active"] = v.IsActive
	}

	switch r.EntityType {
	case KpiMasterPillar:
		if set["owner"] {
			updates["owner_id"] = v.OwnerID
		}
		return imp.applyUpdates(tx, &models.Pillar{}, id, updates, r)
	case KpiMasterEnabler:
		if set["owner"] {
			updates["owner_id"] = v.OwnerID
		}
		return imp.applyUpdates(tx, &models.Enabler{}, id, updates, r)
	case KpiMasterOperationalObjective:
		// The goal is mandatory, so it is always present on a valid row.
		updates["goal_id"] = v.GoalID
		if set["pillar"] {
			updates["pillar_id"] = v.PillarID
		}
		if set["enabler"] {
			updates["enabler_id"] = v.EnablerID
		}
		return imp.applyUpdates(tx, &models.OperationalObjective{}, id, updates, r)
	case KpiMasterProcess:
		updates["operational_objective_id"] = v.OperationalObjectiveID
		updates["goal_id"] = v.GoalID
		if set["pillar"] {
			updates["pillar_id"] = v.PillarID
		}
		if set["enabler"] {
			updates["enabler_id"] = v.EnablerID
		}
		if set["department"] {
			updates["department_id"] = v.DepartmentID
		}
		if set["unit"] {
			updates["unit"] = r.Values["unit"]
		}
		return imp.applyUpdates(tx, &models.Process{}, id, updates, r)
	case KpiMasterInitiative:
		updates["goal_id"] = v.GoalID
		if set["objective"] {
			updates["objective_id"] = v.ObjectiveID
		}
		if set["pillar"] {
			updates["pillar_id"] = v.PillarID
		}
		if set["enabler"] {
			updates["enabler_id"] = v.EnablerID
		}
		if set["owner"] {
			updates["owner_id"] = v.OwnerID
		}
		if set["status"] {
			updates["status"] = v.Status
		}
		return imp.applyUpdates(tx, &models.Initiative{}, id, updates, r)
	case KpiMasterDomain:
		updates["type"] = v.Type
		return imp.applyUpdates(tx, &models.Domain{}, id, updates, r)
	case KpiMasterAwardCriterion:
		updates["criterion_no"] = v.CriterionNo
		return imp.applyUpdates(tx, &models.AwardCriterion{}, id, updates, r)
	case KpiMasterAwardSubCriterion:
		updates["award_criterion_id"] = v.AwardCriterionID
		updates["sub_no"] = v.SubNo
		return imp.applyUpdates(tx, &models.AwardSubCriterion{}, id, updates, r)
	case KpiMasterAwardKpiMeasure:
		// Optimistic-lock mirror: every import update bumps the version.
		updates["version"] = gorm.Expr("version + 1")
		return imp.applyVersionedUpdates(tx, &models.AwardKpiMeasure{}, id, r.Existing.Version, updates, r)
	case KpiMasterDataSource:
		return imp.applyUpdates(tx, &models.KpiDataSource{}, id, updates, r)
	case KpiMasterSegmentationDimension:
		return imp.applyUpdates(tx, &models.KpiSegmentationDimension{}, id, updates, r)
	case KpiMasterOrganization:
		if set["contact_info"] {
			updates["contact_info"] = v.ContactInfo
		}
		return imp.applyUpdates(tx, &models.KpiOrganization{}, id, updates, r)
	}
	return fmt.Errorf("unsupported master-data entity type %q", r.EntityType)
}

func (imp *masterDataImporter) applyUpdates(tx *gorm.DB, model interface{}, id uuid.UUID, updates map[string]interface{}, r *masterImportRec) error {
	res := tx.Model(model).Where("id = ?", id).Updates(updates)
	if res.Error != nil {
		return fmt.Errorf("updating %s %q: %w", imp.entityType, r.recordID(), res.Error)
	}
	if res.RowsAffected == 0 {
		return fmt.Errorf("updating %s %q: record no longer exists", imp.entityType, r.recordID())
	}
	return nil
}

// applyVersionedUpdates applies the update only when the stored version still
// matches the plan, so a concurrent change is reported instead of overwritten.
func (imp *masterDataImporter) applyVersionedUpdates(tx *gorm.DB, model interface{}, id uuid.UUID, version int, updates map[string]interface{}, r *masterImportRec) error {
	res := tx.Model(model).Where("id = ? AND version = ?", id, version).Updates(updates)
	if res.Error != nil {
		return fmt.Errorf("updating %s %q: %w", imp.entityType, r.recordID(), res.Error)
	}
	if res.RowsAffected == 0 {
		return fmt.Errorf("updating %s %q: version conflict (expected version %d); reload and retry", imp.entityType, r.recordID(), version)
	}
	return nil
}
