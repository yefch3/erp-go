package app

import (
	"context"
	"errors"
	"fmt"
	"sort"

	"github.com/jackc/pgx/v5"
	"github.com/sgao19/erp-go/pkg/apierr"
	"github.com/sgao19/erp-go/services/shipping/internal/store"
	"github.com/shopspring/decimal"
)

type deliveryProduct struct {
	Code, Name, Spec, UOM string
}

type deliveryCargo struct {
	ItemID, VersionID int64
	Product           deliveryProduct
	Qty               decimal.Decimal
}

type deliverySnapshots struct {
	latest []deliveryCargo
	all    []deliveryCargo
}

func lockDeliveryContract(ctx context.Context, tx pgx.Tx, tenant, contract int64) error {
	_, err := tx.Exec(ctx, `SELECT pg_advisory_xact_lock(hashtextextended($1,0))`, fmt.Sprintf("shipping-contract:%d:%d", tenant, contract))
	return err
}

func deliverySourceMissing() error {
	return apierr.Conflict("QUALITY_DELIVERY_SOURCE_REQUIRED", "本批货物缺少可核验的外销合同明细，请先关联销售合同并核对货物资料，再完成质检")
}

func deliveryHistoryMissing() error {
	return apierr.Conflict("QUALITY_DELIVERY_HISTORY_REQUIRED", "历史已发运记录缺少可核验的货物明细，无法计算累计交货量，请先核对历史货物关联")
}

// Standalone orders retain contract_id=0 and their negative cargo identities.
// The separate, tenant-scoped link supplies only the sales-contract identity
// needed for its execution check and authoritative inspection verdict.
func deliveryContractID(ctx context.Context, tx pgx.Tx, current store.ShippingSchedule) (int64, error) {
	if current.ContractHandoffID == nil || *current.ContractHandoffID <= 0 {
		return 0, deliverySourceMissing()
	}
	var handoffContract, linked int64
	var manual bool
	err := tx.QueryRow(ctx, `SELECT h.contract_id,coalesce(m.linked_contract_id,0),m.handoff_id IS NOT NULL
	 FROM contract_shipping_handoffs h LEFT JOIN manual_shipping_orders m ON m.tenant_id=h.tenant_id AND m.handoff_id=h.id
	 WHERE h.tenant_id=$1 AND h.id=$2`, current.TenantID, *current.ContractHandoffID).Scan(&handoffContract, &linked, &manual)
	if errors.Is(err, pgx.ErrNoRows) {
		return 0, deliverySourceMissing()
	}
	if err != nil {
		return 0, err
	}
	if manual {
		if handoffContract != 0 || linked <= 0 || (current.ContractID != nil && *current.ContractID != 0 && *current.ContractID != linked) {
			return 0, deliverySourceMissing()
		}
		return linked, nil
	}
	if current.ContractID == nil || *current.ContractID <= 0 || handoffContract != *current.ContractID {
		return 0, deliverySourceMissing()
	}
	return handoffContract, nil
}

func loadDeliverySnapshots(ctx context.Context, tx pgx.Tx, tenant, contract int64) (deliverySnapshots, error) {
	var latest int32
	if err := tx.QueryRow(ctx, `SELECT coalesce(max(version_no),0) FROM contract_shipping_handoffs WHERE tenant_id=$1 AND contract_id=$2`, tenant, contract).Scan(&latest); err != nil {
		return deliverySnapshots{}, err
	}
	rows, err := tx.Query(ctx, `SELECT DISTINCT h.version_no,h.contract_version_id,c.contract_item_id,c.product_code,c.product_name,c.specification,c.uom_code
	 FROM contract_shipping_handoffs h JOIN contract_shipping_handoff_cargo c ON c.tenant_id=h.tenant_id AND c.handoff_id=h.id
	 WHERE h.tenant_id=$1 AND h.contract_id=$2 AND h.contract_version_id>0 AND c.contract_item_id>0
	 ORDER BY h.version_no,h.contract_version_id,c.contract_item_id`, tenant, contract)
	if err != nil {
		return deliverySnapshots{}, err
	}
	defer rows.Close()
	result := deliverySnapshots{}
	for rows.Next() {
		var version int32
		var cargo deliveryCargo
		if err := rows.Scan(&version, &cargo.VersionID, &cargo.ItemID, &cargo.Product.Code, &cargo.Product.Name, &cargo.Product.Spec, &cargo.Product.UOM); err != nil {
			return deliverySnapshots{}, err
		}
		result.all = append(result.all, cargo)
		if version == latest {
			result.latest = append(result.latest, cargo)
		}
	}
	return result, rows.Err()
}

func mapManualDeliveryCargo(cargo deliveryCargo, snapshots deliverySnapshots) (deliveryCargo, error) {
	matches := map[int64]deliveryCargo{}
	for _, candidate := range snapshots.latest {
		// The existing manual form exposes name/spec/UOM but no product code.
		// An omitted code is safe only when exactly one real line matches all
		// three fields; two SKUs/lines with the same display identity are refused.
		if candidate.Product.Name != cargo.Product.Name || candidate.Product.Spec != cargo.Product.Spec || candidate.Product.UOM != cargo.Product.UOM || (cargo.Product.Code != "" && candidate.Product.Code != cargo.Product.Code) {
			continue
		}
		if previous, exists := matches[candidate.ItemID]; exists && (previous.Product != candidate.Product || previous.VersionID != candidate.VersionID) {
			return deliveryCargo{}, deliverySourceMissing()
		}
		matches[candidate.ItemID] = candidate
	}
	if len(matches) != 1 {
		return deliveryCargo{}, apierr.Conflict("QUALITY_MANUAL_CARGO_MAPPING_REQUIRED", "补录货物无法唯一对应关联合同的当前产品明细，请核对产品名称、规格和单位；已委托单据可使用该合同的正式物流任务发运，勿重复发货")
	}
	for _, matched := range matches {
		matched.Qty = cargo.Qty
		return matched, nil
	}
	return deliveryCargo{}, deliverySourceMissing()
}

