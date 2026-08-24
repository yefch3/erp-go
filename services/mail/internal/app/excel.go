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
	"unicode"
	"unicode/utf8"

	"github.com/shopspring/decimal"

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
	Extract(ctx context.Context, in TableExtractionInput) (Extraction, error)
}

// Extraction 是一次模型调用的全部产出：结果、用的哪个模型、花了多少。
//
// Usage 即使在 err 非空时也可能是有值的——模型答了、钱花了，只是答出来的
// 东西解不开。计量要记的是花掉的，不是成功的：把失败那次的消耗漏掉，账就
// 对不上真实账单。
type Extraction struct {
	Workbook Workbook
	Model    string
	Usage    ModelUsage
}

// ModelUsage 是一次调用的用量。只存 token 数，不存金额——token 是事实，
// 折成多少钱是判断，会因为谈折扣、换模型而变。
type ModelUsage struct {
	InputTokens  int64
	OutputTokens int64
}

type TableExtractionInput struct {
	Text        string
	FileName    string
	ContentType string
	FileData    []byte
	Locale      string
	SafetyID    string
	// 当前默认询盘模板的列快照；空时按系统内置 21 列处理。
	Columns []InquiryColumn
}

type Workbook struct {
	Title  string          `json:"title"`
	Sheets []WorkbookSheet `json:"sheets"`
}

type WorkbookSheet struct {
	Name        string   `json:"name"`
	Summary     string   `json:"summary"`
	Columns     []string `json:"columns"`
	ColumnTypes []string `json:"column_types"`
	// 与 Columns 平行的模板字段标识，采购转入按它而不是表头文字对齐。
	ColumnKeys []string   `json:"column_keys,omitempty"`
	Rows       [][]string `json:"rows"`
	// 给人看的那一版：和 Rows 逐格对应，但公式换成算出来的数。文件里要
	// 留活公式（在 Excel 里改数量总价要跟着动），页面上要显示结果——同一
	// 份数据的两种用途，分开存比在渲染时猜哪个是公式可靠。
	//
	// 旧任务的缓存里没有这个字段，取不到时回落 Rows。
	PreviewRows [][]string `json:"preview_rows,omitempty"`
}

// InquiryColumn 是询盘模板的一列。列注册表在采购服务；网关在发起转换时
// 把当前默认模板的列快照随请求传入，邮件服务不反向依赖采购。
type InquiryColumn struct {
	FieldKey     string `json:"field_key"`
	DisplayName  string `json:"display_name"`
	DataType     string `json:"data_type"`
	IsRequired   bool   `json:"is_required"`
	DefaultValue string `json:"default_value"`
}

// SystemInquiryColumns 是系统内置 21 列布局，只在请求没有携带模板快照时
// 兜底（旧任务、直连调试）。正常路径永远使用采购侧默认模板的快照。
func SystemInquiryColumns() []InquiryColumn {
	type c = InquiryColumn
	return []c{
		{FieldKey: "product", DisplayName: "产品", DataType: "TEXT", IsRequired: true},
		{FieldKey: "material_standard", DisplayName: "材质/标准", DataType: "TEXT"},
		{FieldKey: "grade", DisplayName: "牌号/等级", DataType: "TEXT"},
		{FieldKey: "thickness", DisplayName: "厚度", DataType: "TEXT"},
		{FieldKey: "width", DisplayName: "宽度", DataType: "TEXT"},
		{FieldKey: "length_or_form", DisplayName: "长度/形式", DataType: "TEXT"},
		{FieldKey: "surface_requirement", DisplayName: "表面要求", DataType: "TEXT"},
		{FieldKey: "coating", DisplayName: "涂层/镀层", DataType: "TEXT"},
		{FieldKey: "tolerance", DisplayName: "公差", DataType: "TEXT"},
		{FieldKey: "coil_weight", DisplayName: "卷重", DataType: "TEXT"},
		{FieldKey: "coil_id", DisplayName: "卷内径", DataType: "TEXT"},
		{FieldKey: "packaging", DisplayName: "包装", DataType: "TEXT"},
		{FieldKey: "delivery", DisplayName: "交期", DataType: "TEXT"},
		{FieldKey: "payment_terms", DisplayName: "付款条件", DataType: "TEXT"},
		{FieldKey: "incoterm", DisplayName: "贸易术语", DataType: "TEXT"},
		{FieldKey: "port", DisplayName: "港口", DataType: "TEXT"},
		{FieldKey: "quantity_unit", DisplayName: "单位", DataType: "TEXT", IsRequired: true},
		{FieldKey: "remarks", DisplayName: "备注", DataType: "TEXT"},
		{FieldKey: "quantity", DisplayName: "数量", DataType: "NUMBER", IsRequired: true},
		{FieldKey: "unit_price", DisplayName: "单价", DataType: "NUMBER"},
		{FieldKey: "total_price", DisplayName: "总价", DataType: "NUMBER"},
	}
}

