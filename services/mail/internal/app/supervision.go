package app

import (
	"context"
	"strings"
	"time"

	"github.com/sgao19/erp-go/pkg/apierr"
	"github.com/sgao19/erp-go/services/mail/internal/store"
)

// 老板端的员工邮箱监管（2026-09-18）。
//
// 这是**第二条读信的路**。第一条（页面上那个邮箱）要两样东西：信是你自己的
// （owner_id 对得上），和一把你亲手用密码换来的信箱令牌。这条路两样都没有：
// 看的人多半连那个箱的密码都不知道，也不该知道。
//
// 所以这条路自己立三条规矩，缺一条这个功能就不该存在：
//
//  1. **要一条单独的权限**（网关那边是 mail:supervision:read）。不是"管理员
//     就行"，是一条能单独授、单独收的权限。
//  2. **每看一次留一行痕**，写不进去就不给看。见迁移 00068。
//  3. **只读，一个字节都不改**。不标已读、不推 Seen 到邮件服务器、不碰
//     文件夹——员工在 Foxmail 里不会因为老板看过而看到任何变化。第三条
//     靠 inboundFor(markRead=false) 兑现。
//
// 按国家分组不在这一层：国家长在客户身上（customers.country_code），是
// 主数据那边的事，网关把两边拼起来。这里只管"谁有信箱、他的信是什么"。

// SupervisedEmployee 是「有信箱、因而可看」的一个人。名字不在这儿——这个
// 服务不认识员工表，网关去 IAM 那边补。
type SupervisedEmployee struct {
	EmployeeID int64
	Mailboxes  int64
	Unread     int64
	LastReadAt time.Time
}

// SupervisionViewer 是正在看的那个人。三样都要落进日志。
type SupervisionViewer struct {
	EmployeeID int64
	Name       string
	ClientIP   string
}

// SupervisionEntry 是登记簿上的一行。
type SupervisionEntry struct {
	ID         int64
	ViewerID   int64
	ViewerName string
	TargetID   int64
	TargetName string
	Action     string
	Detail     string
	ClientIP   string
	ViewedAt   time.Time
}

const (
	supervisionList = "LIST"
	supervisionOpen = "OPEN"
)

// 监管能看的文件夹。**先只有这两个**：收件箱和已发送就是"他跟客户来往了
// 什么"的全部，草稿和垃圾箱是另一类东西（没发出去的、和不是他选的），
// 等真有人要再加。
const (
	SupervisionInbox = "inbox"
	SupervisionSent  = "sent"
)

// SupervisableEmployees 是这家公司里有信箱的那些人。
func (s *Service) SupervisableEmployees(ctx context.Context, tenantID int64) ([]SupervisedEmployee, error) {
	rows, err := s.q.ListSupervisableEmployees(ctx, tenantID)
	if err != nil {
		return nil, err
	}
	out := make([]SupervisedEmployee, 0, len(rows))
	for _, r := range rows {
		e := SupervisedEmployee{EmployeeID: r.EmployeeID, Mailboxes: r.Mailboxes, Unread: r.Unread}
		if r.LastReadAt.Valid {
			e.LastReadAt = r.LastReadAt.Time
		}
		out = append(out, e)
	}
	return out, nil
}

// SuperviseFolder 是某个员工某个文件夹的列表。
//
// accountID 不给：监管看的是「这个人的信」，不是「这个人某个箱的信」。他绑
// 了三个箱，三个箱的信就都在这儿——要分箱是另一个需求，先别替人猜。
func (s *Service) SuperviseFolder(
	ctx context.Context, tenantID int64, who SupervisionViewer,
	target int64, targetName, folder, keyword, cursor string, size int32,
) (InboundPage, error) {
	folder = strings.ToLower(strings.TrimSpace(folder))
	if folder == "" {
		folder = SupervisionInbox
	}
	if folder != SupervisionInbox && folder != SupervisionSent {
		return InboundPage{}, apierr.Invalid("MAIL_SUPERVISION_FOLDER", "只能看收件箱和已发送")
	}
	if target <= 0 {
		return InboundPage{}, apierr.Invalid("MAIL_SUPERVISION_TARGET", "缺少要查看的员工")
	}
	if err := s.logSupervision(ctx, tenantID, who, target, targetName, supervisionList, folder); err != nil {
		return InboundPage{}, err
	}
	if folder == SupervisionSent {
		return s.ListMailboxSent(ctx, tenantID, target, 0, keyword, cursor, size, ListSort{})
	}
	return s.ListInbound(ctx, tenantID, target, 0, keyword, "INBOX", cursor, size, ListSort{}, false)
}

