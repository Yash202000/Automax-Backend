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

// RequestBaseURL returns "protocol://hostname" of the current request, taken from the context
// the RequestContext middleware fills. It returns "" when the context has no request (background
// jobs, tests), so callers fall back to relative paths instead of building "://".
func RequestBaseURL(ctx context.Context) string {
	hostname, _ := ctx.Value(constants.ContextKeys.HOSTNAME).(string)
	protocol, _ := ctx.Value(constants.ContextKeys.PROTOCOL).(string)
	if hostname == "" || protocol == "" {
		return ""
	}
	return protocol + "://" + hostname
}

// When building the SMS link, encode the token
func BuildSMSLink(ctx context.Context, incidentID string, t time.Duration) string {
	rawToken := GenerateIncidentToken(incidentID, t)
	return BuildSMSLinkFromToken(ctx, incidentID, rawToken)
}

// BuildSMSLinkFromToken builds the SMS link from a pre-generated raw token.
// Use this when you need to also store the token hash before building the URL.
func BuildSMSLinkFromToken(ctx context.Context, incidentID string, rawToken string) string {
	baseURL := GenerateAppURL(ctx)
	return fmt.Sprintf("%s/ivr/incident/sms-link/%s?signed_token=%s",
		baseURL,
		incidentID,
		url.QueryEscape(rawToken), // encodes | → %7C
	)
}

// BuildFeedbackLink builds the public URL a reporter uses to submit their feedback.
func BuildFeedbackLink(ctx context.Context, incidentID, feedbackID string, duration time.Duration) string {
	baseURL := GenerateAppURL(ctx)
	token := GenerateFeedbackToken(feedbackID, incidentID, duration)
	return fmt.Sprintf("%s/api/v1/public-feedback/%s/submit?signed_token=%s",
		baseURL,
		incidentID,
		url.QueryEscape(token),
	)
}
