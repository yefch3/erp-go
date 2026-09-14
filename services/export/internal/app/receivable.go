package app

import (
	"context"
	"errors"
	"log/slog"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/shopspring/decimal"

	"github.com/sgao19/erp-go/pkg/apierr"
	"github.com/sgao19/erp-go/pkg/pgdb"

	"github.com/sgao19/erp-go/services/export/internal/store"
)

// 应收账款到期（E1）。
//
// 业务的原话是「120 天或 150 天账期，笔数太多，不同客户的订单叠在一块，
// 财务用 Excel 记不住哪笔什么时候到期」。系统本来就能算出每张合同还欠
// 多少（核销表求和），缺的一直是**什么时候该收**——全库找不到一个
// due_date。这一层把那个日子变成可查询、可排序、可催的东西。
//
// 口径由业务定：**账期从合同生效日起算**。收到定金/预付款/信用证之后
// 才去下采购单，所以合同生效本身就意味着首付到位，之后的账期算的是尾款。

// ReceivableRow 是催收清单上的一行：这张合同该收多少、什么时候该收、
// 逾期了几天。
type ReceivableRow struct {
	ContractID      int64
	ContractNo      string
	CustomerID      int64
	CustomerName    string
	SalesEmployeeID int64
	SalesEmployee   string
	// 空串表示客户主数据里没配账期——不是「今天到期」。
	DueDate        string
	EffectiveDate  string
	Currency       string
	TotalAmount    string
	ReceivedAmount string
	OpenAmount     string
	// 正数已逾期，负数是还剩几天；DueUnset 时无意义。
	OverdueDays int32
	DueUnset    bool
	// 收款结清（只在 ClosedOnly 视图里非空）：为什么不催了、谁定的。
	ClosedCategory                    string
	ClosedNote                        string
	ClosedByName                      string
	ClosedAt                          string
	ManuallyEntered                   bool
	ExecutionConditionStatus          string
	ExecutionConditionType            string
	ExecutionConditionConfirmedAt     string
	ExecutionConditionConfirmedByName string
	ExecutionConditionNote            string
}

// ReceivableFilter 收窄清单。两个开关互斥地各管一件事：只看逾期的，
// 或只看还没配账期的（后者是催配置，不是催钱）。
type ReceivableFilter struct {
	OverdueOnly bool
	UnsetOnly   bool
	Keyword     string
	// 只看结清了的。默认视图（false）是「该催的」，结清的不在里面——
	// 撤销结清要来这个视图找。
	ClosedOnly bool
}

// ListReceivableDue 返回还没收完的生效合同，按该收的日子排，逾期的在最前。
//
// 围栏沿用出口模块自己的数据范围（同合同列表）：应收是钱的事，谁能看见
// 哪张合同的欠款，和谁能看见哪张合同是同一个问题。
func (s *Service) ListReceivableDue(ctx context.Context, tenantID int64, f ReceivableFilter, page, size int32, op Operator) ([]ReceivableRow, int64, error) {
	visible, err := s.visibleTo(ctx, op)
	if err != nil {
		return nil, 0, err
	}
	if page < 1 {
		page = 1
	}
	if size < 1 || size > 200 {
		size = 50
	}
	rows, err := s.q.ListReceivableDue(ctx, store.ListReceivableDueParams{
		TenantID: tenantID, ScopeAll: visible.All, EmployeeIds: visible.EmployeeIDs,
		OverdueOnly: f.OverdueOnly, UnsetOnly: f.UnsetOnly, Keyword: f.Keyword,
		ClosedOnly: f.ClosedOnly,
		RowLimit:   size, RowOffset: (page - 1) * size,
	})
	if err != nil {
		return nil, 0, err
	}
	out := make([]ReceivableRow, 0, len(rows))
	var total int64
	for _, r := range rows {
		total = r.Total
		out = append(out, ReceivableRow{
			ContractID: r.ID, ContractNo: r.ContractNo,
			CustomerID: r.CustomerID, CustomerName: r.CustomerName,
			SalesEmployeeID: r.SalesEmployeeID, SalesEmployee: r.SalesEmployee,
			DueDate: r.DueDate, EffectiveDate: r.EffectiveDate,
			Currency: r.Currency, TotalAmount: r.TotalAmount,
			ReceivedAmount: r.ReceivedAmount, OpenAmount: r.OpenAmount,
			OverdueDays: r.OverdueDays, DueUnset: r.DueUnset,
			ClosedCategory: r.ClosedCategory, ClosedNote: r.ClosedNote,
			ClosedByName: r.ClosedByName, ClosedAt: r.ClosedAt,
			ManuallyEntered:                   r.CustomerID == 0,
			ExecutionConditionStatus:          r.ExecutionConditionStatus,
			ExecutionConditionType:            r.ExecutionConditionType,
			ExecutionConditionConfirmedAt:     r.ExecutionConditionConfirmedAt,
			ExecutionConditionConfirmedByName: r.ExecutionConditionConfirmedByName,
			ExecutionConditionNote:            r.ExecutionConditionNote,
		})
	}
	return out, total, nil
}

