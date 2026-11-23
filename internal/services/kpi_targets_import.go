package services

// KPI Targets bulk import — validates and commits targets for existing KPIs.
// Periods follow the KPI/metric frequency; milestones derive them from dates.
// Only draft, returned or rejected existing targets can be updated.

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/automax/backend/internal/models"
	"github.com/automax/backend/internal/utils"
	"github.com/google/uuid"
	"github.com/xuri/excelize/v2"
	"gorm.io/datatypes"
	"gorm.io/gorm"
)

const (
	KpiTargetsTemplateFileName = "kpi-targets-import-template.xlsx"

	kpiTargetsTemplateSheet       = "KPI Targets"
	kpiTargetsCategory            = "target"
	kpiTargetsTemplateDataRows    = 4999
	kpiTargetsWorksheetHeaderScan = 15
)

type kpiTargetIssue struct {
	field   string
	message string
}

type kpiTargetImportRec struct {
	Row        int
	KpiCode    string
	KpiType    string
	KpiID      uuid.UUID
	MetricID   *uuid.UUID
	MetricCode string
	MetricName string
	Metric     string // current template: a metric name (or code)

	Target *models.KpiAnnualTarget
	Set    map[string]bool

	Existing *models.KpiAnnualTarget

	Issues    []kpiTargetIssue
	Rejected  bool
	RejectWhy string
	action    string
}

func (r *kpiTargetImportRec) reject(field, message string) {
	r.Rejected = true
	r.Issues = append(r.Issues, kpiTargetIssue{field: field, message: message})
}

func (r *kpiTargetImportRec) reason() string {
	parts := make([]string, 0, len(r.Issues))
	for _, is := range r.Issues {
		parts = append(parts, is.message)
	}
	return strings.Join(parts, " ")
}

func (r *kpiTargetImportRec) recordID() string {
	id := strings.TrimSpace(r.KpiCode)
	if r.Target != nil {
		if r.MetricName != "" {
			id += " / " + r.MetricName
		}
		id += fmt.Sprintf(" / %d / %s", r.Target.TargetYear, r.Target.PeriodCode)
	}
	return id
}

type kpiTargetImportTable struct {
	sheet     string
	headerRow int
	cols      map[string]int
}

type kpiTargetsImporter struct {
	db     *gorm.DB
	ctx    context.Context
	userID uuid.UUID
	opts   KpiImportOptions
	res    *KpiImportResult

	structuralError bool
	sheet           string
	recs            []*kpiTargetImportRec

	existingKpis   map[string]*existingKpi // type|CODE
	kpiByCode      map[string]string       // CODE -> type
	metricsByKpi   map[string][]*models.KpiMetric
	metricByCode   map[string]*models.KpiMetric       // type|kpiID|lower(code)
	metricByName   map[string]*models.KpiMetric       // type|kpiID|lower(name)
	existingTarget map[string]*models.KpiAnnualTarget // live version per identity
	allTargets     []*models.KpiAnnualTarget          // every version, for Milestone overlap checks

	access      *KpiAccessService
	accessCache map[string]string // type|CODE -> "" when allowed, else the rejection reason

	// onlySheet limits the import to one worksheet (the combined KPI
	// workbook's Targets sheet); "" finds the targets sheet.
	onlySheet string
	// createdKpis (type|kpiID): KPIs created by the same workbook import —
	// the importer may set their targets.
	createdKpis map[string]bool
}

// ─── Entry points ───────────────────────────────────────────────────────────

func ValidateKpiTargetsImport(ctx context.Context, db *gorm.DB, data []byte, fileName string, userID uuid.UUID, opts KpiImportOptions) (*KpiImportResult, error) {
	imp, err := buildKpiTargetsPlan(ctx, db, data, fileName, userID, opts)
	if err != nil {
		return nil, err
	}
	return imp.res, nil
}

func CommitKpiTargetsImport(ctx context.Context, db *gorm.DB, data []byte, fileName string, userID uuid.UUID, opts KpiImportOptions) (*KpiImportResult, error) {
	imp, err := buildKpiTargetsPlan(ctx, db, data, fileName, userID, opts)
	if err != nil {
		return nil, err
	}
	if !imp.res.CanCommit {
		return imp.res, nil
	}
	if err := db.WithContext(ctx).Transaction(imp.commit); err != nil {
		imp.res.CommitError = err.Error()
		imp.markRolledBack()
		return imp.res, nil
	}
	imp.res.Committed = true
	return imp.res, nil
}

func validateKpiTargetImportOptions(opts KpiImportOptions) string {
	var problems []string
	switch opts.ExistingMode {
	case "", KpiImportExistingSkip, KpiImportExistingUpdate:
	default:
		problems = append(problems, fmt.Sprintf("existing_mode %q is not valid (use %q or %q).", opts.ExistingMode, KpiImportExistingSkip, KpiImportExistingUpdate))
	}
	switch opts.ErrorMode {
	case "", KpiImportErrorsSkipInvalid, KpiImportErrorsAbort:
	default:
		problems = append(problems, fmt.Sprintf("error_mode %q is not valid (use %q or %q).", opts.ErrorMode, KpiImportErrorsSkipInvalid, KpiImportErrorsAbort))
	}
	return strings.Join(problems, " ")
}

func buildKpiTargetsPlan(ctx context.Context, db *gorm.DB, data []byte, fileName string, userID uuid.UUID, opts KpiImportOptions) (*kpiTargetsImporter, error) {
	return buildKpiTargetsPlanSheet(ctx, db, data, fileName, userID, opts, "", nil)
}

func buildKpiTargetsPlanSheet(ctx context.Context, db *gorm.DB, data []byte, fileName string, userID uuid.UUID, opts KpiImportOptions, onlySheet string, createdKpis map[string]bool) (*kpiTargetsImporter, error) {
	imp := &kpiTargetsImporter{
		db:          db,
		ctx:         ctx,
		userID:      userID,
		onlySheet:   onlySheet,
		createdKpis: createdKpis,
		res: &KpiImportResult{
			FileName: fileName,
			Errors:   []KpiImportIssue{},
			Warnings: []KpiImportIssue{},
			Items:    []KpiImportItem{},
		},
	}
	// An unknown mode must not silently fall back to a default: "update"
	// mistyped as "updte" would otherwise quietly skip every existing row.
	optsErr := validateKpiTargetImportOptions(opts)
	imp.opts = opts
	imp.opts.normalize()
	imp.res.Options = imp.opts
	if optsErr != "" {
		imp.structural("", optsErr)
		imp.finalize()
		return imp, nil
	}

	if err := imp.parseWorkbook(data); err != nil {
		return nil, err
	}
	if !imp.structuralError {
		if err := imp.loadExisting(imp.db.WithContext(imp.ctx)); err != nil {
			return nil, err
		}
		imp.validateRows()
	}
	imp.finalize()
	return imp, nil
}

// ─── Template ───────────────────────────────────────────────────────────────

func KpiTargetsTemplateFileNameFor(lang string) string {
	if lang == KpiTemplateLangAR {
		return "kpi-targets-import-template-ar.xlsx"
	}
	return KpiTargetsTemplateFileName
}

// The columns follow the Set Target form: KPI ("CODE - Name"), Metric and
// Period are dropdowns (what exists in the system); Target Type fills in
// from the Period.
func BuildKpiTargetsImportTemplate(ctx context.Context, db *gorm.DB, lang string) ([]byte, error) {
	language := utils.LanguageEnglish
	if lang == KpiTemplateLangAR {
		language = utils.LanguageArabic
	}
	lists, err := kpiSystemLists(ctx, db, lang)
	if err != nil {
		return nil, fmt.Errorf("loading dropdown lists: %w", err)
	}
	cols := kpiTargetSheetColumns(lang)
	cfg := utils.WorkbookConfig{
		Language:     language,
		Title:        "KPI Targets Import Template",
		Subject:      "Bulk import of KPI targets",
		Creator:      "Automax",
		ActiveSheet:  kpiTargetsTemplateSheet,
		Instructions: buildKpiTargetsInstructions(),
		Sheets: []utils.SheetConfig{{
			Name:         kpiTargetsTemplateSheet,
			Columns:      toExcelColumns(cols),
			DataRows:     kpiTargetsTemplateDataRows,
			FreezeHeader: true,
		}},
		ListsSheet: kpiTemplateListsSheet,
		Lists:      lists,
	}
	return utils.GenerateExcelTemplate(cfg)
}

