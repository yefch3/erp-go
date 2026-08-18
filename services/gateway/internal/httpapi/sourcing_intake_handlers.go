package httpapi

import (
	"archive/zip"
	"bytes"
	"crypto/sha256"
	"encoding/binary"
	"encoding/csv"
	"encoding/xml"
	"fmt"
	"io"
	"mime/multipart"
	"net/http"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"unicode"

	prv1 "github.com/sgao19/erp-go/gen/go/erp/procurement/v1"
)

const standardizedInquiryMaxBytes = 8 << 20

// importSourcingIntake 接收邮件模块已经生成的标准 Excel，或员工手工上传的同格式文件。
// 两种入口最终都调用采购服务的同一套询盘创建校验。
func (s *Server) importSourcingIntake(w http.ResponseWriter, r *http.Request) {
	r.Body = http.MaxBytesReader(w, r.Body, standardizedInquiryMaxBytes)
	if err := r.ParseMultipartForm(standardizedInquiryMaxBytes); err != nil {
		s.writeError(w, http.StatusBadRequest, "SC_INTAKE_FILE_INVALID", "上传文件无效或超过 8MB")
		return
	}
	file, header, err := r.FormFile("file")
	if err != nil {
		s.writeError(w, http.StatusBadRequest, "SC_INTAKE_FILE_REQUIRED", "请选择标准询盘 Excel 文件")
		return
	}
	defer func() { _ = file.Close() }()
	data, err := io.ReadAll(file)
	if err != nil {
		s.writeError(w, http.StatusBadRequest, "SC_INTAKE_FILE_READ_FAILED", "无法读取上传文件")
		return
	}
	lines, err := parseStandardizedInquiry(header, data)
	if err != nil {
		s.writeError(w, http.StatusBadRequest, "SC_INTAKE_FILE_INVALID", err.Error())
		return
	}
	title := strings.TrimSpace(r.FormValue("title"))
	if title == "" {
		title = strings.TrimSuffix(header.Filename, filepath.Ext(header.Filename))
	}
	customerID, _ := strconv.ParseInt(r.FormValue("customer_id"), 10, 64)
	resp, err := s.Sourcing.CreateCase(r.Context(), &prv1.CreateCaseRequest{
		Title: title, CustomerId: customerID, CustomerName: strings.TrimSpace(r.FormValue("customer_name")),
		ContactName: strings.TrimSpace(r.FormValue("contact_name")), ContactEmail: strings.TrimSpace(r.FormValue("contact_email")),
		// -1 表示手工上传；摘要用于阻止同一文件被重复导入。
		SourceMailId: -1, SourceAttachmentId: inquiryFingerprint(data), Lines: lines,
		SourceFileName: filepath.Base(header.Filename), SourceContentType: header.Header.Get("Content-Type"), SourceFileData: data,
	})
	if err != nil {
		s.writeGRPCError(w, err)
		return
	}
	s.writeProto(w, resp)
}

func inquiryFingerprint(data []byte) int64 {
	sum := sha256.Sum256(data)
	value := int64(binary.BigEndian.Uint64(sum[:8]) & ((1 << 63) - 1))
	if value == 0 {
		return 1
	}
	return value
}

func parseStandardizedInquiry(header *multipart.FileHeader, data []byte) ([]*prv1.SourcingLineInput, error) {
	fields := standardizedInquiryFields()
	ext := strings.ToLower(filepath.Ext(header.Filename))
	var rows [][]string
	var err error
	switch ext {
	case ".xlsx":
		rows, err = readFirstXLSXSheet(data)
	case ".csv":
		rows, err = csv.NewReader(bytes.NewReader(data)).ReadAll()
	default:
		return nil, fmt.Errorf("仅支持 .xlsx 或 .csv 标准询盘文件")
	}
	if err != nil {
		return nil, fmt.Errorf("无法解析标准询盘文件：%w", err)
	}
	if len(rows) < 2 {
		return nil, fmt.Errorf("标准询盘文件没有可导入的明细")
	}
	columns, err := validateInquiryHeaders(rows[0], fields)
	if err != nil {
		return nil, err
	}
	value := func(row []string, names ...string) string {
		for _, name := range names {
			if idx, ok := columns[normalizeInquiryHeader(name)]; ok && idx < len(row) {
				return strings.TrimSpace(row[idx])
			}
		}
		return ""
	}
	lines := make([]*prv1.SourcingLineInput, 0, len(rows)-1)
	for _, row := range rows[1:] {
		line := &prv1.SourcingLineInput{}
		for _, field := range fields {
			cell := value(row, field.Header)
			if cell == "" {
				cell = strings.TrimSpace(field.DefaultValue)
			}
			assignInquiryField(line, field.Key, cell)
		}
		if line.Product == "" && line.Quantity == "" && line.MaterialStandard == "" && line.Remarks == "" {
			continue
		}
		lines = append(lines, line)
	}
	if len(lines) == 0 {
		return nil, fmt.Errorf("标准询盘文件没有可导入的有效明细")
	}
	return lines, nil
}

