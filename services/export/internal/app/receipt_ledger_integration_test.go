package app

import (
	"bytes"
	"context"
	"log/slog"
	"os"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/shopspring/decimal"

	"github.com/sgao19/erp-go/pkg/apierr"
	"github.com/sgao19/erp-go/pkg/pgdb"
)

// F2 之后核销的对面站着另一个服务。这个假账本让下面的测试能盯住真正难的
// 那部分——**跨库之后金额还守不守得住**——而不用把采购也拉起来。
//
// 它记下每一次 SetClaim，因为「报回去的是总额不是增量」是一条只有看调用
// 记录才验得出来的规矩：报增量的话重试一次就把数加了两遍。
type fakeLedger struct {
	mu     sync.Mutex
	rows   map[int64]BankRow
	claims []string
	fails  bool
}

func newFakeLedger(rows ...BankRow) *fakeLedger {
	m := make(map[int64]BankRow, len(rows))
	for _, r := range rows {
		m[r.ID] = r
	}
	return &fakeLedger{rows: m}
}

func (f *fakeLedger) Get(_ context.Context, id int64) (BankRow, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	r, ok := f.rows[id]
	if !ok {
		return BankRow{}, apierr.NotFound("BANK_TXN_NOT_FOUND", "银行流水不存在")
	}
	return r, nil
}

func (f *fakeLedger) List(context.Context, BankLedgerQuery) ([]BankRow, int64, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	out := make([]BankRow, 0, len(f.rows))
	for _, r := range f.rows {
		out = append(out, r)
	}
	return out, int64(len(out)), nil
}

func (f *fakeLedger) Record(_ context.Context, in BankRowInput) (BankRow, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	id := int64(len(f.rows) + 1000)
	r := BankRow{
		ID: id, BankRef: in.BankRef, Direction: in.Direction, Amount: in.Amount,
		Currency: in.Currency, ValueDate: in.ValueDate, Counterparty: in.Counterparty,
		RemittanceInfo: in.RemittanceInfo, Ownership: OwnershipCustomer,
	}
	f.rows[id] = r
	return r, nil
}

func (f *fakeLedger) SetClaim(_ context.Context, id int64, claimed string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.fails {
		return context.DeadlineExceeded
	}
	f.claims = append(f.claims, claimed)
	r := f.rows[id]
	r.ClaimedAmount = claimed
	f.rows[id] = r
	return nil
}

func (f *fakeLedger) SetOwnership(_ context.Context, id int64, ownership, detail string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	r := f.rows[id]
	r.Ownership, r.OwnershipDetail = ownership, detail
	f.rows[id] = r
	return nil
}

func (f *fakeLedger) ListAccounts(context.Context) ([]BankAccount, error) { return nil, nil }
func (f *fakeLedger) CreateAccount(context.Context, BankAccount) (int64, error) {
	return 1, nil
}

func (f *fakeLedger) claimLog() []string {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]string(nil), f.claims...)
}

