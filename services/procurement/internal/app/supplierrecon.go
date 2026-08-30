package app

import (
	"context"
	"errors"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/shopspring/decimal"

	"github.com/sgao19/erp-go/pkg/apierr"
	"github.com/sgao19/erp-go/pkg/pgdb"
)

// 供应商对账：一张采购单一行，付了多少由人填，算不算完也由人说。
//
// 需求的原话是「供应商核销里面列出所有的采购订单，然后员工手动核销，系统
// 不需要做任何核对步骤，供应商发票只是员工用来上传留凭证用的」。所以这一层
// 从第一行起就没有「数字要对得上」这类闸——**唯一**的一道是退款不能超过
// 已付净额，那不是数字对不上，那是退一笔从来没付出去过的钱。
//
// 落的还是 payment_allocations 那张表（payment_id 留空），于是「这张采购单
// 付了多少」= sum(amount) WHERE po_id=? 这句话在三处地方一个字都不用改，
// 老的预付核销行和新的手填行天然一起求和。
//
// **这一页现在是供应商这边唯一的界面。** 供应商发票页、付款页、往来汇总页
// 都已下线（用户要的是「只留一个和客户对账类似的供应商对账」）。它们的服务
// 层实现还在原地——历史数据要读得出来、要冲得掉，而 buf 的破坏性检查也不
// 允许删 RPC——但没有任何界面再走那条路了。

// SupplierReconRow 是一张采购单在核销这件事上的当前样子。
type SupplierReconRow struct {
	POID         int64
	PONo         string
	SupplierID   int64
	SupplierName string
	Currency     string
	OrderStatus  string
	BuyerName    string
	OrderedDate  string
	ExpectedDate string
	// 应付到期日 = 下单当天 + 供应商账期。**空串表示没配账期**，不是
	// 「今天到期」——那种情况下 DueUnset 为真，OverdueDays 无意义。
	DueDate string
	// 正数已逾期，负数是还剩几天。DueUnset 为真时这个数没有意义。
	OverdueDays int32
	DueUnset    bool
	// 订单金额、已付净额、以及两者之差。差为负表示多付了。
	OrderedAmount string
	PaidAmount    string
	OpenAmount    string
	// PaidAmount 里有多少是走「发票 → 付款单核销」那条老路进来的（按发票行
	// 归属分摊到这张采购单）。单列出来是为了让已付这个数**解释得清**：
	// 员工在这一页只看得见自己手填的那些行，剩下的差额不说明来处，就会被
	// 当成漏记而重填一遍，同一笔钱在账上出现两次。
	InvoicePaidAmount string
	// 完成确认（只在已完成视图里非空）：为什么算完了、谁说的、什么时候。
	ClosedCategory string
	ClosedNote     string
	ClosedByName   string
	ClosedAt       string
}

// SupplierReconFilter 收窄清单。
type SupplierReconFilter struct {
	Keyword string
	// 两个互斥的筛子，都不给就是全部。只看逾期的；或者只看还没配账期的
	// （后者是催配置，不是催钱）。和客户侧同款。
	OverdueOnly bool
	UnsetOnly   bool
	// 只看确认完成了的。默认视图（false）是「还要人来处理的」。
	ClosedOnly bool
	Page       int32
	PageSize   int32
}

// POPaymentInput 是员工手填的一笔钱。
type POPaymentInput struct {
	POID int64
	// 界面上**永远填正数**，退款由 IsRefund 表达。翻号只发生在写库那一刻，
	// 于是所有守门都在正数域里比大小——负数一旦进到比较里，「不能超过」
	// 那类判断会静默失效。
	Amount   string
	IsRefund bool
	PaidAt   string // YYYY-MM-DD，钱哪天付的；空表示不记
	Note     string
}

