package services

// KPI Dictionary import template: one workbook for every KPI record type —
// all master data, KPI definitions, metrics, annual targets and performance
// by period. Every reference column (pillar, goal, department …) is a
// dropdown fed from the Lists sheet, which is filled at download time with
// what already exists in the system plus whatever the user adds on the
// master data sheets of the same workbook.

import (
	"context"
	"fmt"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/automax/backend/internal/models"
	"github.com/automax/backend/internal/utils"
	"gorm.io/gorm"
)

// Lists-sheet dropdown sources.
const (
	listPillarsEnablers = "List_PillarsEnablers"
	listPillars         = "List_Pillars"
	listEnablers        = "List_Enablers"
	listGoals           = "List_Goals"
	listObjectives      = "List_Objectives"
	listProcesses       = "List_Processes"
	listDepartments     = "List_Departments"
	listOrganizations   = "List_Organizations"
	listDomains         = "List_Domains"
	listCriteria        = "List_Criteria"
	listAwardMeasures   = "List_AwardMeasures"
	listDataSources     = "List_DataSources"
	listCriteriaNames   = "List_CriteriaNames"
	listSubCriteria     = "List_SubCriteria"
	listSegDimensions   = "List_SegmentationDimensions"
	listKpiCodes        = "List_KpiCodes"
	listMetricCodes     = "List_MetricCodes"
	listPeriodCodes     = "List_PeriodCodes"

	// Lookup tables for auto-filled cells (parallel columns, row for row).
	lookupOpObjName    = "Lookup_OpObjName"
	lookupOpObjParent  = "Lookup_OpObjParent"
	lookupOpObjPillar  = "Lookup_OpObjPillar"
	lookupParentName   = "Lookup_ParentName"
	lookupParentGoal   = "Lookup_ParentGoal"
	lookupParentPillar = "Lookup_ParentPillar"
	lookupKpiCode      = "Lookup_KpiCode"
	lookupKpiType      = "Lookup_KpiType"
)

// kpiTemplateFormulaRows is how many data rows get the auto-fill formulas.
const kpiTemplateFormulaRows = 1000

// lookupFormula fills a cell from a lookup table: when cell {col}{row} has a
// value found exactly once in keys, the matching entry of values ("" when
// missing or ambiguous — the user then fills it in).
func lookupFormula(col, keys, values string) string {
	return fmt.Sprintf(`IF($%[1]s{row}="","",IF(COUNTIF(%[2]s,$%[1]s{row})<>1,"",INDEX(%[3]s,MATCH($%[1]s{row},%[2]s,0))&""))`, col, keys, values)
}

// kpiTemplateCodeRows is how many rows per sheet the KPI and metric code
// dropdowns read from the Dictionary and Metrics sheets.
const kpiTemplateCodeRows = 300

// withLists returns a copy of cols with a dropdown list set on the given
// column keys (columns of the Metrics/Targets/Master Data imports that refer
// to records on other sheets).
func withLists(cols []kpiTemplateColumn, lists map[string]string) []kpiTemplateColumn {
	out := append([]kpiTemplateColumn(nil), cols...)
	for i := range out {
		if l, ok := lists[out[i].key]; ok {
			out[i].list = l
			out[i].values, out[i].valuesAr = nil, nil
		}
	}
	return out
}

// withKpiTypeFormula fills KPI Type from the KPI Code picked on the row.
func withKpiTypeFormula(cols []kpiTemplateColumn) []kpiTemplateColumn {
	code := templateColLetter(cols, "kpi_code")
	for i := range cols {
		if cols[i].key == "kpi_type" {
			cols[i].formula = lookupFormula(code, lookupKpiCode, lookupKpiType)
			cols[i].hintEn = "Filled automatically from the KPI Code."
			cols[i].hintAr = "يُعبأ تلقائياً من كود المؤشر."
		}
	}
	return cols
}

// templateColLetter is the Excel column letter of key in cols ("A" if absent).
func templateColLetter(cols []kpiTemplateColumn, key string) string {
	for i, c := range cols {
		if c.key == key {
			return string(rune('A' + i))
		}
	}
	return "A"
}

// Header colours that group the dictionary columns.
const (
	colorBasic       = "1F3864"
	colorAlignment   = "1F6F78"
	colorOwnership   = "6A3D9A"
	colorMeasurement = "2E7D32"
	colorTargets     = "B45F06"
)

// KpiDictionaryTemplateFileNameFor returns the download name for the selected
// template language.
func KpiDictionaryTemplateFileNameFor(lang string) string {
	if lang == KpiTemplateLangAR {
		return "kpi-dictionary-import-template-ar.xlsx"
	}
	return KpiDictionaryTemplateFileName
}

// BuildKpiDictionaryImportTemplate returns a ready-to-fill Excel workbook for
// the KPI Dictionary bulk import. The target year columns cover the current
// year plus the next four. lang selects a single header language (en or ar)
// — the workbook is not bilingual. The Lists sheet is filled from db.
func BuildKpiDictionaryImportTemplate(ctx context.Context, db *gorm.DB, lang string) ([]byte, error) {
	fromYear := time.Now().Year()
	toYear := fromYear + kpiTemplateYearSpan - 1
	language := utils.LanguageEnglish
	if lang == KpiTemplateLangAR {
		language = utils.LanguageArabic
	}
	lists, err := kpiTemplateLists(ctx, db, lang)
	if err != nil {
		return nil, fmt.Errorf("loading dropdown lists: %w", err)
	}

	sheets := []utils.SheetConfig{
		buildKpiMasterSheet(kpiTemplatePillarsSheet, mdPillar),
		buildKpiMasterSheet(kpiTemplateEnablersSheet, mdEnabler),
		buildKpiMasterSheet(kpiTemplateGoalsSheet, mdGoal),
		buildKpiMasterSheet(kpiTemplateObjectivesSheet, mdObjective),
		buildKpiMasterSheet(kpiTemplateProcessesSheet, mdProcess),
		buildKpiMasterSheet(kpiTemplateInitiativesSheet, mdInitiative),
	}
	// The other Master Data tabs, with the same columns as their own import;
	// a sub-criterion picks its criterion from a dropdown.
	for _, st := range kpiWorkbookMasterStages {
		spec, _ := masterSpecFor(st.entity)
		cols := withLists(spec.cols, map[string]string{"award_criterion": listCriteriaNames})
		sheets = append(sheets, utils.SheetConfig{Name: spec.sheet, Columns: toExcelColumns(cols), DataRows: kpiTemplateMasterRows, FreezeHeader: true})
	}
	metricCols := withKpiTypeFormula(withLists(markKpiMetricTemplateRequired(kpiMetricTemplateColumns()),
		map[string]string{"kpi_code": listKpiCodes, "data_source": listDataSources}))
	targetCols := withKpiTypeFormula(withLists(markKpiTargetTemplateRequired(kpiTargetTemplateColumns()),
		map[string]string{"kpi_code": listKpiCodes, "metric_code": listMetricCodes, "period_code": listPeriodCodes}))
	sheets = append(sheets,
		buildKpiDictionarySheet(kpiTemplateStrategicDictSheet, models.KPITypeStrategic, fromYear, toYear),
		buildKpiDictionarySheet(kpiTemplateOperationalDict, models.KPITypeOperational, fromYear, toYear),
		buildKpiDictionarySheet(kpiTemplateAwardDictSheet, models.KPITypeAward, fromYear, toYear),
		// Same columns as the Metrics import.
		utils.SheetConfig{Name: kpiTemplateMetricsSheet, Columns: toExcelColumns(metricCols),
			DataRows: kpiMetricsTemplateDataRows, FreezeHeader: true},
		// Same columns as the Targets import: Period, Annual and Milestone /
		// Ad Hoc targets with thresholds.
		utils.SheetConfig{Name: kpiTemplateTargetsSheet, Columns: toExcelColumns(targetCols),
			DataRows: 4999, FreezeHeader: true},
		buildKpiPerformanceSheet(),
	)
	cfg := utils.WorkbookConfig{
		Language:     language,
		Title:        "KPI Dictionary Import Template",
		Subject:      "Bulk import of the KPI Dictionary workbook",
		Creator:      "Automax",
		ActiveSheet:  kpiTemplateInstructionsSheet,
		Instructions: buildKpiDictionaryInstructions(fromYear, toYear),
		Sheets:       sheets,
		ListsSheet:   kpiTemplateListsSheet,
		Lists:        lists,
	}
	return utils.GenerateExcelTemplate(cfg)
}

