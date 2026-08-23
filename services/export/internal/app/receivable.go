package app

import (
	"context"
	"log/slog"
	"time"

	"github.com/sgao19/erp-go/pkg/apierr"

	"github.com/sgao19/erp-go/services/export/internal/store"
)

// 应收账款到期（E1）。
//
// 业务的原话是「120 天或 150 天账期，笔数太多，不同客户的订单叠在一块，
// 财务用 Excel 记不住哪笔什么时候到期」。系统本来就能算出每张合同还欠
// 多少（核销表求和），缺的一直是**什么时候该收**——全库找不到一个
// due_date。这一层把那个日子变成可查询、可排序、可催的东西。
//
// 口径由业务定：**账期从合同生效日起算**。收到定金/预付款/信用证之后
// 才去下采购单，所以合同生效本身就意味着首付到位，之后的账期算的是尾款。

// ReceivableRow 是催收清单上的一行：这张合同该收多少、什么时候该收、
// 逾期了几天。
type ReceivableRow struct {
	ContractID      int64
	ContractNo      string
	CustomerID      int64
	CustomerName    string
	SalesEmployeeID int64
	SalesEmployee   string
	// 空串表示客户主数据里没配账期——不是「今天到期」。
	DueDate        string
	EffectiveDate  string
	Currency       string
	TotalAmount    string
	ReceivedAmount string
	OpenAmount     string
	// 正数已逾期，负数是还剩几天；DueUnset 时无意义。
	OverdueDays int32
	DueUnset    bool
}

// ReceivableFilter 收窄清单。两个开关互斥地各管一件事：只看逾期的，
// 或只看还没配账期的（后者是催配置，不是催钱）。
type ReceivableFilter struct {
	OverdueOnly bool
	UnsetOnly   bool
	Keyword     string
}

// ListReceivableDue 返回还没收完的生效合同，按该收的日子排，逾期的在最前。
//
// 围栏沿用出口模块自己的数据范围（同合同列表）：应收是钱的事，谁能看见
// 哪张合同的欠款，和谁能看见哪张合同是同一个问题。
func (s *Service) ListReceivableDue(ctx context.Context, tenantID int64, f ReceivableFilter, page, size int32, op Operator) ([]ReceivableRow, int64, error) {
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
	rows, err := s.q.ListReceivableDue(ctx, store.ListReceivableDueParams{
		TenantID: tenantID, ScopeAll: visible.All, EmployeeIds: visible.EmployeeIDs,
		OverdueOnly: f.OverdueOnly, UnsetOnly: f.UnsetOnly, Keyword: f.Keyword,
		RowLimit: size, RowOffset: (page - 1) * size,
	})
	if err != nil {
		return nil, 0, err
	}
	out := make([]ReceivableRow, 0, len(rows))
	var total int64
	for _, r := range rows {
		total = r.Total
		out = append(out, ReceivableRow{
			ContractID: r.ID, ContractNo: r.ContractNo,
			CustomerID: r.CustomerID, CustomerName: r.CustomerName,
			SalesEmployeeID: r.SalesEmployeeID, SalesEmployee: r.SalesEmployee,
			DueDate: r.DueDate, EffectiveDate: r.EffectiveDate,
			Currency: r.Currency, TotalAmount: r.TotalAmount,
			ReceivedAmount: r.ReceivedAmount, OpenAmount: r.OpenAmount,
			OverdueDays: r.OverdueDays, DueUnset: r.DueUnset,
		})
	}
	return out, total, nil
}

// BackfillReceivableDue 给存量合同补到期日：生效了但没有到期日的，按传入
// 的客户账期补算。幂等——只碰为空的行，跑几遍结果一样。
//
// 存在的理由是时间差：到期日从今天起才在签署时写入，而在此之前生效的
// 合同一张都没有。没有它，E1 上线当天的清单是空的。
func (s *Service) BackfillReceivableDue(ctx context.Context, tenantID, customerID int64, paymentDays int32) (int64, error) {
	if paymentDays <= 0 {
		return 0, nil
	}
	return s.q.BackfillReceivableDue(ctx, store.BackfillReceivableDueParams{
		TenantID: tenantID, CustomerID: customerID, PaymentDays: paymentDays,
	})
}

