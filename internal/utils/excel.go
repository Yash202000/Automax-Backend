package utils

import (
	"fmt"
	"regexp"
	"strconv"
	"strings"

	"github.com/xuri/excelize/v2"
)

type Language string

const (
	LanguageEnglish Language = "en"
	LanguageArabic  Language = "ar"
)

type LocalizedText struct {
	EN string
	AR string
}

func (t LocalizedText) For(lang Language) string {
	if lang == LanguageArabic {
		return t.AR
	}
	return t.EN
}

type WorkbookConfig struct {
	Language     Language
	Title        string
	Subject      string
	Creator      string
	ActiveSheet  string
	Instructions *InstructionConfig
	Sheets       []SheetConfig
	// Lists, when set, adds a read-only sheet (ListsSheet, default "Lists")
	// as the last sheet, with one column per list. Each list becomes a
	// workbook name that a DropdownConfig.Source can point to.
	ListsSheet string
	Lists      []ListConfig
}

// ListConfig is one dropdown source on the Lists sheet: the fixed Values
// first, then Formulas (each returns a value or ""; non-empty results must
// come first, e.g. a compacted copy of a column the user fills in). The
// workbook name Name covers only the non-empty cells.
type ListConfig struct {
	Name     string
	Title    LocalizedText
	Values   []string
	Formulas []string
	// Lookup lists are columns of a lookup table read with INDEX/MATCH
	// (several lists side by side, row for row). Their name covers every
	// written row, blanks included, so the columns stay aligned.
	Lookup bool
}

type SheetConfig struct {
	Name             string
	Columns          []ColumnConfig
	NumberedSections []NumberedSectionConfig
	DataRows         int // number of data rows; validations cover rows 2 through DataRows+1
	FreezeHeader     bool
	AutoFilter       bool
	// ConditionalFills colour data cells by a per-row rule, e.g. grey out
	// the cells a row does not use.
	ConditionalFills []ConditionalFill
}

// ConditionalFill colours the data cells of the columns Keys when Formula is
// true. Formula is an Excel formula without the leading "=", written for row
// 2 with relative row references (e.g. `$K2="Target Range"`); "{col:key}" is
// replaced by the letter of the column with that key.
type ConditionalFill struct {
	Keys      []string
	Formula   string
	Fill      string // background, hex RGB
	FontColor string // optional, hex RGB
}

type ColumnConfig struct {
	Key      string
	Name     LocalizedText
	Width    float64
	Required bool // required: header gets a red (*) marker
	Dropdown *DropdownConfig
	// HeaderColor is the header fill (hex RGB, e.g. "1F6F78"); empty uses
	// the default dark blue.
	HeaderColor string
	// Hint is shown as an input message when a cell in the column is
	// selected (columns without a dropdown).
	Hint LocalizedText
	// Formula, when set, is written into the column's data cells (rows 2 to
	// FormulaRows+1, default all data rows); "{row}" is replaced by the row
	// number. Used to fill a cell automatically from another one on the
	// row; the user can still type over it.
	Formula     string
	FormulaRows int
	// AllowIf, for a column without a dropdown, refuses a typed value on
	// rows where this formula is false (same form as ConditionalFill.Formula:
	// row 2, relative row references, "{col:key}" for column letters).
	// AllowIfError is the message shown. Excel checks typed values only,
	// not pasted ones, so the importer must still enforce the rule.
	AllowIf      string
	AllowIfError LocalizedText
	// Hidden hides the column (e.g. a helper column of formulas that other
	// columns' dropdowns read).
	Hidden bool
}

type DropdownConfig struct {
	Values   []string
	ValuesAR []string
	// Source is a workbook name from WorkbookConfig.Lists, or a formula
	// returning a range (row 2, relative row references, "{col:key}" for
	// column letters; at most 255 characters); it replaces Values/ValuesAR.
	Source string
	// AllowOther shows a warning instead of refusing a value that is not
	// in the list.
	AllowOther  bool
	PromptTitle LocalizedText
	Prompt      LocalizedText
	ErrorTitle  LocalizedText
	Error       LocalizedText
}

