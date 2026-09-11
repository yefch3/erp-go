package app

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"

	"github.com/sgao19/erp-go/pkg/apierr"
	"github.com/sgao19/erp-go/pkg/pgdb"
	"github.com/sgao19/erp-go/services/iam/internal/store"
)

// 改登录邮箱。
//
// 这个文件存在是因为「保存」这个动作对邮箱这一列来说是错的。登录同时看两样东西：
// employees.email 找到账号，employees.email_verified_at 证明这个信箱真的存在、
// 真的是他的。UpdateEmployeeDetails 只写前者——新地址凭空继承了旧地址挣来的
// 那个「已验证」，而系统里没有任何一处会去问「这个信箱收得到信吗」。
//
// 所以改邮箱走和激活同一条路：发一封信到**新地址**，点开了才落库，
// 地址和验证时间一起写（SetEmployeeEmailVerified，激活也用它）。
// 打错字的代价变成「信退回来了，什么都没改」，旧地址照常能登录。
//
// ─────────────────────────────────────────────────────────────────────────
// 为什么不复用 employee_invitations 那张表
//
// 形状一样，判断相反。invitationFault 里有三条对邀请正确、对改邮箱恰好反过来：
//
//   · 已经验证过的人不能再邀请（errInviteAlreadyActive）——而改邮箱**只**发生在
//     已经验证过的人身上
//   · 兑换时 employees.email 必须还等于链接发往的地址——而改邮箱的全部意义
//     就是这两个不相等
//   · 兑换要设密码——改邮箱的人早就有密码了，再问一次是凭空多一道坎，
//     还把「读到这封信」升级成了「能改密码」
//
// 复用那张表就是把这三条判断改成 if 分支，让一段本来说得很清楚的代码开始
// 讨价还价。两张表、两条路，各自的规则都能一眼读完。
//
// ─────────────────────────────────────────────────────────────────────────
// 不碰 mail_accounts —— 这是想清楚之后的决定，不是漏掉
//
// employees.email 是**登录身份**，由点开链接证明。
// mail_accounts.email 是**发件信箱**，由一次真实的 IMAP/SMTP 登录证明
// （mailaccount.go 的 VerifyMailSecret：先验证、验证成功才准写存储）。
//
// 两者在实际使用中通常是同一个地址，但代码里没有任何一处要求它们相等，
// 而把前者同步给后者会**弄坏信箱**：provider/smtp.go 拿 acct.Email 同时当
// From 头和 SMTP 信封发件人（c.Mail(acct.Email)），凭据却还是旧信箱的——
// 263 和 Gmail 都会直接拒信（550 sender not allowed）；OAuth 那条路更干脆，
// acct.Email 就是 XOAUTH2 的用户名，认证直接失败。
//
// 换句话说，「同步过去」会把一个能用的信箱改成一个不能用的信箱。真要改绑，
// 只有员工自己能做——授权码在他手里——而那条路（/api/mailbox/verify）已经在了。
// 这里能做的是**说一声**，见页面上的提示。
// ─────────────────────────────────────────────────────────────────────────

const (
	// 三天，比邀请的七天短。
	//
	// 邀请是发给一个还进不来的人的，他可能在休假，等他回来是唯一的选择。
	// 改邮箱是发给一个**现在就能正常登录**的人的：链接死了他没有任何损失，
	// 让管理员再点一次「重新发送」就行。窗口短一点，等于活着的钥匙少一点。
	emailChangeTTL = 72 * time.Hour

	emailChangeTokenBytes = 32
)

