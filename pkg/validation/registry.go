package validation

// package validators

import (
	ar "github.com/go-playground/locales/ar"
	en "github.com/go-playground/locales/en"
	ut "github.com/go-playground/universal-translator"

	// ut "github.com/go-playground/universal-translator"
	// utpkg "github.com/go-playground/universal-translator"
	"github.com/go-playground/validator/v10"
	ar_translations "github.com/go-playground/validator/v10/translations/ar"
	en_translations "github.com/go-playground/validator/v10/translations/en"
)

var (
	validate *validator.Validate
	TransEn  ut.Translator
	TransAr  ut.Translator
	Trans    ut.Translator // Default translator (English)
	uni      *ut.UniversalTranslator
)

func InitValidatorRegistry() {
	// Initialize locales
	enLocale := en.New()
	arLocale := ar.New()

	// Create universal translator with English as fallback
	uni = ut.New(enLocale, enLocale, arLocale)

	// Get translators
	TransEn, _ = uni.GetTranslator("en")
	TransAr, _ = uni.GetTranslator("ar")
	Trans = TransEn // Default to English

	// Create validator
	validate = validator.New()

	// Register default translations for both languages
	en_translations.RegisterDefaultTranslations(validate, TransEn)
	ar_translations.RegisterDefaultTranslations(validate, TransAr)
	// Register custom validators for both languages
	// registerAuthValidators(Validate, TransEn)
	// registerContactValidators(Validate, TransEn)

	// registerAuthValidators(Validate, TransAr)
	// registerContactValidators(Validate, TransAr)
	registerCustomValidators(validate, TransEn, "en")
	registerCustomValidators(validate, TransAr, "ar")
}
