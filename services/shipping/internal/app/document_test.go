package app

import (
	"context"
	"testing"
)

type documentFilesStub struct {
	size        int64
	contentType string
	data        []byte
}

func (f documentFilesStub) PresignPut(context.Context, string) (string, int32, error) {
	return "http://upload.example", 600, nil
}
func (f documentFilesStub) PresignDownload(context.Context, string, string) (string, error) {
	return "http://download.example", nil
}
func (f documentFilesStub) PresignPreview(context.Context, string, string) (string, error) {
	return "http://preview.example", nil
}
func (f documentFilesStub) Stat(context.Context, string) (int64, string, error) {
	return f.size, f.contentType, nil
}
func (f documentFilesStub) ReadFile(context.Context, string, int64) ([]byte, error) {
	if f.data != nil {
		return f.data, nil
	}
	data := validTestPDF()
	if f.size > int64(len(data)) {
		data = append(data, make([]byte, int(f.size)-len(data))...)
		for i := len(validTestPDF()); i < len(data); i++ {
			data[i] = ' '
		}
	}
	return data, nil
}
func (documentFilesStub) Remove(context.Context, string) error { return nil }

func TestDocumentValidation(t *testing.T) {
	for _, name := range []string{"contract.pdf", "装箱单.xlsx", "scan.JPG"} {
		if _, err := cleanDocumentName(name); err != nil {
			t.Fatalf("%q should be accepted: %v", name, err)
		}
	}
	for _, name := range []string{"", "virus.exe", "page.html"} {
		if _, err := cleanDocumentName(name); errorCode(err) != "SHIPPING_DOCUMENT_TYPE_INVALID" && errorCode(err) != "SHIPPING_DOCUMENT_NAME_INVALID" {
			t.Fatalf("%q code=%q err=%v", name, errorCode(err), err)
		}
	}
	if _, err := validateDocumentCategory("PACKING_LIST"); err != nil {
		t.Fatalf("valid category: %v", err)
	}
	if _, err := validateDocumentCategory("SECRET"); errorCode(err) != "SHIPPING_DOCUMENT_CATEGORY_INVALID" {
		t.Fatalf("invalid category code=%q", errorCode(err))
	}
}

func TestDocumentStorageRequired(t *testing.T) {
	if err := New(pingerStub{}).requireDocumentStorage(); errorCode(err) != "SHIPPING_DOCUMENT_STORAGE_UNAVAILABLE" {
		t.Fatalf("code=%q err=%v", errorCode(err), err)
	}
}

func TestDocumentContentTypes(t *testing.T) {
	if !allowedDocumentContentTypes["application/pdf"] || !allowedDocumentContentTypes["application/vnd.openxmlformats-officedocument.spreadsheetml.sheet"] {
		t.Fatal("expected document content types are missing")
	}
	if allowedDocumentContentTypes["text/html"] || allowedDocumentContentTypes["application/x-msdownload"] {
		t.Fatal("executable/renderable unsafe types must not be allowed")
	}
}

func validTestPDF() []byte {
	return []byte("%PDF-1.7\n1 0 obj\n<< /Type /Catalog >>\nendobj\nxref\n0 1\n0000000000 65535 f \ntrailer\n<< /Root 1 0 R >>\nstartxref\n45\n%%EOF")
}

func TestDocumentContentValidation(t *testing.T) {
	if err := validateDocumentContent("file.pdf", validTestPDF()); err != nil {
		t.Fatalf("valid PDF should pass: %v", err)
	}
	if err := validateDocumentContent("fake.pdf", []byte("%PDF-1.4\n%%EOF")); errorCode(err) != "SHIPPING_DOCUMENT_CONTENT_INVALID" {
		t.Fatalf("fake PDF code=%q err=%v", errorCode(err), err)
	}
}