// UpdateManualReceivable corrects the identifying fields of a finance-only
// opening record. Money movements remain append-only and are corrected through
// reversal, so this operation cannot rewrite received amounts.
func (s *Service) UpdateManualReceivable(ctx context.Context, tenantID, contractID int64,
	customerName, contractNo, dueDate string, op Operator) (store.ContractReceiptProgressRow, error) {
	customerName = strings.TrimSpace(customerName)
	contractNo = strings.TrimSpace(contractNo)
	dueDate = strings.TrimSpace(dueDate)
	if customerName == "" || contractNo == "" {
		return store.ContractReceiptProgressRow{}, apierr.Invalid("EX_MANUAL_RECEIVABLE_REQUIRED", "请填写客户和合同号")
	}
	if err := validBusinessDate(dueDate, "EX_MANUAL_RECEIVABLE_DUE_DATE", "应收到期日"); err != nil {
		return store.ContractReceiptProgressRow{}, err
	}
	err := pgdb.InTx(ctx, s.pool, func(ctx context.Context, tx pgx.Tx) error {
		var versionID int64
		var source, oldCustomer, oldNo, oldDue string
		err := tx.QueryRow(ctx, `SELECT entry_source, current_version_id, customer_name,
			coalesce(nullif(external_contract_no,''), contract_no), coalesce(receivable_due_date::text,'')
			FROM contracts WHERE tenant_id=$1 AND id=$2 FOR UPDATE`, tenantID, contractID).
			Scan(&source, &versionID, &oldCustomer, &oldNo, &oldDue)
		if err == pgx.ErrNoRows {
			return apierr.NotFound("EX_CONTRACT_NOT_FOUND", "合同不存在")
		}
		if err != nil {
			return err
		}
		if source != "EXISTING_CONTRACT" {
			return apierr.Conflict("EX_MANUAL_RECEIVABLE_ONLY", "系统生成的合同请在出口合同中修改")
		}
		if oldCustomer == customerName && oldNo == contractNo && oldDue == dueDate {
			return nil
		}
		if _, err = tx.Exec(ctx, `SELECT set_config('erp.contract_correction','on',true)`); err != nil {
			return err
		}
		if _, err = tx.Exec(ctx, `UPDATE contracts SET customer_name=$3, external_contract_no=$4,
			receivable_due_date=nullif($5,'')::date, updated_by=$6, updated_at=now()
			WHERE tenant_id=$1 AND id=$2`, tenantID, contractID, customerName, contractNo, dueDate, op.ID); err != nil {
			return translateUnique(err, "EX_EXTERNAL_CONTRACT_NO_TAKEN", "该合同号已存在")
		}
		if _, err = tx.Exec(ctx, `UPDATE contract_versions SET buyer_name=$3 WHERE tenant_id=$1 AND id=$2`, tenantID, versionID, customerName); err != nil {
			return err
		}
		_, err = tx.Exec(ctx, `INSERT INTO contract_corrections
			(tenant_id,contract_id,contract_version_id,before_data,after_data,corrected_by,corrected_by_name)
			VALUES ($1,$2,$3,jsonb_build_object('customerName',$4,'contractNo',$5,'dueDate',$6),
			jsonb_build_object('customerName',$7,'contractNo',$8,'dueDate',$9),$10,$11)`,
			tenantID, contractID, versionID, oldCustomer, oldNo, oldDue, customerName, contractNo, dueDate, op.ID, op.Name)
		if err != nil {
			return err
		}
		_, err = tx.Exec(ctx, `UPDATE receivable_reminders SET read_at=now()
			WHERE tenant_id=$1 AND contract_id=$2 AND read_at IS NULL`, tenantID, contractID)
		return err
	})
	if err != nil {
		return store.ContractReceiptProgressRow{}, err
	}
	return s.q.ContractReceiptProgress(ctx, store.ContractReceiptProgressParams{TenantID: tenantID, ContractID: contractID})
}

