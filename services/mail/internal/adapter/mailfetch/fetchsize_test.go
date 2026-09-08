package mailfetch

import (
	"bufio"
	"context"
	"fmt"
	"net"
	"strings"
	"sync"
	"testing"
	"time"
)

// 一批最多下多少字节，是这段逻辑的全部要点：原来按封数限流，等于假设每封信
// 一样大。50 封普通信几 MB 没问题，混进一封 55 MB 的就在 90 秒里下不完，而
// 下不完游标就不前进——那个信箱从此不再收新信。

func TestBatchesAreSplitByBytesNotByCount(t *testing.T) {
	const mb = 1 << 20
	uids := []uint32{1, 2, 3, 4, 5}
	sizes := map[uint32]int64{1: 6 * mb, 2: 6 * mb, 3: 6 * mb, 4: 6 * mb, 5: 6 * mb}

	got := splitBySize(uids, sizes, 20*mb)
	if len(got) != 2 {
		t.Fatalf("30MB 按 20MB 一批应该切成两批，实际 %d 批：%v", len(got), got)
	}
	if len(got[0]) != 3 || len(got[1]) != 2 {
		t.Errorf("应该是 3+2（18MB 装得下，再加一封超了），实际 %v", got)
	}
}

func TestAMessageBiggerThanTheLimitGetsItsOwnBatch(t *testing.T) {
	const mb = 1 << 20
	uids := []uint32{10, 11, 12}
	sizes := map[uint32]int64{10: 1 * mb, 11: 55 * mb, 12: 1 * mb}

	got := splitBySize(uids, sizes, 20*mb)
	if len(got) != 3 {
		t.Fatalf("大信该自成一批，前后各一批，实际 %d 批：%v", len(got), got)
	}
	if len(got[1]) != 1 || got[1][0] != 11 {
		t.Errorf("中间那一批应该只有 55MB 那一封，实际 %v", got[1])
	}
}

// **顺序不能乱。** 收信游标是「取到的最大 UID」，一批失败就停在那里、下一轮
// 重来；如果批次不是从小到大，游标会越过还没下到的信，那封信从此永远收不到。
func TestBatchesKeepAscendingUIDOrder(t *testing.T) {
	const mb = 1 << 20
	uids := []uint32{30, 10, 20} // 服务器给的顺序不保证
	sizes := map[uint32]int64{10: 1 * mb, 20: 1 * mb, 30: 1 * mb}

	got := splitBySize(uids, sizes, 20*mb)
	var flat []uint32
	for _, b := range got {
		flat = append(flat, b...)
	}
	for i := 1; i < len(flat); i++ {
		if flat[i] <= flat[i-1] {
			t.Fatalf("UID 必须从小到大，实际 %v", flat)
		}
	}
}

// 服务器没报大小的，按上限算：宁可把它单独拎出来，也不要让它混进一批、
// 把整批的期限估低——估低的后果是整批白下一次。
func TestAnUnknownSizeIsTreatedAsTheWholeBudget(t *testing.T) {
	const mb = 1 << 20
	uids := []uint32{1, 2}
	sizes := map[uint32]int64{1: 1 * mb} // 2 号没报

	got := splitBySize(uids, sizes, 20*mb)
	if len(got) != 2 {
		t.Fatalf("大小不明的该自己一批，实际 %v", got)
	}
}

func TestTheDeadlineGrowsWithTheBatchButHasACeiling(t *testing.T) {
	const mb = 1 << 20
	base := 90 * time.Second

	if got := fetchTimeoutFor(base, 0); got != base {
		t.Errorf("空批就是基准，实际 %v", got)
	}
	small := fetchTimeoutFor(base, 1*mb)
	if small <= base || small > 2*base {
		t.Errorf("1MB 应该略高于基准，实际 %v", small)
	}
	big := fetchTimeoutFor(base, 55*mb)
	if big <= small {
		t.Errorf("55MB 的期限该比 1MB 长，实际 %v vs %v", big, small)
	}
	// 有顶：一条永远下不完的命令会把一个 worker 永久占住。
	huge := fetchTimeoutFor(base, 100*1024*mb)
	if huge != base*maxFetchTimeoutFactor {
		t.Errorf("再大也该封顶在 %v，实际 %v", base*maxFetchTimeoutFactor, huge)
	}
}

// ---------------------------------------------------------------- 走一遍线

