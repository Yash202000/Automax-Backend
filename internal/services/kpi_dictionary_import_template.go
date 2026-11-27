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
	"slices"
	"sort"
	"strconv"
	"strings"

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
	listKpiLabels       = "List_KpiLabels"   // "CODE - Name", for the Targets KPI column
	listMetricNames     = "List_MetricNames" // for the Targets Metric column

	// Lookup tables for auto-filled cells (parallel columns, row for row).
	lookupOpObjName    = "Lookup_OpObjName"
	lookupOpObjParent  = "Lookup_OpObjParent"
	lookupOpObjPillar  = "Lookup_OpObjPillar"
	lookupParentName   = "Lookup_ParentName"
	lookupParentGoal   = "Lookup_ParentGoal"
	lookupParentPillar = "Lookup_ParentPillar"
	lookupKpiCode      = "Lookup_KpiCode"
	lookupKpiType      = "Lookup_KpiType"
	lookupOpObjGoal    = "Lookup_OpObjGoal"
	// KPI code → metric name of the system's metrics, sorted by KPI code so
	// each KPI's metrics are adjacent (the Targets Metric dropdown shows the
	// picked KPI's block).
	lookupMetricKpi  = "Lookup_MetricKpi"
	lookupMetricName = "Lookup_MetricName"
	// The same for metric codes (Performance Metric Code), criterion number
	// → sub-criterion label (Dictionary Sub-Criteria), and criterion name →
	// number (Award Sub-criteria Sub-Criterion No.).
	lookupMetricCodeKpi = "Lookup_MetricCodeKpi"
	lookupMetricCode    = "Lookup_MetricCode"
	lookupSubCritNo     = "Lookup_SubCritNo"
	lookupSubCrit       = "Lookup_SubCrit"
	lookupCritName      = "Lookup_CritName"
	lookupCritNo        = "Lookup_CritNo"
	// KPI codes that exist in the system (none typed in the workbook): a KPI
	// listed here without metrics gets an empty Metric list.
	lookupSysKpiCode = "Lookup_SysKpiCode"
	// listEmpty is an empty list, for a dropdown with nothing to offer.
	listEmpty = "List_Empty"
	// Reporting period type of each metric ("m" monthly, "q" quarterly,
	// "h" semi-annual, "a" annual; "" when unknown or Ad Hoc), keyed by
	// "KPI code|metric name" and "KPI code|metric code"; and the type of each
	// Period value (codes, labels, Arabic names). For the Period check.
	lookupMetricKey      = "Lookup_MetricKey"
	lookupMetricFreq     = "Lookup_MetricFreq"
	lookupMetricCodeKey  = "Lookup_MetricCodeKey"
	lookupMetricCodeFreq = "Lookup_MetricCodeFreq"
	lookupPeriodName     = "Lookup_PeriodName"
	lookupPeriodType     = "Lookup_PeriodType"
	// KPI code → period type of its only metric (Performance rows without a
	// Metric Code use it); "" when the KPI has none or several.
	lookupSoleMetricKpi  = "Lookup_SoleMetricKpi"
	lookupSoleMetricFreq = "Lookup_SoleMetricFreq"
)