func kpiTargetColumns() []colSpec {
	return []colSpec{
		// "KPI" holds "CODE - Name" (the current template); "KPI Code" the code.
		{"kpi_code", hdrAny("kpi", "kpi code", "المؤشر", "كود المؤشر"), true},
		// Optional: taken from the KPI when its code is used by one KPI type.
		{"kpi_type", hdrAny("kpi type", "نوع المؤشر"), false},
		{"metric", hdrAny("metric", "المقياس"), false},
		{"metric_code", hdrAny("metric code", "كود المقياس"), false},
		{"metric_name", hdrAny("metric name", "اسم المقياس"), false},
		{"target_year", hdrPrefixAny("target year", "السنة المستهدفة"), false},
		{"period_code", hdrAny("period", "period code", "الفترة", "رمز الفترة"), false},
		{"period_start", hdrPrefixAny("period start", "بداية الفترة"), false},
		{"period_end", hdrPrefixAny("period end", "نهاية الفترة"), false},
		{"target_value", hdrAny("target value", "قيمة المستهدف"), true},
		// Optional: follows the Period (see defaultTargetType).
		{"target_type", hdrAny("target type", "نوع المستهدف"), false},
		{"target_basis", hdrAny("target basis", "أساس المستهدف"), false},
		{"target_rationale", hdrAny("target rationale", "مبررات المستهدف"), false},
		{"threshold_mode", hdrAny("threshold mode", "نمط الحدود"), false},
		{"excellent_threshold", hdrPrefixAny("excellent threshold", "حد التميز"), false},
		{"achieved_threshold", hdrPrefixAny("achieved threshold", "حد التحقق"), false},
		{"warning_threshold", hdrPrefixAny("warning threshold", "حد التحذير"), false},
		{"target_range_min", hdrAny("target range min", "range min", "أدنى قيمة للنطاق"), false},
		{"target_range_max", hdrAny("target range max", "range max", "أعلى قيمة للنطاق"), false},
		{"target_status", hdrAny("target status", "حالة المستهدف"), false},
		{"effective_from", hdrPrefixAny("effective from", "ساري من"), false},
		{"effective_to", hdrPrefixAny("effective to", "ساري حتى"), false},
	}
}

func kpiTargetTemplateColumns() []kpiTemplateColumn {
	return []kpiTemplateColumn{
		{key: "kpi_code", ar: "كود المؤشر", en: "KPI Code", width: 16},
		{key: "kpi_type", ar: "نوع المؤشر", en: "KPI Type", width: 14,
			values: []string{"strategic", "operational", "award"}, valuesAr: []string{"استراتيجي", "تشغيلي", "جائزة"}},
		{key: "metric_code", ar: "كود المقياس", en: "Metric Code", width: 18},
		{key: "metric_name", ar: "اسم المقياس", en: "Metric Name", width: 30},
		{key: "target_year", ar: "السنة المستهدفة", en: "Target Year", width: 14},
		{key: "period_code", ar: "رمز الفترة", en: "Period Code", width: 16},
		{key: "period_start", ar: "بداية الفترة (YYYY-MM-DD)", en: "Period Start (YYYY-MM-DD)", width: 20},
		{key: "period_end", ar: "نهاية الفترة (YYYY-MM-DD)", en: "Period End (YYYY-MM-DD)", width: 20},
		{key: "target_value", ar: "قيمة المستهدف", en: "Target Value", width: 14},
		{key: "target_type", ar: "نوع المستهدف", en: "Target Type", width: 20,
			values: []string{"Period Target", "Annual Target", "Milestone / Ad Hoc"}, valuesAr: []string{"مستهدف فترة", "مستهدف سنوي", "مرحلة / حسب الحاجة"}},
		{key: "target_basis", ar: "أساس المستهدف", en: "Target Basis", width: 22,
			values:   []string{"Strategic Plan", "Previous Year", "Benchmark", "Regulatory Requirement", "Contract / SLA", "Management Decision", "Forecast", "Other"},
			valuesAr: []string{"الخطة الاستراتيجية", "العام السابق", "المعيار المرجعي", "متطلب تنظيمي", "عقد / اتفاقية مستوى خدمة", "قرار إداري", "توقع", "أخرى"}},
		{key: "target_rationale", ar: "مبررات المستهدف", en: "Target Rationale", width: 36},
		{key: "threshold_mode", ar: "نمط الحدود", en: "Threshold Mode", width: 24,
			values:   []string{"Use Global KPI Rules", "Percentage of Target", "Absolute Values", "Target Range", "No Thresholds"},
			valuesAr: []string{"استخدام قواعد المؤشر العامة", "نسبة من المستهدف", "قيم مطلقة", "نطاق مستهدف", "بدون حدود"}},
		{key: "excellent_threshold", ar: "حد التميز", en: "Excellent Threshold", width: 16},
		{key: "achieved_threshold", ar: "حد التحقق", en: "Achieved Threshold", width: 16},
		{key: "warning_threshold", ar: "حد التحذير", en: "Warning Threshold", width: 16},
		{key: "target_range_min", ar: "أدنى قيمة للنطاق", en: "Target Range Min", width: 16},
		{key: "target_range_max", ar: "أعلى قيمة للنطاق", en: "Target Range Max", width: 16},
		{key: "target_status", ar: "حالة المستهدف", en: "Target Status", width: 16,
			values: []string{"draft", "submitted"}, valuesAr: []string{"مسودة", "مقدم"}},
		{key: "effective_from", ar: "ساري من (YYYY-MM-DD)", en: "Effective From (YYYY-MM-DD)", width: 20},
		{key: "effective_to", ar: "ساري حتى (YYYY-MM-DD)", en: "Effective To (YYYY-MM-DD)", width: 20},
	}
}

func markKpiTargetTemplateRequired(cols []kpiTemplateColumn) []kpiTemplateColumn {
	required := make(map[string]bool, len(cols))
	for _, s := range kpiTargetColumns() {
		required[s.key] = s.required
	}
	for i := range cols {
		cols[i].required = required[cols[i].key]
	}
	return cols
}

func kpiTargetColumnLabel(key string) string {
	for _, c := range kpiTargetTemplateColumns() {
		if c.key == key {
			return c.en
		}
	}
	return key
}

func buildKpiTargetsInstructions() *utils.InstructionConfig {
	text := func(en, ar string) utils.LocalizedText { return utils.LocalizedText{EN: en, AR: ar} }
	item := func(labelEN, labelAR, bodyEN, bodyAR string) utils.InstructionItem {
		return utils.InstructionItem{Label: text(labelEN, labelAR), Text: text(bodyEN, bodyAR)}
	}
	return &utils.InstructionConfig{LabelWidth: 30, TextWidth: 110, Sections: []utils.InstructionSection{
		{Items: []utils.InstructionItem{item("Purpose", "الغرض", "This workbook creates or updates targets for existing KPIs, optionally attached to one of the KPI's metrics. The KPI itself must already exist in the KPI Dictionary.", "ينشئ هذا المصنف مستهدفات لمؤشرات موجودة أو يحدّثها، مع إمكانية ربطها بأحد مقاييس المؤشر. يجب أن يكون المؤشر نفسه موجوداً مسبقاً في دليل مؤشرات الأداء.")}, BlankAfter: true},
		{Title: text("How to use", "طريقة الاستخدام"), Items: []utils.InstructionItem{
			item("1. One row per target", "1. صف واحد لكل مستهدف", "Fill the KPI Targets sheet starting at row 2. One target per KPI, period and metric combination.", "املأ ورقة مستهدفات المؤشرات بدءاً من الصف 2. مستهدف واحد لكل مؤشر وفترة ومقياس."),
			item("2. Save and upload", "2. الحفظ والرفع", "Save as .xlsx, upload it, click Validate to review the report and preview, then Commit.", "احفظ الملف بصيغة .xlsx، وارفعه، ثم اضغط تحقق لمراجعة التقرير والمعاينة، ثم تنفيذ."),
		}, BlankAfter: true},
		{Title: text("Rules", "القواعد"), Items: []utils.InstructionItem{
			item("Mandatory columns", "الأعمدة المطلوبة", "Columns marked with a red * are mandatory: KPI, Metric, Period, Target Value and Target Type. Target Year is also required unless the Period is a custom date range. The columns follow the Set Target form.", "الأعمدة المميزة بعلامة * الحمراء مطلوبة: المؤشر والمقياس والفترة وقيمة المستهدف ونوع المستهدف. وتكون السنة المستهدفة مطلوبة أيضاً ما لم تكن الفترة نطاق تاريخ مخصص. الأعمدة بنفس ترتيب نموذج تحديد المستهدف."),
			item("KPI resolution", "مطابقة المؤشر", "Pick the KPI from the dropdown (code - name). Only the code is used, so typing just the code also works.", "اختر المؤشر من القائمة (الكود - الاسم). يُستخدم الكود فقط، لذا تكفي كتابة الكود أيضاً."),
			item("Metric", "المقياس", "Pick the metric name of the KPI. It can be left empty when the KPI has only one metric.", "اختر اسم مقياس المؤشر. يمكن تركه فارغاً إذا كان للمؤشر مقياس واحد فقط."),
			item("Period", "الفترة", "The Period must match the metric's (or KPI's) reporting frequency: jan..dec for monthly, q1..q4 for quarterly, h1/h2 for semi-annual, annual for an Annual Target. Choose \"Custom date range\" for a Milestone / Ad Hoc target and fill Period Start and Period End.", "يجب أن تطابق الفترة تكرار الإبلاغ للمقياس (أو المؤشر): jan..dec للشهري، q1..q4 للربع سنوي، h1/h2 للنصف سنوي، annual للمستهدف السنوي. اختر \"نطاق تاريخ مخصص\" لمستهدف المرحلة / حسب الحاجة واملأ بداية الفترة ونهايتها."),
			item("Milestone dates", "تواريخ المرحلة", "Period Start and Period End (YYYY-MM-DD) are only used for a custom date range. Target Type fills in from the Period (Milestone / Ad Hoc, Annual Target or Period Target) and can be changed.", "تُستخدم بداية الفترة ونهايتها (YYYY-MM-DD) لنطاق التاريخ المخصص فقط. ويُعبأ نوع المستهدف من الفترة (مرحلة / حسب الحاجة أو مستهدف سنوي أو مستهدف فترة) ويمكن تغييره."),
			item("Status", "الحالة", "New targets are saved as draft. Submission, approval and locking stay on the Targets page.", "تُحفظ المستهدفات الجديدة كمسودة. ويبقى التقديم والاعتماد والقفل في صفحة المستهدفات."),
			item("Existing targets", "المستهدفات الحالية", "With \\\"skip\\\", an existing target with the same KPI, metric, year and period is left untouched. With \\\"update\\\", it is updated in place; only draft, returned or rejected targets can be updated — approved, submitted, locked and superseded targets are rejected.", "مع \\\"تخطي\\\" يُترك المستهدف الحالي ذو المؤشر والمقياس والسنة والفترة نفسها دون تغيير. ومع \\\"تحديث\\\" يُحدّث في مكانه؛ ولا يمكن تحديث إلا المستهدفات المسودة أو المعادة أو المرفوضة — أما المعتمدة والمقدمة والمقفلة والمستبدلة فتُرفض."),
			item("Formats", "التنسيقات", "Blank optional cells keep the existing value on update and use the form defaults on create (Target Basis: Management Decision; Threshold Mode: Use Global KPI Rules).", "تحافظ الخلايا الاختيارية الفارغة على القيمة الحالية عند التحديث وتستخدم القيم الافتراضية عند الإنشاء (أساس المستهدف: قرار إداري؛ نمط الحدود: استخدام قواعد المؤشر العامة)."),
			item("Validation report", "تقرير التحقق", "Every rejected row is listed with its Excel row number and reason. \\\"skip_invalid\\\" commits valid rows; \\\"abort\\\" commits nothing while any error remains.", "يُدرج كل صف مرفوض مع رقم صفه في Excel وسبب الرفض. \\\"تخطي غير الصالح\\\" ينفذ الصفوف الصالحة؛ و\\\"إلغاء\\\" لا ينفذ أي شيء ما دام هناك خطأ."),
		}, BlankAfter: true},
	}}
}