// NumberedSectionConfig produces a header row followed by pre-numbered input rows.
// HeaderColumns are written starting at column C; column A is # and column B is Title.
type NumberedSectionConfig struct {
	Title         LocalizedText
	HeaderColumns []LocalizedText
	Rows          int
}

type InstructionConfig struct {
	LabelWidth float64
	TextWidth  float64
	Sections   []InstructionSection
}

type InstructionSection struct {
	Title      LocalizedText
	Items      []InstructionItem
	BlankAfter bool
}

type InstructionItem struct {
	Label       LocalizedText
	Text        LocalizedText
	BlankBefore bool
}

type excelStyles struct {
	title, section, header, label, note, body int
}

// GenerateExcelTemplate creates an XLSX workbook and returns its bytes.
// Business-specific text and columns belong in the calling service; Excel mechanics stay here.
func GenerateExcelTemplate(cfg WorkbookConfig) ([]byte, error) {
	if len(cfg.Sheets) == 0 && cfg.Instructions == nil {
		return nil, fmt.Errorf("workbook must contain at least one sheet")
	}
	if cfg.Language != LanguageArabic {
		cfg.Language = LanguageEnglish
	}

	f := excelize.NewFile()
	defer f.Close()

	styles, err := newExcelStyles(f)
	if err != nil {
		return nil, fmt.Errorf("creating Excel styles: %w", err)
	}

	firstName := ""
	if cfg.Instructions != nil {
		firstName = "Instructions"
	} else {
		firstName = cfg.Sheets[0].Name
	}
	if firstName == "" {
		return nil, fmt.Errorf("sheet name cannot be empty")
	}
	if err := f.SetSheetName("Sheet1", firstName); err != nil {
		return nil, fmt.Errorf("renaming initial sheet: %w", err)
	}

	if cfg.Instructions != nil {
		if err := writeInstructions(f, styles, firstName, cfg.Language, *cfg.Instructions); err != nil {
			return nil, err
		}
	}
	for i, sheet := range cfg.Sheets {
		if strings.TrimSpace(sheet.Name) == "" {
			return nil, fmt.Errorf("sheet name cannot be empty")
		}
		if cfg.Instructions == nil && i == 0 {
			if err := writeSheet(f, styles, cfg.Language, sheet); err != nil {
				return nil, fmt.Errorf("writing sheet %q: %w", sheet.Name, err)
			}
			continue
		}
		if sheet.Name == firstName {
			return nil, fmt.Errorf("sheet %q conflicts with the initial sheet", sheet.Name)
		}
		if _, err := f.NewSheet(sheet.Name); err != nil {
			return nil, fmt.Errorf("creating sheet %q: %w", sheet.Name, err)
		}
		if err := writeSheet(f, styles, cfg.Language, sheet); err != nil {
			return nil, fmt.Errorf("writing sheet %q: %w", sheet.Name, err)
		}
	}

	if len(cfg.Lists) > 0 {
		if err := writeLists(f, styles, cfg); err != nil {
			return nil, err
		}
	}

	active := cfg.ActiveSheet
	if active == "" {
		active = firstName
	}
	idx, err := f.GetSheetIndex(active)
	if err != nil || idx < 0 {
		return nil, fmt.Errorf("active sheet %q does not exist", active)
	}
	f.SetActiveSheet(idx)

	if cfg.Title != "" || cfg.Subject != "" || cfg.Creator != "" {
		if err := f.SetDocProps(&excelize.DocProperties{Title: cfg.Title, Subject: cfg.Subject, Creator: cfg.Creator}); err != nil {
			return nil, fmt.Errorf("setting workbook properties: %w", err)
		}
	}
	buf, err := f.WriteToBuffer()
	if err != nil {
		return nil, fmt.Errorf("serializing workbook: %w", err)
	}
	return append([]byte(nil), buf.Bytes()...), nil
}