// 核销这件事跨库之后还守不守得住。钉的是四条：
//
//	· 分配总额不许超过到账金额——这是整个页面存在的理由
//	· 币种不一致要拒绝，不能猜汇率
//	· 归属不是「客户收款」的行不许核到应收合同
//	· 冲销之后钱回到未分配余额，而且报回账本的是**重算后的总额**
func TestAllocateAgainstSharedLedger(t *testing.T) {
	dsn := os.Getenv("EXPORT_TEST_DSN")
	if dsn == "" {
		t.Skip("EXPORT_TEST_DSN not set; skipping DB-backed receipt test")
	}
	ctx := context.Background()
	pool, err := pgdb.New(ctx, dsn)
	if err != nil {
		t.Fatal(err)
	}
	defer pool.Close()
	tenantID := time.Now().UnixNano()
	defer func() {
		_, _ = pool.Exec(ctx, `DELETE FROM receipt_allocations WHERE tenant_id=$1`, tenantID)
		_, _ = pool.Exec(ctx, `DELETE FROM contract_versions WHERE tenant_id=$1`, tenantID)
		_, _ = pool.Exec(ctx, `DELETE FROM contracts WHERE tenant_id=$1`, tenantID)
	}()

	contractID := seedContract(ctx, t, pool, tenantID, "CT-LEDGER-1", "USD", "10000")
	eurID := seedContract(ctx, t, pool, tenantID, "CT-LEDGER-EUR", "EUR", "10000")

	ledger := newFakeLedger(
		BankRow{ID: 7001, BankRef: "REF-A", Direction: "CREDIT", Amount: "10000.00",
			Currency: "USD", ValueDate: "2026-08-20", Ownership: OwnershipCustomer},
		BankRow{ID: 7002, BankRef: "REF-OUT", Direction: "DEBIT", Amount: "500.00",
			Currency: "USD", ValueDate: "2026-08-20", Ownership: OwnershipCustomer},
		BankRow{ID: 7003, BankRef: "REF-TAX", Direction: "CREDIT", Amount: "800.00",
			Currency: "USD", ValueDate: "2026-08-20", Ownership: OwnershipTaxRefund},
	)
	svc := New(pool, Deps{Bank: ledger})
	op := Operator{ID: 5, Name: "Finance"}

	// 超额分配：到账 10000，想核 12000。这一条守不住，整个页面就没有意义。
	_, err = svc.Allocate(ctx, tenantID, 7001,
		[]AllocationLine{{ContractID: contractID, Amount: "12000"}}, op)
	if err == nil || !strings.Contains(err.Error(), "EX_ALLOC_EXCEEDS_PAYMENT") {
		t.Fatalf("超额分配应该被拒绝，实际 %v", err)
	}

	// 币种不一致：猜一个汇率就等于凭空造钱，拒绝才是诚实的。
	_, err = svc.Allocate(ctx, tenantID, 7001,
		[]AllocationLine{{ContractID: eurID, Amount: "100"}}, op)
	if err == nil || !strings.Contains(err.Error(), "EX_ALLOC_CURRENCY_MISMATCH") {
		t.Fatalf("币种不一致应该被拒绝，实际 %v", err)
	}

	// 付出去的钱核不到应收上。
	_, err = svc.Allocate(ctx, tenantID, 7002,
		[]AllocationLine{{ContractID: contractID, Amount: "100"}}, op)
	if err == nil || !strings.Contains(err.Error(), "EX_TX_NOT_CREDIT") {
		t.Fatalf("出账应该被拒绝，实际 %v", err)
	}

	// 归属是退税的行不该出现在应收核销里——F2 之前这条闸看的是 disposition，
	// 现在看归属，同一件事换了个说得通的说法。
	_, err = svc.Allocate(ctx, tenantID, 7003,
		[]AllocationLine{{ContractID: contractID, Amount: "100"}}, op)
	if err == nil || !strings.Contains(err.Error(), "EX_TX_IRRELEVANT") {
		t.Fatalf("归属非客户的行应该被拒绝，实际 %v", err)
	}

	// 正常核一半。
	view, err := svc.Allocate(ctx, tenantID, 7001,
		[]AllocationLine{{ContractID: contractID, Amount: "4000"}}, op)
	if err != nil {
		t.Fatalf("allocate: %v", err)
	}
	if view.AllocatedAmount != "4000.00" || view.UnallocatedAmount != "6000.00" {
		t.Fatalf("核了一半之后对不上: 已核 %s 未核 %s", view.AllocatedAmount, view.UnallocatedAmount)
	}
	// 只核了一半，这一行还该留在队列里。
	if got := view.Disposition(); got != DispositionUnprocessed {
		t.Fatalf("核了一半应该还是待处理，实际 %s", got)
	}

	// 再核剩下的：核完，状态翻成已核销。
	view, err = svc.Allocate(ctx, tenantID, 7001,
		[]AllocationLine{{ContractID: contractID, Amount: "6000"}}, op)
	if err != nil {
		t.Fatalf("allocate rest: %v", err)
	}
	if got := view.Disposition(); got != DispositionAllocated {
		t.Fatalf("核完应该是已核销，实际 %s", got)
	}

	// **报给账本的是总额不是增量**：两次分别是 4000 和 10000，不是 4000 和 6000。
	// 报增量的话，一次网络重试就把数加了两遍。
	if log := ledger.claimLog(); len(log) != 2 || log[0] != "4000.00" || log[1] != "10000.00" {
		t.Fatalf("报给账本的应该是重算后的总额，实际 %v", log)
	}

	// 冲销：钱回到未分配余额，账本上的认领数跟着降回去。
	if len(view.Allocations) == 0 {
		t.Fatal("没有可冲销的记录")
	}
	first := view.Allocations[0]
	view, err = svc.ReverseAllocation(ctx, tenantID, first.ID, "客户重复付款", op)
	if err != nil {
		t.Fatalf("reverse: %v", err)
	}
	if view.UnallocatedAmount != "4000.00" {
		t.Fatalf("冲销之后未分配余额应该是 4000.00，实际 %s", view.UnallocatedAmount)
	}
	if log := ledger.claimLog(); log[len(log)-1] != "6000.00" {
		t.Fatalf("冲销之后应该报 6000.00，实际 %v", log)
	}
}

