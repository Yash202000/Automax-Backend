package utils

import (
	"strings"

	"github.com/google/uuid"
)

// ParseUUIDList parses a comma-separated string of UUIDs, trimming whitespace
// and silently skipping any entries that fail to parse.
func ParseUUIDList(raw string) []uuid.UUID {
	if raw == "" {
		return nil
	}
	parts := strings.Split(raw, ",")
	result := make([]uuid.UUID, 0, len(parts))
	for _, p := range parts {
		p = strings.TrimSpace(p)
		if id, err := uuid.Parse(p); err == nil {
			result = append(result, id)
		}
	}
	return result
}
