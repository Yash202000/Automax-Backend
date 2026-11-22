package handlers

import (
	"bytes"
	"context"
	"errors"
	"io"
	"log"
	"net/url"
	"strings"
	"time"

	"github.com/automax/backend/internal/models"
	"github.com/automax/backend/internal/repository"
	"github.com/automax/backend/internal/services"
	"github.com/automax/backend/internal/storage"
	"github.com/automax/backend/pkg/constants"
	"github.com/automax/backend/pkg/i18n"
	"github.com/automax/backend/pkg/utils"
	"github.com/automax/backend/pkg/validation"
	"github.com/gofiber/fiber/v2"
	"github.com/google/uuid"
)

// CintrixRecordingFetcher is the slice of CTIHandler the stream endpoint needs.
type CintrixRecordingFetcher interface {
	FetchRecording(ctx context.Context, callLog *models.CallLog) ([]byte, string, error)
}

// CallRecordingAuditHandler serves the Call Recording History:
//   - POST /call-recordings/:call_log_id/access — save the audit entry, return a grant URL
//   - GET  /call-recordings/stream?grant=      — stream the recording for a valid grant
//   - GET  /admin/call-recordings/history     — the history list (permission-gated)
type CallRecordingAuditHandler struct {
	service     services.CallRecordingAuditService
	callLogRepo repository.CallLogRepository
	storage     *storage.MinIOStorage
	cintrix     CintrixRecordingFetcher
}

func NewCallRecordingAuditHandler(service services.CallRecordingAuditService, callLogRepo repository.CallLogRepository, storage *storage.MinIOStorage, cintrix CintrixRecordingFetcher) *CallRecordingAuditHandler {
	return &CallRecordingAuditHandler{service: service, callLogRepo: callLogRepo, storage: storage, cintrix: cintrix}
}

// RequestAccess handles POST /api/v1/call-recordings/:call_log_id/access.
// The audit entry is written before any URL is issued; if the write fails the
// user gets 503 and no way to play or download ("no gaps").
func (h *CallRecordingAuditHandler) RequestAccess(c *fiber.Ctx) error {
	ctx := c.UserContext()
	user, ok := c.Locals(constants.ContextKeys.User).(*models.User)
	if !ok || user == nil || user.ID == uuid.Nil {
		return utils.ErrorResponse(c, fiber.StatusUnauthorized, i18n.T(ctx, "user_not_authenticated"))
	}

	callLogID, err := uuid.Parse(c.Params("call_log_id"))
	if err != nil {
		return utils.ErrorResponse(c, fiber.StatusBadRequest, i18n.T(ctx, "invalid_id"))
	}

	var req models.CallRecordingAccessRequest
	if err := c.BodyParser(&req); err != nil {
		return utils.ErrorResponse(c, fiber.StatusBadRequest, i18n.T(ctx, "invalid_request_body"))
	}
	if verrs := validation.ValidateStruct(ctx, &req); len(verrs) != 0 {
		return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{"success": false, "errors": verrs})
	}

	grant, token, err := h.service.RequestAccess(ctx, user, callLogID, req.Activity, c.IP(), string(c.Request().Header.UserAgent()))
	switch {
	case errors.Is(err, services.ErrRecordingNotFound):
		return utils.ErrorResponse(c, fiber.StatusNotFound, i18n.T(ctx, "recording_not_found"))
	case errors.Is(err, services.ErrRecordingAuditFailed):
		log.Printf("[CallRecordingAudit] audit save failed user=%s call_log=%s activity=%s: %v", user.ID, callLogID, req.Activity, err)
		return utils.ErrorResponse(c, fiber.StatusServiceUnavailable, i18n.T(ctx, "recording_audit_failed"))
	case err != nil:
		return utils.InternalErrorResponse(c, err, i18n.T(ctx, "internal_server_error"))
	}

	streamURL := utils.GenerateAppURL(ctx) + "/api/v1/call-recordings/stream?grant=" + url.QueryEscape(token)
	return c.JSON(fiber.Map{
		"success": true,
		"data": models.CallRecordingAccessResponse{
			URL:       streamURL,
			ExpiresAt: grant.ExpiresAt,
		},
	})
}