// ── 记一笔收款 ─────────────────────────────────────────────────────
//
// 员工在待核销页上选一张合同，把金额和到账日期手填进去。**一根线都不连
// 银行流水**——那本账在采购库里，只用来存银行给的 statement。
//
// 落的还是 receipt_allocations 那张表（transaction_id 留空），于是「这张
// 合同收了多少」= sum(amount + fee_amount) 这句话在六处地方一个字都不用改，
// 老行和新行天然一起求和。

// ContractReceiptInput 是员工手填的一笔钱。
type ContractReceiptInput struct {
	ContractID int64
	// 界面上**永远填正数**，退款由 IsRefund 表达。翻号只发生在写库那一刻，
	// 于是所有守门都在正数域里比大小——负数一旦进到比较里，「不能超过」
	// 那类判断会静默失效。
	Amount     string
	IsRefund   bool
	ReceivedAt string // YYYY-MM-DD，钱哪天到的；空表示不记
	Note       string
}

// ManualReceivableInput is a historical balance entered directly by Finance.
// It deliberately skips contract approval and operational events: this is an
// opening money record, not a new sale to procure or ship.
type ManualReceivableInput struct {
	CustomerName, ContractNo, Currency string
	TotalAmount, ReceivedAmount        string
	ReceivedAt, DueDate, Note          string
}

var manualReceivableMaxMoney = decimal.RequireFromString("9999999999999999.99")

