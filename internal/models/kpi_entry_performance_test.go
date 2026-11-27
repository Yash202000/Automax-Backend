package models

import (
	"encoding/json"
	"math"
	"testing"

	"gorm.io/datatypes"
)

func f(v float64) *float64 { return &v }

func TestAchievementPercent(t *testing.T) {
	cases := []struct {
		name           string
		actual, target float64
		direction      string
		want           float64
		wantOK         bool
	}{
		{"HiB 80/100", 80, 100, DirectionHigherIsBetter, 80, true},
		{"HiB over", 120, 100, DirectionHigherIsBetter, 120, true},
		{"HiB zero target", 5, 0, DirectionHigherIsBetter, 0, false},
		{"HiB negative target better", -5, -10, DirectionHigherIsBetter, 150, true},
		{"HiB negative actual floors at 0", -5, 10, DirectionHigherIsBetter, 0, true},
		{"LiB target/actual", 8, 10, DirectionLowerIsBetter, 125, true},
		{"LiB worse", 20, 10, DirectionLowerIsBetter, 50, true},
		{"LiB polarity descending", 20, 10, KPIPolarityDescending, 50, true},
		{"LiB actual 0 is best", 0, 5, DirectionLowerIsBetter, 100, true},
		{"LiB negative actual is best", -1, 5, DirectionLowerIsBetter, 100, true},
		{"LiB zero target zero actual", 0, 0, DirectionLowerIsBetter, 100, true},
		{"LiB zero target positive actual", 3, 0, DirectionLowerIsBetter, 0, true},
		{"LiB negative target better", -20, -10, DirectionLowerIsBetter, 200, true},
		{"LiB negative target worse", -5, -10, DirectionLowerIsBetter, 50, true},
		{"Exact hit", 50, 50, DirectionExactTarget, 100, true},
		{"Exact deviation", 45, 50, DirectionExactTarget, 90, true},
		{"Exact zero target miss", 1, 0, DirectionExactTarget, 0, true},
	}
	for _, c := range cases {
		got, ok := AchievementPercent(c.actual, c.target, c.direction)
		if ok != c.wantOK || got != c.want || math.IsNaN(got) || math.IsInf(got, 0) {
			t.Errorf("%s: got (%v,%v) want (%v,%v)", c.name, got, ok, c.want, c.wantOK)
		}
	}
}

