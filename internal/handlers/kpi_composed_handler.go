package handlers

import (
	"github.com/automax/backend/internal/models"
	"github.com/automax/backend/internal/services"
	"github.com/automax/backend/pkg/i18n"
	"github.com/automax/backend/pkg/utils"
	"github.com/gofiber/fiber/v2"
	"github.com/google/uuid"
	"gorm.io/gorm"
)

// KpiComposedHandler serves read-only, composed views over the KPI dictionary +
// targets + performance + bands — the "KPI Card" one-pager, the per-KPI
// dashboard, and the per-KPI annual rollup — none of which exist as a single
// query anywhere else in the API (each of those pieces otherwise requires a
// separate round trip that the frontend would have to assemble itself).
type KpiComposedHandler struct {
	db *gorm.DB
}

func NewKpiComposedHandler(db *gorm.DB) *KpiComposedHandler {
	return &KpiComposedHandler{db: db}
}

// kpiCardBase is the common shape extracted from whichever of the three
// dictionary tables the requested KPI lives in.
type kpiCardBase struct {
	Code               string
	NameEn             string
	NameAr             string
	DescriptionEn      string
	DescriptionAr      string
	ActivationStatus   string
	Lifecycle          string
	OwnerLabel         string
	OwnerType          string
	PillarEnablerLabel string
	StrategicGoalLabel string
	CriterionLabel     string
	ReportingFrequency string
	DataSource         string
	RelatedUnitsLabel  string
	Formula            string
	Polarity           string
	UnitOfMeasure      string
	Baseline           float64
}