func (s *Service) CreateManualReceivable(ctx context.Context, tenantID int64, in ManualReceivableInput, op Operator) (store.ContractReceiptProgressRow, error) {
	in.CustomerName = strings.TrimSpace(in.CustomerName)
	in.ContractNo = strings.TrimSpace(in.ContractNo)
	in.Currency = strings.ToUpper(strings.TrimSpace(in.Currency))
	if in.CustomerName == "" || in.ContractNo == "" || in.Currency == "" {
		return store.ContractReceiptProgressRow{}, apierr.Invalid("EX_MANUAL_RECEIVABLE_REQUIRED", "请填写客户、合同号和币种")
	}
	total, err := decimal.NewFromString(strings.TrimSpace(in.TotalAmount))
	if err != nil || !total.IsPositive() || total.GreaterThan(manualReceivableMaxMoney) {
		return store.ContractReceiptProgressRow{}, apierr.Invalid("EX_MANUAL_RECEIVABLE_TOTAL", "合同金额必须大于 0")
	}
	if total.Exponent() < -2 {
		return store.ContractReceiptProgressRow{}, apierr.Invalid("EX_MANUAL_RECEIVABLE_TOTAL_PRECISION", "合同金额最多两位小数")
	}
	received := decimal.Zero
	if strings.TrimSpace(in.ReceivedAmount) != "" {
		received, err = decimal.NewFromString(strings.TrimSpace(in.ReceivedAmount))
		if err != nil || received.IsNegative() || received.GreaterThan(manualReceivableMaxMoney) {
			return store.ContractReceiptProgressRow{}, apierr.Invalid("EX_MANUAL_RECEIVABLE_RECEIVED", "已收金额不能小于 0")
		}
		if received.Exponent() < -2 {
			return store.ContractReceiptProgressRow{}, apierr.Invalid("EX_MANUAL_RECEIVABLE_RECEIVED_PRECISION", "已收金额最多两位小数")
		}
	}
	if received.GreaterThan(total) {
		return store.ContractReceiptProgressRow{}, apierr.Invalid("EX_MANUAL_RECEIVABLE_EXCEEDS", "已收金额不能超过合同金额")
	}
	if err := validBusinessDate(in.ReceivedAt, "EX_MANUAL_RECEIVABLE_DATE", "收款日期"); err != nil {
		return store.ContractReceiptProgressRow{}, err
	}
	if err := validBusinessDate(in.DueDate, "EX_MANUAL_RECEIVABLE_DUE_DATE", "应收到期日"); err != nil {
		return store.ContractReceiptProgressRow{}, err
	}
	rate, err := s.rates.Latest(ctx, in.Currency)
	if err != nil {
		return store.ContractReceiptProgressRow{}, err
	}
	internalNo, err := s.number.Next(ctx, "CONTRACT")
	if err != nil {
		return store.ContractReceiptProgressRow{}, err
	}
	var contractID int64
	err = pgdb.InTx(ctx, s.pool, func(ctx context.Context, tx pgx.Tx) error {
		q := s.q.WithTx(tx)
		err := tx.QueryRow(ctx, `INSERT INTO contracts
			(tenant_id, contract_no, external_contract_no, entry_source, customer_id, customer_name,
			 status, sales_employee_id, sales_employee, opening_received_amount, signed_at, effective_at, receivable_due_date,
			 signature_source, created_by, updated_by)
			VALUES ($1,$2,$3,'EXISTING_CONTRACT',0,$4,'EXECUTING',$5,$6,0,now(),now(),nullif($7::text,'')::date,'MANUAL',$5,$5)
			RETURNING id`, tenantID, internalNo, in.ContractNo, in.CustomerName, op.ID, op.Name, strings.TrimSpace(in.DueDate)).Scan(&contractID)
		if err != nil {
			return translateUnique(err, "EX_EXTERNAL_CONTRACT_NO_TAKEN", "该合同号已存在")
		}
		versionID, err := q.CreateContractVersion(ctx, store.CreateContractVersionParams{
			TenantID: tenantID, ContractID: contractID, VersionNo: 1,
			BuyerName: in.CustomerName, SellerName: s.seller.Name, SellerAddress: s.seller.Address,
			Currency: in.Currency, Incoterm: "", TotalAmount: total.StringFixed(2),
			BaseAmount: baseAmount(total, rate).StringFixed(2), FxRate: rate.Rate.String(),
			FxRateAt: tsFrom(rate.At), FxSource: rate.Source, FxBaseCurrency: rate.Base,
			CreatedBy: op.ID,
		})
		if err != nil {
			return err
		}
		if err := q.SetContractVersionStatus(ctx, store.SetContractVersionStatusParams{TenantID: tenantID, ID: versionID, NewStatus: "APPROVED"}); err != nil {
			return err
		}
		if err := q.FinalizeExistingContract(ctx, store.FinalizeExistingContractParams{TenantID: tenantID, ID: contractID, VersionID: versionID, UpdatedBy: op.ID}); err != nil {
			return err
		}
		if received.IsZero() {
			return nil
		}
		_, err = q.AddReceiptAllocation(ctx, store.AddReceiptAllocationParams{
			TenantID: tenantID, ContractID: contractID, ContractNo: in.ContractNo,
			CustomerName: in.CustomerName, Amount: received.StringFixed(2), FeeAmount: "0",
			FeeCategory: "OTHER", Currency: in.Currency, AllocatedBy: op.ID,
			AllocatedByName: op.Name, ReceivedAt: strings.TrimSpace(in.ReceivedAt), Note: strings.TrimSpace(in.Note),
		})
		return err
	})
	if err != nil {
		return store.ContractReceiptProgressRow{}, err
	}
	return s.q.ContractReceiptProgress(ctx, store.ContractReceiptProgressParams{TenantID: tenantID, ContractID: contractID})
}

