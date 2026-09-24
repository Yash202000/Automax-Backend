package handlers

import (
	"fmt"
	"strconv"
	"strings"

	"github.com/automax/backend/internal/models"
	"github.com/automax/backend/pkg/utils"
	"github.com/gofiber/fiber/v2"
	"gorm.io/gorm"
)

type KpiDashboardHandler struct {
	db *gorm.DB
}

func NewKpiDashboardHandler(db *gorm.DB) *KpiDashboardHandler {
	return &KpiDashboardHandler{db: db}
}

type TypeCount struct {
	Type  string `json:"type"`
	Count int64  `json:"count"`
}

type GoalCount struct {
	Goal  string `json:"goal"`
	Count int64  `json:"count"`
}

type PerformanceTrend struct {
	Year           int     `json:"year"`
	Quarter        int     `json:"quarter"`
	AvgAchievement float64 `json:"avg_achievement"`
	KpiCount       int64   `json:"kpi_count"`
}

type KpiCardDef struct {
	Code             string  `json:"code"`
	Type             string  `json:"type"`
	NameEn           string  `json:"name_en"`
	NameAr           string  `json:"name_ar"`
	Formula          string  `json:"formula"`
	Baseline         float64 `json:"baseline"`
	UnitOfMeasure    string  `json:"unit_of_measure"`
	Polarity         string  `json:"polarity"`
	ReportingFreq    string  `json:"reporting_frequency"`
	DataSource       string  `json:"data_source"`
	StrategicGoal    string  `json:"strategic_goal,omitempty"`
	OwnerDept        string  `json:"owner_dept,omitempty"`
	ActivationStatus string  `json:"activation_status"`
}

type BenchmarkSummary struct {
	KpiCode         string  `json:"kpi_code"`
	Zone            string  `json:"zone"`
	BenchmarkEntity string  `json:"benchmark_entity"`
	AvgInternal     float64 `json:"avg_internal"`
	AvgBenchmark    float64 `json:"avg_benchmark"`
	AvgVariance     float64 `json:"avg_variance"`
}

type SegSummary struct {
	DimensionName  string  `json:"dimension_name"`
	SegmentName    string  `json:"segment_name"`
	AvgAchievement float64 `json:"avg_achievement"`
	AvgPct         float64 `json:"avg_pct"`
}

type TrendData struct {
	Period string  `json:"period"`
	Value  float64 `json:"value"`
}

type KpiPerformanceSummary struct {
	KpiCode        string      `json:"kpi_code"`
	TotalTarget    float64     `json:"total_target"`
	TotalActual    float64     `json:"total_actual"`
	AvgAchievement float64     `json:"avg_achievement"`
	LastUpdated    string      `json:"last_updated"`
	QuarterlyTrend []TrendData `json:"quarterly_trend"`
}

type EnhancedKpiDashboardData struct {
	TotalStrategic        int64                   `json:"total_strategic"`
	TotalOperational      int64                   `json:"total_operational"`
	TotalAward            int64                   `json:"total_award"`
	PendingReviews        int64                   `json:"pending_reviews"`
	ActiveKpisByType      []TypeCount             `json:"active_kpis_by_type"`
	KpisByGoal            []GoalCount             `json:"kpis_by_goal"`
	PerformanceTrends     []PerformanceTrend      `json:"performance_trends"`
	BenchmarkSummaries    []BenchmarkSummary      `json:"benchmark_summaries"`
	SegmentationSummaries []SegSummary            `json:"segmentation_summaries"`
	RecentKpiCards        []KpiCardDef            `json:"recent_kpi_cards"`
	TopPerformers         []KpiPerformanceSummary `json:"top_performers"`
	LowPerformers         []KpiPerformanceSummary `json:"low_performers"`
}