// SuperviseMail 是具体一封信：正文、附件、整条会话。
//
// **不标已读**（inboundFor 的第四个参数）。理由见文件头第三条。
func (s *Service) SuperviseMail(
	ctx context.Context, tenantID int64, who SupervisionViewer,
	target int64, targetName string, id int64,
) (InboundView, []ThreadItem, error) {
	if target <= 0 || id <= 0 {
		return InboundView{}, nil, apierr.Invalid("MAIL_SUPERVISION_TARGET", "缺少要查看的员工或邮件")
	}
	// 先取信，再记痕：日志里要写主题，而主题只有取到信才知道。取不到就
	// 什么都没发生，也就没有什么可记的。
	view, err := s.inboundFor(ctx, tenantID, target, id, false)
	if err != nil {
		return InboundView{}, nil, err
	}
	if err := s.logSupervision(ctx, tenantID, who, target, targetName, supervisionOpen, view.Subject); err != nil {
		return InboundView{}, nil, err
	}
	// 整条会话跟着给：一封回信单独看是半句话。取不到就只给这一封——
	// 少一段上下文，比整个页面打不开强。
	thread, err := s.GetMailThread(ctx, tenantID, target, id, view.ThreadKey)
	if err != nil {
		s.log.Warn("could not read the thread under supervision",
			"target", target, "mail", id, "err", err)
		thread = nil
	}
	return view, thread, nil
}

// SupervisionLog 是登记簿。targetID 传 0 表示不限某个人。
func (s *Service) SupervisionLog(ctx context.Context, tenantID, targetID int64, limit int32) ([]SupervisionEntry, error) {
	if limit <= 0 || limit > 200 {
		limit = 100
	}
	rows, err := s.q.ListSupervisionLog(ctx, store.ListSupervisionLogParams{
		TenantID: tenantID, TargetID: targetID, RowLimit: limit,
	})
	if err != nil {
		return nil, err
	}
	out := make([]SupervisionEntry, 0, len(rows))
	for _, r := range rows {
		e := SupervisionEntry{
			ID: r.ID, ViewerID: r.ViewerID, ViewerName: r.ViewerName,
			TargetID: r.TargetID, TargetName: r.TargetName,
			Action: r.Action, Detail: r.Detail, ClientIP: r.ClientIp,
		}
		if r.ViewedAt.Valid {
			e.ViewedAt = r.ViewedAt.Time
		}
		out = append(out, e)
	}
	return out, nil
}

// logSupervision 写登记簿。**写不进去就不给看**。
//
// 别处的日志写失败大多只记一条 warn 就过去了——那些日志丢一行是遗憾，这一行
// 丢了是"有人读了别人的信而没有任何记录"。这个功能立得住的前提就是这一行，
// 所以它失败时这次读取也不发生。和导出那条路同一个道理。
func (s *Service) logSupervision(
	ctx context.Context, tenantID int64, who SupervisionViewer,
	target int64, targetName, action, detail string,
) error {
	if who.EmployeeID <= 0 {
		return apierr.Invalid("MAIL_SUPERVISION_VIEWER", "认不出是谁在查看，拒绝")
	}
	// 主题可能很长（有人会把整段需求写进主题）。截一下：日志是用来回答
	// "看了哪一封"的，不是用来存正文的。
	if len([]rune(detail)) > 200 {
		detail = string([]rune(detail)[:200]) + "…"
	}
	err := s.q.CreateSupervisionLog(ctx, store.CreateSupervisionLogParams{
		TenantID: tenantID,
		ViewerID: who.EmployeeID, ViewerName: who.Name,
		TargetID: target, TargetName: targetName,
		Action: action, Detail: detail, ClientIp: who.ClientIP,
	})
	if err != nil {
		s.log.Error("could not record a supervision view; refusing the read",
			"viewer", who.EmployeeID, "target", target, "action", action, "err", err)
		return apierr.Invalid("MAIL_SUPERVISION_NOT_LOGGED", "这次查看没能记入日志，已中止")
	}
	return nil
}
