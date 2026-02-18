package constants

type contextKey string

var ContextKeys = struct {
	AUTH_DATA             contextKey
	VALIDATOR             contextKey
	VALIDATION_TRANSLATOR contextKey
	VALIDATION_LANGUAGE   contextKey
	DB                    contextKey
}{
	AUTH_DATA:             "AuthData",
	VALIDATOR:             "Validator",
	VALIDATION_TRANSLATOR: "ValidationTranslator",
	VALIDATION_LANGUAGE:   "ValidationLanguage",
	DB:                    "DB",
}
