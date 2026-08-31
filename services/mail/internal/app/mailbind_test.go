package app

import (
	"context"
	"encoding/base64"
	"log/slog"
	"os"
	"strings"
	"testing"
	"time"
	"unicode/utf8"

	"github.com/sgao19/erp-go/pkg/pgdb"
)

// 「其他」那一档的主机由员工自己填，所以它是这一批改动里唯一一个把主机名
// 交回给调用方的地方——必须挡住内网。
//
// 不挡的话，「绑个邮箱」就成了一个按钮：让邮件服务带着凭据去敲任意
// host:port。云元数据地址（169.254.169.254）、内网段、非邮件端口全从这条路
// 进来，而失败长得和「授权码错了」一模一样，探测者看得见响应时间的差别。
func TestCustomHostRefusesAnythingThatIsNotAPublicMailServer(t *testing.T) {
	for _, c := range []struct {
		name string
		host string
		port int32
		want string
	}{
		{"云元数据地址", "169.254.169.254", 993, "域名"},
		{"本机", "127.0.0.1", 993, "域名"},
		{"内网段", "10.0.0.5", 993, "域名"},
		{"IPv6 本机", "::1", 993, "域名"},
		{"localhost", "localhost", 993, "内网"},
		{"内网后缀", "mail.internal", 993, "内网"},
		{"单标签主机名", "mail", 993, "完整域名"},
		{"空", "", 993, "填写"},
		{"非邮件端口", "imap.example.com", 8080, "端口"},
		{"HTTP 端口", "imap.example.com", 80, "端口"},
		{"注入字符", "imap.example.com/../x", 993, "非法字符"},
	} {
		t.Run(c.name, func(t *testing.T) {
			err := validateCustomHost(c.host, c.port)
			if err == nil {
				t.Fatalf("%q:%d 竟然放行了——这条路会让邮件服务带着凭据去连它",
					c.host, c.port)
			}
			if !strings.Contains(err.Error(), c.want) {
				t.Errorf("要说清楚拒的是什么，拿到：%v", err)
			}
		})
	}

	// 正常的邮件服务器要放行，否则「其他」那一档等于不存在。
	for _, port := range []int32{143, 465, 587, 993, 994} {
		if err := validateCustomHost("imap.example.com", port); err != nil {
			t.Errorf("端口 %d 是标准邮件端口，不该拒：%v", port, err)
		}
	}
}

// 「其他」那一档还要求主机属于地址那个域，挡的是冒名。
//
// 不挡的话，绑定的活体证明就塌了一半：一个人可以填 ceo@bigcorp.com 配上
// 自己控制的 imap.evil.example，那台服务器对任何密码都说 yes，于是 ERP 里
// 出现一个"已验证"的 ceo@bigcorp.com，同事看到的是这个人拥有它。
//
// 挑预设服务商时这条证明是硬的——服务器是我们查表定的，所以那一档不受
// 这个限制（me@gmail.com 走 imap.gmail.com 天经地义）。
func TestCustomHostMustBelongToTheAddressDomain(t *testing.T) {
	for _, c := range []struct {
		name       string
		host, mail string
		ok         bool
	}{
		{"自建同域", "imap.newco.com", "me@newco.com", true},
		{"同域另一个前缀", "mail.newco.com", "me@newco.com", true},
		{"域名本身", "newco.com", "me@newco.com", true},
		{"多级子域", "imap.mx.newco.com", "me@newco.com", true},
		{"冒名：别人的地址配自己的服务器", "imap.evil.example", "ceo@bigcorp.com", false},
		{"看着像但不是", "imap.newco.com.evil.example", "me@newco.com", false},
		{"大小写不影响", "IMAP.NewCo.com", "Me@NEWCO.com", true},
	} {
		t.Run(c.name, func(t *testing.T) {
			err := hostServesDomain(c.host, c.mail)
			if c.ok && err != nil {
				t.Errorf("%s 服务 %s 是正常形态，不该拒：%v", c.host, c.mail, err)
			}
			if !c.ok && err == nil {
				t.Errorf("%s 配 %s 放行了——这等于"+
					"「填一个别人的地址 + 一台自己控制的服务器」就能冒名", c.host, c.mail)
			}
		})
	}
}