// taxonomyFilterSQL builds a WHERE-clause fragment (with its args, in order)
// that restricts a kpi_performances query to rows whose underlying KPI
// dictionary record matches the given Objective/Criteria/Sub-Criteria
// filters. Returns ("", nil) when all three are empty — callers should only
// apply the fragment when it's non-empty, exactly like the existing
// kpi_type/year/quarter conditions in perfQuery().
//
// kpi_performances only carries kpi_code + kpi_type (no FK to the dictionary
// tables), so each condition is wrapped per type and reaches back into
// strategic_kpis/operational_kpis/award_kpis via kpi_code — all three tables
// share the same taxonomy columns (operational_objective_id, process_id,
// award_sub_criterion_id), so the inner condition is identical per branch.
//
// objectiveIDs, criteriaIDs, and subCriteriaIDs each support multiple
// values, combined with OR within the same filter (any of several selected
// ids matches) and AND across the three filters (when more than one is set,
// a row must match at least one selected value in *each* of them — an
// inconsistent combination, e.g. a Sub-Criteria that doesn't belong to any
// selected Criteria, simply yields no rows, which is correct AND-semantics).
//
// Each objective id matches either a parent Objective (broad — every KPI
// under any of its child Processes) or one specific child Process (exact),
// using the same id list for both columns: the two id spaces never collide,
// so a given id only ever matches its own branch.
func taxonomyFilterSQL(objectiveIDs, criteriaIDs, subCriteriaIDs []string) (string, []interface{}) {
	if len(objectiveIDs) == 0 && len(criteriaIDs) == 0 && len(subCriteriaIDs) == 0 {
		return "", nil
	}

	var conditions []string
	var innerArgs []interface{}
	if len(objectiveIDs) > 0 {
		// Matches a row tagged directly with a selected parent Objective, one
		// tagged with a selected child Process, OR one tagged with ANY
		// Process whose own parent is a selected Objective — this last leg
		// makes a parent selection broadly match every KPI under any of its
		// children even when a row's operational_objective_id wasn't kept in
		// sync with its process_id (e.g. older data seeded before that
		// derivation existed).
		conditions = append(conditions, "(operational_objective_id IN (?) OR process_id IN (?) OR process_id IN (SELECT id FROM processes WHERE operational_objective_id IN (?)))")
		innerArgs = append(innerArgs, objectiveIDs, objectiveIDs, objectiveIDs)
	}
	if len(criteriaIDs) > 0 {
		conditions = append(conditions, "award_sub_criterion_id IN (SELECT id FROM award_sub_criterions WHERE award_criterion_id IN (?))")
		innerArgs = append(innerArgs, criteriaIDs)
	}
	if len(subCriteriaIDs) > 0 {
		conditions = append(conditions, "award_sub_criterion_id IN (?)")
		innerArgs = append(innerArgs, subCriteriaIDs)
	}
	innerWhere := strings.Join(conditions, " AND ")

	tables := []struct{ name, typeLiteral string }{
		{"strategic_kpis", "strategic"},
		{"operational_kpis", "operational"},
		{"award_kpis", "award"},
	}
	var branches []string
	var args []interface{}
	for _, t := range tables {
		branches = append(branches, fmt.Sprintf(
			"(kpi_type = '%s' AND kpi_code IN (SELECT code FROM %s WHERE %s))",
			t.typeLiteral, t.name, innerWhere,
		))
		args = append(args, innerArgs...)
	}
	return "(" + strings.Join(branches, " OR ") + ")", args
}

// dictionaryTaxonomyWhere builds the same Objective/Criteria/Sub-Criteria
// condition as taxonomyFilterSQL's inner per-table clause, but for direct
// use against a KPI dictionary table (strategic_kpis/operational_kpis/
// award_kpis) — these carry operational_objective_id/process_id/
// award_sub_criterion_id columns directly, so no kpi_code/kpi_type
// reach-back is needed. Returns ("", nil) when all three are empty.
func dictionaryTaxonomyWhere(objectiveIDs, criteriaIDs, subCriteriaIDs []string) (string, []interface{}) {
	if len(objectiveIDs) == 0 && len(criteriaIDs) == 0 && len(subCriteriaIDs) == 0 {
		return "", nil
	}
	var conditions []string
	var args []interface{}
	if len(objectiveIDs) > 0 {
		// See the matching comment in taxonomyFilterSQL — the third leg makes
		// a parent Objective selection broadly match every KPI under any of
		// its children even when a row's own operational_objective_id wasn't
		// kept in sync with its process_id.
		conditions = append(conditions, "(operational_objective_id IN (?) OR process_id IN (?) OR process_id IN (SELECT id FROM processes WHERE operational_objective_id IN (?)))")
		args = append(args, objectiveIDs, objectiveIDs, objectiveIDs)
	}
	if len(criteriaIDs) > 0 {
		conditions = append(conditions, "award_sub_criterion_id IN (SELECT id FROM award_sub_criterions WHERE award_criterion_id IN (?))")
		args = append(args, criteriaIDs)
	}
	if len(subCriteriaIDs) > 0 {
		conditions = append(conditions, "award_sub_criterion_id IN (?)")
		args = append(args, subCriteriaIDs)
	}
	return strings.Join(conditions, " AND "), args
}

