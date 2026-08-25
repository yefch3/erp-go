package app

import (
	"context"
	"log/slog"

	"github.com/jackc/pgx/v5"
	"github.com/shopspring/decimal"

	"github.com/sgao19/erp-go/pkg/pgdb"
	"github.com/sgao19/erp-go/services/inventory/internal/store"
)

// RefTypeContract is the only thing that reserves stock today. Shipment plans
// convert a reservation into a lock; they do not create one.
const RefTypeContract = "CONTRACT"

// ContractEffective is the slice of export's event this service acts on.
type ContractEffective struct {
	ContractID   int64          `json:"contract_id"`
	ContractNo   string         `json:"contract_no"`
	VersionID    int64          `json:"version_id"`
	VersionNo    int32          `json:"version_no"`
	CustomerName string         `json:"customer_name"`
	DeliveryDate string         `json:"delivery_date"`
	Items        []ContractLine `json:"items"`
}

type ContractLine struct {
	ItemID      int64  `json:"contract_item_id"`
	ProductID   int64  `json:"product_id"`
	SkuID       int64  `json:"sku_id"`
	ProductCode string `json:"product_code"`
	ProductName string `json:"product_name"`
	Qty         string `json:"qty"`
	UomID       int64  `json:"uom_id"`
	UomCode     string `json:"uom_code"`
}

// shortageLine is what procurement needs: how much of a contract line stock
// could not cover.
type shortageLine struct {
	ContractItemID int64  `json:"contract_item_id"`
	ProductID      int64  `json:"product_id"`
	SkuID          int64  `json:"sku_id"`
	ProductCode    string `json:"product_code"`
	ProductName    string `json:"product_name"`
	UomID          int64  `json:"uom_id"`
	UomCode        string `json:"uom_code"`
	DemandQty      string `json:"demand_qty"`
	ReservedQty    string `json:"reserved_qty"`
	ShortageQty    string `json:"shortage_qty"`
}

// stockShortageEvent is published after every contract is allocated against.
// It is published even when nothing is short, because "this contract needs
// nothing bought" is also a fact procurement has to be able to act on — and
// because a consumer that only ever hears about problems cannot tell the
// difference between "no shortage" and "not processed yet".
type stockShortageEvent struct {
	ContractID   int64          `json:"contract_id"`
	ContractNo   string         `json:"contract_no"`
	VersionID    int64          `json:"version_id"`
	VersionNo    int32          `json:"version_no"`
	CustomerName string         `json:"customer_name"`
	DeliveryDate string         `json:"delivery_date"`
	Lines        []shortageLine `json:"lines"`
}

// AllocateContract is the heart of the two-layer model.
//
// A contract that takes effect claims stock immediately — layer 1, a soft
// reservation. What stock cannot cover is a shortage, and the shortage, not
// the contract, is what makes somebody buy something.
//
// Reserving rather than merely querying is the whole point. Two contracts
// signed the same afternoon against 2000 units in the warehouse must not both
// read "2000 available" and both conclude they need nothing: the first takes
// 1500 and the second sees 500. A read-only check cannot express that, no
// matter how carefully it is written.
//
// Everything happens in one transaction, with the candidate stock rows locked
// FOR UPDATE in a stable order, so concurrent contracts queue rather than
// interleave.
func (s *Service) AllocateContract(ctx context.Context, tenantID int64, e ContractEffective, log *slog.Logger, claim EventClaim) error {
	if len(e.Items) == 0 {
		log.Warn("contract effective with no lines, nothing to allocate",
			"contract_id", e.ContractID, "contract_no", e.ContractNo)
		return nil
	}

	var lines, short int
	err := pgdb.InTx(ctx, s.pool, func(ctx context.Context, tx pgx.Tx) error {
		// 认领与这一笔业务写入同生共死：崩溃一起回滚，提交一起落库。
		// 见 eventclaim.go。
		if err := claim(ctx, tx); err != nil {
			return err
		}
		q := s.q.WithTx(tx)
		for _, line := range e.Items {
			demand, err := decimal.NewFromString(line.Qty)
			if err != nil || demand.LessThanOrEqual(decimal.Zero) {
				log.Warn("contract line with unusable quantity, skipped",
					"contract_no", e.ContractNo, "item", line.ItemID, "qty", line.Qty)
				continue
			}
			if _, err := s.reserveLine(ctx, q, tenantID, e, line, demand); err != nil {
				return err
			}
		}
		// Releasing the previous version can free quantity that these very
		// lines are short of, so it has to happen before the shortage is
		// announced — otherwise the contract asks procurement to buy goods it
		// just handed back to itself.
		if err := s.releaseSuperseded(ctx, tx, q, tenantID, e, log); err != nil {
			return err
		}
		var err error
		lines, short, err = s.announceAllocation(ctx, tx, q, tenantID, e.ContractID)
		return err
	})
	if err != nil {
		return err
	}
	log.Info("contract allocated against stock",
		"contract_id", e.ContractID, "contract_no", e.ContractNo,
		"version_no", e.VersionNo, "lines", lines, "short_lines", short)
	return nil
}

