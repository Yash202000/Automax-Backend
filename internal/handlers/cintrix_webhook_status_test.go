package handlers

import (
	"testing"

	"github.com/google/uuid"
)

func TestReconcileDirectCallAgent(t *testing.T) {
	caller := uuid.New()
	callee := uuid.New()

	t.Run("mis-resolved agent equals caller: substitute the dialed callee", func(t *testing.T) {
		gotID, gotUser := reconcileDirectCallAgent("1017", "1017", "1041", &caller, &callee)
		if gotID != "1041" {
			t.Errorf("identifier = %q, want %q", gotID, "1041")
		}
		if gotUser != &callee {
			t.Errorf("userID = %v, want the callee's id", gotUser)
		}
	})

	t.Run("agent equals caller but DID unresolved: no recipient", func(t *testing.T) {
		gotID, gotUser := reconcileDirectCallAgent("1017", "1017", "", &caller, nil)
		if gotID != "" {
			t.Errorf("identifier = %q, want empty", gotID)
		}
		if gotUser != nil {
			t.Errorf("userID = %v, want nil", gotUser)
		}
	})

	t.Run("queue/IVR call (agent differs from caller): unchanged", func(t *testing.T) {
		gotID, gotUser := reconcileDirectCallAgent("1044", "7034415345", "", &callee, nil)
		if gotID != "1044" {
			t.Errorf("identifier = %q, want %q", gotID, "1044")
		}
		if gotUser != &callee {
			t.Errorf("userID = %v, want the agent's id", gotUser)
		}
	})

	t.Run("unresolved agent (missed call): unchanged", func(t *testing.T) {
		gotID, gotUser := reconcileDirectCallAgent("", "1041", "1017", nil, &callee)
		if gotID != "" {
			t.Errorf("identifier = %q, want empty", gotID)
		}
		if gotUser != nil {
			t.Errorf("userID = %v, want nil", gotUser)
		}
	})
}

func TestNormalizeOutcome(t *testing.T) {
	cases := map[string]string{
		"answered":  "completed",
		"completed": "completed",
		"missed":    "missed",
		"abandoned": "missed", // inbound queue call nobody took; UI has no "abandoned" style
		// An agent's own dial that was never answered stays distinct from "missed",
		// which would render an OUTGOING failure as an incoming missed call.
		"failed": "failed",
		"":       "missed",
		"weird":  "missed",
	}
	for in, want := range cases {
		if got := normalizeOutcome(in); got != want {
			t.Errorf("normalizeOutcome(%q) = %q, want %q", in, got, want)
		}
	}
}
