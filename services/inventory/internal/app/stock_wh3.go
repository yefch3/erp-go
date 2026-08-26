package app

import (
	"context"
	"errors"
	"strings"

	"github.com/jackc/pgx/v5"
	"github.com/shopspring/decimal"

	"github.com/sgao19/erp-go/pkg/apierr"
	"github.com/sgao19/erp-go/pkg/pgdb"
	"github.com/sgao19/erp-go/services/inventory/internal/store"
)

// GetStock 按租户读取单条库存快照，供库存详情页展示数量、占用和成本汇总。
func (s *Service) GetStock(ctx context.Context, tenantID, id int64) (store.GetStockRow, error) {
	row, err := s.q.GetStock(ctx, store.GetStockParams{TenantID: tenantID, ID: id})
	if errors.Is(err, pgx.ErrNoRows) {
		return store.GetStockRow{}, apierr.NotFound("IV_STOCK_NOT_FOUND", "库存记录不存在")
	}
	return row, err
}

// FreezeStock 原子冻结尚可用的库存，并追加一条不可修改的库存流水。
func (s *Service) FreezeStock(ctx context.Context, tenantID, id int64, qty, reason string, op Operator) error {
	return s.changeFrozen(ctx, tenantID, id, qty, reason, op, true)
}

// UnfreezeStock 原子释放已冻结库存，并追加一条不可修改的库存流水。
func (s *Service) UnfreezeStock(ctx context.Context, tenantID, id int64, qty, reason string, op Operator) error {
	return s.changeFrozen(ctx, tenantID, id, qty, reason, op, false)
}

func (s *Service) changeFrozen(ctx context.Context, tenantID, id int64, qtyText, reason string, op Operator, freeze bool) error {
	qty, err := decimal.NewFromString(qtyText)
	if err != nil || qty.LessThanOrEqual(decimal.Zero) {
		return apierr.Invalid("IV_QTY_INVALID", "数量必须大于 0")
	}
	if strings.TrimSpace(reason) == "" {
		return apierr.Invalid("IV_FREEZE_REASON_REQUIRED", "请填写原因")
	}

	return pgdb.InTx(ctx, s.pool, func(ctx context.Context, tx pgx.Tx) error {
		q := s.q.WithTx(tx)
		before, err := q.GetStockForUpdate(ctx, store.GetStockForUpdateParams{TenantID: tenantID, ID: id})
		if errors.Is(err, pgx.ErrNoRows) {
			return apierr.NotFound("IV_STOCK_NOT_FOUND", "库存记录不存在")
		}
		if err != nil {
			return err
		}

		limitText := before.AvailableQty
		if !freeze {
			limitText = before.FrozenQty
		}
		limit, err := decimal.NewFromString(limitText)
		if err != nil {
			return err
		}
		if qty.GreaterThan(limit) {
			if freeze {
				return apierr.Conflict("IV_FREEZE_EXCEEDS_AVAILABLE", "冻结数量不能超过可用库存")
			}
			return apierr.Conflict("IV_UNFREEZE_EXCEEDS_FROZEN", "解冻数量不能超过已冻结库存")
		}

		var onHand, available, avgCost string
		movement := "FREEZE"
		if freeze {
			after, err := q.AddFrozenQty(ctx, store.AddFrozenQtyParams{TenantID: tenantID, ID: id, Qty: qty.String()})
			if err != nil {
				return err
			}
			onHand, available, avgCost = after.OnHandQty, after.AvailableQty, after.AvgCost
		} else {
			after, err := q.ReleaseFrozenQty(ctx, store.ReleaseFrozenQtyParams{TenantID: tenantID, ID: id, Qty: qty.String()})
			if err != nil {
				return err
			}
			onHand, available, avgCost = after.OnHandQty, after.AvailableQty, after.AvgCost
			movement = "UNFREEZE"
		}

		return q.AppendLedger(ctx, store.AppendLedgerParams{
			TenantID: tenantID, StockID: id, WarehouseID: before.WarehouseID, SkuID: before.SkuID,
			Movement: movement, Qty: qty.String(), RefType: "STOCK", RefID: id, RefNo: "",
			OnHandAfter: onHand, AvailableAfter: available, OperatorID: op.ID,
			Remark: strings.TrimSpace(reason), UnitCost: "0", Amount: "0", AvgCostAfter: avgCost,
		})
	})
}
