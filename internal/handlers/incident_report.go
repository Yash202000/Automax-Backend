package handlers

import (
	"bytes"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"html"
	"html/template"
	"io"
	"log"
	"net/http"
	"os"
	"os/exec"
	"strings"
	"time"

	"github.com/automax/backend/internal/models"
	"github.com/automax/backend/pkg/constants"
	"github.com/automax/backend/pkg/i18n"
	"github.com/automax/backend/pkg/utils"
	"github.com/gofiber/fiber/v2"
	"github.com/google/uuid"
)

// ── translations ─────────────────────────────────────────────────────────────

type reportLabels struct {
	Dir               string
	Title             string
	SectionIncident   string
	SectionReporter   string
	SectionLocation   string
	SectionHistory    string
	SectionComments   string
	SectionRevisions  string
	SectionCaller     string
	SectionAttach     string
	IncidentNo        string
	Date              string
	Status            string
	Channel           string
	Classification    string
	LocationLbl       string
	Description       string
	Title2            string
	Source            string
	Priority          string
	SLA               string
	SLABreached       string
	SLADeadline       string
	DueDate           string
	ResolvedAt        string
	ClosedAt          string
	Reporter          string
	ReporterEmail     string
	ReporterMobile    string
	ReporterName      string
	Assignee          string
	Department        string
	Workflow          string
	Latitude          string
	Longitude         string
	Address           string
	City              string
	State             string
	Country           string
	PostalCode        string
	RecordTypeLbl     string
	Comment           string
	CommentBy         string
	Internal          string
	ColDate           string
	ColName           string
	ColAction         string
	ColTransition     string
	ColComment        string
	ColFeedback       string
	ColField          string
	ColOldValue       string
	ColNewValue       string
	CallerName        string
	CallerMobile      string
	CallerEmail       string
	AttName           string
	AttType           string
	AttSize           string
	AttUploadedBy     string
	AttUploadedByRole string
	AttUploadedAt     string
	AttDeleted        string
	AttDeletedAt      string
	AttBeforeImage    string
	AttAfterImage     string
	PrintDate         string
	Yes               string
	No                string
	PriorityLabels    [6]string // index 1-5
}

var labelsAR = reportLabels{
	Dir:               "rtl",
	Title:             "تفاصيل البلاغ",
	SectionIncident:   "البلاغ",
	SectionReporter:   "مقدم البلاغ",
	SectionLocation:   "الموقع",
	SectionHistory:    "سجل العمليات",
	SectionComments:   "التعليقات",
	SectionRevisions:  "سجل التعديلات",
	SectionCaller:     "تفاصيل المتصل",
	SectionAttach:     "المرفقات",
	IncidentNo:        "رقم البلاغ",
	Date:              "تاريخ البلاغ",
	Status:            "الحالة",
	Channel:           "القناة",
	Classification:    "التصنيف",
	LocationLbl:       "الموقع",
	Description:       "الوصف",
	Title2:            "عنوان البلاغ",
	Source:            "المصدر",
	Priority:          "الأولوية",
	SLA:               "SLA",
	SLABreached:       "تجاوز SLA",
	SLADeadline:       "موعد SLA",
	DueDate:           "تاريخ الاستحقاق",
	ResolvedAt:        "تاريخ الحل",
	ClosedAt:          "تاريخ الإغلاق",
	Reporter:          "المُبلِّغ",
	ReporterEmail:     "البريد الإلكتروني",
	ReporterMobile:    "رقم الجوال",
	ReporterName:      "اسم المُبلِّغ",
	Assignee:          "المسؤول",
	Department:        "القسم",
	Workflow:          "سير العمل",
	Latitude:          "خط العرض",
	Longitude:         "خط الطول",
	Address:           "العنوان",
	City:              "المدينة",
	State:             "المنطقة",
	Country:           "الدولة",
	PostalCode:        "الرمز البريدي",
	RecordTypeLbl:     "نوع السجل",
	Comment:           "التعليق",
	CommentBy:         "بواسطة",
	Internal:          "[تعليق داخلي]",
	ColDate:           "التاريخ",
	ColName:           "الاسم",
	ColAction:         "الإجراء",
	ColTransition:     "الانتقال",
	ColComment:        "التعليقات",
	ColFeedback:       "التغذية الراجعة",
	ColField:          "الحقل",
	ColOldValue:       "القيمة القديمة",
	ColNewValue:       "القيمة الجديدة",
	CallerName:        "اسم المتصل",
	CallerMobile:      "رقم المتصل",
	CallerEmail:       "بريد المتصل",
	AttName:           "اسم الملف",
	AttType:           "النوع",
	AttSize:           "الحجم",
	AttUploadedBy:     "رُفع بواسطة",
	AttUploadedByRole: "الدور",
	AttUploadedAt:     "تاريخ الرفع",
	AttDeleted:        "تم حذف هذا الملف",
	AttDeletedAt:      "تاريخ الحذف",
	AttBeforeImage:    "قبل",
	AttAfterImage:     "بعد",
	PrintDate:         "تاريخ الطباعة",
	Yes:               "نعم",
	No:                "لا",
	PriorityLabels:    [6]string{"", "حرجة", "عالية", "متوسطة", "منخفضة", "منخفضة جداً"},
}

