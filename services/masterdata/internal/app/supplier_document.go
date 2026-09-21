package app

import (
	"bytes"
	"context"
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"io"
	"net/http"
	"path"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/sgao19/erp-go/pkg/apierr"
)

type supplierAccessKey struct{}

// WithSupplierAccess receives the current employee from the authenticated RPC
// boundary. Zero is reserved for the existing highest system role.
func WithSupplierAccess(ctx context.Context, employeeID int64) context.Context {
	return context.WithValue(ctx, supplierAccessKey{}, employeeID)
}

func supplierAccessEmployee(ctx context.Context) int64 {
	id, _ := ctx.Value(supplierAccessKey{}).(int64)
	return id
}

type SupplierDocument struct {
	ID, SupplierID, DocumentID, SizeBytes                                                    int64
	Version, RemindDays                                                                      int32
	Title, Remark, FileName, ContentType, ExpiresOn, UploadedByName, CreatedAt, SupplierName string
	ReminderEnabled, Current, Unread                                                         bool
}

type SupplierDocumentInput struct {
	SupplierID, ReplacesID, OperatorID               int64
	Title, Remark, FileName, ExpiresOn, OperatorName string
	Content                                          []byte
	RemindDays                                       int32
	ReminderEnabled                                  bool
}

const supplierDocumentOwnerPredicate = `($3::bigint=0 OR EXISTS(SELECT 1 FROM supplier_owners o
 WHERE o.tenant_id=d.tenant_id AND o.supplier_id=d.supplier_id AND o.employee_id=$3 AND o.status='ACTIVE'
 AND (o.start_date IS NULL OR o.start_date<=CURRENT_DATE) AND (o.end_date IS NULL OR o.end_date>=CURRENT_DATE)))`
const supplierDocumentCurrentPredicate = `NOT EXISTS(SELECT 1 FROM supplier_documents newer WHERE newer.tenant_id=d.tenant_id AND newer.document_id=d.document_id AND newer.version>d.version AND newer.deleted_at IS NULL)`

func (s *Service) supplierExists(ctx context.Context, tenantID, supplierID int64) error {
	var id int64
	err := s.pool.QueryRow(ctx, `SELECT id FROM suppliers WHERE tenant_id=$1 AND id=$2`, tenantID, supplierID).Scan(&id)
	if err == pgx.ErrNoRows {
		return apierr.NotFound("MD_SUPPLIER_NOT_FOUND", "供应商不存在")
	}
	return err
}

func (s *Service) ListSupplierDocuments(ctx context.Context, tenantID, supplierID, employeeID int64, reminders bool) ([]SupplierDocument, error) {
	if supplierID > 0 {
		if err := s.supplierExists(ctx, tenantID, supplierID); err != nil {
			return nil, err
		}
	}
	rows, err := s.pool.Query(ctx, `SELECT d.id,d.supplier_id,d.document_id,d.version,d.title,d.remark,d.file_name,d.content_type,d.size_bytes,
 COALESCE(d.expires_on::text,''),d.remind_days,d.reminder_enabled,d.uploaded_by_name,d.created_at,COALESCE(NULLIF(s.name_zh,''),NULLIF(s.name_en,''),s.name),
 `+supplierDocumentCurrentPredicate+`, NOT EXISTS(SELECT 1 FROM supplier_document_reads rr WHERE rr.tenant_id=d.tenant_id AND rr.revision_id=d.id AND rr.employee_id=$4)
 FROM supplier_documents d JOIN suppliers s ON s.tenant_id=d.tenant_id AND s.id=d.supplier_id
 WHERE d.deleted_at IS NULL AND d.tenant_id=$1 AND ($2::bigint=0 OR d.supplier_id=$2)
 AND (NOT $5::boolean OR (`+supplierDocumentOwnerPredicate+` AND s.status='ACTIVE' AND d.reminder_enabled AND d.expires_on IS NOT NULL
 AND d.expires_on-d.remind_days<=(CURRENT_TIMESTAMP AT TIME ZONE 'UTC')::date AND `+supplierDocumentCurrentPredicate+`))
 ORDER BY d.document_id DESC,d.version DESC`, tenantID, supplierID, supplierAccessEmployee(ctx), employeeID, reminders)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []SupplierDocument{}
	for rows.Next() {
		var d SupplierDocument
		var created time.Time
		if err = rows.Scan(&d.ID, &d.SupplierID, &d.DocumentID, &d.Version, &d.Title, &d.Remark, &d.FileName, &d.ContentType, &d.SizeBytes, &d.ExpiresOn, &d.RemindDays, &d.ReminderEnabled, &d.UploadedByName, &created, &d.SupplierName, &d.Current, &d.Unread); err != nil {
			return nil, err
		}
		d.CreatedAt = created.UTC().Format(time.RFC3339)
		out = append(out, d)
	}
	return out, rows.Err()
}