// POPaymentEntry 是这张采购单上的一笔钱，时间序。
type POPaymentEntry struct {
	AllocationID int64
	PaidAt       string
	Amount       string
	// 只有改造前从付款单分配来的老行才可能非零。界面上要标「不计入已付」，
	// 否则明细加起来会比行上的已付多，页面不解释差在哪。
	FeeAmount   string
	Currency    string
	Note        string
	AllocatedAt string
	AllocatedBy string
	// 非零表示这是一条冲销行，指向它冲掉的那一笔。
	ReversalOf    int64
	ReverseReason string
	// 老行带着付款单号；手填行为空。这一列同时也是**冲销走哪条路**的依据。
	PaymentNo string
	// 非空表示这笔钱是核销在发票上的，只是那张发票有行指向本采购单。
	// 界面上要标出来——它算进了本单的已付，但它不是「付在这张单上」的钱，
	// 冲掉它会同时影响这张发票关联的其它采购单。
	InvoiceNo string
}

// poClosureCategories 和客户侧同一份白名单。SETTLED（正常付清）是最常见的
// 一档——既然完成与否完全由人确认，「付齐了，正常结案」就必须有位置放。
var poClosureCategories = map[string]bool{
	"SETTLED": true, "LOSS": true, "ROUNDING": true, "CANCELLED": true, "OTHER": true,
}

// maxMoney 是 NUMERIC(18,2) 装得下的上限（16 位整数部分）。挡在应用层，
// 是为了把「多打了几个零」翻成一句人话，而不是一个 22003 加 500。
var maxMoney = decimal.RequireFromString("9999999999999999.99")

// reconSelect 是清单和单行详情共用的那段 SELECT。两处共用一份，是因为记完
// 一笔钱之后界面要拿到「这一行现在长什么样」，那必须和列表算的是同一个数。
const reconSelect = `
	SELECT po.id, po.po_no, po.supplier_id, po.supplier_name, po.currency,
	       po.status, po.buyer_name,
	       coalesce(po.ordered_at::date::text, '')          AS ordered_date,
	       coalesce(po.expected_date::text, '')             AS expected_date,
	       coalesce(po.payable_due_date::text, '')          AS due_date,
	       -- 到期日为空时这个数没有意义，界面靠 due_unset 分流，不会去读它。
	       coalesce((current_date - po.payable_due_date), 0)::int AS overdue_days,
	       (po.payable_due_date IS NULL)::bool              AS due_unset,
	       po.total_amount::text                            AS ordered_amount,
	       (coalesce(p.paid, 0) + coalesce(ip.paid, 0))::text        AS paid_amount,
	       (po.total_amount - coalesce(p.paid, 0)
	                        - coalesce(ip.paid, 0))::text            AS open_amount,
	       coalesce(ip.paid, 0)::text                       AS invoice_paid_amount,
	       coalesce(cl.category, '')                        AS closed_category,
	       coalesce(cl.note, '')                            AS closed_note,
	       coalesce(cl.closed_by_name, '')                  AS closed_by_name,
	       coalesce(cl.created_at::text, '')                AS closed_at`

// reconFrom：已付是**算出来的**，不是存的状态——冲销是负行，求和天然反映
// 当下的真相。只求 amount 不含 fee_amount，和供应商往来汇总的预付列同口径。
//
// **已付要把两条路的钱都算进来。** payment_allocations 上有一条
// CHECK((invoice_id IS NOT NULL) <> (po_id IS NOT NULL))：挂发票的核销行
// po_id 必然为空。所以只按 po_id 求和的话，一张通过「录发票 → 付款单核销
// 到发票」付清的采购单，在这一页上会恒显示「一分未付」。发票页和付款页
// 现在虽然已经下线，但**历史数据全走这条路**，这条腿只会更重要不会更轻。
//
// 后果不只是数字难看：员工照着那个 0 再手填一遍，同一笔钱在账上就出现两次；
// 而「确认完成」会把那个错的差额永久快照进 closure 记录。所以 ip 这条腿是
// 必须的，不是锦上添花。
const reconFrom = `
	  FROM purchase_orders po
	  LEFT JOIN (
	      SELECT po_id, sum(amount) AS paid
	        FROM payment_allocations
	       WHERE tenant_id = $1 AND po_id IS NOT NULL
	       GROUP BY po_id
	  ) p ON p.po_id = po.id
	  -- 走发票那条路的钱，按发票行的采购单归属分摊回来。
	  --
	  -- 分摊而不是全额算给某一张单：一张发票可以跨几张采购单开（发票行各自
	  -- 带 po_id），把整笔核销算给其中一张会凭空多出钱。分母用发票**全部**
	  -- 行的合计，包含 po_id 为空的杂项行——那部分本来就不属于任何采购单，
	  -- 不该被摊进来。
	  LEFT JOIN (
	      SELECT l.po_id, round(sum(a.amount * l.po_amount / l.inv_total), 2) AS paid
	        FROM payment_allocations a
	        JOIN (
	            SELECT il.invoice_id, il.po_id, sum(il.amount) AS po_amount,
	                   (SELECT sum(x.amount) FROM supplier_invoice_lines x
	                     WHERE x.tenant_id = il.tenant_id
	                       AND x.invoice_id = il.invoice_id) AS inv_total
	              FROM supplier_invoice_lines il
	             WHERE il.tenant_id = $1 AND il.po_id IS NOT NULL
	             GROUP BY il.tenant_id, il.invoice_id, il.po_id
	        ) l ON l.invoice_id = a.invoice_id AND l.inv_total <> 0
	       WHERE a.tenant_id = $1 AND a.invoice_id IS NOT NULL
	       GROUP BY l.po_id
	  ) ip ON ip.po_id = po.id
	  LEFT JOIN purchase_order_payment_closures cl
	         ON cl.tenant_id = po.tenant_id AND cl.po_id = po.id
	        AND cl.revoked_at IS NULL`

