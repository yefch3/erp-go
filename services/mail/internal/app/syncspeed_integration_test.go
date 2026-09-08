package app

import (
	"bytes"
	"context"
	"encoding/json"
	"log/slog"
	"strings"
	"testing"
	"time"
)

// slowHost 是一台「下载很慢、但本机处理照常快」的服务器。延迟全部加在
// Fetch 上，正是真实世界里时间花掉的地方。
type slowHost struct {
	*folderHost
	delay time.Duration
}

func (h *slowHost) Fetch(ctx context.Context, a MailAccount, folder string, since, n uint32) (FetchResult, error) {
	time.Sleep(h.delay)
	return h.folderHost.Fetch(ctx, a, folder, since, n)
}

// folder synced 那行日志报的速度，必须是从服务器上搬信的速度。
//
// 这条测试存在的原因：#389 第一版把计时起点放在 Fetch 之后，量到的是解析
// 加入库——本机操作，快一到两个数量级。日志照常打印、字段名照旧、数字也很
// 好看，没有任何地方看得出它答非所问。生产上量到的 1268 KB/s 就是这么来的，
// 而同一条线路真实的下载速度只有它的一半上下。
//
// 所以这里钉的不是「有没有打日志」，是「计时窗口有没有罩住下载」。
func TestSyncSpeedIsMeasuredOverTheDownloadNotTheIngest(t *testing.T) {
	f := newFolderFixture(t, 9311)
	ctx := context.Background()

	const delay = 200 * time.Millisecond
	f.host.serves = map[string][]RawMessage{
		"INBOX": {{UID: 1, Raw: rawMail("slow@mid", "慢线路上的信")}},
	}
	f.svc.UseMailbox(&slowHost{folderHost: f.host, delay: delay})

	var logbuf bytes.Buffer
	f.svc.log = slog.New(slog.NewJSONHandler(&logbuf, nil))

	cfg := SyncConfig{TenantID: f.tenantID, BatchSize: 50, HistoryCap: 500}
	if _, err := f.svc.SyncMailbox(ctx, cfg, f.account); err != nil {
		t.Fatal(err)
	}

	line := findLogLine(t, logbuf.String(), "folder synced", "INBOX")
	if line == nil {
		t.Fatalf("收了信却没有 folder synced 这行日志：%s", logbuf.String())
	}

	fetchNanos, ok := line["fetch"].(float64)
	if !ok {
		t.Fatalf("日志里没有 fetch 这个字段，无从知道下载花了多久：%v", line)
	}
	if time.Duration(fetchNanos) < delay {
		t.Errorf("fetch 报的是 %v，比服务器实际慢的 %v 还短——计时窗口没罩住下载，"+
			"量到的是入库速度", time.Duration(fetchNanos), delay)
	}

	// took 是整趟，必然不短于其中的下载那段。两个字段搞反了同样会被这条抓住。
	tookNanos, ok := line["took"].(float64)
	if !ok {
		t.Fatalf("日志里没有 took 这个字段：%v", line)
	}
	if tookNanos < fetchNanos {
		t.Errorf("took=%v 比 fetch=%v 还短，两个字段反了",
			time.Duration(tookNanos), time.Duration(fetchNanos))
	}
}

// findLogLine 在 JSON 日志里找 msg 和 folder 都对得上的那一行。
func findLogLine(t *testing.T, out, msg, folder string) map[string]any {
	t.Helper()
	for _, raw := range strings.Split(out, "\n") {
		if strings.TrimSpace(raw) == "" {
			continue
		}
		var rec map[string]any
		if err := json.Unmarshal([]byte(raw), &rec); err != nil {
			continue
		}
		if rec["msg"] == msg && rec["folder"] == folder {
			return rec
		}
	}
	return nil
}
