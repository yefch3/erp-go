// Package app holds the inventory use cases. It owns the only numbers in the
// system that answer "is there any, and can I promise it to somebody".
package app

import (
	"context"
	"log/slog"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/shopspring/decimal"

	"github.com/sgao19/erp-go/pkg/apierr"
	"github.com/sgao19/erp-go/pkg/pgdb"
	"github.com/sgao19/erp-go/services/inventory/internal/store"
)

type Operator struct {
	ID   int64
	Name string
}

// Numbering issues document numbers. masterdata owns every number in the
// system, so an outbound asks for one rather than inventing a format of its
// own that would drift from the rest.
type Numbering interface {
	Next(ctx context.Context, bizType string) (string, error)
}

type Service struct {
	pool      *pgxpool.Pool
	q         *store.Queries
	numbering Numbering
}

func New(pool *pgxpool.Pool, numbering Numbering) *Service {
	return &Service{pool: pool, q: store.New(pool), numbering: numbering}
}

func (s *Service) ListWarehouses(ctx context.Context, tenantID int64, includeInactive bool) ([]store.ListWarehousesRow, error) {
	rows, err := s.q.ListWarehouses(ctx, store.ListWarehousesParams{
		TenantID: tenantID, IncludeInactive: includeInactive,
	})
	if err != nil || len(rows) > 0 {
		return rows, err
	}
	// 空 ≠ 该空：也可能只是这家公司还没被播过种。见 warehouseseed.go——补种
	// 放在这里而不是开户时，是为了让不囤货的贸易公司永远不会凭空多出一个
	// 它根本没有的仓库。
	if err := s.seedDefaultWarehouses(ctx, tenantID); err != nil {
		return nil, err
	}
	return s.q.ListWarehouses(ctx, store.ListWarehousesParams{
		TenantID: tenantID, IncludeInactive: includeInactive,
	})
}

type StockFilter struct {
	WarehouseID int64
	Keyword     string
	InStockOnly bool
}

func (s *Service) ListStocks(ctx context.Context, tenantID int64, f StockFilter, page, size int32) ([]store.ListStocksRow, int64, error) {
	page, size = normalizePage(page, size)
	rows, err := s.q.ListStocks(ctx, store.ListStocksParams{
		TenantID: tenantID, WarehouseID: f.WarehouseID, Keyword: f.Keyword,
		InStockOnly: f.InStockOnly, RowLimit: size, RowOffset: (page - 1) * size,
	})
	if err != nil {
		return nil, 0, err
	}
	var total int64
	if len(rows) > 0 {
		total = rows[0].Total
	}
	return rows, total, nil
}

type LedgerFilter struct {
	SkuID    int64
	Movement string
}

func (s *Service) ListLedger(ctx context.Context, tenantID int64, f LedgerFilter, page, size int32) ([]store.ListLedgerRow, int64, error) {
	page, size = normalizePage(page, size)
	rows, err := s.q.ListLedger(ctx, store.ListLedgerParams{
		TenantID: tenantID, SkuID: f.SkuID, Movement: f.Movement,
		RowLimit: size, RowOffset: (page - 1) * size,
	})
	if err != nil {
		return nil, 0, err
	}
	var total int64
	if len(rows) > 0 {
		total = rows[0].Total
	}
	return rows, total, nil
}

// ReceiveLine is one SKU arriving.
type ReceiveLine struct {
	ProductID   int64
	SkuID       int64
	ProductCode string
	ProductName string
	UomID       int64
	UomCode     string
	Qty         string
	// What a unit cost. Empty means "do not move the average" — the receipt
	// comes in at whatever the row already carries, which is the right
	// behaviour for a stock count correction and the wrong one for a purchase.
	// Purchases always carry a price, so this only stays empty by choice.
	UnitCost string
}