// ExtractedInquiry 是模型唯一允许返回的形状：每行是按模板字段标识 keyed
// 的事实。公司工作簿由服务端按模板列组装，模型不能决定列名、顺序或公式。
type ExtractedInquiry struct {
	Title   string              `json:"title"`
	Summary string              `json:"summary"`
	Items   []map[string]string `json:"items"`
}

// NewTemplateWorkbook 按模板列把抽取结果落成工作簿：列名与顺序来自模板，
// 空值落模板默认值；当模板同时包含数量、单价、总价三列时，总价列生成
// 「数量×单价」公式（列位置按模板顺序动态计算）。
func NewTemplateWorkbook(in ExtractedInquiry, columns []InquiryColumn) Workbook {
	if len(columns) == 0 {
		columns = SystemInquiryColumns()
	}
	qtyIdx, priceIdx, totalIdx := -1, -1, -1
	for i, column := range columns {
		switch column.FieldKey {
		case "quantity":
			qtyIdx = i
		case "unit_price":
			priceIdx = i
		case "total_price":
			totalIdx = i
		}
	}
	withFormula := qtyIdx >= 0 && priceIdx >= 0 && totalIdx >= 0

	headers := make([]string, len(columns))
	keys := make([]string, len(columns))
	types := make([]string, len(columns))
	for i, column := range columns {
		headers[i] = column.DisplayName
		keys[i] = column.FieldKey
		switch column.DataType {
		case "NUMBER":
			types[i] = "number"
		case "DATE":
			types[i] = "date"
		default:
			types[i] = "string"
		}
	}
	if withFormula {
		types[totalIdx] = "formula"
	}

	rows := make([][]string, 0, len(in.Items))
	previews := make([][]string, 0, len(in.Items))
	for i, item := range in.Items {
		excelRow := i + 2 // row 1 is the header
		row := make([]string, len(columns))
		preview := make([]string, len(columns))
		for c, column := range columns {
			value := strings.TrimSpace(item[column.FieldKey])
			if value == "" {
				value = column.DefaultValue
			}
			row[c] = value
			preview[c] = value
		}
		// 总价只在这一行确实有数量和单价时才落公式。
		//
		// 询盘阶段单价永远是空的——提示词写死了「报价是工厂后面给的，这里
		// 绝不计算也绝不臆造」。从前不管有没有单价都写公式，于是每一行的
		// 总价都是一个指着空格子的算式：Excel 里算出 0，预览里露出
		// 「=S2*T2」。一列从头到尾没有意义，还看着像坏了。
		if withFormula {
			qty, qtyOK := safeExcelDecimal(row[qtyIdx])
			price, priceOK := safeExcelDecimal(row[priceIdx])
			if qtyOK && priceOK {
				row[totalIdx] = fmt.Sprintf("=%s%d*%s%d",
					excelColumn(qtyIdx+1), excelRow, excelColumn(priceIdx+1), excelRow)
				// 预览显示算出来的数，不是算式。浏览器里那张表是把单元格
				// 原样打出来的，公式落进去就成了给人看的乱码。
				preview[totalIdx] = multiplyDecimalText(qty, price)
			} else {
				row[totalIdx] = ""
				preview[totalIdx] = ""
			}
		}
		rows = append(rows, row)
		previews = append(previews, preview)
	}
	return Workbook{Title: in.Title, Sheets: []WorkbookSheet{{
		Name: "询价明细", Summary: in.Summary,
		Columns: headers, ColumnTypes: types, ColumnKeys: keys,
		Rows: rows, PreviewRows: previews,
	}}}
}

// multiplyDecimalText 把两个已经过 safeExcelDecimal 的十进制文本相乘，
// 只为预览显示。文件里落的仍是公式：在 Excel 里改了数量或单价，总价要跟
// 着动。
func multiplyDecimalText(a, b string) string {
	x, err := decimal.NewFromString(a)
	if err != nil {
		return ""
	}
	y, err := decimal.NewFromString(b)
	if err != nil {
		return ""
	}
	return x.Mul(y).String()
}

type ExcelResult struct {
	FileName string
	Data     []byte
	Workbook Workbook
	Model    string
	// 这次调用花了多少。**失败时也可能有值**——模型答了、钱花了，只是答出
	// 来的东西解不开。记账要记花掉的，不是成功的。
	Usage ModelUsage
}

