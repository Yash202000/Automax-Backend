package utils

import (
	"context"
	"fmt"
	"net/url"
	"time"

	"github.com/automax/backend/pkg/constants"
)

// Generate App URL from context and id
func GenerateAppURL(ctx context.Context) string {
	hostname, _ := ctx.Value(constants.ContextKeys.HOSTNAME).(string)
	protocol, _ := ctx.Value(constants.ContextKeys.PROTOCOL).(string)

	url := protocol + "://" + hostname
	return url
}

// When building the SMS link, encode the token
func BuildSMSLink(ctx context.Context, incidentID string, t time.Duration) string {
	baseURL := GenerateAppURL(ctx)
	token := GenerateIncidentToken(incidentID, t)
	return fmt.Sprintf("%s/ivr/incident/sms-link/%s?signed_token=%s",
		baseURL,
		incidentID,
		url.QueryEscape(token), // encodes | → %7C
	)
}
