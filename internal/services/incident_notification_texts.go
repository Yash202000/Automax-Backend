package services

import (
	"fmt"

	"github.com/automax/backend/internal/models"
)

// Arabic text builders for incident in-app notifications.
// English text is built inline at each call site; these produce the Arabic counterpart
// stored in subject_ar / body_ar and returned when Accept-Language: ar.

func IncidentUpdatedTextsAr(incidentNumber, title string) (subject, body string) {
	return fmt.Sprintf("تم تحديث البلاغ رقم %s", incidentNumber),
		fmt.Sprintf("تم تحديث بلاغ \"%s\" (%s)", title, incidentNumber)
}

func IncidentAssignedTransitionTextsAr(incidentNumber, title, stateName string) (subject, body string) {
	return fmt.Sprintf("بلاغ رقم %s مسند اليك", incidentNumber),
		fmt.Sprintf("بلاغ \"%s\" اسند لك. تم تغيير الحالة الى : %s", title, stateName)
}

func IncidentAssignedDirectTextsAr(incidentNumber, title string) (subject, body string) {
	return fmt.Sprintf("بلاغ رقم %s مسند اليك", incidentNumber),
		fmt.Sprintf("بلاغ \"%s\" اسند لك", title)
}

func PartialCloseExpiryTextsAr(incidentNumber, title, revertStateName, timeStr, expiresAt string) (subject, body string) {
	subject = fmt.Sprintf("البلاغ %s: موعد انتهاء الإغلاق الجزئي قريب", incidentNumber)
	body = fmt.Sprintf(
		"سيتم إعادة هذا البلاغ تلقائياً إلى '%s' إذا لم يتم إغلاقه خلال %s.\n\nرقم البلاغ: %s\nالعنوان: %s\nتاريخ الانتهاء: %s",
		revertStateName, timeStr, incidentNumber, title, expiresAt,
	)
	return
}

// extensionNotificationContentAr is the Arabic counterpart of extensionNotificationContent
// (extension_service.go). targetName/prevName are the display names of the users involved.
func extensionNotificationContentAr(action, extension, targetName, prevName string) (subject, body string) {
	switch action {
	case models.ExtensionActionCreate:
		return "تم إنشاء تحويلة PBX جديدة",
			fmt.Sprintf("تم إنشاء التحويلة %s وهي متاحة الآن للإسناد.", extension)
	case models.ExtensionActionRelease:
		return "تم تحرير تحويلة PBX",
			fmt.Sprintf("تم تحرير التحويلة %s من %s وهي متاحة الآن.", extension, targetName)
	case models.ExtensionActionTakeover:
		return "تمت إعادة إسناد تحويلة PBX",
			fmt.Sprintf("تمت إعادة إسناد التحويلة %s إلى %s (كانت مسندة سابقاً إلى %s).", extension, targetName, prevName)
	default: // assign
		return "تم إسناد تحويلة PBX",
			fmt.Sprintf("تم إسناد التحويلة %s إلى %s.", extension, targetName)
	}
}

// SimilarIncidentTexts builds the EN and AR in-app notification shown to the creator of a
// new incident when an earlier incident exists with the same location and classification.
func SimilarIncidentTexts(prevNumber, status, statusAr string) (subject, body, subjectAr, bodyAr string) {
	subject = "Similar incident previously detected"
	body = fmt.Sprintf("This incident was also identified during the previous scan. Previous Incident: %s - %s.", prevNumber, status)
	if statusAr == "" {
		statusAr = status
	}
	subjectAr = "تم اكتشاف بلاغ مماثل سابقاً"
	bodyAr = fmt.Sprintf("تم تحديد هذا البلاغ أيضاً خلال الفحص السابق. البلاغ السابق: %s - %s.", prevNumber, statusAr)
	return
}
