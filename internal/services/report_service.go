package services

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"math"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/automax/backend/internal/models"
	"github.com/automax/backend/internal/repository"
	"github.com/automax/backend/pkg/constants"
	"github.com/automax/backend/pkg/fonts"
	"github.com/automax/backend/pkg/utils"
	"github.com/google/uuid"
	"github.com/jung-kurt/gofpdf"
	"github.com/xuri/excelize/v2"
	"golang.org/x/text/cases"
	"golang.org/x/text/language"
)

type ReportService interface {
	// Report CRUD
	CreateReport(ctx context.Context, req *models.ReportCreateRequest, userID uuid.UUID) (*models.ReportResponse, error)
	GetReport(ctx context.Context, id uuid.UUID) (*models.ReportResponse, error)
	ListReports(ctx context.Context, filter *models.ReportFilter) ([]models.ReportResponse, int64, error)
	UpdateReport(ctx context.Context, id uuid.UUID, req *models.ReportUpdateRequest, userID uuid.UUID) (*models.ReportResponse, error)
	DeleteReport(ctx context.Context, id uuid.UUID, userID uuid.UUID) error
	DuplicateReport(ctx context.Context, id uuid.UUID, userID uuid.UUID) (*models.ReportResponse, error)

	// Report Execution
	ExecuteReport(ctx context.Context, id uuid.UUID, req *models.ReportExecuteRequest, userID uuid.UUID) (*models.ReportResultResponse, error)
	PreviewReport(ctx context.Context, req *models.ReportCreateRequest) (*models.ReportResultResponse, error)
	QueryReport(ctx context.Context, req *models.ReportQueryRequest) (*models.ReportQueryResponse, error)
	ExportReport(ctx context.Context, req *models.ReportExportRequest) ([]byte, string, string, error)
	GetExecutionHistory(ctx context.Context, reportID uuid.UUID, page, limit int) ([]models.ReportExecutionResponse, int64, error)

	// Metadata
	GetDataSources(ctx context.Context) []models.DataSourceInfo
}

// queryHandlerFn is the unified signature all data-source handlers must satisfy.
type queryHandlerFn func(
	ctx context.Context,
	filters []models.ReportFilterConfig,
	sorting *models.ReportSortConfig,
	page, limit int,
) ([]map[string]interface{}, int64, error)

type reportService struct {
	reportRepo         repository.ReportRepository
	rejectionLogRepo   repository.RejectionLogRepository
	locationRepo       repository.LocationRepository
	classificationRepo repository.ClassificationRepository
	workflowRepo       repository.WorkflowRepository
	queryHandlers      map[string]queryHandlerFn // ← single registry
}

func NewReportService(
	reportRepo repository.ReportRepository,
	rejectionLogRepo repository.RejectionLogRepository,
	locationRepo repository.LocationRepository,
	classificationRepo repository.ClassificationRepository,
	workflowRepo repository.WorkflowRepository,

) ReportService {
	// return &reportService{
	// 	reportRepo:       reportRepo,
	// 	rejectionLogRepo: rejectionLogRepo,
	// }
	s := &reportService{
		reportRepo:         reportRepo,
		rejectionLogRepo:   rejectionLogRepo,
		locationRepo:       locationRepo,
		classificationRepo: classificationRepo,
		workflowRepo:       workflowRepo,
	}
	// All four switch statements collapse into this one map.
	// Adding a new data source = one line here, zero changes elsewhere.
	s.queryHandlers = map[string]queryHandlerFn{
		"incidents":                 reportRepo.ExecuteIncidentQuery,
		"requests":                  reportRepo.ExecuteRequestQuery,
		"users":                     reportRepo.ExecuteUserQuery,
		"users_performance":         reportRepo.ExecuteUserPerformanceQuery,
		"workflows":                 reportRepo.ExecuteWorkflowQuery,
		"departments":               reportRepo.ExecuteDepartmentQuery,
		"locations":                 reportRepo.ExecuteLocationQuery,
		"classifications":           reportRepo.ExecuteClassificationQuery,
		"rejection_logs":            rejectionLogRepo.ExecuteRejectionLogQuery,
		"action_logs":               reportRepo.ExecuteActionLogQuery,
		"locations_by_count":        reportRepo.ExecuteLocationCountQuery,
		"locations_by_status":       reportRepo.ExecuteLocationCountByStatusQuery,
		"classifications_by_count":  reportRepo.ExecuteClassificationCountQuery,
		"classifications_by_status": reportRepo.ExecuteClassificationCountByStatusQuery,
		"departments_by_count":      reportRepo.ExecuteDepartmentCountQuery,
		"departments_by_status":     reportRepo.ExecuteDepartmentCountByStatusQuery,
	}
	return s
}

// Report CRUD

func (s *reportService) CreateReport(ctx context.Context, req *models.ReportCreateRequest, userID uuid.UUID) (*models.ReportResponse, error) {
	columnsJSON, _ := json.Marshal(req.Config.Columns)
	filtersJSON, _ := json.Marshal(req.Config.Filters)
	sortingJSON, _ := json.Marshal(req.Config.Sorting)

	report := &models.Report{
		Name:          req.Name,
		NameAr:        req.NameAr,
		Description:   req.Description,
		DescriptionAr: req.DescriptionAr,
		DataSource:    req.DataSource,
		Columns:       string(columnsJSON),
		Filters:       string(filtersJSON),
		Sorting:       string(sortingJSON),
		OutputFormat:  "table",
		IsPublic:      req.IsPublic,
		CreatedByID:   userID,
	}

	if err := s.reportRepo.Create(ctx, report); err != nil {
		return nil, err
	}

	return s.GetReport(ctx, report.ID)
}

func (s *reportService) GetReport(ctx context.Context, id uuid.UUID) (*models.ReportResponse, error) {
	report, err := s.reportRepo.FindByIDWithRelations(ctx, id)
	if err != nil {
		return nil, err
	}

	return toReportResponse(report), nil
}

func (s *reportService) ListReports(ctx context.Context, filter *models.ReportFilter) ([]models.ReportResponse, int64, error) {
	reports, total, err := s.reportRepo.List(ctx, filter)
	if err != nil {
		return nil, 0, err
	}

	responses := make([]models.ReportResponse, len(reports))
	for i, r := range reports {
		responses[i] = *toReportResponse(&r)
	}

	return responses, total, nil
}

func (s *reportService) UpdateReport(ctx context.Context, id uuid.UUID, req *models.ReportUpdateRequest, userID uuid.UUID) (*models.ReportResponse, error) {
	report, err := s.reportRepo.FindByID(ctx, id)
	if err != nil {
		return nil, err
	}

	// Only creator can update (or implement permission check)
	if report.CreatedByID != userID {
		return nil, errors.New("you can only update your own reports")
	}

	if req.Name != "" {
		report.Name = req.Name
	}
	if req.NameAr != "" {
		report.NameAr = req.NameAr
	}
	if req.Description != "" {
		report.Description = req.Description
	}
	if req.DescriptionAr != "" {
		report.DescriptionAr = req.DescriptionAr
	}
	if req.Config != nil {
		if req.Config.Columns != nil {
			columnsJSON, _ := json.Marshal(req.Config.Columns)
			report.Columns = string(columnsJSON)
		}
		if req.Config.Filters != nil {
			filtersJSON, _ := json.Marshal(req.Config.Filters)
			report.Filters = string(filtersJSON)
		}
		if req.Config.Sorting != nil {
			sortingJSON, _ := json.Marshal(req.Config.Sorting)
			report.Sorting = string(sortingJSON)
		}
	}
	if req.IsPublic != nil {
		report.IsPublic = *req.IsPublic
	}
	if req.TimestampKey != "" {
		report.TimestampKey = req.TimestampKey
	}

	if err := s.reportRepo.Update(ctx, report); err != nil {
		return nil, err
	}

	return s.GetReport(ctx, id)
}

