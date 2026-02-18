package handlers

import (
	"bytes"
	"encoding/base64"
	"fmt"
	"html"
	"io"
	"os"
	"os/exec"
	"strings"
	"time"

	"github.com/automax/backend/internal/models"
	"github.com/automax/backend/pkg/utils"
	"github.com/gofiber/fiber/v2"
	"github.com/google/uuid"
)

// ── translations ─────────────────────────────────────────────────────────────

type reportLabels struct {
	Dir             string
	Title           string
	SectionIncident string
	SectionReporter string
	SectionLocation string
	SectionHistory  string
	SectionComments string
	SectionAttach   string
	IncidentNo      string
	Date            string
	Status          string
	Channel         string
	Classification  string
	LocationLbl     string
	Description     string
	Title2          string
	Source          string
	Priority        string
	SLA             string
	SLABreached     string
	SLADeadline     string
	DueDate         string
	ResolvedAt      string
	ClosedAt        string
	Reporter        string
	ReporterEmail   string
	ReporterMobile  string
	ReporterName    string
	Assignee        string
	Department      string
	Workflow        string
	Latitude        string
	Longitude       string
	Address         string
	City            string
	State           string
	Country         string
	PostalCode      string
	RecordTypeLbl   string
	Comment         string
	CommentBy       string
	Internal        string
	ColDate         string
	ColName         string
	ColAction       string
	ColComment      string
	AttName         string
	AttType         string
	AttSize         string
	AttUploadedBy   string
	AttUploadedAt   string
	PrintDate       string
	Yes             string
	No              string
	PriorityLabels  [6]string // index 1-5
}

var labelsAR = reportLabels{
	Dir:             "rtl",
	Title:           "تفاصيل البلاغ",
	SectionIncident: "البلاغ",
	SectionReporter: "مقدم البلاغ",
	SectionLocation: "الموقع",
	SectionHistory:  "سجل العمليات",
	SectionComments: "التعليقات",
	SectionAttach:   "المرفقات",
	IncidentNo:      "رقم البلاغ",
	Date:            "تاريخ البلاغ",
	Status:          "الحالة",
	Channel:         "القناة",
	Classification:  "التصنيف",
	LocationLbl:     "الموقع",
	Description:     "الوصف",
	Title2:          "عنوان البلاغ",
	Source:          "المصدر",
	Priority:        "الأولوية",
	SLA:             "SLA",
	SLABreached:     "تجاوز SLA",
	SLADeadline:     "موعد SLA",
	DueDate:         "تاريخ الاستحقاق",
	ResolvedAt:      "تاريخ الحل",
	ClosedAt:        "تاريخ الإغلاق",
	Reporter:        "المُبلِّغ",
	ReporterEmail:   "البريد الإلكتروني",
	ReporterMobile:  "رقم الجوال",
	ReporterName:    "اسم المُبلِّغ",
	Assignee:        "المسؤول",
	Department:      "القسم",
	Workflow:        "سير العمل",
	Latitude:        "خط العرض",
	Longitude:       "خط الطول",
	Address:         "العنوان",
	City:            "المدينة",
	State:           "المنطقة",
	Country:         "الدولة",
	PostalCode:      "الرمز البريدي",
	RecordTypeLbl:   "نوع السجل",
	Comment:         "التعليق",
	CommentBy:       "بواسطة",
	Internal:        "[تعليق داخلي]",
	ColDate:         "التاريخ",
	ColName:         "الاسم",
	ColAction:       "الإجراء",
	ColComment:      "التعليقات",
	AttName:         "اسم الملف",
	AttType:         "النوع",
	AttSize:         "الحجم",
	AttUploadedBy:   "رُفع بواسطة",
	AttUploadedAt:   "تاريخ الرفع",
	PrintDate:       "تاريخ الطباعة",
	Yes:             "نعم",
	No:              "لا",
	PriorityLabels:  [6]string{"", "حرجة", "عالية", "متوسطة", "منخفضة", "منخفضة جداً"},
}

