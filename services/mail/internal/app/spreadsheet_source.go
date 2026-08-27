package app

import (
	"archive/zip"
	"bytes"
	"encoding/xml"
	"fmt"
	"io"
	"path"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"unicode"
)

// SpreadsheetSourceRefs finds requested detail rows before a spreadsheet is
// handed to the model. It deliberately recognizes only the explicit INQ Q'ty
// layout: unknown workbooks retain the existing model-only path rather than
// guessing at a table and rejecting a legitimate attachment.
type SpreadsheetSourceRow struct {
	SourceRef        string            `json:"source_ref"`
	Cells            map[string]string `json:"cells"`
	QuantityUnitHint string            `json:"quantity_unit_hint,omitempty"`
}

type SpreadsheetSourceDocument struct {
	Rows          []SpreadsheetSourceRow
	ContextBlocks []SpreadsheetContextBlock
	Sheets        []SpreadsheetSheetInfo
}

type SpreadsheetSheetInfo struct {
	Name          string `json:"name"`
	Role          string `json:"role"`
	DetailRows    int    `json:"detail_rows"`
	ContextBlocks int    `json:"context_blocks"`
}

type SpreadsheetContextBlock struct {
	SourceRef string                   `json:"source_ref"`
	Sheet     string                   `json:"sheet"`
	Scope     string                   `json:"scope"`
	Lines     []SpreadsheetContextLine `json:"lines"`
	matchKeys []string
	global    bool
}

type SpreadsheetContextLine struct {
	SourceRef string                   `json:"source_ref"`
	Cells     []SpreadsheetContextCell `json:"cells"`
}

type SpreadsheetContextCell struct {
	Column string `json:"column"`
	Value  string `json:"value"`
}

var spreadsheetLotMarker = regexp.MustCompile(`(?i)\blot\s+([A-Z]{1,5}\.[A-Z0-9]+(?:\s+PT)?)\b`)

func SpreadsheetSourceRefs(data []byte) ([]string, error) {
	rows, err := SpreadsheetSourceRows(data)
	if err != nil {
		return nil, err
	}
	refs := make([]string, len(rows))
	for i, row := range rows {
		refs[i] = row.SourceRef
	}
	return refs, nil
}

func SpreadsheetSourceRows(data []byte) ([]SpreadsheetSourceRow, error) {
	document, err := ParseSpreadsheetSource(data)
	if err != nil {
		return nil, err
	}
	return document.Rows, nil
}

// ParseSpreadsheetSource classifies every worksheet. Request tables become
// deterministic source rows; all other sheets become bounded, source-addressed
// context blocks. A large sheet that cannot be classified fails explicitly
// instead of being silently ignored.
func ParseSpreadsheetSource(data []byte) (SpreadsheetSourceDocument, error) {
	zr, err := zip.NewReader(bytes.NewReader(data), int64(len(data)))
	if err != nil {
		return SpreadsheetSourceDocument{}, fmt.Errorf("open xlsx: %w", err)
	}
	files := make(map[string]*zip.File, len(zr.File))
	for _, file := range zr.File {
		files[file.Name] = file
	}
	shared, err := xlsxSharedStrings(files["xl/sharedStrings.xml"])
	if err != nil {
		return SpreadsheetSourceDocument{}, err
	}
	sheets, err := xlsxSheets(files)
	if err != nil {
		return SpreadsheetSourceDocument{}, err
	}
	var document SpreadsheetSourceDocument
	for _, sheet := range sheets {
		file := files[sheet.path]
		if file == nil {
			return SpreadsheetSourceDocument{}, fmt.Errorf("xlsx sheet %q is missing", sheet.name)
		}
		rows, err := xlsxRows(file, shared)
		if err != nil {
			return SpreadsheetSourceDocument{}, fmt.Errorf("read xlsx sheet %q: %w", sheet.name, err)
		}
		detailRows := spreadsheetDetailRows(sheet.name, rows)
		if len(detailRows) > 0 {
			document.Rows = append(document.Rows, detailRows...)
			document.Sheets = append(document.Sheets, SpreadsheetSheetInfo{Name: sheet.name, Role: "detail", DetailRows: len(detailRows)})
			continue
		}
		blocks, err := spreadsheetContextBlocks(sheet.name, rows)
		if err != nil {
			return SpreadsheetSourceDocument{}, err
		}
		document.ContextBlocks = append(document.ContextBlocks, blocks...)
		document.Sheets = append(document.Sheets, SpreadsheetSheetInfo{Name: sheet.name, Role: "context", ContextBlocks: len(blocks)})
	}
	return document, nil
}

