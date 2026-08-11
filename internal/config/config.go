package config

import (
	"os"
	"strconv"
	"strings"
)

type Config struct {
	Env                        string // env: APP_ENV ("development" | "staging" | "production"). Default: "production".
	Server                     ServerConfig
	Database                   DatabaseConfig
	Redis                      RedisConfig
	MinIO                      MinIOConfig
	JWT                        JWTConfig
	LDAP                       LDAPConfig
	LoginRateLimit             LoginRateLimitConfig
	FrontendURL                string // env: FRONTEND_URL — base URL of the frontend app, used in notification links
	SSOPrivateKey              string // env: SSO_RSA_PRIVATE_KEY (PEM, optional — auto-gen if empty)
	SSOIssuerURL               string // env: SSO_ISSUER_URL (e.g. https://automax.example.com — embedded in iss claim)
	SSOFrontendURL             string // env: SSO_FRONTEND_URL (e.g. https://automax.example.com — where /sso-complete lives)
	NafathAPIBaseURL           string // env: NAFATH_API_BASE_URL (e.g. https://nafath.amanathail.gov.sa)
	CountryCode                string // env: COUNTRY_CODE (e.g. +966, +91)
	Escalation                 EscalationConfig
	ReadyToClose               ReadyToCloseConfig
	SmsFeedback                SmsFeedbackConfig
	Documenta                  DocumentaConfig
	AIQuality                  AIQualityConfig
	AutoAssign                 AutoAssignConfig
	GoalManagement             GoalManagementConfig
	License                    LicenseConfig
	Integration                IntegrationConfig
	FinalCloseWhatsAppFeedback FinalCloseWhatsAppFeedbackConfig
	Cintrix                    CintrixConfig
	PBX                        PBXConfig
	Security                   SecurityConfig
}

// SecurityConfig holds the HTTP response security headers applied to every
// response by middleware.SecurityHeaders. Each value is env-overridable, and
// setting one to an empty string omits that header entirely, so a deployment
// whose reverse proxy already injects a header can turn the app-level one off
// rather than emitting a conflicting duplicate.
type SecurityConfig struct {
	// ContentSecurityPolicy is the default policy for API responses. The API
	// serves JSON, so the default grants no resource origins beyond 'self' and
	// blocks framing, plugins, base-tag hijacking and cross-origin form posts.
	// Handlers that return HTML override it per-response via middleware.SetCSP.
	// env: CONTENT_SECURITY_POLICY
	ContentSecurityPolicy string
	// CSPReportOnly sends the policy as Content-Security-Policy-Report-Only
	// instead of enforcing it. Use it for a staged rollout: deploy with
	// CSP_REPORT_ONLY=true, collect violations, then switch to false to enforce.
	// env: CSP_REPORT_ONLY
	CSPReportOnly bool
	// CSPReportURI, when non-empty, is appended to the policy as a report-uri
	// directive so violations are posted to a collector. env: CSP_REPORT_URI
	CSPReportURI string
	// PermissionsPolicy disables browser features the app never uses.
	// env: PERMISSIONS_POLICY
	PermissionsPolicy string
	// FrameOptions is the legacy clickjacking defence for browsers that predate
	// CSP frame-ancestors. env: X_FRAME_OPTIONS
	FrameOptions string
	// ReferrerPolicy controls what is leaked in the Referer header when the HTML
	// incident report links out to a third-party URL. env: REFERRER_POLICY
	ReferrerPolicy string
	// HSTSMaxAgeSeconds is the Strict-Transport-Security max-age. The header is
	// only emitted on requests that arrived over HTTPS; 0 disables it entirely.
	// env: HSTS_MAX_AGE_SECONDS
	HSTSMaxAgeSeconds int
	// HSTSIncludeSubdomains adds includeSubDomains to the HSTS header. Leave it
	// off unless every subdomain of the deployment is HTTPS-only.
	// env: HSTS_INCLUDE_SUBDOMAINS
	HSTSIncludeSubdomains bool
}

