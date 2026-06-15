package validation

import (
	"regexp"
	"strings"
	"unicode"

	ut "github.com/go-playground/universal-translator"
	"github.com/go-playground/validator/v10"
)

// Example validator funcs
func validateNotBlank(fl validator.FieldLevel) bool {
	return len(fl.Field().String()) > 0
}

// validateBlogStatus ensures status is valid
func validateAlphaAndSpaces(fl validator.FieldLevel) bool {
	for _, r := range fl.Field().String() {
		if !unicode.IsLetter(r) && r != ' ' {
			return false
		}
	}
	return true
}

// validateTitle ensures title is valid (letters, numbers, spaces, and basic punctuation only)
func validateTitle(fl validator.FieldLevel) bool {
	for _, r := range fl.Field().String() {
		if !unicode.IsLetter(r) && !unicode.IsNumber(r) && r != ' ' && r != '.' && r != ',' && r != '!' && r != '?' {
			return false
		}
	}
	return true
}

// validateSlug ensures slug is lowercase alphanumeric with hyphens
func validateSlug1(fl validator.FieldLevel) bool {
	regex := regexp.MustCompile(`^[a-z0-9]+(?:-[a-z0-9]+)*$`)
	slug, ok := fl.Field().Interface().(string)
	if !ok {
		return false
	}
	return regex.MatchString(slug)
}

// validateNoSpaces ensures there is no space in the field

func validateNoSpaces(fl validator.FieldLevel) bool {
	regex := regexp.MustCompile(`^\S*$`)
	str, ok := fl.Field().Interface().(string)
	if !ok {
		return false
	}
	return regex.MatchString(str)
}

// validateBlogStatus ensures status is valid
func validateBlogStatus(fl validator.FieldLevel) bool {
	status, ok := fl.Field().Interface().(string)
	if !ok {
		return false
	}
	validStatuses := []string{"draft", "pending", "approved", "published", "archived", "deleted"}
	for _, s := range validStatuses {
		if status == s {
			return true
		}
	}
	return false
}
var nameRegex = regexp.MustCompile(`^[\p{L}0-9\s\-'.,&()/]+$`)

// validateName ensures the name contains at least one letter and only letters, numbers, spaces, and common punctuation
func validateName(fl validator.FieldLevel) bool {
	s := fl.Field().String()
	if !nameRegex.MatchString(s) {
		return false
	}
	for _, r := range s {
		if unicode.IsLetter(r) {
			return true
		}
	}
	return false
}

func GetTranslator(lang string) ut.Translator {
	// Normalize: "en-US" -> "en", "fr-FR" -> "fr"
	lang = strings.ToLower(strings.Split(lang, "-")[0])

	trans, found := uni.GetTranslator(lang)
	if !found {
		trans, _ = uni.GetTranslator("en") // fallback
	}
	return trans
}