func (h *KpiComposedHandler) loadCardBase(ctx *fiber.Ctx, kpiType string, id uuid.UUID) (*kpiCardBase, error) {
	db := h.db.WithContext(ctx.UserContext())

	ownerLabel := func(ownerType string, deptName, orgName, agencyName string) string {
		if ownerType == models.KPIOwnerTypeExternal && orgName != "" {
			return orgName
		}
		if deptName != "" {
			return deptName
		}
		return agencyName
	}

	switch kpiType {
	case models.KPITypeOperational:
		var k models.OperationalKPI
		if err := db.Preload("Goal").Preload("Domain").Preload("OwnerDept").Preload("OwnerOrg").Preload("OwningAgency").
			Preload("OperationalObjective.Pillar").Preload("OperationalObjective.Enabler").
			First(&k, id).Error; err != nil {
			return nil, err
		}
		pillarEnabler := ""
		if k.OperationalObjective != nil {
			if k.OperationalObjective.Pillar != nil {
				pillarEnabler = k.OperationalObjective.Pillar.NameEn
			} else if k.OperationalObjective.Enabler != nil {
				pillarEnabler = k.OperationalObjective.Enabler.NameEn
			}
		}
		goalTitle := ""
		if k.Goal != nil {
			goalTitle = k.Goal.Title
		}
		criterion := ""
		if k.Domain != nil {
			criterion = k.Domain.NameEn
		}
		deptName, orgName, agencyName := "", "", ""
		if k.OwnerDept != nil {
			deptName = k.OwnerDept.Name
		}
		if k.OwnerOrg != nil {
			orgName = k.OwnerOrg.NameEn
		}
		if k.OwningAgency != nil {
			agencyName = k.OwningAgency.Name
		}
		return &kpiCardBase{
			Code: k.Code, NameEn: k.NameEn, NameAr: k.NameAr,
			DescriptionEn: k.DescriptionEn, DescriptionAr: k.DescriptionAr,
			ActivationStatus: k.ActivationStatus, Lifecycle: k.Lifecycle,
			OwnerLabel: ownerLabel(k.OwnerType, deptName, orgName, agencyName), OwnerType: k.OwnerType,
			PillarEnablerLabel: pillarEnabler, StrategicGoalLabel: goalTitle, CriterionLabel: criterion,
			ReportingFrequency: k.ReportingFrequency, DataSource: k.DataSource,
			Formula: k.Formula, Polarity: k.Polarity, UnitOfMeasure: k.UnitOfMeasure, Baseline: k.Baseline,
		}, nil
	case models.KPITypeAward:
		var k models.AwardKPI
		if err := db.Preload("AwardSubCriterion.AwardCriterion").Preload("Domain").
			Preload("OwnerDept").Preload("OwnerOrg").Preload("OwningAgency").
			First(&k, id).Error; err != nil {
			return nil, err
		}
		criterion := ""
		if k.AwardSubCriterion != nil && k.AwardSubCriterion.AwardCriterion != nil {
			criterion = k.AwardSubCriterion.AwardCriterion.NameEn
		} else if k.Domain != nil {
			criterion = k.Domain.NameEn
		}
		deptName, orgName, agencyName := "", "", ""
		if k.OwnerDept != nil {
			deptName = k.OwnerDept.Name
		}
		if k.OwnerOrg != nil {
			orgName = k.OwnerOrg.NameEn
		}
		if k.OwningAgency != nil {
			agencyName = k.OwningAgency.Name
		}
		return &kpiCardBase{
			Code: k.Code, NameEn: k.NameEn, NameAr: k.NameAr,
			DescriptionEn: k.DescriptionEn, DescriptionAr: k.DescriptionAr,
			ActivationStatus: k.ActivationStatus, Lifecycle: k.Lifecycle,
			OwnerLabel: ownerLabel(k.OwnerType, deptName, orgName, agencyName), OwnerType: k.OwnerType,
			CriterionLabel:     criterion,
			ReportingFrequency: k.ReportingFrequency, DataSource: k.DataSource,
			Formula: k.Formula, Polarity: k.Polarity, UnitOfMeasure: k.UnitOfMeasure, Baseline: k.Baseline,
		}, nil
	default:
		var k models.StrategicKPI
		if err := db.Preload("Goal").Preload("Pillar").Preload("Domain").
			Preload("OwnerDept").Preload("OwnerOrg").Preload("OwningAgency").
			First(&k, id).Error; err != nil {
			return nil, err
		}
		pillarEnabler := ""
		if k.Pillar != nil {
			pillarEnabler = k.Pillar.NameEn
		}
		goalTitle := ""
		if k.Goal != nil {
			goalTitle = k.Goal.Title
		}
		criterion := ""
		if k.Domain != nil {
			criterion = k.Domain.NameEn
		}
		deptName, orgName, agencyName := "", "", ""
		if k.OwnerDept != nil {
			deptName = k.OwnerDept.Name
		}
		if k.OwnerOrg != nil {
			orgName = k.OwnerOrg.NameEn
		}
		if k.OwningAgency != nil {
			agencyName = k.OwningAgency.Name
		}
		return &kpiCardBase{
			Code: k.Code, NameEn: k.NameEn, NameAr: k.NameAr,
			DescriptionEn: k.DescriptionEn, DescriptionAr: k.DescriptionAr,
			ActivationStatus: k.ActivationStatus, Lifecycle: k.Lifecycle,
			OwnerLabel: ownerLabel(k.OwnerType, deptName, orgName, agencyName), OwnerType: k.OwnerType,
			PillarEnablerLabel: pillarEnabler, StrategicGoalLabel: goalTitle, CriterionLabel: criterion,
			ReportingFrequency: k.ReportingFrequency, DataSource: k.DataSource, RelatedUnitsLabel: k.RelatedUnits,
			Formula: k.Formula, Polarity: k.Polarity, UnitOfMeasure: k.UnitOfMeasure, Baseline: k.Baseline,
		}, nil
	}
}

func (h *KpiComposedHandler) effectiveBand(ctx *fiber.Ctx, kpiCode string) models.KpiPerformanceBandResponse {
	db := h.db.WithContext(ctx.UserContext())
	var override models.KpiPerformanceBand
	if err := db.Where("kpi_code = ?", kpiCode).First(&override).Error; err == nil {
		return override.ToResponse()
	}
	var global models.KpiPerformanceBand
	if err := db.Where("kpi_code IS NULL").Order("created_at ASC").First(&global).Error; err == nil {
		return global.ToResponse()
	}
	return models.DefaultKpiPerformanceBand
}