// validateInquiryHeaders 严格校验上传文件的表头是否与系统标准询盘格式一致。
// 列顺序可以调整，但不允许缺列、额外列、空白列名或重复列名，避免采购规格被静默丢弃。
type standardizedInquiryField struct {
	Key          string
	Header       string
	DefaultValue string
}

func validateInquiryHeaders(headers []string, fields []standardizedInquiryField) (map[string]int, error) {
	columns := make(map[string]int, len(headers))
	actualNames := make(map[string]string, len(headers))
	seenHeaders := make(map[string]struct{}, len(headers))
	blankColumns := make([]string, 0)
	duplicateColumns := make([]string, 0)
	for i, cell := range headers {
		name := strings.TrimSpace(cell)
		normalized := normalizeStrictInquiryHeader(name)
		if normalized == "" {
			blankColumns = append(blankColumns, strconv.Itoa(i+1))
			continue
		}
		if _, exists := seenHeaders[normalized]; exists {
			duplicateColumns = append(duplicateColumns, name)
			continue
		}
		seenHeaders[normalized] = struct{}{}
		actualNames[normalized] = name
		columns[normalizeInquiryHeader(name)] = i
	}

	expected := make(map[string]string, len(fields))
	for _, field := range fields {
		displayName := strings.TrimSpace(field.Header)
		normalized := normalizeStrictInquiryHeader(displayName)
		if normalized == "" {
			return nil, fmt.Errorf("系统标准字段存在空白 Excel 表头，请联系管理员")
		}
		if previous, exists := expected[normalized]; exists {
			return nil, fmt.Errorf("系统标准字段存在重复 Excel 表头“%s”和“%s”，请联系管理员", previous, displayName)
		}
		expected[normalized] = displayName
	}

	missing := make([]string, 0)
	for normalized, displayName := range expected {
		if _, ok := actualNames[normalized]; !ok {
			missing = append(missing, displayName)
		}
	}
	unknown := make([]string, 0)
	for normalized, name := range actualNames {
		if _, ok := expected[normalized]; !ok {
			unknown = append(unknown, name)
		}
	}
	sort.Strings(missing)
	sort.Strings(unknown)
	sort.Strings(duplicateColumns)

	problems := make([]string, 0, 4)
	if len(missing) > 0 {
		problems = append(problems, "缺少列："+strings.Join(missing, "、"))
	}
	if len(unknown) > 0 {
		problems = append(problems, "非标准列："+strings.Join(unknown, "、"))
	}
	if len(blankColumns) > 0 {
		problems = append(problems, "空白表头位于第 "+strings.Join(blankColumns, "、")+" 列")
	}
	if len(duplicateColumns) > 0 {
		problems = append(problems, "重复列："+strings.Join(duplicateColumns, "、"))
	}
	if len(problems) > 0 {
		return nil, fmt.Errorf("上传文件的表头与系统标准不一致；%s。请使用邮件模块生成的标准文件，或系统规定的手工上传格式", strings.Join(problems, "；"))
	}
	return columns, nil
}

func normalizeStrictInquiryHeader(value string) string {
	return strings.ToLower(strings.TrimSpace(value))
}

func standardizedInquiryFields() []standardizedInquiryField {
	names := []struct{ key, display string }{
		{"product", "产品"}, {"material_standard", "材质/标准"}, {"grade", "牌号/等级"},
		{"thickness", "厚度"}, {"width", "宽度"}, {"length_or_form", "长度/形式"},
		{"surface_requirement", "表面要求"}, {"coating", "涂层/镀层"}, {"tolerance", "公差"},
		{"coil_weight", "卷重"}, {"coil_id", "卷内径"}, {"packaging", "包装"}, {"delivery", "交期"},
		{"payment_terms", "付款条件"}, {"incoterm", "贸易术语"}, {"port", "港口"},
		{"quantity", "数量"}, {"quantity_unit", "单位"}, {"remarks", "备注"},
	}
	fields := make([]standardizedInquiryField, 0, len(names))
	for _, item := range names {
		fields = append(fields, standardizedInquiryField{Key: item.key, Header: item.display})
	}
	return fields
}

