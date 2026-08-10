package handlers

import (
	"bytes"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/automax/backend/internal/models"
	"github.com/automax/backend/internal/repository"
	"github.com/automax/backend/pkg/constants"
	"github.com/gofiber/fiber/v2"
	"github.com/google/uuid"
	"gorm.io/gorm"
)

// CTIHandler exchanges the server-held Cintrix API key for a per-agent,
// short-lived widget token, and proxies authenticated recording playback.
// The browser never sees the API secret.
type CTIHandler struct {
	cintrixURL       string
	keyID            string
	keySecret        string
	callLogRepo      repository.CallLogRepository
	userRepo         repository.UserRepository
	extRepo          repository.ExtensionAssignmentRepository
	db               *gorm.DB
	httpClient       *http.Client
	noRedirectClient *http.Client
}

func NewCTIHandler(cintrixURL, keyID, keySecret string, callLogRepo repository.CallLogRepository, userRepo repository.UserRepository, extRepo repository.ExtensionAssignmentRepository, db *gorm.DB) *CTIHandler {
	return &CTIHandler{
		cintrixURL:  strings.TrimRight(cintrixURL, "/"),
		keyID:       keyID,
		keySecret:   keySecret,
		callLogRepo: callLogRepo,
		userRepo:    userRepo,
		extRepo:     extRepo,
		db:          db,
		httpClient:  &http.Client{Timeout: 10 * time.Second},
		noRedirectClient: &http.Client{
			Timeout: 10 * time.Second,
			CheckRedirect: func(req *http.Request, via []*http.Request) error {
				return http.ErrUseLastResponse
			},
		},
	}
}

// GetWidgetToken godoc: GET /api/v1/cti/widget-token (authenticated).
func (h *CTIHandler) GetWidgetToken(c *fiber.Ctx) error {
	if h.cintrixURL == "" || h.keyID == "" || h.keySecret == "" {
		return c.Status(fiber.StatusServiceUnavailable).JSON(fiber.Map{
			"error": "CTI integration is not configured",
		})
	}

	email, _ := c.Locals(constants.ContextKeys.Email).(string)
	name, _ := c.Locals(constants.ContextKeys.UserName).(string)
	if email == "" {
		return c.Status(fiber.StatusUnauthorized).JSON(fiber.Map{"error": "no user in context"})
	}
	if name == "" {
		name = email
	}

	body, err := json.Marshal(map[string]string{"email": email, "name": name})
	if err != nil {
		return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{"error": err.Error()})
	}
	mac := hmac.New(sha256.New, []byte(h.keySecret))
	mac.Write(body)
	sig := hex.EncodeToString(mac.Sum(nil))

	req, err := http.NewRequestWithContext(c.UserContext(), http.MethodPost,
		fmt.Sprintf("%s/api/v1/cti/widget-token", h.cintrixURL), bytes.NewReader(body))
	if err != nil {
		return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{"error": err.Error()})
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-Api-Key", h.keyID)
	req.Header.Set("X-Signature", sig)

	resp, err := h.httpClient.Do(req)
	if err != nil {
		return c.Status(fiber.StatusBadGateway).JSON(fiber.Map{"error": "call system unreachable"})
	}
	defer resp.Body.Close()
	payload, _ := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if resp.StatusCode != http.StatusOK {
		return c.Status(fiber.StatusBadGateway).JSON(fiber.Map{
			"error": fmt.Sprintf("cintrix token exchange failed (%d)", resp.StatusCode),
		})
	}

	var out map[string]interface{}
	if err := json.Unmarshal(payload, &out); err != nil {
		return c.Status(fiber.StatusBadGateway).JSON(fiber.Map{"error": "invalid cintrix response"})
	}
	out["cintrix_url"] = h.cintrixURL

	// Best-effort: persist the Cintrix-allocated softphone extension onto the
	// authenticated user so it always reflects the widget's current
	// assignment. Never fail the token response over this.
	if ext, ok := out["extension"].(string); ok && strings.TrimSpace(ext) != "" {
		h.syncUserExtension(c, ext)
	}

	return c.JSON(out)
}

// syncUserExtension makes Automax's extension_assignments reflect the
// Cintrix-allocated extension for the authenticated agent. Best-effort: any
// failure is logged and swallowed (never fails the token response).
//
// The current PBX extension lives in extension_assignments (User.Extension is a
// transient gorm:"-" field, so writing it persists NOTHING — the old bug where
// Admin → Users and the softphone disagreed). Cintrix is the source of truth for
// extension ALLOCATION, so we bypass Automax's manual "pool" check and force the
// assignment to match: take the extension over from any other holder, release
// the agent's previous extension, assign the new one — one tx, with history
// (mirrors ExtensionService.AssignExtension minus the pool gate).
func (h *CTIHandler) syncUserExtension(c *fiber.Ctx, ext string) {
	ext = strings.TrimSpace(ext)
	if ext == "" || h.extRepo == nil || h.db == nil {
		return
	}
	userID, ok := c.Locals(constants.ContextKeys.UserID).(uuid.UUID)
	if !ok {
		log.Printf("cti: could not resolve user id from context to sync extension")
		return
	}
	ctx := c.UserContext()

	cur, err := h.extRepo.GetByUserID(ctx, userID)
	if err != nil {
		log.Printf("cti: load current extension for user %s failed: %v", userID, err)
		return
	}
	if cur != nil && cur.Extension == ext {
		return // already correct
	}
	prev, err := h.extRepo.GetByExtension(ctx, ext)
	if err != nil {
		log.Printf("cti: load holder of extension %s failed: %v", ext, err)
		return
	}
	if prev != nil && prev.UserID == userID {
		return // extension already this user's (defensive; cur check usually covers it)
	}
	action := models.ExtensionActionAssign
	if prev != nil {
		action = models.ExtensionActionTakeover
	}

	txErr := h.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		if prev != nil { // takeover: drop the other holder's current row
			if err := h.extRepo.DeleteByExtensionTx(tx, ext); err != nil {
				return err
			}
		}
		if cur != nil && cur.Extension != ext { // one-per-user: release the old one
			if err := h.extRepo.DeleteByUserTx(tx, userID); err != nil {
				return err
			}
			if err := h.extRepo.CreateHistoryTx(tx, &models.ExtensionAssignmentHistory{
				Extension:  cur.Extension,
				UserID:     &userID,
				AssignedBy: userID,
				Action:     models.ExtensionActionRelease,
				Note:       "auto-released on Cintrix extension sync",
			}); err != nil {
				return err
			}
		}
		if err := h.extRepo.AssignTx(tx, &models.ExtensionAssignment{
			Extension:  ext,
			UserID:     userID,
			AssignedBy: userID,
			Note:       "synced from Cintrix",
		}); err != nil {
			return err
		}
		return h.extRepo.CreateHistoryTx(tx, &models.ExtensionAssignmentHistory{
			Extension:  ext,
			UserID:     &userID,
			AssignedBy: userID,
			Action:     action,
			Note:       "synced from Cintrix",
		})
	})
	if txErr != nil {
		log.Printf("cti: failed to sync extension %s for user %s: %v", ext, userID, txErr)
	}
}

