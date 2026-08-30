package app

import (
	"context"

	"github.com/sgao19/erp-go/services/procurement/internal/store"
)

// 存量采购单的应付到期日补算。
//
// 为什么需要它：到期日是从「配了账期之后下的单」那一刻起才写入的，在此
// 之前下的单一张都没有；而供应商的 payment_days 又是同一批改动才有的列，
// 迁移当天全是 0。两件事叠起来的结果是——上线第一天，对账页上的存量单
// 一张不落全部显示「未配账期」，三个数字里有意义的只剩一个。
//
// 迁移里不做这件事是有意的：账期在 masterdata 的库里，SQL 迁移够不着；
// 而且迁移执行的那一刻，账期本身也还没人填。所以它只能是一个「先去供应
// 商详情页把账期配上，再回来点一下」的人工动作。
//
// 口径上刻意不给员工留输入框：账期只有一个出处，就是供应商详情页上配的
// 那个数。手填一个数进来，等于让同一件事有两个答案。

// PayableBackfillResult 是一次补算的账单。
//
// 报「补了多少」不够——没补上的那部分才是下一步要干的活。跳过的都是没配
// 账期的供应商，员工看到「还有 5 家没配、涉及 12 张单」才知道回头去哪儿。
// 只报成功数会让一次补了一半的操作看起来像做完了。
type PayableBackfillResult struct {
	// UpdatedOrders 真正补上到期日的采购单数。
	UpdatedOrders int64
	// AppliedSuppliers 配了账期、参与了补算的供应商数。
	AppliedSuppliers int32
	// SkippedSuppliers 挂着单但没配账期（或主数据查不到）的供应商数。
	SkippedSuppliers int32
	// SkippedOrders 因此仍然没有到期日的采购单数。
	SkippedOrders int64
}

// BackfillPayableDue 把存量采购单的应付到期日按供应商配的账期补齐。
//
// 幂等：只写 payable_due_date 为空的行，跑几遍结果一样。所以不包在一个
// 大事务里——中途断了就再点一次，已经补好的部分不会被重算。一个横跨几
// 千行、几十次跨服务往返的事务，风险比它省下的那点一致性大得多。
func (s *Service) BackfillPayableDue(ctx context.Context, tenantID int64, op Operator) (PayableBackfillResult, error) {
	if err := s.requireFullScope(ctx, op); err != nil {
		return PayableBackfillResult{}, err
	}
	pending, err := s.q.ListSuppliersMissingPayableDue(ctx, tenantID)
	if err != nil {
		return PayableBackfillResult{}, err
	}
	out := PayableBackfillResult{}
	for _, row := range pending {
		days := s.supplierPaymentDays(ctx, row.SupplierID)
		// 0 有两种来路——没配账期，或者主数据这一刻查不到。两种都只能
		// 跳过：没有账期就没有算法，而**空不等于当天到期**，编一个日子
		// 会让「今天该付谁」这句话变成假的。
		if days <= 0 {
			out.SkippedSuppliers++
			out.SkippedOrders += row.OrderCount
			continue
		}
		n, err := s.q.BackfillPayableDue(ctx, store.BackfillPayableDueParams{
			TenantID: tenantID, SupplierID: row.SupplierID, PaymentDays: days,
		})
		if err != nil {
			return PayableBackfillResult{}, err
		}
		out.AppliedSuppliers++
		out.UpdatedOrders += n
	}
	// 一次动几百上千行，事后只能靠 updated_at 反推是哪一批——留一行日志，
	// 至少「谁在什么时候补的、补了多少」有据可查。仓里没有审计表，这是
	// 现有条件下最接近留痕的东西。
	s.log.InfoContext(ctx, "补算了存量采购单的应付到期日",
		"tenant_id", tenantID, "operator", op.Name,
		"updated_orders", out.UpdatedOrders, "applied_suppliers", out.AppliedSuppliers,
		"skipped_suppliers", out.SkippedSuppliers, "skipped_orders", out.SkippedOrders)
	// 别人开着的对账页得知道整张表刚被改过，否则要等他下次翻页才看得见。
	if out.UpdatedOrders > 0 {
		s.nudge(ctx, tenantID)
	}
	return out, nil
}

// supplierPaymentDays 取供应商当前配的账期；取不到就当没配。
//
// 查不着不报错是故意的：一家供应商被停用或删了，不该让另外二十家的补算
// 一起失败。这一家会进「跳过」那一栏，员工看得见。
func (s *Service) supplierPaymentDays(ctx context.Context, supplierID int64) int32 {
	if s.suppliers == nil || supplierID <= 0 {
		return 0
	}
	supplier, err := s.suppliers.Get(ctx, supplierID)
	if err != nil {
		return 0
	}
	return supplier.PaymentDays
}