// KpiCardTargetRow is one row of the Card's "Target Plan" table.
type KpiCardTargetRow struct {
	TargetYear      int     `json:"target_year"`
	PeriodCode      string  `json:"period_code"`
	TargetValue     float64 `json:"target_value"`
	TargetFrequency string  `json:"target_frequency"`
	Notes           string  `json:"notes"`
}

// KpiCardResponse composes dictionary fields + target plan + performance
// bands into the Excel's one-page "KPI Card" view.
type KpiCardResponse struct {
	Code               string                       `json:"code"`
	Type               string                       `json:"type"`
	NameEn             string                       `json:"name_en"`
	NameAr             string                       `json:"name_ar"`
	DescriptionEn      string                       `json:"description_en"`
	DescriptionAr      string                       `json:"description_ar"`
	ActivationStatus   string                       `json:"activation_status"`
	Lifecycle          string                       `json:"lifecycle"`
	OwnerLabel         string                       `json:"owner_label"`
	OwnerType          string                       `json:"owner_type"`
	PillarEnablerLabel string                       `json:"pillar_enabler_label"`
	StrategicGoalLabel string                       `json:"strategic_goal_label"`
	CriterionLabel     string                       `json:"criterion_label"`
	ReportingFrequency string                       `json:"reporting_frequency"`
	DataSource         string                       `json:"data_source"`
	RelatedUnitsLabel  string                       `json:"related_units_label"`
	Formula            string                       `json:"formula"`
	Polarity           string                       `json:"polarity"`
	UnitOfMeasure      string                       `json:"unit_of_measure"`
	Baseline           float64                      `json:"baseline"`
	TargetPlan         []KpiCardTargetRow           `json:"target_plan"`
	Bands              models.KpiPerformanceBandResponse `json:"bands"`
}

// GetKpiCard returns the composed one-pager for a single KPI.
// GET /kpi/:type/:id/card
func (h *KpiComposedHandler) GetKpiCard(c *fiber.Ctx) error {
	kpiType := c.Params("type")
	if !isValidKpiType(kpiType) {
		return utils.ErrorResponse(c, fiber.StatusBadRequest, i18n.T(c.UserContext(), "invalid_id"))
	}
	id, err := uuid.Parse(c.Params("id"))
	if err != nil {
		return utils.ErrorResponse(c, fiber.StatusBadRequest, i18n.T(c.UserContext(), "invalid_id"))
	}

	base, err := h.loadCardBase(c, kpiType, id)
	if err != nil {
		return utils.ErrorResponse(c, fiber.StatusNotFound, i18n.T(c.UserContext(), "not_found"))
	}

	var targets []models.KpiAnnualTarget
	h.db.WithContext(c.UserContext()).
		Where("kpi_code = ? AND kpi_type = ?", base.Code, kpiType).
		Order("target_year ASC").Find(&targets)

	targetPlan := make([]KpiCardTargetRow, len(targets))
	for i, t := range targets {
		targetPlan[i] = KpiCardTargetRow{
			TargetYear: t.TargetYear, PeriodCode: t.PeriodCode, TargetValue: t.TargetValue,
			TargetFrequency: t.ReportingFrequencySnapshot, Notes: t.TargetRationale,
		}
	}

	resp := KpiCardResponse{
		Code: base.Code, Type: kpiType, NameEn: base.NameEn, NameAr: base.NameAr,
		DescriptionEn: base.DescriptionEn, DescriptionAr: base.DescriptionAr,
		ActivationStatus: base.ActivationStatus, Lifecycle: base.Lifecycle,
		OwnerLabel: base.OwnerLabel, OwnerType: base.OwnerType,
		PillarEnablerLabel: base.PillarEnablerLabel, StrategicGoalLabel: base.StrategicGoalLabel,
		CriterionLabel: base.CriterionLabel, ReportingFrequency: base.ReportingFrequency,
		DataSource: base.DataSource, RelatedUnitsLabel: base.RelatedUnitsLabel,
		Formula: base.Formula, Polarity: base.Polarity, UnitOfMeasure: base.UnitOfMeasure, Baseline: base.Baseline,
		TargetPlan: targetPlan, Bands: h.effectiveBand(c, base.Code),
	}

	return utils.SuccessResponse(c, fiber.StatusOK, "", resp)
}

