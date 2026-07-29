package services

import (
	"fmt"
	"regexp"

	"github.com/automax/backend/internal/models"
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
