package app

import (
	"context"
	"errors"
	"sort"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/shopspring/decimal"

	"github.com/sgao19/erp-go/pkg/apierr"
	"github.com/sgao19/erp-go/pkg/grpcx"
	"github.com/sgao19/erp-go/pkg/pgdb"
)

type DeliveryQualityLine struct {
	ContractItemID int64
	Qty            string
}

type DeliveryQualityInput struct {
	POIDs           []int64
	ContractID      int64
	ContractItemIDs []int64
	Lines           []DeliveryQualityLine
}

type DeliveryQualityResult struct {
	Allowed      bool
	Reason, Code string
}

// The same identity used by contract_requirement_group_open and
// contractGroupConflict: old supplier commitments survive a sale revision.
type deliveryProductGroup struct {
	ProductID, SKUID      int64
	Code, Name, Spec, UOM string
}

type deliveryRequirement struct {
	ID, RequirementID int64
	Qty               decimal.Decimal
	Group             deliveryProductGroup
}

func qualityDenied(code, reason string) DeliveryQualityResult {
	return DeliveryQualityResult{Code: code, Reason: reason}
}

// AuthorizeDeliveryQualityCheck exposes only a release verdict to the user
// whose authorized delivery action needs it, never the buyer's documents.
func (s *Service) AuthorizeDeliveryQualityCheck(ctx context.Context, tenant int64) error {
	actor, ok := grpcx.OperatorFromContext(ctx)
	if !ok || actor.TenantID <= 0 || actor.EmployeeID <= 0 {
		return apierr.Unauthorized("QUALITY_DELIVERY_IDENTITY", "请先登录")
	}
	if actor.TenantID != tenant {
		return apierr.Permission("QUALITY_DELIVERY_TENANT", "租户不匹配")
	}
	if s.permissions == nil {
		return apierr.Internal("QUALITY_DELIVERY_AUTH", "质检放行权限服务不可用")
	}
	for _, permission := range []string{"procurement:order:write", "procurement:receipt:write", "inventory:stock:write", "shipping:schedule:write", "shipping:progress:write", "shipping:route:write", "export:shipment:write"} {
		allowed, err := s.permissions.Allowed(ctx, actor.EmployeeID, permission)
		if err != nil {
			return err
		}
		if allowed {
			return nil
		}
	}
	return apierr.Permission("QUALITY_DELIVERY_FORBIDDEN", "没有核验交货质检状态的权限")
}

// CheckDeliveryQuality is authoritative in procurement, which owns inspection
// results. Callers must supply the persisted source of the goods they deliver.
// PO mode checks the whole order. Contract mode also proves quantity coverage.
func (s *Service) CheckDeliveryQuality(ctx context.Context, tenant int64, in DeliveryQualityInput) (DeliveryQualityResult, error) {
	if tenant <= 0 || (len(in.POIDs) == 0 && in.ContractID <= 0) || (len(in.POIDs) > 0 && (in.ContractID != 0 || len(in.ContractItemIDs) > 0 || len(in.Lines) > 0)) {
		return qualityDenied("QUALITY_DELIVERY_SOURCE_REQUIRED", "交货缺少可核验的采购质检来源，请先补齐并完成质检"), nil
	}
	result := DeliveryQualityResult{}
	err := pgdb.InTx(ctx, s.pool, func(ctx context.Context, tx pgx.Tx) error {
		ids := append([]int64(nil), in.POIDs...)
		if in.ContractID > 0 {
			var err error
			ids, result, err = deliveryContractOrders(ctx, tx, tenant, in)
			if err != nil || result.Code != "" {
				return err
			}
		}
		sort.Slice(ids, func(i, j int) bool { return ids[i] < ids[j] })
		var previous int64
		for _, id := range ids {
			if id <= 0 {
				result = qualityDenied("QUALITY_DELIVERY_SOURCE_REQUIRED", "交货缺少可核验的采购质检来源，请先补齐并完成质检")
				return nil
			}
			if id == previous {
				continue
			}
			previous = id
			var err error
			result, err = checkOrderDeliveryQuality(ctx, tx, tenant, id)
			if err != nil || !result.Allowed {
				return err
			}
		}
		if len(ids) == 0 {
			result = qualityDenied("QUALITY_DELIVERY_NOT_INSPECTED", "交货货物尚无已合格的采购质检记录，请先完成质检")
		}
		return nil
	})
	return result, err
}

