package services

import (
	"encoding/json"
	"log"
	"strconv"
	"strings"

	"github.com/automax/backend/internal/config"
	"github.com/automax/backend/pkg/constants"
)

// configDefault is one config row created at startup if it does not exist yet.
// Key is the internal "module.name" identifier; the table stores module and name separately.
type configDefault struct {
	Key         string
	ValueType   string // string | number | bool | json | array
	Description string
	Value       any // marshalled to JSON
}

// buildConfigDefaults lists the configs seeded for every environment. Static entries are fixed
// defaults; the rest take the value config.go resolved at startup (env var if set, else its
// default) so the table starts in sync with the running app. Secrets are never included.
func buildConfigDefaults(cfg *config.Config) []configDefault {
	iv := cfg.ImageValidation

	return []configDefault{
		{"app.maintenance_mode", "bool", "When true, the application shows a maintenance notice", false},
		{"app.default_language", "string", "Default UI language (en | ar)", "en"},
		{"auth.session_timeout_minutes", "number", "Idle session timeout in minutes", 60},
		{"auth.max_login_attempts", "number", "Failed logins allowed before the account is temporarily blocked", 5},
		{"incident.auto_close_days", "number", "Days after resolution before an incident is auto-closed", 7},
		{"notification.channels", "json", "Notification channels enabled by default", map[string]bool{"email": true, "sms": true, "whatsapp": false}},

		{"incident.sources", "array", "Allowed incident sources (origin of the record)", incidentSources()},
		{"incident.max_description_length", "number", "Maximum incident description length", cfg.MaxDescriptionLength},

		{"setting.citizen_attachment_enabled", "bool", "Allow citizens to attach files to incidents", true},
		{"setting.internal_attachment_enabled", "bool", "Allow internal users to attach files to incidents", true},
		{"setting.citizen_size_limit_bytes", "number", "Max attachment size in bytes for citizen-submitted incidents", sizeLimit("CitizenAttachmentSizeLimit", cfg.CitizenAttachmentSizeLimit)},
		{"setting.internal_size_limit_bytes", "number", "Max attachment size in bytes for internally-created incidents", sizeLimit("InternalAttachmentSizeLimit", cfg.InternalAttachmentSizeLimit)},
		{"setting.citizen_max_files_per_upload", "number", "Max files a citizen can attach in one upload", 5},
		{"setting.internal_max_files_per_upload", "number", "Max files an internal user can attach in one upload", 10},
		{"setting.images_format", "array", "Accepted image formats (active = allowed)", attachmentFormats("jpg", "jpeg", "png", "gif", "webp")},
		{"setting.audio_format", "array", "Accepted audio formats (active = allowed)", attachmentFormats("mp3", "wav")},
		{"setting.archives_format", "array", "Accepted archive formats (active = allowed)", attachmentFormats("zip", "rar", "7z")},
		{"setting.presentation_format", "array", "Accepted presentation formats (active = allowed)", attachmentFormats("ppt", "pptx")},
		{"setting.video_format", "array", "Accepted video formats (active = allowed)", attachmentFormats("mp4")},
		{"setting.documents_format", "array", "Accepted document formats (active = allowed)", attachmentFormats("pdf", "doc", "docx", "txt", "rtf", "odt")},
		{"setting.spreadsheet_format", "array", "Accepted spreadsheet formats (active = allowed)", attachmentFormats("xlsx", "xls", "csv")},

		{"image_validation.max_size_bytes", "number", "Maximum accepted image upload size in bytes", iv.MaxSizeBytes},
		{"image_validation.allowed_mime_types", "array", "Accepted image content types", iv.AllowedMimeTypes},
		{"image_validation.black_mean_threshold", "number", "Mean luminance (0-255) at or below which an image is a mostly-black candidate", iv.BlackMeanThreshold},
		{"image_validation.white_mean_threshold", "number", "Mean luminance (0-255) at or above which an image is a mostly-white candidate", iv.WhiteMeanThreshold},
		{"image_validation.low_detail_stddev_threshold", "number", "Per-tile luminance standard deviation at or below which a tile is flat/featureless", iv.LowDetailStdDevThreshold},
		{"image_validation.blur_variance_threshold", "number", "Per-tile variance-of-Laplacian score at or below which a tile is blurry", iv.BlurVarianceThreshold},
		{"image_validation.tile_grid_size", "number", "Number of tiles per side of the analysis grid", iv.TileGridSize},
		{"image_validation.black_coverage_fraction", "number", "Fraction of black tiles that rejects an image as mostly black", iv.BlackCoverageFraction},
		{"image_validation.white_coverage_fraction", "number", "Fraction of white tiles that rejects an image as mostly white", iv.WhiteCoverageFraction},
		{"image_validation.low_detail_coverage_fraction", "number", "Fraction of flat tiles that rejects an image as low detail", iv.LowDetailCoverageFraction},
		{"image_validation.blur_coverage_fraction", "number", "Fraction of blurry tiles that rejects an image as blurry", iv.BlurCoverageFraction},

		{"geo.nearby_incident_radius_meters", "number", "Default search radius in meters for nearby incidents", cfg.Geo.NearbyIncidentRadiusMeters},
		{"geo.recurrence_incident_radius_meters", "number", "Radius in meters within which an earlier same-classification incident counts as a recurrence", cfg.Geo.RecurrenceIncidentRadiusMeters},
		{"geo.postgis_enabled", "bool", "Use PostGIS ST_DWithin for radius filtering instead of haversine", cfg.Geo.PostGISEnabled},

		{"client.code", "string", "Client identifier, e.g. EPM940", cfg.ClientCode},
		{"client.chatbot_url", "string", "URL of the chatbot", cfg.ChatbotURL},

		{"sms_feedback.delay_minutes", "number", "Minutes to wait before sending the SMS feedback fallback", cfg.SmsFeedback.DelayMinutes},
		{"app.sync_dept_attributes_to_user", "bool", "Auto-append a department's locations/classifications to a user when the department is assigned", cfg.SyncDeptAttributesToUser},
	}
}

