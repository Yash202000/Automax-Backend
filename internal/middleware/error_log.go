package middleware

import (
	"fmt"
	"net/http"
	"regexp"
	"strings"

	"github.com/automax/backend/internal/models"
	"github.com/automax/backend/internal/services"
	"github.com/automax/backend/pkg/constants"
	"github.com/automax/backend/pkg/utils"
	"github.com/gofiber/fiber/v2"
	"github.com/google/uuid"
)

// errorLogRule maps a request to an Error Log module. Only requests that match a
// rule are logged, so errors outside the Error Log scope are ignored. Add one rule
// per module here.
type errorLogRule struct {
	method  string   // "" = any
	methods []string // alternative to method: any of these
	path    *regexp.Regexp
	module  string
	action  func(c *fiber.Ctx) string
	// related returns the related record (type, id, label); may return empty values.
	related func(c *fiber.Ctx) (string, string, string)
	// extra returns additional technical details (for example the file size); may be nil.
	extra func(c *fiber.Ctx) string
	// critical5xx makes 5xx errors of this rule Critical (default: High).
	critical5xx bool
	// log4xx lists extra 4xx statuses that count as errors for this module.
	// 5xx is always logged.
	log4xx []int
}

var errorLogRules = []errorLogRule{
	// Incident actions: save failed, status update failed (5xx only; validation errors are not logged).
	{
		methods: []string{fiber.MethodPost, fiber.MethodPut},
		path:    regexp.MustCompile(`^/api/v1/(incidents|complaints|queries)(/[0-9a-fA-F-]{36}(/transition|/state|/assign)?)?/?$`),
		module:  models.ErrorModuleIncident,
		action:  IncidentActionName,
		related: IncidentRelated,
	},
	// Login / access: unexpected system errors only (wrong password is a 401 and is not logged).
	{
		method:      fiber.MethodPost,
		path:        regexp.MustCompile(`^/api/v1/auth/(login|refresh|sso/login|sso/register|forgot-password|verify-reset-otp|reset-password)/?$`),
		module:      models.ErrorModuleAuth,
		action:      AuthActionName,
		critical5xx: true,
	},
	{
		method:      fiber.MethodPost,
		path:        regexp.MustCompile(`^/api/v1/ldap/login/?$`),
		module:      models.ErrorModuleAuth,
		action:      AuthActionName,
		critical5xx: true,
	},
	{
		method:      fiber.MethodGet,
		path:        regexp.MustCompile(`^/api/v1/auth/sso/validate/?$`),
		module:      models.ErrorModuleAuth,
		action:      AuthActionName,
		critical5xx: true,
	},
	// Import: wrong format, missing columns, file too large, import failed.
	{
		method:  fiber.MethodPost,
		path:    regexp.MustCompile(`/(import|metrics/import|dictionary-import/(validate|commit))/?$`),
		module:  models.ErrorModuleImport,
		action:  ImportAction,
		related: ImportRelated,
		log4xx:  []int{400, 413, 415, 422},
	},
	// Attachments: upload failed, storage error, file too large / type not allowed.
	{
		method:  fiber.MethodPost,
		path:    regexp.MustCompile(`/(incidents|complaints|queries)/[^/]+/attachments/?$`),
		module:  models.ErrorModuleAttachment,
		action:  func(c *fiber.Ctx) string { return "Upload attachment" },
		related: AttachmentRelated,
		extra:   AttachmentDetails,
		log4xx:  []int{413, 415},
	},
}

var attachmentPathRe = regexp.MustCompile(`/(?:incidents|complaints|queries)/([^/]+)/attachments/?$`)

var incidentIDPathRe = regexp.MustCompile(`^/api/v1/(?:incidents|complaints|queries)/([0-9a-fA-F-]{36})`)

// IncidentRelated returns the incident the request was about (empty for "create").
func IncidentRelated(c *fiber.Ctx) (string, string, string) {
	id := ""
	if m := incidentIDPathRe.FindStringSubmatch(c.Path()); m != nil {
		id = m[1]
	}
	return "incident", id, ""
}

// IncidentActionName names the incident action from the request method and path.
func IncidentActionName(c *fiber.Ctx) string {
	path := strings.TrimSuffix(c.Path(), "/")
	switch {
	case strings.HasSuffix(path, "/transition"):
		return "Update incident status"
	case strings.HasSuffix(path, "/state"):
		return "Force incident status"
	case strings.HasSuffix(path, "/assign"):
		return "Assign incident"
	case c.Method() == fiber.MethodPost:
		return "Create incident"
	default:
		return "Update incident"
	}
}