// ─── Lists sheet ────────────────────────────────────────────────────────────

// kpiTemplateLists builds the dropdown sources: existing records (in the
// template language when they have an Arabic name), followed for pillars,
// enablers, goals and objectives by formulas that copy the names typed on
// the matching master data sheet.
func kpiTemplateLists(ctx context.Context, db *gorm.DB, lang string) ([]utils.ListConfig, error) {
	imp := newKpiImporter(ctx, db, "", KpiImportOptions{})
	if err := imp.loadExisting(); err != nil {
		return nil, err
	}
	var sources []models.KpiDataSource
	if err := imp.db.Where("is_active = ?", true).Find(&sources).Error; err != nil {
		return nil, err
	}

	display := func(en, ar string) string {
		if lang == KpiTemplateLangAR && strings.TrimSpace(ar) != "" {
			return strings.TrimSpace(ar)
		}
		return strings.TrimSpace(en)
	}
	names := func(kinds ...string) []string {
		var out []string
		seen := map[string]bool{}
		for _, kind := range kinds {
			var block []string
			for _, n := range imp.md[kind] {
				v := display(n.NameEn, n.NameAr)
				if v != "" && !seen[normName(v)] {
					seen[normName(v)] = true
					block = append(block, v)
				}
			}
			sort.Strings(block)
			out = append(out, block...)
		}
		return out
	}

	var criteria []string
	crit := append([]*mdNode(nil), imp.md[mdCriterion]...)
	sort.Slice(crit, func(i, j int) bool { return crit[i].CriterionNo < crit[j].CriterionNo })
	for _, c := range crit {
		label := "Criterion"
		if lang == KpiTemplateLangAR {
			label = "المعيار"
		}
		criteria = append(criteria, fmt.Sprintf("%s %d - %s", label, c.CriterionNo, display(c.NameEn, c.NameAr)))
	}
	var dataSources []string
	for _, s := range sources {
		dataSources = append(dataSources, display(s.NameEn, s.NameAr))
	}
	sort.Strings(dataSources)

	var dims []models.KpiSegmentationDimension
	if err := imp.db.Where("is_active = ?", true).Find(&dims).Error; err != nil {
		return nil, err
	}
	var segDims []string
	for _, d := range dims {
		segDims = append(segDims, display(d.NameEn, d.NameAr))
	}
	sort.Strings(segDims)

	var critNames []string
	for _, c := range crit {
		critNames = append(critNames, display(c.NameEn, c.NameAr))
	}
	var subNos []string
	for _, n := range imp.md[mdSubCriterion] {
		if n.SubNo != "" {
			subNos = append(subNos, n.SubNo)
		}
	}
	sort.Strings(subNos)

	var kpiCodes, metricCodes []string
	seenMetric := map[string]bool{}
	for _, ek := range imp.existingKpis {
		kpiCodes = append(kpiCodes, ek.Code)
		for _, m := range ek.Metrics {
			if c := strings.TrimSpace(m.MetricCode); c != "" && !seenMetric[strings.ToUpper(c)] {
				seenMetric[strings.ToUpper(c)] = true
				metricCodes = append(metricCodes, c)
			}
		}
	}
	sort.Strings(kpiCodes)
	sort.Strings(metricCodes)

	// Lookup tables: operational objective → parent objective, pillar (its
	// own, else the parent's; KPIs link to a pillar only); parent objective
	// → strategic goal, pillar; KPI code → KPI type.
	nodeName := func(n *mdNode) string {
		if n == nil {
			return ""
		}
		return display(n.NameEn, n.NameAr)
	}
	firstName := func(nodes ...*mdNode) string {
		for _, n := range nodes {
			if v := nodeName(n); v != "" {
				return v
			}
		}
		return ""
	}
	var opName, opParent, opPillar []string
	for _, n := range imp.md[mdProcess] {
		var pp *mdNode
		if n.Objective != nil {
			pp = n.Objective.Pillar
		}
		opName = append(opName, nodeName(n))
		opParent = append(opParent, nodeName(n.Objective))
		opPillar = append(opPillar, firstName(n.Pillar, pp)) // KPIs link to a pillar only
	}
	var parName, parGoal, parPillar []string
	for _, n := range imp.md[mdObjective] {
		parName = append(parName, nodeName(n))
		parGoal = append(parGoal, nodeName(n.Goal))
		parPillar = append(parPillar, firstName(n.Pillar))
	}
	var kCode, kType []string
	for _, ek := range imp.existingKpis {
		kCode = append(kCode, ek.Code)
		kType = append(kType, ek.Type)
	}
	// Rows typed on the master sheets of this workbook (Name in column A).
	idxCell := func(sheet, col, idx string) string {
		return fmt.Sprintf(`INDEX('%s'!$%s:$%s,%s)`, sheet, col, col, idx)
	}
	opSheet, parSheet := kpiTemplateProcessesSheet, kpiTemplateObjectivesSheet
	opSheetName := sheetRowFormulas(kpiTemplateMasterRows, opSheet, "A", func(i string) string { return idxCell(opSheet, "A", i) + `&""` })
	opSheetParent := sheetRowFormulas(kpiTemplateMasterRows, opSheet, "A", func(i string) string { return idxCell(opSheet, "B", i) + `&""` })
	opSheetPillar := sheetRowFormulas(kpiTemplateMasterRows, opSheet, "A", func(i string) string {
		c, b := idxCell(opSheet, "C", i), idxCell(opSheet, "B", i)
		return fmt.Sprintf(`IF(%[1]s<>"",%[1]s&"",IFERROR(INDEX(%[3]s,MATCH(%[2]s,%[4]s,0))&"",""))`, c, b, lookupParentPillar, lookupParentName)
	})
	parSheetName := sheetRowFormulas(kpiTemplateMasterRows, parSheet, "A", func(i string) string { return idxCell(parSheet, "A", i) + `&""` })
	parSheetGoal := sheetRowFormulas(kpiTemplateMasterRows, parSheet, "A", func(i string) string { return idxCell(parSheet, "B", i) + `&""` })
	parSheetPillar := sheetRowFormulas(kpiTemplateMasterRows, parSheet, "A", func(i string) string {
		return idxCell(parSheet, "C", i) + `&""` // pillar only
	})
	var kSheetCode, kSheetType []string
	for _, d := range []struct{ sheet, kpiType string }{
		{kpiTemplateStrategicDictSheet, models.KPITypeStrategic},
		{kpiTemplateOperationalDict, models.KPITypeOperational},
		{kpiTemplateAwardDictSheet, models.KPITypeAward},
	} {
		sheet, kpiType := d.sheet, d.kpiType
		kSheetCode = append(kSheetCode, sheetRowFormulas(kpiTemplateCodeRows, sheet, "A", func(i string) string { return idxCell(sheet, "A", i) + `&""` })...)
		kSheetType = append(kSheetType, sheetRowFormulas(kpiTemplateCodeRows, sheet, "A", func(i string) string { return fmt.Sprintf(`IF(%s>0,"%s","")`, i, kpiType) })...)
	}

	critLabel := "Criterion"
	if lang == KpiTemplateLangAR {
		critLabel = "المعيار"
	}
	critSpec, _ := masterSpecFor(KpiMasterAwardCriterion)
	subSpec, _ := masterSpecFor(KpiMasterAwardSubCriterion)
	metricTplCols := kpiMetricTemplateColumns()

	text := func(en, ar string) utils.LocalizedText { return utils.LocalizedText{EN: en, AR: ar} }
	rows := kpiTemplateMasterRows
	return []utils.ListConfig{
		{Name: listPillarsEnablers, Title: text("Pillars & Enablers", "الركائز والممكنات"), Values: names(mdPillar, mdEnabler),
			Formulas: sheetNamesFormulas(rows, kpiTemplatePillarsSheet, kpiTemplateEnablersSheet)},
		{Name: listPillars, Title: text("Pillars", "الركائز"), Values: names(mdPillar), Formulas: sheetNamesFormulas(rows, kpiTemplatePillarsSheet)},
		{Name: listEnablers, Title: text("Enablers", "الممكنات"), Values: names(mdEnabler), Formulas: sheetNamesFormulas(rows, kpiTemplateEnablersSheet)},
		{Name: listGoals, Title: text("Strategic Goals", "الأهداف الاستراتيجية"), Values: names(mdGoal), Formulas: sheetNamesFormulas(rows, kpiTemplateGoalsSheet)},
		{Name: listObjectives, Title: text("Parent Objectives", "الأهداف الرئيسية"), Values: names(mdObjective), Formulas: sheetNamesFormulas(rows, kpiTemplateObjectivesSheet)},
		{Name: listProcesses, Title: text("Operational Objectives", "الأهداف التشغيلية"), Values: names(mdProcess), Formulas: sheetNamesFormulas(rows, kpiTemplateProcessesSheet)},
		{Name: listDepartments, Title: text("Departments", "الإدارات"), Values: names(mdDepartment)},
		{Name: listOrganizations, Title: text("Organizations", "المنظمات"), Values: names(mdOrganization), Formulas: masterNamesFormulas(rows, KpiMasterOrganization)},
		{Name: listDomains, Title: text("Domains", "المجالات"), Values: names(mdDomain), Formulas: masterNamesFormulas(rows, KpiMasterDomain)},
		{Name: listCriteria, Title: text("Award Criteria", "معايير الجائزة"), Values: criteria,
			Formulas: sheetRowFormulas(rows, critSpec.sheet, templateColLetter(critSpec.cols, "name_en"), func(idx string) string {
				return fmt.Sprintf(`"%s "&INDEX('%[2]s'!$%[3]s:$%[3]s,%[5]s)&" - "&INDEX('%[2]s'!$%[4]s:$%[4]s,%[5]s)`,
					critLabel, critSpec.sheet, templateColLetter(critSpec.cols, "criterion_no"), templateColLetter(critSpec.cols, "name_en"), idx)
			})},
		{Name: listCriteriaNames, Title: text("Award Criterion Names", "أسماء معايير الجائزة"), Values: critNames, Formulas: masterNamesFormulas(rows, KpiMasterAwardCriterion)},
		{Name: listSubCriteria, Title: text("Award Sub-criterion Numbers", "أرقام المعايير الفرعية"), Values: subNos,
			Formulas: sheetNamesFormulasIn(rows, templateColLetter(subSpec.cols, "sub_no"), subSpec.sheet)},
		{Name: listSegDimensions, Title: text("Segmentation Dimensions", "أبعاد التجزئة"), Values: segDims, Formulas: masterNamesFormulas(rows, KpiMasterSegmentationDimension)},
		{Name: listKpiCodes, Title: text("KPI Codes", "أكواد المؤشرات"), Values: kpiCodes,
			Formulas: sheetNamesFormulas(kpiTemplateCodeRows, kpiTemplateStrategicDictSheet, kpiTemplateOperationalDict, kpiTemplateAwardDictSheet)},
		{Name: listMetricCodes, Title: text("Metric Codes", "أكواد المقاييس"), Values: metricCodes,
			Formulas: sheetNamesFormulasIn(kpiTemplateCodeRows, templateColLetter(metricTplCols, "metric_code"), kpiTemplateMetricsSheet)},
		{Name: lookupOpObjName, Title: text("Lookup: Operational Objective", "بحث: الهدف التشغيلي"), Values: opName, Formulas: opSheetName, Lookup: true},
		{Name: lookupOpObjParent, Title: text("Lookup: its Parent Objective", "بحث: الهدف الرئيسي"), Values: opParent, Formulas: opSheetParent, Lookup: true},
		{Name: lookupOpObjPillar, Title: text("Lookup: its Pillar / Enabler", "بحث: الركيزة / الممكن"), Values: opPillar, Formulas: opSheetPillar, Lookup: true},
		{Name: lookupParentName, Title: text("Lookup: Parent Objective", "بحث: الهدف الرئيسي"), Values: parName, Formulas: parSheetName, Lookup: true},
		{Name: lookupParentGoal, Title: text("Lookup: its Strategic Goal", "بحث: الهدف الاستراتيجي"), Values: parGoal, Formulas: parSheetGoal, Lookup: true},
		{Name: lookupParentPillar, Title: text("Lookup: its Pillar / Enabler", "بحث: الركيزة / الممكن"), Values: parPillar, Formulas: parSheetPillar, Lookup: true},
		{Name: lookupKpiCode, Title: text("Lookup: KPI Code", "بحث: كود المؤشر"), Values: kCode, Formulas: kSheetCode, Lookup: true},
		{Name: lookupKpiType, Title: text("Lookup: its KPI Type", "بحث: نوع المؤشر"), Values: kType, Formulas: kSheetType, Lookup: true},
		{Name: listPeriodCodes, Title: text("Period Codes", "رموز الفترات"),
			Values: []string{"jan", "feb", "mar", "apr", "may", "jun", "jul", "aug", "sep", "oct", "nov", "dec", "q1", "q2", "q3", "q4", "h1", "h2", "annual"}},
		{Name: listAwardMeasures, Title: text("Award KPIs and Measures", "مؤشرات ومقاييس الجائزة"), Values: names(mdAwardMeasure), Formulas: masterNamesFormulas(rows, KpiMasterAwardKpiMeasure)},
		{Name: listDataSources, Title: text("Data Sources", "مصادر البيانات"), Values: dataSources, Formulas: masterNamesFormulas(rows, KpiMasterDataSource)},
	}, nil
}

