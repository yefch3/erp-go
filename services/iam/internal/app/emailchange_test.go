package app

import (
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgconn"

	"github.com/sgao19/erp-go/services/iam/internal/store"
)

// goodEmailChange 是一条什么毛病都没有的变更，好让每个用例只弄坏一样东西，
// 失败时自己说出是哪一样。
func goodEmailChange() store.GetEmailChangeByTokenRow {
	return store.GetEmailChangeByTokenRow{
		ID: 9, TenantID: 1, EmployeeID: 42,
		OldEmail:     "alice@aaaindustryinc.com",
		NewEmail:     "alice.li@aaaindustryinc.com",
		CurrentEmail: "alice@aaaindustryinc.com",
		ExpiresAt:    ts(now.Add(24 * time.Hour)),
		// 改邮箱**只**发生在已经激活过的人身上——这正是和邀请相反的地方。
		EmailVerifiedAt: ts(now.Add(-30 * 24 * time.Hour)),
		EmployeeName:    "李爱丽", EmployeeStatus: "ACTIVE", TenantStatus: "ACTIVE",
	}
}

func TestAGoodEmailChangeLinkIsUsable(t *testing.T) {
	if err := emailChangeFault(goodEmailChange(), now); err != nil {
		t.Fatalf("一条没有任何问题的变更被拒了：%v", err)
	}
}

func TestEveryReasonAnEmailChangeLinkFailsIsToldApart(t *testing.T) {
	// 这些话本身就是产品。读到它的人没有别的渠道可问，而对着其中三种说
	// 「请重试」是废话。
	cases := []struct {
		name  string
		spoil func(*store.GetEmailChangeByTokenRow)
		want  error
	}{
		{"已经用过", func(c *store.GetEmailChangeByTokenRow) { c.UsedAt = ts(now.Add(-time.Hour)) }, errConfirmUsed},
		{"已过期", func(c *store.GetEmailChangeByTokenRow) { c.ExpiresAt = ts(now.Add(-time.Second)) }, errConfirmExpired},
		{"地址又被改过", func(c *store.GetEmailChangeByTokenRow) { c.CurrentEmail = "bob@aaaindustryinc.com" }, errConfirmMoved},
		{"账号退回未激活", func(c *store.GetEmailChangeByTokenRow) {
			c.EmailVerifiedAt = ts(time.Time{})
			c.EmailVerifiedAt.Valid = false
		}, errConfirmUnavailable},
		{"人已离职", func(c *store.GetEmailChangeByTokenRow) { c.EmployeeStatus = "INACTIVE" }, errConfirmUnavailable},
		{"公司被停用", func(c *store.GetEmailChangeByTokenRow) { c.TenantStatus = "SUSPENDED" }, errConfirmUnavailable},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			row := goodEmailChange()
			c.spoil(&row)
			if err := emailChangeFault(row, now); !errors.Is(err, c.want) {
				t.Fatalf("得到 %v，想要 %v", err, c.want)
			}
		})
	}
}

// 这条检查存在的全部理由。
//
// 两个管理员先后发起两次变更：先改到 B，再改到 C。到 C 的那次生效之后，
// 到 B 的那封信还躺在某个信箱里——如果它还能兑换，谁读到它谁就能把这个账号
// 拽回 B，而做出第二次决定的人完全不会知道。
func TestALinkDiesOnceTheAddressMovesAgain(t *testing.T) {
	row := goodEmailChange()
	row.CurrentEmail = "alice.wang@aaaindustryinc.com" // 后一次变更已经生效
	if err := emailChangeFault(row, now); !errors.Is(err, errConfirmMoved) {
		t.Fatalf("一条起点已经不存在的变更被接受了：%v", err)
	}
}

func TestTheOldAddressComparisonIgnoresCaseAndPadding(t *testing.T) {
	// employees.email 是手打进去的，" Alice@" 正是手会打出来的东西。
	// 为了一个空格拒掉一条好链接，和上面那种真被人动过手脚的情况
	// 在用户那边长得一模一样，而且常见得多。
	row := goodEmailChange()
	row.CurrentEmail = "  Alice@AAAindustryinc.com "
	if err := emailChangeFault(row, now); err != nil {
		t.Fatalf("一条链接因为大小写和空格被拒了：%v", err)
	}
}

func TestAnEmailChangeLinkExpiresExactlyAtItsDeadline(t *testing.T) {
	row := goodEmailChange()
	row.ExpiresAt = ts(now)
	if err := emailChangeFault(row, now); err != nil {
		t.Fatalf("截止那一刻本身被拒了：%v", err)
	}
	if err := emailChangeFault(row, now.Add(time.Nanosecond)); !errors.Is(err, errConfirmExpired) {
		t.Fatalf("过了截止时间的链接被接受了：%v", err)
	}
}

// 「已验证」这一条在两边**方向相反**，而这正是不复用邀请那张表的理由之一。
// 写成一个用例，是因为将来有人把两条路合并时，会先看到这里。
func TestVerifiedMeansOppositeThingsForInvitesAndEmailChanges(t *testing.T) {
	inv := goodInvitation()
	inv.EmailVerifiedAt = ts(now.Add(-time.Minute))
	if err := invitationFault(inv, now); err == nil {
		t.Fatal("邀请：已验证的账号还能被激活")
	}
	row := goodEmailChange()
	row.EmailVerifiedAt.Valid = false
	if err := emailChangeFault(row, now); err == nil {
		t.Fatal("改邮箱：没验证过的账号还能改邮箱")
	}
}