func TestEvaluatePerformanceStatus(t *testing.T) {
	cases := []struct {
		name       string
		actual     float64
		rules      PerformanceRules
		calculable bool
		raw        float64
		capped     float64
		status     string
	}{
		{"default exceeded", 100, PerformanceRules{Target: f(100)}, true, 100, 100, PerformanceStatusExceeded},
		{"default achieved", 80, PerformanceRules{Target: f(100)}, true, 80, 80, PerformanceStatusAchieved},
		{"default warning 50", 50, PerformanceRules{Target: f(100)}, true, 50, 50, PerformanceStatusWarning},
		{"default below", 49, PerformanceRules{Target: f(100)}, true, 49, 49, PerformanceStatusBelowTarget},
		{"cap", 150, PerformanceRules{Target: f(100)}, true, 150, 100, PerformanceStatusExceeded},
		{"no target", 5, PerformanceRules{}, false, 0, 0, PerformanceStatusNotCalculable},
		{"band green 95", 90, PerformanceRules{Target: f(100), GreenMin: f(95), AmberMin: f(75)}, true, 90, 90, PerformanceStatusWarning},
		{"band amber 0", 10, PerformanceRules{Target: f(100), GreenMin: f(50), AmberMin: f(0)}, true, 10, 10, PerformanceStatusWarning},
		{"pct thresholds beat band", 90, PerformanceRules{Target: f(100), ThresholdMode: ThresholdModePercentage,
			ExcellentThreshold: f(110), AchievedThreshold: f(85), WarningThreshold: f(70), GreenMin: f(95)}, true, 90, 90, PerformanceStatusAchieved},
		{"global mode ignores thresholds", 90, PerformanceRules{Target: f(100), ThresholdMode: ThresholdModeGlobal,
			AchievedThreshold: f(95), GreenMin: f(80)}, true, 90, 90, PerformanceStatusAchieved},
		{"absolute HiB", 70, PerformanceRules{Target: f(100), ThresholdMode: ThresholdModeAbsolute,
			AchievedThreshold: f(90), WarningThreshold: f(60)}, true, 70, 70, PerformanceStatusWarning},
		{"absolute LiB", 12, PerformanceRules{Direction: DirectionLowerIsBetter, Target: f(10), ThresholdMode: ThresholdModeAbsolute,
			AchievedThreshold: f(12), WarningThreshold: f(15)}, true, 83.33, 83.33, PerformanceStatusAchieved},
		{"LiB actual 0", 0, PerformanceRules{Direction: DirectionLowerIsBetter, Target: f(5)}, true, 100, 100, PerformanceStatusExceeded},
		{"LiB on target", 20, PerformanceRules{Direction: DirectionLowerIsBetter, Target: f(20)}, true, 100, 100, PerformanceStatusExceeded},
		{"informational", 30, PerformanceRules{Direction: DirectionInformational, Target: f(100)}, false, 0, 0, PerformanceStatusInformational},
		{"range inside", 55, PerformanceRules{Direction: DirectionTargetRange, Target: f(50), RangeMin: f(40), RangeMax: f(60)}, true, 100, 100, PerformanceStatusInRange},
		{"range below", 30, PerformanceRules{Direction: DirectionTargetRange, Target: f(50), RangeMin: f(40), RangeMax: f(60)}, true, 75, 75, PerformanceStatusOutOfRange},
		{"range above", 90, PerformanceRules{Direction: DirectionTargetRange, Target: f(50), RangeMin: f(40), RangeMax: f(60)}, true, 50, 50, PerformanceStatusOutOfRange},
		{"range mode on HiB metric", 45, PerformanceRules{Target: f(50), ThresholdMode: ThresholdModeTargetRange, RangeMin: f(40), RangeMax: f(60)}, true, 100, 100, PerformanceStatusInRange},
		{"range direction w/o bounds uses target", 50, PerformanceRules{Direction: DirectionTargetRange, Target: f(50)}, true, 100, 100, PerformanceStatusInRange},
		{"exact never exceeded", 50, PerformanceRules{Direction: DirectionExactTarget, Target: f(50)}, true, 100, 100, PerformanceStatusAchieved},
		{"exact deviation", 30, PerformanceRules{Direction: DirectionExactTarget, Target: f(50)}, true, 60, 60, PerformanceStatusWarning},
	}
	for _, c := range cases {
		r := EvaluatePerformance(c.actual, c.rules, 100)
		if r.Calculable != c.calculable || r.Raw != c.raw || r.Capped != c.capped || r.Status != c.status {
			t.Errorf("%s: got %+v", c.name, r)
		}
	}
}

func TestEntryComputeAchievementLowerIsBetter(t *testing.T) {
	// 1/20 = 5% against a LiB target of 10% → 200% raw, stored capped 100.
	e := KpiEntry{CalculationTypeSnapshot: "Percentage - Ratio", DirectionSnapshot: DirectionLowerIsBetter,
		NumeratorValue: f(1), DenominatorValue: f(20), TargetValueSnapshot: f(10)}
	if err := e.ComputeActualValue(); err != nil {
		t.Fatal(err)
	}
	e.ComputeAchievement()
	if e.ActualValue != 5 || e.AchievementPercentage == nil || *e.AchievementPercentage != 100 || e.PerformanceStatus != PerformanceStatusExceeded {
		t.Fatalf("got actual %v pct %v status %s", e.ActualValue, e.AchievementPercentage, e.PerformanceStatus)
	}
	// Informational: no achievement stored.
	e = KpiEntry{DirectionSnapshot: DirectionInformational, ActualValue: 3, TargetValueSnapshot: f(10)}
	e.ComputeAchievement()
	if e.AchievementPercentage != nil || e.PerformanceStatus != PerformanceStatusInformational {
		t.Fatalf("informational: %v %s", e.AchievementPercentage, e.PerformanceStatus)
	}
}

func comps(vals ...[2]float64) datatypes.JSON {
	var cs []KpiEntryComponentValue
	for i, v := range vals {
		cs = append(cs, KpiEntryComponentValue{Component: "c", Value: v[0], Weight: v[1], Sequence: i + 1})
	}
	b, _ := json.Marshal(cs)
	return b
}

