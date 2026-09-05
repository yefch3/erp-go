package mailfetch

import (
	"bufio"
	"context"
	"net"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/emersion/go-imap/utf7"
)

// 一台会答 COPYUID 的假服务器。hasMove 决定它声明不声明 MOVE；
// copyuid 是 MOVE/COPY 应答里带的码（空 = 不带）。
type moveFake struct {
	addr string
	mu   sync.Mutex
	cmds []string
}

func startMoveFake(t *testing.T, hasMove bool, copyuid string) *moveFake {
	t.Helper()
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = ln.Close() })
	f := &moveFake{addr: ln.Addr().String()}
	caps := "IMAP4rev1 UIDPLUS"
	if hasMove {
		caps += " MOVE"
	}
	go func() {
		conn, err := ln.Accept()
		if err != nil {
			return
		}
		defer conn.Close()
		w := bufio.NewWriter(conn)
		r := bufio.NewReader(conn)
		say := func(s string) { _, _ = w.WriteString(s + "\r\n"); _ = w.Flush() }
		say("* OK [CAPABILITY " + caps + "] fake")
		for {
			line, err := r.ReadString('\n')
			if err != nil {
				return
			}
			tag, rest, _ := strings.Cut(strings.TrimRight(line, "\r\n"), " ")
			verb, rest2, _ := strings.Cut(rest, " ")
			if strings.EqualFold(verb, "UID") {
				verb, _, _ = strings.Cut(rest2, " ")
				verb = "UID " + strings.ToUpper(verb)
			} else {
				verb = strings.ToUpper(verb)
			}
			f.mu.Lock()
			f.cmds = append(f.cmds, verb)
			f.mu.Unlock()
			switch verb {
			case "LOGIN":
				say(tag + " OK done")
			case "CAPABILITY":
				say("* CAPABILITY " + caps)
				say(tag + " OK done")
			case "SELECT", "EXAMINE":
				say("* 3 EXISTS")
				say("* OK [UIDVALIDITY 7] ok")
				say(tag + " OK [READ-WRITE] done")
			case "UID MOVE", "UID COPY":
				if copyuid != "" {
					say(tag + " OK [COPYUID " + copyuid + "] done")
				} else {
					say(tag + " OK done")
				}
			case "CREATE", "RENAME", "DELETE":
				say(tag + " OK done")
			case "LIST":
				say(`* LIST (\HasNoChildren) "/" "INBOX"`)
				enc, _ := utf7.Encoding.NewEncoder().String("客户")
				say(`* LIST (\\HasNoChildren) "/" "` + enc + `"`)
				say(tag + " OK done")
			case "UID STORE":
				say("* 1 FETCH (FLAGS (\\Deleted))")
				say(tag + " OK done")
			case "EXPUNGE":
				say("* 1 EXPUNGE")
				say(tag + " OK done")
			case "LOGOUT":
				say("* BYE")
				say(tag + " OK done")
				return
			default:
				say(tag + " OK done")
			}
		}
	}()
	return f
}

func (f *moveFake) saw(verb string) bool {
	f.mu.Lock()
	defer f.mu.Unlock()
	for _, c := range f.cmds {
		if c == verb {
			return true
		}
	}
	return false
}

// 有 MOVE 的服务器（263、QQ、Gmail）：一条 UID MOVE，应答里的 COPYUID 要接住。
func TestMoveKeepsTheNewUIDFromCopyUID(t *testing.T) {
	srv := startMoveFake(t, true, "7 5 42")
	f := NewIMAP(5*time.Second, 2*time.Second, nil)
	moved, err := f.MoveMessages(context.Background(), acctFor(srv.addr), "INBOX", []uint32{5}, "已删除")
	if err != nil {
		t.Fatal(err)
	}
	if moved[5] != 42 {
		t.Fatalf("旧 5 应该对到新 42，实际 %v", moved)
	}
	if !srv.saw("UID MOVE") || srv.saw("UID COPY") {
		t.Errorf("声明了 MOVE 就该用 MOVE：%v", srv.cmds)
	}
}

// 没有 MOVE 的服务器（163）：COPY + 标删除 + EXPUNGE，COPYUID 在 COPY 的应答里。
func TestCopyFallbackAlsoKeepsTheNewUID(t *testing.T) {
	srv := startMoveFake(t, false, "7 5 42")
	f := NewIMAP(5*time.Second, 2*time.Second, nil)
	moved, err := f.MoveMessages(context.Background(), acctFor(srv.addr), "INBOX", []uint32{5}, "已删除")
	if err != nil {
		t.Fatal(err)
	}
	if moved[5] != 42 {
		t.Fatalf("COPY 路径也该接住 COPYUID，实际 %v", moved)
	}
	for _, must := range []string{"UID COPY", "UID STORE", "EXPUNGE"} {
		if !srv.saw(must) {
			t.Errorf("回退路径少了 %s：%v", must, srv.cmds)
		}
	}
	if srv.saw("UID MOVE") {
		t.Errorf("没声明 MOVE 不该发 MOVE：%v", srv.cmds)
	}
}

// 集合是范围的时候按位置一一对应：1:3 → 10:12。
func TestCopyUIDRangesPairUpByPosition(t *testing.T) {
	srv := startMoveFake(t, true, "7 1:3 10:12")
	f := NewIMAP(5*time.Second, 2*time.Second, nil)
	moved, err := f.MoveMessages(context.Background(), acctFor(srv.addr), "INBOX", []uint32{1, 2, 3}, "已删除")
	if err != nil {
		t.Fatal(err)
	}
	for k, v := range map[uint32]uint32{1: 10, 2: 11, 3: 12} {
		if moved[k] != v {
			t.Errorf("%d 应该对到 %d，实际 %v", k, v, moved)
		}
	}
}

// 服务器不带 COPYUID：挪成功、map 为空、不报错——调用方回退到搜索。
func TestAMoveWithoutCopyUIDStillSucceeds(t *testing.T) {
	srv := startMoveFake(t, true, "")
	f := NewIMAP(5*time.Second, 2*time.Second, nil)
	moved, err := f.MoveMessages(context.Background(), acctFor(srv.addr), "INBOX", []uint32{5}, "已删除")
	if err != nil {
		t.Fatalf("没有 COPYUID 不该算失败：%v", err)
	}
	if len(moved) != 0 {
		t.Errorf("没有 COPYUID 应该返回空 map，实际 %v", moved)
	}
}

// 建/改/删/列目录：命令真的发出去了，中文名用 UTF-7 编码，列回来时解回中文。
func TestFolderCommandsGoOverTheWireWithUTF7Names(t *testing.T) {
	srv := startMoveFake(t, true, "")
	f := NewIMAP(5*time.Second, 2*time.Second, nil)
	acct := acctFor(srv.addr)
	ctx := context.Background()
	if err := f.CreateFolder(ctx, acct, "客户"); err != nil {
		t.Fatal(err)
	}
	if err := f.RenameFolder(ctx, acct, "客户", "客户2026"); err != nil {
		t.Fatal(err)
	}
	if err := f.DeleteFolder(ctx, acct, "客户2026"); err != nil {
		t.Fatal(err)
	}
	names, err := f.ListFolders(ctx, acct)
	if err != nil {
		t.Fatal(err)
	}
	for _, must := range []string{"CREATE", "RENAME", "DELETE", "LIST"} {
		if !srv.saw(must) {
			t.Errorf("没发 %s：%v", must, srv.cmds)
		}
	}
	joined := strings.Join(names, ",")
	if !strings.Contains(joined, "客户") {
		t.Errorf("LIST 回来的 UTF-7 名字应该解成中文，实际 %v", names)
	}
}
