package app

import (
	"context"
	"fmt"
	"strings"
	"unicode/utf8"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/sgao19/erp-go/pkg/apierr"
	"github.com/shopspring/decimal"
)

func validateManualOrder(v *ContractHandoff) error {
	v.ContractNo = strings.TrimSpace(v.ContractNo)
	v.ManualOrderNo = strings.TrimSpace(v.ManualOrderNo)
	v.FinalCurrency = strings.ToUpper(strings.TrimSpace(v.FinalCurrency))
	if v.ContractNo == "" {
		return apierr.Invalid("MANUAL_CONTRACT_NO_REQUIRED", "请填写关联外销合同单号，系统暂无此合同也可以保存")
	}
	for _, field := range []struct {
		text  string
		limit int
	}{{v.ContractNo, 50}, {v.ManualOrderNo, 80}, {v.CustomerName, 200}, {v.PortOfLoading, 100}, {v.PortOfDischarge, 100}, {v.FinalForwarderName, 200}, {v.ActualCarrierName, 200}, {v.FinalServiceOption, 200}, {v.ForwarderContractNo, 80}} {
		if utf8.RuneCountInString(field.text) > field.limit {
			return apierr.Invalid("MANUAL_FIELD_TOO_LONG", "填写的资料过长，请缩短后重试")
		}
	}
	if len(v.FinalCurrency) != 3 || strings.IndexFunc(v.FinalCurrency, func(r rune) bool { return r < 'A' || r > 'Z' }) >= 0 {
		return apierr.Invalid("MANUAL_CURRENCY", "请填写三位币种代码")
	}
	for _, d := range []string{v.FinalETD, v.FinalETA} {
		if d != "" && !validD4Date(d) {
			return apierr.Invalid("MANUAL_DATE", "日期格式应为 YYYY-MM-DD")
		}
	}
	if v.FinalETD != "" && v.FinalETA != "" && v.FinalETA < v.FinalETD {
		return apierr.Invalid("MANUAL_DATE_ORDER", "预计到港日期不能早于开船日期")
	}
	v.FinalFreightAmount = strings.TrimSpace(v.FinalFreightAmount)
	v.AmountMissing = v.FinalFreightAmount == ""
	if !v.AmountMissing {
		a, e := decimal.NewFromString(v.FinalFreightAmount)
		if e != nil || a.IsNegative() || !a.Equal(a.Round(2)) || a.GreaterThanOrEqual(decimal.New(1, 16)) {
			return apierr.Invalid("MANUAL_AMOUNT", "物流费用应为非负数，最多两位小数")
		}
	}
	if v.LinkedContractID < 0 || v.ID < 0 || v.FinalForwarderID < 0 || v.ActualCarrierID < 0 || len(v.CargoItems) > 500 {
		return apierr.Invalid("MANUAL_INPUT", "无效的物流资料或产品数量超过500行")
	}
	for i := range v.CargoItems {
		c := &v.CargoItems[i]
		c.ProductName = strings.TrimSpace(c.ProductName)
		c.UomCode = strings.TrimSpace(c.UomCode)
		q, e := decimal.NewFromString(c.Quantity)
		if c.ProductName == "" || c.UomCode == "" || utf8.RuneCountInString(c.ProductName) > 200 || utf8.RuneCountInString(c.UomCode) > 20 || utf8.RuneCountInString(c.ProductCode) > 100 || e != nil || !q.IsPositive() || !q.Equal(q.Round(4)) || q.GreaterThanOrEqual(decimal.New(1, 14)) {
			return apierr.Invalid("MANUAL_CARGO", fmt.Sprintf("第%d行请填写产品、单位和有效数量（最多四位小数）", i+1))
		}
	}
	return nil
}