// reserveLine takes as much as it can for one contract line and returns what
// it got. Falling short is a normal outcome, not an error: it is precisely
// the signal that something has to be bought.
func (s *Service) reserveLine(ctx context.Context, q *store.Queries, tenantID int64, e ContractEffective, line ContractLine, demand decimal.Decimal) (decimal.Decimal, error) {
	// Idempotency lives here: one reservation per contract line, so a
	// redelivered event finds the row already present and takes nothing more.
	created, err := q.CreateReservation(ctx, store.CreateReservationParams{
		TenantID: tenantID, RefType: RefTypeContract, RefID: e.ContractID,
		RefLineID: line.ItemID, RefNo: e.ContractNo,
		ProductID: line.ProductID, SkuID: line.SkuID, DemandQty: demand.String(),
		// The contract snapshot. Kept so that when the missing goods arrive
		// months later, the top-up can re-announce the shrunken shortage
		// without asking export what this line was ever about.
		VersionID: e.VersionID, VersionNo: e.VersionNo,
		CustomerName: e.CustomerName, DeliveryDate: e.DeliveryDate,
		ProductCode: line.ProductCode, ProductName: line.ProductName,
		UomID: line.UomID, UomCode: line.UomCode,
	})
	if err != nil && err != pgx.ErrNoRows {
		return decimal.Zero, err
	}
	existing, err := q.GetReservationByRef(ctx, store.GetReservationByRefParams{
		TenantID: tenantID, RefType: RefTypeContract, RefID: e.ContractID, RefLineID: line.ItemID,
	})
	if err != nil {
		return decimal.Zero, err
	}
	already, err := decimal.NewFromString(existing.ReservedQty)
	if err != nil {
		return decimal.Zero, err
	}
	if created == 0 {
		// Seen before. Whatever it managed to reserve then still stands — but
		// the description is refreshed, so a replay repairs a snapshot that
		// predates the columns holding it.
		if err := q.RefreshReservationSnapshot(ctx, store.RefreshReservationSnapshotParams{
			TenantID: tenantID, ID: existing.ID, RefNo: e.ContractNo,
			VersionID: e.VersionID, VersionNo: e.VersionNo,
			CustomerName: e.CustomerName, DeliveryDate: e.DeliveryDate,
			ProductCode: line.ProductCode, ProductName: line.ProductName,
			UomID: line.UomID, UomCode: line.UomCode,
		}); err != nil {
			return decimal.Zero, err
		}
		return already, nil
	}

	return s.takeStock(ctx, q, tenantID, takeRequest{
		ReservationID: existing.ID, ProductID: line.ProductID, SkuID: line.SkuID,
		Want: demand, RefID: e.ContractID, RefNo: e.ContractNo,
		Remark: "合同生效预留",
	})
}

// takeRequest is one attempt to move quantity off the shelf into a
// reservation. Contract activation and a later top-up differ only in the
// remark they leave in the ledger.
type takeRequest struct {
	ReservationID int64
	ProductID     int64
	SkuID         int64
	Want          decimal.Decimal
	RefID         int64
	RefNo         string
	Remark        string
}

// takeStock reserves up to Want from whatever is available, and returns what
// it actually got. Getting less is not an error anywhere it is called from:
// on activation it is the shortage that raises a purchase requirement, and on
// a top-up it is the shortage that still blocks shipping.
func (s *Service) takeStock(ctx context.Context, q *store.Queries, tenantID int64, r takeRequest) (decimal.Decimal, error) {
	rows, err := q.StocksForItemForUpdate(ctx, store.StocksForItemForUpdateParams{
		TenantID: tenantID, ProductID: r.ProductID, SkuID: r.SkuID,
	})
	if err != nil {
		return decimal.Zero, err
	}
	taken := decimal.Zero
	for _, row := range rows {
		remaining := r.Want.Sub(taken)
		if remaining.LessThanOrEqual(decimal.Zero) {
			break
		}
		avail, err := decimal.NewFromString(row.AvailableQty)
		if err != nil {
			return decimal.Zero, err
		}
		take := decimal.Min(avail, remaining)
		if take.LessThanOrEqual(decimal.Zero) {
			continue
		}
		after, err := q.AddReservedQty(ctx, store.AddReservedQtyParams{
			TenantID: tenantID, ID: row.ID, Qty: take.String(),
		})
		if err != nil {
			return decimal.Zero, err
		}
		if err := q.AddReservationLine(ctx, store.AddReservationLineParams{
			TenantID: tenantID, ReservationID: r.ReservationID, StockID: row.ID,
			WarehouseID: row.WarehouseID, Qty: take.String(),
		}); err != nil {
			return decimal.Zero, err
		}
		if err := q.AppendLedger(ctx, store.AppendLedgerParams{
			TenantID: tenantID, StockID: row.ID, WarehouseID: row.WarehouseID,
			SkuID: r.SkuID, Movement: "RESERVE", Qty: take.String(),
			RefType: RefTypeContract, RefID: r.RefID, RefNo: r.RefNo,
			OnHandAfter: after.OnHandQty, AvailableAfter: after.AvailableQty,
			Remark: r.Remark,
			// A reservation moves no value, only who may have the goods.
			UnitCost: "0", Amount: "0", AvgCostAfter: after.AvgCost,
		}); err != nil {
			return decimal.Zero, err
		}
		taken = taken.Add(take)
	}
	if taken.GreaterThan(decimal.Zero) {
		if _, err := q.AddReservationQty(ctx, store.AddReservationQtyParams{
			TenantID: tenantID, ID: r.ReservationID, Qty: taken.String(),
		}); err != nil {
			return decimal.Zero, err
		}
	}
	return taken, nil
}