// Stream handles GET /api/v1/call-recordings/stream?grant=<token>.
// The grant is the credential (it works directly in <audio src> / <a href>),
// and it can only exist once the matching audit entry has been saved.
func (h *CallRecordingAuditHandler) Stream(c *fiber.Ctx) error {
	ctx := c.UserContext()
	grant, err := h.service.VerifyGrant(c.Query("grant"))
	if err != nil {
		return utils.ErrorResponse(c, fiber.StatusUnauthorized, i18n.T(ctx, "recording_grant_invalid"))
	}

	callLog, err := h.callLogRepo.FindByID(ctx, grant.CallLogID)
	if err != nil {
		return utils.ErrorResponse(c, fiber.StatusNotFound, i18n.T(ctx, "recording_not_found"))
	}

	disposition := "inline"
	if grant.Activity == models.RecordingActivityDownloaded {
		disposition = "attachment"
	}
	c.Set("Cache-Control", "private, no-store")

	// Uploaded recording in MinIO: stream ranges straight from storage.
	if len(callLog.Attachments) > 0 {
		att := callLog.Attachments[0]
		size, serr := h.storage.StatFile(ctx, att.FilePath)
		if serr != nil {
			return utils.InternalErrorResponse(c, serr, i18n.T(ctx, "failed_to_retrieve_file"))
		}
		fetch := func(start, end int64) (io.ReadCloser, error) {
			return h.storage.GetFileRange(ctx, att.FilePath, start, end)
		}
		return sendWithRange(c, fetch, size, orDefault(att.MimeType, "audio/mpeg"), recordingFileName(callLog, att.FileName), disposition)
	}

	// Cintrix recording: fetched server-to-server, then served from memory.
	data, contentType, err := h.cintrix.FetchRecording(ctx, callLog)
	switch {
	case errors.Is(err, ErrCintrixRecordingNotFound):
		return utils.ErrorResponse(c, fiber.StatusNotFound, i18n.T(ctx, "recording_not_found"))
	case errors.Is(err, ErrCintrixNotConfigured):
		return utils.ErrorResponse(c, fiber.StatusServiceUnavailable, err.Error())
	case err != nil:
		log.Printf("[CallRecordingAudit] cintrix fetch failed call_log=%s: %v", callLog.ID, err)
		return utils.ErrorResponse(c, fiber.StatusBadGateway, i18n.T(ctx, "recording_not_found"))
	}
	fetch := func(start, end int64) (io.ReadCloser, error) {
		return io.NopCloser(bytes.NewReader(data[start : end+1])), nil
	}
	return sendWithRange(c, fetch, int64(len(data)), orDefault(contentType, "audio/wav"), recordingFileName(callLog, "recording.wav"), disposition)
}

// recordingFileName names the file after the call ("recording-<call_uuid>.ext").
func recordingFileName(cl *models.CallLog, original string) string {
	if cl.CallUuid == "" {
		return original
	}
	return "recording-" + cl.CallUuid + fileExt(original)
}

func orDefault(s, def string) string {
	if s == "" {
		return def
	}
	return s
}

func fileExt(name string) string {
	if i := strings.LastIndex(name, "."); i >= 0 {
		return name[i:]
	}
	return ""
}

// ListHistory handles GET /api/v1/admin/call-recordings/history.
// Newest first, paginated, with per-tab counts.
func (h *CallRecordingAuditHandler) ListHistory(c *fiber.Ctx) error {
	ctx := c.UserContext()
	var filter models.CallRecordingAuditFilter
	if err := c.QueryParser(&filter); err != nil {
		return utils.ErrorResponse(c, fiber.StatusBadRequest, i18n.T(ctx, "invalid_query_parameters"))
	}
	if s := c.Query("start_date"); s != "" {
		if t, ok := parseFilterDate(s, false); ok {
			filter.StartDate = &t
		} else {
			return utils.ErrorResponse(c, fiber.StatusBadRequest, i18n.T(ctx, "invalid_query_parameters"))
		}
	}
	if s := c.Query("end_date"); s != "" {
		if t, ok := parseFilterDate(s, true); ok {
			filter.EndDate = &t
		} else {
			return utils.ErrorResponse(c, fiber.StatusBadRequest, i18n.T(ctx, "invalid_query_parameters"))
		}
	}
	if verrs := validation.ValidateStruct(ctx, &filter); len(verrs) != 0 {
		return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{"success": false, "errors": verrs})
	}

	items, total, counts, err := h.service.List(ctx, &filter)
	if err != nil {
		return utils.InternalErrorResponse(c, err, i18n.T(ctx, "internal_server_error"))
	}
	totalPages := (int(total) + filter.Limit - 1) / filter.Limit
	return c.JSON(fiber.Map{
		"success":     true,
		"data":        items,
		"counts":      counts,
		"total_items": total,
		"total_pages": totalPages,
		"page":        filter.Page,
		"limit":       filter.Limit,
	})
}

// RejectModification answers any PUT/PATCH/DELETE on a history entry:
// entries cannot be edited or deleted by anyone, administrators included.
func (h *CallRecordingAuditHandler) RejectModification(c *fiber.Ctx) error {
	return utils.ErrorResponse(c, fiber.StatusMethodNotAllowed, i18n.T(c.UserContext(), "audit_entry_immutable"))
}

// parseFilterDate accepts YYYY-MM-DD (start or end of that day, server-local)
// or RFC3339.
func parseFilterDate(s string, endOfDay bool) (time.Time, bool) {
	if t, err := time.ParseInLocation("2006-01-02", s, time.Local); err == nil {
		if endOfDay {
			t = t.Add(24*time.Hour - time.Nanosecond)
		}
		return t, true
	}
	if t, err := time.Parse(time.RFC3339, s); err == nil {
		return t, true
	}
	return time.Time{}, false
}
