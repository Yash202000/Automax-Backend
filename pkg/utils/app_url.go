package utils

import (
	"context"
	"fmt"
	"net/url"
	"time"

	"github.com/automax/backend/pkg/constants"
	"github.com/google/uuid"
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

// Generate App URL from context and id
func GenerateAttachmentAppURL(ctx context.Context, attachmentID uuid.UUID) string {
	hostname, _ := ctx.Value(constants.ContextKeys.HOSTNAME).(string)
	protocol, _ := ctx.Value(constants.ContextKeys.PROTOCOL).(string)
	token, _ := ctx.Value(constants.ContextKeys.Token).(string)

	url := protocol + "://" + hostname + "/api/v1/calls/" + fmt.Sprint(attachmentID) + "/preview?token=" + token
	return url
}