// SaveManualShippingOrder creates only logistics data. ContractNo is an unbound
// reference; no contract event is emitted and no financial state is replayed.
func (s *Service) SaveManualShippingOrder(ctx context.Context, tenantID int64, v ContractHandoff, op Operator) (ContractHandoff, error) {
	if tenantID <= 0 || op.ID <= 0 {
		return ContractHandoff{}, apierr.Invalid("MANUAL_CONTEXT", "缺少当前公司或操作人")
	}
	if err := validateManualOrder(&v); err != nil {
		return ContractHandoff{}, err
	}
	err := pgx.BeginFunc(ctx, s.pool, func(tx pgx.Tx) error {
		if v.ID == 0 {
			if err := tx.QueryRow(ctx, `SELECT nextval('contract_shipping_handoffs_id_seq')`).Scan(&v.ID); err != nil {
				return err
			}
			if v.ManualOrderNo == "" {
				v.ManualOrderNo = fmt.Sprintf("LO-%08d", v.ID)
			}
			// Negative version namespace is reserved for standalone logistics records;
			// contract_id remains zero. A future link must not overwrite this identity.
			_, err := tx.Exec(ctx, `INSERT INTO contract_shipping_handoffs(id,tenant_id,contract_id,contract_no,contract_version_id,version_no,batch_no,currency,status) VALUES($1,$2,0,$3,-($1::bigint),0,1,$4,'DRAFT')`, v.ID, tenantID, v.ContractNo, v.FinalCurrency)
			if err != nil {
				return err
			}
			_, err = tx.Exec(ctx, `INSERT INTO manual_shipping_orders(handoff_id,tenant_id,order_no,created_by,created_by_name) VALUES($1,$2,$3,$4,$5)`, v.ID, tenantID, v.ManualOrderNo, op.ID, op.Name)
			if err != nil {
				return err
			}
		} else {
			var status, orderNo, contractNo string
			var linkedID int64
			err := tx.QueryRow(ctx, `SELECT h.status,m.order_no,h.contract_no,m.linked_contract_id FROM contract_shipping_handoffs h JOIN manual_shipping_orders m ON m.handoff_id=h.id AND m.tenant_id=h.tenant_id WHERE h.tenant_id=$1 AND h.id=$2 FOR UPDATE OF h`, tenantID, v.ID).Scan(&status, &orderNo, &contractNo, &linkedID)
			if err == pgx.ErrNoRows {
				return apierr.NotFound("MANUAL_ORDER_NOT_FOUND", "补录物流实单不存在")
			}
			if err != nil {
				return err
			}
			if status != "DRAFT" && status != "RETURNED" {
				return apierr.Conflict("MANUAL_ORDER_LOCKED", "该实单已进入后续流程，不能修改补录资料")
			}
			if linkedID > 0 && (v.LinkedContractID != linkedID || v.ContractNo != contractNo) {
				return apierr.Conflict("SHIPPING_CONTRACT_LINK_LOCKED", "已关联的合同号不能手动修改")
			}
			if orderNo != v.ManualOrderNo {
				return apierr.Conflict("MANUAL_ORDER_NO_LOCKED", "已建立的物流实单号不能修改")
			}
		}
		if v.LinkedContractID > 0 {
			if err := linkManualContract(ctx, tx, tenantID, v.ID, v.LinkedContractID, v.ContractNo, op); err != nil {
				return err
			}
		}
		amount := v.FinalFreightAmount
		if v.AmountMissing {
			amount = "0"
		}
		_, err := tx.Exec(ctx, `UPDATE contract_shipping_handoffs SET contract_no=$3,customer_name=$4,port_of_loading=$5,port_of_discharge=$6,final_forwarder_id=$7,final_forwarder_name=$8,actual_carrier_id=$9,actual_carrier_name=$10,final_service_option=$11,currency=$12::text,final_currency=$12::text,final_freight_amount=$13::numeric,final_etd=NULLIF($14,'')::date,final_eta=NULLIF($15,'')::date,payment_terms=$16,remark=$17,forwarder_contract_no=$18,operator_id=$19,operator_name=$20,updated_at=now(),status='DRAFT' WHERE tenant_id=$1 AND id=$2`, tenantID, v.ID, v.ContractNo, v.CustomerName, v.PortOfLoading, v.PortOfDischarge, v.FinalForwarderID, v.FinalForwarderName, v.ActualCarrierID, v.ActualCarrierName, v.FinalServiceOption, v.FinalCurrency, amount, v.FinalETD, v.FinalETA, v.PaymentTerms, v.Remark, v.ForwarderContractNo, op.ID, op.Name)
		if err != nil {
			return err
		}
		if _, err = tx.Exec(ctx, `UPDATE manual_shipping_orders SET amount_missing=$3 WHERE tenant_id=$1 AND handoff_id=$2`, tenantID, v.ID, v.AmountMissing); err != nil {
			return err
		}
		if _, err = tx.Exec(ctx, `DELETE FROM contract_shipping_handoff_cargo WHERE tenant_id=$1 AND handoff_id=$2`, tenantID, v.ID); err != nil {
			return err
		}
		for i, c := range v.CargoItems {
			if _, err = tx.Exec(ctx, `INSERT INTO contract_shipping_handoff_cargo(tenant_id,handoff_id,contract_item_id,line_no,product_code,product_name,specification,quantity,uom_code,remark) VALUES($1,$2,$3,$4,$5,$6,$7,$8::numeric,$9,$10)`, tenantID, v.ID, -int64(i+1), i+1, c.ProductCode, c.ProductName, c.Specification, c.Quantity, c.UomCode, c.Remark); err != nil {
				return err
			}
		}
		return nil
	})
	if err != nil {
		if p, ok := err.(*pgconn.PgError); ok && p.Code == "23505" {
			return ContractHandoff{}, apierr.Conflict("MANUAL_ORDER_NO_EXISTS", "本公司已存在该物流实单号，请检查是否重复补录")
		}
		return ContractHandoff{}, err
	}
	return s.GetContractHandoff(ctx, tenantID, v.ID)
}
