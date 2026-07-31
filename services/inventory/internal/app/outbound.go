package app

import (
	"context"
	"encoding/json"
	"strconv"

	"github.com/jackc/pgx/v5"
	"github.com/shopspring/decimal"

	"github.com/sgao19/erp-go/pkg/apierr"
	"github.com/sgao19/erp-go/pkg/outbox"
	"github.com/sgao19/erp-go/pkg/pgdb"
	"github.com/sgao19/erp-go/services/inventory/internal/store"
)

// The only status this service branches on. CONFIRMED and CANCELLED are set
// by their own statements and never tested for, so they stay in the SQL.
const outboundDraft = "DRAFT"

// OutboundLine is one contract line about to be picked.
type OutboundLine struct {
	RefLineID int64
	Qty       string
}

type CreateOutboundInput struct {
	ContractID   int64
	OutboundType string
	Remark       string
	Lines        []OutboundLine
}

// stockOutboundEvent tells the rest of the system that goods physically left.
// Export will use it to fill in shipped quantities; nothing consumes it yet,
// and publishing before there is a reader is deliberate — the alternative is
// discovering months later that the shipments of the intervening period were
// never recorded anywhere but here.
type stockOutboundEvent struct {
	OutboundNo string              `json:"outbound_no"`
	ContractID int64               `json:"contract_id"`
	ContractNo string              `json:"contract_no"`
	Lines      []outboundEventLine `json:"lines"`
}

type outboundEventLine struct {
	ContractItemID int64  `json:"contract_item_id"`
	ProductID      int64  `json:"product_id"`
	SkuID          int64  `json:"sku_id"`
	Qty            string `json:"qty"`
}