// ─── Parsing ────────────────────────────────────────────────────────────────

func (imp *kpiTargetsImporter) parseWorkbook(data []byte) error {
	xf, err := excelize.OpenReader(bytes.NewReader(data))
	if err != nil {
		// Reported like any other structural problem, not as a server error.
		imp.structural("", "The uploaded file is not a valid Excel (.xlsx) workbook — download the template, fill it in and save it as .xlsx.")
		return nil
	}
	defer xf.Close()

	sheet, rows, headerRow := imp.locateSheet(xf)
	if sheet == "" {
		return nil
	}
	imp.sheet = sheet

	table := imp.readTargetTable(sheet, rows, headerRow)
	if imp.structuralError {
		return nil
	}
	for i := headerRow + 1; i < len(rows); i++ {
		if targetRowEmpty(rows[i]) {
			continue
		}
		raw := func(key string) string {
			j, ok := table.cols[key]
			if !ok {
				return ""
			}
			return strings.TrimSpace(cell(rows[i], j))
		}
		imp.recs = append(imp.recs, imp.parseTargetRow(i+1, raw))
	}
	return nil
}

func (imp *kpiTargetsImporter) locateSheet(xf *excelize.File) (string, [][]string, int) {
	type candidate struct {
		sheet     string
		rows      [][]string
		headerRow int
	}
	var found []candidate
	for _, name := range xf.GetSheetList() {
		normalized := normHeader(name)
		if normalized == normHeader("Instructions") || normalized == "تعليمات" || normalized == normHeader(kpiTemplateListsSheet) || normalized == "القوائم" {
			continue
		}
		if imp.onlySheet != "" && name != imp.onlySheet {
			continue
		}
		rows, err := xf.GetRows(name, excelize.Options{RawCellValue: true})
		if err != nil {
			imp.structural(name, fmt.Sprintf("The worksheet could not be read: %v", err))
			continue
		}
		for i := 0; i < len(rows) && i < kpiTargetsWorksheetHeaderScan; i++ {
			if isTargetHeaderRow(rows[i]) {
				found = append(found, candidate{sheet: name, rows: rows, headerRow: i})
				break
			}
		}
	}
	switch len(found) {
	case 0:
		imp.structural("", "The workbook does not contain a KPI Targets sheet — keep the template's \"KPI Targets\" worksheet and its header row.")
		return "", nil, 0
	case 1:
		return found[0].sheet, found[0].rows, found[0].headerRow
	default:
		imp.structural(found[1].sheet, fmt.Sprintf("More than one worksheet looks like the KPI Targets sheet (%q and %q) — keep only one.", found[0].sheet, found[1].sheet))
		return "", nil, 0
	}
}

func isTargetHeaderRow(row []string) bool {
	for _, h := range row {
		switch normHeader(h) {
		case "kpi code", "كود المؤشر", "target value", "قيمة المستهدف":
			return true
		}
	}
	return false
}

func (imp *kpiTargetsImporter) readTargetTable(sheet string, rows [][]string, headerRow int) *kpiTargetImportTable {
	table := &kpiTargetImportTable{sheet: sheet, headerRow: headerRow, cols: map[string]int{}}
	specs := kpiTargetColumns()
	header := rows[headerRow]
	matched := make([]bool, len(header))
	for j, h := range header {
		normalized := normHeader(h)
		if normalized == "" {
			continue
		}
		for _, spec := range specs {
			if _, taken := table.cols[spec.key]; taken {
				continue
			}
			if spec.match(normalized) {
				table.cols[spec.key] = j
				matched[j] = true
				break
			}
		}
		if !matched[j] {
			imp.warn(sheet, headerRow+1, "", "", fmt.Sprintf("Column %q is not part of the KPI Targets template and will be ignored.", inputHeaderText(h)))
		}
	}
	for _, spec := range specs {
		if _, ok := table.cols[spec.key]; !ok && spec.required {
			imp.structural(sheet, fmt.Sprintf("Mandatory column %q is missing from the header row.", kpiTargetColumnLabel(spec.key)))
		}
	}
	return table
}

func targetRowEmpty(row []string) bool {
	for _, v := range row {
		if strings.TrimSpace(v) != "" {
			return false
		}
	}
	return true
}