var (
	errEmailChangeSameAddress = apierr.Invalid("IAM_EMAIL_CHANGE_SAME",
		"新邮箱和当前邮箱一样，无需变更")
	errEmailChangeNotVerified = apierr.Conflict("IAM_EMAIL_CHANGE_NOT_VERIFIED",
		"该员工还没有激活账号，请直接修改邮箱后重新发送邀请")
	errEmailChangeTaken = apierr.Conflict("IAM_EMAIL_CHANGE_TAKEN",
		"这个邮箱已经被其他账号使用")
	errEmailChangePendingElsewhere = apierr.Conflict("IAM_EMAIL_CHANGE_PENDING_ELSEWHERE",
		"这个邮箱已经有另一位员工在变更中，请等对方确认或取消后再试")
	errEmailChangeNotActive = apierr.Conflict("IAM_EMAIL_CHANGE_NOT_ACTIVE",
		"该员工已离职或停用，无法变更邮箱")

	// 链接失效的四种原因分开说。对着其中三种讲「请重试」是废话，
	// 而看到这句话的人没有别的渠道可问。
	errConfirmBadToken = apierr.Invalid("IAM_EMAIL_CHANGE_BAD_TOKEN",
		"确认链接无效，请向管理员索取新的链接")
	errConfirmUsed = apierr.Invalid("IAM_EMAIL_CHANGE_USED",
		"这个链接已经用过了，新邮箱已经生效，请直接用新邮箱登录")
	errConfirmExpired = apierr.Invalid("IAM_EMAIL_CHANGE_EXPIRED",
		"确认链接已过期，请向管理员索取新的链接")
	errConfirmMoved = apierr.Invalid("IAM_EMAIL_CHANGE_MOVED",
		"该账号的邮箱在这封信发出后又被改过，此链接不再有效")
	errConfirmUnavailable = apierr.Invalid("IAM_EMAIL_CHANGE_UNAVAILABLE",
		"账号当前不可变更邮箱，请联系管理员")
)

// EmailChange 是发信需要的一切。token 在这条返回路径上出现且仅出现一次：
// 它从不从存储里读回来，因为存储里只有它的哈希。
type EmailChange struct {
	Token     string
	NewEmail  string
	OldEmail  string
	Name      string
	ExpiresAt time.Time
}