// CreateOutbound turns part of a contract into a picking list.
//
// This is where "缺口没补上就出不了库" stops being advice. A line may only draw
// on what stock is actually holding for it — reserved, minus whatever earlier
// picking lists already claimed. Ask for more and the request is refused with
// the missing quantity named, because that number is the one somebody has to
// go and buy.
//
// Before refusing, it tries a top-up: goods that arrived since the contract
// took effect are not automatically attached to the contract that was waiting
// for them, so the moment somebody tries to ship is the right moment to look
// again. A successful top-up shrinks the shortage, which is re-announced so
// the purchase requirement it raised can shrink or close with it.
//
// Creating the list converts reservation into lock — layer 1 to layer 2. The
// goods were already off the market; now they are also off the shelf, and
// availability does not move, because counting the same crate twice is the
// one mistake this model exists to prevent.
func (s *Service) CreateOutbound(ctx context.Context, tenantID int64, in CreateOutboundInput, op Operator) (store.CreateOutboundRow, error) {
	if in.ContractID == 0 {
		return store.CreateOutboundRow{}, apierr.Invalid("IV_CONTRACT_REQUIRED", "请选择要出库的合同")
	}
	if len(in.Lines) == 0 {
		return store.CreateOutboundRow{}, apierr.Invalid("IV_LINES_REQUIRED", "出库明细不能为空")
	}
	wanted := make(map[int64]decimal.Decimal, len(in.Lines))
	order := make([]int64, 0, len(in.Lines))
	for _, l := range in.Lines {
		qty, err := decimal.NewFromString(l.Qty)
		if err != nil || qty.LessThanOrEqual(decimal.Zero) {
			return store.CreateOutboundRow{}, apierr.Invalid("IV_QTY_INVALID", "出库数量必须大于 0")
		}
		if _, dup := wanted[l.RefLineID]; dup {
			return store.CreateOutboundRow{}, apierr.Invalid("IV_LINE_DUPLICATED", "同一合同明细在一张出库单里只能出现一次")
		}
		wanted[l.RefLineID] = qty
		order = append(order, l.RefLineID)
	}
	if in.OutboundType == "" {
		in.OutboundType = "SALES"
	}

	var head store.CreateOutboundRow
	toppedUp := false
	err := pgdb.InTx(ctx, s.pool, func(ctx context.Context, tx pgx.Tx) error {
		q := s.q.WithTx(tx)
		toppedUp = false

		type picked struct {
			res store.ReservationForUpdateRow
			qty decimal.Decimal
		}
		lines := make([]picked, 0, len(order))
		var contractNo, customer string

		for _, refLineID := range order {
			want := wanted[refLineID]
			res, err := q.ReservationForUpdate(ctx, store.ReservationForUpdateParams{
				TenantID: tenantID, RefType: RefTypeContract,
				RefID: in.ContractID, RefLineID: refLineID,
			})
			if err == pgx.ErrNoRows {
				// No reservation means the contract never took effect, or this
				// line is not part of it. Either way there is nothing to ship.
				return apierr.Invalid("IV_NOT_RESERVED",
					"该明细没有预留库存，无法出库（合同尚未生效？）")
			}
			if err != nil {
				return err
			}
			contractNo, customer = res.RefNo, res.CustomerName

			demand, err := decimal.NewFromString(res.DemandQty)
			if err != nil {
				return err
			}
			shipped, err := decimal.NewFromString(res.ShippedQty)
			if err != nil {
				return err
			}
			locked, err := decimal.NewFromString(res.LockedQty)
			if err != nil {
				return err
			}
			reserved, err := decimal.NewFromString(res.ReservedQty)
			if err != nil {
				return err
			}
			// Never more than was sold, whatever the warehouse holds.
			if want.GreaterThan(demand.Sub(shipped).Sub(locked)) {
				return apierr.Invalid("IV_EXCEEDS_CONTRACT",
					"出库数量超过合同该明细的剩余数量").
					WithMeta("product", res.ProductName).
					WithMeta("remaining", demand.Sub(shipped).Sub(locked).String())
			}

			ready := reserved.Sub(locked).Sub(shipped)
			if want.GreaterThan(ready) {
				// Goods may be sitting free that this line is owed — stock
				// that predates the contract, or a top-up the receiving path
				// missed. Claim the whole remaining shortage, not just enough
				// for this shipment: leaving the rest available would let a
				// later contract take goods this one is still owed.
				got, err := s.takeStock(ctx, q, tenantID, takeRequest{
					ReservationID: res.ID, ProductID: res.ProductID, SkuID: res.SkuID,
					Want:   demand.Sub(reserved),
					RefID:  in.ContractID,
					RefNo:  res.RefNo,
					Remark: "出库前补足预留",
				})
				if err != nil {
					return err
				}
				if got.GreaterThan(decimal.Zero) {
					toppedUp = true
					ready = ready.Add(got)
				}
			}
			if want.GreaterThan(ready) {
				// The refusal that gives the purchase requirement its teeth.
				// The numbers go in the message as well as the metadata: this
				// is the one error in the flow where "how many are missing" is
				// the entire content, and it should survive being read in a
				// log line or a toast that shows nothing else.
				missing := want.Sub(ready)
				return apierr.Invalid("IV_STOCK_SHORT",
					"库存不足，无法出库："+res.ProductName+
						" 需出 "+trimQty(want)+"，可出 "+trimQty(ready)+
						"，缺 "+trimQty(missing)+"，请先完成采购入库").
					WithMeta("product", res.ProductName).
					WithMeta("requested", want.String()).
					WithMeta("ready", ready.String()).
					WithMeta("missing", missing.String())
			}
			lines = append(lines, picked{res: res, qty: want})
		}

		// The number is drawn only once every line has passed, and from
		// masterdata, which owns every document number in the system. Asking
		// earlier would burn a number on each refusal — and refusals are the
		// normal case here, so the outbound series would jump by ten between
		// consecutive shipments and look like lost paperwork.
		no, err := s.numbering.Next(ctx, "OUTBOUND")
		if err != nil {
			return err
		}
		head, err = q.CreateOutbound(ctx, store.CreateOutboundParams{
			TenantID: tenantID, OutboundNo: no, OutboundType: in.OutboundType,
			RefType: RefTypeContract, RefID: in.ContractID, RefNo: contractNo,
			CustomerName: customer, OperatorID: op.ID, OperatorName: op.Name,
			Remark: in.Remark,
		})
		if err != nil {
			return err
		}

		for _, p := range lines {
			itemID, err := q.CreateOutboundItem(ctx, store.CreateOutboundItemParams{
				TenantID: tenantID, OutboundID: head.ID, ReservationID: p.res.ID,
				RefLineID: p.res.RefLineID, ProductID: p.res.ProductID, SkuID: p.res.SkuID,
				ProductCode: p.res.ProductCode, ProductName: p.res.ProductName,
				UomCode: p.res.UomCode, Qty: p.qty.String(),
			})
			if err != nil {
				return err
			}
			if err := s.lockForItem(ctx, q, tenantID, head, p.res, itemID, p.qty); err != nil {
				return err
			}
		}
		if toppedUp {
			_, _, err := s.announceAllocation(ctx, tx, q, tenantID, in.ContractID)
			return err
		}
		return nil
	})
	if err != nil {
		return store.CreateOutboundRow{}, err
	}
	return head, nil
}

