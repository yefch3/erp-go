// Package xlsx implements the deliberately small spreadsheet surface used by
// ERP interchange and mail attachments. It writes one sheet and reads cached
// cell values from bounded workbooks without evaluating formulas.
package xlsx

import (
	"archive/zip"
	"bytes"
	"encoding/xml"
	"errors"
	"fmt"
	"io"
	"math"
	"strconv"
	"strings"
)

const (
	MaxFileBytes = 2 << 20
	MaxRows      = 1000
	MaxColumns   = 64
	maxPartBytes = 4 << 20
)

// Build creates a single-sheet XLSX using inline strings. The resulting file
// opens in Excel, LibreOffice and Numbers and deliberately contains no macros,
// external links or formulas.
func Build(sheetName string, rows [][]string) ([]byte, error) {
	return build(sheetName, rows, nil, nil, nil)
}

// Formula is a trusted, server-authored spreadsheet formula and its cached
// numeric result. Callers must never pass formulas supplied by an uploaded
// workbook or other untrusted input.
type Formula struct {
	Expression  string
	CachedValue string
}

// BuildWithFormulas creates the same bounded workbook as Build and replaces
// explicitly addressed cells with trusted formulas. Formula references use
// Excel notation such as H5.
func BuildWithFormulas(sheetName string, rows [][]string, formulas map[string]Formula) ([]byte, error) {
	return build(sheetName, rows, formulas, nil, nil)
}

// BuildWithFormulasAndNumbers also writes explicitly addressed cells as real
// spreadsheet numbers. Their display format keeps useful decimal places while
// suppressing trailing zeroes. Callers must only mark trusted numeric fields.
func BuildWithFormulasAndNumbers(sheetName string, rows [][]string, formulas map[string]Formula, numbers map[string]string) ([]byte, error) {
	return build(sheetName, rows, formulas, numbers, nil)
}

// BuildWrapped creates a worksheet with explicit column widths, wrapped data
// text and row heights sized for the wrapped contents. Numbers are written as
// numeric cells, while all other values remain literal text.
func BuildWrapped(sheetName string, rows [][]string, numbers map[string]string, widths []int) ([]byte, error) {
	return build(sheetName, rows, nil, numbers, widths)
}

