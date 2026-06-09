package constants

type contextKey string

var ContextKeys = struct {
	AUTH_DATA             contextKey
	VALIDATOR             contextKey
	VALIDATION_TRANSLATOR contextKey
	ACCEPT_LANGUAGE       contextKey
	DB                    contextKey
	REPORT_COLUMNS        contextKey
	REPORT_DATA_SOURCE    contextKey
	REPORT_TIMEZONE       contextKey
	HOSTNAME              contextKey
	PROTOCOL              contextKey
	IP_ADDRESS            contextKey
	USER_AGENT            contextKey
	UserID                contextKey
	ActorID               contextKey
	Email                 contextKey
	UserEmail             contextKey
	Role                  contextKey
	Token                 contextKey
	SessionID             contextKey
	UserName              contextKey
	User                  contextKey
}{
	AUTH_DATA:             "AuthData",
	VALIDATOR:             "Validator",
	VALIDATION_TRANSLATOR: "ValidationTranslator",
	ACCEPT_LANGUAGE:       "AcceptLanguage",
	DB:                    "DB",
	// REPORT_COLUMNS holds a []models.ColumnField of requested columns.
	// The repository uses it to skip enrichment queries for unused columns.
	REPORT_COLUMNS: "ReportColumns",
	// REPORT_DATA_SOURCE holds the active data source string (e.g. "incidents",
	// "users") so applyFilters can select the correct allowed-fields map.
	REPORT_DATA_SOURCE: "ReportDataSource",
	REPORT_TIMEZONE:    "ReportTimezone",
	HOSTNAME:           "hostname",
	PROTOCOL:           "protocol",
	IP_ADDRESS:         "ip_address",
	USER_AGENT:         "user_agent",
	UserID:             "user_id",
	ActorID:            "actor_id",

	Email:     "email",
	UserEmail: "user_email",
	Role:      "role",
	Token:     "token",
	SessionID: "session_id",
	UserName:  "user_name",
	User:      "user",
}
