package utils

import (
	"bytes"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/smtp"
	"os"
	"strings"
	"time"

	"github.com/automax/backend/internal/models"
	"github.com/twilio/twilio-go"
	openapi "github.com/twilio/twilio-go/rest/api/v2010"
)

func SendWhatsApp(phone string, message string) error {

	metaURL := os.Getenv("METAURL")
	accessToken := os.Getenv("META_ACCESS_TOKEN")

	// Keys validation
	if metaURL == "" {
		return fmt.Errorf("whatsapp config error: METAURL not set")
	}

	if accessToken == "" {
		return fmt.Errorf("whatsapp config error: META_ACCESS_TOKEN not set")
	}

	// Request payload
	payload := map[string]interface{}{
		"messaging_product": "whatsapp",
		"recipient_type":    "individual",
		"to":                phone,
		"type":              "text",
		"text": map[string]interface{}{
			"preview_url": false,
			"body":        message,
		},
	}

	jsonData, err := json.Marshal(payload)
	if err != nil {
		return fmt.Errorf("whatsapp payload marshal failed: %w", err)
	}

	req, err := http.NewRequest("POST", metaURL, bytes.NewBuffer(jsonData))
	if err != nil {
		return fmt.Errorf("whatsapp request creation failed: %w", err)
	}

	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+accessToken)

	client := &http.Client{
		Timeout: 10 * time.Second,
	}

	resp, err := client.Do(req)
	if err != nil {
		return fmt.Errorf("whatsapp network error: %w", err)
	}
	defer resp.Body.Close()

	bodyBytes, _ := io.ReadAll(resp.Body)

	// Success case
	if resp.StatusCode >= 200 && resp.StatusCode < 300 {
		return nil
	}

	// parsing structured Meta error
	var metaErr models.MetaErrorResponse
	if err := json.Unmarshal(bodyBytes, &metaErr); err == nil && metaErr.Error.Code != 0 {

		switch metaErr.Error.Code {

		case 100:
			return fmt.Errorf("whatsapp invalid parameter (code 100): %s", metaErr.Error.Message)

		case 190:
			return fmt.Errorf("whatsapp access token expired (code 190)")

		case 200:
			return fmt.Errorf("whatsapp permission denied (code 200)")

		case 2500:
			return fmt.Errorf("whatsapp invalid endpoint (code 2500): %s", metaErr.Error.Message)

		default:
			return fmt.Errorf(
				"whatsapp api error (code %d): %s | trace: %s",
				metaErr.Error.Code,
				metaErr.Error.Message,
				metaErr.Error.FBTraceID,
			)
		}
	}

	//If response is not structured JSON
	return fmt.Errorf(
		"whatsapp http error (%d): %s",
		resp.StatusCode,
		string(bodyBytes),
	)
}

// // WhatsApp Service
// func SendWhatsApp(phone string, message string) error {

// 	payload := map[string]interface{}{
// 		"messaging_product": "whatsapp",
// 		"recipient_type":    "individual",
// 		"to":                phone,
// 		"type":              "text",
// 		"text": map[string]interface{}{
// 			"preview_url": false,
// 			"body":        message,
// 		},
// 	}

// 	jsonData, _ := json.Marshal(payload)

// 	req, err := http.NewRequest("POST", os.Getenv("METAURL"), bytes.NewBuffer(jsonData))
// 	if err != nil {
// 		return err
// 	}

// 	req.Header.Set("Content-Type", "application/json")
// 	req.Header.Set("Authorization", "Bearer "+os.Getenv("META_ACCESS_TOKEN"))
// 	client := &http.Client{}
// 	resp, err := client.Do(req)
// 	if err != nil {
// 		return err
// 	}
// 	defer resp.Body.Close()
// 	if resp.StatusCode >= 300 {
// 		bodyBytes, _ := io.ReadAll(resp.Body)
// 		return fmt.Errorf("whatsapp send failed: %s", string(bodyBytes))
// 	}

// 	return nil
// }

