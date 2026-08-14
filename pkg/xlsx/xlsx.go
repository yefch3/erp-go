// Package xlsx implements the deliberately small spreadsheet surface used by
// fixed ERP interchange templates. It is not a general Excel engine: it
// writes one sheet and reads the first sheet, which keeps supplier-controlled
// workbooks bounded and auditable without a heavyweight parser.
package xlsx

import (
	"archive/zip"
	"bytes"
	"encoding/xml"
	"errors"
	"fmt"
	"io"
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
	return BuildWithFormulas(sheetName, rows, nil)
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
	if len(rows) == 0 || len(rows) > MaxRows {
		return nil, fmt.Errorf("xlsx rows out of range: %d", len(rows))
	}
	for _, row := range rows {
		if len(row) > MaxColumns {
			return nil, fmt.Errorf("xlsx columns out of range: %d", len(row))
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
		"xl/styles.xml":              `<?xml version="1.0" encoding="UTF-8" standalone="yes"?><styleSheet xmlns="http://schemas.openxmlformats.org/spreadsheetml/2006/main"><fonts count="2"><font><sz val="11"/><name val="Calibri"/></font><font><b/><sz val="11"/><name val="Calibri"/></font></fonts><fills count="2"><fill><patternFill patternType="none"/></fill><fill><patternFill patternType="gray125"/></fill></fills><borders count="1"><border/></borders><cellStyleXfs count="1"><xf numFmtId="0" fontId="0" fillId="0" borderId="0"/></cellStyleXfs><cellXfs count="2"><xf numFmtId="0" fontId="0" fillId="0" borderId="0" xfId="0"/><xf numFmtId="0" fontId="1" fillId="0" borderId="0" xfId="0" applyFont="1"/></cellXfs></styleSheet>`,
	}
	var workbook bytes.Buffer
	workbook.WriteString(`<?xml version="1.0" encoding="UTF-8" standalone="yes"?><workbook xmlns="http://schemas.openxmlformats.org/spreadsheetml/2006/main" xmlns:r="http://schemas.openxmlformats.org/officeDocument/2006/relationships"><sheets><sheet name="`)
	escape(&workbook, sheetName)
	workbook.WriteString(`" sheetId="1" r:id="rId1"/></sheets></workbook>`)
	parts["xl/workbook.xml"] = workbook.String()

	var sheet bytes.Buffer
	sheet.WriteString(`<?xml version="1.0" encoding="UTF-8" standalone="yes"?><worksheet xmlns="http://schemas.openxmlformats.org/spreadsheetml/2006/main"><sheetViews><sheetView workbookViewId="0"><pane ySplit="1" topLeftCell="A2" activePane="bottomLeft" state="frozen"/></sheetView></sheetViews><cols>`)
	for column, width := range columnWidths(rows) {
		sheet.WriteString(`<col min="` + strconv.Itoa(column+1) + `" max="` + strconv.Itoa(column+1) + `" width="` + strconv.Itoa(width) + `" customWidth="1"/>`)
	}
	sheet.WriteString(`</cols><sheetData>`)
	for ri, row := range rows {
		sheet.WriteString(`<row r="` + strconv.Itoa(ri+1) + `">`)
		for ci, value := range row {
			ref := cellRef(ci, ri+1)
			if formula, ok := formulas[ref]; ok {
				if strings.TrimSpace(formula.Expression) == "" {
					return nil, fmt.Errorf("xlsx formula is empty at %s", ref)
				}
				sheet.WriteString(`<c r="` + ref + `"><f>`)
				escape(&sheet, formula.Expression)
				sheet.WriteString(`</f><v>`)
				escape(&sheet, formula.CachedValue)
				sheet.WriteString(`</v></c>`)
				continue
			}
			if value == "" {
				continue
			}
			sheet.WriteString(`<c r="` + ref + `" t="inlineStr"`)
			if ri == 0 {
				sheet.WriteString(` s="1"`)
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
			value := cell.Value
			switch cell.Type {
			case "inlineStr":
				value = cell.Inline.join()
			case "s":
				index, convErr := strconv.Atoi(strings.TrimSpace(cell.Value))
				if convErr != nil || index < 0 || index >= len(shared) {
					return nil, errors.New("xlsx shared string index is invalid")
				}
				value = shared[index]
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
	Cells []sheetCell `xml:"c"`
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
	if file.UncompressedSize64 > maxPartBytes {
		return nil, errors.New("xlsx part is too large")
	}
	r, err := file.Open()
	if err != nil {
		return nil, err
	}
	defer r.Close()
	data, err := io.ReadAll(io.LimitReader(r, maxPartBytes+1))
	if err != nil || len(data) > maxPartBytes {
		return nil, errors.New("xlsx part could not be read")
	}
	return data, nil
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
