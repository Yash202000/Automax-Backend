package services

// KPI Dictionary bulk import — reads the "KPI Dictionary" Excel workbook
// (master data sheets, the three Dictionary sheets and the Performance
// sheet), validates every row against the template and the
// existing system data, and builds an import plan. The plan doubles as the
// preview shown to the user; CommitKpiDictionaryImport re-validates the same
// file and applies the plan in one transaction.
//
// Where each workbook category lands:
//   - Pillars / Enablers / Strategic Goals / Operational Objectives /
//     Initiatives sheets    → pillars, enablers, goals, operational_objectives,
//                             initiatives (+ owning departments). Older
//                             workbooks have one numbered Master Data sheet
//                             instead; it is still read.
//   - Dictionary sheets     → strategic_kpis / operational_kpis / award_kpis
//                             (+ processes, domains, award criteria/
//                             sub-criteria and departments they reference
//                             that don't exist yet)
//   - "Targets YYYY" cols   → kpi_annual_targets (period_code "annual")
//   - Performance sheet     → approved kpi_entries, one per row (KPI, year,
//                             quarter) with an Actual Result (older workbooks:
//                             three wide per-type sheets), against the KPI's
//                             metric (a default metric is created when the
//                             KPI has none, since entries require one)

import (
	"bytes"
	"context"
	"fmt"
	"math"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"time"
	"unicode"

	"github.com/automax/backend/internal/models"
	"github.com/google/uuid"
	"github.com/xuri/excelize/v2"
	"gorm.io/gorm"
)

const (
	KpiImportExistingSkip   = "skip"
	KpiImportExistingUpdate = "update"

	KpiImportErrorsSkipInvalid = "skip_invalid"
	KpiImportErrorsAbort       = "abort"

	kpiImportSeverityError   = "error"
	kpiImportSeverityWarning = "warning"

	kpiImportActionCreate = "create"
	kpiImportActionUpdate = "update"
	kpiImportActionSkip   = "skip"
	kpiImportActionReject = "reject"

	kpiImportMaxFileSize = 20 << 20

	// Template Related Constants
	KpiDictionaryTemplateFileName = "kpi-dictionary-import-template.xlsx"

	KpiTemplateLangEN = "en"
	KpiTemplateLangAR = "ar"

	kpiTemplateInstructionsSheet  = "Instructions"
	kpiTemplatePillarsSheet       = "Pillars"
	kpiTemplateEnablersSheet      = "Enablers"
	kpiTemplateGoalsSheet         = "Goals"
	kpiTemplateObjectivesSheet    = "Parent Objectives"      // operational_objectives
	kpiTemplateProcessesSheet     = "Operational Objectives" // processes (the screens' name)
	kpiTemplateInitiativesSheet   = "Initiatives"
	kpiTemplateStrategicDictSheet = "Strategic Dictionary"
	kpiTemplateOperationalDict    = "Operational Dictionary"
	kpiTemplateAwardDictSheet     = "Award Dictionary"
	kpiTemplatePerformanceSheet   = "Performance"
	kpiTemplateListsSheet         = "Lists"

	kpiTemplateMasterRows = 200
	kpiTemplateYearSpan   = 5
)

type kpiTemplateColumn struct {
	key      string
	ar       string
	en       string
	width    float64
	required bool     // mandatory, marked with a red (*) in the template
	values   []string // optional English data-validation drop-down values
	valuesAr []string // optional Arabic data-validation drop-down values
	// list is a Lists-sheet dropdown (a utils.ListConfig name) used instead
	// of values; such dropdowns warn about, but accept, other values.
	list   string
	color  string // header colour, groups related columns
	hintEn string // input message for a column without a dropdown
	hintAr string
	// formula fills the column automatically from another cell of the row
	// ("{row}" = row number); the user can type over it.
	formula string
	// allowIf refuses a typed value on rows where it is false (see
	// utils.ColumnConfig.AllowIf); allowErrEn/Ar is the message shown.
	allowIf                string
	allowErrEn, allowErrAr string
	// listFormula is a dropdown source formula (see utils.DropdownConfig
	// Source) used instead of list, e.g. a list filtered by another cell.
	listFormula string
	hidden      bool // helper column, hidden in the template
	text        bool // data cells formatted as Text (Excel keeps "2026-01" as typed)
	// blankDefault is the value (one of values) a blank cell is imported
	// as — the form's default; the dropdown's prompt says so.
	blankDefault string
}

// Record categories reported in the preview/summary, in display order.
var KpiImportCategories = []string{
	"department", "organization", "pillar", "enabler", "strategic_goal", "operational_objective",
	"process", "domain", "award_criterion", "award_sub_criterion", "award_kpi_measure", "data_source",
	"segmentation_dimension", "initiative", "kpi", "metric", "target", "annual_target", "performance",
}

type KpiImportOptions struct {
	// ExistingMode: "skip" leaves records that already exist untouched;
	// "update" overwrites KPI definitions, annual targets and performance
	// entries that already exist. Existing master data is only ever
	// referenced, never modified.
	ExistingMode string `json:"existing_mode"`
	// ErrorMode: "skip_invalid" commits every valid record and rejects the
	// invalid ones (and anything depending on them); "abort" refuses to
	// commit anything while any error remains.
	ErrorMode string `json:"error_mode"`
}

func (o *KpiImportOptions) normalize() {
	if o.ExistingMode != KpiImportExistingUpdate {
		o.ExistingMode = KpiImportExistingSkip
	}
	if o.ErrorMode != KpiImportErrorsAbort {
		o.ErrorMode = KpiImportErrorsSkipInvalid
	}
}

type KpiImportIssue struct {
	Severity string `json:"severity"`
	Sheet    string `json:"sheet"`
	Row      int    `json:"row"` // Excel row number; 0 for workbook/sheet-level issues
	RecordID string `json:"record_id"`
	Field    string `json:"field"`
	Message  string `json:"message"`
}

type KpiImportItem struct {
	Category string `json:"category"`
	KpiType  string `json:"kpi_type,omitempty"`
	RecordID string `json:"record_id"`
	Name     string `json:"name"`
	Sheet    string `json:"sheet"`
	Row      int    `json:"row"`
	Action   string `json:"action"`
	Reason   string `json:"reason,omitempty"`
}

type KpiImportCounts struct {
	Created    int `json:"created"`
	Updated    int `json:"updated"`
	Skipped    int `json:"skipped"`
	Rejected   int `json:"rejected"`
	RolledBack int `json:"rolled_back"`
}

type KpiImportResult struct {
	FileName    string                      `json:"file_name"`
	Options     KpiImportOptions            `json:"options"`
	Committed   bool                        `json:"committed"`
	CanCommit   bool                        `json:"can_commit"`
	CommitError string                      `json:"commit_error,omitempty"`
	Errors      []KpiImportIssue            `json:"errors"`
	Warnings    []KpiImportIssue            `json:"warnings"`
	Items       []KpiImportItem             `json:"items"`
	Categories  map[string]*KpiImportCounts `json:"categories"`
	Totals      KpiImportCounts             `json:"totals"`
}

// ─── Plan model ─────────────────────────────────────────────────────────────

const (
	mdDepartment   = "department"
	mdPillar       = "pillar"
	mdEnabler      = "enabler"
	mdGoal         = "strategic_goal"
	mdObjective    = "operational_objective"
	mdProcess      = "process"
	mdDomain       = "domain"
	mdCriterion    = "award_criterion"
	mdSubCriterion = "award_sub_criterion"
	mdInitiative   = "initiative"
	mdOrganization = "organization"
	mdAwardMeasure = "award_kpi_measure"
)

// mdNode is one master-data record: either an existing DB row (Exists) or
// one this import will create (ID pre-generated so other new records can
// reference it before anything is written).
type mdNode struct {
	Kind        string
	NameEn      string
	NameAr      string
	ID          uuid.UUID
	Exists      bool
	FromSheet   bool // declared on the Master Data sheet (listed in the preview even if it exists)
	Sheet       string
	Row         int
	Rejected    bool
	RejectWhy   string
	Pillar      *mdNode
	Enabler     *mdNode
	Goal        *mdNode
	Objective   *mdNode
	Owner       *mdNode // owning department
	Criterion   *mdNode
	CriterionNo int
	SubNo       string
	Code        string // award KPI measure code (KPI-AWRD-MSR-NN), existing records only
	DeptType    string
	DomainType  string
	// Inactive / Status: a new record's Active and (initiatives) Status
	// columns on the workbook's master data sheets.
	Inactive bool
	Status   string
	action   string // set by finalize
}

type kpiTargetRec struct {
	Year       int
	Value      float64
	Row        int
	Field      string
	ExistingID *uuid.UUID
	Rejected   bool
	RejectWhy  string
	action     string
}

type kpiRec struct {
	Type   string
	Code   string
	NameEn string
	NameAr string
	Sheet  string
	Row    int

	// Stub for a KPI that only exists in the DB (referenced from a
	// Performance sheet) — never written, only used for its IDs.
	DBOnly bool

	ID         uuid.UUID
	ExistingID *uuid.UUID
	MetricID   *uuid.UUID // existing metric to import targets/entries against
	newMetric  bool

	Pillar, Goal, Objective, Process, Domain, SubCriterion, OwnerDept, Agency, OwnerOrg, AwardMeasure *mdNode
	RelatedDepts                                                                                      []*mdNode

	OwnerType, Polarity, Frequency, Lifecycle, DataSource, Formula string
	DescriptionEn, DescriptionAr, SegAxes, Notes                   string
	Classification, TargetRequirement, UnitOfMeasure               string
	Baseline                                                       float64
	// present lists the optional columns whose cell holds a value on this
	// row. On update only these are written, so a blank cell (or a column
	// an older template does not have) keeps the stored value.
	present map[string]bool
	// ownerDeptName is the KPI's owner department as text, used to
	// cross-check the Owner Department column on Performance sheets
	// (including KPIs that only exist in the database).
	ownerDeptName string

	Targets []*kpiTargetRec

	Rejected  bool
	RejectWhy string
	action    string
}

type kpiPerfRec struct {
	Kpi *kpiRec
	// Metric is the KPI metric the result is for; nil means the KPI's first
	// metric, or the default metric the import creates.
	Metric      *models.KpiMetric
	MetricGiven bool // the row named the metric (shown in the record ID)
	Sheet       string
	Row         int
	Year        int
	PeriodCode  string // jan..dec, q1..q4, h1, h2 or annual
	Target      *float64
	Actual      float64
	ExistingID  *uuid.UUID
	Rejected    bool
	RejectWhy   string
	action      string
}

type existingKpi struct {
	ID            uuid.UUID
	Type          string
	Code          string
	NameEn        string
	NameAr        string
	Polarity      string
	Frequency     string
	Baseline      float64
	MetricID      *uuid.UUID
	Metrics       []*models.KpiMetric // display order; MetricID is the first
	OwnerDeptName string
}

// kpiImportMode limits a pass of the KPI Dictionary import. The combined
// workbook import runs the definitions pass, then the Metrics sheet, then
// the values pass, so targets and results can attach to metrics created
// from the same file.
type kpiImportMode int

const (
	kpiImportAll         kpiImportMode = iota // everything in one pass
	kpiImportDefinitions                      // master data and KPI definitions only
	kpiImportValues                           // annual targets and performance only (KPIs must already exist)
)

type kpiImporter struct {
	db     *gorm.DB
	ctx    context.Context
	userID uuid.UUID
	opts   KpiImportOptions
	now    time.Time
	res    *KpiImportResult

	structuralError bool
	mode            kpiImportMode

	md map[string][]*mdNode

	kpis          []*kpiRec
	kpiByTypeCode map[string]*kpiRec
	kpiByCode     map[string]*kpiRec
	perfs         []*kpiPerfRec

	existingKpis    map[string]*existingKpi // type|CODE
	existingCodeTyp map[string]string       // CODE → type
	existingTargets map[string]uuid.UUID    // type|CODE|year
	existingEntries map[string]uuid.UUID    // kpiID|type|metricID|year|period_code
	deptCodes       map[string]bool

	dataSourceEN map[string]string // data source EN/AR name (normalised) → EN name; loaded on first use
}

// dataSourceName returns the English name of the data source whose English
// or Arabic name matches, as the KPI form stores it; other text is kept as
// written (Data Source is free text).
func (imp *kpiImporter) dataSourceName(v string) string {
	if imp.dataSourceEN == nil {
		imp.dataSourceEN = map[string]string{}
		var sources []models.KpiDataSource
		if err := imp.db.WithContext(imp.ctx).Find(&sources).Error; err == nil {
			for _, s := range sources {
				if s.NameEn == "" {
					continue
				}
				imp.dataSourceEN[normName(s.NameEn)] = s.NameEn
				if s.NameAr != "" {
					imp.dataSourceEN[normName(s.NameAr)] = s.NameEn
				}
			}
		}
	}
	if en, ok := imp.dataSourceEN[normName(v)]; ok {
		return en
	}
	return v
}

// ─── Entry points ───────────────────────────────────────────────────────────

// ValidateKpiDictionaryImport and CommitKpiDictionaryImport (the combined
// KPI workbook: master data, KPIs, metrics, targets and performance) are in
// kpi_workbook_import.go.

func newKpiImporter(ctx context.Context, db *gorm.DB, fileName string, opts KpiImportOptions) *kpiImporter {
	return &kpiImporter{
		db:              db.WithContext(ctx),
		ctx:             ctx,
		opts:            opts,
		now:             time.Now(),
		md:              map[string][]*mdNode{},
		kpiByTypeCode:   map[string]*kpiRec{},
		kpiByCode:       map[string]*kpiRec{},
		existingKpis:    map[string]*existingKpi{},
		existingCodeTyp: map[string]string{},
		existingTargets: map[string]uuid.UUID{},
		existingEntries: map[string]uuid.UUID{},
		deptCodes:       map[string]bool{},
		res: &KpiImportResult{
			FileName: fileName,
			Options:  opts,
			Errors:   []KpiImportIssue{},
			Warnings: []KpiImportIssue{},
			Items:    []KpiImportItem{},
		},
	}
}

func buildKpiImportPlan(ctx context.Context, db *gorm.DB, data []byte, fileName string, userID uuid.UUID, opts KpiImportOptions) (*kpiImporter, error) {
	return buildKpiImportPlanMode(ctx, db, data, fileName, userID, opts, kpiImportAll)
}

