package app

import (
	"context"
	"errors"
	"fmt"

	"github.com/jackc/pgx/v5"

	"github.com/sgao19/erp-go/pkg/apierr"
	"github.com/sgao19/erp-go/services/mail/internal/store"
)

// 智能转换的每月额度（计量第三步）。
//
// 00040 记下了用量，用量页回答了「这个月转了多少」。这里回答下一个问题：
// **这家公司这个月还能转多少。**
//
// 为什么需要：每一次转换都是我们真金白银付给模型厂的钱。在此之前一家客户
// 公司想转多少次就转多少次——一个没有上限的成本口子，只要有人拿脚本循环
// 点，账单就没有边界。额度不是为了限制客户，是为了让「这家公司一个月最多
// 花我们多少」有一个能写进合同的数。
//
// 三条约定，都写在这一处：
//
//  1. **没设额度 = 不限。** 不给任何公司凭空安一个默认上限——一个我们没
//     承诺过的数字突然开始拦人，比不拦更糟。
//  2. **上限 0 = 一次都不许用。** 和「不限」是相反的两件事，所以「有没有
//     设」和「设成几」分开表达，不用 0 兼职表示「不限」。
//  3. **月份由数据库说了算。** 见 CurrentUsageMonth 的注释。

// ExcelQuota 是一家公司当下的额度状况，页面上那条百分比条要的就是这些。
type ExcelQuota struct {
	// Limited 说明有没有上限。false 时 MonthlyRuns 没有意义。
	Limited bool
	// MonthlyRuns 是每月可以转换的次数上限。
	MonthlyRuns int64
	// UsedThisMonth 是本月已经发起的次数（含失败——失败也真的花了钱）。
	UsedThisMonth int64
	// CurrentMonth 是数据库认定的当前月份 YYYY-MM。前端拿它和用户选的月份
	// 比对：只有看着当月时，「还剩多少」才是一个活的事实。
	CurrentMonth string
}

// Exhausted 说明这家公司这个月已经不能再转了。
func (q ExcelQuota) Exhausted() bool {
	return q.Limited && q.UsedThisMonth >= q.MonthlyRuns
}

// ExcelQuotaFor 出这家公司当下的额度状况。
func (s *Service) ExcelQuotaFor(ctx context.Context, tenantID int64) (ExcelQuota, error) {
	month, err := s.q.CurrentUsageMonth(ctx)
	if err != nil {
		return ExcelQuota{}, err
	}
	used, err := s.q.CountExcelRunsThisMonth(ctx, tenantID)
	if err != nil {
		return ExcelQuota{}, err
	}
	out := ExcelQuota{UsedThisMonth: used, CurrentMonth: month}
	row, err := s.q.GetExcelQuota(ctx, tenantID)
	if errors.Is(err, pgx.ErrNoRows) {
		return out, nil // 没设 = 不限
	}
	if err != nil {
		return ExcelQuota{}, err
	}
	out.Limited = true
	out.MonthlyRuns = row.MonthlyRuns
	return out, nil
}

// ensureExcelQuota 在发起任务前拦一道。
//
// 拦在发起而不是拦在 worker：用户点了按钮，要当场知道能不能做。排进队列再
// 失败，等于让人等上十几秒才被告知「本来就不该让你点」。
//
// 读一次、判一次、再插入——不是一条原子语句。所以两个人同时点，理论上能
// 越过上限一次。这是想清楚之后接受的：这套东西一个月几十到几百次，越过一
// 两次不改变任何成本结论，而把额度判断塞进插入语句会让那条 SQL 变成没人
// 敢改的样子，还换不回一句能给人看的错误话（「已用 200/200」）。
func (s *Service) ensureExcelQuota(ctx context.Context, tenantID int64) error {
	quota, err := s.ExcelQuotaFor(ctx, tenantID)
	if err != nil {
		return err
	}
	if !quota.Exhausted() {
		return nil
	}
	return apierr.Invalid("MAIL_EXCEL_QUOTA_EXHAUSTED", fmt.Sprintf(
		"本月智能转换额度已用完：已用 %d 次，上限 %d 次。下个月 1 号自动重置，需要提额请联系我们。",
		quota.UsedThisMonth, quota.MonthlyRuns))
}

