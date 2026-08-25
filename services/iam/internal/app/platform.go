package app

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"

	"github.com/sgao19/erp-go/pkg/apierr"
	"github.com/sgao19/erp-go/pkg/pgdb"
	"github.com/sgao19/erp-go/services/iam/internal/store"
)

// 平台开户（E7）：操作员给客户公司的第一位管理员发邀请，对方点开链接激活，
// 之后由他自己邀请本公司员工。
//
// 和配置路径（EnsureExtraTenant）的根本区别在身份证明：那边是操作员当场断言
// 地址、直接发密码；这边地址由本人点开发到自己邮箱的链接来证明，密码也由本人
// 在激活页设置——「我说是就是」换成「他真的收到了」。
//
// 谁能调这里的每一个方法，由 platform_operators 名单决定，不由权限系统决定。
// 理由见 00044 的表注释：每家新公司的超管自动持全部权限，任何做成权限的
// 「开公司」都会流进客户管理员手里。

var errPlatformOnly = apierr.Permission("IAM_PLATFORM_ONLY", "只有平台操作员可以执行此操作")

// PlatformTenant 是开户页清单里的一行。
type PlatformTenant struct {
	ID             int64
	Name           string
	Status         string
	CreatedAt      time.Time
	AdminEmail     string
	AdminActivated bool
}

// IsPlatformOperator 报告这位员工在不在平台名单上。给网关的导航探测用；
// 各写操作自己也查，不依赖调用方问过。
func (s *Service) IsPlatformOperator(ctx context.Context, employeeID int64) (bool, error) {
	return s.q.IsPlatformOperator(ctx, employeeID)
}

func (s *Service) requireOperator(ctx context.Context, employeeID int64) error {
	ok, err := s.q.IsPlatformOperator(ctx, employeeID)
	if err != nil {
		return err
	}
	if !ok {
		return errPlatformOnly
	}
	return nil
}

// PlatformTenants 列出每家公司与其管理员的激活状态。
func (s *Service) PlatformTenants(ctx context.Context, operatorID int64) ([]PlatformTenant, error) {
	if err := s.requireOperator(ctx, operatorID); err != nil {
		return nil, err
	}
	rows, err := s.q.ListTenantsForPlatform(ctx)
	if err != nil {
		return nil, err
	}
	out := make([]PlatformTenant, 0, len(rows))
	for _, r := range rows {
		t := PlatformTenant{
			ID: r.ID, Name: r.Name, Status: r.Status,
			AdminEmail: r.AdminEmail, AdminActivated: r.AdminActivated,
		}
		if r.CreatedAt.Valid {
			t.CreatedAt = r.CreatedAt.Time
		}
		out = append(out, t)
	}
	return out, nil
}

