package mailfetch

import (
	"bufio"
	"context"
	"fmt"
	"net"
	"strings"
	"testing"
	"time"

	"github.com/sgao19/erp-go/services/mail/internal/app"
)

// 一台假 IMAP：LOGIN 按 loginReply 答，SELECT 按 selectReply 答。
func startLoginFake(t *testing.T, loginReply, selectReply string) string {
	t.Helper()
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = ln.Close() })
	go func() {
		conn, err := ln.Accept()
		if err != nil {
			return
		}
		defer conn.Close()
		w := bufio.NewWriter(conn)
		r := bufio.NewReader(conn)
		say := func(s string) { _, _ = w.WriteString(s + "\r\n"); _ = w.Flush() }
		say("* OK [CAPABILITY IMAP4rev1] fake")
		for {
			line, err := r.ReadString('\n')
			if err != nil {
				return
			}
			tag, rest, _ := strings.Cut(strings.TrimRight(line, "\r\n"), " ")
			verb, _, _ := strings.Cut(rest, " ")
			switch strings.ToUpper(verb) {
			case "LOGIN":
				if loginReply == "hangup" {
					// 263 在握手中途掐线就是这样：收到 LOGIN，一声不吭把连接关了。
					return
				}
				say(tag + " " + loginReply)
			case "CAPABILITY":
				say("* CAPABILITY IMAP4rev1")
				say(tag + " OK")
			case "SELECT", "EXAMINE":
				say(tag + " " + selectReply)
			case "LOGOUT":
				say("* BYE")
				say(tag + " OK")
				return
			default:
				say(tag + " OK")
			}
		}
	}()
	return ln.Addr().String()
}

func acctFor(addr string) app.MailAccount {
	host, port, _ := net.SplitHostPort(addr)
	var p int
	_, _ = fmt.Sscanf(port, "%d", &p)
	return app.MailAccount{Email: "u@x", Secret: "pw", IMAPHost: host, IMAPPort: p, IMAPSecurity: "NONE"}
}

// 163 拒绝授权码：LOGIN NO。这是唯一一种「重新登录能修好」的失败，必须带类型。
func TestARefusedLoginIsTypedAsCredentialRejected(t *testing.T) {
	addr := startLoginFake(t, "NO Login error or password error", "OK")
	f := NewIMAP(5*time.Second, 2*time.Second, nil)
	err := f.VerifyLogin(context.Background(), acctFor(addr))
	if err == nil {
		t.Fatal("应该失败")
	}
	if !app.IsCredentialRejected(err) {
		t.Fatalf("LOGIN 被拒应该是凭据错误，实际类型丢了：%v", err)
	}
	if !strings.Contains(err.Error(), "授权码") {
		t.Errorf("给人看的文字要说清是授权码的事：%v", err)
	}
}

// 登录成功、打开信箱被拒（163 的 Unsafe Login 就是这样）：**不是**凭据问题，
// 重登修不好，横幅不该劝人重登。
func TestAFailedSelectAfterAGoodLoginIsNotACredentialProblem(t *testing.T) {
	addr := startLoginFake(t, "OK logged in", "NO Unsafe Login. Please contact kefu@188.com")
	f := NewIMAP(5*time.Second, 2*time.Second, nil)
	_, err := f.Fetch(context.Background(), acctFor(addr), "INBOX", 0, 10)
	if err == nil {
		t.Fatal("应该失败")
	}
	if app.IsCredentialRejected(err) {
		t.Fatalf("打开信箱失败不是凭据问题，却被标成了：%v", err)
	}
}

// 包了一层再包一层，类型也得还认得出来——同步链上每一层都在 %w。
func TestCredentialTypeSurvivesWrapping(t *testing.T) {
	inner := app.NewCredentialRejected(fmt.Errorf("邮箱拒绝了这个授权码：NO"))
	wrapped := fmt.Errorf("同步 INBOX 失败：%w", fmt.Errorf("第二层：%w", inner))
	if !app.IsCredentialRejected(wrapped) {
		t.Fatal("包了两层就认不出来了")
	}
	if app.IsCredentialRejected(fmt.Errorf("收取邮件失败：imap: connection closed")) {
		t.Fatal("普通错误不该被当成凭据错误")
	}
}

// 263 在 LOGIN 中途掐线：go-imap 返回的也是一个普通错误，而这**不是**授权码
// 错。误判的后果就是这个 PR 要消灭的假横幅在另一个入口重新出现。
func TestAConnectionDroppedDuringLoginIsNotACredentialProblem(t *testing.T) {
	addr := startLoginFake(t, "hangup", "OK")
	f := NewIMAP(5*time.Second, 2*time.Second, nil)
	err := f.VerifyLogin(context.Background(), acctFor(addr))
	if err == nil {
		t.Fatal("应该失败")
	}
	if app.IsCredentialRejected(err) {
		t.Fatalf("登录中途断线被当成了授权码错：%v", err)
	}
}