const (
	// DefaultContentSecurityPolicy is the restrictive baseline applied to every
	// response. The backend serves JSON to a separately-hosted frontend, so no
	// third-party origin needs to be trusted here; 'self' only ever matters for
	// the handful of endpoints a browser navigates to directly. data: is allowed
	// for images because the HTML incident report embeds its logos and image
	// attachments as data: URIs (internal/handlers/incident_report.go).
	DefaultContentSecurityPolicy = "default-src 'self'; " +
		"script-src 'self'; " +
		"style-src 'self'; " +
		"img-src 'self' data:; " +
		"font-src 'self' data:; " +
		"connect-src 'self'; " +
		"object-src 'none'; " +
		"base-uri 'none'; " +
		"form-action 'self'; " +
		"frame-ancestors 'none'; " +
		"upgrade-insecure-requests"

	// DefaultPermissionsPolicy switches off the browser features this app has no
	// use for. An empty allowlist "()" denies the feature to every origin,
	// including this one.
	DefaultPermissionsPolicy = "accelerometer=(), autoplay=(), camera=(), display-capture=(), " +
		"encrypted-media=(), fullscreen=(), geolocation=(), gyroscope=(), magnetometer=(), " +
		"microphone=(), midi=(), payment=(), usb=(), xr-spatial-tracking=()"
)

// PBXConfig holds settings for the external PBX used by the extension-assignment
// feature. The extension pool is fetched from BaseURL with "?action=list".
type PBXConfig struct {
	// BaseURL is the PBX endpoint that lists/creates extensions, e.g.
	// https://zkff.automaxsw.com/create_user.php. The extension pool is read from
	// BaseURL + "?action=list"; a single extension from + "&username=<ext>".
	// env: PBX_BASE_URL
	BaseURL string
	// InsecureSkipVerify disables PBX TLS certificate validation. Dev/staging
	// escape hatch only; defaults to false. env: PBX_INSECURE_SKIP_VERIFY
	InsecureSkipVerify bool
}

// FinalCloseWhatsAppFeedbackConfig holds settings for deleting a still-active
// WhatsApp feedback session once the final-close feedback has already been
// submitted via another channel (e.g. the SMS fallback link), so the WhatsApp
// feedback link stops working for the reporter.
type FinalCloseWhatsAppFeedbackConfig struct {
	// SessionBaseURL is the full session endpoint of the WhatsApp feedback API,
	// including the "/session" path segment, e.g.
	// https://unicam.discretal.com/automax3-feedback/session. The mobile number
	// is appended directly to this value to form the DELETE URL.
	// env: FINAL_CLOSE_WHATSAPP_FEEDBACK_SESSION_BASE_URL
	SessionBaseURL string
}

type IntegrationConfig struct {
	SecretsKey string // env: INTEGRATION_SECRETS_KEY (64-char hex = 32 bytes for AES-256-GCM)
}

type LicenseConfig struct {
	EncryptionKey   string // env: LICENSE_ENCRYPTION_KEY (64-char hex = 32 bytes for AES-256)
	GracePeriodDays int    // env: LICENSE_GRACE_PERIOD_DAYS (default: 7)
	Enabled         bool   // env: LICENSE_ENABLED (default: true, set false for dev bypass)
	// DevSeedEnabled forces the dev-license auto-seeder to run even if APP_ENV != "development".
	// env: LICENSE_DEV_SEED (default: false). For CI pipelines that seed their own DB.
	DevSeedEnabled bool
	// DevSeedExpiryDays controls how long the auto-seeded dev license is valid.
	// env: LICENSE_DEV_EXPIRY_DAYS (default: 90). Kept short so developers exercise
	// the expiry and renewal flow rather than running against a never-expiring license.
	DevSeedExpiryDays int
}

