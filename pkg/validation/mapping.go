// validation/blog_validators.go
package validation

import (
	ut "github.com/go-playground/universal-translator"
	"github.com/go-playground/validator/v10"
)

// Messages per locale
var messages = map[string]map[string]string{
	"en": {
		"slug":         "{0} must be a valid slug (lowercase letters, numbers, and hyphens only)",
		"notblank":     "{0} cannot be blank",
		"blog_status":  "{0} must be one of: draft, pending, approved, published, archived",
		"alpha_spaces": "{0} can only contain letters and spaces",
		"no_spaces":    "{0} cannot contain spaces",
		"required":     "{0} is required",
		"email":        "{0} must be a valid email address",
		"min":          "{0} must be at least {1} characters",
		"max":          "{0} must be at most {1} characters",
		"uuid":         "{0} must be a valid UUID",
		"title":        "{0} must be a valid title (letters, numbers, spaces, and basic punctuation only)",
		"name":         "{0} can only contain letters, numbers, spaces, and basic punctuation (- ' . , & ( ) /)",
		"startswith12": "{0} must start with 1 or 2",
	},
	"fr": {
		"slug":         "{0} doit être un slug valide (lettres minuscules, chiffres et tirets uniquement)",
		"notblank":     "{0} ne peut pas être vide",
		"blog_status":  "{0} doit être l'un des: draft, pending, approved, published, archived",
		"alpha_spaces": "{0} ne peut contenir que des lettres et des espaces",
		"no_spaces":    "{0} ne peut pas contenir d'espaces",
		"required":     "{0} est obligatoire",
		"email":        "{0} doit être une adresse e-mail valide",
		"min":          "{0} doit contenir au moins {1} caractères",
		"max":          "{0} doit contenir au plus {1} caractères",
	},
	"ar": {
		"slug":         "{0} يجب أن يكون slug صالحاً (أحرف صغيرة وأرقام وشرطات فقط)",
		"notblank":     "{0} لا يمكن أن يكون فارغاً",
		"blog_status":  "{0} يجب أن يكون أحد: draft, pending, approved, published, archived",
		"alpha_spaces": "{0} يمكن أن يحتوي على أحرف ومسافات فقط",
		"no_spaces":    "{0} لا يمكن أن يحتوي على مسافات",
		"required":     "{0} مطلوب",
		"email":        "{0} يجب أن يكون بريداً إلكترونياً صالحاً",
		"min":          "{0} يجب أن يحتوي على {1} أحرف على الأقل",
		"max":          "{0} يجب أن لا يتجاوز {1} أحرف",
		"uuid":         "{0} يجب أن يكون UUID صالحاً",
		"title":        "{0} يجب أن يكون عنواناً صالحاً (أحرف وأرقام ومسافات وعلامات ترقيم أساسية فقط)",
		"name":         "{0} يمكن أن يحتوي على أحرف وأرقام ومسافات وعلامات ترقيم أساسية فقط (- ' . , & ( ) /)",
		"startswith12": "{0} يجب أن يبدأ بـ 1 أو 2",
	},
}

func registerCustomValidators(validate *validator.Validate, trans ut.Translator, lang string) {
	msgs, ok := messages[lang]
	if !ok {
		msgs = messages["en"] // fallback
	}

	registerCustom := func(tag string, fn validator.Func, hasParam bool) {
		// Only register the function once (first locale call handles this)
		validate.RegisterValidation(tag, fn, true)

		msg := msgs[tag]
		validate.RegisterTranslation(tag, trans,
			func(ut ut.Translator) error {
				return ut.Add(tag, msg, true)
			},
			func(ut ut.Translator, fe validator.FieldError) string {
				t, _ := ut.T(tag, fe.Field(), fe.Param())
				return t
			},
		)
	}

	registerCustom("slug", validateSlug1, false)
	registerCustom("notblank", validateNotBlank, false)
	registerCustom("blog_status", validateBlogStatus, false)
	registerCustom("alpha_spaces", validateAlphaAndSpaces, false)
	registerCustom("no_spaces", validateNoSpaces, false)
	registerCustom("title", validateTitle, false)
	registerCustom("name", validateName, false)
	registerCustom("startswith12", validateStartswith12, false)

	// Override default validator translations
	for _, tag := range []string{"required", "email", "min", "max"} {
		msg := msgs[tag]
		validate.RegisterTranslation(tag, trans,
			func(ut ut.Translator) error {
				return ut.Add(tag, msg, true)
			},
			func(ut ut.Translator, fe validator.FieldError) string {
				t, _ := ut.T(tag, fe.Field(), fe.Param())
				return t
			},
		)
	}
}