// 两个人同时核销同一笔汇款。
//
// 原来靠 SELECT ... FOR UPDATE 锁住银行流水那一行；行搬到采购库之后跨库锁不
// 住，换成了本库的事务级建议锁。**这个测试就是那把锁的理由**：不排队的话，
// 两边都会看见「还没核」，加起来发出去的钱比实际到账的多。
func TestConcurrentAllocationsCannotOverdraw(t *testing.T) {
	dsn := os.Getenv("EXPORT_TEST_DSN")
	if dsn == "" {
		t.Skip("EXPORT_TEST_DSN not set; skipping DB-backed concurrency test")
	}
	ctx := context.Background()
	pool, err := pgdb.New(ctx, dsn)
	if err != nil {
		t.Fatal(err)
	}
	defer pool.Close()
	tenantID := time.Now().UnixNano()
	defer func() {
		_, _ = pool.Exec(ctx, `DELETE FROM receipt_allocations WHERE tenant_id=$1`, tenantID)
		_, _ = pool.Exec(ctx, `DELETE FROM contract_versions WHERE tenant_id=$1`, tenantID)
		_, _ = pool.Exec(ctx, `DELETE FROM contracts WHERE tenant_id=$1`, tenantID)
	}()

	a := seedContract(ctx, t, pool, tenantID, "CT-RACE-A", "USD", "10000")
	b := seedContract(ctx, t, pool, tenantID, "CT-RACE-B", "USD", "10000")

	ledger := newFakeLedger(BankRow{
		ID: 7100, BankRef: "REF-RACE", Direction: "CREDIT", Amount: "10000.00",
		Currency: "USD", ValueDate: "2026-08-20", Ownership: OwnershipCustomer,
	})
	svc := New(pool, Deps{Bank: ledger})

	// 两边各要核 6000。到账只有 10000，所以**必须恰好一边成功**。
	var wg sync.WaitGroup
	errs := make([]error, 2)
	for i, contractID := range []int64{a, b} {
		wg.Add(1)
		go func(i int, cid int64) {
			defer wg.Done()
			_, errs[i] = svc.Allocate(ctx, tenantID, 7100,
				[]AllocationLine{{ContractID: cid, Amount: "6000"}},
				Operator{ID: int64(i + 1), Name: "Clerk"})
		}(i, contractID)
	}
	wg.Wait()

	ok := 0
	for _, e := range errs {
		if e == nil {
			ok++
			continue
		}
		if !strings.Contains(e.Error(), "EX_ALLOC_EXCEEDS_PAYMENT") {
			t.Fatalf("失败的那一边理由应该是超额，实际 %v", e)
		}
	}
	if ok != 1 {
		t.Fatalf("应该恰好一边成功，实际成功 %d 边（errs=%v）——锁没起作用，"+
			"这正是「发出去的钱比到账多」的现场", ok, errs)
	}

	// 再从账上确认一遍：核销总额不能超过到账。
	var sum decimal.Decimal
	var raw string
	if err := pool.QueryRow(ctx, `SELECT coalesce(sum(amount),0)::text
		FROM receipt_allocations WHERE tenant_id=$1 AND transaction_id=7100`,
		tenantID).Scan(&raw); err != nil {
		t.Fatal(err)
	}
	sum, _ = decimal.NewFromString(raw)
	if sum.GreaterThan(decimal.NewFromInt(10000)) {
		t.Fatalf("核销总额 %s 超过了到账的 10000", sum)
	}
}

