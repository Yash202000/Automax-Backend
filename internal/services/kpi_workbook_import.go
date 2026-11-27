package services

// Combined KPI workbook import (KPI Dictionary bulk import): every record
// type the KPI module has, in one file, imported in dependency order inside
// one transaction so a sheet can refer to records created by an earlier
// sheet of the same file:
//
//  1. Master data sheets handled by the Master Data import (same columns and
//     rules as each Master Data tab): Domains, Organizations, Data Sources,
//     Segmentation Dimensions, Award Criteria, Award Sub-criteria,
//     Award KPI Measures.
//  2. Definitions: Pillars … Initiatives sheets and the three Dictionary
//     sheets (KPI definitions).
//  3. Metrics sheet (same columns and rules as the Metrics import), then the
//     Targets sheet (every target type, same as the Targets import).
//  4. Values: annual targets (Dictionary sheets) and the Performance sheet,
//     attached to the KPI's metric (Metric Code when it has several; a
//     default metric is created for a KPI that has none).
//
// Each stage writes its valid rows; Validate then rolls everything back,
// Commit keeps it unless the error option is "abort" and there are errors.

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"strings"

	"github.com/google/uuid"
	"github.com/xuri/excelize/v2"
	"gorm.io/gorm"
)

// kpiWorkbookMasterStages are the Master Data import entities of stage 1, in
// dependency order, with their report category.
var kpiWorkbookMasterStages = []struct{ entity, category string }{
	{KpiMasterDomain, "domain"},
	{KpiMasterOrganization, "organization"},
	{KpiMasterDataSource, "data_source"},
	{KpiMasterSegmentationDimension, "segmentation_dimension"},
	{KpiMasterAwardCriterion, "award_criterion"},
	{KpiMasterAwardSubCriterion, "award_sub_criterion"},
	{KpiMasterAwardKpiMeasure, "award_kpi_measure"},
}

// The combined workbook's Metrics and Targets sheets.
const (
	kpiTemplateMetricsSheet = "Metrics"
	kpiTemplateTargetsSheet = "Targets"
)

var errKpiWorkbookRollback = errors.New("kpi workbook import: roll back")

// ValidateKpiDictionaryImport validates the combined workbook and returns the
// import preview. It runs the whole import in a transaction that is rolled
// back, so references between sheets are checked exactly as on commit.
func ValidateKpiDictionaryImport(ctx context.Context, db *gorm.DB, data []byte, fileName string, userID uuid.UUID, opts KpiImportOptions) (*KpiImportResult, error) {
	var run *kpiWorkbookRun
	err := db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		var err error
		if run, err = runKpiWorkbookStages(ctx, tx, data, fileName, userID, opts); err != nil {
			return err
		}
		return errKpiWorkbookRollback
	})
	if run == nil {
		return nil, err
	}
	if err != nil && !errors.Is(err, errKpiWorkbookRollback) {
		// A write failed while checking: report it like a failed commit.
		run.res.CommitError = err.Error()
		run.markRolledBack()
	}
	return run.res, nil
}

// CommitKpiDictionaryImport imports the combined workbook in one
// transaction (serialized with an advisory lock). afterKpiCreated (optional)
// runs after a successful commit for each newly created KPI (used to start
// its dictionary workflow, which manages its own writes).
func CommitKpiDictionaryImport(ctx context.Context, db *gorm.DB, data []byte, fileName string, userID uuid.UUID, opts KpiImportOptions,
	afterKpiCreated func(ctx context.Context, kpiType string, kpiID uuid.UUID) error) (*KpiImportResult, error) {
	var run *kpiWorkbookRun
	err := db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		var err error
		if run, err = runKpiWorkbookStages(ctx, tx, data, fileName, userID, opts); err != nil {
			return err
		}
		if !run.res.CanCommit {
			return errKpiWorkbookRollback // nothing to write, or "abort" with errors
		}
		return nil
	})
	if run == nil {
		return nil, err
	}
	switch {
	case errors.Is(err, errKpiWorkbookRollback):
		return run.res, nil
	case err != nil:
		run.res.CommitError = err.Error()
		run.markRolledBack()
		return run.res, nil
	}
	run.res.Committed = true

	if afterKpiCreated != nil && run.defs != nil {
		for _, k := range run.defs.kpis {
			if k.action == kpiImportActionCreate {
				if err := afterKpiCreated(ctx, k.Type, k.ID); err != nil {
					run.res.Warnings = append(run.res.Warnings, KpiImportIssue{Sheet: k.Sheet, Row: k.Row, RecordID: k.Code,
						Message: fmt.Sprintf("KPI imported, but its workflow could not be started (it will be initialised on first use): %v", err)})
				}
			}
		}
	}
	return run.res, nil
}

