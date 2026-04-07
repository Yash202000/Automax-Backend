package config

import (
	"os"
	"strconv"
	"strings"
)

type Config struct {
	Server         ServerConfig
	Database       DatabaseConfig
	Redis          RedisConfig
	MinIO          MinIOConfig
	JWT            JWTConfig
	LDAP           LDAPConfig
	SSOPrivateKey  string // env: SSO_RSA_PRIVATE_KEY (PEM, optional — auto-gen if empty)
	SSOIssuerURL   string // env: SSO_ISSUER_URL (e.g. https://automax.example.com — embedded in iss claim)
	SSOFrontendURL string // env: SSO_FRONTEND_URL (e.g. https://automax.example.com — where /sso-complete lives)
	Escalation     EscalationConfig
	ReadyToClose   ReadyToCloseConfig
	Documenta      DocumentaConfig
	AIQuality      AIQualityConfig
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

type DocumentaConfig struct {
	BaseURL       string
	APIKey        string
	WorkspaceName string
	Enabled       bool
}

type EscalationConfig struct {
	DailyHour    int
	DailyMinute  int
	WeeklyHour   int
	WeeklyMinute int
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
	Port string
	Host string
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
	Secret     string
	ExpireHour int
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

func Load() *Config {
	return &Config{
		Server: ServerConfig{
			Port: getEnv("SERVER_PORT", "8080"),
			Host: getEnv("SERVER_HOST", "0.0.0.0"),
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
			Secret:     getEnv("JWT_SECRET", "your-super-secret-jwt-key-change-in-production"),
			ExpireHour: getEnvAsInt("JWT_EXPIRE_HOUR", 24),
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
		SSOPrivateKey:  getEnv("SSO_RSA_PRIVATE_KEY", ""),
		SSOIssuerURL:   getEnv("SSO_ISSUER_URL", ""),
		SSOFrontendURL: getEnv("SSO_FRONTEND_URL", ""),
		Escalation: EscalationConfig{
			DailyHour:    getEnvAsInt("ESCALATION_DAILY_HOUR", 18),
			DailyMinute:  getEnvAsInt("ESCALATION_DAILY_MINUTE", 0),
			WeeklyHour:   getEnvAsInt("ESCALATION_WEEKLY_HOUR", 9),
			WeeklyMinute: getEnvAsInt("ESCALATION_WEEKLY_MINUTE", 0),
		},
		Documenta: DocumentaConfig{
			BaseURL:       getEnv("DOCUMENTA_BASE_URL", "http://localhost:9090"),
			APIKey:        getEnv("DOCUMENTA_API_KEY", ""),
			WorkspaceName: getEnv("DOCUMENTA_WORKSPACE_NAME", "Automax"),
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