// reconOrderScope 挑出「钱真的可能出去过」的单。DRAFT / PENDING_APPROVAL /
// REJECTED 是意向，从来没有钱离开过。
//
// CANCELLED 要分两种，因为**草稿也能取消**（CancelOrder 接受 DRAFT / ORDERED /
// REJECTED 三种前置状态）。分界线是 ordered_at：它在采购单真正下出去的那一刻
// 写一次，此后再没人清过它，取消也不清。
//
//	· ordered_at 非空 = 这张单真的下出去过 → 收进来，**哪怕一笔钱都还没录**。
//	  「订金付了、单取消了、厂里还没退」正是这时候要有人来了结它；如果按
//	  「已付非零」来筛，那笔还没录进系统的订金就永远找不到入口录——想记一笔
//	  钱，前提是那张单先出现在页面上。
//	· ordered_at 为空 = 取消掉的草稿，从来没有钱 → 不收，否则队列里全是
//	  没意义的行。
//
// 后面那句 paid <> 0 是兜底：万一有历史数据 ordered_at 是空的却挂着钱，
// 它也得有人来了结，不能因为一列元数据缺失就从账上消失。
const reconOrderScope = `
	   AND (po.status IN ` + committedOrders + `
	        OR (po.status = 'CANCELLED'
	            AND (po.ordered_at IS NOT NULL
	                 OR coalesce(p.paid, 0) <> 0 OR coalesce(ip.paid, 0) <> 0)))`

