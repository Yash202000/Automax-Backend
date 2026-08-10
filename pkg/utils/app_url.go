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

// Generate App URL from context and id
func GenerateAttachmentAppURL(ctx context.Context, attachmentID uuid.UUID) string {
	hostname, _ := ctx.Value(constants.ContextKeys.HOSTNAME).(string)
	protocol, _ := ctx.Value(constants.ContextKeys.PROTOCOL).(string)
	token, _ := ctx.Value(constants.ContextKeys.Token).(string)

	url := protocol + "://" + hostname + "/api/v1/calls/" + fmt.Sprint(attachmentID) + "/preview?token=" + token
	return url
}

// GenerateCTIRecordingAppURL builds the token-bearing URL the frontend uses to
// play/download a Cintrix-recorded call's audio, proxied via
// GET /api/v1/cti/recording. Same "?token=" query-param auth pattern as
// GenerateAttachmentAppURL, so it works directly in <audio src> / <a href>.
func GenerateCTIRecordingAppURL(ctx context.Context, callUuid string) string {
	hostname, _ := ctx.Value(constants.ContextKeys.HOSTNAME).(string)
	protocol, _ := ctx.Value(constants.ContextKeys.PROTOCOL).(string)
	token, _ := ctx.Value(constants.ContextKeys.Token).(string)

	return protocol + "://" + hostname + "/api/v1/cti/recording?call_uuid=" +
		url.QueryEscape(callUuid) + "&token=" + url.QueryEscape(token)
}