func TestComputeActualValue(t *testing.T) {
	cases := []struct {
		name    string
		e       KpiEntry
		want    float64
		wantErr bool
	}{
		{"direct", KpiEntry{CalculationTypeSnapshot: "Direct Value", DirectActualValue: f(7)}, 7, false},
		{"direct missing", KpiEntry{CalculationTypeSnapshot: "Direct Value"}, 0, true},
		{"pct ratio", KpiEntry{CalculationTypeSnapshot: "Percentage - Ratio", NumeratorValue: f(1), DenominatorValue: f(4)}, 25, false},
		{"ratio denom 0", KpiEntry{CalculationTypeSnapshot: "Ratio", NumeratorValue: f(1), DenominatorValue: f(0)}, 0, true},
		{"ratio no denom", KpiEntry{CalculationTypeSnapshot: "Ratio", NumeratorValue: f(1)}, 0, true},
		{"sum", KpiEntry{CalculationTypeSnapshot: "Sum", ComponentValues: comps([2]float64{10, 0}, [2]float64{20, 0})}, 30, false},
		{"sum empty", KpiEntry{CalculationTypeSnapshot: "Sum", ComponentValues: comps()}, 0, true},
		{"average", KpiEntry{CalculationTypeSnapshot: "Average", ComponentValues: comps([2]float64{10, 0}, [2]float64{20, 0})}, 15, false},
		{"weighted", KpiEntry{CalculationTypeSnapshot: "Weighted Average", ComponentValues: comps([2]float64{10, 1}, [2]float64{40, 2})}, 30, false},
		{"weighted zero weights", KpiEntry{CalculationTypeSnapshot: "Weighted Average", ComponentValues: comps([2]float64{10, 0}, [2]float64{40, 0})}, 0, true},
		{"weighted negative weight", KpiEntry{CalculationTypeSnapshot: "Weighted Average", ComponentValues: comps([2]float64{10, -1}, [2]float64{40, 2})}, 0, true},
		{"difference components", KpiEntry{CalculationTypeSnapshot: "Difference", ComponentValues: comps([2]float64{50, 0}, [2]float64{20, 0}, [2]float64{5, 0})}, 25, false},
		{"difference num/den", KpiEntry{CalculationTypeSnapshot: "Difference", NumeratorValue: f(50), DenominatorValue: f(20)}, 30, false},
		{"difference one component", KpiEntry{CalculationTypeSnapshot: "Difference", ComponentValues: comps([2]float64{50, 0})}, 0, true},
	}
	for _, c := range cases {
		c.e.ActualValue = 999 // must be reset, never accumulated onto
		err := c.e.ComputeActualValue()
		if (err != nil) != c.wantErr || (!c.wantErr && c.e.ActualValue != c.want) {
			t.Errorf("%s: got %v err %v", c.name, c.e.ActualValue, err)
		}
	}
}

func TestApplyToRecomputesAndKeepsUnsentInputs(t *testing.T) {
	// Sum edit: [10,20] then [1] must give 1, not 31.
	e := KpiEntry{CalculationTypeSnapshot: "Sum", ComponentValues: comps([2]float64{10, 0}, [2]float64{20, 0})}
	if err := e.ComputeActualValue(); err != nil || e.ActualValue != 30 {
		t.Fatalf("initial %v %v", e.ActualValue, err)
	}
	req := KpiEntryUpdateRequest{ComponentValues: []KpiEntryComponentValue{{Component: "a", Value: 1}}}
	if err := req.ApplyTo(&e, false); err != nil || e.ActualValue != 1 {
		t.Fatalf("after edit %v %v", e.ActualValue, err)
	}

	// Note-only adjustment of a ratio entry keeps numerator/denominator.
	r := KpiEntry{CalculationTypeSnapshot: "Percentage - Ratio", NumeratorValue: f(1), DenominatorValue: f(20), DataQualityNotes: "orig", PerformanceCommentary: "c"}
	note := KpiEntryUpdateRequest{DataQualityNotes: "checked"}
	if err := note.ApplyTo(&r, true); err != nil || r.ActualValue != 5 || *r.NumeratorValue != 1 || r.PerformanceCommentary != "c" || r.DataQualityNotes != "checked" {
		t.Fatalf("note-only adjustment: %+v err %v", r, err)
	}

	// Explicit null on a required input is rejected, not silently stale.
	d := KpiEntry{CalculationTypeSnapshot: "Direct Value", DirectActualValue: f(77)}
	clear := KpiEntryUpdateRequest{}
	clear.MarkExplicitNulls([]byte(`{"direct_actual_value": null}`))
	if err := clear.ApplyTo(&d, false); err == nil {
		t.Fatalf("expected error clearing a required direct value, got actual %v", d.ActualValue)
	}

	// Denominator 0 on edit is rejected like on create.
	z := KpiEntry{CalculationTypeSnapshot: "Ratio", NumeratorValue: f(1), DenominatorValue: f(2)}
	zero := KpiEntryUpdateRequest{NumeratorValue: f(5), DenominatorValue: f(0)}
	if err := zero.ApplyTo(&z, false); err == nil {
		t.Fatal("expected error for denominator 0")
	}
}