func build(sheetName string, rows [][]string, formulas map[string]Formula, numbers map[string]string, widths []int) ([]byte, error) {
	if len(rows) == 0 || len(rows) > MaxRows {
		return nil, fmt.Errorf("xlsx rows out of range: %d", len(rows))
	}
	for _, row := range rows {
		if len(row) > MaxColumns {
			return nil, fmt.Errorf("xlsx columns out of range: %d", len(row))
		}
		if widths != nil && len(row) != len(widths) {
			return nil, errors.New("xlsx column widths do not match row")
		}
	}
	for _, width := range widths {
		if width < 1 || width > 255 {
			return nil, fmt.Errorf("xlsx column width out of range: %d", width)
		}
	}
	if strings.TrimSpace(sheetName) == "" {
		sheetName = "Sheet1"
	}
	if len([]rune(sheetName)) > 31 {
		sheetName = string([]rune(sheetName)[:31])
	}

	var out bytes.Buffer
	zw := zip.NewWriter(&out)
	parts := map[string]string{
		"[Content_Types].xml":        `<?xml version="1.0" encoding="UTF-8" standalone="yes"?><Types xmlns="http://schemas.openxmlformats.org/package/2006/content-types"><Default Extension="rels" ContentType="application/vnd.openxmlformats-package.relationships+xml"/><Default Extension="xml" ContentType="application/xml"/><Override PartName="/xl/workbook.xml" ContentType="application/vnd.openxmlformats-officedocument.spreadsheetml.sheet.main+xml"/><Override PartName="/xl/worksheets/sheet1.xml" ContentType="application/vnd.openxmlformats-officedocument.spreadsheetml.worksheet+xml"/><Override PartName="/xl/styles.xml" ContentType="application/vnd.openxmlformats-officedocument.spreadsheetml.styles+xml"/></Types>`,
		"_rels/.rels":                `<?xml version="1.0" encoding="UTF-8" standalone="yes"?><Relationships xmlns="http://schemas.openxmlformats.org/package/2006/relationships"><Relationship Id="rId1" Type="http://schemas.openxmlformats.org/officeDocument/2006/relationships/officeDocument" Target="xl/workbook.xml"/></Relationships>`,
		"xl/_rels/workbook.xml.rels": `<?xml version="1.0" encoding="UTF-8" standalone="yes"?><Relationships xmlns="http://schemas.openxmlformats.org/package/2006/relationships"><Relationship Id="rId1" Type="http://schemas.openxmlformats.org/officeDocument/2006/relationships/worksheet" Target="worksheets/sheet1.xml"/><Relationship Id="rId2" Type="http://schemas.openxmlformats.org/officeDocument/2006/relationships/styles" Target="styles.xml"/></Relationships>`,
		"xl/styles.xml":              `<?xml version="1.0" encoding="UTF-8" standalone="yes"?><styleSheet xmlns="http://schemas.openxmlformats.org/spreadsheetml/2006/main"><numFmts count="1"><numFmt numFmtId="164" formatCode="#,##0.####"/></numFmts><fonts count="2"><font><sz val="11"/><name val="Calibri"/></font><font><b/><sz val="11"/><name val="Calibri"/></font></fonts><fills count="2"><fill><patternFill patternType="none"/></fill><fill><patternFill patternType="gray125"/></fill></fills><borders count="1"><border/></borders><cellStyleXfs count="1"><xf numFmtId="0" fontId="0" fillId="0" borderId="0"/></cellStyleXfs><cellXfs count="3"><xf numFmtId="0" fontId="0" fillId="0" borderId="0" xfId="0"/><xf numFmtId="0" fontId="1" fillId="0" borderId="0" xfId="0" applyFont="1"/><xf numFmtId="164" fontId="0" fillId="0" borderId="0" xfId="0" applyNumberFormat="1"/></cellXfs></styleSheet>`,
	}
	if widths != nil {
		styles := strings.Replace(parts["xl/styles.xml"], `<cellXfs count="3">`, `<cellXfs count="6">`, 1)
		wrappedStyles := `<xf numFmtId="0" fontId="0" fillId="0" borderId="0" xfId="0" applyAlignment="1"><alignment vertical="top" wrapText="1"/></xf>` +
			`<xf numFmtId="164" fontId="0" fillId="0" borderId="0" xfId="0" applyNumberFormat="1" applyAlignment="1"><alignment vertical="top"/></xf>` +
			`<xf numFmtId="3" fontId="0" fillId="0" borderId="0" xfId="0" applyNumberFormat="1" applyAlignment="1"><alignment vertical="top"/></xf>`
		parts["xl/styles.xml"] = strings.Replace(styles, `</cellXfs>`, wrappedStyles+`</cellXfs>`, 1)
	}
	var workbook bytes.Buffer
	workbook.WriteString(`<?xml version="1.0" encoding="UTF-8" standalone="yes"?><workbook xmlns="http://schemas.openxmlformats.org/spreadsheetml/2006/main" xmlns:r="http://schemas.openxmlformats.org/officeDocument/2006/relationships"><sheets><sheet name="`)
	escape(&workbook, sheetName)
	workbook.WriteString(`" sheetId="1" r:id="rId1"/></sheets></workbook>`)
	parts["xl/workbook.xml"] = workbook.String()

	var sheet bytes.Buffer
	sheet.WriteString(`<?xml version="1.0" encoding="UTF-8" standalone="yes"?><worksheet xmlns="http://schemas.openxmlformats.org/spreadsheetml/2006/main"><sheetViews><sheetView workbookViewId="0"><pane ySplit="1" topLeftCell="A2" activePane="bottomLeft" state="frozen"/></sheetView></sheetViews><cols>`)
	columnSizes := widths
	if columnSizes == nil {
		columnSizes = columnWidths(rows)
	}
	for column, width := range columnSizes {
		sheet.WriteString(`<col min="` + strconv.Itoa(column+1) + `" max="` + strconv.Itoa(column+1) + `" width="` + strconv.Itoa(width) + `" customWidth="1"/>`)
	}
	sheet.WriteString(`</cols><sheetData>`)
	for ri, row := range rows {
		sheet.WriteString(`<row r="` + strconv.Itoa(ri+1) + `"`)
		if widths != nil && ri > 0 {
			sheet.WriteString(` ht="` + strconv.Itoa(wrappedRowHeight(row, widths)) + `" customHeight="1"`)
		}
		sheet.WriteString(`>`)
		for ci, value := range row {
			ref := cellRef(ci, ri+1)
			if formula, ok := formulas[ref]; ok {
				if strings.TrimSpace(formula.Expression) == "" {
					return nil, fmt.Errorf("xlsx formula is empty at %s", ref)
				}
				sheet.WriteString(`<c r="` + ref + `" s="2"><f>`)
				escape(&sheet, formula.Expression)
				sheet.WriteString(`</f><v>`)
				escape(&sheet, formula.CachedValue)
				sheet.WriteString(`</v></c>`)
				continue
			}
			if number, ok := numbers[ref]; ok {
				number = strings.TrimSpace(number)
				parsed, parseErr := strconv.ParseFloat(number, 64)
				if parseErr != nil || math.IsNaN(parsed) || math.IsInf(parsed, 0) {
					return nil, fmt.Errorf("xlsx number is invalid at %s", ref)
				}
				style := "2"
				if widths != nil {
					style = "4"
					if parsed == math.Trunc(parsed) {
						style = "5"
					}
				}
				sheet.WriteString(`<c r="` + ref + `" s="` + style + `"><v>`)
				escape(&sheet, number)
				sheet.WriteString(`</v></c>`)
				continue
			}
			if value == "" {
				continue
			}
			sheet.WriteString(`<c r="` + ref + `" t="inlineStr"`)
			if ri == 0 {
				sheet.WriteString(` s="1"`)
			} else if widths != nil {
				sheet.WriteString(` s="3"`)
			}
			sheet.WriteString(`><is><t xml:space="preserve">`)
			escape(&sheet, value)
			sheet.WriteString(`</t></is></c>`)
		}
		sheet.WriteString(`</row>`)
	}
	sheet.WriteString(`</sheetData></worksheet>`)
	parts["xl/worksheets/sheet1.xml"] = sheet.String()

	for _, name := range []string{"[Content_Types].xml", "_rels/.rels", "xl/workbook.xml", "xl/_rels/workbook.xml.rels", "xl/styles.xml", "xl/worksheets/sheet1.xml"} {
		w, err := zw.Create(name)
		if err != nil {
			return nil, err
		}
		if _, err := io.WriteString(w, parts[name]); err != nil {
			return nil, err
		}
	}
	if err := zw.Close(); err != nil {
		return nil, err
	}
	return out.Bytes(), nil
}