// lockForItem moves one picked line's quantity from reserved to locked on the
// warehouse rows the reservation originally drew it from, and records where
// each piece came from so confirming and cancelling can be exact.
func (s *Service) lockForItem(ctx context.Context, q *store.Queries, tenantID int64,
	head store.CreateOutboundRow, res store.ReservationForUpdateRow, itemID int64, qty decimal.Decimal) error {
	rows, err := q.ReservationLinesForUpdate(ctx, store.ReservationLinesForUpdateParams{
		TenantID: tenantID, ReservationID: res.ID,
	})
	if err != nil {
		return err
	}
	left := qty
	for _, row := range rows {
		if left.LessThanOrEqual(decimal.Zero) {
			break
		}
		remaining, err := decimal.NewFromString(row.RemainingQty)
		if err != nil {
			return err
		}
		take := decimal.Min(remaining, left)
		if take.LessThanOrEqual(decimal.Zero) {
			continue
		}
		after, err := q.ConvertReserveToLock(ctx, store.ConvertReserveToLockParams{
			TenantID: tenantID, ID: row.StockID, Qty: take.String(),
		})
		if err != nil {
			return err
		}
		if err := q.CommitReservationLine(ctx, store.CommitReservationLineParams{
			TenantID: tenantID, ID: row.ID, Qty: take.String(),
		}); err != nil {
			return err
		}
		if err := q.CreateOutboundItemStock(ctx, store.CreateOutboundItemStockParams{
			TenantID: tenantID, OutboundItemID: itemID, ReservationLineID: row.ID,
			StockID: row.StockID, WarehouseID: row.WarehouseID, Qty: take.String(),
		}); err != nil {
			return err
		}
		if err := q.AppendLedger(ctx, store.AppendLedgerParams{
			TenantID: tenantID, StockID: row.StockID, WarehouseID: row.WarehouseID,
			SkuID: res.SkuID, Movement: "CONVERT_RESERVE_TO_LOCK", Qty: take.String(),
			RefType: "OUTBOUND", RefID: head.ID, RefNo: head.OutboundNo,
			OnHandAfter: after.OnHandQty, AvailableAfter: after.AvailableQty,
			Remark: "生成出库单，预留转锁定",
			// Locking moves no value either; the goods are still ours.
			UnitCost: "0", Amount: "0", AvgCostAfter: after.AvgCost,
		}); err != nil {
			return err
		}
		left = left.Sub(take)
	}
	if left.GreaterThan(decimal.Zero) {
		// The reservation counters said there was enough but the per-warehouse
		// rows disagree. That is corruption, not a business outcome, and the
		// transaction has to die rather than ship a number nobody can back.
		return apierr.Internal("IV_RESERVATION_INCONSISTENT",
			"预留明细与预留总量不一致，出库已中止")
	}
	return q.LockReservationQty(ctx, store.LockReservationQtyParams{
		TenantID: tenantID, ID: res.ID, Qty: qty.String(),
	})
}

