package app

import (
	"context"
	"fmt"
	"path"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/sgao19/erp-go/pkg/apierr"
	"github.com/shopspring/decimal"
)

// OrderContractState is the D4 contract/payment gate layered on the existing
// purchase order. The commercial order itself remains the accepted D1-D3
// record; these fields only control when Finance may see a payment request.
type OrderContractState struct {
	ID, SourceBusinessID                                int64
	BusinessType, ExportContractNo, BusinessDocumentNo  string
	PaymentTerms, SignedContractName, SignedContractURL string
	SignedContractUploadedAt, ContractVerifiedAt        string
	ContractVerifiedByName, PaymentRequestedAt          string
	PaymentRequestedByName                              string
}

type ContractUpload struct {
	Key, URL string
	Expires  int32
}

func contractKeyPrefix(tenantID, orderID int64) string {
	return fmt.Sprintf("d4-contracts/procurement/%d/%d/", tenantID, orderID)
}

func (s *Service) PresignOrderContract(ctx context.Context, tenantID, orderID int64, fileName string) (ContractUpload, error) {
	if s.files == nil {
		return ContractUpload{}, apierr.Conflict("PO_CONTRACT_FILES_UNAVAILABLE", "文件存储未配置")
	}
	if _, err := s.GetOrder(ctx, tenantID, orderID); err != nil {
		return ContractUpload{}, err
	}
	name := strings.TrimSpace(path.Base(strings.ReplaceAll(fileName, "\\", "/")))
	if name == "" || name == "." {
		return ContractUpload{}, apierr.Invalid("PO_CONTRACT_FILE_REQUIRED", "请选择签署合同文件")
	}
	key := fmt.Sprintf("%s%d-%s", contractKeyPrefix(tenantID, orderID), time.Now().UnixNano(), name)
	url, expires, err := s.files.PresignPut(ctx, key)
	return ContractUpload{Key: key, URL: url, Expires: expires}, err
}

func (s *Service) SaveOrderContract(ctx context.Context, tenantID, orderID int64, contractNo, paymentTerms, key, fileName string, op Operator) (OrderContractState, error) {
	contractNo, paymentTerms, key, fileName = strings.TrimSpace(contractNo), strings.TrimSpace(paymentTerms), strings.TrimSpace(key), strings.TrimSpace(fileName)
	if contractNo == "" || paymentTerms == "" || key == "" || fileName == "" {
		return OrderContractState{}, apierr.Invalid("PO_CONTRACT_REQUIRED", "请填写采购合同号、付款条件并上传双方签署合同")
	}
	if !objectKeyBelongsTo(key, contractKeyPrefix(tenantID, orderID)) {
		return OrderContractState{}, apierr.Invalid("PO_CONTRACT_FILE_MISMATCH", "合同文件与采购订单不匹配")
	}
	cmd, err := s.pool.Exec(ctx, `UPDATE purchase_orders SET business_document_no=$3,payment_terms=$4,signed_contract_key=$5,signed_contract_name=$6,signed_contract_uploaded_at=now(),contract_verified_at=null,contract_verified_by=0,contract_verified_by_name='',payment_requested_at=null,updated_at=now() WHERE tenant_id=$1 AND id=$2 AND status='ORDERED' AND payment_requested_at IS NULL`, tenantID, orderID, contractNo, paymentTerms, key, fileName)
	if err != nil {
		return OrderContractState{}, err
	}
	if cmd.RowsAffected() == 0 {
		return OrderContractState{}, apierr.Conflict("PO_CONTRACT_NOT_EDITABLE", "只有审批通过且尚未申请付款的采购订单可以登记签署合同")
	}
	s.nudge(ctx, tenantID)
	return s.OrderContractState(ctx, tenantID, orderID)
}

func (s *Service) VerifyOrderContract(ctx context.Context, tenantID, orderID int64, op Operator) (OrderContractState, error) {
	cmd, err := s.pool.Exec(ctx, `UPDATE purchase_orders SET contract_verified_at=now(),contract_verified_by=$3,contract_verified_by_name=$4,updated_at=now() WHERE tenant_id=$1 AND id=$2 AND status='ORDERED' AND signed_contract_key<>'' AND payment_requested_at IS NULL`, tenantID, orderID, op.ID, op.Name)
	if err != nil {
		return OrderContractState{}, err
	}
	if cmd.RowsAffected() == 0 {
		return OrderContractState{}, apierr.Conflict("PO_CONTRACT_NOT_VERIFIABLE", "请先上传当前工厂双方签署的采购合同")
	}
	s.nudge(ctx, tenantID)
	return s.OrderContractState(ctx, tenantID, orderID)
}

func (s *Service) RequestOrderPayment(ctx context.Context, tenantID, orderID int64, op Operator) (OrderContractState, error) {
	cmd, err := s.pool.Exec(ctx, `UPDATE purchase_orders SET payment_requested_at=now(),payment_requested_by=$3,payment_requested_by_name=$4,updated_at=now() WHERE tenant_id=$1 AND id=$2 AND status='ORDERED' AND contract_verified_at IS NOT NULL AND payment_requested_at IS NULL`, tenantID, orderID, op.ID, op.Name)
	if err != nil {
		return OrderContractState{}, err
	}
	if cmd.RowsAffected() == 0 {
		return OrderContractState{}, apierr.Conflict("PO_PAYMENT_NOT_READY", "采购合同经财务上级或老板核验后才能申请付款")
	}
	s.nudge(ctx, tenantID)
	return s.OrderContractState(ctx, tenantID, orderID)
}

