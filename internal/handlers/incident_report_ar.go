package handlers

import (
	"encoding/json"
	"regexp"
	"strings"

	"github.com/automax/backend/internal/models"
)

var revisionActionLabelsAr = map[string]string{
	"field_change":       "تعديل حقل",
	"comment_added":      "إضافة تعليق",
	"comment_modified":   "تعديل تعليق",
	"comment_deleted":    "حذف تعليق",
	"attachment_added":   "إضافة مرفق",
	"attachment_removed": "حذف مرفق",
	"assignee_changed":   "تغيير المسؤول",
	"status_changed":     "تغيير الحالة",
	"created":            "إنشاء",
	"ivr_sms_sent":       "إرسال رسالة IVR",
	"ivr_sms_submitted":  "تقديم رسالة IVR",
}

// Field labels that hold names of entities (states, classifications, ...) whose
// old/new values can be translated through the name map.
var revisionFieldLabelsAr = map[string]string{
	"Status":                 "الحالة",
	"Title":                  "عنوان البلاغ",
	"Description":            "الوصف",
	"Classification":         "التصنيف",
	"Assigned To":            "المسؤول",
	"AssignedTo":             "المسؤول",
	"Department":             "القسم",
	"Location":               "الموقع",
	"Due Date":               "تاريخ الاستحقاق",
	"Reporter Name":          "اسم المُبلِّغ",
	"Reporter Email":         "البريد الإلكتروني",
	"Reporter Phone":         "رقم الجوال",
	"Custom Fields":          "الحقول المخصصة",
	"Dynamic Attributes":     "السمات الديناميكية",
	"GIS Location":           "الموقع الجغرافي",
	"Partial Close Duration": "مدة الإغلاق الجزئي",
	"Master Incident":        "البلاغ الرئيسي",
	"Merged Incidents":       "البلاغات المدمجة",
	"Priority":               "الأولوية",
}

var nameValueFields = map[string]bool{"Status": true, "Classification": true, "Department": true, "Location": true}

var sourceLabelsAr = map[string]string{
	"web": "الموقع الإلكتروني", "mobile": "تطبيق الجوال", "ivr": "الرد الآلي (IVR)",
	"whatsapp": "واتساب", "facebook": "فيسبوك", "twitter": "تويتر", "email": "البريد الإلكتروني",
	"epmportal": "بوابة EPM", "viusional": "Visional",
}

var recordTypeLabelsAr = map[string]string{
	"incident": "بلاغ", "request": "طلب", "complaint": "شكوى", "query": "استفسار",
}

func localizeEnum(ar bool, m map[string]string, v string) string {
	if !ar {
		return v
	}
	if t, ok := m[strings.ToLower(strings.TrimSpace(v))]; ok {
		return t
	}
	return v
}

// descTemplate translates one family of auto-generated revision sentences.
// Capture groups listed in names are looked up in the name map; the others are
// inserted verbatim (numbers, user text, file names).
type descTemplate struct {
	re    *regexp.Regexp
	ar    string // uses {1},{2}... placeholders
	names map[int]bool
	enums map[int]map[string]string
}

func tpl(pattern, ar string, nameGroups ...int) descTemplate {
	t := descTemplate{re: regexp.MustCompile("(?s)^" + pattern + "$"), ar: ar, names: map[int]bool{}}
	for _, g := range nameGroups {
		t.names[g] = true
	}
	return t
}

