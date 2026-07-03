package handlers

import (
	"strconv"

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

type StatusCount struct {
	Status string `json:"status"`
	Count  int64  `json:"count"`
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
	DimensionName string  `json:"dimension_name"`
	SegmentName   string  `json:"segment_name"`
	AvgAchievement float64 `json:"avg_achievement"`
	AvgPct        float64 `json:"avg_pct"`
}

type TrendData struct {
	Period string  `json:"period"`
	Value  float64 `json:"value"`
}

type KpiPerformanceSummary struct {
	KpiCode         string       `json:"kpi_code"`
	TotalTarget     float64      `json:"total_target"`
	TotalActual     float64      `json:"total_actual"`
	AvgAchievement  float64      `json:"avg_achievement"`
	LastUpdated     string       `json:"last_updated"`
	QuarterlyTrend  []TrendData  `json:"quarterly_trend"`
}

type EnhancedKpiDashboardData struct {
	TotalStrategic        int64               `json:"total_strategic"`
	TotalOperational      int64               `json:"total_operational"`
	TotalAward            int64               `json:"total_award"`
	PendingReviews        int64               `json:"pending_reviews"`
	KpisByStatus          []StatusCount       `json:"kpis_by_status"`
	KpisByGoal            []GoalCount         `json:"kpis_by_goal"`
	PerformanceTrends     []PerformanceTrend  `json:"performance_trends"`
	BenchmarkSummaries    []BenchmarkSummary  `json:"benchmark_summaries"`
	SegmentationSummaries []SegSummary        `json:"segmentation_summaries"`
	RecentKpiCards        []KpiCardDef        `json:"recent_kpi_cards"`
	TopPerformers         []KpiPerformanceSummary `json:"top_performers"`
	LowPerformers         []KpiPerformanceSummary `json:"low_performers"`
}

func (h *KpiDashboardHandler) GetDashboard(c *fiber.Ctx) error {
	var data EnhancedKpiDashboardData

	h.db.WithContext(c.UserContext()).Model(&models.StrategicKPI{}).Count(&data.TotalStrategic)
	h.db.WithContext(c.UserContext()).Model(&models.OperationalKPI{}).Count(&data.TotalOperational)
	h.db.WithContext(c.UserContext()).Model(&models.AwardKPI{}).Count(&data.TotalAward)
	h.db.WithContext(c.UserContext()).Model(&models.KpiPerformance{}).Where("status = ?", "draft").Count(&data.PendingReviews)

	h.db.WithContext(c.UserContext()).Model(&models.StrategicKPI{}).
		Select("activation_status as status, count(*) as count").
		Group("activation_status").Scan(&data.KpisByStatus)

	h.db.WithContext(c.UserContext()).Model(&models.StrategicKPI{}).
		Select("sg.name_en as goal, count(*) as count").
		Joins("left join strategic_goals sg on sg.id = strategic_kpis.strategic_goal_id").
		Group("sg.name_en").Scan(&data.KpisByGoal)

	h.db.WithContext(c.UserContext()).Model(&models.KpiPerformance{}).
		Select("year, quarter, AVG(achievement_pct) as avg_achievement, count(*) as kpi_count").
		Where("status = ?", "published").
		Group("year, quarter").
		Order("year DESC, quarter DESC").
		Limit(8).
		Scan(&data.PerformanceTrends)

	h.db.WithContext(c.UserContext()).Model(&models.KpiBenchmark{}).
		Select("kpi_code, zone, benchmark_entity, "+
			"AVG(internal_achievement) as avg_internal, "+
			"AVG(benchmark_achievement) as avg_benchmark, "+
			"AVG(internal_achievement - benchmark_achievement) as avg_variance").
		Group("kpi_code, zone, benchmark_entity").
		Order("avg_variance DESC").
		Limit(10).
		Scan(&data.BenchmarkSummaries)

	h.db.WithContext(c.UserContext()).Model(&models.KpiSegmentation{}).
		Select("dimension_name, segment_name, "+
			"AVG(achievement) as avg_achievement, "+
			"CASE WHEN AVG(target) > 0 THEN (AVG(achievement) / AVG(target)) * 100 ELSE 0 END as avg_pct").
		Group("dimension_name, segment_name").
		Order("avg_pct DESC").
		Limit(10).
		Scan(&data.SegmentationSummaries)

	var strategicCards []KpiCardDef
	h.db.WithContext(c.UserContext()).Model(&models.StrategicKPI{}).
		Select("code, 'strategic' as type, name_en, name_ar, formula, baseline, polarity, reporting_frequency as reporting_freq, data_source, activation_status").
		Limit(10).
		Order("created_at DESC").
		Scan(&strategicCards)
	data.RecentKpiCards = strategicCards

	var topPerfs, lowPerfs []KpiPerformanceSummary
	h.db.WithContext(c.UserContext()).Model(&models.KpiPerformance{}).
		Select("kpi_code, SUM(target) as total_target, SUM(actual) as total_actual, AVG(achievement_pct) as avg_achievement, MAX(updated_at) as last_updated").
		Where("status = ?", "published").
		Group("kpi_code").
		Having("AVG(achievement_pct) >= ?", 80).
		Order("avg_achievement DESC").
		Limit(5).
		Scan(&topPerfs)

	h.db.WithContext(c.UserContext()).Model(&models.KpiPerformance{}).
		Select("kpi_code, SUM(target) as total_target, SUM(actual) as total_actual, AVG(achievement_pct) as avg_achievement, MAX(updated_at) as last_updated").
		Where("status = ?", "published").
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
		Select("code, 'strategic' as type, name_en, name_ar, formula, baseline, polarity, reporting_frequency as reporting_freq, data_source, activation_status")

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
			Select("code, 'operational' as type, name_en, name_ar, formula, baseline, polarity, reporting_frequency as reporting_freq, data_source, activation_status")
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
			Select("code, 'award' as type, name_en, name_ar, formula, baseline, polarity, reporting_frequency as reporting_freq, data_source, activation_status")
		if search != "" {
			q3 = q3.Where("(name_en ILIKE ? OR name_ar ILIKE ? OR code ILIKE ?)", "%"+search+"%", "%"+search+"%", "%"+search+"%")
		}
		if err := q3.Order("code ASC").Find(&awardCards).Error; err != nil {
			return utils.ErrorResponse(c, fiber.StatusInternalServerError, "Failed to load KPI card definitions")
		}
	}

	allCards := append(append(strategicCards, operationalCards...), awardCards...)
	return utils.SuccessResponse(c, fiber.StatusOK, "", allCards)
}