// costOf prices one receipt line. A line with no price given is booked at the
// average the row already carries, so a correction cannot silently drag the
// valuation towards zero — receiving 10,000 units at "no cost" would halve
// the average of everything already on the shelf.
func costOf(unitCost string, qty decimal.Decimal, current string) (unit, amount decimal.Decimal, err error) {
	if unitCost == "" {
		unit, err = decimal.NewFromString(orZeroCost(current))
	} else {
		unit, err = decimal.NewFromString(unitCost)
	}
	if err != nil {
		return decimal.Zero, decimal.Zero, apierr.Invalid("IV_COST_INVALID", "单价格式不正确")
	}
	if unit.IsNegative() {
		return decimal.Zero, decimal.Zero, apierr.Invalid("IV_COST_NEGATIVE", "单价不能为负数")
	}
	return unit, unit.Mul(qty), nil
}

func orZeroCost(v string) string {
	if v == "" {
		return "0"
	}
	return v
}

// ReceiveStock puts goods into a warehouse and writes the ledger entry that
// explains them. Manual for now; purchase receipts will call the same path
// once they exist, which is why the reference fields are already there.
func (s *Service) ReceiveStock(ctx context.Context, tenantID, warehouseID int64, lines []ReceiveLine, remark string, op Operator) error {
	if warehouseID == 0 {
		return apierr.Invalid("IV_WAREHOUSE_REQUIRED", "请选择仓库")
	}
	if len(lines) == 0 {
		return apierr.Invalid("IV_LINES_REQUIRED", "入库明细不能为空")
	}
	for i, l := range lines {
		qty, err := decimal.NewFromString(l.Qty)
		if err != nil || qty.LessThanOrEqual(decimal.Zero) {
			return apierr.Invalid("IV_QTY_INVALID", "入库数量必须大于 0").
				WithMeta("line", decimal.NewFromInt(int64(i+1)).String())
		}
		// Product is required; SKU is not. A product without variants has no
		// SKU row at all, and demanding one here would make half the catalogue
		// impossible to receive.
		if l.ProductID == 0 {
			return apierr.Invalid("IV_PRODUCT_REQUIRED", "入库明细必须指定产品").
				WithMeta("line", decimal.NewFromInt(int64(i+1)).String())
		}
	}

	return pgdb.InTx(ctx, s.pool, func(ctx context.Context, tx pgx.Tx) error {
		q := s.q.WithTx(tx)
		for _, l := range lines {
			qty, err := decimal.NewFromString(l.Qty)
			if err != nil {
				return err
			}
			unit, amount, err := s.priceLine(ctx, q, tenantID, warehouseID, l, qty)
			if err != nil {
				return err
			}
			row, err := q.UpsertStockOnInbound(ctx, store.UpsertStockOnInboundParams{
				TenantID: tenantID, WarehouseID: warehouseID,
				ProductID: l.ProductID, SkuID: l.SkuID, UomID: l.UomID, UomCode: l.UomCode,
				ProductCode: l.ProductCode, ProductName: l.ProductName, Qty: l.Qty,
				Amount: amount.String(), CostCurrency: baseCurrency,
			})
			if err != nil {
				return err
			}
			if err := q.AppendLedger(ctx, store.AppendLedgerParams{
				TenantID: tenantID, StockID: row.ID, WarehouseID: warehouseID,
				SkuID: l.SkuID, Movement: "INBOUND", Qty: l.Qty,
				RefType: "MANUAL", RefID: 0, RefNo: "",
				OnHandAfter: row.OnHandQty, AvailableAfter: row.AvailableQty,
				OperatorID: op.ID, Remark: remark,
				UnitCost: unit.String(), Amount: amount.String(), AvgCostAfter: row.AvgCost,
			}); err != nil {
				return err
			}
			if err := s.fillWaiting(ctx, tx, q, tenantID, l.ProductID, l.SkuID, 0); err != nil {
				return err
			}
		}
		return nil
	})
}