// AuthActionName names the login / access action from the request path.
func AuthActionName(c *fiber.Ctx) string {
	path := strings.TrimSuffix(c.Path(), "/")
	switch {
	case strings.HasSuffix(path, "/ldap/login"):
		return "LDAP login"
	case strings.HasSuffix(path, "/sso/login"), strings.HasSuffix(path, "/sso/validate"), strings.HasSuffix(path, "/sso/register"):
		return "SSO login"
	case strings.HasSuffix(path, "/refresh"):
		return "Refresh session"
	case strings.HasSuffix(path, "/login"):
		return "Login"
	default:
		return "Password reset"
	}
}

// AttachmentRelated returns the incident the file was uploaded to and the file name.
// The ID is read from the path because route params are not set for requests that
// Fiber rejects early (for example a body over the size limit).
func AttachmentRelated(c *fiber.Ctx) (string, string, string) {
	incidentID := ""
	if m := attachmentPathRe.FindStringSubmatch(c.Path()); m != nil {
		incidentID = m[1]
	}
	return "incident", incidentID, UploadedFileName(c)
}

// UploadedFileName returns the name of the uploaded "file" field, or "" if it cannot be read.
func UploadedFileName(c *fiber.Ctx) (name string) {
	defer func() { _ = recover() }()
	if fh, err := c.FormFile("file"); err == nil && fh != nil {
		name = fh.Filename
	}
	return
}

// AttachmentDetails returns the file size and type of the uploaded file.
func AttachmentDetails(c *fiber.Ctx) string {
	if fh, err := c.FormFile("file"); err == nil && fh != nil {
		return fmt.Sprintf("file=%s size=%d bytes type=%s", fh.Filename, fh.Size, fh.Header.Get("Content-Type"))
	}
	return fmt.Sprintf("request size=%d bytes", len(c.Body()))
}

var importEntityRe = regexp.MustCompile(`([^/]+)/(?:metrics/)?import/?$|([^/]+)/dictionary-import/`)

func ImportAction(c *fiber.Ctx) string {
	if m := importEntityRe.FindStringSubmatch(c.Path()); m != nil {
		for _, g := range m[1:] {
			if g != "" {
				return "Import " + g
			}
		}
	}
	return "Import file"
}

func ImportRelated(c *fiber.Ctx) (string, string, string) {
	if fh, err := c.FormFile("file"); err == nil && fh != nil {
		return "import_file", "", fh.Filename
	}
	return "", "", ""
}

// ErrorLog records system errors in the Error Log automatically. It runs after the
// handler, reads the status/message the handler returned, and saves a log entry
// in the background. It never changes the response and never fails the request.
func ErrorLog() fiber.Handler {
	return func(c *fiber.Ctx) (retErr error) {
		defer func() {
			// Panic: log it, then let the recover middleware turn it into a 500.
			if r := recover(); r != nil {
				RecordRequestError(c, http.StatusInternalServerError, "Internal Server Error", fmt.Sprintf("panic: %v", r))
				panic(r)
			}
		}()

		retErr = c.Next()
		c.Locals(utils.LocalErrorRecorded, true)

		status := c.Response().StatusCode()
		message, _ := c.Locals(utils.LocalErrorMessage).(string)
		cause, _ := c.Locals(utils.LocalErrorCause).(string)

		// An error returned to Fiber's error handler (not written by ErrorResponse).
		if retErr != nil {
			status = http.StatusInternalServerError
			var fe *fiber.Error
			if e, ok := retErr.(*fiber.Error); ok {
				fe = e
				status = fe.Code
			}
			message = retErr.Error()
		}

		if status >= 400 {
			RecordRequestError(c, status, message, cause)
		} else if rowErrors, ok := c.Locals(utils.LocalImportErrors).([]string); ok {
			RecordImportRowErrors(c, rowErrors)
		}
		return retErr
	}
}

// RecordImportRowErrors logs an import that finished (HTTP 200) but had failed rows.
func RecordImportRowErrors(c *fiber.Ctx, rowErrors []string) {
	defer func() { _ = recover() }() // logging must never break the request

	rule := MatchErrorLogRule(c)
	if rule == nil || len(rowErrors) == 0 {
		return
	}

	shown := rowErrors
	if len(shown) > 20 {
		shown = shown[:20]
	}
	details := fmt.Sprintf("%s %s -> HTTP %d\n%s", c.Method(), c.Path(), c.Response().StatusCode(), strings.Join(shown, "\n"))
	if len(rowErrors) > len(shown) {
		details += fmt.Sprintf("\n... and %d more", len(rowErrors)-len(shown))
	}

	userID, userName := RequestUser(c)

	relType, relID, relLabel := SafeRelated(rule, c)

	services.LogError(models.ErrorLogEntry{
		Module:           rule.module,
		Action:           rule.action(c),
		ErrorType:        models.ErrorTypeRowFailed,
		Severity:         models.ErrorSeverityLow,
		Message:          fmt.Sprintf("%d row(s) failed to import", len(rowErrors)),
		TechnicalDetails: details,
		UserID:           userID,
		UserName:         userName,
		RelatedType:      relType,
		RelatedID:        relID,
		RelatedLabel:     relLabel,
	})
}

