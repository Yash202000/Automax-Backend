package middleware

import (
	"strconv"
	"strings"

	"github.com/automax/backend/internal/config"
	"github.com/gofiber/fiber/v2"
)

const (
	cspHeader           = "Content-Security-Policy"
	cspReportOnlyHeader = "Content-Security-Policy-Report-Only"

	// cspSettingsKey is the Locals key under which SecurityHeaders records the
	// CSP header name and report URI in force for the current request, so that
	// SetCSP can override the policy for one response without losing the
	// deployment's report-only mode.
	cspSettingsKey = "security_headers_csp"
)

// cspSettings carries the per-deployment CSP knobs down to handlers that need
// to publish a page-specific policy.
type cspSettings struct {
	header    string
	reportURI string
}

// SecurityHeaders returns middleware that attaches the recommended HTTP
// security response headers to every response.
//
// The headers are written before the rest of the chain runs, which means they
// are present on error and panic responses too, and that a handler serving a
// browser-rendered page can still replace the policy for its own response by
// calling SetCSP — a later Set on the same header name overwrites this one.
//
// Register it as the first app.Use after recover so nothing escapes it.
func SecurityHeaders(cfg config.SecurityConfig) fiber.Handler {
	settings := cspSettings{header: cspHeader, reportURI: cfg.CSPReportURI}
	if cfg.CSPReportOnly {
		settings.header = cspReportOnlyHeader
	}

	policy := withReportURI(cfg.ContentSecurityPolicy, cfg.CSPReportURI)
	hsts := buildHSTS(cfg.HSTSMaxAgeSeconds, cfg.HSTSIncludeSubdomains)

	return func(c *fiber.Ctx) error {
		if policy != "" {
			c.Locals(cspSettingsKey, settings)
			c.Set(settings.header, policy)
		}
		if cfg.PermissionsPolicy != "" {
			c.Set("Permissions-Policy", cfg.PermissionsPolicy)
		}
		if cfg.FrameOptions != "" {
			c.Set("X-Frame-Options", cfg.FrameOptions)
		}
		if cfg.ReferrerPolicy != "" {
			c.Set("Referrer-Policy", cfg.ReferrerPolicy)
		}

		// Several handlers return files with Content-Disposition: inline and a
		// stored, user-influenced content type, so sniffing must be off.
		c.Set("X-Content-Type-Options", "nosniff")

		// Only meaningful over TLS, and browsers ignore it on plain HTTP.
		// Protocol() reads X-Forwarded-Proto, so this works behind a
		// TLS-terminating proxy where the app itself listens on HTTP.
		if hsts != "" && c.Protocol() == "https" {
			c.Set("Strict-Transport-Security", hsts)
		}

		return c.Next()
	}
}

// SetCSP replaces the Content-Security-Policy for the current response only.
// Use it from handlers that return HTML, whose page needs directives the strict
// API-wide default does not grant. The configured report-only mode and report
// URI are preserved, so a staged rollout stays report-only everywhere.
func SetCSP(c *fiber.Ctx, policy string) {
	settings, ok := c.Locals(cspSettingsKey).(cspSettings)
	if !ok {
		settings = cspSettings{header: cspHeader}
	}
	c.Set(settings.header, withReportURI(policy, settings.reportURI))
}

// withReportURI appends a report-uri directive to policy when one is
// configured. report-uri is deprecated in favour of report-to, but it is what
// browsers still implement without an accompanying Reporting-Endpoints header.
func withReportURI(policy, reportURI string) string {
	if policy == "" || reportURI == "" {
		return policy
	}
	return strings.TrimRight(strings.TrimSpace(policy), ";") + "; report-uri " + reportURI
}

// buildHSTS renders the Strict-Transport-Security value, or "" when disabled.
func buildHSTS(maxAgeSeconds int, includeSubdomains bool) string {
	if maxAgeSeconds <= 0 {
		return ""
	}
	value := "max-age=" + strconv.Itoa(maxAgeSeconds)
	if includeSubdomains {
		value += "; includeSubDomains"
	}
	return value
}