// sizeFake 是一台会报大小的假服务器，记下每条 UID FETCH 要了哪些 UID。
type sizeFake struct {
	addr    string
	mu      sync.Mutex
	fetches [][]int // 每条内容 FETCH 实际要了哪些 UID
	sizes   map[int]int
}

func startSizeFake(t *testing.T, sizes map[int]int) *sizeFake {
	t.Helper()
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = ln.Close() })
	f := &sizeFake{addr: ln.Addr().String(), sizes: sizes}
	go func() {
		conn, err := ln.Accept()
		if err != nil {
			return
		}
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
			case strings.HasPrefix(upper, "LOGIN"), strings.HasPrefix(upper, "CAPABILITY"):
				say(tag + " OK done")
			case strings.HasPrefix(upper, "SELECT"), strings.HasPrefix(upper, "EXAMINE"):
				say(fmt.Sprintf("* %d EXISTS", len(sizes)))
				say("* OK [UIDVALIDITY 7] ok")
				say(tag + " OK [READ-ONLY] done")
			case strings.HasPrefix(upper, "UID SEARCH"):
				var ids []string
				for u := 1; u <= len(sizes); u++ {
					ids = append(ids, fmt.Sprint(u))
				}
				say("* SEARCH " + strings.Join(ids, " "))
				say(tag + " OK done")
			case strings.HasPrefix(upper, "UID FETCH") && strings.Contains(upper, "RFC822.SIZE"):
				for u, n := range sizes {
					say(fmt.Sprintf("* %d FETCH (UID %d RFC822.SIZE %d)", u, u, n))
				}
				say(tag + " OK done")
			case strings.HasPrefix(upper, "UID FETCH"):
				set, _, _ := strings.Cut(rest[len("UID FETCH "):], " ")
				want := expandUIDSet(set)
				f.mu.Lock()
				f.fetches = append(f.fetches, want)
				f.mu.Unlock()
				for _, u := range want {
					body := "From: a@b\r\nSubject: s\r\nMessage-ID: <" + fmt.Sprint(u) + "@x>\r\n\r\nx\r\n"
					say(fmt.Sprintf("* %d FETCH (UID %d FLAGS () INTERNALDATE \"01-Sep-2026 10:00:00 +0800\" BODY[] {%d}", u, u, len(body)))
					_, _ = w.WriteString(body)
					say(")")
				}
				say(tag + " OK done")
			case strings.HasPrefix(upper, "LOGOUT"):
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

func (f *sizeFake) contentFetches() [][]int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([][]int(nil), f.fetches...)
}

// 走一遍真的线：一个信箱里混着一封大信，内容必须分成几条 FETCH 发，而且大的
// 那封自己一条——不能像原来那样一条命令把 50 封全要过来。
func TestABigMessageIsFetchedOnItsOwnOverTheWire(t *testing.T) {
	const mb = 1 << 20
	srv := startSizeFake(t, map[int]int{1: 1 * mb, 2: 30 * mb, 3: 1 * mb})
	f := NewIMAP(5*time.Second, 2*time.Second, nil)

	res, err := f.Fetch(context.Background(), acctFor(srv.addr), "INBOX", 0, 50)
	if err != nil {
		t.Fatal(err)
	}
	if len(res.Messages) != 3 {
		t.Fatalf("三封都该收到，实际 %d 封", len(res.Messages))
	}
	got := srv.contentFetches()
	if len(got) < 2 {
		t.Fatalf("混着一封 30MB 的信，内容不该一条命令要完：%v", got)
	}
	for _, set := range got {
		if len(set) > 1 {
			for _, u := range set {
				if u == 2 {
					t.Errorf("30MB 那封该自己一批，实际和别人挤在一起：%v", set)
				}
			}
		}
	}
}

// expandUIDSet 把 IMAP 的集合写法展开：go-imap 会把连号写成 1:3，假服务器
// 得认得——不然它只回第一封，而那会让测试为了错误的理由变红。
func expandUIDSet(set string) []int {
	var out []int
	for _, part := range strings.Split(set, ",") {
		lo, hi, isRange := strings.Cut(part, ":")
		var a, b int
		_, _ = fmt.Sscanf(lo, "%d", &a)
		if !isRange {
			b = a
		} else {
			_, _ = fmt.Sscanf(hi, "%d", &b)
		}
		for u := a; u <= b && u > 0; u++ {
			out = append(out, u)
		}
	}
	return out
}