// AIQualityConfig holds settings for the AI Quality Monitor.
type AIQualityConfig struct {
	// APIEndpoint is the URL of the external AI quality API. env: AI_QUALITY_API_ENDPOINT
	APIEndpoint string
	// APIKey is the bearer token used when calling the AI quality API. env: AI_QUALITY_API_KEY
	APIKey string
	// CheckIntervalMinutes controls how often the monitor polls (default 10). env: AI_QUALITY_CHECK_INTERVAL_MINUTES
	CheckIntervalMinutes int
	// AppProtocol is the protocol used to construct attachment fallback URLs (http/https). env: APP_PROTOCOL
	AppProtocol string
	// AppHost is the host:port of this server used to construct attachment fallback URLs. env: APP_HOST
	AppHost string
	// AppToken is an internal bearer token used when downloading attachments via the API fallback. env: APP_TOKEN
	AppToken string
}

// AutoAssignConfig holds settings for the Auto-Assign Monitor.
type AutoAssignConfig struct {
	// StateCode is the workflow state code to scan for unassigned incidents. env: AUTO_ASSIGN_STATE_CODE
	// Leave empty to disable the monitor entirely.
	StateCode string
	// IntervalMinutes controls how often the monitor sweeps (default 5). env: AUTO_ASSIGN_INTERVAL_MINUTES
	IntervalMinutes int
}

type GoalManagementConfig struct {
	Enabled bool // env: GOAL_MANAGEMENT
}

type DocumentaConfig struct {
	BaseURL       string
	ClientID      string
	ClientSecret  string
	WorkspaceName string
	Enabled       bool
}

// CintrixConfig holds settings for the Cintrix CTI integration (contact-center
// softphone widget). env: CINTRIX_URL, CINTRIX_API_KEY_ID, CINTRIX_API_KEY_SECRET,
// CINTRIX_WEBHOOK_SECRET.
// CINTRIX_URL must be the Cintrix FRONTEND origin (its nginx serves
// cti-widget.js and proxies /api to the backend) — NOT the bare backend port;
// the widget script 404s there and the widget never renders.
// — leaving these unset disables the CTI routes.
// WebhookSecret authenticates inbound call-event webhooks from Cintrix
// (Authorization: Bearer + X-Cintrix-Signature HMAC); leaving it unset
// disables the webhook route (503).
type CintrixConfig struct {
	URL           string
	APIKeyID      string
	APIKeySecret  string
	WebhookSecret string
}

type EscalationConfig struct {
	DailyHour    int
	DailyMinute  int
	WeeklyHour   int
	WeeklyMinute int
}

// SmsFeedbackConfig controls the delayed SMS fallback sent when the WhatsApp chatbot
// has not received a response within the configured window after a final-close transition.
type SmsFeedbackConfig struct {
	// DelayMinutes is how long to wait before sending the SMS fallback. env: SMS_FEEDBACK_DELAY_MINUTES (default: 2880)
	DelayMinutes int
}

type ReadyToCloseConfig struct {

	// env: READY_TO_CLOSE_DURATION_OPTIONS default: "1 Day,2 Days,1 Week,2 Weeks,1 Month,3 Months"
	DefaultDurationOptions []string
	// PreExpiryNotificationHours controls how many hours before expiry the warning notification is sent. env: READY_TO_CLOSE_PRE_EXPIRY_HOURS (default: 24)
	PreExpiryNotificationHours int
	// RevertStateCode is the workflow state code incidents are moved back to when they expire in Ready to Close. env: READY_TO_CLOSE_REVERT_STATE_CODE (default: "under_resolution")
	RevertStateCode string
	// StateCode is the workflow state code that activates this feature. env: READY_TO_CLOSE_STATE_CODE (default: "ready_to_close")
	StateCode string
}

type ServerConfig struct {
	Port         string
	Host         string
	AllowOrigins string // env: ALLOW_ORIGINS (comma-separated)
}

type DatabaseConfig struct {
	Host     string
	Port     string
	User     string
	Password string
	DBName   string
	SSLMode  string
}