var descTemplates = func() []descTemplate {
	rt := func(g int) map[int]map[string]string { return map[int]map[string]string{g: recordTypeLabelsAr} }
	created := tpl(`(incident|request|complaint|query) (\S+) created`, "تم إنشاء {1} {2}")
	created.enums = rt(1)
	created2 := tpl(`(Incident|Request|Complaint|Query) (\S+) created`, "تم إنشاء {1} {2}")
	created2.enums = rt(1)
	return []descTemplate{
		created, created2,
		tpl(`Fields updated`, "تم تحديث الحقول"),
		tpl(`Title changed from (.*) to (.*)`, "تغير العنوان من {1} إلى {2}"),
		tpl(`Description changed`, "تم تغيير الوصف"),
		tpl(`Dynamic attributes updated`, "تم تحديث السمات الديناميكية"),
		tpl(`Custom fields updated`, "تم تحديث الحقول المخصصة"),
		tpl(`Classification cleared \(was (.*)\)`, "تم مسح التصنيف (كان {1})", 1),
		tpl(`Classification changed from (.*?)(?: to (.*))?`, "تغير التصنيف من {1} إلى {2}", 1, 2),
		tpl(`Department cleared \(was (.*)\)`, "تم مسح القسم (كان {1})", 1),
		tpl(`Department changed from (.*?)(?: to (.*))?`, "تغير القسم من {1} إلى {2}", 1, 2),
		tpl(`Location cleared \(was (.*)\)`, "تم مسح الموقع (كان {1})", 1),
		tpl(`Location changed from (.*?)(?: to (.*))?`, "تغير الموقع من {1} إلى {2}", 1, 2),
		tpl(`Location changed to '(.*)' \(synced from master incident (\S+)\)`, "تغير الموقع إلى '{1}' (مزامنة من البلاغ الرئيسي {2})", 1),
		tpl(`AssignedTo changed from (.*) to Unassigned`, "تغير المسؤول من {1} إلى غير معيّن"),
		tpl(`AssignedTo changed from (.*?)(?: to (.*))?`, "تغير المسؤول من {1} إلى {2}"),
		tpl(`Due Date cleared \(was (.*)\)`, "تم مسح تاريخ الاستحقاق (كان {1})"),
		tpl(`Due Date changed from (.*) to (.*)`, "تغير تاريخ الاستحقاق من {1} إلى {2}"),
		tpl(`Status changed from (.*?) to (.*?)(?: — Partial Close Duration: (.*?))?(?:; Comment: (.*))?`, "تغيرت الحالة من {1} إلى {2}", 1, 2),
		tpl(`Automatic reversion: status changed from (.*) to (.*) \(Partial Close expired\)`, "رجوع تلقائي: تغيرت الحالة من {1} إلى {2} (انتهت مدة الإغلاق الجزئي)", 1, 2),
		tpl(`Pre-expiry notification sent: Partial Close expires in (.*) \(at (.*)\)`, "تم إرسال تنبيه ما قبل الانتهاء: ينتهي الإغلاق الجزئي خلال {1} (في {2})"),
		tpl(`Comment added by (.*?) - (.*)`, "تمت إضافة تعليق بواسطة {1} - {2}"),
		tpl(`Comment modified - (.*)`, "تم تعديل التعليق - {1}"),
		tpl(`Comment deleted - (.*)`, "تم حذف التعليق - {1}"),
		tpl(`Attachment added to master incident (\S+) - (.*)`, "تمت إضافة مرفق إلى البلاغ الرئيسي {1} - {2}"),
		tpl(`Attachment added - (.*)`, "تمت إضافة مرفق - {1}"),
		tpl(`Attachment removed - (.*)`, "تم حذف المرفق - {1}"),
		tpl(`Incident linked to existing request (\S+)`, "تم ربط البلاغ بالطلب القائم {1}"),
		tpl(`Incident (\S+) converted and linked to this request`, "تم تحويل البلاغ {1} وربطه بهذا الطلب"),
		tpl(`Incident converted to request (\S+) \(bulk conversion\)`, "تم تحويل البلاغ إلى الطلب {1} (تحويل جماعي)"),
		tpl(`Incident converted to request (\S+)`, "تم تحويل البلاغ إلى الطلب {1}"),
		tpl(`Request created from incident (\S+)`, "تم إنشاء الطلب من البلاغ {1}"),
		tpl(`Feedback provided during conversion to request (\S+)`, "تم تقديم ملاحظات أثناء التحويل إلى الطلب {1}"),
		tpl(`(\d+) incidents added to existing request via bulk conversion`, "تمت إضافة {1} بلاغات إلى طلب قائم عبر التحويل الجماعي"),
		tpl(`Request created from (\d+) incidents via bulk conversion`, "تم إنشاء طلب من {1} بلاغات عبر التحويل الجماعي"),
		tpl(`(\d+) ticket\(s\) merged into this master ticket: (.*)`, "تم دمج {1} بلاغ(ات) في هذا البلاغ الرئيسي: {2}"),
		tpl(`This ticket is merged into the Master ticket (\S+)`, "تم دمج هذا البلاغ في البلاغ الرئيسي {1}"),
		tpl(`Incident unmerged from master \(bulk\)`, "تم فك دمج البلاغ من البلاغ الرئيسي (جماعي)"),
		tpl(`Incident unmerged from master`, "تم فك دمج البلاغ من البلاغ الرئيسي"),
		tpl(`Automatically closed due to master incident (\S+) being closed`, "تم الإغلاق تلقائياً بسبب إغلاق البلاغ الرئيسي {1}"),
		tpl(`IVR SMS sent to citizen on incident creation`, "تم إرسال رسالة IVR للمواطن عند إنشاء البلاغ"),
		tpl(`Citizen submitted additional information via IVR SMS link`, "قدّم المواطن معلومات إضافية عبر رابط رسالة IVR"),
		tpl(`(.+) changed from (.*) to (.*)`, "تغير {1} من {2} إلى {3}"),
	}
}()