// KpiAnnualRollupRow is one year's Target/Actual/Achievement rollup.
type KpiAnnualRollupRow struct {
	Year           int      `json:"year"`
	Target         *float64 `json:"target"`
	Actual         *float64 `json:"actual"`
	AchievementPct *float64 `json:"achievement_pct"`
	IsDerived      bool     `json:"is_derived"`
}

// computeAnnualRollup builds one row per year that has any performance data,
// preferring an explicit period_type='annual' KpiPerformance row for that
// year; otherwise it averages the KPI's sub-annual entries for that year and
// flags the row as derived.
func (h *KpiComposedHandler) computeAnnualRollup(ctx *fiber.Ctx, kpiCode, kpiType, polarity string) []KpiAnnualRollupRow {
	db := h.db.WithContext(ctx.UserContext())

	var years []int
	db.Model(&models.KpiPerformance{}).
		Where("kpi_code = ? AND kpi_type = ?", kpiCode, kpiType).
		Distinct().Order("year ASC").Pluck("year", &years)

	rows := make([]KpiAnnualRollupRow, 0, len(years))
	for _, year := range years {
		var annual models.KpiPerformance
		err := db.Where("kpi_code = ? AND kpi_type = ? AND year = ? AND period_type = ?", kpiCode, kpiType, year, "annual").
			First(&annual).Error
		if err == nil {
			target, actual, pct := annual.Target, annual.Actual, annual.AchievementPct
			rows = append(rows, KpiAnnualRollupRow{Year: year, Target: &target, Actual: &actual, AchievementPct: &pct, IsDerived: false})
			continue
		}

		var subRows []models.KpiPerformance
		db.Where("kpi_code = ? AND kpi_type = ? AND year = ? AND period_type != ?", kpiCode, kpiType, year, "annual").Find(&subRows)
		if len(subRows) == 0 {
			continue
		}
		var targetSum, actualSum float64
		for _, r := range subRows {
			targetSum += r.Target
			actualSum += r.Actual
		}
		avgTarget := targetSum / float64(len(subRows))
		avgActual := actualSum / float64(len(subRows))
		pct := services.CalculateAchievement(avgActual, avgTarget, polarity)
		rows = append(rows, KpiAnnualRollupRow{Year: year, Target: &avgTarget, Actual: &avgActual, AchievementPct: &pct, IsDerived: true})
	}
	return rows
}

// GetKpiAnnualRollup returns the per-year Target/Actual/Achievement rollup for
// one KPI. GET /kpi/:type/:id/annual-rollup
func (h *KpiComposedHandler) GetKpiAnnualRollup(c *fiber.Ctx) error {
	kpiType := c.Params("type")
	if !isValidKpiType(kpiType) {
		return utils.ErrorResponse(c, fiber.StatusBadRequest, i18n.T(c.UserContext(), "invalid_id"))
	}
	id, err := uuid.Parse(c.Params("id"))
	if err != nil {
		return utils.ErrorResponse(c, fiber.StatusBadRequest, i18n.T(c.UserContext(), "invalid_id"))
	}
	base, err := h.loadCardBase(c, kpiType, id)
	if err != nil {
		return utils.ErrorResponse(c, fiber.StatusNotFound, i18n.T(c.UserContext(), "not_found"))
	}
	rows := h.computeAnnualRollup(c, base.Code, kpiType, base.Polarity)
	return utils.SuccessResponse(c, fiber.StatusOK, "", rows)
}

// KpiSingleDashboardResponse is the per-KPI dashboard view: header + annual
// rollup + band legend + latest narrative + benchmark summary.
type KpiSingleDashboardResponse struct {
	Card             KpiCardResponse      `json:"card"`
	AnnualRollup     []KpiAnnualRollupRow `json:"annual_rollup"`
	TrendDescription string               `json:"trend_description"`
	Justification    string               `json:"justification"`
	CorrectiveAction string               `json:"corrective_action"`
	Benchmarks       []models.KpiBenchmarkResponse `json:"benchmarks"`
}