var labelsEN = reportLabels{
	Dir:               "ltr",
	Title:             "Incident Report",
	SectionIncident:   "Incident Details",
	SectionReporter:   "Reporter",
	SectionLocation:   "Location",
	SectionHistory:    "Transition History",
	SectionComments:   "Comments",
	SectionRevisions:  "Revision History",
	SectionCaller:     "Caller Details",
	SectionAttach:     "Attachments",
	IncidentNo:        "Incident No.",
	Date:              "Created Date",
	Status:            "Status",
	Channel:           "Channel",
	Classification:    "Classification",
	LocationLbl:       "Location",
	Description:       "Description",
	Title2:            "Title",
	Source:            "Source",
	Priority:          "Priority",
	SLA:               "SLA",
	SLABreached:       "SLA Breached",
	SLADeadline:       "SLA Deadline",
	DueDate:           "Due Date",
	ResolvedAt:        "Resolved At",
	ClosedAt:          "Closed At",
	Reporter:          "Reporter",
	ReporterEmail:     "Reporter Email",
	ReporterMobile:    "Reporter Mobile",
	ReporterName:      "Reporter Name",
	Assignee:          "Assigned To",
	Department:        "Department",
	Workflow:          "Workflow",
	Latitude:          "Latitude",
	Longitude:         "Longitude",
	Address:           "Address",
	City:              "City",
	State:             "State",
	Country:           "Country",
	PostalCode:        "Postal Code",
	RecordTypeLbl:     "Record Type",
	Comment:           "Comment",
	CommentBy:         "By",
	Internal:          "[Internal Comment]",
	ColDate:           "Date",
	ColName:           "Name",
	ColAction:         "Action",
	ColTransition:     "Transition",
	ColComment:        "Comment",
	ColFeedback:       "Feedback",
	ColField:          "Field",
	ColOldValue:       "Old Value",
	ColNewValue:       "New Value",
	CallerName:        "Caller Name",
	CallerMobile:      "Caller Mobile",
	CallerEmail:       "Caller Email",
	AttName:           "File Name",
	AttType:           "Type",
	AttSize:           "Size",
	AttUploadedBy:     "Uploaded By",
	AttUploadedByRole: "Role",
	AttUploadedAt:     "Uploaded At",
	AttDeleted:        "This file has been deleted",
	AttDeletedAt:      "Deleted At",
	AttBeforeImage:    "Before Image",
	AttAfterImage:     "After Image",
	PrintDate:         "Print Date",
	Yes:               "Yes",
	No:                "No",
	PriorityLabels:    [6]string{"", "Critical", "High", "Medium", "Low", "Very Low"},
}

// ── handler ───────────────────────────────────────────────────────────────────

func (h *IncidentHandler) GenerateReport(c *fiber.Ctx) error {
	idStr := c.Params("id")
	id, err := uuid.Parse(idStr)
	if err != nil {
		return utils.ErrorResponse(c, fiber.StatusBadRequest, i18n.T(c.UserContext(), "invalid_id"))
	}

	reportData, err := h.incidentRepo.GetReportIncidentData(c.UserContext(), id)
	if err != nil {
		return utils.ErrorResponse(c, fiber.StatusNotFound, i18n.T(c.UserContext(), "incident_not_found"))
	}

	if reportData.LocationID != nil {
		reportData.LocationName, err = h.locationRepo.FetchLocationFullPathByID(c.UserContext(), *reportData.LocationID)
		if err != nil {
			log.Printf("Location err: %v", err)
		}
	}

	if reportData.ClassificationID != nil {
		reportData.ClassificationName, err = h.classificationRepo.FetchClassificationFullPathByID(c.UserContext(), *reportData.ClassificationID)
		if err != nil {
			log.Printf("Classification err: %v", err)
		}
	}

	reportLookupValues, err := h.incidentRepo.GetReportLookupValues(c.UserContext(), id)
	if err != nil {
		reportLookupValues = nil
	}

	reportTransitions, err := h.incidentRepo.GetReportTransitions(c.UserContext(), id)
	if err != nil {
		return utils.ErrorResponse(c, fiber.StatusInternalServerError, i18n.T(c.UserContext(), "failed_to_fetch_transitions"))
	}

	reportAttachments, err := h.incidentRepo.GetReportAttachments(c.UserContext(), id)
	if err != nil {
		return utils.ErrorResponse(c, fiber.StatusInternalServerError, i18n.T(c.UserContext(), "failed_to_fetch_attachments"))
	}

	reportRevisions, err := h.incidentRepo.GetReportRevisions(c.UserContext(), id)
	if err != nil {
		reportRevisions = nil
	}

	lbl := labelsAR
	if c.Query("lang", "ar") == "en" {
		lbl = labelsEN
	}

	var reportCustomFields []reportCustomField
	if reportData.CustomFields != "" && h.lookupRepo != nil {
		var cf map[string]struct {
			CategoryID string `json:"category_id"`
			FieldType  string `json:"field_type"`
			Value      string `json:"value"`
		}
		if json.Unmarshal([]byte(reportData.CustomFields), &cf) == nil {
			for _, entry := range cf {
				catID, parseErr := uuid.Parse(entry.CategoryID)
				if parseErr != nil {
					continue
				}
				cat, fetchErr := h.lookupRepo.FindCategoryByID(c.UserContext(), catID)
				if fetchErr != nil {
					continue
				}
				label := cat.Name
				if lbl.Dir == "rtl" && cat.NameAr != "" {
					label = cat.NameAr
				}
				link := ""
				if cat.RedirectURL != "" {
					link = strings.ReplaceAll(cat.RedirectURL, ":id", entry.Value)
				}
				reportCustomFields = append(reportCustomFields, reportCustomField{
					Label: label,
					Value: entry.Value,
					URL:   link,
				})
			}
		}
	}

	leftLogoB64 := fetchLogoBase64(h.cfg.Report.LogoLeftURL)
	rightLogoB64 := fetchLogoBase64(h.cfg.Report.LogoRightURL)

	format := c.Query("format", "pdf")
	htmlBytes := buildReportHTML(c, h, reportData, reportLookupValues, leftLogoB64, rightLogoB64, lbl, reportTransitions, reportAttachments, reportRevisions, reportCustomFields)

	switch format {
	case "html":
		c.Set("Content-Type", "text/html; charset=utf-8")
		return c.Send(htmlBytes)

	case "pdf":
		tmpHTML, terr := os.CreateTemp("", "report-*.html")
		if terr != nil {
			return utils.ErrorResponse(c, fiber.StatusInternalServerError, i18n.T(c.UserContext(), "failed_to_create_temp"))
		}
		defer os.Remove(tmpHTML.Name())
		if _, terr = tmpHTML.Write(htmlBytes); terr != nil {
			return utils.ErrorResponse(c, fiber.StatusInternalServerError, i18n.T(c.UserContext(), "failed_to_write_html"))
		}
		tmpHTML.Close()

		tmpPDF, terr := os.CreateTemp("", "report-*.pdf")
		if terr != nil {
			return utils.ErrorResponse(c, fiber.StatusInternalServerError, i18n.T(c.UserContext(), "failed_to_create_pdf_temp"))
		}
		pdfPath := tmpPDF.Name()
		tmpPDF.Close()
		defer os.Remove(pdfPath)

		chromeBin := h.cfg.Report.ChromeBin

		var stderr bytes.Buffer
		cmd := exec.CommandContext(c.UserContext(),
			chromeBin,
			"--headless", "--disable-gpu", "--no-sandbox", "--disable-dev-shm-usage",
			"--no-margins", "--print-to-pdf-no-header",
			fmt.Sprintf("--print-to-pdf=%s", pdfPath),
			fmt.Sprintf("file://%s", tmpHTML.Name()),
		)
		cmd.Stderr = &stderr
		if terr = cmd.Run(); terr != nil {
			return utils.ErrorResponse(c, fiber.StatusInternalServerError,
				fmt.Sprintf("PDF generation failed: exec_error=%s stderr=%s", terr.Error(), stderr.String()))
		}

		pdfData, terr := os.ReadFile(pdfPath)
		if terr != nil || len(pdfData) == 0 {
			log.Println("report pdf data err", err)
			return utils.ErrorResponse(c, fiber.StatusInternalServerError, i18n.T(c.UserContext(), "failed_to_read_pdf"))
		}

		c.Set("Content-Type", "application/pdf")
		c.Set("Content-Disposition", fmt.Sprintf(
			`attachment; filename="incident_%s_%s.pdf"`,
			reportData.IncidentNumber, time.Now().In(appTimezone(h.cfg.Report.AppRegion)).Format("20060102"),
		))
		return c.Send(pdfData)

	case "json":
		c.Set("Content-Type", "application/json")
		c.Set("Content-Disposition", fmt.Sprintf(`attachment; filename=incident_%s_%s.json`,
			reportData.IncidentNumber, time.Now().In(appTimezone(h.cfg.Report.AppRegion)).Format("20060102")))
		return c.JSON(map[string]interface{}{"generated_at": time.Now().In(appTimezone(h.cfg.Report.AppRegion)).Format(time.RFC3339), "incident": reportData})

	default:
		return utils.ErrorResponse(c, fiber.StatusBadRequest, i18n.T(c.UserContext(), "use_format_pdf_html_json"))
	}
}

