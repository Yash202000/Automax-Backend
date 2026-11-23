package services

import (
	"strings"
	"testing"
	"time"

	"github.com/automax/backend/internal/models"
)

func TestCanonicalTargetPeriodCode(t *testing.T) {
	cases := []struct {
		code     string
		year     int
		want     string
		wantYear int
		wantErr  bool
	}{
		{"q3", 2026, "q3", 2026, false},
		{"Q3", 2026, "q3", 2026, false},
		{" JAN ", 2026, "jan", 2026, false},
		{"2026-Q3", 2026, "q3", 2026, false},
		{"2026-q3", 0, "q3", 2026, false},
		{"2026-07", 2026, "jul", 2026, false},
		{"2026-H1", 2026, "h1", 2026, false},
		{"2026", 2026, "annual", 2026, false},
		{"ANNUAL", 2026, "annual", 2026, false},
		{"2027-Q1", 2026, "", 0, true}, // embedded year disagrees with target year
		{"c:20260215-20260315", 2026, "c:20260215-20260315", 2026, false},
		{"C:20260215-20260315", 2026, "c:20260215-20260315", 2026, false},
		{"c:20260315-20260215", 2026, "", 0, true},
		{"foo", 2026, "", 0, true},
		{"q5", 2026, "", 0, true},
		{"13", 2026, "", 0, true},
		{"2026-13", 2026, "", 0, true},
		{"m01", 2026, "", 0, true},
		{"", 2026, "", 0, true},
	}
	for _, c := range cases {
		got, year, err := CanonicalTargetPeriodCode(c.code, c.year)
		if (err != nil) != c.wantErr {
			t.Errorf("%q/%d: err=%v, wantErr=%v", c.code, c.year, err, c.wantErr)
			continue
		}
		if !c.wantErr && (got != c.want || year != c.wantYear) {
			t.Errorf("%q/%d: got %q/%d, want %q/%d", c.code, c.year, got, year, c.want, c.wantYear)
		}
	}
}

func TestValidateKpiTargetPeriod(t *testing.T) {
	quarterly := &models.KpiMetric{Name: "Q", ReportingFrequency: "Quarterly"}
	inherit := &models.KpiMetric{Name: "Inherit"}
	adhoc := &models.KpiMetric{Name: "AdHoc", ReportingFrequency: "Ad Hoc"}
	from := time.Date(2026, 4, 1, 0, 0, 0, 0, time.UTC)
	due := time.Date(2026, 6, 30, 0, 0, 0, 0, time.UTC)
	window := &models.KpiMetric{Name: "Win", ReportingFrequency: "Quarterly", StartDate: &from, DueDate: &due}
	const (
		period    = models.KpiTargetTypePeriod
		annual    = models.KpiTargetTypeAnnual
		milestone = models.KpiTargetTypeMilestone
	)
	cases := []struct {
		name    string
		metric  *models.KpiMetric
		kpiFreq string
		ttype   string
		year    int
		code    string
		ok      bool
	}{
		{"quarterly metric q1", quarterly, "monthly", period, 2026, "q1", true},
		{"quarterly metric jan", quarterly, "monthly", period, 2026, "jan", false},
		{"inherit monthly KPI jan", inherit, "monthly", period, 2026, "jan", true},
		{"inherit monthly KPI q1", inherit, "monthly", period, 2026, "q1", false},
		{"KPI-level semi h2", nil, "semi_annual", period, 2026, "h2", true},
		{"KPI-level semi q2", nil, "semi_annual", period, 2026, "q2", false},
		{"KPI-level no freq annual", nil, "", period, 2026, "annual", true},
		{"KPI-level no freq q1", nil, "", period, 2026, "q1", false},
		{"annual target annual on quarterly", quarterly, "quarterly", annual, 2026, "annual", true},
		{"annual target q1", quarterly, "quarterly", annual, 2026, "q1", false},
		{"annual target h2", nil, "semi_annual", annual, 2026, "h2", false},
		{"period target custom code", quarterly, "quarterly", period, 2026, "c:20260101-20260131", false},
		{"milestone KPI-level", nil, "quarterly", milestone, 2026, "c:20260201-20260220", true},
		{"milestone needs range", nil, "quarterly", milestone, 2026, "q1", false},
		{"adhoc metric period target", adhoc, "quarterly", period, 2026, "q1", false},
		{"adhoc KPI period target", nil, "custom", period, 2026, "annual", false},
		{"adhoc metric annual target", adhoc, "quarterly", annual, 2026, "annual", true},
		{"adhoc metric milestone", adhoc, "quarterly", milestone, 2026, "c:20260201-20260220", true},
		{"window q2 inside", window, "", period, 2026, "q2", true},
		{"window q1 outside", window, "", period, 2026, "q1", false},
		{"window annual 2027 outside", window, "", annual, 2027, "annual", false},
		{"window milestone outside", window, "", milestone, 2026, "c:20260325-20260410", false},
	}
	for _, c := range cases {
		err := ValidateKpiTargetPeriod(c.metric, c.kpiFreq, c.ttype, c.year, c.code)
		if (err == nil) != c.ok {
			t.Errorf("%s: err=%v, want ok=%v", c.name, err, c.ok)
		}
	}
}