func (s *reportService) DeleteReport(ctx context.Context, id uuid.UUID, userID uuid.UUID) error {
	report, err := s.reportRepo.FindByID(ctx, id)
	if err != nil {
		return err
	}

	// Only creator can delete
	if report.CreatedByID != userID {
		return errors.New("you can only delete your own reports")
	}

	return s.reportRepo.Delete(ctx, id)
}

func (s *reportService) DuplicateReport(ctx context.Context, id uuid.UUID, userID uuid.UUID) (*models.ReportResponse, error) {
	original, err := s.reportRepo.FindByID(ctx, id)
	if err != nil {
		return nil, err
	}

	duplicate := &models.Report{
		Name:         original.Name + " (Copy)",
		Description:  original.Description,
		DataSource:   original.DataSource,
		Columns:      original.Columns,
		Filters:      original.Filters,
		Sorting:      original.Sorting,
		Grouping:     original.Grouping,
		OutputFormat: original.OutputFormat,
		IsPublic:     false, // New report is private by default
		IsScheduled:  false, // Don't copy schedule
		Schedule:     "",
		CreatedByID:  userID,
	}

	if err := s.reportRepo.Create(ctx, duplicate); err != nil {
		return nil, err
	}

	return s.GetReport(ctx, duplicate.ID)
}

// dispatchQuery resolves the handler for dataSource and runs it.
// This is the single place that returns "unsupported data source".
func (s *reportService) dispatchQuery(
	ctx context.Context,
	dataSource string,
	filters []models.ReportFilterConfig,
	sorting *models.ReportSortConfig,
	page, limit int,
) ([]map[string]interface{}, int64, error) {
	handler, ok := s.queryHandlers[dataSource]
	if !ok {
		return nil, 0, fmt.Errorf("unsupported data source: %q", dataSource)
	}
	return handler(ctx, filters, sorting, page, limit)
}

// firstSorting extracts the first sort config or nil — used by every caller.
func firstSorting(sorting []models.ReportSortConfig) *models.ReportSortConfig {
	if len(sorting) > 0 {
		return &sorting[0]
	}
	return nil
}

// Report Execution
func (s *reportService) QueryReport(ctx context.Context, req *models.ReportQueryRequest) (*models.ReportQueryResponse, error) {
	data, total, err := s.dispatchQuery(ctx,
		req.DataSource, req.Filters, firstSorting(req.Sorting), req.Page, req.Limit,
	)
	if err != nil {
		return nil, err
	}

	totalPages := int(total) / req.Limit
	if int(total)%req.Limit > 0 {
		totalPages++
	}
	return &models.ReportQueryResponse{
		Success:    true,
		Data:       data,
		Columns:    req.Columns,
		TotalItems: total,
		TotalPages: totalPages,
		Page:       req.Page,
		Limit:      req.Limit,
	}, nil
}

func (s *reportService) PreviewReport(ctx context.Context, req *models.ReportCreateRequest) (*models.ReportResultResponse, error) {
	const previewLimit = 50
	data, total, err := s.dispatchQuery(ctx,
		req.DataSource, req.Config.Filters, firstSorting(req.Config.Sorting), 1, previewLimit,
	)
	if err != nil {
		return nil, err
	}
	return &models.ReportResultResponse{
		Columns: req.Config.Columns,
		Data:    data,
		Total:   total,
		Page:    1,
		Limit:   previewLimit,
	}, nil
}

func (s *reportService) ExportReport(ctx context.Context, req *models.ReportExportRequest) ([]byte, string, string, error) {
	const exportLimit = 10000
	data, _, err := s.dispatchQuery(ctx,
		req.DataSource, req.Filters, firstSorting(req.Sorting), 1, exportLimit,
	)
	if err != nil {
		return nil, "", "", err
	}

	title := ""
	if req.Options != nil && req.Options.Title != "" {
		title = req.Options.Title
	}

	if title == "" {
		lang, _ := ctx.Value(constants.ContextKeys.ACCEPT_LANGUAGE).(string)
		title = resolveTitle(title, req.DataSource, lang)
	}

	// Resolve user timezone for timestamps and date values
	var tz string
	if req.Options != nil {
		tz = req.Options.Timezone
	}
	loc := utils.ResolveTimezone(tz)
	convertDataTimezone(data, loc)

	filename := "Report"
	if req.DataSource != "" {
		filename = cases.Title(language.English).String(req.DataSource) + "_Report"
	}
	filename += "_" + time.Now().In(loc).Format("2006-01-02_150405")
	log.Printf("Exporting report with title: %s, filename: %s", title, filename)
	filters := s.buildFilterDisplay(ctx, req.Filters)
	if req.Format == "xlsx" {
		xlsxData, err := s.generateExcel(data, req.Columns, title, req.Options, filters)
		if err != nil {
			return nil, "", "", err
		}
		return xlsxData, filename + ".xlsx",
			"application/vnd.openxmlformats-officedocument.spreadsheetml.sheet", nil
	}
	log.Println(req.Filters)
	// in ExportReport, change the generatePDF call:
	pdfData, err := s.generatePDF(ctx, data, req.Columns, title, req.Options, filters)
	log.Println("pdf generation started ")
	if err != nil {
		log.Println(err)
		return nil, "", "", err
	}
	return pdfData, filename + ".pdf", "application/pdf", nil
}

