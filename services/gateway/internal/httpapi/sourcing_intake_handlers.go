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

	mdv1 "github.com/sgao19/erp-go/gen/go/erp/masterdata/v1"
	prv1 "github.com/sgao19/erp-go/gen/go/erp/procurement/v1"
)

const standardizedInquiryMaxBytes = 8 << 20

type sourcingCustomerOption struct {
	ID   string `json:"id"`
	Code string `json:"code"`
	Name string `json:"name"`
}

type sourcingCustomerContactOption struct {
	ID         string `json:"id"`
	Name       string `json:"name"`
	Department string `json:"department"`
	Title      string `json:"title"`
	Email      string `json:"email"`
	IsPrimary  bool   `json:"isPrimary"`
}

// listSourcingCustomerOptions 返回询盘上传所需的最小客户选择项。
// 采购角色不需要因此取得完整客户档案的读取权限。
func (s *Server) listSourcingCustomerOptions(w http.ResponseWriter, r *http.Request) {
	resp, err := s.Customers.ListCustomers(r.Context(), &mdv1.ListCustomersRequest{
		Page: pageFromQuery(r), Keyword: r.URL.Query().Get("keyword"),
	})
	if err != nil {
		s.writeGRPCError(w, err)
		return
	}
	items := make([]sourcingCustomerOption, 0, len(resp.GetCustomers()))
	for _, customer := range resp.GetCustomers() {
		items = append(items, sourcingCustomerOption{
			ID: strconv.FormatInt(customer.GetId(), 10), Code: customer.GetCode(), Name: customer.GetName(),
		})
	}
	s.writeJSON(w, map[string]any{"customers": items})
}

// listSourcingCustomerContacts 只返回所选客户的有效联系人，主联系人优先顺序由主数据服务维护。
func (s *Server) listSourcingCustomerContacts(w http.ResponseWriter, r *http.Request) {
	customerID := idFromPath(r)
	if _, err := s.resolveActiveCustomer(r.Context(), customerID); err != nil {
		s.writeGRPCError(w, err)
		return
	}
	resp, err := s.Customers.ListCustomerContacts(r.Context(), &mdv1.ListCustomerContactsRequest{CustomerId: customerID})
	if err != nil {
		s.writeGRPCError(w, err)
		return
	}
	items := make([]sourcingCustomerContactOption, 0, len(resp.GetContacts()))
	for _, contact := range resp.GetContacts() {
		if contact.GetStatus() != "ACTIVE" {
			continue
		}
		items = append(items, sourcingCustomerContactOption{
			ID: strconv.FormatInt(contact.GetId(), 10), Name: contact.GetName(), Department: contact.GetDepartment(),
			Title: contact.GetTitle(), Email: contact.GetEmail(), IsPrimary: contact.GetIsPrimary(),
		})
	}
	s.writeJSON(w, map[string]any{"contacts": items})
}