// ListSupplierRecon 列出采购订单，按有没有人确认完成分成两页。
//
// 数据范围和供应商往来汇总同一个立场：拿不到全量就明说，不悄悄缩水。
// 按人截断的核销清单是**静默错误**——页面打得开、表头和按钮都在、里面
// 少了一半的单，而且没有一行报错。财务不是任何一张采购单的 buyer，
// SELF 对它就等于零行。
func (s *Service) ListSupplierRecon(ctx context.Context, tenantID int64,
	f SupplierReconFilter, op Operator) ([]SupplierReconRow, int64, error) {
	if err := s.requireFullScope(ctx, op); err != nil {
		return nil, 0, err
	}
	page, size := normalizePage(f.Page, f.PageSize)
	rows, err := s.pool.Query(ctx, reconSelect+`,
	       count(*) OVER () AS total`+reconFrom+`
	 WHERE po.tenant_id = $1`+reconOrderScope+`
	   -- 两页：待核销 = 没有活着的确认；已完成 = 有。
	   -- **这里故意不看「还欠多少」。** 需求明说「不一定数字对不上就不能
	   -- 核销完成，也不一定数字一样就可以核销完成」——所以分界线只有
	   -- 「有没有人点过确认」这一条，金额一个字都不参与。
	   --
	   -- 同样注意 po.closed_at（货结案）不在这里出现：那一列说的是货收齐了
	   -- 或者短收关单了，是履约的生命周期。钱有没有付完是另一件事，一张
	   -- 货已结案、尾款还没付的单必须留在待核销页上。别把两者「改一致」。
	   AND (CASE WHEN $2::bool THEN cl.id IS NOT NULL ELSE cl.id IS NULL END)
	   AND ($3::text = ''
	        OR po.po_no ILIKE '%' || $3::text || '%'
	        OR po.supplier_name ILIKE '%' || $3::text || '%')
	   AND ($4::bool = false
	        OR (po.payable_due_date IS NOT NULL AND po.payable_due_date < current_date))
	   AND ($5::bool = false OR po.payable_due_date IS NULL)
	 -- 该付的排在前面，没配账期的垫底：它们缺的是配置，不是钱。
	 ORDER BY po.payable_due_date ASC NULLS LAST, po.id DESC
	 LIMIT $6::int OFFSET $7::int`,
		tenantID, f.ClosedOnly, strings.TrimSpace(f.Keyword),
		f.OverdueOnly, f.UnsetOnly, size, (page-1)*size)
	if err != nil {
		return nil, 0, err
	}
	defer rows.Close()
	out := make([]SupplierReconRow, 0, size)
	var total int64
	for rows.Next() {
		var r SupplierReconRow
		if err := rows.Scan(&r.POID, &r.PONo, &r.SupplierID, &r.SupplierName,
			&r.Currency, &r.OrderStatus, &r.BuyerName, &r.OrderedDate, &r.ExpectedDate,
			&r.DueDate, &r.OverdueDays, &r.DueUnset,
			&r.OrderedAmount, &r.PaidAmount, &r.OpenAmount, &r.InvoicePaidAmount,
			&r.ClosedCategory, &r.ClosedNote, &r.ClosedByName, &r.ClosedAt,
			&total); err != nil {
			return nil, 0, err
		}
		out = append(out, r)
	}
	return out, total, rows.Err()
}

// reconRowOf 读一行，给写操作返回「现在长什么样」。不带订单状态过滤：
// 记完钱之后那一行必须回得来，哪怕它刚好被过滤条件排除在当前页之外。
func (s *Service) reconRowOf(ctx context.Context, tenantID, poID int64) (SupplierReconRow, error) {
	var r SupplierReconRow
	err := s.pool.QueryRow(ctx, reconSelect+reconFrom+`
	 WHERE po.tenant_id = $1 AND po.id = $2`, tenantID, poID).
		Scan(&r.POID, &r.PONo, &r.SupplierID, &r.SupplierName,
			&r.Currency, &r.OrderStatus, &r.BuyerName, &r.OrderedDate, &r.ExpectedDate,
			&r.DueDate, &r.OverdueDays, &r.DueUnset,
			&r.OrderedAmount, &r.PaidAmount, &r.OpenAmount, &r.InvoicePaidAmount,
			&r.ClosedCategory, &r.ClosedNote, &r.ClosedByName, &r.ClosedAt)
	if err == pgx.ErrNoRows {
		return SupplierReconRow{}, apierr.NotFound("PR_POPAY_PO_NOT_FOUND", "采购单不存在")
	}
	return r, err
}