func (s *reportService) ExecuteReport(
	ctx context.Context,
	id uuid.UUID,
	req *models.ReportExecuteRequest,
	userID uuid.UUID,
) (*models.ReportResultResponse, error) {
	report, err := s.reportRepo.FindByID(ctx, id)
	if err != nil {
		return nil, err
	}

	// Parse stored config
	var columns []models.ReportColumnConfig
	var filters []models.ReportFilterConfig
	var storedSorting []models.ReportSortConfig
	json.Unmarshal([]byte(report.Columns), &columns)
	json.Unmarshal([]byte(report.Filters), &filters)
	json.Unmarshal([]byte(report.Sorting), &storedSorting)

	if len(req.Filters) > 0 {
		filters = req.Filters
	}

	page := max(req.Page, 1)
	limit := req.Limit
	if limit < 1 || limit > 1000 {
		limit = 100
	}

	// Record execution start
	now := time.Now()
	execution := &models.ReportExecution{
		ReportID:     id,
		ExecutedByID: userID,
		Status:       "running",
		StartedAt:    &now,
	}
	s.reportRepo.CreateExecution(ctx, execution)

	data, total, queryErr := s.dispatchQuery(ctx,
		report.DataSource, filters, firstSorting(storedSorting), page, limit,
	)

	// Record execution result regardless of error
	completedAt := time.Now()
	execution.CompletedAt = &completedAt
	if queryErr != nil {
		execution.Status = "failed"
		execution.Error = queryErr.Error()
	} else {
		execution.Status = "completed"
		execution.ResultCount = int(total)
	}
	s.reportRepo.UpdateExecution(ctx, execution)

	if queryErr != nil {
		return nil, queryErr
	}
	return &models.ReportResultResponse{
		Columns: columns,
		Data:    data,
		Total:   total,
		Page:    page,
		Limit:   limit,
	}, nil
}
func (s *reportService) generateExcel(
	data []map[string]interface{},
	columns []models.ColumnField,
	title string,
	options *models.ReportExportOptions,
	filterDisplay map[string]interface{},
) ([]byte, error) {

	f := excelize.NewFile()
	sheet := "Report"
	f.SetSheetName("Sheet1", sheet)

	// ── Column widths ─────────────────────────────────────────────────────
	if len(columns) > 0 {
		f.SetColWidth(sheet, "A", "A", 8) // S.No. column
		lastCol, _ := excelize.ColumnNumberToName(len(columns) + 1)
		f.SetColWidth(sheet, "B", lastCol, 27)
	}

	// Determine starting row
	headerRow := 1
	if title != "" {
		headerRow = 2
	}
	dataStartRow := headerRow + 1

	// ── Title row ─────────────────────────────────────────────────────────
	if title != "" {
		lastCol, _ := excelize.ColumnNumberToName(len(columns) + 1)
		f.MergeCell(sheet, "A1", lastCol+"1")
		titleStyle, _ := f.NewStyle(&excelize.Style{
			Font:      &excelize.Font{Bold: true, Size: 14},
			Alignment: &excelize.Alignment{Horizontal: "center", Vertical: "center"},
		})
		f.SetCellValue(sheet, "A1", title)
		f.SetCellStyle(sheet, "A1", "A1", titleStyle)
		f.SetRowHeight(sheet, 1, 32)
	}

	// ── Header style — same as working version ────────────────────────────
	headerStyleId, err := f.NewStyle(&excelize.Style{
		Alignment: &excelize.Alignment{Horizontal: "center", Vertical: "center"},
		Font:      &excelize.Font{Bold: true, Size: 15, Color: "FFFFFF"},
		Fill:      excelize.Fill{Type: "pattern", Color: []string{"3B82F6"}, Pattern: 1},
	})
	if err == nil {
		f.SetRowStyle(sheet, headerRow, headerRow, headerStyleId)
	}
	f.SetRowHeight(sheet, headerRow, 30)

	// ── Body style — bulk apply to 1000 rows, same as working version ─────
	bodyStyleId, err := f.NewStyle(&excelize.Style{
		Alignment: &excelize.Alignment{Horizontal: "center", Vertical: "center"},
	})
	if err == nil {
		f.SetRowStyle(sheet, dataStartRow, dataStartRow+1000, bodyStyleId)
	}

	// ── Detect language from options ──────────────────────────────────────
	// (title already resolved before calling this function)

	// ── Write headers ─────────────────────────────────────────────────────
	headerRowStr := strconv.Itoa(headerRow)
	f.SetCellValue(sheet, "A"+headerRowStr, "#")
	for i, col := range columns {
		colName, _ := excelize.ColumnNumberToName(i + 2)
		f.SetCellValue(sheet, colName+headerRowStr, col.Label)
	}

	// ── Write data rows ───────────────────────────────────────────────────
	for rowIdx, row := range data {
		excelRow := strconv.Itoa(dataStartRow + rowIdx)
		f.SetCellValue(sheet, "A"+excelRow, rowIdx+1)
		for colIdx, col := range columns {
			colName, _ := excelize.ColumnNumberToName(colIdx + 2)
			val := ""
			if v, ok := row[col.Label]; ok && v != nil {
				val = formatExportValue(col.Label, v)
			}
			if val == "" {
				val = "-"
			}
			f.SetCellValue(sheet, colName+excelRow, val)
		}
	}

	// ── Freeze pane ───────────────────────────────────────────────────────
	freezeCell := "A" + strconv.Itoa(dataStartRow)
	f.SetPanes(sheet, &excelize.Panes{
		Freeze:      true,
		YSplit:      headerRow,
		TopLeftCell: freezeCell,
		ActivePane:  "bottomLeft",
	})

	// ── Filters & Stats ───────────────────────────────────────────────────
	currentRow := dataStartRow + len(data) + 2 // one blank row after last data row
	lastDataCol, _ := excelize.ColumnNumberToName(len(columns) + 1)

	if len(filterDisplay) > 0 {
		filterKeys := make([]string, 0, len(filterDisplay))
		for k := range filterDisplay {
			filterKeys = append(filterKeys, k)
		}
		sort.Strings(filterKeys)

		filterHeaderStyle, _ := f.NewStyle(&excelize.Style{
			Font: &excelize.Font{Bold: true, Size: 10},
			Fill: excelize.Fill{Type: "pattern", Color: []string{"E8EDF5"}, Pattern: 1},
		})
		rowStr := strconv.Itoa(currentRow)
		f.MergeCell(sheet, "A"+rowStr, lastDataCol+rowStr)
		f.SetCellValue(sheet, "A"+rowStr, "Filters:")
		f.SetCellStyle(sheet, "A"+rowStr, lastDataCol+rowStr, filterHeaderStyle)
		currentRow++

		filterRowStyle, _ := f.NewStyle(&excelize.Style{
			Fill: excelize.Fill{Type: "pattern", Color: []string{"F5F7FA"}, Pattern: 1},
		})
		for _, k := range filterKeys {
			v := filterDisplay[k]
			rawKey := stripSortPrefix(k)
			label := cases.Title(language.English).String(strings.ReplaceAll(rawKey, "_", " "))
			rowStr = strconv.Itoa(currentRow)
			f.SetCellValue(sheet, "A"+rowStr, label+":")
			f.SetCellValue(sheet, "B"+rowStr, formatFilterValue("", v))
			f.SetCellStyle(sheet, "A"+rowStr, "B"+rowStr, filterRowStyle)
			currentRow++
		}

		statsStyle, _ := f.NewStyle(&excelize.Style{
			Font: &excelize.Font{Bold: true, Size: 10, Color: "3B82F6"},
			Fill: excelize.Fill{Type: "pattern", Color: []string{"E8EDF5"}, Pattern: 1},
		})
		rowStr = strconv.Itoa(currentRow)
		f.SetCellValue(sheet, "A"+rowStr, fmt.Sprintf("Total Rows: %d", len(data)))
		f.SetCellStyle(sheet, "A"+rowStr, "A"+rowStr, statsStyle)
		currentRow += 2 // blank row before timestamp
	}

	// ── Timestamp (uses user timezone) ────────────────────────────────────
	if options != nil && options.IncludeTimestamp {
		loc := utils.ResolveTimezone(options.Timezone)
		f.SetCellValue(sheet, "A"+strconv.Itoa(currentRow),
			fmt.Sprintf("Generated: %s", time.Now().In(loc).Format("2006-01-02 15:04:05")))
	}

	// ── Output ────────────────────────────────────────────────────────────
	var buf bytes.Buffer
	if err := f.Write(&buf); err != nil {
		return nil, fmt.Errorf("excel write error: %w", err)
	}
	return buf.Bytes(), nil
}

// computeFilterBoxH returns the height of the filter+stats box drawn by drawFiltersAndStats.
func computeFilterBoxH(filterDisplay map[string]interface{}) float64 {
	const lineH = 5.5
	const padding = 3.0
	filterLines := 1 + len(filterDisplay) // "Filters:" header + one line per entry
	statsLines := 2                       // "Total Rows" + "Total Pages"
	maxLines := filterLines
	if statsLines > maxLines {
		maxLines = statsLines
	}
	return float64(maxLines)*lineH + padding*2
}

// computeActualPageCount simulates the row-drawing loop to return the real page count.
// firstPageDataY is the Y coordinate where the first data row will be drawn.
func computeActualPageCount(
	pdf *gofpdf.Fpdf,
	data []map[string]interface{},
	columns []models.ColumnField,
	colWidths []float64,
	firstPageDataY float64,
	hdrH float64,
	lineH float64,
	baseFontSize float64,
	rtlMode bool,
	setFont func(string, float64, string),
) int {
	const pageH = 210.0
	const leftMargin = 16.0
	bottomBound := pageH - leftMargin

	colCount := len(columns)
	minRowH := lineH + 2
	currentY := firstPageDataY
	pages := 1

	renderWidths := colWidths
	if rtlMode {
		renderWidths = reverseWidths(colWidths)
	}

	for _, row := range data {
		vals := make([]string, colCount)
		for i, col := range columns {
			v := row[col.Label]
			if v == nil {
				vals[i] = ""
			} else {
				vals[i] = formatExportValue(col.Label, v)
			}
		}
		if rtlMode {
			vals = reverseStrings(vals)
		}

		rowH := minRowH
		for i := range columns {
			setFont("", baseFontSize, vals[i])
			nb := len(pdf.SplitLines([]byte(vals[i]), renderWidths[i]-2))
			if nb < 1 {
				nb = 1
			}
			if needed := float64(nb)*lineH + 2; needed > rowH {
				rowH = needed
			}
		}

		if currentY+rowH > bottomBound {
			pages++
			currentY = leftMargin + hdrH
		}
		currentY += rowH
	}

	if pages < 1 {
		return 1
	}
	return pages
}