// GetKpiDashboard returns the composed per-KPI dashboard.
// GET /kpi/:type/:id/dashboard
func (h *KpiComposedHandler) GetKpiDashboard(c *fiber.Ctx) error {
	kpiType := c.Params("type")
	if !isValidKpiType(kpiType) {
		return utils.ErrorResponse(c, fiber.StatusBadRequest, i18n.T(c.UserContext(), "invalid_id"))
	}
	id, err := uuid.Parse(c.Params("id"))
	if err != nil {
		return utils.ErrorResponse(c, fiber.StatusBadRequest, i18n.T(c.UserContext(), "invalid_id"))
	}

	base, err := h.loadCardBase(c, kpiType, id)
	if err != nil {
		return utils.ErrorResponse(c, fiber.StatusNotFound, i18n.T(c.UserContext(), "not_found"))
	}

	var targets []models.KpiAnnualTarget
	h.db.WithContext(c.UserContext()).
		Where("kpi_code = ? AND kpi_type = ?", base.Code, kpiType).
		Order("target_year ASC").Find(&targets)
	targetPlan := make([]KpiCardTargetRow, len(targets))
	for i, t := range targets {
		targetPlan[i] = KpiCardTargetRow{
			TargetYear: t.TargetYear, PeriodCode: t.PeriodCode, TargetValue: t.TargetValue,
			TargetFrequency: t.ReportingFrequencySnapshot, Notes: t.TargetRationale,
		}
	}

	card := KpiCardResponse{
		Code: base.Code, Type: kpiType, NameEn: base.NameEn, NameAr: base.NameAr,
		DescriptionEn: base.DescriptionEn, DescriptionAr: base.DescriptionAr,
		ActivationStatus: base.ActivationStatus, Lifecycle: base.Lifecycle,
		OwnerLabel: base.OwnerLabel, OwnerType: base.OwnerType,
		PillarEnablerLabel: base.PillarEnablerLabel, StrategicGoalLabel: base.StrategicGoalLabel,
		CriterionLabel: base.CriterionLabel, ReportingFrequency: base.ReportingFrequency,
		DataSource: base.DataSource, RelatedUnitsLabel: base.RelatedUnitsLabel,
		Formula: base.Formula, Polarity: base.Polarity, UnitOfMeasure: base.UnitOfMeasure, Baseline: base.Baseline,
		TargetPlan: targetPlan, Bands: h.effectiveBand(c, base.Code),
	}

	rollup := h.computeAnnualRollup(c, base.Code, kpiType, base.Polarity)

	var latest models.KpiPerformance
	trend, justification, corrective := "", "", ""
	if err := h.db.WithContext(c.UserContext()).
		Where("kpi_code = ? AND kpi_type = ?", base.Code, kpiType).
		Order("year DESC, quarter DESC, created_at DESC").First(&latest).Error; err == nil {
		trend, justification, corrective = latest.TrendDescription, latest.Justification, latest.CorrectiveAction
	}

	var benchmarks []models.KpiBenchmark
	h.db.WithContext(c.UserContext()).
		Where("kpi_code = ? AND kpi_type = ?", base.Code, kpiType).
		Order("year DESC").Limit(5).Find(&benchmarks)
	benchmarkResp := make([]models.KpiBenchmarkResponse, len(benchmarks))
	for i, b := range benchmarks {
		benchmarkResp[i] = b.ToBenchmarkResponse()
	}

	resp := KpiSingleDashboardResponse{
		Card: card, AnnualRollup: rollup,
		TrendDescription: trend, Justification: justification, CorrectiveAction: corrective,
		Benchmarks: benchmarkResp,
	}
	return utils.SuccessResponse(c, fiber.StatusOK, "", resp)
}

// parseRollupQuery reads the year/period_code/organization_id query params
// shared by GetMetricPeriodRollup and GetCompositeScore.
func parseRollupQuery(c *fiber.Ctx) (year int, periodCode string, organizationID *uuid.UUID, err error) {
	periodCode = c.Query("period_code")
	if periodCode == "" {
		return 0, "", nil, fiber.NewError(fiber.StatusBadRequest, "period_code is required")
	}
	year = c.QueryInt("year", 0)
	if year == 0 {
		return 0, "", nil, fiber.NewError(fiber.StatusBadRequest, "year is required")
	}
	if orgStr := c.Query("organization_id"); orgStr != "" {
		oid, parseErr := uuid.Parse(orgStr)
		if parseErr != nil {
			return 0, "", nil, fiber.NewError(fiber.StatusBadRequest, "invalid organization_id")
		}
		organizationID = &oid
	}
	return year, periodCode, organizationID, nil
}