// RequestEmailChange 记下「这个人要改成那个地址」并铸一把一次性钥匙。
//
// 和 InviteEmployee 一样**不发信**：iam 没有邮件客户端，也不可能有——
// mail 服务已经依赖 iam，反向的边会成环。发信是网关的事。
//
// 关键是这个函数**不改 employees.email**。在有人点开链接之前，
// 数据库里那个人的邮箱一个字都没动，他照常用旧地址登录。
func (s *Service) RequestEmailChange(ctx context.Context, tenantID, employeeID, requestedBy int64, newEmail string) (EmailChange, error) {
	newEmail = strings.ToLower(strings.TrimSpace(newEmail))
	if err := validateEmployeeEmail(newEmail); err != nil {
		return EmailChange{}, err
	}
	emp, err := s.q.GetEmployee(ctx, store.GetEmployeeParams{TenantID: tenantID, ID: employeeID})
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return EmailChange{}, apierr.NotFound("IAM_EMP_NOT_FOUND", "员工不存在")
		}
		return EmailChange{}, err
	}
	if emp.Status != "ACTIVE" {
		return EmailChange{}, errEmailChangeNotActive
	}
	// 没激活过的人不走这条路。他的地址从来没被证明过，没有什么「已验证」可以
	// 被凭空继承——直接改掉再发邀请才是对的，也正是管理员在发出邀请之前
	// 改一个打错的地址时想做的事。UpdateEmployee 保留了那条路。
	if !emp.EmailVerifiedAt.Valid {
		return EmailChange{}, errEmailChangeNotVerified
	}
	oldEmail := strings.ToLower(strings.TrimSpace(emp.Email))
	if newEmail == oldEmail {
		return EmailChange{}, errEmailChangeSameAddress
	}
	if err := s.checkCompanyAddress(ctx, tenantID, newEmail); err != nil {
		return EmailChange{}, err
	}
	// 先查一次「地址被占了吗」。查询侧的答案会过期（另一个人可能在这之后
	// 才占走），employees_email_key 才是真门——但那道门要到兑换那一刻才关，
	// 那时信已经发出去、人已经点过了，解释不清。这里查一次，是为了让
	// 绝大多数冲突在管理员还看着屏幕的时候就说清楚。
	taken, err := s.q.EmailBelongsToSomeoneElse(ctx, store.EmailBelongsToSomeoneElseParams{
		Email: newEmail, EmployeeID: employeeID,
	})
	if err != nil {
		return EmailChange{}, err
	}
	if taken {
		return EmailChange{}, errEmailChangeTaken
	}

	raw := make([]byte, emailChangeTokenBytes)
	if _, err := rand.Read(raw); err != nil {
		return EmailChange{}, fmt.Errorf("email change: token: %w", err)
	}
	token := base64.RawURLEncoding.EncodeToString(raw)
	sum := sha256.Sum256([]byte(token))
	expires := time.Now().Add(emailChangeTTL)

	err = pgdb.InTx(ctx, s.pool, func(ctx context.Context, tx pgx.Tx) error {
		q := s.q.WithTx(tx)
		// 替换，绝不累积。和插入同一个事务，所以这里失败不会让这个人
		// 手上一把活钥匙都不剩。
		if _, err := q.DeleteLiveEmailChanges(ctx, store.DeleteLiveEmailChangesParams{
			TenantID: tenantID, EmployeeID: employeeID,
		}); err != nil {
			return err
		}
		_, err := q.CreateEmailChange(ctx, store.CreateEmailChangeParams{
			TenantID: tenantID, EmployeeID: employeeID,
			NewEmail: newEmail, OldEmail: oldEmail,
			TokenHash:   sum[:],
			ExpiresAt:   pgtype.Timestamptz{Time: expires, Valid: true},
			RequestedBy: requestedBy,
		})
		if err != nil {
			// 撞上 employee_email_changes_live_address：别人也在改成这个地址。
			// 每个人自己那把锁（employee_email_changes_live）撞不上——同一个事务里
			// 刚刚删过。
			return translateUnique(err,
				errEmailChangePendingElsewhere.Code, errEmailChangePendingElsewhere.Msg)
		}
		// 「谁在什么时候要把谁的登录地址改到哪」，进变更记录，和发起同一个事务。
		// 这一行必须在信发出**之前**就落下：发信失败时，已经存在的那把钥匙
		// 也得有据可查。
		return q.InsertDirectoryChange(ctx, store.InsertDirectoryChangeParams{
			TenantID: tenantID, EntityType: "EMPLOYEE", EntityID: employeeID,
			Action:     "EMAIL_CHANGE_REQUESTED",
			BeforeData: emailJSON(oldEmail), AfterData: emailJSON(newEmail),
			OperatorID: requestedBy,
		})
	})
	if err != nil {
		return EmailChange{}, err
	}
	s.log.Info("email change requested",
		"tenant_id", tenantID, "employee_id", employeeID,
		"from", oldEmail, "to", newEmail, "requested_by", requestedBy)
	return EmailChange{
		Token: token, NewEmail: newEmail, OldEmail: oldEmail,
		Name: emp.Name, ExpiresAt: expires,
	}, nil
}

// CancelEmailChange 撤回一个还没被点开的变更。
//
// 存在的理由很实在：地址打错了，信发到了一个不存在的信箱，那把钥匙会在那里
// 躺三天，而员工列表上会一直挂着「邮箱变更待确认」。撤回让管理员能把它清掉，
// 也能立刻重新发起一次到正确的地址。
func (s *Service) CancelEmailChange(ctx context.Context, tenantID, employeeID int64) error {
	n, err := s.q.DeleteLiveEmailChanges(ctx, store.DeleteLiveEmailChangesParams{
		TenantID: tenantID, EmployeeID: employeeID,
	})
	if err != nil {
		return err
	}
	if n == 0 {
		return apierr.NotFound("IAM_EMAIL_CHANGE_NONE", "该员工没有待确认的邮箱变更")
	}
	s.log.Info("email change cancelled", "tenant_id", tenantID, "employee_id", employeeID)
	return nil
}