// ── 提醒（E1 第二期）───────────────────────────────────────────────
//
// 清单页解决了「记不住」，提醒解决的是「不用记得去打开」。
//
// 一条提醒就是一条站内信，收件人是合同的销售负责人。四个时机压在一条
// SQL 里判断（到期前 30 天进入视野、7 天内、当天、逾期后每 7 天一轮），
// 幂等交给唯一键——所以这个 worker 可以随便多跑，停几天再起来也能补齐。

// ReceivableReminder 是收件箱里的一条。
type ReceivableReminder struct {
	ID           int64
	ContractID   int64
	ContractNo   string
	CustomerName string
	// SOON / DUE / OVERDUE
	Type       string
	PeriodNo   int32
	DueDate    string
	OpenAmount string
	Currency   string
	Title      string
	Content    string
	DetailURL  string
	CreatedAt  string
	Unread     bool
}

// SweepReceivableReminders 扫一趟，把该提醒而未提醒的写成站内信，返回新增条数。
//
// 跨租户：worker 没有租户上下文，而新开一个租户不该需要额外配置才被覆盖。
// 每一行写回的仍是那张合同自己的 tenant_id。
func (s *Service) SweepReceivableReminders(ctx context.Context) (int64, error) {
	return s.q.SweepReceivableReminders(ctx)
}

// ReceivableInbox 是某人的应收提醒收件箱：未读在前。
func (s *Service) ReceivableInbox(ctx context.Context, tenantID, employeeID int64, unreadOnly bool, limit int32) ([]ReceivableReminder, int64, error) {
	if employeeID == 0 {
		return nil, 0, apierr.Unauthorized("AUTH_TOKEN_MISSING", "缺少登录凭证")
	}
	if limit <= 0 || limit > 100 {
		limit = 50
	}
	rows, err := s.q.ListReceivableReminders(ctx, store.ListReceivableRemindersParams{
		TenantID: tenantID, EmployeeID: employeeID, UnreadOnly: unreadOnly, RowLimit: limit,
	})
	if err != nil {
		return nil, 0, err
	}
	out := make([]ReceivableReminder, 0, len(rows))
	var unread int64
	for _, r := range rows {
		unread = r.UnreadTotal
		out = append(out, ReceivableReminder{
			ID: r.ID, ContractID: r.ContractID, ContractNo: r.ContractNo,
			CustomerName: r.CustomerName, Type: r.ReminderType, PeriodNo: r.PeriodNo,
			DueDate: r.DueDate, OpenAmount: r.OpenAmount, Currency: r.Currency,
			Title: r.Title, Content: r.Content, DetailURL: r.DetailUrl,
			CreatedAt: r.CreatedAt.Time.Format(time.RFC3339), Unread: r.Unread,
		})
	}
	return out, unread, nil
}

// MarkReceivableRemindersRead 标记已读；ids 为空表示全部已读。
func (s *Service) MarkReceivableRemindersRead(ctx context.Context, tenantID, employeeID int64, ids []int64) (int64, error) {
	if employeeID == 0 {
		return 0, apierr.Unauthorized("AUTH_TOKEN_MISSING", "缺少登录凭证")
	}
	if ids == nil {
		ids = []int64{}
	}
	return s.q.MarkReceivableRemindersRead(ctx, store.MarkReceivableRemindersReadParams{
		TenantID: tenantID, EmployeeID: employeeID, Ids: ids,
	})
}

// RunReceivableReminderWorker 每天扫一趟。
//
// 每天一次就够：到期日是「天」这个粒度的事，一天之内多扫几遍不会多出
// 任何东西（唯一键挡着），少扫一遍也不会漏（范围判断而非等号）。
//
// 启动时先跑一趟，因为服务重启后第一件该做的事就是补上停机期间的提醒。
func (s *Service) RunReceivableReminderWorker(ctx context.Context, interval time.Duration, log *slog.Logger) {
	if interval <= 0 {
		interval = 24 * time.Hour
	}
	run := func() {
		n, err := s.SweepReceivableReminders(ctx)
		if err != nil {
			log.Error("应收提醒扫描失败，下一轮重试", "err", err)
			return
		}
		if n > 0 {
			log.Info("应收提醒已发出", "count", n)
		}
	}
	run()
	ticker := time.NewTicker(interval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			run()
		}
	}
}
