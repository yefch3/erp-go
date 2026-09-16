package app

import (
	"context"
	"errors"
	"strings"

	"github.com/jackc/pgx/v5"

	"github.com/sgao19/erp-go/pkg/apierr"
	"github.com/sgao19/erp-go/services/mail/internal/store"
)

// 主邮箱：公司的邮箱，员工只是暂时拿着用。见迁移 00067。
//
// 和员工自己绑邮箱（VerifyMailSecret）比，只有三处不同：
//
//   - **谁动的手。** 管理员。所以 employeeID 来自请求、actor 来自登录令牌——
//     这是有意开的第二条进凭据存储的路。mailbind.go 文件头那条「绑的永远是
//     自己名下」的规矩对员工那条路仍然成立；这条路网关只放行「管理员工账号」
//     的权限，和开账号、重置密码同一档。
//   - **kind = COMPANY。** 员工那边看到它没有「解绑」、改不了密码（界面不显示，
//     服务层再拒一道）；登录 ERP 时网关替他开锁，不问密码。
//   - **分配即默认。** 写信时预选它。
//
// 验证、落库、留痕走的都是员工那条路的同一套函数——两条路各写一套，改一处
// 忘一处，是这类代码最常见的死法。
const (
	mailKindPersonal = "PERSONAL"
	mailKindCompany  = "COMPANY"
)

const (
	// 管理员分配主邮箱。
	bindActionAssign = "ASSIGN"
	// 换主邮箱时收回旧的那一条留痕。detail 里写换成了哪个。
	bindActionReplace = "REPLACE"
)

// errCompanyMailboxManaged 是员工碰主邮箱时得到的那一句。三处用（改密码、
// 解绑、以后的退出），话要一样。
var errCompanyMailboxManaged = apierr.Permission("MAIL_COMPANY_MAILBOX_MANAGED",
	"这是公司分配的主邮箱，密码和解绑由管理员管理")

// CompanyMailboxView 是「这个人现在拿着的主邮箱」——管理员在员工详情页看
// 的，和网关登录时问的，是同一份。
type CompanyMailboxView struct {
	AccountID  int64
	Email      string
	VerifiedAt string
	// AuthFailed 说 LastError 是不是凭据问题：密码被改了、被服务商锁了。
	// 那时管理员要重新填一次密码——员工自己是填不了的。
	AuthFailed bool
	LastError  string
	IsActive   bool
	AssignedAt string
}