// fillWaiting hands newly arrived goods to the contracts that were already
// short of them, soonest delivery first.
//
// Without this the model quietly lies. "A contract that takes effect claims
// stock immediately" only holds if stock arriving late is claimed too;
// otherwise goods bought specifically to cover a shortage sit marked
// available, a later contract takes them, and the one that paid for the
// purchase order is still short.
//
// It also closes the loop procurement started: topping up shrinks the
// shortage, the shrunken shortage is re-announced, and the requirement that
// caused the purchase shrinks or closes on its own.
//
// skipAnnounce names a contract whose caller will publish its own allocation
// afterwards, so it does not go out twice.
func (s *Service) fillWaiting(ctx context.Context, tx pgx.Tx, q *store.Queries, tenantID, productID, skuID, skipAnnounce int64) error {
	waiting, err := q.WaitingReservationsForItem(ctx, store.WaitingReservationsForItemParams{
		TenantID: tenantID, ProductID: productID, SkuID: skuID,
	})
	if err != nil || len(waiting) == 0 {
		return err
	}
	touched := make(map[int64]bool, len(waiting))
	for _, r := range waiting {
		short, err := decimal.NewFromString(r.ShortageQty)
		if err != nil || short.LessThanOrEqual(decimal.Zero) {
			continue
		}
		got, err := s.takeStock(ctx, q, tenantID, takeRequest{
			ReservationID: r.ID, ProductID: r.ProductID, SkuID: r.SkuID,
			Want: short, RefID: r.RefID, RefNo: r.RefNo,
			Remark: "到货后补足预留",
		})
		if err != nil {
			return err
		}
		if got.GreaterThan(decimal.Zero) {
			touched[r.RefID] = true
		}
		// Nothing left to hand out; the rest stay short and stay on the
		// purchase list.
		if got.LessThan(short) {
			break
		}
	}
	for contractID := range touched {
		if contractID == skipAnnounce {
			continue
		}
		if _, _, err := s.announceAllocation(ctx, tx, q, tenantID, contractID); err != nil {
			return err
		}
	}
	return nil
}

func normalizePage(page, size int32) (int32, int32) {
	if page < 1 {
		page = 1
	}
	if size < 1 {
		size = 20
	}
	if size > 200 {
		size = 200
	}
	return page, size
}

// PurchaseReceived is the slice of procurement's event this service acts on.
type PurchaseReceived struct {
	POID        int64          `json:"po_id"`
	PONo        string         `json:"po_no"`
	ReceiptNo   string         `json:"receipt_no"`
	WarehouseID int64          `json:"warehouse_id"`
	OperatorID  int64          `json:"operator_id"`
	Lines       []PurchaseLine `json:"lines"`
}

type PurchaseLine struct {
	ProductID   int64  `json:"product_id"`
	SkuID       int64  `json:"sku_id"`
	ProductCode string `json:"product_code"`
	ProductName string `json:"product_name"`
	UomID       int64  `json:"uom_id"`
	UomCode     string `json:"uom_code"`
	Qty         string `json:"qty"`
	UnitCost    string `json:"unit_cost"`
	Currency    string `json:"currency"`
}