// kpiSystemLists is the Lists sheet of a single-sheet import template
// (Master Data tab, Metrics, Targets): the same dropdown and lookup sources,
// filled only with what exists in the system — there are no other sheets
// in the file to read new names from.
func kpiSystemLists(ctx context.Context, db *gorm.DB, lang string) ([]utils.ListConfig, error) {
	lists, err := kpiTemplateLists(ctx, db, lang)
	if err != nil {
		return nil, err
	}
	for i := range lists {
		lists[i].Formulas = nil
	}
	return lists, nil
}

// masterNamesFormulas lists the names typed on a Master Data import sheet
// of the workbook (its English name column).
func masterNamesFormulas(rows int, entity string) []string {
	spec, _ := masterSpecFor(entity)
	col := "A"
	for i, c := range spec.cols {
		if c.key == "name_en" {
			col = string(rune('A' + i))
		}
	}
	return sheetNamesFormulasIn(rows, col, spec.sheet)
}

// sheetNamesFormulas returns one formula per row that lists the non-empty
// names in column A of the given sheets, in order and without gaps (the
// k-th formula returns the k-th name, or "" past the end). AGGREGATE(15,6,…)
// is SMALL that skips the #DIV/0! of empty cells, without an array formula.
func sheetNamesFormulas(rows int, sheets ...string) []string {
	return sheetNamesFormulasIn(rows, "A", sheets...)
}