func newExcelStyles(f *excelize.File) (excelStyles, error) {
	var s excelStyles

	border := []excelize.Border{
		{Type: "left", Color: "B4C6E7", Style: 1},
		{Type: "right", Color: "B4C6E7", Style: 1},
		{Type: "top", Color: "B4C6E7", Style: 1},
		{Type: "bottom", Color: "B4C6E7", Style: 1},
	}

	var err error

	if s.title, err = f.NewStyle(&excelize.Style{
		Font:      &excelize.Font{Bold: true, Size: 16, Color: "1F3864"},
		Alignment: &excelize.Alignment{Vertical: "center"},
	}); err != nil {
		return s, err
	}

	if s.section, err = f.NewStyle(&excelize.Style{
		Font:      &excelize.Font{Bold: true, Color: "FFFFFF"},
		Fill:      excelize.Fill{Type: "pattern", Color: []string{"1F3864"}, Pattern: 1},
		Alignment: &excelize.Alignment{Vertical: "center", WrapText: true},
		Border:    border,
	}); err != nil {
		return s, err
	}

	if s.header, err = f.NewStyle(&excelize.Style{
		Font: &excelize.Font{Bold: true, Color: "FFFFFF"},
		Fill: excelize.Fill{
			Type:    "pattern",
			Color:   []string{"1F3864"},
			Pattern: 1,
		},
		Alignment: &excelize.Alignment{
			Horizontal: "center",
			Vertical:   "center",
			WrapText:   true,
		},
		Border: border,
	}); err != nil {
		return s, err
	}

	if s.label, err = f.NewStyle(&excelize.Style{
		Font:      &excelize.Font{Bold: true, Color: "1F3864"},
		Alignment: &excelize.Alignment{Vertical: "top", WrapText: true},
	}); err != nil {
		return s, err
	}

	if s.note, err = f.NewStyle(&excelize.Style{
		Font:      &excelize.Font{Color: "333333"},
		Alignment: &excelize.Alignment{Vertical: "top", WrapText: true},
	}); err != nil {
		return s, err
	}

	if s.body, err = f.NewStyle(&excelize.Style{
		Alignment: &excelize.Alignment{Vertical: "center"},
		Border:    border,
	}); err != nil {
		return s, err
	}

	return s, nil
}