// PendingEmailChange 是员工列表那一列要显示的东西。
type PendingEmailChange struct {
	NewEmail  string
	ExpiresAt time.Time
}

// PendingEmailChanges 整个公司一条查询，不是每行一条。理由同 PendingInvitations。
func (s *Service) PendingEmailChanges(ctx context.Context, tenantID int64) (map[int64]PendingEmailChange, error) {
	rows, err := s.q.ListLiveEmailChanges(ctx, tenantID)
	if err != nil {
		return nil, err
	}
	out := make(map[int64]PendingEmailChange, len(rows))
	for _, r := range rows {
		out[r.EmployeeID] = PendingEmailChange{NewEmail: r.NewEmail, ExpiresAt: r.ExpiresAt.Time}
	}
	return out, nil
}

// EmailChangeTarget 是确认页在任何人证明任何事之前可以知道的：这封信是给谁的、
// 要把地址从哪改到哪。这里没有秘密——拿着 token 的人就是收到这封信的人。
type EmailChangeTarget struct {
	Name     string
	OldEmail string
	NewEmail string
}

// PeekEmailChange 回答「这个链接还有效吗」而不兑换它。
//
// 存在是为了让页面能在**问「确认吗」之前**就说「链接已过期」，而不是之后。
// 和 Confirm 共用同一个判断函数，故意的——两者不能各自演化出对
// 「什么叫可用的链接」的不同看法。
func (s *Service) PeekEmailChange(ctx context.Context, token string) (EmailChangeTarget, error) {
	c, err := s.usableEmailChange(ctx, token)
	if err != nil {
		return EmailChangeTarget{}, err
	}
	return EmailChangeTarget{Name: c.EmployeeName, OldEmail: c.OldEmail, NewEmail: c.NewEmail}, nil
}

// ConfirmedEmailChange 是网关在落库之后需要知道的：改的是谁，好把他的会话踢掉。
type ConfirmedEmailChange struct {
	TenantID   int64
	EmployeeID int64
	Name       string
	OldEmail   string
	NewEmail   string
}

// ConfirmEmailChange 兑换链接：地址和「已验证」一起落库。
//
// 不设密码、不发会话。点开这封信证明的是「这个信箱是我的」，不是
// 「我是这个账号的主人」——后者要靠密码，而他本来就有。把链接变成一条
// 进门的路，正是它绝不能变成的东西。
func (s *Service) ConfirmEmailChange(ctx context.Context, token string) (ConfirmedEmailChange, error) {
	c, err := s.usableEmailChange(ctx, token)
	if err != nil {
		return ConfirmedEmailChange{}, err
	}
	err = pgdb.InTx(ctx, s.pool, func(ctx context.Context, tx pgx.Tx) error {
		q := s.q.WithTx(tx)
		// 先花掉 token，行数就是竞态的答案。同一个链接被点两下时两个请求
		// 一起到这里，UPDATE 自己的 WHERE 决定哪一个是那次变更。
		n, err := q.ConsumeEmailChange(ctx, c.ID)
		if err != nil {
			return err
		}
		if n == 0 {
			return errConfirmUsed
		}
		// 地址和验证时间一起写。整件事就是为了这一行——激活用的也是它。
		if err := q.SetEmployeeEmailVerified(ctx, store.SetEmployeeEmailVerifiedParams{
			TenantID: c.TenantID, ID: c.EmployeeID, Email: c.NewEmail,
		}); err != nil {
			// 唯一索引：在这封信飞行的三天里，这个地址被别人占走了。
			// 前面那次预检查过，但查询侧的答案会过期，employees_email_key
			// 才是真门。
			return translateUnique(err, "IAM_EMAIL_CHANGE_TAKEN",
				"这个邮箱在此期间已经被其他账号使用，变更没有生效，请联系管理员")
		}
		// 操作人是**员工自己**：点开链接的是他，不是当初发起的那个管理员。
		// 记成管理员会让审计说错话——那个人三天前只是提议，真正让它生效的
		// 是这一次点击。
		return q.InsertDirectoryChange(ctx, store.InsertDirectoryChangeParams{
			TenantID: c.TenantID, EntityType: "EMPLOYEE", EntityID: c.EmployeeID,
			Action:     "EMAIL_CHANGED",
			BeforeData: emailJSON(c.OldEmail), AfterData: emailJSON(c.NewEmail),
			OperatorID: c.EmployeeID,
		})
	})
	if err != nil {
		return ConfirmedEmailChange{}, err
	}
	s.log.Info("email change confirmed",
		"tenant_id", c.TenantID, "employee_id", c.EmployeeID,
		"from", c.OldEmail, "to", c.NewEmail)
	return ConfirmedEmailChange{
		TenantID: c.TenantID, EmployeeID: c.EmployeeID, Name: c.EmployeeName,
		OldEmail: c.OldEmail, NewEmail: c.NewEmail,
	}, nil
}