// sheetRowFormulas returns one formula per row for the k-th non-empty row
// of keyCol on sheet; render builds the value from that row number (e.g.
// "Criterion "&number&" - "&name).
func sheetRowFormulas(rows int, sheet, keyCol string, render func(idx string) string) []string {
	last := rows + 1
	out := make([]string, 0, rows)
	for k := 1; k <= rows; k++ {
		idx := fmt.Sprintf(`_xlfn.AGGREGATE(15,6,ROW('%[1]s'!$%[2]s$2:$%[2]s$%[3]d)/('%[1]s'!$%[2]s$2:$%[2]s$%[3]d<>""),%[4]d)`, sheet, keyCol, last, k)
		out = append(out, fmt.Sprintf(`IFERROR(%s,"")`, render(idx)))
	}
	return out
}

// sheetNamesFormulasIn is sheetNamesFormulas for name column col.
func sheetNamesFormulasIn(rows int, col string, sheets ...string) []string {
	last := rows + 1
	count := func(s string) string {
		return fmt.Sprintf(`SUMPRODUCT(--('%[1]s'!$%[3]s$2:$%[3]s$%[2]d<>""))`, s, last, col)
	}
	nth := func(s, k string) string {
		return fmt.Sprintf(`INDEX('%[1]s'!$%[4]s:$%[4]s,_xlfn.AGGREGATE(15,6,ROW('%[1]s'!$%[4]s$2:$%[4]s$%[2]d)/('%[1]s'!$%[4]s$2:$%[4]s$%[2]d<>""),%[3]s))`, s, last, k, col)
	}
	var out []string
	for k := 1; k <= rows*len(sheets); k++ {
		// IF(k<=c0, nth(s0,k), IF(k<=c0+c1, nth(s1,k-(c0)), …)) where ci is
		// the number of names on sheet i.
		var build func(i int, before []string) string
		build = func(i int, before []string) string {
			idx := strconv.Itoa(k)
			if len(before) > 0 {
				idx = fmt.Sprintf("%d-(%s)", k, strings.Join(before, "+"))
			}
			cur := nth(sheets[i], idx)
			if i == len(sheets)-1 {
				return cur
			}
			upTo := append(append([]string(nil), before...), count(sheets[i]))
			return fmt.Sprintf("IF(%d<=%s,%s,%s)", k, strings.Join(upTo, "+"), cur, build(i+1, upTo))
		}
		out = append(out, fmt.Sprintf(`IFERROR(%s,"")`, build(0, nil)))
	}
	return out
}

// ─── Sheets ─────────────────────────────────────────────────────────────────

