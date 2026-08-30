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
// **这条路和供应商付款页那条路是两条路，不是一条路的两种走法。**
// 付款页回答的是「一笔电汇怎么拆到几张发票上」，它那 8 道金额守门原样留着；
// 这里回答的是「这张采购单的账人认不认完」。两者共用一张核销表、各有各的
// 入口和规矩。

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
	// 订单金额、已付净额、以及两者之差。差为负表示多付了。
	OrderedAmount string
	PaidAmount    string
	OpenAmount    string
	// 完成确认（只在已完成视图里非空）：为什么算完了、谁说的、什么时候。
	ClosedCategory string
	ClosedNote     string
	ClosedByName   string
	ClosedAt       string
}

// SupplierReconFilter 收窄清单。
type SupplierReconFilter struct {
	Keyword string
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
}

// poClosureCategories 和客户侧同一份白名单。SETTLED（正常付清）是最常见的
// 一档——既然完成与否完全由人确认，「付齐了，正常结案」就必须有位置放。
var poClosureCategories = map[string]bool{
	"SETTLED": true, "LOSS": true, "ROUNDING": true, "CANCELLED": true, "OTHER": true,
}

// reconSelect 是清单和单行详情共用的那段 SELECT。两处共用一份，是因为记完
// 一笔钱之后界面要拿到「这一行现在长什么样」，那必须和列表算的是同一个数。
const reconSelect = `
	SELECT po.id, po.po_no, po.supplier_id, po.supplier_name, po.currency,
	       po.status, po.buyer_name,
	       coalesce(po.ordered_at::date::text, '')          AS ordered_date,
	       coalesce(po.expected_date::text, '')             AS expected_date,
	       po.total_amount::text                            AS ordered_amount,
	       coalesce(p.paid, 0)::text                        AS paid_amount,
	       (po.total_amount - coalesce(p.paid, 0))::text    AS open_amount,
	       coalesce(cl.category, '')                        AS closed_category,
	       coalesce(cl.note, '')                            AS closed_note,
	       coalesce(cl.closed_by_name, '')                  AS closed_by_name,
	       coalesce(cl.created_at::text, '')                AS closed_at`

// reconFrom：已付是**算出来的**，不是存的状态——冲销是负行，求和天然反映
// 当下的真相。只求 amount 不含 fee_amount，和供应商往来汇总的预付列同口径。
const reconFrom = `
	  FROM purchase_orders po
	  LEFT JOIN (
	      SELECT po_id, sum(amount) AS paid
	        FROM payment_allocations
	       WHERE tenant_id = $1 AND po_id IS NOT NULL
	       GROUP BY po_id
	  ) p ON p.po_id = po.id
	  LEFT JOIN purchase_order_payment_closures cl
	         ON cl.tenant_id = po.tenant_id AND cl.po_id = po.id
	        AND cl.revoked_at IS NULL`

// reconOrderScope 挑出「钱真的可能出去过」的单。DRAFT / PENDING_APPROVAL /
// REJECTED 是意向，从来没有钱离开过；CANCELLED 只有在已经付过钱的时候才需要
// 有人来了结它（退款、或者认下这笔损失）。
const reconOrderScope = `
	   AND (po.status IN ` + committedOrders + `
	        OR (po.status = 'CANCELLED' AND coalesce(p.paid, 0) <> 0))`

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
	 ORDER BY po.ordered_at DESC NULLS LAST, po.id DESC
	 LIMIT $4::int OFFSET $5::int`,
		tenantID, f.ClosedOnly, strings.TrimSpace(f.Keyword), size, (page-1)*size)
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
			&r.OrderedAmount, &r.PaidAmount, &r.OpenAmount,
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
			&r.OrderedAmount, &r.PaidAmount, &r.OpenAmount,
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
	rows, err := s.pool.Query(ctx, `
		SELECT a.id,
		       coalesce(a.paid_at::text, ''),
		       a.amount::text, a.fee_amount::text, a.currency, a.note,
		       a.allocated_at::text, a.allocated_by_name,
		       coalesce(a.reversal_of, 0), a.reverse_reason,
		       -- LEFT JOIN：手填行没有付款单，INNER JOIN 会让它们整行消失。
		       coalesce(sp.payment_no, '')
		  FROM payment_allocations a
		  LEFT JOIN supplier_payments sp ON sp.id = a.payment_id
		 WHERE a.tenant_id = $1 AND a.po_id = $2
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
			&e.ReversalOf, &e.ReverseReason, &e.PaymentNo); err != nil {
			return nil, err
		}
		out = append(out, e)
	}
	return out, rows.Err()
}

// ReversePOPayment 冲销一笔记错的付款：写一条相反的记录，不删原记录。
// 「记错了」和「从没发生过」是两件事，账上要看得出有人改过。
func (s *Service) ReversePOPayment(ctx context.Context, tenantID, allocID int64,
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
	if poID == 0 {
		return SupplierReconRow{}, apierr.Invalid("PR_POPAY_NOT_ON_ORDER",
			"这笔核销挂在发票上，不在采购单上——请到供应商付款页冲销")
	}

	// **挂着付款单的老行必须走老路。**
	//
	// 这张采购单的明细里同时住着两种行：手填行（payment_id 为空）和改造前
	// 从付款单分配出来的预付行。界面上它们长得一样、冲销按钮也一样，但老行
	// 的冲销要多做两件事，缺一件都会留下查不出来的烂账：
	//
	//	· AuthorizeSupplierPayment——付款单有它自己的可见范围
	//	· 冲完之后付款单的未分配余额要跟着弹回去，否则那笔钱在采购单上
	//	  已经退回、在付款单上却还占着，两本账各说各话且不报错
	//
	// 这两件 ReverseSupplierPaymentAllocation 都做了，所以原样转过去，而不是
	// 在这里复制一遍（复制必然漏，而且下次改只会改一边）。
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