func validateSupplierDocument(in SupplierDocumentInput) error {
	if in.SupplierID <= 0 || in.OperatorID <= 0 || strings.TrimSpace(in.Title) == "" || len(in.Title) > 300 || len(in.Remark) > 5000 || in.RemindDays < 0 || in.RemindDays > 3650 {
		return apierr.Invalid("MD_SUPPLIER_DOCUMENT_FIELDS_INVALID", "资料名称必填，提醒天数无效")
	}
	if in.ExpiresOn != "" {
		if _, err := time.Parse("2006-01-02", in.ExpiresOn); err != nil {
			return apierr.Invalid("MD_SUPPLIER_DOCUMENT_DATE_INVALID", "提醒日期无效")
		}
	}
	if len(in.Content) > CustomerDocumentMaxBytes {
		return apierr.Invalid("MD_SUPPLIER_DOCUMENT_TOO_LARGE", "文件不能超过 10 MB")
	}
	if len(in.Content) > 0 && strings.TrimSpace(in.FileName) == "" {
		return apierr.Invalid("MD_SUPPLIER_DOCUMENT_FILE_NAME_REQUIRED", "文件名必填")
	}
	if len(in.Content) == 0 && in.ReplacesID == 0 {
		return apierr.Invalid("MD_SUPPLIER_DOCUMENT_FILE_REQUIRED", "请选择文件")
	}
	return nil
}

