package app

import (
	"github.com/sgao19/erp-go/pkg/apierr"
	"github.com/shopspring/decimal"
	"strings"
)

func validateHistoricalOrders(in *ExistingContractInput) error {
	if in.History.RecordOnly {
		return validateHistoricalRecord(in)
	}
	if in.History.TakeoverDate == "" {
		return apierr.Invalid("EX_TAKEOVER_DATE", "请填写历史合同接续日期")
	}
	if err := validBusinessDate(in.History.TakeoverDate, "EX_TAKEOVER_DATE", "接续日期"); err != nil {
		return err
	}
	if in.SignedDate > in.History.TakeoverDate || in.EffectiveDate > in.History.TakeoverDate {
		return apierr.Invalid("EX_TAKEOVER_DATE", "接续日期不能早于合同签订或生效日期")
	}
	// Reject retired operational fields instead of silently discarding history.
	if in.ProcurementEmployeeID != 0 || in.SupplierID != 0 {
		return apierr.Invalid("EX_HISTORY_MODULE_DATA", "原采购订单请在采购模块录入关联")
	}
	values := []string{in.OpeningReceivedAmount}
	for _, item := range in.Items {
		values = append(values, item.OpeningProcuredQty, item.OpeningArrivedQty, item.OpeningShippedQty)
	}
	for _, value := range values {
		if strings.TrimSpace(value) == "" {
			continue
		}
		n, err := decimal.NewFromString(value)
		if err != nil || !n.IsZero() {
			return apierr.Invalid("EX_HISTORY_MODULE_DATA", "历史采购、物流和收付款请在对应模块录入关联")
		}
	}
	// Opening operational and financial balances belong to their own modules.
	in.ProcurementEmployeeID = 0
	in.SupplierID = 0
	in.OpeningReceivedAmount = "0"
	for i := range in.Items {
		in.Items[i].OpeningProcuredQty = "0"
		in.Items[i].OpeningArrivedQty = "0"
		in.Items[i].OpeningShippedQty = "0"
	}
	return nil
}

// Historical records describe the original agreement, not an opening receivable.
func validateHistoricalRecord(in *ExistingContractInput) error {
	if strings.TrimSpace(in.ExternalContractNo) == "" {
		return apierr.Invalid("EX_EXTERNAL_CONTRACT_NO_REQUIRED", "请填写原合同号")
	}
	total, err := decimal.NewFromString(strings.TrimSpace(in.History.TotalAmount))
	if err != nil || !total.IsPositive() || !total.Equal(total.Round(2)) || total.GreaterThan(decimal.RequireFromString("9999999999999999.99")) {
		return apierr.Invalid("EX_HISTORY_TOTAL", "合同金额须大于 0 且最多两位小数")
	}
	if in.History.Status != "EXECUTING" && in.History.Status != "COMPLETED" {
		return apierr.Invalid("EX_HISTORY_STATUS", "请选择执行中或已完成")
	}
	if in.SignedDate == "" {
		return apierr.Invalid("EX_SIGNED_DATE_INVALID", "请填写签订日期")
	}
	if err := validBusinessDate(in.SignedDate, "EX_SIGNED_DATE_INVALID", "签订日期"); err != nil {
		return err
	}
	if len(in.Items) > 0 || in.SupplierID != 0 || in.ProcurementEmployeeID != 0 || (in.OpeningReceivedAmount != "" && in.OpeningReceivedAmount != "0") || in.Terms.ReceivableDueDate != "" {
		return apierr.Invalid("EX_HISTORY_MODULE_DATA", "历史合同仅保存基本资料和附件，不录入产品或财务期初数据")
	}
	in.History.TotalAmount = total.StringFixed(2)
	in.EffectiveDate = in.SignedDate
	in.OpeningReceivedAmount = "0"
	in.Terms = Terms{Text: in.Terms.Text}
	return nil
}
