package services

// KPI Dictionary bulk import — reads the approved "KPI Dictionary" Excel
// workbook (Master Data, the three Dictionary sheets and the three quarterly
// Performance sheets), validates every row against the template and the
// existing system data, and builds an import plan. The plan doubles as the
// preview shown to the user; CommitKpiDictionaryImport re-validates the same
// file and applies the plan in one transaction.
//
// Where each workbook category lands:
//   - Master Data sheet     → pillars, enablers, goals, operational_objectives,
//                             initiatives (+ owning departments)
//   - Dictionary sheets     → strategic_kpis / operational_kpis / award_kpis
//                             (+ processes, domains, award criteria/
//                             sub-criteria and departments they reference
//                             that don't exist yet)
//   - "Targets YYYY" cols   → kpi_annual_targets (period_code "annual")
//   - Performance sheets    → approved kpi_entries, one per KPI per quarter
//                             that has an Actual Result, against the KPI's
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
)

// Record categories reported in the preview/summary, in display order.
var KpiImportCategories = []string{
	"department", "pillar", "enabler", "strategic_goal", "operational_objective",
	"process", "domain", "award_criterion", "award_sub_criterion", "initiative",
	"kpi", "metric", "annual_target", "performance",
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
	DeptType    string
	DomainType  string
	action      string // set by finalize
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

	Pillar, Goal, Objective, Process, Domain, SubCriterion, OwnerDept, Agency *mdNode

	OwnerType, Polarity, Frequency, Lifecycle, DataSource, Formula string
	DescriptionEn, DescriptionAr, SegAxes, RelatedUnits, Notes     string
	Baseline                                                       float64

	Targets []*kpiTargetRec

	Rejected  bool
	RejectWhy string
	action    string
}

type kpiPerfRec struct {
	Kpi        *kpiRec
	Sheet      string
	Row        int
	Year       int
	Quarter    int
	Target     *float64
	Actual     float64
	ExistingID *uuid.UUID
	Rejected   bool
	RejectWhy  string
	action     string
}

type existingKpi struct {
	ID        uuid.UUID
	Type      string
	Code      string
	NameEn    string
	Polarity  string
	Frequency string
	Baseline  float64
	MetricID  *uuid.UUID
}

type kpiImporter struct {
	db     *gorm.DB
	ctx    context.Context
	userID uuid.UUID
	opts   KpiImportOptions
	now    time.Time
	res    *KpiImportResult

	structuralError bool

	md map[string][]*mdNode

	kpis          []*kpiRec
	kpiByTypeCode map[string]*kpiRec
	kpiByCode     map[string]*kpiRec
	perfs         []*kpiPerfRec

	existingKpis    map[string]*existingKpi // type|CODE
	existingCodeTyp map[string]string       // CODE → type
	existingTargets map[string]uuid.UUID    // type|CODE|year
	existingEntries map[string]uuid.UUID    // kpiID|type|year|period_code
	deptCodes       map[string]bool
}

// ─── Entry points ───────────────────────────────────────────────────────────

// ValidateKpiDictionaryImport parses and validates the workbook and returns
// the import preview. Nothing is written.
func ValidateKpiDictionaryImport(ctx context.Context, db *gorm.DB, data []byte, fileName string, userID uuid.UUID, opts KpiImportOptions) (*KpiImportResult, error) {
	imp, err := buildKpiImportPlan(ctx, db, data, fileName, userID, opts)
	if err != nil {
		return nil, err
	}
	imp.finalize()
	return imp.res, nil
}