var labelsEN = reportLabels{
	Dir:             "ltr",
	Title:           "Incident Report",
	SectionIncident: "Incident Details",
	SectionReporter: "Submitter",
	SectionLocation: "Location",
	SectionHistory:  "Transition History",
	SectionComments: "Comments",
	SectionAttach:   "Attachments",
	IncidentNo:      "Incident No.",
	Date:            "Created Date",
	Status:          "Status",
	Channel:         "Channel",
	Classification:  "Classification",
	LocationLbl:     "Location",
	Description:     "Description",
	Title2:          "Title",
	Source:          "Source",
	Priority:        "Priority",
	SLA:             "SLA",
	SLABreached:     "SLA Breached",
	SLADeadline:     "SLA Deadline",
	DueDate:         "Due Date",
	ResolvedAt:      "Resolved At",
	ClosedAt:        "Closed At",
	Reporter:        "Reporter",
	ReporterEmail:   "Reporter Email",
	ReporterMobile:  "Reporter Mobile",
	ReporterName:    "Reporter Name",
	Assignee:        "Assigned To",
	Department:      "Department",
	Workflow:        "Workflow",
	Latitude:        "Latitude",
	Longitude:       "Longitude",
	Address:         "Address",
	City:            "City",
	State:           "State",
	Country:         "Country",
	PostalCode:      "Postal Code",
	RecordTypeLbl:   "Record Type",
	Comment:         "Comment",
	CommentBy:       "By",
	Internal:        "[Internal Comment]",
	ColDate:         "Date",
	ColName:         "Name",
	ColAction:       "Action",
	ColComment:      "Comment",
	AttName:         "File Name",
	AttType:         "Type",
	AttSize:         "Size",
	AttUploadedBy:   "Uploaded By",
	AttUploadedAt:   "Uploaded At",
	PrintDate:       "Print Date",
	Yes:             "Yes",
	No:              "No",
	PriorityLabels:  [6]string{"", "Critical", "High", "Medium", "Low", "Very Low"},
}

// ── handler ───────────────────────────────────────────────────────────────────

func (h *IncidentHandler) GenerateReport(c *fiber.Ctx) error {
	idStr := c.Params("id")
	id, err := uuid.Parse(idStr)
	if err != nil {
		return utils.ErrorResponse(c, fiber.StatusBadRequest, "Invalid ID")
	}

	incident, err := h.service.GetIncident(c.Context(), id)
	if err != nil {
		return utils.ErrorResponse(c, fiber.StatusNotFound, "Incident not found")
	}

	rawIncident, err := h.incidentRepo.FindByIDWithRelations(c.Context(), id)
	if err != nil {
		return utils.ErrorResponse(c, fiber.StatusNotFound, "Incident not found")
	}

	// language selection: ?lang=ar (default) or ?lang=en
	lbl := labelsAR
	if c.Query("lang", "ar") == "en" {
		lbl = labelsEN
	}

	htmlBytes := buildReportHTML(c, h, incident, rawIncident, lbl)

	format := c.Query("format", "pdf")
	switch format {
	case "html":
		c.Set("Content-Type", "text/html; charset=utf-8")
		return c.Send(htmlBytes)

	case "pdf":
		tmpHTML, terr := os.CreateTemp("", "report-*.html")
		if terr != nil {
			return utils.ErrorResponse(c, fiber.StatusInternalServerError, "Failed to create temp file")
		}
		defer os.Remove(tmpHTML.Name())
		if _, terr = tmpHTML.Write(htmlBytes); terr != nil {
			return utils.ErrorResponse(c, fiber.StatusInternalServerError, "Failed to write HTML")
		}
		tmpHTML.Close()

		tmpPDF, terr := os.CreateTemp("", "report-*.pdf")
		if terr != nil {
			return utils.ErrorResponse(c, fiber.StatusInternalServerError, "Failed to create PDF temp")
		}
		pdfPath := tmpPDF.Name()
		tmpPDF.Close()
		defer os.Remove(pdfPath)

		var stderr bytes.Buffer
		cmd := exec.CommandContext(c.Context(),
			"google-chrome",
			"--headless", "--disable-gpu", "--no-sandbox", "--disable-dev-shm-usage",
			"--no-margins", "--print-to-pdf-no-header",
			fmt.Sprintf("--print-to-pdf=%s", pdfPath),
			fmt.Sprintf("file://%s", tmpHTML.Name()),
		)
		cmd.Stderr = &stderr
		if terr = cmd.Run(); terr != nil {
			return utils.ErrorResponse(c, fiber.StatusInternalServerError,
				fmt.Sprintf("PDF generation failed: %s", stderr.String()))
		}

		pdfData, terr := os.ReadFile(pdfPath)
		if terr != nil || len(pdfData) == 0 {
			return utils.ErrorResponse(c, fiber.StatusInternalServerError, "Failed to read PDF output")
		}

		c.Set("Content-Type", "application/pdf")
		c.Set("Content-Disposition", fmt.Sprintf(
			`attachment; filename="incident_%s_%s.pdf"`,
			incident.IncidentNumber, time.Now().Format("20060102"),
		))
		return c.Send(pdfData)

	case "json":
		c.Set("Content-Type", "application/json")
		c.Set("Content-Disposition", fmt.Sprintf(`attachment; filename=incident_%s_%s.json`,
			incident.IncidentNumber, time.Now().Format("20060102")))
		return c.JSON(map[string]interface{}{"generated_at": time.Now().Format(time.RFC3339), "incident": incident})

	default:
		return utils.ErrorResponse(c, fiber.StatusBadRequest, "Use format=pdf, html, or json")
	}
}