// PlatformCreateTenant 开一家公司并给它的第一位管理员发一张邀请。
//
// 开出来的公司**没有任何邮箱域名**——这是 E7 定下的设计修正：域名检查防的是
// 员工邀请手滑，不是开户；第一位管理员的地址由他自己点激活链接证明。所以任意
// 多家用 @263.net、@gmail.com 邮箱的公司都能开，互不阻塞。域名之后由公司在
// 自己的设置里补。
//
// 返回的 Invitation 带着一次性 token。发信是网关的事（邀请信从操作员自己绑定
// 的邮箱发出）——iam 没有邮件客户端，也不能有：mail 服务依赖 iam，反向的边会
// 成环。这和员工邀请的分工一模一样。
func (s *Service) PlatformCreateTenant(ctx context.Context, operatorID int64, companyName, adminEmail string) (PlatformTenant, Invitation, error) {
	if err := s.requireOperator(ctx, operatorID); err != nil {
		return PlatformTenant{}, Invitation{}, err
	}
	companyName = strings.TrimSpace(companyName)
	adminEmail = strings.ToLower(strings.TrimSpace(adminEmail))
	if companyName == "" {
		return PlatformTenant{}, Invitation{}, apierr.Invalid("IAM_PLATFORM_NAME_REQUIRED", "请填写公司名称")
	}
	if at := strings.LastIndex(adminEmail, "@"); at < 1 || at == len(adminEmail)-1 {
		return PlatformTenant{}, Invitation{}, apierr.Invalid("IAM_PLATFORM_EMAIL_INVALID", "请填写有效的管理员邮箱")
	}

	var tenantID, adminID int64
	err := pgdb.InTx(ctx, s.pool, func(ctx context.Context, tx pgx.Tx) error {
		var err error
		tenantID, adminID, _, err = s.seedTenantCore(ctx, s.q.WithTx(tx), companyName, adminEmail)
		return err
	})
	if err != nil {
		// 地址是全系统的身份（00023 的唯一索引）。撞上它的不是异常，是开户页
		// 最常见的用户错误，要说人话。
		var pgErr *pgconn.PgError
		if errors.As(err, &pgErr) && pgErr.Code == "23505" &&
			strings.Contains(pgErr.ConstraintName, "email") {
			return PlatformTenant{}, Invitation{}, apierr.Conflict("IAM_PLATFORM_EMAIL_TAKEN",
				fmt.Sprintf("邮箱 %s 已属于系统里的另一个账号", adminEmail))
		}
		return PlatformTenant{}, Invitation{}, err
	}

	// 事务提交后再铸邀请：邀请表有自己的一次性语义，失败了公司也已经开好，
	// 操作员在页面上点「重发邀请」就能补——比把两件事绑成一个事务、让邮箱
	// 打嗝连累开户要稳。
	inv, err := s.InviteEmployee(ctx, tenantID, adminID, operatorID)
	if err != nil {
		return PlatformTenant{}, Invitation{}, fmt.Errorf("公司已开出（id=%d），但邀请生成失败: %w", tenantID, err)
	}
	s.log.Info("platform: tenant created and admin invited",
		"tenant_id", tenantID, "tenant", companyName, "admin", adminEmail, "operator", operatorID)
	return PlatformTenant{
		ID: tenantID, Name: companyName, Status: "ACTIVE",
		CreatedAt: time.Now(), AdminEmail: adminEmail,
	}, inv, nil
}

// PlatformReinvite 给还没激活的管理员再铸一张邀请（旧链接随之作废）。
func (s *Service) PlatformReinvite(ctx context.Context, operatorID, tenantID int64) (Invitation, error) {
	if err := s.requireOperator(ctx, operatorID); err != nil {
		return Invitation{}, err
	}
	admin, err := s.q.FindTenantAdmin(ctx, tenantID)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return Invitation{}, apierr.NotFound("IAM_PLATFORM_NO_ADMIN", "这家公司没有管理员档案")
		}
		return Invitation{}, err
	}
	// 已激活会被 InviteEmployee 自己拒绝（IAM_INVITE_ALREADY_ACTIVE）——那不是
	// 重发，是披着邀请外衣的改密码。
	return s.InviteEmployee(ctx, tenantID, admin.ID, operatorID)
}

// PlatformSetTenantStatus 启停一家公司。SUSPENDED 直接挡登录（Login 里已有）。
func (s *Service) PlatformSetTenantStatus(ctx context.Context, operatorID, operatorTenantID, tenantID int64, status string) error {
	if err := s.requireOperator(ctx, operatorID); err != nil {
		return err
	}
	if status != "ACTIVE" && status != "SUSPENDED" {
		return apierr.Invalid("IAM_PLATFORM_BAD_STATUS", "状态只能是 ACTIVE 或 SUSPENDED")
	}
	// 操作员停用自己所在的公司 = 把自己锁在门外，而能解锁的入口正是被锁的
	// 这个页面。这不是权限问题，是把钥匙反锁进屋的问题，直接拒绝。
	if tenantID == operatorTenantID && status == "SUSPENDED" {
		return apierr.Invalid("IAM_PLATFORM_SELF_SUSPEND", "不能停用平台操作员自己所在的公司")
	}
	n, err := s.q.SetTenantStatus(ctx, store.SetTenantStatusParams{ID: tenantID, Status: status})
	if err != nil {
		return err
	}
	if n == 0 {
		return apierr.NotFound("IAM_PLATFORM_TENANT_NOT_FOUND", "公司不存在")
	}
	s.log.Info("platform: tenant status changed",
		"tenant_id", tenantID, "status", status, "operator", operatorID)
	return nil
}