func buildKpiMasterSheet(name, kind string) utils.SheetConfig {
	nameCol := kpiTemplateColumn{key: "md_name", en: "Name", ar: "الاسم", width: 46, required: true,
		hintEn: "English or Arabic name. One record per row, no blank rows in between.",
		hintAr: "الاسم بالإنجليزية أو بالعربية. سجل واحد في كل صف دون صفوف فارغة بينها."}
	ref := func(key, en, ar, list string, required bool) kpiTemplateColumn {
		return kpiTemplateColumn{key: key, en: en, ar: ar, width: 40, required: required, list: list}
	}
	// The columns and their dependencies follow the Master Data forms.
	cols := markTemplateRequired(masterSheetColumns(kind), []kpiTemplateColumn{nameCol})
	switch kind {
	case mdPillar, mdEnabler:
		cols = append(cols, ref("md_owner", "Owner Department", "الإدارة المالكة", listDepartments, false))
	case mdObjective:
		cols = append(cols,
			ref("md_goal", "Strategic Goal", "الهدف الاستراتيجي", listGoals, true),
			ref("md_pillar", "Pillar", "الركيزة", listPillars, false),
			ref("md_enabler", "Enabler", "الممكن", listEnablers, false))
	case mdProcess:
		parentCol := ref("md_parent", "Parent Objective", "الهدف الرئيسي", listObjectives, true)
		parentCol.hintEn, parentCol.hintAr = "The strategic goal is taken from the parent objective.", "يُؤخذ الهدف الاستراتيجي من الهدف الرئيسي."
		cols = append(cols, parentCol,
			ref("md_pillar", "Pillar", "الركيزة", listPillars, false),
			ref("md_enabler", "Enabler", "الممكن", listEnablers, false))
	case mdInitiative:
		goal := ref("md_goal", "Strategic Goal", "الهدف الاستراتيجي", listGoals, true)
		goal.hintEn, goal.hintAr = "Filled automatically when a Parent Objective is picked.", "يُعبأ تلقائياً عند اختيار الهدف الرئيسي."
		goal.formula = lookupFormula("C", lookupParentName, lookupParentGoal) // C = Parent Objective
		cols = append(cols,
			goal,
			ref("md_parent", "Parent Objective", "الهدف الرئيسي", listObjectives, false),
			ref("md_pillar", "Pillar", "الركيزة", listPillars, false),
			ref("md_enabler", "Enabler", "الممكن", listEnablers, false),
			ref("md_owner", "Initiative Owner", "مالك المبادرة", listDepartments, false))
	}
	return utils.SheetConfig{Name: name, Columns: toExcelColumns(cols), DataRows: kpiTemplateMasterRows, FreezeHeader: true}
}

// markTemplateRequired adds the importer's required flags to template columns.
func markTemplateRequired(specs []colSpec, cols []kpiTemplateColumn) []kpiTemplateColumn {
	required := make(map[string]bool, len(specs))
	for _, s := range specs {
		required[s.key] = s.required
	}
	for i := range cols {
		cols[i].required = cols[i].required || required[cols[i].key]
	}
	return cols
}

func buildKpiDictionarySheet(name, kpiType string, fromYear, toYear int) utils.SheetConfig {
	cols := markTemplateRequired(dictColumns(kpiType),
		append(kpiTemplateDictionaryColumns(kpiType), kpiTemplateAnnualTargetColumns(fromYear, toYear)...))
	return utils.SheetConfig{Name: name, Columns: toExcelColumns(cols), DataRows: 4999, FreezeHeader: true}
}

func buildKpiPerformanceSheet() utils.SheetConfig {
	periods := []string{"Jan", "Feb", "Mar", "Apr", "May", "Jun", "Jul", "Aug", "Sep", "Oct", "Nov", "Dec", "Q1", "Q2", "Q3", "Q4", "H1", "H2", "Annual"}
	periodsAr := []string{"يناير", "فبراير", "مارس", "أبريل", "مايو", "يونيو", "يوليو", "أغسطس", "سبتمبر", "أكتوبر", "نوفمبر", "ديسمبر",
		"الربع 1", "الربع 2", "الربع 3", "الربع 4", "النصف 1", "النصف 2", "سنوي"}
	cols := markTemplateRequired(perfRowColumns(), []kpiTemplateColumn{
		{key: "code", en: "Code", ar: "الكود", width: 20, list: listKpiCodes,
			hintEn: "KPI code from a Dictionary sheet or one already in the system.",
			hintAr: "كود المؤشر من إحدى أوراق الدليل أو مؤشر موجود في النظام."},
		{key: "metric_code", en: "Metric Code", ar: "كود المقياس", width: 18, list: listMetricCodes,
			hintEn: "Only when the KPI has more than one metric (Metrics sheet or system).",
			hintAr: "فقط إذا كان للمؤشر أكثر من مقياس (ورقة المقاييس أو النظام)."},
		{key: "year", en: "Year", ar: "السنة", width: 10, hintEn: "e.g. 2025", hintAr: "مثال 2025"},
		{key: "period", en: "Period", ar: "الفترة", width: 12, required: true, values: periods, valuesAr: periodsAr,
			hintEn: "Must match the metric's (or KPI's) reporting frequency: a month for Monthly, Q1–Q4 for Quarterly, H1/H2 for Semi-Annual, Annual for Annual.",
			hintAr: "يجب أن تطابق تكرار الإبلاغ للمقياس (أو المؤشر): شهر للشهري، الربع 1–4 للربع سنوي، النصف 1/2 للنصف سنوي، سنوي للسنوي."},
		{key: "target", en: "Target", ar: "المستهدف", width: 14},
		{key: "actual", en: "Actual Result", ar: "النتيجة الفعلية", width: 16,
			hintEn: "Only periods that have ended. The achievement is calculated by the system.",
			hintAr: "للفترات المنتهية فقط. يحسب النظام نسبة الإنجاز."},
	})
	return utils.SheetConfig{Name: kpiTemplatePerformanceSheet, Columns: toExcelColumns(cols), DataRows: 4999, FreezeHeader: true, AutoFilter: true}
}

func toExcelColumns(columns []kpiTemplateColumn) []utils.ColumnConfig {
	result := make([]utils.ColumnConfig, 0, len(columns))
	for _, col := range columns {
		converted := utils.ColumnConfig{
			Key:         col.key,
			Name:        utils.LocalizedText{EN: col.en, AR: col.ar},
			Width:       col.width,
			Required:    col.required,
			HeaderColor: col.color,
			Hint:        utils.LocalizedText{EN: col.hintEn, AR: col.hintAr},
			Formula:     col.formula,
			FormulaRows: kpiTemplateFormulaRows,
		}
		switch {
		case col.list != "":
			converted.Dropdown = &utils.DropdownConfig{
				Source:      col.list,
				AllowOther:  true,
				PromptTitle: utils.LocalizedText{EN: "Choose from the list", AR: "اختر من القائمة"},
				Prompt: utils.LocalizedText{
					EN: "Pick an existing value. A new one must first be added on its own sheet or in the system.",
					AR: "اختر قيمة موجودة. القيمة الجديدة تُضاف أولاً في ورقتها أو في النظام."},
				ErrorTitle: utils.LocalizedText{EN: "Not in the list", AR: "غير موجود في القائمة"},
				Error: utils.LocalizedText{
					EN: "This value is not in the list. Keep it only if it should be created by the import.",
					AR: "هذه القيمة غير موجودة في القائمة. احتفظ بها فقط إذا كان يجب إنشاؤها عند الاستيراد."},
			}
		case len(col.values) > 0 || len(col.valuesAr) > 0:
			converted.Dropdown = &utils.DropdownConfig{
				Values: col.values, ValuesAR: col.valuesAr,
				PromptTitle: utils.LocalizedText{EN: "Allowed values", AR: "القيم المسموحة"},
				Prompt:      utils.LocalizedText{EN: strings.Join(col.values, ", "), AR: strings.Join(col.valuesAr, ", ")},
				ErrorTitle:  utils.LocalizedText{EN: "Invalid value", AR: "قيمة غير صالحة"},
				Error:       utils.LocalizedText{EN: "Choose one of: " + strings.Join(col.values, ", "), AR: "اختر أحد الخيارات: " + strings.Join(col.valuesAr, ", ")},
			}
		}
		result = append(result, converted)
	}
	return result
}

