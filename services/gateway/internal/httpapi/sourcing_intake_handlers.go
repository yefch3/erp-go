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
	columns := make(map[string]int, len(rows[0]))
	for i, cell := range rows[0] {
		columns[normalizeInquiryHeader(cell)] = i
	}
	if !hasAnyHeader(columns, "product", "产品", "productname", "产品名称") {
		return nil, fmt.Errorf("缺少“产品”列，请使用标准询盘模板")
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
		line := &prv1.SourcingLineInput{
			Product: value(row, "产品", "产品名称", "product", "product name"), MaterialStandard: value(row, "材质/标准", "材质标准", "material standard"),
			Grade: value(row, "牌号/等级", "牌号", "等级", "grade"), Thickness: value(row, "厚度", "thickness"), Width: value(row, "宽度", "width"),
			LengthOrForm: value(row, "长度/形式", "长度", "形式", "length/form"), SurfaceRequirement: value(row, "表面要求", "surface requirement"),
			Coating: value(row, "涂层/镀层", "涂层", "镀层", "coating"), Tolerance: value(row, "公差", "tolerance"), CoilWeight: value(row, "卷重", "coil weight"),
			CoilId: value(row, "卷内径", "coil id"), Packaging: value(row, "包装", "packaging"), Delivery: value(row, "交期", "delivery"),
			PaymentTerms: value(row, "付款条件", "payment terms"), Incoterm: value(row, "贸易术语", "incoterm"), Port: value(row, "港口", "port"),
			QuantityUnit: value(row, "单位", "quantity unit", "unit"), Remarks: value(row, "备注", "remarks"), Quantity: value(row, "数量", "quantity"),
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