func assignInquiryField(line *prv1.SourcingLineInput, key, value string) {
	switch key {
	case "product":
		line.Product = value
	case "material_standard":
		line.MaterialStandard = value
	case "grade":
		line.Grade = value
	case "thickness":
		line.Thickness = value
	case "width":
		line.Width = value
	case "length_or_form":
		line.LengthOrForm = value
	case "surface_requirement":
		line.SurfaceRequirement = value
	case "coating":
		line.Coating = value
	case "tolerance":
		line.Tolerance = value
	case "coil_weight":
		line.CoilWeight = value
	case "coil_id":
		line.CoilId = value
	case "packaging":
		line.Packaging = value
	case "delivery":
		line.Delivery = value
	case "payment_terms":
		line.PaymentTerms = value
	case "incoterm":
		line.Incoterm = value
	case "port":
		line.Port = value
	case "quantity":
		line.Quantity = value
	case "quantity_unit":
		line.QuantityUnit = value
	case "remarks":
		line.Remarks = value
	}
}

func normalizeInquiryHeader(value string) string {
	return strings.Map(func(r rune) rune {
		if unicode.IsSpace(r) || strings.ContainsRune("_-/（）()", r) {
			return -1
		}
		return unicode.ToLower(r)
	}, strings.TrimSpace(value))
}

func hasAnyHeader(columns map[string]int, names ...string) bool {
	for _, name := range names {
		if _, ok := columns[normalizeInquiryHeader(name)]; ok {
			return true
		}
	}
	return false
}

type xlsxSharedStrings struct {
	Items []struct {
		Text string `xml:"t"`
		Runs []struct {
			Text string `xml:"t"`
		} `xml:"r"`
	} `xml:"si"`
}
type xlsxWorksheet struct {
	Rows []struct {
		Cells []struct {
			Ref    string `xml:"r,attr"`
			Type   string `xml:"t,attr"`
			Value  string `xml:"v"`
			Inline struct {
				Text string `xml:"t"`
			} `xml:"is"`
		} `xml:"c"`
	} `xml:"sheetData>row"`
}

// readFirstXLSXSheet 只读取标准模板的首个工作表，避免引入重量级 Excel 运行时依赖。
func readFirstXLSXSheet(data []byte) ([][]string, error) {
	zr, err := zip.NewReader(bytes.NewReader(data), int64(len(data)))
	if err != nil {
		return nil, err
	}
	files := map[string]*zip.File{}
	var sheets []string
	for _, f := range zr.File {
		name := strings.ReplaceAll(f.Name, "\\", "/")
		files[name] = f
		if strings.HasPrefix(name, "xl/worksheets/") && strings.HasSuffix(name, ".xml") {
			sheets = append(sheets, name)
		}
	}
	sort.Strings(sheets)
	if len(sheets) == 0 {
		return nil, fmt.Errorf("工作簿中没有工作表")
	}
	shared := []string{}
	if f := files["xl/sharedStrings.xml"]; f != nil {
		var doc xlsxSharedStrings
		if err := decodeZipXML(f, &doc); err != nil {
			return nil, err
		}
		for _, item := range doc.Items {
			text := item.Text
			for _, run := range item.Runs {
				text += run.Text
			}
			shared = append(shared, text)
		}
	}
	var sheet xlsxWorksheet
	if err := decodeZipXML(files[sheets[0]], &sheet); err != nil {
		return nil, err
	}
	rows := make([][]string, 0, len(sheet.Rows))
	for _, sourceRow := range sheet.Rows {
		row := []string{}
		for _, cell := range sourceRow.Cells {
			idx := xlsxColumnIndex(cell.Ref)
			for len(row) <= idx {
				row = append(row, "")
			}
			text := cell.Value
			switch cell.Type {
			case "s":
				n, _ := strconv.Atoi(cell.Value)
				if n >= 0 && n < len(shared) {
					text = shared[n]
				}
			case "inlineStr":
				text = cell.Inline.Text
			}
			row[idx] = text
		}
		rows = append(rows, row)
	}
	return rows, nil
}

func decodeZipXML(file *zip.File, target any) error {
	r, err := file.Open()
	if err != nil {
		return err
	}
	defer func() { _ = r.Close() }()
	return xml.NewDecoder(r).Decode(target)
}

func xlsxColumnIndex(ref string) int {
	index := 0
	for _, r := range ref {
		if r < 'A' || r > 'Z' {
			break
		}
		index = index*26 + int(r-'A'+1)
	}
	if index == 0 {
		return 0
	}
	return index - 1
}