func writeSheet(f *excelize.File, styles excelStyles, lang Language, cfg SheetConfig) error {
	if len(cfg.NumberedSections) > 0 {
		return writeNumberedSections(f, styles, lang, cfg)
	}
	for i, col := range cfg.Columns {
		cell, err := excelize.CoordinatesToCellName(i+1, 1)
		if err != nil {
			return err
		}
		label := col.Name.For(lang)
		if err := f.SetCellValue(cfg.Name, cell, label); err != nil {
			return err
		}
		headerStyle := styles.header
		if col.HeaderColor != "" {
			if headerStyle, err = headerStyleWithColor(f, col.HeaderColor); err != nil {
				return err
			}
		}
		if err := f.SetCellStyle(cfg.Name, cell, cell, headerStyle); err != nil {
			return err
		}
		if col.Required && label != "" {
			// Mandatory marker: red asterisk on the normal header.
			runs := []excelize.RichTextRun{
				{Text: label},
				{Text: " *", Font: &excelize.Font{Bold: true, Color: "FF0000"}},
			}
			if err := f.SetCellRichText(cfg.Name, cell, runs); err != nil {
				return err
			}
		}
		name, err := excelize.ColumnNumberToName(i + 1)
		if err != nil {
			return err
		}
		width := col.Width
		if width <= 0 {
			width = 18
		}
		if err := f.SetColWidth(cfg.Name, name, name, width); err != nil {
			return err
		}
		if col.Hidden {
			if err := f.SetColVisible(cfg.Name, name, false); err != nil {
				return err
			}
		}
	}
	if len(cfg.Columns) > 0 {
		if err := f.SetRowHeight(cfg.Name, 1, 32); err != nil {
			return err
		}
		if cfg.FreezeHeader {
			if err := f.SetPanes(cfg.Name, &excelize.Panes{Freeze: true, YSplit: 1, TopLeftCell: "A2", ActivePane: "bottomLeft"}); err != nil {
				return err
			}
		}
		if cfg.AutoFilter {
			end, err := excelize.ColumnNumberToName(len(cfg.Columns))
			if err != nil {
				return err
			}
			if err := f.AutoFilter(cfg.Name, fmt.Sprintf("A1:%s1", end), []excelize.AutoFilterOptions{}); err != nil {
				return err
			}
		}
	}
	rows := cfg.DataRows
	if rows <= 0 {
		rows = 4999
	}
	for i, col := range cfg.Columns {
		if col.Formula == "" {
			continue
		}
		name, err := excelize.ColumnNumberToName(i + 1)
		if err != nil {
			return err
		}
		n := col.FormulaRows
		if n <= 0 || n > rows {
			n = rows
		}
		for r := 2; r <= n+1; r++ {
			if err := f.SetCellFormula(cfg.Name, fmt.Sprintf("%s%d", name, r), strings.ReplaceAll(col.Formula, "{row}", strconv.Itoa(r))); err != nil {
				return err
			}
		}
	}
	for i, col := range cfg.Columns {
		name, err := excelize.ColumnNumberToName(i + 1)
		if err != nil {
			return err
		}
		if col.Dropdown == nil {
			if col.AllowIf != "" {
				// One validation per cell: the rule plus the column's hint.
				formula, err := resolveColumnRefs(cfg, col.AllowIf)
				if err != nil {
					return err
				}
				if len([]rune(formula)) > 255 { // Excel's limit for a validation formula
					return fmt.Errorf("typing rule for %s: formula is %d characters, Excel allows 255", col.Key, len([]rune(formula)))
				}
				dv := excelize.NewDataValidation(true)
				dv.Sqref = fmt.Sprintf("%s2:%s%d", name, name, rows+1)
				dv.Type = "custom"
				dv.Formula1 = xmlTextEscaper.Replace(formula) // written into the XML as-is
				if hint := col.Hint.For(lang); hint != "" {
					dv.SetInput(col.Name.For(lang), hint)
				}
				dv.SetError(excelize.DataValidationErrorStyleStop, col.Name.For(lang), col.AllowIfError.For(lang))
				if err := f.AddDataValidation(cfg.Name, dv); err != nil {
					return err
				}
				continue
			}
			if hint := col.Hint.For(lang); hint != "" {
				dv := excelize.NewDataValidation(true)
				dv.Sqref = fmt.Sprintf("%s2:%s%d", name, name, rows+1)
				dv.SetInput(col.Name.For(lang), hint)
				if err := f.AddDataValidation(cfg.Name, dv); err != nil {
					return err
				}
			}
			continue
		}
		values := col.Dropdown.Values
		if lang == LanguageArabic && len(col.Dropdown.ValuesAR) > 0 {
			values = col.Dropdown.ValuesAR
		}
		if len(values) == 0 && col.Dropdown.Source == "" {
			continue
		}
		dv := excelize.NewDataValidation(true)
		dv.Sqref = fmt.Sprintf("%s2:%s%d", name, name, rows+1)
		if col.Dropdown.Source != "" {
			source, err := resolveColumnRefs(cfg, col.Dropdown.Source)
			if err != nil {
				return err
			}
			if len([]rune(source)) > 255 { // Excel's limit for a validation formula
				return fmt.Errorf("dropdown for %s: source formula is %d characters, Excel allows 255", col.Key, len([]rune(source)))
			}
			dv.SetSqrefDropList(xmlTextEscaper.Replace(source)) // written into the XML as-is
		} else if err := dv.SetDropList(values); err != nil {
			return fmt.Errorf("dropdown for %s: %w", col.Key, err)
		}
		promptTitle := col.Dropdown.PromptTitle.For(lang)
		if promptTitle == "" {
			promptTitle = "Allowed values"
			if lang == LanguageArabic {
				promptTitle = "القيم المسموحة"
			}
		}
		prompt := col.Dropdown.Prompt.For(lang)
		if prompt == "" {
			prompt = strings.Join(values, ", ")
		}
		dv.SetInput(promptTitle, prompt)
		errorTitle := col.Dropdown.ErrorTitle.For(lang)
		if errorTitle == "" {
			errorTitle = "Invalid value"
			if lang == LanguageArabic {
				errorTitle = "قيمة غير صالحة"
			}
		}
		errorText := col.Dropdown.Error.For(lang)
		if errorText == "" {
			errorText = "Choose one of: " + strings.Join(values, ", ")
			if lang == LanguageArabic {
				errorText = "اختر أحد الخيارات: " + strings.Join(values, ", ")
			}
		}
		errorStyle := excelize.DataValidationErrorStyleStop
		if col.Dropdown.AllowOther {
			errorStyle = excelize.DataValidationErrorStyleWarning
		}
		dv.SetError(errorStyle, errorTitle, errorText)
		if err := f.AddDataValidation(cfg.Name, dv); err != nil {
			return err
		}
	}
	return writeConditionalFills(f, cfg, rows)
}

