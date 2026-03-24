package utils

// Generate Attachment URL from context and id
func GenerateAttachmentURL(protocol, hostname, attachID, token string) string {
	url := protocol + "://" + hostname + "/api/v1/attachments/" + attachID + "/preview?token=" + token
	return url
}
