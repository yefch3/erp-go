package mailfetch

import (
	"bufio"
	"context"
	"errors"
	"net"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/sgao19/erp-go/services/mail/internal/app"
)

// 一台只管登录、选中文件夹和搜索的假服务器。search 决定它怎么答 UID SEARCH：
//
//	"refuse"     照 263 的原话回 NO
//	"hangup"     一个字不回，直接断开——连接坏了，不是拒绝
//	"found"      * SEARCH 9 12
//	"badcharset" CHARSET UTF-8 回 BADCHARSET，US-ASCII 那次回 * SEARCH 5
type searchFake struct {
	addr   string
	mu     sync.Mutex
	logins int
	lines  []string // 每一条 UID SEARCH 的原文
}

func startSearchFake(t *testing.T, search string) *searchFake {
	t.Helper()
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = ln.Close() })
	f := &searchFake{addr: ln.Addr().String()}
	go func() {
		for {
			conn, err := ln.Accept()
			if err != nil {
				return
			}
			go f.serve(conn, search)
		}
	}()
	return f
}

func (f *searchFake) serve(conn net.Conn, search string) {
	defer conn.Close()
	w := bufio.NewWriter(conn)
	r := bufio.NewReader(conn)
	say := func(s string) { _, _ = w.WriteString(s + "\r\n"); _ = w.Flush() }
	say("* OK [CAPABILITY IMAP4rev1 UIDPLUS] fake")
	for {
		line, err := r.ReadString('\n')
		if err != nil {
			return
		}
		line = strings.TrimRight(line, "\r\n")
		tag, rest, _ := strings.Cut(line, " ")
		upper := strings.ToUpper(rest)
		switch {
		case strings.HasPrefix(upper, "LOGIN"):
			f.mu.Lock()
			f.logins++
			f.mu.Unlock()
			say(tag + " OK done")
		case strings.HasPrefix(upper, "CAPABILITY"):
			say("* CAPABILITY IMAP4rev1 UIDPLUS")
			say(tag + " OK done")
		case strings.HasPrefix(upper, "SELECT"), strings.HasPrefix(upper, "EXAMINE"):
			say("* 3 EXISTS")
			say("* OK [UIDVALIDITY 7] ok")
			say(tag + " OK [READ-ONLY] done")
		case strings.HasPrefix(upper, "UID SEARCH"):
			f.mu.Lock()
			f.lines = append(f.lines, rest)
			f.mu.Unlock()
			switch search {
			case "refuse":
				say(tag + " NO UID SEARCH search error: can't search that criteria")
			case "hangup":
				return
			case "found":
				say("* SEARCH 9 12")
				say(tag + " OK done")
			case "badcharset":
				if strings.Contains(upper, "CHARSET UTF-8") {
					say(tag + " NO [BADCHARSET (US-ASCII)] charset not supported")
				} else {
					say("* SEARCH 5")
					say(tag + " OK done")
				}
			}
		case strings.HasPrefix(upper, "LOGOUT"):
			say("* BYE")
			say(tag + " OK done")
			return
		default: // NOOP and anything else
			say(tag + " OK done")
		}
	}
}

func (f *searchFake) counts() (logins, searches int) {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.logins, len(f.lines)
}

func (f *searchFake) searchLines() []string {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]string(nil), f.lines...)
}

func searchAcct(addr string) app.MailAccount {
	a := acctFor(addr)
	a.AccountID = 7
	return a
}

// 263 那种服务器：第一次问被拒，之后在有效期内不再问——不管是单封还是一批。
// 2026-09-23 生产上这一句一天被问了 54,081 遍。
func TestARefusedMessageIDSearchIsNotAskedAgain(t *testing.T) {
	srv := startSearchFake(t, "refuse")
	f := NewIMAP(5*time.Second, 2*time.Second, nil)
	acct := searchAcct(srv.addr)
	ctx := context.Background()

	_, _, err := f.FindUIDByMessageID(ctx, acct, "已删除", "a@x")
	if !errors.Is(err, app.ErrMessageIDSearchRefused) {
		t.Fatalf("第一次应当报「服务器拒绝」，实际 %v", err)
	}
	_, _, err = f.FindUIDByMessageID(ctx, acct, "已删除", "b@x")
	if !errors.Is(err, app.ErrMessageIDSearchRefused) {
		t.Fatalf("第二次也应当报「服务器拒绝」，实际 %v", err)
	}
	if _, err := f.FindUIDsByMessageIDs(ctx, acct, "已删除", []string{"c@x", "d@x"}); !errors.Is(err, app.ErrMessageIDSearchRefused) {
		t.Fatalf("批量版也应当认得这条记录，实际 %v", err)
	}
	if _, searches := srv.counts(); searches != 1 {
		t.Fatalf("被拒之后不该再问服务器：一共问了 %d 次", searches)
	}
}

