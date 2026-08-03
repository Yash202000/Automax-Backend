package handlers

// normalizeOutcome maps a Cintrix call outcome to the CallLog.Status vocabulary
// the frontend renders. "answered"/"completed" are the only "success" states;
// everything else (missed, abandoned, failed, empty, unknown) is a non-answer.
func normalizeOutcome(raw string) string {
	switch raw {
	case "answered", "completed":
		return "completed"
	default:
		return "missed"
	}
}