func f(v float64) *float64 { return &v }

func TestNormalizeKpiTargetThresholds(t *testing.T) {
	cases := []struct {
		name      string
		target    models.KpiAnnualTarget
		direction string
		ok        bool
	}{
		{"HiB ordered", models.KpiAnnualTarget{ThresholdMode: "Absolute Values", ExcellentThreshold: f(110), AchievedThreshold: f(100), WarningThreshold: f(80)}, "Higher is Better", true},
		{"HiB reversed", models.KpiAnnualTarget{ThresholdMode: "Absolute Values", ExcellentThreshold: f(50), AchievedThreshold: f(100), WarningThreshold: f(150)}, "Higher is Better", false},
		{"LiB ordered", models.KpiAnnualTarget{ThresholdMode: "Absolute Values", ExcellentThreshold: f(10), AchievedThreshold: f(100), WarningThreshold: f(200)}, "Lower is Better", true},
		{"LiB reversed", models.KpiAnnualTarget{ThresholdMode: "Absolute Values", ExcellentThreshold: f(200), AchievedThreshold: f(100), WarningThreshold: f(10)}, "Lower is Better", false},
		{"partial thresholds", models.KpiAnnualTarget{ThresholdMode: "Absolute Values", ExcellentThreshold: f(110), WarningThreshold: f(80)}, "Higher is Better", true},
		{"pct negative", models.KpiAnnualTarget{ThresholdMode: "Percentage of Target", AchievedThreshold: f(-10)}, "Higher is Better", false},
		{"pct too high", models.KpiAnnualTarget{ThresholdMode: "Percentage of Target", ExcellentThreshold: f(5000)}, "Higher is Better", false},
		{"pct ok", models.KpiAnnualTarget{ThresholdMode: "percentage of target", ExcellentThreshold: f(120), AchievedThreshold: f(100), WarningThreshold: f(80)}, "Higher is Better", true},
		{"range ok", models.KpiAnnualTarget{ThresholdMode: "Target Range", TargetValue: 15, TargetRangeMin: f(10), TargetRangeMax: f(20)}, "", true},
		{"range min>max", models.KpiAnnualTarget{ThresholdMode: "Target Range", TargetValue: 15, TargetRangeMin: f(100), TargetRangeMax: f(10)}, "", false},
		{"range missing", models.KpiAnnualTarget{ThresholdMode: "Target Range"}, "", false},
		{"range target outside", models.KpiAnnualTarget{ThresholdMode: "Target Range", TargetValue: 100, TargetRangeMin: f(10), TargetRangeMax: f(20)}, "", false},
		{"bogus mode", models.KpiAnnualTarget{ThresholdMode: "Banana"}, "", false},
		{"bogus basis", models.KpiAnnualTarget{TargetBasis: "Whatever"}, "", false},
		{"global clears", models.KpiAnnualTarget{ThresholdMode: "Use Global KPI Rules", ExcellentThreshold: f(1)}, "Higher is Better", true},
	}
	for _, c := range cases {
		tgt := c.target
		err := NormalizeKpiTargetThresholds(&tgt, c.direction)
		if (err == nil) != c.ok {
			t.Errorf("%s: err=%v, want ok=%v", c.name, err, c.ok)
		}
		if c.ok && (tgt.ThresholdMode == "Use Global KPI Rules" || tgt.ThresholdMode == "No Thresholds") &&
			(tgt.ExcellentThreshold != nil || tgt.TargetRangeMin != nil) {
			t.Errorf("%s: unused thresholds not cleared", c.name)
		}
	}
	tgt := models.KpiAnnualTarget{ThresholdMode: "percentage of target", TargetBasis: "strategic plan"}
	if err := NormalizeKpiTargetThresholds(&tgt, ""); err != nil || tgt.ThresholdMode != "Percentage of Target" || tgt.TargetBasis != "Strategic Plan" {
		t.Errorf("enum values not canonicalised: %+v %v", tgt, err)
	}
}