// RecordPOPayment 在一张采购单上手记一笔付款（或退款）。
//
// 有意**不设**「不能超过采购单金额」的上限：多付、汇路尾差、并笔付款都是
// 真事，而这次改造的原则就是数字由人负责、系统不替人判断对错。
func (s *Service) RecordPOPayment(ctx context.Context, tenantID int64,
	in POPaymentInput, op Operator) (SupplierReconRow, error) {
	if err := s.requireFullScope(ctx, op); err != nil {
		return SupplierReconRow{}, err
	}
	amount, err := decimal.NewFromString(strings.TrimSpace(in.Amount))
	if err != nil || !amount.IsPositive() {
		return SupplierReconRow{}, apierr.Invalid("PR_POPAY_AMOUNT_INVALID",
			"金额必须是大于零的数字——退款也填正数，选「退款」即可")
	}
	// 库里那一列是 NUMERIC(18,2)。不在这里挡住的话，两位以外的小数会被
	// **静默四舍五入**（填 0.006 存成 0.01，员工不知道自己填的数被改了），
	// 而 0.004 舍成 0.00 会撞 CHECK (amount <> 0)、超长的数会撞
	// numeric field overflow——两种都是一句驱动层英文加一个 500。
	if amount.Exponent() < -2 {
		return SupplierReconRow{}, apierr.Invalid("PR_POPAY_AMOUNT_PRECISION",
			"金额最多两位小数——多出来的位数会被四舍五入，那是在替你改数字")
	}
	if amount.GreaterThan(maxMoney) {
		return SupplierReconRow{}, apierr.Invalid("PR_POPAY_AMOUNT_TOO_LARGE",
			"金额超出账本能记的范围——请确认是不是多打了几个零")
	}
	paidAt := strings.TrimSpace(in.PaidAt)
	if paidAt != "" {
		if _, err := time.Parse("2006-01-02", paidAt); err != nil {
			return SupplierReconRow{}, apierr.Invalid("PR_POPAY_DATE_INVALID",
				"付款日期格式应为 YYYY-MM-DD")
		}
	}

	err = pgdb.InTx(ctx, s.pool, func(ctx context.Context, tx pgx.Tx) error {
		// FOR UPDATE：退款的天花板在行锁下读，两笔并发退款必须串行，
		// 否则各自都以为额度够。
		var currency string
		if err := tx.QueryRow(ctx, `
			SELECT currency FROM purchase_orders
			 WHERE tenant_id=$1 AND id=$2 FOR UPDATE`,
			tenantID, in.POID).Scan(&currency); err == pgx.ErrNoRows {
			return apierr.NotFound("PR_POPAY_PO_NOT_FOUND", "采购单不存在")
		} else if err != nil {
			return err
		}
		stored := amount
		if in.IsRefund {
			// 唯一的一道闸。求和包含老的预付核销行——「这张采购单上实际付
			// 出去过多少」不区分钱是从付款单分配的还是员工手填记进来的。
			var paidText string
			if err := tx.QueryRow(ctx, `
				SELECT coalesce(sum(amount),0)::text FROM payment_allocations
				 WHERE tenant_id=$1 AND po_id=$2`, tenantID, in.POID).Scan(&paidText); err != nil {
				return err
			}
			paid := decimal.RequireFromString(paidText)
			if amount.GreaterThan(paid) {
				return apierr.Invalid("PR_POPAY_REFUND_EXCEEDS_PAID",
					"退款 "+amount.String()+" 超过这张采购单的已付净额 "+paid.String()+
						"——退不出从来没付出去过的钱")
			}
			stored = amount.Neg()
		}
		// 币种跟采购单走，不让人填——问一个只有一个正确答案的问题只会制造
		// 错答案。payment_id 留空就是「手填的」。
		_, err := tx.Exec(ctx, `
			INSERT INTO payment_allocations
			  (tenant_id, payment_id, invoice_id, po_id, amount, fee_amount,
			   currency, paid_at, note, allocated_by, allocated_by_name)
			VALUES ($1, NULL, NULL, $2, $3::numeric, 0, $4,
			        nullif($5::text,'')::date, $6, $7, $8)`,
			tenantID, in.POID, stored.String(), currency,
			paidAt, strings.TrimSpace(in.Note), op.ID, op.Name)
		return err
	})
	if err != nil {
		return SupplierReconRow{}, err
	}
	s.nudge(ctx, tenantID)
	return s.reconRowOf(ctx, tenantID, in.POID)
}