// GetMetricPeriodRollup returns one metric's aggregated period Actual —
// Baseline, Target, Actual, Achievement (raw + capped), entry count, and an
// explicit HasData flag — the calculation the doc's "Monthly Calculation
// Method" (section 4.3) and BR-09/BR-10 describe. Approved Entries are
// aggregated (sum-numerator/sum-denominator for ratio metrics, recalculating
// the ratio; AggregationMethod-driven otherwise) rather than any single
// Entry being treated as the period's value.
func (h *KpiComposedHandler) GetMetricPeriodRollup(c *fiber.Ctx) error {
	kpiType := c.Params("type")
	if !isValidKpiType(kpiType) {
		return utils.ErrorResponse(c, fiber.StatusBadRequest, i18n.T(c.UserContext(), "invalid_id"))
	}
	kpiID, err := uuid.Parse(c.Params("id"))
	if err != nil {
		return utils.ErrorResponse(c, fiber.StatusBadRequest, i18n.T(c.UserContext(), "invalid_id"))
	}
	metricID, err := uuid.Parse(c.Params("metricId"))
	if err != nil {
		return utils.ErrorResponse(c, fiber.StatusBadRequest, i18n.T(c.UserContext(), "invalid_id"))
	}
	year, periodCode, organizationID, qErr := parseRollupQuery(c)
	if qErr != nil {
		return utils.ErrorResponse(c, fiber.StatusBadRequest, qErr.Error())
	}

	db := h.db.WithContext(c.UserContext())

	var metric models.KpiMetric
	if err := db.Where("id = ? AND kpi_id = ? AND kpi_type = ?", metricID, kpiID, kpiType).First(&metric).Error; err != nil {
		return utils.ErrorResponse(c, fiber.StatusNotFound, i18n.T(c.UserContext(), "not_found"))
	}

	kpiCode, err := getKPICode(db, kpiType, kpiID)
	if err != nil || kpiCode == "" {
		return utils.ErrorResponse(c, fiber.StatusNotFound, i18n.T(c.UserContext(), "not_found"))
	}

	resp := buildMetricPeriodRollupMap(db, &metric, kpiID, kpiCode, kpiType, year, periodCode, organizationID)
	return utils.SuccessResponse(c, fiber.StatusOK, "", resp)
}

// buildMetricPeriodRollupMap assembles the period-rollup response shape
// (Baseline/Actual/Target/Achievement raw+capped/entry count) shared by
// GetMetricPeriodRollup (caller-specified period) and GetMetricDisplayRollup
// (server-resolved period).
func buildMetricPeriodRollupMap(db *gorm.DB, metric *models.KpiMetric, kpiID uuid.UUID, kpiCode, kpiType string, year int, periodCode string, organizationID *uuid.UUID) fiber.Map {
	rollup := services.AggregateMetricPeriod(db, metric, kpiID, kpiType, year, periodCode, organizationID)

	resp := fiber.Map{
		"metric_id":       metric.ID,
		"metric_name":     metric.Name,
		"period_code":     periodCode,
		"year":            year,
		"baseline":        metric.BaselineValue,
		"has_data":        rollup.HasData,
		"entry_count":     rollup.EntryCount,
		"actual":          rollup.Actual,
		"sum_numerator":   rollup.SumNumerator,
		"sum_denominator": rollup.SumDenominator,
	}

	targetQuery := db.Where(
		"kpi_code = ? AND kpi_type = ? AND metric_id = ? AND target_year = ? AND period_code = ? AND target_status = 'approved'",
		kpiCode, kpiType, metric.ID, year, periodCode,
	)
	if organizationID != nil {
		targetQuery = targetQuery.Where("organization_id = ?", *organizationID)
	} else {
		targetQuery = targetQuery.Where("organization_id IS NULL")
	}
	var target models.KpiAnnualTarget
	if err := targetQuery.Order("version DESC").First(&target).Error; err == nil {
		resp["target"] = target.TargetValue
		if rollup.HasData {
			achievement := services.ComputeAchievementRawCapped(*rollup.Actual, target.TargetValue, metric.Direction, 100)
			if achievement != nil {
				resp["achievement_raw"] = achievement.Raw
				resp["achievement_capped"] = achievement.Capped
				resp["performance_status"] = services.PerformanceStatusFor(achievement.Capped)
			}
		}
	}

	return resp
}