// emailJSON 是变更记录里 before/after 那两个 JSONB 的内容。
//
// 只放邮箱一个字段，不是整行快照（snapshotJSON 那样）。变更记录会被
// 「谁能看」这件事管着——备注就在整行快照里，而这两条记录将来可能要给
// 员工本人看（是他点开的链接）。只放这一个字段，就不会有第二个泄漏口。
func emailJSON(addr string) []byte {
	b, err := json.Marshal(struct {
		Email string `json:"email"`
	}{Email: addr})
	if err != nil {
		return []byte(`{}`)
	}
	return b
}

// usableEmailChange 解析 token 并逐条拒绝它可能不好用的每个理由，
// 顺序经过挑选：坏 token 不会让任何昂贵的东西跑起来。
func (s *Service) usableEmailChange(ctx context.Context, token string) (store.GetEmailChangeByTokenRow, error) {
	var zero store.GetEmailChangeByTokenRow
	token = strings.TrimSpace(token)
	if token == "" {
		return zero, errConfirmBadToken
	}
	sum := sha256.Sum256([]byte(token))
	c, err := s.q.GetEmailChangeByToken(ctx, sum[:])
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return zero, errConfirmBadToken
		}
		return zero, err
	}
	if err := emailChangeFault(c, time.Now()); err != nil {
		return zero, err
	}
	return c, nil
}

// emailChangeFault 说出一个已解析的链接为什么不能兑换，或者 nil。
//
// 和查询分开，并且把时钟当参数传进来，所以每一条拒绝都能不碰数据库地跑一遍，
// 也不用为了其中一条等三天。
func emailChangeFault(c store.GetEmailChangeByTokenRow, now time.Time) error {
	if c.UsedAt.Valid {
		return errConfirmUsed
	}
	if now.After(c.ExpiresAt.Time) {
		return errConfirmExpired
	}
	// 地址在这封信发出之后又动过了。
	//
	// 这是 invitationFault 里那条检查的镜像，方向相反：那边确认「链接发往的
	// 地址还是账号现在的地址」，这边确认「账号现在的地址还是当初要改的那个」。
	// 两个管理员先后发起两次变更时，第一封信必须失效——否则它会把账号带到
	// 一个早就被推翻的目的地，而做出第二次决定的人不会知道。
	if !strings.EqualFold(strings.TrimSpace(c.CurrentEmail), c.OldEmail) {
		return errConfirmMoved
	}
	// 已验证在这里是**必须成立**的，和 invitationFault 恰好相反。
	// 不成立说明这个账号在此期间被退回了未激活状态，那时该走的是邀请。
	if !c.EmailVerifiedAt.Valid {
		return errConfirmUnavailable
	}
	if c.TenantStatus != "ACTIVE" || c.EmployeeStatus != "ACTIVE" {
		// 公司被停用，或者人在信发出和点开之间离职了。两种情况一句话：
		// 对面那个人无论如何都做不了什么，而对一个没登录的调用者
		// 说清「你们公司被停用了」说得比需要的多。
		return errConfirmUnavailable
	}
	return nil
}
