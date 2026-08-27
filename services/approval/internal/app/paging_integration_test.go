package app

import (
	"context"
	"os"
	"testing"
	"time"

	"github.com/sgao19/erp-go/pkg/pgdb"
)

// 翻页的时候同一条不能出现两次，也不能被跳过。
//
// 排序键打平（一批单据同时提交，submitted_at 落在同一毫秒）而排序里没有唯一
// 列收口时，Postgres 对这些行的先后不作任何保证——每次查询可以给出不同的顺序。
// 配上 OFFSET 分页，第 1 页和第 2 页各查一次数据库，两次顺序不一样，于是某一条
// 在两页都出现，另一条一次都不出现。
//
// 用户看到的是「我明明看到那条，翻到第二页就没了」——而且**每次刷新还不一样**，
// 这种问题报上来基本查不出。
func TestSubmittedListPagesWithoutRepeatsOrGaps(t *testing.T) {
	dsn := os.Getenv("APPROVAL_TEST_DSN")
	if dsn == "" {
		t.Skip("APPROVAL_TEST_DSN not set")
	}
	ctx := context.Background()
	pool, err := pgdb.New(ctx, dsn)
	if err != nil {
		t.Fatal(err)
	}
	defer pool.Close()

	tenantID := time.Now().UnixNano()
	const submitter = 4242
	t.Cleanup(func() {
		_, _ = pool.Exec(ctx, `DELETE FROM approval_instances WHERE tenant_id=$1`, tenantID)
	})

	// 25 条**提交时间完全相同**的实例。这是排序打平的极端情况，但不是编出来
	// 的：批量导入、一次提交多张单，时间戳就是会落在同一个瞬间。
	const n = 25
	want := map[string]bool{}
	for i := 0; i < n; i++ {
		no := "AP-PAGE-" + itoa(i)
		want[no] = true
		if _, err := pool.Exec(ctx, `INSERT INTO approval_instances
			(tenant_id, definition_id, biz_type, biz_id, biz_no, biz_summary,
			 submitter_id, submitter_name, status, current_seq, submitted_at, amount)
			VALUES ($1,1,'CONTRACT',$2,$3,'{}',$4,'提交人','RUNNING',1,
			        timestamptz '2026-08-27 10:00:00+08', 0)`,
			tenantID, i+1, no, submitter); err != nil {
			t.Fatal(err)
		}
	}

	svc := New(pool, nil, nil, "")

	// 一页 10 条翻完，把看到的单号收集起来。
	const pageSize = 10
	seen := map[string]int{}
	for page := int32(1); page <= 3; page++ {
		rows, total, err := svc.MySubmitted(ctx, tenantID, submitter, "", "", "", page, pageSize)
		if err != nil {
			t.Fatalf("第 %d 页: %v", page, err)
		}
		if total != n {
			t.Fatalf("总数该是 %d，实际 %d", n, total)
		}
		for _, r := range rows {
			seen[r.BizNo]++
		}
		// **每翻一页就动一条已经翻过去的行**：审批人正好在你翻页的时候批了
		// 一单，这是天天发生的事。在 Postgres 里 UPDATE 会把行挪到堆的末尾，
		// 于是「打平的行按什么顺序出来」跟着变——没有唯一列收口的话，这一下
		// 就足以让某条重复出现、另一条被跳过。排序键本身不动。
		if _, err := pool.Exec(ctx, `UPDATE approval_instances
			SET biz_summary = biz_summary
			WHERE tenant_id=$1 AND biz_no=$2`,
			tenantID, "AP-PAGE-"+itoa(int(page-1)*pageSize)); err != nil {
			t.Fatal(err)
		}
	}

	for no, times := range seen {
		if times > 1 {
			t.Errorf("%s 在翻页中出现了 %d 次——同一条被数了两遍", no, times)
		}
	}
	for no := range want {
		if seen[no] == 0 {
			t.Errorf("%s 一次都没出现——翻页把它跳过去了", no)
		}
	}
	if len(seen) != n {
		t.Fatalf("三页该正好覆盖 %d 条，实际见到 %d 条", n, len(seen))
	}
}
