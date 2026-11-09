package handlers

import (
	"fmt"
	"regexp"
	"strings"

	"github.com/automax/backend/internal/models"
	"github.com/automax/backend/internal/services"
	"github.com/google/uuid"
	"gorm.io/gorm"
)

// kpiUserName names a user for activity text: full name, else email.
func kpiUserName(db *gorm.DB, id uuid.UUID) string {
	var u models.User
	if err := db.Unscoped().Select("first_name, last_name, email").First(&u, id).Error; err != nil {
		return "a user"
	}
	if name := strings.Join(strings.Fields(u.FirstName+" "+u.LastName), " "); name != "" {
		return name
	}
	return u.Email
}

// kpiEntryLabel describes an entry by its metric and period for activity
// text, e.g. `"Roads Resurfaced" Oct 2026 (v2)`.
func kpiEntryLabel(db *gorm.DB, e *models.KpiEntry) string {
	var metric models.KpiMetric
	name := "metric"
	if err := db.Unscoped().Select("name").First(&metric, e.MetricID).Error; err == nil {
		name = metric.Name
	}
	label := fmt.Sprintf("%q %s", name, services.FormatPeriodLabel(e.PeriodCode, e.ReportingYear))
	if e.EntryVersion > 1 {
		label += fmt.Sprintf(" (v%d)", e.EntryVersion)
	}
	return label
}

var activityUUIDPattern = regexp.MustCompile(`[0-9a-fA-F]{8}-[0-9a-fA-F]{4}-[0-9a-fA-F]{4}-[0-9a-fA-F]{4}-[0-9a-fA-F]{12}`)

// humanizeKpiActivity replaces record IDs left in activity descriptions
// (older entries were logged with raw UUIDs) with the user, metric, entry
// or target they refer to. Unknown IDs are left as they are.
func humanizeKpiActivity(db *gorm.DB, logs []models.ActionLogResponse) {
	seen := map[string]bool{}
	var ids []string
	for _, l := range logs {
		for _, id := range activityUUIDPattern.FindAllString(l.Description, -1) {
			if !seen[id] {
				seen[id] = true
				ids = append(ids, id)
			}
		}
	}
	if len(ids) == 0 {
		return
	}

	labels := map[string]string{}
	var users []models.User
	db.Unscoped().Select("id, first_name, last_name, email").Where("id IN ?", ids).Find(&users)
	for _, u := range users {
		name := strings.Join(strings.Fields(u.FirstName+" "+u.LastName), " ")
		if name == "" {
			name = u.Email
		}
		labels[u.ID.String()] = name
	}
	var metrics []models.KpiMetric
	db.Unscoped().Select("id, name").Where("id IN ?", ids).Find(&metrics)
	for _, m := range metrics {
		labels[m.ID.String()] = fmt.Sprintf("%q", m.Name)
	}
	var entries []models.KpiEntry
	db.Unscoped().Select("id, metric_id, period_code, reporting_year, entry_version").Where("id IN ?", ids).Find(&entries)
	for i := range entries {
		labels[entries[i].ID.String()] = kpiEntryLabel(db, &entries[i])
	}
	var targets []models.KpiAnnualTarget
	db.Unscoped().Select("id, kpi_code, period_code, target_year").Where("id IN ?", ids).Find(&targets)
	for _, t := range targets {
		labels[t.ID.String()] = fmt.Sprintf("%s %s", t.KpiCode, services.FormatPeriodLabel(t.PeriodCode, t.TargetYear))
	}

	for i := range logs {
		logs[i].Description = activityUUIDPattern.ReplaceAllStringFunc(logs[i].Description, func(id string) string {
			if label, ok := labels[strings.ToLower(id)]; ok {
				return label
			}
			return id
		})
	}
}