// RecordContractReceipt 记一笔收款（或退款）到一张合同上。
//
// 有意**不设**「不能超过合同金额」那种上限：多收、汇路尾差、并笔付款都是
// 真事，而这次改造的原则就是数字由人负责、系统不替人判断对错。唯一保留的
// 一道是退款不能超过这张合同的已收净额——那不是「数字对不上」，那是退一笔
// 从来没收到过的钱，物理上不成立。
func (s *Service) RecordContractReceipt(ctx context.Context, tenantID int64, in ContractReceiptInput, op Operator) (store.ContractReceiptProgressRow, error) {
	amount, err := decimal.NewFromString(strings.TrimSpace(in.Amount))
	if err != nil || !amount.IsPositive() {
		return store.ContractReceiptProgressRow{}, apierr.Invalid("EX_RECEIPT_AMOUNT_INVALID",
			"金额必须是大于零的数字——退款也填正数，选「退款」即可")
	}
	receivedAt := strings.TrimSpace(in.ReceivedAt)
	if receivedAt != "" {
		if _, err := time.Parse("2006-01-02", receivedAt); err != nil {
			return store.ContractReceiptProgressRow{}, apierr.Invalid("EX_RECEIPT_DATE_INVALID",
				"到账日期格式应为 YYYY-MM-DD")
		}
	}

	progress, err := s.q.ContractReceiptProgress(ctx, store.ContractReceiptProgressParams{
		TenantID: tenantID, ContractID: in.ContractID,
	})
	if err == pgx.ErrNoRows {
		return store.ContractReceiptProgressRow{}, apierr.NotFound("EX_CONTRACT_NOT_FOUND", "合同不存在")
	}
	if err != nil {
		return store.ContractReceiptProgressRow{}, err
	}
	// 币种跟合同走，不让人填——问一个只有一个正确答案的问题只会制造错答案。
	if progress.Currency == "" {
		return store.ContractReceiptProgressRow{}, apierr.Invalid("EX_CONTRACT_NO_VERSION",
			"这张合同还没有生效版本，先让合同生效再记收款")
	}
	stored := amount
	if in.IsRefund {
		received := decimal.RequireFromString(progress.ReceivedAmount)
		if amount.GreaterThan(received) {
			return store.ContractReceiptProgressRow{}, apierr.Invalid("EX_REFUND_EXCEEDS_RECEIVED",
				"退款 "+amount.String()+" 超过这张合同的已收净额 "+received.String()+
					"——退不出从来没收到过的钱")
		}
		stored = amount.Neg()
	}

	if _, err := s.q.AddReceiptAllocation(ctx, store.AddReceiptAllocationParams{
		TenantID: tenantID,
		// 0 = 不挂银行流水。新模型下这里永远是 0。
		TransactionID: 0,
		ContractID:    in.ContractID,
		ContractNo:    progress.ContractNo,
		CustomerName:  progress.CustomerName,
		Amount:        stored.String(),
		FeeAmount:     "0",
		// 手工记账没有「手续费」这个概念了——员工填的就是到账的那个数，
		// 一个数说完。fee_amount 恒为 0，所以这一档只是为了过 CHECK
		// （那一列不收空串），语义上不参与任何计算。
		FeeCategory: "OTHER",
		Currency:    progress.Currency,
		ReversalOf:  0,
		AllocatedBy: op.ID, AllocatedByName: op.Name,
		ReceivedAt: receivedAt,
		Note:       strings.TrimSpace(in.Note),
	}); err != nil {
		return store.ContractReceiptProgressRow{}, err
	}
	return s.q.ContractReceiptProgress(ctx, store.ContractReceiptProgressParams{
		TenantID: tenantID, ContractID: in.ContractID,
	})
}

