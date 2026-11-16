package app

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"sort"

	"github.com/jackc/pgx/v5"
	"github.com/sgao19/erp-go/pkg/apierr"
	"github.com/sgao19/erp-go/pkg/pgdb"
	"github.com/shopspring/decimal"
)

type HandoffBatch struct {
	BatchNo    int32             `json:"batchNo"`
	Title      string            `json:"title"`
	Quantities map[string]string `json:"quantities"`
}
type HandoffPlan struct {
	VersionID int64          `json:"versionId,string"`
	Revision  int64          `json:"revision"`
	Batches   []HandoffBatch `json:"batches"`
}

func (s *Service) handoffPlan(ctx context.Context, tenant int64, view ContractView) (HandoffPlan, error) {
	out := HandoffPlan{VersionID: view.Version.ID, Batches: []HandoffBatch{}}
	var raw []byte
	err := s.pool.QueryRow(ctx, `SELECT revision,batches FROM contract_handoff_plans WHERE tenant_id=$1 AND contract_version_id=$2`, tenant, view.Version.ID).Scan(&out.Revision, &raw)
	if errors.Is(err, pgx.ErrNoRows) {
		return out, nil
	}
	if err != nil {
		return out, err
	}
	err = json.Unmarshal(raw, &out.Batches)
	return out, err
}

// A handoff plan is an operational allocation, not an amendment of signed terms.
// It is version-bound and frozen by the same contract lock as Finance's release.
func (s *Service) saveHandoffPlan(ctx context.Context, tenant int64, view ContractView, cmd WorkflowCommand, op Operator) (HandoffPlan, error) {
	var plan HandoffPlan
	if err := json.Unmarshal(cmd.Data, &plan); err != nil {
		return plan, apierr.Invalid("EX_HANDOFF_PLAN", "请核对分批交接资料")
	}
	if plan.VersionID != view.Version.ID {
		return plan, apierr.Conflict("EX_HANDOFF_VERSION", "合同版本已变化，请刷新")
	}
	if _, err := allocatedShipments(view, plan.Batches); err != nil {
		return plan, err
	}
	err := pgdb.InTx(ctx, s.pool, func(ctx context.Context, tx pgx.Tx) error {
		var owner, latest int64
		var state string
		var released bool
		if err := tx.QueryRow(ctx, `SELECT sales_employee_id,status,condition_confirmed_at IS NOT NULL,(SELECT id FROM contract_versions WHERE tenant_id=$1 AND contract_id=$2 ORDER BY version_no DESC LIMIT 1) FROM contracts WHERE tenant_id=$1 AND id=$2 FOR UPDATE`, tenant, view.Contract.ID).Scan(&owner, &state, &released, &latest); err != nil {
			return err
		}
		if owner != op.ID {
			return apierr.Permission("EX_HANDOFF_OWNER", "只有负责销售可以维护交接分批")
		}
		if latest != plan.VersionID || released || view.Contract.EntrySource != "STANDARD" || (state != "DRAFT" && state != "REJECTED" && state != "PENDING_SIGN" && state != "EXECUTING") {
			return apierr.Conflict("EX_HANDOFF_FROZEN", "仅普通合同可在财务放行前维护当前版本的交接分批")
		}
		raw, _ := json.Marshal(plan.Batches)
		var revision int64
		if plan.Revision == 0 {
			err := tx.QueryRow(ctx, `INSERT INTO contract_handoff_plans(tenant_id,contract_version_id,batches,updated_by) VALUES($1,$2,$3,$4) ON CONFLICT DO NOTHING RETURNING revision`, tenant, plan.VersionID, raw, op.ID).Scan(&revision)
			if errors.Is(err, pgx.ErrNoRows) {
				return apierr.Conflict("EX_HANDOFF_REVISION", "交接分批已更新，请刷新")
			}
			if err != nil {
				return err
			}
		} else {
			err := tx.QueryRow(ctx, `UPDATE contract_handoff_plans SET batches=$3,revision=revision+1,updated_by=$4,updated_at=now() WHERE tenant_id=$1 AND contract_version_id=$2 AND revision=$5 RETURNING revision`, tenant, plan.VersionID, raw, op.ID, plan.Revision).Scan(&revision)
			if errors.Is(err, pgx.ErrNoRows) {
				return apierr.Conflict("EX_HANDOFF_REVISION", "交接分批已更新，请刷新")
			}
			if err != nil {
				return err
			}
		}
		plan.Revision = revision
		_, err := tx.Exec(ctx, `INSERT INTO contract_workflow_actions(tenant_id,contract_id,action,reason,actor_id,actor_name,data) VALUES($1,$2,'handoff_save',$3,$4,$5,$6)`, tenant, view.Contract.ID, cmd.Reason, op.ID, op.Name, raw)
		return err
	})
	return plan, err
}