// 被拒绝时连接是好的，不能扔：下一条命令接着用它，而不是重新握手登录。
// 从前每次被拒都扔，一次失败的搜索就是一次多余的登录。
func TestARefusalKeepsTheConnection(t *testing.T) {
	srv := startSearchFake(t, "refuse")
	f := NewIMAP(5*time.Second, 2*time.Second, nil)
	acct := searchAcct(srv.addr)

	_, _, _ = f.FindUIDByMessageID(context.Background(), acct, "已删除", "a@x")
	if _, err := f.RecentMessageIDs(context.Background(), acct, "INBOX", 10); err != nil {
		t.Fatalf("下一条命令应当照常：%v", err)
	}
	if logins, _ := srv.counts(); logins != 1 {
		t.Fatalf("被拒绝后连接应当留着复用，实际登录了 %d 次", logins)
	}
}

// 连接断了不是拒绝：不记下来，下次照问（很可能就好了）。
func TestADroppedConnectionIsNotMistakenForARefusal(t *testing.T) {
	srv := startSearchFake(t, "hangup")
	f := NewIMAP(5*time.Second, 2*time.Second, nil)
	acct := searchAcct(srv.addr)
	ctx := context.Background()

	_, _, err := f.FindUIDByMessageID(ctx, acct, "已删除", "a@x")
	if err == nil || errors.Is(err, app.ErrMessageIDSearchRefused) {
		t.Fatalf("断线应当是普通错误，不是「服务器拒绝」：%v", err)
	}
	_, _, _ = f.FindUIDByMessageID(ctx, acct, "已删除", "b@x")
	logins, searches := srv.counts()
	if searches != 2 {
		t.Fatalf("断线之后应当再问一次，实际问了 %d 次", searches)
	}
	if logins != 2 {
		t.Fatalf("断掉的连接应当扔掉重连，实际登录了 %d 次", logins)
	}
}

// 找到了：取最新的那个 UID（重试过的挪动可能在同一文件夹里留下两份）。
func TestAFoundMessageIDReturnsTheNewestUID(t *testing.T) {
	srv := startSearchFake(t, "found")
	f := NewIMAP(5*time.Second, 2*time.Second, nil)
	uid, ok, err := f.FindUIDByMessageID(context.Background(), searchAcct(srv.addr), "已删除", "a@x")
	if err != nil || !ok || uid != 12 {
		t.Fatalf("应当找到 12，实际 uid=%d ok=%v err=%v", uid, ok, err)
	}
	if lines := srv.searchLines(); !strings.Contains(lines[0], "<a@x>") {
		t.Errorf("Message-ID 要带上尖括号去搜：%q", lines[0])
	}
}

// 发出去的命令没变：先 CHARSET UTF-8，服务器说不认这个字符集再用 US-ASCII。
// 这是 go-imap 的 UidSearch 原来的做法，换成 Execute 之后要照样保留。
func TestABadCharsetStillFallsBackToASCII(t *testing.T) {
	srv := startSearchFake(t, "badcharset")
	f := NewIMAP(5*time.Second, 2*time.Second, nil)
	uid, ok, err := f.FindUIDByMessageID(context.Background(), searchAcct(srv.addr), "INBOX", "a@x")
	if err != nil || !ok || uid != 5 {
		t.Fatalf("US-ASCII 那次应当找到 5，实际 uid=%d ok=%v err=%v", uid, ok, err)
	}
	if lines := srv.searchLines(); len(lines) != 2 || !strings.Contains(strings.ToUpper(lines[1]), "CHARSET US-ASCII") {
		t.Fatalf("应当先 UTF-8 再 US-ASCII：%q", lines)
	}
}

// 记录会过期：拒绝也可能是一时的，六小时后再问一次。
func TestARefusalIsForgottenAfterTheRetryWindow(t *testing.T) {
	r := newSearchRefusals()
	t0 := time.Date(2026, 9, 23, 0, 0, 0, 0, time.UTC)
	if !r.remember(7, t0) {
		t.Fatal("第一次被拒应当算新消息（要记一行日志）")
	}
	if r.remember(7, t0.Add(time.Hour)) {
		t.Fatal("有效期内再被拒不算新消息")
	}
	if !r.recent(7, t0.Add(time.Hour+messageIDSearchRetryAfter-time.Second)) {
		t.Fatal("有效期内应当还记着")
	}
	if r.recent(7, t0.Add(time.Hour+messageIDSearchRetryAfter)) {
		t.Fatal("过了有效期应当忘掉，好再问一次")
	}
	if r.recent(8, t0) {
		t.Fatal("别的信箱不受影响")
	}
}