// 服务商表和前端的清单用同一串代号。这条测试钉的是「服务端认得的代号」，
// 前端漏加一家时对不上，服务端会当场报错而不是猜一个主机连上去。
func TestProviderLookup(t *testing.T) {
	// 个人邮箱按地址后缀认得出来；企业邮认不出来（域名是各家公司自己的），
	// 必须由员工在表单里挑——猜错的代价是拿着凭据去登了另一家的服务器。
	for _, c := range []struct {
		email string
		want  string
	}{
		{"me@gmail.com", "gmail"},
		{"me@163.com", "netease163"},
		{"me@126.com", "netease126"},
		{"me@qq.com", "qq"},
		{"me@FOXMAIL.COM", "qq"},
		{"me@outlook.com", "outlook"},
	} {
		p, ok := MailProviderForAddress(strings.ToLower(c.email))
		if !ok || p.Code != c.want {
			t.Errorf("%s 该认出 %s，拿到 %q/%v", c.email, c.want, p.Code, ok)
		}
	}
	// 企业邮的域名认不出来，这是对的：sunrise.com 可能托管在任何一家。
	if p, ok := MailProviderForAddress("me@sunrise.com"); ok {
		t.Errorf("公司自己的域名不该被猜成 %s——它可能托管在任何一家", p.Code)
	}
	if _, ok := MailProviderByCode("does-not-exist"); ok {
		t.Error("认不得的代号必须答 false，不能猜一个")
	}
	// 每一家的主机和端口都得填全，缺一个就是绑完不能收发。
	for _, code := range MailProviderCodes() {
		p, _ := MailProviderByCode(code)
		if p.SMTPHost == "" || p.IMAPHost == "" || p.SMTPPort == 0 || p.IMAPPort == 0 {
			t.Errorf("%s 的收发服务器没填全：%+v", code, p)
		}
		if !mailPorts[p.SMTPPort] || !mailPorts[p.IMAPPort] {
			t.Errorf("%s 用了一个不在允许清单里的端口：%d/%d",
				code, p.SMTPPort, p.IMAPPort)
		}
	}
}