func TestCanonicalKpiTargetSegmentation(t *testing.T) {
	a, err := CanonicalKpiTargetSegmentation([]models.KpiTargetSegmentationValue{{Dimension: "Region", Value: "North"}, {Dimension: " Gender ", Value: "Male"}})
	if err != nil {
		t.Fatal(err)
	}
	b, _ := CanonicalKpiTargetSegmentation([]models.KpiTargetSegmentationValue{{Dimension: "Gender", Value: "Male"}, {Dimension: "Region", Value: "North"}})
	if string(a) != string(b) || !strings.HasPrefix(string(a), `[{"dimension":"Gender"`) {
		t.Errorf("not canonical: %s vs %s", a, b)
	}
	if KpiTargetSegmentationKey(a) == KpiTargetSegmentationKey(nil) {
		t.Error("segmented and unsegmented keys must differ")
	}
	if _, err := CanonicalKpiTargetSegmentation([]models.KpiTargetSegmentationValue{{Dimension: "", Value: "x"}}); err == nil {
		t.Error("empty dimension accepted")
	}
	if _, err := CanonicalKpiTargetSegmentation([]models.KpiTargetSegmentationValue{{Dimension: "G", Value: "M"}, {Dimension: "g", Value: "F"}}); err == nil {
		t.Error("duplicate dimension accepted")
	}
	if v, err := CanonicalKpiTargetSegmentation(nil); v != nil || err != nil {
		t.Error("empty segmentation should be nil")
	}
}

func TestKpiTargetStatusRules(t *testing.T) {
	for _, s := range []string{"draft", "returned", "rejected"} {
		if !models.IsKpiTargetEditableStatus(s) || models.IsKpiTargetImmutableStatus(s) {
			t.Errorf("%s should be editable", s)
		}
	}
	for _, s := range []string{"approved", "locked", "superseded"} {
		if models.IsKpiTargetEditableStatus(s) || !models.IsKpiTargetImmutableStatus(s) {
			t.Errorf("%s should be immutable", s)
		}
	}
	if models.IsKpiTargetEditableStatus("submitted") || models.IsKpiTargetImmutableStatus("submitted") {
		t.Error("submitted is neither editable nor immutable (admin override only)")
	}
}

func TestLiveTargetBefore(t *testing.T) {
	v1 := &models.KpiAnnualTarget{Version: 1, TargetStatus: "superseded"}
	v2 := &models.KpiAnnualTarget{Version: 2, TargetStatus: "draft"}
	v3 := &models.KpiAnnualTarget{Version: 3, TargetStatus: "superseded"}
	if !liveTargetBefore(v2, v1) || liveTargetBefore(v1, v2) {
		t.Error("live v2 should win over superseded v1")
	}
	if !liveTargetBefore(v2, v3) {
		t.Error("non-superseded should win over a higher superseded version")
	}
}

func TestValidateKpiTargetImportOptions(t *testing.T) {
	if validateKpiTargetImportOptions(KpiImportOptions{}) != "" {
		t.Error("defaults should be valid")
	}
	if validateKpiTargetImportOptions(KpiImportOptions{ExistingMode: "bogus"}) == "" ||
		validateKpiTargetImportOptions(KpiImportOptions{ErrorMode: "bogus"}) == "" {
		t.Error("bogus modes accepted")
	}
}

func TestModelParseTargetDate(t *testing.T) {
	if d, err := models.ParseTargetDate("X", "2026-06-01T00:00:00Z"); err != nil || d.Format("2006-01-02") != "2026-06-01" {
		t.Errorf("timestamp: %v %v", d, err)
	}
	if _, err := models.ParseTargetDate("X", "garbage"); err == nil {
		t.Error("garbage accepted")
	}
	if _, err := models.ParseTargetDate("X", "2026-02-30"); err == nil {
		t.Error("2026-02-30 accepted")
	}
	if d, err := models.ParseTargetDate("X", ""); d != nil || err != nil {
		t.Error("empty should be nil")
	}
}