func spreadsheetDetailRows(sheetName string, rows map[int]map[int]string) []SpreadsheetSourceRow {
	rowNumbers := sortedSpreadsheetRows(rows)
	var result []SpreadsheetSourceRow
	for _, headerRow := range rowNumbers {
		headers := rows[headerRow]
		qtyCol := 0
		for col, value := range headers {
			if normalizeHeader(value) == "inqqty" {
				qtyCol = col
				break
			}
		}
		if qtyCol == 0 {
			continue
		}
		quantityUnitHint := spreadsheetQuantityUnitHint(headers)
		blankRows := 0
		for detailRow := headerRow + 1; blankRows < 3; detailRow++ {
			values, ok := rows[detailRow]
			if !ok || len(values) == 0 {
				blankRows++
				continue
			}
			blankRows = 0
			if strings.TrimSpace(values[qtyCol]) == "" || spreadsheetTotalRow(values, qtyCol) {
				continue
			}
			hasIdentity := false
			for col := 1; col < qtyCol; col++ {
				if strings.TrimSpace(values[col]) != "" {
					hasIdentity = true
					break
				}
			}
			if !hasIdentity {
				continue
			}
			record := SpreadsheetSourceRow{SourceRef: sheetName + "!" + strconv.Itoa(detailRow), Cells: map[string]string{}, QuantityUnitHint: quantityUnitHint}
			for col, header := range headers {
				header = strings.TrimSpace(header)
				if value := strings.TrimSpace(values[col]); header != "" && value != "" {
					record.Cells[header] = value
				}
			}
			result = append(result, record)
		}
	}
	return result
}

func spreadsheetTotalRow(values map[int]string, qtyCol int) bool {
	for col := 1; col < qtyCol; col++ {
		value := normalizeHeader(values[col])
		if value == "total" || value == "subtotal" {
			return true
		}
	}
	return false
}

func spreadsheetContextBlocks(sheetName string, rows map[int]map[int]string) ([]SpreadsheetContextBlock, error) {
	lines := spreadsheetContextLines(sheetName, rows)
	if len(lines) == 0 {
		return []SpreadsheetContextBlock{{SourceRef: sheetName, Sheet: sheetName, Scope: "empty", global: true}}, nil
	}

	var markers []struct {
		index int
		key   string
	}
	for i, line := range lines {
		if match := spreadsheetLotMarker.FindStringSubmatch(contextLineText(line)); match != nil {
			markers = append(markers, struct {
				index int
				key   string
			}{i, normalizeMatchKey(match[1])})
		}
	}
	if len(markers) > 0 {
		keys := make([]string, 0, len(markers))
		for _, marker := range markers {
			keys = append(keys, marker.key)
		}
		var blocks []SpreadsheetContextBlock
		if markers[0].index > 0 {
			blocks = append(blocks, newContextBlock(sheetName, "sheet_global", lines[:markers[0].index], keys, false))
		}
		for i, marker := range markers {
			end := len(lines)
			if i+1 < len(markers) {
				end = markers[i+1].index
			}
			blocks = append(blocks, newContextBlock(sheetName, "lot", lines[marker.index:end], []string{marker.key}, false))
		}
		return blocks, nil
	}

	if headerIndex, lotCol := spreadsheetLotTable(lines); headerIndex >= 0 {
		headers := lines[headerIndex]
		var blocks []SpreadsheetContextBlock
		lastKey := ""
		for _, line := range lines[headerIndex+1:] {
			value := contextCellValue(line, lotCol)
			if value != "" {
				lastKey = normalizeMatchKey(value)
			}
			if lastKey == "" {
				continue
			}
			blocks = append(blocks, newContextBlock(sheetName, "reference_row", []SpreadsheetContextLine{headers, line}, []string{lastKey}, false))
		}
		return blocks, nil
	}

	if len(lines) > 80 || spreadsheetContextSize(lines) > 12_000 {
		return nil, fmt.Errorf("xlsx context sheet %q is too large and has no LOT sections or LOT table", sheetName)
	}
	return []SpreadsheetContextBlock{newContextBlock(sheetName, "sheet_global", lines, nil, true)}, nil
}

func spreadsheetContextLines(sheetName string, rows map[int]map[int]string) []SpreadsheetContextLine {
	var lines []SpreadsheetContextLine
	for _, rowNo := range sortedSpreadsheetRows(rows) {
		var cells []SpreadsheetContextCell
		cols := make([]int, 0, len(rows[rowNo]))
		for col := range rows[rowNo] {
			cols = append(cols, col)
		}
		sort.Ints(cols)
		for _, col := range cols {
			if value := strings.TrimSpace(rows[rowNo][col]); value != "" {
				cells = append(cells, SpreadsheetContextCell{Column: xlsxColumnName(col), Value: value})
			}
		}
		if len(cells) > 0 {
			lines = append(lines, SpreadsheetContextLine{SourceRef: sheetName + "!" + strconv.Itoa(rowNo), Cells: cells})
		}
	}
	return lines
}

