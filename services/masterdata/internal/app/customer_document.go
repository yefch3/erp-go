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

const CustomerDocumentMaxBytes = 10 << 20

type documentFiles interface {
	Put(context.Context, string, io.Reader, int64, string) (string, error)
	Get(context.Context, string) (io.ReadCloser, error)
	Remove(context.Context, string) error
}

type CustomerDocument struct {
	ID, CustomerID, DocumentID, SizeBytes                                                    int64
	Version, RemindDays                                                                      int32
	Title, Remark, FileName, ContentType, ExpiresOn, UploadedByName, CreatedAt, CustomerName string
	ReminderEnabled, Current, Unread                                                         bool
}

type CustomerDocumentInput struct {
	CustomerID, ReplacesID, OperatorID               int64
	Title, Remark, FileName, ExpiresOn, OperatorName string
	Content                                          []byte
	RemindDays                                       int32
	ReminderEnabled                                  bool
}

// The scope is always derived from IAM at the RPC boundary, never from request fields.
const documentOwnerPredicate = `($3::bigint=0 OR EXISTS(SELECT 1 FROM customer_owners o
 WHERE o.tenant_id=d.tenant_id AND o.customer_id=d.customer_id AND o.employee_id=$3 AND o.status='ACTIVE'
 AND (o.start_date IS NULL OR o.start_date<=CURRENT_DATE) AND (o.end_date IS NULL OR o.end_date>=CURRENT_DATE)))`
const documentCurrentPredicate = `NOT EXISTS(SELECT 1 FROM customer_documents newer WHERE newer.tenant_id=d.tenant_id AND newer.document_id=d.document_id AND newer.version>d.version)`

func (s *Service) ListCustomerDocuments(ctx context.Context, tenantID, customerID, employeeID int64, reminders bool) ([]CustomerDocument, error) {
	if customerID > 0 {
		if err := s.AuthorizeCustomer(ctx, tenantID, customerID); err != nil {
			return nil, err
		}
	}
	rows, err := s.pool.Query(ctx, `SELECT d.id,d.customer_id,d.document_id,d.version,d.title,d.remark,d.file_name,d.content_type,d.size_bytes,
 COALESCE(d.expires_on::text,''),d.remind_days,d.reminder_enabled,d.uploaded_by_name,d.created_at,c.name,
 `+documentCurrentPredicate+`, NOT EXISTS(SELECT 1 FROM customer_document_reads rr WHERE rr.tenant_id=d.tenant_id AND rr.revision_id=d.id AND rr.employee_id=$4)
 FROM customer_documents d JOIN customers c ON c.tenant_id=d.tenant_id AND c.id=d.customer_id
 WHERE d.deleted_at IS NULL AND d.tenant_id=$1 AND ($2::bigint=0 OR d.customer_id=$2) AND `+documentOwnerPredicate+`
 AND (NOT $5::boolean OR (c.status='ACTIVE' AND d.reminder_enabled AND d.expires_on IS NOT NULL AND d.expires_on-d.remind_days<=(CURRENT_TIMESTAMP AT TIME ZONE 'UTC')::date AND `+documentCurrentPredicate+`))
 ORDER BY d.document_id DESC,d.version DESC`, tenantID, customerID, customerAccessEmployee(ctx), employeeID, reminders)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []CustomerDocument{}
	for rows.Next() {
		var d CustomerDocument
		var created time.Time
		err = rows.Scan(&d.ID, &d.CustomerID, &d.DocumentID, &d.Version, &d.Title, &d.Remark, &d.FileName, &d.ContentType, &d.SizeBytes, &d.ExpiresOn, &d.RemindDays, &d.ReminderEnabled, &d.UploadedByName, &created, &d.CustomerName, &d.Current, &d.Unread)
		if err != nil {
			return nil, err
		}
		d.CreatedAt = created.UTC().Format(time.RFC3339)
		out = append(out, d)
	}
	return out, rows.Err()
}

func validateCustomerDocument(in CustomerDocumentInput) error {
	if in.CustomerID <= 0 || in.OperatorID <= 0 || strings.TrimSpace(in.Title) == "" || len(in.Title) > 300 || len(in.Remark) > 5000 || in.RemindDays < 0 || in.RemindDays > 3650 {
		return apierr.Invalid("MD_DOCUMENT_FIELDS_INVALID", "资料名称必填，提前提醒天数需在 0 至 3650 之间")
	}
	if in.ExpiresOn != "" {
		if _, err := time.Parse("2006-01-02", in.ExpiresOn); err != nil {
			return apierr.Invalid("MD_DOCUMENT_DATE_INVALID", "到期日期无效")
		}
	}
	if len(in.Content) > CustomerDocumentMaxBytes {
		return apierr.Invalid("MD_DOCUMENT_TOO_LARGE", "文件不能超过 10 MB")
	}
	if len(in.Content) == 0 && in.ReplacesID == 0 {
		return apierr.Invalid("MD_DOCUMENT_FILE_REQUIRED", "请选择文件")
	}
	if len(in.Content) > 0 && (strings.TrimSpace(in.FileName) == "" || len(in.FileName) > 255) {
		return apierr.Invalid("MD_DOCUMENT_NAME_INVALID", "文件名无效")
	}
	return nil
}

