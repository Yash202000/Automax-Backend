package metricformula

import (
	"math"
	"testing"
)

func TestEvaluate(t *testing.T) {
	tests := []struct {
		name     string
		formula  string
		siblings map[string]float64
		want     float64
		wantErr  bool
	}{
		{
			name:     "simple division",
			formula:  "${tasks_completed} / ${tasks_total} * 100",
			siblings: map[string]float64{"tasks_completed": 42, "tasks_total": 100},
			want:     42,
		},
		{
			name:     "spaces in names",
			formula:  "${tasks completed} / ${tasks total} * 100",
			siblings: map[string]float64{"tasks completed": 3, "tasks total": 12},
			want:     25,
		},
		{
			name:     "min helper",
			formula:  "min(${a}, ${b})",
			siblings: map[string]float64{"a": 5, "b": 3},
			want:     3,
		},
		{
			name:     "missing sibling defaults to 0",
			formula:  "${never_declared} + 10",
			siblings: map[string]float64{},
			want:     10,
		},
		{
			name:     "arithmetic only",
			formula:  "2 + 3 * 4",
			siblings: map[string]float64{},
			want:     14,
		},
		{
			name:     "pow",
			formula:  "pow(${base}, 2)",
			siblings: map[string]float64{"base": 4},
			want:     16,
		},
		{
			name:     "divide by zero → Inf → error",
			formula:  "${a} / ${b}",
			siblings: map[string]float64{"a": 1, "b": 0},
			wantErr:  true,
		},
		{
			name:    "empty formula",
			formula: "",
			wantErr: true,
		},
		{
			name:    "syntax error",
			formula: "${a} + ",
			wantErr: true,
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got, err := Evaluate(tc.formula, tc.siblings)
			if tc.wantErr {
				if err == nil {
					t.Fatalf("expected error, got nil (value=%v)", got)
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if math.Abs(got-tc.want) > 1e-9 {
				t.Errorf("got %v, want %v", got, tc.want)
			}
		})
	}
}

func TestSanitizeIdent(t *testing.T) {
	cases := map[string]string{
		"tasks_completed":    "tasks_completed",
		"tasks completed":    "tasks_completed",
		"A/B Score":          "A_B_Score",
		"42metric":           "_42metric",
		"  leading trailing": "leading_trailing",
		"":                   "_",
	}
	for in, want := range cases {
		if got := sanitizeIdent(in); got != want {
			t.Errorf("sanitizeIdent(%q) = %q, want %q", in, got, want)
		}
	}
}