type RedisConfig struct {
	Host     string
	Port     string
	Password string
	DB       int
}

type MinIOConfig struct {
	Endpoint        string
	AccessKeyID     string
	SecretAccessKey string
	UseSSL          bool
	BucketName      string
}

type JWTConfig struct {
	Secret             string
	ExpireHour         float64
	RefreshExpireHour  float64
	RememberExpireHour float64
}

type LDAPConfig struct {
	Enabled            bool
	URL                string
	BaseDN             string
	BindDN             string
	BindPassword       string
	UserSearchBase     string
	UserSearchFilter   string
	GroupSearchBase    string
	GroupSearchFilter  string
	InsecureSkipVerify bool
}

type LoginRateLimitConfig struct {
	Enabled         bool
	MaxAttempts     int
	RateLimitWindow int // in minutes
	BlockDuration   int // in minutes
	BypassForAdmin  bool
}

func Load() *Config {
	return &Config{
		Env: getEnv("APP_ENV", "production"),
		Server: ServerConfig{
			Port:         getEnv("SERVER_PORT", "8080"),
			Host:         getEnv("SERVER_HOST", "0.0.0.0"),
			AllowOrigins: getEnv("ALLOW_ORIGINS", "http://localhost:3000,http://localhost:5173,http://localhost:5174"),
			// FrontendURL: getEnv("FRONTEND_URL", "http://localhost:5173"),
		},
		Database: DatabaseConfig{
			Host:     getEnv("DB_HOST", "localhost"),
			Port:     getEnv("DB_PORT", "5432"),
			User:     getEnv("DB_USER", "automax"),
			Password: getEnv("DB_PASSWORD", "automax123"),
			DBName:   getEnv("DB_NAME", "automax"),
			SSLMode:  getEnv("DB_SSLMODE", "disable"),
		},
		Redis: RedisConfig{
			Host:     getEnv("REDIS_HOST", "localhost"),
			Port:     getEnv("REDIS_PORT", "6379"),
			Password: getEnv("REDIS_PASSWORD", ""),
			DB:       getEnvAsInt("REDIS_DB", 0),
		},
		MinIO: MinIOConfig{
			Endpoint:        getEnv("MINIO_ENDPOINT", "localhost:9000"),
			AccessKeyID:     getEnv("MINIO_ACCESS_KEY", "minioadmin"),
			SecretAccessKey: getEnv("MINIO_SECRET_KEY", "minioadmin"),
			UseSSL:          getEnvAsBool("MINIO_USE_SSL", false),
			BucketName:      getEnv("MINIO_BUCKET", "automax"),
		},
		JWT: JWTConfig{
			Secret:             getEnv("JWT_SECRET", "your-super-secret-jwt-key-change-in-production"),
			ExpireHour:         getEnvAsFloat("JWT_EXPIRE_HOUR", 24),
			RefreshExpireHour:  getEnvAsFloat("JWT_REFRESH_EXPIRE_HOUR", 168),
			RememberExpireHour: getEnvAsFloat("JWT_REMEMBER_EXPIRE_HOUR", 720),
		},
		LDAP: LDAPConfig{
			Enabled:            getEnvAsBool("LDAP_ENABLED", false),
			URL:                getEnv("LDAP_URL", "ldap://localhost:389"),
			BaseDN:             getEnv("LDAP_BASE_DN", "dc=example,dc=com"),
			BindDN:             getEnv("LDAP_BIND_DN", ""),
			BindPassword:       getEnv("LDAP_BIND_PASSWORD", ""),
			UserSearchBase:     getEnv("LDAP_USER_SEARCH_BASE", "ou=users,dc=example,dc=com"),
			UserSearchFilter:   getEnv("LDAP_USER_SEARCH_FILTER", "(sAMAccountName={{username}})"),
			GroupSearchBase:    getEnv("LDAP_GROUP_SEARCH_BASE", "ou=groups,dc=example,dc=com"),
			GroupSearchFilter:  getEnv("LDAP_GROUP_SEARCH_FILTER", "(member={{userDN}})"),
			InsecureSkipVerify: getEnvAsBool("LDAP_INSECURE_SKIP_VERIFY", true),
		},
		LoginRateLimit: LoginRateLimitConfig{
			Enabled:         getEnvAsBool("LOGIN_RATE_LIMIT_ENABLED", true),
			MaxAttempts:     getEnvAsInt("MAX_LOGIN_ATTEMPTS", 5),
			RateLimitWindow: getEnvAsInt("RATE_LIMIT_WINDOW", 5),
			BlockDuration:   getEnvAsInt("BLOCK_DURATION", 15),
			BypassForAdmin:  getEnvAsBool("BYPASS_RATE_LIMIT_FOR_ADMIN", true),
		},
		SSOPrivateKey:    getEnv("SSO_RSA_PRIVATE_KEY", ""),
		SSOIssuerURL:     getEnv("SSO_ISSUER_URL", ""),
		FrontendURL:      getEnv("FRONTEND_URL", ""),
		SSOFrontendURL:   getEnv("SSO_FRONTEND_URL", ""),
		NafathAPIBaseURL: getEnv("NAFATH_API_BASE_URL", ""),
		CountryCode:      getEnv("COUNTRY_CODE", "+966"),
		Escalation: EscalationConfig{
			DailyHour:    getEnvAsInt("ESCALATION_DAILY_HOUR", 18),
			DailyMinute:  getEnvAsInt("ESCALATION_DAILY_MINUTE", 0),
			WeeklyHour:   getEnvAsInt("ESCALATION_WEEKLY_HOUR", 9),
			WeeklyMinute: getEnvAsInt("ESCALATION_WEEKLY_MINUTE", 0),
		},
		Documenta: DocumentaConfig{
			BaseURL:       getEnv("DOCUMENTA_BASE_URL", "http://localhost:8090"),
			ClientID:      getEnv("DOCUMENTA_CLIENT_ID", ""),
			ClientSecret:  getEnv("DOCUMENTA_CLIENT_SECRET", ""),
			WorkspaceName: getEnv("DOCUMENTA_WORKSPACE_NAME", "automax"),
			Enabled:       getEnvAsBool("DOCUMENTA_ENABLED", false),
		},
		ReadyToClose: ReadyToCloseConfig{
			DefaultDurationOptions:     getEnvAsStringSlice("READY_TO_CLOSE_DURATION_OPTIONS", []string{"1 Day", "2 Days", "1 Week", "2 Weeks", "1 Month", "3 Months"}),
			PreExpiryNotificationHours: getEnvAsInt("READY_TO_CLOSE_PRE_EXPIRY_HOURS", 24),
			RevertStateCode:            getEnv("READY_TO_CLOSE_REVERT_STATE_CODE", "under_resolution"),
			StateCode:                  getEnv("READY_TO_CLOSE_STATE_CODE", "ready_to_close"),
		},
		AIQuality: AIQualityConfig{
			APIEndpoint:          getEnv("AI_QUALITY_API_ENDPOINT", ""),
			APIKey:               getEnv("AI_QUALITY_API_KEY", ""),
			CheckIntervalMinutes: getEnvAsInt("AI_QUALITY_CHECK_INTERVAL_MINUTES", 10),
			AppProtocol:          getEnv("APP_PROTOCOL", "http"),
			AppHost:              getEnv("APP_HOST", "localhost:8080"),
			AppToken:             getEnv("APP_TOKEN", ""),
		},
		FinalCloseWhatsAppFeedback: FinalCloseWhatsAppFeedbackConfig{
			SessionBaseURL: getEnv("FINAL_CLOSE_WHATSAPP_FEEDBACK_SESSION_BASE_URL", ""),
		},
		AutoAssign: AutoAssignConfig{
			StateCode:       getEnv("AUTO_ASSIGN_STATE_CODE", ""),
			IntervalMinutes: getEnvAsInt("AUTO_ASSIGN_INTERVAL_MINUTES", 5),
		},
		GoalManagement: GoalManagementConfig{
			Enabled: getEnvAsBool("GOAL_MANAGEMENT", false),
		},
		License: LicenseConfig{
			EncryptionKey:     getEnv("LICENSE_ENCRYPTION_KEY", ""),
			GracePeriodDays:   getEnvAsInt("LICENSE_GRACE_PERIOD_DAYS", 7),
			Enabled:           getEnvAsBool("LICENSE_ENABLED", true),
			DevSeedEnabled:    getEnvAsBool("LICENSE_DEV_SEED", false),
			DevSeedExpiryDays: getEnvAsInt("LICENSE_DEV_EXPIRY_DAYS", 90),
		},
		Integration: IntegrationConfig{
			SecretsKey: getEnv("INTEGRATION_SECRETS_KEY", ""),
		},
		SmsFeedback: SmsFeedbackConfig{
			DelayMinutes: getEnvAsInt("SMS_FEEDBACK_DELAY_MINUTES", 0),
		},
		Cintrix: CintrixConfig{
			URL:           getEnv("CINTRIX_URL", ""),
			APIKeyID:      getEnv("CINTRIX_API_KEY_ID", ""),
			APIKeySecret:  getEnv("CINTRIX_API_KEY_SECRET", ""),
			WebhookSecret: getEnv("CINTRIX_WEBHOOK_SECRET", ""),
		},
		PBX: PBXConfig{
			BaseURL:            getEnv("PBX_BASE_URL", "https://zkff.automaxsw.com/create_user.php"),
			InsecureSkipVerify: getEnvAsBool("PBX_INSECURE_SKIP_VERIFY", false),
		},
		Security: SecurityConfig{
			ContentSecurityPolicy: getEnv("CONTENT_SECURITY_POLICY", DefaultContentSecurityPolicy),
			CSPReportOnly:         getEnvAsBool("CSP_REPORT_ONLY", false),
			CSPReportURI:          getEnv("CSP_REPORT_URI", ""),
			PermissionsPolicy:     getEnv("PERMISSIONS_POLICY", DefaultPermissionsPolicy),
			FrameOptions:          getEnv("X_FRAME_OPTIONS", "DENY"),
			ReferrerPolicy:        getEnv("REFERRER_POLICY", "no-referrer"),
			HSTSMaxAgeSeconds:     getEnvAsInt("HSTS_MAX_AGE_SECONDS", 31536000),
			HSTSIncludeSubdomains: getEnvAsBool("HSTS_INCLUDE_SUBDOMAINS", false),
		},
	}
}

