package handlers

import (
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

type KpiDashboardData struct {
	TotalStrategic   int64        `json:"total_strategic"`
	TotalOperational int64        `json:"total_operational"`
	TotalAward       int64        `json:"total_award"`
	PendingReviews   int64        `json:"pending_reviews"`
	KpisByStatus     []StatusCount `json:"kpis_by_status"`
	KpisByGoal       []GoalCount   `json:"kpis_by_goal"`
}

func (h *KpiDashboardHandler) GetDashboard(c *fiber.Ctx) error {
	var data KpiDashboardData

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

	return utils.SuccessResponse(c, fiber.StatusOK, "", data)
}
