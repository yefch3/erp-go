package app

import (
	"archive/zip"
	"bytes"
	"context"
	"encoding/xml"
	"errors"
	"fmt"
	"io"
	"math"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"unicode/utf8"

	"github.com/sgao19/erp-go/pkg/apierr"
	"github.com/sgao19/erp-go/services/mail/internal/store"
)

var plainDecimal = regexp.MustCompile(`^-?(?:0|[1-9][0-9]*)(?:\.[0-9]+)?$`)

const (
	maxExcelSelectedText = 200_000
	maxExcelSheets       = 20
	maxExcelColumns      = 80
	maxExcelRows         = 10_000
)

// TableExtractor is the one intentionally external step in conversion. It
// receives bytes only after the mail service has proved that the caller owns
// the message and that the attachment belongs to it.
type TableExtractor interface {
	Extract(ctx context.Context, in TableExtractionInput) (Workbook, string, error)
}

type TableExtractionInput struct {
	Text        string
	FileName    string
	ContentType string
	FileData    []byte
	Locale      string
	SafetyID    string
}

type Workbook struct {
	Title  string          `json:"title"`
	Sheets []WorkbookSheet `json:"sheets"`
}

type WorkbookSheet struct {
	Name        string     `json:"name"`
	Summary     string     `json:"summary"`
	Columns     []string   `json:"columns"`
	ColumnTypes []string   `json:"column_types"`
	Rows        [][]string `json:"rows"`
}

type ExcelResult struct {
	FileName string
	Data     []byte
	Workbook Workbook
	Model    string
}