func computeColWidths(pageWidth float64, colCount int) []float64 {
	if colCount == 0 {
		return nil
	}
	// Always divide equally — same as table-layout:fixed in HTML
	// No clamping min/max: let the math decide, just like the browser does
	colWidth := pageWidth / float64(colCount)
	widths := make([]float64, colCount)
	for i := range widths {
		widths[i] = colWidth
	}
	return widths
}

func truncateRunes(s string, maxRunes int) string {
	runes := []rune(s)
	if len(runes) <= maxRunes {
		return s
	}
	return string(runes[:maxRunes-3]) + "..."
}

func isRTL(s string) bool {
	for _, r := range s {
		if r >= 0x0600 && r <= 0x06FF {
			return true
		}
		if r >= 0x0590 && r <= 0x05FF {
			return true
		}
	}
	return false
}

func reverseColumns(cols []models.ColumnField) []models.ColumnField {
	out := make([]models.ColumnField, len(cols))
	for i, c := range cols {
		out[len(cols)-1-i] = c
	}
	return out
}

func reverseWidths(w []float64) []float64 {
	out := make([]float64, len(w))
	for i, v := range w {
		out[len(w)-1-i] = v
	}
	return out
}

func reverseStrings(s []string) []string {
	out := make([]string, len(s))
	for i, v := range s {
		out[len(s)-1-i] = v
	}
	return out
}

func setupArabicFont(pdf *gofpdf.Fpdf) {
	log.Printf("NotoArabicRegular size: %d bytes", len(fonts.NotoArabicRegular))
	if len(fonts.NotoArabicRegular) == 0 {
		log.Println("WARNING: Arabic font not loaded, will fall back to Arial")
		return
	}
	pdf.AddUTF8FontFromBytes("NotoArabic", "", fonts.NotoArabicRegular)
	pdf.AddUTF8FontFromBytes("NotoArabic", "B", fonts.NotoArabicRegular)
}

func (s *reportService) generatePDF(
	ctx context.Context,
	data []map[string]interface{},
	columns []models.ColumnField,
	title string,
	options *models.ReportExportOptions,
	filterDisplay map[string]interface{}, // NEW
) ([]byte, error) {

	lang, _ := ctx.Value(constants.ContextKeys.ACCEPT_LANGUAGE).(string)
	rtlMode := lang == "ar" || lang == "he" || isRTL(title)
	arabicLoaded := len(fonts.NotoArabicRegular) > 0
	log.Println("RTL Mode: ", rtlMode)
	pdf := gofpdf.New("L", "mm", "A4", "")
	pdf.SetMargins(16, 16, 16) // match HTML template: @page { margin: 16mm }
	pdf.SetAutoPageBreak(true, 16)

	setupArabicFont(pdf)
	pdf.AddPage()

	// A4 landscape = 297mm wide; 297 - 16 - 16 = 265mm usable (matches HTML template)
	pageWidth := 265.0
	const snoWidth = 12.0 // fixed width for the serial-number column
	colCount := len(columns)
	colWidths := computeColWidths(pageWidth-snoWidth, colCount)

	// Body font: 9pt (≈ HTML 12px). Scale down for many columns.
	baseFontSize := 9.0
	switch {
	case colCount > 20:
		baseFontSize = 6.5
	case colCount > 15:
		baseFontSize = 7.5
	case colCount > 10:
		baseFontSize = 8.0
	}
	// Header font: 1pt above body — readable but never larger than the column width allows.
	headerFontSize := baseFontSize + 1.0

	// lineH: height of one wrapped text line — 1pt ≈ 0.353mm, add ~2mm padding
	lineH := baseFontSize*0.353 + 2.2

	const leftMargin = 16.0
	const pageH = 210.0 // A4 height in mm

	setFont := func(style string, size float64, text string) {
		if arabicLoaded && (rtlMode || isRTL(text)) {
			pdf.SetFont("NotoArabic", style, size)
		} else {
			pdf.SetFont("Arial", style, size)
		}
	}

	drawHeader := func() {
		pdf.SetFillColor(244, 246, 248)
		pdf.SetTextColor(34, 34, 34)

		orderedCols := columns
		orderedWidths := colWidths
		if rtlMode {
			orderedCols = reverseColumns(columns)
			orderedWidths = reverseWidths(colWidths)
		}

		// Measure every label at the header font to find the tallest column,
		// then size the row to fit — prevents text being clipped or overflowing.
		setFont("B", headerFontSize, "")
		maxLines := 1
		for i, col := range orderedCols {
			nb := len(pdf.SplitLines([]byte(col.Label), orderedWidths[i]-2))
			if nb < 1 {
				nb = 1
			}
			if nb > maxLines {
				maxLines = nb
			}
		}
		hdrH := float64(maxLines)*lineH + 3.5

		startY := pdf.GetY()
		curX := leftMargin

		// Serial number header cell (always at the visual left)
		pdf.SetFillColor(244, 246, 248)
		pdf.Rect(curX, startY, snoWidth, hdrH, "FD")
		pdf.SetXY(curX+1, startY+1)
		setFont("B", headerFontSize, "")
		pdf.MultiCell(snoWidth-2, lineH, "#", "", "C", false)
		curX += snoWidth

		for i, col := range orderedCols {
			pdf.SetFillColor(244, 246, 248)
			pdf.Rect(curX, startY, orderedWidths[i], hdrH, "FD")
			pdf.SetXY(curX+1, startY+1)
			setFont("B", headerFontSize, col.Label)
			pdf.MultiCell(orderedWidths[i]-2, lineH, col.Label, "", "C", false)
			curX += orderedWidths[i]
		}
		pdf.SetXY(leftMargin, startY+hdrH)

		pdf.SetTextColor(0, 0, 0)
		pdf.SetFillColor(255, 255, 255)
	}

	// ── Title ────────────────────────────────────────────────────────────────
	setFont("B", 14, title)
	pdf.SetTextColor(0, 0, 0)
	pdf.CellFormat(0, 10, title, "", 1, "C", false, 0, "")
	pdf.Ln(3)

	// ── Timestamp ────────────────────────────────────────────────────────────
	// ── Timestamp (uses user timezone) ───────────────────────────────────────────
	if options != nil && options.IncludeTimestamp {
		loc := utils.ResolveTimezone(options.Timezone)
		setFont("", 9, "")
		pdf.SetTextColor(128, 128, 128)
		pdf.CellFormat(0, 6, fmt.Sprintf("Generated: %s", time.Now().In(loc).Format("2006-01-02 15:04:05")), "", 1, "C", false, 0, "")
		pdf.SetTextColor(0, 0, 0)
		pdf.Ln(3)
	}

	// ── Filters + Stats (first page only) ────────────────────────────────────────
	// Measure header height (same logic as drawHeader) so we can compute the real
	// Y where data rows start on the first page — needed for accurate page count.
	setFont("B", headerFontSize, "")
	hdrMaxLines := 1
	for i, col := range columns {
		nb := len(pdf.SplitLines([]byte(col.Label), colWidths[i]-2))
		if nb < 1 {
			nb = 1
		}
		if nb > hdrMaxLines {
			hdrMaxLines = nb
		}
	}
	measuredHdrH := float64(hdrMaxLines)*lineH + 3.5
	// firstPageDataY = current cursor + filter-box height + 4mm gap + Ln(2) + header
	firstPageDataY := pdf.GetY() + computeFilterBoxH(filterDisplay) + 4.0 + 2.0 + measuredHdrH
	actualPageCount := computeActualPageCount(pdf, data, columns, colWidths, firstPageDataY, measuredHdrH, lineH, baseFontSize, rtlMode, setFont)

	drawFiltersAndStats(pdf, filterDisplay, len(data), actualPageCount, setFont)
	pdf.Ln(2)

	// ── Table header ─────────────────────────────────────────────────────────────
	drawHeader()

	// ── Rows ─────────────────────────────────────────────────────────────────
	// minRowH: a row must be at least this tall even when content is short
	minRowH := lineH + 2
	bottomBound := pageH - leftMargin // leave 16mm bottom margin

	fill := false
	for rowIdx, row := range data {
		// Build values without truncation — MultiCell will wrap long text
		vals := make([]string, colCount)
		for i, col := range columns {
			v := row[col.Label]
			if v == nil {
				vals[i] = ""
			} else {
				vals[i] = formatExportValue(col.Label, v)
			}
		}

		renderCols := columns
		renderWidths := colWidths
		renderVals := vals
		if rtlMode {
			renderCols = reverseColumns(columns)
			renderWidths = reverseWidths(colWidths)
			renderVals = reverseStrings(vals)
		}

		// Calculate the tallest cell in this row to fix uniform row height
		rowH := minRowH
		for i := range renderCols {
			setFont("", baseFontSize, renderVals[i])
			nb := len(pdf.SplitLines([]byte(renderVals[i]), renderWidths[i]-2))
			if nb < 1 {
				nb = 1
			}
			needed := float64(nb)*lineH + 2
			if needed > rowH {
				rowH = needed
			}
		}

		startY := pdf.GetY()

		// Page break before drawing (check combined height)
		if startY+rowH > bottomBound {
			pdf.AddPage()
			drawHeader()
			startY = pdf.GetY()
		}

		// Alternating row background
		var fillR, fillG, fillB int
		if fill {
			fillR, fillG, fillB = 240, 245, 255
		} else {
			fillR, fillG, fillB = 255, 255, 255
		}

		curX := leftMargin

		// Serial number cell (always at the visual left)
		snoVal := strconv.Itoa(rowIdx + 1)
		setFont("", baseFontSize, "")
		pdf.SetFillColor(fillR, fillG, fillB)
		pdf.Rect(curX, startY, snoWidth, rowH, "F")
		pdf.SetDrawColor(51, 51, 51)
		pdf.Rect(curX, startY, snoWidth, rowH, "D")
		pdf.SetXY(curX+1, startY+1)
		pdf.MultiCell(snoWidth-2, lineH, snoVal, "", "C", false)
		curX += snoWidth

		for i := range renderCols {
			val := renderVals[i]
			setFont("", baseFontSize, val)
			align := "L"
			if rtlMode || isRTL(val) {
				align = "R"
			}
			// Background fill
			pdf.SetFillColor(fillR, fillG, fillB)
			pdf.Rect(curX, startY, renderWidths[i], rowH, "F")
			// Border (border: 1px solid #333)
			pdf.SetDrawColor(51, 51, 51)
			pdf.Rect(curX, startY, renderWidths[i], rowH, "D")
			// Text with 1mm inset on each side so it doesn't touch the border
			pdf.SetXY(curX+1, startY+1)
			pdf.MultiCell(renderWidths[i]-2, lineH, val, "", align, false)
			curX += renderWidths[i]
		}
		pdf.SetXY(leftMargin, startY+rowH)
		fill = !fill
	}

	// ── Footer ───────────────────────────────────────────────────────────────
	pdf.Ln(5)
	setFont("", 9, "")
	pdf.SetTextColor(100, 100, 100)
	footerText := fmt.Sprintf("Total records: %d", len(data))
	if rtlMode {
		footerText = fmt.Sprintf("إجمالي السجلات: %d", len(data))
	}
	pdf.CellFormat(0, 6, footerText, "", 1, "L", false, 0, "")

	// ── Output ───────────────────────────────────────────────────────────────
	var buf bytes.Buffer
	if err := pdf.Output(&buf); err != nil {
		return nil, fmt.Errorf("pdf output error: %w", err)
	}
	return buf.Bytes(), nil
}