func RecordRequestError(c *fiber.Ctx, status int, message, cause string) {
	defer func() { _ = recover() }() // logging must never break the request

	rule := MatchErrorLogRule(c)
	if rule == nil {
		return
	}
	if status < 500 && !ContainsInt(rule.log4xx, status) {
		return
	}
	if message == "" {
		message = http.StatusText(status)
	}

	userID, userName := RequestUser(c)

	relType, relID, relLabel := SafeRelated(rule, c)

	details := fmt.Sprintf("%s %s -> HTTP %d", c.Method(), c.Path(), status)
	if extra := SafeExtra(rule, c); extra != "" {
		details += "\n" + extra
	}
	if cause != "" {
		details += "\n" + cause
	}

	errType := ErrorTypeFromStatus(status, message, cause)
	severity := models.ErrorSeverityMedium
	if status >= 500 {
		severity = models.ErrorSeverityHigh
		// Login/access system errors and storage outages stop users from working: Critical.
		if rule.critical5xx || errType == models.ErrorTypeStorageError {
			severity = models.ErrorSeverityCritical
		}
	}

	services.LogError(models.ErrorLogEntry{
		Module:           rule.module,
		Action:           rule.action(c),
		ErrorType:        errType,
		Severity:         severity,
		Message:          message,
		TechnicalDetails: details,
		UserID:           userID,
		UserName:         userName,
		RelatedType:      relType,
		RelatedID:        relID,
		RelatedLabel:     relLabel,
	})
}

func MatchErrorLogRule(c *fiber.Ctx) *errorLogRule {
	path := c.Path()
	for i := range errorLogRules {
		r := &errorLogRules[i]
		if r.method != "" && r.method != c.Method() {
			continue
		}
		if len(r.methods) > 0 && !ContainsString(r.methods, c.Method()) {
			continue
		}
		if r.path.MatchString(path) {
			return r
		}
	}
	return nil
}

func ErrorTypeFromStatus(status int, message, cause string) string {
	text := strings.ToLower(message + " " + cause)
	switch {
	case status == http.StatusRequestEntityTooLarge || strings.Contains(text, "too large"):
		return models.ErrorTypeFileTooLarge
	case status == http.StatusUnsupportedMediaType || strings.Contains(text, "file type") || strings.Contains(text, "format") || strings.Contains(text, "column"):
		return models.ErrorTypeInvalidFile
	case status == http.StatusGatewayTimeout || strings.Contains(text, "timeout") || strings.Contains(text, "deadline exceeded"):
		return models.ErrorTypeTimeout
	case status >= 500 && (strings.Contains(text, "upload") || strings.Contains(text, "storage") || strings.Contains(text, "minio")):
		return models.ErrorTypeStorageError
	case status >= 500:
		return models.ErrorTypeUnexpected
	default:
		return models.ErrorTypeInvalidFile
	}
}

func ContainsInt(list []int, v int) bool {
	for _, x := range list {
		if x == v {
			return true
		}
	}
	return false
}

// SafeRelated reads the related record of a rule; a failure here must not stop the log entry.
func SafeRelated(rule *errorLogRule, c *fiber.Ctx) (relType, relID, relLabel string) {
	defer func() { _ = recover() }()
	if rule.related != nil {
		relType, relID, relLabel = rule.related(c)
	}
	return
}

// SafeExtra reads the extra technical details of a rule; a failure here must not stop the log entry.
func SafeExtra(rule *errorLogRule, c *fiber.Ctx) (extra string) {
	defer func() { _ = recover() }()
	if rule.extra != nil {
		extra = rule.extra(c)
	}
	return
}

// ErrorLogTokenReader reads the user from a bearer token. It is set at startup and used
// only for requests rejected before authentication ran (for example a body over the limit).
var ErrorLogTokenReader func(token string) (uuid.UUID, string, bool)

// RequestUser returns the user who made the request, or nil / "" if unknown.
func RequestUser(c *fiber.Ctx) (*uuid.UUID, string) {
	defer func() { _ = recover() }()
	if id, ok := c.Locals(constants.ContextKeys.UserID).(uuid.UUID); ok {
		name, _ := c.Locals(constants.ContextKeys.UserName).(string)
		return &id, name
	}
	if ErrorLogTokenReader != nil {
		if token := strings.TrimPrefix(c.Get("Authorization"), "Bearer "); token != "" {
			if id, name, ok := ErrorLogTokenReader(token); ok {
				return &id, name
			}
		}
	}
	return nil, ""
}

func ContainsString(list []string, v string) bool {
	for _, x := range list {
		if x == v {
			return true
		}
	}
	return false
}