// ConvertInboundToExcel accepts exactly one user-selected source. It does not
// accept object keys or arbitrary files from the browser: attachment ids are
// resolved against the already owner-scoped mail record.
func (s *Service) ConvertInboundToExcel(
	ctx context.Context, tenantID, ownerID, inboundID int64,
	attachmentID *int64, selectedText *string, locale string,
) (ExcelResult, error) {
	if s.tables == nil {
		return ExcelResult{}, apierr.Invalid("MAIL_EXCEL_NOT_CONFIGURED", "Excel 智能转换尚未配置")
	}
	if inboundID <= 0 {
		return ExcelResult{}, apierr.Invalid("MAIL_EXCEL_MAIL_REQUIRED", "缺少邮件标识")
	}
	if (attachmentID == nil) == (selectedText == nil) {
		return ExcelResult{}, apierr.Invalid("MAIL_EXCEL_SOURCE_REQUIRED", "请选择一段正文或一个附件")
	}

	row, err := s.q.GetInbound(ctx, store.GetInboundParams{TenantID: tenantID, ID: inboundID})
	if err != nil || row.OwnerID != ownerID {
		return ExcelResult{}, errNotFound()
	}

	in := TableExtractionInput{Locale: normalizeExcelLocale(locale)}
	// This is an opaque, stable identifier rather than an email address or a
	// name. The API uses it only for abuse monitoring.
	in.SafetyID = fmt.Sprintf("tenant-%d-user-%d", tenantID, ownerID)
	if selectedText != nil {
		text := strings.TrimSpace(*selectedText)
		if text == "" {
			return ExcelResult{}, apierr.Invalid("MAIL_EXCEL_TEXT_EMPTY", "选中的文字为空")
		}
		if len(text) > maxExcelSelectedText {
			return ExcelResult{}, apierr.Invalid("MAIL_EXCEL_TEXT_TOO_LARGE", "选中的文字过长，请缩小选择范围")
		}
		body := row.BodyText
		if strings.TrimSpace(body) == "" {
			body = HTMLToText(row.BodyHtml)
		}
		if !containsNormalizedText(body, text) {
			return ExcelResult{}, apierr.Invalid("MAIL_EXCEL_TEXT_NOT_IN_MAIL", "选中的文字不属于这封邮件")
		}
		in.Text = text
	} else {
		if s.files == nil {
			return ExcelResult{}, apierr.Invalid("NT_STORAGE_UNAVAILABLE", "文件存储未配置")
		}
		atts, err := s.q.ListInboundAttachments(ctx, store.ListInboundAttachmentsParams{
			TenantID: tenantID, InboundID: inboundID,
		})
		if err != nil {
			return ExcelResult{}, err
		}
		var chosen *store.ListInboundAttachmentsRow
		for i := range atts {
			if atts[i].ID == *attachmentID {
				chosen = &atts[i]
				break
			}
		}
		if chosen == nil || chosen.FileKey == "" {
			return ExcelResult{}, apierr.NotFound("MAIL_EXCEL_ATTACHMENT_NOT_FOUND", "附件不存在或未保存")
		}
		if !supportedTableSource(chosen.FileName, chosen.ContentType) {
			return ExcelResult{}, apierr.Invalid("MAIL_EXCEL_FILE_TYPE", "这个附件类型暂不支持转换为 Excel")
		}
		r, err := s.files.Get(ctx, chosen.FileKey)
		if err != nil {
			return ExcelResult{}, apierr.Internal("MAIL_EXCEL_ATTACHMENT_READ", "读取附件失败，请重试").Wrap(err)
		}
		defer r.Close()
		data, err := io.ReadAll(io.LimitReader(r, MaxAttachmentBytes+1))
		if err != nil {
			return ExcelResult{}, apierr.Internal("MAIL_EXCEL_ATTACHMENT_READ", "读取附件失败，请重试").Wrap(err)
		}
		if len(data) == 0 || len(data) > MaxAttachmentBytes {
			return ExcelResult{}, apierr.Invalid("MAIL_EXCEL_ATTACHMENT_SIZE", "附件为空或超过 10 MB")
		}
		in.FileName, in.ContentType, in.FileData = chosen.FileName, chosen.ContentType, data
	}

	book, model, err := s.tables.Extract(ctx, in)
	if err != nil {
		s.log.Error("table extraction failed", "mail", inboundID, "err", err)
		return ExcelResult{}, apierr.Internal("MAIL_EXCEL_MODEL_FAILED", "智能转换失败，请稍后重试").Wrap(err)
	}
	if err := validateWorkbook(&book); err != nil {
		s.log.Error("table extraction returned an invalid workbook", "mail", inboundID, "err", err)
		return ExcelResult{}, apierr.Internal("MAIL_EXCEL_INVALID_RESULT", "模型返回的表格格式无效，请重试").Wrap(err)
	}
	data, err := buildXLSX(book)
	if err != nil {
		return ExcelResult{}, apierr.Internal("MAIL_EXCEL_BUILD_FAILED", "生成 Excel 失败，请重试").Wrap(err)
	}
	name := safeExcelFileName(book.Title)
	if name == "" {
		name = "email-table"
	}
	return ExcelResult{FileName: name + ".xlsx", Data: data, Workbook: book, Model: model}, nil
}

func containsNormalizedText(body, selected string) bool {
	norm := func(s string) string { return strings.Join(strings.Fields(s), " ") }
	return strings.Contains(norm(body), norm(selected))
}

func normalizeExcelLocale(v string) string {
	switch strings.ToLower(strings.TrimSpace(v)) {
	case "zh", "zh-cn", "zh-hans":
		return "zh"
	case "es", "es-es":
		return "es"
	default:
		return "en"
	}
}

func supportedTableSource(name, contentType string) bool {
	ct := strings.ToLower(strings.TrimSpace(strings.Split(contentType, ";")[0]))
	if strings.HasPrefix(ct, "image/") || ct == "application/pdf" || strings.HasPrefix(ct, "text/") {
		return true
	}
	switch strings.ToLower(filepath.Ext(name)) {
	case ".pdf", ".txt", ".csv", ".tsv", ".md", ".rtf",
		".doc", ".docx", ".odt", ".ppt", ".pptx",
		".xls", ".xlsx", ".ods", ".png", ".jpg", ".jpeg",
		".gif", ".webp", ".bmp", ".tif", ".tiff":
		return true
	}
	return false
}