// importSourcingIntake 接收邮件模块已经生成的标准 Excel，或员工手工上传的同格式文件。
// 两种入口最终都调用采购服务的同一套询盘创建校验。解析成功后把本次
// 使用的模板 ID/编码/版本一起保存，之后模板改版不影响这份询盘。
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
	templateID, err := parseOptionalManualInquiryTemplateID(r.FormValue("inquiry_template_id"))
	if err != nil {
		s.writeError(w, http.StatusBadRequest, "SC_INTAKE_TEMPLATE_INVALID", err.Error())
		return
	}
	if templateID == 0 {
		list, listErr := s.InquiryTemplates.ListInquiryTemplates(r.Context(), &prv1.ListInquiryTemplatesRequest{})
		if listErr != nil {
			s.writeGRPCError(w, listErr)
			return
		}
		templateID, err = recognizeInquiryTemplate(header, data, list.GetTemplates())
		if err != nil {
			s.writeError(w, http.StatusBadRequest, "SC_INTAKE_TEMPLATE_NOT_RECOGNIZED", err.Error())
			return
		}
	}
	template, err := s.InquiryTemplates.GetInquiryTemplate(r.Context(), &prv1.GetInquiryTemplateRequest{Id: templateID})
	if err != nil {
		s.writeGRPCError(w, err)
		return
	}
	if template.GetTemplate().GetStatus() != "ACTIVE" {
		s.writeError(w, http.StatusConflict, "SC_INTAKE_TEMPLATE_INACTIVE", "所选询盘模板已停用或已被新版本替代，请重新选择生效中的模板")
		return
	}
	lines, err := parseStandardizedInquiry(header, data, intakeFieldsFromTemplate(template.GetTemplate()))
	if err != nil {
		s.writeError(w, http.StatusBadRequest, "SC_INTAKE_FILE_INVALID", err.Error())
		return
	}
	title := strings.TrimSpace(r.FormValue("title"))
	if title == "" {
		title = strings.TrimSuffix(header.Filename, filepath.Ext(header.Filename))
	}
	customerID, _ := strconv.ParseInt(r.FormValue("customer_id"), 10, 64)
	contactID, _ := strconv.ParseInt(r.FormValue("contact_id"), 10, 64)
	customer, contact, err := s.resolveActiveCustomerContact(r.Context(), customerID, contactID)
	if err != nil {
		s.writeGRPCError(w, err)
		return
	}
	resp, err := s.Sourcing.CreateCase(r.Context(), &prv1.CreateCaseRequest{
		Title: title, CustomerId: customerID, CustomerName: customer.GetName(),
		ContactName: contact.GetName(), ContactEmail: contact.GetEmail(),
		// -1 表示手工上传；摘要用于阻止同一文件被重复导入。
		SourceMailId: -1, SourceAttachmentId: inquiryFingerprint(data), Lines: lines,
		SourceFileName: filepath.Base(header.Filename), SourceContentType: header.Header.Get("Content-Type"), SourceFileData: data,
		InquiryTemplateId: template.GetTemplate().GetId(), InquiryTemplateCode: template.GetTemplate().GetTemplateCode(),
		InquiryTemplateVersion: template.GetTemplate().GetVersion(),
	})
	if err != nil {
		s.writeGRPCError(w, err)
		return
	}
	s.writeProto(w, resp)
}

// parseOptionalManualInquiryTemplateID 允许员工把格式留空交给系统按表头识别；
// 一旦员工明确选择，则必须是合法的正整数，不能静默回退到默认格式。
func parseOptionalManualInquiryTemplateID(value string) (int64, error) {
	value = strings.TrimSpace(value)
	if value == "" {
		return 0, nil
	}
	id, err := strconv.ParseInt(value, 10, 64)
	if err != nil || id <= 0 {
		return 0, fmt.Errorf("所选询盘格式无效，请重新选择")
	}
	return id, nil
}

// recognizeInquiryTemplate 用完整表头集合匹配生效中的询盘格式。列顺序可以不同，
// 但列名必须完全一致；若多个格式相同，唯一默认格式优先，否则要求员工明确选择。
func recognizeInquiryTemplate(header *multipart.FileHeader, data []byte, templates []*prv1.InquiryTemplate) (int64, error) {
	rows, err := readStandardizedInquiryRows(header, data)
	if err != nil {
		return 0, err
	}
	if len(rows) == 0 {
		return 0, fmt.Errorf("标准询盘文件没有表头")
	}
	matches := make([]*prv1.InquiryTemplate, 0, 1)
	for _, template := range templates {
		if template.GetStatus() != "ACTIVE" {
			continue
		}
		if _, matchErr := validateInquiryHeaders(rows[0], intakeFieldsFromTemplate(template), false); matchErr == nil {
			matches = append(matches, template)
		}
	}
	if len(matches) == 1 {
		return matches[0].GetId(), nil
	}
	if len(matches) > 1 {
		defaults := make([]*prv1.InquiryTemplate, 0, 1)
		for _, match := range matches {
			if match.GetIsDefault() {
				defaults = append(defaults, match)
			}
		}
		if len(defaults) == 1 {
			return defaults[0].GetId(), nil
		}
		return 0, fmt.Errorf("文件表头同时匹配多个询盘格式，请在上传窗口中明确选择一个格式")
	}
	return 0, fmt.Errorf("无法根据文件表头识别询盘格式，请检查文件是否由邮件标准化流程生成，或在上传窗口中选择正确格式后重试")
}