func (s *reportService) buildFilterDisplay(ctx context.Context, filters []models.ReportFilterConfig) map[string]interface{} {
	// Always-present entries sorted to the top via numeric prefix.
	// drawFiltersAndStats strips the "N_" prefix before rendering.
	display := map[string]interface{}{
		"1_Location":       "All",
		"2_Classification": "All",
		"3_From Date":      "-",
		"4_To Date":        "-",
	}

	for _, f := range filters {
		if f.Value == nil {
			continue
		}
		switch f.Field {
		case "location_id":
			ids := toStringSlice(f.Value)
			if len(ids) > 1 {
				display["1_Location"] = "Multiple"
				continue
			}
			if id, err := uuid.Parse(ids[0]); err == nil {
				if loc, err := s.locationRepo.FindByID(ctx, id); err == nil && loc != nil {
					display["1_Location"] = loc.Name
				}
			}

		case "classification_id":
			ids := toStringSlice(f.Value)
			if len(ids) > 1 {
				display["2_Classification"] = "Multiple"
				continue
			}
			if id, err := uuid.Parse(ids[0]); err == nil {
				if cls, err := s.classificationRepo.FindByID(ctx, id); err == nil && cls != nil {
					display["2_Classification"] = cls.Name
				}
			}
		// case "status_id":
		// 	ids := toStringSlice(f.Value)
		// 	if len(ids) > 1 {
		// 		display["5_Status"] = "Multiple"
		// 		continue
		// 	}
		// 	if id, err := uuid.Parse(ids[0]); err == nil {
		// 		if wrflw, err := s.workflowRepo.FindStateByID(ctx, id); err == nil && wrflw != nil {
		// 			display["5_Status"] = wrflw.Name
		// 		}
		// 	}

		// case "status_name", "status":
		// 	ids := toStringSlice(f.Value)
		// 	if len(ids) > 1 {
		// 		display["5_Status"] = "Multiple"
		// 	} else {
		// 		display["5_Status"] = ids[0]
		// 	}

		// Both "created_at" (incidents/requests) and "incident_created_at"
		// (count-group queries) map to From / To Date.
		case "created_at", "incident_created_at":
			switch f.Operator {
			case "between":
				if m, ok := f.Value.(map[string]interface{}); ok {
					if from, ok := m["from"].(string); ok && from != "" {
						display["3_From Date"] = cleanDateStr(from)
					}
					if to, ok := m["to"].(string); ok && to != "" {
						display["4_To Date"] = cleanDateStr(to)
					}
				}
			case "gte":
				display["3_From Date"] = cleanDateStr(fmt.Sprintf("%v", f.Value))
			case "lte":
				display["4_To Date"] = cleanDateStr(fmt.Sprintf("%v", f.Value))
			}

			// default:
			// 	label := cases.Title(language.English).String(strings.ReplaceAll(f.Field, "_", " "))
			// 	display[label] = formatFilterValue(f.Operator, f.Value)
		}
	}

	return display
}

func toStringSlice(v interface{}) []string {
	switch val := v.(type) {
	case []interface{}:
		out := make([]string, 0, len(val))
		for _, item := range val {
			out = append(out, fmt.Sprintf("%v", item))
		}
		return out
	case []string:
		return val
	case string:
		return []string{val}
	default:
		return []string{fmt.Sprintf("%v", v)}
	}
}

func formatValue(v interface{}) string {
	switch val := v.(type) {
	case string:
		return val
	case time.Time:
		if val.IsZero() {
			return ""
		}
		return val.Format("2006-01-02 15:04:05")
	case float64:
		if val == float64(int64(val)) {
			return strconv.FormatInt(int64(val), 10)
		}
		return strconv.FormatFloat(val, 'f', 2, 64)
	case int:
		return strconv.Itoa(val)
	case int64:
		return strconv.FormatInt(val, 10)
	case bool:
		if val {
			return "Yes"
		}
		return "No"
	case nil:
		return ""
	default:
		return fmt.Sprintf("%v", val)
	}
}

// convertDataTimezone converts all time.Time values in report data to the given timezone.
// Called once before rendering so all formatters automatically use the correct timezone.
func convertDataTimezone(data []map[string]interface{}, loc *time.Location) {
	for _, row := range data {
		for k, v := range row {
			if t, ok := v.(time.Time); ok {
				row[k] = t.In(loc)
			}
		}
	}
}

