package migrations

import (
	"log"

	"github.com/google/uuid"
	"gorm.io/gorm"
)

// smsSeed is one default SMS template. Seeded rows replace SMS text that used to be
// hardcoded in Go; admins can edit them afterwards in Notification Templates.
type smsSeed struct {
	Code       string
	Name       string
	ModuleType string
	ActionType string // only set where sendByActionType looks the template up
	BodyEN     string
	BodyAR     string
	Variables  string
	// OnlyIfNoActionTemplate skips seeding when an active template for the same
	// action_type+channel already exists (action-type sends fan out to ALL active templates).
	OnlyIfNoActionTemplate bool
}

var defaultSMSTemplates = []smsSeed{
	{
		Code: "OTP_SMS", Name: "One-time passcode (OTP)", ModuleType: "auth",
		BodyEN:    "Your OTP is {{otp}}",
		BodyAR:    "رمز التحقق الخاص بك هو {{otp}}",
		Variables: `["otp"]`,
	},
	{
		Code: "INCIDENT_REQUEST_INFO_SMS", Name: "Request additional info (citizen)", ModuleType: "incident",
		BodyEN:    "Dear Beneficiary,\nPlease complete your incident details through the link below and fill in the required information to proceed with your request:\n{{sms_link}}\nThank you for your cooperation.",
		BodyAR:    "عزيزي المستفيد،\nنأمل استكمال بيانات البلاغ عبر الرابط أدناه وتعبئة الحقول المطلوبة، وذلك لاستكمال معالجة البلاغ:\n{{sms_link}}\nشكرًا لتعاونكم.",
		Variables: `["sms_link"]`,
	},
	{
		Code: "INCIDENT_MERGE_CLOSED_SMS", Name: "Merged incident auto-closed", ModuleType: "incident",
		BodyEN:    "Your incident {{incident_number}} has been automatically closed as it was merged with master incident {{master_incident_number}}. The master incident has been resolved.",
		BodyAR:    "تم إغلاق بلاغك رقم {{incident_number}} تلقائيًا لدمجه مع البلاغ الرئيسي رقم {{master_incident_number}}. وقد تمت معالجة البلاغ الرئيسي.",
		Variables: `["incident_number","master_incident_number"]`,
	},
	{
		Code: "INCIDENT_MERGE_STATUS_SMS", Name: "Merged incident status updated", ModuleType: "incident",
		BodyEN:    "Your incident {{incident_number}} status has been updated to '{{to_state}}' (master incident: {{master_incident_number}}). Comment: {{comment}}",
		BodyAR:    "تم تحديث حالة بلاغك رقم {{incident_number}} إلى '{{to_state}}' (البلاغ الرئيسي: {{master_incident_number}}). ملاحظة: {{comment}}",
		Variables: `["incident_number","to_state","master_incident_number","comment"]`,
	},
	{
		Code: "IVR_INCIDENT_CREATED_SMS", Name: "IVR incident created", ModuleType: "incident",
		BodyEN:    "Thank you for contacting Eastern Province Municipality. Your incident {{incident_number}} has been created.",
		BodyAR:    "شكرًا لتواصلكم مع أمانة المنطقة الشرقية. تم إنشاء بلاغكم رقم {{incident_number}}.",
		Variables: `["incident_number"]`,
	},
	{
		Code: "INCIDENT_FEEDBACK_REQUEST_SMS", Name: "Public feedback request", ModuleType: "incident",
		BodyEN:    "Please rate your experience. Submit your feedback here: {{feedback_link}}",
		BodyAR:    "نأمل تقييم تجربتكم عبر الرابط التالي: {{feedback_link}}",
		Variables: `["feedback_link"]`,
	},
	{
		Code: "INCIDENT_CLOSURE_NOT_BELONG_SMS", Name: "Closure: incident belongs to another department", ModuleType: "incident",
		BodyEN:    "Dear Citizen, your incident (ID: {{incident_number}}) has been reviewed. The issue belongs to the {{department_name}} department and does not fall under our department's scope for processing. Kindly contact the concerned department for further assistance. We appreciate your understanding.",
		BodyAR:    "عزيزي المواطن، تمت مراجعة بلاغك رقم {{incident_number}}. البلاغ يخص إدارة {{department_name}} وليس من اختصاصنا. نأمل التواصل مع الجهة المعنية. شكرًا لتفهمكم.",
		Variables: `["incident_number","department_name"]`,
	},
	{
		Code: "INC_TO_REQ_SMS", Name: "Incident converted to request", ModuleType: "incident", ActionType: "convert_to_request",
		BodyEN:                 "Your incident {{incident_number}} has been converted to request {{request_number}}",
		BodyAR:                 "تم تحويل بلاغك رقم {{incident_number}} إلى طلب رقم {{request_number}}",
		Variables:              `["incident_number","request_number"]`,
		OnlyIfNoActionTemplate: true,
	},
	{
		Code: "INCIDENT_MISSING_INFO_SMS", Name: "Closure: missing information", ModuleType: "incident", ActionType: "missing_info",
		BodyEN:                 "Dear Citizen, your incident (ID: {{incident_number}}) has been closed due to insufficient information. We kindly request that you raise it again with all the required details so we can assist you better. Thank you!",
		BodyAR:                 "عزيزي المواطن، تم إغلاق بلاغك رقم {{incident_number}} لعدم كفاية المعلومات. نأمل إعادة رفع البلاغ مع تعبئة جميع البيانات المطلوبة. شكرًا لكم.",
		Variables:              `["incident_number"]`,
		OnlyIfNoActionTemplate: true,
	},
	{
		Code: "SLA_POLICY_STEP_SMS", Name: "SLA escalation policy step", ModuleType: "incident",
		BodyEN:    "SLA ALERT [Step {{step_order}}] — {{policy_name}}: Incident {{incident_number}} in '{{state_name}}' for {{hours_in_state}}h (limit {{sla_hours}}h). View: {{incident_url}}",
		BodyAR:    "تنبيه SLA [الخطوة {{step_order}}] — {{policy_name}}: البلاغ {{incident_number}} في حالة '{{state_name}}' منذ {{hours_in_state}} ساعة (الحد {{sla_hours}} ساعة). عرض: {{incident_url}}",
		Variables: `["step_order","policy_name","incident_number","state_name","hours_in_state","sla_hours","incident_url"]`,
	},
	{
		Code: "SLA_GLOBAL_BREACH_SMS", Name: "SLA global breach", ModuleType: "incident",
		BodyEN:    "SLA BREACHED: Incident {{incident_number}} (\"{{incident_title}}\") has been open {{hours_open}}h (SLA: {{sla_hours}}h). Immediate action required. View: {{incident_url}}",
		BodyAR:    "تجاوز SLA: البلاغ {{incident_number}} (\"{{incident_title}}\") مفتوح منذ {{hours_open}} ساعة (SLA: {{sla_hours}} ساعة). مطلوب إجراء فوري. عرض: {{incident_url}}",
		Variables: `["incident_number","incident_title","hours_open","sla_hours","incident_url"]`,
	},
	{
		Code: "SLA_GROUP_BREACH_SMS", Name: "SLA breach digest (escalation group)", ModuleType: "incident",
		BodyEN:    "Dear {{first_name}} {{last_name}}, This is an automated SLA breach notification. {{incident_count}} incident(s) exceeded SLA. Classification: {{classification_name}}. View Incidents: {{sla_page_url}}. - SLA Monitoring System",
		BodyAR:    "عزيزي {{first_name}} {{last_name}}، هذا تنبيه آلي بتجاوز SLA. عدد البلاغات المتجاوزة: {{incident_count}}. التصنيف: {{classification_name}}. عرض البلاغات: {{sla_page_url}}",
		Variables: `["first_name","last_name","incident_count","classification_name","sla_page_url"]`,
	},
}