func inquiryFingerprint(data []byte) int64 {
	sum := sha256.Sum256(data)
	value := int64(binary.BigEndian.Uint64(sum[:8]) & ((1 << 63) - 1))
	if value == 0 {
		return 1
	}
	return value
}

// 模板的列定义转成解析用的字段表，按模板设定的顺序。
func intakeFieldsFromTemplate(template *prv1.InquiryTemplate) []standardizedInquiryField {
	protoFields := append([]*prv1.InquiryTemplateField(nil), template.GetFields()...)
	sort.SliceStable(protoFields, func(i, j int) bool { return protoFields[i].GetSortOrder() < protoFields[j].GetSortOrder() })
	fields := make([]standardizedInquiryField, 0, len(protoFields))
	for _, field := range protoFields {
		fields = append(fields, standardizedInquiryField{
			Key: field.GetFieldKey(), Header: field.GetDisplayName(), DefaultValue: field.GetDefaultValue(),
		})
	}
	return fields
}

func parseStandardizedInquiry(header *multipart.FileHeader, data []byte, fields []standardizedInquiryField) ([]*prv1.SourcingLineInput, error) {
	rows, err := readStandardizedInquiryRows(header, data)
	if err != nil {
		return nil, err
	}
	return parseStandardizedInquiryRows(rows, fields)
}

func readStandardizedInquiryRows(header *multipart.FileHeader, data []byte) ([][]string, error) {
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
	return rows, nil
}

func parseStandardizedInquiryRows(rows [][]string, fields []standardizedInquiryField) ([]*prv1.SourcingLineInput, error) {
	if len(rows) < 2 {
		return nil, fmt.Errorf("标准询盘文件没有可导入的明细")
	}
	// 明确选择模板后，缺失列由模板补成空字段进入销售人工复核；未知列仍然
	// 拒绝，避免客户提供的数据因为模板不匹配而被静默丢弃。
	columns, err := validateInquiryHeaders(rows[0], fields, true)
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

// validateInquiryHeaders 校验上传文件表头。自动识别模板时要求完整匹配；员工
// 已明确选择模板时允许缺列，缺失字段会以空值进入待复核。额外列、空白列名
// 和重复列名始终拒绝，避免客户提供的数据被静默丢弃。
type standardizedInquiryField struct {
	Key          string
	Header       string
	DefaultValue string
}

func validateInquiryHeaders(headers []string, fields []standardizedInquiryField, allowMissing bool) (map[string]int, error) {
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
	if len(missing) > 0 && !allowMissing {
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
	value = strings.TrimPrefix(strings.TrimSpace(value), "\ufeff")
	return strings.ToLower(strings.TrimSpace(value))
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
	case "unit_price", "total_price":
		// 价格在询盘阶段为空或由公式计算，不属于采购明细事实。
	default:
		// 模板的 custom.* 自定义列随明细保存，键即模板里的字段标识。
		if strings.HasPrefix(key, "custom.") && value != "" {
			if line.CustomFields == nil {
				line.CustomFields = map[string]string{}
			}
			line.CustomFields[key] = value
		}
	}
}

func normalizeInquiryHeader(value string) string {
	value = strings.TrimPrefix(strings.TrimSpace(value), "\ufeff")
	return strings.Map(func(r rune) rune {
		if unicode.IsSpace(r) || strings.ContainsRune("_-/（）()", r) {
			return -1
		}
		return unicode.ToLower(r)
	}, strings.TrimSpace(value))
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