func (s *Service) OrderContractState(ctx context.Context, tenantID, orderID int64) (OrderContractState, error) {
	var v OrderContractState
	var key string
	err := s.pool.QueryRow(ctx, `SELECT id,source_business_id,business_type,export_contract_no,business_document_no,payment_terms,signed_contract_key,signed_contract_name,coalesce(signed_contract_uploaded_at::text,''),coalesce(contract_verified_at::text,''),contract_verified_by_name,coalesce(payment_requested_at::text,''),payment_requested_by_name FROM purchase_orders WHERE tenant_id=$1 AND id=$2`, tenantID, orderID).Scan(&v.ID, &v.SourceBusinessID, &v.BusinessType, &v.ExportContractNo, &v.BusinessDocumentNo, &v.PaymentTerms, &key, &v.SignedContractName, &v.SignedContractUploadedAt, &v.ContractVerifiedAt, &v.ContractVerifiedByName, &v.PaymentRequestedAt, &v.PaymentRequestedByName)
	if err == pgx.ErrNoRows {
		return v, apierr.NotFound("PO_ORDER_NOT_FOUND", "采购单不存在")
	}
	if err == nil && key != "" && s.files != nil {
		v.SignedContractURL, _ = s.files.PresignGet(ctx, key)
	}
	return v, err
}

type ExternalPayableInput struct {
	BusinessType                                                                                                                    string
	SourceBusinessID, PayeeID                                                                                                       int64
	ExportContractNo, BusinessDocumentNo, PayeeName, Currency, Amount, DueDate, PaymentTerms, SignedContractKey, SignedContractName string
}

func (s *Service) CreateExternalPayable(ctx context.Context, tenantID int64, in ExternalPayableInput, op Operator) (SupplierReconRow, error) {
	in.BusinessType = strings.ToUpper(strings.TrimSpace(in.BusinessType))
	in.PayeeName = strings.TrimSpace(in.PayeeName)
	in.Currency = strings.ToUpper(strings.TrimSpace(in.Currency))
	if in.BusinessType != "LOGISTICS" || in.SourceBusinessID == 0 || in.PayeeName == "" || in.BusinessDocumentNo == "" || in.SignedContractKey == "" {
		return SupplierReconRow{}, apierr.Invalid("PR_EXTERNAL_PAYABLE_REQUIRED", "物流付款资料不完整")
	}
	amount, err := decimal.NewFromString(strings.TrimSpace(in.Amount))
	if err != nil || !amount.IsPositive() {
		return SupplierReconRow{}, apierr.Invalid("PR_EXTERNAL_PAYABLE_AMOUNT", "物流付款金额必须大于 0")
	}
	var id int64
	err = s.pool.QueryRow(ctx, `INSERT INTO purchase_orders (tenant_id,po_no,supplier_id,supplier_name,currency,total_amount,status,buyer_id,buyer_name,remark,ordered_at,payable_due_date,business_type,source_business_id,export_contract_no,business_document_no,payment_terms,signed_contract_key,signed_contract_name,signed_contract_uploaded_at,contract_verified_at,contract_verified_by,contract_verified_by_name,payment_requested_at,payment_requested_by,payment_requested_by_name) VALUES ($1,$2,$3,$4,$5,$6,'ORDERED',$7,$8,'D4 物流付款申请',now(),nullif($9,'')::date,'LOGISTICS',$10,$11,$12,$13,$14,$15,now(),now(),$7,$8,now(),$7,$8) ON CONFLICT (tenant_id,business_type,source_business_id) WHERE source_business_id<>0 AND business_type='LOGISTICS' DO UPDATE SET supplier_id=excluded.supplier_id,supplier_name=excluded.supplier_name,currency=excluded.currency,total_amount=excluded.total_amount,payable_due_date=excluded.payable_due_date,export_contract_no=excluded.export_contract_no,business_document_no=excluded.business_document_no,payment_terms=excluded.payment_terms,signed_contract_key=excluded.signed_contract_key,signed_contract_name=excluded.signed_contract_name,payment_requested_at=now(),payment_requested_by=excluded.payment_requested_by,payment_requested_by_name=excluded.payment_requested_by_name WHERE purchase_orders.payment_requested_at IS NULL RETURNING id`, tenantID, in.BusinessDocumentNo, in.PayeeID, in.PayeeName, in.Currency, amount.StringFixed(2), op.ID, op.Name, in.DueDate, in.SourceBusinessID, in.ExportContractNo, in.BusinessDocumentNo, in.PaymentTerms, in.SignedContractKey, in.SignedContractName).Scan(&id)
	if err != nil {
		return SupplierReconRow{}, err
	}
	s.nudge(ctx, tenantID)
	return s.reconRowOf(ctx, tenantID, id)
}

func (s *Service) GetExternalPayable(ctx context.Context, tenantID int64, businessType string, sourceID int64) (SupplierReconRow, error) {
	var id int64
	err := s.pool.QueryRow(ctx, `SELECT id FROM purchase_orders WHERE tenant_id=$1 AND business_type=$2 AND source_business_id=$3`, tenantID, strings.ToUpper(strings.TrimSpace(businessType)), sourceID).Scan(&id)
	if err == pgx.ErrNoRows {
		return SupplierReconRow{}, apierr.NotFound("PR_EXTERNAL_PAYABLE_NOT_FOUND", "付款申请尚未建立")
	}
	if err != nil {
		return SupplierReconRow{}, err
	}
	return s.reconRowOf(ctx, tenantID, id)
}
