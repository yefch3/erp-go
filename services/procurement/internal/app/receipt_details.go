package app

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"net/http"
	"path/filepath"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/sgao19/erp-go/pkg/apierr"
)

type ReceiptProof struct {
	ID, SizeBytes         int64
	FileName, ContentType string
	Data                  []byte
}
type ReceiptDetails struct {
	ActualDeliveryDate, RequestKey string
	Proofs                         []ReceiptProof
}
type SavedReceiptDetails struct {
	ActualDeliveryDate string
	Proofs             []ReceiptProof
}

func prepareReceiptDetails(in ReceiptDetails, warehouseID int64, lines []ReceiptLine, remark string) (ReceiptDetails, string, error) {
	if in.ActualDeliveryDate != "" {
		if _, err := time.Parse("2006-01-02", in.ActualDeliveryDate); err != nil {
			return in, "", apierr.Invalid("PO_DELIVERY_DATE_INVALID", "交接日期无效")
		}
	}
	if len(in.RequestKey) > 100 {
		return in, "", apierr.Invalid("PO_DELIVERY_KEY_INVALID", "交接请求标识过长")
	}
	if len(in.Proofs) > 5 {
		return in, "", apierr.Invalid("PO_DELIVERY_PROOFS_LIMIT", "每次交接最多上传5份凭据")
	}
	total := 0
	for i := range in.Proofs {
		f := &in.Proofs[i]
		f.FileName = filepath.Base(strings.ReplaceAll(strings.TrimSpace(f.FileName), "\\", "/"))
		total += len(f.Data)
		if f.FileName == "" || f.FileName == "." || len(f.Data) == 0 || len(f.Data) > 2<<20 || total > 2<<20 {
			return in, "", apierr.Invalid("PO_DELIVERY_PROOF_INVALID", "凭据不能为空，单个不超过2MB，合计不超过2MB")
		}
		if f.ContentType == "" {
			f.ContentType = http.DetectContentType(f.Data)
		}
	}
	encoded, _ := json.Marshal(struct {
		Warehouse int64
		Lines     []ReceiptLine
		Remark    string
		Details   ReceiptDetails
	}{warehouseID, lines, remark, in})
	sum := sha256.Sum256(encoded)
	return in, hex.EncodeToString(sum[:]), nil
}
func saveReceiptDetails(ctx context.Context, tx pgx.Tx, tenantID, poID, receiptID int64, in ReceiptDetails, hash string) error {
	if in.ActualDeliveryDate == "" && in.RequestKey == "" && len(in.Proofs) == 0 {
		return nil
	}
	_, err := tx.Exec(ctx, `INSERT INTO purchase_receipt_delivery_details(receipt_id,tenant_id,po_id,actual_delivery_date,request_key,request_hash) VALUES($1,$2,$3,coalesce(nullif($4,'')::date,CURRENT_DATE),$5,$6)`, receiptID, tenantID, poID, in.ActualDeliveryDate, in.RequestKey, hash)
	if err != nil {
		return err
	}
	for _, f := range in.Proofs {
		if _, err = tx.Exec(ctx, `INSERT INTO purchase_receipt_proof_files(tenant_id,po_id,receipt_id,file_name,content_type,file_data,size_bytes) VALUES($1,$2,$3,$4,$5,$6,$7)`, tenantID, poID, receiptID, f.FileName, f.ContentType, f.Data, len(f.Data)); err != nil {
			return err
		}
	}
	return nil
}
func (s *Service) ReceiptDetails(ctx context.Context, tenantID, poID int64) (map[int64]SavedReceiptDetails, error) {
	out := map[int64]SavedReceiptDetails{}
	rows, err := s.pool.Query(ctx, `SELECT receipt_id,actual_delivery_date::text FROM purchase_receipt_delivery_details WHERE tenant_id=$1 AND po_id=$2`, tenantID, poID)
	if err != nil {
		return nil, err
	}
	for rows.Next() {
		var id int64
		var d SavedReceiptDetails
		if err = rows.Scan(&id, &d.ActualDeliveryDate); err != nil {
			rows.Close()
			return nil, err
		}
		out[id] = d
	}
	err = rows.Err()
	rows.Close()
	if err != nil {
		return nil, err
	}
	files, err := s.pool.Query(ctx, `SELECT receipt_id,id,file_name,content_type,size_bytes FROM purchase_receipt_proof_files WHERE tenant_id=$1 AND po_id=$2 ORDER BY id`, tenantID, poID)
	if err != nil {
		return nil, err
	}
	defer files.Close()
	for files.Next() {
		var rid int64
		var f ReceiptProof
		if err = files.Scan(&rid, &f.ID, &f.FileName, &f.ContentType, &f.SizeBytes); err != nil {
			return nil, err
		}
		d := out[rid]
		d.Proofs = append(d.Proofs, f)
		out[rid] = d
	}
	return out, files.Err()
}
func (s *Service) DownloadReceiptProof(ctx context.Context, tenantID, poID, fileID int64) (ReceiptProof, error) {
	var f ReceiptProof
	err := s.pool.QueryRow(ctx, `SELECT id,file_name,content_type,file_data,size_bytes FROM purchase_receipt_proof_files WHERE tenant_id=$1 AND po_id=$2 AND id=$3`, tenantID, poID, fileID).Scan(&f.ID, &f.FileName, &f.ContentType, &f.Data, &f.SizeBytes)
	if err == pgx.ErrNoRows {
		return f, apierr.NotFound("PO_RECEIPT_PROOF_NOT_FOUND", "交接凭据不存在")
	}
	return f, err
}
