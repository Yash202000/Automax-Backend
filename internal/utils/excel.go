package utils

import (
	"fmt"
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
}

type SheetConfig struct {
	Name             string
	Columns          []ColumnConfig
	NumberedSections []NumberedSectionConfig
	DataRows         int // number of data rows; validations cover rows 2 through DataRows+1
	FreezeHeader     bool
	AutoFilter       bool
}

type ColumnConfig struct {
	Key      string
	Name     LocalizedText
	Width    float64
	Dropdown *DropdownConfig
}

type DropdownConfig struct {
	Values      []string
	ValuesAR    []string
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
	for _, sheet := range cfg.Sheets {
		if strings.TrimSpace(sheet.Name) == "" {
			return nil, fmt.Errorf("sheet name cannot be empty")
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
		if err := f.SetCellValue(cfg.Name, cell, col.Name.For(lang)); err != nil {
			return err
		}
		if err := f.SetCellStyle(cfg.Name, cell, cell, styles.header); err != nil {
			return err
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
		if col.Dropdown == nil {
			continue
		}
		values := col.Dropdown.Values
		if lang == LanguageArabic && len(col.Dropdown.ValuesAR) > 0 {
			values = col.Dropdown.ValuesAR
		}
		if len(values) == 0 {
			continue
		}
		name, err := excelize.ColumnNumberToName(i + 1)
		if err != nil {
			return err
		}
		dv := excelize.NewDataValidation(true)
		dv.Sqref = fmt.Sprintf("%s2:%s%d", name, name, rows+1)
		if err := dv.SetDropList(values); err != nil {
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
		dv.SetError(excelize.DataValidationErrorStyleStop, errorTitle, errorText)
		if err := f.AddDataValidation(cfg.Name, dv); err != nil {
			return err
		}
	}
	return nil
}

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
