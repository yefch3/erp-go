package app

import (
	"context"
	"sort"

	"github.com/jackc/pgx/v5"
	"github.com/shopspring/decimal"

	"github.com/sgao19/erp-go/pkg/apierr"
)

// DeliveryQualityGuard asks procurement for the authoritative inspection result
// of the orders that actually supplied the picked stock, not another order of
// the same product or sales contract.
type DeliveryQualityGuard interface {
	Check(context.Context, []int64) error
}

func (s *Service) UseDeliveryQualityGuard(g DeliveryQualityGuard) { s.deliveryQuality = g }

func (s *Service) checkOutboundQuality(ctx context.Context, tx pgx.Tx, tenantID, outboundID int64) error {
	if s.deliveryQuality == nil {
		return apierr.Conflict("IV_QUALITY_UNAVAILABLE", "质检核验服务未配置，不能确认出库")
	}
	// Stock is pooled by warehouse/product/SKU. It cannot distinguish lots, so
	// every purchase source in the current, not-yet-emptied pool must pass.
	// Once a consistent ledger reaches zero, older lots no longer supply stock.
	// Lock in a stable order
	// before reading provenance: a concurrent receipt must not add uninspected
	// goods between this check and consuming the outbound lock.
	rows, err := tx.Query(ctx, `SELECT s.id,s.on_hand_qty::text FROM stocks s
		WHERE s.tenant_id=$1 AND s.id IN (
			SELECT p.stock_id FROM outbound_item_stocks p
			JOIN outbound_items i ON i.tenant_id=p.tenant_id AND i.id=p.outbound_item_id
			WHERE p.tenant_id=$1 AND i.outbound_id=$2
		) ORDER BY s.id FOR UPDATE`, tenantID, outboundID)
	if err != nil {
		return err
	}
	type pickedStock struct {
		id     int64
		onHand decimal.Decimal
	}
	stocks := []pickedStock{}
	for rows.Next() {
		var stock pickedStock
		if err := rows.Scan(&stock.id, &stock.onHand); err != nil {
			rows.Close()
			return err
		}
		stocks = append(stocks, stock)
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return err
	}
	if len(stocks) == 0 {
		return apierr.Conflict("IV_QUALITY_SOURCE_REQUIRED", "出库货物缺少可核验的采购质检来源，不能发货")
	}
	poSet := map[int64]bool{}
	for _, stock := range stocks {
		ledger, err := tx.Query(ctx, `SELECT movement,qty::text,ref_type,ref_id,on_hand_after::text FROM stock_ledger
			WHERE tenant_id=$1 AND stock_id=$2 AND movement IN ('INBOUND','OUTBOUND','ADJUST')
			ORDER BY id`, tenantID, stock.id)
		if err != nil {
			return err
		}
		balance := decimal.Zero
		hasSource, unverifiable, corrupt := false, false, false
		stockPOs := map[int64]bool{}
		for ledger.Next() {
			var movement, refType string
			var qty, onHandAfter decimal.Decimal
			var refID int64
			if err := ledger.Scan(&movement, &qty, &refType, &refID, &onHandAfter); err != nil {
				ledger.Close()
				return err
			}
			switch movement {
			case "INBOUND":
				balance = balance.Add(qty)
				if !qty.IsPositive() {
					corrupt = true
				}
				if refType != "PURCHASE" || refID <= 0 || !qty.IsPositive() {
					unverifiable = true
				} else {
					hasSource = true
					stockPOs[refID] = true
				}
			case "OUTBOUND":
				balance = balance.Sub(qty)
				if !qty.IsPositive() {
					corrupt = true
				}
			default:
				// Adjustments and opening/manual receipts have no authoritative
				// inspection link. Never infer their quality from another PO.
				corrupt = true
			}
			if balance.IsNegative() || !balance.Equal(onHandAfter) {
				// A missing or inconsistent physical movement must never be
				// hidden by a later zero or a matching final balance.
				corrupt = true
			}
			if balance.IsZero() && !corrupt {
				hasSource, unverifiable = false, false
				stockPOs = map[int64]bool{}
			}
		}
		ledger.Close()
		if err := ledger.Err(); err != nil {
			return err
		}
		if !hasSource || unverifiable || corrupt || !balance.Equal(stock.onHand) {
			return apierr.Conflict("IV_QUALITY_SOURCE_REQUIRED", "库存含有无法核验质检的入库来源，请先补齐采购与质检资料，不能确认出库")
		}
		for id := range stockPOs {
			poSet[id] = true
		}
	}
	poIDs := make([]int64, 0, len(poSet))
	for id := range poSet {
		poIDs = append(poIDs, id)
	}
	sort.Slice(poIDs, func(i, j int) bool { return poIDs[i] < poIDs[j] })
	return s.deliveryQuality.Check(ctx, poIDs)
}