// ConfirmOutbound is the moment the goods leave. on_hand and locked fall
// together, so availability does not change: what just shipped had not been
// available since the day the contract was signed.
func (s *Service) ConfirmOutbound(ctx context.Context, tenantID, outboundID int64, op Operator) error {
	return pgdb.InTx(ctx, s.pool, func(ctx context.Context, tx pgx.Tx) error {
		q := s.q.WithTx(tx)
		head, err := q.GetOutboundForUpdate(ctx, store.GetOutboundForUpdateParams{
			TenantID: tenantID, ID: outboundID,
		})
		if err == pgx.ErrNoRows {
			return apierr.NotFound("IV_OUTBOUND_NOT_FOUND", "出库单不存在")
		}
		if err != nil {
			return err
		}
		if head.Status != outboundDraft {
			return apierr.Invalid("IV_OUTBOUND_NOT_DRAFT", "只有待出库的单据可以确认出库").
				WithMeta("status", head.Status)
		}

		items, err := q.OutboundItemsOf(ctx, store.OutboundItemsOfParams{
			TenantID: tenantID, OutboundID: head.ID,
		})
		if err != nil {
			return err
		}
		eventLines := make([]outboundEventLine, 0, len(items))
		for _, item := range items {
			picks, err := q.OutboundItemStocksOf(ctx, store.OutboundItemStocksOfParams{
				TenantID: tenantID, OutboundItemID: item.ID,
			})
			if err != nil {
				return err
			}
			for _, p := range picks {
				after, err := q.ConsumeLockOnOutbound(ctx, store.ConsumeLockOnOutboundParams{
					TenantID: tenantID, ID: p.StockID, Qty: p.Qty,
				})
				if err != nil {
					return err
				}
				if err := q.AppendLedger(ctx, store.AppendLedgerParams{
					TenantID: tenantID, StockID: p.StockID, WarehouseID: p.WarehouseID,
					SkuID: item.SkuID, Movement: "OUTBOUND", Qty: p.Qty,
					RefType: "OUTBOUND", RefID: head.ID, RefNo: head.OutboundNo,
					OnHandAfter: after.OnHandQty, AvailableAfter: after.AvailableQty,
					OperatorID: op.ID, Remark: "确认出库",
					// The one movement that takes value out — and what it
					// takes out is the cost of goods sold.
					UnitCost: after.UnitCost, Amount: after.Amount,
					AvgCostAfter: after.AvgCostAfter,
				}); err != nil {
					return err
				}
			}
			if _, err := q.ShipReservationQty(ctx, store.ShipReservationQtyParams{
				TenantID: tenantID, ID: item.ReservationID, Qty: item.Qty,
			}); err != nil {
				return err
			}
			eventLines = append(eventLines, outboundEventLine{
				ContractItemID: item.RefLineID, ProductID: item.ProductID,
				SkuID: item.SkuID, Qty: item.Qty,
			})
		}

		if err := q.SetOutboundConfirmed(ctx, store.SetOutboundConfirmedParams{
			TenantID: tenantID, ID: head.ID,
		}); err != nil {
			return err
		}
		payload, err := json.Marshal(stockOutboundEvent{
			OutboundNo: head.OutboundNo, ContractID: head.RefID,
			ContractNo: head.RefNo, Lines: eventLines,
		})
		if err != nil {
			return err
		}
		return outbox.Append(ctx, tx, outbox.Event{
			TenantID: tenantID, AggregateType: "outbound",
			AggregateID: strconv.FormatInt(head.ID, 10),
			EventType:   "StockOutbound", Payload: payload,
		})
	})
}

// CancelOutbound puts a picking list back. Only a draft: once the goods have
// left, undoing it is a return, which is a different act with different
// paperwork and is not modelled here.
func (s *Service) CancelOutbound(ctx context.Context, tenantID, outboundID int64, reason string, op Operator) error {
	if reason == "" {
		return apierr.Invalid("IV_REASON_REQUIRED", "请填写取消原因")
	}
	return pgdb.InTx(ctx, s.pool, func(ctx context.Context, tx pgx.Tx) error {
		q := s.q.WithTx(tx)
		head, err := q.GetOutboundForUpdate(ctx, store.GetOutboundForUpdateParams{
			TenantID: tenantID, ID: outboundID,
		})
		if err == pgx.ErrNoRows {
			return apierr.NotFound("IV_OUTBOUND_NOT_FOUND", "出库单不存在")
		}
		if err != nil {
			return err
		}
		if head.Status != outboundDraft {
			return apierr.Invalid("IV_OUTBOUND_NOT_DRAFT", "已出库或已取消的单据不能再取消").
				WithMeta("status", head.Status)
		}

		items, err := q.OutboundItemsOf(ctx, store.OutboundItemsOfParams{
			TenantID: tenantID, OutboundID: head.ID,
		})
		if err != nil {
			return err
		}
		for _, item := range items {
			picks, err := q.OutboundItemStocksOf(ctx, store.OutboundItemStocksOfParams{
				TenantID: tenantID, OutboundItemID: item.ID,
			})
			if err != nil {
				return err
			}
			for _, p := range picks {
				after, err := q.ReleaseLockToReserve(ctx, store.ReleaseLockToReserveParams{
					TenantID: tenantID, ID: p.StockID, Qty: p.Qty,
				})
				if err != nil {
					return err
				}
				if err := q.UncommitReservationLine(ctx, store.UncommitReservationLineParams{
					TenantID: tenantID, ID: p.ReservationLineID, Qty: p.Qty,
				}); err != nil {
					return err
				}
				if err := q.AppendLedger(ctx, store.AppendLedgerParams{
					TenantID: tenantID, StockID: p.StockID, WarehouseID: p.WarehouseID,
					SkuID: item.SkuID, Movement: "RELEASE_LOCK", Qty: p.Qty,
					RefType: "OUTBOUND", RefID: head.ID, RefNo: head.OutboundNo,
					OnHandAfter: after.OnHandQty, AvailableAfter: after.AvailableQty,
					OperatorID: op.ID, Remark: "取消出库单，锁定退回预留",
					UnitCost: "0", Amount: "0", AvgCostAfter: after.AvgCost,
				}); err != nil {
					return err
				}
			}
			if err := q.UnlockReservationQty(ctx, store.UnlockReservationQtyParams{
				TenantID: tenantID, ID: item.ReservationID, Qty: item.Qty,
			}); err != nil {
				return err
			}
		}
		return q.SetOutboundCancelled(ctx, store.SetOutboundCancelledParams{
			TenantID: tenantID, ID: head.ID, Reason: reason,
		})
	})
}

