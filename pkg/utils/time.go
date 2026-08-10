package utils

import (
	"fmt"
	"os"
	"strconv"
	"time"
)

// ResolveTimezone returns the *time.Location for the given IANA timezone name.
// Fallback chain: tz param → APP_TIMEZONE env var → UTC.
func ResolveTimezone(tz string) *time.Location {
	if tz == "" {
		tz = os.Getenv("APP_TIMEZONE")
	}
	if tz == "" {
		return time.UTC
	}
	loc, err := time.LoadLocation(tz)
	if err != nil {
		return time.UTC
	}
	return loc
}

// ParseTimeFlexible parses a timestamp written in any of the layouts this codebase
// hands around - RFC3339, the several PostgreSQL text renderings, Go's time.Time
// String(), and the custom 12-hour form.
func ParseTimeFlexible(t string) (time.Time, error) {
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

// pluralize renders "1 hour" / "3 hours" — full words, since these strings are
// read by humans in a report cell rather than parsed.
func pluralize(n int64, unit string) string {
	if n == 1 {
		return fmt.Sprintf("%d %s", n, unit)
	}
	return fmt.Sprintf("%d %ss", n, unit)
}

// FormatMinutesDuration renders an elapsed-minute count in the largest unit that
// fits, with the remainder in the next unit down:
//
//	45   -> "45 minutes"
//	60   -> "1 hour"
//	200  -> "3 hours 20 minutes"
//	2880 -> "2 days"
//	5000 -> "3 days 11 hours"
//
// Anything under an hour stays in minutes, and 48 hours is where it switches to
// days. A negative count returns "" — the frtDerived clock-skew guard already
// prevents one, so it would mean the data is wrong, not that the duration is.
func FormatMinutesDuration(minutes int64) string {
	const (
		minutesPerHour = 60
		minutesPerDay  = 24 * minutesPerHour
		dayThreshold   = 48 * minutesPerHour
	)

	switch {
	case minutes < 0:
		return ""

	case minutes < minutesPerHour:
		return pluralize(minutes, "minute")

	case minutes < dayThreshold:
		hours, rem := minutes/minutesPerHour, minutes%minutesPerHour
		if rem == 0 {
			return pluralize(hours, "hour")
		}
		return pluralize(hours, "hour") + " " + pluralize(rem, "minute")

	default:
		days, rem := minutes/minutesPerDay, (minutes%minutesPerDay)/minutesPerHour
		if rem == 0 {
			return pluralize(days, "day")
		}
		return pluralize(days, "day") + " " + pluralize(rem, "hour")
	}
}

// FormatMinutesValue is the interface{} front door to FormatMinutesDuration, for
// callers holding a value scanned out of a database row. A NULL minute count
// yields "" rather than "0 minutes": "no first response yet" and "responded to
// instantly" are not the same thing.
func FormatMinutesValue(v interface{}) string {
	switch n := v.(type) {
	case nil:
		return ""
	case int64:
		return FormatMinutesDuration(n)
	case int:
		return FormatMinutesDuration(int64(n))
	case int32:
		return FormatMinutesDuration(int64(n))
	case float64:
		return FormatMinutesDuration(int64(n))
	case string:
		if n == "" {
			return ""
		}
		parsed, err := strconv.ParseInt(n, 10, 64)
		if err != nil {
			return ""
		}
		return FormatMinutesDuration(parsed)
	default:
		return ""
	}
}

func CalculateDuration(start, end string) (string, error) {
	if start == "" {
		return "", fmt.Errorf("start time is empty")
	}

	startTime, err := ParseTimeFlexible(start)
	if err != nil {
		return "", fmt.Errorf("invalid start time format: %v", err)
	}

	var endTime time.Time
	if end != "" {
		endTime, err = ParseTimeFlexible(end)
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
