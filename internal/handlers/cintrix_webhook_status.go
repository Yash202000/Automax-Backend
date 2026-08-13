package handlers

import (
	"strings"

	"github.com/google/uuid"
)

// isOutboundDirection reports whether a Cintrix call-event `direction` denotes an
// agent-originated (outbound) call, for which the dialed party — not the caller —
// is the "other party" shown to the agent.
func isOutboundDirection(direction string) bool {
	switch strings.ToLower(strings.TrimSpace(direction)) {
	case "outbound", "outgoing":
		return true
	default:
		return false
	}
}

// reconcileDirectCallAgent corrects Cintrix's wrong-leg agent resolution on
// direct softphone-to-softphone calls. Cintrix resolves the answering leg from
// the bridge, which for a direct call can mis-resolve to the CALLER's own leg —
// yielding agent_email == the caller. A participant can't be both the initiator
// and the recipient of the same call, so when the resolved agent equals the
// caller we substitute the dialed callee (from the DID) as the real answering
// party. Without this the caller is added as both initiator and recipient,
// collapsing the call's perspective so every viewer sees the caller's side
// (outgoing) and the "other party" resolves back to the caller.
//
// Queue/IVR calls are unaffected: there the agent extension differs from the
// external caller, so the equality never holds. When the agent is unresolved
// (missed calls) it is returned unchanged, and if the DID didn't resolve to a
// user the substitution yields an empty identifier (no recipient), which is
// still better than a self-recipient.
func reconcileDirectCallAgent(agentIdentifier, callerNumber, dialedIdentifier string, agentUserID, dialedUserID *uuid.UUID) (string, *uuid.UUID) {
	if agentIdentifier != "" && agentIdentifier == callerNumber {
		return dialedIdentifier, dialedUserID
	}
	return agentIdentifier, agentUserID
}

// normalizeOutcome maps a Cintrix call outcome to the CallLog.Status vocabulary
// the frontend renders. "answered"/"completed" are the only "success" states;
// everything else (missed, abandoned, failed, empty, unknown) is a non-answer.
func normalizeOutcome(raw string) string {
	switch raw {
	case "answered", "completed":
		return "completed"
	case "failed":
		// An agent-originated dial the far end never answered. Kept distinct from
		// "missed": missed means a call came IN and the agent didn't take it, so
		// labelling a failed OUTGOING dial "Missed" (with the incoming
		// missed-call icon) misreads who didn't answer whom. The call-history UI
		// already styles "failed" separately (red, PhoneOff icon).
		return "failed"
	default:
		// missed / abandoned / voicemail / transferred / unknown: an inbound call
		// nobody took. "abandoned" deliberately lands here — the UI has no style
		// for it and would render it unstyled.
		return "missed"
	}
}