// TenantExcelQuota 是平台运营看到的一行：某家公司的额度、用量和成本。
//
// 成本只在这一行出现，不在客户那一侧。这不是藏，是分工：客户要知道的是
// 「还能转几次」，我们要知道的是「这个月花了多少」。两个口径都要，只是给
// 的人不同。
type TenantExcelQuota struct {
	TenantID      int64
	Limited       bool
	MonthlyRuns   int64
	UsedThisMonth int64
	InputTokens   int64
	OutputTokens  int64
	// 按当下单价折出来的估算金额。没配单价时是空串——**不猜价格**，一个猜
	// 出来的成本比没有成本更坏，因为它看着像账。
	EstimatedCost string
	Currency      string
}

// ListExcelQuotas 出所有公司的额度和本月用量。
//
// 这一条**跨租户**，只给平台运营。租户隔离在这里让位于职责：额度是我们和
// 客户之间的商务约定，看全表的是我们，不是任何一家客户。守门在网关那一层
// （requirePlatformOperator），和死信台账用的是同一道门。
func (s *Service) ListExcelQuotas(ctx context.Context) ([]TenantExcelQuota, string, error) {
	month, err := s.q.CurrentUsageMonth(ctx)
	if err != nil {
		return nil, "", err
	}
	quotas, err := s.q.ListExcelQuotas(ctx)
	if err != nil {
		return nil, "", err
	}
	runs, err := s.q.ExcelRunsByTenantThisMonth(ctx)
	if err != nil {
		return nil, "", err
	}
	// 一家公司可能只出现在其中一边：设了额度还没用过，或者用过但没设额度。
	// 两边都要出现在结果里，否则平台页会漏掉正好该看的那一家。
	byTenant := make(map[int64]*TenantExcelQuota, len(quotas)+len(runs))
	for _, q := range quotas {
		byTenant[q.TenantID] = &TenantExcelQuota{
			TenantID: q.TenantID, Limited: true, MonthlyRuns: q.MonthlyRuns,
		}
	}
	for _, r := range runs {
		row, ok := byTenant[r.TenantID]
		if !ok {
			row = &TenantExcelQuota{TenantID: r.TenantID}
			byTenant[r.TenantID] = row
		}
		row.UsedThisMonth = r.Runs
		row.InputTokens = r.InputTokens
		row.OutputTokens = r.OutputTokens
	}
	// 金额在最后统一算，不在上面那个循环里——上面只走「本月有用量」的公司，
	// 于是「设了额度但这个月一次没转」的那几家拿不到金额字段，页面按空串
	// 显示成「未配单价」。而单价明明配着，真实答案是「本月 0 元」。
	//
	// 空是「不知道」、0 是「不要钱」——这条规矩这里自己违反了一次：把一个
	// 真实为 0 的数说成了不知道。配了单价，每一行都该有金额，哪怕是 0。
	out := make([]TenantExcelQuota, 0, len(byTenant))
	for _, row := range byTenant {
		if s.pricing.Configured() {
			row.EstimatedCost = estimateCost(row.InputTokens, row.OutputTokens, s.pricing)
			row.Currency = s.pricing.Currency
		}
		out = append(out, *row)
	}
	return out, month, nil
}

// SetExcelQuota 给一家公司定上限；limited=false 是恢复不限（删掉那一行）。
//
// 同样跨租户，同样只走平台运营那道门。
func (s *Service) SetExcelQuota(ctx context.Context, tenantID int64, limited bool, monthlyRuns, operatorID int64) error {
	if tenantID <= 0 {
		return apierr.Invalid("MAIL_EXCEL_QUOTA_TENANT_REQUIRED", "请指定公司")
	}
	if !limited {
		return s.q.ClearExcelQuota(ctx, tenantID)
	}
	if monthlyRuns < 0 {
		return apierr.Invalid("MAIL_EXCEL_QUOTA_NEGATIVE", "额度不能是负数")
	}
	return s.q.SetExcelQuota(ctx, store.SetExcelQuotaParams{
		TenantID: tenantID, MonthlyRuns: monthlyRuns, UpdatedBy: operatorID,
	})
}
