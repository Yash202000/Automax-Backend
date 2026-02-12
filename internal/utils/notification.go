package utils

import (
	"bytes"
	"encoding/base64"
	"fmt"
	"net/smtp"
	"os"
	"strings"

	"github.com/automax/backend/internal/models"
	"github.com/twilio/twilio-go"
	openapi "github.com/twilio/twilio-go/rest/api/v2010"
)

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
				Email:  email,
				Type:   recipientType,
				Status: "failed",
				Error:  err.Error(),
			})
		}
		return recipientStatuses, err
	}

	// All succeeded
	for _, email := range allRecipients {
		recipientType := GetRecipientType(email, to, cc, bcc)
		recipientStatuses = append(recipientStatuses, models.RecipientInfo{
			Email:  email,
			Type:   recipientType,
			Status: "success",
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