func buildKpiImportPlanMode(ctx context.Context, db *gorm.DB, data []byte, fileName string, userID uuid.UUID, opts KpiImportOptions, mode kpiImportMode) (*kpiImporter, error) {
	opts.normalize()
	imp := newKpiImporter(ctx, db, fileName, opts)
	imp.userID = userID
	imp.mode = mode

	// File-level checks: supported format and a readable workbook.
	lower := strings.ToLower(fileName)
	if !(strings.HasSuffix(lower, ".xlsx") || strings.HasSuffix(lower, ".xlsm")) {
		imp.structural("", "Unsupported file format — upload an Excel workbook (.xlsx or .xlsm). Legacy .xls files must be re-saved as .xlsx.")
		return imp, nil
	}
	if len(data) == 0 {
		imp.structural("", "The uploaded file is empty.")
		return imp, nil
	}
	if len(data) > kpiImportMaxFileSize {
		imp.structural("", fmt.Sprintf("The file is too large (max %d MB).", kpiImportMaxFileSize>>20))
		return imp, nil
	}
	if !bytes.HasPrefix(data, []byte("PK")) {
		imp.structural("", "The file is not a valid Excel workbook (it may be corrupted or a renamed file of another type).")
		return imp, nil
	}
	f, err := excelize.OpenReader(bytes.NewReader(data))
	if err != nil {
		imp.structural("", "The workbook could not be opened: "+err.Error())
		return imp, nil
	}
	defer f.Close()

	if err := imp.loadExisting(); err != nil {
		return nil, fmt.Errorf("failed to load existing data: %w", err)
	}

	// Missing sheets don't stop the scan — every template problem in the
	// workbook is reported in one pass.
	sheets := imp.identifySheets(f)

	rows := func(name string) [][]string {
		if name == "" {
			return nil
		}
		r, err := f.GetRows(name, excelize.Options{RawCellValue: true})
		if err != nil {
			imp.structural(name, "The worksheet could not be read: "+err.Error())
		}
		return r
	}

	// Parse every sheet first so structural (template) problems surface
	// before any row-level validation runs.
	masterRows := rows(sheets["master"])
	masterTables := map[string]*sheetTable{}
	for _, m := range kpiMasterSheets {
		if name := sheets[m.key]; name != "" {
			masterTables[m.key] = imp.readTable(name, rows(name), masterSheetColumns(m.kind), tableRows)
		}
	}
	dictSheets := []struct{ key, kpiType string }{
		{"strategic_dict", models.KPITypeStrategic},
		{"operational_dict", models.KPITypeOperational},
		{"award_dict", models.KPITypeAward},
	}
	perfSheets := []struct{ key, kpiType string }{
		{"strategic_perf", models.KPITypeStrategic},
		{"operational_perf", models.KPITypeOperational},
		{"award_perf", models.KPITypeAward},
	}
	dictTables := map[string]*sheetTable{}
	for _, d := range dictSheets {
		dictTables[d.key] = imp.readTable(sheets[d.key], rows(sheets[d.key]), dictColumns(d.kpiType), tableDictionary)
	}
	perfTables := map[string]*sheetTable{}
	var perfRows *sheetTable
	if mode != kpiImportDefinitions {
		for _, p := range perfSheets {
			if name := sheets[p.key]; name != "" {
				perfTables[p.key] = imp.readTable(name, rows(name), perfColumns(p.kpiType), tableQuarterColumns)
			}
		}
		if name := sheets["perf"]; name != "" {
			perfRows = imp.readTable(name, rows(name), perfRowColumns(), tableRows)
		}
	}
	if imp.structuralError {
		return imp, nil
	}

	if sheets["master"] != "" {
		imp.parseMasterData(sheets["master"], masterRows)
	}
	for _, m := range kpiMasterSheets {
		imp.parseMasterSheet(m.kind, masterTables[m.key])
	}
	for _, d := range dictSheets {
		imp.parseDictionary(d.kpiType, dictTables[d.key])
	}
	switch mode {
	case kpiImportDefinitions:
		// Targets are written by the values pass.
		for _, k := range imp.kpis {
			k.Targets = nil
		}
	case kpiImportValues:
		// The definitions pass wrote every valid KPI; one still missing was
		// rejected there (its errors are already reported).
		for _, k := range imp.kpis {
			if k.ExistingID == nil && !k.Rejected {
				k.Rejected, k.RejectWhy = true, "KPI definition was not imported"
			}
		}
	}
	for _, p := range perfSheets {
		imp.parsePerformance(p.kpiType, perfTables[p.key])
	}
	imp.parsePerformanceRows(perfRows)
	imp.resolveMetricsAndExisting()
	return imp, nil
}

// ─── Issue helpers ──────────────────────────────────────────────────────────

func (imp *kpiImporter) structural(sheet, msg string) {
	imp.structuralError = true
	if sheet == "" {
		sheet = "(workbook)"
	}
	imp.res.Errors = append(imp.res.Errors, KpiImportIssue{Severity: kpiImportSeverityError, Sheet: sheet, Message: msg})
}

func (imp *kpiImporter) errorf(sheet string, row int, recordID, field, msg string) {
	imp.res.Errors = append(imp.res.Errors, KpiImportIssue{Severity: kpiImportSeverityError, Sheet: sheet, Row: row, RecordID: recordID, Field: field, Message: msg})
}

func (imp *kpiImporter) warn(sheet string, row int, recordID, field, msg string) {
	imp.res.Warnings = append(imp.res.Warnings, KpiImportIssue{Severity: kpiImportSeverityWarning, Sheet: sheet, Row: row, RecordID: recordID, Field: field, Message: msg})
}

// ─── Existing data ──────────────────────────────────────────────────────────

func (imp *kpiImporter) addExistingNode(n *mdNode) {
	n.Exists = true
	imp.md[n.Kind] = append(imp.md[n.Kind], n)
}

func (imp *kpiImporter) loadExisting() error {
	db := imp.db
	var pillars []models.Pillar
	var enablers []models.Enabler
	var goals []models.Goal
	var objectives []models.OperationalObjective
	var processes []models.Process
	var depts []models.Department
	var domains []models.Domain
	var criteria []models.AwardCriterion
	var subs []models.AwardSubCriterion
	var initiatives []models.Initiative
	var orgs []models.KpiOrganization
	var measures []models.AwardKpiMeasure
	for _, q := range []struct {
		dest interface{}
	}{{&pillars}, {&enablers}, {&goals}, {&objectives}, {&processes}, {&depts}, {&domains}, {&criteria}, {&subs}, {&initiatives}, {&orgs}, {&measures}} {
		if err := db.Find(q.dest).Error; err != nil {
			return err
		}
	}

	deptByID := map[uuid.UUID]*mdNode{}
	for _, d := range depts {
		n := &mdNode{Kind: mdDepartment, NameEn: d.Name, NameAr: d.NameAr, ID: d.ID, DeptType: d.Type}
		deptByID[d.ID] = n
		imp.addExistingNode(n)
	}
	// Department codes are unique across soft-deleted rows too.
	var codes []string
	if err := db.Unscoped().Model(&models.Department{}).Pluck("code", &codes).Error; err != nil {
		return err
	}
	for _, c := range codes {
		imp.deptCodes[strings.ToUpper(c)] = true
	}

	pillarByID, enablerByID := map[uuid.UUID]*mdNode{}, map[uuid.UUID]*mdNode{}
	link := func(m map[uuid.UUID]*mdNode, id *uuid.UUID) *mdNode {
		if id == nil {
			return nil
		}
		return m[*id]
	}
	for _, p := range pillars {
		n := &mdNode{Kind: mdPillar, NameEn: p.NameEn, NameAr: p.NameAr, ID: p.ID}
		pillarByID[p.ID] = n
		imp.addExistingNode(n)
	}
	for _, e := range enablers {
		n := &mdNode{Kind: mdEnabler, NameEn: e.NameEn, NameAr: e.NameAr, ID: e.ID}
		enablerByID[e.ID] = n
		imp.addExistingNode(n)
	}
	goalByID := map[uuid.UUID]*mdNode{}
	for _, g := range goals {
		n := &mdNode{Kind: mdGoal, NameEn: g.Title, ID: g.ID}
		goalByID[g.ID] = n
		imp.addExistingNode(n)
	}
	objByID := map[uuid.UUID]*mdNode{}
	for _, o := range objectives {
		n := &mdNode{Kind: mdObjective, NameEn: o.NameEn, NameAr: o.NameAr, ID: o.ID,
			Pillar: link(pillarByID, o.PillarID), Enabler: link(enablerByID, o.EnablerID)}
		if o.GoalID != nil {
			n.Goal = goalByID[*o.GoalID]
		}
		objByID[o.ID] = n
		imp.addExistingNode(n)
	}
	for _, p := range processes {
		imp.addExistingNode(&mdNode{Kind: mdProcess, NameEn: p.NameEn, NameAr: p.NameAr, ID: p.ID, Objective: objByID[p.OperationalObjectiveID],
			Pillar: link(pillarByID, p.PillarID), Enabler: link(enablerByID, p.EnablerID)})
	}
	for _, d := range domains {
		imp.addExistingNode(&mdNode{Kind: mdDomain, NameEn: d.NameEn, NameAr: d.NameAr, ID: d.ID, DomainType: d.Type})
	}
	critByID := map[uuid.UUID]*mdNode{}
	for _, c := range criteria {
		n := &mdNode{Kind: mdCriterion, NameEn: c.NameEn, NameAr: c.NameAr, ID: c.ID, CriterionNo: c.CriterionNo}
		critByID[c.ID] = n
		imp.addExistingNode(n)
	}
	for _, s := range subs {
		imp.addExistingNode(&mdNode{Kind: mdSubCriterion, NameEn: s.NameEn, NameAr: s.NameAr, ID: s.ID, SubNo: s.SubNo, Criterion: critByID[s.AwardCriterionID]})
	}
	for _, i := range initiatives {
		imp.addExistingNode(&mdNode{Kind: mdInitiative, NameEn: i.NameEn, NameAr: i.NameAr, ID: i.ID})
	}
	for _, o := range orgs {
		imp.addExistingNode(&mdNode{Kind: mdOrganization, NameEn: o.NameEn, NameAr: o.NameAr, ID: o.ID})
	}
	for _, m := range measures {
		imp.addExistingNode(&mdNode{Kind: mdAwardMeasure, NameEn: m.NameEn, NameAr: m.NameAr, ID: m.ID, Code: m.Code})
	}

	// Existing KPIs across all three dictionary tables, with their first
	// metric (the one imported targets/entries attach to).
	type kpiRow struct {
		ID                 uuid.UUID
		Code               string
		NameEn             string
		NameAr             string
		Polarity           string
		ReportingFrequency string
		Baseline           float64
		OwnerDeptID        *uuid.UUID
	}
	for _, t := range []struct {
		kpiType string
		model   interface{}
	}{
		{models.KPITypeStrategic, &models.StrategicKPI{}},
		{models.KPITypeOperational, &models.OperationalKPI{}},
		{models.KPITypeAward, &models.AwardKPI{}},
	} {
		var rows []kpiRow
		if err := db.Model(t.model).Select("id, code, name_en, name_ar, polarity, reporting_frequency, baseline, owner_dept_id").Scan(&rows).Error; err != nil {
			return err
		}
		for _, r := range rows {
			code := strings.ToUpper(strings.TrimSpace(r.Code))
			ownerName := ""
			if r.OwnerDeptID != nil {
				if d := deptByID[*r.OwnerDeptID]; d != nil {
					ownerName = d.NameEn
				}
			}
			imp.existingKpis[t.kpiType+"|"+code] = &existingKpi{ID: r.ID, Type: t.kpiType, Code: r.Code, NameEn: r.NameEn, NameAr: r.NameAr, Polarity: r.Polarity, Frequency: r.ReportingFrequency, Baseline: r.Baseline, OwnerDeptName: ownerName}
			imp.existingCodeTyp[code] = t.kpiType
		}
	}
	var metrics []models.KpiMetric
	if err := db.Order("display_order ASC, created_at ASC").Find(&metrics).Error; err != nil {
		return err
	}
	byKpi := map[string]*existingKpi{}
	for _, ek := range imp.existingKpis {
		byKpi[ek.Type+"|"+ek.ID.String()] = ek
	}
	for i := range metrics {
		if ek := byKpi[metrics[i].KpiType+"|"+metrics[i].KpiID.String()]; ek != nil {
			ek.Metrics = append(ek.Metrics, &metrics[i])
			if ek.MetricID == nil {
				id := metrics[i].ID
				ek.MetricID = &id
			}
		}
	}

	type targetRow struct {
		ID      uuid.UUID
		KpiCode string
		KpiType string
		Y       int
	}
	var targets []targetRow
	if err := db.Raw(`SELECT id, kpi_code, kpi_type, COALESCE(NULLIF(target_year, 0), year) AS y FROM kpi_annual_targets
		WHERE deleted_at IS NULL AND (lower(period_code) = 'annual' OR (COALESCE(period_code, '') = '' AND period_type = 'annual'))`).Scan(&targets).Error; err != nil {
		return err
	}
	for _, t := range targets {
		imp.existingTargets[fmt.Sprintf("%s|%s|%d", t.KpiType, strings.ToUpper(t.KpiCode), t.Y)] = t.ID
	}

	type entryRow struct {
		ID            uuid.UUID
		KpiID         uuid.UUID
		KpiType       string
		MetricID      uuid.UUID
		ReportingYear int
		PeriodCode    string
	}
	var entries []entryRow
	if err := db.Raw(`SELECT id, kpi_id, kpi_type, metric_id, reporting_year, period_code FROM kpi_entries
		WHERE deleted_at IS NULL AND status NOT IN ? ORDER BY created_at`,
		[]string{models.KpiEntryStatusRejected, models.KpiEntryStatusSuperseded, models.KpiEntryStatusReversed, models.KpiEntryStatusReversal}).Scan(&entries).Error; err != nil {
		return err
	}
	for _, e := range entries {
		imp.existingEntries[fmt.Sprintf("%s|%s|%s|%d|%s", e.KpiID, e.KpiType, e.MetricID, e.ReportingYear, strings.ToLower(e.PeriodCode))] = e.ID
	}
	return nil
}

// ─── Sheet identification & table reading ───────────────────────────────────

var kpiImportSheetRules = []struct {
	key   string
	label string
	match func(n string) bool
}{
	{"pillars", kpiTemplatePillarsSheet, hdrAny("pillars", "الركائز")},
	{"enablers", kpiTemplateEnablersSheet, hdrAny("enablers", "الممكنات")},
	{"goals", kpiTemplateGoalsSheet, hdrAny("goals", "strategic goals", "الأهداف", "الأهداف الاستراتيجية")},
	{"objectives", kpiTemplateObjectivesSheet, hdrAny("parent objectives", "الأهداف الرئيسية")},
	{"processes", kpiTemplateProcessesSheet, hdrAny("operational objectives", "الأهداف التشغيلية")},
	{"initiatives", kpiTemplateInitiativesSheet, hdrAny("initiatives", "المبادرات")},
	{"perf", kpiTemplatePerformanceSheet, hdrAny("performance", "الأداء")},
	{"lists", kpiTemplateListsSheet, hdrAny("lists", "القوائم")},
	{"master", "Master Data", func(n string) bool { return strings.Contains(n, "master data") }},
	{"strategic_dict", "Strategic Dictionary", func(n string) bool {
		return strings.Contains(n, "strategic dictionary") && !strings.Contains(n, "perf")
	}},
	{"operational_dict", "Operational Dictionary", func(n string) bool {
		return strings.Contains(n, "operational") && strings.Contains(n, "dict") && !strings.Contains(n, "perf")
	}},
	{"award_dict", "Award Dictionary", func(n string) bool {
		return strings.Contains(n, "award") && strings.Contains(n, "dict") && !strings.Contains(n, "perf")
	}},
	{"strategic_perf", "Strategic Performance", func(n string) bool {
		return strings.Contains(n, "perf") && strings.Contains(n, "strateg") && !strings.Contains(n, "operational") && !strings.Contains(n, "award")
	}},
	{"operational_perf", "Operational Performance", func(n string) bool {
		return strings.Contains(n, "perf") && strings.Contains(n, "operational")
	}},
	{"award_perf", "Award Performance", func(n string) bool {
		return strings.Contains(n, "perf") && strings.Contains(n, "award")
	}},
}

func (imp *kpiImporter) identifySheets(f *excelize.File) map[string]string {
	found := map[string]string{}
	for _, name := range f.GetSheetList() {
		n := normHeader(name)
		for _, rule := range kpiImportSheetRules {
			if rule.match(n) {
				if prev, dup := found[rule.key]; dup {
					imp.structural(name, fmt.Sprintf("More than one worksheet looks like the %s sheet (%q and %q) — keep only one.", rule.label, prev, name))
				} else {
					found[rule.key] = name
				}
				break
			}
		}
	}
	// Only the dictionaries are mandatory: master data and performance are
	// optional, in either the current or the older layout.
	for _, rule := range kpiImportSheetRules {
		if !strings.HasSuffix(rule.key, "_dict") {
			continue
		}
		if _, ok := found[rule.key]; !ok {
			imp.structural("", fmt.Sprintf("Mandatory worksheet %q is missing — the file does not match the KPI Dictionary template.", rule.label))
		}
	}
	return found
}

