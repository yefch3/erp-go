package app

import (
	"encoding/json"
	"fmt"
	"regexp"
	"slices"
	"sort"
	"strconv"
	"strings"
	"unicode"

	"github.com/sgao19/erp-go/pkg/xlsx"
)

// SpreadsheetSourceRow is a locally identified inquiry line and its source cells.
type SpreadsheetSourceRow struct {
	SourceRef        string            `json:"source_ref"`
	Cells            map[string]string `json:"cells"`
	QuantityUnitHint string            `json:"quantity_unit_hint,omitempty"`
}

type SpreadsheetSourceDocument struct {
	Rows          []SpreadsheetSourceRow
	ContextBlocks []SpreadsheetContextBlock
	Sheets        []SpreadsheetSheetInfo
	ModelText     string
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
	sheets, err := xlsx.ReadWorkbook(data)
	if err != nil {
		return SpreadsheetSourceDocument{}, err
	}
	var document SpreadsheetSourceDocument
	var contextSheets []xlsx.Worksheet
	for _, sheet := range sheets {
		for _, row := range sortedSpreadsheetRows(sheet.Rows) {
			if columns := spreadsheetConflictingQuantityColumns(sheet.Rows[row]); len(columns) > 1 {
				return SpreadsheetSourceDocument{}, &spreadsheetQuantityConflict{sheet.Name, row, columns}
			}
		}
		detailRows := spreadsheetDetailRows(sheet.Name, sheet.Rows)
		if len(detailRows) > 0 {
			document.Rows = append(document.Rows, detailRows...)
			document.Sheets = append(document.Sheets, SpreadsheetSheetInfo{Name: sheet.Name, Role: "detail", DetailRows: len(detailRows)})
			continue
		}
		contextSheets = append(contextSheets, sheet)
	}
	if len(document.Rows) == 0 {
		// Give the model original row addresses and raw cells for small unknown
		// layouts. For a larger workbook, keep the existing file input path.
		document.ModelText = unclassifiedSpreadsheetText(sheets)
		return document, nil
	}
	for _, sheet := range contextSheets {
		blocks, err := spreadsheetContextBlocks(sheet.Name, sheet.Rows)
		if err != nil {
			return SpreadsheetSourceDocument{}, err
		}
		document.ContextBlocks = append(document.ContextBlocks, blocks...)
		document.Sheets = append(document.Sheets, SpreadsheetSheetInfo{Name: sheet.Name, Role: "context", ContextBlocks: len(blocks)})
	}
	return document, nil
}

func unclassifiedSpreadsheetText(sheets []xlsx.Worksheet) string {
	const maxTextBytes = 48_000
	var blocks []SpreadsheetContextBlock
	remaining := maxTextBytes
	for _, sheet := range sheets {
		if len(sheet.Rows) > 200 {
			return ""
		}
		lines := spreadsheetContextLines(sheet.Name, sheet.Rows)
		remaining -= spreadsheetContextSize(lines)
		if remaining < 0 {
			return ""
		}
		blocks = append(blocks, newContextBlock(sheet.Name, "unclassified", lines, nil, false))
	}
	encoded, err := json.Marshal(blocks)
	if err != nil || len(encoded) > maxTextBytes {
		return ""
	}
	return string(encoded)
}

type spreadsheetQuantityConflict struct {
	sheet   string
	row     int
	columns []string
}

type spreadsheetContextLimit struct{ sheet string }

func (e *spreadsheetContextLimit) Error() string {
	return fmt.Sprintf("工作表 %s 的内容较多且未识别出明细表头或批次分组，请明确表头或拆分工作表后重试", e.sheet)
}

var spreadsheetProductHeaders = []string{
	"product", "productdescription", "description", "producto", "item", "steel", "产品", "产品名称", "品名",
}

var spreadsheetQuantityHeaders = []string{
	"quantitymt", "qtymt", "数量mt", "requestedquantity", "requestedqty", "orderquantity",
	"quantity", "qty", "cantidad", "数量",
}

var spreadsheetUnitHeaders = []string{"quantityunit", "unit", "uom", "unidad", "单位"}

func (e *spreadsheetQuantityConflict) Error() string {
	return fmt.Sprintf("工作表 %s 第 %d 行有多个可能的需求数量列（%s），请确认并将需求数量列标为 INQ Q’ty 后重试", e.sheet, e.row, strings.Join(e.columns, "、"))
}