// appTimezone returns the report timezone based on the configured app region.
// appRegion=SA → Arabia Standard Time (UTC+3); anything else → IST (UTC+5:30).
func appTimezone(appRegion string) *time.Location {
	if strings.ToUpper(appRegion) == "SA" {
		return time.FixedZone("AST", 3*60*60)
	}
	return time.FixedZone("IST", 5*60*60+30*60)
}

// ── HTML builder ──────────────────────────────────────────────────────────────
func fetchLogoBase64(url string) string {
	resp, err := http.Get(url)
	if err != nil || resp.StatusCode != 200 {
		return ""
	}
	defer resp.Body.Close()
	data, err := io.ReadAll(resp.Body)
	if err != nil {
		return ""
	}
	return base64.StdEncoding.EncodeToString(data)
}

type reportCustomField struct {
	Label string
	Value string
	URL   string
}

func buildReportHTML(
	c *fiber.Ctx,
	h *IncidentHandler,
	data *models.IncidentReportData,
	lookupValues []models.IncidentReportLookupValue,
	leftLogoB64 string,
	rightLogoB64 string,
	l reportLabels,
	reportTransitions []models.IncidentReportTransition,
	reportAttachments []models.IncidentReportAttachment,
	reportRevisions []models.IncidentReportRevision,
	customFields []reportCustomField,
) []byte {
	var b bytes.Buffer

	// helpers
	tz := appTimezone(h.cfg.Report.AppRegion)
	ts := func(t time.Time) string { return t.In(tz).Format("02/01/2006 03:04 PM") }
	tsp := func(t *time.Time) string {
		if t == nil {
			return ""
		}
		return ts(*t)
	}
	localName := func(en, ar string) string {
		if l.Dir == "rtl" && ar != "" {
			return ar
		}
		return en
	}

	// helper: safely dereference *string
	ptrStr := func(p *string) string {
		if p == nil {
			return ""
		}
		return *p
	}

	b.WriteString(fmt.Sprintf(`<!DOCTYPE html>
<html dir="%s" lang="%s">
<head><meta charset="UTF-8"><title>%s</title>
<style>
*{box-sizing:border-box;margin:0;padding:0}
body{font-family:'Segoe UI',Tahoma,Arial,sans-serif;font-size:10.5pt;color:#222;background:#fff;direction:%s}
.page{max-width:780px;margin:0 auto;padding:14px}
.logo-bar{display:flex;justify-content:space-between;align-items:center;padding:6px 0 8px 0}
.logo-bar img{height:56px;width:auto;object-fit:contain}
.main-header{background:#375a6e;color:#fff;text-align:center;padding:10px 8px;font-size:12pt;font-weight:bold;letter-spacing:0.5px}
.section-header{background:#6491a5;color:#fff;text-align:center;padding:5px 8px;font-size:11pt;font-weight:bold}
.grid{width:100%%;border-collapse:collapse}
.grid tr:nth-child(even) td{background:#e8f4fa}
.grid tr:nth-child(odd) td{background:#fff}
.grid td{padding:4px 7px;border-bottom:1px solid #c8dce4;font-size:9.5pt;vertical-align:middle}
.grid td.lbl{color:#505050;width:22%%;text-align:%s;border-%s:1px solid #c8dce4;white-space:nowrap}
.grid td.val{color:#b41e1e;font-weight:bold;width:28%%;text-align:%s}
.tbl{width:100%%;border-collapse:collapse}
.tbl thead tr{background:#375a6e;color:#fff}
.tbl thead td{padding:5px 7px;font-size:9.5pt;font-weight:bold;text-align:center}
.tbl tbody tr:nth-child(even) td{background:#e8f4fa}
.tbl tbody tr:nth-child(odd) td{background:#fff}
.tbl tbody td{padding:4px 7px;border-bottom:1px solid #c8dce4;font-size:9pt;text-align:center}
.tbl tbody td.cmt{text-align:%s}
.comment-block{padding:6px 8px;border-bottom:1px solid #c8dce4;font-size:9pt}
.comment-block:nth-child(even){background:#e8f4fa}
.comment-meta{font-size:8pt;color:#666;margin-bottom:3px}
.comment-internal{color:#c00;font-size:8pt;margin-top:2px}
.att-card{border:1px solid #c8dce4;margin:6px 0;overflow:hidden}
.att-name{padding:5px 8px;font-weight:bold;font-size:9.5pt;color:#375a6e;border-bottom:1px solid #c8dce4}
.att-transition{display:flex;flex-wrap:wrap;gap:12px;padding:4px 8px;font-size:8.5pt;background:#e8f1f5;color:#375a6e;border-bottom:1px solid #c8dce4}
.att-img{display:block;max-width:100%%;max-height:280px;object-fit:contain;margin:0 auto;padding:6px}
.att-meta{display:flex;flex-wrap:wrap;gap:12px;padding:5px 8px;font-size:8.5pt;background:#f0f8fc;border-top:1px solid #c8dce4}
.att-card:nth-child(even){background:#f7fbfd}
.att-group-header{background:#6491a5;color:#fff;padding:5px 8px;font-size:10pt;font-weight:bold;margin:10px 0 4px 0}
.att-deleted{padding:10px 12px;color:#c0392b;background:#fdf3f2;border:1px dashed #c0392b;margin:8px;border-radius:4px;font-size:9pt;text-align:center;font-weight:bold}
.badge-breached{color:#fff;background:#c0392b;padding:1px 5px;border-radius:3px;font-size:8pt}
.badge-ok{color:#fff;background:#27ae60;padding:1px 5px;border-radius:3px;font-size:8pt}
.footer{margin-top:14px;display:flex;justify-content:space-between;font-size:8pt;color:#888;border-top:1px solid #c8dce4;padding-top:5px}
</style></head><body><div class="page">`,
		l.Dir, func() string {
			if l.Dir == "rtl" {
				return "ar"
			}
			return "en"
		}(),
		html.EscapeString(l.Title),
		l.Dir,
		// lbl alignment
		func() string {
			if l.Dir == "rtl" {
				return "right"
			}
			return "left"
		}(),
		func() string {
			if l.Dir == "rtl" {
				return "right"
			}
			return "left"
		}(),
		func() string {
			if l.Dir == "rtl" {
				return "right"
			}
			return "left"
		}(),
		func() string {
			if l.Dir == "rtl" {
				return "right"
			}
			return "left"
		}(),
	))

	// ── Logo Bar + Title ─────────────────────────────────────────────────────
	// url := utils.GenerateAppURL(c.UserContext())
	// if leftLogoB64 == "" {
	// 	leftLogoB64 = fetchLogoBase64(url + "/epm-logo.png")

	// }

	// if rightLogoB64 == "" {
	// 	rightLogoB64 = fetchLogoBase64(url + "/callcenter.png")
	// }
	// ── Logo Bar + Title ─────────────────────────────────────────────────────

	// Logos row
	b.WriteString(`<div class="logo-bar">`)
	if leftLogoB64 != "" {
		fmt.Fprintf(&b, `<img src="data:image/png;base64,%s" alt="EPM Logo">`, leftLogoB64)
	} else {
		b.WriteString(`<span></span>`)
	}
	if rightLogoB64 != "" {
		fmt.Fprintf(&b, `<img src="data:image/png;base64,%s" alt="Call Center Logo">`, rightLogoB64)
	} else {
		b.WriteString(`<span></span>`)
	}
	b.WriteString(`</div>`)

	// Title heading below logos
	fmt.Fprintf(&b, `<div class="main-header">%s</div>`, html.EscapeString(l.Title)) //  ── Section: Incident Details ─────────────────────────────────────────────
	statusName := localName(data.StatusName, data.StatusNameAr)
	classDisplay := localName(data.ClassificationName, data.ClassificationNameAr)
	locationDisplay := localName(data.LocationName, data.LocationNameAr)

	slaStatus := l.No
	slaClass := "badge-ok"
	if data.SLABreached {
		slaStatus = l.Yes
		slaClass = "badge-breached"
	}

	secHeader(&b, l.SectionIncident)
	b.WriteString(`<table class="grid">`)
	row2(&b, l.IncidentNo, html.EscapeString(data.IncidentNumber), l.Date, html.EscapeString(ts(data.CreatedAt)))
	row2(&b, l.Source, html.EscapeString(data.Source), l.RecordTypeLbl, html.EscapeString(data.RecordType))
	row1(&b, l.Status, html.EscapeString(statusName))
	row1(&b, l.Title2, html.EscapeString(data.Title))

	// lookup values — group by category, one row per category
	if len(lookupValues) > 0 {
		type catGroup struct {
			label  string
			values []string
		}
		seen := make(map[uuid.UUID]int)
		var groups []catGroup
		for _, lv := range lookupValues {
			catLabel := localName(lv.CategoryName, lv.CategoryNameAr)
			if catLabel == "" {
				catLabel = lv.Code
			}
			valName := localName(lv.Name, lv.NameAr)
			if idx, ok := seen[lv.CategoryID]; ok {
				groups[idx].values = append(groups[idx].values, valName)
			} else {
				seen[lv.CategoryID] = len(groups)
				groups = append(groups, catGroup{label: catLabel, values: []string{valName}})
			}
		}
		for i := 0; i < len(groups); i += 2 {
			g1 := groups[i]
			if i+1 < len(groups) {
				g2 := groups[i+1]
				row2(&b,
					html.EscapeString(g1.label), html.EscapeString(strings.Join(g1.values, ", ")),
					html.EscapeString(g2.label), html.EscapeString(strings.Join(g2.values, ", ")),
				)
			} else {
				row1(&b, html.EscapeString(g1.label), html.EscapeString(strings.Join(g1.values, ", ")))
			}
		}
	}

	row1(&b, l.Classification, html.EscapeString(classDisplay))
	row1(&b, l.LocationLbl, html.EscapeString(locationDisplay))
	if data.Description != "" {
		row1(&b, l.Description, html.EscapeString(data.Description))
	}
	if data.SLADeadline != nil {
		row2(&b, l.SLABreached, fmt.Sprintf(`<span class="%s">%s</span>`, slaClass, html.EscapeString(slaStatus)),
			l.SLADeadline, html.EscapeString(tsp(data.SLADeadline)))
	} else {
		row1(&b, l.SLABreached, fmt.Sprintf(`<span class="%s">%s</span>`, slaClass, html.EscapeString(slaStatus)))
	}
	var dateFields [][2]string

	if data.DueDate != nil {
		dateFields = append(dateFields, [2]string{l.DueDate, html.EscapeString(tsp(data.DueDate))})
	}
	if data.ResolvedAt != nil {
		dateFields = append(dateFields, [2]string{l.ResolvedAt, html.EscapeString(tsp(data.ResolvedAt))})
	}
	if data.ClosedAt != nil {
		dateFields = append(dateFields, [2]string{l.ClosedAt, html.EscapeString(tsp(data.ClosedAt))})
	}

	// pair them: row2 for pairs, row1 for leftover
	for i := 0; i < len(dateFields); i += 2 {
		if i+1 < len(dateFields) {
			row2(&b, dateFields[i][0], dateFields[i][1], dateFields[i+1][0], dateFields[i+1][1])
		} else {
			row1(&b, dateFields[i][0], dateFields[i][1])
		}
	}
	b.WriteString(`</table>`)

	// ── Section: Custom Fields ────────────────────────────────────────────────
	if len(customFields) > 0 {
		cfHeader := "Custom Fields"
		if l.Dir == "rtl" {
			cfHeader = "الحقول المخصصة"
		}
		secHeader(&b, cfHeader)
		b.WriteString(`<table class="grid">`)
		for i := 0; i < len(customFields); i += 2 {
			cf1 := customFields[i]
			val1 := html.EscapeString(cf1.Value)
			if cf1.URL != "" {
				val1 = fmt.Sprintf(`<a href="%s" target="_blank">%s</a>`, html.EscapeString(cf1.URL), html.EscapeString(cf1.Value))
			}
			if i+1 < len(customFields) {
				cf2 := customFields[i+1]
				val2 := html.EscapeString(cf2.Value)
				if cf2.URL != "" {
					val2 = fmt.Sprintf(`<a href="%s" target="_blank">%s</a>`, html.EscapeString(cf2.URL), html.EscapeString(cf2.Value))
				}
				row2(&b, html.EscapeString(cf1.Label), val1, html.EscapeString(cf2.Label), val2)
			} else {
				row1(&b, html.EscapeString(cf1.Label), val1)
			}
		}
		b.WriteString(`</table>`)
	}

	// ── Section: Submitter ────────────────────────────────────────────────────

	assigneeName := data.AssigneesName
	if assigneeName == "" {
		assigneeName = strings.TrimSpace(data.AssigneeFirstName + " " + data.AssigneeLastName)
	}

	// Client / source specific report tweaks.
	//   VD2 client: the "Caller Details" section is hidden.
	//   'visional' source: the incident's own reporter (name/phone/email) replaces
	//   the internal creator's details inside the Reporter section.
	clientCode := strings.TrimSpace(h.cfg.ClientCode)
	isVD2 := strings.EqualFold(clientCode, constants.CLIENT_CODE.VD2)
	isVisionalSource := strings.EqualFold(strings.TrimSpace(data.Source), constants.INCIDENT_SOURCE.VIUSIONAL)

	secHeader(&b, l.SectionReporter)
	b.WriteString(`<table class="grid">`)
	if isVisionalSource {
		// data.CallerName / data.CallerPhone are the incident's reporter_name /
		// reporter_phone (aliased in the report query); data.ReporterEmail is the
		// incident's reporter_email.
		row2(&b, l.ReporterName, html.EscapeString(data.CallerName), l.Assignee, html.EscapeString(assigneeName))
		row2(&b, l.ReporterEmail, html.EscapeString(data.ReporterEmail), l.ReporterMobile, html.EscapeString(data.CallerPhone))
	} else {
		row2(&b, l.Reporter, html.EscapeString(data.CreatorFullName), l.Assignee, html.EscapeString(assigneeName))
		row2(&b, l.ReporterEmail, html.EscapeString(data.CreatorEmail), l.ReporterMobile, html.EscapeString(data.CreatorPhone))
	}
	row1(&b, l.Department, html.EscapeString(data.DepartmentName))
	b.WriteString(`</table>`)

	// ── Section: Caller Details ─────────────────────────
	if data.CallerPhone != "" && !isVD2 && !isVisionalSource {
		secHeader(&b, l.SectionCaller)
		b.WriteString(`<table class="grid">`)
		row2(&b, l.CallerName, html.EscapeString(data.CallerName), l.CallerMobile, html.EscapeString(data.CallerPhone))
		b.WriteString(`</table>`)
	}

	// ── Section: Location ─────────────────────────────────────────────────────
	hasLocation := data.Latitude != nil || data.Longitude != nil || data.Address != "" ||
		data.City != "" || data.State != "" || data.Country != "" || data.PostalCode != ""
	if hasLocation {
		secHeader(&b, l.SectionLocation)
		b.WriteString(`<table class="grid">`)
		if data.Latitude != nil || data.Longitude != nil {
			lat, lon := "", ""
			if data.Latitude != nil {
				lat = fmt.Sprintf("%.8f", *data.Latitude)
			}
			if data.Longitude != nil {
				lon = fmt.Sprintf("%.8f", *data.Longitude)
			}
			row2(&b, l.Latitude, html.EscapeString(lat), l.Longitude, html.EscapeString(lon))
		}
		if data.Address != "" {
			row1(&b, l.Address, html.EscapeString(data.Address))
		}
		if data.City != "" || data.State != "" {
			row2(&b, l.City, html.EscapeString(data.City), l.State, html.EscapeString(data.State))
		}
		if data.Country != "" || data.PostalCode != "" {
			row2(&b, l.Country, html.EscapeString(data.Country), l.PostalCode, html.EscapeString(data.PostalCode))
		}
		b.WriteString(`</table>`)
	}

	// ── Section: Revision History ─────────────────────────────────────────────
	if len(reportRevisions) > 0 {
		textAlign := "left"
		if l.Dir == "rtl" {
			textAlign = "right"
		}
		secHeader(&b, l.SectionRevisions)
		b.WriteString(`<table class="tbl"><thead><tr>`)
		fmt.Fprintf(&b, `<td style="width:18%%">%s</td><td style="width:20%%">%s</td><td style="width:18%%">%s</td><td style="width:22%%">%s</td><td style="width:22%%">%s</td>`,
			html.EscapeString(l.ColDate),
			html.EscapeString(l.ColName),
			html.EscapeString(l.ColAction),
			html.EscapeString(l.ColOldValue),
			html.EscapeString(l.ColNewValue),
		)
		b.WriteString(`</tr></thead><tbody>`)
		for _, rev := range reportRevisions {
			byName := strings.TrimSpace(rev.PerformedByFirstName + " " + rev.PerformedByLastName)
			var changes []models.IncidentRevisionChange
			if rev.Changes != "" {
				_ = json.Unmarshal([]byte(rev.Changes), &changes)
			}
			if len(changes) == 0 {
				fmt.Fprintf(&b, `<tr><td>%s</td><td>%s</td><td>%s</td><td class="cmt" colspan="2" style="text-align:%s">%s</td></tr>`,
					html.EscapeString(ts(rev.CreatedAt)),
					html.EscapeString(byName),
					html.EscapeString(rev.ActionType),
					textAlign,
					html.EscapeString(rev.ActionDescription),
				)
			} else {
				for i, ch := range changes {
					oldVal, newVal := "", ""
					if ch.OldValue != nil {
						oldVal = *ch.OldValue
					}
					if ch.NewValue != nil {
						newVal = *ch.NewValue
					}
					if i == 0 {
						fmt.Fprintf(&b, `<tr><td>%s</td><td>%s</td><td>%s</td><td class="cmt" style="text-align:%s">%s</td><td class="cmt" style="text-align:%s">%s</td></tr>`,
							html.EscapeString(ts(rev.CreatedAt)),
							html.EscapeString(byName),
							html.EscapeString(ch.FieldLabel),
							textAlign, html.EscapeString(oldVal),
							textAlign, html.EscapeString(newVal),
						)
					} else {
						fmt.Fprintf(&b, `<tr><td></td><td></td><td>%s</td><td class="cmt" style="text-align:%s">%s</td><td class="cmt" style="text-align:%s">%s</td></tr>`,
							html.EscapeString(ch.FieldLabel),
							textAlign, html.EscapeString(oldVal),
							textAlign, html.EscapeString(newVal),
						)
					}
				}
			}
		}
		b.WriteString(`</tbody></table>`)
	}

	// ── Section: Transition History ───────────────────────────────────────────
	if len(reportTransitions) > 0 {
		secHeader(&b, l.SectionHistory)
		b.WriteString(`<table class="tbl"><thead><tr>`)
		txtAlign := "left"
		if l.Dir == "rtl" {
			txtAlign = "right"
		}
		fmt.Fprintf(&b, `<td style="width:15%%">%s</td><td style="width:17%%">%s</td><td style="width:16%%">%s</td><td style="width:14%%">%s</td><td style="width:19%%;text-align:%s">%s</td><td style="width:19%%;text-align:%s">%s</td>`,
			html.EscapeString(l.ColDate), html.EscapeString(l.ColName),
			html.EscapeString(l.ColTransition), html.EscapeString(l.Status),
			txtAlign, html.EscapeString(l.ColComment),
			txtAlign, html.EscapeString(l.ColFeedback),
		)
		b.WriteString(`</tr></thead><tbody>`)
		for _, tr := range reportTransitions {
			byName := strings.TrimSpace(tr.PerformedByFirstName + " " + tr.PerformedByLastName)
			transitionName := localName(tr.TransitionName, tr.TransitionNameAr)
			status := localName(tr.ToStateName, tr.ToStateNameAr)
			fmt.Fprintf(&b, `<tr><td>%s</td><td>%s</td><td>%s</td><td>%s</td><td class="cmt">%s</td><td class="cmt">%s</td></tr>`,
				html.EscapeString(ts(tr.TransitionedAt)),
				html.EscapeString(byName),
				html.EscapeString(transitionName),
				html.EscapeString(status),
				html.EscapeString(tr.Comment),
				html.EscapeString(tr.FeedbackComment),
			)
		}
		b.WriteString(`</tbody></table>`)
	}

	// ── Section: Attachments ──────────────────────────────────────────────────
	// renderAttCard renders one attachment's card body (name/image/meta); prefixHTML,
	// when non-empty, is written before the file name (used for the per-card
	// transition context line on the default, non-grouped layout).
	renderAttCard := func(att models.IncidentReportAttachment, prefixHTML string) {
		b.WriteString(`<div class="att-card">`)
		if prefixHTML != "" {
			b.WriteString(prefixHTML)
		}
		fmt.Fprintf(&b, `<div class="att-name">%s</div>`, html.EscapeString(att.FileName))

		if att.DeletedAt != nil {
			fmt.Fprintf(&b, `<div class="att-deleted">&#x1F5D1; %s</div>`, html.EscapeString(l.AttDeleted))
		} else if strings.HasPrefix(att.MimeType, "image/") && att.FilePath != "" {
			if fr, ferr := h.storage.GetFile(c.UserContext(), att.FilePath); ferr == nil {
				if imgData, rerr := io.ReadAll(fr); rerr == nil && len(imgData) > 0 {
					encoded := base64.StdEncoding.EncodeToString(imgData)
					fmt.Fprintf(&b, `<img class="att-img" src="data:%s;base64,%s" alt="%s">`,
						att.MimeType, encoded, html.EscapeString(att.FileName))
				}
				fr.Close()
			}
		}

		uploadedBy := strings.TrimSpace(att.UploadedByFirstName + " " + att.UploadedByLastName)
		sizeStr := fmt.Sprintf("%.1f KB", float64(att.FileSize)/1024)
		if att.FileSize < 1024 {
			sizeStr = fmt.Sprintf("%d bytes", att.FileSize)
		}
		fmt.Fprintf(&b,
			`<div class="att-meta"><span>%s: <b>%s</b></span><span>%s: <b>%s</b></span><span>%s: <b>%s</b></span><span>%s: <b>%s</b></span><span>%s: <b>%s</b></span>`,
			html.EscapeString(l.AttType), html.EscapeString(att.MimeType),
			html.EscapeString(l.AttSize), sizeStr,
			html.EscapeString(l.AttUploadedBy), html.EscapeString(uploadedBy),
			html.EscapeString(l.AttUploadedByRole), html.EscapeString(att.UploadedByRole),
			html.EscapeString(l.AttUploadedAt), html.EscapeString(ts(att.CreatedAt)),
		)
		if att.DeletedAt != nil {
			fmt.Fprintf(&b, `<span style="color:#c0392b">%s: <b>%s</b></span>`,
				html.EscapeString(l.AttDeletedAt), html.EscapeString(ts(*att.DeletedAt)))
		}
		b.WriteString(`</div>`)
		b.WriteString(`</div>`)
	}

	if len(reportAttachments) > 0 {
		secHeader(&b, l.SectionAttach)
		b.WriteString(`<div>`)

		clientCode := strings.TrimSpace(h.cfg.ClientCode)
		if strings.EqualFold(clientCode, constants.CLIENT_CODE.EPM940) {
			// EPM940: group attachments by workflow state. Creation-time uploads
			// (no transition) sit under "Incident Creation"; transition uploads
			// sit under "Incident <to-state name>". Groups keep first-seen order.
			prefixLabel := "Incident"
			creationLabel := "Incident Creation"
			if l.Dir == "rtl" {
				prefixLabel = "البلاغ"
				creationLabel = "إنشاء البلاغ"
			}
			type attGroup struct {
				label string
				atts  []models.IncidentReportAttachment
			}
			seen := make(map[string]int)
			var groups []attGroup
			for _, att := range reportAttachments {
				var label string
				if att.TransitionHistoryID == nil {
					label = creationLabel
				} else if trName := localName(ptrStr(att.TransitionName), ptrStr(att.TransitionNameAr)); trName != "" {
					label = prefixLabel + " " + trName
				} else {
					label = prefixLabel
				}
				if idx, ok := seen[label]; ok {
					groups[idx].atts = append(groups[idx].atts, att)
				} else {
					seen[label] = len(groups)
					groups = append(groups, attGroup{label: label, atts: []models.IncidentReportAttachment{att}})
				}
			}
			for _, g := range groups {
				fmt.Fprintf(&b, `<div class="att-group-header">%s</div>`, html.EscapeString(g.label))
				for _, att := range g.atts {
					renderAttCard(att, "")
				}
			}
		} else {
			for _, att := range reportAttachments {
				// Resolve transition context; no transition → show as incident creation upload
				var attTransitionLabel string
				if att.TransitionHistoryID == nil {
					attTransitionLabel = `<div class="att-transition"><span>Uploaded at: <b>Incident Creation</b></span></div>`
				} else {
					transName := localName(ptrStr(att.TransitionName), ptrStr(att.TransitionNameAr))
					fromName := localName(ptrStr(att.FromStateName), ptrStr(att.FromStateNameAr))
					toName := localName(ptrStr(att.ToStateName), ptrStr(att.ToStateNameAr))
					if transName == "" {
						transName = "NA"
					}
					if fromName == "" {
						fromName = "NA"
					}
					if toName == "" {
						toName = "NA"
					}
					attTransitionLabel = fmt.Sprintf(
						`<div class="att-transition"><span>Transition: <b>%s</b></span><span>From: <b>%s</b></span><span>To: <b>%s</b></span></div>`,
						html.EscapeString(transName), html.EscapeString(fromName), html.EscapeString(toName),
					)
				}
				renderAttCard(att, attTransitionLabel)
			}
		}
		b.WriteString(`</div>`)
	}

	// ── Footer ────────────────────────────────────────────────────────────────
	fmt.Fprintf(&b,
		`<div class="footer"><span>%s</span><span>%s: %s</span></div>`,
		html.EscapeString(data.IncidentNumber),
		html.EscapeString(l.PrintDate),
		html.EscapeString(ts(time.Now())),
	)

	b.WriteString(`</div></body></html>`)
	return b.Bytes()
}