func (imp *kpiTargetsImporter) parseTargetRow(row int, raw func(string) string) *kpiTargetImportRec {
	set := map[string]bool{}
	read := func(key string) string {
		v := raw(key)
		if v != "" {
			set[key] = true
		}
		return v
	}
	r := &kpiTargetImportRec{
		Row:        row,
		KpiCode:    kpiCodeFromLabel(read("kpi_code")),
		MetricCode: read("metric_code"),
		MetricName: read("metric_name"),
		Metric:     read("metric"),
		Target: &models.KpiAnnualTarget{
			TargetType:     "Period Target",
			TargetBasis:    "Management Decision",
			ThresholdMode:  "Use Global KPI Rules",
			TargetStatus:   "draft",
			Version:        1,
			PeriodType:     "annual",
			TargetRangeMin: nil,
		},
		Set: set,
	}

	if r.KpiCode == "" {
		r.reject("KPI", "KPI is required.")
	}
	if typ := read("kpi_type"); typ == "" {
		// Resolved from the KPI code in validateRows.
	} else if v, ok := normalizeMetricChoice(typ, kpiTargetKpiTypeChoices); !ok {
		r.reject("KPI Type", fmt.Sprintf("KPI Type %q is not valid. Allowed: strategic, operational, award.", typ))
	} else {
		r.KpiType = v
	}

	applyChoice := func(field, rawKey, def string, choices []metricChoice, allowed string, target *string) {
		rawValue := read(rawKey)
		if rawValue == "" {
			*target = def
			return
		}
		v, ok := normalizeMetricChoice(rawValue, choices)
		if !ok {
			r.reject(field, fmt.Sprintf("%s %q is not valid. Allowed: %s.", field, rawValue, allowed))
			*target = def
			return
		}
		*target = v
	}
	// "Custom date range" in the Period column = a Milestone / Ad Hoc target
	// over Period Start – Period End (as on the Set Target form).
	periodRaw := read("period_code")
	customRange := isCustomRangeChoice(periodRaw)
	if customRange {
		periodRaw = ""
	}
	if typ := read("target_type"); typ == "" {
		r.Target.TargetType = defaultTargetType(customRange, periodRaw)
	} else if v, ok := normalizeMetricChoice(typ, kpiTargetTypeChoices); !ok {
		r.reject("Target Type", fmt.Sprintf("Target Type %q is not valid. Allowed: Period Target, Annual Target, Milestone / Ad Hoc.", typ))
	} else {
		r.Target.TargetType = v
	}
	applyChoice("Target Basis", "target_basis", "Management Decision", kpiTargetBasisChoices, "Strategic Plan, Previous Year, Benchmark, Regulatory Requirement, Contract / SLA, Management Decision, Forecast, Other", &r.Target.TargetBasis)
	applyChoice("Threshold Mode", "threshold_mode", "Use Global KPI Rules", kpiTargetThresholdChoices, "Use Global KPI Rules, Percentage of Target, Absolute Values, Target Range, No Thresholds", &r.Target.ThresholdMode)

	if status := read("target_status"); status != "" {
		if v, ok := normalizeMetricChoice(status, kpiTargetStatusChoices); !ok {
			r.reject("Target Status", fmt.Sprintf("Target Status %q is not valid. Import accepts draft or submitted only; approvals stay on the Targets page.", status))
		} else {
			r.Target.TargetStatus = v
		}
	}

	isMilestone := r.Target.TargetType == "Milestone / Ad Hoc"
	if customRange && !isMilestone {
		r.reject("Target Type", fmt.Sprintf("A custom date range is a Milestone / Ad Hoc target, not %s.", r.Target.TargetType))
	}
	yearRaw := read("target_year")
	if yearRaw != "" {
		if v, ok := parseMetricInt(yearRaw); !ok {
			r.reject("Target Year", fmt.Sprintf("Target Year %q must be a whole number.", yearRaw))
		} else if v < 2020 || v > 2040 {
			r.reject("Target Year", fmt.Sprintf("Target Year %d must be between 2020 and 2040.", v))
		} else {
			r.Target.TargetYear = v
		}
	}

	if periodRaw != "" && isMilestone {
		r.reject("Period Code", "Period Code must be blank for a Milestone / Ad Hoc target — it is derived from Period Start and Period End.")
	} else if periodRaw != "" {
		if IsCustomPeriodCode(periodRaw) {
			r.reject("Period Code", "Custom period codes are only allowed for Milestone / Ad Hoc targets; fill Period Start and Period End instead.")
		} else if code, year, err := CanonicalTargetPeriodCode(periodRaw, r.Target.TargetYear); err != nil {
			r.reject("Period Code", err.Error()+".")
		} else {
			r.Target.PeriodCode = code
			if r.Target.TargetYear == 0 && yearRaw == "" && year != 0 {
				// A year-embedded code ("2026-Q3") carries its own year.
				if err := ValidateKpiTargetYear(year); err != nil {
					r.reject("Target Year", err.Error()+".")
				} else {
					r.Target.TargetYear = year
				}
			}
		}
	}

	var start, end *time.Time
	if rawStart := read("period_start"); rawStart != "" {
		if v, ok := parseMetricDate(rawStart); !ok {
			r.reject("Period Start", fmt.Sprintf("Period Start %q must be a date in YYYY-MM-DD format.", rawStart))
		} else {
			start = v
		}
	}
	if rawEnd := read("period_end"); rawEnd != "" {
		if v, ok := parseMetricDate(rawEnd); !ok {
			r.reject("Period End", fmt.Sprintf("Period End %q must be a date in YYYY-MM-DD format.", rawEnd))
		} else {
			end = v
		}
	}
	if start != nil && end != nil && end.Before(*start) {
		r.reject("Period End", "Period End cannot be before Period Start.")
	}
	if isMilestone {
		if start == nil || end == nil {
			r.reject("Period Start", "Period Start and Period End are required for a Milestone / Ad Hoc target.")
		} else if !r.Rejected {
			r.Target.PeriodStart = start
			r.Target.PeriodEnd = end
			r.Target.PeriodCode = CustomPeriodCode(*start, *end)
			r.Target.TargetYear = start.Year()
			if err := validateMilestoneYears(*start, *end); err != nil {
				r.reject("Period Start", err.Error()+".")
			}
		}
	} else {
		r.Target.PeriodStart = start
		r.Target.PeriodEnd = end
		if r.Target.PeriodCode == "" && !customRange {
			r.reject("Period Code", "Period is required for Period and Annual targets.")
		}
		if r.Target.TargetYear == 0 {
			r.reject("Target Year", "Target Year is required for Period and Annual targets.")
		}
	}

	// A blank Target Value is checked once the metric is known: only an
	// Informational metric's target may omit it (see validateRows).
	if rawValue := read("target_value"); rawValue == "" {
	} else if v, ok := parseMetricFloat(rawValue); !ok {
		r.reject("Target Value", fmt.Sprintf("Target Value %q must be a number.", rawValue))
	} else {
		r.Target.TargetValue = v
	}

	r.Target.TargetRationale = read("target_rationale")

	for _, f := range []struct {
		field  string
		key    string
		target **float64
	}{
		{"Excellent Threshold", "excellent_threshold", &r.Target.ExcellentThreshold},
		{"Achieved Threshold", "achieved_threshold", &r.Target.AchievedThreshold},
		{"Warning Threshold", "warning_threshold", &r.Target.WarningThreshold},
		{"Target Range Min", "target_range_min", &r.Target.TargetRangeMin},
		{"Target Range Max", "target_range_max", &r.Target.TargetRangeMax},
	} {
		rawValue := read(f.key)
		if rawValue == "" {
			continue
		}
		if v, ok := parseMetricFloat(rawValue); !ok {
			r.reject(f.field, fmt.Sprintf("%s %q must be a number.", f.field, rawValue))
		} else {
			value := v
			*f.target = &value
		}
	}
	if rawValue := read("effective_from"); rawValue != "" {
		if v, ok := parseMetricDate(rawValue); !ok {
			r.reject("Effective From", fmt.Sprintf("Effective From %q must be a date in YYYY-MM-DD format.", rawValue))
		} else {
			r.Target.EffectiveFrom = v
		}
	}
	if rawValue := read("effective_to"); rawValue != "" {
		if v, ok := parseMetricDate(rawValue); !ok {
			r.reject("Effective To", fmt.Sprintf("Effective To %q must be a date in YYYY-MM-DD format.", rawValue))
		} else {
			r.Target.EffectiveTo = v
		}
	}
	if r.Target.EffectiveFrom != nil && r.Target.EffectiveTo != nil && r.Target.EffectiveTo.Before(*r.Target.EffectiveFrom) {
		r.reject("Effective To", "Effective To cannot be earlier than Effective From.")
	}

	if n := len([]rune(r.Target.TargetRationale)); n > models.KpiTargetRationaleMaxLen {
		r.reject("Target Rationale", fmt.Sprintf("Target Rationale must be at most %d characters (got %d).", models.KpiTargetRationaleMaxLen, n))
	}
	return r
}

// ─── Existing data ──────────────────────────────────────────────────────────

func (imp *kpiTargetsImporter) loadExisting(db *gorm.DB) error {
	imp.existingKpis = map[string]*existingKpi{}
	imp.kpiByCode = map[string]string{}
	imp.metricsByKpi = map[string][]*models.KpiMetric{}
	imp.metricByCode = map[string]*models.KpiMetric{}
	imp.metricByName = map[string]*models.KpiMetric{}
	imp.existingTarget = map[string]*models.KpiAnnualTarget{}
	imp.allTargets = nil

	for _, t := range []struct {
		kpiType string
		model   interface{}
	}{
		{models.KPITypeStrategic, &models.StrategicKPI{}},
		{models.KPITypeOperational, &models.OperationalKPI{}},
		{models.KPITypeAward, &models.AwardKPI{}},
	} {
		var rows []struct {
			ID     uuid.UUID
			Code   string
			NameEn string
		}
		if err := db.Model(t.model).Select("id, code, name_en").Scan(&rows).Error; err != nil {
			return err
		}
		for _, row := range rows {
			code := strings.ToUpper(strings.TrimSpace(row.Code))
			key := t.kpiType + "|" + code
			imp.existingKpis[key] = &existingKpi{ID: row.ID, Type: t.kpiType, Code: row.Code, NameEn: row.NameEn}
			if _, ok := imp.kpiByCode[code]; !ok {
				imp.kpiByCode[code] = t.kpiType
			}
		}
	}

	var metrics []models.KpiMetric
	if err := db.Find(&metrics).Error; err != nil {
		return err
	}
	for i := range metrics {
		m := &metrics[i]
		group := strings.ToLower(m.KpiType) + "|" + m.KpiID.String()
		imp.metricsByKpi[group] = append(imp.metricsByKpi[group], m)
		if code := strings.ToLower(strings.TrimSpace(m.MetricCode)); code != "" {
			imp.metricByCode[group+"|"+code] = m
		}
		if name := strings.ToLower(strings.TrimSpace(m.Name)); name != "" {
			imp.metricByName[group+"|"+name] = m
		}
	}

	// Every version is loaded: a key whose v1 is superseded is represented
	// by its live revision, not reported as missing or as the stale v1.
	var targets []models.KpiAnnualTarget
	if err := db.Order("version").Find(&targets).Error; err != nil {
		return err
	}
	for i := range targets {
		t := &targets[i]
		imp.allTargets = append(imp.allTargets, t)
		key := kpiTargetIdentity(t.KpiType, t.KpiCode, t.TargetYear, t.PeriodCode, t.MetricID, t.OrganizationID, KpiTargetSegmentationKey(t.SegmentationValues))
		if cur, ok := imp.existingTarget[key]; !ok || liveTargetBefore(t, cur) {
			imp.existingTarget[key] = t
		}
	}
	return nil
}

// liveTargetBefore reports whether a should represent its identity instead
// of b: a non-superseded version wins, then the higher version.
func liveTargetBefore(a, b *models.KpiAnnualTarget) bool {
	aSup, bSup := a.TargetStatus == models.KpiTargetStatusSuperseded, b.TargetStatus == models.KpiTargetStatusSuperseded
	if aSup != bSup {
		return bSup
	}
	return a.Version > b.Version
}

// validateMilestoneYears keeps a Milestone's dates inside the target year range.
func validateMilestoneYears(start, end time.Time) error {
	if err := ValidateKpiTargetYear(start.Year()); err != nil {
		return fmt.Errorf("Period Start year %d is outside %d–%d", start.Year(), KpiTargetMinYear, KpiTargetMaxYear)
	}
	if err := ValidateKpiTargetYear(end.Year()); err != nil {
		return fmt.Errorf("Period End year %d is outside %d–%d", end.Year(), KpiTargetMinYear, KpiTargetMaxYear)
	}
	return nil
}

// ValidateMilestoneYears is validateMilestoneYears for the Targets API.
func ValidateMilestoneYears(start, end time.Time) error { return validateMilestoneYears(start, end) }