func validateWorkbook(book *Workbook) error {
	book.Title = strings.TrimSpace(book.Title)
	if len(book.Sheets) == 0 || len(book.Sheets) > maxExcelSheets {
		return errors.New("invalid sheet count")
	}
	seen := map[string]bool{}
	for i := range book.Sheets {
		s := &book.Sheets[i]
		s.Name = uniqueSheetName(cleanSheetName(s.Name), seen)
		if len(s.Columns) == 0 || len(s.Columns) > maxExcelColumns || len(s.Rows) > maxExcelRows {
			return fmt.Errorf("sheet %q has invalid dimensions", s.Name)
		}
		if len(s.ColumnTypes) != len(s.Columns) {
			return fmt.Errorf("sheet %q column types do not match columns", s.Name)
		}
		for j := range s.Columns {
			s.Columns[j] = strings.TrimSpace(s.Columns[j])
			if s.Columns[j] == "" {
				s.Columns[j] = fmt.Sprintf("Column %d", j+1)
			}
			switch s.ColumnTypes[j] {
			case "string", "number", "boolean", "date":
			default:
				return fmt.Errorf("sheet %q has invalid column type", s.Name)
			}
		}
		for j := range s.Rows {
			if len(s.Rows[j]) > len(s.Columns) {
				return fmt.Errorf("sheet %q row is wider than columns", s.Name)
			}
			for len(s.Rows[j]) < len(s.Columns) {
				s.Rows[j] = append(s.Rows[j], "")
			}
		}
	}
	return nil
}

func cleanSheetName(v string) string {
	v = strings.TrimSpace(v)
	v = strings.Map(func(r rune) rune {
		if strings.ContainsRune(`[]:*?/\\`, r) || r < 0x20 {
			return ' '
		}
		return r
	}, v)
	v = strings.TrimSpace(v)
	if v == "" {
		v = "Sheet"
	}
	for utf8.RuneCountInString(v) > 31 {
		_, n := utf8.DecodeLastRuneInString(v)
		v = v[:len(v)-n]
	}
	return v
}

func uniqueSheetName(base string, seen map[string]bool) string {
	name := base
	for n := 2; seen[strings.ToLower(name)]; n++ {
		suffix := fmt.Sprintf(" (%d)", n)
		trimmed := base
		for utf8.RuneCountInString(trimmed)+utf8.RuneCountInString(suffix) > 31 {
			_, size := utf8.DecodeLastRuneInString(trimmed)
			trimmed = trimmed[:len(trimmed)-size]
		}
		name = trimmed + suffix
	}
	seen[strings.ToLower(name)] = true
	return name
}

func safeExcelFileName(v string) string {
	v = strings.TrimSpace(v)
	v = strings.Map(func(r rune) rune {
		if r < 0x20 || strings.ContainsRune(`/\\:*?"<>|`, r) {
			return '-'
		}
		return r
	}, v)
	for utf8.RuneCountInString(v) > 80 {
		_, n := utf8.DecodeLastRuneInString(v)
		v = v[:len(v)-n]
	}
	return strings.Trim(v, " .-")
}

// buildXLSX emits the small, stable OOXML subset needed by a data workbook.
// Strings use inlineStr, avoiding a shared-string table and, importantly,
// ensuring a customer value beginning with '=' is data rather than a formula.
func buildXLSX(book Workbook) ([]byte, error) {
	var out bytes.Buffer
	z := zip.NewWriter(&out)
	write := func(name, body string) error {
		w, err := z.Create(name)
		if err != nil {
			return err
		}
		_, err = io.WriteString(w, body)
		return err
	}
	if err := write("[Content_Types].xml", contentTypesXML(len(book.Sheets))); err != nil {
		return nil, err
	}
	if err := write("_rels/.rels", rootRelsXML); err != nil {
		return nil, err
	}
	if err := write("xl/workbook.xml", workbookXML(book.Sheets)); err != nil {
		return nil, err
	}
	if err := write("xl/_rels/workbook.xml.rels", workbookRelsXML(len(book.Sheets))); err != nil {
		return nil, err
	}
	if err := write("xl/styles.xml", stylesXML); err != nil {
		return nil, err
	}
	for i, sheet := range book.Sheets {
		if err := write(fmt.Sprintf("xl/worksheets/sheet%d.xml", i+1), worksheetXML(sheet)); err != nil {
			return nil, err
		}
	}
	if err := z.Close(); err != nil {
		return nil, err
	}
	return out.Bytes(), nil
}