// GetMetricDisplayRollup is what the Metric Card actually calls: it resolves
// which period to show (the real current period if it has data, otherwise
// the most recent period that does — services.ResolveDisplayPeriod) instead
// of requiring the caller to already know the right period. Without this, a
// metric populated for a period other than "right now" (testing next month,
// backfilling a past one) would read as a misleading "No Data" purely
// because the calendar hasn't caught up to it yet.
func (h *KpiComposedHandler) GetMetricDisplayRollup(c *fiber.Ctx) error {
	kpiType := c.Params("type")
	if !isValidKpiType(kpiType) {
		return utils.ErrorResponse(c, fiber.StatusBadRequest, i18n.T(c.UserContext(), "invalid_id"))
	}
	kpiID, err := uuid.Parse(c.Params("id"))
	if err != nil {
		return utils.ErrorResponse(c, fiber.StatusBadRequest, i18n.T(c.UserContext(), "invalid_id"))
	}
	metricID, err := uuid.Parse(c.Params("metricId"))
	if err != nil {
		return utils.ErrorResponse(c, fiber.StatusBadRequest, i18n.T(c.UserContext(), "invalid_id"))
	}

	db := h.db.WithContext(c.UserContext())

	var metric models.KpiMetric
	if err := db.Where("id = ? AND kpi_id = ? AND kpi_type = ?", metricID, kpiID, kpiType).First(&metric).Error; err != nil {
		return utils.ErrorResponse(c, fiber.StatusNotFound, i18n.T(c.UserContext(), "not_found"))
	}
	kpiCode, err := getKPICode(db, kpiType, kpiID)
	if err != nil || kpiCode == "" {
		return utils.ErrorResponse(c, fiber.StatusNotFound, i18n.T(c.UserContext(), "not_found"))
	}

	year, periodCode, isFallback := services.ResolveDisplayPeriod(db, &metric, kpiID, kpiCode, kpiType)

	resp := buildMetricPeriodRollupMap(db, &metric, kpiID, kpiCode, kpiType, year, periodCode, nil)
	resp["is_fallback_period"] = isFallback

	return utils.SuccessResponse(c, fiber.StatusOK, "", resp)
}

// GetMetricPeriodSeries returns one metric's Baseline/Target/Actual/
// Achievement across every period of a year in a single call — the doc's
// Figure 1 trend chart (section 5), letting the frontend render the whole
// year without one round trip per period.
func (h *KpiComposedHandler) GetMetricPeriodSeries(c *fiber.Ctx) error {
	kpiType := c.Params("type")
	if !isValidKpiType(kpiType) {
		return utils.ErrorResponse(c, fiber.StatusBadRequest, i18n.T(c.UserContext(), "invalid_id"))
	}
	kpiID, err := uuid.Parse(c.Params("id"))
	if err != nil {
		return utils.ErrorResponse(c, fiber.StatusBadRequest, i18n.T(c.UserContext(), "invalid_id"))
	}
	metricID, err := uuid.Parse(c.Params("metricId"))
	if err != nil {
		return utils.ErrorResponse(c, fiber.StatusBadRequest, i18n.T(c.UserContext(), "invalid_id"))
	}
	year := c.QueryInt("year", 0)
	if year == 0 {
		return utils.ErrorResponse(c, fiber.StatusBadRequest, "year is required")
	}
	var organizationID *uuid.UUID
	if orgStr := c.Query("organization_id"); orgStr != "" {
		oid, parseErr := uuid.Parse(orgStr)
		if parseErr != nil {
			return utils.ErrorResponse(c, fiber.StatusBadRequest, "invalid organization_id")
		}
		organizationID = &oid
	}

	db := h.db.WithContext(c.UserContext())

	var metric models.KpiMetric
	if err := db.Where("id = ? AND kpi_id = ? AND kpi_type = ?", metricID, kpiID, kpiType).First(&metric).Error; err != nil {
		return utils.ErrorResponse(c, fiber.StatusNotFound, i18n.T(c.UserContext(), "not_found"))
	}
	kpiCode, err := getKPICode(db, kpiType, kpiID)
	if err != nil || kpiCode == "" {
		return utils.ErrorResponse(c, fiber.StatusNotFound, i18n.T(c.UserContext(), "not_found"))
	}

	points := services.BuildMetricPeriodSeries(db, &metric, kpiID, kpiCode, kpiType, year, organizationID, 100)

	return utils.SuccessResponse(c, fiber.StatusOK, "", fiber.Map{
		"metric_id":   metric.ID,
		"metric_name": metric.Name,
		"year":        year,
		"points":      points,
	})
}