// 离职和公司停用故意答同一句话。调用者没有登录，告诉他是哪一种，
// 说得比他问的多。
func TestASuspendedCompanyAndADepartedEmployeeAnswerAlikeOnEmailChange(t *testing.T) {
	left, suspended := goodEmailChange(), goodEmailChange()
	left.EmployeeStatus = "INACTIVE"
	suspended.TenantStatus = "SUSPENDED"
	a, b := emailChangeFault(left, now), emailChangeFault(suspended, now)
	if a.Error() != b.Error() {
		t.Fatalf("两种情况答得不一样：%q vs %q", a, b)
	}
}

// 窗口是产品决定，不是算术的副产品：这封信发给一个**现在就能正常登录**的人，
// 链接死了他没有任何损失，所以比邀请的七天短。
func TestTheEmailChangeWindowIsThreeDays(t *testing.T) {
	if emailChangeTTL != 72*time.Hour {
		t.Fatalf("改邮箱的窗口是 %v", emailChangeTTL)
	}
	if emailChangeTTL >= invitationTTL {
		t.Fatalf("改邮箱的窗口（%v）不该比邀请的（%v）长", emailChangeTTL, invitationTTL)
	}
}

func TestTheEmailChangeTokenIsBigEnoughToBeUnguessable(t *testing.T) {
	if emailChangeTokenBytes < 32 {
		t.Fatalf("token 只有 %d 字节，至少要 32", emailChangeTokenBytes)
	}
}

// 存储里只有 sha256(token)，从来没有 token 本身。断言在源码上，
// 因为要证明的是**某样东西不存在**——没有一条把明文 token 写进列里的路——
// 而运行时的调用没法展示一个不存在。
func TestTheRawEmailChangeTokenIsNeverStored(t *testing.T) {
	src := readSource(t, "emailchange.go")
	if !strings.Contains(src, "sha256.Sum256([]byte(token))") {
		t.Fatal("token 在查库之前不再被哈希了")
	}
	if strings.Contains(src, "TokenHash: []byte(token)") {
		t.Fatal("明文 token 被写进了存储")
	}
}

// 整件事的核心：**在有人点开链接之前，employees.email 一个字都不能动。**
//
// 断言在源码上，理由同上——这条性质说的是「RequestEmailChange 里没有任何一处
// 写员工的邮箱」。它一旦被破坏，表现是「保存完就生效了」，看起来完全正常，
// 而坏掉的东西要到某人打错一个字母、然后再也登录不上的那天才被发现。
func TestRequestingAChangeDoesNotTouchTheEmployeeAddress(t *testing.T) {
	src := readSource(t, "emailchange.go")
	body := between(t, src, "func (s *Service) RequestEmailChange", "\n}\n")
	for _, writer := range []string{
		"UpdateEmployeeDetails",
		"SetEmployeeEmailVerified",
		"UpdateOwnProfile",
	} {
		if strings.Contains(body, writer) {
			t.Fatalf("RequestEmailChange 调了 %s——发起变更时不该改动员工那一行", writer)
		}
	}
	// 反过来也钉住：真正落库的地方必须是 Confirm，而且必须走那个
	// 「地址和验证时间一起写」的语句。
	confirm := between(t, src, "func (s *Service) ConfirmEmailChange", "\n}\n")
	if !strings.Contains(confirm, "SetEmployeeEmailVerified") {
		t.Fatal("ConfirmEmailChange 不再用 SetEmployeeEmailVerified 落库——" +
			"地址和验证时间必须一起写，这是整件事的全部意义")
	}
}

// 直接改邮箱这条路，对已激活的人必须是关着的。
//
// 这是**现存问题**本身：UpdateEmployeeDetails 只写 email 不写 email_verified_at，
// 新地址凭空继承了旧地址挣来的「已验证」。UpdateEmployee 里那道门是唯一的拦截点。
func TestUpdateEmployeeRefusesToMoveAVerifiedAddress(t *testing.T) {
	src := readSource(t, "service.go")
	body := between(t, src, "func (s *Service) UpdateEmployee(", "\n}\n")
	if !strings.Contains(body, "IAM_EMP_EMAIL_LOCKED") {
		t.Fatal("UpdateEmployee 不再拦「直接改已验证过的邮箱」——" +
			"这条路一开，新地址就会凭空继承旧地址的已验证状态")
	}
	if !strings.Contains(body, "current.EmailVerifiedAt.Valid") {
		t.Fatal("那道门不再看 email_verified_at——没激活过的人应该还能直接改")
	}
}

// 邮箱撞车不能报成「工号已存在」。管理员会盯着一个毫无问题的工号找半天，
// 而他改的是邮箱。
func TestAnEmailClashIsNotReportedAsACodeClash(t *testing.T) {
	cases := []struct {
		constraint string
		wantCode   string
	}{
		{"employees_email_key", "IAM_EMP_EMAIL_TAKEN"},
		{"employees_tenant_id_code_key", "IAM_EMP_CODE_TAKEN"},
	}
	for _, c := range cases {
		t.Run(c.constraint, func(t *testing.T) {
			err := translateEmployeeUnique(fakeUniqueViolation(c.constraint))
			if !strings.Contains(err.Error(), c.wantCode) {
				t.Fatalf("撞 %s 报的是 %v，想要 %s", c.constraint, err, c.wantCode)
			}
		})
	}
}

// fakeUniqueViolation 造一个 Postgres 唯一约束冲突，好在不碰数据库的情况下
// 把「撞了哪个索引」这件事验一遍。
func fakeUniqueViolation(constraint string) error {
	return &pgconn.PgError{Code: "23505", ConstraintName: constraint}
}
