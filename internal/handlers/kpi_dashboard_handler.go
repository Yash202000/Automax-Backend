package handlers

import (
	"fmt"
	"strconv"
	"strings"

	"github.com/automax/backend/internal/models"
	"github.com/automax/backend/internal/services"
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
	QuarterlyTrend []TrendData `json:"quarterly_trend" gorm:"-"`
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
	// AvailableYears lists every reporting year that actually has KPI
	// Targets or Entries, so the Year filter offers real data years rather
	// than a hardcoded window around today.
	AvailableYears []int `json:"available_years"`
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

// periodMonthRangeSQL returns SQL expressions for the first and last
// calendar month (1-12) a KPI Target/Entry period covers, derived from its
// period_code — the bare labels the Target/Entry pickers send ("jan".."dec",
// "q1".."q4", "h1"/"h2", "annual"), with any legacy "YYYY-" prefix
// stripped so "2027-Q1"/"2027-02" resolve the same way. An explicit
// period_start/period_end (set on Targets, and on custom periods) wins over
// the code. Anything unrecognised ("annual", custom labels) spans the whole
// year.
func periodMonthRangeSQL(codeCol, startCol, endCol string) (string, string) {
	code := fmt.Sprintf("regexp_replace(lower(%s), '^[0-9]{4}-', '')", codeCol)
	var startCases, endCases []string
	for i, m := range services.PeriodCodesForFrequency(models.KPIFrequencyMonthly) {
		startCases = append(startCases, fmt.Sprintf("WHEN '%s' THEN %d WHEN '%02d' THEN %d", m, i+1, i+1, i+1))
		endCases = append(endCases, fmt.Sprintf("WHEN '%s' THEN %d WHEN '%02d' THEN %d", m, i+1, i+1, i+1))
	}
	for q := 1; q <= 4; q++ {
		startCases = append(startCases, fmt.Sprintf("WHEN 'q%d' THEN %d", q, 3*q-2))
		endCases = append(endCases, fmt.Sprintf("WHEN 'q%d' THEN %d", q, 3*q))
	}
	startCases = append(startCases, "WHEN 'h1' THEN 1 WHEN 'h2' THEN 7")
	endCases = append(endCases, "WHEN 'h1' THEN 6 WHEN 'h2' THEN 12")
	startExpr := fmt.Sprintf("COALESCE(EXTRACT(MONTH FROM %s)::int, CASE %s %s ELSE 1 END)", startCol, code, strings.Join(startCases, " "))
	endExpr := fmt.Sprintf("COALESCE(EXTRACT(MONTH FROM %s)::int, CASE %s %s ELSE 12 END)", endCol, code, strings.Join(endCases, " "))
	return startExpr, endExpr
}

// periodFilterSQL builds the Year/Quarter condition for a KPI Target/Entry
// table: the row's reporting year must equal year (when set), and its
// period's months must overlap the selected calendar quarter (when set) —
// Q1 = Jan-Mar ... Q4 = Oct-Dec. So a monthly "feb" row is Q1 only, a
// quarterly "q2" row is Q2 only, a semi-annual "h1" row is Q1 and Q2, and
// an annual row is in every quarter of its year. Returns ("", nil) when
// neither is set.
func periodFilterSQL(yearExpr, codeCol, startCol, endCol string, year, quarter int) (string, []interface{}) {
	var conditions []string
	var args []interface{}
	if year != 0 {
		conditions = append(conditions, yearExpr+" = ?")
		args = append(args, year)
	}
	if quarter >= 1 && quarter <= 4 {
		startExpr, endExpr := periodMonthRangeSQL(codeCol, startCol, endCol)
		conditions = append(conditions, fmt.Sprintf("%s <= ? AND %s >= ?", startExpr, endExpr))
		args = append(args, 3*quarter, 3*quarter-2)
	}
	return strings.Join(conditions, " AND "), args
}

// targetYearExpr is a KpiAnnualTarget's reporting year: target_year, with
// the older `year` column as a fallback for rows saved before target_year
// was populated.
const targetYearExpr = "COALESCE(NULLIF(target_year, 0), year)"

// kpiPeriodScopeSQL restricts a KPI dictionary table to KPIs that have a
// Target or an Entry in the selected Year/Quarter — the dictionary records
// carry no year of their own, so a KPI "belongs" to a year/quarter through
// the periods it is actually planned (kpi_annual_targets, matched by
// kpi_code) or measured (kpi_entries, matched by kpi_id) in. Returns
// ("", nil) when neither filter is set.
func kpiPeriodScopeSQL(table, typeLiteral string, year, quarter int) (string, []interface{}) {
	entrySQL, entryArgs := periodFilterSQL("reporting_year", "period_code", "period_start", "period_end", year, quarter)
	if entrySQL == "" {
		return "", nil
	}
	targetSQL, targetArgs := periodFilterSQL(targetYearExpr, "period_code", "period_start", "period_end", year, quarter)
	sql := fmt.Sprintf(
		"(%[1]s.id IN (SELECT kpi_id FROM kpi_entries WHERE deleted_at IS NULL AND kpi_type = '%[2]s' AND %[3]s)"+
			" OR %[1]s.code IN (SELECT kpi_code FROM kpi_annual_targets WHERE deleted_at IS NULL AND kpi_type = '%[2]s' AND %[4]s))",
		table, typeLiteral, entrySQL, targetSQL,
	)
	return sql, append(entryArgs, targetArgs...)
}

// kpiDictionaryUnionSQL flattens the three KPI dictionary tables into one
// derived table (id, code, kpi_type + taxonomy columns) so kpi_entries —
// which only carries kpi_id + kpi_type — can be joined back to its KPI's
// code and taxonomy in a single query. Entries whose KPI no longer exists
// drop out of the join.
const kpiDictionaryUnionSQL = `(
	SELECT id, code, 'strategic' AS kpi_type, operational_objective_id, process_id, award_sub_criterion_id FROM strategic_kpis WHERE deleted_at IS NULL
	UNION ALL
	SELECT id, code, 'operational', operational_objective_id, process_id, award_sub_criterion_id FROM operational_kpis WHERE deleted_at IS NULL
	UNION ALL
	SELECT id, code, 'award', operational_objective_id, process_id, award_sub_criterion_id FROM award_kpis WHERE deleted_at IS NULL
) d`

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
	if v, err := strconv.Atoi(c.Query("quarter")); err == nil && v >= 1 && v <= 4 {
		quarter = v
	}

	// dictQuery scopes a KPI dictionary table (Strategic/Operational/Award)
	// query to the selected Objective/Criteria/Sub-Criteria filters, using
	// its own direct taxonomy columns, and to the selected Year/Quarter via
	// the KPI's own Targets/Entries (see kpiPeriodScopeSQL).
	dictQuery := func(model interface{}, table, typeLiteral string) *gorm.DB {
		q := h.db.WithContext(c.UserContext()).Model(model)
		if sql, args := dictionaryTaxonomyWhere(objectiveIDs, criteriaIDs, subCriteriaIDs); sql != "" {
			q = q.Where(sql, args...)
		}
		if sql, args := kpiPeriodScopeSQL(table, typeLiteral, year, quarter); sql != "" {
			q = q.Where(sql, args...)
		}
		return q
	}
	strategicQuery := func() *gorm.DB { return dictQuery(&models.StrategicKPI{}, "strategic_kpis", "strategic") }
	operationalQuery := func() *gorm.DB { return dictQuery(&models.OperationalKPI{}, "operational_kpis", "operational") }
	awardQuery := func() *gorm.DB { return dictQuery(&models.AwardKPI{}, "award_kpis", "award") }

	// The Total Strategic/Operational/Award cards, and the Strategic-only
	// status/goal breakdowns below, are each scoped to one KPI type — when
	// kpiType filters to a different type, that section has nothing in
	// scope and is left at its zero value rather than showing unfiltered
	// counts.
	if kpiType == "" || kpiType == "strategic" {
		strategicQuery().Count(&data.TotalStrategic)
	}
	if kpiType == "" || kpiType == "operational" {
		operationalQuery().Count(&data.TotalOperational)
	}
	if kpiType == "" || kpiType == "award" {
		awardQuery().Count(&data.TotalAward)
	}

	if kpiType == "" || kpiType == "strategic" {
		strategicQuery().
			Select("g.title as goal, count(*) as count").
			Joins("left join goals g on g.id = strategic_kpis.goal_id").
			Group("g.title").Scan(&data.KpisByGoal)
	}

	// Active KPIs by Type: for the donut chart, each KPI type's count of
	// currently-active KPIs (activation_status = 'active'), across all
	// three dictionary tables — not just Strategic. Respects the same
	// kpiType gating and Objective/Criteria/Sub-Criteria/Year/Quarter
	// filters as the Total cards above, so a type filter narrows this to a
	// single slice rather than showing every type unfiltered.
	var activeByType []TypeCount
	if kpiType == "" || kpiType == "strategic" {
		var count int64
		strategicQuery().Where("activation_status = ?", "active").Count(&count)
		activeByType = append(activeByType, TypeCount{Type: "strategic", Count: count})
	}
	if kpiType == "" || kpiType == "operational" {
		var count int64
		operationalQuery().Where("activation_status = ?", "active").Count(&count)
		activeByType = append(activeByType, TypeCount{Type: "operational", Count: count})
	}
	if kpiType == "" || kpiType == "award" {
		var count int64
		awardQuery().Where("activation_status = ?", "active").Count(&count)
		activeByType = append(activeByType, TypeCount{Type: "award", Count: count})
	}
	data.ActiveKpisByType = activeByType

	// entryQuery scopes kpi_entries — the actual per-period KPI results —
	// joined back to their KPI's dictionary record (for code + taxonomy), to
	// the selected Type/Year/Quarter/Objective/Criteria/Sub-Criteria filters.
	// Year/Quarter match the entry's own reporting_year + period_code via
	// the same month-overlap rule as the dictionary scoping above.
	entryQuery := func(status string) *gorm.DB {
		q := h.db.WithContext(c.UserContext()).Table("kpi_entries e").
			Joins("JOIN "+kpiDictionaryUnionSQL+" ON d.id = e.kpi_id AND d.kpi_type = e.kpi_type").
			Where("e.deleted_at IS NULL AND e.status = ?", status)
		if kpiType != "" {
			q = q.Where("e.kpi_type = ?", kpiType)
		}
		if sql, args := periodFilterSQL("e.reporting_year", "e.period_code", "e.period_start", "e.period_end", year, quarter); sql != "" {
			q = q.Where(sql, args...)
		}
		if sql, args := dictionaryTaxonomyWhere(objectiveIDs, criteriaIDs, subCriteriaIDs); sql != "" {
			q = q.Where(sql, args...)
		}
		return q
	}
	approvedEntries := func() *gorm.DB {
		return entryQuery(models.KpiEntryStatusApproved).Where("e.achievement_percentage IS NOT NULL")
	}

	entryQuery(models.KpiEntryStatusSubmitted).Count(&data.PendingReviews)

	// Performance trend buckets each approved entry by the calendar quarter
	// its period ends in (a "feb" entry is Q1, "h1" is Q2, "annual" is Q4).
	// When a Quarter filter is active every in-scope entry overlaps that
	// quarter, so they are all bucketed under it instead.
	_, endMonth := periodMonthRangeSQL("e.period_code", "e.period_start", "e.period_end")
	quarterExpr := fmt.Sprintf("((%s - 1) / 3 + 1)", endMonth)
	if quarter != 0 {
		quarterExpr = strconv.Itoa(quarter)
	}
	approvedEntries().
		Select("e.reporting_year as year, " + quarterExpr + " as quarter, AVG(e.achievement_percentage) as avg_achievement, count(DISTINCT e.kpi_id) as kpi_count").
		Group("1, 2").
		Order("1 DESC, 2 DESC").
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

	cardQuery := func(q *gorm.DB, typeLiteral string) []KpiCardDef {
		var cards []KpiCardDef
		q.Select("code, '" + typeLiteral + "' as type, name_en, name_ar, formula, baseline, unit_of_measure, polarity, reporting_frequency as reporting_freq, data_source, activation_status").
			Limit(10).
			Order("created_at DESC").
			Scan(&cards)
		return cards
	}

	var recentCards []KpiCardDef
	switch kpiType {
	case "operational":
		recentCards = cardQuery(operationalQuery(), "operational")
	case "award":
		recentCards = cardQuery(awardQuery(), "award")
	default:
		recentCards = cardQuery(strategicQuery(), "strategic")
	}
	data.RecentKpiCards = recentCards

	perfSummarySelect := "d.code as kpi_code, " +
		"SUM(COALESCE(e.target_value_snapshot, 0)) as total_target, " +
		"SUM(COALESCE(e.actual_value, 0)) as total_actual, " +
		"AVG(e.achievement_percentage) as avg_achievement, " +
		"MAX(e.updated_at) as last_updated"
	var topPerfs, lowPerfs []KpiPerformanceSummary
	approvedEntries().
		Select(perfSummarySelect).
		Group("d.code").
		Having("AVG(e.achievement_percentage) >= ?", 80).
		Order("avg_achievement DESC").
		Limit(5).
		Scan(&topPerfs)

	approvedEntries().
		Select(perfSummarySelect).
		Group("d.code").
		Having("AVG(e.achievement_percentage) < ?", 80).
		Order("avg_achievement ASC").
		Limit(5).
		Scan(&lowPerfs)

	data.TopPerformers = topPerfs
	data.LowPerformers = lowPerfs

	// Only years belonging to KPIs that still exist — entries/targets left
	// behind by a deleted KPI would otherwise offer a year that filters to
	// nothing.
	h.db.WithContext(c.UserContext()).Raw(
		"SELECT DISTINCT y FROM (" +
			"SELECT e.reporting_year AS y FROM kpi_entries e JOIN " + kpiDictionaryUnionSQL +
			" ON d.id = e.kpi_id AND d.kpi_type = e.kpi_type WHERE e.deleted_at IS NULL" +
			" UNION SELECT " + targetYearExpr + " FROM kpi_annual_targets t JOIN " + kpiDictionaryUnionSQL +
			" ON d.code = t.kpi_code AND d.kpi_type = t.kpi_type WHERE t.deleted_at IS NULL" +
			") years WHERE y > 0 ORDER BY y",
	).Scan(&data.AvailableYears)

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