// attachmentFormat is one entry of an attachment_format.* config: a file extension and whether it is allowed.
type attachmentFormat struct {
	Name   string `json:"name"`
	Active bool   `json:"active"`
}

// attachmentFormats builds a format list with every extension active; admins switch off what they
// don't want.
func attachmentFormats(exts ...string) []attachmentFormat {
	out := make([]attachmentFormat, len(exts))
	for i, e := range exts {
		out[i] = attachmentFormat{Name: e, Active: true}
	}
	return out
}

func incidentSources() []string {
	s := constants.INCIDENT_SOURCE
	return []string{s.WEB, s.MOBILE, s.IVR, s.WHATSAPP, s.FACEBOOK, s.TWITTER, s.EMAIL, s.EPMPORTAL, s.VIUSIONAL}
}

// sizeLimit converts a byte limit held as a string in config.go to a number; an unparsable
// value is kept as the original string rather than silently replaced.
func sizeLimit(name, raw string) any {
	n, err := strconv.ParseInt(strings.TrimSpace(raw), 10, 64)
	if err != nil {
		log.Printf("[ConfigService] %s=%q is not a number, seeding it as a string", name, raw)
		return raw
	}
	return n
}

// marshalConfigValue encodes a default's value as JSON.
func marshalConfigValue(v any) (json.RawMessage, error) {
	b, err := json.Marshal(v)
	return json.RawMessage(b), err
}

// configModule is the part of an internal key ("module.name") before the first dot:
// "auth.max_login_attempts" -> "auth".
func configModule(key string) string {
	module, _, _ := strings.Cut(key, ".")
	return module
}

// configName is the part of an internal key after the first dot: "auth.max_login_attempts" -> "max_login_attempts".
func configName(key string) string {
	_, name, _ := strings.Cut(key, ".")
	return name
}
