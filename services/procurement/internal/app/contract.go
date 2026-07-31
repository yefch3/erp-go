package app

import (
	"context"
	"log/slog"

	"github.com/jackc/pgx/v5"
	"github.com/shopspring/decimal"

	"github.com/sgao19/erp-go/pkg/pgdb"
	"github.com/sgao19/erp-go/services/procurement/internal/store"
)

// StockAllocated is what inventory publishes after a contract has claimed
// whatever stock it could. Procurement acts on the SHORTAGE, never on the
// contract quantity.
//
// This is the correction that matters: a contract for 1500 units against 2000
// in the warehouse needs nothing bought. Sourcing gross demand would raise a
// purchase requirement for every line of every contract ever signed, and a
// buyer working that list would order goods the company already owns.
type StockAllocated struct {
	ContractID   int64           `json:"contract_id"`
	ContractNo   string          `json:"contract_no"`
	VersionID    int64           `json:"version_id"`
	VersionNo    int32           `json:"version_no"`
	CustomerName string          `json:"customer_name"`
	DeliveryDate string          `json:"delivery_date"`
	Lines        []AllocatedLine `json:"lines"`
}

type AllocatedLine struct {
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

// RequirementsFromShortage turns the part of a contract that stock could not
// cover into things somebody has to buy.
//
// Idempotent in two directions, because Kafka promises no better than
// at-least-once:
//
//   - the same event twice hits the unique index on the contract line and
//     refreshes the snapshot instead of creating a duplicate;
//   - a NEW version of the same contract retires the previous version's
//     untouched requirements, because a change rewrites the lines and the old
//     ones no longer describe anything that is owed.
//
// Lines with no shortage produce no requirement. They still count as
// processed, so a later top-up — stock arriving and covering more of the
// contract — is the only thing that could reduce one, never this handler.
func (s *Service) RequirementsFromShortage(ctx context.Context, tenantID int64, e StockAllocated, log *slog.Logger) error {
	if len(e.Lines) == 0 {
		log.Warn("allocation event with no lines, nothing to source",
			"contract_id", e.ContractID, "contract_no", e.ContractNo)
		return nil
	}

	var superseded []store.SupersedeRequirementsBeforeRow
	var strandedOrders int64
	sourced, covered := 0, 0
	err := pgdb.InTx(ctx, s.pool, func(ctx context.Context, tx pgx.Tx) error {
		q := s.q.WithTx(tx)
		sourced, covered = 0, 0
		for _, line := range e.Lines {
			short, err := decimal.NewFromString(line.ShortageQty)
			if err != nil {
				log.Warn("allocation line with unusable shortage, skipped",
					"contract_no", e.ContractNo, "item", line.ContractItemID,
					"shortage", line.ShortageQty)
				continue
			}
			if short.LessThanOrEqual(decimal.Zero) {
				// Stock covers it. No new requirement — and if an earlier
				// allocation was short and raised one, it is closed now:
				// leaving it standing would have a buyer order goods the
				// warehouse has since received.
				if _, err := q.CloseCoveredRequirement(ctx, store.CloseCoveredRequirementParams{
					TenantID: tenantID, ContractItemID: line.ContractItemID,
					Reason: "库存已可满足，无需采购",
				}); err != nil {
					return err
				}
				covered++
				continue
			}
			if _, err := q.UpsertRequirement(ctx, store.UpsertRequirementParams{
				TenantID: tenantID, ContractID: e.ContractID, ContractNo: e.ContractNo,
				ContractVersionID: e.VersionID, VersionNo: e.VersionNo,
				ContractItemID: line.ContractItemID, CustomerName: e.CustomerName,
				ProductID: line.ProductID, SkuID: line.SkuID,
				ProductCode: line.ProductCode, ProductName: line.ProductName,
				UomID: line.UomID, UomCode: line.UomCode,
				// The shortage, not the contract quantity.
				RequiredQty: short.String(),
				// The customer's delivery date is the outside limit. Backing
				// off a purchasing lead time needs supplier data this service
				// does not have yet.
				RequiredDate: e.DeliveryDate,
			}); err != nil {
				return err
			}
			sourced++
		}

		rows, err := q.SupersedeRequirementsBefore(ctx, store.SupersedeRequirementsBeforeParams{
			TenantID: tenantID, ContractID: e.ContractID, KeepVersionID: e.VersionID,
			Reason: "合同变更，已由新版本的需求取代",
		})
		if err != nil {
			return err
		}
		superseded = rows

		strandedOrders, err = q.CountOrderedOnOldVersions(ctx, store.CountOrderedOnOldVersionsParams{
			TenantID: tenantID, ContractID: e.ContractID, KeepVersionID: e.VersionID,
		})
		return err
	})
	if err != nil {
		return err
	}

	// The list just moved for reasons nobody on the page did: stock covered a
	// shortage, or a contract change retired a line. Without this the buyer
	// would go on working from a list that is quietly wrong.
	s.nudge(ctx, tenantID)
	log.Info("requirements sourced from stock shortage",
		"contract_id", e.ContractID, "contract_no", e.ContractNo,
		"version_no", e.VersionNo, "short_lines", sourced,
		"covered_by_stock", covered, "superseded", len(superseded))
	if strandedOrders > 0 {
		// Deliberately loud and deliberately not fatal. Goods were already
		// ordered against terms that have since changed; no automatic rule
		// gets that right, so it goes to a person.
		log.Warn("contract changed after purchase orders were raised against it",
			"contract_id", e.ContractID, "contract_no", e.ContractNo,
			"ordered_requirements_on_old_versions", strandedOrders)
	}
	return nil
}
