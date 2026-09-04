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
			t.Logf("登录前声明：%v", names)
			t.Logf("  支持 %s：%v  ← announceID 靠它决定发不发", idCapability, supported)
		})
	}
}
