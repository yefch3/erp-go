package mailfetch

import (
	"bufio"
	"net"
	"strings"
	"testing"
	"time"

	"github.com/emersion/go-imap/client"
)

// ID 命令发到线上必须长这样：ID ("name" "erp-go" "version" "1.0" ...)。
//
// 网易认的就是这条；括号、引号、成对的键值，少一样它就继续回 Unsafe Login。
func TestIDCommandIsAFlatParenthesisedKeyValueList(t *testing.T) {
	cmd := (&idCommand{fields: []string{"name", "erp-go", "version", "1.0"}}).Command()
	if cmd.Name != "ID" {
		t.Fatalf("命令名 %q", cmd.Name)
	}
	if len(cmd.Arguments) != 1 {
		t.Fatalf("应该只有一个参数（那个列表），实际 %d 个", len(cmd.Arguments))
	}
	list, ok := cmd.Arguments[0].([]interface{})
	if !ok {
		t.Fatalf("参数应该是一个列表，实际 %T", cmd.Arguments[0])
	}
	if len(list)%2 != 0 {
		t.Fatalf("键值必须成对，实际 %d 项：%v", len(list), list)
	}
	want := []string{"name", "erp-go", "version", "1.0"}
	for i, v := range list {
		if s, _ := v.(string); s != want[i] {
			t.Errorf("第 %d 项是 %v，应该是 %q", i, v, want[i])
		}
	}
}

// 我们真的报了身份，而不是发一条空的 ID。
func TestWeAnnounceWhoWeAre(t *testing.T) {
	if len(idFields) == 0 || len(idFields)%2 != 0 {
		t.Fatalf("身份字段必须成对且非空：%v", idFields)
	}
	joined := strings.Join(idFields, " ")
	for _, must := range []string{"name", "version", "vendor"} {
		if !strings.Contains(joined, must) {
			t.Errorf("身份里应该有 %q：%v", must, idFields)
		}
	}
}

// 一个假的 IMAP 服务器，只够走完 CAPABILITY 和 ID 两步。
type fakeIMAP struct {
	caps string
	// ID 命令来了怎么答："ok"、"bad"（服务器拒绝）、"silent"（不支持，不该被问到）
	idReply  string
	gotID    chan string
	listener net.Listener
}

func startFakeIMAP(t *testing.T, caps, idReply string) *fakeIMAP {
	t.Helper()
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	f := &fakeIMAP{caps: caps, idReply: idReply, gotID: make(chan string, 4), listener: ln}
	go f.serve()
	t.Cleanup(func() { _ = ln.Close() })
	return f
}

func (f *fakeIMAP) serve() {
	conn, err := f.listener.Accept()
	if err != nil {
		return
	}
	defer conn.Close()
	w := bufio.NewWriter(conn)
	r := bufio.NewReader(conn)
	writeLine := func(s string) {
		_, _ = w.WriteString(s + "\r\n")
		_ = w.Flush()
	}
	writeLine("* OK [CAPABILITY IMAP4rev1 " + f.caps + "] fake ready")
	for {
		line, err := r.ReadString('\n')
		if err != nil {
			return
		}
		line = strings.TrimRight(line, "\r\n")
		tag, rest, _ := strings.Cut(line, " ")
		verb, args, _ := strings.Cut(rest, " ")
		switch strings.ToUpper(verb) {
		case "LOGIN":
			writeLine(tag + " OK logged in")
		case "CAPABILITY":
			writeLine("* CAPABILITY IMAP4rev1 " + f.caps)
			writeLine(tag + " OK done")
		case "ID":
			f.gotID <- args
			switch f.idReply {
			case "bad":
				writeLine(tag + " BAD not today")
			default:
				writeLine(`* ID ("name" "fake")`)
				writeLine(tag + " OK done")
			}
		case "LOGOUT":
			writeLine("* BYE")
			writeLine(tag + " OK bye")
			return
		default:
			writeLine(tag + " OK ignored")
		}
	}
}

func (f *fakeIMAP) dial(t *testing.T) *client.Client {
	t.Helper()
	c, err := client.Dial(f.listener.Addr().String())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = c.Logout() })
	if err := c.Login("u", "p"); err != nil {
		t.Fatal(err)
	}
	return c
}

type capturingLog struct{ warns int }

func (l *capturingLog) Warn(string, ...any) { l.warns++ }

// 服务器声明支持 ID 时，我们要真的发出去，而且带着身份。
func TestIDIsSentWhenTheServerSupportsIt(t *testing.T) {
	srv := startFakeIMAP(t, "ID", "ok")
	log := &capturingLog{}
	announceID(srv.dial(t), log)
	select {
	case args := <-srv.gotID:
		for _, must := range []string{"name", "erp-go"} {
			if !strings.Contains(args, must) {
				t.Errorf("发出去的 ID 里没有 %q：%s", must, args)
			}
		}
	case <-time.After(3 * time.Second):
		t.Fatal("服务器支持 ID，但我们一条都没发")
	}
	if log.warns != 0 {
		t.Errorf("服务器答 OK，不该有告警")
	}
}

// 服务器不声明支持就不发——不给不需要它的服务商添一条命令。
func TestIDIsNotSentWhenUnsupported(t *testing.T) {
	srv := startFakeIMAP(t, "IDLE", "silent")
	announceID(srv.dial(t), &capturingLog{})
	select {
	case args := <-srv.gotID:
		t.Fatalf("服务器没声明支持 ID，不该发：%s", args)
	case <-time.After(300 * time.Millisecond):
	}
}

// **服务器拒绝 ID 不能让登录失败。** 这是一条自我介绍，不是认证：为它把一个
// 本来能用的信箱判死，比不发还糟。
func TestARefusedIDDoesNotBreakTheConnection(t *testing.T) {
	srv := startFakeIMAP(t, "ID", "bad")
	c := srv.dial(t)
	log := &capturingLog{}
	announceID(c, log) // 不返回错误，按定义
	if log.warns == 0 {
		t.Error("被拒绝了应该留一条告警，好让人查得到")
	}
	// 连接还能继续用。
	if err := c.Noop(); err != nil {
		t.Errorf("ID 被拒之后连接就废了：%v", err)
	}
}