// announceAllocation publishes what the contract now holds, so procurement can
// raise, shrink or close requirements against the real shortage. It returns
// how many lines went out and how many of them are still short.
//
// It reads the final committed state rather than accumulating as it goes,
// because several things in one transaction can move the same numbers —
// reserving, releasing a superseded version, handing freed goods to whoever
// was waiting. A tally built along the way is out of date by the end.
//
// Only the current version's lines go out. An older version's reservations
// may still be sitting there, and re-announcing them under the new version id
// would resurrect requirements the change was supposed to retire.
func (s *Service) announceAllocation(ctx context.Context, tx pgx.Tx, q *store.Queries, tenantID, contractID int64) (int, int, error) {
	rows, err := q.ReservationsOfContract(ctx, store.ReservationsOfContractParams{
		TenantID: tenantID, RefType: RefTypeContract, RefID: contractID,
	})
	if err != nil || len(rows) == 0 {
		return 0, 0, err
	}
	current := rows[0].VersionID
	for _, r := range rows {
		if r.VersionID > current {
			current = r.VersionID
		}
	}
	e := stockShortageEvent{ContractID: contractID}
	short := 0
	for _, r := range rows {
		if r.VersionID != current {
			continue
		}
		e.ContractNo, e.VersionID, e.VersionNo = r.RefNo, r.VersionID, r.VersionNo
		e.CustomerName, e.DeliveryDate = r.CustomerName, r.DeliveryDate
		e.Lines = append(e.Lines, shortageLine{
			ContractItemID: r.RefLineID, ProductID: r.ProductID, SkuID: r.SkuID,
			ProductCode: r.ProductCode, ProductName: r.ProductName,
			UomID: r.UomID, UomCode: r.UomCode,
			DemandQty: r.DemandQty, ReservedQty: r.ReservedQty, ShortageQty: r.ShortageQty,
		})
		if d, err := decimal.NewFromString(r.ShortageQty); err == nil && d.GreaterThan(decimal.Zero) {
			short++
		}
	}
	if len(e.Lines) == 0 {
		return 0, 0, nil
	}
	payload, err := json.Marshal(e)
	if err != nil {
		return 0, 0, err
	}
	return len(e.Lines), short, outbox.Append(ctx, tx, outbox.Event{
		TenantID: tenantID, AggregateType: "contract",
		AggregateID: strconv.FormatInt(contractID, 10),
		EventType:   "StockAllocated", Payload: payload,
	})
}

type OutboundFilter struct {
	Status  string
	Keyword string
}

func (s *Service) ListOutbounds(ctx context.Context, tenantID int64, f OutboundFilter, page, size int32) ([]store.ListOutboundsRow, int64, error) {
	page, size = normalizePage(page, size)
	rows, err := s.q.ListOutbounds(ctx, store.ListOutboundsParams{
		TenantID: tenantID, Status: f.Status, Keyword: f.Keyword,
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

func (s *Service) OutboundItems(ctx context.Context, tenantID, outboundID int64) ([]store.OutboundItemsOfRow, error) {
	return s.q.OutboundItemsOf(ctx, store.OutboundItemsOfParams{
		TenantID: tenantID, OutboundID: outboundID,
	})
}

func (s *Service) ListShippableContracts(ctx context.Context, tenantID int64, keyword string, page, size int32) ([]store.ListShippableContractsRow, int64, error) {
	page, size = normalizePage(page, size)
	rows, err := s.q.ListShippableContracts(ctx, store.ListShippableContractsParams{
		TenantID: tenantID, Keyword: keyword, RowLimit: size, RowOffset: (page - 1) * size,
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

func (s *Service) ShippableLines(ctx context.Context, tenantID, contractID int64) ([]store.ShippableLinesOfRow, error) {
	return s.q.ShippableLinesOf(ctx, store.ShippableLinesOfParams{
		TenantID: tenantID, RefID: contractID,
	})
}

// trimQty drops the trailing zeros a NUMERIC(18,4) always carries. "缺 8" reads
// as a fact; "缺 8.0000" reads as a machine talking.
func trimQty(d decimal.Decimal) string {
	return d.Truncate(4).String()
}
