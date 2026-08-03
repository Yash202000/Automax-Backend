package handlers

import "testing"

func TestNormalizeOutcome(t *testing.T) {
	cases := map[string]string{
		"answered":  "completed",
		"completed": "completed",
		"missed":    "missed",
		"abandoned": "missed",
		"failed":    "missed",
		"":          "missed",
		"weird":     "missed",
	}
	for in, want := range cases {
		if got := normalizeOutcome(in); got != want {
			t.Errorf("normalizeOutcome(%q) = %q, want %q", in, got, want)
		}
	}
}