// kpiTargetIdentity is a target's BR-01 key without Version: KPI, year,
// period, metric, organization and segment.
func kpiTargetIdentity(kpiType, kpiCode string, year int, periodCode string, metricID, orgID *uuid.UUID, segment string) string {
	metric := ""
	if metricID != nil {
		metric = metricID.String()
	}
	org := ""
	if orgID != nil {
		org = orgID.String()
	}
	return strings.ToLower(strings.TrimSpace(kpiType)) + "|" +
		strings.ToLower(strings.TrimSpace(kpiCode)) + "|" +
		strconv.Itoa(year) + "|" +
		strings.ToLower(strings.TrimSpace(periodCode)) + "|" +
		metric + "|" + org + "|" + segment
}

// canManageTargets checks the importing user's per-KPI access, the same rule
// the Targets API route applies (KpiActionManageTargets; the Super Admin
// passes). Returns "" when allowed, else the rejection message.
func (imp *kpiTargetsImporter) canManageTargets(ek *existingKpi) string {
	key := ek.Type + "|" + strings.ToUpper(ek.Code)
	if imp.createdKpis[ek.Type+"|"+ek.ID.String()] {
		return "" // created by this workbook import
	}
	if imp.access == nil {
		imp.access = NewKpiAccessService(imp.db)
		imp.accessCache = map[string]string{}
	}
	if why, ok := imp.accessCache[key]; ok {
		return why
	}
	why := ""
	allowed, err := imp.access.Can(imp.ctx, imp.userID, ek.Type, ek.ID, KpiActionManageTargets)
	switch {
	case err != nil:
		why = fmt.Sprintf("Your access to KPI %s could not be checked; try again.", ek.Code)
	case !allowed:
		why = fmt.Sprintf("You are not allowed to manage targets of KPI %s — only its KPI Owner (or the Super Admin) can.", ek.Code)
	}
	imp.accessCache[key] = why
	return why
}

// ─── Row validation ─────────────────────────────────────────────────────────

func (imp *kpiTargetsImporter) validateRows() {
	accepted := map[string]int{}
	var acceptedMilestones []*kpiTargetImportRec
	for _, r := range imp.recs {
		if r.Rejected {
			continue
		}
		code := strings.ToUpper(strings.TrimSpace(r.KpiCode))
		if r.KpiType == "" {
			var types []string
			for _, typ := range []string{models.KPITypeStrategic, models.KPITypeOperational, models.KPITypeAward} {
				if imp.existingKpis[typ+"|"+code] != nil {
					types = append(types, typ)
				}
			}
			switch len(types) {
			case 0:
				r.reject("KPI", fmt.Sprintf("KPI %s was not found. Import the KPI Dictionary first or pick a KPI from the list.", r.KpiCode))
				continue
			case 1:
				r.KpiType = types[0]
			default:
				r.reject("KPI", fmt.Sprintf("KPI code %s is used by %s KPIs — add a KPI Type column to choose one.", r.KpiCode, strings.Join(types, " and ")))
				continue
			}
		}
		kpiKey := r.KpiType + "|" + code
		ek := imp.existingKpis[kpiKey]
		if ek == nil {
			if other, ok := imp.kpiByCode[code]; ok {
				r.reject("KPI Code", fmt.Sprintf("KPI %s exists as %s, not %s — check the KPI Type column.", r.KpiCode, other, r.KpiType))
			} else {
				r.reject("KPI Code", fmt.Sprintf("KPI %s (%s) was not found. Import the KPI Dictionary first or correct the KPI Code and KPI Type.", r.KpiCode, r.KpiType))
			}
			continue
		}
		r.KpiID = ek.ID
		r.Target.KpiCode = ek.Code
		r.Target.KpiType = ek.Type
		if why := imp.canManageTargets(ek); why != "" {
			r.reject("KPI Code", why)
			continue
		}

		group := r.KpiType + "|" + r.KpiID.String()
		metric, metricErr := imp.resolveMetric(r, group)
		if metricErr != "" {
			r.reject("Metric", metricErr)
			continue
		}
		if metric != nil {
			r.MetricID = &metric.ID
			r.MetricName = metric.Name
			r.Target.MetricID = &metric.ID
			r.Target.CalculationTypeSnapshot = metric.CalculationType
			r.Target.DirectionSnapshot = metric.Direction
			r.Target.UnitSnapshot = metric.Unit
			r.Target.DecimalPrecisionSnapshot = metric.DecimalPrecision
			r.Target.AggregationMethodSnapshot = metric.AggregationMethod
			r.Target.ReportingFrequencySnapshot = metric.ReportingFrequency
		}

		db := imp.db.WithContext(imp.ctx)
		frequency := GetKPIReportingFrequency(db, ek.Code, ek.Type)
		if err := ValidateKpiTargetPeriod(metric, frequency, r.Target.TargetType, r.Target.TargetYear, r.Target.PeriodCode); err != nil {
			r.reject("Period Code", err.Error())
			continue
		}
		if r.Target.TargetType != models.KpiTargetTypeMilestone {
			// Period dates always follow the period itself.
			if start, end, ok := PeriodDateRange(r.Target.TargetYear, r.Target.PeriodCode); ok {
				r.Target.PeriodStart, r.Target.PeriodEnd = &start, &end
			}
		}

		direction := KpiTargetDirection(db, ek.Code, ek.Type, metric)
		if !r.Set["target_value"] && KpiTargetValueRequired(direction) {
			r.reject("Target Value", "Target Value is required.")
			continue
		}

		identity := kpiTargetIdentity(ek.Type, ek.Code, r.Target.TargetYear, r.Target.PeriodCode, r.MetricID, nil, "")
		if row, dup := accepted[identity]; dup {
			r.reject("Period Code", fmt.Sprintf("Duplicate target in this file — row %d targets the same KPI, metric and period.", row))
			continue
		}

		existing := imp.existingTarget[identity]
		if r.Target.TargetType == models.KpiTargetTypeMilestone {
			if other := imp.milestoneOverlap(r, existing, acceptedMilestones); other != nil {
				if other.Row > 0 {
					r.reject("Period Start", fmt.Sprintf("This date range overlaps row %d, which targets the same KPI and metric.", other.Row))
				} else {
					r.reject("Period Start", fmt.Sprintf("This date range overlaps an existing Milestone / Ad Hoc target (%s, status '%s').",
						FormatPeriodLabel(other.Target.PeriodCode, other.Target.TargetYear), other.Target.TargetStatus))
				}
				continue
			}
		}

		if existing != nil {
			r.Existing = existing
			if imp.opts.ExistingMode == KpiImportExistingUpdate {
				switch existing.TargetStatus {
				case "draft", "returned", "rejected":
					// Editable, matching UpdateTarget's rule.
				default:
					r.reject("Target Status", fmt.Sprintf("The existing target is %s and cannot be updated through import. Only draft, returned or rejected targets are editable.", existing.TargetStatus))
					continue
				}
				imp.preserveExisting(r, existing)
			}
		}
		if !r.Rejected {
			imp.normalizeThresholds(r, direction)
		}
		if !r.Rejected {
			accepted[identity] = r.Row
			if r.Target.TargetType == models.KpiTargetTypeMilestone {
				acceptedMilestones = append(acceptedMilestones, r)
			}
		}
	}
}

// normalizeThresholds applies the shared threshold rules to the (merged) row
// and warns about values its threshold mode does not use.
func (imp *kpiTargetsImporter) normalizeThresholds(r *kpiTargetImportRec, direction string) {
	fields := []string{"Excellent Threshold", "Achieved Threshold", "Warning Threshold", "Target Range Min", "Target Range Max"}
	values := func() []*float64 {
		return []*float64{r.Target.ExcellentThreshold, r.Target.AchievedThreshold, r.Target.WarningThreshold, r.Target.TargetRangeMin, r.Target.TargetRangeMax}
	}
	keys := []string{"excellent_threshold", "achieved_threshold", "warning_threshold", "target_range_min", "target_range_max"}
	before := values()
	if err := NormalizeKpiTargetThresholds(r.Target, direction); err != nil {
		r.reject("Threshold Mode", err.Error()+".")
		return
	}
	after := values()
	var dropped []string
	for i := range fields {
		if before[i] != nil && after[i] == nil && r.Set[keys[i]] {
			dropped = append(dropped, fields[i])
		}
	}
	if len(dropped) > 0 {
		imp.warn(imp.sheet, r.Row, r.recordID(), "Threshold Mode",
			fmt.Sprintf("%s ignored: Threshold Mode %q does not use them.", strings.Join(dropped, ", "), r.Target.ThresholdMode))
	}
}