// kpiWorkbookRun is one pass over the workbook: the merged report and the
// definitions-stage plan (for the KPIs it created).
type kpiWorkbookRun struct {
	res        *KpiImportResult
	defs       *kpiImporter
	structural bool
	seen       map[string]bool // de-duplicates issues reported by two stages
}

// runKpiWorkbookStages runs every stage against tx. Stages always write
// their valid rows (so later stages can see them); the caller decides
// whether tx is kept.
func runKpiWorkbookStages(ctx context.Context, tx *gorm.DB, data []byte, fileName string, userID uuid.UUID, opts KpiImportOptions) (*kpiWorkbookRun, error) {
	opts.normalize()
	if err := tx.Exec("SELECT pg_advisory_xact_lock(hashtextextended(?, 0))", "kpi-dictionary-import").Error; err != nil {
		return nil, fmt.Errorf("locking dictionary import: %w", err)
	}
	run := &kpiWorkbookRun{
		res:  &KpiImportResult{FileName: fileName, Options: opts, Errors: []KpiImportIssue{}, Warnings: []KpiImportIssue{}, Items: []KpiImportItem{}},
		seen: map[string]bool{},
	}
	stageOpts := opts
	stageOpts.ErrorMode = KpiImportErrorsSkipInvalid

	// Workbook-level problems (format, unreadable file, missing Dictionary
	// sheets) are reported by the definitions stage; only look for the
	// optional sheets when the file opens.
	var sheetNames []string
	if f, err := excelize.OpenReader(bytes.NewReader(data)); err == nil {
		sheetNames = f.GetSheetList()
		f.Close()
	}
	sheetNamed := func(names ...string) string {
		for _, s := range sheetNames {
			for _, n := range names {
				if normHeader(s) == normHeader(n) {
					return s
				}
			}
		}
		return ""
	}

	// 1. Master data sheets of the Master Data import.
	for _, st := range kpiWorkbookMasterStages {
		spec, _ := masterSpecFor(st.entity)
		sheet := sheetNamed(spec.sheet)
		if sheet == "" {
			continue
		}
		imp, err := buildKpiMasterDataPlanSheet(ctx, tx, st.entity, data, fileName, userID, stageOpts, sheet)
		if err != nil {
			return nil, err
		}
		if imp.res.CanCommit {
			if err := imp.commit(tx); err != nil {
				return nil, fmt.Errorf("%s sheet: %w", spec.sheet, err)
			}
		}
		run.add(imp.res, imp.structuralError, st.category)
	}

	// 2. Master data of the Dictionary import and the KPI definitions.
	defs, err := buildKpiImportPlanMode(ctx, tx, data, fileName, userID, stageOpts, kpiImportDefinitions)
	if err != nil {
		return nil, err
	}
	defs.finalize()
	run.defs = defs
	if defs.res.CanCommit {
		if err := defs.commit(tx); err != nil {
			return nil, err
		}
	}
	run.add(defs.res, defs.structuralError, "")
	if defs.structuralError {
		run.finish(opts)
		return run, nil
	}

	// 3. Metrics sheet. KPIs created above need no collaborator access.
	created := map[string]bool{}
	for _, k := range defs.kpis {
		if k.action == kpiImportActionCreate {
			created[k.Type+"|"+k.ID.String()] = true
		}
	}
	if sheet := sheetNamed(kpiTemplateMetricsSheet, "KPI Metrics", "المقاييس"); sheet != "" {
		mimp, err := buildKpiMetricsPlanSheet(ctx, tx, data, fileName, userID, stageOpts, sheet, created)
		if err != nil {
			return nil, err
		}
		if mimp.res.CanCommit {
			if err := mimp.revalidateCommit(tx); err != nil {
				return nil, fmt.Errorf("%s sheet: %w", sheet, err)
			}
			if err := mimp.commit(tx); err != nil {
				return nil, fmt.Errorf("%s sheet: %w", sheet, err)
			}
		}
		run.add(mimp.res, mimp.structuralError, "")
	}

	// 3b. Targets sheet (every target type: Period, Annual, Milestone / Ad
	// Hoc, with thresholds), same columns and rules as the Targets import.
	if sheet := sheetNamed(kpiTemplateTargetsSheet, "KPI Targets", "المستهدفات"); sheet != "" {
		timp, err := buildKpiTargetsPlanSheet(ctx, tx, data, fileName, userID, stageOpts, sheet, created)
		if err != nil {
			return nil, err
		}
		if timp.res.CanCommit {
			if err := timp.commit(tx); err != nil {
				return nil, fmt.Errorf("%s sheet: %w", sheet, err)
			}
		}
		run.add(timp.res, timp.structuralError, "")
	}

	// 4. Annual targets and performance, now that KPIs and metrics exist.
	vals, err := buildKpiImportPlanMode(ctx, tx, data, fileName, userID, stageOpts, kpiImportValues)
	if err != nil {
		return nil, err
	}
	vals.finalize()
	if vals.res.CanCommit {
		if err := vals.commit(tx); err != nil {
			return nil, err
		}
	}
	run.add(vals.res, vals.structuralError, "")

	run.finish(opts)
	return run, nil
}