func xmlText(v string) string {
	var b bytes.Buffer
	_ = xml.EscapeText(&b, []byte(v))
	return b.String()
}

func workbookXML(sheets []WorkbookSheet) string {
	var b strings.Builder
	b.WriteString(`<?xml version="1.0" encoding="UTF-8" standalone="yes"?><workbook xmlns="http://schemas.openxmlformats.org/spreadsheetml/2006/main" xmlns:r="http://schemas.openxmlformats.org/officeDocument/2006/relationships"><sheets>`)
	for i, s := range sheets {
		fmt.Fprintf(&b, `<sheet name="%s" sheetId="%d" r:id="rId%d"/>`, xmlText(s.Name), i+1, i+1)
	}
	b.WriteString(`</sheets></workbook>`)
	return b.String()
}

func worksheetXML(s WorkbookSheet) string {
	var b strings.Builder
	b.WriteString(`<?xml version="1.0" encoding="UTF-8" standalone="yes"?><worksheet xmlns="http://schemas.openxmlformats.org/spreadsheetml/2006/main"><sheetViews><sheetView workbookViewId="0"><pane ySplit="1" topLeftCell="A2" activePane="bottomLeft" state="frozen"/></sheetView></sheetViews><cols>`)
	for i, column := range s.Columns {
		width := math.Min(48, math.Max(10, float64(utf8.RuneCountInString(column)+3)))
		for _, row := range s.Rows {
			if i < len(row) {
				width = math.Min(48, math.Max(width, float64(utf8.RuneCountInString(row[i])+2)))
			}
		}
		fmt.Fprintf(&b, `<col min="%d" max="%d" width="%.1f" customWidth="1"/>`, i+1, i+1, width)
	}
	b.WriteString(`</cols><sheetData>`)
	writeRow := func(row int, values []string, types []string, style int) {
		fmt.Fprintf(&b, `<row r="%d">`, row)
		for col, value := range values {
			ref := excelColumn(col+1) + strconv.Itoa(row)
			kind := "string"
			if col < len(types) {
				kind = types[col]
			}
			writeCell(&b, ref, value, kind, style)
		}
		b.WriteString(`</row>`)
	}
	writeRow(1, s.Columns, nil, 1)
	for i, row := range s.Rows {
		writeRow(i+2, row, s.ColumnTypes, 0)
	}
	b.WriteString(`</sheetData>`)
	if len(s.Rows) > 0 {
		fmt.Fprintf(&b, `<autoFilter ref="A1:%s%d"/>`, excelColumn(len(s.Columns)), len(s.Rows)+1)
	}
	b.WriteString(`</worksheet>`)
	return b.String()
}

func writeCell(b *strings.Builder, ref, value, kind string, style int) {
	styleAttr := ""
	if style > 0 {
		styleAttr = fmt.Sprintf(` s="%d"`, style)
	}
	switch kind {
	case "number":
		if n, ok := safeExcelDecimal(value); ok {
			fmt.Fprintf(b, `<c r="%s"%s><v>%s</v></c>`, ref, styleAttr, n)
			return
		}
		// A number-looking value that is unsafe as an Excel number must also
		// carry the explicit Text number format. Some spreadsheet renderers
		// otherwise display even an inline string as scientific notation.
		if style == 0 {
			styleAttr = ` s="2"`
		}
	case "boolean":
		v := strings.ToLower(strings.TrimSpace(value))
		if v == "true" || v == "false" {
			if v == "true" {
				v = "1"
			} else {
				v = "0"
			}
			fmt.Fprintf(b, `<c r="%s" t="b"%s><v>%s</v></c>`, ref, styleAttr, v)
			return
		}
	}
	fmt.Fprintf(b, `<c r="%s" t="inlineStr"%s><is><t xml:space="preserve">%s</t></is></c>`, ref, styleAttr, xmlText(value))
}