func spreadsheetLotTable(lines []SpreadsheetContextLine) (int, string) {
	for i, line := range lines {
		for _, cell := range line.Cells {
			header := normalizeHeader(cell.Value)
			if header == "lot" || header == "lote" {
				return i, cell.Column
			}
		}
	}
	return -1, ""
}

func newContextBlock(sheetName, scope string, lines []SpreadsheetContextLine, keys []string, global bool) SpreadsheetContextBlock {
	ref := sheetName
	if len(lines) > 0 {
		ref = lines[0].SourceRef
		if len(lines) > 1 {
			ref += "-" + strings.TrimPrefix(lines[len(lines)-1].SourceRef, sheetName+"!")
		}
	}
	return SpreadsheetContextBlock{SourceRef: ref, Sheet: sheetName, Scope: scope, Lines: lines, matchKeys: keys, global: global}
}

func RelevantSpreadsheetContext(blocks []SpreadsheetContextBlock, rows []SpreadsheetSourceRow) []SpreadsheetContextBlock {
	keys := map[string]bool{}
	for _, row := range rows {
		for header, value := range row.Cells {
			if normalized := normalizeHeader(header); normalized == "lot" || normalized == "lote" {
				keys[normalizeMatchKey(value)] = true
			}
		}
	}
	var result []SpreadsheetContextBlock
	for _, block := range blocks {
		matched := block.global
		for _, key := range block.matchKeys {
			if keys[key] {
				matched = true
				break
			}
		}
		if matched {
			result = append(result, block)
		}
	}
	return result
}

func sortedSpreadsheetRows(rows map[int]map[int]string) []int {
	result := make([]int, 0, len(rows))
	for rowNo := range rows {
		result = append(result, rowNo)
	}
	sort.Ints(result)
	return result
}

func contextLineText(line SpreadsheetContextLine) string {
	var values []string
	for _, cell := range line.Cells {
		values = append(values, cell.Value)
	}
	return strings.Join(values, " | ")
}

func contextCellValue(line SpreadsheetContextLine, column string) string {
	for _, cell := range line.Cells {
		if cell.Column == column {
			return strings.TrimSpace(cell.Value)
		}
	}
	return ""
}

func spreadsheetContextSize(lines []SpreadsheetContextLine) int {
	total := 0
	for _, line := range lines {
		total += len(contextLineText(line))
	}
	return total
}

func normalizeMatchKey(value string) string {
	return strings.ToUpper(strings.Join(strings.Fields(strings.TrimSpace(value)), " "))
}

func xlsxColumnName(column int) string {
	var result []byte
	for column > 0 {
		column--
		result = append([]byte{byte('A' + column%26)}, result...)
		column /= 26
	}
	return string(result)
}

func spreadsheetQuantityUnitHint(headers map[int]string) string {
	for _, header := range headers {
		normalized := normalizeHeader(header)
		if strings.Contains(normalized, "usdton") || strings.Contains(normalized, "usdmt") {
			return "MT"
		}
	}
	return ""
}

type xlsxSheet struct{ name, path string }

func xlsxSheets(files map[string]*zip.File) ([]xlsxSheet, error) {
	workbook := files["xl/workbook.xml"]
	rels := files["xl/_rels/workbook.xml.rels"]
	if workbook == nil || rels == nil {
		return nil, fmt.Errorf("xlsx workbook relationships are missing")
	}
	read := func(file *zip.File) ([]byte, error) {
		r, err := file.Open()
		if err != nil {
			return nil, err
		}
		defer r.Close()
		return io.ReadAll(r)
	}
	workbookData, err := read(workbook)
	if err != nil {
		return nil, err
	}
	relsData, err := read(rels)
	if err != nil {
		return nil, err
	}
	var book struct {
		Sheets []struct {
			Name string `xml:"name,attr"`
			ID   string `xml:"id,attr"`
		} `xml:"sheets>sheet"`
	}
	if err := xml.Unmarshal(workbookData, &book); err != nil {
		return nil, err
	}
	var relationships struct {
		Items []struct {
			ID     string `xml:"Id,attr"`
			Target string `xml:"Target,attr"`
		} `xml:"Relationship"`
	}
	if err := xml.Unmarshal(relsData, &relationships); err != nil {
		return nil, err
	}
	targets := make(map[string]string, len(relationships.Items))
	for _, rel := range relationships.Items {
		// OPC relationship targets may be relative to xl/workbook.xml or
		// package-absolute (for example /xl/worksheets/sheet1.xml). Both are
		// valid; joining an absolute package target under xl makes a false
		// xl/xl/... path and reports a present sheet as missing.
		if strings.HasPrefix(rel.Target, "/") {
			targets[rel.ID] = strings.TrimPrefix(path.Clean(rel.Target), "/")
		} else {
			targets[rel.ID] = path.Clean(path.Join("xl", rel.Target))
		}
	}
	result := make([]xlsxSheet, 0, len(book.Sheets))
	for _, sheet := range book.Sheets {
		target := targets[sheet.ID]
		if target == "" {
			return nil, fmt.Errorf("xlsx sheet %q has no relationship", sheet.Name)
		}
		result = append(result, xlsxSheet{name: sheet.Name, path: target})
	}
	return result, nil
}