// ConvertInboundToExcel accepts exactly one user-selected source. It does not
// accept object keys or arbitrary files from the browser: attachment ids are
// resolved against the already owner-scoped mail record.
func (s *Service) ConvertInboundToExcel(
	ctx context.Context, tenantID, ownerID, inboundID int64,
	attachmentID *int64, selectedText *string, locale string,
	columns []InquiryColumn,
) (ExcelResult, error) {
	if s.tables == nil {
		return ExcelResult{}, apierr.Invalid("MAIL_EXCEL_NOT_CONFIGURED", "Excel 智能转换尚未配置")
	}
	if len(columns) == 0 {
		columns = SystemInquiryColumns()
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

	in := TableExtractionInput{Locale: normalizeExcelLocale(locale), Columns: columns}
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
		if !selectionBelongsToMail(row.BodyText, row.BodyHtml, text) {
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

	extracted, err := s.tables.Extract(ctx, in)
	book, model := extracted.Workbook, extracted.Model
	// 失败也要把用量带出去。这几次照样计费，漏掉它们账就对不上真实账单。
	spent := ExcelResult{Model: model, Usage: extracted.Usage}
	if err != nil {
		s.log.Error("table extraction failed", "mail", inboundID, "err", err)
		return spent, apierr.Internal("MAIL_EXCEL_MODEL_FAILED", "智能转换失败，请稍后重试").Wrap(err)
	}
	if err := validateWorkbook(&book); err != nil {
		s.log.Error("table extraction returned an invalid workbook", "mail", inboundID, "err", err)
		return spent, apierr.Internal("MAIL_EXCEL_INVALID_RESULT", "模型返回的表格格式无效，请重试").Wrap(err)
	}
	data, err := buildXLSX(book)
	if err != nil {
		return spent, apierr.Internal("MAIL_EXCEL_BUILD_FAILED", "生成 Excel 失败，请重试").Wrap(err)
	}
	name := safeExcelFileName(book.Title)
	if name == "" {
		name = "客户询价单"
	}
	// 从 spent 上补齐，不另起一个空壳——新建一个返回值就会把上面记下来的用量
	// 丢在半路，而成功恰恰是绝大多数情况，账会一直是 0。
	spent.FileName, spent.Data, spent.Workbook = name+".xlsx", data, book
	return spent, nil
}

// selectionBelongsToMail 判断这段选中的文字确实出自这封信。
//
// 存在的理由是：选中的文字会原样交给模型，所以必须先证明它来自这封邮件，
// 而不是谁往请求里塞的一段话。
//
// **两个版本都要试**。一封信常常同时带纯文本和 HTML 两个版本，内容可以差
// 得很远——尤其是带表格的报价单，纯文本那版往往是发信软件草草压平的。而
// 页面上渲染的是 HTML 版，人从那里选字。从前只比纯文本版，于是明明是这封
// 信里的字，系统说不是。任一版命中就算数。
func selectionBelongsToMail(bodyText, bodyHTML, selected string) bool {
	if containsNormalizedText(bodyText, selected) {
		return true
	}
	return containsNormalizedText(HTMLToText(bodyHTML), selected)
}

// containsNormalizedText 比较时抹掉两类差异：**所有空白**，以及全角半角
// 标点。
//
// 空白整个丢掉而不是压成一个空格：中文正文里「规格：3.0」和「规格: 3.0」
// 只差一个空格，浏览器跨单元格拖选还会带出制表符和换行——压成一个空格仍
// 然对不上。丢掉空白不会放外来内容进来：能通过的字符串必须是这封信去掉
// 空白后的子串，内容还是这封信的。
func containsNormalizedText(body, selected string) bool {
	if strings.TrimSpace(body) == "" {
		return false
	}
	return strings.Contains(normalizeForSelection(body), normalizeForSelection(selected))
}

// 全角标点 → 半角。只做标点，不动文字：把全角汉字数字也一起折了会让
// 「１２３」和「123」混为一谈，那是另一回事。
var fullWidthPunct = strings.NewReplacer(
	"，", ",", "。", ".", "：", ":", "；", ";", "！", "!", "？", "?",
	"（", "(", "）", ")", "［", "[", "］", "]", "｛", "{", "｝", "}",
	"　", " ", "＂", `"`, "＇", "'", "－", "-", "／", "/", "＼", "\\",
	"＝", "=", "＋", "+", "％", "%", "＊", "*", "＃", "#", "＠", "@",
	"“", `"`, "”", `"`, "‘", "'", "’", "'", "、", ",",
)

func normalizeForSelection(s string) string {
	return strings.Map(func(r rune) rune {
		if unicode.IsSpace(r) {
			return -1
		}
		return r
	}, fullWidthPunct.Replace(s))
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
			case "string", "number", "boolean", "date", "formula":
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
		// 预览行对不上就整份丢掉，不半修半留。它只是同一份数据给人看的
		// 那一版，缺了回落到 Rows 仍然可读；留一份长度不齐的反而会让页面
		// 错位。
		if len(s.PreviewRows) != len(s.Rows) {
			s.PreviewRows = nil
			continue
		}
		for j := range s.PreviewRows {
			if len(s.PreviewRows[j]) > len(s.Columns) {
				s.PreviewRows = nil
				break
			}
			for len(s.PreviewRows[j]) < len(s.Columns) {
				s.PreviewRows[j] = append(s.PreviewRows[j], "")
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
	b.WriteString(`</sheets><calcPr calcId="191029" fullCalcOnLoad="1" forceFullCalc="1"/></workbook>`)
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
	writeRow := func(row int, values []string, types []string, cached []string, style int) {
		fmt.Fprintf(&b, `<row r="%d">`, row)
		for col, value := range values {
			ref := excelColumn(col+1) + strconv.Itoa(row)
			kind := "string"
			if col < len(types) {
				kind = types[col]
			}
			cache := ""
			if col < len(cached) {
				cache = cached[col]
			}
			writeCell(&b, ref, value, kind, cache, style)
		}
		b.WriteString(`</row>`)
	}
	writeRow(1, s.Columns, nil, nil, 1)
	// 公式格的缓存值取预览行里算好的那个数。有些看表的工具不会重算公式，
	// 只显示文件里存的那份缓存——从前一律存 0，于是在那些工具里总价永远
	// 是 0。
	usePreview := len(s.PreviewRows) == len(s.Rows)
	for i, row := range s.Rows {
		var cached []string
		if usePreview {
			cached = s.PreviewRows[i]
		}
		writeRow(i+2, row, s.ColumnTypes, cached, 0)
	}
	b.WriteString(`</sheetData>`)
	if len(s.Rows) > 0 {
		fmt.Fprintf(&b, `<autoFilter ref="A1:%s%d"/>`, excelColumn(len(s.Columns)), len(s.Rows)+1)
	}
	b.WriteString(`</worksheet>`)
	return b.String()
}

func writeCell(b *strings.Builder, ref, value, kind, cached string, style int) {
	styleAttr := ""
	if style > 0 {
		styleAttr = fmt.Sprintf(` s="%d"`, style)
	}
	switch kind {
	case "formula":
		formula := strings.TrimPrefix(value, "=")
		if formula != "" {
			shown, ok := safeExcelDecimal(cached)
			if !ok {
				shown = "0"
			}
			fmt.Fprintf(b, `<c r="%s" s="3"><f>%s</f><v>%s</v></c>`, ref, xmlText(formula), shown)
			return
		}
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

const stylesXML = `<?xml version="1.0" encoding="UTF-8" standalone="yes"?><styleSheet xmlns="http://schemas.openxmlformats.org/spreadsheetml/2006/main"><numFmts count="2"><numFmt numFmtId="164" formatCode="@"/><numFmt numFmtId="165" formatCode="#,##0.00"/></numFmts><fonts count="2"><font><sz val="11"/><name val="Calibri"/></font><font><b/><color rgb="FFFFFFFF"/><sz val="11"/><name val="Calibri"/></font></fonts><fills count="3"><fill><patternFill patternType="none"/></fill><fill><patternFill patternType="gray125"/></fill><fill><patternFill patternType="solid"><fgColor rgb="FF1F4E78"/><bgColor indexed="64"/></patternFill></fill></fills><borders count="1"><border><left/><right/><top/><bottom/><diagonal/></border></borders><cellStyleXfs count="1"><xf numFmtId="0" fontId="0" fillId="0" borderId="0"/></cellStyleXfs><cellXfs count="4"><xf numFmtId="0" fontId="0" fillId="0" borderId="0" xfId="0"/><xf numFmtId="0" fontId="1" fillId="2" borderId="0" xfId="0" applyFont="1" applyFill="1" applyAlignment="1"><alignment horizontal="center" vertical="center" wrapText="1"/></xf><xf numFmtId="164" fontId="0" fillId="0" borderId="0" xfId="0" applyNumberFormat="1" quotePrefix="1"/><xf numFmtId="165" fontId="0" fillId="0" borderId="0" xfId="0" applyNumberFormat="1"/></cellXfs></styleSheet>`
