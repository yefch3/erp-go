package app

import (
	"context"
	"encoding/json"
	"fmt"
	"path"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/sgao19/erp-go/pkg/apierr"
	"github.com/shopspring/decimal"
)

const BizTypeShippingRequote = "SHIPPING_REQUOTE"

type FinalRequoteInput struct {
	FinalForwarderID, ActualCarrierID                                                                                                                   int64
	FinalForwarderName, ActualCarrierName, FinalServiceOption, FinalCurrency, FinalFreightAmount, FinalETD, FinalETA, PaymentTerms, ForwarderContractNo string
}
type ContractUpload struct {
	Key, URL string
	Expires  int32
}
type ContractHandoff struct {
	ID, ContractID, ContractVersionID, CustomerID, ScheduleID                                                                                                                                                                                                                                                                   int64
	VersionNo, BatchNo                                                                                                                                                                                                                                                                                                          int32
	ContractNo, CustomerName, ShipmentGroupKey, CarrierForwarder, ServiceOptionName                                                                                                                                                                                                                                             string
	CustomerManaged                                                                                                                                                                                                                                                                                                             bool
	Currency, FreightAmount, ChargeBasis, PortOfLoading, PortOfDischarge, EstimatedDeparture, EstimatedArrival, ValidUntil, Remark, Status, CreatedAt                                                                                                                                                                           string
	FinalForwarderID, ActualCarrierID, ApprovalInstanceID                                                                                                                                                                                                                                                                       int64
	FinalForwarderName, ActualCarrierName, FinalServiceOption, FinalCurrency, FinalFreightAmount, FinalETD, FinalETA, PaymentTerms, ForwarderContractNo, ReturnReason, OperatorName, SignedContractName, SignedContractURL, SignedContractUploadedAt, ContractVerifiedAt, ContractVerifiedByName, PaymentRequestedAt, UpdatedAt string
	SignedContractKey                                                                                                                                                                                                                                                                                                           string
}

const handoffSelect = `SELECT id,contract_id,contract_no,contract_version_id,version_no,customer_id,customer_name,batch_no,shipment_group_key,carrier_forwarder,service_option_name,customer_managed,currency,freight_amount::text,charge_basis,port_of_loading,port_of_discharge,coalesce(estimated_departure::text,''),coalesce(estimated_arrival::text,''),coalesce(valid_until::text,''),remark,status,coalesce(schedule_id,0),created_at::text,final_forwarder_id,final_forwarder_name,actual_carrier_id,actual_carrier_name,final_service_option,final_currency,final_freight_amount::text,coalesce(final_etd::text,''),coalesce(final_eta::text,''),payment_terms,forwarder_contract_no,coalesce(approval_instance_id,0),return_reason,operator_name,signed_contract_key,signed_contract_name,coalesce(signed_contract_uploaded_at::text,''),coalesce(contract_verified_at::text,''),contract_verified_by_name,coalesce(payment_requested_at::text,''),updated_at::text FROM contract_shipping_handoffs`