// The explicitly marked inquiry quantity wins over generic order quantities.
// Otherwise, equal candidates are ambiguous and must not be selected by map order.
func spreadsheetConflictingQuantityColumns(headers map[int]string) []string {
	var explicit, required, generic []int
	hasProduct := false
	for col, header := range headers {
		name := normalizeHeader(header)
		switch {
		case name == "inqqty":
			explicit = append(explicit, col)
		case name == "tonsmtrequeriment" || name == "tonsmtrequirement":
			required = append(required, col)
		case name == "mt" || name == "tons" || name == "ton" || slices.Contains(spreadsheetQuantityHeaders, name):
			generic = append(generic, col)
		case name == "sku" || name == "skucaasa" || name == "itemcode" || name == "productcode" || slices.Contains(spreadsheetProductHeaders, name):
			hasProduct = true
		}
	}
	columns := explicit
	if len(columns) == 0 && hasProduct {
		columns = required
		if len(columns) == 0 {
			columns = generic
		}
	}
	if len(columns) < 2 {
		return nil
	}
	sort.Ints(columns)
	labels := make([]string, 0, len(columns))
	for _, col := range columns {
		labels = append(labels, xlsxColumnName(col))
	}
	return labels
}

func spreadsheetDetailRows(sheetName string, rows map[int]map[int]string) []SpreadsheetSourceRow {
	rowNumbers := sortedSpreadsheetRows(rows)
	sheetSharedValues := spreadsheetSheetSharedValues(rows)
	var result []SpreadsheetSourceRow
	for _, headerRow := range rowNumbers {
		layout, ok := spreadsheetDetailLayout(rows, headerRow)
		if !ok {
			continue
		}
		blankRows := 0
		for detailRow := headerRow + 1; blankRows < 3; detailRow++ {
			values, ok := rows[detailRow]
			if !ok || len(values) == 0 {
				blankRows++
				continue
			}
			blankRows = 0
			if _, nextHeader := spreadsheetDetailLayout(rows, detailRow); nextHeader {
				break
			}
			if strings.TrimSpace(values[layout.qtyCol]) == "" || spreadsheetTotalRow(values, layout.qtyCol) {
				continue
			}
			hasIdentity := false
			if layout.itemCol > 0 {
				hasIdentity = strings.TrimSpace(values[layout.itemCol]) != ""
			} else {
				for col := 1; col < layout.qtyCol; col++ {
					if strings.TrimSpace(values[col]) != "" {
						hasIdentity = true
						break
					}
				}
			}
			if !hasIdentity {
				continue
			}
			record := SpreadsheetSourceRow{SourceRef: sheetName + "!" + strconv.Itoa(detailRow), Cells: map[string]string{}, QuantityUnitHint: layout.quantityUnitHint}
			for col, header := range layout.headers {
				header = strings.TrimSpace(header)
				if value := strings.TrimSpace(values[col]); header != "" && value != "" {
					record.Cells[header] = value
				}
			}
			for header, value := range layout.sharedValues {
				record.Cells[header] = value
			}
			for header, value := range sheetSharedValues {
				record.Cells[header] = value
			}
			result = append(result, record)
		}
	}
	return result
}

func spreadsheetSheetSharedValues(rows map[int]map[int]string) map[string]string {
	var packing []string
	insidePacking := false
	for _, rowNo := range sortedSpreadsheetRows(rows) {
		values := rows[rowNo]
		if !insidePacking {
			for _, value := range values {
				if normalizeHeader(value) == "packingconditions" {
					insidePacking = true
					break
				}
			}
			if !insidePacking {
				continue
			}
		}
		cols := make([]int, 0, len(values))
		for col := range values {
			cols = append(cols, col)
		}
		sort.Ints(cols)
		for _, col := range cols {
			value := strings.TrimSpace(values[col])
			if value != "" && normalizeHeader(value) != "packingconditions" {
				packing = append(packing, value)
			}
		}
	}
	if len(packing) == 0 {
		return nil
	}
	return map[string]string{"PACKING CONDITIONS": strings.Join(packing, "\n")}
}

type spreadsheetTableLayout struct {
	headers          map[int]string
	qtyCol           int
	itemCol          int
	quantityUnitHint string
	sharedValues     map[string]string
}