// kpiTemplateDictionaryColumns lists a Dictionary sheet's columns in groups,
// each with its own header colour: basic details, strategic alignment,
// ownership, measurement, then the annual targets.
func kpiTemplateDictionaryColumns(kpiType string) []kpiTemplateColumn {
	group := func(color string, cols ...kpiTemplateColumn) []kpiTemplateColumn {
		for i := range cols {
			cols[i].color = color
		}
		return cols
	}
	pick := func(key, en, ar, list string, width float64) kpiTemplateColumn {
		return kpiTemplateColumn{key: key, en: en, ar: ar, width: width, list: list}
	}

	nameCol := kpiTemplateColumn{key: "name", ar: "المؤشر الاستراتيجي", en: "Strategic Indicator", width: 40}
	switch kpiType {
	case models.KPITypeOperational:
		nameCol.en, nameCol.ar = "Operational Indicator", "المؤشر التشغيلي"
	case models.KPITypeAward:
		nameCol.en, nameCol.ar = "Indicator / Measure", "مؤشر / مقياس"
	}
	cols := group(colorBasic,
		kpiTemplateColumn{key: "code", ar: "الكود", en: "Code", width: 18,
			hintEn: "Unique code, e.g. KPI-P1-01-01 (strategic), OP-P1-01-01 (operational), KPI-AW-05-01 (award).",
			hintAr: "كود فريد، مثال KPI-P1-01-01 (استراتيجي)، OP-P1-01-01 (تشغيلي)، KPI-AW-05-01 (جائزة)."},
		nameCol,
		kpiTemplateColumn{key: "description", ar: "الوصف", en: "Description", width: 40},
	)

	// As on the KPI forms: pick the operational objective; its parent
	// objective and strategic goal follow from it.
	objective := pick("objective", "Operational Objective", "الهدف التشغيلي", listProcesses, 34)
	objective.required = true // checked per row by the importer
	parent := pick("parent_objective", "Parent Objective", "الهدف الرئيسي", listObjectives, 30)
	parent.hintEn = "Leave empty — it is taken from the operational objective. Fill it only to create a new operational objective here, or when the same name exists under several parent objectives."
	parent.hintAr = "اتركه فارغاً — يُؤخذ من الهدف التشغيلي. املأه فقط لإنشاء هدف تشغيلي جديد هنا، أو إذا كان الاسم نفسه موجوداً تحت أكثر من هدف رئيسي."
	pillar := pick("pillar", "Pillar / Enabler", "الركيزة / الممكن", listPillarsEnablers, 30)
	pillar.hintEn, pillar.hintAr = "Optional — taken from the operational objective when empty.", "اختياري — يُؤخذ من الهدف التشغيلي عند تركه فارغاً."
	// Filled automatically from the operational objective (columns D, E, F:
	// code, name and description come first).
	parent.formula = lookupFormula("D", lookupOpObjName, lookupOpObjParent)
	pillar.formula = lookupFormula("D", lookupOpObjName, lookupOpObjPillar)
	parent.hintEn = "Filled automatically from the Operational Objective. Type a parent only to create a new operational objective here, or when the name exists under several parent objectives (the cell then stays empty)."
	parent.hintAr = "يُعبأ تلقائياً من الهدف التشغيلي. اكتب الهدف الرئيسي فقط لإنشاء هدف تشغيلي جديد هنا، أو عندما يكون الاسم موجوداً تحت أكثر من هدف رئيسي (تبقى الخلية فارغة عندها)."
	pillar.hintEn, pillar.hintAr = "Filled automatically from the Operational Objective; you can change it.", "يُعبأ تلقائياً من الهدف التشغيلي؛ ويمكنك تغييره."
	alignment := []kpiTemplateColumn{objective, parent, pillar}
	switch kpiType {
	case models.KPITypeAward:
		alignment = append(alignment,
			pick("criterion", "Criterion", "المعيار", listCriteria, 26),
			kpiTemplateColumn{key: "sub_no", ar: "رقم المعيار الفرعي", en: "Sub-Criterion #", width: 18, list: listSubCriteria,
				hintEn: "e.g. 5.1 — must belong to the chosen criterion.", hintAr: "مثال 5.1 — يجب أن يتبع المعيار المختار."},
			kpiTemplateColumn{key: "sub_name", ar: "المعيار الفرعي", en: "Sub-Criterion", width: 30,
				hintEn: "Needed only when the sub-criterion does not exist yet.", hintAr: "مطلوب فقط إذا لم يكن المعيار الفرعي موجوداً."},
		)
	}
	alignment = append(alignment, pick("domain", "Related Domain / Award Criterion", "المجال المرتبط / معيار الجائزة", listDomains, 32))
	if kpiType == models.KPITypeStrategic {
		alignment = append(alignment, kpiTemplateColumn{key: "seg", ar: "محاور التجزئة", en: "Segmentation Axes", width: 26, list: listSegDimensions,
			hintEn: "Pick a dimension, or type several separated by ; (semicolon).", hintAr: "اختر بُعداً، أو اكتب عدة أبعاد مفصولة بـ ; (فاصلة منقوطة)."})
	}
	alignment = append(alignment, pick("award_measure", "Award KPIs and Measures", "مؤشرات ومقاييس الجائزة", listAwardMeasures, 30))
	cols = append(cols, group(colorAlignment, alignment...)...)

	cols = append(cols, group(colorOwnership,
		pick("owner_dept", "Owner Department", "الإدارة المالكة", listDepartments, 30),
		kpiTemplateColumn{key: "owner_type", ar: "نوع المالك", en: "Owner Type", width: 16,
			values: []string{"Internal", "External"}, valuesAr: []string{"داخلي", "خارجي"}},
		pick("agency", "Owning Agency", "الوكالة / الإدارة المالكة", listDepartments, 30),
		pick("owner_org", "Owner Organization", "المنظمة المالكة", listOrganizations, 30),
		kpiTemplateColumn{key: "related_depts", ar: "الإدارات ذات الصلة", en: "Related Departments", width: 34,
			hintEn: "One or more names from the Departments column of the Lists sheet, separated by ; (semicolon).",
			hintAr: "اسم أو أكثر من عمود الإدارات في ورقة القوائم، مفصولة بـ ; (فاصلة منقوطة)."},
	)...)

	cols = append(cols, group(colorMeasurement,
		kpiTemplateColumn{key: "polarity", ar: "الاتجاه", en: "Polarity / Direction", width: 20,
			values: []string{"Ascending", "Descending"}, valuesAr: []string{"تصاعدي", "تنازلي"}},
		kpiTemplateColumn{key: "frequency", ar: "تكرار الإبلاغ", en: "Reporting Frequency", width: 18,
			values:   []string{"Monthly", "Quarterly", "Semi-Annual", "Annual", "Custom"},
			valuesAr: []string{"شهري", "ربع سنوي", "نصف سنوي", "سنوي", "مخصص"}},
		kpiTemplateColumn{key: "activation", ar: "حالة التفعيل", en: "Activation Status", width: 20,
			values:   []string{"Active", "Inactive", "Draft", "Reviewed", "Approved", "Closed"},
			valuesAr: []string{"نشط", "غير نشط", "مسودة", "مراجَع", "معتمد", "مغلق"}},
		kpiTemplateColumn{key: "classification", ar: "تصنيف المؤشر", en: "KPI Classification", width: 20,
			values: []string{"KPI", "Measurement", "Output"}, valuesAr: []string{"مؤشر", "مقياس", "مخرج"}},
		kpiTemplateColumn{key: "unit", ar: "وحدة القياس", en: "Unit of Measure", width: 16},
		kpiTemplateColumn{key: "baseline", ar: "خط الأساس", en: "Baseline", width: 14},
		kpiTemplateColumn{key: "formula", ar: "معادلة الحساب", en: "Calculation Formula", width: 30},
		pick("data_source", "Data Source", "مصدر البيانات", listDataSources, 26),
		kpiTemplateColumn{key: "target_requirement", ar: "متطلب المستهدف", en: "Target Requirement", width: 20,
			values: []string{"Mandatory", "Optional"}, valuesAr: []string{"إلزامي", "اختياري"}},
		kpiTemplateColumn{key: "lifecycle", ar: "دورة حياة المؤشر", en: "KPI Lifecycle", width: 22},
		kpiTemplateColumn{key: "notes", ar: "ملاحظات", en: "Notes", width: 32},
	)...)
	return cols
}