// GetRecording godoc: GET /api/v1/cti/recording?call_uuid=<uuid> (authenticated).
//
// Looks up the CallLog by call_uuid, reads recording_url out of its Meta JSON
// (stored verbatim from the Cintrix call.ended webhook), and makes an
// integration-signed request to that URL. Cintrix streams the recording bytes
// back (200, audio/wav) — MinIO is never exposed publicly — and we relay those
// bytes to the browser with the audio content type.
func (h *CTIHandler) GetRecording(c *fiber.Ctx) error {
	if h.cintrixURL == "" || h.keyID == "" || h.keySecret == "" {
		return c.Status(fiber.StatusServiceUnavailable).JSON(fiber.Map{
			"error": "CTI integration is not configured",
		})
	}

	callUUID := c.Query("call_uuid")
	if callUUID == "" {
		return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{"error": "call_uuid is required"})
	}

	callLog, err := h.callLogRepo.FindByCallUUID(c.UserContext(), callUUID)
	if err != nil {
		return c.Status(fiber.StatusNotFound).JSON(fiber.Map{"error": "call log not found"})
	}

	recordingURL := models.RecordingURLFromMeta(callLog.Meta)
	if recordingURL == "" {
		return c.Status(fiber.StatusNotFound).JSON(fiber.Map{"error": "no recording for this call"})
	}

	cdrID, err := cdrIDFromRecordingURL(recordingURL)
	if err != nil {
		return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{"error": "invalid recording url"})
	}

	// Sign the exact bytes "cdr_id=<id>" — same HMAC convention as the
	// widget-token exchange, just over a synthetic query string instead of a
	// JSON body (per the recording endpoint's auth contract).
	qs := "cdr_id=" + cdrID
	mac := hmac.New(sha256.New, []byte(h.keySecret))
	mac.Write([]byte(qs))
	sig := hex.EncodeToString(mac.Sum(nil))

	req, err := http.NewRequestWithContext(c.UserContext(), http.MethodGet, recordingURL, nil)
	if err != nil {
		return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{"error": err.Error()})
	}
	req.Header.Set("X-Api-Key", h.keyID)
	req.Header.Set("X-Signature", sig)

	resp, err := h.noRedirectClient.Do(req)
	if err != nil {
		return c.Status(fiber.StatusBadGateway).JSON(fiber.Map{"error": "recording service unreachable"})
	}
	defer resp.Body.Close()

	switch {
	case resp.StatusCode == http.StatusOK:
		// Cintrix streams the recording bytes through (MinIO stays private);
		// relay them to the browser with the audio content type. Recordings
		// are small, so read fully rather than juggle a streaming body close.
		data, rerr := io.ReadAll(resp.Body)
		if rerr != nil {
			return c.Status(fiber.StatusBadGateway).JSON(fiber.Map{"error": "recording read failed"})
		}
		ct := resp.Header.Get("Content-Type")
		if ct == "" {
			ct = "audio/wav"
		}
		c.Set("Content-Type", ct)
		c.Set("Content-Disposition", `inline; filename="recording.wav"`)
		return c.Send(data)
	case resp.StatusCode == http.StatusNotFound:
		return c.Status(fiber.StatusNotFound).JSON(fiber.Map{"error": "recording not found"})
	default:
		return c.Status(fiber.StatusBadGateway).JSON(fiber.Map{
			"error": fmt.Sprintf("cintrix recording fetch failed (%d)", resp.StatusCode),
		})
	}
}

// cdrIDFromRecordingURL extracts the CDR id from a recording URL shaped like
// "http://<cintrix>/api/v1/cdr/{id}/recording" — the path segment immediately
// before the trailing "/recording".
func cdrIDFromRecordingURL(recordingURL string) (string, error) {
	parsed, err := url.Parse(recordingURL)
	if err != nil {
		return "", err
	}
	trimmed := strings.TrimSuffix(strings.TrimRight(parsed.Path, "/"), "/recording")
	idx := strings.LastIndex(trimmed, "/")
	if idx == -1 || idx == len(trimmed)-1 {
		return "", fmt.Errorf("recording url missing cdr id segment: %s", parsed.Path)
	}
	cdrID := trimmed[idx+1:]
	if cdrID == "" {
		return "", fmt.Errorf("recording url missing cdr id segment: %s", parsed.Path)
	}
	return cdrID, nil
}
