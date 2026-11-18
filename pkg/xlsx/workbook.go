package xlsx

import (
	"archive/zip"
	"bytes"
	"encoding/xml"
	"fmt"
	"path"
	"strings"
)

// Worksheet contains raw cached cell values at their original, one-based coordinates.
type Worksheet struct {
	Name string
	Rows map[int]map[int]string
}

// ReadWorkbook reads the worksheets in workbook order, preserving sparse cell addresses.
func ReadWorkbook(data []byte) ([]Worksheet, error) {
	const partLimit = 16 << 20
	const workbookLimit = 64 << 20
	if len(data) == 0 || len(data) > 10<<20 {
		return nil, fmt.Errorf("xlsx file is empty or too large")
	}
	archive, err := zip.NewReader(bytes.NewReader(data), int64(len(data)))
	if err != nil {
		return nil, fmt.Errorf("open xlsx: %w", err)
	}
	files := make(map[string]*zip.File, len(archive.File))
	for _, file := range archive.File {
		files[file.Name] = file
	}
	sheets, err := workbookSheets(files)
	if err != nil {
		return nil, err
	}
	if len(sheets) == 0 || len(sheets) > 256 {
		return nil, fmt.Errorf("xlsx worksheet count is out of range")
	}

	var shared []string
	var total uint64
	if file := files["xl/sharedStrings.xml"]; file != nil {
		part, err := readBoundedPart(file, partLimit)
		if err != nil {
			return nil, err
		}
		total = uint64(len(part))
		shared, err = parseSharedStrings(part)
		if err != nil {
			return nil, err
		}
	}
	result := make([]Worksheet, 0, len(sheets))
	for _, sheet := range sheets {
		file := files[sheet.path]
		if file == nil {
			return nil, fmt.Errorf("xlsx sheet %q is missing", sheet.name)
		}
		if file.UncompressedSize64 > workbookLimit-total {
			return nil, fmt.Errorf("xlsx workbook data is too large")
		}
		part, err := readBoundedPart(file, partLimit)
		if err != nil {
			return nil, fmt.Errorf("read xlsx sheet %q: %w", sheet.name, err)
		}
		total += uint64(len(part))
		if total > workbookLimit {
			return nil, fmt.Errorf("xlsx workbook data is too large")
		}
		var doc worksheet
		if err := xml.Unmarshal(part, &doc); err != nil {
			return nil, fmt.Errorf("read xlsx sheet %q: %w", sheet.name, err)
		}
		rows := make(map[int]map[int]string, len(doc.Rows))
		rowNumber := 0
		for _, row := range doc.Rows {
			rowNumber++
			if row.Number > 0 {
				rowNumber = row.Number
			}
			if rowNumber > 1048576 {
				return nil, fmt.Errorf("xlsx row is out of range")
			}
			cells := make(map[int]string)
			for _, cell := range row.Cells {
				col, ok := columnIndex(cell.Ref)
				if !ok || col >= 16384 {
					return nil, fmt.Errorf("xlsx cell reference is invalid")
				}
				value, err := decodeCell(cell, shared)
				if err != nil {
					return nil, fmt.Errorf("read xlsx sheet %q cell %s: %w", sheet.name, cell.Ref, err)
				}
				if strings.TrimSpace(value) != "" {
					cells[col+1] = value
				}
			}
			if len(cells) > 0 {
				rows[rowNumber] = cells
			}
		}
		result = append(result, Worksheet{Name: sheet.name, Rows: rows})
	}
	return result, nil
}

type xlsxSheet struct{ name, path string }

func workbookSheets(files map[string]*zip.File) ([]xlsxSheet, error) {
	workbook := files["xl/workbook.xml"]
	rels := files["xl/_rels/workbook.xml.rels"]
	if workbook == nil || rels == nil {
		return nil, fmt.Errorf("xlsx workbook relationships are missing")
	}
	read := func(file *zip.File) ([]byte, error) { return readBoundedPart(file, 16<<20) }
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