func secHeader(b *bytes.Buffer, title string) {
	fmt.Fprintf(b, `<div class="section-header">%s</div>`, html.EscapeString(title))
}

func row1(b *bytes.Buffer, label, value string) {
	fmt.Fprintf(b, `<tr><td class="lbl">%s</td><td class="val" colspan="3">%s</td></tr>`, label, value)
}

func row2(b *bytes.Buffer, label1, value1, label2, value2 string) {
	fmt.Fprintf(b, `<tr><td class="lbl">%s</td><td class="val">%s</td><td class="lbl">%s</td><td class="val">%s</td></tr>`,
		label1, value1, label2, value2)
}

// ── template-based PDF helpers ─────────────────────────────────────────────

func renderIncidentHTMLToPDF(htmlData []byte, chromeBin string) ([]byte, error) {
	tmpHTML, err := os.CreateTemp("", "inc-report-*.html")
	if err != nil {
		return nil, err
	}
	defer os.Remove(tmpHTML.Name())
	if _, err = tmpHTML.Write(htmlData); err != nil {
		return nil, err
	}
	tmpHTML.Close()

	tmpPDF, err := os.CreateTemp("", "inc-report-*.pdf")
	if err != nil {
		return nil, err
	}
	pdfPath := tmpPDF.Name()
	tmpPDF.Close()
	os.Remove(pdfPath) // let Chromium write fresh
	defer os.Remove(pdfPath)

	var stderr bytes.Buffer
	cmd := exec.Command(
		chromeBin,
		"--headless", "--disable-gpu", "--no-sandbox", "--disable-dev-shm-usage",
		"--no-margins", "--print-to-pdf-no-header",
		fmt.Sprintf("--print-to-pdf=%s", pdfPath),
		fmt.Sprintf("file://%s", tmpHTML.Name()),
	)
	cmd.Stderr = &stderr
	if err = cmd.Run(); err != nil {
		return nil, fmt.Errorf("exec_error=%s stderr=%s", err.Error(), stderr.String())
	}

	data, err := os.ReadFile(pdfPath)
	if err != nil || len(data) == 0 {
		log.Print("reprt render err:", err)
		return nil, fmt.Errorf("failed to read PDF output")
	}
	return data, nil
}

