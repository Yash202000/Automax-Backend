package utils

import (
	"fmt"
	"time"
)

// ResolveTimezone returns the *time.Location for the given IANA timezone name.
// Falls back to UTC if the timezone is empty or invalid.
func ResolveTimezone(tz string) *time.Location {
	if tz == "" {
		return time.UTC
	}
	loc, err := time.LoadLocation(tz)
	if err != nil {
		return time.UTC
	}
	return loc
}

func parseTimeFlexible(t string) (time.Time, error) {
	formats := []string{
		time.RFC3339,                              // 2006-01-02T15:04:05Z07:00
		"2006-01-02T15:04:05.999999999Z07:00",     // RFC3339 with nanoseconds
		"2006-01-02 15:04:05.999999999-07:00",     // PostgreSQL with colon offset e.g. +05:30
		"2006-01-02 15:04:05.999999999-07",        // PostgreSQL short offset e.g. +00
		"2006-01-02 15:04:05-07:00",               // without nanoseconds, colon offset
		"2006-01-02 15:04:05-07",                  // without nanoseconds, short offset
		"2006-01-02 15:04:05.999999999 -0700 MST", // Go time.Time string → "2026-01-19 14:13:00.969689 +0530 IST"
		"2006-01-02 15:04:05 -0700 MST",           // same without nanoseconds
		"01-02-2006 03:04:05 PM",                  // custom format
	}

	for _, layout := range formats {
		if parsed, err := time.Parse(layout, t); err == nil {
			return parsed, nil
		}
	}
	return time.Time{}, fmt.Errorf("unsupported time format: %s", t)
}

func CalculateDuration(start, end string) (string, error) {
	if start == "" {
		return "", fmt.Errorf("start time is empty")
	}

	startTime, err := parseTimeFlexible(start)
	if err != nil {
		return "", fmt.Errorf("invalid start time format: %v", err)
	}

	var endTime time.Time
	if end != "" {
		endTime, err = parseTimeFlexible(end)
		if err != nil {
			return "", fmt.Errorf("invalid end time format: %v", err)
		}
	} else {
		endTime = time.Now()
	}

	if endTime.Before(startTime) {
		return "0s", nil
	}

	duration := endTime.Sub(startTime)
	days := int(duration.Hours()) / 24
	hours := int(duration.Hours()) % 24
	minutes := int(duration.Minutes()) % 60
	seconds := int(duration.Seconds()) % 60

	switch {
	case days > 0:
		return fmt.Sprintf("%dd %dh %dm", days, hours, minutes), nil
	case hours > 0:
		return fmt.Sprintf("%dh %dm", hours, minutes), nil
	case minutes > 0:
		return fmt.Sprintf("%dm %ds", minutes, seconds), nil
	default:
		return fmt.Sprintf("%ds", seconds), nil
	}
}
