package services

import (
	"fmt"
	"math"
	"sort"
	"strings"

	"github.com/automax/backend/internal/models"
	"github.com/google/uuid"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

// revalidateCommit re-checks the 100% active-weight cap, approval locks and
// planned creates inside the commit transaction: it locks the affected KPI
// rows so concurrent imports serialize, refreshes the state those checks read,
// and re-runs them.
func (imp *kpiMetricsImporter) revalidateCommit(tx *gorm.DB) error {
	byType := map[string][]uuid.UUID{}
	for _, r := range imp.recs {
		if r.action == kpiImportActionCreate || r.action == kpiImportActionUpdate {
			byType[r.KpiType] = append(byType[r.KpiType], r.KpiID)
		}
	}
	// Lock types and rows in a stable order so concurrent imports serialize.
	types := make([]string, 0, len(byType))
	for typ := range byType {
		types = append(types, typ)
	}
	sort.Strings(types)
	for _, typ := range types {
		ids := byType[typ]
		sort.Slice(ids, func(i, j int) bool { return ids[i].String() < ids[j].String() })
		var rows []struct {
			ID               uuid.UUID
			ActivationStatus string
		}
		if err := tx.Model(kpiModelFor(typ)).Select("id, activation_status").
			Where("id IN ?", ids).
			Order("id ASC").
			Clauses(clause.Locking{Strength: "UPDATE"}).
			Scan(&rows).Error; err != nil {
			return fmt.Errorf("locking KPI rows for the commit re-check: %w", err)
		}
		for _, row := range rows {
			status := strings.ToLower(strings.TrimSpace(row.ActivationStatus))
			imp.kpiLocked[typ+"|"+row.ID.String()] = status == "approved" || status == "active" || status == "closed"
		}
	}

	// Refresh the state applyWeightLimit reads, now that the locks are held.
	var metrics []models.KpiMetric
	if err := tx.Order("display_order ASC, created_at ASC").Find(&metrics).Error; err != nil {
		return fmt.Errorf("reading metrics for the commit re-check: %w", err)
	}
	imp.metricsByKpi = map[string][]*models.KpiMetric{}
	imp.metricByCode = map[string]*models.KpiMetric{}
	imp.metricByName = map[string]*models.KpiMetric{}
	byID := map[uuid.UUID]*models.KpiMetric{}
	for i := range metrics {
		m := &metrics[i]
		group := strings.ToLower(m.KpiType) + "|" + m.KpiID.String()
		imp.metricsByKpi[group] = append(imp.metricsByKpi[group], m)
		byID[m.ID] = m
		if code := strings.ToLower(strings.TrimSpace(m.MetricCode)); code != "" {
			imp.metricByCode[group+"|"+code] = m
		}
		if name := strings.ToLower(strings.TrimSpace(m.Name)); name != "" {
			imp.metricByName[group+"|"+name] = m
		}
	}

	// A concurrent import may have created a metric planned as new. Re-resolve
	// those rows under the lock so they skip or update instead of duplicating.
	claimed := map[uuid.UUID]int{}
	for _, r := range imp.recs {
		if r.action == kpiImportActionUpdate && r.ExistingID != nil {
			claimed[*r.ExistingID] = r.Row
		}
	}
	for _, r := range imp.recs {
		if r.action != kpiImportActionCreate || r.Rejected {
			continue
		}
		group := r.KpiType + "|" + r.KpiID.String()
		codeKey := strings.ToLower(strings.TrimSpace(r.Metric.MetricCode))
		nameKey := strings.ToLower(strings.TrimSpace(r.Metric.Name))
		m := imp.matchExistingMetric(group, codeKey, nameKey)
		if m == nil {
			continue
		}
		if row, dup := claimed[m.ID]; dup {
			r.reject("Metric Code", fmt.Sprintf("Duplicate metric in this file — row %d already matches the same existing metric.", row))
			continue
		}
		if other := imp.metricByName[group+"|"+nameKey]; other != nil && other.ID != m.ID {
			r.reject("Metric Name", fmt.Sprintf("Metric name %q already exists under this KPI.", r.Metric.Name))
			continue
		}
		claimed[m.ID] = r.Row
		id := m.ID
		r.ExistingID = &id
		r.ExistingWeight = m.Weight
		r.ExistingActive = strings.EqualFold(m.MetricStatus, "Active")
		r.ExistingBaseline = m.BaselineValue
		r.ExistingBaselinePeriod = m.BaselinePeriod
		if imp.opts.ExistingMode == KpiImportExistingUpdate {
			r.action = kpiImportActionUpdate
		} else {
			r.action = kpiImportActionSkip
		}
	}
	for _, r := range imp.recs {
		if r.action != kpiImportActionUpdate {
			continue
		}
		m := byID[*r.ExistingID]
		if m == nil {
			r.reject("Metric Name", fmt.Sprintf("Metric %q no longer exists under KPI %s; it may have been deleted.", r.Metric.Name, r.KpiCode))
			continue
		}
		group := r.KpiType + "|" + r.KpiID.String()
		nameKey := strings.ToLower(strings.TrimSpace(r.Metric.Name))
		if other := imp.metricByName[group+"|"+nameKey]; other != nil && other.ID != m.ID {
			r.reject("Metric Name", fmt.Sprintf("Metric name %q already exists under this KPI.", r.Metric.Name))
		}
		// Blank cells are not written on update; mirror the current value
		// the commit leaves in place so the checks below and the weight
		// delta use the fresh row.
		if !r.Set["weight"] {
			r.Metric.Weight = m.Weight
		}
		if !r.Set["metric_status"] {
			r.Metric.MetricStatus = m.MetricStatus
		}
		if !r.Set["baseline_value"] {
			r.Metric.BaselineValue = m.BaselineValue
		}
		if !r.Set["baseline_period"] {
			r.Metric.BaselinePeriod = m.BaselinePeriod
		}
		if !r.Set["start_date"] {
			r.Metric.StartDate = m.StartDate
		}
		if !r.Set["due_date"] {
			r.Metric.DueDate = m.DueDate
		}
		r.ExistingWeight = m.Weight
		r.ExistingActive = strings.EqualFold(m.MetricStatus, "Active")
		// Validate the merged pair against the fresh stored dates, since the
		// commit writes only the supplied date cell.
		if r.Metric.StartDate != nil && r.Metric.DueDate != nil &&
			r.Metric.DueDate.Before(*r.Metric.StartDate) {
			r.reject("Due Date", "Due Date cannot be earlier than Start Date.")
		}
		// A KPI approved after planning locks weight and baseline.
		if imp.kpiLocked[r.KpiType+"|"+r.KpiID.String()] {
			if r.Set["weight"] && math.Abs(m.Weight-r.Metric.Weight) >= 0.005 {
				r.reject("Weight", "The KPI is approved; Weight is locked and can only be changed before approval.")
			}
			if (r.Set["baseline_value"] || r.Set["baseline_period"]) &&
				(math.Abs(m.BaselineValue-r.Metric.BaselineValue) >= 0.00005 || m.BaselinePeriod != r.Metric.BaselinePeriod) {
				r.reject("Baseline Value", "The KPI is approved; Baseline is locked and can only be changed through an approved baseline revision.")
			}
		}
	}

	imp.applyWeightLimit()
	// Rebuild the report from the re-checked rows; errors derive from them.
	imp.res.Errors = []KpiImportIssue{}
	imp.finalize()
	// Skip mode can legitimately end with every planned create now existing;
	// only abort mode turns re-check rejections into a rollback.
	if imp.opts.ErrorMode == KpiImportErrorsAbort && len(imp.res.Errors) > 0 {
		return fmt.Errorf("the commit re-check rejected rows; nothing was committed")
	}
	return nil
}

// commit writes the validated plan; updates keep the metric ID so linked
// targets and entries stay valid.
func (imp *kpiMetricsImporter) commit(tx *gorm.DB) error {
	for _, r := range imp.recs {
		switch r.action {
		case kpiImportActionCreate:
			m := *r.Metric
			m.KpiID = r.KpiID
			m.KpiType = r.KpiType
			m.CreatedByID = imp.userID
			// New metrics start at their baseline, matching the metric form.
			m.CurrentValue = m.BaselineValue
			if err := tx.Omit(clause.Associations).Create(&m).Error; err != nil {
				return fmt.Errorf("creating metric %q for KPI %s: %w", m.Name, r.KpiCode, err)
			}
		case kpiImportActionUpdate:
			if r.ExistingID == nil {
				return fmt.Errorf("metric %q for KPI %s has no existing record to update", r.Metric.Name, r.KpiCode)
			}
			res := tx.Model(&models.KpiMetric{}).Where("id = ?", *r.ExistingID).
				Updates(kpiMetricUpdateMap(r.Metric, r.Set))
			if res.Error != nil {
				return fmt.Errorf("updating metric %q for KPI %s: %w", r.Metric.Name, r.KpiCode, res.Error)
			}
			if res.RowsAffected == 0 {
				return fmt.Errorf("updating metric %q for KPI %s: record no longer exists", r.Metric.Name, r.KpiCode)
			}
		}
	}
	return nil
}

// kpiMetricUpdateMap includes only fields present in the row; identity and
// runtime values are untouched.
func kpiMetricUpdateMap(m *models.KpiMetric, set map[string]bool) map[string]interface{} {
	updates := map[string]interface{}{"name": m.Name}
	if set["metric_code"] {
		updates["metric_code"] = m.MetricCode
	}
	if set["metric_description"] {
		updates["metric_description"] = m.MetricDescription
	}
	if set["metric_status"] {
		updates["metric_status"] = m.MetricStatus
	}
	if set["display_order"] {
		updates["display_order"] = m.DisplayOrder
	}
	if set["metric_type"] {
		updates["metric_type"] = m.MetricType
	}
	if set["unit"] {
		updates["unit"] = m.Unit
	}
	if set["custom_unit_label"] {
		updates["custom_unit_label"] = m.CustomUnitLabel
	}
	if set["baseline_value"] {
		updates["baseline_value"] = m.BaselineValue
	}
	if set["baseline_period"] {
		updates["baseline_period"] = m.BaselinePeriod
	}
	if set["weight"] {
		updates["weight"] = m.Weight
	}
	if set["formula"] {
		updates["formula"] = m.Formula
	}
	if set["calculation_type"] {
		updates["calculation_type"] = m.CalculationType
	}
	if set["direction"] {
		updates["direction"] = m.Direction
	}
	if set["decimal_precision"] {
		updates["decimal_precision"] = m.DecimalPrecision
	}
	if set["aggregation_method"] {
		updates["aggregation_method"] = m.AggregationMethod
	}
	if set["reporting_frequency"] {
		updates["reporting_frequency"] = m.ReportingFrequency
	}
	if set["numerator_label"] {
		updates["numerator_label"] = m.NumeratorLabel
	}
	if set["numerator_variable_code"] {
		updates["numerator_variable_code"] = m.NumeratorVariableCode
	}
	if set["denominator_label"] {
		updates["denominator_label"] = m.DenominatorLabel
	}
	if set["denominator_variable_code"] {
		updates["denominator_variable_code"] = m.DenominatorVariableCode
	}
	if set["direct_actual_label"] {
		updates["direct_actual_label"] = m.DirectActualLabel
	}
	if set["allow_manual_actual_override"] {
		updates["allow_manual_actual_override"] = m.AllowManualActualOverride
	}
	if set["advanced_formula_enabled"] {
		updates["advanced_formula_enabled"] = m.AdvancedFormulaEnabled
	}
	if set["formula_code"] {
		updates["formula_code"] = m.FormulaCode
	}
	if set["divide_by_zero_handling"] {
		updates["divide_by_zero_handling"] = m.DivideByZeroHandling
	}
	if set["rounding_rule"] {
		updates["rounding_rule"] = m.RoundingRule
	}
	if set["calculation_trace_required"] {
		updates["calculation_trace_required"] = m.CalculationTraceRequired
	}
	if set["metric_owner"] {
		updates["metric_owner_id"] = m.MetricOwnerID
	}
	if set["data_source"] {
		updates["data_source"] = m.DataSource
	}
	if set["evidence_required"] {
		updates["evidence_required"] = m.EvidenceRequired
	}
	if set["start_date"] {
		updates["start_date"] = m.StartDate
	}
	if set["due_date"] {
		updates["due_date"] = m.DueDate
	}
	return updates
}