// SendSMTPWithCCBCC sends email with TO, CC, BCC, and attachments support
func SendSMTPWithCCBCC(to []string, cc []string, bcc []string, subject, body string, attachments []models.AttachmentData) (models.RecipientArray, error) {
	host := os.Getenv("SMTP_HOST")
	port := os.Getenv("SMTP_PORT")
	user := os.Getenv("SMTP_USER")
	pass := os.Getenv("SMTP_PASS")
	from := os.Getenv("SMTP_FROM")

	addr := fmt.Sprintf("%s:%s", host, port)
	auth := smtp.PlainAuth("", user, pass, host)

	// Build recipient list for SMTP (TO + CC + BCC)
	allRecipients := make([]string, 0, len(to)+len(cc)+len(bcc))
	allRecipients = append(allRecipients, to...)
	allRecipients = append(allRecipients, cc...)
	allRecipients = append(allRecipients, bcc...)

	// Build email message with MIME multipart for attachments
	var msg bytes.Buffer
	boundary := "==_boundary_=="

	// Headers
	msg.WriteString(fmt.Sprintf("From: %s\r\n", from))
	if len(to) > 0 {
		msg.WriteString(fmt.Sprintf("To: %s\r\n", strings.Join(to, ", ")))
	}
	if len(cc) > 0 {
		msg.WriteString(fmt.Sprintf("Cc: %s\r\n", strings.Join(cc, ", ")))
	}
	// BCC is not included in headers (that's the point of BCC)
	msg.WriteString(fmt.Sprintf("Subject: %s\r\n", subject))
	msg.WriteString("MIME-Version: 1.0\r\n")
	msg.WriteString(fmt.Sprintf("Content-Type: multipart/mixed; boundary=\"%s\"\r\n", boundary))
	msg.WriteString("\r\n")

	// Body part
	msg.WriteString(fmt.Sprintf("--%s\r\n", boundary))
	msg.WriteString("Content-Type: text/plain; charset=\"UTF-8\"\r\n")
	msg.WriteString("\r\n")
	msg.WriteString(body)
	msg.WriteString("\r\n")

	// Attachment parts
	for _, att := range attachments {
		msg.WriteString(fmt.Sprintf("--%s\r\n", boundary))
		msg.WriteString(fmt.Sprintf("Content-Type: %s\r\n", att.ContentType))
		msg.WriteString("Content-Transfer-Encoding: base64\r\n")
		msg.WriteString(fmt.Sprintf("Content-Disposition: attachment; filename=\"%s\"\r\n", att.Filename))
		msg.WriteString("\r\n")

		// Encode attachment data to base64
		encoded := base64.StdEncoding.EncodeToString(att.Data)
		// Split into 76-character lines as per RFC 2045
		for i := 0; i < len(encoded); i += 76 {
			end := i + 76
			if end > len(encoded) {
				end = len(encoded)
			}
			msg.WriteString(encoded[i:end])
			msg.WriteString("\r\n")
		}
	}

	msg.WriteString(fmt.Sprintf("--%s--\r\n", boundary))

	// Send email
	err := smtp.SendMail(addr, auth, from, allRecipients, msg.Bytes())

	// Build recipient status array
	var recipientStatuses models.RecipientArray
	if err != nil {
		// All failed
		for _, email := range allRecipients {
			recipientType := GetRecipientType(email, to, cc, bcc)
			recipientStatuses = append(recipientStatuses, models.RecipientInfo{
				Channel: email,
				Type:    recipientType,
				Status:  "failed",
				Error:   err.Error(),
			})
		}
		return recipientStatuses, err
	}

	// All succeeded
	for _, email := range allRecipients {
		recipientType := GetRecipientType(email, to, cc, bcc)
		recipientStatuses = append(recipientStatuses, models.RecipientInfo{
			Channel: email,
			Type:    recipientType,
			Status:  "success",
		})
	}

	return recipientStatuses, nil
}

func SendSMS(to, message string) error {
	accountSID := os.Getenv("TWILIO_ACCOUNT_SID")
	authToken := os.Getenv("TWILIO_AUTH_TOKEN")
	from := os.Getenv("TWILIO_PHONE_NUMBER")

	if accountSID == "" || authToken == "" || from == "" {
		return fmt.Errorf("twilio env vars missing: TWILIO_ACCOUNT_SID / TWILIO_AUTH_TOKEN / TWILIO_PHONE_NUMBER")
	}

	client := twilio.NewRestClientWithParams(twilio.ClientParams{
		Username: accountSID,
		Password: authToken,
	})

	params := &openapi.CreateMessageParams{}
	params.SetTo(to)
	params.SetFrom(from)
	params.SetBody(message)

	_, err := client.Api.CreateMessage(params)
	if err != nil {
		return fmt.Errorf("twilio send sms error: %w", err)
	}

	return nil
}

// GetRecipientType determines if an email is in TO, CC, or BCC list
func GetRecipientType(email string, to []string, cc []string, bcc []string) string {
	if contains(to, email) {
		return "to"
	}
	if contains(cc, email) {
		return "cc"
	}
	if contains(bcc, email) {
		return "bcc"
	}
	return "to" // default
}

// Helper function to check if string slice contains a value
func contains(slice []string, item string) bool {
	for _, s := range slice {
		if s == item {
			return true
		}
	}
	return false
}