// seedContract 造一张生效合同，带一个已批准的版本——核销要拿版本上的币种和
// 金额来判断，没有版本的合同核不了（这本身也是一条被测过的规矩）。
func seedContract(ctx context.Context, t *testing.T, pool *pgxpool.Pool,
	tenantID int64, no, currency, total string) int64 {
	t.Helper()
	var id int64
	if err := pool.QueryRow(ctx, `INSERT INTO contracts
		(tenant_id, contract_no, customer_id, customer_name, status,
		 sales_employee_id, sales_employee, effective_at, created_by, updated_by)
		VALUES ($1,$2,9,'客户','EFFECTIVE',3,'销售', now() - interval '3 days', 1, 1)
		RETURNING id`, tenantID, no).Scan(&id); err != nil {
		t.Fatal(err)
	}
	var versionID int64
	if err := pool.QueryRow(ctx, `INSERT INTO contract_versions
		(tenant_id, contract_id, version_no, status, currency, total_amount,
		 created_by, fx_rate, fx_rate_at, fx_source)
		VALUES ($1,$2,1,'APPROVED',$3,$4::numeric,1,1,now(),'TEST')
		RETURNING id`, tenantID, id, currency, total).Scan(&versionID); err != nil {
		t.Fatal(err)
	}
	if _, err := pool.Exec(ctx, `UPDATE contracts SET current_version_id=$2 WHERE id=$1`,
		id, versionID); err != nil {
		t.Fatal(err)
	}
	return id
}

// 一边核销、一边把同一笔流水标成「与应收无关」。
//
// 「已经核销掉的钱不能改归属」是一个**读了再判断**的检查，不拿锁就挡不住
// 任何东西：甲读到空、乙核销成功、甲照样把归属改走，于是几条核销记录指着
// 一笔写着「我不是客户的钱」的流水，而合同上那笔钱照样算收到了。
//
// F2 之前这里是 SELECT ... FOR UPDATE；行搬到采购库之后我改写时把锁丢了，
// 判断留着——判断留着更糟，因为它看起来像还挡着。
func TestMarkIrrelevantRacesAllocation(t *testing.T) {
	dsn := os.Getenv("EXPORT_TEST_DSN")
	if dsn == "" {
		t.Skip("EXPORT_TEST_DSN not set; skipping DB-backed race test")
	}
	ctx := context.Background()
	pool, err := pgdb.New(ctx, dsn)
	if err != nil {
		t.Fatal(err)
	}
	defer pool.Close()

	// 单跑一次赢面靠运气，所以跑几轮：**只要有一轮出现「归属改走了而核销
	// 还在」，就是账错了**。
	for round := 0; round < 12; round++ {
		tenantID := time.Now().UnixNano() + int64(round)
		contractID := seedContract(ctx, t, pool, tenantID, "CT-RACE-MI", "USD", "10000")
		ledger := newFakeLedger(BankRow{
			ID: 7200, BankRef: "REF-MI", Direction: "CREDIT", Amount: "10000.00",
			Currency: "USD", ValueDate: "2026-08-20", Ownership: OwnershipCustomer,
		})
		svc := New(pool, Deps{Bank: ledger})

		var wg sync.WaitGroup
		var allocErr, markErr error
		wg.Add(2)
		go func() {
			defer wg.Done()
			_, allocErr = svc.Allocate(ctx, tenantID, 7200,
				[]AllocationLine{{ContractID: contractID, Amount: "10000"}},
				Operator{ID: 1, Name: "甲"})
		}()
		go func() {
			defer wg.Done()
			_, markErr = svc.MarkIrrelevant(ctx, tenantID, 7200, "TAX_REFUND", "", Operator{ID: 2, Name: "乙"})
		}()
		wg.Wait()

		var live string
		if err := pool.QueryRow(ctx, `SELECT coalesce(sum(amount),0)::text
			FROM receipt_allocations WHERE tenant_id=$1 AND transaction_id=7200`,
			tenantID).Scan(&live); err != nil {
			t.Fatal(err)
		}
		row, _ := ledger.Get(ctx, 7200)

		// 两个动作互斥：要么核销成了（归属还是客户），要么标记成了（一分没核）。
		allocated := mustDec(live).IsPositive()
		movedAway := row.Ownership != OwnershipCustomer
		if allocated && movedAway {
			t.Fatalf("第 %d 轮：核销了 %s 却把归属改成了 %q——"+
				"这几条核销记录现在指着一笔写着「我不是客户的钱」的流水"+
				"（alloc=%v mark=%v）", round, live, row.Ownership, allocErr, markErr)
		}
		if !allocated && !movedAway && allocErr == nil && markErr == nil {
			t.Fatalf("第 %d 轮：两个都说成功了，账上却什么都没发生", round)
		}

		_, _ = pool.Exec(ctx, `DELETE FROM receipt_allocations WHERE tenant_id=$1`, tenantID)
		_, _ = pool.Exec(ctx, `DELETE FROM contract_versions WHERE tenant_id=$1`, tenantID)
		_, _ = pool.Exec(ctx, `DELETE FROM contracts WHERE tenant_id=$1`, tenantID)
	}
}

