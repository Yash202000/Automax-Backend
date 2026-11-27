package models

import (
	"fmt"
	"regexp"
	"strconv"
	"strings"
)

var durationLabelRe = regexp.MustCompile(`(?i)^(\d+)\s*(hour|day|week|month|year)s?$`)

// Arabic forms per unit: singular, dual, plural (3-10), accusative singular (11+).
var arDurationForms = map[string][4]string{
	"hour":  {"ساعة واحدة", "ساعتان", "ساعات", "ساعة"},
	"day":   {"يوم واحد", "يومان", "أيام", "يومًا"},
	"week":  {"أسبوع واحد", "أسبوعان", "أسابيع", "أسبوعًا"},
	"month": {"شهر واحد", "شهران", "أشهر", "شهرًا"},
	"year":  {"سنة واحدة", "سنتان", "سنوات", "سنة"},
}

// LocalizeDurationLabelAr turns "2 Days" into Arabic; unknown labels are returned unchanged.
func LocalizeDurationLabelAr(label string) string {
	m := durationLabelRe.FindStringSubmatch(strings.TrimSpace(label))
	if m == nil {
		return label
	}
	n, _ := strconv.Atoi(m[1])
	f := arDurationForms[strings.ToLower(m[2])]
	switch {
	case n == 1:
		return f[0]
	case n == 2:
		return f[1]
	case n >= 3 && n <= 10:
		return fmt.Sprintf("%d %s", n, f[2])
	default:
		return fmt.Sprintf("%d %s", n, f[3])
	}
}

// NormalizeDurationLabel converts an Arabic label back to the canonical English one
// ("يومان" -> "2 Days"). English/unknown labels are returned unchanged.
func NormalizeDurationLabel(label string) string {
	label = strings.TrimSpace(label)
	for unit, f := range arDurationForms {
		for n, phrase := range map[int]string{1: f[0], 2: f[1]} {
			if label == phrase {
				return englishDuration(n, unit)
			}
		}
		for _, form := range f[2:] {
			if strings.HasSuffix(label, " "+form) {
				if n, err := strconv.Atoi(strings.TrimSpace(strings.TrimSuffix(label, form))); err == nil {
					return englishDuration(n, unit)
				}
			}
		}
	}
	return label
}

func englishDuration(n int, unit string) string {
	s := fmt.Sprintf("%d %s", n, strings.ToUpper(unit[:1])+unit[1:])
	if n != 1 {
		s += "s"
	}
	return s
}

// NormalizeDurationLabels normalizes every label (see NormalizeDurationLabel).
func NormalizeDurationLabels(in []string) []string {
	out := make([]string, len(in))
	for i, l := range in {
		out[i] = NormalizeDurationLabel(l)
	}
	return out
}
