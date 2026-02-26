package constants

type contextKey string

var ContextKeys = struct {
	AUTH_DATA             contextKey
	VALIDATOR             contextKey
	VALIDATION_TRANSLATOR contextKey
	ACCEPT_LANGUAGE       contextKey
	DB                    contextKey
	REPORT_COLUMNS        contextKey
}{
	AUTH_DATA:             "AuthData",
	VALIDATOR:             "Validator",
	VALIDATION_TRANSLATOR: "ValidationTranslator",
	ACCEPT_LANGUAGE:       "AcceptLanguage",
	DB:                    "DB",
	// REPORT_COLUMNS holds a map[string]bool of requested column names.
	// The repository uses it to skip enrichment queries for unused columns.
	REPORT_COLUMNS: "ReportColumns",
}