func deliveryContractOrders(ctx context.Context, tx pgx.Tx, tenant int64, in DeliveryQualityInput) ([]int64, DeliveryQualityResult, error) {
	wanted := map[int64]decimal.Decimal{}
	for _, id := range in.ContractItemIDs {
		if id <= 0 {
			return nil, qualityDenied("QUALITY_DELIVERY_LINES_INVALID", "交货产品明细无效"), nil
		}
		wanted[id] = decimal.Zero
	}
	for _, line := range in.Lines {
		qty, err := decimal.NewFromString(line.Qty)
		if line.ContractItemID <= 0 || err != nil || !qty.IsPositive() {
			return nil, qualityDenied("QUALITY_DELIVERY_LINES_INVALID", "交货产品明细及累计数量无效"), nil
		}
		if prior, ok := wanted[line.ContractItemID]; ok && prior.IsPositive() {
			return nil, qualityDenied("QUALITY_DELIVERY_LINES_INVALID", "交货产品明细不能重复"), nil
		}
		wanted[line.ContractItemID] = qty
	}
	// No selection means the complete latest contract. It is deliberately not
	// satisfied by one passed line while another line was never inspected.
	rows, err := tx.Query(ctx, `SELECT contract_item_id,id,required_qty,product_id,coalesce(sku_id,0),product_code,product_name,spec,uom_code FROM purchase_requirements
	 WHERE tenant_id=$1 AND contract_id=$2 AND version_no=(SELECT max(version_no) FROM purchase_requirements WHERE tenant_id=$1 AND contract_id=$2)
	 ORDER BY contract_item_id`, tenant, in.ContractID)
	if err != nil {
		return nil, DeliveryQualityResult{}, err
	}
	required := map[int64]deliveryRequirement{}
	groups := map[deliveryProductGroup][]deliveryRequirement{}
	for rows.Next() {
		var r deliveryRequirement
		if err = rows.Scan(&r.ID, &r.RequirementID, &r.Qty, &r.Group.ProductID, &r.Group.SKUID, &r.Group.Code, &r.Group.Name, &r.Group.Spec, &r.Group.UOM); err != nil {
			rows.Close()
			return nil, DeliveryQualityResult{}, err
		}
		required[r.ID] = r
		groups[r.Group] = append(groups[r.Group], r)
	}
	rows.Close()
	if err = rows.Err(); err != nil {
		return nil, DeliveryQualityResult{}, err
	}
	if len(wanted) == 0 {
		for id := range required {
			wanted[id] = decimal.Zero
		}
	}
	if len(wanted) == 0 {
		return nil, qualityDenied("QUALITY_DELIVERY_NOT_INSPECTED", "合同货物没有可核验的采购质检记录，请先完成质检"), nil
	}
	// Schedules and draft shipments can predate a contract revision. Preserve
	// their exact old product source only when it identifies one latest line.
	missing := []int64{}
	for id := range wanted {
		if _, current := required[id]; !current {
			missing = append(missing, id)
		}
	}
	historical := map[int64]deliveryProductGroup{}
	ambiguous := map[int64]bool{}
	if len(missing) > 0 {
		oldRows, err := tx.Query(ctx, `SELECT contract_item_id,product_id,coalesce(sku_id,0),product_code,product_name,spec,uom_code
		 FROM purchase_requirements WHERE tenant_id=$1 AND contract_id=$2 AND contract_item_id=ANY($3)`, tenant, in.ContractID, missing)
		if err != nil {
			return nil, DeliveryQualityResult{}, err
		}
		for oldRows.Next() {
			var id int64
			var group deliveryProductGroup
			if err = oldRows.Scan(&id, &group.ProductID, &group.SKUID, &group.Code, &group.Name, &group.Spec, &group.UOM); err != nil {
				oldRows.Close()
				return nil, DeliveryQualityResult{}, err
			}
			if previous, exists := historical[id]; exists && previous != group {
				ambiguous[id] = true
			}
			historical[id] = group
		}
		oldRows.Close()
		if err = oldRows.Err(); err != nil {
			return nil, DeliveryQualityResult{}, err
		}
	}
	ids := []int64{}
	selectedGroups := map[deliveryProductGroup]bool{}
	resolvedWanted := map[int64]decimal.Decimal{}
	for id, qty := range wanted {
		r, exists := required[id]
		if !exists {
			group, known := historical[id]
			matches := groups[group]
			if !known || ambiguous[id] || len(matches) != 1 {
				return nil, qualityDenied("QUALITY_DELIVERY_LINE_NOT_FOUND", "交货产品不属于当前合同明细、无法唯一对应改版后的产品或尚无采购质检来源"), nil
			}
			r = matches[0]
		}
		if qty.IsZero() {
			qty = r.Qty
		}
		// Old and latest IDs may refer to the same physical goods. Aggregate
		// their requested quantities before proving coverage, never separately.
		qty = resolvedWanted[r.ID].Add(qty)
		if !qty.IsPositive() || qty.GreaterThan(r.Qty) {
			return nil, qualityDenied("QUALITY_DELIVERY_QTY_INVALID", "交货累计数量超出合同明细数量"), nil
		}
		resolvedWanted[r.ID] = qty
		selectedGroups[r.Group] = true
	}
	wanted = resolvedWanted
	for group := range selectedGroups {
		orderRows, e := tx.Query(ctx, `SELECT o.id,r.id,i.qty FROM purchase_orders o
		 JOIN purchase_order_items i ON i.tenant_id=o.tenant_id AND i.po_id=o.id
		 JOIN purchase_requirements r ON r.tenant_id=i.tenant_id AND r.id=i.requirement_id
		 WHERE o.tenant_id=$1 AND r.contract_id=$2
		 AND r.product_id=$3 AND coalesce(r.sku_id,0)=$4 AND r.product_code=$5
		 AND r.product_name=$6 AND r.spec=$7 AND r.uom_code=$8
		 AND i.product_id=r.product_id AND coalesce(i.sku_id,0)=coalesce(r.sku_id,0)
		 AND i.product_code=r.product_code AND i.product_name=r.product_name AND i.uom_code=r.uom_code AND i.spec=r.spec
		 AND o.business_type='PROCUREMENT' AND o.status IN ('ORDERED','PARTIALLY_RECEIVED','RECEIVED','CLOSED')`, tenant, in.ContractID, group.ProductID, group.SKUID, group.Code, group.Name, group.Spec, group.UOM)
		if e != nil {
			return nil, DeliveryQualityResult{}, e
		}
		// Current-version requirements retain their explicit procurement
		// source. Only unassigned supply can be shared across revisions.
		currentSupply := map[int64]decimal.Decimal{}
		for _, r := range groups[group] {
			currentSupply[r.RequirementID] = decimal.Zero
		}
		unassigned := decimal.Zero
		for orderRows.Next() {
			var po, requirement int64
			var committed decimal.Decimal
			if e = orderRows.Scan(&po, &requirement, &committed); e != nil {
				orderRows.Close()
				return nil, DeliveryQualityResult{}, e
			}
			ids = append(ids, po)
			if own, current := currentSupply[requirement]; current {
				currentSupply[requirement] = own.Add(committed)
			} else {
				unassigned = unassigned.Add(committed)
			}
		}
		orderRows.Close()
		if e = orderRows.Err(); e != nil {
			return nil, DeliveryQualityResult{}, e
		}
		allocated := map[int64]decimal.Decimal{}
		for _, r := range groups[group] {
			own := currentSupply[r.RequirementID]
			allocated[r.ID] = decimal.Min(own, r.Qty)
			unassigned = unassigned.Add(decimal.Max(own.Sub(r.Qty), decimal.Zero))
		}
		// Allocate historical and excess supply once in latest-line order.
		// Reserving current sources first keeps a later identical line's own
		// goods available, while this stable pass prevents historical supply
		// from being claimed again by another line in a separate request.
		for _, r := range groups[group] {
			shared := decimal.Min(unassigned, r.Qty.Sub(allocated[r.ID]))
			unassigned = unassigned.Sub(shared)
			allocated[r.ID] = allocated[r.ID].Add(shared)
			if qty, selected := wanted[r.ID]; selected && allocated[r.ID].LessThan(qty) {
				return nil, qualityDenied("QUALITY_DELIVERY_QTY_UNCOVERED", "交货累计数量超过该合同明细已有采购质检可覆盖数量，请先补齐采购并完成质检"), nil
			}
		}
	}
	return ids, DeliveryQualityResult{}, nil
}