// formatExportValue formats a value for export, handling enums like priority
func formatExportValue(col string, v interface{}) string {
	if v == nil {
		return ""
	}

	// Handle priority enum
	if col == "priority" {
		switch val := v.(type) {
		case float64:
			return getPriorityLabel(int(val))
		case int:
			return getPriorityLabel(val)
		case int64:
			return getPriorityLabel(int(val))
		}
	}

	// Handle boolean fields
	if col == "sla_breached" || col == "is_active" || col == "is_default" || col == "is_public" || col == "is_super_admin" {
		switch val := v.(type) {
		case bool:
			if val {
				return "Yes"
			}
			return "No"
		}
	}

	return formatValue(v)
}

func getPriorityLabel(priority int) string {
	switch priority {
	case 1:
		return "Critical"
	case 2:
		return "High"
	case 3:
		return "Medium"
	case 4:
		return "Low"
	case 5:
		return "Minimal"
	default:
		return strconv.Itoa(priority)
	}
}
func drawFiltersAndStats(
	pdf *gofpdf.Fpdf,
	filterDisplay map[string]interface{},
	totalRows int,
	totalPages int,
	setFont func(string, float64, string),
) {
	// Sort filter keys for consistent ordering (numeric "N_" prefixes sort to top)
	filterKeys := make([]string, 0, len(filterDisplay))
	for k := range filterDisplay {
		filterKeys = append(filterKeys, k)
	}
	sort.Strings(filterKeys)

	// Build filter lines; strip any "N_" sort prefix before rendering the label
	filterLines := []string{"Filters:"}
	for _, k := range filterKeys {
		v := filterDisplay[k]
		rawKey := stripSortPrefix(k)
		label := cases.Title(language.English).String(strings.ReplaceAll(rawKey, "_", " "))
		filterLines = append(filterLines, fmt.Sprintf("  %s: %s", label, formatFilterValue("", v)))
	}

	statsLines := []string{
		fmt.Sprintf("Total Rows  : %d", totalRows),
		fmt.Sprintf("Total Pages : %d", totalPages),
	}

	const (
		lineH     = 5.5
		padding   = 3.0
		leftX     = 16.0  // match left margin
		pageWidth = 265.0 // 297 - 16 - 16 (A4 landscape with 16mm margins)
		rightColW = 55.0
	)
	leftColW := pageWidth - rightColW - 4

	maxLines := len(filterLines)
	if len(statsLines) > maxLines {
		maxLines = len(statsLines)
	}
	boxH := float64(maxLines)*lineH + padding*2

	boxTopY := pdf.GetY()

	// Background box
	pdf.SetFillColor(245, 247, 250)
	pdf.Rect(leftX, boxTopY, pageWidth, boxH, "F")

	// Left: filter lines
	for i, line := range filterLines {
		pdf.SetXY(leftX+2, boxTopY+padding+float64(i)*lineH)
		if i == 0 {
			setFont("B", 8, "")
		} else {
			setFont("", 8, "")
		}
		pdf.SetTextColor(60, 60, 60)
		pdf.CellFormat(leftColW, lineH, truncateRunes(line, 80), "", 0, "L", false, 0, "")
	}

	// Right: stats lines
	for i, line := range statsLines {
		pdf.SetXY(leftX+leftColW+2, boxTopY+padding+float64(i)*lineH)
		setFont("B", 8, "")
		pdf.SetTextColor(59, 130, 246)
		pdf.CellFormat(rightColW, lineH, line, "", 0, "R", false, 0, "")
	}

	// Move cursor below box
	pdf.SetY(boxTopY + boxH + 4)
	pdf.SetTextColor(0, 0, 0)
}

// stripSortPrefix removes a leading "N_" numeric sort prefix from a filter display key,
// e.g. "1_Location" → "Location", "3_From Date" → "From Date".
func stripSortPrefix(k string) string {
	idx := strings.Index(k, "_")
	if idx <= 0 {
		return k
	}
	for _, c := range k[:idx] {
		if c < '0' || c > '9' {
			return k
		}
	}
	return k[idx+1:]
}

// formatFilterValue formats filter values cleanly for display
func formatFilterValue(operator string, value interface{}) string {
	switch v := value.(type) {

	case string:
		return cleanDateStr(v)

	// between — map{"from": "...", "to": "..."}
	case map[string]interface{}:
		from, _ := v["from"].(string)
		to, _ := v["to"].(string)
		from = cleanDateStr(from)
		to = cleanDateStr(to)
		if from != "" && to != "" {
			return fmt.Sprintf("%s -> %s", from, to)
		}
		if from != "" {
			return fmt.Sprintf("from %s", from)
		}
		if to != "" {
			return fmt.Sprintf("until %s", to)
		}
		return fmt.Sprintf("%v", value)

	// array / IN operator
	case []interface{}:
		parts := make([]string, len(v))
		for i, item := range v {
			parts[i] = fmt.Sprintf("%v", item)
		}
		return strings.Join(parts, ", ")

	case []string:
		return strings.Join(v, ", ")

	default:
		return fmt.Sprintf("%v", value)
	}
}

// cleanDateStr formats a date/datetime string for display in the report filter section.
// Handles datetime-local ("2026-06-01T00:01"), ISO ("2026-06-01T00:01:00.000Z"),
// and plain date ("2026-06-01") formats.
func cleanDateStr(s string) string {
	if idx := strings.Index(s, "T"); idx != -1 {
		timePart := s[idx+1:]
		// Strip trailing Z and fractional seconds for cleaner display
		timePart = strings.TrimSuffix(timePart, "Z")
		if dotIdx := strings.Index(timePart, "."); dotIdx != -1 {
			timePart = timePart[:dotIdx]
		}
		// If time is midnight (00:00 or 00:00:00), show date only
		if timePart == "00:00" || timePart == "00:00:00" {
			return s[:idx]
		}
		return s[:idx] + " " + timePart
	}
	return s
}

func (s *reportService) GetExecutionHistory(ctx context.Context, reportID uuid.UUID, page, limit int) ([]models.ReportExecutionResponse, int64, error) {
	executions, total, err := s.reportRepo.ListExecutions(ctx, reportID, page, limit)
	if err != nil {
		return nil, 0, err
	}

	responses := make([]models.ReportExecutionResponse, len(executions))
	for i, e := range executions {
		responses[i] = toExecutionResponse(&e)
	}

	return responses, total, nil
}

// Metadata

