package app

import (
	"context"
	"log/slog"
	"strconv"

	"github.com/jackc/pgx/v5"

	"github.com/sgao19/erp-go/pkg/livefeed"
	"github.com/sgao19/erp-go/pkg/pgdb"
	"github.com/sgao19/erp-go/services/export/internal/store"
)

// StockOutbound is the slice of inventory's event export acts on: goods
// physically left the warehouse against a contract.
type StockOutbound struct {
	OutboundNo string            `json:"outbound_no"`
	ContractID int64             `json:"contract_id"`
	ContractNo string            `json:"contract_no"`
	Lines      []OutboundShipped `json:"lines"`
}

type OutboundShipped struct {
	ContractItemID int64  `json:"contract_item_id"`
	ProductID      int64  `json:"product_id"`
	SkuID          int64  `json:"sku_id"`
	Qty            string `json:"qty"`
}

// ApplyShipment records what left the warehouse against a contract.
//
// Until now the outbound event had no reader, which meant the warehouse could
// ship a contract in full and the salesperson who sold it would see no sign
// of it anywhere — they would have to ring the warehouse to find out whether
// their customer's goods were on a boat. That is the gap this closes.
//
// Nothing is written onto the contract itself. An approved version is frozen
// by a database trigger because it records what was agreed; what has since
// shipped is a fact about the world and belongs beside it, not inside it.
func (s *Service) ApplyShipment(ctx context.Context, tenantID int64, e StockOutbound, log *slog.Logger, claim EventClaim) error {
	if e.ContractID == 0 || len(e.Lines) == 0 {
		log.Warn("outbound event with nothing to record, skipping",
			"outbound_no", e.OutboundNo, "contract_id", e.ContractID)
		return nil
	}

	var owner int64
	recorded := 0
	err := pgdb.InTx(ctx, s.pool, func(ctx context.Context, tx pgx.Tx) error {
		// 认领与这一笔业务写入同生共死：崩溃一起回滚，提交一起落库。
		// 见 eventclaim.go。
		if err := claim(ctx, tx); err != nil {
			return err
		}
		q := s.q.WithTx(tx)
		recorded = 0
		for _, l := range e.Lines {
			n, err := q.RecordShipment(ctx, store.RecordShipmentParams{
				TenantID: tenantID, ContractID: e.ContractID,
				ContractItemID: l.ContractItemID, ProductID: l.ProductID,
				SkuID: l.SkuID, OutboundNo: e.OutboundNo, Qty: l.Qty,
			})
			if err != nil {
				return err
			}
			// Zero means this outbound was already recorded. Not an error:
			// at-least-once delivery makes it the expected case on a retry.
			recorded += int(n)
		}
		row, err := q.GetContract(ctx, store.GetContractParams{
			TenantID: tenantID, ID: e.ContractID,
		})
		if err != nil && err != pgx.ErrNoRows {
			return err
		}
		owner = row.SalesEmployeeID
		return nil
	})
	if err != nil {
		return err
	}
	if recorded == 0 {
		return nil // seen before; nobody needs telling twice
	}

	// The person who sold it is the one waiting to hear that it went out.
	if s.live != nil && owner != 0 {
		s.live.ToEmployees(ctx, tenantID, []int64{owner}, livefeed.Event{
			Type: livefeed.DocChanged, Subject: "CONTRACT:" + strconv.FormatInt(e.ContractID, 10),
		})
	}
	log.Info("shipment recorded against contract",
		"contract_no", e.ContractNo, "outbound_no", e.OutboundNo, "lines", recorded)
	return nil
}

// ShipmentProgress reports how much of each line of a contract version has
// shipped, so the sales side can answer "where is my customer's order".
func (s *Service) ShipmentProgress(ctx context.Context, tenantID, contractID, versionID int64) ([]store.ShipmentProgressOfRow, error) {
	return s.q.ShipmentProgressOf(ctx, store.ShipmentProgressOfParams{
		TenantID: tenantID, ContractID: contractID, ContractVersionID: versionID,
	})
}