// spreadsheetDetailLayout keeps the original INQ Q'ty contract and also
// recognizes mill RFQs whose repeated table header is SKU + T/W/L + MT. The
// latter is deliberately a strong signature: treating a lone Qty or MT cell
// as a detail table would turn totals and note blocks into products.
func spreadsheetDetailLayout(rows map[int]map[int]string, headerRow int) (spreadsheetTableLayout, bool) {
	headers := rows[headerRow]
	for col, value := range headers {
		if normalizeHeader(value) == "inqqty" {
			return spreadsheetTableLayout{
				headers: headers, qtyCol: col,
				quantityUnitHint: spreadsheetQuantityUnitHint(headers),
			}, true
		}
	}

	cols := map[string]int{}
	for col, value := range headers {
		cols[normalizeHeader(value)] = col
	}
	// Identify the requested-tonnage column by its explicit header and the
	// surrounding product dimensions. "Sheets per MT" is a conversion factor.
	itemCol := firstSpreadsheetColumn(cols, "item", "product", "description")
	actualThicknessCol := firstSpreadsheetColumn(cols, "invocingthicknessmmreal", "invoicingthicknessmmreal", "actualthicknessmm")
	requestedTonsCol := firstSpreadsheetColumn(cols, "tonsmtrequeriment", "tonsmtrequirement")
	if itemCol > 0 && actualThicknessCol > 0 && requestedTonsCol > 0 && cols["widthmm"] > 0 && cols["lengthmm"] > 0 {
		canonical := make(map[int]string, len(headers))
		for col, header := range headers {
			canonical[col] = header
		}
		canonical[itemCol] = "Product"
		canonical[actualThicknessCol] = "Thickness [mm]"
		canonical[requestedTonsCol] = "Quantity"
		return spreadsheetTableLayout{headers: canonical, qtyCol: requestedTonsCol, itemCol: itemCol, quantityUnitHint: "MT"}, true
	}
	skuCol := firstSpreadsheetColumn(cols, "skucaasa", "sku", "itemcode", "productcode")
	thicknessCol := firstSpreadsheetColumn(cols, "tmm", "thicknessmm")
	widthCol := firstSpreadsheetColumn(cols, "wmm", "widthmm")
	lengthCol := firstSpreadsheetColumn(cols, "lmm", "lengthmm")
	qtyCol := firstSpreadsheetColumn(cols, "mt", "ton", "tons")
	if skuCol == 0 || thicknessCol == 0 || widthCol == 0 || lengthCol == 0 || qtyCol == 0 {
		return spreadsheetGenericDetailLayout(headers, cols)
	}

	canonical := make(map[int]string, len(headers))
	for col, value := range headers {
		canonical[col] = value
	}
	canonical[thicknessCol] = "Thickness [mm]"
	canonical[widthCol] = "Width [mm]"
	canonical[lengthCol] = "Length [mm]"
	canonical[qtyCol] = "Quantity"

	shared := map[string]string{}
	for col := skuCol + 1; col < thicknessCol; col++ {
		if value := strings.TrimSpace(headers[col]); value != "" {
			canonical[col] = "DESCRIPTION"
			shared["SECTION REQUIREMENTS"] = value
			break
		}
	}
	if preceding := rows[headerRow-1]; len(preceding) > 0 {
		var terms []string
		for col, value := range preceding {
			value = strings.TrimSpace(value)
			if col != qtyCol && value != "" {
				terms = append(terms, value)
			}
		}
		if len(terms) > 0 {
			sort.Strings(terms)
			shared["COMMERCIAL TERMS"] = strings.Join(terms, " | ")
		}
	}
	return spreadsheetTableLayout{
		headers: canonical, qtyCol: qtyCol, quantityUnitHint: "MT", sharedValues: shared,
	}, true
}

func spreadsheetGenericDetailLayout(headers map[int]string, cols map[string]int) (spreadsheetTableLayout, bool) {
	item := firstSpreadsheetColumn(cols, spreadsheetProductHeaders...)
	quantity := firstSpreadsheetColumn(cols, spreadsheetQuantityHeaders...)
	unit := firstSpreadsheetColumn(cols, spreadsheetUnitHeaders...)
	hasDimensions := cols["widthmm"] > 0 && cols["lengthmm"] > 0
	hint := ""
	if quantity > 0 {
		switch normalizeHeader(headers[quantity]) {
		case "quantitymt", "qtymt", "数量mt":
			hint = "MT"
		}
	}
	if item == 0 || quantity == 0 || (unit == 0 && hint == "" && !hasDimensions) {
		return spreadsheetTableLayout{}, false
	}
	canonical := make(map[int]string, len(headers))
	for col, name := range headers {
		canonical[col] = name
	}
	canonical[item], canonical[quantity] = "Product", "Quantity"
	if unit > 0 {
		canonical[unit] = "Quantity Unit"
	}
	return spreadsheetTableLayout{headers: canonical, qtyCol: quantity, itemCol: item, quantityUnitHint: hint}, true
}

func firstSpreadsheetColumn(columns map[string]int, names ...string) int {
	for _, name := range names {
		if column := columns[name]; column > 0 {
			return column
		}
	}
	return 0
}

func spreadsheetTotalRow(values map[int]string, qtyCol int) bool {
	for col := 1; col < qtyCol; col++ {
		value := normalizeHeader(values[col])
		if value == "total" || value == "subtotal" || value == "totaltons" || value == "subtotaltons" {
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
		return nil, &spreadsheetContextLimit{sheet: sheetName}
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

func normalizeHeader(value string) string {
	return strings.Map(func(r rune) rune {
		if unicode.IsLetter(r) || unicode.IsNumber(r) {
			return unicode.ToLower(r)
		}
		return -1
	}, value)
}