// CommitKpiDictionaryImport re-validates the workbook and, when the chosen
// options allow it, writes the whole plan in a single transaction — any
// write failure rolls back every record. afterKpiCreated (optional) runs
// after a successful commit for each newly created KPI (used to start its
// dictionary workflow, which manages its own writes).
func CommitKpiDictionaryImport(ctx context.Context, db *gorm.DB, data []byte, fileName string, userID uuid.UUID, opts KpiImportOptions,
	afterKpiCreated func(ctx context.Context, kpiType string, kpiID uuid.UUID) error) (*KpiImportResult, error) {
	imp, err := buildKpiImportPlan(ctx, db, data, fileName, userID, opts)
	if err != nil {
		return nil, err
	}
	imp.finalize()
	if !imp.res.CanCommit {
		return imp.res, nil
	}

	if err := db.WithContext(ctx).Transaction(imp.commit); err != nil {
		imp.res.CommitError = err.Error()
		imp.markRolledBack()
		return imp.res, nil
	}
	imp.res.Committed = true

	if afterKpiCreated != nil {
		for _, k := range imp.kpis {
			if k.action == kpiImportActionCreate {
				if err := afterKpiCreated(ctx, k.Type, k.ID); err != nil {
					imp.warn(k.Sheet, k.Row, k.Code, "", fmt.Sprintf("KPI imported, but its workflow could not be started (it will be initialised on first use): %v", err))
				}
			}
		}
	}
	return imp.res, nil
}

