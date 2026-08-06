package app

import (
	"archive/zip"
	"bytes"
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"image/jpeg"
	"image/png"
	"path"
	"strings"

	"github.com/jackc/pgx/v5"

	"github.com/sgao19/erp-go/pkg/apierr"
	"github.com/sgao19/erp-go/pkg/pgdb"
	"github.com/sgao19/erp-go/services/shipping/internal/store"
)

const maxDocumentBytes int64 = 20 * 1024 * 1024

// Files is the private object store used by shipping documents. File bytes do
// not pass through the gateway; the application validates the stored object
// before it records any metadata.
type Files interface {
	PresignPut(context.Context, string) (string, int32, error)
	PresignDownload(context.Context, string, string) (string, error)
	PresignPreview(context.Context, string, string) (string, error)
	Stat(context.Context, string) (int64, string, error)
	ReadFile(context.Context, string, int64) ([]byte, error)
	Remove(context.Context, string) error
}

var documentCategories = map[string]bool{
	"EXPORT_CONTRACT": true, "BOOKING_CONFIRMATION": true,
	"COMMERCIAL_INVOICE": true, "PACKING_LIST": true,
	"CUSTOMS_DOCUMENT": true, "BILL_OF_LADING_DRAFT": true,
	"BILL_OF_LADING_FINAL": true, "ARRIVAL_NOTICE": true, "OTHER": true,
}

var allowedDocumentExtensions = map[string]bool{
	".pdf": true, ".doc": true, ".docx": true, ".xls": true, ".xlsx": true,
	".png": true, ".jpg": true, ".jpeg": true,
}

var previewContentTypes = map[string]bool{
	"application/pdf": true, "image/png": true, "image/jpeg": true,
}

var allowedDocumentContentTypes = map[string]bool{
	"application/pdf": true, "application/msword": true,
	"application/vnd.openxmlformats-officedocument.wordprocessingml.document": true,
	"application/vnd.ms-excel": true,
	"application/vnd.openxmlformats-officedocument.spreadsheetml.sheet": true,
	"image/png": true, "image/jpeg": true,
	// Some browsers cannot identify older Office files. The extension is still
	// checked independently, and this generic non-renderable type stays safe.
	"application/octet-stream": true,
}

func randomHex(bytes int) string {
	b := make([]byte, bytes)
	_, _ = rand.Read(b)
	return hex.EncodeToString(b)
}

func cleanDocumentName(fileName string) (string, error) {
	name := strings.TrimSpace(path.Base(strings.ReplaceAll(fileName, "\\", "/")))
	if name == "" || name == "." || len([]rune(name)) > 255 {
		return "", apierr.Invalid("SHIPPING_DOCUMENT_NAME_INVALID", "文件名无效或过长")
	}
	if !allowedDocumentExtensions[strings.ToLower(path.Ext(name))] {
		return "", apierr.Invalid("SHIPPING_DOCUMENT_TYPE_INVALID", "仅支持 PDF、Word、Excel、PNG 和 JPG 文件")
	}
	return name, nil
}

// validateDocumentSignature rejects files that merely use an allowed suffix
// or Content-Type while their bytes belong to another format.
func validateDocumentContent(fileName string, data []byte) error {
	ext := strings.ToLower(path.Ext(fileName))
	ole := []byte{0xd0, 0xcf, 0x11, 0xe0, 0xa1, 0xb1, 0x1a, 0xe1}
	valid := false
	switch ext {
	case ".pdf":
		valid = bytes.HasPrefix(data, []byte("%PDF-")) &&
			bytes.Contains(data, []byte("startxref")) &&
			bytes.HasSuffix(bytes.TrimSpace(data), []byte("%%EOF"))
	case ".png":
		_, err := png.DecodeConfig(bytes.NewReader(data))
		valid = err == nil
	case ".jpg", ".jpeg":
		_, err := jpeg.DecodeConfig(bytes.NewReader(data))
		valid = err == nil
	case ".docx", ".xlsx":
		zr, err := zip.NewReader(bytes.NewReader(data), int64(len(data)))
		if err == nil {
			required := "word/document.xml"
			if ext == ".xlsx" {
				required = "xl/workbook.xml"
			}
			hasTypes, hasBody := false, false
			for _, entry := range zr.File {
				hasTypes = hasTypes || entry.Name == "[Content_Types].xml"
				hasBody = hasBody || entry.Name == required
			}
			valid = hasTypes && hasBody
		}
	case ".doc", ".xls":
		valid = bytes.HasPrefix(data, ole)
	}
	if !valid {
		return apierr.Invalid("SHIPPING_DOCUMENT_CONTENT_INVALID", "文件内容与扩展名不匹配，可能已损坏或不是真实的 "+strings.TrimPrefix(strings.ToUpper(ext), ".")+" 文件")
	}
	return nil
}

func validateDocumentCategory(category string) (string, error) {
	category = strings.ToUpper(strings.TrimSpace(category))
	if !documentCategories[category] {
		return "", apierr.Invalid("SHIPPING_DOCUMENT_CATEGORY_INVALID", "单证分类无效")
	}
	return category, nil
}

