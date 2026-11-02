package services

import (
	"fmt"
	"regexp"
	"strconv"
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

// PeriodCodesForFrequency lists every bare period code within one year for a
// given reporting frequency, in calendar order — used to build a full-year
// Baseline/Target/Actual trend series (doc Figure 1) without the caller
// needing to know the month/quarter/half-year layout itself. Accepts both
// the KPI-level lowercase convention ("monthly", "semi_annual") and the
// KpiMetric-level capitalized convention its own Reporting Frequency
// dropdown sends ("Monthly", "Semiannual") — the two have never agreed on
// casing/spelling, so this normalizes rather than silently defaulting to
// "annual" for a metric that's actually monthly.
func PeriodCodesForFrequency(frequency string) []string {
	switch strings.ToLower(strings.ReplaceAll(frequency, " ", "")) {
	case models.KPIFrequencyMonthly:
		return append([]string{}, monthPeriodCodes...)
	case models.KPIFrequencyQuarterly:
		return []string{"q1", "q2", "q3", "q4"}
	case models.KPIFrequencySemiAnnual, "semiannual":
		return []string{"h1", "h2"}
	default:
		return []string{"annual"}
	}
}

// FormatPeriodLabel renders a bare period code + year as a human label (e.g.
// "Jul 2026", "Q3 2026", "H2 2026", "Annual 2026"), mirroring the frontend's
// formatPeriodLabel helper so the same period reads identically everywhere.
func FormatPeriodLabel(periodCode string, year int) string {
	if start, end, ok := ParseCustomPeriod(periodCode); ok {
		if start.Year() == end.Year() {
			return fmt.Sprintf("%s – %s", start.Format("02 Jan"), end.Format("02 Jan 2006"))
		}
		return fmt.Sprintf("%s – %s", start.Format("02 Jan 2006"), end.Format("02 Jan 2006"))
	}
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

var periodOrdinals = map[string]int{
	"jan": 1, "feb": 2, "mar": 3, "apr": 4, "may": 5, "jun": 6,
	"jul": 7, "aug": 8, "sep": 9, "oct": 10, "nov": 11, "dec": 12,
	"q1": 1, "q2": 2, "q3": 3, "q4": 4,
	"h1": 1, "h2": 2,
	"annual": 1,
}

func PeriodSortKey(year int, periodCode string) int {
	if start, _, ok := ParseCustomPeriod(periodCode); ok {
		return start.Year()*100 + int(start.Month())
	}
	return year*100 + periodOrdinals[strings.ToLower(periodCode)]
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

// NormalizeFrequency maps both reporting-frequency spellings onto the KPI
// constants: the KPI dictionary's ("monthly", "semi_annual", "annually") and
// the Metric form's ("Monthly", "Semiannual", "Annual", "Ad Hoc"). Returns ""
// for an empty or unknown value.
func NormalizeFrequency(frequency string) string {
	switch strings.ToLower(strings.NewReplacer(" ", "", "_", "", "-", "").Replace(frequency)) {
	case "monthly":
		return models.KPIFrequencyMonthly
	case "quarterly":
		return models.KPIFrequencyQuarterly
	case "semiannual":
		return models.KPIFrequencySemiAnnual
	case "annual", "annually":
		return models.KPIFrequencyAnnually
	case "adhoc", "custom":
		return models.KPIFrequencyCustom
	}
	return ""
}

var targetPeriodTypes = map[string]string{
	models.KPIFrequencyMonthly:    "month",
	models.KPIFrequencyQuarterly:  "quarter",
	models.KPIFrequencySemiAnnual: "semi_annual",
	models.KPIFrequencyAnnually:   "annual",
}

var yearEmbeddedPeriod = regexp.MustCompile(`(?i)^(\d{4})(?:-(0[1-9]|1[0-2]|Q[1-4]|H[12]))?$`)

// Milestone / Ad Hoc targets cover an arbitrary date range instead of a whole
// month/quarter/half/year. Their period code is built from the dates
// ("c:20260215-20260315"), so entries and roll-ups keep matching a
// target by period code exactly as they do for regular periods. The code is
// kept compact ("c:20260215-20260315", 19 chars) because the legacy
// period_key column that mirrors it is only varchar(20).
const CustomPeriodPrefix = "c:"

var customPeriodPattern = regexp.MustCompile(`^c:(\d{8})-(\d{8})$`)

// IsCustomPeriodCode reports whether a period code is a custom date range.
func IsCustomPeriodCode(periodCode string) bool {
	return strings.HasPrefix(strings.ToLower(periodCode), CustomPeriodPrefix)
}

// CustomPeriodCode builds the period code for a custom date range.
func CustomPeriodCode(start, end time.Time) string {
	return CustomPeriodPrefix + start.Format("20060102") + "-" + end.Format("20060102")
}

// ParseCustomPeriod returns the start and end dates (UTC) of a custom period
// code; ok is false if the code is not a valid custom range.
func ParseCustomPeriod(periodCode string) (start, end time.Time, ok bool) {
	m := customPeriodPattern.FindStringSubmatch(strings.ToLower(periodCode))
	if m == nil {
		return time.Time{}, time.Time{}, false
	}
	start, err1 := time.Parse("20060102", m[1])
	end, err2 := time.Parse("20060102", m[2])
	if err1 != nil || err2 != nil || end.Before(start) {
		return time.Time{}, time.Time{}, false
	}
	return start, end, true
}

// PeriodDateRange returns the first and last calendar day (UTC) of a period
// code within a year. Accepts the bare labels ("sep", "q2", "h1", "annual")
// and the year-embedded keys ("2026-09", "2026-Q2", "2026-H1", "2026"), whose
// own year wins over the year argument.
func PeriodDateRange(year int, periodCode string) (time.Time, time.Time, bool) {
	if IsCustomPeriodCode(periodCode) {
		return ParseCustomPeriod(periodCode)
	}
	code := strings.ToLower(periodCode)
	if m := yearEmbeddedPeriod.FindStringSubmatch(code); m != nil {
		year, _ = strconv.Atoi(m[1])
		switch month, err := strconv.Atoi(m[2]); {
		case m[2] == "":
			code = "annual"
		case err == nil:
			code = monthPeriodCodes[month-1]
		default:
			code = m[2]
		}
	}
	firstMonth, months := 0, 0
	switch {
	case code == "annual":
		firstMonth, months = 1, 12
	case len(code) == 2 && code[0] == 'q' && code[1] >= '1' && code[1] <= '4':
		firstMonth, months = int(code[1]-'1')*3+1, 3
	case code == "h1" || code == "h2":
		firstMonth, months = int(code[1]-'1')*6+1, 6
	default:
		for i, m := range monthPeriodCodes {
			if m == code {
				firstMonth, months = i+1, 1
			}
		}
	}
	if months == 0 {
		return time.Time{}, time.Time{}, false
	}
	start := time.Date(year, time.Month(firstMonth), 1, 0, 0, 0, 0, time.UTC)
	return start, start.AddDate(0, months, -1), true
}

// ValidateTargetPeriodForMetric enforces REL-05 for a target attached to a
// metric: the period must belong to the metric's own reporting frequency
// (falling back to the KPI's when the metric inherits it) and must overlap
// the metric's validity window (start_date..due_date). Ad Hoc frequency skips
// the frequency check; a metric with no dates has no window.
func ValidateTargetPeriodForMetric(metric *models.KpiMetric, kpiFrequency string, year int, periodCode string) error {
	if IsCustomPeriodCode(periodCode) {
		return validateCustomPeriodForMetric(metric, periodCode)
	}
	frequency := NormalizeFrequency(metric.ReportingFrequency)
	if frequency == "" {
		frequency = NormalizeFrequency(kpiFrequency)
	}
	if periodType, ok := targetPeriodTypes[frequency]; ok {
		if !periodKeyPatterns[periodType].MatchString(periodCode) {
			return fmt.Errorf("Period %s does not match metric %q, which is reported %s. Choose a %s period.",
				FormatPeriodLabel(periodCode, year), metric.Name, frequencyLabel(frequency), frequencyLabel(frequency))
		}
	}

	if metric.StartDate == nil && metric.DueDate == nil {
		return nil
	}
	periodStart, periodEnd, ok := PeriodDateRange(year, periodCode)
	if !ok {
		return nil
	}
	day := func(t *time.Time) time.Time {
		u := t.UTC()
		return time.Date(u.Year(), u.Month(), u.Day(), 0, 0, 0, 0, time.UTC)
	}
	if (metric.StartDate != nil && periodEnd.Before(day(metric.StartDate))) ||
		(metric.DueDate != nil && periodStart.After(day(metric.DueDate))) {
		return fmt.Errorf("Period %s (%s to %s) is outside the validity period of metric %q (%s).",
			FormatPeriodLabel(periodCode, year), periodStart.Format("2006-01-02"), periodEnd.Format("2006-01-02"),
			metric.Name, validityLabel(metric.StartDate, metric.DueDate))
	}
	return nil
}

// validateCustomPeriodForMetric checks a Milestone / Ad Hoc date range: it
// must be well-formed and lie entirely within the metric's validity window.
// There is no frequency check — a custom range is not a reporting period.
func validateCustomPeriodForMetric(metric *models.KpiMetric, periodCode string) error {
	start, end, ok := ParseCustomPeriod(periodCode)
	if !ok {
		return fmt.Errorf("Period Start and Period End are required, and Period End cannot be before Period Start.")
	}
	day := func(t *time.Time) time.Time {
		u := t.UTC()
		return time.Date(u.Year(), u.Month(), u.Day(), 0, 0, 0, 0, time.UTC)
	}
	if (metric.StartDate != nil && start.Before(day(metric.StartDate))) ||
		(metric.DueDate != nil && end.After(day(metric.DueDate))) {
		return fmt.Errorf("Period %s is outside the validity period of metric %q (%s).",
			FormatPeriodLabel(periodCode, start.Year()), metric.Name, validityLabel(metric.StartDate, metric.DueDate))
	}
	return nil
}

func frequencyLabel(frequency string) string {
	switch frequency {
	case models.KPIFrequencyMonthly:
		return "monthly"
	case models.KPIFrequencyQuarterly:
		return "quarterly"
	case models.KPIFrequencySemiAnnual:
		return "semi-annual"
	default:
		return "annual"
	}
}

func validityLabel(start, due *time.Time) string {
	switch {
	case start != nil && due != nil:
		return start.UTC().Format("2006-01-02") + " to " + due.UTC().Format("2006-01-02")
	case start != nil:
		return "from " + start.UTC().Format("2006-01-02")
	default:
		return "until " + due.UTC().Format("2006-01-02")
	}
}