func wrappedRowHeight(row []string, widths []int) int {
	lineCount := 1
	for column, text := range row {
		// Allow for font differences and the cell's horizontal padding.
		lineWidth := max(1, widths[column]*9/10-2)
		lines := 0
		for _, segment := range strings.Split(text, "\n") {
			units := 0
			for _, char := range segment {
				units++
				if char > 255 {
					units++
				}
			}
			lines += max(1, (units+lineWidth-1)/lineWidth)
		}
		lineCount = max(lineCount, lines)
	}
	return min(409, lineCount*17+4)
}

func columnWidths(rows [][]string) []int {
	columns := 0
	for _, row := range rows {
		if len(row) > columns {
			columns = len(row)
		}
	}
	widths := make([]int, columns)
	for _, row := range rows {
		for column, value := range row {
			width := 0
			for _, r := range value {
				if r > 255 {
					width += 2
				} else {
					width++
				}
			}
			if width > widths[column] {
				widths[column] = width
			}
		}
	}
	for i, width := range widths {
		width += 2
		if width < 10 {
			width = 10
		}
		if width > 48 {
			width = 48
		}
		widths[i] = width
	}
	return widths
}

// Parse reads the first worksheet and returns a rectangular row slice. It
// accepts inline strings, shared strings and numeric cells produced by Excel.
func Parse(data []byte) ([][]string, error) {
	if len(data) == 0 || len(data) > MaxFileBytes {
		return nil, errors.New("xlsx file is empty or too large")
	}
	zr, err := zip.NewReader(bytes.NewReader(data), int64(len(data)))
	if err != nil {
		return nil, errors.New("invalid xlsx archive")
	}
	var sheetData, sharedData []byte
	for _, file := range zr.File {
		switch file.Name {
		case "xl/worksheets/sheet1.xml":
			sheetData, err = readPart(file)
		case "xl/sharedStrings.xml":
			sharedData, err = readPart(file)
		}
		if err != nil {
			return nil, err
		}
	}
	if len(sheetData) == 0 {
		return nil, errors.New("xlsx first worksheet is missing")
	}
	shared, err := parseSharedStrings(sharedData)
	if err != nil {
		return nil, err
	}
	var doc worksheet
	if err := xml.Unmarshal(sheetData, &doc); err != nil {
		return nil, errors.New("invalid xlsx worksheet")
	}
	if len(doc.Rows) > MaxRows {
		return nil, errors.New("xlsx has too many rows")
	}
	out := make([][]string, 0, len(doc.Rows))
	for _, row := range doc.Rows {
		values := make([]string, 0)
		for _, cell := range row.Cells {
			column, ok := columnIndex(cell.Ref)
			if !ok || column >= MaxColumns {
				return nil, errors.New("xlsx cell reference is invalid")
			}
			for len(values) <= column {
				values = append(values, "")
			}
			value, err := decodeCell(cell, shared)
			if err != nil {
				return nil, err
			}
			values[column] = strings.TrimSpace(value)
		}
		out = append(out, values)
	}
	return out, nil
}