// AssignCompanyMailbox 由管理员（actorID）把一个邮箱分配给员工（employeeID）
// 当主邮箱。密码由管理员现在输入，拿去邮件服务器真的登录一次，成功了才存。
//
// 已经有一个主邮箱的，**换掉**：旧的收回（留历史，见 UnbindMailAccount），
// 新的分上。一个人同时只有一个——00067 的部分唯一索引也这么要求。
func (s *Service) AssignCompanyMailbox(
	ctx context.Context, tenantID, actorID, employeeID int64, in BindRequest,
) (BindResult, error) {
	if employeeID <= 0 {
		return BindResult{}, apierr.Invalid("MAIL_EMPLOYEE_REQUIRED", "缺少员工")
	}
	email := strings.ToLower(strings.TrimSpace(in.Email))
	secret := strings.TrimSpace(in.Secret)
	if email == "" || secret == "" {
		return BindResult{}, apierr.Invalid("MAIL_ADDRESS_AND_CODE_REQUIRED", "请填写主邮箱地址和密码")
	}
	if !strings.Contains(email, "@") || strings.HasSuffix(email, "@") {
		return BindResult{}, apierr.Invalid("MAIL_ADDRESS_INVALID", "邮箱地址格式不对")
	}
	hosts, err := s.resolveHosts(ctx, tenantID, email, in)
	if err != nil {
		s.recordBinding(ctx, tenantID, employeeID, actorID, 0, email, in.Provider,
			bindActionFailed, err.Error())
		return BindResult{}, err
	}
	// 地址已经是别人的：现在就说，别先去邮件服务器登录一次再撞唯一约束。
	// 「收回 A 的、分给 B」是第二步（交接）的事，这一步只分没人占着的地址。
	username := ""
	if prev, err := s.q.GetMailAccountByEmail(ctx, store.GetMailAccountByEmailParams{
		TenantID: tenantID, Email: email,
	}); err == nil {
		if prev.EmployeeID != employeeID {
			return BindResult{}, apierr.Conflict("MAIL_ADDRESS_TAKEN",
				"这个邮箱地址已经被本公司的另一个人绑定了")
		}
		username = prev.Username
	}
	if s.mailbox != nil {
		if err := s.mailbox.VerifyLogin(ctx, MailAccount{
			EmployeeID: employeeID,
			Email:      email, Username: username, Secret: secret,
			IMAPHost:     hosts.IMAPHost,
			IMAPPort:     int(hosts.IMAPPort),
			IMAPSecurity: hosts.IMAPSecurity,
		}); err != nil {
			s.recordBinding(ctx, tenantID, employeeID, actorID, 0, email, in.Provider,
				bindActionFailed, err.Error())
			return BindResult{}, hostRejected{err}
		}
	}
	// 一个人同时只有一个主邮箱：已经拿着一个别的地址的，先收回它。
	// 放在真的登录成功**之后**：新的登不上，旧的不能先没了。
	if cur, err := s.q.GetCompanyMailboxForEmployee(ctx, store.GetCompanyMailboxForEmployeeParams{
		TenantID: tenantID, EmployeeID: employeeID,
	}); err == nil && cur.Email != email {
		if _, err := s.q.UnbindMailAccount(ctx, store.UnbindMailAccountParams{
			TenantID: tenantID, ID: cur.ID, EmployeeID: employeeID,
		}); err != nil {
			return BindResult{}, err
		}
		s.recordBinding(ctx, tenantID, employeeID, actorID, cur.ID, cur.Email, "",
			bindActionReplace, "换成 "+email)
	}
	res, err := s.storeBinding(ctx, tenantID, employeeID, email, username, secret, hosts, in.Provider,
		mailKindCompany, actorID, bindActionAssign)
	if err != nil {
		return BindResult{}, err
	}
	// 分配即默认：写信时预选它。失败只记日志——箱已经分好了，默认哪个是
	// 次要的，员工自己也能改。
	if err := s.SetDefaultMailbox(ctx, tenantID, employeeID, res.AccountID); err != nil {
		s.log.Warn("could not make the company mailbox the default",
			"employee", employeeID, "account", res.AccountID, "err", err)
	}
	if s.mailbox == nil {
		res.Detail = "邮件通道未启用，已保存账号"
	}
	return res, nil
}

// CompanyMailboxOf 说这个人现在拿着哪个主邮箱。第二个返回值 false = 没有。
func (s *Service) CompanyMailboxOf(ctx context.Context, tenantID, employeeID int64) (CompanyMailboxView, bool, error) {
	row, err := s.q.GetCompanyMailboxForEmployee(ctx, store.GetCompanyMailboxForEmployeeParams{
		TenantID: tenantID, EmployeeID: employeeID,
	})
	if errors.Is(err, pgx.ErrNoRows) {
		return CompanyMailboxView{}, false, nil
	}
	if err != nil {
		return CompanyMailboxView{}, false, err
	}
	v := CompanyMailboxView{
		AccountID: row.ID, Email: row.Email,
		AuthFailed: row.AuthFailed, LastError: row.LastError, IsActive: row.IsActive,
	}
	if row.VerifiedAt.Valid {
		v.VerifiedAt = row.VerifiedAt.Time.Format("2006-01-02 15:04")
	}
	if row.CreatedAt.Valid {
		v.AssignedAt = row.CreatedAt.Time.Format("2006-01-02 15:04")
	}
	return v, true, nil
}