func getEnv(key, defaultValue string) string {
	if value, exists := os.LookupEnv(key); exists {
		return value
	}
	return defaultValue
}

func getEnvAsInt(key string, defaultValue int) int {
	if value, exists := os.LookupEnv(key); exists {
		if intValue, err := strconv.Atoi(value); err == nil {
			return intValue
		}
	}
	return defaultValue
}

func getEnvAsBool(key string, defaultValue bool) bool {
	if value, exists := os.LookupEnv(key); exists {
		if boolValue, err := strconv.ParseBool(value); err == nil {
			return boolValue
		}
	}
	return defaultValue
}

func getEnvAsStringSlice(key string, defaultValue []string) []string {
	if value, exists := os.LookupEnv(key); exists && value != "" {
		parts := strings.Split(value, ",")
		result := make([]string, 0, len(parts))
		for _, p := range parts {
			if trimmed := strings.TrimSpace(p); trimmed != "" {
				result = append(result, trimmed)
			}
		}
		if len(result) > 0 {
			return result
		}
	}
	return defaultValue
}

func getEnvAsFloat(key string, defaultValue float64) float64 {
	if value, exists := os.LookupEnv(key); exists {
		if floatValue, err := strconv.ParseFloat(value, 64); err == nil {
			return floatValue
		}
	}
	return defaultValue
}