// ReverseContractReceipt 冲销一笔记错的收款：写一条相反的记录，不删原记录。
//
// 和核销侧同一条纪律——「记错了」和「从没发生过」是两件事，账上要看得出
// 有人改过。
func (s *Service) ReverseContractReceipt(ctx context.Context, tenantID, entryID int64, reason string, op Operator) (store.ContractReceiptProgressRow, error) {
	if strings.TrimSpace(reason) == "" {
		return store.ContractReceiptProgressRow{}, apierr.Invalid("EX_REVERSE_REASON_REQUIRED",
			"请填写冲销原因——没有理由的冲销事后没人说得清")
	}
	orig, err := s.q.GetAllocation(ctx, store.GetAllocationParams{TenantID: tenantID, ID: entryID})
	if err == pgx.ErrNoRows {
		return store.ContractReceiptProgressRow{}, apierr.NotFound("EX_ALLOC_NOT_FOUND", "这笔记录不存在")
	}
	if err != nil {
		return store.ContractReceiptProgressRow{}, err
	}
	if orig.ReversalOf != 0 {
		return store.ContractReceiptProgressRow{}, apierr.Invalid("EX_ALLOC_IS_REVERSAL",
			"这本身就是一条冲销记录")
	}
	// **挂着银行流水的老行必须走老路。**
	//
	// 这张合同的明细里同时住着两种行：新的手工记账（transaction_id 为空）和
	// F2 之前从银行流水核销出来的老行。界面上它们长得一样、冲销按钮也一样，
	// 但老行的冲销要多做三件事，缺一件都会留下查不出来的烂账：
	//
	//	· 拿那一行流水的建议锁（否则和并发的核销抢同一行）
	//	· 撞上「认差结清」要拒绝（结清记的是「认下那一刻的余额」，
	//	  事后把钱冲走会让那句话和事实同时成立又互相矛盾）
	//	· 把重算后的认领量报回采购的账本，否则那一行的 claimed_amount
	//	  停在旧值，永远回不到收款对账的「待处理」队列——钱在出口这边
	//	  已经空出来，在账本上却还认领着，全程没有一行报错
	//
	// 这三件事 ReverseAllocation 都做了，所以这里原样转过去，而不是在这
	// 复制一遍（复制必然漏，而且下次改只会改一边）。
	if orig.TransactionID != 0 {
		if _, err := s.ReverseAllocation(ctx, tenantID, entryID, reason, op); err != nil {
			return store.ContractReceiptProgressRow{}, err
		}
		return s.q.ContractReceiptProgress(ctx, store.ContractReceiptProgressParams{
			TenantID: tenantID, ContractID: orig.ContractID,
		})
	}
	reversed, err := s.q.AllocationReversed(ctx, store.AllocationReversedParams{
		TenantID: tenantID, AllocationID: entryID,
	})
	if err != nil {
		return store.ContractReceiptProgressRow{}, err
	}
	if reversed {
		return store.ContractReceiptProgressRow{}, apierr.Invalid("EX_ALLOC_ALREADY_REVERSED",
			"这笔记录已经冲销过了")
	}
	if _, err := s.q.AddReceiptAllocation(ctx, store.AddReceiptAllocationParams{
		TenantID: tenantID, TransactionID: orig.TransactionID,
		ContractID: orig.ContractID, ContractNo: orig.ContractNo,
		CustomerName: orig.CustomerName,
		// 镜像负行。fee_category 跟着原行走，否则按类别求和会对不上
		// （而且空串过不了 CHECK）。
		Amount:      decimal.RequireFromString(orig.Amount).Neg().String(),
		FeeAmount:   decimal.RequireFromString(orig.FeeAmount).Neg().String(),
		FeeCategory: orig.FeeCategory,
		Currency:    orig.Currency,
		ReversalOf:  entryID, ReverseReason: strings.TrimSpace(reason),
		AllocatedBy: op.ID, AllocatedByName: op.Name,
		ReceivedAt: "", Note: "",
	}); err != nil {
		var pgErr interface{ SQLState() string }
		if errors.As(err, &pgErr) && pgErr.SQLState() == "23505" {
			return store.ContractReceiptProgressRow{}, apierr.Conflict("EX_ALLOC_ALREADY_REVERSED",
				"这笔记录已经冲销过了")
		}
		return store.ContractReceiptProgressRow{}, err
	}
	return s.q.ContractReceiptProgress(ctx, store.ContractReceiptProgressParams{
		TenantID: tenantID, ContractID: orig.ContractID,
	})
}

