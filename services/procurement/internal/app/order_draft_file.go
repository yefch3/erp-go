package app

import (
	"context"
	"strings"

	"github.com/jackc/pgx/v5"
	"github.com/sgao19/erp-go/pkg/apierr"
)

const maxOrderDraftFile = 10 << 20

type OrderDraftFile struct {
	ID, POID, SizeBytes, UploadedByID                 int64
	FileName, ContentType, UploadedByName, UploadedAt string
}

func (s *Service) ListOrderDraftFiles(ctx context.Context, tenantID, poID int64) ([]OrderDraftFile, error) {
	rows, err := s.pool.Query(ctx, `SELECT id,po_id,file_name,content_type,size_bytes,uploaded_by_id,uploaded_by_name,uploaded_at::text
		FROM purchase_order_draft_files WHERE tenant_id=$1 AND po_id=$2 ORDER BY uploaded_at DESC,id DESC`, tenantID, poID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []OrderDraftFile{}
	for rows.Next() {
		var f OrderDraftFile
		if err := rows.Scan(&f.ID, &f.POID, &f.FileName, &f.ContentType, &f.SizeBytes, &f.UploadedByID, &f.UploadedByName, &f.UploadedAt); err != nil {
			return nil, err
		}
		out = append(out, f)
	}
	return out, rows.Err()
}

func (s *Service) UploadOrderDraftFile(ctx context.Context, tenantID, poID int64, name, contentType string, data []byte, op Operator) (OrderDraftFile, error) {
	name = strings.TrimSpace(name)
	if name == "" || len(data) == 0 {
		return OrderDraftFile{}, apierr.Invalid("PO_DRAFT_FILE_REQUIRED", "请选择需要上传的采购资料")
	}
	if len(data) > maxOrderDraftFile {
		return OrderDraftFile{}, apierr.Invalid("PO_DRAFT_FILE_TOO_LARGE", "单个采购资料不能超过 10 MB")
	}
	var status string
	if err := s.pool.QueryRow(ctx, `SELECT status FROM purchase_orders WHERE tenant_id=$1 AND id=$2`, tenantID, poID).Scan(&status); err == pgx.ErrNoRows {
		return OrderDraftFile{}, apierr.NotFound("PO_ORDER_NOT_FOUND", "采购单不存在")
	} else if err != nil {
		return OrderDraftFile{}, err
	}
	meta, metaErr := s.HistoricalOrderMeta(ctx, tenantID, poID)
	if metaErr != nil {
		return OrderDraftFile{}, metaErr
	}
	if status != poDraft && !(meta.Historical && status == poOrdered) {
		return OrderDraftFile{}, apierr.Conflict("PO_DRAFT_FILE_LOCKED", "采购单提交审批后不能修改草稿资料")
	}
	if contentType == "" {
		contentType = "application/octet-stream"
	}
	var id int64
	if err := s.pool.QueryRow(ctx, `INSERT INTO purchase_order_draft_files(tenant_id,po_id,file_name,content_type,size_bytes,file_data,uploaded_by_id,uploaded_by_name)
		VALUES($1,$2,$3,$4,$5,$6,$7,$8) RETURNING id`, tenantID, poID, name, contentType, len(data), data, op.ID, op.Name).Scan(&id); err != nil {
		return OrderDraftFile{}, err
	}
	files, err := s.ListOrderDraftFiles(ctx, tenantID, poID)
	if err != nil {
		return OrderDraftFile{}, err
	}
	for _, f := range files {
		if f.ID == id {
			return f, nil
		}
	}
	return OrderDraftFile{}, apierr.NotFound("PO_DRAFT_FILE_NOT_FOUND", "采购资料不存在")
}

func (s *Service) DownloadOrderDraftFile(ctx context.Context, tenantID, poID, fileID int64) (string, string, []byte, error) {
	var name, contentType string
	var data []byte
	err := s.pool.QueryRow(ctx, `SELECT file_name,content_type,file_data FROM purchase_order_draft_files WHERE tenant_id=$1 AND po_id=$2 AND id=$3`, tenantID, poID, fileID).Scan(&name, &contentType, &data)
	if err == pgx.ErrNoRows {
		return "", "", nil, apierr.NotFound("PO_DRAFT_FILE_NOT_FOUND", "采购资料不存在")
	}
	return name, contentType, data, err
}

func (s *Service) DeleteOrderDraftFile(ctx context.Context, tenantID, poID, fileID int64) error {
	cmd, err := s.pool.Exec(ctx, `DELETE FROM purchase_order_draft_files f USING purchase_orders o
		WHERE f.tenant_id=$1 AND f.po_id=$2 AND f.id=$3 AND o.tenant_id=f.tenant_id AND o.id=f.po_id AND o.status='DRAFT'`, tenantID, poID, fileID)
	if err != nil {
		return err
	}
	if cmd.RowsAffected() == 0 {
		return apierr.NotFound("PO_DRAFT_FILE_NOT_FOUND", "采购资料不存在或订单已提交")
	}
	return nil
}
