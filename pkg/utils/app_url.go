package utils

import (
	"context"

	"github.com/automax/backend/pkg/constants"
)

// Generate App URL from context and id
func GenerateAppURL(ctx context.Context) string {
	hostname, _ := ctx.Value(constants.ContextKeys.HOSTNAME).(string)
	protocol, _ := ctx.Value(constants.ContextKeys.PROTOCOL).(string)

	url := protocol + "://" + hostname
	return url
}
