package services

import (
	"fmt"
	"regexp"
	"strings"
	"time"

	"github.com/automax/backend/internal/models"
	"github.com/google/uuid"
	"gorm.io/gorm"
)

// Each pattern accepts two conventions: the older year-embedded period key
// (e.g. "2027-01", "2027-Q1", "2027-H1", "2027" — still used by the legacy
// Performance "Period Key" manual entry field) and the newer bare label
// (e.g. "jan", "q1", "h1", "annual" — what the KPI Target/Entry period
// pickers send, since target_year/reporting_year already carries the year
// as its own field). Both are valid; a bare label isn't a lesser format.
var periodKeyPatterns = map[string]*regexp.Regexp{
	"month":       regexp.MustCompile(`(?i)^(\d{4}-(0[1-9]|1[0-2])|jan|feb|mar|apr|may|jun|jul|aug|sep|oct|nov|dec)$`),
	"quarter":     regexp.MustCompile(`(?i)^(\d{4}-Q[1-4]|q[1-4])$`),
	"semi_annual": regexp.MustCompile(`(?i)^(\d{4}-H[12]|h[12])$`),
	"annual":      regexp.MustCompile(`(?i)^(\d{4}|annual)$`),
}

// frequencyPeriodType maps a KPI's configured reporting frequency to the
// period type its targets/actuals are expected to use.
var frequencyPeriodType = map[string]string{
	models.KPIFrequencyMonthly:   "month",
	models.KPIFrequencyQuarterly: "quarter",
	models.KPIFrequencyAnnually:  "annual",
}

// ValidatePeriod checks that periodType/periodKey are well-formed and, when
// the KPI has a configured reporting frequency, consistent with it. The
// "custom" period type bypasses the frequency check, for ad-hoc periods.
func ValidatePeriod(reportingFrequency, periodType, periodKey string) error {
	if periodType == "" || periodKey == "" {
		return fmt.Errorf("period_type and period_key are required")
	}
	if pattern, ok := periodKeyPatterns[periodType]; ok && !pattern.MatchString(periodKey) {
		return fmt.Errorf("period_key %q is not a valid %s period", periodKey, periodType)
	}
	if periodType == "custom" || reportingFrequency == "" {
		return nil
	}
	if expected, ok := frequencyPeriodType[reportingFrequency]; ok && expected != periodType {
		return fmt.Errorf("period type %q does not match this KPI's reporting frequency (%q expects %q periods)", periodType, reportingFrequency, expected)
	}
	return nil
}

// GetKPIReportingFrequency looks up the reporting frequency configured on a
// KPI's dictionary record. Returns "" if the KPI can't be found.
func GetKPIReportingFrequency(db *gorm.DB, kpiCode, kpiType string) string {
	switch kpiType {
	case models.KPITypeOperational:
		var k models.OperationalKPI
		if err := db.Select("reporting_frequency").Where("code = ?", kpiCode).First(&k).Error; err == nil {
			return k.ReportingFrequency
		}
	case models.KPITypeAward:
		var k models.AwardKPI
		if err := db.Select("reporting_frequency").Where("code = ?", kpiCode).First(&k).Error; err == nil {
			return k.ReportingFrequency
		}
	default:
		var k models.StrategicKPI
		if err := db.Select("reporting_frequency").Where("code = ?", kpiCode).First(&k).Error; err == nil {
			return k.ReportingFrequency
		}
	}
	return ""
}

var monthPeriodCodes = []string{"jan", "feb", "mar", "apr", "may", "jun", "jul", "aug", "sep", "oct", "nov", "dec"}
var monthLabels = []string{"Jan", "Feb", "Mar", "Apr", "May", "Jun", "Jul", "Aug", "Sep", "Oct", "Nov", "Dec"}

// CurrentPeriodCode derives the bare period-code label ("jan".."dec",
// "q1".."q4", "h1"/"h2", "annual") that `now` falls into for a given KPI
// reporting frequency — the same bare-label convention the frontend's period
// pickers already use (see getPeriodOptionsByFrequency on the client).
func CurrentPeriodCode(reportingFrequency string, now time.Time) string {
	switch reportingFrequency {
	case models.KPIFrequencyMonthly:
		return monthPeriodCodes[int(now.Month())-1]
	case models.KPIFrequencyQuarterly:
		return fmt.Sprintf("q%d", (int(now.Month())-1)/3+1)
	case models.KPIFrequencySemiAnnual:
		if now.Month() <= 6 {
			return "h1"
		}
		return "h2"
	default:
		return "annual"
	}
}

// FormatPeriodLabel renders a bare period code + year as a human label (e.g.
// "Jul 2026", "Q3 2026", "H2 2026", "Annual 2026"), mirroring the frontend's
// formatPeriodLabel helper so the same period reads identically everywhere.
func FormatPeriodLabel(periodCode string, year int) string {
	code := strings.ToLower(periodCode)
	for i, m := range monthPeriodCodes {
		if m == code {
			return fmt.Sprintf("%s %d", monthLabels[i], year)
		}
	}
	switch code {
	case "q1", "q2", "q3", "q4", "h1", "h2":
		return fmt.Sprintf("%s %d", strings.ToUpper(code), year)
	case "annual":
		return fmt.Sprintf("Annual %d", year)
	}
	return fmt.Sprintf("%s %d", periodCode, year)
}

// GetEffectiveTarget finds the approved KpiAnnualTarget for a metric's KPI in
// the current reporting period — the same period-matching lookup CreateEntry
// already uses to snapshot a target onto a new entry — so a display (like the
// Metric Card's Target tile) can show exactly the number a new entry
// submitted right now would actually be measured against, instead of a
// static metric-config value that can silently disagree with it. Returns
// (nil, "") if no approved target exists for the current period, letting the
// caller fall back to the metric's own configured target_value.
func GetEffectiveTarget(db *gorm.DB, kpiCode, kpiType string, metricID uuid.UUID) (*float64, string) {
	freq := GetKPIReportingFrequency(db, kpiCode, kpiType)
	now := time.Now()
	year := now.Year()
	periodCode := CurrentPeriodCode(freq, now)

	var target models.KpiAnnualTarget
	err := db.Where(
		"kpi_code = ? AND kpi_type = ? AND metric_id = ? AND target_year = ? AND period_code = ? AND target_status = 'approved'",
		kpiCode, kpiType, metricID, year, periodCode,
	).First(&target).Error
	if err != nil {
		return nil, ""
	}
	value := target.TargetValue
	return &value, FormatPeriodLabel(periodCode, year)
}