// 列表**不做详情才需要的活**。
//
// 原来列表对每一行调 viewOf，而 viewOf 会再查一次核销明细、再扫一次汇款附言
// 找合同号——20 行一页最多 40 次往返，换来的东西列表上一样也用不着。
//
// 这个测试从行为上钉住：列表给出正确的已核金额，但不带明细也不带建议。
func TestListDoesNotDoDetailWork(t *testing.T) {
	dsn := os.Getenv("EXPORT_TEST_DSN")
	if dsn == "" {
		t.Skip("EXPORT_TEST_DSN not set; skipping DB-backed list test")
	}
	ctx := context.Background()
	pool, err := pgdb.New(ctx, dsn)
	if err != nil {
		t.Fatal(err)
	}
	defer pool.Close()
	tenantID := time.Now().UnixNano()
	defer func() {
		_, _ = pool.Exec(ctx, `DELETE FROM receipt_allocations WHERE tenant_id=$1`, tenantID)
		_, _ = pool.Exec(ctx, `DELETE FROM contract_versions WHERE tenant_id=$1`, tenantID)
		_, _ = pool.Exec(ctx, `DELETE FROM contracts WHERE tenant_id=$1`, tenantID)
	}()

	contractID := seedContract(ctx, t, pool, tenantID, "EXP-2026-0031", "USD", "10000")
	ledger := newFakeLedger(
		// 附言里带一个真实存在的合同号：详情会把它扫成建议，列表不该扫。
		BankRow{ID: 7300, BankRef: "REF-L1", Direction: "CREDIT", Amount: "10000.00",
			Currency: "USD", ValueDate: "2026-08-20", Ownership: OwnershipCustomer,
			RemittanceInfo: "PAYMENT FOR EXP-2026-0031"},
		BankRow{ID: 7301, BankRef: "REF-L2", Direction: "CREDIT", Amount: "500.00",
			Currency: "USD", ValueDate: "2026-08-21", Ownership: OwnershipCustomer},
	)
	svc := New(pool, Deps{Bank: ledger})
	op := Operator{ID: 5, Name: "Finance"}

	if _, err := svc.Allocate(ctx, tenantID, 7300,
		[]AllocationLine{{ContractID: contractID, Amount: "4000"}}, op); err != nil {
		t.Fatal(err)
	}

	rows, _, err := svc.ListTransactions(ctx, tenantID, TransactionQuery{Page: 1, Size: 20})
	if err != nil {
		t.Fatal(err)
	}
	byID := map[int64]TransactionView{}
	for _, r := range rows {
		byID[r.Transaction.ID] = r
	}
	// 已核金额要对——这是列表唯一需要从核销表拿的东西。
	if got := byID[7300].AllocatedAmount; got != "4000.00" {
		t.Fatalf("列表上的已核金额不对: %q", got)
	}
	if got := byID[7300].UnallocatedAmount; got != "6000.00" {
		t.Fatalf("列表上的未核金额不对: %q", got)
	}
	// 一分没核的那行也要给 0，不能给空。
	if got := byID[7301].AllocatedAmount; got != "0.00" {
		t.Fatalf("没核过的行应该是 0.00，实际 %q", got)
	}
	// 明细和建议是详情的活，列表不该带。
	if len(byID[7300].Allocations) != 0 || len(byID[7300].Suggestions) != 0 {
		t.Fatalf("列表带上了详情才要的东西: allocs=%d suggestions=%d",
			len(byID[7300].Allocations), len(byID[7300].Suggestions))
	}
	// 详情该带的还得带——别把活删过头了。
	detail, err := svc.GetTransaction(ctx, tenantID, 7300)
	if err != nil {
		t.Fatal(err)
	}
	if len(detail.Allocations) == 0 {
		t.Fatal("详情应该带核销明细")
	}
	if len(detail.Suggestions) == 0 {
		t.Fatal("详情应该把附言里的合同号扫成建议")
	}
}