type colSpec struct {
	key      string
	match    func(h string) bool
	required bool
}

type sheetTable struct {
	sheet     string
	headerRow int // 0-based index of the English header row
	cols      map[string]int
	colLabel  map[string]string
	years     map[int]int       // annual target year → column (dictionary sheets)
	quarters  map[string][3]int // "YYYY-Q" → target/actual/achievement column (performance sheets; -1 = absent)
	qLabels   map[string][3]string
	rows      [][]string
}

// kpiImportColumnLabels are the template's English header names, used in
// structural error messages.
var kpiImportColumnLabels = map[string]string{
	"code": "Code", "name": "Indicator name", "pillar": "Pillar / Enabler", "goal": "Goal",
	"objective": "Operational Objective", "process": "Related Process Name", "domain": "Related Domain / Award Criterion",
	"seg": "Segmentation Axes", "polarity": "Polarity / Direction", "activation": "Activation Status",
	"description": "Description", "owner_type": "Owner Type", "owner_dept": "Owner Department",
	"agency": "الوكالة / الإدارة المالكة", "related_depts": "Related Departments", "frequency": "Reporting Frequency",
	"classification": "KPI Classification", "target_requirement": "Target Requirement", "unit": "Unit of Measure",
	"owner_org": "Owner Organization", "award_measure": "Award KPIs and Measures",
	"lifecycle": "KPI Lifecycle", "data_source": "Data Source", "formula": "Formula", "baseline": "Baseline",
	"notes": "Notes", "criterion": "Criteria", "sub_no": "Sub-Criterion #", "sub_name": "Sub-Criterion", "sub_criterion": "Sub-Criteria",
	"name_en": "EPM KPI (EN)", "name_ar": "EPM KPI (AR)", "description_en": "Description (EN)", "description_ar": "Description (AR)",
	"parent_objective": "Parent Objective", "md_parent": "Parent Objective",
	"md_name": "Name (English)", "md_name_ar": "Name (Arabic)", "md_active": "Active", "md_status": "Status", "md_owner": "Owner (Department)", "md_pillar": "Pillar", "md_enabler": "Enabler", "md_goal": "Goal",
	"year": "Year", "quarter": "Quarter", "target": "Target", "actual": "Actual Result", "achievement": "Achievement",
}

func hdrEq(s string) func(string) bool { return func(h string) bool { return h == s } }
func hdrPrefix(s string) func(string) bool {
	return func(h string) bool { return strings.HasPrefix(h, s) }
}

// hdrAny matches any of the given exact header labels. It lets one spec accept
// both the English and Arabic template headers (the template is single-language).
func hdrAny(values ...string) func(string) bool {
	return func(h string) bool {
		for _, v := range values {
			if h == v {
				return true
			}
		}
		return false
	}
}

func hdrPrefixAny(prefixes ...string) func(string) bool {
	return func(h string) bool {
		for _, p := range prefixes {
			if strings.HasPrefix(h, p) {
				return true
			}
		}
		return false
	}
}

func hdrContainsAny(values ...string) func(string) bool {
	return func(h string) bool {
		for _, v := range values {
			if strings.Contains(h, v) {
				return true
			}
		}
		return false
	}
}

func dictColumns(kpiType string) []colSpec {
	cols := []colSpec{
		{"code", hdrAny("code", "الكود"), true},
		// The KPI forms require an operational objective for every KPI type;
		// its parent objective, goal and (when not given) pillar follow from it.
		{"pillar", hdrPrefixAny("pillar", "الركيزة"), false},
		{"goal", hdrAny("goal", "strategic goal", "الهدف", "الهدف الاستراتيجي"), false},
		// Checked per row rather than as a mandatory column, so that rows of
		// older workbooks without it are each rejected with a reason.
		{"objective", hdrAny("objective", "operational objective", "الهدف التشغيلي"), false},
		{"parent_objective", hdrAny("parent objective", "الهدف الرئيسي"), false},
		// Older operational sheets: "Operational Objective" was the parent
		// objective and this column the operational objective (process).
		{"process", hdrPrefixAny("related process", "اسم العملية"), false},
		{"domain", func(h string) bool {
			return hdrAny("domains", "domain", "المجالات", "المجال")(h) || hdrPrefixAny("related domain", "المجال المرتبط")(h)
		}, false},
		{"seg", hdrPrefixAny("segmentation axes", "محاور التجزئة"), false},
		{"polarity", hdrPrefixAny("polarity", "الاتجاه"), true},
		// Not applied on import (workflow-managed); optional so the current
		// template can leave it out.
		{"activation", hdrPrefixAny("activation status", "حالة التفعيل"), false},
		{"description", hdrAny("description", "الوصف"), false},
		// Current template: the KPI form's labels.
		{"name_en", hdrAny("epm kpi (en)", "مؤشر الأمانة (إنجليزي)", "name (en)"), false},
		{"name_ar", hdrAny("epm kpi (ar)", "مؤشر الأمانة (عربي)", "name (ar)"), false},
		{"description_en", hdrAny("description (en)", "الوصف (إنجليزي)"), false},
		{"description_ar", hdrAny("description (ar)", "الوصف (عربي)"), false},
		// Criteria / Sub-Criteria apply to every KPI type, as on the forms.
		// "Sub-Criterion" (an older award column holding the name of a new
		// sub-criterion) is listed first so it keeps its meaning.
		{"criterion", hdrAny("criteria", "criterion", "المعيار"), false},
		{"sub_name", hdrAny("sub-criterion"), false},
		{"sub_criterion", hdrAny("sub-criteria", "sub criteria", "المعيار الفرعي"), false},
		{"owner_type", hdrAny("owner type", "نوع المالك"), false},
		{"owner_dept", hdrPrefixAny("owner department", "الإدارة المالكة", "المالك"), true},
		{"agency", hdrContainsAny("owning agency", "الوكالة / الإدارة المالكة"), false},
		// Related Departments replaced the free-text Related Administrative
		// Units; workbooks with the old header still map here.
		{"related_depts", hdrPrefixAny("related departments", "الإدارات ذات الصلة", "related administrative units", "الوحدات الإدارية"), false},
		{"classification", hdrPrefixAny("kpi classification", "classification", "تصنيف المؤشر"), false},
		{"target_requirement", hdrPrefixAny("target requirement", "متطلب المستهدف"), false},
		{"unit", hdrPrefixAny("unit of measure", "وحدة القياس"), false},
		{"owner_org", hdrPrefixAny("owner organization", "المنظمة المالكة"), false},
		{"award_measure", hdrPrefixAny("award kpis and measures", "award kpi measure", "مؤشرات ومقاييس الجائزة", "مؤشر الجائزة"), false},
		{"frequency", hdrPrefixAny("reporting frequency", "تكرار الإبلاغ"), true},
		{"lifecycle", hdrPrefixAny("kpi lifecycle", "دورة حياة المؤشر"), false},
		{"data_source", hdrAny("data source", "مصدر البيانات"), false},
		// "Formula" / "الصيغة" as on the KPI form; older templates: "Calculation Formula".
		{"formula", func(h string) bool {
			return hdrAny("formula", "الصيغة")(h) || hdrPrefixAny("calculation formula", "معادلة الحساب")(h)
		}, false},
		{"baseline", hdrAny("baseline", "خط الأساس"), false},
		{"notes", hdrAny("notes", "ملاحظات"), false},
	}
	switch kpiType {
	// Older templates: one name column per KPI type, and for awards the
	// sub-criterion number in its own column. Checked per row (the current
	// template has EPM KPI (EN) / (AR) instead).
	case models.KPITypeStrategic:
		cols = append(cols, colSpec{"name", hdrAny("strategic indicator", "المؤشر الاستراتيجي"), false})
	case models.KPITypeOperational:
		cols = append(cols, colSpec{"name", hdrAny("operational indicator", "المؤشر التشغيلي"), false})
	case models.KPITypeAward:
		cols = append(cols,
			colSpec{"name", hdrAny("indicator / measure", "مؤشر / مقياس"), false},
			colSpec{"sub_no", hdrAny("sub-criterion #", "رقم المعيار الفرعي"), false},
		)
	}
	return cols
}

func perfColumns(kpiType string) []colSpec {
	cols := []colSpec{
		{"code", hdrAny("code", "الكود"), true},
		{"owner_dept", hdrPrefixAny("owner department", "الإدارة المالكة", "المالك"), false},
	}
	switch kpiType {
	case models.KPITypeStrategic:
		cols = append(cols, colSpec{"name", hdrAny("strategic indicator", "المؤشر الاستراتيجي"), false})
	case models.KPITypeOperational:
		cols = append(cols, colSpec{"name", hdrAny("operational indicator", "المؤشر التشغيلي"), false})
	case models.KPITypeAward:
		cols = append(cols, colSpec{"name", hdrPrefixAny("award indicators", "مؤشرات الجائزة"), false})
	}
	return cols
}

var (
	annualTargetHeaderRe = regexp.MustCompile(`^(?:target\S*|مستهدفات)\s*(\d{4})$`)
	quarterHeaderRe      = regexp.MustCompile(`^(target|actual result|achievement|المستهدف|النتيجة الفعلية|نسبة الإنجاز)\s+(\d{4})\s+q([1-4])$`)
	quarterHeaderPos     = map[string]int{"target": 0, "actual result": 1, "achievement": 2, "المستهدف": 0, "النتيجة الفعلية": 1, "نسبة الإنجاز": 2}
)

// readTable locates the header row (the row holding a "Code" column — the
// English header row; the Arabic header row sits directly above it) and maps
// every template column. Headers are matched against both header rows so a
// column whose "English" header is still in Arabic is recognised too.
// tableMode says which repeated year/quarter columns readTable collects.
type tableMode int

const (
	tableDictionary     tableMode = iota // "Targets YYYY" columns
	tableQuarterColumns                  // older wide Performance sheets: "Actual Result YYYY Qn" …
	tableRows                            // fixed columns only
)

func (imp *kpiImporter) readTable(sheet string, rows [][]string, specs []colSpec, mode tableMode) *sheetTable {
	t := &sheetTable{sheet: sheet, headerRow: -1, cols: map[string]int{}, colLabel: map[string]string{},
		years: map[int]int{}, quarters: map[string][3]int{}, qLabels: map[string][3]string{}}
	if sheet == "" {
		return t
	}
	// The header row is the one holding the first spec's column (Code, or
	// Name on the master data sheets).
	anchor := specs[0]
	for i := 0; i < len(rows) && i < 15; i++ {
		for _, c := range rows[i] {
			if h := normHeader(c); h != "" && anchor.match(h) {
				t.headerRow = i
				break
			}
		}
		if t.headerRow >= 0 {
			break
		}
	}
	if t.headerRow < 0 {
		imp.structural(sheet, fmt.Sprintf("Header row not found — the sheet must have the template's header row with a %q column.", kpiImportColumnLabels[anchor.key]))
		return t
	}
	eng := rows[t.headerRow]
	var ar []string
	if t.headerRow > 0 {
		ar = rows[t.headerRow-1]
	}
	width := len(eng)
	if len(ar) > width {
		width = len(ar)
	}
	for j := 0; j < width; j++ {
		e, a := normHeader(cell(eng, j)), normHeader(cell(ar, j))
		label := strings.TrimSpace(strings.ReplaceAll(cell(eng, j), "\n", " "))
		if label == "" {
			label = strings.TrimSpace(strings.ReplaceAll(cell(ar, j), "\n", " "))
		}
		for _, s := range specs {
			if _, taken := t.cols[s.key]; taken {
				continue
			}
			if (e != "" && s.match(e)) || (a != "" && s.match(a)) {
				t.cols[s.key] = j
				t.colLabel[s.key] = inputHeaderText(label)
				break
			}
		}
		if mode == tableDictionary {
			for _, h := range []string{e, a} {
				if m := annualTargetHeaderRe.FindStringSubmatch(h); m != nil {
					y, _ := strconv.Atoi(m[1])
					if _, dup := t.years[y]; !dup {
						t.years[y] = j
					}
					break
				}
			}
		} else if mode == tableQuarterColumns {
			var m []string
			for _, h := range []string{e, a} {
				if h != "" {
					if mm := quarterHeaderRe.FindStringSubmatch(h); mm != nil {
						m = mm
						break
					}
				}
			}
			if m != nil {
				key := m[2] + "-" + m[3]
				idx, ok := t.quarters[key]
				lbl := t.qLabels[key]
				if !ok {
					idx = [3]int{-1, -1, -1}
				}
				if pos, ok := quarterHeaderPos[m[1]]; ok && idx[pos] < 0 {
					idx[pos] = j
					lbl[pos] = label
				}
				t.quarters[key] = idx
				t.qLabels[key] = lbl
			}
		}
	}
	for _, s := range specs {
		if _, ok := t.cols[s.key]; !ok && s.required {
			imp.structural(sheet, fmt.Sprintf("Mandatory column %q is missing or its header was changed.", kpiImportColumnLabels[s.key]))
		}
	}
	// "Targets YYYY" columns are optional: the current template sets targets
	// on the Targets sheet; older workbooks still carry them.
	if mode == tableQuarterColumns {
		hasActual := false
		for _, idx := range t.quarters {
			if idx[1] >= 0 {
				hasActual = true
			}
		}
		if !hasActual {
			imp.structural(sheet, "No quarterly \"Actual Result YYYY Qn\" columns were found.")
		}
	}
	t.rows = rows
	return t
}

func (t *sheetTable) get(row []string, key string) string {
	j, ok := t.cols[key]
	if !ok {
		return ""
	}
	return strings.TrimSpace(cell(row, j))
}

func (t *sheetTable) label(key string) string {
	if l, ok := t.colLabel[key]; ok {
		return l
	}
	if l, ok := kpiImportColumnLabels[key]; ok {
		return l // column missing from the sheet
	}
	return key
}

// blank reports whether none of the mapped data columns hold a value (the
// template ships with pre-numbered empty rows).
func (t *sheetTable) blank(row []string) bool {
	for _, j := range t.cols {
		if strings.TrimSpace(cell(row, j)) != "" {
			return false
		}
	}
	for _, j := range t.years {
		if strings.TrimSpace(cell(row, j)) != "" {
			return false
		}
	}
	for _, idx := range t.quarters {
		for _, j := range idx {
			if j >= 0 && strings.TrimSpace(cell(row, j)) != "" {
				return false
			}
		}
	}
	return true
}

// ─── Master Data sheet ──────────────────────────────────────────────────────