// writeConditionalFills adds the sheet's ConditionalFills as formula-based
// conditional formatting over rows 2 to rows+1.
func writeConditionalFills(f *excelize.File, cfg SheetConfig, rows int) error {
	if len(cfg.ConditionalFills) == 0 {
		return nil
	}
	letter, err := columnLetters(cfg)
	if err != nil {
		return err
	}
	for _, cf := range cfg.ConditionalFills {
		formula, err := resolveColumnRefs(cfg, cf.Formula)
		if err != nil {
			return err
		}
		style := &excelize.Style{Fill: excelize.Fill{Type: "pattern", Pattern: 1, Color: []string{cf.Fill}}}
		if cf.FontColor != "" {
			style.Font = &excelize.Font{Color: cf.FontColor}
		}
		id, err := f.NewConditionalStyle(style)
		if err != nil {
			return err
		}
		for _, key := range cf.Keys {
			l, ok := letter[key]
			if !ok {
				return fmt.Errorf("conditional fill on sheet %s: unknown column %q", cfg.Name, key)
			}
			if err := f.SetConditionalFormat(cfg.Name, fmt.Sprintf("%s2:%s%d", l, l, rows+1),
				[]excelize.ConditionalFormatOptions{{Type: "formula", Criteria: formula, Format: &id}}); err != nil {
				return err
			}
		}
	}
	return nil
}

// columnLetters maps the sheet's column keys to their letters.
func columnLetters(cfg SheetConfig) (map[string]string, error) {
	letter := make(map[string]string, len(cfg.Columns))
	for i, col := range cfg.Columns {
		name, err := excelize.ColumnNumberToName(i + 1)
		if err != nil {
			return nil, err
		}
		letter[col.Key] = name
	}
	return letter, nil
}

var columnRefRe = regexp.MustCompile(`\{col:([A-Za-z0-9_]+)\}`)

// resolveColumnRefs replaces "{col:key}" in a formula with the column letter.
func resolveColumnRefs(cfg SheetConfig, formula string) (string, error) {
	letter, err := columnLetters(cfg)
	if err != nil {
		return "", err
	}
	var missing string
	out := columnRefRe.ReplaceAllStringFunc(formula, func(m string) string {
		key := columnRefRe.FindStringSubmatch(m)[1]
		if l, ok := letter[key]; ok {
			return l
		}
		missing = key
		return m
	})
	if missing != "" {
		return "", fmt.Errorf("sheet %s: formula refers to unknown column %q", cfg.Name, missing)
	}
	return out, nil
}

