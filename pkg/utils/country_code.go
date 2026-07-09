package utils

import "strings"

func NormalizeMobile(mobile, systemCountryCode string) string {
	mobile = strings.TrimSpace(mobile)
	mobile = strings.TrimPrefix(mobile, "+")

	// code -> expected local number length (digits after the code)
	knownCountryCodes := map[string]int{
		"91":  10, // India
		"966": 9,  // Saudi Arabia
	}

	for code, localLen := range knownCountryCodes {
		if strings.HasPrefix(mobile, code) && len(mobile) == len(code)+localLen {
			mobile = strings.TrimPrefix(mobile, code)
			break
		}
	}

	return systemCountryCode + mobile
}