// GetCompositeScore returns the overall weighted KPI score for one period —
// doc section 8's "Composite KPI Calculation" table plus the Raw Overall KPI
// Score, with an optional capped display score (BR-11) alongside it.
func (h *KpiComposedHandler) GetCompositeScore(c *fiber.Ctx) error {
	kpiType := c.Params("type")
	if !isValidKpiType(kpiType) {
		return utils.ErrorResponse(c, fiber.StatusBadRequest, i18n.T(c.UserContext(), "invalid_id"))
	}
	kpiID, err := uuid.Parse(c.Params("id"))
	if err != nil {
		return utils.ErrorResponse(c, fiber.StatusBadRequest, i18n.T(c.UserContext(), "invalid_id"))
	}
	year, periodCode, organizationID, qErr := parseRollupQuery(c)
	if qErr != nil {
		return utils.ErrorResponse(c, fiber.StatusBadRequest, qErr.Error())
	}

	db := h.db.WithContext(c.UserContext())
	kpiCode, err := getKPICode(db, kpiType, kpiID)
	if err != nil || kpiCode == "" {
		return utils.ErrorResponse(c, fiber.StatusNotFound, i18n.T(c.UserContext(), "not_found"))
	}

	result, err := services.ComputeCompositeScore(db, kpiID, kpiCode, kpiType, year, periodCode, organizationID, 100)
	if err != nil {
		return utils.ErrorResponse(c, fiber.StatusInternalServerError, i18n.T(c.UserContext(), "failed_to_load_data"))
	}

	return utils.SuccessResponse(c, fiber.StatusOK, "", result)
}

// GetCompositeScoreLatest is what the KPI Overview page actually calls: each
// metric contributes its own most-recent period with data (same resolution
// as GetMetricDisplayRollup) instead of requiring every metric to already
// have data for one identical shared period. Two metrics on different
// cadences (one last updated in August, another in September) both show up
// in the list instead of the whole composite reading "No Data" purely
// because they don't line up on the same period.
func (h *KpiComposedHandler) GetCompositeScoreLatest(c *fiber.Ctx) error {
	kpiType := c.Params("type")
	if !isValidKpiType(kpiType) {
		return utils.ErrorResponse(c, fiber.StatusBadRequest, i18n.T(c.UserContext(), "invalid_id"))
	}
	kpiID, err := uuid.Parse(c.Params("id"))
	if err != nil {
		return utils.ErrorResponse(c, fiber.StatusBadRequest, i18n.T(c.UserContext(), "invalid_id"))
	}

	db := h.db.WithContext(c.UserContext())
	kpiCode, err := getKPICode(db, kpiType, kpiID)
	if err != nil || kpiCode == "" {
		return utils.ErrorResponse(c, fiber.StatusNotFound, i18n.T(c.UserContext(), "not_found"))
	}

	result, err := services.ComputeCompositeScoreLatest(db, kpiID, kpiCode, kpiType, 100)
	if err != nil {
		return utils.ErrorResponse(c, fiber.StatusInternalServerError, i18n.T(c.UserContext(), "failed_to_load_data"))
	}

	return utils.SuccessResponse(c, fiber.StatusOK, "", result)
}