func (s *Service) SaveSupplierDocument(ctx context.Context, tenantID int64, in SupplierDocumentInput) (int64, error) {
	if err := validateSupplierDocument(in); err != nil {
		return 0, err
	}
	files := s.files
	if files == nil {
		return 0, apierr.Conflict("MD_FILES_UNAVAILABLE", "文件存储未配置")
	}
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return 0, err
	}
	defer func() { _ = tx.Rollback(ctx) }()
	var exists int64
	if err = tx.QueryRow(ctx, `SELECT id FROM suppliers WHERE tenant_id=$1 AND id=$2 FOR UPDATE`, tenantID, in.SupplierID).Scan(&exists); err != nil {
		if err == pgx.ErrNoRows {
			return 0, apierr.NotFound("MD_SUPPLIER_NOT_FOUND", "供应商不存在")
		}
		return 0, err
	}
	var id, documentID, size int64
	version := int32(1)
	key, fileName, contentType := "", "", ""
	if err = tx.QueryRow(ctx, `SELECT nextval('supplier_documents_id_seq')`).Scan(&id); err != nil {
		return 0, err
	}
	documentID = id
	if in.ReplacesID > 0 {
		err = tx.QueryRow(ctx, `SELECT document_id,version,file_key,file_name,content_type,size_bytes FROM supplier_documents d WHERE deleted_at IS NULL AND tenant_id=$1 AND supplier_id=$2 AND id=$3 AND `+supplierDocumentCurrentPredicate, tenantID, in.SupplierID, in.ReplacesID).Scan(&documentID, &version, &key, &fileName, &contentType, &size)
		if err == pgx.ErrNoRows {
			return 0, apierr.Conflict("MD_SUPPLIER_DOCUMENT_VERSION_CHANGED", "资料已更新，请刷新后重试")
		}
		if err != nil {
			return 0, err
		}
		version++
	}
	uploaded := false
	if len(in.Content) > 0 {
		fileName = path.Base(strings.ReplaceAll(in.FileName, `\`, "/"))
		contentType = http.DetectContentType(in.Content)
		size = int64(len(in.Content))
		token := make([]byte, 16)
		if _, err = rand.Read(token); err != nil {
			return 0, err
		}
		key = fmt.Sprintf("supplier-documents/%d/%d/%s", tenantID, in.SupplierID, hex.EncodeToString(token))
		if _, err = files.Put(ctx, key, bytes.NewReader(in.Content), size, contentType); err != nil {
			return 0, err
		}
		uploaded = true
	}
	_, err = tx.Exec(ctx, `INSERT INTO supplier_documents(id,tenant_id,supplier_id,document_id,version,title,remark,file_key,file_name,content_type,size_bytes,expires_on,remind_days,reminder_enabled,uploaded_by,uploaded_by_name) VALUES($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,NULLIF($12,'')::date,$13,$14,$15,$16)`, id, tenantID, in.SupplierID, documentID, version, strings.TrimSpace(in.Title), in.Remark, key, fileName, contentType, size, in.ExpiresOn, in.RemindDays, in.ReminderEnabled, in.OperatorID, in.OperatorName)
	if err != nil {
		if uploaded {
			_ = files.Remove(ctx, key)
		}
		return 0, err
	}
	if err = tx.Commit(ctx); err != nil {
		return 0, err
	}
	return id, nil
}

func (s *Service) GetSupplierDocumentFile(ctx context.Context, tenantID, supplierID, revisionID int64) (string, string, []byte, error) {
	if err := s.supplierExists(ctx, tenantID, supplierID); err != nil {
		return "", "", nil, err
	}
	var key, name, kind string
	err := s.pool.QueryRow(ctx, `SELECT file_key,file_name,content_type FROM supplier_documents WHERE deleted_at IS NULL AND tenant_id=$1 AND supplier_id=$2 AND id=$3`, tenantID, supplierID, revisionID).Scan(&key, &name, &kind)
	if err == pgx.ErrNoRows {
		return "", "", nil, apierr.NotFound("MD_SUPPLIER_DOCUMENT_NOT_FOUND", "资料不存在")
	}
	if err != nil {
		return "", "", nil, err
	}
	files := s.files
	if files == nil {
		return "", "", nil, apierr.Conflict("MD_FILES_UNAVAILABLE", "文件存储未配置")
	}
	reader, err := files.Get(ctx, key)
	if err != nil {
		return "", "", nil, err
	}
	defer func() { _ = reader.Close() }()
	data, err := io.ReadAll(io.LimitReader(reader, CustomerDocumentMaxBytes+1))
	if err == nil && len(data) > CustomerDocumentMaxBytes {
		return "", "", nil, apierr.Invalid("MD_SUPPLIER_DOCUMENT_TOO_LARGE", "文件不能超过 10 MB")
	}
	return name, kind, data, err
}

func (s *Service) DeleteSupplierDocument(ctx context.Context, tenantID, supplierID, revisionID int64) error {
	if err := s.supplierExists(ctx, tenantID, supplierID); err != nil {
		return err
	}
	tag, err := s.pool.Exec(ctx, `UPDATE supplier_documents SET deleted_at=now() WHERE tenant_id=$1 AND supplier_id=$2 AND deleted_at IS NULL AND document_id=(SELECT document_id FROM supplier_documents WHERE tenant_id=$1 AND supplier_id=$2 AND id=$3)`, tenantID, supplierID, revisionID)
	if err != nil {
		return err
	}
	if tag.RowsAffected() == 0 {
		return apierr.NotFound("MD_SUPPLIER_DOCUMENT_NOT_FOUND", "附件不存在或已删除")
	}
	return nil
}

func (s *Service) MarkSupplierDocumentRemindersRead(ctx context.Context, tenantID, employeeID int64, ids []int64) (int64, error) {
	tag, err := s.pool.Exec(ctx, `INSERT INTO supplier_document_reads(tenant_id,revision_id,employee_id) SELECT d.tenant_id,d.id,$2 FROM supplier_documents d JOIN suppliers s ON s.tenant_id=d.tenant_id AND s.id=d.supplier_id WHERE d.deleted_at IS NULL AND d.tenant_id=$1 AND `+supplierDocumentOwnerPredicate+` AND (cardinality($4::bigint[])=0 OR d.id=ANY($4::bigint[])) AND s.status='ACTIVE' AND d.reminder_enabled AND d.expires_on-d.remind_days<=(CURRENT_TIMESTAMP AT TIME ZONE 'UTC')::date AND `+supplierDocumentCurrentPredicate+` ON CONFLICT DO NOTHING`, tenantID, employeeID, supplierAccessEmployee(ctx), ids)
	return tag.RowsAffected(), err
}