// xmlTextEscaper escapes a formula that excelize writes into the XML as-is.
var xmlTextEscaper = strings.NewReplacer("&", "&amp;", "<", "&lt;", ">", "&gt;")

func writeNumberedSections(f *excelize.File, styles excelStyles, lang Language, cfg SheetConfig) error {
	if err := f.SetColWidth(cfg.Name, "A", "A", 6); err != nil {
		return err
	}
	if err := f.SetColWidth(cfg.Name, "B", "B", 46); err != nil {
		return err
	}
	if err := f.SetColWidth(cfg.Name, "C", "F", 28); err != nil {
		return err
	}
	row := 1
	for _, section := range cfg.NumberedSections {
		if section.Rows < 1 {
			return fmt.Errorf("numbered section %q must have at least one row", section.Title.For(lang))
		}
		lastCol := len(section.HeaderColumns) + 2
		if err := setCell(f, cfg.Name, 1, row, "#", styles.section); err != nil {
			return err
		}
		if err := setCell(f, cfg.Name, 2, row, section.Title.For(lang), styles.section); err != nil {
			return err
		}
		for i, h := range section.HeaderColumns {
			if err := setCell(f, cfg.Name, i+3, row, h.For(lang), styles.section); err != nil {
				return err
			}
		}
		if err := f.SetRowHeight(cfg.Name, row, 22); err != nil {
			return err
		}
		row++
		firstData := row
		for n := 1; n <= section.Rows; n++ {
			if err := setCell(f, cfg.Name, 1, row, n, styles.body); err != nil {
				return err
			}
			row++
		}
		endCell, err := excelize.CoordinatesToCellName(lastCol, row-1)
		if err != nil {
			return err
		}
		startCell, err := excelize.CoordinatesToCellName(2, firstData)
		if err != nil {
			return err
		}
		if err := f.SetCellStyle(cfg.Name, startCell, endCell, styles.body); err != nil {
			return err
		}
		row++
	}
	return f.SetPanes(cfg.Name, &excelize.Panes{Freeze: true, YSplit: 1, TopLeftCell: "A2", ActivePane: "bottomLeft"})
}

func writeInstructions(f *excelize.File, styles excelStyles, sheet string, lang Language, cfg InstructionConfig) error {
	labelWidth, textWidth := cfg.LabelWidth, cfg.TextWidth
	if labelWidth <= 0 {
		labelWidth = 30
	}
	if textWidth <= 0 {
		textWidth = 110
	}
	if err := f.SetColWidth(sheet, "A", "A", labelWidth); err != nil {
		return err
	}
	if err := f.SetColWidth(sheet, "B", "B", textWidth); err != nil {
		return err
	}
	row := 1
	writeItem := func(item InstructionItem) error {
		if item.BlankBefore {
			row++
		}
		if err := setCell(f, sheet, 1, row, item.Label.For(lang), styles.label); err != nil {
			return err
		}
		value := item.Text.For(lang)
		if err := setCell(f, sheet, 2, row, value, styles.note); err != nil {
			return err
		}
		height := 21.0
		n := len([]rune(value))
		if n > 80 {
			height = 36
		}
		if n > 160 {
			height = 52
		}
		if err := f.SetRowHeight(sheet, row, height); err != nil {
			return err
		}
		row++
		return nil
	}
	for _, section := range cfg.Sections {
		if section.Title.EN != "" || section.Title.AR != "" {
			if err := f.MergeCell(sheet, fmt.Sprintf("A%d", row), fmt.Sprintf("B%d", row)); err != nil {
				return err
			}
			if err := f.SetCellValue(sheet, fmt.Sprintf("A%d", row), section.Title.For(lang)); err != nil {
				return err
			}
			if err := f.SetCellStyle(sheet, fmt.Sprintf("A%d", row), fmt.Sprintf("B%d", row), styles.section); err != nil {
				return err
			}
			if err := f.SetRowHeight(sheet, row, 22); err != nil {
				return err
			}
			row++
		}
		for _, item := range section.Items {
			if err := writeItem(item); err != nil {
				return err
			}
		}
		if section.BlankAfter {
			row++
		}
	}
	return nil
}