// SeedDefaultSMSTemplates inserts the default SMS templates that replace formerly
// hardcoded SMS text. Idempotent: existing codes (including admin edits) are never touched.
func SeedDefaultSMSTemplates(db *gorm.DB) error {
	for _, t := range defaultSMSTemplates {
		var count int64
		if err := db.Table("notification_templates").
			Where("code = ? AND channel = 'sms' AND deleted_at IS NULL", t.Code).
			Count(&count).Error; err != nil {
			return err
		}
		if count > 0 {
			continue
		}
		if t.OnlyIfNoActionTemplate {
			if err := db.Table("notification_templates").
				Where("action_type = ? AND channel = 'sms' AND is_active = true AND deleted_at IS NULL", t.ActionType).
				Count(&count).Error; err != nil {
				return err
			}
			if count > 0 {
				log.Printf("[SMS-SEED] skipping %s: an active %s SMS template already exists", t.Code, t.ActionType)
				continue
			}
		}
		row := map[string]any{
			"id": uuid.New(), "name": t.Name, "code": t.Code, "channel": "sms",
			"module_type": t.ModuleType, "action_type": t.ActionType,
			"body_en": t.BodyEN, "body_ar": t.BodyAR, "variables": t.Variables,
			"is_active": true, "created_at": gorm.Expr("NOW()"), "updated_at": gorm.Expr("NOW()"),
		}
		if err := db.Table("notification_templates").Create(row).Error; err != nil {
			log.Printf("[SMS-SEED] failed to seed %s: %v", t.Code, err)
			return err
		}
		log.Printf("[SMS-SEED] seeded default SMS template %s", t.Code)
	}
	return nil
}
