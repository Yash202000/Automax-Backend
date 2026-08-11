package utils

import (
	"os"
	"strings"
)

// countryCode pairs an international dialing code with the number of digits that
// follow it in a complete number.
type countryCode struct {
	Code     string
	LocalLen int
}

// knownCountryCodes are tried in order, longest code first, so matching is
// deterministic and a longer code is never shadowed by a shorter prefix of it.
var knownCountryCodes = []countryCode{
	{Code: "966", LocalLen: 9}, // Saudi Arabia
	{Code: "91", LocalLen: 10}, // India
}

// defaultCountryCode is used when COUNTRY_CODE is unset, matching the default in
// internal/config.Config.CountryCode.
const defaultCountryCode = "+966"

// SystemCountryCode returns the COUNTRY_CODE env var (e.g. "+966"). It exists so
// callers with no access to config.Config — repositories, model mappers — resolve
// the same value the configured services use, rather than a second hardcoded one.
func SystemCountryCode() string {
	if code := strings.TrimSpace(os.Getenv("COUNTRY_CODE")); code != "" {
		return code
	}
	return defaultCountryCode
}

// stripInternationalPrefix removes a leading "+" or "00" from an already-trimmed
// number, leaving bare digits.
func stripInternationalPrefix(mobile string) string {
	if strings.HasPrefix(mobile, "+") {
		return strings.TrimPrefix(mobile, "+")
	}
	return strings.TrimPrefix(mobile, "00")
}

// splitCountryCode separates a known country code from the national number that
// follows it, discarding the national trunk zero. It reports false when no code
// matches.
//
// The length check is what makes this safe: a bare national number can
// legitimately begin with another country's code — an Indian mobile may start
// "966" — so a code is only accepted when what follows it is exactly as long as
// that country's numbers are.
func splitCountryCode(digits string) (code, local string, ok bool) {
	for _, cc := range knownCountryCodes {
		if !strings.HasPrefix(digits, cc.Code) {
			continue
		}
		local := strings.TrimLeft(strings.TrimPrefix(digits, cc.Code), "0")
		if len(local) == cc.LocalLen {
			return cc.Code, local, true
		}
	}
	return "", "", false
}

// localLenFor returns the expected national-number length for a country code
// written in either "+966" or "966" form.
func localLenFor(systemCountryCode string) (int, bool) {
	digits := stripInternationalPrefix(strings.TrimSpace(systemCountryCode))
	for _, cc := range knownCountryCodes {
		if cc.Code == digits {
			return cc.LocalLen, true
		}
	}
	return 0, false
}

// NormalizeMobile renders a mobile number in E.164 form, e.g. "+966565818990".
//
// Leading zeros are always discarded: a national trunk zero is a dialling prefix,
// not part of the number, and gluing it behind a country code produces an invalid
// address that carriers reject. A "+" or "00" international prefix is understood.
//
// A number that already carries a recognizable country code keeps that code —
// only a number with no code at all is assumed to belong to systemCountryCode.
// When the value has no code and does not match the system country's national
// length, it is returned unchanged rather than being given a country code it
// probably does not belong to; the caller can then reject it instead of sending to
// an address this function invented.
func NormalizeMobile(mobile, systemCountryCode string) string {
	trimmed := strings.TrimSpace(mobile)
	if trimmed == "" {
		return mobile
	}

	digits := stripInternationalPrefix(trimmed)

	if code, local, ok := splitCountryCode(digits); ok {
		return "+" + code + local
	}

	local := strings.TrimLeft(digits, "0")
	if localLen, ok := localLenFor(systemCountryCode); ok && len(local) == localLen {
		return systemCountryCode + local
	}

	return trimmed
}

// NationalMobile renders a mobile number the way it is dialled domestically:
// country code removed, exactly one leading zero. "+966565818990",
// "+9660565818990" and "565818990" all become "0565818990".
//
// Values that carry no digits at all — the literal "N/A" that appears in incident
// data, or a blank — are returned untouched, so display never invents a phone
// number out of a placeholder.
func NationalMobile(mobile string) string {
	trimmed := strings.TrimSpace(mobile)
	if !hasDigit(trimmed) {
		return mobile
	}

	digits := stripInternationalPrefix(trimmed)

	local := ""
	if _, stripped, ok := splitCountryCode(digits); ok {
		local = stripped
	} else {
		local = strings.TrimLeft(digits, "0")
	}
	if local == "" {
		return mobile
	}

	return "0" + local
}

// MobileMatchVariants returns every shape a number may have been stored in, so an
// exact comparison finds it regardless of the channel that captured it.
//
// Covered: the value as supplied, the bare national number, the national number
// with its trunk zero, and — for both the system country code and any code found
// in the input — the "+CC", "CC", "+CC0" and "CC0" forms. The last two look wrong
// because they are: historic rows were written by a NormalizeMobile that kept the
// trunk zero behind the country code, and those rows must stay findable.
func MobileMatchVariants(supplied string) []string {
	trimmed := strings.TrimSpace(supplied)
	if trimmed == "" {
		return []string{supplied}
	}

	variants := []string{trimmed}
	add := func(v string) {
		if v == "" {
			return
		}
		for _, existing := range variants {
			if existing == v {
				return
			}
		}
		variants = append(variants, v)
	}

	digits := stripInternationalPrefix(trimmed)
	add(digits)

	// Resolve the national number, plus the country code it belongs to if the
	// input carried one.
	codes := map[string]struct{}{}
	local := ""
	if code, stripped, ok := splitCountryCode(digits); ok {
		local, codes[code] = stripped, struct{}{}
	} else {
		local = strings.TrimLeft(digits, "0")
	}
	if local == "" {
		return variants
	}

	// A number supplied without a country code may still be stored with one, so
	// every code whose national numbers are this long is a candidate — not just
	// the system country's. Without this, an Indian number supplied as
	// "09454578502" would never generate the "+919454578502" that is stored.
	for _, cc := range knownCountryCodes {
		if cc.LocalLen == len(local) {
			codes[cc.Code] = struct{}{}
		}
	}

	// The system country code is always a candidate too: rows written by the
	// historic NormalizeMobile carry it regardless of the number's real length.
	if sysDigits := stripInternationalPrefix(SystemCountryCode()); sysDigits != "" {
		codes[sysDigits] = struct{}{}
	}

	add(local)
	add("0" + local)
	for code := range codes {
		add("+" + code + local)
		add(code + local)
		// Legacy shapes written before the trunk zero was stripped.
		add("+" + code + "0" + local)
		add(code + "0" + local)
	}

	return variants
}

// hasDigit reports whether s contains at least one ASCII digit.
func hasDigit(s string) bool {
	for _, r := range s {
		if r >= '0' && r <= '9' {
			return true
		}
	}
	return false
}

// HasCountryCode reports whether mobile is already written with an international
// dialing code — either an explicit "+" or "00" prefix, or a bare known country
// code followed by exactly the expected number of local digits.
func HasCountryCode(mobile string) bool {
	trimmed := strings.TrimSpace(mobile)
	if strings.HasPrefix(trimmed, "+") || strings.HasPrefix(trimmed, "00") {
		return true
	}
	_, _, ok := splitCountryCode(trimmed)
	return ok
}