func allocatedShipments(view ContractView, batches []HandoffBatch) ([]effectiveEventShipment, error) {
	if len(batches) == 0 || len(batches) > 100 {
		return nil, apierr.Invalid("EX_HANDOFF_BATCHES", "请填写 1 至 100 个运输批次")
	}
	goods := map[string]effectiveEventItem{}
	totals := map[string]decimal.Decimal{}
	numbers := map[int32]bool{}
	for _, item := range effectiveEvent(view).Items {
		goods[fmt.Sprint(item.ItemID)] = item
	}
	out := []effectiveEventShipment{}
	for _, batch := range batches {
		if batch.BatchNo <= 0 || numbers[batch.BatchNo] {
			return nil, apierr.Invalid("EX_HANDOFF_BATCH_NO", "批次编号必须为不重复的正整数")
		}
		numbers[batch.BatchNo] = true
		shipment := effectiveEventShipment{BatchNo: batch.BatchNo, ShipmentGroupKey: batch.Title, Currency: view.Version.Currency, FreightAmount: "0", PortOfLoading: view.Version.PortOfLoading, PortOfDischarge: view.Version.PortOfDischarge, AllocationStatus: "ALLOCATED", Items: []effectiveEventItem{}}
		for id, raw := range batch.Quantities {
			item, ok := goods[id]
			qty, err := decimal.NewFromString(raw)
			if !ok || err != nil || qty.IsNegative() || qty.Exponent() < -4 {
				return nil, apierr.Invalid("EX_HANDOFF_QUANTITY", "请填写合同明细对应的非负数量，最多四位小数")
			}
			if qty.IsZero() {
				continue
			}
			item.Qty = qty.String()
			shipment.Items = append(shipment.Items, item)
			totals[id] = totals[id].Add(qty)
		}
		if len(shipment.Items) == 0 {
			return nil, apierr.Invalid("EX_HANDOFF_EMPTY_BATCH", "每个批次至少分配一项货物")
		}
		sort.Slice(shipment.Items, func(i, j int) bool { return shipment.Items[i].LineNo < shipment.Items[j].LineNo })
		out = append(out, shipment)
	}
	for id, item := range goods {
		qty, err := decimal.NewFromString(item.Qty)
		if err != nil || !totals[id].Equal(qty) {
			return nil, apierr.Invalid("EX_HANDOFF_TOTAL", "各批次分配数量合计必须等于合同数量").WithMeta("product", item.ProductName, "qty", item.Qty)
		}
	}
	return out, nil
}

// Frozen quote snapshots are references only, never final execution suppliers.
func (s *Service) hydrateHandoffDetails(ctx context.Context, tenant int64, view ContractView, event *contractEffectiveEvent) error {
	var raw, sourceRaw []byte
	err := s.pool.QueryRow(ctx, `SELECT body,source_snapshot FROM customer_offers WHERE tenant_id=$1 AND contract_id=$2 AND confirmed_at IS NOT NULL`, tenant, view.Contract.ID).Scan(&raw, &sourceRaw)
	var body OfferBody
	var source OfferInquiry
	if err != nil && !errors.Is(err, pgx.ErrNoRows) {
		return err
	}
	if len(raw) > 0 {
		if err := json.Unmarshal(raw, &body); err != nil {
			return err
		}
		if err := json.Unmarshal(sourceRaw, &source); err != nil {
			return err
		}
	}
	for i := range event.Items {
		// Match the frozen accepted line by product and specification; do not rely
		// on a current editable inquiry or on candidate array position.
		for _, line := range body.Lines {
			if line.Product != event.Items[i].ProductName || offerSpecification(line.OfferProduct, source) != event.Items[i].Spec {
				continue
			}
			for _, selection := range body.CustomerSelections {
				if selection.ProductID == line.ID && selection.QuoteID == line.FactoryQuoteID {
					snapshot := productReference(selection.Quote, line.ID, line.Product)
					event.Items[i].PresaleReference = snapshot
					break
				}
			}
			if len(event.Items[i].PresaleReference) == 0 {
				for _, q := range source.Quotes {
					if q.ID == line.FactoryQuoteID {
						snapshot := productReference(q, line.ID, line.Product)
						event.Items[i].PresaleReference = snapshot
						break
					}
				}
			}
			break
		}
	}
	refs := body.CustomerLogistics
	plan, err := s.handoffPlan(ctx, tenant, view)
	if err != nil {
		return err
	}
	if len(plan.Batches) > 0 {
		event.Shipments, err = allocatedShipments(view, plan.Batches)
		if err != nil {
			return err
		}
	}
	if len(plan.Batches) == 0 && len(event.Shipments) > 1 {
		// Legacy batches have no per-line allocation: send one explicit pending
		// allocation task, instead of duplicating the full sale into every batch.
		legacy, _ := json.Marshal(event.Shipments)
		event.Shipments = []effectiveEventShipment{{BatchNo: 1, Currency: event.Currency, FreightAmount: "0", PortOfLoading: event.PortOfLoading, PortOfDischarge: event.PortOfDischarge, AllocationStatus: "PENDING_ALLOCATION", Remark: "原运输方案未记录逐批货物数量，请核对分批安排", PresaleReferences: legacy}}
	}
	for i := range event.Shipments {
		shipment := &event.Shipments[i]
		if len(shipment.PresaleReferences) == 0 && len(refs) > 0 {
			shipment.PresaleReferences, _ = json.Marshal(refs)
		}
		if shipment.AllocationStatus == "" {
			shipment.AllocationStatus = "ALLOCATED"
		}
		if shipment.Items == nil {
			shipment.Items = append([]effectiveEventItem{}, event.Items...)
		}
	}
	return nil
}

func productReference(q OfferSourceQuote, productID, productName string) json.RawMessage {
	raw, _ := json.Marshal(q)
	var snapshot map[string]any
	_ = json.Unmarshal(raw, &snapshot)
	snapshot["productId"] = productID
	snapshot["productName"] = productName
	result, _ := json.Marshal(snapshot)
	return result
}