// ── HTML builder ──────────────────────────────────────────────────────────────

func buildReportHTML(
	c *fiber.Ctx,
	h *IncidentHandler,
	inc *models.IncidentDetailResponse,
	raw *models.Incident,
	l reportLabels,
) []byte {
	var b bytes.Buffer

	// helpers
	ts := func(t time.Time) string { return t.Format("02/01/2006 03:04 PM") }
	tsp := func(t *time.Time) string {
		if t == nil {
			return ""
		}
		return ts(*t)
	}

	b.WriteString(fmt.Sprintf(`<!DOCTYPE html>
<html dir="%s" lang="%s">
<head><meta charset="UTF-8"><title>%s</title>
<style>
*{box-sizing:border-box;margin:0;padding:0}
body{font-family:'Segoe UI',Tahoma,Arial,sans-serif;font-size:10.5pt;color:#222;background:#fff;direction:%s}
.page{max-width:780px;margin:0 auto;padding:14px}
.main-header{background:#375a6e;color:#fff;text-align:center;padding:11px;font-size:16pt;font-weight:bold}
.section-header{background:#6491a5;color:#fff;text-align:center;padding:5px 8px;font-size:11pt;font-weight:bold;margin-top:10px}
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
.att-img{display:block;max-width:100%%;max-height:280px;object-fit:contain;margin:0 auto;padding:6px}
.att-meta{display:flex;flex-wrap:wrap;gap:12px;padding:5px 8px;font-size:8.5pt;background:#f0f8fc;border-top:1px solid #c8dce4}
.att-card:nth-child(even){background:#f7fbfd}
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
				return "left"
			}
			return "right"
		}(),
		func() string {
			if l.Dir == "rtl" {
				return "left"
			}
			return "right"
		}(),
		func() string {
			if l.Dir == "rtl" {
				return "right"
			}
			return "left"
		}(),
	))

	b.WriteString(fmt.Sprintf(`<div class="main-header">%s</div>`, html.EscapeString(l.Title)))

	// ── Section: Incident Details ─────────────────────────────────────────────
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

	priorityLabel := ""

	slaStatus := l.No
	slaClass := "badge-ok"
	if inc.SLABreached {
		slaStatus = l.Yes
		slaClass = "badge-breached"
	}

	secHeader(&b, l.SectionIncident)
	b.WriteString(`<table class="grid">`)
	row2(&b, l.IncidentNo, html.EscapeString(inc.IncidentNumber), l.Date, html.EscapeString(ts(inc.CreatedAt)))
	row2(&b, l.Status, html.EscapeString(statusName), l.Channel, html.EscapeString(inc.Channel))
	row2(&b, l.Source, html.EscapeString(inc.Source), l.RecordTypeLbl, html.EscapeString(inc.RecordType))
	if workflowName != "" {
		row2(&b, l.Workflow, html.EscapeString(workflowName), l.Priority, html.EscapeString(priorityLabel))
	} else {
		row1(&b, l.Priority, html.EscapeString(priorityLabel))
	}
	row1(&b, l.Title2, html.EscapeString(inc.Title))
	row1(&b, l.Classification, html.EscapeString(classPath))
	row1(&b, l.LocationLbl, html.EscapeString(locationName))
	if inc.Description != "" {
		row1(&b, l.Description, html.EscapeString(inc.Description))
	}
	// SLA row
	row2(&b, l.SLABreached, fmt.Sprintf(`<span class="%s">%s</span>`, slaClass, html.EscapeString(slaStatus)),
		l.SLADeadline, html.EscapeString(tsp(inc.SLADeadline)))
	if inc.DueDate != nil || inc.ResolvedAt != nil || inc.ClosedAt != nil {
		row2(&b, l.DueDate, html.EscapeString(tsp(inc.DueDate)), l.ResolvedAt, html.EscapeString(tsp(inc.ResolvedAt)))
		if inc.ClosedAt != nil {
			row1(&b, l.ClosedAt, html.EscapeString(tsp(inc.ClosedAt)))
		}
	}
	row2(&b, l.Date+" ("+l.Status+")", html.EscapeString(ts(inc.UpdatedAt)), "", "")
	b.WriteString(`</table>`)

	// ── Section: Submitter ────────────────────────────────────────────────────
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

	secHeader(&b, l.SectionReporter)
	b.WriteString(`<table class="grid">`)
	row2(&b, l.Reporter, html.EscapeString(reporterName), l.Assignee, html.EscapeString(assigneeName))
	row2(&b, l.ReporterEmail, html.EscapeString(inc.ReporterEmail), l.ReporterMobile, html.EscapeString(inc.CreatedByMobile))
	row2(&b, l.Department, html.EscapeString(deptName), l.ReporterName, html.EscapeString(inc.CreatedByName))
	b.WriteString(`</table>`)

	// ── Section: Location ─────────────────────────────────────────────────────
	hasLocation := inc.Latitude != nil || inc.Longitude != nil || inc.Address != "" ||
		inc.City != "" || inc.State != "" || inc.Country != "" || inc.PostalCode != ""
	if hasLocation {
		secHeader(&b, l.SectionLocation)
		b.WriteString(`<table class="grid">`)
		if inc.Latitude != nil || inc.Longitude != nil {
			lat, lon := "", ""
			if inc.Latitude != nil {
				lat = fmt.Sprintf("%.8f", *inc.Latitude)
			}
			if inc.Longitude != nil {
				lon = fmt.Sprintf("%.8f", *inc.Longitude)
			}
			row2(&b, l.Latitude, html.EscapeString(lat), l.Longitude, html.EscapeString(lon))
		}
		if inc.Address != "" {
			row1(&b, l.Address, html.EscapeString(inc.Address))
		}
		if inc.City != "" || inc.State != "" {
			row2(&b, l.City, html.EscapeString(inc.City), l.State, html.EscapeString(inc.State))
		}
		if inc.Country != "" || inc.PostalCode != "" {
			row2(&b, l.Country, html.EscapeString(inc.Country), l.PostalCode, html.EscapeString(inc.PostalCode))
		}
		b.WriteString(`</table>`)
	}

	// ── Section: Transition History ───────────────────────────────────────────
	if len(inc.TransitionHistory) > 0 {
		secHeader(&b, l.SectionHistory)
		b.WriteString(`<table class="tbl"><thead><tr>`)
		fmt.Fprintf(&b, `<td style="width:20%%">%s</td><td style="width:20%%">%s</td><td style="width:18%%">%s</td><td style="width:42%%;text-align:%s">%s</td>`,
			html.EscapeString(l.ColDate), html.EscapeString(l.ColName),
			html.EscapeString(l.ColAction),
			func() string {
				if l.Dir == "rtl" {
					return "right"
				}
				return "left"
			}(),
			html.EscapeString(l.ColComment),
		)
		b.WriteString(`</tr></thead><tbody>`)
		for _, th := range inc.TransitionHistory {
			byName := ""
			if th.PerformedBy != nil {
				byName = strings.TrimSpace(th.PerformedBy.FirstName + " " + th.PerformedBy.LastName)
			}
			action := ""
			if th.ToState != nil {
				action = th.ToState.Name
			}
			fmt.Fprintf(&b, `<tr><td>%s</td><td>%s</td><td>%s</td><td class="cmt">%s</td></tr>`,
				html.EscapeString(ts(th.TransitionedAt)),
				html.EscapeString(byName),
				html.EscapeString(action),
				html.EscapeString(th.Comment),
			)
		}
		b.WriteString(`</tbody></table>`)
	}

	// ── Section: Comments ─────────────────────────────────────────────────────
	if len(inc.Comments) > 0 {
		secHeader(&b, l.SectionComments)
		b.WriteString(`<div>`)
		for _, cm := range inc.Comments {
			b.WriteString(`<div class="comment-block">`)
			byName := ""
			if cm.Author != nil {
				byName = strings.TrimSpace(cm.Author.FirstName + " " + cm.Author.LastName)
			}
			fmt.Fprintf(&b, `<div class="comment-meta">%s: <b>%s</b> &nbsp;|&nbsp; %s</div>`,
				html.EscapeString(l.CommentBy),
				html.EscapeString(byName),
				html.EscapeString(ts(cm.CreatedAt)),
			)
			fmt.Fprintf(&b, `<div>%s</div>`, html.EscapeString(cm.Content))
			if cm.IsInternal {
				fmt.Fprintf(&b, `<div class="comment-internal">%s</div>`, html.EscapeString(l.Internal))
			}
			b.WriteString(`</div>`)
		}
		b.WriteString(`</div>`)
	}

	// ── Section: Attachments ──────────────────────────────────────────────────
	if len(raw.Attachments) > 0 {
		secHeader(&b, l.SectionAttach)
		b.WriteString(`<div>`)
		for _, att := range raw.Attachments {
			b.WriteString(`<div class="att-card">`)
			fmt.Fprintf(&b, `<div class="att-name">%s</div>`, html.EscapeString(att.FileName))

			// embed image as base64 data URI
			if strings.HasPrefix(att.MimeType, "image/") && att.FilePath != "" {
				if fr, ferr := h.storage.GetFile(c.Context(), att.FilePath); ferr == nil {
					if imgData, rerr := io.ReadAll(fr); rerr == nil && len(imgData) > 0 {
						encoded := base64.StdEncoding.EncodeToString(imgData)
						fmt.Fprintf(&b, `<img class="att-img" src="data:%s;base64,%s" alt="%s">`,
							att.MimeType, encoded, html.EscapeString(att.FileName))
					}
					fr.Close()
				}
			}

			uploadedBy := ""
			if att.UploadedBy != nil {
				uploadedBy = strings.TrimSpace(att.UploadedBy.FirstName + " " + att.UploadedBy.LastName)
			}
			sizeStr := fmt.Sprintf("%.1f KB", float64(att.FileSize)/1024)
			if att.FileSize < 1024 {
				sizeStr = fmt.Sprintf("%d bytes", att.FileSize)
			}
			fmt.Fprintf(&b,
				`<div class="att-meta"><span>%s: <b>%s</b></span><span>%s: <b>%s</b></span><span>%s: <b>%s</b></span><span>%s: <b>%s</b></span></div>`,
				html.EscapeString(l.AttType), html.EscapeString(att.MimeType),
				html.EscapeString(l.AttSize), sizeStr,
				html.EscapeString(l.AttUploadedBy), html.EscapeString(uploadedBy),
				html.EscapeString(l.AttUploadedAt), html.EscapeString(ts(att.CreatedAt)),
			)
			b.WriteString(`</div>`)
		}
		b.WriteString(`</div>`)
	}

	// ── Footer ────────────────────────────────────────────────────────────────
	fmt.Fprintf(&b,
		`<div class="footer"><span>%s</span><span>%s: %s</span></div>`,
		html.EscapeString(inc.IncidentNumber),
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