// parseMasterData walks the Master Data sheet's sections — each starts with
// a "#" header row whose next cell names the section (Pillars, Enablers,
// Strategic Goals, Operational Objectives, Initiatives). Cross references
// inside the sheet use each section's "#" number.
func (imp *kpiImporter) parseMasterData(sheet string, rows [][]string) {
	type pending struct {
		node                  *mdNode
		pillarRef, enablerRef string
		goalRef               string
		ownerName             string
		row                   int
	}
	var section string
	var hdr map[string]int
	refs := map[string]map[string]*mdNode{mdPillar: {}, mdEnabler: {}, mdGoal: {}}
	var pend []pending
	seen := map[string]map[string]int{}

	numCol := -1
	for i, r := range rows {
		excelRow := i + 1
		// Section header: a "#" cell followed by the section title.
		isHeader := false
		for j, c := range r {
			if strings.TrimSpace(c) == "#" && j+1 < len(r) && strings.TrimSpace(r[j+1]) != "" {
				numCol = j
				title := normHeader(r[j+1])
				switch {
				case strings.HasPrefix(title, "pill"), strings.HasPrefix(title, "الركائز"), strings.HasPrefix(title, "ركائز"):
					section = mdPillar
				case strings.HasPrefix(title, "enabler"), strings.HasPrefix(title, "الممكنات"), strings.HasPrefix(title, "ممكنات"):
					section = mdEnabler
				case strings.HasPrefix(title, "strategic goal"), strings.HasPrefix(title, "الأهداف الاستراتيجية"), strings.HasPrefix(title, "الهدف الاستراتيجي"):
					section = mdGoal
				case strings.HasPrefix(title, "operational objective"), strings.HasPrefix(title, "الأهداف التشغيلية"), strings.HasPrefix(title, "الهدف التشغيلي"):
					section = mdObjective
				case strings.HasPrefix(title, "initiative"), strings.HasPrefix(title, "المبادرات"), strings.HasPrefix(title, "مبادرة"):
					section = mdInitiative
				default:
					section = "" // reference-only lists (e.g. Strategic indicators) are not imported
				}
				hdr = map[string]int{}
				for k := j + 1; k < len(r); k++ {
					hdr[normHeader(r[k])] = k
				}
				isHeader = true
				break
			}
		}
		if isHeader || section == "" || numCol < 0 {
			continue
		}
		num := strings.TrimSpace(cell(r, numCol))
		name := strings.TrimSpace(cell(r, numCol+1))
		if name == "" {
			continue // numbered but empty template row
		}
		findCol := func(pred func(string) bool) string {
			for h, k := range hdr {
				if pred(h) {
					return strings.TrimSpace(cell(r, k))
				}
			}
			return ""
		}
		if seen[section] == nil {
			seen[section] = map[string]int{}
		}
		if prev, dup := seen[section][normName(name)]; dup {
			imp.errorf(sheet, excelRow, name, "Name", fmt.Sprintf("Duplicate %s — already listed on row %d.", kindLabel(section), prev))
			continue
		}
		seen[section][normName(name)] = excelRow

		node := imp.sheetNode(section, name, nil, sheet, excelRow)
		p := pending{node: node, row: excelRow}
		switch section {
		case mdPillar, mdEnabler:
			p.ownerName = findCol(func(h string) bool {
				return strings.Contains(h, "owner") || strings.Contains(h, "مالك") || strings.Contains(h, "المالكة")
			})
			if num != "" {
				refs[section][num] = node
			}
		case mdGoal:
			p.pillarRef = findCol(hdrPrefixAny("pillar ref", "مرجع الركيزة"))
			p.enablerRef = findCol(hdrPrefixAny("enabler ref", "مرجع الممكن"))
			if num != "" {
				refs[mdGoal][num] = node
			}
		case mdObjective:
			p.goalRef = findCol(hdrPrefixAny("strategic goal ref", "مرجع الهدف الاستراتيجي"))
		case mdInitiative:
			p.ownerName = findCol(hdrPrefixAny("initiative owner", "مالك المبادرة"))
			p.pillarRef = findCol(hdrPrefixAny("pillar ref", "مرجع الركيزة"))
			p.enablerRef = findCol(hdrPrefixAny("enabler ref", "مرجع الممكن"))
			p.goalRef = findCol(hdrPrefixAny("strategic goal ref", "مرجع الهدف الاستراتيجي"))
		}
		pend = append(pend, p)
	}

	resolveRef := func(p pending, kind, ref, field string) *mdNode {
		if ref == "" {
			return nil
		}
		ref = strings.TrimSuffix(ref, ".0")
		n, ok := refs[kind][ref]
		if !ok {
			imp.errorf(sheet, p.row, p.node.NameEn, field, fmt.Sprintf("Reference %q does not match any %s number on this sheet.", ref, kindLabel(kind)))
			imp.rejectNode(p.node, "invalid reference")
			return nil
		}
		return n
	}
	for _, p := range pend {
		n := p.node
		switch n.Kind {
		case mdPillar, mdEnabler:
			if p.ownerName != "" && !n.Exists {
				n.Owner = imp.department(p.ownerName, sheet, p.row, n.NameEn, "Owner Department")
			}
		case mdGoal:
			n.Pillar = resolveRef(p, mdPillar, p.pillarRef, "Pillar Ref.")
			n.Enabler = resolveRef(p, mdEnabler, p.enablerRef, "Enabler Ref.")
			if p.pillarRef == "" && p.enablerRef == "" && !n.Exists {
				imp.warn(sheet, p.row, n.NameEn, "Pillar Ref.", "Strategic goal is not linked to any pillar or enabler.")
			}
		case mdObjective:
			if p.goalRef == "" {
				imp.errorf(sheet, p.row, n.NameEn, "Strategic Goal Ref.", "Operational objective must reference a strategic goal.")
				imp.rejectNode(n, "missing strategic goal")
				continue
			}
			n.Goal = resolveRef(p, mdGoal, p.goalRef, "Strategic Goal Ref.")
			if n.Goal != nil {
				n.Pillar, n.Enabler = n.Goal.Pillar, n.Goal.Enabler
			}
		case mdInitiative:
			n.Pillar = resolveRef(p, mdPillar, p.pillarRef, "Pillar Ref.")
			n.Enabler = resolveRef(p, mdEnabler, p.enablerRef, "Enabler Ref.")
			n.Goal = resolveRef(p, mdGoal, p.goalRef, "Strategic Goal Ref.")
			if p.ownerName != "" {
				// Initiative owners are usually people, not departments —
				// link only when the name matches an existing department.
				if d := imp.findExact(mdDepartment, p.ownerName, nil); d != nil {
					n.Owner = d
				} else {
					imp.warn(sheet, p.row, n.NameEn, "Initiative Owner",
						fmt.Sprintf("Initiative owner %q does not match an existing department; no owner will be linked.", p.ownerName))
				}
			}
		}
	}
}

// ─── Master data sheets (one per entity) ────────────────────────────────────

// kpiMasterSheets are the template's master data sheets, in the order they
// are read: a sheet may only reference records from itself or the sheets
// before it (or the system).
var kpiMasterSheets = []struct{ key, kind string }{
	{"pillars", mdPillar}, {"enablers", mdEnabler}, {"goals", mdGoal},
	{"objectives", mdObjective}, {"processes", mdProcess}, {"initiatives", mdInitiative},
}

func masterSheetColumns(kind string) []colSpec {
	// The same headers as the Master Data import templates; "Name" and
	// "Owner Department" / "Initiative Owner" from older workbooks too.
	cols := []colSpec{{"md_name", hdrAny("name", "الاسم", "name (english)", "الاسم (بالإنجليزية)"), true}}
	nameAr := colSpec{"md_name_ar", hdrAny("name (arabic)", "الاسم (بالعربية)"), false}
	active := colSpec{"md_active", hdrAny("active", "نشط"), false}
	owner := func(older ...string) colSpec {
		return colSpec{"md_owner", func(h string) bool {
			return hdrAny("owner (department)", "المالك (الإدارة)")(h) || hdrPrefixAny(older...)(h)
		}, false}
	}
	goal := hdrAny("goal", "strategic goal", "الهدف", "الهدف الاستراتيجي")
	pillar := colSpec{"md_pillar", hdrAny("pillar", "الركيزة"), false}
	enabler := colSpec{"md_enabler", hdrAny("enabler", "الممكن", "الممكّن"), false}
	switch kind {
	case mdPillar, mdEnabler:
		cols = append(cols, nameAr, owner("owner department", "الإدارة المالكة"), active)
	case mdObjective:
		cols = append(cols, nameAr, colSpec{"md_goal", goal, true}, pillar, enabler, active)
	case mdProcess:
		cols = append(cols, nameAr,
			colSpec{"md_parent", hdrAny("parent objective", "الهدف الرئيسي"), true},
			// The goal follows the parent objective; a value is only checked.
			colSpec{"md_goal", goal, false},
			pillar, enabler, active)
	case mdInitiative:
		cols = append(cols, nameAr,
			colSpec{"md_goal", goal, true},
			colSpec{"md_parent", hdrAny("parent objective", "الهدف الرئيسي"), false},
			pillar, enabler,
			owner("initiative owner", "مالك المبادرة"),
			colSpec{"md_status", hdrAny("status", "الحالة"), false})
	}
	return cols
}

// parseMasterSheet reads one master data sheet. References to other master
// data are by name: a record listed on an earlier sheet or one that already
// exists in the system.
func (imp *kpiImporter) parseMasterSheet(kind string, t *sheetTable) {
	if t == nil || t.headerRow < 0 {
		return
	}
	seen := map[string]int{}
	pastTemplateRows := false
	for i := t.headerRow + 1; i < len(t.rows); i++ {
		r := t.rows[i]
		if t.blank(r) {
			continue
		}
		excelRow := i + 1
		// The workbook's dropdowns and auto-fills list the first
		// kpiTemplateMasterRows rows of a master data sheet only.
		if excelRow > kpiTemplateMasterRows+1 && !pastTemplateRows {
			pastTemplateRows = true
			imp.warn(t.sheet, excelRow, "", "", fmt.Sprintf(
				"This sheet takes %d rows; rows from %d on are imported, but the workbook's dropdowns do not list them. For more records, use the Master Data import.",
				kpiTemplateMasterRows, kpiTemplateMasterRows+2))
		}
		name := t.get(r, "md_name")
		if name == "" {
			imp.errorf(t.sheet, excelRow, "", t.label("md_name"), "Name is required.")
			continue
		}
		if len([]rune(name)) > 255 {
			imp.errorf(t.sheet, excelRow, name, t.label("md_name"), "Name must be at most 255 characters.")
			continue
		}
		var n *mdNode
		ref := func(refKind, key string) *mdNode {
			v := t.get(r, key)
			if v == "" {
				return nil
			}
			if c := imp.sameNameCount(refKind, v); c > 1 {
				imp.errorf(t.sheet, excelRow, name, t.label(key),
					fmt.Sprintf("%q matches %d %s records — rename the duplicates in Master Data so the name is unique.", v, c, kindLabel(refKind)))
				imp.rejectNode(n, "ambiguous reference")
				return nil
			}
			found, fuzzy := imp.find(refKind, v, nil)
			if found == nil {
				imp.errorf(t.sheet, excelRow, name, t.label(key),
					fmt.Sprintf("%s %q was not found in the system or on the %s sheet.", strings.ToUpper(kindLabel(refKind)[:1])+kindLabel(refKind)[1:], v, kpiMasterSheetName(refKind)))
				imp.rejectNode(n, "invalid reference")
				return nil
			}
			if fuzzy {
				imp.warn(t.sheet, excelRow, name, t.label(key), fmt.Sprintf("%q matched to %s %q.", v, kindLabel(refKind), found.NameEn))
			}
			return found
		}

		// An operational objective (process) belongs to its parent objective:
		// the same name may exist under another parent.
		var parent *mdNode
		dupKey := normName(name)
		if kind == mdProcess {
			if t.get(r, "md_parent") == "" {
				imp.errorf(t.sheet, excelRow, name, t.label("md_parent"), "Operational objective must reference a parent objective.")
				continue
			}
			if parent = ref(mdObjective, "md_parent"); parent == nil {
				continue
			}
			dupKey = fmt.Sprintf("%p|%s", parent, dupKey)
		}
		if prev, dup := seen[dupKey]; dup {
			imp.errorf(t.sheet, excelRow, name, t.label("md_name"), fmt.Sprintf("Duplicate %s — already listed on row %d.", kindLabel(kind), prev))
			continue
		}
		seen[dupKey] = excelRow
		n = imp.sheetNode(kind, name, parent, t.sheet, excelRow)
		// Arabic name, Active and Status apply to a record this sheet
		// creates (an existing one is linked, not changed).
		if !n.Exists {
			if ar := t.get(r, "md_name_ar"); ar != "" {
				if len([]rune(ar)) > 255 {
					imp.errorf(t.sheet, excelRow, name, t.label("md_name_ar"), "Name (Arabic) must be at most 255 characters.")
					imp.rejectNode(n, "invalid Arabic name")
				}
				n.NameAr = ar
			}
			if raw := t.get(r, "md_active"); raw != "" {
				if active, ok := parseMetricBool(raw, true); ok {
					n.Inactive = !active
				} else {
					imp.errorf(t.sheet, excelRow, name, t.label("md_active"), fmt.Sprintf("Active %q must be Yes or No.", raw))
					imp.rejectNode(n, "invalid Active value")
				}
			}
			if raw := t.get(r, "md_status"); raw != "" {
				if status, ok := normalizeMetricChoice(raw, masterInitiativeStatusChoices); ok {
					n.Status = status
				} else {
					imp.errorf(t.sheet, excelRow, name, t.label("md_status"), fmt.Sprintf("Status %q is not valid. Allowed: Draft, Active, On Hold, Complete, Cancelled.", raw))
					imp.rejectNode(n, "invalid status")
				}
			}
		}
		if parent != nil && parent.Rejected {
			imp.errorf(t.sheet, excelRow, name, t.label("md_parent"), fmt.Sprintf("Parent objective %q was rejected (%s).", parent.NameEn, parent.RejectWhy))
			imp.rejectNode(n, "parent objective rejected")
		}

		switch kind {
		case mdPillar, mdEnabler:
			if owner := t.get(r, "md_owner"); owner != "" && !n.Exists {
				n.Owner = imp.department(owner, t.sheet, excelRow, name, t.label("md_owner"))
			}
		case mdObjective:
			if t.get(r, "md_goal") == "" {
				imp.errorf(t.sheet, excelRow, name, t.label("md_goal"), "Parent objective must reference a goal.")
				imp.rejectNode(n, "missing strategic goal")
				continue
			}
			n.Goal = ref(mdGoal, "md_goal")
			n.Pillar = ref(mdPillar, "md_pillar")
			n.Enabler = ref(mdEnabler, "md_enabler")
		case mdProcess:
			// The goal comes from the parent objective, as on the form; a
			// Goal cell that names another goal is refused (as in the
			// Operational Objectives import).
			if g := t.get(r, "md_goal"); g != "" && parent.Goal != nil &&
				normName(g) != normName(parent.Goal.NameEn) && (parent.Goal.NameAr == "" || normName(g) != normName(parent.Goal.NameAr)) {
				imp.errorf(t.sheet, excelRow, name, t.label("md_goal"),
					fmt.Sprintf("Goal %q is not the goal of parent objective %q (%q). The goal follows the parent objective.", g, parent.NameEn, parent.Goal.NameEn))
				imp.rejectNode(n, "goal does not match the parent objective")
				continue
			}
			n.Objective = parent
			n.Pillar = ref(mdPillar, "md_pillar")
			n.Enabler = ref(mdEnabler, "md_enabler")
		case mdInitiative:
			if t.get(r, "md_goal") == "" {
				imp.errorf(t.sheet, excelRow, name, t.label("md_goal"), "Initiative must reference a goal.")
				imp.rejectNode(n, "missing strategic goal")
				continue
			}
			n.Goal = ref(mdGoal, "md_goal")
			n.Objective = ref(mdObjective, "md_parent")
			n.Pillar = ref(mdPillar, "md_pillar")
			n.Enabler = ref(mdEnabler, "md_enabler")
			if owner := t.get(r, "md_owner"); owner != "" {
				// Initiative owners are usually people, not departments —
				// link only when the name matches an existing department.
				if d := imp.findExact(mdDepartment, owner, nil); d != nil {
					n.Owner = d
				} else {
					imp.warn(t.sheet, excelRow, name, t.label("md_owner"),
						fmt.Sprintf("Initiative owner %q does not match an existing department; no owner will be linked.", owner))
				}
			}
		}
	}
}