// 已经绑了信箱的人，不能靠「报一个别人的地址、不输授权码」白拿一把解锁令牌。
//
// 这是地址交还给调用方之后新开的一个口子，而且是**把邮箱门整个拆掉**那种：
// 解锁令牌的键是 t{租户}.e{员工}，和地址无关，所以骗到的那把令牌解的是他
// **自己的**信箱。从前不可能——地址恒等于登录地址，永远找得到他自己那一行，
// 于是永远走到"要么输码、要么验 OAuth"。
//
// 「没什么可验的」这句话只有一个合法依据：这个人名下一个信箱都没有。
func TestSomebodyWithAMailboxCannotSkipTheGateWithAStrangersAddress(t *testing.T) {
	dsn := os.Getenv("MAIL_TEST_DSN")
	if dsn == "" {
		t.Skip("MAIL_TEST_DSN not set")
	}
	ctx := context.Background()
	pool, err := pgdb.New(ctx, dsn)
	if err != nil {
		t.Fatal(err)
	}
	defer pool.Close()
	box, err := NewSecretBox(base64.StdEncoding.EncodeToString(make([]byte, 32)), 1)
	if err != nil {
		t.Fatal(err)
	}
	tenantID := time.Now().UnixNano()
	mine, theirs := tenantID%100000+850001, tenantID%100000+850002
	defer func() {
		_, _ = pool.Exec(ctx, "DELETE FROM mail_binding_log WHERE tenant_id=$1", tenantID)
		_, _ = pool.Exec(ctx, "DELETE FROM mail_accounts WHERE tenant_id=$1", tenantID)
	}()
	svc := New(pool, Deps{Secrets: box}, slog.New(slog.NewTextHandler(os.Stderr, nil)))

	if _, err := svc.VerifyMailSecret(ctx, tenantID, mine, BindRequest{
		Email: "me@qq.com", Provider: "qq", Secret: "code",
	}); err != nil {
		t.Fatal(err)
	}
	if _, err := svc.VerifyMailSecret(ctx, tenantID, theirs, BindRequest{
		Email: "colleague@qq.com", Provider: "qq", Secret: "code",
	}); err != nil {
		t.Fatal(err)
	}

	// 报同事的地址、不输授权码。必须报错，不能答「无需验证」。
	res, err := svc.VerifyMailSecret(ctx, tenantID, mine, BindRequest{
		Email: "colleague@qq.com",
	})
	if err == nil {
		t.Fatalf("拿别人的地址、空授权码，竟然通过了（%q）——网关会照这个"+
			"发一把解锁令牌，而那把令牌解的是他自己的信箱，密码门形同虚设",
			res.Detail)
	}
	if !strings.Contains(err.Error(), "不在你名下") {
		t.Errorf("要说「不是你的信箱」，而且不能说出它属于谁——说了这里就成了"+
			"探测口。拿到：%v", err)
	}

	// 反面：真的一个信箱都没有的人，仍然拿得到一把令牌——活动、草稿那几个
	// 不碰邮件内容的页面要进得去。
	nobody := tenantID%100000 + 850003
	pass, err := svc.VerifyMailSecret(ctx, tenantID, nobody, BindRequest{})
	if err != nil {
		t.Fatalf("一个信箱都没绑的人应该直接放行：%v", err)
	}
	if pass.Detail == "" {
		t.Error("放行时要说一句为什么")
	}

	// 自己那个地址认得出来，答的是他自己那一行。
	//
	// 这条测试跑在没有真邮件通道的环境里（Deps 里没有 mailbox），所以到这
	// 一步就返回「邮件通道未启用」了——「密码绑的信箱不输码要被要求输码」
	// 那一条在这里验不到，它在 AuthKind 那个分支上，需要一个真的 IMAP 端。
	// 这里能验的是**它没有把自己的地址也当成别人的**。
	own, err := svc.VerifyMailSecret(ctx, tenantID, mine, BindRequest{Email: "me@qq.com"})
	if err != nil {
		t.Fatalf("自己的地址不该被当成别人的：%v", err)
	}
	if own.Email != "me@qq.com" {
		t.Errorf("答的应该是他自己那一行，拿到 %q", own.Email)
	}
}

// 截断按字符走，不按字节。
//
// 直接切字节会把一个中文切成两半，留下无效的 UTF-8 序列，而 Postgres 的
// text 列拒收（22021）——于是一句超长的中文错误信息不但没被记下，还让整条
// 留痕 INSERT 失败。**最该留痕的那种失败，恰好是最记不下的那种**，而
// recordBinding 是尽力而为的，抛出来只会被一句 Warn 吞掉。
func TestTruncationDoesNotCutAChineseCharacterInHalf(t *testing.T) {
	long := strings.Repeat("授权码不对", 200) // 每个字三字节
	for _, max := range []int{500, 320, 32, 1, 0} {
		got := truncateUTF8(long, max)
		if len(got) > max {
			t.Errorf("max=%d 时长度是 %d，超了", max, len(got))
		}
		if !utf8.ValidString(got) {
			t.Errorf("max=%d 时截出了无效的 UTF-8——Postgres 会拒收整条 INSERT", max)
		}
	}
	// 没超长的原样返回，别把 ASCII 也削掉。
	if got := truncateUTF8("abc", 500); got != "abc" {
		t.Errorf("没超长的不该动，拿到 %q", got)
	}
}