func xlsxSharedStrings(file *zip.File) ([]string, error) {
	if file == nil {
		return nil, nil
	}
	r, err := file.Open()
	if err != nil {
		return nil, err
	}
	defer r.Close()
	decoder := xml.NewDecoder(r)
	var result []string
	inside, text := false, strings.Builder{}
	for {
		token, err := decoder.Token()
		if err == io.EOF {
			break
		}
		if err != nil {
			return nil, err
		}
		switch value := token.(type) {
		case xml.StartElement:
			if value.Name.Local == "si" {
				inside = true
				text.Reset()
			}
		case xml.CharData:
			if inside {
				text.Write([]byte(value))
			}
		case xml.EndElement:
			if value.Name.Local == "si" {
				result = append(result, text.String())
				inside = false
			}
		}
	}
	return result, nil
}

func xlsxRows(file *zip.File, shared []string) (map[int]map[int]string, error) {
	r, err := file.Open()
	if err != nil {
		return nil, err
	}
	defer r.Close()
	decoder := xml.NewDecoder(r)
	result := map[int]map[int]string{}
	for {
		token, err := decoder.Token()
		if err == io.EOF {
			break
		}
		if err != nil {
			return nil, err
		}
		start, ok := token.(xml.StartElement)
		if !ok || start.Name.Local != "row" {
			continue
		}
		rowNo := xlsxAttr(start, "r")
		n, _ := strconv.Atoi(rowNo)
		if n == 0 {
			continue
		}
		cells, err := xlsxRowCells(decoder, shared)
		if err != nil {
			return nil, err
		}
		result[n] = cells
	}
	return result, nil
}

func xlsxRowCells(decoder *xml.Decoder, shared []string) (map[int]string, error) {
	cells := map[int]string{}
	for {
		token, err := decoder.Token()
		if err != nil {
			return nil, err
		}
		end, ok := token.(xml.EndElement)
		if ok && end.Name.Local == "row" {
			return cells, nil
		}
		start, ok := token.(xml.StartElement)
		if !ok || start.Name.Local != "c" {
			continue
		}
		ref, kind := xlsxAttr(start, "r"), xlsxAttr(start, "t")
		col := xlsxColumn(ref)
		value, err := xlsxCellValue(decoder)
		if err != nil {
			return nil, err
		}
		if kind == "s" {
			i, err := strconv.Atoi(value)
			if err != nil || i < 0 || i >= len(shared) {
				return nil, fmt.Errorf("invalid shared string index %q", value)
			}
			value = shared[i]
		}
		if col > 0 {
			cells[col] = value
		}
	}
}

func xlsxCellValue(decoder *xml.Decoder) (string, error) {
	var value strings.Builder
	insideValue, insideInline := false, false
	for {
		token, err := decoder.Token()
		if err != nil {
			return "", err
		}
		switch item := token.(type) {
		case xml.StartElement:
			switch item.Name.Local {
			case "v":
				insideValue = true
			case "t":
				insideInline = true
			}
		case xml.CharData:
			if insideValue || insideInline {
				value.Write([]byte(item))
			}
		case xml.EndElement:
			switch item.Name.Local {
			case "v":
				insideValue = false
			case "t":
				insideInline = false
			case "c":
				return value.String(), nil
			}
		}
	}
}

func xlsxAttr(start xml.StartElement, name string) string {
	for _, attr := range start.Attr {
		if attr.Name.Local == name {
			return attr.Value
		}
	}
	return ""
}

func xlsxColumn(ref string) int {
	value := 0
	for _, r := range ref {
		if r < 'A' || r > 'Z' {
			break
		}
		value = value*26 + int(r-'A'+1)
	}
	return value
}

func normalizeHeader(value string) string {
	return strings.Map(func(r rune) rune {
		if unicode.IsLetter(r) || unicode.IsNumber(r) {
			return unicode.ToLower(r)
		}
		return -1
	}, value)
}