func kpiMasterSheetName(kind string) string {
	switch kind {
	case mdPillar:
		return kpiTemplatePillarsSheet
	case mdEnabler:
		return kpiTemplateEnablersSheet
	case mdGoal:
		return kpiTemplateGoalsSheet
	case mdObjective:
		return kpiTemplateObjectivesSheet
	case mdProcess:
		return kpiTemplateProcessesSheet
	}
	return kpiTemplateInitiativesSheet
}

// sheetNode returns the node for a master-data record declared on the
// Master Data sheet — reusing the existing record with that exact name if
// there is one, otherwise a new node to create.
func (imp *kpiImporter) sheetNode(kind, name string, scope *mdNode, sheet string, row int) *mdNode {
	if n := imp.findExact(kind, name, scope); n != nil {
		n.FromSheet = true
		if n.Sheet == "" {
			n.Sheet, n.Row = sheet, row
		}
		return n
	}
	n := imp.newNode(kind, name, sheet, row)
	n.FromSheet = true
	return n
}

func (imp *kpiImporter) newNode(kind, name, sheet string, row int) *mdNode {
	n := &mdNode{Kind: kind, ID: uuid.New(), Sheet: sheet, Row: row}
	n.NameEn, n.NameAr = splitName(name)
	imp.md[kind] = append(imp.md[kind], n)
	return n
}

func (imp *kpiImporter) rejectNode(n *mdNode, why string) {
	if n != nil && !n.Exists && !n.Rejected {
		n.Rejected, n.RejectWhy = true, why
	}
}

func scopeMatches(n, scope *mdNode) bool {
	if scope == nil {
		return true
	}
	switch n.Kind {
	case mdProcess:
		return n.Objective == scope
	case mdSubCriterion:
		return n.Criterion == scope
	}
	return true
}

// findAll returns every record of kind with exactly this English or Arabic
// name (operational objectives may share a name under different parents).
func (imp *kpiImporter) findAll(kind, name string) []*mdNode {
	key := normName(name)
	var out []*mdNode
	for _, n := range imp.md[kind] {
		if key != "" && (normName(n.NameEn) == key || (n.NameAr != "" && normName(n.NameAr) == key)) {
			out = append(out, n)
		}
	}
	return out
}

// sameNameCount is how many records of kind are called name (EN or AR):
// above 1 the name does not say which record is meant.
func (imp *kpiImporter) sameNameCount(kind, name string) int {
	return len(imp.findAll(kind, name))
}

func (imp *kpiImporter) findExact(kind, name string, scope *mdNode) *mdNode {
	key := normName(name)
	if key == "" {
		return nil
	}
	for _, n := range imp.md[kind] {
		if scopeMatches(n, scope) && (normName(n.NameEn) == key || (n.NameAr != "" && normName(n.NameAr) == key)) {
			return n
		}
	}
	return nil
}

// find resolves a master-data name: exact (EN or AR, case/space-insensitive)
// first, then a unique prefix match either way round (e.g. "Urban Planning"
// → "Urban Planning and Land Management"). fuzzy reports the latter so the
// caller can warn about it.
func (imp *kpiImporter) find(kind, name string, scope *mdNode) (node *mdNode, fuzzy bool) {
	if n := imp.findExact(kind, name, scope); n != nil {
		return n, false
	}
	key := normName(name)
	if len(key) < 4 {
		return nil, false
	}
	var cands []*mdNode
	for _, n := range imp.md[kind] {
		if !scopeMatches(n, scope) {
			continue
		}
		for _, nm := range []string{normName(n.NameEn), normName(n.NameAr)} {
			if nm != "" && (strings.HasPrefix(nm, key) || strings.HasPrefix(key, nm)) {
				cands = append(cands, n)
				break
			}
		}
	}
	if len(cands) == 1 {
		return cands[0], true
	}
	return nil, false
}

// department resolves an owner/agency name to a department, creating one
// (flagged with a warning) when it doesn't exist yet.
func (imp *kpiImporter) department(name, sheet string, row int, recordID, field string) *mdNode {
	// Exact match only — similar department names are often genuinely
	// different units.
	if n := imp.findExact(mdDepartment, name, nil); n != nil {
		return n
	}
	if len([]rune(name)) > 100 {
		imp.errorf(sheet, row, recordID, field, fmt.Sprintf("Department name %q is longer than 100 characters.", name))
		return nil
	}
	n := imp.newNode(mdDepartment, name, sheet, row)
	n.DeptType = "internal"
	imp.warn(sheet, row, recordID, field, fmt.Sprintf("Department %q does not exist and will be created.", name))
	return n
}

var awardMeasureCodeRe = regexp.MustCompile(`^KPI-AWRD-MSR-\d+$`)

// awardMeasure resolves an Award KPI measure by its code (KPI-AWRD-MSR-NN)
// or its English/Arabic name.
func (imp *kpiImporter) awardMeasure(v string) *mdNode {
	for _, n := range imp.md[mdAwardMeasure] {
		if n.Code != "" && strings.EqualFold(n.Code, strings.TrimSpace(v)) {
			return n
		}
	}
	return imp.findExact(mdAwardMeasure, v, nil)
}

// splitList splits a multi-value cell (Related Departments) on new lines,
// semicolons and commas, including the Arabic comma.
func splitList(s string) []string {
	var out []string
	for _, p := range strings.FieldsFunc(s, func(r rune) bool {
		return r == '\n' || r == ';' || r == ',' || r == '،' || r == '؛'
	}) {
		if p = strings.TrimSpace(p); p != "" {
			out = append(out, p)
		}
	}
	return out
}

// ─── Dictionary sheets ──────────────────────────────────────────────────────

var kpiCodeRe = regexp.MustCompile(`^[A-Z][A-Z0-9]*(-[A-Z0-9]+)+$`)

func (imp *kpiImporter) parseDictionary(kpiType string, t *sheetTable) {
	if t == nil || t.headerRow < 0 {
		return
	}
	_, a := t.cols["name_en"]
	_, b := t.cols["name_ar"]
	if _, c := t.cols["name"]; !a && !b && !c {
		imp.structural(t.sheet, fmt.Sprintf("Mandatory column %q is missing or its header was changed.", kpiImportColumnLabels["name_en"]))
		return
	}
	for i := t.headerRow + 1; i < len(t.rows); i++ {
		r := t.rows[i]
		if t.blank(r) {
			continue
		}
		excelRow := i + 1
		code := strings.ToUpper(t.get(r, "code"))
		rec := &kpiRec{Type: kpiType, Code: code, Sheet: t.sheet, Row: excelRow, ID: uuid.New()}
		id := code
		if id == "" {
			id = t.get(r, "name_en")
		}
		if id == "" {
			id = t.get(r, "name")
		}
		fail := func(key, msg string) {
			imp.errorf(t.sheet, excelRow, id, t.label(key), msg)
			if !rec.Rejected {
				rec.Rejected, rec.RejectWhy = true, "KPI row has validation errors"
			}
		}
		refFail := func(key string, n *mdNode) {
			if n != nil && n.Rejected {
				fail(key, fmt.Sprintf("References %s %q, which was rejected (%s).", kindLabel(n.Kind), n.NameEn, n.RejectWhy))
			}
		}

		// Identifier
		switch {
		case code == "":
			fail("code", "KPI code is required.")
		case len(code) > 50:
			fail("code", "KPI code must be at most 50 characters.")
		case !kpiCodeRe.MatchString(code):
			fail("code", fmt.Sprintf("KPI code %q is not in the expected format (e.g. KPI-P1-01-01, OP-P1-01-01).", code))
		}
		if code != "" {
			if prev, dup := imp.kpiByCode[code]; dup {
				fail("code", fmt.Sprintf("Duplicate KPI code — already defined on %q row %d.", prev.Sheet, prev.Row))
			} else if other, ok := imp.existingCodeTyp[code]; ok && other != kpiType {
				fail("code", fmt.Sprintf("KPI code %q already belongs to an existing %s KPI.", code, other))
			} else {
				imp.kpiByCode[code] = rec
				imp.kpiByTypeCode[kpiType+"|"+code] = rec
			}
			if ek, ok := imp.existingKpis[kpiType+"|"+code]; ok {
				eid := ek.ID
				rec.ExistingID = &eid
				rec.ID = ek.ID
				rec.MetricID = ek.MetricID
			}
		}

		// Name / descriptive fields
		_, hasNameEn := t.cols["name_en"]
		_, hasNameAr := t.cols["name_ar"]
		if hasNameEn || hasNameAr {
			// Current template: EPM KPI (EN) is required, as on the form.
			rec.NameEn, rec.NameAr = t.get(r, "name_en"), t.get(r, "name_ar")
			if rec.NameEn == "" {
				fail("name_en", "EPM KPI (EN) is required.")
			} else if len([]rune(rec.NameEn)) > 255 {
				fail("name_en", "EPM KPI (EN) must be at most 255 characters.")
			}
			if len([]rune(rec.NameAr)) > 255 {
				fail("name_ar", "EPM KPI (AR) must be at most 255 characters.")
			}
		} else {
			name := t.get(r, "name")
			if name == "" {
				fail("name", "KPI name is required.")
			} else if len([]rune(name)) > 255 {
				fail("name", "KPI name must be at most 255 characters.")
			}
			rec.NameEn, rec.NameAr = splitName(name)
		}
		if desc := t.get(r, "description"); hasArabic(desc) {
			rec.DescriptionAr = desc
		} else {
			rec.DescriptionEn = desc
		}
		if v := t.get(r, "description_en"); v != "" {
			rec.DescriptionEn = v
		}
		if v := t.get(r, "description_ar"); v != "" {
			rec.DescriptionAr = v
		}
		rec.Formula = t.get(r, "formula")
		if seg := t.get(r, "seg"); seg != "" {
			if kpiType == models.KPITypeStrategic {
				rec.SegAxes = seg
			} else {
				// Segmentation axes only exist on StrategicKPI; do not drop
				// the cell silently when an older workbook has the column.
				imp.warn(t.sheet, excelRow, id, t.label("seg"),
					fmt.Sprintf("Segmentation Axes is not supported for %s KPIs; the value is ignored.", kpiType))
			}
		}
		// On update only the optional cells that hold a value are written;
		// a blank cell keeps what is stored (see kpiRec.present).
		rec.present = map[string]bool{}
		for _, key := range []string{"classification", "target_requirement", "unit", "owner_type", "owner_org", "award_measure", "related_depts", "criterion", "sub_criterion", "sub_no", "sub_name", "seg", "lifecycle", "notes", "description", "description_en", "description_ar", "name_ar", "domain", "agency", "data_source", "formula", "baseline"} {
			if t.get(r, key) != "" {
				rec.present[key] = true
			}
		}
		if v := t.get(r, "classification"); v != "" && !strings.HasPrefix(normName(v), "select") {
			if c, ok := mapClassification(v); ok {
				rec.Classification = c
			} else {
				fail("classification", fmt.Sprintf("Invalid KPI classification %q — expected KPI, Measurement or Output.", v))
			}
		}
		rec.TargetRequirement = "mandatory"
		if v := t.get(r, "target_requirement"); v != "" && !strings.HasPrefix(normName(v), "select") {
			if tr, ok := mapTargetRequirement(v); ok {
				rec.TargetRequirement = tr
			} else {
				fail("target_requirement", fmt.Sprintf("Invalid target requirement %q — expected Mandatory or Optional.", v))
			}
		}
		rec.UnitOfMeasure = t.get(r, "unit")
		if len([]rune(rec.UnitOfMeasure)) > 50 {
			fail("unit", "Unit of Measure must be at most 50 characters.")
		}
		rec.Notes = t.get(r, "notes")
		rec.Lifecycle = t.get(r, "lifecycle")
		if len([]rune(rec.Lifecycle)) > 100 {
			fail("lifecycle", "KPI Lifecycle must be at most 100 characters.")
		}
		rec.DataSource = t.get(r, "data_source")
		if rec.DataSource != "" {
			rec.DataSource = imp.dataSourceName(rec.DataSource)
		}
		if len([]rune(rec.DataSource)) > 255 {
			fail("data_source", "Data Source must be at most 255 characters.")
		}

		// Enumerations
		if v := t.get(r, "polarity"); v == "" {
			fail("polarity", "Polarity / Direction is required.")
		} else if p, ok := mapPolarity(v); ok {
			rec.Polarity = p
		} else {
			fail("polarity", fmt.Sprintf("Invalid polarity %q — expected Ascending or Descending.", v))
		}
		if v := t.get(r, "activation"); v == "" {
			// Not applied anyway: new KPIs start as Draft.
		} else if status, ok := mapActivation(v); !ok {
			fail("activation", fmt.Sprintf("Invalid activation status %q — expected Active, Inactive, Draft, Reviewed, Approved or Closed.", v))
		} else if status != models.KPIStatusDraft {
			// Activation is workflow-managed: the import must not move a KPI
			// through the workflow. Say so instead of silently ignoring the
			// workbook value.
			imp.warn(t.sheet, excelRow, id, t.label("activation"),
				fmt.Sprintf("Activation Status %q is not applied on import — new KPIs start as Draft and existing KPIs keep their workflow status.", v))
		}
		if v := t.get(r, "frequency"); v == "" {
			fail("frequency", "Reporting Frequency is required.")
		} else if fr, ok := mapFrequency(v); ok {
			rec.Frequency = fr
		} else {
			fail("frequency", fmt.Sprintf("Invalid reporting frequency %q — expected Monthly, Quarterly, Semi-Annual, Annual or Custom.", v))
		}
		ownerTypeRaw := t.get(r, "owner_type")
		if ownerTypeRaw != "" && !strings.HasPrefix(normName(ownerTypeRaw), "select") {
			if ot, ok := mapOwnerType(ownerTypeRaw); ok {
				rec.OwnerType = ot
			} else {
				fail("owner_type", fmt.Sprintf("Invalid owner type %q — expected Internal or External.", ownerTypeRaw))
			}
		}

		// Numbers
		if v := t.get(r, "baseline"); v != "" {
			if b, ok := parseImportNumber(v); ok {
				rec.Baseline = b
			} else {
				fail("baseline", fmt.Sprintf("Baseline %q is not a valid number.", v))
			}
		}

		// Master-data references. As on the KPI forms, the operational
		// objective is required for every KPI type; its parent objective and
		// goal follow from it, and so does the pillar when none is given.
		if pillarName := t.get(r, "pillar"); pillarName != "" {
			if c := imp.sameNameCount(mdPillar, pillarName) + imp.sameNameCount(mdEnabler, pillarName); c > 1 {
				fail("pillar", fmt.Sprintf("Pillar / Enabler %q matches %d records — rename the duplicates in Master Data so the name is unique.", pillarName, c))
			}
			n, fuzzy := imp.find(mdPillar, pillarName, nil)
			if n == nil {
				n, fuzzy = imp.find(mdEnabler, pillarName, nil)
			}
			if n == nil {
				fail("pillar", fmt.Sprintf("Pillar / Enabler %q does not exist in the system or on the Pillars / Enablers sheet.", pillarName))
			} else {
				if fuzzy {
					imp.warn(t.sheet, excelRow, id, t.label("pillar"), fmt.Sprintf("%q matched to %s %q.", pillarName, kindLabel(n.Kind), n.NameEn))
				}
				refFail("pillar", n)
				if n.Kind == mdPillar {
					rec.Pillar = n
				} else {
					imp.warn(t.sheet, excelRow, id, t.label("pillar"),
						fmt.Sprintf("%q is an enabler — KPIs link to a pillar only, so no enabler link is stored.", n.NameEn))
				}
			}
		}

		goalName := t.get(r, "goal")
		var goalNode *mdNode
		if goalName != "" {
			var fuzzy bool
			goalNode, fuzzy = imp.find(mdGoal, goalName, nil)
			if goalNode != nil && fuzzy {
				imp.warn(t.sheet, excelRow, id, t.label("goal"), fmt.Sprintf("%q matched to goal %q.", goalName, goalNode.NameEn))
			}
		}

		// Older operational sheets name the parent objective "Operational
		// Objective" and the operational objective "Related Process Name".
		procKey, parentKey := "objective", "parent_objective"
		if _, old := t.cols["process"]; old {
			procKey, parentKey = "process", "objective"
		}
		procName, parentName := t.get(r, procKey), t.get(r, parentKey)
		var parent *mdNode
		if parentName != "" {
			var fuzzy bool
			if c := imp.sameNameCount(mdObjective, parentName); c > 1 {
				fail(parentKey, fmt.Sprintf("Parent objective %q matches %d records — rename the duplicates in Master Data so the name is unique.", parentName, c))
			} else if parent, fuzzy = imp.find(mdObjective, parentName, nil); parent == nil {
				fail(parentKey, fmt.Sprintf("Parent objective %q does not exist in the system or on the %s sheet.", parentName, kpiTemplateObjectivesSheet))
			} else {
				if fuzzy {
					imp.warn(t.sheet, excelRow, id, t.label(parentKey), fmt.Sprintf("%q matched to parent objective %q.", parentName, parent.NameEn))
				}
				refFail(parentKey, parent)
			}
		}
		switch {
		case procName == "":
			fail(procKey, "Operational Objective is required.")
		case len([]rune(procName)) > 255:
			fail(procKey, "Operational Objective must be at most 255 characters.")
		case parentName != "" && parent == nil:
			// already reported
		default:
			var proc *mdNode
			if parent != nil {
				proc = imp.findExact(mdProcess, procName, parent)
			} else {
				switch matches := imp.findAll(mdProcess, procName); len(matches) {
				case 0:
					fail(procKey, fmt.Sprintf("Operational objective %q does not exist in the system or on the %s sheet. To create it here, also fill Parent Objective.", procName, kpiTemplateProcessesSheet))
				case 1:
					proc = matches[0]
				default:
					parents := make([]string, 0, len(matches))
					for _, m := range matches {
						if m.Objective != nil {
							parents = append(parents, fmt.Sprintf("%q", m.Objective.NameEn))
						}
					}
					fail(procKey, fmt.Sprintf("Operational objective %q exists under %d parent objectives (%s), so its Goal cannot be filled in. Add a \"Parent Objective\" column with the right parent, or rename the duplicates in Master Data.", procName, len(matches), strings.Join(parents, ", ")))
				}
			}
			if proc == nil && parent != nil {
				proc = imp.newNode(mdProcess, procName, t.sheet, excelRow)
				proc.Objective = parent
				imp.warn(t.sheet, excelRow, id, t.label(procKey), fmt.Sprintf("Operational objective %q does not exist under %q and will be created.", procName, parent.NameEn))
			}
			if proc == nil {
				break
			}
			refFail(procKey, proc)
			if proc.Objective != nil && !proc.Rejected {
				refFail(procKey, proc.Objective)
			}
			rec.Process, rec.Objective = proc, proc.Objective
			if rec.Objective != nil {
				rec.Goal = rec.Objective.Goal
			}
			if rec.Pillar == nil {
				rec.Pillar = proc.Pillar
				if rec.Pillar == nil && rec.Objective != nil {
					rec.Pillar = rec.Objective.Pillar
				}
			}
			switch {
			case goalName != "" && goalNode == nil:
				imp.warn(t.sheet, excelRow, id, t.label("goal"), fmt.Sprintf("Goal %q was not found; the goal of the operational objective is used.", goalName))
			case goalNode != nil && rec.Goal != nil && goalNode != rec.Goal:
				imp.warn(t.sheet, excelRow, id, t.label("goal"), fmt.Sprintf("Goal %q does not match the operational objective's goal %q; the objective's goal is used.", goalName, rec.Goal.NameEn))
			case goalNode != nil && rec.Goal == nil:
				rec.Goal = goalNode
			}
		}

		domainName := t.get(r, "domain")
		if domainName != "" {
			if d := imp.findExact(mdDomain, domainName, nil); d != nil {
				rec.Domain = d
			} else if len([]rune(domainName)) > 255 {
				fail("domain", "Related Domain must be at most 255 characters.")
			} else {
				d := imp.newNode(mdDomain, domainName, t.sheet, excelRow)
				d.DomainType = "strategy"
				if strings.HasPrefix(normName(domainName), "criterion") {
					d.DomainType = "award"
				}
				rec.Domain = d
			}
		}

		imp.resolveAwardCriteria(t, r, excelRow, id, rec, domainName, fail)

		if ownerName := t.get(r, "owner_dept"); ownerName == "" {
			fail("owner_dept", "Owner Department is required.")
		} else {
			rec.OwnerDept = imp.department(ownerName, t.sheet, excelRow, id, t.label("owner_dept"))
			if rec.OwnerDept == nil {
				fail("owner_dept", "Owner Department could not be resolved.")
			} else {
				rec.ownerDeptName = rec.OwnerDept.NameEn
			}
		}
		if agency := t.get(r, "agency"); agency != "" {
			rec.Agency = imp.department(agency, t.sheet, excelRow, id, t.label("agency"))
		}
		orgName := t.get(r, "owner_org")
		// As on the KPI form: Internal unless External is chosen. A blank
		// Owner Type with an Owner Organization means External. (The
		// department's own type does not decide it: most departments are
		// marked external, and an External KPI needs an organization.)
		if rec.OwnerType == "" {
			rec.OwnerType = models.KPIOwnerTypeInternal
			if orgName != "" {
				rec.OwnerType = models.KPIOwnerTypeExternal
			}
		}
		if orgName == "" && rec.OwnerType == models.KPIOwnerTypeExternal {
			fail("owner_org", "Owner Organization is required when the Owner Type is External.")
		}
		if orgName != "" {
			switch {
			case rec.OwnerType != models.KPIOwnerTypeExternal:
				imp.warn(t.sheet, excelRow, id, t.label("owner_org"),
					fmt.Sprintf("Owner Organization %q is ignored because the Owner Type is Internal.", orgName))
			case len([]rune(orgName)) > 255:
				fail("owner_org", "Owner Organization must be at most 255 characters.")
			default:
				rec.OwnerOrg = imp.findExact(mdOrganization, orgName, nil)
				if rec.OwnerOrg == nil {
					rec.OwnerOrg = imp.newNode(mdOrganization, orgName, t.sheet, excelRow)
					imp.warn(t.sheet, excelRow, id, t.label("owner_org"), fmt.Sprintf("Organization %q does not exist and will be created.", orgName))
				}
			}
		}
		if v := t.get(r, "award_measure"); v != "" {
			rec.AwardMeasure = imp.awardMeasure(v)
			switch {
			case rec.AwardMeasure != nil:
			case len([]rune(v)) > 255:
				fail("award_measure", "Award KPIs and Measures must be at most 255 characters.")
			case awardMeasureCodeRe.MatchString(strings.ToUpper(v)):
				fail("award_measure", fmt.Sprintf("Award KPI measure %q does not exist.", v))
			default:
				rec.AwardMeasure = imp.newNode(mdAwardMeasure, v, t.sheet, excelRow)
				imp.warn(t.sheet, excelRow, id, t.label("award_measure"), fmt.Sprintf("Award KPI measure %q does not exist and will be created.", v))
			}
		}
		// Related departments link existing departments only (including
		// ones this workbook creates as owners): the former free-text
		// Related Administrative Units column holds labels such as "All
		// Service Owners" that must not become departments.
		seen := map[*mdNode]bool{}
		for _, name := range splitList(t.get(r, "related_depts")) {
			d := imp.findExact(mdDepartment, name, nil)
			switch {
			case d == nil:
				imp.warn(t.sheet, excelRow, id, t.label("related_depts"),
					fmt.Sprintf("Department %q was not found and is not linked. Add it under Departments, then import again with \"update\".", name))
			case !seen[d]:
				seen[d] = true
				rec.RelatedDepts = append(rec.RelatedDepts, d)
			}
		}

		// Annual targets
		years := make([]int, 0, len(t.years))
		for y := range t.years {
			years = append(years, y)
		}
		sort.Ints(years)
		for _, y := range years {
			raw := strings.TrimSpace(cell(r, t.years[y]))
			if raw == "" {
				continue
			}
			tr := &kpiTargetRec{Year: y, Row: excelRow, Field: strings.TrimSpace(cell(t.rows[t.headerRow], t.years[y]))}
			v, ok := parseImportNumber(raw)
			switch {
			case y < 2020 || y > 2040:
				imp.errorf(t.sheet, excelRow, id, tr.Field, fmt.Sprintf("Target year %d is outside the permitted range (2020–2040).", y))
				tr.Rejected, tr.RejectWhy = true, "invalid target"
			case !ok:
				imp.errorf(t.sheet, excelRow, id, tr.Field, fmt.Sprintf("Target %q is not a valid number.", raw))
				tr.Rejected, tr.RejectWhy = true, "invalid target"
			case v < 0:
				imp.errorf(t.sheet, excelRow, id, tr.Field, fmt.Sprintf("Target %v must not be negative.", v))
				tr.Rejected, tr.RejectWhy = true, "invalid target"
			}
			tr.Value = v
			if eid, ok := imp.existingTargets[fmt.Sprintf("%s|%s|%d", kpiType, code, y)]; ok {
				tr.ExistingID = &eid
			}
			rec.Targets = append(rec.Targets, tr)
		}

		imp.kpis = append(imp.kpis, rec)
	}
}