func buildKpiImportPlan(ctx context.Context, db *gorm.DB, data []byte, fileName string, userID uuid.UUID, opts KpiImportOptions) (*kpiImporter, error) {
	opts.normalize()
	imp := &kpiImporter{
		db:              db.WithContext(ctx),
		ctx:             ctx,
		userID:          userID,
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
		dictTables[d.key] = imp.readTable(sheets[d.key], rows(sheets[d.key]), dictColumns(d.kpiType), true)
	}
	perfTables := map[string]*sheetTable{}
	for _, p := range perfSheets {
		perfTables[p.key] = imp.readTable(sheets[p.key], rows(sheets[p.key]), perfColumns(p.kpiType), false)
	}
	if imp.structuralError {
		return imp, nil
	}

	imp.parseMasterData(sheets["master"], masterRows)
	for _, d := range dictSheets {
		imp.parseDictionary(d.kpiType, dictTables[d.key])
	}
	for _, p := range perfSheets {
		imp.parsePerformance(p.kpiType, perfTables[p.key])
	}
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
	for _, q := range []struct {
		dest interface{}
	}{{&pillars}, {&enablers}, {&goals}, {&objectives}, {&processes}, {&depts}, {&domains}, {&criteria}, {&subs}, {&initiatives}} {
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

	for _, p := range pillars {
		imp.addExistingNode(&mdNode{Kind: mdPillar, NameEn: p.NameEn, NameAr: p.NameAr, ID: p.ID})
	}
	for _, e := range enablers {
		imp.addExistingNode(&mdNode{Kind: mdEnabler, NameEn: e.NameEn, NameAr: e.NameAr, ID: e.ID})
	}
	goalByID := map[uuid.UUID]*mdNode{}
	for _, g := range goals {
		n := &mdNode{Kind: mdGoal, NameEn: g.Title, ID: g.ID}
		goalByID[g.ID] = n
		imp.addExistingNode(n)
	}
	objByID := map[uuid.UUID]*mdNode{}
	for _, o := range objectives {
		n := &mdNode{Kind: mdObjective, NameEn: o.NameEn, NameAr: o.NameAr, ID: o.ID}
		if o.GoalID != nil {
			n.Goal = goalByID[*o.GoalID]
		}
		objByID[o.ID] = n
		imp.addExistingNode(n)
	}
	for _, p := range processes {
		imp.addExistingNode(&mdNode{Kind: mdProcess, NameEn: p.NameEn, NameAr: p.NameAr, ID: p.ID, Objective: objByID[p.OperationalObjectiveID]})
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

	// Existing KPIs across all three dictionary tables, with their first
	// metric (the one imported targets/entries attach to).
	type kpiRow struct {
		ID                 uuid.UUID
		Code               string
		NameEn             string
		Polarity           string
		ReportingFrequency string
		Baseline           float64
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
		if err := db.Model(t.model).Select("id, code, name_en, polarity, reporting_frequency, baseline").Scan(&rows).Error; err != nil {
			return err
		}
		for _, r := range rows {
			code := strings.ToUpper(strings.TrimSpace(r.Code))
			imp.existingKpis[t.kpiType+"|"+code] = &existingKpi{ID: r.ID, Type: t.kpiType, Code: r.Code, NameEn: r.NameEn, Polarity: r.Polarity, Frequency: r.ReportingFrequency, Baseline: r.Baseline}
			imp.existingCodeTyp[code] = t.kpiType
		}
	}
	var metrics []models.KpiMetric
	if err := db.Order("display_order ASC, created_at ASC").Find(&metrics).Error; err != nil {
		return err
	}
	for _, ek := range imp.existingKpis {
		for i := range metrics {
			if metrics[i].KpiID == ek.ID && metrics[i].KpiType == ek.Type {
				id := metrics[i].ID
				ek.MetricID = &id
				break
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
		ReportingYear int
		PeriodCode    string
	}
	var entries []entryRow
	if err := db.Raw(`SELECT id, kpi_id, kpi_type, reporting_year, period_code FROM kpi_entries
		WHERE deleted_at IS NULL AND status <> ? ORDER BY created_at`, models.KpiEntryStatusRejected).Scan(&entries).Error; err != nil {
		return err
	}
	for _, e := range entries {
		imp.existingEntries[fmt.Sprintf("%s|%s|%d|%s", e.KpiID, e.KpiType, e.ReportingYear, strings.ToLower(e.PeriodCode))] = e.ID
	}
	return nil
}

// ─── Sheet identification & table reading ───────────────────────────────────

var kpiImportSheetRules = []struct {
	key   string
	label string
	match func(n string) bool
}{
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
	for _, rule := range kpiImportSheetRules {
		if _, ok := found[rule.key]; !ok {
			imp.structural("", fmt.Sprintf("Mandatory worksheet %q is missing — the file does not match the approved KPI Dictionary template.", rule.label))
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
	"code": "Code", "name": "Indicator name", "pillar": "Pillar / Enabler", "goal": "Strategic Goal",
	"objective": "Operational Objective", "process": "Related Process Name", "domain": "Related Domain / Award Criterion",
	"seg": "Segmentation Axes", "polarity": "Polarity / Direction", "activation": "Activation Status",
	"description": "Description", "owner_type": "Owner Type", "owner_dept": "Owner Department",
	"agency": "الوكالة / الإدارة المالكة", "units": "Related Administrative Units", "frequency": "Reporting Frequency",
	"lifecycle": "KPI Lifecycle", "data_source": "Data Source", "formula": "Calculation Formula", "baseline": "Baseline",
	"notes": "Notes", "criterion": "Criterion", "sub_no": "Sub-Criterion #", "sub_name": "Sub-Criterion",
}

func hdrEq(s string) func(string) bool { return func(h string) bool { return h == s } }
func hdrPrefix(s string) func(string) bool {
	return func(h string) bool { return strings.HasPrefix(h, s) }
}

func dictColumns(kpiType string) []colSpec {
	cols := []colSpec{
		{"code", hdrEq("code"), true},
		{"pillar", hdrPrefix("pillar"), true},
		{"goal", hdrEq("strategic goal"), kpiType != models.KPITypeOperational},
		{"domain", hdrPrefix("related domain"), false},
		{"seg", hdrPrefix("segmentation axes"), false},
		{"polarity", hdrPrefix("polarity"), true},
		{"activation", hdrPrefix("activation status"), true},
		{"description", hdrEq("description"), false},
		{"owner_type", hdrEq("owner type"), false},
		{"owner_dept", func(h string) bool { return strings.HasPrefix(h, "owner department") || h == "المالك" }, true},
		{"agency", func(h string) bool {
			return h == "الوكالة / الإدارة المالكة" || strings.Contains(h, "owning agency")
		}, false},
		{"units", hdrPrefix("related administrative units"), false},
		{"frequency", hdrPrefix("reporting frequency"), true},
		{"lifecycle", hdrPrefix("kpi lifecycle"), false},
		{"data_source", hdrEq("data source"), false},
		{"formula", hdrPrefix("calculation formula"), false},
		{"baseline", hdrEq("baseline"), false},
		{"notes", hdrEq("notes"), false},
	}
	switch kpiType {
	case models.KPITypeStrategic:
		cols = append(cols, colSpec{"name", hdrEq("strategic indicator"), true})
	case models.KPITypeOperational:
		cols = append(cols,
			colSpec{"name", hdrEq("operational indicator"), true},
			colSpec{"objective", hdrEq("operational objective"), true},
			colSpec{"process", hdrPrefix("related process"), true},
		)
	case models.KPITypeAward:
		cols = append(cols,
			colSpec{"name", func(h string) bool { return h == "مؤشر / مقياس" || h == "indicator / measure" }, true},
			colSpec{"criterion", func(h string) bool { return h == "criterion" || h == "المعيار" }, true},
			colSpec{"sub_no", hdrEq("sub-criterion #"), true},
			colSpec{"sub_name", hdrEq("sub-criterion"), false},
		)
	}
	return cols
}

func perfColumns(kpiType string) []colSpec {
	cols := []colSpec{
		{"code", hdrEq("code"), true},
		{"owner_dept", hdrPrefix("owner department"), false},
	}
	switch kpiType {
	case models.KPITypeStrategic:
		cols = append(cols, colSpec{"name", hdrEq("strategic indicator"), false})
	case models.KPITypeOperational:
		cols = append(cols, colSpec{"name", hdrEq("operational indicator"), false})
	case models.KPITypeAward:
		cols = append(cols, colSpec{"name", hdrPrefix("award indicators"), false})
	}
	return cols
}

var (
	annualTargetHeaderRe = regexp.MustCompile(`^(?:target\S*|مستهدفات)\s*(\d{4})$`)
	quarterHeaderRe      = regexp.MustCompile(`^(target|actual result|achievement)\s+(\d{4})\s+q([1-4])$`)
)

// readTable locates the header row (the row holding a "Code" column — the
// English header row; the Arabic header row sits directly above it) and maps
// every template column. Headers are matched against both header rows so a
// column whose "English" header is still in Arabic is recognised too.
func (imp *kpiImporter) readTable(sheet string, rows [][]string, specs []colSpec, dictionary bool) *sheetTable {
	t := &sheetTable{sheet: sheet, headerRow: -1, cols: map[string]int{}, colLabel: map[string]string{},
		years: map[int]int{}, quarters: map[string][3]int{}, qLabels: map[string][3]string{}}
	if sheet == "" {
		return t
	}
	for i := 0; i < len(rows) && i < 15; i++ {
		for _, c := range rows[i] {
			if normHeader(c) == "code" {
				t.headerRow = i
				break
			}
		}
		if t.headerRow >= 0 {
			break
		}
	}
	if t.headerRow < 0 {
		imp.structural(sheet, "Header row not found — the sheet must have the template's header rows with a \"Code\" column.")
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
				t.colLabel[s.key] = label
				break
			}
		}
		if dictionary {
			for _, h := range []string{e, a} {
				if m := annualTargetHeaderRe.FindStringSubmatch(h); m != nil {
					y, _ := strconv.Atoi(m[1])
					if _, dup := t.years[y]; !dup {
						t.years[y] = j
					}
					break
				}
			}
		} else if m := quarterHeaderRe.FindStringSubmatch(e); m != nil {
			key := m[2] + "-" + m[3]
			idx, ok := t.quarters[key]
			lbl := t.qLabels[key]
			if !ok {
				idx = [3]int{-1, -1, -1}
			}
			pos := map[string]int{"target": 0, "actual result": 1, "achievement": 2}[m[1]]
			if idx[pos] < 0 {
				idx[pos] = j
				lbl[pos] = label
			}
			t.quarters[key] = idx
			t.qLabels[key] = lbl
		}
	}
	for _, s := range specs {
		if _, ok := t.cols[s.key]; !ok && s.required {
			imp.structural(sheet, fmt.Sprintf("Mandatory column %q is missing or its header was changed.", kpiImportColumnLabels[s.key]))
		}
	}
	if dictionary && len(t.years) == 0 {
		imp.structural(sheet, "No annual target columns (\"Targets YYYY\") were found.")
	}
	if !dictionary {
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
				case strings.HasPrefix(title, "pill"):
					section = mdPillar
				case strings.HasPrefix(title, "enabler"):
					section = mdEnabler
				case strings.HasPrefix(title, "strategic goal"):
					section = mdGoal
				case strings.HasPrefix(title, "operational objective"):
					section = mdObjective
				case strings.HasPrefix(title, "initiative"):
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

		node := imp.sheetNode(section, name, sheet, excelRow)
		p := pending{node: node, row: excelRow}
		switch section {
		case mdPillar, mdEnabler:
			p.ownerName = findCol(func(h string) bool { return strings.Contains(h, "owner") })
			if num != "" {
				refs[section][num] = node
			}
		case mdGoal:
			p.pillarRef = findCol(hdrPrefix("pillar ref"))
			p.enablerRef = findCol(hdrPrefix("enabler ref"))
			if num != "" {
				refs[mdGoal][num] = node
			}
		case mdObjective:
			p.goalRef = findCol(hdrPrefix("strategic goal ref"))
		case mdInitiative:
			p.ownerName = findCol(hdrPrefix("initiative owner"))
			p.pillarRef = findCol(hdrPrefix("pillar ref"))
			p.enablerRef = findCol(hdrPrefix("enabler ref"))
			p.goalRef = findCol(hdrPrefix("strategic goal ref"))
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
				n.Owner = imp.department(p.ownerName, sheet, p.row, n.NameEn)
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
				}
			}
		}
	}
}

// sheetNode returns the node for a master-data record declared on the
// Master Data sheet — reusing the existing record with that exact name if
// there is one, otherwise a new node to create.
func (imp *kpiImporter) sheetNode(kind, name, sheet string, row int) *mdNode {
	if n := imp.findExact(kind, name, nil); n != nil {
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
func (imp *kpiImporter) department(name, sheet string, row int, recordID string) *mdNode {
	// Exact match only — similar department names are often genuinely
	// different units.
	if n := imp.findExact(mdDepartment, name, nil); n != nil {
		return n
	}
	if len([]rune(name)) > 100 {
		imp.errorf(sheet, row, recordID, "Owner Department", fmt.Sprintf("Department name %q is longer than 100 characters.", name))
		return nil
	}
	n := imp.newNode(mdDepartment, name, sheet, row)
	n.DeptType = "internal"
	imp.warn(sheet, row, recordID, "Owner Department", fmt.Sprintf("Department %q does not exist and will be created.", name))
	return n
}

// ─── Dictionary sheets ──────────────────────────────────────────────────────

var kpiCodeRe = regexp.MustCompile(`^[A-Z][A-Z0-9]*(-[A-Z0-9]+)+$`)

func (imp *kpiImporter) parseDictionary(kpiType string, t *sheetTable) {
	if t == nil || t.headerRow < 0 {
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
		name := t.get(r, "name")
		if name == "" {
			fail("name", "KPI name is required.")
		} else if len([]rune(name)) > 255 {
			fail("name", "KPI name must be at most 255 characters.")
		}
		rec.NameEn, rec.NameAr = splitName(name)
		if desc := t.get(r, "description"); hasArabic(desc) {
			rec.DescriptionAr = desc
		} else {
			rec.DescriptionEn = desc
		}
		rec.Formula = t.get(r, "formula")
		rec.SegAxes = t.get(r, "seg")
		rec.RelatedUnits = t.get(r, "units")
		rec.Notes = t.get(r, "notes")
		rec.Lifecycle = t.get(r, "lifecycle")
		if len([]rune(rec.Lifecycle)) > 100 {
			fail("lifecycle", "KPI Lifecycle must be at most 100 characters.")
		}
		rec.DataSource = t.get(r, "data_source")
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
			fail("activation", "Activation Status is required.")
		} else if _, ok := mapActivation(v); !ok {
			fail("activation", fmt.Sprintf("Invalid activation status %q — expected Active, Inactive, Draft, Reviewed, Approved or Closed.", v))
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

		// Master-data references
		pillarName := t.get(r, "pillar")
		if pillarName == "" {
			fail("pillar", "Pillar / Enabler is required.")
		} else {
			n, fuzzy := imp.find(mdPillar, pillarName, nil)
			if n == nil {
				n, fuzzy = imp.find(mdEnabler, pillarName, nil)
			}
			if n == nil {
				fail("pillar", fmt.Sprintf("Pillar / Enabler %q does not exist in the system or on the Master Data sheet.", pillarName))
			} else {
				if fuzzy {
					imp.warn(t.sheet, excelRow, id, t.label("pillar"), fmt.Sprintf("%q matched to %s %q.", pillarName, kindLabel(n.Kind), n.NameEn))
				}
				refFail("pillar", n)
				if n.Kind == mdPillar {
					rec.Pillar = n
				}
			}
		}

		goalName := t.get(r, "goal")
		var goalNode *mdNode
		if goalName != "" {
			var fuzzy bool
			goalNode, fuzzy = imp.find(mdGoal, goalName, nil)
			if goalNode != nil && fuzzy {
				imp.warn(t.sheet, excelRow, id, t.label("goal"), fmt.Sprintf("%q matched to strategic goal %q.", goalName, goalNode.NameEn))
			}
		}

		switch kpiType {
		case models.KPITypeOperational:
			objName := t.get(r, "objective")
			if objName == "" {
				fail("objective", "Operational Objective is required.")
			} else if obj, fuzzy := imp.find(mdObjective, objName, nil); obj == nil {
				fail("objective", fmt.Sprintf("Operational objective %q does not exist in the system or on the Master Data sheet.", objName))
			} else {
				if fuzzy {
					imp.warn(t.sheet, excelRow, id, t.label("objective"), fmt.Sprintf("%q matched to operational objective %q.", objName, obj.NameEn))
				}
				refFail("objective", obj)
				rec.Objective = obj
				rec.Goal = obj.Goal
				if rec.Pillar == nil {
					rec.Pillar = obj.Pillar
				}
				switch {
				case goalName != "" && goalNode == nil:
					imp.warn(t.sheet, excelRow, id, t.label("goal"), fmt.Sprintf("Strategic goal %q was not found; the goal is taken from the operational objective instead.", goalName))
				case goalNode != nil && obj.Goal != nil && goalNode != obj.Goal:
					imp.warn(t.sheet, excelRow, id, t.label("goal"), fmt.Sprintf("Strategic goal %q does not match the objective's goal %q; the objective's goal is used.", goalName, obj.Goal.NameEn))
				}
				procName := t.get(r, "process")
				if procName == "" {
					fail("process", "Related Process Name is required.")
				} else if len([]rune(procName)) > 255 {
					fail("process", "Process name must be at most 255 characters.")
				} else if proc, _ := imp.find(mdProcess, procName, obj); proc != nil {
					rec.Process = proc
				} else {
					proc := imp.newNode(mdProcess, procName, t.sheet, excelRow)
					proc.Objective = obj
					rec.Process = proc
				}
			}
		default:
			if goalName == "" {
				fail("goal", "Strategic Goal is required.")
			} else if goalNode == nil {
				fail("goal", fmt.Sprintf("Strategic goal %q does not exist in the system or on the Master Data sheet.", goalName))
			} else {
				refFail("goal", goalNode)
				rec.Goal = goalNode
				if rec.Pillar != nil && goalNode.Pillar != nil && goalNode.Pillar != rec.Pillar {
					imp.warn(t.sheet, excelRow, id, t.label("pillar"), fmt.Sprintf("Pillar %q differs from the pillar of strategic goal %q (%q).", rec.Pillar.NameEn, goalNode.NameEn, goalNode.Pillar.NameEn))
				}
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

		if kpiType == models.KPITypeAward {
			imp.resolveAwardCriteria(t, r, excelRow, id, rec, domainName, fail)
		}

		if ownerName := t.get(r, "owner_dept"); ownerName == "" {
			fail("owner_dept", "Owner Department is required.")
		} else {
			rec.OwnerDept = imp.department(ownerName, t.sheet, excelRow, id)
			if rec.OwnerDept == nil {
				fail("owner_dept", "Owner Department could not be resolved.")
			}
		}
		if agency := t.get(r, "agency"); agency != "" {
			rec.Agency = imp.department(agency, t.sheet, excelRow, id)
		}
		if rec.OwnerType == "" {
			rec.OwnerType = models.KPIOwnerTypeInternal
			if rec.OwnerDept != nil && rec.OwnerDept.DeptType == "external" {
				rec.OwnerType = models.KPIOwnerTypeExternal
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
			case y < 2000 || y > 2100:
				imp.errorf(t.sheet, excelRow, id, tr.Field, fmt.Sprintf("Target year %d is outside the permitted range (2000–2100).", y))
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

func (imp *kpiImporter) resolveAwardCriteria(t *sheetTable, r []string, excelRow int, id string, rec *kpiRec, domainName string, fail func(key, msg string)) {
	critRaw := t.get(r, "criterion")
	subNo := strings.TrimSuffix(t.get(r, "sub_no"), ".0")
	subName := t.get(r, "sub_name")
	critNo, ok := parseCriterionNo(critRaw)
	if critRaw == "" {
		fail("criterion", "Criterion is required.")
		return
	}
	if !ok {
		fail("criterion", fmt.Sprintf("Criterion %q is not valid — expected e.g. \"Criterion 5\".", critRaw))
		return
	}
	var crit *mdNode
	for _, n := range imp.md[mdCriterion] {
		if n.CriterionNo == critNo {
			crit = n
			break
		}
	}
	if crit == nil {
		name := fmt.Sprintf("Criterion %d", critNo)
		if d := normName(domainName); strings.HasPrefix(d, normName(name)+":") {
			name = strings.TrimSpace(domainName[strings.Index(domainName, ":")+1:])
		}
		crit = imp.newNode(mdCriterion, name, t.sheet, excelRow)
		crit.CriterionNo = critNo
	}
	if subNo == "" {
		fail("sub_no", "Sub-Criterion # is required.")
		return
	}
	if !regexp.MustCompile(`^\d+(\.\d+)*$`).MatchString(subNo) {
		fail("sub_no", fmt.Sprintf("Sub-Criterion # %q is not valid — expected e.g. \"5.1\".", subNo))
		return
	}
	if !strings.HasPrefix(subNo, strconv.Itoa(critNo)+".") {
		fail("sub_no", fmt.Sprintf("Sub-Criterion %s does not belong to Criterion %d.", subNo, critNo))
		return
	}
	for _, n := range imp.md[mdSubCriterion] {
		if n.Criterion == crit && n.SubNo == subNo {
			rec.SubCriterion = n
			return
		}
	}
	if subName == "" {
		fail("sub_name", fmt.Sprintf("Sub-Criterion %s does not exist yet, so its name is required.", subNo))
		return
	}
	sub := imp.newNode(mdSubCriterion, subName, t.sheet, excelRow)
	sub.SubNo = subNo
	sub.Criterion = crit
	rec.SubCriterion = sub
}

// ─── Performance sheets ─────────────────────────────────────────────────────

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
		for _, k := range keys {
			if j := t.quarters[k][1]; j >= 0 && strings.TrimSpace(cell(r, j)) != "" {
				hasActual = true
			}
		}
		if !hasActual {
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

		kpi := imp.kpiByTypeCode[kpiType+"|"+code]
		if kpi == nil {
			if ek, ok := imp.existingKpis[kpiType+"|"+code]; ok {
				eid := ek.ID
				kpi = &kpiRec{Type: kpiType, Code: ek.Code, NameEn: ek.NameEn, DBOnly: true, ID: ek.ID, ExistingID: &eid,
					MetricID: ek.MetricID, Polarity: ek.Polarity, Frequency: ek.Frequency, Baseline: ek.Baseline, DataSource: ""}
				imp.kpiByTypeCode[kpiType+"|"+code] = kpi
				imp.kpis = append(imp.kpis, kpi)
			}
		}
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

		for _, k := range keys {
			idx, lbl := t.quarters[k], t.qLabels[k]
			actualRaw := ""
			if idx[1] >= 0 {
				actualRaw = strings.TrimSpace(cell(r, idx[1]))
			}
			if actualRaw == "" {
				continue
			}
			parts := strings.SplitN(k, "-", 2)
			year, _ := strconv.Atoi(parts[0])
			q, _ := strconv.Atoi(parts[1])
			rec := &kpiPerfRec{Kpi: kpi, Sheet: t.sheet, Row: excelRow, Year: year, Quarter: q}
			reject := func(field, msg string) {
				imp.errorf(t.sheet, excelRow, fmt.Sprintf("%s %d Q%d", code, year, q), field, msg)
				rec.Rejected, rec.RejectWhy = true, "invalid performance value"
			}
			if year < 2000 || year > 2100 {
				reject(lbl[1], fmt.Sprintf("Year %d is outside the permitted range (2000–2100).", year))
			}
			if v, ok := parseImportNumber(actualRaw); !ok {
				reject(lbl[1], fmt.Sprintf("Actual result %q is not a valid number.", actualRaw))
			} else if v < 0 {
				reject(lbl[1], fmt.Sprintf("Actual result %v must not be negative.", v))
			} else {
				rec.Actual = v
			}
			if idx[0] >= 0 {
				if raw := strings.TrimSpace(cell(r, idx[0])); raw != "" {
					if v, ok := parseImportNumber(raw); !ok {
						reject(lbl[0], fmt.Sprintf("Target %q is not a valid number.", raw))
					} else if v < 0 {
						reject(lbl[0], fmt.Sprintf("Target %v must not be negative.", v))
					} else {
						rec.Target = &v
					}
				}
			}
			if rec.Target == nil && !rec.Rejected {
				imp.warn(t.sheet, excelRow, fmt.Sprintf("%s %d Q%d", code, year, q), lbl[1], "Actual result has no quarterly target — achievement cannot be calculated.")
			}
			if quarterEnd(year, q).After(imp.now) {
				reject(lbl[1], fmt.Sprintf("Actual result reported for %d Q%d, which has not ended yet.", year, q))
			}
			// The workbook's own Achievement column is informational — the
			// system recalculates it; flag noticeable disagreements.
			if !rec.Rejected && rec.Target != nil && *rec.Target > 0 && idx[2] >= 0 && kpi.Polarity != models.KPIPolarityDescending {
				if raw := strings.TrimSpace(cell(r, idx[2])); raw != "" {
					if sheetAch, ok := parseImportNumber(raw); ok {
						calc := rec.Actual / *rec.Target
						if math.Abs(sheetAch-calc) > 0.01 && math.Abs(sheetAch-calc*100) > 1 {
							imp.warn(t.sheet, excelRow, fmt.Sprintf("%s %d Q%d", code, year, q), lbl[2],
								fmt.Sprintf("Workbook achievement %v differs from the calculated value %.4f; the calculated value is used.", sheetAch, calc))
						}
					}
				}
			}
			imp.perfs = append(imp.perfs, rec)
		}
	}
}

// ─── Plan resolution ────────────────────────────────────────────────────────

func (imp *kpiImporter) resolveMetricsAndExisting() {
	for _, p := range imp.perfs {
		if p.Kpi.ExistingID != nil {
			key := fmt.Sprintf("%s|%s|%d|q%d", *p.Kpi.ExistingID, p.Kpi.Type, p.Year, p.Quarter)
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
		case k.DBOnly:
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
		if p.action == kpiImportActionCreate || p.action == kpiImportActionUpdate {
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
			for _, n := range []*mdNode{k.Pillar, k.Goal, k.Objective, k.Process, k.Domain, k.SubCriterion, k.OwnerDept, k.Agency} {
				mark(n)
			}
		}
	}
	for _, kind := range []string{mdPillar, mdEnabler, mdGoal, mdObjective, mdInitiative, mdDepartment} {
		for _, n := range imp.md[kind] {
			if n.FromSheet && !n.Exists && !n.Rejected {
				mark(n)
			}
		}
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
		if k.DBOnly {
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
		items = append(items, KpiImportItem{Category: "performance", KpiType: p.Kpi.Type, RecordID: fmt.Sprintf("%s %d Q%d", p.Kpi.Code, p.Year, p.Quarter),
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

func normHeader(s string) string {
	s = strings.ToLower(strings.TrimSpace(multiSpaceRe.ReplaceAllString(s, " ")))
	return strings.TrimSuffix(s, ":")
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
	case "reviewed":
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

func quarterEnd(year, q int) time.Time {
	return time.Date(year, time.Month(3*q), 1, 0, 0, 0, 0, time.Local).AddDate(0, 1, 0).Add(-time.Nanosecond)
}

func quarterStart(year, q int) time.Time {
	return time.Date(year, time.Month(3*q-2), 1, 0, 0, 0, 0, time.Local)
}

func kindLabel(kind string) string {
	return map[string]string{
		mdDepartment: "department", mdPillar: "pillar", mdEnabler: "enabler", mdGoal: "strategic goal",
		mdObjective: "operational objective", mdProcess: "process", mdDomain: "domain",
		mdCriterion: "award criterion", mdSubCriterion: "award sub-criterion", mdInitiative: "initiative",
	}[kind]
}
