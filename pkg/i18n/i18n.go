package i18n

import (
	"context"
	"fmt"

	"github.com/automax/backend/pkg/constants"
)

const (
	langEN = 0
	langAR = 1
)

func langIndex(ctx context.Context) int {
	lang, _ := ctx.Value(constants.ContextKeys.ACCEPT_LANGUAGE).(string)
	if lang == "ar" {
		return langAR
	}
	return langEN
}

// T returns the translated string for key based on Accept-Language in ctx.
// Falls back to English, then to the key itself if no entry found.
func T(ctx context.Context, key string) string {
	if pair, ok := messages[key]; ok {
		idx := langIndex(ctx)
		if idx < len(pair) && pair[idx] != "" {
			return pair[idx]
		}
		if pair[langEN] != "" {
			return pair[langEN]
		}
	}
	return key
}

// Tf returns the translated string with fmt.Sprintf formatting applied.
func Tf(ctx context.Context, key string, args ...interface{}) string {
	return fmt.Sprintf(T(ctx, key), args...)
}