// ── 确认核销完成 ───────────────────────────────────────────────────
//
// 这张表原本叫「收款结清」，管的是「算式说还欠、人说不欠了」时停掉催收。
// 新模型下它升格成**待核销 / 已完成两页的分界线**：有一条活着的记录就是
// 已完成，没有就还在待核销。
//
// 升格之所以成立，是因为它当初就是照「完成与数字无关」设计的——差额只存
// 快照、不做校验，正负都收。所以这里三条规矩一条都不用新增：
//
//	· 差额是正是负都能确认完成（多收着不退也是一种了结）
//	· 数字对上了**不会**自动确认完成，必须有人点
//	· 确认完成之后照样能继续记收款，钱真的又来了就撤销完成
var receivableClosureCategories = map[string]bool{
	// 新模型下最常见的一档：收齐了，正常结案。老的四档全是「有差额」的
	// 理由，因为老模型里收满是算式自动消失、根本走不到这张表。
	"SETTLED": true,
	"LOSS":    true, "ROUNDING": true, "CANCELLED": true, "OTHER": true,
}

// CloseReceivable 确认这张合同的收款核销完成，差额快照进记录。
func (s *Service) CloseReceivable(ctx context.Context, tenantID, contractID int64, category, note string, op Operator) error {
	category = strings.ToUpper(strings.TrimSpace(category))
	if !receivableClosureCategories[category] {
		return apierr.Invalid("EX_RCLOSE_CATEGORY_INVALID",
			"完成类别只能是正常收完、损耗、尾差、合同取消或其他")
	}
	// 差额取快照用的是和清单同一套算法（在途版本 + 核销求和）。
	progress, err := s.q.ContractReceiptProgress(ctx, store.ContractReceiptProgressParams{
		TenantID: tenantID, ContractID: contractID,
	})
	if err == pgx.ErrNoRows {
		return apierr.NotFound("EX_CONTRACT_NOT_FOUND", "合同不存在")
	}
	if err != nil {
		return err
	}
	if _, err := s.q.InsertReceivableClosure(ctx, store.InsertReceivableClosureParams{
		TenantID: tenantID, ContractID: contractID,
		OpenAmount: progress.OpenAmount, Category: category,
		Note:       strings.TrimSpace(note),
		ClosedByID: op.ID, ClosedByName: op.Name,
	}); err != nil {
		var pgErr interface{ SQLState() string }
		if errors.As(err, &pgErr) && pgErr.SQLState() == "23505" {
			return apierr.Conflict("EX_RCLOSE_TWICE", "这张合同已经结清过了")
		}
		return err
	}
	return nil
}

// ReopenReceivable 撤销结清，合同回到催收清单（如果还有未收）。
func (s *Service) ReopenReceivable(ctx context.Context, tenantID, contractID int64, reason string, op Operator) error {
	if strings.TrimSpace(reason) == "" {
		return apierr.Invalid("EX_RCLOSE_REASON_REQUIRED",
			"请填写撤销原因——没有理由的撤销事后没人说得清")
	}
	n, err := s.q.RevokeReceivableClosure(ctx, store.RevokeReceivableClosureParams{
		TenantID: tenantID, ContractID: contractID,
		RevokedByID: op.ID, RevokedByName: op.Name,
		RevokeReason: strings.TrimSpace(reason),
	})
	if err != nil {
		return err
	}
	if n == 0 {
		return apierr.NotFound("EX_RCLOSE_NONE", "这张合同没有可撤销的结清")
	}
	return nil
}

// ── 提醒（E1 第二期）───────────────────────────────────────────────
//
// 清单页解决了「记不住」，提醒解决的是「不用记得去打开」。
//
// 一条提醒就是一条站内信，收件人是合同的销售负责人。四个时机压在一条
// SQL 里判断（到期前 30 天进入视野、7 天内、当天、逾期后每 7 天一轮），
// 幂等交给唯一键——所以这个 worker 可以随便多跑，停几天再起来也能补齐。