// resolveAwardCriteria links the KPI to an award sub-criterion. As on the KPI
// forms both columns are optional and apply to every KPI type: Criteria
// ("Criterion 5 - Name") narrows the choice, Sub-Criteria ("5.1 - Name")
// is what is stored. Older award sheets give the number and, for a new
// sub-criterion, its name in separate columns.
func (imp *kpiImporter) resolveAwardCriteria(t *sheetTable, r []string, excelRow int, id string, rec *kpiRec, domainName string, fail func(key, msg string)) {
	critRaw := t.get(r, "criterion")
	subKey, subNo, subName := "sub_criterion", "", ""
	if v := t.get(r, "sub_criterion"); v != "" {
		// A value picked from the template's list ("<number> - <name>") is
		// an existing sub-criterion, whatever its number looks like (older
		// records may have a plain "1" instead of "1.1").
		if n := imp.subCriterionByLabel(v); n != nil {
			if critRaw != "" {
				critNo, ok := parseCriterionNo(critRaw)
				if !ok {
					fail("criterion", fmt.Sprintf("Criteria %q is not valid — pick one from the list, e.g. \"Criterion 5 - Name\".", critRaw))
					return
				}
				if n.Criterion != nil && critNo != n.Criterion.CriterionNo {
					fail("sub_criterion", fmt.Sprintf("Sub-Criteria %q does not belong to Criterion %d.", v, critNo))
					return
				}
			}
			rec.SubCriterion = n
			return
		}
		m := subCriterionLabelRe.FindStringSubmatch(v)
		if m == nil {
			fail("sub_criterion", fmt.Sprintf("Sub-Criteria %q is not valid — pick one from the list, e.g. \"5.1 - Name\".", v))
			return
		}
		subNo, subName = m[1], strings.TrimSpace(m[2])
	} else if v := strings.TrimSuffix(t.get(r, "sub_no"), ".0"); v != "" {
		subKey, subNo, subName = "sub_no", v, t.get(r, "sub_name")
		if !regexp.MustCompile(`^\d+(\.\d+)*$`).MatchString(subNo) {
			fail("sub_no", fmt.Sprintf("Sub-Criterion # %q is not valid — expected e.g. \"5.1\".", subNo))
			return
		}
	}
	if critRaw == "" && subNo == "" {
		return
	}
	critNo, ok := parseCriterionNo(critRaw)
	if critRaw == "" {
		critNo, ok = parseCriterionNo(subNo) // "5.1" belongs to criterion 5
	}
	if !ok {
		fail("criterion", fmt.Sprintf("Criteria %q is not valid — pick one from the list, e.g. \"Criterion 5 - Name\".", critRaw))
		return
	}
	var crit *mdNode
	for _, n := range imp.md[mdCriterion] {
		if n.CriterionNo == critNo {
			crit = n
			break
		}
	}
	if subNo == "" {
		imp.warn(t.sheet, excelRow, id, t.label("criterion"),
			"Only a sub-criterion is linked to a KPI — pick Sub-Criteria too; the criterion alone is not stored.")
		return
	}
	if !strings.HasPrefix(subNo, strconv.Itoa(critNo)+".") {
		fail(subKey, fmt.Sprintf("Sub-Criteria %s does not belong to Criterion %d.", subNo, critNo))
		return
	}
	if crit == nil {
		name := fmt.Sprintf("Criterion %d", critNo)
		if _, label, found := strings.Cut(critRaw, " - "); found && strings.TrimSpace(label) != "" {
			name = strings.TrimSpace(label)
		} else if d := normName(domainName); strings.HasPrefix(d, normName(name)+":") {
			name = strings.TrimSpace(domainName[strings.Index(domainName, ":")+1:])
		}
		// Award criteria are unique by number and by name.
		for _, n := range imp.md[mdCriterion] {
			if normName(n.NameEn) == normName(name) || (n.NameAr != "" && normName(n.NameAr) == normName(name)) {
				fail("criterion", fmt.Sprintf("An award criterion named %q already exists (Criterion %d). Pick that criterion, or give Criterion %d another name.", name, n.CriterionNo, critNo))
				return
			}
		}
		crit = imp.newNode(mdCriterion, name, t.sheet, excelRow)
		crit.CriterionNo = critNo
	}
	for _, n := range imp.md[mdSubCriterion] {
		if n.Criterion == crit && n.SubNo == subNo {
			rec.SubCriterion = n
			return
		}
	}
	if subName == "" {
		fail(subKey, fmt.Sprintf("Sub-Criterion %s does not exist yet, so its name is required.", subNo))
		return
	}
	// Sub-criteria are unique by number and by name within their criterion.
	for _, n := range imp.md[mdSubCriterion] {
		if n.Criterion == crit && (normName(n.NameEn) == normName(subName) || (n.NameAr != "" && normName(n.NameAr) == normName(subName))) {
			fail(subKey, fmt.Sprintf("A sub-criterion named %q already exists under Criterion %d (No. %s).", subName, critNo, n.SubNo))
			return
		}
	}
	sub := imp.newNode(mdSubCriterion, subName, t.sheet, excelRow)
	sub.SubNo = subNo
	sub.Criterion = crit
	rec.SubCriterion = sub
}

// subCriterionByLabel finds the existing sub-criterion whose dropdown label
// ("<sub no> - <English or Arabic name>", as the template lists it) matches.
func (imp *kpiImporter) subCriterionByLabel(v string) *mdNode {
	want := normName(v)
	for _, n := range imp.md[mdSubCriterion] {
		if n.SubNo == "" {
			continue
		}
		for _, name := range []string{n.NameEn, n.NameAr} {
			if name != "" && normName(n.SubNo+" - "+name) == want {
				return n
			}
		}
	}
	return nil
}

// subCriterionLabelRe splits a Sub-Criteria value "5.1 - Name" (the name is
// optional) into number and name.
var subCriterionLabelRe = regexp.MustCompile(`^\s*(\d+(?:\.\d+)+)\s*(?:[-–—:]\s*(.*))?$`)

// ─── Performance sheets ─────────────────────────────────────────────────────

