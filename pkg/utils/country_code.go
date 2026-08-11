package utils

import "strings"

// countryCodeLocalLen maps an international dialing code to the number of digits
// that follow it in a complete number. Shared by NormalizeMobile and
// HasCountryCode so both agree on what counts as a country code.
var countryCodeLocalLen = map[string]int{
	"91":  10, // India
	"966": 9,  // Saudi Arabia
}

func NormalizeMobile(mobile, systemCountryCode string) string {
	mobile = strings.TrimSpace(mobile)
	mobile = strings.TrimPrefix(mobile, "+")

	for code, localLen := range countryCodeLocalLen {
		if strings.HasPrefix(mobile, code) && len(mobile) == len(code)+localLen {
			mobile = strings.TrimPrefix(mobile, code)
			break
		}
	}

	return systemCountryCode + mobile
}

// HasCountryCode reports whether mobile is already written with an international
// dialing code — either an explicit "+" or "00" prefix, or a bare known country
// code followed by exactly the expected number of local digits, as in
// "966501234567".
//
// The length check is what keeps a national number that merely happens to start
// with those digits (e.g. "0966123") from being misread as international.
func HasCountryCode(mobile string) bool {
	mobile = strings.TrimSpace(mobile)
	if strings.HasPrefix(mobile, "+") || strings.HasPrefix(mobile, "00") {
		return true
	}
	for code, localLen := range countryCodeLocalLen {
		if strings.HasPrefix(mobile, code) && len(mobile) == len(code)+localLen {
			return true
		}
	}
	return false
}

// LocalMobileWithLeadingZero returns mobile with a "0" prepended when it is a
// bare national number — one written with neither an international dialing code
// nor the leading trunk zero that national dialling normally uses. So
// "501234567" becomes "0501234567", while "0501234567", "+966501234567" and
// "966501234567" are all returned unchanged, as is a blank string.
//
// This exists because the same number reaches the system in all three shapes
// depending on the channel that captured it, so a value has to be compared
// against its equivalent forms rather than matched literally.
func LocalMobileWithLeadingZero(mobile string) string {
	mobile = strings.TrimSpace(mobile)
	if mobile == "" || strings.HasPrefix(mobile, "0") || HasCountryCode(mobile) {
		return mobile
	}
	return "0" + mobile
}
