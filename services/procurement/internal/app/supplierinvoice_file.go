package app

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"path"
	"strings"

	"github.com/sgao19/erp-go/pkg/apierr"
)

// The scan of the invoice. The invoice row is the claim as typed in; the scan
// is the paper that backs it up when the factory and the buyer disagree about
// what the paper said. One scan per invoice: the column, not a table, because
// the document *is* one sheet — a second upload replaces, it does not append.

// InvoiceFilePresign is a short-lived upload capability.
type InvoiceFilePresign struct {
	Key       string
	UploadURL string
	Expires   int32
}

// PresignSupplierInvoiceFile hands back a key and a URL the browser PUTs the
// scan to. Nothing is recorded yet — an upload that never completes leaves no
// change on the invoice, only an orphan object a later sweep can collect.
func (s *Service) PresignSupplierInvoiceFile(ctx context.Context, tenantID, invoiceID int64, fileName string, op Operator) (InvoiceFilePresign, error) {
	if s.files == nil {
		return InvoiceFilePresign{}, apierr.Conflict("INV_FILES_UNAVAILABLE", "文件存储未配置")
	}
	if strings.TrimSpace(fileName) == "" {
		return InvoiceFilePresign{}, apierr.Invalid("INV_FILE_NAME_REQUIRED", "文件名必填")
	}
	// Same fence as every by-ID invoice operation: out of scope answers
	// "not found", and the existence check rides along.
	if err := s.AuthorizeSupplierInvoice(ctx, tenantID, invoiceID, op); err != nil {
		return InvoiceFilePresign{}, err
	}
	key := invoiceObjectKey(tenantID, invoiceID, fileName)
	url, expires, err := s.files.PresignPut(ctx, key)
	if err != nil {
		return InvoiceFilePresign{}, err
	}
	return InvoiceFilePresign{Key: key, UploadURL: url, Expires: expires}, nil
}

// AttachSupplierInvoiceFile records an object the browser already uploaded.
// Replacing is allowed — a blurry first scan is normal — and the previous
// object is removed best-effort after the row points at the new one: a row
// pointing at nothing is a broken link, an unreferenced object is only litter.
func (s *Service) AttachSupplierInvoiceFile(ctx context.Context, tenantID, invoiceID int64, key string, op Operator) (SupplierInvoice, error) {
	if s.files == nil {
		return SupplierInvoice{}, apierr.Conflict("INV_FILES_UNAVAILABLE", "文件存储未配置")
	}
	key = strings.TrimSpace(key)
	if key == "" {
		return SupplierInvoice{}, apierr.Invalid("INV_FILE_KEY_REQUIRED", "文件标识必填")
	}
	// A client could otherwise register a key presigned for another invoice —
	// or for another tenant's.
	if !strings.HasPrefix(key, fmt.Sprintf("supplier-invoices/%d/%d/", tenantID, invoiceID)) {
		return SupplierInvoice{}, apierr.Invalid("INV_FILE_KEY_MISMATCH", "文件标识与发票不匹配")
	}
	if err := s.AuthorizeSupplierInvoice(ctx, tenantID, invoiceID, op); err != nil {
		return SupplierInvoice{}, err
	}
	var previous string
	err := s.pool.QueryRow(ctx, `
		UPDATE supplier_invoices AS n SET attachment_key = $3
		  FROM supplier_invoices AS o
		 WHERE n.id = o.id AND n.tenant_id = $1 AND n.id = $2
		RETURNING o.attachment_key`,
		tenantID, invoiceID, key).Scan(&previous)
	if err != nil {
		return SupplierInvoice{}, err
	}
	if previous != "" && previous != key {
		// Row first, object second; a removal hiccup must not fail the attach.
		_ = s.files.Remove(ctx, previous)
	}
	s.nudge(ctx, tenantID)
	return s.GetSupplierInvoice(ctx, tenantID, invoiceID)
}

// invoiceObjectKey namespaces by tenant and invoice so one glance at the
// bucket says what an object belongs to, and a random prefix keeps same-named
// re-uploads from colliding while the row still points at the old one.
func invoiceObjectKey(tenantID, invoiceID int64, fileName string) string {
	buf := make([]byte, 8)
	_, _ = rand.Read(buf)
	base := path.Base(strings.ReplaceAll(fileName, "\\", "/"))
	return fmt.Sprintf("supplier-invoices/%d/%d/%s-%s", tenantID, invoiceID, hex.EncodeToString(buf), base)
}

// attachmentDisplayName recovers the original file name from a key minted by
// invoiceObjectKey. The random prefix is hex, so the first '-' is always the
// separator, never part of the name.
func attachmentDisplayName(key string) string {
	if key == "" {
		return ""
	}
	base := path.Base(key)
	if _, name, ok := strings.Cut(base, "-"); ok {
		return name
	}
	return base
}