func perfRowColumns() []colSpec {
	return []colSpec{
		{"code", hdrAny("code", "الكود"), true},
		{"metric_code", hdrAny("metric code", "كود المقياس"), false},
		{"year", hdrAny("year", "السنة"), true},
		// Period (current template) or Quarter (older templates); one of
		// them is checked per row.
		{"period", hdrAny("period", "الفترة"), false},
		{"quarter", hdrAny("quarter", "الربع"), false},
		{"target", hdrAny("target", "المستهدف"), false},
		{"actual", hdrAny("actual result", "النتيجة الفعلية"), true},
		{"achievement", hdrAny("achievement", "نسبة الإنجاز"), false},
		{"name", hdrAny("indicator", "المؤشر"), false},
	}
}

// perfKpi returns the KPI a performance row belongs to: one on a Dictionary
// sheet, or a stub for one that only exists in the system.
func (imp *kpiImporter) perfKpi(kpiType, code string) *kpiRec {
	if kpi := imp.kpiByTypeCode[kpiType+"|"+code]; kpi != nil {
		return kpi
	}
	ek, ok := imp.existingKpis[kpiType+"|"+code]
	if !ok {
		return nil
	}
	eid := ek.ID
	kpi := &kpiRec{Type: kpiType, Code: ek.Code, NameEn: ek.NameEn, DBOnly: true, ID: ek.ID, ExistingID: &eid,
		MetricID: ek.MetricID, Polarity: ek.Polarity, Frequency: ek.Frequency, Baseline: ek.Baseline, DataSource: "", ownerDeptName: ek.OwnerDeptName}
	imp.kpiByTypeCode[kpiType+"|"+code] = kpi
	imp.kpis = append(imp.kpis, kpi)
	return kpi
}

// addPerf validates one period's actual result (and optional target and
// achievement) and adds it to the plan. metric is the KPI metric it is for
// (nil = the KPI's first metric or the default one). labels are the target,
// actual and achievement column headers.
func (imp *kpiImporter) addPerf(kpi *kpiRec, metric *models.KpiMetric, metricGiven bool, sheet string, excelRow, year int, periodCode, actualRaw, targetRaw, achRaw string, labels [3]string) {
	rec := &kpiPerfRec{Kpi: kpi, Metric: metric, MetricGiven: metricGiven, Sheet: sheet, Row: excelRow, Year: year, PeriodCode: periodCode}
	rid := perfRecordID(rec)
	reject := func(field, msg string) {
		imp.errorf(sheet, excelRow, rid, field, msg)
		rec.Rejected, rec.RejectWhy = true, "invalid performance value"
	}
	if year < 2020 || year > 2040 {
		reject(labels[1], fmt.Sprintf("Year %d is outside the permitted range (2020–2040).", year))
	}
	if v, ok := parseImportNumber(actualRaw); !ok {
		reject(labels[1], fmt.Sprintf("Actual result %q is not a valid number.", actualRaw))
	} else if v < 0 {
		reject(labels[1], fmt.Sprintf("Actual result %v must not be negative.", v))
	} else {
		rec.Actual = v
	}
	if targetRaw != "" {
		if v, ok := parseImportNumber(targetRaw); !ok {
			reject(labels[0], fmt.Sprintf("Target %q is not a valid number.", targetRaw))
		} else if v < 0 {
			reject(labels[0], fmt.Sprintf("Target %v must not be negative.", v))
		} else {
			rec.Target = &v
		}
	}
	if rec.Target == nil && !rec.Rejected {
		imp.warn(sheet, excelRow, rid, labels[1], "Actual result has no target for this period — achievement cannot be calculated.")
	}
	// Same rules as entering a result on screen: the period must match the
	// metric's (else the KPI's) reporting frequency, lie in the metric's
	// validity window and have ended.
	if !rec.Rejected {
		check := metric
		if check == nil {
			check = &models.KpiMetric{Name: kpi.NameEn}
			if kpi.MetricID != nil {
				for _, m := range imp.kpiMetrics(kpi) {
					if m.ID == *kpi.MetricID {
						check = m
					}
				}
			}
		}
		if err := ValidateEntryPeriod(check, kpi.Frequency, year, periodCode, imp.now); err != nil {
			reject(labels[1], err.Error()+".")
		}
	}
	// The workbook's own Achievement value is informational — the system
	// recalculates it; flag noticeable disagreements.
	if !rec.Rejected && rec.Target != nil && *rec.Target > 0 && achRaw != "" && kpi.Polarity != models.KPIPolarityDescending {
		if sheetAch, ok := parseImportNumber(achRaw); ok {
			calc := rec.Actual / *rec.Target
			if math.Abs(sheetAch-calc) > 0.01 && math.Abs(sheetAch-calc*100) > 1 {
				imp.warn(sheet, excelRow, rid, labels[2],
					fmt.Sprintf("Workbook achievement %v differs from the calculated value %.4f; the calculated value is used.", sheetAch, calc))
			}
		}
	}
	imp.perfs = append(imp.perfs, rec)
}

// perfRecordID labels a performance record in the report, e.g.
// "KPI-01 2026 Q1", "KPI-01 2026 JAN" or "KPI-01 / M2 2026 H1".
func perfRecordID(p *kpiPerfRec) string {
	code := p.Kpi.Code
	if p.MetricGiven && p.Metric != nil && p.Metric.MetricCode != "" {
		code += " / " + p.Metric.MetricCode
	}
	return fmt.Sprintf("%s %d %s", code, p.Year, strings.ToUpper(p.PeriodCode))
}

// kpiMetrics returns the KPI's existing metrics (display order).
func (imp *kpiImporter) kpiMetrics(kpi *kpiRec) []*models.KpiMetric {
	if ek := imp.existingKpis[kpi.Type+"|"+strings.ToUpper(kpi.Code)]; ek != nil {
		return ek.Metrics
	}
	return nil
}

// perfPeriodAliases maps the Period column's English and Arabic labels to
// period codes.
var perfPeriodAliases = func() map[string]string {
	m := map[string]string{"annual": "annual", "annually": "annual", "yearly": "annual", "year": "annual", "سنوي": "annual", "سنة": "annual"}
	arMonths := []string{"يناير", "فبراير", "مارس", "أبريل", "مايو", "يونيو", "يوليو", "أغسطس", "سبتمبر", "أكتوبر", "نوفمبر", "ديسمبر"}
	enMonths := []string{"january", "february", "march", "april", "may", "june", "july", "august", "september", "october", "november", "december"}
	for i, c := range monthPeriodCodes {
		m[c], m[enMonths[i]], m[arMonths[i]] = c, c, c
	}
	m["sept"] = "sep"
	m["ابريل"], m["اغسطس"], m["اكتوبر"] = "apr", "aug", "oct"
	arOrd := []string{"الأول", "الثاني", "الثالث", "الرابع"}
	for q := 1; q <= 4; q++ {
		c := fmt.Sprintf("q%d", q)
		m[c], m[fmt.Sprintf("الربع %d", q)], m["الربع "+arOrd[q-1]] = c, c, c
	}
	for h := 1; h <= 2; h++ {
		c := fmt.Sprintf("h%d", h)
		m[c], m[fmt.Sprintf("النصف %d", h)], m["النصف "+arOrd[h-1]] = c, c, c
	}
	return m
}()

// parsePerfPeriod reads the Period column: Jan–Dec, Q1–Q4, H1/H2 or Annual
// (English or Arabic), or a year-embedded code such as 2026-07 or 2026-Q3.
func parsePerfPeriod(raw string, year int) (string, bool) {
	v := strings.ToLower(multiSpaceRe.ReplaceAllString(strings.TrimSpace(raw), " "))
	if c, ok := perfPeriodAliases[v]; ok {
		return c, true
	}
	if c, y, err := CanonicalTargetPeriodCode(v, year); err == nil && !IsCustomPeriodCode(c) && (year == 0 || y == year) {
		return c, true
	}
	return "", false
}

// parsePerformanceRows reads the Performance sheet: one row per KPI, year
// and quarter. The KPI type comes from the code.
func (imp *kpiImporter) parsePerformanceRows(t *sheetTable) {
	if t == nil || t.headerRow < 0 {
		return
	}
	labels := [3]string{t.label("target"), t.label("actual"), t.label("achievement")}
	seen := map[string]int{}
	for i := t.headerRow + 1; i < len(t.rows); i++ {
		r := t.rows[i]
		if t.blank(r) {
			continue
		}
		excelRow := i + 1
		code := strings.ToUpper(t.get(r, "code"))
		periodKey, periodRaw := "period", t.get(r, "period")
		if periodRaw == "" && t.get(r, "quarter") != "" {
			periodKey, periodRaw = "quarter", t.get(r, "quarter")
		}
		yearRaw := t.get(r, "year")
		actualRaw, targetRaw := t.get(r, "actual"), t.get(r, "target")
		id := strings.TrimSpace(fmt.Sprintf("%s %s %s", code, yearRaw, periodRaw))

		if actualRaw == "" {
			if targetRaw != "" || t.get(r, "achievement") != "" {
				imp.warn(t.sheet, excelRow, id, labels[1],
					"Target/achievement is filled but Actual Result is empty — the row is not imported. Period targets on this sheet are only stored together with an actual result.")
			}
			continue
		}
		bad := false
		if code == "" {
			imp.errorf(t.sheet, excelRow, id, t.label("code"), "KPI code is required.")
			bad = true
		}
		year, err := strconv.Atoi(strings.TrimSuffix(yearRaw, ".0"))
		if err != nil {
			imp.errorf(t.sheet, excelRow, id, t.label("year"), fmt.Sprintf("Year %q is not valid — enter a year such as 2025.", yearRaw))
			bad = true
		}
		var periodCode string
		switch {
		case periodRaw == "":
			imp.errorf(t.sheet, excelRow, id, t.label("period"), "Period is required — choose a month, quarter, half-year or Annual.")
			bad = true
		case periodKey == "quarter":
			if q, ok := parseQuarter(periodRaw); ok {
				periodCode = fmt.Sprintf("q%d", q)
			} else {
				imp.errorf(t.sheet, excelRow, id, t.label("quarter"), fmt.Sprintf("Quarter %q is not valid — choose Q1, Q2, Q3 or Q4.", periodRaw))
				bad = true
			}
		default:
			c, ok := parsePerfPeriod(periodRaw, year)
			if !ok {
				imp.errorf(t.sheet, excelRow, id, t.label("period"), fmt.Sprintf("Period %q is not valid — choose Jan–Dec, Q1–Q4, H1, H2 or Annual.", periodRaw))
				bad = true
			}
			periodCode = c
		}
		if bad {
			continue
		}

		kpiType := imp.existingCodeTyp[code]
		if rec := imp.kpiByCode[code]; rec != nil {
			kpiType = rec.Type
		}
		var kpi *kpiRec
		if kpiType != "" {
			kpi = imp.perfKpi(kpiType, code)
		}
		if kpi == nil {
			imp.errorf(t.sheet, excelRow, id, t.label("code"), fmt.Sprintf("KPI %q is not defined on a Dictionary sheet or in the system.", code))
			continue
		}
		if name := t.get(r, "name"); name != "" && kpi.NameEn != "" && normName(name) != normName(kpi.NameEn) && normName(name) != normName(kpi.NameAr) {
			imp.warn(t.sheet, excelRow, id, t.label("name"), fmt.Sprintf("Indicator name %q differs from the dictionary name %q.", name, kpi.NameEn))
		}

		// Which metric: the Metric Code column, else the KPI's only metric.
		var metric *models.KpiMetric
		metrics := imp.kpiMetrics(kpi)
		mc := t.get(r, "metric_code")
		if mc != "" {
			// A metric code wins over another metric's name.
			for _, m := range metrics {
				if strings.EqualFold(strings.TrimSpace(m.MetricCode), mc) {
					metric = m
					break
				}
			}
			for _, m := range metrics {
				if metric == nil && normName(m.Name) == normName(mc) {
					metric = m
				}
			}
			if metric == nil {
				imp.errorf(t.sheet, excelRow, id, t.label("metric_code"), fmt.Sprintf("KPI %s has no metric with code or name %q (on the Metrics sheet or in the system).", kpi.Code, mc))
				continue
			}
		} else if len(metrics) > 1 {
			var codes []string
			for _, m := range metrics {
				if c := strings.TrimSpace(m.MetricCode); c != "" {
					codes = append(codes, c)
				} else {
					codes = append(codes, m.Name) // a metric without a code is named instead
				}
			}
			imp.errorf(t.sheet, excelRow, id, t.label("code"), fmt.Sprintf("KPI %s has %d metrics — fill Metric Code with one of: %s.", kpi.Code, len(metrics), strings.Join(codes, ", ")))
			continue
		} else if len(metrics) == 1 {
			metric = metrics[0]
		}

		rid := perfRecordID(&kpiPerfRec{Kpi: kpi, Metric: metric, MetricGiven: mc != "", Year: year, PeriodCode: periodCode})
		if prev, dup := seen[rid]; dup {
			imp.errorf(t.sheet, excelRow, rid, t.label("code"), fmt.Sprintf("Duplicate row for this KPI, metric and period — already on row %d.", prev))
			continue
		}
		seen[rid] = excelRow
		imp.addPerf(kpi, metric, mc != "", t.sheet, excelRow, year, periodCode, actualRaw, targetRaw, t.get(r, "achievement"), labels)
	}
}

// parseQuarter accepts Q1–Q4, 1–4 and the Arabic "الربع 1".
func parseQuarter(s string) (int, bool) {
	v := strings.TrimSuffix(strings.TrimSpace(strings.ToUpper(s)), ".0")
	v = strings.TrimSpace(strings.TrimPrefix(strings.TrimPrefix(v, "الربع"), "Q"))
	q, err := strconv.Atoi(v)
	return q, err == nil && q >= 1 && q <= 4
}