// ReceivePurchase books a delivery into stock.
//
// It is deliberately the same path as a manual receipt, with a different
// reference on the ledger row. Two code paths for "goods arrived" would
// eventually disagree about which one hands stock to waiting contracts, and
// the one that forgot would silently leave a purchase order's whole reason
// for existing unfulfilled.
func (s *Service) ReceivePurchase(ctx context.Context, tenantID int64, e PurchaseReceived, log *slog.Logger) error {
	warehouseID := e.WarehouseID
	if warehouseID == 0 {
		// Procurement should always name one, but a delivery that cannot be
		// put anywhere is worse than one put in the default warehouse.
		row, err := s.q.ListWarehouses(ctx, store.ListWarehousesParams{TenantID: tenantID})
		if err != nil {
			return err
		}
		if len(row) == 0 {
			return apierr.Invalid("IV_NO_WAREHOUSE", "没有可用仓库，无法收货")
		}
		warehouseID = row[0].ID
		log.Warn("purchase receipt without a warehouse, using the first one",
			"receipt_no", e.ReceiptNo, "warehouse_id", warehouseID)
	}

	filled := 0
	err := pgdb.InTx(ctx, s.pool, func(ctx context.Context, tx pgx.Tx) error {
		q := s.q.WithTx(tx)
		filled = 0
		for _, l := range e.Lines {
			qty, err := decimal.NewFromString(l.Qty)
			if err != nil || qty.LessThanOrEqual(decimal.Zero) {
				log.Warn("purchase line with unusable quantity, skipped",
					"receipt_no", e.ReceiptNo, "product", l.ProductID, "qty", l.Qty)
				continue
			}
			// A purchase in a currency the cost pool does not hold cannot be
			// averaged in. The line is booked at the existing average rather
			// than failing the whole delivery: the goods really did arrive,
			// and refusing to record them over a valuation question would
			// leave the warehouse unable to book real stock.
			if l.Currency != "" && l.Currency != baseCurrency {
				log.Error("purchase line in an unsupported currency, booked at the current average",
					"receipt_no", e.ReceiptNo, "product", l.ProductCode,
					"currency", l.Currency, "base", baseCurrency)
				l.UnitCost = ""
			}
			unit, amount, err := s.priceLine(ctx, q, tenantID, warehouseID, ReceiveLine{
				ProductID: l.ProductID, SkuID: l.SkuID, UnitCost: l.UnitCost,
			}, qty)
			if err != nil {
				return err
			}
			row, err := q.UpsertStockOnInbound(ctx, store.UpsertStockOnInboundParams{
				TenantID: tenantID, WarehouseID: warehouseID,
				ProductID: l.ProductID, SkuID: l.SkuID, UomID: l.UomID, UomCode: l.UomCode,
				ProductCode: l.ProductCode, ProductName: l.ProductName, Qty: qty.String(),
				Amount: amount.String(), CostCurrency: baseCurrency,
			})
			if err != nil {
				return err
			}
			if err := q.AppendLedger(ctx, store.AppendLedgerParams{
				TenantID: tenantID, StockID: row.ID, WarehouseID: warehouseID,
				SkuID: l.SkuID, Movement: "INBOUND", Qty: qty.String(),
				RefType: "PURCHASE", RefID: e.POID, RefNo: e.PONo,
				OnHandAfter: row.OnHandQty, AvailableAfter: row.AvailableQty,
				OperatorID: e.OperatorID, Remark: "采购到货 " + e.ReceiptNo,
				UnitCost: unit.String(), Amount: amount.String(), AvgCostAfter: row.AvgCost,
			}); err != nil {
				return err
			}
			if err := s.fillWaiting(ctx, tx, q, tenantID, l.ProductID, l.SkuID, 0); err != nil {
				return err
			}
			filled++
		}
		return nil
	})
	if err != nil {
		return err
	}
	log.Info("purchase receipt booked into stock",
		"po_no", e.PONo, "receipt_no", e.ReceiptNo, "lines", filled)
	return nil
}

// baseCurrency is the currency stock is valued in.
//
// Hardcoded, and that is a known limit rather than an oversight: a single
// cost pool can only hold one currency, and converting a foreign-currency
// purchase at the receipt-date rate needs the fx service, which this service
// does not talk to yet. A receipt in another currency is refused loudly
// rather than averaged in to produce a number that means nothing.
const baseCurrency = "CNY"

// priceLine works out what one arriving line is worth, and refuses to mix
// currencies into a pool that already holds another.
func (s *Service) priceLine(ctx context.Context, q *store.Queries, tenantID, warehouseID int64, l ReceiveLine, qty decimal.Decimal) (decimal.Decimal, decimal.Decimal, error) {
	current := ""
	existing, err := q.FindStock(ctx, store.FindStockParams{
		TenantID: tenantID, WarehouseID: warehouseID,
		ProductID: l.ProductID, SkuID: l.SkuID,
	})
	if err == nil {
		if existing.CostCurrency != baseCurrency {
			return decimal.Zero, decimal.Zero, apierr.Invalid("IV_COST_CURRENCY_MISMATCH",
				"该库存的成本币种是 "+existing.CostCurrency+"，暂不支持多币种成本").
				WithMeta("stock_currency", existing.CostCurrency).
				WithMeta("receipt_currency", baseCurrency)
		}
		current = existing.AvgCost
	} else if err != pgx.ErrNoRows {
		return decimal.Zero, decimal.Zero, err
	}
	return costOf(l.UnitCost, qty, current)
}