// kpiTemplateFormulaRows is how many data rows get the auto-fill formulas.
const kpiTemplateFormulaRows = 4999

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
// the KPI Dictionary bulk import. lang selects a single header language (en
// or ar) — the workbook is not bilingual. The Lists sheet is filled from db.
func BuildKpiDictionaryImportTemplate(ctx context.Context, db *gorm.DB, lang string) ([]byte, error) {
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
		if st.entity == KpiMasterOrganization {
			continue // not offered in the workbook; older files that have the sheet still import
		}
		spec, _ := masterSpecFor(st.entity)
		cols := withLists(spec.cols, map[string]string{"award_criterion": listCriteriaNames})
		sheets = append(sheets, utils.SheetConfig{Name: spec.sheet, Columns: toExcelColumns(cols), DataRows: kpiTemplateMasterRows, FreezeHeader: true,
			ConditionalFills: masterTemplateFills(st.entity)})
	}
	metricCols := withKpiTypeFormula(withLists(markKpiMetricTemplateRequired(kpiMetricTemplateColumns()),
		map[string]string{"kpi_code": listKpiCodes, "data_source": listDataSources}))
	targetCols := kpiTargetSheetColumns(lang)
	sheets = append(sheets,
		buildKpiDictionarySheet(kpiTemplateStrategicDictSheet, models.KPITypeStrategic),
		buildKpiDictionarySheet(kpiTemplateOperationalDict, models.KPITypeOperational),
		buildKpiDictionarySheet(kpiTemplateAwardDictSheet, models.KPITypeAward),
		// Same columns as the Metrics import.
		utils.SheetConfig{Name: kpiTemplateMetricsSheet, Columns: toExcelColumns(metricCols),
			DataRows: kpiMetricsTemplateDataRows, FreezeHeader: true},
		// Same columns as the Targets import: Period, Annual and Milestone /
		// Ad Hoc targets with thresholds.
		utils.SheetConfig{Name: kpiTemplateTargetsSheet, Columns: toExcelColumns(targetCols),
			DataRows: 4999, FreezeHeader: true, ConditionalFills: targetSheetFills()},
		buildKpiPerformanceSheet(),
	)
	cfg := utils.WorkbookConfig{
		Language:     language,
		Title:        "KPI Dictionary Import Template",
		Subject:      "Bulk import of the KPI Dictionary workbook",
		Creator:      "Automax",
		ActiveSheet:  kpiTemplateInstructionsSheet,
		Instructions: buildKpiDictionaryInstructions(),
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

	var kpiCodes, kpiLabels, metricCodes, metricNames []string
	seenMetric, seenMetricName := map[string]bool{}, map[string]bool{}
	for _, ek := range imp.existingKpis {
		kpiCodes = append(kpiCodes, ek.Code)
		kpiLabels = append(kpiLabels, kpiLabel(ek.Code, display(ek.NameEn, ek.NameAr)))
		for _, m := range ek.Metrics {
			if c := strings.TrimSpace(m.MetricCode); c != "" && !seenMetric[strings.ToUpper(c)] {
				seenMetric[strings.ToUpper(c)] = true
				metricCodes = append(metricCodes, c)
			}
			if n := strings.TrimSpace(m.Name); n != "" && !seenMetricName[normName(n)] {
				seenMetricName[normName(n)] = true
				metricNames = append(metricNames, n)
			}
		}
	}
	sort.Strings(kpiCodes)
	sort.Strings(kpiLabels)
	sort.Strings(metricCodes)
	sort.Strings(metricNames)

	// KPI code → metric name pairs, grouped by KPI code (case-insensitive,
	// as Excel's MATCH/COUNTIF compare).
	type kpiMetric struct{ code, name string }
	var pairs []kpiMetric
	seenPair := map[string]bool{}
	for _, ek := range imp.existingKpis {
		for _, m := range ek.Metrics {
			n := strings.TrimSpace(m.Name)
			key := strings.ToUpper(ek.Code) + "|" + normName(n)
			if n == "" || seenPair[key] {
				continue
			}
			seenPair[key] = true
			pairs = append(pairs, kpiMetric{ek.Code, n})
		}
	}
	sort.Slice(pairs, func(i, j int) bool {
		ci, cj := strings.ToUpper(pairs[i].code), strings.ToUpper(pairs[j].code)
		if ci != cj {
			return ci < cj
		}
		return pairs[i].name < pairs[j].name
	})
	pairCodes, pairNames := make([]string, len(pairs)), make([]string, len(pairs))
	for i, p := range pairs {
		pairCodes[i], pairNames[i] = p.code, p.name
	}

	// KPI code → metric code, grouped the same way.
	var codePairs []kpiMetric
	seenCodePair := map[string]bool{}
	for _, ek := range imp.existingKpis {
		for _, m := range ek.Metrics {
			c := strings.TrimSpace(m.MetricCode)
			key := strings.ToUpper(ek.Code) + "|" + strings.ToUpper(c)
			if c == "" || seenCodePair[key] {
				continue
			}
			seenCodePair[key] = true
			codePairs = append(codePairs, kpiMetric{ek.Code, c})
		}
	}
	sort.Slice(codePairs, func(i, j int) bool {
		ci, cj := strings.ToUpper(codePairs[i].code), strings.ToUpper(codePairs[j].code)
		if ci != cj {
			return ci < cj
		}
		return strings.ToUpper(codePairs[i].name) < strings.ToUpper(codePairs[j].name)
	})
	mcKpis, mcCodes := make([]string, len(codePairs)), make([]string, len(codePairs))
	for i, p := range codePairs {
		mcKpis[i], mcCodes[i] = p.code, p.name
	}

	// Criterion number → sub-criterion label (as listed: "5.1 - Name"),
	// grouped by criterion; criterion name (as listed) → number.
	type critSub struct {
		no    int
		label string
	}
	var critSubs []critSub
	for _, n := range imp.md[mdSubCriterion] {
		if n.SubNo != "" && n.Criterion != nil {
			critSubs = append(critSubs, critSub{n.Criterion.CriterionNo, n.SubNo + " - " + display(n.NameEn, n.NameAr)})
		}
	}
	sort.Slice(critSubs, func(i, j int) bool {
		if critSubs[i].no != critSubs[j].no {
			return critSubs[i].no < critSubs[j].no
		}
		return critSubs[i].label < critSubs[j].label
	})
	subCritNos, subCritLabels := make([]string, len(critSubs)), make([]string, len(critSubs))
	for i, c := range critSubs {
		subCritNos[i], subCritLabels[i] = strconv.Itoa(c.no), c.label
	}
	// Each metric's reporting period type: its own frequency, else its KPI's
	// (as the import checks Targets and Performance periods).
	var metricKeys, metricFreqs, metricCodeKeys, metricCodeFreqs, soleKpis, soleFreqs []string
	seenMetricKey, seenMetricCodeKey, seenSoleKpi := map[string]bool{}, map[string]bool{}, map[string]bool{}
	for _, ek := range imp.existingKpis {
		for _, m := range ek.Metrics {
			freq := NormalizeFrequency(m.ReportingFrequency)
			if freq == "" {
				freq = NormalizeFrequency(ek.Frequency)
			}
			typ := periodTypeLetter[freq]
			if n := strings.TrimSpace(m.Name); n != "" {
				if k := strings.ToUpper(ek.Code + "|" + n); !seenMetricKey[k] {
					seenMetricKey[k] = true
					metricKeys, metricFreqs = append(metricKeys, ek.Code+"|"+n), append(metricFreqs, typ)
				}
			}
			// Performance Metric Code: the metric's code, or its name.
			for _, c := range []string{strings.TrimSpace(m.MetricCode), strings.TrimSpace(m.Name)} {
				if k := strings.ToUpper(ek.Code + "|" + c); c != "" && !seenMetricCodeKey[k] {
					seenMetricCodeKey[k] = true
					metricCodeKeys, metricCodeFreqs = append(metricCodeKeys, ek.Code+"|"+c), append(metricCodeFreqs, typ)
				}
			}
		}
		if len(ek.Metrics) == 1 && !seenSoleKpi[strings.ToUpper(ek.Code)] {
			seenSoleKpi[strings.ToUpper(ek.Code)] = true
			m := ek.Metrics[0]
			freq := NormalizeFrequency(m.ReportingFrequency)
			if freq == "" {
				freq = NormalizeFrequency(ek.Frequency)
			}
			soleKpis, soleFreqs = append(soleKpis, ek.Code), append(soleFreqs, periodTypeLetter[freq])
		}
	}
	periodNames, periodTypes := periodTypeTable()

	var sysKpiCodes []string
	seenSysKpi := map[string]bool{}
	for _, ek := range imp.existingKpis {
		if c := strings.ToUpper(ek.Code); !seenSysKpi[c] {
			seenSysKpi[c] = true
			sysKpiCodes = append(sysKpiCodes, ek.Code)
		}
	}
	sort.Strings(sysKpiCodes)
	var critNameKeys, critNoValues []string
	for _, c := range imp.md[mdCriterion] {
		critNameKeys = append(critNameKeys, display(c.NameEn, c.NameAr))
		critNoValues = append(critNoValues, strconv.Itoa(c.CriterionNo))
	}

	// Sub-criteria as on the KPI forms: "5.1 - Name".
	var subLabels []string
	subs := append([]*mdNode(nil), imp.md[mdSubCriterion]...)
	sort.Slice(subs, func(i, j int) bool { return subs[i].SubNo < subs[j].SubNo })
	for _, n := range subs {
		if n.SubNo != "" {
			subLabels = append(subLabels, n.SubNo+" - "+display(n.NameEn, n.NameAr))
		}
	}

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
	var opName, opParent, opPillar, opGoal []string
	for _, n := range imp.md[mdProcess] {
		var pp, goal *mdNode
		if n.Objective != nil {
			pp, goal = n.Objective.Pillar, n.Objective.Goal
		}
		opName = append(opName, nodeName(n))
		opParent = append(opParent, nodeName(n.Objective))
		opGoal = append(opGoal, nodeName(goal))
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
	opNameCol, opParCol, opPilCol := kpiWorkbookMasterCol(mdProcess, "name_en"), kpiWorkbookMasterCol(mdProcess, "operational_objective"), kpiWorkbookMasterCol(mdProcess, "pillar")
	parNameCol, parGoalCol, parPilCol := kpiWorkbookMasterCol(mdObjective, "name_en"), kpiWorkbookMasterCol(mdObjective, "goal"), kpiWorkbookMasterCol(mdObjective, "pillar")
	opSheetName := sheetRowFormulas(kpiTemplateMasterRows, opSheet, opNameCol, func(i string) string { return idxCell(opSheet, opNameCol, i) + `&""` })
	opSheetParent := sheetRowFormulas(kpiTemplateMasterRows, opSheet, opNameCol, func(i string) string { return idxCell(opSheet, opParCol, i) + `&""` })
	opSheetPillar := sheetRowFormulas(kpiTemplateMasterRows, opSheet, opNameCol, func(i string) string {
		c, b := idxCell(opSheet, opPilCol, i), idxCell(opSheet, opParCol, i)
		return fmt.Sprintf(`IF(%[1]s<>"",%[1]s&"",IFERROR(INDEX(%[3]s,MATCH(%[2]s,%[4]s,0))&"",""))`, c, b, lookupParentPillar, lookupParentName)
	})
	opSheetGoal := sheetRowFormulas(kpiTemplateMasterRows, opSheet, opNameCol, func(i string) string {
		return fmt.Sprintf(`IFERROR(INDEX(%s,MATCH(%s,%s,0))&"","")`, lookupParentGoal, idxCell(opSheet, opParCol, i), lookupParentName)
	})
	parSheetName := sheetRowFormulas(kpiTemplateMasterRows, parSheet, parNameCol, func(i string) string { return idxCell(parSheet, parNameCol, i) + `&""` })
	parSheetGoal := sheetRowFormulas(kpiTemplateMasterRows, parSheet, parNameCol, func(i string) string { return idxCell(parSheet, parGoalCol, i) + `&""` })
	parSheetPillar := sheetRowFormulas(kpiTemplateMasterRows, parSheet, parNameCol, func(i string) string {
		return idxCell(parSheet, parPilCol, i) + `&""` // pillar only
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
	// "CODE - Name" of the KPIs on the three Dictionary sheets (the Arabic
	// name in an Arabic template when given).
	kpiSheetLabels := sheetLabelFormulas(kpiTemplateCodeRows, "A", func(sheet, idx string) string {
		code := fmt.Sprintf(`INDEX('%s'!$A:$A,%s)`, sheet, idx)
		name := fmt.Sprintf(`INDEX('%s'!$B:$B,%s)`, sheet, idx)
		if lang == KpiTemplateLangAR {
			name = fmt.Sprintf(`IF(INDEX('%[1]s'!$C:$C,%[2]s)<>"",INDEX('%[1]s'!$C:$C,%[2]s),%[3]s)`, sheet, idx, name)
		}
		return code + `&" - "&` + name
	}, kpiTemplateStrategicDictSheet, kpiTemplateOperationalDict, kpiTemplateAwardDictSheet)
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
		{Name: listGoals, Title: text("Goals", "الأهداف"), Values: names(mdGoal), Formulas: sheetNamesFormulas(rows, kpiTemplateGoalsSheet)},
		{Name: listObjectives, Title: text("Parent Objectives", "الأهداف الرئيسية"), Values: names(mdObjective), Formulas: sheetNamesFormulas(rows, kpiTemplateObjectivesSheet)},
		{Name: listProcesses, Title: text("Operational Objectives", "الأهداف التشغيلية"), Values: names(mdProcess), Formulas: sheetNamesFormulas(rows, kpiTemplateProcessesSheet)},
		{Name: listDepartments, Title: text("Departments", "الإدارات"), Values: names(mdDepartment)},
		{Name: listDomains, Title: text("Domains", "المجالات"), Values: names(mdDomain), Formulas: masterNamesFormulas(rows, KpiMasterDomain)},
		{Name: listCriteria, Title: text("Award Criteria", "معايير الجائزة"), Values: criteria,
			Formulas: sheetRowFormulas(rows, critSpec.sheet, templateColLetter(critSpec.cols, "name_en"), func(idx string) string {
				return fmt.Sprintf(`"%s "&INDEX('%[2]s'!$%[3]s:$%[3]s,%[5]s)&" - "&INDEX('%[2]s'!$%[4]s:$%[4]s,%[5]s)`,
					critLabel, critSpec.sheet, templateColLetter(critSpec.cols, "criterion_no"), templateColLetter(critSpec.cols, "name_en"), idx)
			})},
		{Name: listCriteriaNames, Title: text("Award Criterion Names", "أسماء معايير الجائزة"), Values: critNames, Formulas: masterNamesFormulas(rows, KpiMasterAwardCriterion)},
		{Name: listSubCriteria, Title: text("Award Sub-criteria", "المعايير الفرعية"), Values: subLabels,
			Formulas: sheetRowFormulas(rows, subSpec.sheet, templateColLetter(subSpec.cols, "sub_no"), func(idx string) string {
				return fmt.Sprintf(`INDEX('%[1]s'!$%[2]s:$%[2]s,%[4]s)&" - "&INDEX('%[1]s'!$%[3]s:$%[3]s,%[4]s)`,
					subSpec.sheet, templateColLetter(subSpec.cols, "sub_no"), templateColLetter(subSpec.cols, "name_en"), idx)
			})},
		{Name: listSegDimensions, Title: text("Segmentation Dimensions", "أبعاد التجزئة"), Values: segDims, Formulas: masterNamesFormulas(rows, KpiMasterSegmentationDimension)},
		{Name: listKpiCodes, Title: text("KPI Codes", "أكواد المؤشرات"), Values: kpiCodes,
			Formulas: sheetNamesFormulas(kpiTemplateCodeRows, kpiTemplateStrategicDictSheet, kpiTemplateOperationalDict, kpiTemplateAwardDictSheet)},
		{Name: listMetricCodes, Title: text("Metric Codes", "أكواد المقاييس"), Values: metricCodes,
			Formulas: sheetNamesFormulasIn(kpiTemplateCodeRows, templateColLetter(metricTplCols, "metric_code"), kpiTemplateMetricsSheet)},
		{Name: lookupOpObjName, Title: text("Lookup: Operational Objective", "بحث: الهدف التشغيلي"), Values: opName, Formulas: opSheetName, Lookup: true},
		{Name: lookupOpObjGoal, Title: text("Lookup: its Goal", "بحث: الهدف"), Values: opGoal, Formulas: opSheetGoal, Lookup: true},
		{Name: lookupOpObjParent, Title: text("Lookup: its Parent Objective", "بحث: الهدف الرئيسي"), Values: opParent, Formulas: opSheetParent, Lookup: true},
		{Name: lookupOpObjPillar, Title: text("Lookup: its Pillar / Enabler", "بحث: الركيزة / الممكن"), Values: opPillar, Formulas: opSheetPillar, Lookup: true},
		{Name: lookupParentName, Title: text("Lookup: Parent Objective", "بحث: الهدف الرئيسي"), Values: parName, Formulas: parSheetName, Lookup: true},
		{Name: lookupParentGoal, Title: text("Lookup: its Goal", "بحث: الهدف"), Values: parGoal, Formulas: parSheetGoal, Lookup: true},
		{Name: lookupParentPillar, Title: text("Lookup: its Pillar / Enabler", "بحث: الركيزة / الممكن"), Values: parPillar, Formulas: parSheetPillar, Lookup: true},
		{Name: lookupKpiCode, Title: text("Lookup: KPI Code", "بحث: كود المؤشر"), Values: kCode, Formulas: kSheetCode, Lookup: true},
		{Name: lookupKpiType, Title: text("Lookup: its KPI Type", "بحث: نوع المؤشر"), Values: kType, Formulas: kSheetType, Lookup: true},
		{Name: lookupMetricKpi, Title: text("Lookup: Metric's KPI Code", "بحث: كود مؤشر المقياس"), Values: pairCodes, Lookup: true},
		{Name: lookupMetricName, Title: text("Lookup: Metric", "بحث: المقياس"), Values: pairNames, Lookup: true},
		{Name: lookupMetricCodeKpi, Title: text("Lookup: Metric Code's KPI Code", "بحث: كود مؤشر كود المقياس"), Values: mcKpis, Lookup: true},
		{Name: lookupMetricCode, Title: text("Lookup: Metric Code", "بحث: كود المقياس"), Values: mcCodes, Lookup: true},
		{Name: lookupSubCritNo, Title: text("Lookup: Sub-criterion's Criterion No.", "بحث: رقم معيار المعيار الفرعي"), Values: subCritNos, Lookup: true},
		{Name: lookupSubCrit, Title: text("Lookup: Sub-criterion", "بحث: المعيار الفرعي"), Values: subCritLabels, Lookup: true},
		{Name: lookupCritName, Title: text("Lookup: Criterion", "بحث: المعيار"), Values: critNameKeys, Lookup: true},
		{Name: lookupCritNo, Title: text("Lookup: its Criterion No.", "بحث: رقم المعيار"), Values: critNoValues, Lookup: true},
		{Name: lookupSysKpiCode, Title: text("Lookup: KPI Codes in the system", "بحث: أكواد المؤشرات في النظام"), Values: sysKpiCodes, Lookup: true},
		{Name: listEmpty, Title: text("(empty list)", "(قائمة فارغة)")},
		{Name: lookupMetricKey, Title: text("Lookup: KPI Code|Metric", "بحث: كود المؤشر|المقياس"), Values: metricKeys, Lookup: true},
		{Name: lookupMetricFreq, Title: text("Lookup: its period type", "بحث: نوع فترته"), Values: metricFreqs, Lookup: true},
		{Name: lookupMetricCodeKey, Title: text("Lookup: KPI Code|Metric Code", "بحث: كود المؤشر|كود المقياس"), Values: metricCodeKeys, Lookup: true},
		{Name: lookupMetricCodeFreq, Title: text("Lookup: its period type", "بحث: نوع فترته"), Values: metricCodeFreqs, Lookup: true},
		{Name: lookupPeriodName, Title: text("Lookup: Period", "بحث: الفترة"), Values: periodNames, Lookup: true},
		{Name: lookupPeriodType, Title: text("Lookup: its period type", "بحث: نوع فترته"), Values: periodTypes, Lookup: true},
		{Name: lookupSoleMetricKpi, Title: text("Lookup: KPI Code (one metric)", "بحث: كود المؤشر (مقياس واحد)"), Values: soleKpis, Lookup: true},
		{Name: lookupSoleMetricFreq, Title: text("Lookup: its metric's period type", "بحث: نوع فترة مقياسه"), Values: soleFreqs, Lookup: true},
		{Name: listPeriodCodes, Title: text("Periods", "الفترات"), Values: targetPeriodChoices(lang)},
		{Name: listKpiLabels, Title: text("KPIs", "المؤشرات"), Values: kpiLabels, Formulas: kpiSheetLabels},
		{Name: listMetricNames, Title: text("Metrics", "المقاييس"), Values: metricNames,
			Formulas: sheetNamesFormulasIn(kpiTemplateCodeRows, templateColLetter(metricTplCols, "metric_name"), kpiTemplateMetricsSheet)},
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
	return sheetLabelFormulas(rows, col, func(sheet, idx string) string {
		return fmt.Sprintf(`INDEX('%[1]s'!$%[2]s:$%[2]s,%[3]s)`, sheet, col, idx)
	}, sheets...)
}

// sheetLabelFormulas is sheetNamesFormulasIn with the value built by render
// from the sheet and the row number of each non-empty cell of col.
func sheetLabelFormulas(rows int, col string, render func(sheet, idx string) string, sheets ...string) []string {
	last := rows + 1
	count := func(s string) string {
		return fmt.Sprintf(`SUMPRODUCT(--('%[1]s'!$%[3]s$2:$%[3]s$%[2]d<>""))`, s, last, col)
	}
	nth := func(s, k string) string {
		return render(s, fmt.Sprintf(`_xlfn.AGGREGATE(15,6,ROW('%[1]s'!$%[3]s$2:$%[3]s$%[2]d)/('%[1]s'!$%[3]s$2:$%[3]s$%[2]d<>""),%[4]s)`, s, last, col, k))
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

// kpiWorkbookMasterEntity is the Master Data import a master data sheet of
// the KPI workbook mirrors ("" for Goals, which have no such import).
func kpiWorkbookMasterEntity(kind string) string {
	switch kind {
	case mdPillar:
		return KpiMasterPillar
	case mdEnabler:
		return KpiMasterEnabler
	case mdObjective:
		return KpiMasterOperationalObjective // Parent Objectives
	case mdProcess:
		return KpiMasterProcess // Operational Objectives
	case mdInitiative:
		return KpiMasterInitiative
	}
	return ""
}

// kpiWorkbookMasterColumns are the columns of a master data sheet of the KPI
// workbook: the same as its Master Data import template.
func kpiWorkbookMasterColumns(kind string) []kpiTemplateColumn {
	spec, ok := masterSpecFor(kpiWorkbookMasterEntity(kind))
	if !ok { // Goals
		return []kpiTemplateColumn{{key: "name_en", en: "Name", ar: "الاسم", width: 46, required: true,
			hintEn: "English or Arabic name. One record per row, no blank rows in between.",
			hintAr: "الاسم بالإنجليزية أو بالعربية. سجل واحد في كل صف دون صفوف فارغة بينها."}}
	}
	cols := masterTemplateColumns(spec)
	for i := range cols {
		if cols[i].key == "name_en" {
			cols[i].hintEn = "One record per row, no blank rows in between. A record that already exists is linked, not changed."
			cols[i].hintAr = "سجل واحد في كل صف دون صفوف فارغة بينها. السجل الموجود يُربط ولا يُغيَّر."
		}
	}
	return cols
}

// kpiWorkbookMasterCol is the column letter of key on a workbook master data sheet.
func kpiWorkbookMasterCol(kind, key string) string {
	return templateColLetter(kpiWorkbookMasterColumns(kind), key)
}

func buildKpiMasterSheet(name, kind string) utils.SheetConfig {
	return utils.SheetConfig{Name: name, Columns: toExcelColumns(kpiWorkbookMasterColumns(kind)), DataRows: kpiTemplateMasterRows,
		FreezeHeader: true, ConditionalFills: masterTemplateFills(kpiWorkbookMasterEntity(kind))}
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

func buildKpiDictionarySheet(name, kpiType string) utils.SheetConfig {
	cols := markTemplateRequired(dictColumns(kpiType), kpiTemplateDictionaryColumns(kpiType))
	// Sub-Criteria lists only the picked Criteria's sub-criteria.
	cols = withFollowedGoal(cols, "objective", lookupOpObjName, lookupOpObjGoal, "Objective", "الهدف التشغيلي")
	cols = append(cols, criterionNoHelperColumn(templateColLetter(cols, "criterion")))
	for i := range cols {
		if cols[i].key == "sub_criterion" {
			cols[i].listFormula = filteredList("criterion_no_key", lookupSubCritNo, lookupSubCrit, listSubCriteria, lookupCritNo)
		}
	}
	fills := append(followedGoalFills("goal", "objective", lookupOpObjName, lookupOpObjGoal),
		mismatchFill("sub_criterion", "criterion_no_key", lookupSubCritNo, lookupSubCrit, lookupCritNo),
		ambiguousNameFill("objective", lookupOpObjName))
	return utils.SheetConfig{Name: name, Columns: toExcelColumns(cols), DataRows: 4999, FreezeHeader: true, ConditionalFills: fills}
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
	// Metric Code lists only the picked KPI's metric codes.
	cols = append(cols, kpiCodeHelperColumn(templateColLetter(cols, "code")))
	perfMetricType := metricPeriodTypeColumn(templateColLetter(cols, "kpi_code_key"), templateColLetter(cols, "metric_code"), lookupMetricCodeKey, lookupMetricCodeFreq)
	// No Metric Code: the import uses the KPI's only metric.
	perfMetricType.formula = fmt.Sprintf(`IF(AND($%[1]s{row}<>"",$%[2]s{row}=""),IFERROR(INDEX(%[4]s,MATCH($%[1]s{row},%[3]s,0))&"",""),%[5]s)`,
		templateColLetter(cols, "kpi_code_key"), templateColLetter(cols, "metric_code"), lookupSoleMetricKpi, lookupSoleMetricFreq, perfMetricType.formula)
	cols = append(cols, perfMetricType, periodTypeColumn(templateColLetter(cols, "period")))
	for i := range cols {
		if cols[i].key == "metric_code" {
			cols[i].listFormula = filteredList("kpi_code_key", lookupMetricCodeKpi, lookupMetricCode, listMetricCodes, lookupSysKpiCode)
		}
	}
	return utils.SheetConfig{Name: kpiTemplatePerformanceSheet, Columns: toExcelColumns(cols), DataRows: 4999, FreezeHeader: true, AutoFilter: true,
		ConditionalFills: []utils.ConditionalFill{
			mismatchFill("metric_code", "kpi_code_key", lookupMetricCodeKpi, lookupMetricCode, lookupSysKpiCode),
			performancePeriodFill(),
		}}
}

func toExcelColumns(columns []kpiTemplateColumn) []utils.ColumnConfig {
	result := make([]utils.ColumnConfig, 0, len(columns))
	for _, col := range columns {
		converted := utils.ColumnConfig{
			Key:          col.key,
			Name:         utils.LocalizedText{EN: col.en, AR: col.ar},
			Width:        col.width,
			Required:     col.required,
			HeaderColor:  col.color,
			Hint:         utils.LocalizedText{EN: col.hintEn, AR: col.hintAr},
			Formula:      col.formula,
			FormulaRows:  kpiTemplateFormulaRows,
			AllowIf:      col.allowIf,
			AllowIfError: utils.LocalizedText{EN: col.allowErrEn, AR: col.allowErrAr},
			Hidden:       col.hidden,
			Text:         col.text,
		}
		source := col.list
		if col.listFormula != "" {
			source = col.listFormula
		}
		switch {
		case source != "":
			converted.Dropdown = &utils.DropdownConfig{
				Source:      source,
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
			promptEn, promptAr := strings.Join(col.values, ", "), strings.Join(col.valuesAr, ", ")
			if col.blankDefault != "" {
				defAr := col.blankDefault
				for i, v := range col.values {
					if v == col.blankDefault && i < len(col.valuesAr) {
						defAr = col.valuesAr[i]
					}
				}
				promptEn += ". Blank on a new record: " + col.blankDefault + " (the form's default); on update, blank keeps the stored value."
				promptAr += ". الخلية الفارغة في سجل جديد: " + defAr + " (القيمة الافتراضية في النموذج)؛ وعند التحديث تبقى القيمة المحفوظة."
			}
			converted.Dropdown = &utils.DropdownConfig{
				Values: col.values, ValuesAR: col.valuesAr,
				PromptTitle: utils.LocalizedText{EN: "Allowed values", AR: "القيم المسموحة"},
				Prompt:      utils.LocalizedText{EN: promptEn, AR: promptAr},
				ErrorTitle:  utils.LocalizedText{EN: "Invalid value", AR: "قيمة غير صالحة"},
				Error:       utils.LocalizedText{EN: "Choose one of: " + strings.Join(col.values, ", "), AR: "اختر أحد الخيارات: " + strings.Join(col.valuesAr, ", ")},
			}
		}
		result = append(result, converted)
	}
	return result
}

// kpiTemplateDictionaryColumns lists a Dictionary sheet's columns in the
// order of the KPI create form (the same for every KPI type), grouped by
// header colour: basic details, strategic alignment, ownership, measurement.
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
	codeHint := map[string][2]string{
		models.KPITypeStrategic:   {"e.g. KPI-P1-01-01", "مثال KPI-P1-01-01"},
		models.KPITypeOperational: {"e.g. OP-P1-01-01", "مثال OP-P1-01-01"},
		models.KPITypeAward:       {"e.g. KPI-AW-05-01", "مثال KPI-AW-05-01"},
	}[kpiType]

	cols := group(colorBasic,
		kpiTemplateColumn{key: "code", ar: "الكود", en: "Code", width: 18,
			hintEn: "Unique code: uppercase letters and digits separated by hyphens, " + codeHint[0] + ".",
			hintAr: "كود فريد: أحرف كبيرة وأرقام مفصولة بشرطات، " + codeHint[1] + "."},
		kpiTemplateColumn{key: "name_en", ar: "مؤشر الأمانة (إنجليزي)", en: "EPM KPI (EN)", width: 40, required: true},
		kpiTemplateColumn{key: "name_ar", ar: "مؤشر الأمانة (عربي)", en: "EPM KPI (AR)", width: 40},
	)

	// Read-only on the KPI form: no dropdown here; it follows the Objective.
	goal := kpiTemplateColumn{key: "goal", en: "Goal", ar: "الهدف", width: 32}
	goal.hintEn, goal.hintAr = "Filled automatically from the Objective (read-only, as on the KPI form). To change it, change the Objective.",
		"يُعبأ تلقائياً من الهدف التشغيلي (للقراءة فقط كما في نموذج المؤشر). لتغييره غيّر الهدف التشغيلي."
	objective := pick("objective", "Objective", "الهدف التشغيلي", listProcesses, 36)
	objective.required = true // checked per row by the importer
	objective.hintEn, objective.hintAr = "An operational objective (Lists sheet or Operational Objectives sheet).", "هدف تشغيلي (من ورقة القوائم أو ورقة الأهداف التشغيلية)."
	criteria := pick("criterion", "Criteria", "المعيار", listCriteria, 30)
	criteria.hintEn, criteria.hintAr = "Optional. Narrows the Sub-Criteria; only the sub-criterion is stored.", "اختياري. يحدد المعايير الفرعية؛ ويُحفظ المعيار الفرعي فقط."
	sub := pick("sub_criterion", "Sub-Criteria", "المعيار الفرعي", listSubCriteria, 34)
	sub.hintEn, sub.hintAr = "Optional, e.g. \"5.1 - Name\". Must belong to the Criteria when both are given.", "اختياري، مثال \"5.1 - الاسم\". يجب أن يتبع المعيار عند تعبئة الاثنين."
	pillar := pick("pillar", "Pillar", "الركيزة", listPillars, 30)
	pillar.hintEn, pillar.hintAr = "Filled automatically from the Objective in this sheet; you can change it.", "يُعبأ تلقائياً من الهدف في هذه الورقة؛ ويمكنك تغييره."
	cols = append(cols, group(colorAlignment,
		objective, goal, criteria, sub, pillar, // Objective first: Goal follows it
		pick("award_measure", "Award KPIs and Measures", "مؤشرات ومقاييس الجائزة", listAwardMeasures, 30),
		pick("domain", "Domains", "المجالات", listDomains, 30),
	)...)

	// No Owner Type / Owner Organization: KPIs imported from Excel are
	// Internal; an external owner is set on the KPI form. (The import still
	// reads both columns in older files.)
	cols = append(cols, group(colorOwnership,
		pick("owner_dept", "Owner Department", "الإدارة المالكة", listDepartments, 30),
		pick("agency", "Owning Agency", "الوكالة / الإدارة المالكة", listDepartments, 30),
		kpiTemplateColumn{key: "related_depts", ar: "الإدارات ذات الصلة", en: "Related Departments", width: 34,
			hintEn: "One or more names from the Departments column of the Lists sheet, separated by ; (semicolon).",
			hintAr: "اسم أو أكثر من عمود الإدارات في ورقة القوائم، مفصولة بـ ; (فاصلة منقوطة)."},
	)...)

	cols = append(cols, group(colorMeasurement,
		kpiTemplateColumn{key: "classification", ar: "تصنيف المؤشر", en: "KPI Classification", width: 20,
			values: []string{"KPI", "Measurement", "Output"}, valuesAr: []string{"مؤشر", "مقياس", "مخرج"}},
		kpiTemplateColumn{key: "polarity", ar: "الاتجاه", en: "Polarity", width: 16,
			values: []string{"Ascending", "Descending"}, valuesAr: []string{"تصاعدي", "تنازلي"}},
		kpiTemplateColumn{key: "target_requirement", ar: "متطلب المستهدف", en: "Target Requirement", width: 20,
			values: []string{"Mandatory", "Optional"}, valuesAr: []string{"إلزامي", "اختياري"}, blankDefault: "Mandatory"},
		kpiTemplateColumn{key: "frequency", ar: "تكرار الإبلاغ", en: "Reporting Frequency", width: 18,
			values:   []string{"Monthly", "Quarterly", "Semi-Annual", "Annual", "Custom"},
			valuesAr: []string{"شهري", "ربع سنوي", "نصف سنوي", "سنوي", "مخصص"}},
		kpiTemplateColumn{key: "baseline", ar: "خط الأساس", en: "Baseline", width: 14},
		kpiTemplateColumn{key: "unit", ar: "وحدة القياس", en: "Unit of Measure", width: 16},
		kpiTemplateColumn{key: "description_en", ar: "الوصف (إنجليزي)", en: "Description (EN)", width: 40},
		kpiTemplateColumn{key: "description_ar", ar: "الوصف (عربي)", en: "Description (AR)", width: 40},
		kpiTemplateColumn{key: "formula", ar: "الصيغة", en: "Formula", width: 30}, // the KPI form's label
		pick("data_source", "Data Source", "مصدر البيانات", listDataSources, 26),
	)...)

	// Strategic Goal and Pillar follow the Objective picked on the row.
	obj := templateColLetter(cols, "objective")
	for i := range cols {
		switch cols[i].key {
		case "goal":
			cols[i].formula = lookupFormula(obj, lookupOpObjName, lookupOpObjGoal)
		case "pillar":
			cols[i].formula = lookupFormula(obj, lookupOpObjName, lookupOpObjPillar)
		}
	}
	return cols
}

// kpiLabel is how a KPI is shown in the Targets KPI dropdown: "CODE - Name".
func kpiLabel(code, name string) string {
	if strings.TrimSpace(name) == "" {
		return code
	}
	return code + " - " + strings.TrimSpace(name)
}

// targetPeriodChoices are the Targets Period dropdown values: period codes in
// the English template, Arabic names in the Arabic one (the importer accepts
// both), then the custom date range option.
func targetPeriodChoices(lang string) []string {
	if lang == KpiTemplateLangAR {
		return []string{"يناير", "فبراير", "مارس", "أبريل", "مايو", "يونيو", "يوليو", "أغسطس", "سبتمبر", "أكتوبر", "نوفمبر", "ديسمبر",
			"الربع 1", "الربع 2", "الربع 3", "الربع 4", "النصف 1", "النصف 2", "سنوي", customRangeChoice(lang)}
	}
	return []string{"jan", "feb", "mar", "apr", "may", "jun", "jul", "aug", "sep", "oct", "nov", "dec", "q1", "q2", "q3", "q4", "h1", "h2", "annual", customRangeChoice(lang)}
}

// targetStatusColumn is the Targets sheet's Status: what the Set Target
// form's buttons decide (Save as Draft / Submit). Approval stays on the
// Targets page.
func targetStatusColumn() kpiTemplateColumn {
	return kpiTemplateColumn{key: "target_status", en: "Status", ar: "الحالة", width: 14,
		values: []string{"Draft", "Submitted"}, valuesAr: []string{"مسودة", "مقدم"},
		hintEn: "Draft (or empty) = Save as Draft. Submitted = Submit for approval. Approving stays on the Targets page.",
		hintAr: "مسودة (أو فارغ) = حفظ كمسودة. مقدم = إرسال للاعتماد. يبقى الاعتماد في صفحة المستهدفات."}
}

// targetSheetFills are the Targets sheet's colour rules: threshold cells
// used / not used by the row's Threshold Mode, and a Metric that does not
// belong to the row's KPI.
func targetSheetFills() []utils.ConditionalFill {
	fills := append(targetThresholdFills(), targetMetricMismatchFill(), targetPeriodFill())
	return append(fills, periodRangeFills()...)
}

// targetMetricMismatchFill turns the Metric cell red when it is not a metric
// of the row's KPI, e.g. after the KPI was changed (Excel cannot clear the
// cell without a macro). Only KPIs with metrics in the system are checked,
// as for the filtered dropdown; the import rejects any mismatch anyway.
func targetMetricMismatchFill() utils.ConditionalFill {
	return mismatchFill("metric", "kpi_code_key", lookupMetricKpi, lookupMetricName, lookupSysKpiCode)
}

// targetThresholdFills shows on the Targets sheet which threshold cells the
// row's Threshold Mode uses, as the Set Target form shows only those fields:
// Percentage of Target / Absolute Values use Excellent, Achieved and Warning;
// Target Range uses Range Min and Max; Use Global KPI Rules (also an empty
// cell) and No Thresholds use none. Unused cells are greyed out, used ones
// tinted green; rows without a KPI are left plain. English and Arabic mode
// names are both checked, so it works in either template. (Values typed in
// unused cells are ignored by the import, with a warning.)
func targetThresholdFills() []utils.ConditionalFill {
	kpi := "${col:kpi_code}2"
	usesThresholds, usesRange := targetThresholdModeTests()
	thresholds, ranges := targetThresholdKeys, targetRangeKeys
	const unusedFill, unusedFont, usedFill = "E7E6E6", "A6A6A6", "E2EFDA"
	return []utils.ConditionalFill{
		{Keys: thresholds, Formula: fmt.Sprintf(`AND(%s<>"",NOT(%s))`, kpi, usesThresholds), Fill: unusedFill, FontColor: unusedFont},
		{Keys: ranges, Formula: fmt.Sprintf(`AND(%s<>"",NOT(%s))`, kpi, usesRange), Fill: unusedFill, FontColor: unusedFont},
		{Keys: thresholds, Formula: fmt.Sprintf(`AND(%s<>"",%s)`, kpi, usesThresholds), Fill: usedFill},
		{Keys: ranges, Formula: fmt.Sprintf(`AND(%s<>"",%s)`, kpi, usesRange), Fill: usedFill},
	}
}

var (
	targetThresholdKeys = []string{"excellent_threshold", "achieved_threshold", "warning_threshold"}
	targetRangeKeys     = []string{"target_range_min", "target_range_max"}
)

// targetThresholdModeTests are Excel tests (row 2, relative) for "this row's
// Threshold Mode uses Excellent / Achieved / Warning" and "... uses Range
// Min / Max", with the English and Arabic mode names.
func targetThresholdModeTests() (usesThresholds, usesRange string) {
	mode := "${col:threshold_mode}2"
	is := func(values ...string) string {
		parts := make([]string, len(values))
		for i, v := range values {
			parts[i] = fmt.Sprintf(`%s="%s"`, mode, v)
		}
		return "OR(" + strings.Join(parts, ",") + ")"
	}
	return is("Percentage of Target", "Absolute Values", "نسبة من المستهدف", "قيم مطلقة"), is("Target Range", "نطاق مستهدف")
}

// withThresholdTypingRules stops typing a threshold the row's Threshold Mode
// does not use, as the Set Target form shows only the fields of the chosen
// mode. (Excel checks typed values, not pasted ones; the import ignores
// unused thresholds with a warning.)
func withThresholdTypingRules(cols []kpiTemplateColumn) []kpiTemplateColumn {
	usesThresholds, usesRange := targetThresholdModeTests()
	for i := range cols {
		switch {
		case slices.Contains(targetThresholdKeys, cols[i].key):
			cols[i].allowIf = usesThresholds
			cols[i].allowErrEn = "Excellent, Achieved and Warning Threshold are used only when Threshold Mode is Percentage of Target or Absolute Values. Choose the Threshold Mode first."
			cols[i].allowErrAr = "حدود التميز والتحقق والتحذير تُستخدم فقط عندما يكون نمط الحدود نسبة من المستهدف أو قيم مطلقة. اختر نمط الحدود أولاً."
		case slices.Contains(targetRangeKeys, cols[i].key):
			cols[i].allowIf = usesRange
			cols[i].allowErrEn = "Range Min and Range Max are used only when Threshold Mode is Target Range. Choose the Threshold Mode first."
			cols[i].allowErrAr = "أدنى وأعلى قيمة للنطاق تُستخدمان فقط عندما يكون نمط الحدود نطاق مستهدف. اختر نمط الحدود أولاً."
		}
	}
	return cols
}

// kpiCodeHelperColumn is a hidden Targets column holding the code of the
// row's KPI ("CODE - Name" or a typed code → CODE), for the Metric dropdown.
// It has no header, so the import ignores it.
func kpiCodeHelperColumn(kpiCol string) kpiTemplateColumn {
	return kpiTemplateColumn{key: "kpi_code_key", width: 4, hidden: true,
		formula: fmt.Sprintf(`IF($%[1]s{row}="","",TRIM(LEFT($%[1]s{row},FIND(" - ",$%[1]s{row}&" - ")-1)))`, kpiCol)}
}

// criterionNoHelperColumn is a hidden Dictionary column holding the number
// of the row's Criteria ("Criterion 5 - Name" / "المعيار 5 - …" or a typed
// 5 → 5), for the Sub-Criteria dropdown. No header: the import ignores it.
func criterionNoHelperColumn(critCol string) kpiTemplateColumn {
	label := fmt.Sprintf(`TRIM(LEFT($%[1]s{row},FIND(" - ",$%[1]s{row}&" - ")-1))`, critCol)
	return kpiTemplateColumn{key: "criterion_no_key", width: 4, hidden: true,
		formula: fmt.Sprintf(`IF($%[1]s{row}="","",IFERROR(TRIM(MID(%[2]s,FIND(" ",%[2]s)+1,20)),%[2]s))`, critCol, label)}
}

// metricsOfKpiList is the Metric dropdown source: the metrics of the row's
// KPI (helper column keyCol) when the system has metrics for it, otherwise
// every metric name (a KPI new in this file, or no KPI picked yet).
func metricsOfKpiList(keyCol string) string {
	return filteredList(keyCol, lookupMetricKpi, lookupMetricName, listMetricNames, lookupSysKpiCode)
}

// filteredList is a dropdown source showing only the values the lookup table
// (lookupKey → lookupValue, grouped by key) has for the row's key (helper
// column keyCol). A key in the system (listed in known) without values gets
// an empty list; an empty key, or one not in the system yet (new in this
// file), gets the whole fallback list.
func filteredList(keyCol, lookupKey, lookupValue, fallback, known string) string {
	key := "${col:" + keyCol + "}2"
	return fmt.Sprintf(`IF(%[1]s="",%[2]s,IF(COUNTIF(%[3]s,%[1]s),OFFSET(%[4]s,MATCH(%[1]s,%[3]s,0)-1,0,COUNTIF(%[3]s,%[1]s)),IF(COUNTIF(%[5]s,%[1]s),%[6]s,%[2]s)))`,
		key, fallback, lookupKey, lookupValue, known, listEmpty)
}

// Excel's "Bad" style, for a cell whose value no longer fits the row.
const badFill, badFont = "FFC7CE", "9C0006"

// mismatchFill turns valueKey's cell red when it holds an existing value
// (one of lookupValue) that belongs to another key than the row's (helper
// column keyCol) — what is left behind when the key column is changed
// (Excel cannot clear the cell without a macro). Only keys in the system
// (listed in known) are checked, so a key without values makes any existing
// value red. A new value typed on purpose (e.g. a metric on this workbook's
// Metrics sheet) is not marked: the import checks it.
func mismatchFill(valueKey, keyCol, lookupKey, lookupValue, known string) utils.ConditionalFill {
	key, value := "${col:"+keyCol+"}2", "${col:"+valueKey+"}2"
	return utils.ConditionalFill{
		Keys: []string{valueKey},
		Formula: fmt.Sprintf(`AND(%[2]s<>"",%[1]s<>"",COUNTIF(%[5]s,%[1]s)>0,COUNTIF(%[4]s,%[2]s)>0,COUNTIFS(%[3]s,%[1]s,%[4]s,%[2]s)=0)`,
			key, value, lookupKey, lookupValue, known),
		Fill: badFill, FontColor: badFont,
	}
}

// parentGoalMismatchFill turns redKey's cell red when the row's Parent
// Objective (parentKey) belongs to another strategic goal than the row's
// Goal (goalKey) — the import rejects such a row.
func parentGoalMismatchFill(goalKey, parentKey, redKey string) utils.ConditionalFill {
	g, p := "${col:"+goalKey+"}2", "${col:"+parentKey+"}2"
	return utils.ConditionalFill{
		Keys: []string{redKey},
		// Not checked when the parent's name is ambiguous (ambiguousNameFill).
		Formula: fmt.Sprintf(`AND(%[2]s<>"",%[1]s<>"",COUNTIF(%[3]s,%[2]s)=1,INDEX(%[4]s,MATCH(%[2]s,%[3]s,0))<>%[1]s)`,
			g, p, lookupParentName, lookupParentGoal),
		Fill: badFill, FontColor: badFont,
	}
}

// subNoMismatchFill turns the Sub-Criterion No. red when it does not start
// with the picked criterion's number and a dot (e.g. 5.1 for criterion 5) —
// the import rejects such a row. Criteria not yet in the system are not
// checked.
func subNoMismatchFill(critKey, noKey string) utils.ConditionalFill {
	c, n := "${col:"+critKey+"}2", "${col:"+noKey+"}2"
	no := fmt.Sprintf(`INDEX(%[2]s,MATCH(%[1]s,%[3]s,0))`, c, lookupCritNo, lookupCritName)
	return utils.ConditionalFill{
		Keys: []string{noKey},
		Formula: fmt.Sprintf(`AND(%[2]s<>"",%[1]s<>"",ISNUMBER(MATCH(%[1]s,%[3]s,0)),LEFT(%[2]s&"",LEN(%[4]s)+1)<>%[4]s&".")`,
			c, n, lookupCritName, no),
		Fill: badFill, FontColor: badFont,
	}
}

// ambiguousFill, ambiguousFont colour a name that matches several records.
const ambiguousFill, ambiguousFont = "FFEB9C", "9C5700"

// ambiguousNameFill turns a cell amber when its name matches more than one
// record of lookupName: the auto-fill that reads it (Goal, Pillar) is then
// left blank, and the import rejects the row until the duplicates are renamed.
func ambiguousNameFill(key, lookupName string) utils.ConditionalFill {
	return utils.ConditionalFill{
		Keys:    []string{key},
		Formula: fmt.Sprintf(`AND(${col:%[1]s}2<>"",COUNTIF(%[2]s,${col:%[1]s}2)>1)`, key, lookupName),
		Fill:    ambiguousFill, FontColor: ambiguousFont,
	}
}

// masterTemplateFills are the colour rules of a Master Data import sheet
// (its own template, or its tab in the KPI workbook).
func masterTemplateFills(entity string) []utils.ConditionalFill {
	switch entity {
	case KpiMasterInitiative: // Goal fills from the Parent Objective; red when they disagree
		return []utils.ConditionalFill{parentGoalMismatchFill("goal", "objective", "objective"), ambiguousNameFill("objective", lookupParentName)}
	case KpiMasterProcess: // Goal follows the Parent Objective (read-only on the form)
		return append(followedGoalFills("goal", "operational_objective", lookupParentName, lookupParentGoal),
			ambiguousNameFill("operational_objective", lookupParentName))
	case KpiMasterAwardSubCriterion:
		return []utils.ConditionalFill{subNoMismatchFill("award_criterion", "sub_no")}
	}
	return nil
}

// periodTypeLetter is the period type of a normalized reporting frequency
// (Ad Hoc and unknown have none, so they are not checked).
var periodTypeLetter = map[string]string{
	models.KPIFrequencyMonthly: "m", models.KPIFrequencyQuarterly: "q",
	models.KPIFrequencySemiAnnual: "h", models.KPIFrequencyAnnually: "a",
}

// periodTypeTable lists every Period value the templates offer (codes,
// English labels and month names, Arabic names) with its period type.
func periodTypeTable() (names, types []string) {
	add := func(t string, vs ...string) {
		for _, v := range vs {
			names, types = append(names, v), append(types, t)
		}
	}
	add("m", "jan", "feb", "mar", "apr", "may", "jun", "jul", "aug", "sep", "oct", "nov", "dec",
		"january", "february", "march", "april", "june", "july", "august", "september", "october", "november", "december",
		"يناير", "فبراير", "مارس", "أبريل", "مايو", "يونيو", "يوليو", "أغسطس", "سبتمبر", "أكتوبر", "نوفمبر", "ديسمبر")
	add("q", "q1", "q2", "q3", "q4", "الربع 1", "الربع 2", "الربع 3", "الربع 4")
	add("h", "h1", "h2", "النصف 1", "النصف 2")
	add("a", "annual", "سنوي")
	return names, types
}

// metricPeriodTypeColumn is a hidden helper with the reporting period type
// of the row's metric, looked up by "KPI code|metric" (kpiKeyCol: the KPI
// code helper; metricCol: Metric name or Metric Code). Wildcards in the
// name are escaped for MATCH. Empty when unknown.
func metricPeriodTypeColumn(kpiKeyCol, metricCol, lookupKey, lookupFreq string) kpiTemplateColumn {
	esc := fmt.Sprintf(`SUBSTITUTE(SUBSTITUTE(SUBSTITUTE($%[1]s{row}&"|"&$%[2]s{row},"~","~~"),"*","~*"),"?","~?")`, kpiKeyCol, metricCol)
	return kpiTemplateColumn{key: "metric_period_type", width: 4, hidden: true,
		formula: fmt.Sprintf(`IF(OR($%[1]s{row}="",$%[2]s{row}=""),"",IFERROR(INDEX(%[4]s,MATCH(%[5]s,%[3]s,0))&"",""))`,
			kpiKeyCol, metricCol, lookupKey, lookupFreq, esc)}
}

// periodTypeColumn is a hidden helper with the type of the row's Period
// value; empty for a custom date range or a value it does not know.
func periodTypeColumn(periodCol string) kpiTemplateColumn {
	return kpiTemplateColumn{key: "period_type", width: 4, hidden: true,
		formula: fmt.Sprintf(`IF($%[1]s{row}="","",IFERROR(INDEX(%[3]s,MATCH($%[1]s{row}&"",%[2]s,0))&"",""))`,
			periodCol, lookupPeriodName, lookupPeriodType)}
}

// targetPeriodFill turns the Targets Period red when it does not fit, as
// the import checks: an Annual Target needs the Annual period (whatever the
// frequency); a Period Target needs a period of the metric's reporting
// frequency. Milestone / Ad Hoc targets, unknown periods and metrics without
// a known frequency are not checked.
func targetPeriodFill() utils.ConditionalFill {
	p, f, t := "${col:period_type}2", "${col:metric_period_type}2", "${col:target_type}2"
	annual := fmt.Sprintf(`OR(%[1]s="Annual Target",%[1]s="مستهدف سنوي")`, t)
	milestone := fmt.Sprintf(`OR(%[1]s="Milestone / Ad Hoc",%[1]s="مرحلة / حسب الحاجة")`, t)
	return utils.ConditionalFill{
		Keys:    []string{"period_code"},
		Formula: fmt.Sprintf(`AND(%[1]s<>"",NOT(%[4]s),OR(%[3]s,%[2]s<>""),%[1]s<>IF(%[3]s,"a",%[2]s))`, p, f, annual, milestone),
		Fill:    badFill, FontColor: badFont,
	}
}

// performancePeriodFill turns the Performance Period red when it is not a
// period of the metric's reporting frequency (an empty Metric Code: the
// KPI's only metric). Metrics without a known frequency, KPIs with no or
// several metrics (without a Metric Code) and unknown periods are not checked.
func performancePeriodFill() utils.ConditionalFill {
	p, f := "${col:period_type}2", "${col:metric_period_type}2"
	return utils.ConditionalFill{
		Keys:    []string{"period"},
		Formula: fmt.Sprintf(`AND(%[1]s<>"",%[2]s<>"",%[1]s<>%[2]s)`, p, f),
		Fill:    badFill, FontColor: badFont,
	}
}

// followedGoal is the Excel test "the row's Goal differs from the goal of
// its objective" (looked up as the auto-fill does: blank when the objective
// is unknown or its name is ambiguous, and then not checked).
func followedGoalMismatch(goalKey, parentKey, lookupName, lookupGoal string) string {
	g, p := "${col:"+goalKey+"}2", "${col:"+parentKey+"}2"
	expected := fmt.Sprintf(`IF(COUNTIF(%[2]s,%[1]s)<>1,"",INDEX(%[3]s,MATCH(%[1]s,%[2]s,0))&"")`, p, lookupName, lookupGoal)
	return fmt.Sprintf(`AND(%[1]s<>"",%[2]s<>"",%[1]s<>%[2]s)`, g, expected)
}

// withFollowedGoal makes Goal a cell that follows the objective (parentKey):
// it keeps its auto-fill formula, has no dropdown, and refuses a typed goal
// other than the objective's.
func withFollowedGoal(cols []kpiTemplateColumn, parentKey, lookupName, lookupGoal, parentEn, parentAr string) []kpiTemplateColumn {
	for i := range cols {
		if cols[i].key == "goal" {
			cols[i].list, cols[i].values, cols[i].valuesAr = "", nil, nil
			cols[i].allowIf = "NOT(" + followedGoalMismatch("goal", parentKey, lookupName, lookupGoal) + ")"
			cols[i].allowErrEn = "Goal follows the " + parentEn + ". Change the " + parentEn + " instead."
			cols[i].allowErrAr = "يتبع الهدف " + parentAr + ". غيّر " + parentAr + " بدلاً من ذلك."
		}
	}
	return cols
}

// followedGoalFills greys out a Goal that follows its objective (read-only,
// as on the form) and turns it red when it differs from the objective's
// goal (a pasted value: the KPI import then uses the objective's goal, the
// Operational Objectives import rejects the row).
func followedGoalFills(goalKey, parentKey, lookupName, lookupGoal string) []utils.ConditionalFill {
	mismatch := followedGoalMismatch(goalKey, parentKey, lookupName, lookupGoal)
	return []utils.ConditionalFill{
		{Keys: []string{goalKey}, Formula: mismatch, Fill: badFill, FontColor: badFont},
		{Keys: []string{goalKey}, Formula: "NOT(" + mismatch + ")", Fill: "E7E6E6", FontColor: "595959"},
	}
}

// periodRangeUsed is the Excel test for "Period Start / Period End are used
// on this row": a custom date range, i.e. a Milestone / Ad Hoc target.
func periodRangeUsed() string {
	p, t := "${col:period_code}2", "${col:target_type}2"
	return fmt.Sprintf(`OR(%[1]s="%[3]s",%[1]s="%[4]s",%[2]s="Milestone / Ad Hoc",%[2]s="مرحلة / حسب الحاجة")`,
		p, t, customRangeChoice(KpiTemplateLangEN), customRangeChoice(KpiTemplateLangAR))
}

// withPeriodRangeTypingRules stops typing Period Start / End on a row that
// is not a custom date range (the import only uses them for one).
func withPeriodRangeTypingRules(cols []kpiTemplateColumn) []kpiTemplateColumn {
	for i := range cols {
		if cols[i].key == "period_start" || cols[i].key == "period_end" {
			cols[i].allowIf = periodRangeUsed()
			cols[i].allowErrEn = "Period Start and Period End are used only for a custom date range. Choose \"" + customRangeChoice(KpiTemplateLangEN) + "\" as the Period first."
			cols[i].allowErrAr = "تُستخدم بداية الفترة ونهايتها فقط لنطاق التاريخ المخصص. اختر \"" + customRangeChoice(KpiTemplateLangAR) + "\" في الفترة أولاً."
		}
	}
	return cols
}

// periodRangeFills greys out Period Start / End when the row is not a custom
// date range and tints them green when it is (rows without a KPI: plain).
func periodRangeFills() []utils.ConditionalFill {
	kpi, keys := "${col:kpi_code}2", []string{"period_start", "period_end"}
	return []utils.ConditionalFill{
		{Keys: keys, Formula: fmt.Sprintf(`AND(%s<>"",NOT(%s))`, kpi, periodRangeUsed()), Fill: "E7E6E6", FontColor: "A6A6A6"},
		{Keys: keys, Formula: fmt.Sprintf(`AND(%s<>"",%s)`, kpi, periodRangeUsed()), Fill: "E2EFDA"},
	}
}

// customRangeChoice is the Period option for a custom date range.
func customRangeChoice(lang string) string {
	if lang == KpiTemplateLangAR {
		return "نطاق تاريخ مخصص"
	}
	return "Custom date range"
}

// kpiTargetSheetColumns are the Targets sheet columns, in the order of the
// target form: KPI ("CODE - Name"), Metric (name), Target Year, Period (or a
// custom date range with its start and end), value, type, basis, rationale
// and thresholds.
func kpiTargetSheetColumns(lang string) []kpiTemplateColumn {
	ar := lang == KpiTemplateLangAR
	custom := customRangeChoice(lang)
	col := func(key, en, ar string, width float64) kpiTemplateColumn {
		return kpiTemplateColumn{key: key, en: en, ar: ar, width: width}
	}
	byKey := map[string]kpiTemplateColumn{}
	for _, c := range kpiTargetTemplateColumns() {
		byKey[c.key] = c
	}
	kpi := col("kpi_code", "KPI", "المؤشر", 44)
	kpi.required, kpi.list = true, listKpiLabels
	kpi.hintEn, kpi.hintAr = "Pick the KPI (code - name).", "اختر المؤشر (الكود - الاسم)."
	metric := col("metric", "Metric", "المقياس", 32)
	metric.required, metric.list = true, listMetricNames // mandatory, as on the Set Target form
	metric.hintEn, metric.hintAr = "Required: the metric of the KPI the target is for.", "إلزامي: مقياس المؤشر الذي يخص المستهدف."
	year := byKey["target_year"]
	year.required = true // except for a custom date range (see hint)
	year.hintEn, year.hintAr = "Required, except for a custom date range.", "إلزامية، إلا لنطاق التاريخ المخصص."
	period := col("period_code", "Period", "الفترة", 20)
	period.required, period.list = true, listPeriodCodes
	period.hintEn = "jan–dec, q1–q4, h1/h2 or annual (must match the metric's reporting frequency), or \"" + custom + "\" and fill Period Start and Period End."
	period.hintAr = "jan–dec أو q1–q4 أو h1/h2 أو annual (حسب تكرار الإبلاغ للمقياس)، أو \"" + custom + "\" واملأ بداية الفترة ونهايتها."
	start, end := byKey["period_start"], byKey["period_end"]
	start.hintEn, start.hintAr = "Only for \""+custom+"\". Format YYYY-MM-DD.", "فقط عند اختيار \""+custom+"\". بصيغة YYYY-MM-DD."
	end.hintEn, end.hintAr = start.hintEn, start.hintAr
	rationale := byKey["target_rationale"]
	rationale.required = true // as on the Set Target form
	rationale.hintEn = "Required: why this target value. When updating an existing target, an empty cell keeps its rationale."
	rationale.hintAr = "إلزامي: سبب قيمة المستهدف. عند تحديث مستهدف موجود، تُبقي الخلية الفارغة المبررات الحالية."
	value := byKey["target_value"]
	value.required = true
	typ := byKey["target_type"]
	typ.required = true
	typ.hintEn, typ.hintAr = "Filled from the Period: Milestone / Ad Hoc for a custom date range, Annual Target for annual, otherwise Period Target. You can change it.", "يُعبأ من الفترة: مرحلة / حسب الحاجة لنطاق التاريخ المخصص، ومستهدف سنوي للفترة السنوية، وإلا مستهدف فترة. ويمكنك تغييره."
	cols := []kpiTemplateColumn{kpi, metric, year, period, start, end, value, typ,
		byKey["target_basis"], rationale, byKey["threshold_mode"],
		byKey["excellent_threshold"], byKey["achieved_threshold"],
		col("target_range_min", "Range Min", "أدنى قيمة للنطاق", 14), col("target_range_max", "Range Max", "أعلى قيمة للنطاق", 14),
		byKey["warning_threshold"], targetStatusColumn()}

	milestone, annual, periodTarget := "Milestone / Ad Hoc", "Annual Target", "Period Target"
	if ar {
		milestone, annual, periodTarget = "مرحلة / حسب الحاجة", "مستهدف سنوي", "مستهدف فترة"
	}
	k, p := templateColLetter(cols, "kpi_code"), templateColLetter(cols, "period_code")
	cols = append(cols, kpiCodeHelperColumn(k))
	cols = append(cols,
		metricPeriodTypeColumn(templateColLetter(cols, "kpi_code_key"), templateColLetter(cols, "metric"), lookupMetricKey, lookupMetricFreq),
		periodTypeColumn(p))
	for i := range cols {
		if cols[i].key == "metric" {
			cols[i].listFormula = metricsOfKpiList("kpi_code_key")
		}
		if cols[i].key == "target_type" {
			cols[i].formula = fmt.Sprintf(`IF($%[1]s{row}="","",IF($%[2]s{row}="%[3]s","%[4]s",IF(OR(LOWER($%[2]s{row})="annual",$%[2]s{row}="سنوي"),"%[5]s","%[6]s")))`,
				k, p, custom, milestone, annual, periodTarget)
		}
	}
	return withPeriodRangeTypingRules(withThresholdTypingRules(cols))
}

// ─── Instructions ───────────────────────────────────────────────────────────

func buildKpiDictionaryInstructions() *utils.InstructionConfig {
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
				"Only if something is missing from the Lists sheet, one sheet per Master Data tab: Pillars and Enablers → Goals → Parent Objectives (choose the goal) → Operational Objectives (choose the parent objective; the goal follows) → Initiatives (choose the goal); and Domains, Data Sources, Segmentation Dimensions, Award Criteria → Award Sub-criteria (choose the criterion), Award KPI Measures (same columns as each tab's own import). One per row without blank rows, up to 200 rows per sheet (for more, use the Master Data import). Anything added appears in the dropdowns of the later sheets.",
				"فقط إذا كان هناك عنصر غير موجود في ورقة القوائم، ورقة لكل تبويب من البيانات الرئيسية: الركائز والممكنات ← الأهداف ← الأهداف الرئيسية (اختر الهدف) ← الأهداف التشغيلية (اختر الهدف الرئيسي؛ ويُؤخذ الهدف منه) ← المبادرات (اختر الهدف)؛ والمجالات ومصادر البيانات وأبعاد التجزئة ومعايير الجائزة ← المعايير الفرعية (اختر المعيار) ومقاييس مؤشرات الجائزة (بنفس أعمدة استيراد كل تبويب). عنصر في كل صف دون صفوف فارغة، وحتى 200 صف في كل ورقة (للمزيد استخدم استيراد البيانات الرئيسية). أي عنصر يُضاف يظهر في القوائم المنسدلة للأوراق التالية."),
			item("3. KPIs", "3. المؤشرات",
				"Fill the Strategic, Operational or Award Dictionary sheet, one KPI per row. The columns follow the New KPI form. Pick the Objective: the Goal and Pillar fill in from it.",
				"املأ ورقة الدليل الاستراتيجي أو التشغيلي أو الجائزة، مؤشر واحد في كل صف. الأعمدة بنفس ترتيب نموذج إنشاء المؤشر. اختر الهدف التشغيلي: يُعبأ الهدف والركيزة منه تلقائياً."),
			item("4. Metrics (optional)", "4. المقاييس (اختياري)",
				"The metrics of each KPI, same columns as the Metrics import. KPI Code can be a KPI from the Dictionary sheets of this file. A KPI with no metric here gets one default metric (with the KPI's reporting frequency) when it has targets or results.",
				"مقاييس كل مؤشر، بنفس أعمدة استيراد المقاييس. يمكن أن يكون كود المؤشر لمؤشر من أوراق الدليل في هذا الملف. المؤشر الذي ليس له مقياس هنا يحصل على مقياس افتراضي واحد (بتكرار إبلاغ المؤشر) عند وجود مستهدفات أو نتائج له."),
			item("4b. Targets (optional)", "4ب. المستهدفات (اختياري)",
				"Same columns as the Set Target form: pick the KPI (code - name) and the Metric (name), then the Target Year and Period — a month, quarter, half or annual, or \"Custom date range\" with Period Start and Period End. Target Type fills in from the Period.",
				"بنفس أعمدة نموذج تحديد المستهدف: اختر المؤشر (الكود - الاسم) والمقياس (الاسم)، ثم السنة المستهدفة والفترة — شهر أو ربع أو نصف أو سنوي، أو \"نطاق تاريخ مخصص\" مع بداية الفترة ونهايتها. يُعبأ نوع المستهدف من الفترة."),
			item("5. Performance (optional)", "5. الأداء (اختياري)",
				"One row per KPI (and metric) per period: Code, Metric Code (only if the KPI has several metrics), Year, Period, Target and Actual Result. The period must match the reporting frequency — a month for Monthly, Q1–Q4 for Quarterly, H1/H2 for Semi-Annual, Annual for Annual — and must have ended.",
				"صف واحد لكل مؤشر (ومقياس) لكل فترة: الكود وكود المقياس (فقط إذا كان للمؤشر عدة مقاييس) والسنة والفترة والمستهدف والنتيجة الفعلية. يجب أن تطابق الفترة تكرار الإبلاغ — شهر للشهري، الربع 1–4 للربع سنوي، النصف 1/2 للنصف سنوي، سنوي للسنوي — وأن تكون منتهية."),
			item("6. Import", "6. الاستيراد",
				"Save as .xlsx and upload it under KPI Dictionary Import. Click Validate, review the report (each rejected row is listed with its sheet, row number and reason), then Commit.",
				"احفظ الملف بصيغة .xlsx وارفعه في استيراد دليل المؤشرات. اضغط تحقق وراجع التقرير (كل صف مرفوض يظهر مع الورقة ورقم الصف والسبب)، ثم تنفيذ."),
		}, BlankAfter: true},
		{Title: text("Column colours on the Dictionary sheets", "ألوان الأعمدة في أوراق الدليل"), Items: []utils.InstructionItem{
			item("Dark blue", "أزرق داكن", "Basic details: code, EPM KPI (EN) and (AR).", "البيانات الأساسية: الكود ومؤشر الأمانة (إنجليزي) و(عربي)."),
			item("Teal", "أزرق مخضر", "Strategic alignment: Goal (filled from the Objective), Objective (required), Criteria, Sub-Criteria, Pillar (filled from the Objective), Award KPIs and Measures, Domains.", "المواءمة الاستراتيجية: الهدف (يُعبأ من الهدف التشغيلي) والهدف التشغيلي (إلزامي) والمعيار والمعيار الفرعي والركيزة (تُعبأ من الهدف التشغيلي) ومؤشرات ومقاييس الجائزة والمجالات."),
			item("Purple", "بنفسجي", "Ownership: owner department, owning agency, related departments.", "الملكية: الإدارة المالكة والوكالة المالكة والإدارات ذات الصلة."),
			item("Green", "أخضر", "Measurement: classification, polarity, target requirement, reporting frequency, baseline, unit, descriptions (EN/AR), formula, data source.", "القياس: التصنيف والاتجاه ومتطلب المستهدف وتكرار الإبلاغ وخط الأساس والوحدة والوصف (إنجليزي/عربي) والمعادلة ومصدر البيانات."),
			item("Red *", "علامة * حمراء", "Mandatory column.", "عمود إلزامي."),
			item("Amber cell", "خلية كهرمانية", "The name matches more than one record (for example two objectives with the same name), so Goal and Pillar cannot be filled in from it and the import rejects the row. Rename the duplicates in Master Data.", "الاسم يطابق أكثر من سجل (مثل هدفين بنفس الاسم)، لذلك لا يمكن تعبئة الهدف والركيزة منه ويرفض الاستيراد الصف. أعد تسمية السجلات المكررة في البيانات الرئيسية."),
		}, BlankAfter: true},
		{Title: text("Rules", "القواعد"), Items: []utils.InstructionItem{
			item("Dropdowns", "القوائم المنسدلة", "A value that is not in a dropdown shows a warning. Pillars, goals and objectives must exist in the system or on their sheet (add a new objective on the Operational Objectives sheet). Departments, organizations, domains, award criteria and measures that do not exist are created and reported as warnings.", "القيمة غير الموجودة في القائمة تُظهر تحذيراً. يجب أن تكون الركائز والأهداف موجودة في النظام أو في ورقتها (أضف الهدف الجديد في ورقة الأهداف التشغيلية). أما الإدارات والمنظمات والمجالات ومعايير ومقاييس الجائزة غير الموجودة فتُنشأ ويُبلّغ عنها كتحذير."),
			item("KPI code", "كود المؤشر", "Required and unique. Uppercase letters and digits separated by hyphens.", "مطلوب وفريد. أحرف كبيرة وأرقام مفصولة بشرطات."),
			item("Owner", "المالك", "New KPIs imported from this workbook are Internal; existing KPIs keep their owner type and organization on update. For an external owner, set Owner Type and Owner Organization on the KPI form after the import.", "المؤشرات الجديدة المستوردة من هذا المصنف داخلية؛ وتحتفظ المؤشرات الحالية بنوع المالك والمنظمة المالكة عند التحديث. للمالك الخارجي، حدّد نوع المالك والمنظمة المالكة في نموذج المؤشر بعد الاستيراد."),
			item("Related Departments", "الإدارات ذات الصلة", "Existing departments only, separated by ;. A name that is not found is reported and not linked.", "إدارات موجودة فقط مفصولة بـ ;. الاسم غير الموجود يُبلّغ عنه ولا يُربط."),
			item("Status", "الحالة", "New KPIs start as Draft and go through the KPI workflow; existing KPIs keep their workflow status.", "تبدأ المؤشرات الجديدة كمسودة وتمر بسير عمل المؤشر؛ وتحتفظ المؤشرات الحالية بحالتها."),
			item("Numbers", "الأرقام", "Baseline, targets and actual results are numbers; commas and a trailing % are accepted. Negative values are rejected.", "خط الأساس والمستهدفات والنتائج أرقام؛ تُقبل الفواصل والعلامة % في النهاية. تُرفض القيم السالبة."),
			item("Existing records", "السجلات الحالية", "The import options decide whether existing KPIs, targets and results are skipped or updated. On update, a blank optional cell keeps the stored value: only cells that hold a value are changed (to clear a field, use the KPI form).", "تحدد خيارات الاستيراد ما إذا كانت المؤشرات والمستهدفات والنتائج الحالية تُتخطى أو تُحدّث. عند التحديث تحتفظ الخلية الاختيارية الفارغة بالقيمة المحفوظة: تتغير الخلايا التي تحتوي على قيمة فقط (لمسح حقل استخدم نموذج المؤشر)."),
			item("Sheets and headers", "الأوراق والرؤوس", "Do not rename the sheets or change row 1. Sheets you do not need can stay empty.", "لا تغيّر أسماء الأوراق أو الصف الأول. الأوراق غير المطلوبة يمكن تركها فارغة."),
		}},
	}}
}