func (s *Service) requireDocumentStorage() error {
	if s.files == nil {
		return apierr.Invalid("SHIPPING_DOCUMENT_STORAGE_UNAVAILABLE", "文件存储未配置")
	}
	return nil
}

// PresignDocumentUpload checks the schedule and file type before granting a
// short-lived capability to write one object under that schedule's namespace.
func (s *Service) PresignDocumentUpload(ctx context.Context, tenantID, scheduleID int64, fileName string) (string, string, int32, error) {
	if err := s.requireDocumentStorage(); err != nil {
		return "", "", 0, err
	}
	name, err := cleanDocumentName(fileName)
	if err != nil {
		return "", "", 0, err
	}
	if _, err = s.q.GetSchedule(ctx, store.GetScheduleParams{TenantID: tenantID, ID: scheduleID}); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return "", "", 0, apierr.NotFound("SHIPPING_NOT_FOUND", "船期不存在")
		}
		return "", "", 0, err
	}
	key := fmt.Sprintf("shipping-documents/%d/%d/%s-%s", tenantID, scheduleID, randomHex(12), name)
	url, expires, err := s.files.PresignPut(ctx, key)
	return key, url, expires, err
}

// RegisterDocument verifies the bytes already in object storage, then creates
// either a new document series or the next immutable version of an old one.
func (s *Service) RegisterDocument(ctx context.Context, tenantID, scheduleID int64, key, fileName, category, remark string, replacesID int64, op Operator) (store.ShippingDocument, error) {
	if err := s.requireDocumentStorage(); err != nil {
		return store.ShippingDocument{}, err
	}
	name, err := cleanDocumentName(fileName)
	if err != nil {
		return store.ShippingDocument{}, err
	}
	category, err = validateDocumentCategory(category)
	if err != nil {
		return store.ShippingDocument{}, err
	}
	prefix := fmt.Sprintf("shipping-documents/%d/%d/", tenantID, scheduleID)
	if !strings.HasPrefix(key, prefix) {
		return store.ShippingDocument{}, apierr.Invalid("SHIPPING_DOCUMENT_KEY_INVALID", "文件标识与当前船期不匹配")
	}
	size, contentType, err := s.files.Stat(ctx, key)
	if err != nil {
		return store.ShippingDocument{}, apierr.Invalid("SHIPPING_DOCUMENT_NOT_UPLOADED", "文件尚未上传完成")
	}
	if size <= 0 || size > maxDocumentBytes {
		_ = s.files.Remove(ctx, key)
		return store.ShippingDocument{}, apierr.Invalid("SHIPPING_DOCUMENT_SIZE_INVALID", "文件不能为空，且单个文件不能超过 20 MB")
	}
	if contentType == "" {
		contentType = "application/octet-stream"
	}
	contentType = strings.ToLower(strings.TrimSpace(strings.Split(contentType, ";")[0]))
	if !allowedDocumentContentTypes[contentType] {
		_ = s.files.Remove(ctx, key)
		return store.ShippingDocument{}, apierr.Invalid("SHIPPING_DOCUMENT_TYPE_INVALID", "文件内容类型不在允许范围内")
	}
	data, err := s.files.ReadFile(ctx, key, maxDocumentBytes+1)
	if err != nil {
		_ = s.files.Remove(ctx, key)
		return store.ShippingDocument{}, apierr.Invalid("SHIPPING_DOCUMENT_CONTENT_UNREADABLE", "无法读取已上传文件")
	}
	if int64(len(data)) != size {
		_ = s.files.Remove(ctx, key)
		return store.ShippingDocument{}, apierr.Invalid("SHIPPING_DOCUMENT_CONTENT_UNREADABLE", "已上传文件读取不完整")
	}
	if err := validateDocumentContent(name, data); err != nil {
		_ = s.files.Remove(ctx, key)
		return store.ShippingDocument{}, err
	}

	var out store.ShippingDocument
	err = pgdb.InTx(ctx, s.pool, func(ctx context.Context, tx pgx.Tx) error {
		q := s.q.WithTx(tx)
		if _, err := q.GetScheduleForUpdate(ctx, store.GetScheduleForUpdateParams{TenantID: tenantID, ID: scheduleID}); err != nil {
			if errors.Is(err, pgx.ErrNoRows) {
				return apierr.NotFound("SHIPPING_NOT_FOUND", "船期不存在")
			}
			return err
		}
		groupKey, version := randomHex(12), int32(1)
		if replacesID > 0 {
			previous, err := q.GetShippingDocumentForUpdate(ctx, store.GetShippingDocumentForUpdateParams{TenantID: tenantID, ScheduleID: scheduleID, ID: replacesID})
			if err != nil {
				if errors.Is(err, pgx.ErrNoRows) {
					return apierr.NotFound("SHIPPING_DOCUMENT_NOT_FOUND", "要更新版本的单证不存在")
				}
				return err
			}
			groupKey = previous.DocumentGroupKey
			category = previous.Category
			version, err = q.NextShippingDocumentVersion(ctx, store.NextShippingDocumentVersionParams{TenantID: tenantID, ScheduleID: scheduleID, DocumentGroupKey: groupKey})
			if err != nil {
				return err
			}
		}
		out, err = q.CreateShippingDocument(ctx, store.CreateShippingDocumentParams{
			TenantID: tenantID, ScheduleID: scheduleID, DocumentGroupKey: groupKey,
			Category: category, Version: version, FileName: name, FileKey: key,
			FileSize: size, ContentType: contentType, Remark: strings.TrimSpace(remark),
			UploadedBy: op.ID, UploadedByName: op.Name,
		})
		if err != nil {
			return err
		}
		kind := "上传单证"
		if version > 1 {
			kind = "上传单证新版本"
		}
		return addChange(ctx, q, tenantID, scheduleID, "DOCUMENT", "document", "", fmt.Sprintf("%s v%d", name, version), kind, op)
	})
	return out, err
}