func kpiTemplateAnnualTargetColumns(fromYear, toYear int) []kpiTemplateColumn {
	var cols []kpiTemplateColumn
	for year := fromYear; year <= toYear; year++ {
		cols = append(cols, kpiTemplateColumn{
			key:   fmt.Sprintf("target_%d", year),
			ar:    fmt.Sprintf("مستهدفات %d", year),
			en:    fmt.Sprintf("Targets %d", year),
			width: 14,
			color: colorTargets,
		})
	}
	return cols
}

// ─── Instructions ───────────────────────────────────────────────────────────

func buildKpiDictionaryInstructions(fromYear, toYear int) *utils.InstructionConfig {
	text := func(en, ar string) utils.LocalizedText { return utils.LocalizedText{EN: en, AR: ar} }
	item := func(labelEN, labelAR, bodyEN, bodyAR string) utils.InstructionItem {
		return utils.InstructionItem{Label: text(labelEN, labelAR), Text: text(bodyEN, bodyAR)}
	}
	return &utils.InstructionConfig{LabelWidth: 30, TextWidth: 110, Sections: []utils.InstructionSection{
		{Items: []utils.InstructionItem{item("Purpose", "الغرض",
			"Create or update all KPI data in one import: master data, KPIs, their metrics, annual targets and results for any period (monthly, quarterly, half-yearly or annual). Every sheet except the three Dictionary sheets is optional. You do not need to type names that already exist: pick them from the dropdowns.",
			"إنشاء أو تحديث جميع بيانات المؤشرات في عملية استيراد واحدة: البيانات الرئيسية والمؤشرات ومقاييسها ومستهدفاتها السنوية ونتائجها لأي فترة (شهرية أو ربعية أو نصف سنوية أو سنوية). جميع الأوراق اختيارية ما عدا أوراق الدليل الثلاث. لا حاجة لكتابة الأسماء الموجودة: اخترها من القوائم المنسدلة.")}, BlankAfter: true},
		{Title: text("Steps", "الخطوات"), Items: []utils.InstructionItem{
			item("1. Lists (read only)", "1. القوائم (للقراءة فقط)",
				"Shows what already exists in Automax when this template was downloaded: pillars, enablers, goals, objectives, processes, departments, organizations, domains, award criteria and measures, data sources. Download a new template to refresh it.",
				"تعرض ما هو موجود في النظام عند تنزيل هذا القالب: الركائز والممكنات والأهداف والعمليات والإدارات والمنظمات والمجالات ومعايير ومقاييس الجائزة ومصادر البيانات. نزّل قالباً جديداً لتحديثها."),
			item("2. New master data (optional)", "2. بيانات رئيسية جديدة (اختياري)",
				"Only if something is missing from the Lists sheet, one sheet per Master Data tab: Pillars and Enablers → Strategic Goals → Parent Objectives (choose the goal) → Operational Objectives (choose the parent objective; the goal follows) → Initiatives (choose the goal); and Domains, Organizations, Data Sources, Segmentation Dimensions, Award Criteria → Award Sub-criteria (choose the criterion), Award KPI Measures (same columns as each tab's own import). One per row without blank rows. Anything added appears in the dropdowns of the later sheets.",
				"فقط إذا كان هناك عنصر غير موجود في ورقة القوائم، ورقة لكل تبويب من البيانات الرئيسية: الركائز والممكنات ← الأهداف الاستراتيجية ← الأهداف الرئيسية (اختر الهدف) ← الأهداف التشغيلية (اختر الهدف الرئيسي؛ ويُؤخذ الهدف منه) ← المبادرات (اختر الهدف)؛ والمجالات والمنظمات ومصادر البيانات وأبعاد التجزئة ومعايير الجائزة ← المعايير الفرعية (اختر المعيار) ومقاييس مؤشرات الجائزة (بنفس أعمدة استيراد كل تبويب). عنصر في كل صف دون صفوف فارغة. أي عنصر يُضاف يظهر في القوائم المنسدلة للأوراق التالية."),
			item("3. KPIs", "3. المؤشرات",
				"Fill the Strategic, Operational or Award Dictionary sheet, one KPI per row. Pick references from the dropdowns. Put annual targets in the \"Targets YYYY\" columns.",
				"املأ ورقة الدليل الاستراتيجي أو التشغيلي أو الجائزة، مؤشر واحد في كل صف. اختر المراجع من القوائم المنسدلة. ضع المستهدفات السنوية في أعمدة \"مستهدفات YYYY\"."),
			item("4. Metrics (optional)", "4. المقاييس (اختياري)",
				"The metrics of each KPI, same columns as the Metrics import. KPI Code can be a KPI from the Dictionary sheets of this file. A KPI with no metric here gets one default metric (with the KPI's reporting frequency) when it has targets or results.",
				"مقاييس كل مؤشر، بنفس أعمدة استيراد المقاييس. يمكن أن يكون كود المؤشر لمؤشر من أوراق الدليل في هذا الملف. المؤشر الذي ليس له مقياس هنا يحصل على مقياس افتراضي واحد (بتكرار إبلاغ المؤشر) عند وجود مستهدفات أو نتائج له."),
			item("4b. Targets (optional)", "4ب. المستهدفات (اختياري)",
				"Any target type, same columns as the Targets import: Period Target (one period, e.g. Q1 or Jan), Annual Target, Milestone / Ad Hoc (a date range), with threshold mode and thresholds. A simple yearly target can also go in the \"Targets YYYY\" columns of the Dictionary sheets.",
				"أي نوع من المستهدفات، بنفس أعمدة استيراد المستهدفات: مستهدف فترة (فترة واحدة مثل الربع 1 أو يناير)، مستهدف سنوي، مرحلي / مخصص (نطاق تاريخ)، مع نمط الحدود وقيمها. ويمكن وضع المستهدف السنوي البسيط أيضاً في أعمدة \"مستهدفات YYYY\" في أوراق الدليل."),
			item("5. Performance (optional)", "5. الأداء (اختياري)",
				"One row per KPI (and metric) per period: Code, Metric Code (only if the KPI has several metrics), Year, Period, Target and Actual Result. The period must match the reporting frequency — a month for Monthly, Q1–Q4 for Quarterly, H1/H2 for Semi-Annual, Annual for Annual — and must have ended.",
				"صف واحد لكل مؤشر (ومقياس) لكل فترة: الكود وكود المقياس (فقط إذا كان للمؤشر عدة مقاييس) والسنة والفترة والمستهدف والنتيجة الفعلية. يجب أن تطابق الفترة تكرار الإبلاغ — شهر للشهري، الربع 1–4 للربع سنوي، النصف 1/2 للنصف سنوي، سنوي للسنوي — وأن تكون منتهية."),
			item("6. Import", "6. الاستيراد",
				"Save as .xlsx and upload it under KPI Dictionary Import. Click Validate, review the report (each rejected row is listed with its sheet, row number and reason), then Commit.",
				"احفظ الملف بصيغة .xlsx وارفعه في استيراد دليل المؤشرات. اضغط تحقق وراجع التقرير (كل صف مرفوض يظهر مع الورقة ورقم الصف والسبب)، ثم تنفيذ."),
		}, BlankAfter: true},
		{Title: text("Column colours on the Dictionary sheets", "ألوان الأعمدة في أوراق الدليل"), Items: []utils.InstructionItem{
			item("Dark blue", "أزرق داكن", "Basic details: code, name, description.", "البيانات الأساسية: الكود والاسم والوصف."),
			item("Teal", "أزرق مخضر", "Strategic alignment: operational objective (required; its parent objective, strategic goal and pillar are filled from it), award criterion, domain.", "المواءمة الاستراتيجية: الهدف التشغيلي (إلزامي؛ يُؤخذ منه الهدف الرئيسي والهدف الاستراتيجي والركيزة) ومعيار الجائزة والمجال."),
			item("Purple", "بنفسجي", "Ownership: owner department and type, agency, organization, related departments.", "الملكية: الإدارة المالكة ونوع المالك والوكالة والمنظمة والإدارات ذات الصلة."),
			item("Green", "أخضر", "Measurement: direction, frequency, status, classification, unit, baseline, formula, data source.", "القياس: الاتجاه والتكرار والحالة والتصنيف والوحدة وخط الأساس والمعادلة ومصدر البيانات."),
			item("Orange", "برتقالي", fmt.Sprintf("Annual targets %d–%d. Add or remove year columns if needed.", fromYear, toYear), fmt.Sprintf("المستهدفات السنوية %d–%d. أضف أو أزل أعمدة السنوات حسب الحاجة.", fromYear, toYear)),
			item("Red *", "علامة * حمراء", "Mandatory column.", "عمود إلزامي."),
		}, BlankAfter: true},
		{Title: text("Rules", "القواعد"), Items: []utils.InstructionItem{
			item("Dropdowns", "القوائم المنسدلة", "A value that is not in a dropdown shows a warning. Pillars, enablers, goals and parent objectives must exist in the system or on their sheet. An operational objective must exist too, unless Parent Objective is filled (it is then created). Departments, organizations, domains, award criteria and measures that do not exist are created and reported as warnings.", "القيمة غير الموجودة في القائمة تُظهر تحذيراً. يجب أن تكون الركائز والممكنات والأهداف الاستراتيجية والأهداف الرئيسية موجودة في النظام أو في ورقتها. ويجب أن يكون الهدف التشغيلي موجوداً أيضاً ما لم يُملأ الهدف الرئيسي (فيُنشأ عندها). أما الإدارات والمنظمات والمجالات ومعايير ومقاييس الجائزة غير الموجودة فتُنشأ ويُبلّغ عنها كتحذير."),
			item("KPI code", "كود المؤشر", "Required and unique. Uppercase letters and digits separated by hyphens.", "مطلوب وفريد. أحرف كبيرة وأرقام مفصولة بشرطات."),
			item("Owner Type", "نوع المالك", "Defaults to External when an Owner Organization is given, otherwise Internal. Owner Organization is used for external owners only.", "يكون خارجياً عند تعبئة المنظمة المالكة، وإلا داخلياً. تُستخدم المنظمة المالكة للمالك الخارجي فقط."),
			item("Related Departments", "الإدارات ذات الصلة", "Existing departments only, separated by ;. A name that is not found is reported and not linked.", "إدارات موجودة فقط مفصولة بـ ;. الاسم غير الموجود يُبلّغ عنه ولا يُربط."),
			item("Activation Status", "حالة التفعيل", "Checked but not applied: new KPIs start as Draft and existing KPIs keep their workflow status.", "يتم التحقق منها دون تطبيقها: المؤشرات الجديدة تبدأ كمسودة والحالية تحتفظ بحالة سير العمل."),
			item("Segmentation Axes", "محاور التجزئة", "Strategic Dictionary only.", "للدليل الاستراتيجي فقط."),
			item("Numbers", "الأرقام", "Baseline, targets and actual results are numbers; commas and a trailing % are accepted. Negative values are rejected.", "خط الأساس والمستهدفات والنتائج أرقام؛ تُقبل الفواصل والعلامة % في النهاية. تُرفض القيم السالبة."),
			item("Existing records", "السجلات الحالية", "The import options decide whether existing KPIs, targets and results are skipped or updated. On update, only the columns present in the file are changed.", "تحدد خيارات الاستيراد ما إذا كانت المؤشرات والمستهدفات والنتائج الحالية تُتخطى أو تُحدّث. عند التحديث تتغير الأعمدة الموجودة في الملف فقط."),
			item("Sheets and headers", "الأوراق والرؤوس", "Do not rename the sheets or change row 1. Sheets you do not need can stay empty.", "لا تغيّر أسماء الأوراق أو الصف الأول. الأوراق غير المطلوبة يمكن تركها فارغة."),
		}},
	}}
}