// Read the current voyage and every already-dispatched voyage of the same real
// contract, including standalone orders linked through manual_shipping_orders.
// The caller holds the same contract advisory lock used by contract refreshes
// and manual linking, so concurrent batches cannot reuse one passed quantity.
func cumulativeDeliveryLines(ctx context.Context, tx pgx.Tx, current store.ShippingSchedule, contractID int64) ([]DeliveryQualityLine, error) {
	snapshots, err := loadDeliverySnapshots(ctx, tx, current.TenantID, contractID)
	if err != nil {
		return nil, err
	}
	rows, err := tx.Query(ctx, `SELECT v.id,coalesce(v.contract_id,0),coalesce(h.contract_id,0),coalesce(m.linked_contract_id,0),m.handoff_id IS NOT NULL,
	 coalesce(h.allocation_status,''),coalesce(h.contract_version_id,0),coalesce(c.id,0),coalesce(c.contract_item_id,0),coalesce(c.quantity,0)::text,
	 coalesce(c.product_code,''),coalesce(c.product_name,''),coalesce(c.specification,''),coalesce(c.uom_code,'')
	 FROM shipping_schedules v
	 LEFT JOIN contract_shipping_handoffs h ON h.tenant_id=v.tenant_id AND h.id=v.contract_handoff_id
	 LEFT JOIN manual_shipping_orders m ON m.tenant_id=h.tenant_id AND m.handoff_id=h.id
	 LEFT JOIN contract_shipping_handoff_cargo c ON c.tenant_id=h.tenant_id AND c.handoff_id=h.id
	 WHERE v.tenant_id=$1 AND (v.id=$2 OR
	 ((v.contract_id=$3 AND m.handoff_id IS NULL OR m.linked_contract_id=$3)
	  AND (v.atd IS NOT NULL OR v.status IN ('SAILED','IN_TRANSIT','ARRIVED','COMPLETED')
	   OR EXISTS(SELECT 1 FROM shipping_schedule_changes ch WHERE ch.tenant_id=v.tenant_id AND ch.schedule_id=v.id
	    AND ch.field_name='status' AND ch.new_value IN ('SAILED','IN_TRANSIT','ARRIVED')))))
	 ORDER BY v.id,c.line_no,c.id`, current.TenantID, current.ID, contractID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	targets := map[int64]deliveryCargo{}
	var all []deliveryCargo
	for rows.Next() {
		var voyageID, voyageContract, handoffContract, linked, cargoID int64
		var manual bool
		var allocation, qtyText string
		var cargo deliveryCargo
		if err := rows.Scan(&voyageID, &voyageContract, &handoffContract, &linked, &manual, &allocation, &cargo.VersionID, &cargoID, &cargo.ItemID, &qtyText, &cargo.Product.Code, &cargo.Product.Name, &cargo.Product.Spec, &cargo.Product.UOM); err != nil {
			return nil, err
		}
		isCurrent := voyageID == current.ID
		invalid := cargoID <= 0 || (!manual && (voyageContract != contractID || handoffContract != contractID || cargo.ItemID <= 0)) || (manual && (handoffContract != 0 || linked != contractID || (voyageContract != 0 && voyageContract != contractID)))
		cargo.Qty, err = decimal.NewFromString(qtyText)
		if err != nil || !cargo.Qty.IsPositive() {
			invalid = true
		}
		if invalid {
			if !isCurrent {
				return nil, deliveryHistoryMissing()
			}
			return nil, deliverySourceMissing()
		}
		if manual {
			cargo, err = mapManualDeliveryCargo(cargo, snapshots)
			if err != nil {
				return nil, err
			}
		} else if isCurrent && allocation != "ALLOCATED" {
			return nil, deliverySourceMissing()
		}
		all = append(all, cargo)
		if isCurrent {
			if prior, exists := targets[cargo.ItemID]; exists && (prior.Product != cargo.Product || prior.VersionID != cargo.VersionID) {
				return nil, deliverySourceMissing()
			}
			targets[cargo.ItemID] = cargo
		}
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	if len(targets) == 0 {
		return nil, deliverySourceMissing()
	}
	lines := make([]DeliveryQualityLine, 0, len(targets))
	for _, target := range targets {
		total, remapped := decimal.Zero, false
		for _, cargo := range all {
			if cargo.ItemID == target.ItemID || (cargo.VersionID != target.VersionID && cargo.Product == target.Product) {
				total = total.Add(cargo.Qty)
				remapped = remapped || cargo.ItemID != target.ItemID
			}
		}
		if remapped {
			matching := map[int64]bool{}
			for _, candidate := range snapshots.all {
				if candidate.VersionID == target.VersionID && candidate.Product == target.Product {
					matching[candidate.ItemID] = true
				}
			}
			if len(matching) != 1 {
				return nil, apierr.Conflict("QUALITY_VERSION_ALLOCATION_REQUIRED", "合同变更后存在相同规格的多条明细，无法确认历史已发量归属，请先核对货物关联")
			}
		}
		lines = append(lines, DeliveryQualityLine{ContractItemID: target.ItemID, Qty: total.StringFixed(4)})
	}
	sort.Slice(lines, func(i, j int) bool { return lines[i].ContractItemID < lines[j].ContractItemID })
	return lines, nil
}