// 核销成了、但账本那边没跟上时**必须留下痕迹**。
//
// 原来这里是 `_ = s.bank.SetClaim(...)`：采购服务抖一下、或者用户核销完立刻
// 关掉页面（ctx 被取消），那一行就永远停在「待处理」，而没有任何地方能告诉
// 人为什么。核销本身不该因此失败——核销记录已经落库了，那是真相——但一声
// 不吭不行。
func TestClaimReportFailureIsLogged(t *testing.T) {
	dsn := os.Getenv("EXPORT_TEST_DSN")
	if dsn == "" {
		t.Skip("EXPORT_TEST_DSN not set; skipping DB-backed logging test")
	}
	ctx := context.Background()
	pool, err := pgdb.New(ctx, dsn)
	if err != nil {
		t.Fatal(err)
	}
	defer pool.Close()
	tenantID := time.Now().UnixNano()
	defer func() {
		_, _ = pool.Exec(ctx, `DELETE FROM receipt_allocations WHERE tenant_id=$1`, tenantID)
		_, _ = pool.Exec(ctx, `DELETE FROM contract_versions WHERE tenant_id=$1`, tenantID)
		_, _ = pool.Exec(ctx, `DELETE FROM contracts WHERE tenant_id=$1`, tenantID)
	}()

	contractID := seedContract(ctx, t, pool, tenantID, "CT-LOG-1", "USD", "10000")
	ledger := newFakeLedger(BankRow{
		ID: 7400, BankRef: "REF-LOG", Direction: "CREDIT", Amount: "10000.00",
		Currency: "USD", ValueDate: "2026-08-20", Ownership: OwnershipCustomer,
	})
	ledger.fails = true // 账本那边写不进去

	var buf bytes.Buffer
	svc := New(pool, Deps{
		Bank: ledger,
		Log:  slog.New(slog.NewTextHandler(&buf, &slog.HandlerOptions{Level: slog.LevelWarn})),
	})

	view, err := svc.Allocate(ctx, tenantID, 7400,
		[]AllocationLine{{ContractID: contractID, Amount: "10000"}},
		Operator{ID: 5, Name: "Finance"})
	// 核销本身要成功：核销记录已经落库了，为一个筛选把它撤掉是拿真的换假的。
	if err != nil {
		t.Fatalf("账本写不进去不该让核销失败: %v", err)
	}
	if view.AllocatedAmount != "10000.00" {
		t.Fatalf("核销记录该是实打实的: %q", view.AllocatedAmount)
	}

	// 但一定要说出来，而且要说清楚是哪一笔、多少钱、会怎么样。
	logged := buf.String()
	for _, want := range []string{"7400", "10000.00", "待处理"} {
		if !strings.Contains(logged, want) {
			t.Fatalf("日志里少了 %q，查起来对不上号。实际:\n%s", want, logged)
		}
	}
}
