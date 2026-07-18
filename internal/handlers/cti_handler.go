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
	httpClient       *http.Client
	noRedirectClient *http.Client
}

func NewCTIHandler(cintrixURL, keyID, keySecret string, callLogRepo repository.CallLogRepository, userRepo repository.UserRepository) *CTIHandler {
	return &CTIHandler{
		cintrixURL:  strings.TrimRight(cintrixURL, "/"),
		keyID:       keyID,
		keySecret:   keySecret,
		callLogRepo: callLogRepo,
		userRepo:    userRepo,
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

// syncUserExtension persists the Cintrix-allocated extension onto the
// authenticated user. Best-effort: any failure is logged and swallowed.
func (h *CTIHandler) syncUserExtension(c *fiber.Ctx, ext string) {
	if h.userRepo == nil {
		return
	}
	userID, ok := c.Locals(constants.ContextKeys.UserID).(uuid.UUID)
	if !ok {
		log.Printf("cti: could not resolve user id from context to sync extension")
		return
	}
	user, err := h.userRepo.FindByID(c.UserContext(), userID)
	if err != nil || user == nil {
		log.Printf("cti: could not load user %s to sync extension: %v", userID, err)
		return
	}
	if user.Extension == ext {
		return
	}
	user.Extension = ext
	if err := h.userRepo.Update(c.UserContext(), user); err != nil {
		log.Printf("cti: failed to sync extension for user %s: %v", userID, err)
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