// milestoneOverlap checks existing and in-file Milestone / Ad Hoc ranges for
// the same KPI and metric, mirroring the Targets API overlap rule.
func (imp *kpiTargetsImporter) milestoneOverlap(r *kpiTargetImportRec, matched *models.KpiAnnualTarget, accepted []*kpiTargetImportRec) *kpiTargetImportRec {
	if r.Target.PeriodStart == nil || r.Target.PeriodEnd == nil {
		return nil
	}
	for _, other := range imp.allTargets {
		if matched != nil && other.ID == matched.ID {
			continue
		}
		if other.OrganizationID != nil || KpiTargetSegmentationKey(other.SegmentationValues) != "" ||
			!imp.sameTargetScope(r, other.KpiType, other.KpiCode, other.MetricID) {
			continue
		}
		if other.TargetStatus == "rejected" || other.TargetStatus == "superseded" {
			continue
		}
		if !IsCustomPeriodCode(other.PeriodCode) || other.PeriodStart == nil || other.PeriodEnd == nil {
			continue
		}
		if targetRangesOverlap(r.Target.PeriodStart, r.Target.PeriodEnd, other.PeriodStart, other.PeriodEnd) {
			return &kpiTargetImportRec{Target: other}
		}
	}
	for _, other := range accepted {
		if !imp.sameTargetScope(r, other.Target.KpiType, other.Target.KpiCode, other.MetricID) {
			continue
		}
		if targetRangesOverlap(r.Target.PeriodStart, r.Target.PeriodEnd, other.Target.PeriodStart, other.Target.PeriodEnd) {
			return other
		}
	}
	return nil
}

func (imp *kpiTargetsImporter) sameTargetScope(r *kpiTargetImportRec, kpiType, kpiCode string, metricID *uuid.UUID) bool {
	if !strings.EqualFold(strings.TrimSpace(kpiType), r.Target.KpiType) ||
		!strings.EqualFold(strings.TrimSpace(kpiCode), r.Target.KpiCode) {
		return false
	}
	if metricID == nil || r.MetricID == nil {
		return metricID == nil && r.MetricID == nil
	}
	return *metricID == *r.MetricID
}

func targetRangesOverlap(aStart, aEnd, bStart, bEnd *time.Time) bool {
	if aStart == nil || aEnd == nil || bStart == nil || bEnd == nil {
		return false
	}
	return !aEnd.Before(*bStart) && !aStart.After(*bEnd)
}

func (imp *kpiTargetsImporter) resolveMetric(r *kpiTargetImportRec, group string) (*models.KpiMetric, string) {
	codeRaw := r.MetricCode
	nameRaw := r.MetricName
	codeKey := strings.ToLower(strings.TrimSpace(codeRaw))
	nameKey := strings.ToLower(strings.TrimSpace(nameRaw))
	var byCode, byName *models.KpiMetric
	if codeKey != "" {
		byCode = imp.metricByCode[group+"|"+codeKey]
		if byCode == nil {
			return nil, fmt.Sprintf("Metric code %q was not found under KPI %s.", codeRaw, r.KpiCode)
		}
	}
	if nameKey != "" {
		byName = imp.metricByName[group+"|"+nameKey]
		if byName == nil {
			return nil, fmt.Sprintf("Metric name %q was not found under KPI %s.", nameRaw, r.KpiCode)
		}
	}
	if v := strings.TrimSpace(r.Metric); v != "" {
		m := imp.metricByName[group+"|"+strings.ToLower(v)]
		if m == nil {
			m = imp.metricByCode[group+"|"+strings.ToLower(v)]
		}
		if m == nil {
			return nil, fmt.Sprintf("Metric %q was not found under KPI %s.", v, r.KpiCode)
		}
		if (byCode != nil && byCode.ID != m.ID) || (byName != nil && byName.ID != m.ID) {
			return nil, fmt.Sprintf("Metric %q does not match the Metric Code / Metric Name columns.", v)
		}
		return m, ""
	}
	if byCode == nil && byName == nil {
		// No metric given: the KPI's only metric, as the form would preselect.
		switch metrics := imp.metricsByKpi[group]; len(metrics) {
		case 1:
			return metrics[0], ""
		case 0:
			return nil, ""
		default:
			return nil, fmt.Sprintf("KPI %s has %d metrics — choose the Metric.", r.KpiCode, len(metrics))
		}
	}
	if byCode != nil && byName != nil && byCode.ID != byName.ID {
		return nil, fmt.Sprintf("Metric code %q and metric name %q identify different metrics; fill only one or make them match.", codeRaw, nameRaw)
	}
	if byCode != nil {
		return byCode, ""
	}
	return byName, ""
}

// preserveExisting copies blank optional fields from the existing target.
func (imp *kpiTargetsImporter) preserveExisting(r *kpiTargetImportRec, existing *models.KpiAnnualTarget) {
	if !r.Set["target_basis"] {
		r.Target.TargetBasis = existing.TargetBasis
	}
	if !r.Set["target_rationale"] {
		r.Target.TargetRationale = existing.TargetRationale
	}
	if !r.Set["threshold_mode"] {
		r.Target.ThresholdMode = existing.ThresholdMode
	}
	if !r.Set["excellent_threshold"] {
		r.Target.ExcellentThreshold = existing.ExcellentThreshold
	}
	if !r.Set["achieved_threshold"] {
		r.Target.AchievedThreshold = existing.AchievedThreshold
	}
	if !r.Set["warning_threshold"] {
		r.Target.WarningThreshold = existing.WarningThreshold
	}
	if !r.Set["target_range_min"] {
		r.Target.TargetRangeMin = existing.TargetRangeMin
	}
	if !r.Set["target_range_max"] {
		r.Target.TargetRangeMax = existing.TargetRangeMax
	}
	if !r.Set["target_status"] {
		r.Target.TargetStatus = existing.TargetStatus
	}
	if !r.Set["effective_from"] {
		r.Target.EffectiveFrom = existing.EffectiveFrom
	}
	if !r.Set["effective_to"] {
		r.Target.EffectiveTo = existing.EffectiveTo
	}
	if r.Target.TargetType != "Milestone / Ad Hoc" {
		if !r.Set["period_start"] {
			r.Target.PeriodStart = existing.PeriodStart
		}
		if !r.Set["period_end"] {
			r.Target.PeriodEnd = existing.PeriodEnd
		}
	}
	// A partial date update must not leave the merged range invalid.
	if r.Target.PeriodStart != nil && r.Target.PeriodEnd != nil && r.Target.PeriodEnd.Before(*r.Target.PeriodStart) {
		r.reject("Period End", "Period End cannot be before Period Start.")
	}
}

// ─── Result ─────────────────────────────────────────────────────────────────

func (imp *kpiTargetsImporter) finalize() {
	for _, r := range imp.recs {
		if !r.Rejected {
			continue
		}
		r.RejectWhy = r.reason()
		for _, issue := range r.Issues {
			imp.errorf(imp.sheet, r.Row, r.recordID(), issue.field, issue.message)
		}
	}

	existingAction := kpiImportActionSkip
	if imp.opts.ExistingMode == KpiImportExistingUpdate {
		existingAction = kpiImportActionUpdate
	}
	items := make([]KpiImportItem, 0, len(imp.recs))
	for _, r := range imp.recs {
		name := r.MetricName
		if name == "" {
			name = r.KpiCode
		}
		item := KpiImportItem{
			Category: kpiTargetsCategory,
			KpiType:  r.KpiType,
			RecordID: r.recordID(),
			Name:     name,
			Sheet:    imp.sheet,
			Row:      r.Row,
		}
		switch {
		case r.Rejected:
			r.action = kpiImportActionReject
			item.Action = kpiImportActionReject
			item.Reason = r.RejectWhy
		case r.Existing != nil:
			r.action = existingAction
			item.Action = existingAction
			if existingAction == kpiImportActionSkip {
				item.Reason = "target already exists"
			} else {
				item.Reason = "existing target will be updated"
			}
		default:
			r.action = kpiImportActionCreate
			item.Action = kpiImportActionCreate
		}
		items = append(items, item)
	}
	imp.res.Items = items
	imp.res.Categories = map[string]*KpiImportCounts{kpiTargetsCategory: {}}
	imp.res.Totals = KpiImportCounts{}
	imp.count()
	actionable := imp.res.Totals.Created + imp.res.Totals.Updated
	imp.res.CanCommit = !imp.structuralError && actionable > 0 &&
		!(imp.opts.ErrorMode == KpiImportErrorsAbort && len(imp.res.Errors) > 0)
}

func (imp *kpiTargetsImporter) count() {
	imp.res.Categories = map[string]*KpiImportCounts{kpiTargetsCategory: {}}
	imp.res.Totals = KpiImportCounts{}
	c := imp.res.Categories[kpiTargetsCategory]
	for _, it := range imp.res.Items {
		switch it.Action {
		case kpiImportActionCreate:
			c.Created++
			imp.res.Totals.Created++
		case kpiImportActionUpdate:
			c.Updated++
			imp.res.Totals.Updated++
		case kpiImportActionSkip:
			c.Skipped++
			imp.res.Totals.Skipped++
		case kpiImportActionReject:
			c.Rejected++
			imp.res.Totals.Rejected++
		case "rolled_back":
			c.RolledBack++
			imp.res.Totals.RolledBack++
		}
	}
}

func (imp *kpiTargetsImporter) structural(sheet, message string) {
	imp.structuralError = true
	if sheet == "" {
		sheet = "(workbook)"
	}
	imp.res.Errors = append(imp.res.Errors, KpiImportIssue{Severity: kpiImportSeverityError, Sheet: sheet, Message: message})
}

func (imp *kpiTargetsImporter) errorf(sheet string, row int, recordID, field, message string) {
	imp.res.Errors = append(imp.res.Errors, KpiImportIssue{
		Severity: kpiImportSeverityError,
		Sheet:    sheet,
		Row:      row,
		RecordID: recordID,
		Field:    field,
		Message:  message,
	})
}

func (imp *kpiTargetsImporter) warn(sheet string, row int, recordID, field, message string) {
	imp.res.Warnings = append(imp.res.Warnings, KpiImportIssue{
		Severity: kpiImportSeverityWarning,
		Sheet:    sheet,
		Row:      row,
		RecordID: recordID,
		Field:    field,
		Message:  message,
	})
}

