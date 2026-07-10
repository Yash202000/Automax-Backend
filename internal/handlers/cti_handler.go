package handlers

import (
	"bytes"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/automax/backend/pkg/constants"
	"github.com/gofiber/fiber/v2"
)

// CTIHandler exchanges the server-held Cintrix API key for a per-agent,
// short-lived widget token. The browser never sees the API secret.
type CTIHandler struct {
	cintrixURL string
	keyID      string
	keySecret  string
	httpClient *http.Client
}

func NewCTIHandler(cintrixURL, keyID, keySecret string) *CTIHandler {
	return &CTIHandler{
		cintrixURL: strings.TrimRight(cintrixURL, "/"),
		keyID:      keyID,
		keySecret:  keySecret,
		httpClient: &http.Client{Timeout: 10 * time.Second},
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
	return c.JSON(out)
}