func (s *reportService) GetDataSources(ctx context.Context) []models.DataSourceInfo {
	return []models.DataSourceInfo{
		{
			Name:  "incidents",
			Label: "Incidents",
			Fields: []models.DataSourceField{
				{Field: "incident_number", Label: "Incident Number", Type: "string", Filterable: true, Sortable: true},
				{Field: "title", Label: "Title", Type: "string", Filterable: true, Sortable: true},
				{Field: "description", Label: "Description", Type: "string", Filterable: true, Sortable: false},
				{Field: "priority", Label: "Priority", Type: "number", Filterable: true, Sortable: true},
				{Field: "current_state_name", Label: "Status", Type: "string", Filterable: true, Sortable: true},
				{Field: "classification_name", Label: "Classification", Type: "string", Filterable: true, Sortable: true},
				{Field: "department_name", Label: "Department", Type: "string", Filterable: true, Sortable: true},
				{Field: "location_name", Label: "Location", Type: "string", Filterable: true, Sortable: true},
				{Field: "reporter_email", Label: "Reporter Email", Type: "string", Filterable: true, Sortable: true},
				{Field: "assignee_email", Label: "Assignee Email", Type: "string", Filterable: true, Sortable: true},
				{Field: "sla_breached", Label: "SLA Breached", Type: "boolean", Filterable: true, Sortable: true},
				{Field: "created_at", Label: "Created At", Type: "date", Filterable: true, Sortable: true},
				{Field: "updated_at", Label: "Updated At", Type: "date", Filterable: true, Sortable: true},
				{Field: "resolved_at", Label: "Resolved At", Type: "date", Filterable: true, Sortable: true},
				{Field: "closed_at", Label: "Closed At", Type: "date", Filterable: true, Sortable: true},
			},
		},
		{
			Name:  "users",
			Label: "Users",
			Fields: []models.DataSourceField{
				{Field: "email", Label: "Email", Type: "string", Filterable: true, Sortable: true},
				{Field: "username", Label: "Username", Type: "string", Filterable: true, Sortable: true},
				{Field: "first_name", Label: "First Name", Type: "string", Filterable: true, Sortable: true},
				{Field: "last_name", Label: "Last Name", Type: "string", Filterable: true, Sortable: true},
				{Field: "phone", Label: "Phone", Type: "string", Filterable: true, Sortable: false},
				{Field: "is_active", Label: "Active", Type: "boolean", Filterable: true, Sortable: true},
				{Field: "is_super_admin", Label: "Super Admin", Type: "boolean", Filterable: true, Sortable: true},
				{Field: "department_name", Label: "Department", Type: "string", Filterable: true, Sortable: true},
				{Field: "location_name", Label: "Location", Type: "string", Filterable: true, Sortable: true},
				{Field: "created_at", Label: "Created At", Type: "date", Filterable: true, Sortable: true},
			},
		},
		{
			Name:  "workflows",
			Label: "Workflows",
			Fields: []models.DataSourceField{
				{Field: "name", Label: "Name", Type: "string", Filterable: true, Sortable: true},
				{Field: "code", Label: "Code", Type: "string", Filterable: true, Sortable: true},
				{Field: "description", Label: "Description", Type: "string", Filterable: true, Sortable: false},
				{Field: "is_active", Label: "Active", Type: "boolean", Filterable: true, Sortable: true},
				{Field: "is_default", Label: "Default", Type: "boolean", Filterable: true, Sortable: true},
				{Field: "created_at", Label: "Created At", Type: "date", Filterable: true, Sortable: true},
			},
		},
		{
			Name:  "departments",
			Label: "Departments",
			Fields: []models.DataSourceField{
				{Field: "name", Label: "Name", Type: "string", Filterable: true, Sortable: true},
				{Field: "code", Label: "Code", Type: "string", Filterable: true, Sortable: true},
				{Field: "description", Label: "Description", Type: "string", Filterable: true, Sortable: false},
				{Field: "parent_name", Label: "Parent Department", Type: "string", Filterable: true, Sortable: true},
				{Field: "is_active", Label: "Active", Type: "boolean", Filterable: true, Sortable: true},
				{Field: "created_at", Label: "Created At", Type: "date", Filterable: true, Sortable: true},
			},
		},
		{
			Name:  "locations",
			Label: "Locations",
			Fields: []models.DataSourceField{
				{Field: "name", Label: "Name", Type: "string", Filterable: true, Sortable: true},
				{Field: "code", Label: "Code", Type: "string", Filterable: true, Sortable: true},
				{Field: "location_type", Label: "Type", Type: "string", Filterable: true, Sortable: true},
				{Field: "address", Label: "Address", Type: "string", Filterable: true, Sortable: false},
				{Field: "parent_name", Label: "Parent Location", Type: "string", Filterable: true, Sortable: true},
				{Field: "is_active", Label: "Active", Type: "boolean", Filterable: true, Sortable: true},
				{Field: "created_at", Label: "Created At", Type: "date", Filterable: true, Sortable: true},
			},
		},
		{
			Name:  "classifications",
			Label: "Classifications",
			Fields: []models.DataSourceField{
				{Field: "name", Label: "Name", Type: "string", Filterable: true, Sortable: true},
				{Field: "code", Label: "Code", Type: "string", Filterable: true, Sortable: true},
				{Field: "description", Label: "Description", Type: "string", Filterable: true, Sortable: false},
				{Field: "parent_name", Label: "Parent Classification", Type: "string", Filterable: true, Sortable: true},
				{Field: "is_active", Label: "Active", Type: "boolean", Filterable: true, Sortable: true},
				{Field: "created_at", Label: "Created At", Type: "date", Filterable: true, Sortable: true},
			},
		},
		{
			Name:  "action_logs",
			Label: "Action Logs",
			Fields: []models.DataSourceField{
				{Field: "action", Label: "Action", Type: "string", Filterable: true, Sortable: true},
				{Field: "module", Label: "Module", Type: "string", Filterable: true, Sortable: true},
				{Field: "resource_id", Label: "Resource ID", Type: "string", Filterable: true, Sortable: false},
				{Field: "description", Label: "Description", Type: "string", Filterable: true, Sortable: false},
				{Field: "status", Label: "Status", Type: "string", Filterable: true, Sortable: true},
				{Field: "ip_address", Label: "IP Address", Type: "string", Filterable: true, Sortable: false},
				{Field: "user_agent", Label: "User Agent", Type: "string", Filterable: false, Sortable: false},
				{Field: "duration", Label: "Duration (ms)", Type: "number", Filterable: true, Sortable: true},
				{Field: "user_email", Label: "User Email", Type: "string", Filterable: false, Sortable: true},
				{Field: "user_username", Label: "Username", Type: "string", Filterable: false, Sortable: true},
				{Field: "user_full_name", Label: "User Full Name", Type: "string", Filterable: false, Sortable: false},
				{Field: "created_at", Label: "Created At", Type: "date", Filterable: true, Sortable: true},
			},
		},
		{
			Name:  "locations_by_count",
			Label: "Locations by Incident Count",
			Fields: []models.DataSourceField{
				{Field: "location_name", Label: "Location", Type: "string", Filterable: false, Sortable: true},
				{Field: "parent_location_name", Label: "Parent Location", Type: "string", Filterable: false, Sortable: true},
				{Field: "incident_count", Label: "No. of Incidents", Type: "number", Filterable: false, Sortable: true},
				{Field: "status_name", Label: "Status", Type: "uuid", Filterable: true, Sortable: false},
			},
		},
		{
			Name:  "locations_by_status",
			Label: "Locations by Status",
			Fields: []models.DataSourceField{
				{Field: "location_name", Label: "Location", Type: "string", Filterable: false, Sortable: true},
				{Field: "parent_location_name", Label: "Parent Location", Type: "string", Filterable: false, Sortable: true},
				{Field: "status_name", Label: "Status", Type: "string", Filterable: false, Sortable: true},
				{Field: "incident_count", Label: "No. of Incidents", Type: "number", Filterable: false, Sortable: true},
			},
		},
		{
			Name:  "classifications_by_count",
			Label: "Classifications by Incident Count",
			Fields: []models.DataSourceField{
				{Field: "classification_name", Label: "Classification", Type: "string", Filterable: false, Sortable: true},
				{Field: "parent_classification_name", Label: "Parent Classification", Type: "string", Filterable: false, Sortable: true},
				{Field: "incident_count", Label: "No. of Incidents", Type: "number", Filterable: false, Sortable: true},
				{Field: "status_name", Label: "Status", Type: "uuid", Filterable: true, Sortable: false},
			},
		},
		{
			Name:  "classifications_by_status",
			Label: "Classifications by Status",
			Fields: []models.DataSourceField{
				{Field: "classification_name", Label: "Classification", Type: "string", Filterable: false, Sortable: true},
				{Field: "parent_classification_name", Label: "Parent Classification", Type: "string", Filterable: false, Sortable: true},
				{Field: "status_name", Label: "Status", Type: "string", Filterable: false, Sortable: true},
				{Field: "incident_count", Label: "No. of Incidents", Type: "number", Filterable: false, Sortable: true},
			},
		},
		{
			Name:  "departments_by_count",
			Label: "Departments by Incident Count",
			Fields: []models.DataSourceField{
				{Field: "department_name", Label: "Department", Type: "string", Filterable: false, Sortable: true},
				{Field: "parent_department_name", Label: "Parent Department", Type: "string", Filterable: false, Sortable: true},
				{Field: "incident_count", Label: "No. of Incidents", Type: "number", Filterable: false, Sortable: true},
				{Field: "status_name", Label: "Status", Type: "uuid", Filterable: true, Sortable: false},
			},
		},
		{
			Name:  "departments_by_status",
			Label: "Departments by Status",
			Fields: []models.DataSourceField{
				{Field: "department_name", Label: "Department", Type: "string", Filterable: false, Sortable: true},
				{Field: "parent_department_name", Label: "Parent Department", Type: "string", Filterable: false, Sortable: true},
				{Field: "status_name", Label: "Status", Type: "string", Filterable: false, Sortable: true},
				{Field: "incident_count", Label: "No. of Incidents", Type: "number", Filterable: false, Sortable: true},
			},
		},
		{
			Name:  "rejection_logs",
			Label: "Rejected Incidents",
			Fields: []models.DataSourceField{
				{Field: "incident_number", Label: "Incident Number", Type: "string", Filterable: true, Sortable: true},
				{Field: "incident_title", Label: "Incident Title", Type: "string", Filterable: true, Sortable: true},
				{Field: "record_type", Label: "Record Type", Type: "string", Filterable: true, Sortable: true},
				{Field: "rejection_sequence", Label: "Rejection Sequence", Type: "number", Filterable: true, Sortable: true},
				{Field: "total_rejection_count", Label: "Total Rejection Count", Type: "number", Filterable: true, Sortable: true},
				{Field: "received_at", Label: "Received At", Type: "date", Filterable: true, Sortable: true},
				{Field: "rejected_at", Label: "Rejected At", Type: "date", Filterable: true, Sortable: true},
				{Field: "reaction_time_minutes", Label: "Reaction Time (Minutes)", Type: "number", Filterable: true, Sortable: true},
				{Field: "rejection_reason", Label: "Rejection Reason", Type: "string", Filterable: true, Sortable: false},
				{Field: "rejected_by_username", Label: "Rejected By (Username)", Type: "string", Filterable: true, Sortable: true},
				{Field: "rejected_by.full_name", Label: "Rejected By (Full Name)", Type: "string", Filterable: false, Sortable: false},
				{Field: "sla_threshold_hours", Label: "SLA Threshold (Hours)", Type: "number", Filterable: true, Sortable: true},
				{Field: "sla_threshold_minutes", Label: "SLA Threshold (Minutes)", Type: "number", Filterable: true, Sortable: true},
				{Field: "sla_status", Label: "SLA Status", Type: "string", Filterable: true, Sortable: true},
				{Field: "sla_breached_at_rejection", Label: "SLA Breached at Rejection", Type: "boolean", Filterable: true, Sortable: true},
				{Field: "from_state.name", Label: "From State", Type: "string", Filterable: false, Sortable: false},
				{Field: "to_state.name", Label: "To State (Rejected State)", Type: "string", Filterable: false, Sortable: false},
				{Field: "department.name", Label: "Department", Type: "string", Filterable: false, Sortable: false},
				{Field: "classification.name", Label: "Classification", Type: "string", Filterable: false, Sortable: false},
				{Field: "created_at", Label: "Log Created At", Type: "date", Filterable: true, Sortable: true},
			},
		},
	}
}