// ListPOPayments 读这张采购单上的每一笔钱，时间序。
func (s *Service) ListPOPayments(ctx context.Context, tenantID, poID int64,
	op Operator) ([]POPaymentEntry, error) {
	if err := s.requireFullScope(ctx, op); err != nil {
		return nil, err
	}
	// 两类行都要列出来：
	//   · 挂在这张采购单上的（po_id = 本单）——手填的和从付款单分配的预付
	//   · 挂在发票上、而那张发票有行指向本单的（invoice_id 非空）
	//
	// **第二类不列出来就是个洞。** 行上的「已付」把发票那条路的钱按发票行
	// 分摊算了进来（见 reconFrom 的 ip 那条腿），明细里却一条都看不见——
	// 于是那笔钱既解释不清，也没有任何冲销入口：payment_allocations 上
	// CHECK((invoice_id IS NOT NULL) <> (po_id IS NOT NULL)) 保证它的 po_id
	// 是空的，而供应商付款页已经下线。错的金额会被永久焊死在已付里，还会被
	// 「确认完成」快照进 closure 记录。
	rows, err := s.pool.Query(ctx, `
		SELECT a.id,
		       coalesce(a.paid_at::text, ''),
		       a.amount::text, a.fee_amount::text, a.currency, a.note,
		       a.allocated_at::text, a.allocated_by_name,
		       coalesce(a.reversal_of, 0), a.reverse_reason,
		       -- LEFT JOIN：手填行没有付款单，INNER JOIN 会让它们整行消失。
		       coalesce(sp.payment_no, ''),
		       coalesce(si.invoice_no, '') AS invoice_no
		  FROM payment_allocations a
		  LEFT JOIN supplier_payments sp ON sp.id = a.payment_id
		  LEFT JOIN supplier_invoices si ON si.id = a.invoice_id
		 WHERE a.tenant_id = $1
		   AND (a.po_id = $2
		        OR (a.invoice_id IS NOT NULL AND EXISTS (
		              SELECT 1 FROM supplier_invoice_lines il
		               WHERE il.tenant_id = a.tenant_id
		                 AND il.invoice_id = a.invoice_id
		                 AND il.po_id = $2)))
		 ORDER BY a.allocated_at, a.id`, tenantID, poID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []POPaymentEntry
	for rows.Next() {
		var e POPaymentEntry
		if err := rows.Scan(&e.AllocationID, &e.PaidAt, &e.Amount, &e.FeeAmount,
			&e.Currency, &e.Note, &e.AllocatedAt, &e.AllocatedBy,
			&e.ReversalOf, &e.ReverseReason, &e.PaymentNo, &e.InvoiceNo); err != nil {
			return nil, err
		}
		out = append(out, e)
	}
	return out, rows.Err()
}

// ReversePOPayment 冲销一笔记错的付款：写一条相反的记录，不删原记录。
// 「记错了」和「从没发生过」是两件事，账上要看得出有人改过。
// reconPOID 是「冲完之后回哪一行」。挂发票的核销行本身不指向任何采购单，
// 所以这个数只能由调用方给——路由上带着它。
func (s *Service) ReversePOPayment(ctx context.Context, tenantID, reconPOID, allocID int64,
	reason string, op Operator) (SupplierReconRow, error) {
	if err := s.requireFullScope(ctx, op); err != nil {
		return SupplierReconRow{}, err
	}
	if strings.TrimSpace(reason) == "" {
		return SupplierReconRow{}, apierr.Invalid("PR_POPAY_REVERSE_REASON_REQUIRED",
			"请填写冲销原因——没有理由的冲销事后没人说得清")
	}
	var paymentID, poID, reversalOf int64
	var amountText, currency string
	err := s.pool.QueryRow(ctx, `
		SELECT coalesce(payment_id,0), coalesce(po_id,0), coalesce(reversal_of,0),
		       amount::text, currency
		  FROM payment_allocations WHERE tenant_id=$1 AND id=$2`,
		tenantID, allocID).Scan(&paymentID, &poID, &reversalOf, &amountText, &currency)
	if err == pgx.ErrNoRows {
		return SupplierReconRow{}, apierr.NotFound("PAY_ALLOC_NOT_FOUND", "核销记录不存在")
	}
	if err != nil {
		return SupplierReconRow{}, err
	}
	// 挂发票的行（po_id 为空）也从这里冲。
	//
	// **上一版这里是拒绝的**，让人去供应商付款页——那一页已经下线，于是这
	// 句话变成一个做不到的指令，而这类行的钱是算进本页「已付」的（reconFrom
	// 的 ip 那条腿）。结果是一笔记错的发票核销被永久焊死，还会被「确认完成」
	// 快照进记录。既然本页是唯一入口，它就得接这活。
	//
	// 冲完之后回哪一行：挂发票的行本身不指向任何采购单，所以用调用方传进来
	// 的 poID（路由上带着它）。
	if poID == 0 {
		if paymentID == 0 {
			// invoice_id 和 po_id 由 CHECK 保证恰有其一，两个都空说明数据
			// 坏了；说清楚比继续往下走强。
			return SupplierReconRow{}, apierr.Conflict("PR_POPAY_ORPHAN_ROW",
				"这笔核销既不挂采购单也不挂付款单——数据异常，请找管理员")
		}
		if _, err := s.ReverseSupplierPaymentAllocation(ctx, tenantID, allocID, reason, op); err != nil {
			return SupplierReconRow{}, err
		}
		return s.reconRowOf(ctx, tenantID, reconPOID)
	}

	// **挂着付款单的老行转交给老路。**
	//
	// 这张采购单的明细里同时住着两种行：手填行（payment_id 为空）和改造前
	// 从付款单分配出来的预付行。冲一条老行不只是写个负数——它还要动那张
	// 付款单的未分配余额，缺了这一步，钱在采购单上已经退回、在付款单上却
	// 还占着，两本账各说各话且不报错。ReverseSupplierPaymentAllocation
	// 连同 AuthorizeSupplierPayment 一起做了这些，所以原样转过去，而不是
	// 在这里复制一遍（复制必然漏，而且下次改只会改一边）。
	//
	// **上一版这里是明确拒绝的**，理由是「冲付款单的账归
	// procurement:payment:write 管，这道门只要 recon:write」，让人去供应商
	// 付款页操作。那条理由随着付款页下线一起作废了：现在整个系统里没有
	// 第二个入口，再拒绝就等于让所有历史 payment-backed 核销行变成谁也
	// 动不了的死行。职责分离在这里让位给「账必须能改对」。
	if paymentID != 0 {
		if _, err := s.ReverseSupplierPaymentAllocation(ctx, tenantID, allocID, reason, op); err != nil {
			return SupplierReconRow{}, err
		}
		return s.reconRowOf(ctx, tenantID, poID)
	}

	if reversalOf != 0 {
		return SupplierReconRow{}, apierr.Invalid("PR_POPAY_IS_REVERSAL",
			"这本身就是一条冲销记录")
	}
	err = pgdb.InTx(ctx, s.pool, func(ctx context.Context, tx pgx.Tx) error {
		var one int
		if err := tx.QueryRow(ctx, `
			SELECT 1 FROM purchase_orders WHERE tenant_id=$1 AND id=$2 FOR UPDATE`,
			tenantID, poID).Scan(&one); err != nil {
			return err
		}
		// 「已经冲销过」要先于净额闸判——重复冲销撞净额闸会报出「净额会
		// 变负」这种让人摸不着头脑的话。末尾 INSERT 上的唯一索引仍是并发
		// 时的最后一道兜底。
		var already bool
		if err := tx.QueryRow(ctx, `
			SELECT EXISTS(SELECT 1 FROM payment_allocations r
			  WHERE r.tenant_id=$1 AND r.reversal_of=$2)`,
			tenantID, allocID).Scan(&already); err != nil {
			return err
		}
		if already {
			return apierr.Conflict("PR_POPAY_ALREADY_REVERSED", "这笔记录已经冲销过了")
		}
		amt := decimal.RequireFromString(amountText).Neg()
		var netText string
		if err := tx.QueryRow(ctx, `
			SELECT coalesce(sum(amount),0)::text FROM payment_allocations
			 WHERE tenant_id=$1 AND po_id=$2`, tenantID, poID).Scan(&netText); err != nil {
			return err
		}
		if after := decimal.RequireFromString(netText).Add(amt); after.IsNegative() {
			return apierr.Conflict("PR_POPAY_REVERSE_REFUND_FIRST",
				"冲销后该采购单已付净额为 "+after.String()+"（负数）——"+
					"先冲销挂在这张单上的退款，再冲这笔")
		}
		_, err := tx.Exec(ctx, `
			INSERT INTO payment_allocations
			  (tenant_id, payment_id, invoice_id, po_id, amount, fee_amount,
			   currency, paid_at, reversal_of, reverse_reason,
			   allocated_by, allocated_by_name)
			SELECT $1, NULL, NULL, $2, $3::numeric, 0, $4, a.paid_at, $5, $6, $7, $8
			  FROM payment_allocations a WHERE a.tenant_id=$1 AND a.id=$5`,
			tenantID, poID, amt.String(), currency, allocID,
			strings.TrimSpace(reason), op.ID, op.Name)
		var pgErr *pgconn.PgError
		if errors.As(err, &pgErr) && pgErr.Code == "23505" {
			return apierr.Conflict("PR_POPAY_ALREADY_REVERSED", "这笔记录已经冲销过了")
		}
		return err
	})
	if err != nil {
		return SupplierReconRow{}, err
	}
	s.nudge(ctx, tenantID)
	return s.reconRowOf(ctx, tenantID, poID)
}

// ClosePOPayment 确认这张采购单的核销完成，差额快照进记录。
//
// 快照可以是负的：多付了、不追回、就这么了结，也是一种了结。这里对金额
// 不做任何判断——需求明说数字对不上也能完成、数字一样也不自动完成。
func (s *Service) ClosePOPayment(ctx context.Context, tenantID, poID int64,
	category, note string, op Operator) (SupplierReconRow, error) {
	if err := s.requireFullScope(ctx, op); err != nil {
		return SupplierReconRow{}, err
	}
	category = strings.ToUpper(strings.TrimSpace(category))
	if !poClosureCategories[category] {
		return SupplierReconRow{}, apierr.Invalid("PR_POCLOSE_CATEGORY_INVALID",
			"完成类别只能是正常付清、认亏、尾差、订单取消或其他")
	}
	row, err := s.reconRowOf(ctx, tenantID, poID)
	if err != nil {
		return SupplierReconRow{}, err
	}
	if _, err := s.pool.Exec(ctx, `
		INSERT INTO purchase_order_payment_closures
		  (tenant_id, po_id, open_amount, category, note, closed_by_id, closed_by_name)
		VALUES ($1, $2, $3::numeric, $4, $5, $6, $7)`,
		tenantID, poID, row.OpenAmount, category, strings.TrimSpace(note),
		op.ID, op.Name); err != nil {
		var pgErr *pgconn.PgError
		if errors.As(err, &pgErr) && pgErr.Code == "23505" {
			return SupplierReconRow{}, apierr.Conflict("PR_POCLOSE_TWICE",
				"这张采购单已经确认过完成了")
		}
		return SupplierReconRow{}, err
	}
	s.nudge(ctx, tenantID)
	return s.reconRowOf(ctx, tenantID, poID)
}

// ReopenPOPayment 撤销完成确认，采购单回到待核销页。
//
// 撤销不删行——「当初是谁按什么理由说完了的」永远要有答案。WHERE 里那句
// revoked_at IS NULL 本身就是并发控制：两个人同时撤，只有一个改到行。
func (s *Service) ReopenPOPayment(ctx context.Context, tenantID, poID int64,
	reason string, op Operator) (SupplierReconRow, error) {
	if err := s.requireFullScope(ctx, op); err != nil {
		return SupplierReconRow{}, err
	}
	if strings.TrimSpace(reason) == "" {
		return SupplierReconRow{}, apierr.Invalid("PR_POCLOSE_REASON_REQUIRED",
			"请填写撤销原因——没有理由的撤销事后没人说得清")
	}
	tag, err := s.pool.Exec(ctx, `
		UPDATE purchase_order_payment_closures
		   SET revoked_at = now(), revoked_by_id = $3, revoked_by_name = $4,
		       revoke_reason = $5
		 WHERE tenant_id = $1 AND po_id = $2 AND revoked_at IS NULL`,
		tenantID, poID, op.ID, op.Name, strings.TrimSpace(reason))
	if err != nil {
		return SupplierReconRow{}, err
	}
	if tag.RowsAffected() == 0 {
		return SupplierReconRow{}, apierr.NotFound("PR_POCLOSE_NONE",
			"这张采购单没有可撤销的完成确认")
	}
	s.nudge(ctx, tenantID)
	return s.reconRowOf(ctx, tenantID, poID)
}
