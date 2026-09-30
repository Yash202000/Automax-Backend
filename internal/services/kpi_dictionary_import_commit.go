package services

import (
	"fmt"
	"strings"
	"time"
	"unicode"

	"github.com/automax/backend/internal/models"
	"github.com/google/uuid"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

// commit writes the import plan inside tx. Order follows the reference
// chain: departments → pillars/enablers → goals → objectives → processes →
// domains/criteria/sub-criteria → initiatives → KPIs → metrics → targets →
// performance entries. Any error aborts (and rolls back) the whole import.
func (imp *kpiImporter) commit(tx *gorm.DB) error {
	create := func(n *mdNode) bool {
		return n.action == kpiImportActionCreate
	}
	idOf := func(n *mdNode) *uuid.UUID {
		if n == nil || n.Rejected {
			return nil
		}
		id := n.ID
		return &id
	}
	insert := func(label string, v interface{}) error {
		if err := tx.Omit(clause.Associations).Create(v).Error; err != nil {
			return fmt.Errorf("creating %s: %w", label, err)
		}
		return nil
	}

	for _, n := range imp.md[mdDepartment] {
		if !create(n) {
			continue
		}
		d := &models.Department{ID: n.ID, Name: n.NameEn, NameAr: n.NameAr, Code: imp.newDeptCode(n.NameEn), Type: n.DeptType, IsActive: true}
		if d.Type == "" {
			d.Type = "internal"
		}
		if err := insert("department "+n.NameEn, d); err != nil {
			return err
		}
	}
	for _, n := range imp.md[mdPillar] {
		if create(n) {
			if err := insert("pillar "+n.NameEn, &models.Pillar{ID: n.ID, NameEn: n.NameEn, NameAr: n.NameAr, OwnerID: idOf(n.Owner), IsActive: true}); err != nil {
				return err
			}
		}
	}
	for _, n := range imp.md[mdEnabler] {
		if create(n) {
			if err := insert("enabler "+n.NameEn, &models.Enabler{ID: n.ID, NameEn: n.NameEn, NameAr: n.NameAr, OwnerID: idOf(n.Owner), IsActive: true}); err != nil {
				return err
			}
		}
	}
	for _, n := range imp.md[mdGoal] {
		if create(n) {
			g := &models.Goal{ID: n.ID, Title: n.NameEn, Priority: models.GoalPriorityMedium, Status: models.GoalStatusDraft,
				OwnerID: imp.userID, CreatedByID: imp.userID, Path: n.ID.String()}
			if err := insert("strategic goal "+n.NameEn, g); err != nil {
				return err
			}
		}
	}
	for _, n := range imp.md[mdObjective] {
		if create(n) {
			o := &models.OperationalObjective{ID: n.ID, NameEn: n.NameEn, NameAr: n.NameAr, GoalID: idOf(n.Goal), PillarID: idOf(n.Pillar), EnablerID: idOf(n.Enabler), IsActive: true}
			if err := insert("operational objective "+n.NameEn, o); err != nil {
				return err
			}
		}
	}
	for _, n := range imp.md[mdProcess] {
		if create(n) {
			p := &models.Process{ID: n.ID, NameEn: n.NameEn, NameAr: n.NameAr, OperationalObjectiveID: n.Objective.ID, IsActive: true}
			p.GoalID, p.PillarID, p.EnablerID = idOf(n.Objective.Goal), idOf(n.Objective.Pillar), idOf(n.Objective.Enabler)
			if err := insert("process "+n.NameEn, p); err != nil {
				return err
			}
		}
	}
	for _, n := range imp.md[mdDomain] {
		if create(n) {
			if err := insert("domain "+n.NameEn, &models.Domain{ID: n.ID, NameEn: n.NameEn, NameAr: n.NameAr, Type: n.DomainType, IsActive: true}); err != nil {
				return err
			}
		}
	}
	for _, n := range imp.md[mdCriterion] {
		if create(n) {
			if err := insert("award criterion "+n.NameEn, &models.AwardCriterion{ID: n.ID, CriterionNo: n.CriterionNo, NameEn: n.NameEn, NameAr: n.NameAr, IsActive: true}); err != nil {
				return err
			}
		}
	}
	for _, n := range imp.md[mdSubCriterion] {
		if create(n) {
			s := &models.AwardSubCriterion{ID: n.ID, AwardCriterionID: n.Criterion.ID, SubNo: n.SubNo, NameEn: n.NameEn, NameAr: n.NameAr, IsActive: true}
			if err := insert("award sub-criterion "+n.SubNo, s); err != nil {
				return err
			}
		}
	}
	for _, n := range imp.md[mdInitiative] {
		if create(n) {
			i := &models.Initiative{ID: n.ID, NameEn: n.NameEn, NameAr: n.NameAr, GoalID: idOf(n.Goal), PillarID: idOf(n.Pillar), EnablerID: idOf(n.Enabler), OwnerID: idOf(n.Owner), Status: "draft"}
			if err := insert("initiative "+n.NameEn, i); err != nil {
				return err
			}
		}
	}

	// KPI definitions
	for _, k := range imp.kpis {
		switch k.action {
		case kpiImportActionCreate:
			if err := insert("KPI "+k.Code, imp.newKpiModel(k, idOf)); err != nil {
				return err
			}
		case kpiImportActionUpdate:
			if err := tx.Model(kpiModelFor(k.Type)).Where("id = ?", k.ID).Updates(imp.kpiUpdates(k, idOf)).Error; err != nil {
				return fmt.Errorf("updating KPI %s: %w", k.Code, err)
			}
		}
	}

	// Metrics
	for _, k := range imp.kpis {
		if !k.newMetric {
			continue
		}
		m := &models.KpiMetric{
			ID: uuid.New(), KpiID: k.ID, KpiType: k.Type, Name: k.NameEn, MetricCode: cutRunes(k.Code+"-M1", 50),
			MetricStatus: "Active", MetricType: "Numeric", BaselineValue: k.Baseline, Weight: 1,
			CalculationType: "Direct Value", Direction: directionFor(k.Polarity), DecimalPrecision: 2,
			AggregationMethod: "Average", ReportingFrequency: "Quarterly", DataSource: cutRunes(k.DataSource, 100),
			CalculationTraceRequired: true, CreatedByID: imp.userID,
		}
		if err := insert("metric for "+k.Code, m); err != nil {
			return err
		}
		id := m.ID
		k.MetricID = &id
	}

	now := time.Now()
	// Annual targets — imported as approved: they come from the approved
	// KPI Dictionary workbook.
	for _, k := range imp.kpis {
		for _, t := range k.Targets {
			start := time.Date(t.Year, 1, 1, 0, 0, 0, 0, time.Local)
			end := time.Date(t.Year, 12, 31, 23, 59, 59, 0, time.Local)
			switch t.action {
			case kpiImportActionCreate:
				row := &models.KpiAnnualTarget{
					KpiCode: k.Code, KpiType: k.Type, Year: t.Year, TargetYear: t.Year, PeriodType: "annual", PeriodKey: "annual", PeriodCode: "annual",
					PeriodStart: &start, PeriodEnd: &end, TargetValue: t.Value, MetricID: k.MetricID,
					DirectionSnapshot: directionFor(k.Polarity), ReportingFrequencySnapshot: k.Frequency,
					TargetRationale: "Imported from KPI Dictionary workbook " + imp.res.FileName,
					TargetStatus:    "approved", ApprovedByID: &imp.userID, ApprovedAt: &now, Version: 1,
				}
				if err := insert(fmt.Sprintf("target %s %d", k.Code, t.Year), row); err != nil {
					return err
				}
			case kpiImportActionUpdate:
				if err := tx.Model(&models.KpiAnnualTarget{}).Where("id = ?", *t.ExistingID).
					Updates(map[string]interface{}{"target_value": t.Value, "updated_at": now}).Error; err != nil {
					return fmt.Errorf("updating target %s %d: %w", k.Code, t.Year, err)
				}
			}
		}
	}

	// Historical quarterly performance → approved KPI entries.
	for _, p := range imp.perfs {
		if p.action != kpiImportActionCreate && p.action != kpiImportActionUpdate {
			continue
		}
		k := p.Kpi
		if k.MetricID == nil {
			return fmt.Errorf("KPI %s has no metric to attach performance to", k.Code)
		}
		e := models.KpiEntry{
			KpiID: k.ID, KpiType: k.Type, MetricID: *k.MetricID, ReportingYear: p.Year, PeriodCode: fmt.Sprintf("q%d", p.Quarter),
			CalculationTypeSnapshot: "Direct Value", DirectionSnapshot: directionFor(k.Polarity), DecimalPrecisionSnapshot: 2,
			AggregationMethodSnapshot: "Average", TargetValueSnapshot: p.Target, ActualValue: p.Actual,
			DataSourceType: "Import", SourceReference: cutRunes(fmt.Sprintf("%s — %s row %d", imp.res.FileName, p.Sheet, p.Row), 500),
			DataQualityStatus: "Not Verified", Status: models.KpiEntryStatusApproved, EntryVersion: 1,
		}
		actual := p.Actual
		e.DirectActualValue = &actual
		start, end := quarterStart(p.Year, p.Quarter), quarterEnd(p.Year, p.Quarter)
		e.PeriodStart, e.PeriodEnd = &start, &end
		e.ComputeAchievement()

		if p.action == kpiImportActionUpdate {
			if err := tx.Model(&models.KpiEntry{}).Where("id = ?", *p.ExistingID).Updates(map[string]interface{}{
				"target_value_snapshot": e.TargetValueSnapshot, "direct_actual_value": e.DirectActualValue, "actual_value": e.ActualValue,
				"achievement_percentage": e.AchievementPercentage, "variance_value": e.VarianceValue, "performance_status": e.PerformanceStatus,
				"source_reference": e.SourceReference, "updated_at": now,
			}).Error; err != nil {
				return fmt.Errorf("updating performance %s %d Q%d: %w", k.Code, p.Year, p.Quarter, err)
			}
			continue
		}
		e.ID = uuid.New()
		e.SubmittedByID, e.ApprovedByID = &imp.userID, &imp.userID
		if err := insert(fmt.Sprintf("performance %s %d Q%d", k.Code, p.Year, p.Quarter), &e); err != nil {
			return err
		}
	}
	return nil
}

func kpiModelFor(kpiType string) interface{} {
	switch kpiType {
	case models.KPITypeOperational:
		return &models.OperationalKPI{}
	case models.KPITypeAward:
		return &models.AwardKPI{}
	}
	return &models.StrategicKPI{}
}

// newKpiModel builds the dictionary row for a new KPI. Imported KPIs always
// start as Draft; the caller starts their normal dictionary workflow after
// the commit.
func (imp *kpiImporter) newKpiModel(k *kpiRec, idOf func(*mdNode) *uuid.UUID) interface{} {
	switch k.Type {
	case models.KPITypeOperational:
		return &models.OperationalKPI{
			ID: k.ID, Code: k.Code, NameEn: k.NameEn, NameAr: k.NameAr,
			GoalID: idOf(k.Goal), OperationalObjectiveID: k.Objective.ID, ProcessID: k.Process.ID, PillarID: idOf(k.Pillar),
			AwardSubCriterionID: idOf(k.SubCriterion), DomainID: idOf(k.Domain),
			OwnerType: k.OwnerType, OwnerDeptID: idOf(k.OwnerDept), OwningAgencyID: idOf(k.Agency),
			Polarity: k.Polarity, ActivationStatus: models.KPIStatusDraft, DescriptionEn: k.DescriptionEn, DescriptionAr: k.DescriptionAr,
			Formula: k.Formula, Baseline: k.Baseline, ReportingFrequency: k.Frequency, Lifecycle: k.Lifecycle, DataSource: k.DataSource,
			RelatedUnits: k.RelatedUnits, Notes: k.Notes,
		}
	case models.KPITypeAward:
		return &models.AwardKPI{
			ID: k.ID, Code: k.Code, NameEn: k.NameEn, NameAr: k.NameAr,
			GoalID: idOf(k.Goal), AwardSubCriterionID: idOf(k.SubCriterion), PillarID: idOf(k.Pillar), DomainID: idOf(k.Domain),
			OwnerType: k.OwnerType, OwnerDeptID: idOf(k.OwnerDept), OwningAgencyID: idOf(k.Agency),
			Polarity: k.Polarity, ActivationStatus: models.KPIStatusDraft, DescriptionEn: k.DescriptionEn, DescriptionAr: k.DescriptionAr,
			Formula: k.Formula, Baseline: k.Baseline, ReportingFrequency: k.Frequency, Lifecycle: k.Lifecycle, DataSource: k.DataSource,
			RelatedUnits: k.RelatedUnits, Notes: k.Notes,
		}
	}
	return &models.StrategicKPI{
		ID: k.ID, Code: k.Code, NameEn: k.NameEn, NameAr: k.NameAr,
		PillarID: idOf(k.Pillar), GoalID: idOf(k.Goal), DomainID: idOf(k.Domain),
		OwnerType: k.OwnerType, OwnerDeptID: idOf(k.OwnerDept), OwningAgencyID: idOf(k.Agency),
		Polarity: k.Polarity, ActivationStatus: models.KPIStatusDraft, DescriptionEn: k.DescriptionEn, DescriptionAr: k.DescriptionAr,
		Formula: k.Formula, Baseline: k.Baseline, ReportingFrequency: k.Frequency, Lifecycle: k.Lifecycle, DataSource: k.DataSource,
		SegmentationAxes: k.SegAxes, RelatedUnits: k.RelatedUnits, Notes: k.Notes,
	}
}

// kpiUpdates is the "update existing" field set: the KPI's definition as
// given in the workbook. Activation status and workflow are deliberately
// left alone — they are managed by the KPI workflow, not the import.
func (imp *kpiImporter) kpiUpdates(k *kpiRec, idOf func(*mdNode) *uuid.UUID) map[string]interface{} {
	u := map[string]interface{}{
		"name_en": k.NameEn, "name_ar": k.NameAr, "pillar_id": idOf(k.Pillar), "goal_id": idOf(k.Goal), "domain_id": idOf(k.Domain),
		"owner_type": k.OwnerType, "owner_dept_id": idOf(k.OwnerDept), "owning_agency_id": idOf(k.Agency), "polarity": k.Polarity,
		"description_en": k.DescriptionEn, "description_ar": k.DescriptionAr, "formula": k.Formula, "baseline": k.Baseline,
		"reporting_frequency": k.Frequency, "lifecycle": k.Lifecycle, "data_source": k.DataSource, "related_units": k.RelatedUnits,
		"notes": k.Notes, "updated_at": time.Now(),
	}
	switch k.Type {
	case models.KPITypeStrategic:
		u["segmentation_axes"] = k.SegAxes
	case models.KPITypeOperational:
		u["operational_objective_id"], u["process_id"] = idOf(k.Objective), idOf(k.Process)
	case models.KPITypeAward:
		u["award_sub_criterion_id"] = idOf(k.SubCriterion)
	}
	return u
}

// newDeptCode derives a unique department code from the name's initials
// (e.g. "Road Maintenance Department" → "RMD", then "RMD2", …).
func (imp *kpiImporter) newDeptCode(name string) string {
	var b strings.Builder
	for _, w := range strings.Fields(name) {
		r := []rune(w)[0]
		if r < unicode.MaxASCII && (unicode.IsLetter(r) || unicode.IsDigit(r)) {
			b.WriteRune(unicode.ToUpper(r))
		}
	}
	base := b.String()
	if len(base) < 2 {
		base = "DEPT"
	}
	if len(base) > 10 {
		base = base[:10]
	}
	code := base
	for i := 2; imp.deptCodes[code]; i++ {
		code = fmt.Sprintf("%s%d", base, i)
	}
	imp.deptCodes[code] = true
	return code
}

func directionFor(polarity string) string {
	if polarity == models.KPIPolarityDescending {
		return "Lower is Better"
	}
	return "Higher is Better"
}

func cutRunes(s string, n int) string {
	r := []rune(s)
	if len(r) <= n {
		return s
	}
	return string(r[:n])
}
