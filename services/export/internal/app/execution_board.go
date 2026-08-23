package app

import (
	"context"

	"github.com/sgao19/erp-go/services/export/internal/store"
)

// 合同执行进程一览（D2）。
//
// 「这单到哪了」现在没有一个人答得上来：合同金额在出口合同页，订没订货在
// 采购页，货走没走在出运页，钱收没收在银行流水页。四个页面、四个部门，
// 谁都只看得见自己那一段。
//
// 这一层出的是**出口这边能一次答完的三条**：合同谈了多少、货走了多少、
// 钱收了多少。采购和船期在别的服务，由网关按这一页的合同批量取回来拼上
// ——一页两次调用，不是一行两次。
//
// 出运和收款都折成金额，为的是让两个数直接可比：走了六成货、收了三成款，
// 钱和货脱节一眼看得出。数量做不到——一张合同上 100 吨和 50 件加不起来。

// ExecutionRow 是一览表上属于出口的那半行。采购与船期两列由网关补齐。
type ExecutionRow struct {
	ContractID      int64
	ContractNo      string
	CustomerID      int64
	CustomerName    string
	SalesEmployeeID int64
	SalesEmployee   string
	Status          string
	EffectiveDate   string
	Currency        string
	TotalAmount     string
	// 已出运折成金额。可能超过合同金额——改版把量调小而货已经发了。
	ShippedAmount  string
	ReceivedAmount string
	// 空串表示客户主数据里没配账期——不是「今天到期」。
	DueDate string
	// 正数已逾期，负数是还剩几天；DueUnset 时无意义。
	OverdueDays int32
	DueUnset    bool
}

// ExecutionFilter 收窄一览表。Status 留空表示只看在跑的合同
// （生效 / 执行中 / 已完成）——签之前没有进程可言，作废的不必占地方。
type ExecutionFilter struct {
	Status     string
	CustomerID int64
	Keyword    string
}

// ListContractExecution 返回一览表的出口半边。
//
// 围栏沿用合同列表那道：看得见这张合同，才看得见它的进度。**没有对采购、
// 船期再单独设栏**，那是刻意的——同一行里有的列有数、有的列空着，读起来
// 就是「没采购」，而真相是「你没权限看」。一个会给出错误答案的表格比没有
// 这张表格更糟。安全性靠另一头保证：这一行只给进度数字，不给供应商、不给
// 采购价；要看细节得点进去，那时各模块自己的围栏照常生效。
func (s *Service) ListContractExecution(ctx context.Context, tenantID int64, f ExecutionFilter, page, size int32, op Operator) ([]ExecutionRow, int64, error) {
	visible, err := s.visibleTo(ctx, op)
	if err != nil {
		return nil, 0, err
	}
	if page < 1 {
		page = 1
	}
	if size < 1 || size > 200 {
		size = 50
	}
	rows, err := s.q.ListContractExecution(ctx, store.ListContractExecutionParams{
		TenantID: tenantID, ScopeAll: visible.All, EmployeeIds: visible.EmployeeIDs,
		Status: f.Status, CustomerID: f.CustomerID, Keyword: f.Keyword,
		RowLimit: size, RowOffset: (page - 1) * size,
	})
	if err != nil {
		return nil, 0, err
	}
	out := make([]ExecutionRow, 0, len(rows))
	var total int64
	for _, r := range rows {
		total = r.Total
		out = append(out, ExecutionRow{
			ContractID: r.ID, ContractNo: r.ContractNo,
			CustomerID: r.CustomerID, CustomerName: r.CustomerName,
			SalesEmployeeID: r.SalesEmployeeID, SalesEmployee: r.SalesEmployee,
			Status: r.Status, EffectiveDate: r.EffectiveDate,
			Currency: r.Currency, TotalAmount: r.TotalAmount,
			ShippedAmount: r.ShippedAmount, ReceivedAmount: r.ReceivedAmount,
			DueDate: r.DueDate, OverdueDays: r.OverdueDays, DueUnset: r.DueUnset,
		})
	}
	return out, total, nil
}