// The caller holds the purchase-order lock before this check. Lock tasks as
// well so a concurrent first inspection, deletion or round cannot race receipt.
func checkOrderDeliveryQuality(ctx context.Context, tx pgx.Tx, tenant, po int64) (DeliveryQualityResult, error) {
	var kind, status string
	err := tx.QueryRow(ctx, `SELECT business_type,status FROM purchase_orders WHERE tenant_id=$1 AND id=$2 FOR SHARE`, tenant, po).Scan(&kind, &status)
	if errors.Is(err, pgx.ErrNoRows) {
		return qualityDenied("QUALITY_DELIVERY_SOURCE_NOT_FOUND", "交货采购来源不存在或不可核验"), nil
	}
	if err != nil {
		return DeliveryQualityResult{}, err
	}
	if kind != "PROCUREMENT" || (status != "ORDERED" && status != "PARTIALLY_RECEIVED" && status != "RECEIVED" && status != "CLOSED") {
		return qualityDenied("QUALITY_DELIVERY_ORDER_INVALID", "采购单尚未下单或已取消，不能交货"), nil
	}
	rows, err := tx.Query(ctx, `SELECT id,status,procurement_handling_status,completed_at IS NOT NULL FROM quality_inspection_tasks WHERE tenant_id=$1 AND po_id=$2 ORDER BY id FOR SHARE`, tenant, po)
	if err != nil {
		return DeliveryQualityResult{}, err
	}
	var tasks []int64
	result := DeliveryQualityResult{Allowed: true}
	for rows.Next() {
		var id int64
		var state, handling string
		var completed bool
		if err = rows.Scan(&id, &state, &handling, &completed); err != nil {
			rows.Close()
			return DeliveryQualityResult{}, err
		}
		tasks = append(tasks, id)
		if state != "COMPLETED" || handling == "PENDING" || !completed {
			result = qualityDenied("QUALITY_DELIVERY_NOT_PASSED", "货物未完成质检或正在质检、复检及异常处理中，不能交货")
		}
	}
	rows.Close()
	if err = rows.Err(); err != nil {
		return DeliveryQualityResult{}, err
	}
	if len(tasks) == 0 {
		return qualityDenied("QUALITY_DELIVERY_NOT_INSPECTED", "货物尚未质检，请先申请并完成质检后再交货"), nil
	}
	if !result.Allowed {
		return result, nil
	}
	for _, task := range tasks {
		var passed bool
		err = tx.QueryRow(ctx, `SELECT
		 EXISTS(SELECT 1 FROM purchase_order_items WHERE tenant_id=$1 AND po_id=$2)
		 AND EXISTS(SELECT 1 FROM quality_inspection_rounds WHERE tenant_id=$1 AND task_id=$3)
		 AND NOT EXISTS(SELECT 1 FROM purchase_order_items i WHERE i.tenant_id=$1 AND i.po_id=$2 AND NOT EXISTS(
		  SELECT 1 FROM quality_inspection_task_lines l WHERE l.tenant_id=$1 AND l.task_id=$3 AND l.po_item_id=i.id
		  AND l.final_result='PASS' AND l.unresolved_qty=0 AND l.ordered_qty=i.qty AND l.requested_qty=i.qty
		  AND l.qualified_qty>=i.qty AND l.approved_release_qty>=i.qty AND l.uom_code=i.uom_code
		  AND l.product_name=i.product_name AND l.spec=i.spec))
		 AND NOT EXISTS(SELECT 1 FROM quality_inspection_task_lines l WHERE l.tenant_id=$1 AND l.task_id=$3 AND NOT EXISTS(
		  SELECT 1 FROM purchase_order_items i WHERE i.tenant_id=$1 AND i.po_id=$2 AND i.id=l.po_item_id))`, tenant, po, task).Scan(&passed)
		if err != nil {
			return DeliveryQualityResult{}, err
		}
		if !passed {
			return qualityDenied("QUALITY_DELIVERY_NOT_PASSED", "货物存在未检、不合格或未通过复检的明细，不能交货"), nil
		}
	}
	return checkWarehouseDeliveryQuality(ctx, tx, tenant, po)
}

