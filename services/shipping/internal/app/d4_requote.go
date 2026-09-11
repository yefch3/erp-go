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
	OptionID, FinalForwarderID, ActualCarrierID                                                                                                                 int64
	FinalForwarderName, ActualCarrierName, FinalServiceOption, FinalCurrency, FinalFreightAmount, FinalETD, FinalETA, PaymentTerms, ForwarderContractNo, Remark string
}
type ShippingRequoteOption struct {
	ID, HandoffID, ForwarderID, ActualCarrierID, CreatedBy  int64
	ForwarderName, ActualCarrierName, ServiceOption         string
	Currency, FreightAmount, ETD, ETA, PaymentTerms, Remark string
	CreatedByName, CreatedAt, UpdatedAt                     string
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
	CargoItems                                                                                                                                                                                                                                                                                                                  []ContractShippingCargoItem
}
type ContractShippingCargoItem struct {
	ID, ContractItemID                                                 int64
	LineNo                                                             int32
	ProductCode, ProductName, Specification, Quantity, UomCode, Remark string
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
	if err == nil {
		rows, cargoErr := s.pool.Query(ctx, `SELECT id,contract_item_id,line_no,product_code,product_name,specification,quantity::text,uom_code,remark FROM contract_shipping_handoff_cargo WHERE tenant_id=$1 AND handoff_id=$2 ORDER BY line_no,id`, tenantID, id)
		if cargoErr != nil {
			return v, cargoErr
		}
		defer rows.Close()
		for rows.Next() {
			var item ContractShippingCargoItem
			if cargoErr = rows.Scan(&item.ID, &item.ContractItemID, &item.LineNo, &item.ProductCode, &item.ProductName, &item.Specification, &item.Quantity, &item.UomCode, &item.Remark); cargoErr != nil {
				return v, cargoErr
			}
			v.CargoItems = append(v.CargoItems, item)
		}
		if cargoErr = rows.Err(); cargoErr != nil {
			return v, cargoErr
		}
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
func validateFinalRequote(in FinalRequoteInput) (decimal.Decimal, error) {
	amount, err := decimal.NewFromString(strings.TrimSpace(in.FinalFreightAmount))
	if err != nil || !amount.IsPositive() {
		return decimal.Zero, apierr.Invalid("SHIPPING_FINAL_AMOUNT", "请填写有效的最终物流费用")
	}
	if in.FinalForwarderID == 0 || strings.TrimSpace(in.FinalForwarderName) == "" || strings.TrimSpace(in.FinalServiceOption) == "" || strings.TrimSpace(in.FinalCurrency) == "" || !validD4Date(in.FinalETD) || !validD4Date(in.FinalETA) || strings.TrimSpace(in.PaymentTerms) == "" {
		return decimal.Zero, apierr.Invalid("SHIPPING_FINAL_TERMS_REQUIRED", "请完整填写最终货代、运输方案、费用、预计船期和付款条件")
	}
	return amount, nil
}
func scanRequoteOption(row pgx.Row) (ShippingRequoteOption, error) {
	var v ShippingRequoteOption
	err := row.Scan(&v.ID, &v.HandoffID, &v.ForwarderID, &v.ForwarderName, &v.ActualCarrierID, &v.ActualCarrierName,
		&v.ServiceOption, &v.Currency, &v.FreightAmount, &v.ETD, &v.ETA, &v.PaymentTerms, &v.Remark,
		&v.CreatedBy, &v.CreatedByName, &v.CreatedAt, &v.UpdatedAt)
	return v, err
}

const requoteOptionSelect = `SELECT id,handoff_id,forwarder_id,forwarder_name,actual_carrier_id,actual_carrier_name,
	service_option,currency,freight_amount::text,etd::text,eta::text,payment_terms,remark,
	created_by,created_by_name,created_at::text,updated_at::text FROM shipping_execution_requote_options`

func (s *Service) ListShippingRequoteOptions(ctx context.Context, tenantID, handoffID int64) ([]ShippingRequoteOption, error) {
	rows, err := s.pool.Query(ctx, requoteOptionSelect+` WHERE tenant_id=$1 AND handoff_id=$2 ORDER BY updated_at DESC,id DESC`, tenantID, handoffID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []ShippingRequoteOption{}
	for rows.Next() {
		v, e := scanRequoteOption(rows)
		if e != nil {
			return nil, e
		}
		out = append(out, v)
	}
	return out, rows.Err()
}

func (s *Service) shippingRequoteOption(ctx context.Context, tenantID, handoffID, optionID int64) (ShippingRequoteOption, error) {
	v, err := scanRequoteOption(s.pool.QueryRow(ctx, requoteOptionSelect+` WHERE tenant_id=$1 AND handoff_id=$2 AND id=$3`, tenantID, handoffID, optionID))
	if err == pgx.ErrNoRows {
		return v, apierr.NotFound("SHIPPING_REQUOTE_OPTION_NOT_FOUND", "候选物流方案不存在")
	}
	return v, err
}

func (s *Service) SaveFinalRequoteDraft(ctx context.Context, tenantID, id int64, in FinalRequoteInput, op Operator) (ContractHandoff, error) {
	amount, err := validateFinalRequote(in)
	if err != nil {
		return ContractHandoff{}, err
	}
	err = pgx.BeginFunc(ctx, s.pool, func(tx pgx.Tx) error {
		cmd, e := tx.Exec(ctx, `UPDATE contract_shipping_handoffs SET operator_id=$3,operator_name=$4,updated_at=now() WHERE tenant_id=$1 AND id=$2 AND status IN ('WAITING_REQUOTE','RETURNED')`, tenantID, id, op.ID, op.Name)
		if e != nil {
			return e
		}
		if cmd.RowsAffected() == 0 {
			return apierr.Conflict("SHIPPING_REQUOTE_NOT_EDITABLE", "当前任务不能保存候选物流方案")
		}
		if in.OptionID == 0 {
			_, e = tx.Exec(ctx, `INSERT INTO shipping_execution_requote_options
				(tenant_id,handoff_id,forwarder_id,forwarder_name,actual_carrier_id,actual_carrier_name,service_option,currency,freight_amount,etd,eta,payment_terms,remark,created_by,created_by_name)
				VALUES($1,$2,$3,$4,$5,$6,$7,upper($8),$9::numeric,$10::date,$11::date,$12,$13,$14,$15)`, tenantID, id, in.FinalForwarderID, strings.TrimSpace(in.FinalForwarderName), in.ActualCarrierID, strings.TrimSpace(in.ActualCarrierName), strings.TrimSpace(in.FinalServiceOption), strings.TrimSpace(in.FinalCurrency), amount.StringFixed(2), in.FinalETD, in.FinalETA, strings.TrimSpace(in.PaymentTerms), strings.TrimSpace(in.Remark), op.ID, op.Name)
			return e
		}
		cmd, e = tx.Exec(ctx, `UPDATE shipping_execution_requote_options SET forwarder_id=$4,forwarder_name=$5,actual_carrier_id=$6,actual_carrier_name=$7,service_option=$8,currency=upper($9),freight_amount=$10::numeric,etd=$11::date,eta=$12::date,payment_terms=$13,remark=$14,updated_at=now() WHERE tenant_id=$1 AND handoff_id=$2 AND id=$3`, tenantID, id, in.OptionID, in.FinalForwarderID, strings.TrimSpace(in.FinalForwarderName), in.ActualCarrierID, strings.TrimSpace(in.ActualCarrierName), strings.TrimSpace(in.FinalServiceOption), strings.TrimSpace(in.FinalCurrency), amount.StringFixed(2), in.FinalETD, in.FinalETA, strings.TrimSpace(in.PaymentTerms), strings.TrimSpace(in.Remark))
		if e != nil {
			return e
		}
		if cmd.RowsAffected() == 0 {
			return apierr.NotFound("SHIPPING_REQUOTE_OPTION_NOT_FOUND", "候选物流方案不存在")
		}
		return nil
	})
	if err != nil {
		return ContractHandoff{}, err
	}
	return s.GetContractHandoff(ctx, tenantID, id)
}
func (s *Service) DeleteShippingRequoteOption(ctx context.Context, tenantID, handoffID, optionID int64) error {
	cmd, err := s.pool.Exec(ctx, `DELETE FROM shipping_execution_requote_options o USING contract_shipping_handoffs h WHERE o.tenant_id=$1 AND o.handoff_id=$2 AND o.id=$3 AND h.tenant_id=o.tenant_id AND h.id=o.handoff_id AND h.status IN ('WAITING_REQUOTE','RETURNED')`, tenantID, handoffID, optionID)
	if err != nil {
		return err
	}
	if cmd.RowsAffected() == 0 {
		return apierr.NotFound("SHIPPING_REQUOTE_OPTION_NOT_FOUND", "候选物流方案不存在或当前不可删除")
	}
	return nil
}
func (s *Service) SelectFinalRequoteDraft(ctx context.Context, tenantID, id, optionID int64, op Operator) (ContractHandoff, error) {
	option, err := s.shippingRequoteOption(ctx, tenantID, id, optionID)
	if err != nil {
		return ContractHandoff{}, err
	}
	amount, err := validateFinalRequote(FinalRequoteInput{FinalForwarderID: option.ForwarderID, FinalForwarderName: option.ForwarderName, ActualCarrierID: option.ActualCarrierID, ActualCarrierName: option.ActualCarrierName, FinalServiceOption: option.ServiceOption, FinalCurrency: option.Currency, FinalFreightAmount: option.FreightAmount, FinalETD: option.ETD, FinalETA: option.ETA, PaymentTerms: option.PaymentTerms, Remark: option.Remark})
	if err != nil {
		return ContractHandoff{}, err
	}
	cmd, err := s.pool.Exec(ctx, `UPDATE contract_shipping_handoffs SET final_forwarder_id=$3,final_forwarder_name=$4,actual_carrier_id=$5,actual_carrier_name=$6,final_service_option=$7,final_currency=upper($8),final_freight_amount=$9::numeric,final_etd=$10::date,final_eta=$11::date,payment_terms=$12,forwarder_contract_no='',remark=$13,approval_instance_id=0,status='DRAFT',return_reason='',operator_id=$14,operator_name=$15,updated_at=now() WHERE tenant_id=$1 AND id=$2 AND status IN ('WAITING_REQUOTE','RETURNED')`, tenantID, id, option.ForwarderID, strings.TrimSpace(option.ForwarderName), option.ActualCarrierID, strings.TrimSpace(option.ActualCarrierName), strings.TrimSpace(option.ServiceOption), strings.TrimSpace(option.Currency), amount.StringFixed(2), option.ETD, option.ETA, strings.TrimSpace(option.PaymentTerms), strings.TrimSpace(option.Remark), op.ID, op.Name)
	if err != nil {
		return ContractHandoff{}, err
	}
	if cmd.RowsAffected() == 0 {
		return ContractHandoff{}, apierr.Conflict("SHIPPING_REQUOTE_NOT_SELECTABLE", "当前任务不能提交到物流订单草稿")
	}
	return s.GetContractHandoff(ctx, tenantID, id)
}
func (s *Service) SubmitFinalRequote(ctx context.Context, tenantID, id int64, in FinalRequoteInput, op Operator) (ContractHandoff, error) {
	if s.approvals == nil {
		return ContractHandoff{}, apierr.Conflict("SHIPPING_APPROVAL_UNAVAILABLE", "审批服务未配置")
	}
	v, err := s.GetContractHandoff(ctx, tenantID, id)
	if err != nil {
		return v, err
	}
	if v.Status != "DRAFT" {
		return v, apierr.Conflict("SHIPPING_REQUOTE_NOT_DRAFT", "请先在实单物流询价中选择方案并提交到草稿")
	}
	in = FinalRequoteInput{FinalForwarderID: v.FinalForwarderID, FinalForwarderName: v.FinalForwarderName, ActualCarrierID: v.ActualCarrierID, ActualCarrierName: v.ActualCarrierName, FinalServiceOption: v.FinalServiceOption, FinalCurrency: v.FinalCurrency, FinalFreightAmount: v.FinalFreightAmount, FinalETD: v.FinalETD, FinalETA: v.FinalETA, PaymentTerms: v.PaymentTerms, Remark: v.Remark}
	amount, err := validateFinalRequote(in)
	if err != nil {
		return ContractHandoff{}, err
	}
	summary, _ := json.Marshal(map[string]string{"contract": v.ContractNo, "forwarder": in.FinalForwarderName, "carrier": in.ActualCarrierName, "amount": amount.StringFixed(2) + " " + strings.ToUpper(in.FinalCurrency), "etd": in.FinalETD, "payment_terms": in.PaymentTerms})
	instance, err := s.approvals.Submit(ctx, ApprovalSubmission{BizType: BizTypeShippingRequote, BizID: id, BizNo: v.ContractNo, Summary: string(summary), SubmitterID: op.ID, SubmitterName: op.Name, Amount: amount.StringFixed(2)})
	if err != nil {
		return v, err
	}
	_, err = s.pool.Exec(ctx, `UPDATE contract_shipping_handoffs SET approval_instance_id=$3,status='PENDING_APPROVAL',return_reason='',operator_id=$4,operator_name=$5,updated_at=now() WHERE tenant_id=$1 AND id=$2 AND status='DRAFT'`, tenantID, id, instance, op.ID, op.Name)
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
func (s *Service) SaveShippingContract(ctx context.Context, tenantID, id int64, key, name, contractNo string) (ContractHandoff, error) {
	key, name, contractNo = strings.TrimSpace(key), strings.TrimSpace(name), strings.TrimSpace(contractNo)
	if key == "" || name == "" || contractNo == "" || !strings.HasPrefix(key, shippingContractPrefix(tenantID, id)) {
		return ContractHandoff{}, apierr.Invalid("SHIPPING_CONTRACT_FILE_MISMATCH", "合同文件与正式船期任务不匹配")
	}
	if _, _, err := s.files.Stat(ctx, key); err != nil {
		return ContractHandoff{}, apierr.Invalid("SHIPPING_CONTRACT_FILE_MISSING", "上传的合同文件不存在")
	}
	cmd, err := s.pool.Exec(ctx, `UPDATE contract_shipping_handoffs SET signed_contract_key=$3,signed_contract_name=$4,forwarder_contract_no=$5,signed_contract_uploaded_at=now(),contract_verified_at=null,contract_verified_by=0,contract_verified_by_name='',payment_requested_at=null,status='CONTRACT_UPLOADED',updated_at=now() WHERE tenant_id=$1 AND id=$2 AND status IN ('APPROVED','CONTRACT_UPLOADED')`, tenantID, id, key, name, contractNo)
	if err != nil {
		return ContractHandoff{}, err
	}
	if cmd.RowsAffected() == 0 {
		return ContractHandoff{}, apierr.Conflict("SHIPPING_CONTRACT_NOT_EDITABLE", "物流方案批准后才能上传货代合同")
	}
	return s.GetContractHandoff(ctx, tenantID, id)
}
func (s *Service) VerifyShippingContract(ctx context.Context, tenantID, id int64, op Operator) (ContractHandoff, error) {
	cmd, err := s.pool.Exec(ctx, `UPDATE contract_shipping_handoffs SET contract_verified_at=now(),contract_verified_by=$3,contract_verified_by_name=$4,payment_requested_at=now(),status='PAYMENT_REQUESTED',updated_at=now() WHERE tenant_id=$1 AND id=$2 AND status='CONTRACT_UPLOADED' AND signed_contract_key<>''`, tenantID, id, op.ID, op.Name)
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