var statusTemplateRe = regexp.MustCompile(`^Status changed from `)

// localizeReportLogsAr translates the parts of the revision / transition logs that
// are stored as English text at write time (names, action types, field labels,
// generated sentences) so the Arabic report does not mix languages. Free-text
// user content (comments, titles, descriptions) is left untouched.
func localizeReportLogsAr(nameMap map[string]string, revisions []models.IncidentReportRevision, transitions []models.IncidentReportTransition) {
	name := func(v string) string {
		v = strings.TrimSpace(v)
		if ar, ok := nameMap[v]; ok {
			return ar
		}
		if v == "Unassigned" {
			return "غير معيّن"
		}
		return v
	}

	moreRe := regexp.MustCompile(`(?s)^(.+) and (\d+) more changes$`)
	var translate func(text string) string
	translate = func(text string) string {
		if m := moreRe.FindStringSubmatch(text); m != nil {
			return translate(m[1]) + " و" + m[2] + " تغييرات أخرى"
		}
		// "Status changed ...; Label: value" lookup suffixes
		if statusTemplateRe.MatchString(text) && !strings.Contains(text, "; Comment:") {
			if idx := strings.Index(text, "; "); idx >= 0 {
				head := translate(text[:idx])
				rest := strings.Split(text[idx+2:], "; ")
				for i, part := range rest {
					if k := strings.Index(part, ": "); k > 0 {
						lbl := part[:k]
						if ar, ok := revisionFieldLabelsAr[lbl]; ok {
							lbl = ar
						}
						rest[i] = lbl + ": " + name(part[k+2:])
					}
				}
				return head + "؛ " + strings.Join(rest, "؛ ")
			}
		}
		// "<typed comment> | <generated sentence>" — translate only the generated part.
		if idx := strings.Index(text, " | "); idx >= 0 {
			tail := translate(text[idx+3:])
			if tail != text[idx+3:] {
				return text[:idx] + " | " + tail
			}
		}
		// "<generated sentence>; Label: value" — keep value, translate label.
		for _, t := range descTemplates {
			m := t.re.FindStringSubmatch(text)
			if m == nil {
				continue
			}
			out := t.ar
			for g := 1; g < len(m); g++ {
				if m[g] == "" && strings.Contains(out, " إلى {"+itoa(g)+"}") {
					out = strings.ReplaceAll(out, " إلى {"+itoa(g)+"}", "")
				}
			}
			for g := 1; g < len(m); g++ {
				v := m[g]
				switch {
				case t.names[g]:
					v = name(v)
				case t.enums[g] != nil:
					v = localizeEnum(true, t.enums[g], v)
				case strings.HasPrefix(t.ar, "تغير {1} من"):
					if g == 1 {
						if ar, ok := revisionFieldLabelsAr[v]; ok {
							v = ar
						}
					} else {
						v = name(v)
					}
				}
				out = strings.ReplaceAll(out, "{"+itoa(g)+"}", v)
			}
			// status sentences may carry duration / comment suffixes
			if statusTemplateRe.MatchString(text) {
				if len(m) > 3 && m[3] != "" {
					out += " — مدة الإغلاق الجزئي: " + m[3]
				}
				if len(m) > 4 && m[4] != "" {
					out += "؛ التعليق: " + m[4]
				}
			}
			return out
		}
		return text
	}

	for i := range revisions {
		rev := &revisions[i]
		if ar, ok := revisionActionLabelsAr[rev.ActionType]; ok {
			rev.ActionType = ar
		}
		rev.ActionDescription = translate(rev.ActionDescription)
		if rev.Changes == "" {
			continue
		}
		var changes []models.IncidentRevisionChange
		if json.Unmarshal([]byte(rev.Changes), &changes) != nil {
			continue
		}
		for j := range changes {
			ch := &changes[j]
			isName := nameValueFields[ch.FieldLabel] || ch.FieldLabel == "Assigned To"
			if ar, ok := revisionFieldLabelsAr[ch.FieldLabel]; ok {
				ch.FieldLabel = ar
			}
			if isName {
				if ch.OldValue != nil {
					v := name(*ch.OldValue)
					ch.OldValue = &v
				}
				if ch.NewValue != nil {
					v := name(*ch.NewValue)
					ch.NewValue = &v
				}
			}
		}
		if out, err := json.Marshal(changes); err == nil {
			rev.Changes = string(out)
		}
	}

	for i := range transitions {
		transitions[i].Comment = translate(transitions[i].Comment)
	}
}

func itoa(n int) string { return string(rune('0' + n)) }
