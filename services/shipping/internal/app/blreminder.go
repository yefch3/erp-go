package app

import (
	"context"
	"log/slog"
	"time"

	"github.com/sgao19/erp-go/pkg/apierr"
	"github.com/sgao19/erp-go/pkg/grpcx"
	"github.com/sgao19/erp-go/services/shipping/internal/store"
)

// 提单签发提醒（E2）。
//
// 业务的原话：「船出发了以后，要提醒船务部门，签提单要及时，整个部门的」。
//
// 两件事和到港提醒不同：
//
// 一、**收件人是一群人**。到港提醒发给船期负责人一个人；提单签发是部门
// 的活，谁在谁签。但表里仍然**一人一行**——部门是收件人的来源，不是收件
// 人本身。展开成多行之后「谁读了、谁还没读」才是可回答的问题；发一条挂
// 在部门名下，结果是谁都以为别人会处理。
//
// 二、**触发是「一直没做」而不是「时间到了」**。到港提醒是 ETA 前 N 天响
// 一次；提单是开船后还没签就一直催，每 3 天一轮，直到正本上传为止。

// Directory 按角色找人。船期服务不自己解释组织架构，同 Scopes 的分工。
type Directory interface {
	EmployeeIDsByRole(ctx context.Context, roleCode string) ([]int64, error)
}

// UseDirectory 在生产启动时注入。没注入时提醒只发给船期负责人——
// 少发比不发好，也比让服务起不来好。
func (s *Service) UseDirectory(d Directory) { s.directory = d }

// blReminderRole 是「船务部门」在系统里的对应物。
//
// 用角色而不是部门：部门在不同公司叫法不一（操作部、单证部、物流部），
// 角色是按职能定的，LOGISTICS 就是管船的那批人。真要按部门发，换成
// 一个部门查询即可，收件人展开那段逻辑不用动。
const blReminderRole = "LOGISTICS"

// BLReminder 是收件箱里的一条。
type BLReminder struct {
	ID           int64
	ScheduleID   int64
	ScheduleNo   string
	VesselName   string
	VoyageNo     string
	ContractNo   string
	CustomerName string
	PeriodNo     int32
	DepartedOn   string
	Title        string
	Content      string
	DetailURL    string
	CreatedAt    string
	Unread       bool
}

// SweepBLReminders 扫一趟：船开了、提单正本还没上传的，给船务这批人各发
// 一条。返回新增条数。
//
// 收件人 = 物流角色的所有人 + 每张船期自己的负责人。前者是「部门」，后者
// 保证即使角色没配好，至少经办人收得到。
func (s *Service) SweepBLReminders(ctx context.Context) (int64, error) {
	tenants, err := s.q.DistinctTenantIDs(ctx)
	if err != nil {
		return 0, err
	}
	var total int64
	for _, tenantID := range tenants {
		// 「物流部都有谁」必须按**当前这家公司**问，而且必须在循环里面问。
		//
		// 原来这一问在循环外面，用的是后台任务自己的上下文——那上下文里没有
		// 登录用户，而服务之间调用时「没有登录用户」会被当成 1 号公司处理
		// （见 pkg/grpcx/interceptor.go 里的 `if op.TenantID == 0`）。于是拿
		// 回来的永远是第一家公司的物流人员，然后被塞进每一家公司的收件人名
		// 单里：第二家的物流人员收不到提醒，而库里攒下一批收件人是别家员工、
		// 公司号却是本家的提醒——读的时候两边都对不上，谁也看不见。
		var recipients []int64
		if s.directory != nil {
			scoped := grpcx.WithOperator(ctx, grpcx.Operator{TenantID: tenantID})
			if ids, dirErr := s.directory.EmployeeIDsByRole(scoped, blReminderRole); dirErr == nil {
				recipients = ids
			}
			// 查不到就退回「只发负责人」——目录服务抖一下不该让提醒全停。
		}
		ids, err := s.q.ScheduleOwnersPendingBL(ctx, tenantID)
		if err != nil {
			return total, err
		}
		n, err := s.q.SweepBLReminders(ctx, store.SweepBLRemindersParams{
			TenantID: tenantID, RecipientIds: mergeIDs(recipients, ids),
		})
		if err != nil {
			return total, err
		}
		total += n
	}
	return total, nil
}

// mergeIDs 并集去重，且丢掉 0——没有负责人不是一个收件人。
func mergeIDs(a, b []int64) []int64 {
	seen := make(map[int64]struct{}, len(a)+len(b))
	out := make([]int64, 0, len(a)+len(b))
	for _, group := range [][]int64{a, b} {
		for _, id := range group {
			if id <= 0 {
				continue
			}
			if _, ok := seen[id]; ok {
				continue
			}
			seen[id] = struct{}{}
			out = append(out, id)
		}
	}
	return out
}

// BLInbox 是某人的提单提醒收件箱。
func (s *Service) BLInbox(ctx context.Context, tenantID, employeeID int64, unreadOnly bool, limit int32) ([]BLReminder, int64, error) {
	if employeeID == 0 {
		return nil, 0, apierr.Unauthorized("AUTH_TOKEN_MISSING", "缺少登录凭证")
	}
	if limit <= 0 || limit > 100 {
		limit = 50
	}
	rows, err := s.q.ListBLReminders(ctx, store.ListBLRemindersParams{
		TenantID: tenantID, EmployeeID: employeeID, UnreadOnly: unreadOnly, RowLimit: limit,
	})
	if err != nil {
		return nil, 0, err
	}
	out := make([]BLReminder, 0, len(rows))
	var unread int64
	for _, r := range rows {
		unread = r.UnreadTotal
		out = append(out, BLReminder{
			ID: r.ID, ScheduleID: r.ScheduleID, ScheduleNo: r.ScheduleNo,
			VesselName: r.VesselName, VoyageNo: r.VoyageNo, ContractNo: r.ContractNo,
			CustomerName: r.CustomerName, PeriodNo: r.PeriodNo, DepartedOn: r.DepartedOn,
			Title: r.Title, Content: r.Content, DetailURL: r.DetailUrl,
			CreatedAt: r.CreatedAt.Time.Format(time.RFC3339), Unread: r.Unread,
		})
	}
	return out, unread, nil
}

// MarkBLRemindersRead 标记已读；ids 为空表示全部。
func (s *Service) MarkBLRemindersRead(ctx context.Context, tenantID, employeeID int64, ids []int64) (int64, error) {
	if employeeID == 0 {
		return 0, apierr.Unauthorized("AUTH_TOKEN_MISSING", "缺少登录凭证")
	}
	if ids == nil {
		ids = []int64{}
	}
	return s.q.MarkBLRemindersRead(ctx, store.MarkBLRemindersReadParams{
		TenantID: tenantID, EmployeeID: employeeID, Ids: ids,
	})
}

// RunBLReminderWorker 每天扫一趟。
//
// 每天一次够用：提单这件事的粒度是「天」，一天内多扫几遍不会多出任何东西
// （唯一键挡着），少扫一遍也不会漏（范围判断而非等号）。启动先跑一次，
// 补上停机期间该发的。
func (s *Service) RunBLReminderWorker(ctx context.Context, interval time.Duration, log *slog.Logger) {
	if interval <= 0 {
		interval = 24 * time.Hour
	}
	run := func() {
		n, err := s.SweepBLReminders(ctx)
		if err != nil {
			log.Error("提单提醒扫描失败，下一轮重试", "err", err)
			return
		}
		if n > 0 {
			log.Info("提单签发提醒已发出", "count", n)
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
