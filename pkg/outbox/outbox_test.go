package outbox

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
)

// 发件箱里每一行都带着「这是哪家公司的事」。收事件的服务照这一栏去改自己
// 库里的数据——payload 里写着合同号、客户名、吨数，但**哪家公司**只看这一栏。
// 所以漏填不能悄悄兜底成 1：那样公司 4 的合同会去动公司 1 的库存，两边都
// 不报错。2026-09-19 定的规矩，来由见 outbox.go 的注释。

// recordingTx 只记下 Append 有没有真的去执行 INSERT，不连数据库。嵌入
// pgx.Tx 是为了满足接口，其余方法这里都用不到（用到就会空指针，那也正是
// 我们想要的：这个假货只该被用来 Exec）。
type recordingTx struct {
	pgx.Tx
	calls int
}

func (t *recordingTx) Exec(context.Context, string, ...any) (pgconn.CommandTag, error) {
	t.calls++
	return pgconn.CommandTag{}, nil
}

func TestAppendRefusesAnEventWithNoTenant(t *testing.T) {
	ctx := context.Background()
	for _, c := range []struct {
		name   string
		tenant int64
	}{
		{"没填（Go 的零值）", 0},
		{"负数", -1},
	} {
		t.Run(c.name, func(t *testing.T) {
			tx := &recordingTx{}
			err := Append(ctx, tx, Event{
				TenantID: c.tenant, AggregateType: "contract", AggregateID: "21",
				EventType: "ContractStarted", Payload: json.RawMessage(`{}`),
			})
			if err == nil {
				t.Fatal("公司号缺失的事件应该被拒发")
			}
			if !strings.Contains(err.Error(), "tenant_id is required") {
				t.Fatalf("错误信息要说清是公司号的事，实际：%v", err)
			}
			// 带上是哪条事件，否则十来个调用点里不知道是谁漏了。
			if !strings.Contains(err.Error(), "ContractStarted") {
				t.Fatalf("错误信息要点名是哪条事件，实际：%v", err)
			}
			if tx.calls != 0 {
				t.Fatalf("被拒的事件不该写进库，实际执行了 %d 次 INSERT", tx.calls)
			}
		})
	}
}

func TestAppendStillRefusesTheOtherRequiredFields(t *testing.T) {
	ctx := context.Background()
	base := Event{
		TenantID: 4, AggregateType: "contract", AggregateID: "21",
		EventType: "ContractStarted", Payload: json.RawMessage(`{}`),
	}
	for _, c := range []struct {
		name   string
		mangle func(*Event)
	}{
		{"没有 aggregate_type", func(e *Event) { e.AggregateType = "" }},
		{"没有 aggregate_id", func(e *Event) { e.AggregateID = "" }},
		{"没有 event_type", func(e *Event) { e.EventType = "" }},
	} {
		t.Run(c.name, func(t *testing.T) {
			tx := &recordingTx{}
			e := base
			c.mangle(&e)
			if err := Append(ctx, tx, e); err == nil {
				t.Fatal("必填字段缺失应该被拒")
			}
			if tx.calls != 0 {
				t.Fatalf("被拒的事件不该写进库，实际执行了 %d 次 INSERT", tx.calls)
			}
		})
	}
}

func TestAppendWritesAnEventThatNamesItsTenant(t *testing.T) {
	tx := &recordingTx{}
	err := Append(context.Background(), tx, Event{
		TenantID: 4, AggregateType: "contract", AggregateID: "21",
		EventType: "ContractStarted", Payload: json.RawMessage(`{"contract_no":"CT-202609-0019"}`),
	})
	if err != nil {
		t.Fatalf("填全了的事件应该能发：%v", err)
	}
	if tx.calls != 1 {
		t.Fatalf("应该正好写一行，实际 %d 行", tx.calls)
	}
}
