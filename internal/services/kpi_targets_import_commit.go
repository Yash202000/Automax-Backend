package services

import (
	"fmt"
	"sort"

	"github.com/automax/backend/internal/models"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

// kpiTargetEditableStatuses mirrors UpdateTarget's editable-status rule.
var kpiTargetEditableStatuses = []string{"draft", "returned", "rejected"}

// commit writes the validated plan; updates keep the target ID so linked
// entries stay valid.
func (imp *kpiTargetsImporter) commit(tx *gorm.DB) error {
	if err := imp.revalidateCommit(tx); err != nil {
		return err
	}
	for _, r := range imp.recs {
		switch r.action {
		case kpiImportActionCreate:
			t := *r.Target
			t.KpiCode = r.KpiCode
			if t.KpiCode == "" {
				t.KpiCode = r.Target.KpiCode
			}
			t.KpiType = r.KpiType
			if t.KpiType == "" {
				t.KpiType = r.Target.KpiType
			}
			t.Version = 1
			// Legacy columns mirror target_year/period_code, matching
			// KpiAnnualTargetRequest.ToModel.
			t.Year = t.TargetYear
			t.PeriodType = "annual"
			t.PeriodKey = t.PeriodCode
			if t.TargetStatus == "" {
				t.TargetStatus = "draft"
			}
			if t.TargetType == "" {
				t.TargetType = "Period Target"
			}
			if t.TargetBasis == "" {
				t.TargetBasis = "Management Decision"
			}
			if t.ThresholdMode == "" {
				t.ThresholdMode = "Use Global KPI Rules"
			}
			if err := tx.Omit(clause.Associations).Create(&t).Error; err != nil {
				return fmt.Errorf("creating target for KPI %s period %s/%d: %w", t.KpiCode, t.PeriodCode, t.TargetYear, err)
			}
		case kpiImportActionUpdate:
			if r.Existing == nil {
				return fmt.Errorf("target for KPI %s period %s/%d has no existing record to update", r.KpiCode, r.Target.PeriodCode, r.Target.TargetYear)
			}
			// Guard the status in the predicate so a target that becomes
			// non-editable after planning cannot be changed.
			res := tx.Model(&models.KpiAnnualTarget{}).
				Where("id = ? AND target_status IN ?", r.Existing.ID, kpiTargetEditableStatuses).
				Updates(kpiTargetUpdateMap(r.Target, r.Set))
			if res.Error != nil {
				return fmt.Errorf("updating target for KPI %s period %s/%d: %w", r.KpiCode, r.Target.PeriodCode, r.Target.TargetYear, res.Error)
			}
			if res.RowsAffected == 0 {
				return fmt.Errorf("updating target for KPI %s period %s/%d: target is no longer editable (only draft, returned or rejected targets can be updated)", r.KpiCode, r.Target.PeriodCode, r.Target.TargetYear)
			}
		}
	}
	return nil
}

// revalidateCommit serializes concurrent imports on the target identities and
// KPI/metric scopes they write, then re-checks stored targets inside the
// transaction: creates that now exist become skips or updates, and Milestone /
// Ad Hoc ranges that now overlap are rejected instead of inserted.
func (imp *kpiTargetsImporter) revalidateCommit(tx *gorm.DB) error {
	keys := map[string]bool{}
	for _, r := range imp.recs {
		if r.Rejected || (r.action != kpiImportActionCreate && r.action != kpiImportActionUpdate) {
			continue
		}
		keys["identity|"+kpiTargetIdentity(r.KpiType, r.KpiCode, r.Target.TargetYear, r.Target.PeriodCode, r.MetricID, nil)] = true
		if r.Target.TargetType == models.KpiTargetTypeMilestone {
			// Overlap can cross identities, so serialize the whole KPI/metric scope.
			metric := ""
			if r.MetricID != nil {
				metric = r.MetricID.String()
			}
			keys["scope|"+r.KpiID.String()+"|"+metric] = true
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
		if err := tx.Exec("SELECT pg_advisory_xact_lock(hashtextextended(?, 0))", "kpi-target-import|"+key).Error; err != nil {
			return fmt.Errorf("locking target %s for the commit: %w", key, err)
		}
	}

	// Re-read now that the locks are held; concurrent writes become visible.
	if err := imp.loadExisting(tx); err != nil {
		return fmt.Errorf("re-reading targets for the commit: %w", err)
	}
	for _, r := range imp.recs {
		if r.Rejected || (r.action != kpiImportActionCreate && r.action != kpiImportActionUpdate) {
			continue
		}
		identity := kpiTargetIdentity(r.KpiType, r.KpiCode, r.Target.TargetYear, r.Target.PeriodCode, r.MetricID, nil)
		existing := imp.existingTarget[identity]
		if existing == nil && r.action == kpiImportActionUpdate {
			r.reject("Target", "The target no longer exists; it may have been deleted after validation.")
			continue
		}
		if existing != nil {
			r.Existing = existing
			if imp.opts.ExistingMode != KpiImportExistingUpdate {
				// Skip mode leaves the existing target untouched.
				continue
			}
			switch existing.TargetStatus {
			case "draft", "returned", "rejected":
				imp.preserveExisting(r, existing)
			default:
				r.reject("Target Status", fmt.Sprintf("The existing target is %s and cannot be updated through import. Only draft, returned or rejected targets are editable.", existing.TargetStatus))
				continue
			}
		}
		if r.Rejected || r.Target.TargetType != models.KpiTargetTypeMilestone {
			continue
		}
		if other := imp.milestoneOverlap(r, r.Existing, nil); other != nil {
			r.reject("Period Start", fmt.Sprintf("This date range overlaps an existing Milestone / Ad Hoc target (%s, status '%s').",
				FormatPeriodLabel(other.Target.PeriodCode, other.Target.TargetYear), other.Target.TargetStatus))
		}
	}

	// Rebuild actions: creates that now match become skips or updates.
	imp.res.Items = []KpiImportItem{}
	imp.res.Errors = []KpiImportIssue{}
	imp.finalize()
	if imp.opts.ErrorMode == KpiImportErrorsAbort && len(imp.res.Errors) > 0 {
		return fmt.Errorf("the target re-check rejected rows; nothing was committed")
	}
	return nil
}

// kpiTargetUpdateMap includes only fields present in the row; identity,
// approval and runtime fields are untouched.
func kpiTargetUpdateMap(t *models.KpiAnnualTarget, set map[string]bool) map[string]interface{} {
	updates := map[string]interface{}{
		"target_value": t.TargetValue,
		"target_type":  t.TargetType,
		// Legacy mirrors stay in sync with target_year/period_code.
		"year":        t.TargetYear,
		"period_type": "annual",
		"period_key":  t.PeriodCode,
	}
	if set["target_basis"] {
		updates["target_basis"] = t.TargetBasis
	}
	if set["target_rationale"] {
		updates["target_rationale"] = t.TargetRationale
	}
	if set["threshold_mode"] {
		updates["threshold_mode"] = t.ThresholdMode
	}
	if set["excellent_threshold"] {
		updates["excellent_threshold"] = t.ExcellentThreshold
	}
	if set["achieved_threshold"] {
		updates["achieved_threshold"] = t.AchievedThreshold
	}
	if set["warning_threshold"] {
		updates["warning_threshold"] = t.WarningThreshold
	}
	if set["target_range_min"] {
		updates["target_range_min"] = t.TargetRangeMin
	}
	if set["target_range_max"] {
		updates["target_range_max"] = t.TargetRangeMax
	}
	if set["target_status"] {
		updates["target_status"] = t.TargetStatus
	}
	if set["effective_from"] {
		updates["effective_from"] = t.EffectiveFrom
	}
	if set["effective_to"] {
		updates["effective_to"] = t.EffectiveTo
	}
	if t.TargetType == models.KpiTargetTypeMilestone || set["period_start"] {
		updates["period_start"] = t.PeriodStart
	}
	if t.TargetType == models.KpiTargetTypeMilestone || set["period_end"] {
		updates["period_end"] = t.PeriodEnd
	}
	if set["metric_code"] || set["metric_name"] {
		updates["metric_id"] = t.MetricID
		updates["calculation_type_snapshot"] = t.CalculationTypeSnapshot
		updates["direction_snapshot"] = t.DirectionSnapshot
		updates["unit_snapshot"] = t.UnitSnapshot
		updates["decimal_precision_snapshot"] = t.DecimalPrecisionSnapshot
		updates["aggregation_method_snapshot"] = t.AggregationMethodSnapshot
		updates["reporting_frequency_snapshot"] = t.ReportingFrequencySnapshot
	}
	return updates
}