func scanHandoff(row pgx.Row) (ContractHandoff, error) {
	var v ContractHandoff
	err := row.Scan(&v.ID, &v.ContractID, &v.ContractNo, &v.ContractVersionID, &v.VersionNo, &v.CustomerID, &v.CustomerName, &v.BatchNo, &v.ShipmentGroupKey, &v.CarrierForwarder, &v.ServiceOptionName, &v.CustomerManaged, &v.Currency, &v.FreightAmount, &v.ChargeBasis, &v.PortOfLoading, &v.PortOfDischarge, &v.EstimatedDeparture, &v.EstimatedArrival, &v.ValidUntil, &v.Remark, &v.Status, &v.ScheduleID, &v.CreatedAt, &v.FinalForwarderID, &v.FinalForwarderName, &v.ActualCarrierID, &v.ActualCarrierName, &v.FinalServiceOption, &v.FinalCurrency, &v.FinalFreightAmount, &v.FinalETD, &v.FinalETA, &v.PaymentTerms, &v.ForwarderContractNo, &v.ApprovalInstanceID, &v.ReturnReason, &v.OperatorName, &v.SignedContractKey, &v.SignedContractName, &v.SignedContractUploadedAt, &v.ContractVerifiedAt, &v.ContractVerifiedByName, &v.PaymentRequestedAt, &v.UpdatedAt)
	return v, err
}
func (s *Service) GetContractHandoff(ctx context.Context, tenantID, id int64) (ContractHandoff, error) {
	v, err := scanHandoff(s.pool.QueryRow(ctx, handoffSelect+` WHERE tenant_id=$1 AND id=$2`, tenantID, id))
	if err == pgx.ErrNoRows {
		return v, apierr.NotFound("SHIPPING_HANDOFF_NOT_FOUND", "正式船期任务不存在")
	}
	if err == nil && v.SignedContractKey != "" && s.files != nil {
		v.SignedContractURL, _ = s.files.PresignDownload(ctx, v.SignedContractKey, v.SignedContractName)
	}
	return v, err
}
func (s *Service) ListD4ContractHandoffs(ctx context.Context, tenantID int64, status string) ([]ContractHandoff, error) {
	rows, err := s.pool.Query(ctx, handoffSelect+` WHERE tenant_id=$1 AND ($2='' OR status=$2) ORDER BY CASE status WHEN 'WAITING_REQUOTE' THEN 0 WHEN 'RETURNED' THEN 1 WHEN 'PENDING_APPROVAL' THEN 2 ELSE 3 END,updated_at DESC,id DESC`, tenantID, status)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []ContractHandoff
	for rows.Next() {
		v, e := scanHandoff(rows)
		if e != nil {
			return nil, e
		}
		out = append(out, v)
	}
	return out, rows.Err()
}
func validD4Date(v string) bool {
	if strings.TrimSpace(v) == "" {
		return false
	}
	_, e := time.Parse("2006-01-02", v)
	return e == nil
}
func (s *Service) SubmitFinalRequote(ctx context.Context, tenantID, id int64, in FinalRequoteInput, op Operator) (ContractHandoff, error) {
	if s.approvals == nil {
		return ContractHandoff{}, apierr.Conflict("SHIPPING_APPROVAL_UNAVAILABLE", "审批服务未配置")
	}
	amount, e := decimal.NewFromString(strings.TrimSpace(in.FinalFreightAmount))
	if e != nil || !amount.IsPositive() {
		return ContractHandoff{}, apierr.Invalid("SHIPPING_FINAL_AMOUNT", "请填写有效的最终物流费用")
	}
	if in.FinalForwarderID == 0 || strings.TrimSpace(in.FinalForwarderName) == "" || strings.TrimSpace(in.ActualCarrierName) == "" || strings.TrimSpace(in.FinalServiceOption) == "" || strings.TrimSpace(in.FinalCurrency) == "" || !validD4Date(in.FinalETD) || !validD4Date(in.FinalETA) || strings.TrimSpace(in.PaymentTerms) == "" || strings.TrimSpace(in.ForwarderContractNo) == "" {
		return ContractHandoff{}, apierr.Invalid("SHIPPING_FINAL_TERMS_REQUIRED", "请完整填写最终货代、实际承运方、运输方案、费用、船期、付款条件和货代合同号")
	}
	v, err := s.GetContractHandoff(ctx, tenantID, id)
	if err != nil {
		return v, err
	}
	if v.Status != "WAITING_REQUOTE" && v.Status != "RETURNED" {
		return v, apierr.Conflict("SHIPPING_REQUOTE_NOT_EDITABLE", "当前任务不能重新提交物流方案")
	}
	summary, _ := json.Marshal(map[string]string{"contract": v.ContractNo, "forwarder": in.FinalForwarderName, "carrier": in.ActualCarrierName, "amount": amount.StringFixed(2) + " " + strings.ToUpper(in.FinalCurrency), "etd": in.FinalETD, "payment_terms": in.PaymentTerms})
	instance, err := s.approvals.Submit(ctx, ApprovalSubmission{BizType: BizTypeShippingRequote, BizID: id, BizNo: v.ContractNo, Summary: string(summary), SubmitterID: op.ID, SubmitterName: op.Name, Amount: amount.StringFixed(2)})
	if err != nil {
		return v, err
	}
	_, err = s.pool.Exec(ctx, `UPDATE contract_shipping_handoffs SET final_forwarder_id=$3,final_forwarder_name=$4,actual_carrier_id=$5,actual_carrier_name=$6,final_service_option=$7,final_currency=upper($8),final_freight_amount=$9::numeric,final_etd=$10::date,final_eta=$11::date,payment_terms=$12,forwarder_contract_no=$13,approval_instance_id=$14,status='PENDING_APPROVAL',return_reason='',operator_id=$15,operator_name=$16,updated_at=now() WHERE tenant_id=$1 AND id=$2`, tenantID, id, in.FinalForwarderID, strings.TrimSpace(in.FinalForwarderName), in.ActualCarrierID, strings.TrimSpace(in.ActualCarrierName), strings.TrimSpace(in.FinalServiceOption), strings.TrimSpace(in.FinalCurrency), amount.StringFixed(2), in.FinalETD, in.FinalETA, strings.TrimSpace(in.PaymentTerms), strings.TrimSpace(in.ForwarderContractNo), instance, op.ID, op.Name)
	if err != nil {
		return v, err
	}
	return s.GetContractHandoff(ctx, tenantID, id)
}
func (s *Service) ApplyRequoteApproval(ctx context.Context, tenantID, id, instance int64, result, comment string, claim EventClaim) error {
	return pgx.BeginFunc(ctx, s.pool, func(tx pgx.Tx) error {
		if claim != nil {
			if err := claim(ctx, tx); err != nil {
				return err
			}
		}
		status := "RETURNED"
		if result == "APPROVED" {
			status = "APPROVED"
		}
		cmd, err := tx.Exec(ctx, `UPDATE contract_shipping_handoffs SET status=$4,return_reason=$5,updated_at=now() WHERE tenant_id=$1 AND id=$2 AND approval_instance_id=$3 AND status='PENDING_APPROVAL'`, tenantID, id, instance, status, strings.TrimSpace(comment))
		if err != nil {
			return err
		}
		if cmd.RowsAffected() == 0 {
			return apierr.Conflict("SHIPPING_APPROVAL_STALE", "物流方案审批已处理或版本不一致")
		}
		return nil
	})
}
func shippingContractPrefix(tenantID, id int64) string {
	return fmt.Sprintf("d4-contracts/shipping/%d/%d/", tenantID, id)
}
func (s *Service) PresignShippingContract(ctx context.Context, tenantID, id int64, fileName string) (ContractUpload, error) {
	if s.files == nil {
		return ContractUpload{}, apierr.Conflict("SHIPPING_FILES_UNAVAILABLE", "文件存储未配置")
	}
	if _, err := s.GetContractHandoff(ctx, tenantID, id); err != nil {
		return ContractUpload{}, err
	}
	name := strings.TrimSpace(path.Base(strings.ReplaceAll(fileName, "\\", "/")))
	if name == "" || name == "." {
		return ContractUpload{}, apierr.Invalid("SHIPPING_CONTRACT_FILE_REQUIRED", "请选择签署合同文件")
	}
	key := fmt.Sprintf("%s%d-%s", shippingContractPrefix(tenantID, id), time.Now().UnixNano(), name)
	url, expires, err := s.files.PresignPut(ctx, key)
	return ContractUpload{key, url, expires}, err
}
func (s *Service) SaveShippingContract(ctx context.Context, tenantID, id int64, key, name string) (ContractHandoff, error) {
	key, name = strings.TrimSpace(key), strings.TrimSpace(name)
	if key == "" || name == "" || !strings.HasPrefix(key, shippingContractPrefix(tenantID, id)) {
		return ContractHandoff{}, apierr.Invalid("SHIPPING_CONTRACT_FILE_MISMATCH", "合同文件与正式船期任务不匹配")
	}
	if _, _, err := s.files.Stat(ctx, key); err != nil {
		return ContractHandoff{}, apierr.Invalid("SHIPPING_CONTRACT_FILE_MISSING", "上传的合同文件不存在")
	}
	cmd, err := s.pool.Exec(ctx, `UPDATE contract_shipping_handoffs SET signed_contract_key=$3,signed_contract_name=$4,signed_contract_uploaded_at=now(),contract_verified_at=null,contract_verified_by=0,contract_verified_by_name='',status='CONTRACT_UPLOADED',updated_at=now() WHERE tenant_id=$1 AND id=$2 AND status IN ('APPROVED','CONTRACT_UPLOADED')`, tenantID, id, key, name)
	if err != nil {
		return ContractHandoff{}, err
	}
	if cmd.RowsAffected() == 0 {
		return ContractHandoff{}, apierr.Conflict("SHIPPING_CONTRACT_NOT_EDITABLE", "物流方案批准后才能上传货代合同")
	}
	return s.GetContractHandoff(ctx, tenantID, id)
}
func (s *Service) VerifyShippingContract(ctx context.Context, tenantID, id int64, op Operator) (ContractHandoff, error) {
	cmd, err := s.pool.Exec(ctx, `UPDATE contract_shipping_handoffs SET contract_verified_at=now(),contract_verified_by=$3,contract_verified_by_name=$4,status='CONTRACT_VERIFIED',updated_at=now() WHERE tenant_id=$1 AND id=$2 AND status='CONTRACT_UPLOADED' AND signed_contract_key<>''`, tenantID, id, op.ID, op.Name)
	if err != nil {
		return ContractHandoff{}, err
	}
	if cmd.RowsAffected() == 0 {
		return ContractHandoff{}, apierr.Conflict("SHIPPING_CONTRACT_NOT_VERIFIABLE", "请先上传当前货代双方签署的合同")
	}
	return s.GetContractHandoff(ctx, tenantID, id)
}
func (s *Service) MarkShippingPaymentRequested(ctx context.Context, tenantID, id int64) (ContractHandoff, error) {
	cmd, err := s.pool.Exec(ctx, `UPDATE contract_shipping_handoffs SET payment_requested_at=now(),status='PAYMENT_REQUESTED',updated_at=now() WHERE tenant_id=$1 AND id=$2 AND status='CONTRACT_VERIFIED'`, tenantID, id)
	if err != nil {
		return ContractHandoff{}, err
	}
	if cmd.RowsAffected() == 0 {
		return ContractHandoff{}, apierr.Conflict("SHIPPING_PAYMENT_NOT_READY", "货代合同经财务上级或老板核验后才能申请付款")
	}
	return s.GetContractHandoff(ctx, tenantID, id)
}
