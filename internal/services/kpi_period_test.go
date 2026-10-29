package services

import (
	"testing"
	"time"

	"github.com/automax/backend/internal/models"
)

func date(y int, m time.Month, d int) *time.Time {
	t := time.Date(y, m, d, 0, 0, 0, 0, time.UTC)
	return &t
}

func TestPeriodDateRange(t *testing.T) {
	cases := []struct {
		year       int
		code       string
		start, end string
	}{
		{2026, "sep", "2026-09-01", "2026-09-30"},
		{2026, "feb", "2026-02-01", "2026-02-28"},
		{2026, "Q2", "2026-04-01", "2026-06-30"},
		{2026, "h2", "2026-07-01", "2026-12-31"},
		{2026, "annual", "2026-01-01", "2026-12-31"},
		{2020, "2027-03", "2027-03-01", "2027-03-31"},
		{2020, "2027-Q4", "2027-10-01", "2027-12-31"},
		{2020, "2027", "2027-01-01", "2027-12-31"},
	}
	for _, tc := range cases {
		start, end, ok := PeriodDateRange(tc.year, tc.code)
		if !ok || start.Format("2006-01-02") != tc.start || end.Format("2006-01-02") != tc.end {
			t.Errorf("PeriodDateRange(%d, %q) = %s..%s ok=%v, want %s..%s",
				tc.year, tc.code, start.Format("2006-01-02"), end.Format("2006-01-02"), ok, tc.start, tc.end)
		}
	}
	if _, _, ok := PeriodDateRange(2026, "adhoc"); ok {
		t.Error("unknown period code should not resolve")
	}
}

func TestValidateTargetPeriodForMetric(t *testing.T) {
	cases := []struct {
		name    string
		metric  models.KpiMetric
		kpiFreq string
		code    string
		wantErr bool
	}{
		// Client examples from the bug report.
		{"Sep-only metric rejects Jan", models.KpiMetric{ReportingFrequency: "Monthly", StartDate: date(2026, 9, 1), DueDate: date(2026, 9, 30)}, "", "jan", true},
		{"Oct-Dec metric rejects Q2", models.KpiMetric{ReportingFrequency: "Quarterly", StartDate: date(2026, 10, 1), DueDate: date(2026, 12, 31)}, "", "q2", true},
		{"quarterly KPI, monthly metric rejects Q1", models.KpiMetric{ReportingFrequency: "Monthly"}, models.KPIFrequencyQuarterly, "q1", true},

		{"Sep-only metric accepts Sep", models.KpiMetric{ReportingFrequency: "Monthly", StartDate: date(2026, 9, 1), DueDate: date(2026, 9, 30)}, "", "sep", false},
		{"Oct-Dec metric accepts Q4", models.KpiMetric{ReportingFrequency: "Quarterly", StartDate: date(2026, 10, 1), DueDate: date(2026, 12, 31)}, "", "q4", false},
		{"quarterly KPI, monthly metric accepts Jan", models.KpiMetric{ReportingFrequency: "Monthly"}, models.KPIFrequencyQuarterly, "jan", false},
		{"mid-month start still covers that month", models.KpiMetric{ReportingFrequency: "Monthly", StartDate: date(2026, 9, 15)}, "", "sep", false},
		{"open-ended start rejects earlier month", models.KpiMetric{ReportingFrequency: "Monthly", StartDate: date(2026, 9, 15)}, "", "aug", true},
		{"due date only rejects later month", models.KpiMetric{ReportingFrequency: "Monthly", DueDate: date(2026, 3, 31)}, "", "apr", true},
		{"inherits KPI frequency", models.KpiMetric{}, models.KPIFrequencyQuarterly, "q3", false},
		{"inherited KPI frequency still enforced", models.KpiMetric{}, models.KPIFrequencyQuarterly, "mar", true},
		{"semiannual metric accepts H1", models.KpiMetric{ReportingFrequency: "Semiannual"}, "", "h1", false},
		{"annual metric rejects month", models.KpiMetric{ReportingFrequency: "Annual"}, "", "jan", true},
		{"ad hoc skips frequency check", models.KpiMetric{ReportingFrequency: "Ad Hoc"}, models.KPIFrequencyMonthly, "q2", false},
		{"no frequency anywhere, no dates", models.KpiMetric{}, "", "q2", false},
	}
	for _, tc := range cases {
		err := ValidateTargetPeriodForMetric(&tc.metric, tc.kpiFreq, 2026, tc.code)
		if (err != nil) != tc.wantErr {
			t.Errorf("%s: err = %v, wantErr %v", tc.name, err, tc.wantErr)
		}
	}
}