func (s *Service) SaveCustomerDocument(ctx context.Context, tenantID int64, in CustomerDocumentInput) (int64, error) {
	if err := validateCustomerDocument(in); err != nil {
		return 0, err
	}
	if err := s.AuthorizeCustomer(ctx, tenantID, in.CustomerID); err != nil {
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
	// Serialize revisions for a customer and reject replacements based on a stale version.
	var exists int64
	if err = tx.QueryRow(ctx, `SELECT id FROM customers WHERE tenant_id=$1 AND id=$2 FOR UPDATE`, tenantID, in.CustomerID).Scan(&exists); err != nil {
		if err == pgx.ErrNoRows {
			return 0, apierr.NotFound("MD_CUSTOMER_NOT_FOUND", "客户不存在")
		}
		return 0, err
	}
	var id, documentID, size int64
	version := int32(1)
	key, fileName, contentType := "", "", ""
	if err = tx.QueryRow(ctx, `SELECT nextval('customer_documents_id_seq')`).Scan(&id); err != nil {
		return 0, err
	}
	documentID = id
	if in.ReplacesID > 0 {
		err = tx.QueryRow(ctx, `SELECT d.document_id,d.version,d.file_key,d.file_name,d.content_type,d.size_bytes FROM customer_documents d WHERE d.deleted_at IS NULL AND d.tenant_id=$1 AND d.customer_id=$2 AND d.id=$3 AND `+documentCurrentPredicate, tenantID, in.CustomerID, in.ReplacesID).Scan(&documentID, &version, &key, &fileName, &contentType, &size)
		if err == pgx.ErrNoRows {
			return 0, apierr.Conflict("MD_DOCUMENT_VERSION_CHANGED", "资料已更新，请刷新后重试")
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
		key = fmt.Sprintf("customer-documents/%d/%d/%s", tenantID, in.CustomerID, hex.EncodeToString(token))
		if _, err = files.Put(ctx, key, bytes.NewReader(in.Content), size, contentType); err != nil {
			return 0, err
		}
		uploaded = true
	}
	_, err = tx.Exec(ctx, `INSERT INTO customer_documents(id,tenant_id,customer_id,document_id,version,title,remark,file_key,file_name,content_type,size_bytes,expires_on,remind_days,reminder_enabled,uploaded_by,uploaded_by_name)
 VALUES($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,NULLIF($12,'')::date,$13,$14,$15,$16)`, id, tenantID, in.CustomerID, documentID, version, strings.TrimSpace(in.Title), in.Remark, key, fileName, contentType, size, in.ExpiresOn, in.RemindDays, in.ReminderEnabled, in.OperatorID, in.OperatorName)
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

func (s *Service) GetCustomerDocumentFile(ctx context.Context, tenantID, customerID, revisionID int64) (string, string, []byte, error) {
	if err := s.AuthorizeCustomer(ctx, tenantID, customerID); err != nil {
		return "", "", nil, err
	}
	var key, name, contentType string
	err := s.pool.QueryRow(ctx, `SELECT file_key,file_name,content_type FROM customer_documents WHERE deleted_at IS NULL AND tenant_id=$1 AND customer_id=$2 AND id=$3`, tenantID, customerID, revisionID).Scan(&key, &name, &contentType)
	if err == pgx.ErrNoRows {
		return "", "", nil, apierr.NotFound("MD_DOCUMENT_NOT_FOUND", "资料不存在")
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
	defer reader.Close()
	data, err := io.ReadAll(io.LimitReader(reader, CustomerDocumentMaxBytes+1))
	if err != nil {
		return "", "", nil, err
	}
	if len(data) > CustomerDocumentMaxBytes {
		return "", "", nil, apierr.Invalid("MD_DOCUMENT_TOO_LARGE", "文件超过大小限制")
	}
	return name, contentType, data, nil
}

func (s *Service) MarkCustomerDocumentRemindersRead(ctx context.Context, tenantID, employeeID int64, ids []int64) (int64, error) {
	tag, err := s.pool.Exec(ctx, `INSERT INTO customer_document_reads(tenant_id,revision_id,employee_id)
 SELECT d.tenant_id,d.id,$2 FROM customer_documents d JOIN customers c ON c.tenant_id=d.tenant_id AND c.id=d.customer_id
 WHERE d.deleted_at IS NULL AND d.tenant_id=$1 AND `+documentOwnerPredicate+` AND (cardinality($4::bigint[])=0 OR d.id=ANY($4::bigint[]))
 AND c.status='ACTIVE' AND d.reminder_enabled AND d.expires_on-d.remind_days<=(CURRENT_TIMESTAMP AT TIME ZONE 'UTC')::date AND `+documentCurrentPredicate+`
 ON CONFLICT DO NOTHING`, tenantID, employeeID, customerAccessEmployee(ctx), ids)
	return tag.RowsAffected(), err
}

// Deleting an attachment also hides every older revision and stops its reminders.
// Customer access is sufficient; this is not the customer deletion permission.
func (s *Service) DeleteCustomerDocument(ctx context.Context, tenantID, customerID, revisionID int64) error {
	if err := s.AuthorizeCustomer(ctx, tenantID, customerID); err != nil {
		return err
	}
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback(ctx) }()
	var customer int64
	if err = tx.QueryRow(ctx, `SELECT id FROM customers WHERE tenant_id=$1 AND id=$2 FOR UPDATE`, tenantID, customerID).Scan(&customer); err != nil {
		if err == pgx.ErrNoRows {
			return apierr.NotFound("MD_CUSTOMER_NOT_FOUND", "客户不存在")
		}
		return err
	}
	tag, err := tx.Exec(ctx, `UPDATE customer_documents SET deleted_at=now() WHERE tenant_id=$1 AND customer_id=$2 AND deleted_at IS NULL AND document_id=(SELECT document_id FROM customer_documents WHERE tenant_id=$1 AND customer_id=$2 AND id=$3)`, tenantID, customerID, revisionID)
	if err != nil {
		return err
	}
	if tag.RowsAffected() == 0 {
		return apierr.NotFound("MD_DOCUMENT_NOT_FOUND", "附件不存在或已删除")
	}
	return tx.Commit(ctx)
}
