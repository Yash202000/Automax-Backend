package models

import "html/template"

type IncidentReportTmplData struct {
	Dir            string
	Lang           string
	IncidentNo     string
	CreatedAt      string
	Status         string
	Channel        string
	Source         string
	RecordType     string
	WorkflowName   string
	Priority       string
	Title          string
	Classification string
	Location       string
	Description    string
	SLABreached    bool
	SLABadgeClass  string // "badge-breached" or "badge-ok"
	SLABadgeText   string // localised Yes/No
	SLADeadline    string
	DueDate        string
	ResolvedAt     string
	ClosedAt       string
	HasDates       bool // true when any of DueDate/ResolvedAt/ClosedAt is non-empty
	UpdatedAt      string
	Reporter       string
	Assignee       string
	ReporterEmail  string
	ReporterMobile string
	Department     string
	ReporterName   string
	HasLocation    bool
	Latitude       string
	Longitude      string
	Address        string
	City           string
	State          string
	Country        string
	PostalCode     string
	History        []IncidentHistoryTmplRow
	Comments       []IncidentCommentTmplRow
	Attachments    []IncidentAttachmentTmplRow
	PrintDate      string
}

type IncidentHistoryTmplRow struct {
	Date, ByName, Action, Comment string
}

type IncidentCommentTmplRow struct {
	ByName, Date, Content string
	IsInternal            bool
}

type IncidentAttachmentTmplRow struct {
	Name, MimeType, SizeStr, UploadedBy, UploadedAt string
	IsImage                                         bool
	ImageData                                       template.URL // "data:image/jpeg;base64,..." — empty for non-images
}