// add merges one stage's report. category, when set, replaces the stage's
// item category (the Master Data import reports its entity type).
func (run *kpiWorkbookRun) add(res *KpiImportResult, structural bool, category string) {
	run.structural = run.structural || structural
	issue := func(list *[]KpiImportIssue, kind string, items []KpiImportIssue) {
		for _, it := range items {
			key := fmt.Sprintf("%s|%s|%d|%s|%s|%s", kind, it.Sheet, it.Row, it.RecordID, it.Field, it.Message)
			if run.seen[key] {
				continue
			}
			run.seen[key] = true
			*list = append(*list, it)
		}
	}
	issue(&run.res.Errors, "e", res.Errors)
	issue(&run.res.Warnings, "w", res.Warnings)
	for _, it := range res.Items {
		if category != "" {
			it.Category = category
		}
		run.res.Items = append(run.res.Items, it)
	}
}

// finish counts the merged items and decides whether the import may be
// committed under the user's error option.
func (run *kpiWorkbookRun) finish(opts KpiImportOptions) {
	run.count()
	actionable := run.res.Totals.Created + run.res.Totals.Updated
	run.res.CanCommit = !run.structural && actionable > 0 &&
		!(opts.ErrorMode == KpiImportErrorsAbort && len(run.res.Errors) > 0)
}

func (run *kpiWorkbookRun) count() {
	res := run.res
	res.Categories = map[string]*KpiImportCounts{}
	for _, c := range KpiImportCategories {
		res.Categories[c] = &KpiImportCounts{}
	}
	res.Totals = KpiImportCounts{}
	for _, it := range res.Items {
		c := res.Categories[it.Category]
		if c == nil {
			c = &KpiImportCounts{}
			res.Categories[it.Category] = c
		}
		switch strings.ToLower(it.Action) {
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

func (run *kpiWorkbookRun) markRolledBack() {
	for i := range run.res.Items {
		it := &run.res.Items[i]
		if it.Action == kpiImportActionCreate || it.Action == kpiImportActionUpdate {
			it.Action = "rolled_back"
			it.Reason = "transaction rolled back"
		}
	}
	run.count()
	run.res.CanCommit = false
}