func (imp *kpiTargetsImporter) markRolledBack() {
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

// ─── Choices ────────────────────────────────────────────────────────────────

var (
	kpiTargetKpiTypeChoices = []metricChoice{
		mc("strategic", "strategic kpi", "استراتيجي", "مؤشر استراتيجي"),
		mc("operational", "operational kpi", "تشغيلي", "مؤشر تشغيلي"),
		mc("award", "award kpi", "جائزة", "مؤشر جائزة"),
	}
	kpiTargetTypeChoices = []metricChoice{
		mc("Period Target", "period", "مستهدف فترة"),
		mc("Annual Target", "annual", "مستهدف سنوي"),
		mc("Milestone / Ad Hoc", "milestone", "milestone / ad hoc", "مرحلة / حسب الحاجة", "مرحلة"),
	}
	kpiTargetBasisChoices = []metricChoice{
		mc("Strategic Plan", "الخطة الاستراتيجية"),
		mc("Previous Year", "العام السابق"),
		mc("Benchmark", "المعيار المرجعي"),
		mc("Regulatory Requirement", "متطلب تنظيمي"),
		mc("Contract / SLA", "عقد / اتفاقية مستوى خدمة"),
		mc("Management Decision", "قرار إداري"),
		mc("Forecast", "توقع"),
		mc("Other", "أخرى"),
	}
	kpiTargetThresholdChoices = []metricChoice{
		mc("Use Global KPI Rules", "استخدام قواعد المؤشر العامة"),
		mc("Percentage of Target", "نسبة من المستهدف"),
		mc("Absolute Values", "قيم مطلقة"),
		mc("Target Range", "نطاق مستهدف"),
		mc("No Thresholds", "بدون حدود"),
	}
	kpiTargetStatusChoices = []metricChoice{
		mc("draft", "مسودة"),
		mc("submitted", "مقدم", "submitted for approval"),
	}
)

// ─── Shared target rules (Targets API + import) ─────────────────────────────
//
// Everything below is used by both the Targets API (handlers.SetTarget /
// UpdateTarget) and this import, so the two can never disagree on what a
// valid target is.

// Allowed Target Basis and Threshold Mode values.
var (
	KpiTargetBasisValues = []string{"Strategic Plan", "Previous Year", "Benchmark", "Regulatory Requirement",
		"Contract / SLA", "Management Decision", "Forecast", "Other"}
	KpiTargetThresholdModes = []string{kpiThresholdGlobal, kpiThresholdPercentage, kpiThresholdAbsolute,
		kpiThresholdRange, kpiThresholdNone}
)

const (
	kpiThresholdGlobal     = "Use Global KPI Rules"
	kpiThresholdPercentage = "Percentage of Target"
	kpiThresholdAbsolute   = "Absolute Values"
	kpiThresholdRange      = "Target Range"
	kpiThresholdNone       = "No Thresholds"

	// KpiTargetMinYear / KpiTargetMaxYear bound every target's year,
	// including one derived from a Milestone's dates.
	KpiTargetMinYear = 2020
	KpiTargetMaxYear = 2040

	// kpiTargetMaxPercentThreshold caps a "Percentage of Target" threshold.
	kpiTargetMaxPercentThreshold = 1000

	kpiDirectionHigher        = "Higher is Better"
	kpiDirectionLower         = "Lower is Better"
	kpiDirectionInformational = "Informational"
)

// CanonicalChoice returns the allowed value matching v case-insensitively.
func CanonicalChoice(v string, allowed []string) (string, bool) {
	v = strings.TrimSpace(v)
	for _, a := range allowed {
		if strings.EqualFold(v, a) {
			return a, true
		}
	}
	return "", false
}

// ValidateKpiTargetYear checks the 2020–2040 range with a readable message.
func ValidateKpiTargetYear(year int) error {
	if year < KpiTargetMinYear || year > KpiTargetMaxYear {
		if year == 0 {
			return fmt.Errorf("Target Year is required")
		}
		return fmt.Errorf("Target Year must be between %d and %d (got %d)", KpiTargetMinYear, KpiTargetMaxYear, year)
	}
	return nil
}

var targetBarePeriodCodes = map[string]string{
	"jan": "month", "feb": "month", "mar": "month", "apr": "month", "may": "month", "jun": "month",
	"jul": "month", "aug": "month", "sep": "month", "oct": "month", "nov": "month", "dec": "month",
	"q1": "quarter", "q2": "quarter", "q3": "quarter", "q4": "quarter",
	"h1": "semi_annual", "h2": "semi_annual",
	"annual": "annual",
}

// TargetPeriodKind returns the period type of a canonical period code
// ("month", "quarter", "semi_annual", "annual", "custom"), or "" when it is
// not a recognised code.
func TargetPeriodKind(code string) string {
	if _, _, ok := ParseCustomPeriod(code); ok {
		return "custom"
	}
	return targetBarePeriodCodes[code]
}

// CanonicalTargetPeriodCode normalises a period code to the bare lowercase
// label every downstream lookup uses ("jan".."dec", "q1".."q4", "h1"/"h2",
// "annual", or a custom "c:YYYYMMDD-YYYYMMDD" range). Year-embedded forms
// ("2026-Q3", "2026-07", "2026-H1", "2026") are reduced to their label and
// their year is returned; it must agree with year when year is non-zero, and
// is returned as the year to use when year is zero.
func CanonicalTargetPeriodCode(code string, year int) (string, int, error) {
	raw := strings.TrimSpace(code)
	c := strings.ToLower(raw)
	if c == "" {
		return "", year, fmt.Errorf("Period Code is required")
	}
	if IsCustomPeriodCode(c) {
		if _, _, ok := ParseCustomPeriod(c); !ok {
			return "", year, fmt.Errorf("Period Code %q is not a valid date range", raw)
		}
		return c, year, nil
	}
	if _, ok := targetBarePeriodCodes[c]; ok {
		return c, year, nil
	}
	m := yearEmbeddedPeriod.FindStringSubmatch(c)
	if m == nil {
		return "", year, fmt.Errorf("Period Code %q is not a recognised period (use jan..dec, q1..q4, h1/h2 or annual)", raw)
	}
	embedded, _ := strconv.Atoi(m[1])
	label := m[2]
	switch {
	case label == "":
		label = "annual"
	case label[0] >= '0' && label[0] <= '9':
		month, _ := strconv.Atoi(label)
		label = monthPeriodCodes[month-1]
	}
	if year != 0 && year != embedded {
		return "", year, fmt.Errorf("Period Code %q belongs to %d but Target Year is %d", raw, embedded, year)
	}
	return label, embedded, nil
}

// kpiTargetFrequency is the reporting frequency a target's periods follow:
// the metric's own, else the KPI's; an unknown/empty frequency is annual.
func kpiTargetFrequency(metric *models.KpiMetric, kpiFrequency string) string {
	freq := ""
	if metric != nil {
		freq = NormalizeFrequency(metric.ReportingFrequency)
	}
	if freq == "" {
		freq = NormalizeFrequency(kpiFrequency)
	}
	if freq == "" {
		freq = models.KPIFrequencyAnnually
	}
	return freq
}

// ValidateKpiTargetPeriod enforces the period rules for a canonical period
// code (see CanonicalTargetPeriodCode):
//   - Milestone / Ad Hoc ⇒ a custom date range (within the metric's validity window);
//   - Annual Target ⇒ the "annual" period, whatever the reporting frequency;
//   - Period Target ⇒ a period of the metric's (else the KPI's) frequency.
//     An Ad Hoc frequency has no reporting periods, so it only takes
//     Milestone / Ad Hoc or Annual targets.
//
// kpiFrequency is the KPI's configured reporting frequency.
func ValidateKpiTargetPeriod(metric *models.KpiMetric, kpiFrequency, targetType string, year int, periodCode string) error {
	kind := TargetPeriodKind(periodCode)
	switch targetType {
	case models.KpiTargetTypeMilestone:
		if kind != "custom" {
			return fmt.Errorf("Period Start and Period End are required for a Milestone / Ad Hoc target")
		}
		if metric != nil {
			return ValidateTargetPeriodForMetric(metric, kpiFrequency, year, periodCode)
		}
		return nil
	case models.KpiTargetTypeAnnual:
		if kind != "annual" {
			return fmt.Errorf("An Annual Target covers the whole year — its period must be Annual, not %s", FormatPeriodLabel(periodCode, year))
		}
		if metric != nil {
			// Only the validity window applies; the annual period is valid
			// for every reporting frequency.
			m := *metric
			m.ReportingFrequency = "Annual"
			return ValidateTargetPeriodForMetric(&m, models.KPIFrequencyAnnually, year, periodCode)
		}
		return nil
	}

	if kind == "custom" {
		return fmt.Errorf("Custom date ranges are only allowed for Milestone / Ad Hoc targets")
	}
	freq := kpiTargetFrequency(metric, kpiFrequency)
	subject := "This KPI"
	if metric != nil {
		subject = fmt.Sprintf("Metric %q", metric.Name)
	}
	if freq == models.KPIFrequencyCustom {
		return fmt.Errorf("%s is reported Ad Hoc and has no reporting periods — use a Milestone / Ad Hoc target (a date range) or an Annual Target", subject)
	}
	if want := targetPeriodTypes[freq]; kind != want {
		return fmt.Errorf("Period %s does not match %s, which is reported %s. Choose a %s period.",
			FormatPeriodLabel(periodCode, year), strings.ToLower(subject[:1])+subject[1:], frequencyLabel(freq), frequencyLabel(freq))
	}
	if metric != nil {
		return ValidateTargetPeriodForMetric(metric, kpiFrequency, year, periodCode)
	}
	return nil
}

// KpiTargetDirection is the direction thresholds are ordered by: the
// metric's, else the KPI's polarity.
func KpiTargetDirection(db *gorm.DB, kpiCode, kpiType string, metric *models.KpiMetric) string {
	if metric != nil && metric.Direction != "" {
		return metric.Direction
	}
	if GetKPIPolarity(db, kpiCode, kpiType) == models.KPIPolarityDescending {
		return kpiDirectionLower
	}
	return kpiDirectionHigher
}

// KpiTargetValueRequired reports whether a target needs a Target Value — every
// target except one for an Informational metric.
func KpiTargetValueRequired(direction string) bool {
	return direction != kpiDirectionInformational
}

// NormalizeKpiTargetThresholds validates the Target Basis, Threshold Mode and
// threshold values of t, and clears the values its mode does not use (the
// form hides them, so they can only be stale): Excellent/Achieved/Warning
// belong to Percentage of Target and Absolute Values, Range Min/Max to Target
// Range. Thresholds must be ordered by direction — Higher is Better:
// Excellent ≥ Achieved ≥ Warning; Lower is Better: the reverse.
func NormalizeKpiTargetThresholds(t *models.KpiAnnualTarget, direction string) error {
	if t.TargetBasis == "" {
		t.TargetBasis = "Management Decision"
	} else if v, ok := CanonicalChoice(t.TargetBasis, KpiTargetBasisValues); ok {
		t.TargetBasis = v
	} else {
		return fmt.Errorf("Target Basis %q is not valid. Allowed: %s", t.TargetBasis, strings.Join(KpiTargetBasisValues, ", "))
	}
	if t.ThresholdMode == "" {
		t.ThresholdMode = kpiThresholdGlobal
	} else if v, ok := CanonicalChoice(t.ThresholdMode, KpiTargetThresholdModes); ok {
		t.ThresholdMode = v
	} else {
		return fmt.Errorf("Threshold Mode %q is not valid. Allowed: %s", t.ThresholdMode, strings.Join(KpiTargetThresholdModes, ", "))
	}

	switch t.ThresholdMode {
	case kpiThresholdGlobal, kpiThresholdNone:
		t.ExcellentThreshold, t.AchievedThreshold, t.WarningThreshold = nil, nil, nil
		t.TargetRangeMin, t.TargetRangeMax = nil, nil
		return nil
	case kpiThresholdRange:
		t.ExcellentThreshold, t.AchievedThreshold, t.WarningThreshold = nil, nil, nil
		if t.TargetRangeMin == nil || t.TargetRangeMax == nil {
			return fmt.Errorf("Target Range Min and Target Range Max are required when Threshold Mode is Target Range")
		}
		if *t.TargetRangeMax < *t.TargetRangeMin {
			return fmt.Errorf("Target Range Max cannot be lower than Target Range Min")
		}
		if t.TargetValue < *t.TargetRangeMin || t.TargetValue > *t.TargetRangeMax {
			return fmt.Errorf("Target Value %v must lie within the Target Range (%v to %v)", t.TargetValue, *t.TargetRangeMin, *t.TargetRangeMax)
		}
		return nil
	}

	// Percentage of Target / Absolute Values.
	t.TargetRangeMin, t.TargetRangeMax = nil, nil
	named := []struct {
		name  string
		value *float64
	}{{"Excellent", t.ExcellentThreshold}, {"Achieved", t.AchievedThreshold}, {"Warning", t.WarningThreshold}}
	if t.ThresholdMode == kpiThresholdPercentage {
		for _, n := range named {
			if n.value != nil && (*n.value < 0 || *n.value > kpiTargetMaxPercentThreshold) {
				return fmt.Errorf("%s Threshold must be a percentage between 0 and %d (got %v)", n.name, kpiTargetMaxPercentThreshold, *n.value)
			}
		}
	}
	lower := direction == kpiDirectionLower
	if !lower && direction != kpiDirectionHigher {
		return nil // Target Range / Exact Target / Informational metrics have no single order
	}
	for i := 0; i < len(named); i++ {
		for j := i + 1; j < len(named); j++ {
			a, b := named[i], named[j]
			if a.value == nil || b.value == nil {
				continue
			}
			if !lower && *a.value < *b.value {
				return fmt.Errorf("For a Higher is Better target the %s Threshold (%v) cannot be below the %s Threshold (%v)", a.name, *a.value, b.name, *b.value)
			}
			if lower && *a.value > *b.value {
				return fmt.Errorf("For a Lower is Better target the %s Threshold (%v) cannot be above the %s Threshold (%v)", a.name, *a.value, b.name, *b.value)
			}
		}
	}
	return nil
}

// CanonicalKpiTargetSegmentation validates segmentation values (each needs a
// dimension and a value, one value per dimension) and returns them in a
// stable order as JSON — nil when there are none — so the same segment always
// stores and compares identically.
func CanonicalKpiTargetSegmentation(values []models.KpiTargetSegmentationValue) (datatypes.JSON, error) {
	if len(values) == 0 {
		return nil, nil
	}
	out := make([]models.KpiTargetSegmentationValue, 0, len(values))
	seen := map[string]bool{}
	for _, v := range values {
		d, val := strings.TrimSpace(v.Dimension), strings.TrimSpace(v.Value)
		if d == "" || val == "" {
			return nil, fmt.Errorf("Each segmentation value needs a dimension and a value")
		}
		if seen[strings.ToLower(d)] {
			return nil, fmt.Errorf("Segmentation dimension %q is listed more than once", d)
		}
		seen[strings.ToLower(d)] = true
		out = append(out, models.KpiTargetSegmentationValue{Dimension: d, Value: val})
	}
	sort.Slice(out, func(i, j int) bool { return strings.ToLower(out[i].Dimension) < strings.ToLower(out[j].Dimension) })
	data, err := json.Marshal(out)
	if err != nil {
		return nil, err
	}
	return datatypes.JSON(data), nil
}

// KpiTargetSegmentationKey is a stable string for a stored segmentation
// ("" when none), for comparing targets' segments in Go.
func KpiTargetSegmentationKey(raw datatypes.JSON) string {
	t := models.KpiAnnualTarget{SegmentationValues: raw}
	canon, err := CanonicalKpiTargetSegmentation(t.Segmentation())
	if err != nil || canon == nil {
		s := strings.TrimSpace(string(raw))
		if s == "" || s == "null" || s == "[]" {
			return ""
		}
		return s
	}
	return string(canon)
}

// KpiTargetScope restricts q to targets with the same identity as t — KPI,
// metric, year, period, organization and segment (BR-01 without Version) —
// comparing NULLs as equal, like the unique indexes do.
func KpiTargetScope(q *gorm.DB, t *models.KpiAnnualTarget) *gorm.DB {
	q = q.Where("kpi_code = ? AND kpi_type = ? AND target_year = ? AND period_code = ?",
		t.KpiCode, t.KpiType, t.TargetYear, t.PeriodCode)
	if t.MetricID != nil {
		q = q.Where("metric_id = ?", *t.MetricID)
	} else {
		q = q.Where("metric_id IS NULL")
	}
	if t.OrganizationID != nil {
		q = q.Where("organization_id = ?", *t.OrganizationID)
	} else {
		q = q.Where("organization_id IS NULL")
	}
	if len(t.SegmentationValues) > 0 {
		q = q.Where("segmentation_values = ?::jsonb", string(t.SegmentationValues))
	} else {
		q = q.Where("(segmentation_values IS NULL OR segmentation_values = 'null'::jsonb OR segmentation_values = '[]'::jsonb)")
	}
	return q
}

// IsUniqueViolation reports whether err is a Postgres unique-index violation.
func IsUniqueViolation(err error) bool {
	if err == nil {
		return false
	}
	if errors.Is(err, gorm.ErrDuplicatedKey) {
		return true
	}
	msg := err.Error()
	return strings.Contains(msg, "SQLSTATE 23505") || strings.Contains(msg, "duplicate key value")
}

// kpiCodeFromLabel returns the KPI code of a KPI column value: "CODE - Name"
// (the dropdown) or just the code. Codes contain no spaces.
func kpiCodeFromLabel(v string) string {
	v = strings.TrimSpace(v)
	if i := strings.IndexAny(v, " \t"); i > 0 {
		v = v[:i]
	}
	return strings.TrimRight(v, "-–—:")
}

// isCustomRangeChoice reports whether a Period value is the
// "Custom date range" option.
func isCustomRangeChoice(v string) bool {
	switch normName(strings.TrimRight(strings.TrimSpace(v), "…")) {
	case "custom date range", "custom", "custom range", "نطاق تاريخ مخصص", "مخصص":
		return true
	}
	return false
}

// defaultTargetType is the Target Type of a row that leaves it empty, as the
// template's formula fills it: a custom range is a Milestone / Ad Hoc target,
// the annual period an Annual Target, any other period a Period Target.
func defaultTargetType(customRange bool, period string) string {
	switch {
	case customRange:
		return models.KpiTargetTypeMilestone
	case normName(period) == "annual" || normName(period) == "سنوي":
		return models.KpiTargetTypeAnnual
	}
	return "Period Target"
}