type worksheet struct {
	Rows []sheetRow `xml:"sheetData>row"`
}

type sheetRow struct {
	Number int         `xml:"r,attr"`
	Cells  []sheetCell `xml:"c"`
}

type sheetCell struct {
	Ref    string     `xml:"r,attr"`
	Type   string     `xml:"t,attr"`
	Value  string     `xml:"v"`
	Inline richString `xml:"is"`
}

type sharedStrings struct {
	Items []richString `xml:"si"`
}

type richString struct {
	Text string    `xml:"t"`
	Runs []textRun `xml:"r"`
}

type textRun struct {
	Text string `xml:"t"`
}

func (s richString) join() string {
	if len(s.Runs) == 0 {
		return s.Text
	}
	var b strings.Builder
	for _, run := range s.Runs {
		b.WriteString(run.Text)
	}
	return b.String()
}

func parseSharedStrings(data []byte) ([]string, error) {
	if len(data) == 0 {
		return nil, nil
	}
	var doc sharedStrings
	if err := xml.Unmarshal(data, &doc); err != nil {
		return nil, errors.New("invalid xlsx shared strings")
	}
	out := make([]string, len(doc.Items))
	for i, item := range doc.Items {
		out[i] = item.join()
	}
	return out, nil
}

func readPart(file *zip.File) ([]byte, error) {
	return readBoundedPart(file, maxPartBytes)
}

func readBoundedPart(file *zip.File, limit int64) ([]byte, error) {
	if file.UncompressedSize64 > uint64(limit) {
		return nil, errors.New("xlsx part is too large")
	}
	r, err := file.Open()
	if err != nil {
		return nil, err
	}
	defer r.Close()
	data, err := io.ReadAll(io.LimitReader(r, limit+1))
	if err != nil || int64(len(data)) > limit {
		return nil, errors.New("xlsx part could not be read")
	}
	return data, nil
}

func decodeCell(cell sheetCell, shared []string) (string, error) {
	switch cell.Type {
	case "inlineStr":
		return cell.Inline.join(), nil
	case "s":
		ref := strings.TrimSpace(cell.Value)
		if ref == "" {
			return "", nil
		}
		index, err := strconv.Atoi(ref)
		if err != nil || index < 0 || index >= len(shared) {
			return "", fmt.Errorf("invalid shared string index %q", ref)
		}
		return shared[index], nil
	default:
		return cell.Value, nil
	}
}

func columnIndex(ref string) (int, bool) {
	column := 0
	letters := 0
	for _, r := range ref {
		if r >= 'A' && r <= 'Z' {
			column = column*26 + int(r-'A'+1)
			letters++
			continue
		}
		if r >= 'a' && r <= 'z' {
			column = column*26 + int(r-'a'+1)
			letters++
			continue
		}
		break
	}
	return column - 1, letters > 0 && column > 0
}

func cellRef(column, row int) string {
	column++
	var letters [8]byte
	i := len(letters)
	for column > 0 {
		column--
		i--
		letters[i] = byte('A' + column%26)
		column /= 26
	}
	return string(letters[i:]) + strconv.Itoa(row)
}

func escape(w io.Writer, value string) {
	_ = xml.EscapeText(w, []byte(value))
}