// parsePerformance reads an older wide Performance sheet (one per KPI type):
// one row per KPI with Target / Actual Result / Achievement columns for
// every quarter.
func (imp *kpiImporter) parsePerformance(kpiType string, t *sheetTable) {
	if t == nil || t.headerRow < 0 {
		return
	}
	keys := make([]string, 0, len(t.quarters))
	for k := range t.quarters {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	seen := map[string]int{}

	for i := t.headerRow + 1; i < len(t.rows); i++ {
		r := t.rows[i]
		if t.blank(r) {
			continue
		}
		excelRow := i + 1
		code := strings.ToUpper(t.get(r, "code"))
		name := t.get(r, "name")
		id := code
		if id == "" {
			id = name
		}
		hasActual := false
		hasPlanned := false
		for _, k := range keys {
			idx := t.quarters[k]
			if idx[1] >= 0 && strings.TrimSpace(cell(r, idx[1])) != "" {
				hasActual = true
			}
			if (idx[0] >= 0 && strings.TrimSpace(cell(r, idx[0])) != "") ||
				(idx[2] >= 0 && strings.TrimSpace(cell(r, idx[2])) != "") {
				hasPlanned = true
			}
		}
		if !hasActual {
			if hasPlanned {
				imp.warn(t.sheet, excelRow, id, "Actual Result",
					"Quarterly target/achievement is filled but Actual Result is empty — the row is not imported. Quarterly targets are only stored together with an actual result.")
			}
			continue // nothing to import on this row
		}
		if code == "" {
			imp.errorf(t.sheet, excelRow, id, t.label("code"), "KPI code is required.")
			continue
		}
		if prev, dup := seen[code]; dup {
			imp.errorf(t.sheet, excelRow, code, t.label("code"), fmt.Sprintf("Duplicate performance row for this KPI — already on row %d.", prev))
			continue
		}
		seen[code] = excelRow

		kpi := imp.perfKpi(kpiType, code)
		if kpi == nil {
			msg := fmt.Sprintf("KPI %q is not defined on the %s dictionary sheet or in the system.", code, kpiType)
			if other := imp.kpiByCode[code]; other != nil {
				msg = fmt.Sprintf("KPI %q is a %s KPI (sheet %q), not %s.", code, other.Type, other.Sheet, kpiType)
			}
			imp.errorf(t.sheet, excelRow, code, t.label("code"), msg)
			continue
		}
		if name != "" && kpi.NameEn != "" && normName(name) != normName(kpi.NameEn) && normName(name) != normName(kpi.NameAr) {
			imp.warn(t.sheet, excelRow, code, t.label("name"), fmt.Sprintf("Indicator name %q differs from the dictionary name %q.", name, kpi.NameEn))
		}
		if ownerRaw := t.get(r, "owner_dept"); ownerRaw != "" {
			expected := kpi.ownerDeptName
			if kpi.OwnerDept != nil {
				expected = kpi.OwnerDept.NameEn
			}
			if expected != "" && normName(ownerRaw) != normName(expected) {
				imp.warn(t.sheet, excelRow, code, t.label("owner_dept"),
					fmt.Sprintf("Owner Department %q differs from the KPI's owner department %q; the KPI's owner is used.", ownerRaw, expected))
			}
		}

		for _, k := range keys {
			idx, lbl := t.quarters[k], t.qLabels[k]
			at := func(pos int) string {
				if idx[pos] < 0 {
					return ""
				}
				return strings.TrimSpace(cell(r, idx[pos]))
			}
			actualRaw := at(1)
			if actualRaw == "" {
				continue
			}
			parts := strings.SplitN(k, "-", 2)
			year, _ := strconv.Atoi(parts[0])
			q, _ := strconv.Atoi(parts[1])
			imp.addPerf(kpi, nil, false, t.sheet, excelRow, year, fmt.Sprintf("q%d", q), actualRaw, at(0), at(2), lbl)
		}
	}
}

// ─── Plan resolution ────────────────────────────────────────────────────────

func (imp *kpiImporter) resolveMetricsAndExisting() {
	for _, p := range imp.perfs {
		metricID := p.Kpi.MetricID
		if p.Metric != nil {
			metricID = &p.Metric.ID
		}
		if p.Kpi.ExistingID != nil && metricID != nil {
			key := fmt.Sprintf("%s|%s|%s|%d|%s", *p.Kpi.ExistingID, p.Kpi.Type, *metricID, p.Year, p.PeriodCode)
			if eid, ok := imp.existingEntries[key]; ok {
				p.ExistingID = &eid
			}
		}
	}
}

// finalize (re)computes every record's action, the preview items and the
// counts from the current plan state.
func (imp *kpiImporter) finalize() {
	res := imp.res
	existingAction := kpiImportActionSkip
	if imp.opts.ExistingMode == KpiImportExistingUpdate {
		existingAction = kpiImportActionUpdate
	}

	// KPI-level rejection cascades to its targets and performance records.
	for _, k := range imp.kpis {
		switch {
		case k.DBOnly, imp.mode == kpiImportValues && !k.Rejected:
			k.action = ""
		case k.Rejected:
			k.action = kpiImportActionReject
		case k.ExistingID != nil:
			k.action = existingAction
		default:
			k.action = kpiImportActionCreate
		}
		for _, t := range k.Targets {
			switch {
			case k.Rejected:
				t.action, t.RejectWhy = kpiImportActionReject, "KPI definition rejected"
			case t.Rejected:
				t.action = kpiImportActionReject
			case t.ExistingID != nil:
				t.action = existingAction
			default:
				t.action = kpiImportActionCreate
			}
		}
	}
	metricNeeded := map[*kpiRec]bool{}
	for _, p := range imp.perfs {
		switch {
		case p.Kpi.Rejected:
			p.action, p.RejectWhy = kpiImportActionReject, "KPI definition rejected"
		case p.Rejected:
			p.action = kpiImportActionReject
		case p.ExistingID != nil:
			p.action = existingAction
		default:
			p.action = kpiImportActionCreate
		}
		if (p.action == kpiImportActionCreate || p.action == kpiImportActionUpdate) && p.Metric == nil {
			metricNeeded[p.Kpi] = true
		}
	}
	for _, k := range imp.kpis {
		for _, t := range k.Targets {
			if t.action == kpiImportActionCreate || t.action == kpiImportActionUpdate {
				metricNeeded[k] = true
			}
		}
		k.newMetric = metricNeeded[k] && k.MetricID == nil
	}

	// Master data is created only if it is declared on the Master Data sheet
	// or referenced by a KPI that will actually be written.
	used := map[*mdNode]bool{}
	var mark func(n *mdNode)
	mark = func(n *mdNode) {
		if n == nil || used[n] {
			return
		}
		used[n] = true
		for _, dep := range []*mdNode{n.Pillar, n.Enabler, n.Goal, n.Objective, n.Owner, n.Criterion} {
			mark(dep)
		}
	}
	for _, k := range imp.kpis {
		if k.action == kpiImportActionCreate || k.action == kpiImportActionUpdate {
			for _, n := range append([]*mdNode{k.Pillar, k.Goal, k.Objective, k.Process, k.Domain, k.SubCriterion, k.OwnerDept, k.Agency, k.OwnerOrg, k.AwardMeasure}, k.RelatedDepts...) {
				mark(n)
			}
		}
	}
	for _, kind := range []string{mdPillar, mdEnabler, mdGoal, mdObjective, mdProcess, mdInitiative, mdDepartment} {
		for _, n := range imp.md[kind] {
			if n.FromSheet && !n.Exists && !n.Rejected {
				mark(n)
			}
		}
	}
	// The values pass writes no master data: the definitions pass did.
	if imp.mode == kpiImportValues {
		used = map[*mdNode]bool{}
	}
	// A new node depending on a rejected node is rejected too.
	for changed := true; changed; {
		changed = false
		for _, nodes := range imp.md {
			for _, n := range nodes {
				if n.Exists || n.Rejected {
					continue
				}
				for _, dep := range []*mdNode{n.Pillar, n.Enabler, n.Goal, n.Objective, n.Owner, n.Criterion} {
					if dep != nil && dep.Rejected {
						n.Rejected, n.RejectWhy = true, fmt.Sprintf("depends on rejected %s %q", kindLabel(dep.Kind), dep.NameEn)
						changed = true
						break
					}
				}
			}
		}
	}

	// Items
	items := []KpiImportItem{}
	for _, kind := range KpiImportCategories {
		if imp.mode == kpiImportValues {
			break // reported by the definitions pass
		}
		for _, n := range imp.md[kind] {
			var action, reason string
			n.action = ""
			switch {
			case n.Rejected:
				action, reason = kpiImportActionReject, n.RejectWhy
			case n.Exists && n.FromSheet:
				action, reason = kpiImportActionSkip, "already exists"
			case !n.Exists && used[n]:
				action = kpiImportActionCreate
			default:
				continue
			}
			n.action = action
			rid := n.NameEn
			if n.Kind == mdSubCriterion {
				rid = n.SubNo
			} else if n.Kind == mdCriterion {
				rid = fmt.Sprintf("Criterion %d", n.CriterionNo)
			}
			items = append(items, KpiImportItem{Category: kind, RecordID: rid, Name: n.NameEn, Sheet: n.Sheet, Row: n.Row, Action: action, Reason: reason})
		}
	}
	for _, k := range imp.kpis {
		if k.DBOnly || imp.mode == kpiImportValues {
			continue
		}
		reason := k.RejectWhy
		if k.action == kpiImportActionSkip {
			reason = "already exists"
		}
		items = append(items, KpiImportItem{Category: "kpi", KpiType: k.Type, RecordID: k.Code, Name: k.NameEn, Sheet: k.Sheet, Row: k.Row, Action: k.action, Reason: reason})
	}
	for _, k := range imp.kpis {
		if k.newMetric {
			items = append(items, KpiImportItem{Category: "metric", KpiType: k.Type, RecordID: k.Code, Name: k.NameEn, Sheet: k.Sheet, Row: k.Row, Action: kpiImportActionCreate, Reason: "default metric for imported targets/performance"})
		}
	}
	for _, k := range imp.kpis {
		for _, t := range k.Targets {
			reason := t.RejectWhy
			if t.action == kpiImportActionSkip {
				reason = "already exists"
			}
			items = append(items, KpiImportItem{Category: "annual_target", KpiType: k.Type, RecordID: fmt.Sprintf("%s %d", k.Code, t.Year),
				Name: fmt.Sprintf("%s — %d target %v", k.NameEn, t.Year, t.Value), Sheet: k.Sheet, Row: t.Row, Action: t.action, Reason: reason})
		}
	}
	for _, p := range imp.perfs {
		reason := p.RejectWhy
		if p.action == kpiImportActionSkip {
			reason = "already exists"
		}
		tgt := "—"
		if p.Target != nil {
			tgt = strconv.FormatFloat(*p.Target, 'f', -1, 64)
		}
		items = append(items, KpiImportItem{Category: "performance", KpiType: p.Kpi.Type, RecordID: perfRecordID(p),
			Name: fmt.Sprintf("%s — target %s, actual %v", p.Kpi.NameEn, tgt, p.Actual), Sheet: p.Sheet, Row: p.Row, Action: p.action, Reason: reason})
	}
	res.Items = items
	imp.count()

	actionable := res.Totals.Created + res.Totals.Updated
	res.CanCommit = !imp.structuralError && actionable > 0 &&
		!(imp.opts.ErrorMode == KpiImportErrorsAbort && len(res.Errors) > 0)
}

func (imp *kpiImporter) count() {
	res := imp.res
	res.Categories = map[string]*KpiImportCounts{}
	for _, c := range KpiImportCategories {
		res.Categories[c] = &KpiImportCounts{}
	}
	res.Totals = KpiImportCounts{}
	for _, it := range res.Items {
		c := res.Categories[it.Category]
		switch it.Action {
		case kpiImportActionCreate:
			c.Created++
			res.Totals.Created++
		case kpiImportActionUpdate:
			c.Updated++
			res.Totals.Updated++
		case kpiImportActionSkip:
			c.Skipped++
			res.Totals.Skipped++
		case kpiImportActionReject:
			c.Rejected++
			res.Totals.Rejected++
		case "rolled_back":
			c.RolledBack++
			res.Totals.RolledBack++
		}
	}
}

func (imp *kpiImporter) markRolledBack() {
	for i := range imp.res.Items {
		it := &imp.res.Items[i]
		if it.Action == kpiImportActionCreate || it.Action == kpiImportActionUpdate {
			it.Action = "rolled_back"
			it.Reason = "transaction rolled back"
		}
	}
	imp.count()
	imp.res.CanCommit = false
}

// ─── Value helpers ──────────────────────────────────────────────────────────

var multiSpaceRe = regexp.MustCompile(`\s+`)

// inputHeaderText strips the (*) marker and newlines for error messages.
func inputHeaderText(s string) string {
	s = strings.TrimSpace(strings.ReplaceAll(s, "\n", " "))
	return strings.TrimSpace(strings.TrimRight(s, "*"))
}

func normHeader(s string) string {
	s = strings.ToLower(strings.TrimSpace(multiSpaceRe.ReplaceAllString(s, " ")))
	// A trailing ":" or (*) marker is decoration — strip before matching.
	s = strings.TrimSuffix(s, ":")
	s = strings.TrimRight(s, "*")
	return strings.TrimSpace(s)
}

func normName(s string) string {
	s = strings.ToLower(strings.TrimSpace(multiSpaceRe.ReplaceAllString(s, " ")))
	return strings.TrimRight(s, ".")
}

func cell(row []string, j int) string {
	if j < 0 || j >= len(row) {
		return ""
	}
	return row[j]
}

func hasArabic(s string) bool {
	for _, r := range s {
		if unicode.Is(unicode.Arabic, r) {
			return true
		}
	}
	return false
}

// splitName stores Arabic-only names in both columns (name_en is NOT NULL
// and is what most screens display).
func splitName(s string) (en, ar string) {
	s = strings.TrimSpace(s)
	if hasArabic(s) {
		return s, s
	}
	return s, ""
}

func parseImportNumber(s string) (float64, bool) {
	s = strings.TrimSpace(strings.ReplaceAll(s, ",", ""))
	pct := strings.HasSuffix(s, "%")
	s = strings.TrimSpace(strings.TrimSuffix(s, "%"))
	v, err := strconv.ParseFloat(s, 64)
	if err != nil || math.IsNaN(v) || math.IsInf(v, 0) {
		return 0, false
	}
	if pct {
		v /= 100
	}
	return v, true
}

func parseCriterionNo(s string) (int, bool) {
	m := regexp.MustCompile(`(\d+)`).FindString(s)
	if m == "" {
		return 0, false
	}
	n, err := strconv.Atoi(m)
	return n, err == nil && n > 0
}

func mapPolarity(s string) (string, bool) {
	switch normName(s) {
	case "ascending", "higher is better", "positive", "تصاعدي", "تصاعدية":
		return models.KPIPolarityAscending, true
	case "descending", "lower is better", "negative", "تنازلي", "تنازلية":
		return models.KPIPolarityDescending, true
	}
	return "", false
}

func mapActivation(s string) (string, bool) {
	switch normName(s) {
	case "active", "مفعل", "نشط":
		return models.KPIStatusActive, true
	case "inactive", "غير مفعل", "غير نشط":
		return models.KPIStatusInactive, true
	case "draft", "مسودة":
		return models.KPIStatusDraft, true
	case "reviewed", "مراجَع", "مراجع", "مراجعة", "تمت المراجعة":
		return models.KPIStatusReviewed, true
	case "approved", "معتمد":
		return models.KPIStatusApproved, true
	case "closed", "مغلق":
		return models.KPIStatusClosed, true
	}
	return "", false
}

func mapFrequency(s string) (string, bool) {
	switch strings.NewReplacer("-", " ", "_", " ").Replace(normName(s)) {
	case "monthly", "month", "شهري":
		return models.KPIFrequencyMonthly, true
	case "quarterly", "quarter", "ربع سنوي":
		return models.KPIFrequencyQuarterly, true
	case "semi annual", "semiannual", "semi annually", "half yearly", "نصف سنوي":
		return models.KPIFrequencySemiAnnual, true
	case "annual", "annually", "yearly", "سنوي":
		return models.KPIFrequencyAnnually, true
	case "custom", "مخصص":
		return models.KPIFrequencyCustom, true
	}
	return "", false
}

func mapOwnerType(s string) (string, bool) {
	switch normName(s) {
	case "internal", "داخلي":
		return models.KPIOwnerTypeInternal, true
	case "external", "خارجي":
		return models.KPIOwnerTypeExternal, true
	}
	return "", false
}

// mapClassification accepts the KPI Classification values shown on the
// dictionary form (KPI / Measurement / Output, or their Arabic labels).
func mapClassification(s string) (string, bool) {
	switch normName(s) {
	case "kpi", "مؤشر":
		return "kpi", true
	case "measurement", "مقياس":
		return "measurement", true
	case "output", "مخرج":
		return "output", true
	}
	return "", false
}

func mapTargetRequirement(s string) (string, bool) {
	switch normName(s) {
	case "mandatory", "إلزامي", "الزامي":
		return "mandatory", true
	case "optional", "اختياري":
		return "optional", true
	}
	return "", false
}

func kindLabel(kind string) string {
	return map[string]string{
		mdDepartment: "department", mdPillar: "pillar", mdEnabler: "enabler", mdGoal: "strategic goal",
		mdObjective: "operational objective", mdProcess: "process", mdDomain: "domain",
		mdCriterion: "award criterion", mdSubCriterion: "award sub-criterion", mdInitiative: "initiative",
		mdOrganization: "organization", mdAwardMeasure: "award KPI measure",
	}[kind]
}