// safeExcelDecimal accepts only values Excel can keep as typed numbers
// without silently changing a customer's digits. Excel numbers have 15
// significant decimal digits; anything more precise is deliberately stored
// as text. Writing the original lexical value also avoids a lossy float64
// parse/format round trip in our own process.
func safeExcelDecimal(value string) (string, bool) {
	v := strings.TrimSpace(value)
	if !plainDecimal.MatchString(v) {
		return "", false
	}
	digits := strings.TrimPrefix(v, "-")
	digits = strings.ReplaceAll(digits, ".", "")
	digits = strings.TrimLeft(digits, "0")
	if digits == "" {
		digits = "0"
	}
	if len(digits) > 15 {
		return "", false
	}
	return v, true
}

func excelColumn(n int) string {
	var out string
	for n > 0 {
		n--
		out = string(rune('A'+n%26)) + out
		n /= 26
	}
	return out
}

func contentTypesXML(n int) string {
	var b strings.Builder
	b.WriteString(`<?xml version="1.0" encoding="UTF-8" standalone="yes"?><Types xmlns="http://schemas.openxmlformats.org/package/2006/content-types"><Default Extension="rels" ContentType="application/vnd.openxmlformats-package.relationships+xml"/><Default Extension="xml" ContentType="application/xml"/><Override PartName="/xl/workbook.xml" ContentType="application/vnd.openxmlformats-officedocument.spreadsheetml.sheet.main+xml"/><Override PartName="/xl/styles.xml" ContentType="application/vnd.openxmlformats-officedocument.spreadsheetml.styles+xml"/>`)
	for i := 1; i <= n; i++ {
		fmt.Fprintf(&b, `<Override PartName="/xl/worksheets/sheet%d.xml" ContentType="application/vnd.openxmlformats-officedocument.spreadsheetml.worksheet+xml"/>`, i)
	}
	b.WriteString(`</Types>`)
	return b.String()
}

func workbookRelsXML(n int) string {
	var b strings.Builder
	b.WriteString(`<?xml version="1.0" encoding="UTF-8" standalone="yes"?><Relationships xmlns="http://schemas.openxmlformats.org/package/2006/relationships">`)
	for i := 1; i <= n; i++ {
		fmt.Fprintf(&b, `<Relationship Id="rId%d" Type="http://schemas.openxmlformats.org/officeDocument/2006/relationships/worksheet" Target="worksheets/sheet%d.xml"/>`, i, i)
	}
	fmt.Fprintf(&b, `<Relationship Id="rId%d" Type="http://schemas.openxmlformats.org/officeDocument/2006/relationships/styles" Target="styles.xml"/>`, n+1)
	b.WriteString(`</Relationships>`)
	return b.String()
}

const rootRelsXML = `<?xml version="1.0" encoding="UTF-8" standalone="yes"?><Relationships xmlns="http://schemas.openxmlformats.org/package/2006/relationships"><Relationship Id="rId1" Type="http://schemas.openxmlformats.org/officeDocument/2006/relationships/officeDocument" Target="xl/workbook.xml"/></Relationships>`

const stylesXML = `<?xml version="1.0" encoding="UTF-8" standalone="yes"?><styleSheet xmlns="http://schemas.openxmlformats.org/spreadsheetml/2006/main"><numFmts count="1"><numFmt numFmtId="164" formatCode="@"/></numFmts><fonts count="2"><font><sz val="11"/><name val="Calibri"/></font><font><b/><color rgb="FFFFFFFF"/><sz val="11"/><name val="Calibri"/></font></fonts><fills count="3"><fill><patternFill patternType="none"/></fill><fill><patternFill patternType="gray125"/></fill><fill><patternFill patternType="solid"><fgColor rgb="FF1F4E78"/><bgColor indexed="64"/></patternFill></fill></fills><borders count="1"><border><left/><right/><top/><bottom/><diagonal/></border></borders><cellStyleXfs count="1"><xf numFmtId="0" fontId="0" fillId="0" borderId="0"/></cellStyleXfs><cellXfs count="3"><xf numFmtId="0" fontId="0" fillId="0" borderId="0" xfId="0"/><xf numFmtId="0" fontId="1" fillId="2" borderId="0" xfId="0" applyFont="1" applyFill="1" applyAlignment="1"><alignment horizontal="center" vertical="center" wrapText="1"/></xf><xf numFmtId="164" fontId="0" fillId="0" borderId="0" xfId="0" applyNumberFormat="1" quotePrefix="1"/></cellXfs></styleSheet>`