// Warehouse checks can discover a new defect after the factory passed. A
// disposition note (including RETURN or CONCESSION) is not proof that the
// physical stock was removed or became conforming. The old model has no
// return-stock link, so only a later, complete PASS for each affected receipt
// and product can clear a recorded quality problem.
func checkWarehouseDeliveryQuality(ctx context.Context, tx pgx.Tx, tenant, po int64) (DeliveryQualityResult, error) {
	type problem struct {
		receipt, item int64
		state         string
		after         time.Time
		qty           decimal.Decimal
	}
	rows, err := tx.Query(ctx, `SELECT coalesce(receipt_id,0),coalesce(po_item_id,0),status,inspected_at,greatest(inspected_qty,defect_qty)
	 FROM purchase_inspections WHERE tenant_id=$1 AND po_id=$2 AND (result<>'PASS' OR defect_qty>0 OR status<>'RESOLVED')
	 UNION ALL
	 SELECT coalesce(receipt_id,0),coalesce(po_item_id,0),status,reported_at,qty
	 FROM purchase_receipt_exceptions WHERE tenant_id=$1 AND po_id=$2
	 AND exception_type IN ('WRONG_PRODUCT','UNIT_MISMATCH','DAMAGE','QUALITY_DISPUTE','RETURN')`, tenant, po)
	if err != nil {
		return DeliveryQualityResult{}, err
	}
	problems := []problem{}
	for rows.Next() {
		var p problem
		if err = rows.Scan(&p.receipt, &p.item, &p.state, &p.after, &p.qty); err != nil {
			rows.Close()
			return DeliveryQualityResult{}, err
		}
		problems = append(problems, p)
	}
	rows.Close()
	if err = rows.Err(); err != nil {
		return DeliveryQualityResult{}, err
	}
	denied := qualityDenied("QUALITY_DELIVERY_WAREHOUSE_BLOCKED", "货物存在仓内质检不合格或质量异常，请先完成对应收货批次的复检并记录合格结果，不能交货")
	for _, p := range problems {
		if p.state != "RESOLVED" {
			return denied, nil
		}
		receipts, err := tx.Query(ctx, `SELECT r.id,sum(i.qty) FROM purchase_receipts r
		 JOIN purchase_receipt_items i ON i.tenant_id=r.tenant_id AND i.receipt_id=r.id
		 WHERE r.tenant_id=$1 AND r.po_id=$2 AND ($3=0 OR r.id=$3) AND ($4=0 OR i.po_item_id=$4)
		 GROUP BY r.id ORDER BY r.id`, tenant, po, p.receipt, p.item)
		if err != nil {
			return DeliveryQualityResult{}, err
		}
		type affectedReceipt struct {
			id  int64
			qty decimal.Decimal
		}
		affected := []affectedReceipt{}
		for receipts.Next() {
			var r affectedReceipt
			if err = receipts.Scan(&r.id, &r.qty); err != nil {
				receipts.Close()
				return DeliveryQualityResult{}, err
			}
			affected = append(affected, r)
		}
		receipts.Close()
		if err = receipts.Err(); err != nil {
			return DeliveryQualityResult{}, err
		}
		if len(affected) == 0 {
			return denied, nil
		}
		for _, r := range affected {
			var passed bool
			err = tx.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM purchase_inspections
			 WHERE tenant_id=$1 AND po_id=$2 AND receipt_id=$3 AND coalesce(po_item_id,0)=$4
			 AND result='PASS' AND status='RESOLVED' AND defect_qty=0 AND inspected_qty>0
			 AND inspected_qty>=$5::numeric AND inspected_at>$6)`, tenant, po, r.id, p.item, decimal.Max(r.qty, p.qty).String(), p.after).Scan(&passed)
			if err != nil {
				return DeliveryQualityResult{}, err
			}
			if !passed {
				return denied, nil
			}
		}
	}
	return DeliveryQualityResult{Allowed: true}, nil
}