// splitFilterIDs parses a comma-separated multi-value filter query param
// (e.g. "criteria_id=a,b,c") into a clean slice, dropping empty entries so a
// trailing/stray comma or an empty param never turns into a spurious "" id.
func splitFilterIDs(raw string) []string {
	if raw == "" {
		return nil
	}
	var ids []string
	for _, id := range strings.Split(raw, ",") {
		if id = strings.TrimSpace(id); id != "" {
			ids = append(ids, id)
		}
	}
	return ids
}

func (h *KpiDashboardHandler) GetDashboard(c *fiber.Ctx) error {
	var data EnhancedKpiDashboardData

	kpiType := c.Query("kpi_type") // strategic|operational|award — empty means all types
	objectiveIDs := splitFilterIDs(c.Query("objective_id"))
	criteriaIDs := splitFilterIDs(c.Query("criteria_id"))
	subCriteriaIDs := splitFilterIDs(c.Query("sub_criteria_id"))
	var year, quarter int
	if v, err := strconv.Atoi(c.Query("year")); err == nil {
		year = v
	}
	if v, err := strconv.Atoi(c.Query("quarter")); err == nil {
		quarter = v
	}

	// dictQuery scopes a KPI dictionary table (Strategic/Operational/Award)
	// query to the selected Objective/Criteria/Sub-Criteria filters, using
	// its own direct taxonomy columns.
	dictQuery := func(model interface{}) *gorm.DB {
		q := h.db.WithContext(c.UserContext()).Model(model)
		if sql, args := dictionaryTaxonomyWhere(objectiveIDs, criteriaIDs, subCriteriaIDs); sql != "" {
			q = q.Where(sql, args...)
		}
		return q
	}

	// The Total Strategic/Operational/Award cards, and the Strategic-only
	// status/goal breakdowns below, are each scoped to one KPI type — when
	// kpiType filters to a different type, that section has nothing in
	// scope and is left at its zero value rather than showing unfiltered
	// counts.
	if kpiType == "" || kpiType == "strategic" {
		dictQuery(&models.StrategicKPI{}).Count(&data.TotalStrategic)
	}
	if kpiType == "" || kpiType == "operational" {
		dictQuery(&models.OperationalKPI{}).Count(&data.TotalOperational)
	}
	if kpiType == "" || kpiType == "award" {
		dictQuery(&models.AwardKPI{}).Count(&data.TotalAward)
	}

	if kpiType == "" || kpiType == "strategic" {
		dictQuery(&models.StrategicKPI{}).
			Select("g.title as goal, count(*) as count").
			Joins("left join goals g on g.id = strategic_kpis.goal_id").
			Group("g.title").Scan(&data.KpisByGoal)
	}

	// Active KPIs by Type: for the donut chart, each KPI type's count of
	// currently-active KPIs (activation_status = 'active'), across all
	// three dictionary tables — not just Strategic. Respects the same
	// kpiType gating and Objective/Criteria/Sub-Criteria taxonomy filters
	// as the Total cards above, so a type filter narrows this to a single
	// slice rather than showing every type unfiltered.
	var activeByType []TypeCount
	if kpiType == "" || kpiType == "strategic" {
		var count int64
		dictQuery(&models.StrategicKPI{}).Where("activation_status = ?", "active").Count(&count)
		activeByType = append(activeByType, TypeCount{Type: "strategic", Count: count})
	}
	if kpiType == "" || kpiType == "operational" {
		var count int64
		dictQuery(&models.OperationalKPI{}).Where("activation_status = ?", "active").Count(&count)
		activeByType = append(activeByType, TypeCount{Type: "operational", Count: count})
	}
	if kpiType == "" || kpiType == "award" {
		var count int64
		dictQuery(&models.AwardKPI{}).Where("activation_status = ?", "active").Count(&count)
		activeByType = append(activeByType, TypeCount{Type: "award", Count: count})
	}
	data.ActiveKpisByType = activeByType

	// perfQuery/statusQuery scope kpi_performances (and, via the same
	// kpi_type+kpi_code columns, KpiBenchmark/KpiSegmentation) to the
	// selected Type/Year/Quarter/Objective/Criteria/Sub-Criteria filters.
	statusQuery := func(status string) *gorm.DB {
		q := h.db.WithContext(c.UserContext()).Model(&models.KpiPerformance{}).Where("status = ?", status)
		if kpiType != "" {
			q = q.Where("kpi_type = ?", kpiType)
		}
		if year != 0 {
			q = q.Where("year = ?", year)
		}
		if quarter != 0 {
			q = q.Where("quarter = ?", quarter)
		}
		if sql, args := taxonomyFilterSQL(objectiveIDs, criteriaIDs, subCriteriaIDs); sql != "" {
			q = q.Where(sql, args...)
		}
		return q
	}
	perfQuery := func() *gorm.DB { return statusQuery("published") }

	statusQuery("draft").Count(&data.PendingReviews)

	perfQuery().
		Select("year, quarter, AVG(achievement_pct) as avg_achievement, count(*) as kpi_count").
		Group("year, quarter").
		Order("year DESC, quarter DESC").
		Limit(8).
		Scan(&data.PerformanceTrends)

	periodicQuery := func(model interface{}) *gorm.DB {
		q := h.db.WithContext(c.UserContext()).Model(model)
		if kpiType != "" {
			q = q.Where("kpi_type = ?", kpiType)
		}
		if year != 0 {
			q = q.Where("year = ?", year)
		}
		if quarter != 0 {
			q = q.Where("quarter = ?", quarter)
		}
		if sql, args := taxonomyFilterSQL(objectiveIDs, criteriaIDs, subCriteriaIDs); sql != "" {
			q = q.Where(sql, args...)
		}
		return q
	}

	periodicQuery(&models.KpiBenchmark{}).
		Select("kpi_code, zone, benchmark_entity, " +
			"AVG(internal_achievement) as avg_internal, " +
			"AVG(benchmark_achievement) as avg_benchmark, " +
			"AVG(internal_achievement - benchmark_achievement) as avg_variance").
		Group("kpi_code, zone, benchmark_entity").
		Order("avg_variance DESC").
		Limit(10).
		Scan(&data.BenchmarkSummaries)

	periodicQuery(&models.KpiSegmentation{}).
		Select("dimension_name, segment_name, " +
			"AVG(achievement) as avg_achievement, " +
			"CASE WHEN AVG(target) > 0 THEN (AVG(achievement) / AVG(target)) * 100 ELSE 0 END as avg_pct").
		Group("dimension_name, segment_name").
		Order("avg_pct DESC").
		Limit(10).
		Scan(&data.SegmentationSummaries)

	cardQuery := func(model interface{}, typeLiteral string) []KpiCardDef {
		var cards []KpiCardDef
		dictQuery(model).
			Select("code, '" + typeLiteral + "' as type, name_en, name_ar, formula, baseline, unit_of_measure, polarity, reporting_frequency as reporting_freq, data_source, activation_status").
			Limit(10).
			Order("created_at DESC").
			Scan(&cards)
		return cards
	}

	var recentCards []KpiCardDef
	switch kpiType {
	case "operational":
		recentCards = cardQuery(&models.OperationalKPI{}, "operational")
	case "award":
		recentCards = cardQuery(&models.AwardKPI{}, "award")
	default:
		recentCards = cardQuery(&models.StrategicKPI{}, "strategic")
	}
	data.RecentKpiCards = recentCards

	var topPerfs, lowPerfs []KpiPerformanceSummary
	perfQuery().
		Select("kpi_code, SUM(target) as total_target, SUM(actual) as total_actual, AVG(achievement_pct) as avg_achievement, MAX(updated_at) as last_updated").
		Group("kpi_code").
		Having("AVG(achievement_pct) >= ?", 80).
		Order("avg_achievement DESC").
		Limit(5).
		Scan(&topPerfs)

	perfQuery().
		Select("kpi_code, SUM(target) as total_target, SUM(actual) as total_actual, AVG(achievement_pct) as avg_achievement, MAX(updated_at) as last_updated").
		Group("kpi_code").
		Having("AVG(achievement_pct) < ?", 80).
		Order("avg_achievement ASC").
		Limit(5).
		Scan(&lowPerfs)

	data.TopPerformers = topPerfs
	data.LowPerformers = lowPerfs

	return utils.SuccessResponse(c, fiber.StatusOK, "", data)
}