// Helper functions

func toReportResponse(r *models.Report) *models.ReportResponse {
	var columns []models.ReportColumnConfig
	var filters []models.ReportFilterConfig
	var sorting []models.ReportSortConfig

	json.Unmarshal([]byte(r.Columns), &columns)
	json.Unmarshal([]byte(r.Filters), &filters)
	json.Unmarshal([]byte(r.Sorting), &sorting)

	// Build the config object matching frontend structure
	config := models.ReportTemplateConfig{
		Columns: columns,
		Filters: filters,
		Sorting: sorting,
	}

	resp := &models.ReportResponse{
		ID:            r.ID.String(),
		Name:          r.Name,
		NameAr:        r.NameAr,
		TimestampKey:  r.TimestampKey,
		Description:   r.Description,
		DescriptionAr: r.DescriptionAr,
		DataSource:    r.DataSource,
		Config:        config,
		IsPublic:      r.IsPublic,
		IsSystem:      false, // Reports created by users are not system reports
		CanEdit:       true,  // Will be set based on permissions later
		CreatedAt:     r.CreatedAt.Format(time.RFC3339),
		UpdatedAt:     r.UpdatedAt.Format(time.RFC3339),
	}

	if r.CreatedBy != nil {
		resp.CreatedBy = &models.UserBasicResponse{
			ID:        r.CreatedBy.ID.String(),
			Email:     r.CreatedBy.Email,
			Username:  r.CreatedBy.Username,
			FirstName: r.CreatedBy.FirstName,
			LastName:  r.CreatedBy.LastName,
			Avatar:    r.CreatedBy.Avatar,
		}
	}

	return resp
}

func toExecutionResponse(e *models.ReportExecution) models.ReportExecutionResponse {
	resp := models.ReportExecutionResponse{
		ID:          e.ID.String(),
		ReportID:    e.ReportID.String(),
		Status:      e.Status,
		ResultCount: e.ResultCount,
		FilePath:    e.FilePath,
		Error:       e.Error,
		CreatedAt:   e.CreatedAt.Format(time.RFC3339),
	}

	if e.StartedAt != nil {
		resp.StartedAt = e.StartedAt.Format(time.RFC3339)
	}
	if e.CompletedAt != nil {
		resp.CompletedAt = e.CompletedAt.Format(time.RFC3339)
	}

	if e.ExecutedBy != nil {
		resp.ExecutedBy = &models.UserBasicResponse{
			ID:        e.ExecutedBy.ID.String(),
			Email:     e.ExecutedBy.Email,
			Username:  e.ExecutedBy.Username,
			FirstName: e.ExecutedBy.FirstName,
			LastName:  e.ExecutedBy.LastName,
			Avatar:    e.ExecutedBy.Avatar,
		}
	}

	return resp
}

func resolveTitle(title, dataSource, lang string) string {
	if title != "" {
		return title
	}
	log.Print("Language", lang)
	// Fallback titles per data source
	titles := map[string]map[string]string{
		"incidents": {
			"ar": "تقرير الحوادث",
			"en": "Incidents_Report",
		},
		"requests": {
			"ar": "تقرير الطلبات",
			"en": "Requests_Report",
		},
		"users": {
			"ar": "تقرير المستخدمين",
			"en": "Users_Report",
		},
		"location": {
			"ar": "تقرير الموقع",
			"en": "Location_Report",
		},
		"departments": {
			"ar": "تقرير الأقسام",
			"en": "Departments_Report",
		},
		"classifications": {
			"ar": "تقرير التصنيفات",
			"en": "Classifications_Report",
		},
		"workflows": {
			"ar": "تقرير سير العمل",
			"en": "Workflows_Report",
		},
		"action_logs": {
			"ar": "تقرير سجلات الإجراءات",
			"en": "Action_Logs_Report",
		},
		"rejection_logs": {
			"ar": "تقرير سجلات الرفض",
			"en": "Rejection_Logs_Report",
		},
	}

	if langs, ok := titles[dataSource]; ok {
		if lang == "ar" {
			return langs["ar"]
		}
		return langs["en"]
	}

	// Unknown data source — generic fallback
	if lang == "ar" {
		return "تقرير"
	}
	return "Report"
}

// estimateRowHeight returns a row height tall enough for wrapped content
// based on the longest cell value in that row at col width 27
func estimateRowHeight(row map[string]interface{}, columns []models.ColumnField, colWidth float64) float64 {
	const charPerLine = 30.0 // ~chars that fit in width 27 at font size 11
	const lineHeight = 15.0  // points per line in Excel
	const minHeight = 20.0
	const maxHeight = 120.0

	maxLines := 1.0
	for _, col := range columns {
		v, ok := row[col.Label]
		if !ok || v == nil {
			continue
		}
		val := formatExportValue(col.Label, v)
		// Count lines needed: length / chars per line, minimum 1
		lines := math.Ceil(float64(len([]rune(val))) / charPerLine)
		if lines > maxLines {
			maxLines = lines
		}
	}

	height := maxLines * lineHeight
	if height < minHeight {
		return minHeight
	}
	if height > maxHeight {
		return maxHeight
	}
	return height
}
