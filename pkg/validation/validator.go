package validation

import (
	"context"

	"github.com/automax/backend/pkg/constants"
	"github.com/gofiber/fiber/v2/log"

	ut "github.com/go-playground/universal-translator"
	"github.com/go-playground/validator/v10"
)

// ============================================================================
// VALIDATION HELPER FUNCTIONS
// ============================================================================

// ValidateStruct validates a struct using validator from context
func ValidateStruct(ctx context.Context, s interface{}) map[string]string {
	// Check if validator is initialized
	if validate == nil {
		log.Error("Validator not initialized. Call InitValidatorRegistry() first")
		return map[string]string{"error": "Validator not initialized"}
	}

	// Get validator from context with safe type assertion
	lang, ok := ctx.Value(constants.ContextKeys.VALIDATION_LANGUAGE).(string)
	if lang == "" || !ok {
		log.Warn("Language not found in context, defaulting to 'en'")
		lang = "en"
	}
	trans := GetTranslator(lang)

	err := validate.StructCtx(ctx, s)
	if err == nil {
		return nil
	}

	validationErrs, ok := err.(validator.ValidationErrors)
	if !ok {
		// This might be an InvalidValidationError or other error type
		log.Errorf("Unexpected validation error type: %T - %v", err, err)
		return map[string]string{"validation_error": err.Error()}
	}

	errors := make(map[string]string)
	for _, err := range validationErrs {
		// Use field name without struct name prefix
		errors[err.Field()] = err.Translate(trans)
	}

	return errors
}

// ValidateStructSimple returns a simple error message
func ValidateStructSimple(ctx context.Context, s interface{}) error {
	validate := ctx.Value(constants.ContextKeys.VALIDATOR).(*validator.Validate)
	return validate.Struct(s)
}

// GetValidationErrors returns formatted error messages
func GetValidationErrors(ctx context.Context, err error) map[string]string {
	if err == nil {
		return nil
	}

	trans := ctx.Value(constants.ContextKeys.VALIDATION_TRANSLATOR).(ut.Translator)
	errors := make(map[string]string)

	if validationErrs, ok := err.(validator.ValidationErrors); ok {
		for _, e := range validationErrs {
			errors[e.Field()] = e.Translate(trans)
		}
	}

	return errors
}

// IsValidatorInitialized checks if the global validator is initialized
func IsValidatorInitialized() bool {
	return validate != nil
}