// releaseSuperseded hands back what an earlier version of the same contract
// was holding.
//
// A change rewrites the contract's lines, and the old ones stop describing
// anything the company owes. Left alone they keep quantity off the market
// forever and — worse — the warehouse is offered goods to ship against a
// version that no longer exists.
//
// Anything already locked to a picking list or already shipped is left
// exactly where it is. That is the agreed rule for a re-signed contract:
// work in progress continues and a person decides, because no automatic
// answer is right when goods are half out of the door.
func (s *Service) releaseSuperseded(ctx context.Context, tx pgx.Tx, q *store.Queries, tenantID int64, e ContractEffective, log *slog.Logger) error {
	old, err := q.SupersededReservationsOf(ctx, store.SupersededReservationsOfParams{
		TenantID: tenantID, RefType: RefTypeContract, RefID: e.ContractID,
		KeepVersionID: e.VersionID,
	})
	if err != nil || len(old) == 0 {
		return err
	}
	released, held := 0, 0
	freed := map[[2]int64]bool{}
	for _, r := range old {
		locked, err := decimal.NewFromString(r.LockedQty)
		if err != nil {
			return err
		}
		shipped, err := decimal.NewFromString(r.ShippedQty)
		if err != nil {
			return err
		}
		if locked.GreaterThan(decimal.Zero) || shipped.GreaterThan(decimal.Zero) {
			held++
			log.Warn("superseded contract line still has goods committed, left for a person",
				"contract_no", r.RefNo, "old_version", r.VersionNo,
				"line", r.RefLineID, "locked", r.LockedQty, "shipped", r.ShippedQty)
			continue
		}
		lines, err := q.ReservationLinesOf(ctx, store.ReservationLinesOfParams{
			TenantID: tenantID, ReservationID: r.ID,
		})
		if err != nil {
			return err
		}
		for _, l := range lines {
			after, err := q.ReleaseReservedQty(ctx, store.ReleaseReservedQtyParams{
				TenantID: tenantID, ID: l.StockID, Qty: l.Qty,
			})
			if err != nil {
				return err
			}
			if err := q.AppendLedger(ctx, store.AppendLedgerParams{
				TenantID: tenantID, StockID: l.StockID, WarehouseID: l.WarehouseID,
				SkuID: r.SkuID, Movement: "RELEASE_RESERVE", Qty: l.Qty,
				RefType: RefTypeContract, RefID: e.ContractID, RefNo: r.RefNo,
				OnHandAfter: after.OnHandQty, AvailableAfter: after.AvailableQty,
				Remark:   "合同变更，旧版本预留释放",
				UnitCost: "0", Amount: "0", AvgCostAfter: after.AvgCost,
			}); err != nil {
				return err
			}
		}
		if err := q.SetReservationStatus(ctx, store.SetReservationStatusParams{
			TenantID: tenantID, ID: r.ID, NewStatus: "RELEASED",
		}); err != nil {
			return err
		}
		freed[[2]int64{r.ProductID, r.SkuID}] = true
		released++
	}
	// Goods handed back must not sit idle while contracts are short of them.
	// The new version of this very contract is usually first in the queue, so
	// this is what stops a change asking procurement to buy what the change
	// itself released.
	for key := range freed {
		if err := s.fillWaiting(ctx, tx, q, tenantID, key[0], key[1], e.ContractID); err != nil {
			return err
		}
	}
	if released > 0 || held > 0 {
		log.Info("superseded reservations settled",
			"contract_no", e.ContractNo, "new_version", e.VersionNo,
			"released", released, "left_for_review", held)
	}
	return nil
}