func setCell(f *excelize.File, sheet string, col, row int, value interface{}, style int) error {
	cell, err := excelize.CoordinatesToCellName(col, row)
	if err != nil {
		return err
	}
	if err := f.SetCellValue(sheet, cell, value); err != nil {
		return err
	}
	return f.SetCellStyle(sheet, cell, cell, style)
}

func headerStyleWithColor(f *excelize.File, color string) (int, error) {
	return f.NewStyle(&excelize.Style{
		Font:      &excelize.Font{Bold: true, Color: "FFFFFF"},
		Fill:      excelize.Fill{Type: "pattern", Color: []string{color}, Pattern: 1},
		Alignment: &excelize.Alignment{Horizontal: "center", Vertical: "center", WrapText: true},
		Border: []excelize.Border{
			{Type: "left", Color: "B4C6E7", Style: 1},
			{Type: "right", Color: "B4C6E7", Style: 1},
			{Type: "top", Color: "B4C6E7", Style: 1},
			{Type: "bottom", Color: "B4C6E7", Style: 1},
		},
	})
}

// writeLists adds the read-only Lists sheet: one column per list, fixed
// values then formulas, and a workbook name per list that covers only its
// non-empty cells (so dropdowns show no trailing blanks).
func writeLists(f *excelize.File, styles excelStyles, cfg WorkbookConfig) error {
	sheet := cfg.ListsSheet
	if sheet == "" {
		sheet = "Lists"
	}
	if _, err := f.NewSheet(sheet); err != nil {
		return fmt.Errorf("creating sheet %q: %w", sheet, err)
	}
	for i, list := range cfg.Lists {
		col, err := excelize.ColumnNumberToName(i + 1)
		if err != nil {
			return err
		}
		if err := setCell(f, sheet, i+1, 1, list.Title.For(cfg.Language), styles.header); err != nil {
			return err
		}
		if err := f.SetColWidth(sheet, col, col, 34); err != nil {
			return err
		}
		row := 2
		for _, v := range list.Values {
			if err := f.SetCellStr(sheet, fmt.Sprintf("%s%d", col, row), v); err != nil {
				return err
			}
			row++
		}
		for _, formula := range list.Formulas {
			if err := f.SetCellFormula(sheet, fmt.Sprintf("%s%d", col, row), formula); err != nil {
				return err
			}
			row++
		}
		last := row - 1
		if last < 2 {
			last = 2
		}
		// COUNTIF "?*" counts text of at least one character, so formula
		// results of "" are left out.
		ref := fmt.Sprintf("OFFSET('%[1]s'!$%[2]s$2,0,0,MAX(1,COUNTIF('%[1]s'!$%[2]s$2:$%[2]s$%[3]d,\"?*\")),1)", sheet, col, last)
		if list.Lookup {
			ref = fmt.Sprintf("'%[1]s'!$%[2]s$2:$%[2]s$%[3]d", sheet, col, last)
		}
		if err := f.SetDefinedName(&excelize.DefinedName{Name: list.Name, RefersTo: ref}); err != nil {
			return fmt.Errorf("defining list %q: %w", list.Name, err)
		}
	}
	if err := f.SetPanes(sheet, &excelize.Panes{Freeze: true, YSplit: 1, TopLeftCell: "A2", ActivePane: "bottomLeft"}); err != nil {
		return err
	}
	if err := f.ProtectSheet(sheet, &excelize.SheetProtectionOptions{SelectLockedCells: true, SelectUnlockedCells: true}); err != nil {
		return err
	}
	// Formulas are written without cached results; have Excel compute them
	// when the file is opened.
	full := true
	return f.SetCalcProps(&excelize.CalcPropsOptions{FullCalcOnLoad: &full})
}