func (h *KpiDashboardHandler) GetDashboardTrends(c *fiber.Ctx) error {
	kpiCode := c.Query("kpi_code")
	yearStr := c.Query("year")

	q := h.db.WithContext(c.UserContext()).Model(&models.KpiPerformance{}).
		Select("year, quarter, AVG(achievement_pct) as avg_achievement, count(*) as kpi_count")

	if kpiCode != "" {
		q = q.Where("kpi_code = ?", kpiCode)
	}
	if yearStr != "" {
		if year, err := strconv.Atoi(yearStr); err == nil {
			q = q.Where("year = ?", year)
		}
	}

	var trends []PerformanceTrend
	if err := q.Where("status = ?", "published").
		Group("year, quarter").
		Order("year ASC, quarter ASC").
		Scan(&trends).Error; err != nil {
		return utils.ErrorResponse(c, fiber.StatusInternalServerError, "Failed to load trends")
	}

	return utils.SuccessResponse(c, fiber.StatusOK, "", trends)
}

func (h *KpiDashboardHandler) GetKpiCardDefinitions(c *fiber.Ctx) error {
	kpiType := c.Query("type")
	search := c.Query("search")

	var strategicCards []KpiCardDef
	q := h.db.WithContext(c.UserContext()).Model(&models.StrategicKPI{}).
		Select("code, 'strategic' as type, name_en, name_ar, formula, baseline, unit_of_measure, polarity, reporting_frequency as reporting_freq, data_source, activation_status")

	if search != "" {
		q = q.Where("(name_en ILIKE ? OR name_ar ILIKE ? OR code ILIKE ?)", "%"+search+"%", "%"+search+"%", "%"+search+"%")
	}
	if kpiType == "strategic" || kpiType == "" {
		if err := q.Order("code ASC").Find(&strategicCards).Error; err != nil {
			return utils.ErrorResponse(c, fiber.StatusInternalServerError, "Failed to load KPI card definitions")
		}
	}

	var operationalCards []KpiCardDef
	if kpiType == "operational" || kpiType == "" {
		q2 := h.db.WithContext(c.UserContext()).Model(&models.OperationalKPI{}).
			Select("code, 'operational' as type, name_en, name_ar, formula, baseline, unit_of_measure, polarity, reporting_frequency as reporting_freq, data_source, activation_status")
		if search != "" {
			q2 = q2.Where("(name_en ILIKE ? OR name_ar ILIKE ? OR code ILIKE ?)", "%"+search+"%", "%"+search+"%", "%"+search+"%")
		}
		if err := q2.Order("code ASC").Find(&operationalCards).Error; err != nil {
			return utils.ErrorResponse(c, fiber.StatusInternalServerError, "Failed to load KPI card definitions")
		}
	}

	var awardCards []KpiCardDef
	if kpiType == "award" || kpiType == "" {
		q3 := h.db.WithContext(c.UserContext()).Model(&models.AwardKPI{}).
			Select("code, 'award' as type, name_en, name_ar, formula, baseline, unit_of_measure, polarity, reporting_frequency as reporting_freq, data_source, activation_status")
		if search != "" {
			q3 = q3.Where("(name_en ILIKE ? OR name_ar ILIKE ? OR code ILIKE ?)", "%"+search+"%", "%"+search+"%", "%"+search+"%")
		}
		if err := q3.Order("code ASC").Find(&awardCards).Error; err != nil {
			return utils.ErrorResponse(c, fiber.StatusInternalServerError, "Failed to load KPI card definitions")
		}
	}

	allCards := make([]KpiCardDef, 0, len(strategicCards)+len(operationalCards)+len(awardCards))
	allCards = append(allCards, strategicCards...)
	allCards = append(allCards, operationalCards...)
	allCards = append(allCards, awardCards...)
	return utils.SuccessResponse(c, fiber.StatusOK, "", allCards)
}
