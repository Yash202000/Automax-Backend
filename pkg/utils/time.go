package utils

import (
	"fmt"
	"time"
)

func parseTimeFlexible(t string) (time.Time, error) {
	// 1. Try RFC3339
	if parsed, err := time.Parse(time.RFC3339, t); err == nil {
		return parsed, nil
	}

	// 2. Try your custom format: "09-13-2025 09:32:09 AM"
	customLayout := "01-02-2006 03:04:05 PM"
	if parsed, err := time.Parse(customLayout, t); err == nil {
		return parsed, nil
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