// ReceivableReminder 是收件箱里的一条。
type ReceivableReminder struct {
	ID           int64
	ContractID   int64
	ContractNo   string
	CustomerName string
	// SOON / DUE / OVERDUE
	Type       string
	PeriodNo   int32
	DueDate    string
	OpenAmount string
	Currency   string
	Title      string
	Content    string
	DetailURL  string
	CreatedAt  string
	Unread     bool
}

// SweepReceivableReminders 扫一趟，把该提醒而未提醒的写成站内信，返回新增条数。
//
// 跨租户：worker 没有租户上下文，而新开一个租户不该需要额外配置才被覆盖。
// 每一行写回的仍是那张合同自己的 tenant_id。
func (s *Service) SweepReceivableReminders(ctx context.Context) (int64, error) {
	return s.q.SweepReceivableReminders(ctx)
}

// ReceivableInbox 是某人的应收提醒收件箱：未读在前。
func (s *Service) ReceivableInbox(ctx context.Context, tenantID, employeeID int64, unreadOnly bool, limit int32) ([]ReceivableReminder, int64, error) {
	if employeeID == 0 {
		return nil, 0, apierr.Unauthorized("AUTH_TOKEN_MISSING", "缺少登录凭证")
	}
	if limit <= 0 || limit > 100 {
		limit = 50
	}
	rows, err := s.q.ListReceivableReminders(ctx, store.ListReceivableRemindersParams{
		TenantID: tenantID, EmployeeID: employeeID, UnreadOnly: unreadOnly, RowLimit: limit,
	})
	if err != nil {
		return nil, 0, err
	}
	out := make([]ReceivableReminder, 0, len(rows))
	var unread int64
	for _, r := range rows {
		unread = r.UnreadTotal
		out = append(out, ReceivableReminder{
			ID: r.ID, ContractID: r.ContractID, ContractNo: r.ContractNo,
			CustomerName: r.CustomerName, Type: r.ReminderType, PeriodNo: r.PeriodNo,
			DueDate: r.DueDate, OpenAmount: r.OpenAmount, Currency: r.Currency,
			Title: r.Title, Content: r.Content, DetailURL: r.DetailUrl,
			CreatedAt: r.CreatedAt.Time.Format(time.RFC3339), Unread: r.Unread,
		})
	}
	return out, unread, nil
}

// MarkReceivableRemindersRead 标记已读；ids 为空表示全部已读。
func (s *Service) MarkReceivableRemindersRead(ctx context.Context, tenantID, employeeID int64, ids []int64) (int64, error) {
	if employeeID == 0 {
		return 0, apierr.Unauthorized("AUTH_TOKEN_MISSING", "缺少登录凭证")
	}
	if ids == nil {
		ids = []int64{}
	}
	return s.q.MarkReceivableRemindersRead(ctx, store.MarkReceivableRemindersReadParams{
		TenantID: tenantID, EmployeeID: employeeID, Ids: ids,
	})
}

// RunReceivableReminderWorker 每天扫一趟。
//
// 每天一次就够：到期日是「天」这个粒度的事，一天之内多扫几遍不会多出
// 任何东西（唯一键挡着），少扫一遍也不会漏（范围判断而非等号）。
//
// 启动时先跑一趟，因为服务重启后第一件该做的事就是补上停机期间的提醒。
func (s *Service) RunReceivableReminderWorker(ctx context.Context, interval time.Duration, log *slog.Logger) {
	if interval <= 0 {
		interval = 24 * time.Hour
	}
	run := func() {
		n, err := s.SweepReceivableReminders(ctx)
		if err != nil {
			log.Error("应收提醒扫描失败，下一轮重试", "err", err)
			return
		}
		if n > 0 {
			log.Info("应收提醒已发出", "count", n)
		}
	}
	run()
	ticker := time.NewTicker(interval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			run()
		}
	}
}