func (s *Service) ListDocuments(ctx context.Context, tenantID, scheduleID int64) ([]store.ShippingDocument, error) {
	if _, err := s.q.GetSchedule(ctx, store.GetScheduleParams{TenantID: tenantID, ID: scheduleID}); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, apierr.NotFound("SHIPPING_NOT_FOUND", "船期不存在")
		}
		return nil, err
	}
	return s.q.ListShippingDocuments(ctx, store.ListShippingDocumentsParams{TenantID: tenantID, ScheduleID: scheduleID})
}

// DocumentAccess creates a two-minute URL only after the caller has passed the
// gateway permission check and the document has been matched to this schedule.
func (s *Service) DocumentAccess(ctx context.Context, tenantID, scheduleID, documentID int64, mode string, op Operator) (string, error) {
	if err := s.requireDocumentStorage(); err != nil {
		return "", err
	}
	doc, err := s.q.GetShippingDocument(ctx, store.GetShippingDocumentParams{TenantID: tenantID, ScheduleID: scheduleID, ID: documentID})
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return "", apierr.NotFound("SHIPPING_DOCUMENT_NOT_FOUND", "单证不存在")
		}
		return "", err
	}
	mode = strings.ToUpper(strings.TrimSpace(mode))
	if mode == "PREVIEW" {
		if !previewContentTypes[doc.ContentType] {
			return "", apierr.Invalid("SHIPPING_DOCUMENT_PREVIEW_UNSUPPORTED", "该文件类型不支持在线预览，请下载查看")
		}
		return s.files.PresignPreview(ctx, doc.FileKey, doc.ContentType)
	}
	if mode != "DOWNLOAD" {
		return "", apierr.Invalid("SHIPPING_DOCUMENT_ACCESS_MODE_INVALID", "单证访问方式无效")
	}
	url, err := s.files.PresignDownload(ctx, doc.FileKey, doc.FileName)
	if err != nil {
		return "", err
	}
	if err := addChange(ctx, s.q, tenantID, scheduleID, "DOCUMENT", "document_download", "", doc.FileName, "下载单证", op); err != nil {
		return "", err
	}
	return url, nil
}

// InvalidateDocument preserves both metadata and bytes. This is an auditable
// business reversal, not a delete operation.
func (s *Service) InvalidateDocument(ctx context.Context, tenantID, scheduleID, documentID int64, reason string, op Operator) (store.ShippingDocument, error) {
	reason = strings.TrimSpace(reason)
	if reason == "" {
		return store.ShippingDocument{}, apierr.Invalid("SHIPPING_DOCUMENT_VOID_REASON_REQUIRED", "作废原因必填")
	}
	var out store.ShippingDocument
	err := pgdb.InTx(ctx, s.pool, func(ctx context.Context, tx pgx.Tx) error {
		q := s.q.WithTx(tx)
		current, err := q.GetShippingDocumentForUpdate(ctx, store.GetShippingDocumentForUpdateParams{TenantID: tenantID, ScheduleID: scheduleID, ID: documentID})
		if err != nil {
			if errors.Is(err, pgx.ErrNoRows) {
				return apierr.NotFound("SHIPPING_DOCUMENT_NOT_FOUND", "单证不存在")
			}
			return err
		}
		if current.Status == "VOIDED" {
			return apierr.Conflict("SHIPPING_DOCUMENT_ALREADY_VOIDED", "该单证已经作废")
		}
		out, err = q.InvalidateShippingDocument(ctx, store.InvalidateShippingDocumentParams{
			TenantID: tenantID, ScheduleID: scheduleID, ID: documentID,
			VoidedBy: &op.ID, VoidedByName: &op.Name, VoidReason: &reason,
		})
		if err != nil {
			return err
		}
		return addChange(ctx, q, tenantID, scheduleID, "DOCUMENT", "document_status", "ACTIVE", "VOIDED", current.FileName+"："+reason, op)
	})
	return out, err
}