func buildIncidentTmplData(
	c *fiber.Ctx,
	h *IncidentHandler,
	inc *models.IncidentDetailResponse,
	raw *models.Incident,
	l reportLabels,
) map[string]interface{} {
	tz := appTimezone(h.cfg.Report.AppRegion)
	ts := func(t time.Time) string { return t.In(tz).Format("02/01/2006 03:04 PM") }
	tsp := func(t *time.Time) string {
		if t == nil {
			return ""
		}
		return ts(*t)
	}

	lang := "en"
	if l.Dir == "rtl" {
		lang = "ar"
	}

	statusName := ""
	if inc.CurrentState != nil {
		statusName = inc.CurrentState.Name
	}
	classPath := ""
	if inc.Classification != nil {
		classPath = inc.Classification.Name
	}
	locationName := ""
	if inc.Location != nil {
		locationName = inc.Location.Name
	}
	workflowName := ""
	if inc.Workflow != nil {
		workflowName = inc.Workflow.Name
	}

	slaStatus := l.No
	slaClass := "badge-ok"
	if inc.SLABreached {
		slaStatus = l.Yes
		slaClass = "badge-breached"
	}

	reporterName := ""
	if inc.Reporter != nil {
		reporterName = strings.TrimSpace(inc.Reporter.FirstName + " " + inc.Reporter.LastName)
	}
	if inc.ReporterName != "" && reporterName == "" {
		reporterName = inc.ReporterName
	}
	assigneeName := ""
	if inc.Assignee != nil {
		assigneeName = strings.TrimSpace(inc.Assignee.FirstName + " " + inc.Assignee.LastName)
	}
	deptName := ""
	if inc.Department != nil {
		deptName = inc.Department.Name
	}

	hasLocation := inc.Latitude != nil || inc.Longitude != nil || inc.Address != "" ||
		inc.City != "" || inc.State != "" || inc.Country != "" || inc.PostalCode != ""
	lat, lon := "", ""
	if inc.Latitude != nil {
		lat = fmt.Sprintf("%.8f", *inc.Latitude)
	}
	if inc.Longitude != nil {
		lon = fmt.Sprintf("%.8f", *inc.Longitude)
	}

	var history []map[string]interface{}
	for _, th := range inc.TransitionHistory {
		byName := ""
		if th.PerformedBy != nil {
			byName = strings.TrimSpace(th.PerformedBy.FirstName + " " + th.PerformedBy.LastName)
		}
		action := ""
		if th.ToState != nil {
			action = th.ToState.Name
		}
		history = append(history, map[string]interface{}{
			"Date":    ts(th.TransitionedAt),
			"ByName":  byName,
			"Action":  action,
			"Comment": th.Comment,
		})
	}

	var comments []map[string]interface{}
	for _, cm := range inc.Comments {
		byName := ""
		if cm.Author != nil {
			byName = strings.TrimSpace(cm.Author.FirstName + " " + cm.Author.LastName)
		}
		comments = append(comments, map[string]interface{}{
			"ByName":     byName,
			"Date":       ts(cm.CreatedAt),
			"Content":    cm.Content,
			"IsInternal": cm.IsInternal,
		})
	}

	var attachments []map[string]interface{}
	for _, att := range raw.Attachments {
		uploadedBy := ""
		if att.UploadedBy != nil {
			uploadedBy = strings.TrimSpace(att.UploadedBy.FirstName + " " + att.UploadedBy.LastName)
		}
		sizeStr := fmt.Sprintf("%.1f KB", float64(att.FileSize)/1024)
		if att.FileSize < 1024 {
			sizeStr = fmt.Sprintf("%d bytes", att.FileSize)
		}
		isImage := strings.HasPrefix(att.MimeType, "image/")
		imageData := template.URL("") // empty by default, set to data URI if we can fetch and encode the image
		ctx := c.UserContext()
		hostname, _ := ctx.Value(constants.ContextKeys.HOSTNAME).(string)
		protocol, _ := ctx.Value(constants.ContextKeys.PROTOCOL).(string)
		token, _ := ctx.Value(constants.ContextKeys.Token).(string)
		filePath := utils.GenerateAttachmentURL(protocol, hostname, fmt.Sprintf("%s", att.ID), token)
		log.Println("Attachment URL ", filePath)
		if isImage && filePath != "" {
			// fr, ferr := h.storage.GetFile(c.UserContext(), filePath)
			// if ferr != nil {
			// 	log.Fatalf("Failed to get file for attachment %s: %v", att.FileName, ferr)
			// }
			attURL := utils.GenerateAttachmentURL(protocol, hostname, fmt.Sprintf("%s", att.ID), token)
			log.Printf("Fetching attachment from URL: %s", attURL)
			resp, err := http.Get(attURL) // fetch via HTTP, not raw storage SDK
			if err == nil {
				log.Printf("Successfully fetched attachment %s, status: %s", att.FileName, resp.Status)
				defer resp.Body.Close()
				data, err := io.ReadAll(resp.Body)
				if err == nil {
					log.Printf("Read %d bytes for attachment %s", len(data), att.FileName)
					encoded := base64.StdEncoding.EncodeToString(data)
					imageData = template.URL("data:" + att.MimeType + ";base64," + encoded)
				}
			}
		}
		attachments = append(attachments, map[string]interface{}{
			"Name":       att.FileName,
			"MimeType":   att.MimeType,
			"SizeStr":    sizeStr,
			"UploadedBy": uploadedBy,
			"UploadedAt": ts(att.CreatedAt),
			"IsImage":    isImage,
			"ImageData":  imageData,
		})
	}

	hasDates := tsp(inc.DueDate) != "" || tsp(inc.ResolvedAt) != "" || tsp(inc.ClosedAt) != ""

	return map[string]interface{}{
		"Dir":                               l.Dir,
		"Lang":                              lang,
		"IncidentNo":                        inc.IncidentNumber,
		"CreatedAt":                         ts(inc.CreatedAt),
		"Status":                            statusName,
		"Channel":                           inc.Channel,
		"Source":                            inc.Source,
		"RecordType":                        inc.RecordType,
		"WorkflowName":                      workflowName,
		"Priority":                          "",
		"Title":                             inc.Title,
		"Classification":                    classPath,
		"Location":                          locationName,
		"Description":                       inc.Description,
		"SLABreached":                       inc.SLABreached,
		"SLABadgeClass":                     slaClass,
		"SLABadgeText":                      slaStatus,
		"SLADeadline":                       tsp(inc.SLADeadline),
		"DueDate":                           tsp(inc.DueDate),
		"ResolvedAt":                        tsp(inc.ResolvedAt),
		"ClosedAt":                          tsp(inc.ClosedAt),
		"HasDates":                          hasDates,
		"UpdatedAt":                         ts(inc.UpdatedAt),
		"Reporter":                          reporterName,
		"Assignee":                          assigneeName,
		"ReporterEmail":                     inc.ReporterEmail,
		"ReporterMobile":                    inc.CreatedByMobile,
		"Department":                        deptName,
		"ReporterName":                      inc.CreatedByName,
		"HasLocation":                       hasLocation,
		"Latitude":                          lat,
		"Longitude":                         lon,
		"Address":                           inc.Address,
		"City":                              inc.City,
		"State":                             inc.State,
		"Country":                           inc.Country,
		"PostalCode":                        inc.PostalCode,
		"History":                           history,
		i18n.T(c.UserContext(), "comments"): comments,
		"Attachments":                       attachments,
		"PrintDate":                         ts(time.Now()),
	}
}
