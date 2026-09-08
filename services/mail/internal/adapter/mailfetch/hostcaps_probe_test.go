package mailfetch

import (
	"crypto/tls"
	"net"
	"os"
	"sort"
	"strings"
	"testing"
	"time"

	"github.com/emersion/go-imap/client"
)

// 问一台真实的 IMAP 服务器「你支持什么」。
//
// 默认跳过：它要连公网，不该在 CI 里跑。接新服务商、或者某家又开始拒绝我们
// 的时候，这是最快的第一手资料——比翻它家的帮助文档快，也比猜准。
//
//	MAIL_HOST_PROBE=imap.163.com,imap.126.com go test ./services/mail/internal/adapter/mailfetch/ -run TestHostCapabilities -v
//
// **这里问到的是登录前的清单，不是全部。** 很多服务器认证之后才把完整能力
// 报出来，据此下「某家不支持某功能」的结论会错。铁证是 Gmail：它登录前既不
// 声明 UIDPLUS 也不声明 MOVE，而生产库里有 37 封信是靠 Gmail 的 MOVE 挪走、
// 从 COPYUID 拿到新编号记下来的——两样它都有，只是登录后才说。
//
// 服务里的判断走的是登录后的清单（WaitForNews 先登录再让 go-imap 问 IDLE），
// 所以线上行为不受这个局限影响；这个探测只是一手参考。
//
// 2026-09-07 实测（登录前）：
//
//	imap.163.com        ID UIDPLUS SPECIAL-USE STARTTLS APPENDLIMIT=71680000 AUTH=XOAUTH2
//	imap.126.com        同上
//	imap.qq.com         ID IDLE MOVE UIDPLUS CHILDREN NAMESPACE AUTH=LOGIN/PLAIN/XOAUTH2
//	imap.exmail.qq.com  ID IDLE MOVE UIDPLUS CHILDREN NAMESPACE AUTH=LOGIN/PLAIN
//	imap.aliyun.com     ID IDLE UIDPLUS AUTH=EXTERNAL/XOAUTH/XOAUTH2
//	imap.gmail.com      ID IDLE CHILDREN NAMESPACE QUOTA UNSELECT X-GM-EXT-1
//	imap.263.net        ID MOVE UIDPLUS AUTH=PLAIN
//
// 从这批数据能直接用的三条：
//
//   - **263 和网易两家（163/126）不声明 IDLE**，线上也确认了：这几个箱走的是
//     两分钟一轮的轮询，不是推送。见 app.ErrPushUnsupported。
//   - 163/126 声明 SPECIAL-USE，会在 LIST 里直接标出草稿箱/已发送这些，不用
//     按名字猜（roleOf 本来就先看属性）。
//   - 163/126 的 APPENDLIMIT 约 68 MB：往「已发送」存副本的单封上限。
//
// 还有一条容易看反：163/126 只声明 AUTH=XOAUTH2，看着像不让用密码——但它们
// **没有声明 LOGINDISABLED**，所以授权码登录是允许的。真正关掉密码登录的是
// Outlook（它会声明 LOGINDISABLED），两者不能混。
//
// 2026-09-04 实测：163 / 126 / QQ / 263 四家都声明 ID，这正是 announceID 里
// 那句 Support("ID") 会成立的依据。
func TestHostCapabilities(t *testing.T) {
	hosts := os.Getenv("MAIL_HOST_PROBE")
	if hosts == "" {
		t.Skip("MAIL_HOST_PROBE not set (e.g. imap.163.com)")
	}
	for _, host := range strings.Split(hosts, ",") {
		host = strings.TrimSpace(host)
		if host == "" {
			continue
		}
		t.Run(host, func(t *testing.T) {
			d := &net.Dialer{Timeout: 10 * time.Second}
			c, err := client.DialWithDialerTLS(d, net.JoinHostPort(host, "993"),
				&tls.Config{ServerName: host})
			if err != nil {
				t.Fatalf("连不上：%v", err)
			}
			defer func() { _ = c.Logout() }()
			c.Timeout = 15 * time.Second

			caps, err := c.Capability()
			if err != nil {
				t.Fatalf("问不到能力：%v", err)
			}
			names := make([]string, 0, len(caps))
			for k := range caps {
				names = append(names, k)
			}
			sort.Strings(names)
			supported, _ := c.Support(idCapability)
			// 说清楚这是哪一刻的清单：不写的话，下一个人会拿它下
			// 「某家不支持 MOVE」这种结论，而 Gmail 正是这样被冤枉的。
			t.Logf("登录前声明：%v", names)
			t.Logf("  ↑ **登录前**的清单。多数服务器认证后还会多报几项——" +
				"Gmail 登录前不声明 UIDPLUS/MOVE，实际两样都有。")
			t.Logf("  支持 %s：%v  ← announceID 靠它决定发不发", idCapability, supported)
		})
	}
}
