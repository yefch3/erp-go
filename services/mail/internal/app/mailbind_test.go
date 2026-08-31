package app

import (
	"strings"
	"testing"
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
